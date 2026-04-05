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
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/bookerbai/goclaw/internal/middleware"
)

const DefaultMemoryPath = "memory.json"

// ---------------------------------------------------------------------------
// Data structures
// ---------------------------------------------------------------------------

// ContextSection holds one summary section with update timestamp.
type ContextSection struct {
	Summary   string `json:"summary"`
	UpdatedAt string `json:"updatedAt"`
}

// UserContext groups user-related context sections.
type UserContext struct {
	WorkContext     ContextSection `json:"workContext"`
	PersonalContext ContextSection `json:"personalContext"`
	TopOfMind       ContextSection `json:"topOfMind"`
}

// HistoryContext groups history-related context sections.
type HistoryContext struct {
	RecentMonths       ContextSection `json:"recentMonths"`
	EarlierContext     ContextSection `json:"earlierContext"`
	LongTermBackground ContextSection `json:"longTermBackground"`
}

// MemoryFact is the persisted structured fact schema.
type MemoryFact struct {
	ID          string  `json:"id"`
	Content     string  `json:"content"`
	Category    string  `json:"category"`
	Confidence  float64 `json:"confidence"`
	CreatedAt   string  `json:"createdAt"`
	Source      string  `json:"source"`
	SourceError *string `json:"sourceError,omitempty"`
}

// Memory is the persisted memory document for a single agent scope.
// It matches the frontend/gateway memory schema.
type Memory struct {
	Version     string         `json:"version"`
	LastUpdated string         `json:"lastUpdated"`
	User        UserContext    `json:"user"`
	History     HistoryContext `json:"history"`
	Facts       []MemoryFact   `json:"facts"`
}

// newEmptyMemory returns an initialised Memory document with current timestamp.
func newEmptyMemory() *Memory {
	return &Memory{
		Version:     "1.0",
		LastUpdated: time.Now().UTC().Format(time.RFC3339),
		Facts:       []MemoryFact{},
	}
}

func cloneMemory(m *Memory) *Memory {
	if m == nil {
		return nil
	}
	out := *m
	out.Facts = append([]MemoryFact(nil), m.Facts...)
	return &out
}

// ---------------------------------------------------------------------------
// MemoryStore interface
// ---------------------------------------------------------------------------

// MemoryStore is the persistence abstraction used by MemoryMiddleware.
//
// Implementations must be safe for concurrent use — Load and Save can be
// called from multiple goroutines (the update queue worker + the agent goroutine).
type MemoryStore interface {
	// Load returns the current Memory document. Implementations should cache
	// with mtime-based invalidation to avoid redundant disk reads.
	Load() (*Memory, error)

	// Save atomically persists the Memory document.
	// Implementations must update LastUpdated before writing.
	Save(m *Memory) error

	// AddFact appends a new fact if it is not already present
	// (case-insensitive substring deduplication on content). Returns true if
	// the fact was added, false if it was considered a duplicate.
	AddFact(m *Memory, fact MemoryFact) bool

	// Deduplicate removes semantically duplicate facts from m.Facts in place.
	// The default implementation uses case-insensitive substring matching;
	// callers may replace this with an LLM-based deduplication pass.
	Deduplicate(m *Memory)
}

// ---------------------------------------------------------------------------
// JSONFileStore — default MemoryStore implementation
// ---------------------------------------------------------------------------

// JSONFileStore stores Memory as a JSON file with atomic rename-on-write.
//
// Atomic writes prevent partial reads: the document is written to a *.tmp
// sibling and then renamed into place, matching DeerFlow's FileMemoryStorage.
type JSONFileStore struct {
	path string

	mu       sync.RWMutex
	cache    *Memory
	cacheMod time.Time // mtime of the file when cache was populated
}

// NewJSONFileStore creates a JSONFileStore backed by the given file path.
// The directory must already exist; NewJSONFileStore does not create it.
func NewJSONFileStore(path string) *JSONFileStore {
	return &JSONFileStore{path: path}
}

