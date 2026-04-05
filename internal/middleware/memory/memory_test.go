package memory

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/bookerbai/goclaw/internal/middleware"
)

func TestMemoryStore_dedup(t *testing.T) {
	store := NewJSONFileStore("/tmp/goclaw-test-memory.json")

	tests := []struct {
		name     string
		input    []string
		wantKept []string
		wantGone []string
	}{
		{
			name:     "exact duplicate kept once",
			input:    []string{"User prefers Go", "User prefers Go"},
			wantKept: []string{"User prefers Go"},
		},
		{
			name:     "shorter fact contained in longer retained",
			input:    []string{"Go", "User prefers Go"},
			wantKept: []string{"User prefers Go"},
			wantGone: []string{"Go"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := newEmptyMemory()
			for _, c := range tc.input {
				m.Facts = append(m.Facts, MemoryFact{Content: c})
			}

			store.Deduplicate(m)

			for _, want := range tc.wantKept {
				found := false
				for _, f := range m.Facts {
					if strings.EqualFold(f.Content, want) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected fact %q to be kept, got facts: %+v", want, m.Facts)
				}
			}

			for _, gone := range tc.wantGone {
				for _, f := range m.Facts {
					if strings.EqualFold(f.Content, gone) {
						t.Errorf("expected fact %q removed, got %+v", gone, m.Facts)
					}
				}
			}
		})
	}
}

func TestMemoryStore_addFact_dedup(t *testing.T) {
	store := NewJSONFileStore("/tmp/goclaw-test-memory.json")
	m := newEmptyMemory()

	added := store.AddFact(m, MemoryFact{Content: "User prefers Go", Category: "preference", Confidence: 0.9, Source: "thread-1"})
	if !added {
		t.Fatal("expected first AddFact to return true")
	}

	added = store.AddFact(m, MemoryFact{Content: "User prefers Go"})
	if added {
		t.Fatal("expected duplicate AddFact to return false")
	}
	if len(m.Facts) != 1 {
		t.Fatalf("expected 1 fact, got %d: %+v", len(m.Facts), m.Facts)
	}
	if m.Facts[0].ID == "" || m.Facts[0].CreatedAt == "" {
		t.Fatalf("expected auto-filled metadata, got %+v", m.Facts[0])
	}

	added = store.AddFact(m, MemoryFact{Content: "prefers Go"})
	if added {
		t.Fatal("expected substring fact to be rejected")
	}
}

// stubStore is an in-memory MemoryStore for testing.
type stubStore struct {
	mem *Memory
}

func (s *stubStore) Load() (*Memory, error) { return s.mem, nil }
func (s *stubStore) Save(m *Memory) error   { s.mem = m; return nil }
func (s *stubStore) AddFact(m *Memory, fact MemoryFact) bool {
	for _, f := range m.Facts {
		if strings.EqualFold(strings.TrimSpace(f.Content), strings.TrimSpace(fact.Content)) {
			return false
		}
	}
	m.Facts = append(m.Facts, fact)
	return true
}
func (s *stubStore) Deduplicate(m *Memory) {}

func newStubQueue() *UpdateQueue {
	return &UpdateQueue{
		entries:       make(map[string]*updateEntry),
		store:         &stubStore{mem: newEmptyMemory()},
		DebounceDelay: time.Hour,
	}
}

func TestMemoryMiddleware_inject(t *testing.T) {
	facts := []MemoryFact{
		{Content: "User prefers Go"},
		{Content: "User works at ACME Corp"},
		{Content: "User's timezone is UTC+8"},
	}
	store := &stubStore{mem: &Memory{
		Version:     "1.0",
		LastUpdated: time.Now().UTC().Format(time.RFC3339),
		Facts:       facts,
	}}

	mw := NewMemoryMiddleware(store, newStubQueue(), "")

	state := &middleware.State{
		ThreadID: "thread-001",
		Messages: []map[string]any{{"role": "system", "content": "You are a helpful assistant."}},
	}

	if err := mw.BeforeModel(context.Background(), state); err != nil {
		t.Fatalf("Before() returned error: %v", err)
	}

	sysContent, _ := state.Messages[0]["content"].(string)
	if !strings.Contains(sysContent, "<memory>") {
		t.Fatalf("expected memory block, got: %s", sysContent)
	}
	if len(state.MemoryFacts) != 3 {
		t.Fatalf("expected 3 injected facts, got %v", state.MemoryFacts)
	}
}

func TestUpdateQueueProcess_ExtractsFacts(t *testing.T) {
	store := &stubStore{mem: newEmptyMemory()}
	q := &UpdateQueue{entries: make(map[string]*updateEntry), store: store}
	q.entries["thread-1"] = &updateEntry{
		threadID: "thread-1",
		messages: []map[string]any{
			{"role": "human", "content": "我偏好使用 Go 语言做后端开发。"},
			{"role": "assistant", "content": "收到"},
		},
	}

	q.process()

	if len(store.mem.Facts) == 0 {
		t.Fatalf("expected extracted facts to be saved")
	}
	if store.mem.Facts[0].Source == "" {
		t.Fatalf("expected fact source set, got %+v", store.mem.Facts[0])
	}
}

func TestUpdateQueueProcess_CorrectionFact(t *testing.T) {
	store := &stubStore{mem: newEmptyMemory()}
	q := &UpdateQueue{entries: make(map[string]*updateEntry), store: store}
	q.entries["thread-2"] = &updateEntry{
		threadID:           "thread-2",
		correctionDetected: true,
		messages: []map[string]any{
			{"role": "human", "content": "你理解错了，我主要用 Go。"},
			{"role": "assistant", "content": "好的，我会修正。"},
		},
	}

	q.process()

	joined := strings.ToLower(factsContents(store.mem.Facts))
	if !strings.Contains(joined, "corrected") {
		t.Fatalf("expected correction fact, got %+v", store.mem.Facts)
	}
}

func factsContents(facts []MemoryFact) string {
	parts := make([]string, 0, len(facts))
	for _, f := range facts {
		parts = append(parts, f.Content)
	}
	return strings.Join(parts, "\n")
}
