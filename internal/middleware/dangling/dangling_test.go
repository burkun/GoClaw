package dangling

import (
	"context"
	"testing"

	"github.com/bookerbai/goclaw/internal/middleware"
)

func TestDanglingToolCallMiddleware_Before_NoMessages(t *testing.T) {
	mw := New()
	state := &middleware.State{Messages: nil}
	if err := mw.BeforeModel(context.Background(), state); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDanglingToolCallMiddleware_Before_NoAssistant(t *testing.T) {
	mw := New()
	state := &middleware.State{Messages: []map[string]any{
		{"role": "human", "content": "hi"},
	}}
	if err := mw.BeforeModel(context.Background(), state); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(state.Messages) != 1 {
		t.Errorf("expected 1 message, got %d", len(state.Messages))
	}
}

func TestDanglingToolCallMiddleware_Before_NoDangling(t *testing.T) {
	mw := New()
	state := &middleware.State{Messages: []map[string]any{
		{"role": "human", "content": "hi"},
		{"role": "assistant", "tool_calls": []map[string]any{{"id": "tc1"}}},
		{"role": "tool", "tool_call_id": "tc1", "content": "ok"},
	}}
	_ = mw.BeforeModel(context.Background(), state)
	if len(state.Messages) != 3 {
		t.Errorf("expected 3 messages (no placeholder), got %d", len(state.Messages))
	}
}

func TestDanglingToolCallMiddleware_Before_InsertsPlaceholder(t *testing.T) {
	mw := New()
	state := &middleware.State{Messages: []map[string]any{
		{"role": "human", "content": "hi"},
		{"role": "assistant", "tool_calls": []map[string]any{{"id": "tc1"}, {"id": "tc2"}}},
		{"role": "tool", "tool_call_id": "tc1", "content": "ok"},
	}}
	_ = mw.BeforeModel(context.Background(), state)
	if len(state.Messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(state.Messages))
	}
	// Placeholder should be inserted right after assistant message (index 2)
	placeholder := state.Messages[2]
	if placeholder["tool_call_id"] != "tc2" {
		t.Errorf("expected placeholder for tc2 at index 2, got %v", placeholder)
	}
	// Original tool response should be at index 3
	originalTool := state.Messages[3]
	if originalTool["tool_call_id"] != "tc1" {
		t.Errorf("expected original tool response for tc1 at index 3, got %v", originalTool)
	}
}