// Load returns the Memory document, using a cached copy when the file has not
// changed since the last read (mtime comparison).
func (s *JSONFileStore) Load() (*Memory, error) {
	info, statErr := os.Stat(s.path)
	if statErr != nil && !os.IsNotExist(statErr) {
		return nil, fmt.Errorf("memory: stat %s: %w", s.path, statErr)
	}

	s.mu.RLock()
	if s.cache != nil {
		if statErr == nil && info.ModTime().Equal(s.cacheMod) {
			m := cloneMemory(s.cache)
			s.mu.RUnlock()
			return m, nil
		}
		if os.IsNotExist(statErr) && s.cacheMod.IsZero() {
			m := cloneMemory(s.cache)
			s.mu.RUnlock()
			return m, nil
		}
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()

	info, statErr = os.Stat(s.path)
	if os.IsNotExist(statErr) {
		m := newEmptyMemory()
		s.cache = cloneMemory(m)
		s.cacheMod = time.Time{}
		return m, nil
	}
	if statErr != nil {
		return nil, fmt.Errorf("memory: stat %s: %w", s.path, statErr)
	}

	if s.cache != nil && info.ModTime().Equal(s.cacheMod) {
		return cloneMemory(s.cache), nil
	}

	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil, fmt.Errorf("memory: read %s: %w", s.path, err)
	}

	m := newEmptyMemory()
	if err := json.Unmarshal(data, m); err != nil {
		return nil, fmt.Errorf("memory: unmarshal %s: %w", s.path, err)
	}

	s.cache = cloneMemory(m)
	s.cacheMod = info.ModTime()
	return m, nil
}

// Save atomically writes m to the backing file.
// It updates LastUpdated before writing and refreshes the in-memory cache.
func (s *JSONFileStore) Save(m *Memory) error {
	// TODO:
	// 1. Set m.LastUpdated = time.Now().UTC().Format(time.RFC3339).
	// 2. json.Marshal(m) → data.
	// 3. Write data to s.path + ".tmp" (os.WriteFile with 0o644).
	// 4. os.Rename(tmp, s.path) for atomic replacement.
	// 5. s.mu.Lock(); update s.cache = m; stat new file for s.cacheMod; s.mu.Unlock().
	// 6. Return any error encountered.

	m.LastUpdated = time.Now().UTC().Format(time.RFC3339)

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("memory: marshal: %w", err)
	}

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("memory: write tmp %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("memory: rename %s → %s: %w", tmp, s.path, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache = cloneMemory(m)
	if info, err := os.Stat(s.path); err == nil {
		s.cacheMod = info.ModTime()
	}
	return nil
}

