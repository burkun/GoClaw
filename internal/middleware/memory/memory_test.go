package memory

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/bookerbai/goclaw/internal/middleware"
)

// ---------------------------------------------------------------------------
// TestMemoryStore_dedup — JSONFileStore.Deduplicate deduplication tests
// ---------------------------------------------------------------------------

// TestMemoryStore_dedup verifies that Deduplicate removes substring-dominated
// facts while retaining the more specific or unrelated ones.
func TestMemoryStore_dedup(t *testing.T) {
	store := NewJSONFileStore("/tmp/goclaw-test-memory.json")

	tests := []struct {
		name     string
		input    []string
		wantKept []string // facts expected to survive deduplication
		wantGone []string // facts expected to be removed
	}{
		{
			name:     "exact duplicate kept once",
			input:    []string{"User prefers Go", "User prefers Go"},
			wantKept: []string{"User prefers Go"},
		},
		{
			name: "shorter fact contained in longer retained",
			// "Go" is a substring of "User prefers Go" — "Go" should be removed
			// because it is dominated (its lowercased form is contained inside
			// the longer fact's lowercased form).
			input:    []string{"Go", "User prefers Go"},
			wantKept: []string{"User prefers Go"},
			wantGone: []string{"Go"},
		},
		{
			name: "unrelated facts both kept",
			input:    []string{"User likes coffee", "User prefers Go"},
			wantKept: []string{"User likes coffee", "User prefers Go"},
		},
		{
			name:     "empty list unchanged",
			input:    []string{},
			wantKept: []string{},
		},
		{
			name:     "single fact unchanged",
			input:    []string{"Only fact"},
			wantKept: []string{"Only fact"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := newEmptyMemory()
			m.Facts = make([]string, len(tc.input))
			copy(m.Facts, tc.input)

			store.Deduplicate(m)

			for _, want := range tc.wantKept {
				found := false
				for _, f := range m.Facts {
					if strings.EqualFold(f, want) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected fact %q to be kept, got facts: %v", want, m.Facts)
				}
			}

			for _, gone := range tc.wantGone {
				for _, f := range m.Facts {
					if strings.EqualFold(f, gone) {
						t.Errorf("expected fact %q to be removed, but it remains in: %v", gone, m.Facts)
					}
				}
			}
		})
	}
}

// TestMemoryStore_addFact_dedup verifies AddFact prevents duplicate insertion.
func TestMemoryStore_addFact_dedup(t *testing.T) {
	store := NewJSONFileStore("/tmp/goclaw-test-memory.json")
	m := newEmptyMemory()

	added := store.AddFact(m, "User prefers Go")
	if !added {
		t.Fatal("expected first AddFact to return true")
	}

	added = store.AddFact(m, "User prefers Go")
	if added {
		t.Fatal("expected duplicate AddFact to return false")
	}
	if len(m.Facts) != 1 {
		t.Fatalf("expected 1 fact, got %d: %v", len(m.Facts), m.Facts)
	}

	// Substring variant: "prefers Go" is contained within the existing fact.
	added = store.AddFact(m, "prefers Go")
	if added {
		t.Fatal("expected substring fact to be rejected")
	}
}

// ---------------------------------------------------------------------------
// TestMemoryMiddleware_inject — Before() injects facts into system prompt
// ---------------------------------------------------------------------------

// stubStore is an in-memory MemoryStore for testing.
type stubStore struct {
	mem *Memory
}

func (s *stubStore) Load() (*Memory, error)       { return s.mem, nil }
func (s *stubStore) Save(m *Memory) error         { s.mem = m; return nil }
func (s *stubStore) AddFact(m *Memory, fact string) bool {
	for _, f := range m.Facts {
		if strings.EqualFold(f, fact) {
			return false
		}
	}
	m.Facts = append(m.Facts, fact)
	return true
}
func (s *stubStore) Deduplicate(m *Memory) {} // no-op for tests

// newStubQueue returns a non-functional UpdateQueue with no timer for testing.
func newStubQueue() *UpdateQueue {
	return &UpdateQueue{
		entries:       make(map[string]*updateEntry),
		store:         &stubStore{mem: newEmptyMemory()},
		DebounceDelay: time.Hour, // very long so process() never fires in tests
	}
}

// TestMemoryMiddleware_inject verifies that Before() injects facts into the
// system prompt when the Memory document contains facts.
func TestMemoryMiddleware_inject(t *testing.T) {
	facts := []string{"User prefers Go", "User works at ACME Corp", "User's timezone is UTC+8"}
	store := &stubStore{mem: &Memory{
		Version:     "1.0",
		LastUpdated: time.Now().UTC().Format(time.RFC3339),
		Facts:       facts,
	}}

	mw := NewMemoryMiddleware(store, newStubQueue(), "")

	t.Run("injects facts into existing system message", func(t *testing.T) {
		state := &middleware.State{
			ThreadID: "thread-001",
			Messages: []map[string]any{
				{"role": "system", "content": "You are a helpful assistant."},
				{"role": "human", "content": "Hello"},
			},
		}

		if err := mw.Before(context.Background(), state); err != nil {
			t.Fatalf("Before() returned error: %v", err)
		}

		sysContent, _ := state.Messages[0]["content"].(string)
		if !strings.Contains(sysContent, "<memory_facts>") {
			t.Errorf("expected system message to contain <memory_facts> block, got: %s", sysContent)
		}
		for _, f := range facts {
			if !strings.Contains(sysContent, f) {
				t.Errorf("expected system message to contain fact %q", f)
			}
		}
		// Original content preserved.
		if !strings.Contains(sysContent, "You are a helpful assistant") {
			t.Errorf("expected original system content to be preserved")
		}
	})

	t.Run("creates system message when none exists", func(t *testing.T) {
		state := &middleware.State{
			ThreadID: "thread-002",
			Messages: []map[string]any{
				{"role": "human", "content": "Hi"},
			},
		}

		if err := mw.Before(context.Background(), state); err != nil {
			t.Fatalf("Before() returned error: %v", err)
		}

		if state.Messages[0]["role"] != "system" {
			t.Errorf("expected first message to be system, got %v", state.Messages[0]["role"])
		}
		sysContent, _ := state.Messages[0]["content"].(string)
		if !strings.Contains(sysContent, "<memory_facts>") {
			t.Errorf("expected injected system message to contain <memory_facts>")
		}
	})

	t.Run("no injection when store has no facts", func(t *testing.T) {
		emptyStore := &stubStore{mem: newEmptyMemory()}
		emptyMW := NewMemoryMiddleware(emptyStore, newStubQueue(), "")

		state := &middleware.State{
			ThreadID: "thread-003",
			Messages: []map[string]any{
				{"role": "system", "content": "Original."},
				{"role": "human", "content": "Hi"},
			},
		}

		if err := emptyMW.Before(context.Background(), state); err != nil {
			t.Fatalf("Before() returned error: %v", err)
		}

		sysContent, _ := state.Messages[0]["content"].(string)
		if strings.Contains(sysContent, "<memory_facts>") {
			t.Errorf("expected no injection when facts are empty, got: %s", sysContent)
		}
	})
}

