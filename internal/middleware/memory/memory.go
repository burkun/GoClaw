// Package memory implements the MemoryMiddleware for GoClaw.
//
// This mirrors DeerFlow's MemoryMiddleware / FileMemoryStorage pattern:
//   - Before(): loads persisted facts and injects them (≤15) into the system prompt.
//   - After():  filters the conversation, detects user corrections, and enqueues
//     the result for asynchronous LLM-based fact extraction (30 s debounce).
//
// Storage format (JSON):
//
//	{
//	  "version": "1.0",
//	  "lastUpdated": "<RFC3339>",
//	  "facts": [
//	    {
//	      "id": "fact_xxx",
//	      "content": "...",
//	      "category": "context",
//	      "confidence": 0.9,
//	      "createdAt": "<RFC3339>",
//	      "source": "thread_id"
//	    }
//	  ]
//	}
//
// The queue is a singleton goroutine that drains entries after a configurable
// debounce window (default 30 s). Only the latest entry per thread_id is kept
// (replace-on-add semantics), matching DeerFlow's deduplication behaviour.
package memory

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bookerbai/goclaw/internal/logging"
	"github.com/bookerbai/goclaw/internal/middleware"
)

const DefaultMemoryPath = "memory.json"

var (
	globalQueue     *UpdateQueue
	globalQueueOnce sync.Once
	factIDSeq       uint64
)

func newFactID() string {
	return fmt.Sprintf("fact_%d_%d", time.Now().UnixNano(), atomic.AddUint64(&factIDSeq, 1))
}

// ---------------------------------------------------------------------------
// Memory update queue (debounced, singleton)
// ---------------------------------------------------------------------------

// updateEntry is a pending memory update for a single thread.
type updateEntry struct {
	threadID           string
	messages           []map[string]any
	agentName          string
	correctionDetected bool
	queuedAt           time.Time
}

// UpdateQueue debounces memory updates across concurrent agent turns.
// Only the latest entry per thread_id is retained (replace-on-add).
// Processing fires after DebounceDelay has elapsed without new entries.
type UpdateQueue struct {
	mu        sync.Mutex
	entries   map[string]*updateEntry // key: threadID
	timer     *time.Timer
	store     MemoryStore
	extractor FactExtractor
	updater   *LLMMemoryUpdater // Full memory updater for User/History contexts + facts
	maxFacts  int
	ctx       context.Context    // Context for cancellation
	cancel    context.CancelFunc // Cancel function for shutdown

	// DebounceDelay is the wait after the last Add before processing begins.
	// Default: 30 seconds, matching DeerFlow's debounce_seconds config.
	DebounceDelay time.Duration
}

// GetGlobalQueue returns the process-wide UpdateQueue singleton.
// The queue is bound to the provided memoryPath on first initialization.
func GetGlobalQueue(memoryPath string) *UpdateQueue {
	globalQueueOnce.Do(func() {
		store := NewJSONFileStore(memoryPath)
		ctx, cancel := context.WithCancel(context.Background())
		globalQueue = &UpdateQueue{
			entries:       make(map[string]*updateEntry),
			store:         store,
			DebounceDelay: 30 * time.Second,
			maxFacts:      100,
			ctx:           ctx,
			cancel:        cancel,
		}
	})
	return globalQueue
}

// Shutdown stops the update queue and cancels all pending operations.
func (q *UpdateQueue) Shutdown() {
	if q == nil || q.cancel == nil {
		return
	}
	q.cancel()
}

