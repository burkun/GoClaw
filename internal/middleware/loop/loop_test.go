package loop

import (
	"context"
	"strings"
	"testing"

	"github.com/bookerbai/goclaw/internal/middleware"
)

func makeAssistantWithTC(tc []map[string]any) map[string]any {
	return map[string]any{"role": "assistant", "tool_calls": tc}
}

func TestLoopDetectionMiddleware_After_NoLoop(t *testing.T) {
	mw := New(DefaultConfig())
	state := &middleware.State{Messages: []map[string]any{
		makeAssistantWithTC([]map[string]any{{"name": "a"}}),
		makeAssistantWithTC([]map[string]any{{"name": "b"}}),
	}}
	resp := &middleware.Response{ToolCalls: []map[string]any{{"name": "c"}}}

	_ = mw.After(context.Background(), state, resp)
	for _, m := range state.Messages {
		if m["name"] == "loop_detection" {
			t.Error("unexpected loop_detection reminder")
		}
	}
}

func TestLoopDetectionMiddleware_After_DetectsLoop(t *testing.T) {
	mw := New(Config{MaxRepeats: 2})
	tc := []map[string]any{{"name": "read_file", "arguments": `{"path":"/a"}`}}
	state := &middleware.State{Messages: []map[string]any{
		makeAssistantWithTC(tc),
		makeAssistantWithTC(tc),
	}}
	resp := &middleware.Response{ToolCalls: tc}

	_ = mw.After(context.Background(), state, resp)

	found := false
	for _, m := range state.Messages {
		if m["name"] == "loop_detection" {
			found = true
		}
	}
	if !found {
		t.Error("expected loop_detection reminder to be injected")
	}
}

func TestLoopDetectionMiddleware_After_DetectsAlternatingLoop(t *testing.T) {
	mw := New(Config{MaxRepeats: 3, AlternatingMinCycles: 2})
	a := []map[string]any{{"name": "read_file", "arguments": `{"path":"/a"}`}}
	b := []map[string]any{{"name": "grep", "arguments": `{"pattern":"x"}`}}
	state := &middleware.State{Messages: []map[string]any{
		makeAssistantWithTC(a),
		makeAssistantWithTC(b),
		makeAssistantWithTC(a),
		makeAssistantWithTC(b),
	}}
	resp := &middleware.Response{ToolCalls: b}

	_ = mw.After(context.Background(), state, resp)

	foundAlternating := false
	for _, m := range state.Messages {
		if m["name"] == "loop_detection" {
			if content, _ := m["content"].(string); strings.Contains(content, "alternating") {
				foundAlternating = true
			}
		}
	}
	if !foundAlternating {
		t.Error("expected alternating loop reminder to be injected")
	}
}

func TestCollectToolCallHashes_NormalizesArgumentOrder(t *testing.T) {
	tcA := []map[string]any{{"name": "grep", "arguments": `{"b":2,"a":1}`}}
	tcB := []map[string]any{{"name": "grep", "arguments": `{"a":1,"b":2}`}}
	messages := []map[string]any{makeAssistantWithTC(tcA), makeAssistantWithTC(tcB)}
	hashes := collectToolCallHashes(messages, 2)
	if len(hashes) != 2 {
		t.Fatalf("expected 2 hashes, got %d", len(hashes))
	}
	if hashes[0] != hashes[1] {
		t.Fatalf("expected normalized hashes to match, got %s vs %s", hashes[0], hashes[1])
	}
}
