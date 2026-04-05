package memory

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
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

type stubChatModel struct {
	resp *schema.Message
	err  error
}

func (s *stubChatModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	_ = ctx
	_ = input
	_ = opts
	if s.err != nil {
		return nil, s.err
	}
	return s.resp, nil
}

func (s *stubChatModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	_ = ctx
	_ = input
	_ = opts
	if s.err != nil {
		return nil, s.err
	}
	if s.resp == nil {
		return schema.StreamReaderFromArray([]*schema.Message{}), nil
	}
	return schema.StreamReaderFromArray([]*schema.Message{s.resp}), nil
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
			{Content: "User prefers Go", Confidence: 0.9, Category: "preference"},
			{Content: "Low confidence fact", Confidence: 0.4, Category: "general"},
		}},
	}
	q.entries["thread-llm"] = &updateEntry{
		threadID: "thread-llm",
		messages: []map[string]any{{"role": "human", "content": "I prefer Go."}},
	}

	q.process()

	joined := factsContents(store.mem.Facts)
	if !strings.Contains(joined, "User prefers Go") {
		t.Fatalf("expected high-confidence fact saved, got %+v", store.mem.Facts)
	}
	if strings.Contains(joined, "Low confidence fact") {
		t.Fatalf("did not expect low-confidence fact saved, got %+v", store.mem.Facts)
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

func TestEinoFactExtractor_Extract_FiltersDedupAndCorrection(t *testing.T) {
	m := &stubChatModel{resp: schema.AssistantMessage(`[
  {"content":"User prefers Go","category":"preference","confidence":0.9},
  {"content":"User prefers Go","category":"preference","confidence":0.8},
  {"content":"Low confidence fact","category":"general","confidence":0.3},
  {"content":"Missing confidence defaults","category":"general","confidence":0}
]`, nil)}

	extractor := NewEinoFactExtractor(m, 0.7)
	facts, err := extractor.Extract([]map[string]any{{"role": "human", "content": "I prefer Go"}}, true)
	if err != nil {
		t.Fatalf("Extract failed: %v", err)
	}

	joined := strings.ToLower(factsToString(facts))
	if !strings.Contains(joined, "user prefers go") {
		t.Fatalf("expected high-confidence fact kept, got %v", facts)
	}
	if strings.Contains(joined, "low confidence fact") {
		t.Fatalf("expected low-confidence fact filtered, got %v", facts)
	}
	if !strings.Contains(joined, "missing confidence defaults") {
		t.Fatalf("expected default-confidence fact kept, got %v", facts)
	}
	if !strings.Contains(joined, "corrected") {
		t.Fatalf("expected correction fact appended, got %v", facts)
	}
}

func TestEinoFactExtractor_Extract_InvalidJSONReturnsError(t *testing.T) {
	m := &stubChatModel{resp: schema.AssistantMessage("not-json", nil)}
	extractor := NewEinoFactExtractor(m, 0.7)

	_, err := extractor.Extract([]map[string]any{{"role": "human", "content": "hello"}}, false)
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
	if !strings.Contains(err.Error(), "parse facts json") {
		t.Fatalf("expected parse facts json error, got %v", err)
	}
}

func TestUpdateQueue_SetMaxFacts_TruncatesStoredFacts(t *testing.T) {
	store := &stubStore{mem: newEmptyMemory()}
	store.mem.Facts = []MemoryFact{{Content: "old-1"}, {Content: "old-2"}}

	q := &UpdateQueue{
		entries: make(map[string]*updateEntry),
		store:   store,
		extractor: &stubExtractor{facts: []Fact{
			{Content: "new-1", Confidence: 0.9, Category: "context"},
			{Content: "new-2", Confidence: 0.9, Category: "context"},
		}},
	}
	q.SetMaxFacts(2)
	q.entries["thread-cap"] = &updateEntry{
		threadID: "thread-cap",
		messages: []map[string]any{{"role": "human", "content": "new facts"}},
	}

	q.process()

	if len(store.mem.Facts) != 2 {
		t.Fatalf("expected truncated facts length=2, got %d: %+v", len(store.mem.Facts), store.mem.Facts)
	}
	if store.mem.Facts[0].Content != "new-1" || store.mem.Facts[1].Content != "new-2" {
		t.Fatalf("expected latest facts kept, got %+v", store.mem.Facts)
	}
}

func factsToString(facts []Fact) string {
	parts := make([]string, 0, len(facts))
	for _, f := range facts {
		parts = append(parts, f.Content)
	}
	return strings.Join(parts, "\n")
}
