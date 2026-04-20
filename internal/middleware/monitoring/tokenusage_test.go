package monitoring

import (
	"context"
	"testing"

	"goclaw/internal/middleware"
)

func TestTokenUsageMiddleware_Name(t *testing.T) {
	mw := NewTokenUsageMiddleware()
	if mw.Name() != "TokenUsageMiddleware" {
		t.Errorf("expected name 'TokenUsageMiddleware', got %s", mw.Name())
	}
}

func TestTokenUsageMiddleware_AfterModel_EmptyMessages(t *testing.T) {
	mw := NewTokenUsageMiddleware()
	state := &middleware.State{Messages: []map[string]any{}}
	err := mw.AfterModel(context.Background(), state, &middleware.Response{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestTokenUsageMiddleware_AfterModel_NilMessages(t *testing.T) {
	mw := NewTokenUsageMiddleware()
	state := &middleware.State{Messages: nil}
	err := mw.AfterModel(context.Background(), state, &middleware.Response{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestTokenUsageMiddleware_AfterModel_NonAssistantMessage(t *testing.T) {
	mw := NewTokenUsageMiddleware()
	state := &middleware.State{
		Messages: []map[string]any{
			{"role": "human", "content": "hello"},
		},
	}
	err := mw.AfterModel(context.Background(), state, &middleware.Response{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestTokenUsageMiddleware_AfterModel_NoUsageMetadata(t *testing.T) {
	mw := NewTokenUsageMiddleware()
	state := &middleware.State{
		Messages: []map[string]any{
			{"role": "assistant", "content": "response"},
		},
	}
	err := mw.AfterModel(context.Background(), state, &middleware.Response{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestTokenUsageMiddleware_AfterModel_WithUsageMetadata(t *testing.T) {
	mw := NewTokenUsageMiddleware()
	state := &middleware.State{
		Messages: []map[string]any{
			{
				"role":    "assistant",
				"content": "response",
				"usage_metadata": map[string]any{
					"input_tokens":  100,
					"output_tokens": 50,
					"total_tokens":  150,
				},
			},
		},
	}
	err := mw.AfterModel(context.Background(), state, &middleware.Response{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestTokenUsageMiddleware_AfterModel_WithInt64Tokens(t *testing.T) {
	mw := NewTokenUsageMiddleware()
	state := &middleware.State{
		Messages: []map[string]any{
			{
				"role":    "assistant",
				"content": "response",
				"usage_metadata": map[string]any{
					"input_tokens":  int64(100),
					"output_tokens": int64(50),
					"total_tokens":  int64(150),
				},
			},
		},
	}
	err := mw.AfterModel(context.Background(), state, &middleware.Response{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestTokenUsageMiddleware_AfterModel_WithFloat64Tokens(t *testing.T) {
	mw := NewTokenUsageMiddleware()
	state := &middleware.State{
		Messages: []map[string]any{
			{
				"role":    "assistant",
				"content": "response",
				"usage_metadata": map[string]any{
					"input_tokens":  float64(100),
					"output_tokens": float64(50),
					"total_tokens":  float64(150),
				},
			},
		},
	}
	err := mw.AfterModel(context.Background(), state, &middleware.Response{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestTokenUsageMiddleware_AfterModel_WithUint64Tokens(t *testing.T) {
	mw := NewTokenUsageMiddleware()
	state := &middleware.State{
		Messages: []map[string]any{
			{
				"role":    "assistant",
				"content": "response",
				"usage_metadata": map[string]any{
					"input_tokens":  uint64(100),
					"output_tokens": uint64(50),
					"total_tokens":  uint64(150),
				},
			},
		},
	}
	err := mw.AfterModel(context.Background(), state, &middleware.Response{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestTokenUsageMiddleware_AfterModel_MissingKeys(t *testing.T) {
	mw := NewTokenUsageMiddleware()
	state := &middleware.State{
		Messages: []map[string]any{
			{
				"role":           "assistant",
				"content":        "response",
				"usage_metadata": map[string]any{},
			},
		},
	}
	err := mw.AfterModel(context.Background(), state, &middleware.Response{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestTokenUsageMiddleware_AfterModel_InvalidTokenTypes(t *testing.T) {
	mw := NewTokenUsageMiddleware()
	state := &middleware.State{
		Messages: []map[string]any{
			{
				"role":    "assistant",
				"content": "response",
				"usage_metadata": map[string]any{
					"input_tokens":  "invalid",
					"output_tokens": []int{1, 2},
					"total_tokens":  nil,
				},
			},
		},
	}
	err := mw.AfterModel(context.Background(), state, &middleware.Response{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestExtractInt(t *testing.T) {
	tests := []struct {
		name     string
		m        map[string]any
		key      string
		expected int
	}{
		{"nil map", nil, "key", 0},
		{"missing key", map[string]any{}, "key", 0},
		{"int value", map[string]any{"key": 42}, "key", 42},
		{"int64 value", map[string]any{"key": int64(42)}, "key", 42},
		{"float64 value", map[string]any{"key": float64(42.5)}, "key", 42},
		{"uint64 value", map[string]any{"key": uint64(42)}, "key", 42},
		{"invalid type", map[string]any{"key": "string"}, "key", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractInt(tt.m, tt.key)
			if result != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, result)
			}
		})
	}
}