// AddFact appends fact to m.Facts if no existing fact contains the same
// content as a case-insensitive substring. Returns true if fact was added.
func (s *JSONFileStore) AddFact(m *Memory, fact MemoryFact) bool {
	fact.Content = strings.TrimSpace(fact.Content)
	if fact.Content == "" {
		return false
	}
	lf := strings.ToLower(fact.Content)
	for _, f := range m.Facts {
		if strings.Contains(strings.ToLower(strings.TrimSpace(f.Content)), lf) {
			return false
		}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if strings.TrimSpace(fact.ID) == "" {
		fact.ID = newFactID()
	}
	if strings.TrimSpace(fact.Category) == "" {
		fact.Category = "context"
	}
	if fact.Confidence <= 0 {
		fact.Confidence = 0.8
	}
	if strings.TrimSpace(fact.CreatedAt) == "" {
		fact.CreatedAt = now
	}
	if strings.TrimSpace(fact.Source) == "" {
		fact.Source = "unknown"
	}

	m.Facts = append(m.Facts, fact)
	return true
}

// Deduplicate removes redundant facts from m.Facts.
// If one fact's content fully contains another fact's content (case-insensitive),
// the shorter one is considered dominated and removed.
func (s *JSONFileStore) Deduplicate(m *Memory) {
	if len(m.Facts) <= 1 {
		return
	}

	dominated := make([]bool, len(m.Facts))
	for i := range m.Facts {
		li := strings.ToLower(strings.TrimSpace(m.Facts[i].Content))
		if li == "" {
			dominated[i] = true
			continue
		}
		for j := range m.Facts {
			if i == j || dominated[j] {
				continue
			}
			lj := strings.ToLower(strings.TrimSpace(m.Facts[j].Content))
			if lj == "" {
				dominated[j] = true
				continue
			}
			if strings.Contains(li, lj) && li != lj {
				dominated[j] = true
			}
		}
	}

	kept := m.Facts[:0]
	for i, f := range m.Facts {
		if !dominated[i] {
			kept = append(kept, f)
		}
	}
	m.Facts = kept
}

// ---------------------------------------------------------------------------
// Correction detection helpers (mirrors DeerFlow detect_correction)
// ---------------------------------------------------------------------------

var correctionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bthat(?:'s| is) (?:wrong|incorrect)\b`),
	regexp.MustCompile(`(?i)\byou misunderstood\b`),
	regexp.MustCompile(`(?i)\btry again\b`),
	regexp.MustCompile(`(?i)\bredo\b`),
	regexp.MustCompile(`不对`),
	regexp.MustCompile(`你理解错了`),
	regexp.MustCompile(`重试`),
}

var memorySentenceSplitRe = regexp.MustCompile(`[\n。！？!?；;]+`)

// detectCorrection returns true if any recent human message contains an
// explicit correction signal. It inspects at most the last 6 messages.
func detectCorrection(messages []map[string]any) bool {
	// TODO:
	// 1. Filter messages to the last 6 entries where role == "human".
	// 2. For each such message, extract content as string.
	// 3. Test each correctionPatterns regexp against the content.
	// 4. Return true on first match, false if none match.

	var recent []map[string]any
	for _, m := range messages {
		if m["role"] == "human" {
			recent = append(recent, m)
		}
	}
	if len(recent) > 6 {
		recent = recent[len(recent)-6:]
	}
	for _, m := range recent {
		content, _ := m["content"].(string)
		for _, re := range correctionPatterns {
			if re.MatchString(content) {
				return true
			}
		}
	}
	return false
}

// filterMessagesForMemory removes intermediate tool messages and AI messages
// that contain tool_calls, keeping only human turns and final AI responses.
// This mirrors DeerFlow's _filter_messages_for_memory.
func filterMessagesForMemory(messages []map[string]any) []map[string]any {
	// TODO:
	// 1. Iterate over messages.
	// 2. role == "tool": skip.
	// 3. role == "assistant" with non-empty tool_calls field: skip.
	// 4. role == "human": include (strip <uploaded_files>…</uploaded_files> blocks).
	// 5. role == "assistant" with no tool_calls: include.
	// 6. Return filtered list.

	var filtered []map[string]any
	for _, msg := range messages {
		role, _ := msg["role"].(string)
		switch role {
		case "tool":
			// Intermediate tool result — skip.
			continue
		case "assistant":
			if calls, ok := msg["tool_calls"]; ok && calls != nil {
				// Intermediate step with pending tool calls — skip.
				continue
			}
			filtered = append(filtered, msg)
		case "human":
			// Strip ephemeral <uploaded_files> blocks before persisting.
			content, _ := msg["content"].(string)
			if strings.Contains(content, "<uploaded_files>") {
				// TODO: apply regexp to strip the block; skip message if nothing remains.
				cleaned := regexp.MustCompile(`(?s)<uploaded_files>.*?</uploaded_files>\n*`).ReplaceAllString(content, "")
				cleaned = strings.TrimSpace(cleaned)
				if cleaned == "" {
					continue
				}
				msg = copyMsg(msg, cleaned)
			}
			filtered = append(filtered, msg)
		}
	}
	return filtered
}

// copyMsg returns a shallow copy of msg with content overridden.
func copyMsg(msg map[string]any, content string) map[string]any {
	out := make(map[string]any, len(msg))
	for k, v := range msg {
		out[k] = v
	}
	out["content"] = content
	return out
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

	// DebounceDelay is the wait after the last Add before processing begins.
	// Default: 30 seconds, matching DeerFlow's debounce_seconds config.
	DebounceDelay time.Duration
}

var (
	globalQueue     *UpdateQueue
	globalQueueOnce sync.Once
	factIDSeq       uint64
)

func newFactID() string {
	return fmt.Sprintf("fact_%d_%d", time.Now().UnixNano(), atomic.AddUint64(&factIDSeq, 1))
}

// GetGlobalQueue returns the process-wide UpdateQueue singleton.
// The queue is bound to the provided memoryPath on first initialization.
func GetGlobalQueue(memoryPath string) *UpdateQueue {
	globalQueueOnce.Do(func() {
		store := NewJSONFileStore(memoryPath)
		globalQueue = &UpdateQueue{
			entries:       make(map[string]*updateEntry),
			store:         store,
			DebounceDelay: 30 * time.Second,
			maxFacts:      100,
		}
	})
	return globalQueue
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
	// TODO:
	// 1. q.mu.Lock().
	// 2. Merge correctionDetected with any existing entry for threadID.
	// 3. Replace q.entries[threadID] with a new updateEntry.
	// 4. Reset the debounce timer: cancel existing, start new with q.DebounceDelay.
	// 5. q.mu.Unlock().

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
			log.Printf("[MemoryMiddleware] extract failed thread=%s err=%v", entry.threadID, err)
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

	// Try full LLM memory update (User/History contexts + facts) if updater is configured.
	if q.updater != nil {
		ctx := context.Background()
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
				log.Printf("[MemoryMiddleware] saved memory update for thread=%s (contexts + %d newFacts)",
					entry.threadID, len(update.NewFacts))
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
	log.Printf("[MemoryMiddleware] saved %d fact(s) for thread=%s", len(extractedFacts), entry.threadID)
	return nil
}

// sortFactsByConfidence sorts facts by confidence (descending) for retention.
func sortFactsByConfidence(facts []MemoryFact) []MemoryFact {
	sorted := make([]MemoryFact, len(facts))
	copy(sorted, facts)
	// Simple bubble sort for stability
	for i := 0; i < len(sorted)-1; i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].Confidence > sorted[i].Confidence {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	return sorted
}

func deriveFactsFromMessages(messages []map[string]any, correctionDetected bool) []Fact {
	facts := make([]Fact, 0, 8)
	seen := make(map[string]struct{})
	add := func(content string, category FactCategory, confidence float64) {
		content = strings.TrimSpace(content)
		if content == "" {
			return
		}
		key := strings.ToLower(content)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		if string(category) == "" {
			category = CategoryContext
		}
		if confidence <= 0 {
			confidence = 0.8
		}
		facts = append(facts, Fact{Content: content, Category: category, Confidence: confidence})
	}

	if correctionDetected {
		add("User corrected a previous assistant response.", CategoryCorrection, 0.95)
	}

	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		role, _ := msg["role"].(string)
		if role != "human" {
			continue
		}
		content, _ := msg["content"].(string)
		for _, seg := range memorySentenceSplitRe.Split(content, -1) {
			seg = strings.TrimSpace(seg)
			if seg == "" || len(seg) < 6 || len(seg) > 240 {
				continue
			}
			if strings.Contains(seg, "?") || strings.Contains(seg, "？") {
				continue
			}
			add(seg, CategoryContext, 0.8)
			if len(facts) >= 8 {
				return facts
			}
		}
	}
	return facts
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

// Before loads persisted facts and injects them into the system prompt.
//
// Implementation:
//  1. Load Memory from store; on error log and return nil (non-fatal).
//  2. Collect up to MaxFactsToInject facts from m.Facts (most recent last).
//  3. Append them to state.MemoryFacts.
//  4. Prepend a formatted "<memory_facts>…</memory_facts>" block to the
//     first system message in state.Messages (insert one if absent).
func (m *MemoryMiddleware) Before(ctx context.Context, state *middleware.State) error {
	// TODO:
	// 1. mem, err := m.store.Load()
	//    if err != nil { log.Printf("memory load error: %v", err); return nil }
	// 2. facts := mem.Facts; if len(facts) > MaxFactsToInject { facts = facts[len-15:] }
	// 3. state.MemoryFacts = facts
	// 4. Build the injection block:
	//    "<memory_facts>\n" + strings.Join(facts, "\n") + "\n</memory_facts>"
	// 5. Find the first message with role=="system" in state.Messages.
	//    If found, prepend the block to its "content".
	//    If not found, prepend a new system message to state.Messages.

	mem, err := m.store.Load()
	if err != nil {
		// Non-fatal: the agent can proceed without memory.
		log.Printf("[MemoryMiddleware] load failed: %v", err)
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

func selectFactsForInjection(allFacts []MemoryFact, maxFacts, maxTokens int) []MemoryFact {
	if len(allFacts) == 0 {
		return nil
	}
	if maxFacts <= 0 {
		maxFacts = len(allFacts)
	}
	facts := allFacts
	if len(facts) > maxFacts {
		facts = facts[len(facts)-maxFacts:]
	}
	if maxTokens <= 0 {
		return append([]MemoryFact(nil), facts...)
	}

	selectedRev := make([]MemoryFact, 0, len(facts))
	used := 0
	for i := len(facts) - 1; i >= 0; i-- {
		c := strings.TrimSpace(facts[i].Content)
		if c == "" {
			continue
		}
		tokens := estimateTokenCount(c)
		if tokens <= 0 {
			continue
		}
		if used+tokens > maxTokens {
			if len(selectedRev) == 0 {
				selectedRev = append(selectedRev, facts[i])
			}
			break
		}
		selectedRev = append(selectedRev, facts[i])
		used += tokens
	}

	out := make([]MemoryFact, 0, len(selectedRev))
	for i := len(selectedRev) - 1; i >= 0; i-- {
		out = append(out, selectedRev[i])
	}
	return out
}

// formatMemoryForInjection formats the complete memory structure for injection.
// It includes User Context, History, and Facts in a structured XML format.
// This mirrors DeerFlow's format_memory_for_injection() from prompt.py.
func formatMemoryForInjection(mem *Memory, factContents []string) string {
	if mem == nil {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("<memory>\n")

	hasContent := false

	// User Context section
	userHasContent := mem.User.WorkContext.Summary != "" ||
		mem.User.PersonalContext.Summary != "" ||
		mem.User.TopOfMind.Summary != ""

	if userHasContent {
		hasContent = true
		sb.WriteString("User Context:\n")
		if mem.User.WorkContext.Summary != "" {
			sb.WriteString("- Work: ")
			sb.WriteString(mem.User.WorkContext.Summary)
			sb.WriteString("\n")
		}
		if mem.User.PersonalContext.Summary != "" {
			sb.WriteString("- Personal: ")
			sb.WriteString(mem.User.PersonalContext.Summary)
			sb.WriteString("\n")
		}
		if mem.User.TopOfMind.Summary != "" {
			sb.WriteString("- Current Focus: ")
			sb.WriteString(mem.User.TopOfMind.Summary)
			sb.WriteString("\n")
		}
	}

	// History section
	historyHasContent := mem.History.RecentMonths.Summary != "" ||
		mem.History.EarlierContext.Summary != "" ||
		mem.History.LongTermBackground.Summary != ""

	if historyHasContent {
		hasContent = true
		if userHasContent {
			sb.WriteString("\n")
		}
		sb.WriteString("History:\n")
		if mem.History.RecentMonths.Summary != "" {
			sb.WriteString("- Recent: ")
			sb.WriteString(mem.History.RecentMonths.Summary)
			sb.WriteString("\n")
		}
		if mem.History.EarlierContext.Summary != "" {
			sb.WriteString("- Earlier: ")
			sb.WriteString(mem.History.EarlierContext.Summary)
			sb.WriteString("\n")
		}
		if mem.History.LongTermBackground.Summary != "" {
			sb.WriteString("- Long-term: ")
			sb.WriteString(mem.History.LongTermBackground.Summary)
			sb.WriteString("\n")
		}
	}

	// Facts section
	if len(factContents) > 0 {
		hasContent = true
		if userHasContent || historyHasContent {
			sb.WriteString("\n")
		}
		sb.WriteString("Facts:\n")
		for _, fact := range factContents {
			if fact = strings.TrimSpace(fact); fact != "" {
				sb.WriteString("- ")
				sb.WriteString(fact)
				sb.WriteString("\n")
			}
		}
	}

	sb.WriteString("</memory>")

	if !hasContent {
		return ""
	}

	return sb.String()
}

// estimateTokenCount estimates the token count for a given text.
// This uses an improved algorithm that considers both ASCII and non-ASCII characters:
// - ASCII characters (English, numbers, symbols): ~4 chars per token
// - Non-ASCII characters (Chinese, Japanese, etc.): ~1.5 chars per token
// This provides better accuracy for mixed-language content without requiring tiktoken.
func estimateTokenCount(text string) int {
	text = strings.TrimSpace(text)
	if len(text) == 0 {
		return 0
	}

	asciiCount := 0
	nonAsciiCount := 0

	for _, r := range text {
		if r < 128 {
			asciiCount++
		} else {
			nonAsciiCount++
		}
	}

	// ASCII: ~4 chars per token (GPT tokenizer behavior)
	// Non-ASCII: ~1.5 chars per token (CJK characters typically use more tokens)
	asciiTokens := (asciiCount + 3) / 4
	nonAsciiTokens := (nonAsciiCount*2 + 2) / 3 // *2/3 ≈ /1.5

	return asciiTokens + nonAsciiTokens
}

// After filters the conversation and prepares for memory update.
// This is called after each model invocation, but the actual queue operation
// is deferred to AfterAgent to ensure it runs only once per agent run.
func (m *MemoryMiddleware) After(ctx context.Context, state *middleware.State, response *middleware.Response) error {
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
		log.Printf("[MemoryMiddleware] queue is nil, skip enqueue thread=%s", state.ThreadID)
		return nil
	}

	correction := detectCorrection(filtered)
	m.queue.Add(state.ThreadID, filtered, m.agentName, correction)
	return nil
}
