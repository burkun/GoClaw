package summarize

import (
	"context"
	"strings"
	"testing"

	"goclaw/internal/middleware"
)

// mockSummarizer implements Summarizer for testing
type mockSummarizer struct {
	summary string
	err     error
	called  bool
}

func (m *mockSummarizer) Summarize(ctx context.Context, history string) (string, error) {
	m.called = true
	if m.err != nil {
		return "", m.err
	}
	return m.summary, nil
}

func TestSummarizationMiddleware_Name(t *testing.T) {
	mw := NewSummarizationMiddleware(DefaultConfig(), nil)
	if mw.Name() != "SummarizationMiddleware" {
		t.Errorf("expected name SummarizationMiddleware, got %s", mw.Name())
	}
}

func TestSummarizationMiddleware_Disabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = false
	mw := NewSummarizationMiddleware(cfg, nil)

	state := &middleware.State{
		Messages:   []map[string]any{{"role": "user", "content": strings.Repeat("x", 100000)}},
		TokenCount: 100000,
	}

	err := mw.BeforeModel(context.Background(), state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should not modify messages when disabled
	if len(state.Messages) != 1 {
		t.Errorf("expected 1 message, got %d", len(state.Messages))
	}
}

func TestSummarizationMiddleware_NoTriggerBelowThreshold(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.TokenLimit = 10000
	cfg.ThresholdRatio = 0.8
	mw := NewSummarizationMiddleware(cfg, nil)

	state := &middleware.State{
		Messages:   []map[string]any{{"role": "user", "content": "short message"}},
		TokenCount: 100, // Below threshold
	}

	err := mw.BeforeModel(context.Background(), state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should not modify messages when below threshold
	if len(state.Messages) != 1 {
		t.Errorf("expected 1 message, got %d", len(state.Messages))
	}
}

func TestSummarizationMiddleware_TriggersAboveThreshold(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.TokenLimit = 1000
	cfg.ThresholdRatio = 0.8
	cfg.KeepRecentMessages = 2

	mockSumm := &mockSummarizer{summary: "This is a summary of the conversation."}
	mw := NewSummarizationMiddleware(cfg, mockSumm)

	// Create messages that exceed threshold
	messages := make([]map[string]any, 20)
	for i := 0; i < 20; i++ {
		messages[i] = map[string]any{
			"role":    "user",
			"content": strings.Repeat("hello world ", 100), // ~1200 chars each
		}
	}

	state := &middleware.State{
		Messages: messages,
	}

	err := mw.BeforeModel(context.Background(), state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have compressed: 1 summary + 2 recent = 3 messages
	if len(state.Messages) != 3 {
		t.Errorf("expected 3 messages (1 summary + 2 recent), got %d", len(state.Messages))
	}

	// First message should be summary
	if !strings.HasPrefix(state.Messages[0]["content"].(string), SummarySentinel) {
		t.Errorf("expected first message to start with sentinel, got: %s", state.Messages[0]["content"])
	}

	if !mockSumm.called {
		t.Error("expected summarizer to be called")
	}
}

func TestSummarizationMiddleware_FallbackOnNilSummarizer(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.TokenLimit = 1000
	cfg.ThresholdRatio = 0.8
	cfg.KeepRecentMessages = 2
	mw := NewSummarizationMiddleware(cfg, nil) // nil summarizer

	messages := make([]map[string]any, 20)
	for i := 0; i < 20; i++ {
		messages[i] = map[string]any{
			"role":    "user",
			"content": strings.Repeat("test ", 200),
		}
	}

	state := &middleware.State{
		Messages: messages,
	}

	err := mw.BeforeModel(context.Background(), state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should still compress with placeholder
	if len(state.Messages) != 3 {
		t.Errorf("expected 3 messages, got %d", len(state.Messages))
	}

	// Should contain placeholder when no summarizer
	content := state.Messages[0]["content"].(string)
	if !strings.Contains(content, "details omitted") {
		t.Errorf("expected placeholder summary, got: %s", content)
	}
}

func TestSummarizationMiddleware_KeepsRecentMessages(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.TokenLimit = 500
	cfg.ThresholdRatio = 0.8
	cfg.KeepRecentMessages = 5

	mockSumm := &mockSummarizer{summary: "Summary here."}
	mw := NewSummarizationMiddleware(cfg, mockSumm)

	messages := make([]map[string]any, 10)
	for i := 0; i < 10; i++ {
		messages[i] = map[string]any{
			"role":    "user",
			"content": strings.Repeat("abcdefghij ", 100), // ~1200 chars each
		}
	}

	state := &middleware.State{
		Messages: messages,
	}

	err := mw.BeforeModel(context.Background(), state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should keep exactly 5 recent + 1 summary = 6
	if len(state.Messages) != 6 {
		t.Errorf("expected 6 messages, got %d", len(state.Messages))
	}
}

func TestSummarizationMiddleware_ReplacesPriorSummary(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.TokenLimit = 500
	cfg.ThresholdRatio = 0.8
	cfg.KeepRecentMessages = 2

	mockSumm := &mockSummarizer{summary: "New summary."}
	mw := NewSummarizationMiddleware(cfg, mockSumm)

	messages := []map[string]any{
		{"role": "system", "content": SummarySentinel + "\nOld summary that should be replaced."},
		{"role": "user", "content": strings.Repeat("message ", 200)},
		{"role": "user", "content": strings.Repeat("message ", 200)},
		{"role": "user", "content": strings.Repeat("message ", 200)},
	}

	state := &middleware.State{
		Messages: messages,
	}

	err := mw.BeforeModel(context.Background(), state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have replaced old summary with new one
	if len(state.Messages) != 3 {
		t.Errorf("expected 3 messages, got %d", len(state.Messages))
	}

	content := state.Messages[0]["content"].(string)
	if !strings.Contains(content, "New summary") {
		t.Errorf("expected new summary, got: %s", content)
	}
}

func TestEstimateTokens_ASCII(t *testing.T) {
	// ASCII: ~4 chars per token
	msg := map[string]any{"role": "user", "content": "hello"} // 5 chars -> ~1 token
	tokens := estimateTokens([]map[string]any{msg})
	if tokens != 1 {
		t.Errorf("expected 1 token for 5 ASCII chars, got %d", tokens)
	}
}

func TestEstimateTokens_Multibyte(t *testing.T) {
	// CJK characters: ~1.5 chars per token
	msg := map[string]any{"role": "user", "content": "你好世界"} // 4 CJK chars -> ~2-3 tokens
	tokens := estimateTokens([]map[string]any{msg})
	if tokens < 2 || tokens > 4 {
		t.Errorf("expected 2-4 tokens for 4 CJK chars, got %d", tokens)
	}
}

func TestEstimateTokens_Mixed(t *testing.T) {
	// Mix of ASCII and CJK
	msg := map[string]any{"role": "user", "content": "Hello 你好"} // 6 ASCII + 2 CJK
	tokens := estimateTokens([]map[string]any{msg})
	// ASCII: 6/4 = 1, CJK: 2*2/3 = 1, total ~2
	if tokens < 1 || tokens > 3 {
		t.Errorf("expected 1-3 tokens for mixed content, got %d", tokens)
	}
}

func TestFormatTranscript(t *testing.T) {
	messages := []map[string]any{
		{"role": "user", "content": "Hello"},
		{"role": "assistant", "content": "Hi there!"},
	}

	transcript := formatTranscript(messages, "Prior summary here.", "Custom prompt:")

	if !strings.Contains(transcript, "Custom prompt:") {
		t.Error("expected prompt template in transcript")
	}
	if !strings.Contains(transcript, "Prior summary here.") {
		t.Error("expected prior summary in transcript")
	}
	if !strings.Contains(transcript, "User: Hello") {
		t.Error("expected user message in transcript")
	}
	if !strings.Contains(transcript, "Assistant: Hi there!") {
		t.Error("expected assistant message in transcript")
	}
}
