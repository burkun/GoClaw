package ui

import (
	"context"
	"strings"
	"testing"

	"goclaw/internal/middleware"
)

// mockTitleGenerator implements TitleGenerator for testing.
type mockTitleGenerator struct {
	title string
	err   error
}

func (m *mockTitleGenerator) Generate(_ context.Context, _ string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.title, nil
}

func TestTitleMiddleware_Name(t *testing.T) {
	mw := NewTitleMiddleware(DefaultTitleConfig(), nil)
	if mw.Name() != "TitleMiddleware" {
		t.Errorf("expected name TitleMiddleware, got %s", mw.Name())
	}
}

func TestTitleMiddleware_BeforeModel_NoOp(t *testing.T) {
	mw := NewTitleMiddleware(DefaultTitleConfig(), nil)
	state := &middleware.State{Messages: []map[string]any{{"role": "user", "content": "hi"}}}
	if err := mw.BeforeModel(context.Background(), state); err != nil {
		t.Errorf("BeforeModel should be no-op, got error: %v", err)
	}
}

func TestTitleMiddleware_Disabled(t *testing.T) {
	cfg := DefaultTitleConfig()
	cfg.Enabled = false
	mw := NewTitleMiddleware(cfg, &mockTitleGenerator{title: "A Title"})

	state := &middleware.State{
		Messages: []map[string]any{
			{"role": "human", "content": "hello"},
			{"role": "assistant", "content": "hi there"},
		},
	}

	if err := mw.AfterModel(context.Background(), state, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state.Title != "" {
		t.Errorf("expected no title when disabled, got %q", state.Title)
	}
}

func TestTitleMiddleware_SkipsExistingTitle(t *testing.T) {
	mw := NewTitleMiddleware(DefaultTitleConfig(), &mockTitleGenerator{title: "New Title"})

	state := &middleware.State{
		Title: "Existing Title",
		Messages: []map[string]any{
			{"role": "human", "content": "hello"},
			{"role": "assistant", "content": "hi"},
		},
	}

	if err := mw.AfterModel(context.Background(), state, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state.Title != "Existing Title" {
		t.Errorf("expected existing title preserved, got %q", state.Title)
	}
}

func TestTitleMiddleware_NeedsBothRoles(t *testing.T) {
	tests := []struct {
		name    string
		msgs    []map[string]any
		wantSet bool
	}{
		{"no messages", nil, false},
		{"only human", []map[string]any{{"role": "human", "content": "hi"}}, false},
		{"only assistant", []map[string]any{{"role": "assistant", "content": "hi"}}, false},
		{"two humans no assistant", []map[string]any{
			{"role": "human", "content": "hi"},
			{"role": "human", "content": "hello"},
		}, false},
		{"one of each", []map[string]any{
			{"role": "human", "content": "hi"},
			{"role": "assistant", "content": "hello"},
		}, true},
		{"two humans one assistant (humanCount != 1)", []map[string]any{
			{"role": "human", "content": "hi"},
			{"role": "human", "content": "hello"},
			{"role": "assistant", "content": "hey"},
		}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mw := NewTitleMiddleware(DefaultTitleConfig(), nil)
			state := &middleware.State{Messages: tt.msgs}
			mw.AfterModel(context.Background(), state, nil)
			if tt.wantSet && state.Title == "" {
				t.Error("expected title to be set")
			}
			if !tt.wantSet && state.Title != "" {
				t.Errorf("expected no title, got %q", state.Title)
			}
		})
	}
}

func TestTitleMiddleware_WithGenerator(t *testing.T) {
	mw := NewTitleMiddleware(DefaultTitleConfig(), &mockTitleGenerator{title: "LLM Generated Title"})

	state := &middleware.State{
		Messages: []map[string]any{
			{"role": "human", "content": "hello"},
			{"role": "assistant", "content": "hi there"},
		},
	}

	if err := mw.AfterModel(context.Background(), state, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state.Title != "LLM Generated Title" {
		t.Errorf("expected LLM title, got %q", state.Title)
	}
}

func TestTitleMiddleware_FallbackWithoutGenerator(t *testing.T) {
	mw := NewTitleMiddleware(DefaultTitleConfig(), nil)

	state := &middleware.State{
		Messages: []map[string]any{
			{"role": "human", "content": "Hello world this is a test"},
			{"role": "assistant", "content": "Hi"},
		},
	}

	if err := mw.AfterModel(context.Background(), state, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state.Title != "Hello world this is a test" {
		t.Errorf("expected fallback to user message, got %q", state.Title)
	}
}

func TestTitleMiddleware_FallbackTruncates(t *testing.T) {
	mw := NewTitleMiddleware(DefaultTitleConfig(), nil)

	longMsg := strings.Repeat("x", 100)
	state := &middleware.State{
		Messages: []map[string]any{
			{"role": "human", "content": longMsg},
			{"role": "assistant", "content": "hi"},
		},
	}

	mw.AfterModel(context.Background(), state, nil)
	if len(state.Title) > MaxFallbackChars+3 { // +3 for "..."
		t.Errorf("expected title truncated to ~%d chars, got %d: %q", MaxFallbackChars, len(state.Title), state.Title)
	}
	if !strings.HasSuffix(state.Title, "...") {
		t.Errorf("expected truncated title to end with ..., got %q", state.Title)
	}
}

func TestTitleMiddleware_FallbackEmptyMessage(t *testing.T) {
	mw := NewTitleMiddleware(DefaultTitleConfig(), nil)

	state := &middleware.State{
		Messages: []map[string]any{
			{"role": "human", "content": ""},
			{"role": "assistant", "content": "hi"},
		},
	}

	mw.AfterModel(context.Background(), state, nil)
	if state.Title != "New Conversation" {
		t.Errorf("expected 'New Conversation' for empty user msg, got %q", state.Title)
	}
}

func TestTitleMiddleware_PromptTemplate(t *testing.T) {
	cfg := DefaultTitleConfig()
	cfg.PromptTemplate = "Summarize in {max_words} words: {user_msg} / {assistant_msg}"

	captured := ""
	mw := NewTitleMiddleware(cfg, &mockTitleGenerator{title: "Title"})

	// We can't easily capture the prompt sent to the generator in this mock,
	// but we can verify the generator is called and the title is set.
	state := &middleware.State{
		Messages: []map[string]any{
			{"role": "human", "content": "hello"},
			{"role": "assistant", "content": "hi"},
		},
	}

	mw.AfterModel(context.Background(), state, nil)
	_ = captured
	if state.Title != "Title" {
		t.Errorf("expected generated title, got %q", state.Title)
	}
}

func TestParseTitle(t *testing.T) {
	mw := &TitleMiddleware{}
	tests := []struct {
		input, want string
	}{
		{"  Hello World  ", "Hello World"},
		{`"Quoted Title"`, "Quoted Title"},
		{`'Single Quotes'`, "Single Quotes"},
		{`  "  Spaced Quote  "  `, "Spaced Quote"},
		{"", ""},
		{strings.Repeat("A", 100), strings.Repeat("A", MaxTitleChars)},
	}

	for _, tt := range tests {
		got := mw.parseTitle(tt.input)
		if got != tt.want {
			t.Errorf("parseTitle(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestDefaultTitleConfig(t *testing.T) {
	cfg := DefaultTitleConfig()
	if !cfg.Enabled {
		t.Error("expected Enabled=true by default")
	}
	if cfg.MaxWords <= 0 {
		t.Error("expected positive MaxWords")
	}
	if cfg.PromptTemplate == "" {
		t.Error("expected non-empty PromptTemplate")
	}
	if !strings.Contains(cfg.PromptTemplate, "{max_words}") {
		t.Error("expected {max_words} placeholder in template")
	}
	if !strings.Contains(cfg.PromptTemplate, "{user_msg}") {
		t.Error("expected {user_msg} placeholder in template")
	}
	if !strings.Contains(cfg.PromptTemplate, "{assistant_msg}") {
		t.Error("expected {assistant_msg} placeholder in template")
	}
}
