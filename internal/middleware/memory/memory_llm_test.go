package memory

import (
	"errors"
	"strings"
	"testing"
)

type stubExtractor struct {
	facts []Fact
	err   error
}

func (s *stubExtractor) Extract(_ []map[string]any, _ bool) ([]Fact, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.facts, nil
}

func TestParseMemoryFactsOutput_CodeFence(t *testing.T) {
	raw := "```json\n[{\"content\":\"User prefers Go\",\"category\":\"preference\",\"confidence\":0.92}]\n```"
	facts, err := parseMemoryFactsOutput(raw)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(facts) != 1 || facts[0].Content != "User prefers Go" {
		t.Fatalf("unexpected facts: %+v", facts)
	}
}

func TestUpdateQueue_UsesExtractorAndThreshold(t *testing.T) {
	store := &stubStore{mem: newEmptyMemory()}
	q := &UpdateQueue{
		entries: make(map[string]*updateEntry),
		store:   store,
		extractor: &stubExtractor{facts: []Fact{
			{Content: "User prefers Go", Confidence: 0.9},
			{Content: "Low confidence fact", Confidence: 0.4},
		}},
	}
	q.entries["thread-llm"] = &updateEntry{
		threadID: "thread-llm",
		messages: []map[string]any{{"role": "human", "content": "I prefer Go."}},
	}

	q.process()

	joined := strings.Join(store.mem.Facts, "\n")
	if !strings.Contains(joined, "User prefers Go") {
		t.Fatalf("expected high-confidence fact saved, got %v", store.mem.Facts)
	}
	if strings.Contains(joined, "Low confidence fact") {
		t.Fatalf("did not expect low-confidence fact saved, got %v", store.mem.Facts)
	}
}

func TestUpdateQueue_ExtractorErrorFallbackRuleBased(t *testing.T) {
	store := &stubStore{mem: newEmptyMemory()}
	q := &UpdateQueue{
		entries:   make(map[string]*updateEntry),
		store:     store,
		extractor: &stubExtractor{err: errors.New("extract failed")},
	}
	q.entries["thread-fallback"] = &updateEntry{
		threadID: "thread-fallback",
		messages: []map[string]any{{"role": "human", "content": "我偏好使用 Go 语言做后端开发。"}},
	}

	q.process()

	if len(store.mem.Facts) == 0 {
		t.Fatalf("expected fallback derived fact to be saved")
	}
}