// SetExtractor sets or replaces the fact extractor used by the queue.
func (q *UpdateQueue) SetExtractor(extractor FactExtractor) {
	if q == nil {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.extractor = extractor
}

// SetUpdater sets or replaces the full memory updater used by the queue.
// The updater handles User/History context summaries in addition to facts.
func (q *UpdateQueue) SetUpdater(updater *LLMMemoryUpdater) {
	if q == nil {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.updater = updater
}

// SetMaxFacts sets the max number of stored facts retained after each update.
// Values <= 0 disable truncation.
func (q *UpdateQueue) SetMaxFacts(limit int) {
	if q == nil {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.maxFacts = limit
}

// Add enqueues or replaces the pending memory update for threadID.
// If the debounce timer is running it is reset.
func (q *UpdateQueue) Add(threadID string, messages []map[string]any, agentName string, correctionDetected bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	existing := q.entries[threadID]
	merged := correctionDetected
	if existing != nil {
		merged = merged || existing.correctionDetected
	}

	q.entries[threadID] = &updateEntry{
		threadID:           threadID,
		messages:           messages,
		agentName:          agentName,
		correctionDetected: merged,
		queuedAt:           time.Now(),
	}

	if q.timer != nil {
		q.timer.Stop()
	}
	q.timer = time.AfterFunc(q.DebounceDelay, q.process)
}

// process drains all pending entries and extracts/saves facts for each.
// It runs in the background goroutine started by time.AfterFunc.
func (q *UpdateQueue) process() {
	q.mu.Lock()
	snap := q.entries
	q.entries = make(map[string]*updateEntry)
	q.timer = nil
	q.mu.Unlock()

	for _, entry := range snap {
		if err := q.extractAndSave(entry); err != nil {
			logging.Error("[MemoryMiddleware] extract failed", "thread", entry.threadID, "error", err)
		}
	}
}

func (q *UpdateQueue) extractAndSave(entry *updateEntry) error {
	if q == nil || q.store == nil || entry == nil {
		return nil
	}
	mem, err := q.store.Load()
	if err != nil {
		return err
	}

	// Use queue's context for cancellation support
	ctx := q.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	// Try full LLM memory update (User/History contexts + facts) if updater is configured.
	if q.updater != nil {
		update, extractErr := q.updater.ExtractMemoryUpdate(ctx, mem, entry.messages, entry.correctionDetected)
		if extractErr == nil && update != nil && update.HasUpdates() {
			if ApplyUpdates(mem, update, entry.threadID) {
				q.store.Deduplicate(mem)
				if q.maxFacts > 0 && len(mem.Facts) > q.maxFacts {
					// Sort by confidence and keep top N
					mem.Facts = sortFactsByConfidence(mem.Facts)
					mem.Facts = mem.Facts[:q.maxFacts]
				}
				if err := q.store.Save(mem); err != nil {
					return err
				}
				logging.Info("[MemoryMiddleware] saved memory update",
					"thread", entry.threadID,
					"new_facts", len(update.NewFacts))
				return nil
			}
		}
	}

	// Fallback to legacy fact-only extraction.
	var extractedFacts []Fact

	// Try LLM extraction if extractor is configured.
	if q.extractor != nil {
		extracted, extractErr := q.extractor.Extract(entry.messages, entry.correctionDetected)
		if extractErr == nil && len(extracted) > 0 {
			extractedFacts = extracted
		}
	}

	// Fallback to rule-based extraction if LLM produced nothing.
	if len(extractedFacts) == 0 {
		extractedFacts = deriveFactsFromMessages(entry.messages, entry.correctionDetected)
	}

	if len(extractedFacts) == 0 {
		return nil
	}

	addedAny := false
	for _, f := range extractedFacts {
		content := strings.TrimSpace(f.Content)
		if content == "" {
			continue
		}
		confidence := f.Confidence
		if confidence > 0 && confidence < 0.7 {
			continue
		}
		if confidence <= 0 {
			confidence = 0.8
		}
		persisted := MemoryFact{
			Content:    content,
			Category:   string(f.Category),
			Confidence: confidence,
			Source:     entry.threadID,
		}
		if q.store.AddFact(mem, persisted) {
			addedAny = true
		}
	}
	if !addedAny {
		return nil
	}

	q.store.Deduplicate(mem)
	if q.maxFacts > 0 && len(mem.Facts) > q.maxFacts {
		mem.Facts = sortFactsByConfidence(mem.Facts)
		mem.Facts = mem.Facts[:q.maxFacts]
	}
	if err := q.store.Save(mem); err != nil {
		return err
	}
	logging.Info("[MemoryMiddleware] saved facts",
		"count", len(extractedFacts),
		"thread", entry.threadID)
	return nil
}

// ---------------------------------------------------------------------------
// MemoryMiddleware — implements middleware.Middleware
// ---------------------------------------------------------------------------

// MaxFactsToInject is the default maximum number of facts injected into the
// system prompt per turn, matching DeerFlow's "previous 15 facts" behaviour.
const MaxFactsToInject = 15

// MemoryMiddlewareConfig controls runtime injection behavior.
type MemoryMiddlewareConfig struct {
	InjectionEnabled   bool
	MaxFactsToInject   int
	MaxInjectionTokens int
}

func defaultMemoryMiddlewareConfig() MemoryMiddlewareConfig {
	return MemoryMiddlewareConfig{
		InjectionEnabled:   true,
		MaxFactsToInject:   MaxFactsToInject,
		MaxInjectionTokens: 2000,
	}
}

type MemoryMiddlewareOption func(*MemoryMiddlewareConfig)

func WithInjectionEnabled(enabled bool) MemoryMiddlewareOption {
	return func(cfg *MemoryMiddlewareConfig) { cfg.InjectionEnabled = enabled }
}

func WithMaxFactsToInject(maxFacts int) MemoryMiddlewareOption {
	return func(cfg *MemoryMiddlewareConfig) {
		if maxFacts > 0 {
			cfg.MaxFactsToInject = maxFacts
		}
	}
}

func WithMaxInjectionTokens(maxTokens int) MemoryMiddlewareOption {
	return func(cfg *MemoryMiddlewareConfig) {
		if maxTokens > 0 {
			cfg.MaxInjectionTokens = maxTokens
		}
	}
}

// MemoryMiddleware injects persisted facts before model invocation and queues
// conversation updates for asynchronous extraction after model completion.
type MemoryMiddleware struct {
	middleware.MiddlewareWrapper
	store     MemoryStore
	queue     *UpdateQueue
	agentName string // empty = global memory scope
	cfg       MemoryMiddlewareConfig
}

// NewMemoryMiddleware constructs a MemoryMiddleware using the provided store
// and update queue. agentName scopes the memory file (empty = shared/global).
func NewMemoryMiddleware(store MemoryStore, queue *UpdateQueue, agentName string, opts ...MemoryMiddlewareOption) *MemoryMiddleware {
	cfg := defaultMemoryMiddlewareConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return &MemoryMiddleware{store: store, queue: queue, agentName: agentName, cfg: cfg}
}

// Name implements middleware.Middleware.
func (m *MemoryMiddleware) Name() string { return "MemoryMiddleware" }

// BeforeModel loads persisted facts and injects them into the system prompt.
func (m *MemoryMiddleware) BeforeModel(ctx context.Context, state *middleware.State) error {
	mem, err := m.store.Load()
	if err != nil {
		// Non-fatal: the agent can proceed without memory.
		logging.Error("[MemoryMiddleware] load failed", "error", err)
		return nil
	}

	if !m.cfg.InjectionEnabled {
		state.MemoryFacts = nil
		return nil
	}

	facts := selectFactsForInjection(mem.Facts, m.cfg.MaxFactsToInject, m.cfg.MaxInjectionTokens)
	state.MemoryFacts = make([]string, 0, len(facts))
	for _, f := range facts {
		if c := strings.TrimSpace(f.Content); c != "" {
			state.MemoryFacts = append(state.MemoryFacts, c)
		}
	}

	// Format complete memory injection with User Context, History, and Facts
	block := formatMemoryForInjection(mem, state.MemoryFacts)
	if block == "" {
		return nil
	}

	// Find or create the system message.
	for i, msg := range state.Messages {
		if msg["role"] == "system" {
			existing, _ := msg["content"].(string)
			state.Messages[i]["content"] = block + "\n\n" + existing
			return nil
		}
	}
	// No system message found — prepend one.
	sysMsg := map[string]any{"role": "system", "content": block}
	state.Messages = append([]map[string]any{sysMsg}, state.Messages...)
	return nil
}

// AfterModel filters the conversation and prepares for memory update.
// This is called after each model invocation, but the actual queue operation
// is deferred to AfterAgent to ensure it runs only once per agent run.
func (m *MemoryMiddleware) AfterModel(ctx context.Context, state *middleware.State, response *middleware.Response) error {
	_ = ctx
	_ = response
	// No-op: memory update is handled in AfterAgent.
	return nil
}

// AfterAgent queues the conversation for asynchronous memory update at the end
// of the agent run. This mirrors DeerFlow's MemoryMiddleware which uses after_agent.
func (m *MemoryMiddleware) AfterAgent(_ context.Context, state *middleware.State, _ *middleware.Response) error {
	filtered := filterMessagesForMemory(state.Messages)

	hasHuman := false
	hasAssistant := false
	for _, msg := range filtered {
		role, _ := msg["role"].(string)
		if role == "human" {
			hasHuman = true
		}
		if role == "assistant" {
			hasAssistant = true
		}
	}
	if !hasHuman || !hasAssistant {
		return nil
	}
	if m.queue == nil {
		logging.Warn("[MemoryMiddleware] queue is nil, skip enqueue", "thread", state.ThreadID)
		return nil
	}

	correction := detectCorrection(filtered)
	m.queue.Add(state.ThreadID, filtered, m.agentName, correction)
	return nil
}
