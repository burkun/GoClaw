package memory

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bookerbai/goclaw/internal/middleware"
)

type integrationExtractor struct {
	facts []Fact
	err   error
}

func (e *integrationExtractor) Extract(_ []map[string]any, _ bool) ([]Fact, error) {
	if e.err != nil {
		return nil, e.err
	}
	return e.facts, nil
}

func TestMemoryIntegration_AfterProcess_LLMSavesFilteredFacts(t *testing.T) {
	store := NewJSONFileStore(filepath.Join(t.TempDir(), "memory.json"))
	q := &UpdateQueue{
		entries:       make(map[string]*updateEntry),
		store:         store,
		maxFacts:      100,
		DebounceDelay: 30 * time.Second,
		extractor: &integrationExtractor{facts: []Fact{
			{Content: "User prefers Go", Category: CategoryPreference, Confidence: 0.95},
			{Content: "Low confidence fact", Category: CategoryContext, Confidence: 0.30},
		}},
	}
	mw := NewMemoryMiddleware(store, q, "")

	state := &middleware.State{
		ThreadID: "thread-int-1",
		Messages: []map[string]any{
			{"role": "human", "content": "我偏好使用 Go。"},
			{"role": "assistant", "content": "收到，你偏好 Go。"},
		},
	}

	if err := mw.AfterAgent(context.Background(), state, &middleware.Response{}); err != nil {
		t.Fatalf("AfterAgent() failed: %v", err)
	}
	q.process()

	mem, err := store.Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	joined := factsContents(mem.Facts)
	if !strings.Contains(joined, "User prefers Go") {
		t.Fatalf("expected high-confidence fact saved, got %+v", mem.Facts)
	}
	if strings.Contains(joined, "Low confidence fact") {
		t.Fatalf("unexpected low-confidence fact saved, got %+v", mem.Facts)
	}
	if mem.Facts[0].Source == "" {
		t.Fatalf("expected source set, got %+v", mem.Facts[0])
	}
}

func TestMemoryIntegration_BeforeInjectsLatest15Facts(t *testing.T) {
	store := NewJSONFileStore(filepath.Join(t.TempDir(), "memory.json"))
	m := newEmptyMemory()
	for i := 1; i <= 20; i++ {
		m.Facts = append(m.Facts, MemoryFact{Content: fmt.Sprintf("fact-%d", i)})
	}
	if err := store.Save(m); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	q := &UpdateQueue{entries: make(map[string]*updateEntry), store: store}
	mw := NewMemoryMiddleware(store, q, "")

	state := &middleware.State{ThreadID: "thread-int-3", Messages: []map[string]any{{"role": "system", "content": "You are helpful."}}}
	if err := mw.Before(context.Background(), state); err != nil {
		t.Fatalf("Before() failed: %v", err)
	}

	if len(state.MemoryFacts) != MaxFactsToInject {
		t.Fatalf("expected %d injected facts, got %d", MaxFactsToInject, len(state.MemoryFacts))
	}
	if state.MemoryFacts[0] != "fact-6" || state.MemoryFacts[len(state.MemoryFacts)-1] != "fact-20" {
		t.Fatalf("expected latest 15 facts [fact-6..fact-20], got first=%q last=%q", state.MemoryFacts[0], state.MemoryFacts[len(state.MemoryFacts)-1])
	}
}

func TestMemoryIntegration_Before_RespectsInjectionEnabled(t *testing.T) {
	store := NewJSONFileStore(filepath.Join(t.TempDir(), "memory.json"))
	m := newEmptyMemory()
	m.Facts = []MemoryFact{{Content: "fact-a"}, {Content: "fact-b"}}
	if err := store.Save(m); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	q := &UpdateQueue{entries: make(map[string]*updateEntry), store: store}
	mw := NewMemoryMiddleware(store, q, "", WithInjectionEnabled(false))

	state := &middleware.State{ThreadID: "thread-int-4", Messages: []map[string]any{{"role": "system", "content": "You are helpful."}}}
	if err := mw.Before(context.Background(), state); err != nil {
		t.Fatalf("Before() failed: %v", err)
	}

	if len(state.MemoryFacts) != 0 {
		t.Fatalf("expected no injected memory facts when disabled, got %v", state.MemoryFacts)
	}
}
