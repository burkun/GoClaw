package subagentlimit

import (
	"context"
	"errors"
	"testing"

	"github.com/bookerbai/goclaw/internal/middleware"
)

func TestSubagentLimitMiddleware_Before_NotSubagent(t *testing.T) {
	mw := New(Config{MaxConcurrent: 1})
	state := &middleware.State{Extra: map[string]any{}}
	if err := mw.Before(context.Background(), state); err != nil {
		t.Errorf("unexpected error for non-subagent: %v", err)
	}
	if mw.Current() != 0 {
		t.Errorf("counter should remain 0, got %d", mw.Current())
	}
}

func TestSubagentLimitMiddleware_Before_AllowsUnderLimit(t *testing.T) {
	mw := New(Config{MaxConcurrent: 2})
	state := &middleware.State{Extra: map[string]any{"is_subagent": true}}
	if err := mw.Before(context.Background(), state); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if mw.Current() != 1 {
		t.Errorf("expected counter 1, got %d", mw.Current())
	}
}

func TestSubagentLimitMiddleware_Before_RejectsOverLimit(t *testing.T) {
	mw := New(Config{MaxConcurrent: 1})

	state1 := &middleware.State{Extra: map[string]any{"is_subagent": true}}
	_ = mw.Before(context.Background(), state1)

	state2 := &middleware.State{Extra: map[string]any{"is_subagent": true}}
	err := mw.Before(context.Background(), state2)
	if !errors.Is(err, ErrSubagentLimitReached) {
		t.Errorf("expected ErrSubagentLimitReached, got %v", err)
	}
}

func TestSubagentLimitMiddleware_After_Decrements(t *testing.T) {
	mw := New(Config{MaxConcurrent: 2})
	state := &middleware.State{Extra: map[string]any{"is_subagent": true}}
	_ = mw.Before(context.Background(), state)
	_ = mw.After(context.Background(), state, &middleware.Response{})
	if mw.Current() != 0 {
		t.Errorf("expected counter 0 after After, got %d", mw.Current())
	}
}
