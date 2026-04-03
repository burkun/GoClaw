package loop

import (
	"context"
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
