package control

import (
	"context"
	"errors"
	"testing"

	"github.com/bookerbai/goclaw/internal/middleware"
)

func TestSubagentLimitMiddleware_Before_NotSubagent(t *testing.T) {
	mw := NewSubagentLimitMiddleware(SubagentLimitConfig{MaxConcurrent: 1})
	state := &middleware.State{Extra: map[string]any{}}
	if err := mw.BeforeModel(context.Background(), state); err != nil {
		t.Errorf("unexpected error for non-subagent: %v", err)
	}
	if mw.Current() != 0 {
		t.Errorf("counter should remain 0, got %d", mw.Current())
	}
}

func TestSubagentLimitMiddleware_Before_AllowsUnderLimit(t *testing.T) {
	mw := NewSubagentLimitMiddleware(SubagentLimitConfig{MaxConcurrent: 2})
	state := &middleware.State{Extra: map[string]any{"is_subagent": true}}
	if err := mw.BeforeModel(context.Background(), state); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if mw.Current() != 1 {
		t.Errorf("expected counter 1, got %d", mw.Current())
	}
}

func TestSubagentLimitMiddleware_Before_RejectsOverLimit(t *testing.T) {
	mw := NewSubagentLimitMiddleware(SubagentLimitConfig{MaxConcurrent: 1})

	state1 := &middleware.State{Extra: map[string]any{"is_subagent": true}}
	_ = mw.BeforeModel(context.Background(), state1)

	state2 := &middleware.State{Extra: map[string]any{"is_subagent": true}}
	err := mw.BeforeModel(context.Background(), state2)
	if !errors.Is(err, ErrSubagentLimitReached) {
		t.Errorf("expected ErrSubagentLimitReached, got %v", err)
	}
}

func TestSubagentLimitMiddleware_After_Decrements(t *testing.T) {
	mw := NewSubagentLimitMiddleware(SubagentLimitConfig{MaxConcurrent: 2})
	state := &middleware.State{Extra: map[string]any{"is_subagent": true}}
	_ = mw.BeforeModel(context.Background(), state)
	_ = mw.AfterModel(context.Background(), state, &middleware.Response{})
	if mw.Current() != 0 {
		t.Errorf("expected counter 0 after After, got %d", mw.Current())
	}
}

func TestSubagentLimitMiddleware_WrapToolCall_WithinLimit(t *testing.T) {
	mw := NewSubagentLimitMiddleware(SubagentLimitConfig{MaxConcurrent: 2})
	state := &middleware.State{Extra: map[string]any{}}
	_ = mw.BeforeModel(context.Background(), state)

	called := false
	res, err := mw.WrapToolCall(context.Background(), state, &middleware.ToolCall{ID: "1", Name: "task"}, func(ctx context.Context, toolCall *middleware.ToolCall) (*middleware.ToolResult, error) {
		called = true
		return &middleware.ToolResult{ID: toolCall.ID, Output: "ok"}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatalf("expected handler to be called")
	}
	if res == nil || res.Output != "ok" {
		t.Fatalf("unexpected result: %#v", res)
	}
}

func TestSubagentLimitMiddleware_WrapToolCall_TruncatesExcess(t *testing.T) {
	mw := NewSubagentLimitMiddleware(SubagentLimitConfig{MaxConcurrent: 1})
	state := &middleware.State{Extra: map[string]any{}}
	_ = mw.BeforeModel(context.Background(), state)

	// First task call executes normally.
	_, err := mw.WrapToolCall(context.Background(), state, &middleware.ToolCall{ID: "1", Name: "task"}, func(ctx context.Context, toolCall *middleware.ToolCall) (*middleware.ToolResult, error) {
		return &middleware.ToolResult{ID: toolCall.ID, Output: "ok"}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	called := false
	res, err := mw.WrapToolCall(context.Background(), state, &middleware.ToolCall{ID: "2", Name: "task"}, func(ctx context.Context, toolCall *middleware.ToolCall) (*middleware.ToolResult, error) {
		called = true
		return &middleware.ToolResult{ID: toolCall.ID, Output: "should-not-run"}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Fatalf("expected second handler not to be called")
	}
	if res == nil {
		t.Fatalf("expected synthetic result")
	}
	if _, ok := res.Output.(string); !ok {
		t.Fatalf("expected string output, got %#v", res.Output)
	}
}
