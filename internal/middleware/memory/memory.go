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
//	  "version":     "1.0",
//	  "lastUpdated": "<RFC3339>",
//	  "facts":       ["fact1", "fact2", …]
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
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/bookerbai/goclaw/internal/middleware"
)

// ---------------------------------------------------------------------------
// Data structures
// ---------------------------------------------------------------------------

// Memory is the persisted memory document for a single agent scope.
//
// Facts is the primary store — a flat list of short factual statements
// extracted from past conversations (e.g. "User prefers Go over Python").
// UserContext holds a free-form narrative summary updated alongside Facts.
type Memory struct {
	// Version is the document schema version for forward-compatibility checks.
	Version string `json:"version"`

	// LastUpdated is the RFC3339 timestamp of the most recent save.
	LastUpdated string `json:"lastUpdated"`

	// Facts is the deduplicated list of extracted factual statements.
	// Length is bounded at runtime; the injector uses at most the last 15.
	Facts []string `json:"facts"`

	// UserContext is an optional free-form narrative that provides broader
	// context than individual facts (e.g. a paragraph about the user's project).
	UserContext string `json:"userContext,omitempty"`
}

// newEmptyMemory returns an initialised Memory document with current timestamp.
func newEmptyMemory() *Memory {
	return &Memory{
		Version:     "1.0",
		LastUpdated: time.Now().UTC().Format(time.RFC3339),
		Facts:       []string{},
	}
}

func cloneMemory(m *Memory) *Memory {
	if m == nil {
		return nil
	}
	out := *m
	out.Facts = append([]string(nil), m.Facts...)
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

	// AddFact appends a new factual statement if it is not already present
	// (case-insensitive substring deduplication). Returns true if the fact
	// was added, false if it was considered a duplicate.
	AddFact(m *Memory, fact string) bool

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

// AddFact appends fact to m.Facts if no existing fact contains fact as a
// case-insensitive substring. Returns true if fact was added.
func (s *JSONFileStore) AddFact(m *Memory, fact string) bool {
	// TODO:
	// 1. Normalise fact: strings.TrimSpace(fact). Return false if empty.
	// 2. lf := strings.ToLower(fact).
	// 3. For each existing f in m.Facts: if strings.Contains(strings.ToLower(f), lf), return false.
	// 4. m.Facts = append(m.Facts, fact); return true.

	fact = strings.TrimSpace(fact)
	if fact == "" {
		return false
	}
	lf := strings.ToLower(fact)
	for _, f := range m.Facts {
		if strings.Contains(strings.ToLower(f), lf) {
			return false
		}
	}
	m.Facts = append(m.Facts, fact)
	return true
}

// Deduplicate removes redundant facts from m.Facts.
//
// The algorithm: for each pair (i, j), if facts[i] is a substring of facts[j]
// (case-insensitive), facts[i] is considered dominated by facts[j] and removed.
// This is O(n²) but n is bounded at ~100 in practice.
func (s *JSONFileStore) Deduplicate(m *Memory) {
	// TODO:
	// 1. Build a dominated set: for each pair (i,j) where i≠j,
	//    if strings.Contains(lower(facts[j]), lower(facts[i])), mark i as dominated.
	// 2. Rebuild m.Facts keeping only non-dominated entries.

	if len(m.Facts) <= 1 {
		return
	}

	dominated := make([]bool, len(m.Facts))
	for i := range m.Facts {
		li := strings.ToLower(m.Facts[i])
		for j := range m.Facts {
			if i == j || dominated[j] {
				continue
			}
			lj := strings.ToLower(m.Facts[j])
			// If facts[j] fully contains facts[i], facts[i] is more specific
			// and is dominated by the broader facts[j] — keep facts[i] instead.
			// If facts[i] fully contains facts[j], facts[j] is the substring/dominated.
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

	// DebounceDelay is the wait after the last Add before processing begins.
	// Default: 30 seconds, matching DeerFlow's debounce_seconds config.
	DebounceDelay time.Duration
}

var (
	globalQueue     *UpdateQueue
	globalQueueOnce sync.Once
)

// GetGlobalQueue returns the process-wide UpdateQueue singleton.
// The store is a JSONFileStore backed by dataDir/memory.json.
func GetGlobalQueue(dataDir string) *UpdateQueue {
	globalQueueOnce.Do(func() {
		path := filepath.Join(dataDir, "memory.json")
		store := NewJSONFileStore(path)
		globalQueue = &UpdateQueue{
			entries:       make(map[string]*updateEntry),
			store:         store,
			DebounceDelay: 30 * time.Second,
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

	var facts []string

	// Try LLM extraction if extractor is configured.
	if q.extractor != nil {
		extracted, extractErr := q.extractor.Extract(entry.messages, entry.correctionDetected)
		if extractErr == nil && len(extracted) > 0 {
			for _, f := range extracted {
				if f.Confidence >= 0.7 {
					facts = append(facts, f.Content)
				}
			}
		}
	}

	// Fallback to rule-based extraction if LLM produced nothing.
	if len(facts) == 0 {
		facts = deriveFactsFromMessages(entry.messages, entry.correctionDetected)
	}

	if len(facts) == 0 {
		return nil
	}

	addedAny := false
	for _, f := range facts {
		if q.store.AddFact(mem, f) {
			addedAny = true
		}
	}
	if !addedAny {
		return nil
	}

	q.store.Deduplicate(mem)
	if err := q.store.Save(mem); err != nil {
		return err
	}
	log.Printf("[MemoryMiddleware] saved %d fact(s) for thread=%s", len(facts), entry.threadID)
	return nil
}

func deriveFactsFromMessages(messages []map[string]any, correctionDetected bool) []string {
	facts := make([]string, 0, 8)
	seen := make(map[string]struct{})
	add := func(f string) {
		f = strings.TrimSpace(f)
		if f == "" {
			return
		}
		key := strings.ToLower(f)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		facts = append(facts, f)
	}

	if correctionDetected {
		add("User corrected a previous assistant response.")
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
			add(seg)
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

// MaxFactsToInject is the maximum number of facts injected into the system
// prompt per turn, matching DeerFlow's "previous 15 facts" behaviour.
const MaxFactsToInject = 15

// MemoryMiddleware injects persisted facts before model invocation and queues
// conversation updates for asynchronous extraction after model completion.
type MemoryMiddleware struct {
	middleware.MiddlewareWrapper
	store     MemoryStore
	queue     *UpdateQueue
	agentName string // empty = global memory scope
}

// NewMemoryMiddleware constructs a MemoryMiddleware using the provided store
// and update queue. agentName scopes the memory file (empty = shared/global).
func NewMemoryMiddleware(store MemoryStore, queue *UpdateQueue, agentName string) *MemoryMiddleware {
	return &MemoryMiddleware{store: store, queue: queue, agentName: agentName}
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

	facts := mem.Facts
	if len(facts) > MaxFactsToInject {
		facts = facts[len(facts)-MaxFactsToInject:]
	}
	state.MemoryFacts = facts

	if len(facts) == 0 {
		return nil
	}

	block := "<memory_facts>\n" + strings.Join(facts, "\n") + "\n</memory_facts>"

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

// After filters the conversation and enqueues it for asynchronous memory update.
//
// Implementation:
//  1. filtered := filterMessagesForMemory(state.Messages)
//  2. Ensure there is at least one human and one assistant message; skip if not.
//  3. correction := detectCorrection(filtered)
//  4. m.queue.Add(state.ThreadID, filtered, m.agentName, correction)
func (m *MemoryMiddleware) After(ctx context.Context, state *middleware.State, response *middleware.Response) error {
	_ = ctx
	_ = response

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
