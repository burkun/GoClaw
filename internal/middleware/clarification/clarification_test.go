package clarification

import (
	"context"
	"testing"

	"github.com/bookerbai/goclaw/internal/middleware"
)

func TestClarificationMiddleware_Name(t *testing.T) {
	mw := NewClarificationMiddleware()
	if mw.Name() != "ClarificationMiddleware" {
		t.Fatalf("unexpected name: %s", mw.Name())
	}
}

func TestClarificationMiddleware_After_NoToolCalls(t *testing.T) {
	mw := NewClarificationMiddleware()
	state := &middleware.State{}
	resp := &middleware.Response{}
	if err := mw.After(context.Background(), state, resp); err != nil {
		t.Fatalf("after returned error: %v", err)
	}
	if state.Extra != nil {
		t.Fatalf("expected no extra data")
	}
}

func TestClarificationMiddleware_After_JSONOutput(t *testing.T) {
	mw := NewClarificationMiddleware()
	state := &middleware.State{}
	resp := &middleware.Response{ToolCalls: []map[string]any{{
		"name":   "ask_clarification",
		"output": `{"question":"Which file?","options":["a","b"]}`,
	}}}
	if err := mw.After(context.Background(), state, resp); err != nil {
		t.Fatalf("after returned error: %v", err)
	}
	if state.Extra == nil || state.Extra["interrupt"] != true {
		t.Fatalf("expected interrupt=true")
	}
	req, ok := state.Extra["clarification_request"].(ClarificationRequest)
	if !ok {
		t.Fatalf("expected clarification request struct")
	}
	if req.Question != "Which file?" {
		t.Fatalf("unexpected question: %s", req.Question)
	}
}

func TestClarificationMiddleware_After_RawFallback(t *testing.T) {
	mw := NewClarificationMiddleware()
	state := &middleware.State{}
	resp := &middleware.Response{ToolCalls: []map[string]any{{
		"name":   "ask_clarification",
		"output": "not-json",
	}}}
	if err := mw.After(context.Background(), state, resp); err != nil {
		t.Fatalf("after returned error: %v", err)
	}
	if state.Extra["interrupt"] != true {
		t.Fatalf("expected interrupt=true")
	}
	if got, _ := state.Extra["clarification_request"].(string); got != "not-json" {
		t.Fatalf("unexpected fallback value: %q", got)
	}
}

func TestClarificationMiddleware_After_UsesArgumentsWhenOutputMissing(t *testing.T) {
	mw := NewClarificationMiddleware()
	state := &middleware.State{}
	resp := &middleware.Response{ToolCalls: []map[string]any{{
		"name":      "ask_clarification",
		"arguments": `{"question":"Pick one?","options":["x","y"]}`,
	}}}
	if err := mw.After(context.Background(), state, resp); err != nil {
		t.Fatalf("after returned error: %v", err)
	}
	if state.Extra["interrupt"] != true {
		t.Fatalf("expected interrupt=true")
	}
	req, ok := state.Extra["clarification_request"].(ClarificationRequest)
	if !ok || req.Question != "Pick one?" {
		t.Fatalf("unexpected clarification_request: %#v", state.Extra["clarification_request"])
	}
}

func TestClarificationMiddleware_After_UsesMapInputWhenOutputMissing(t *testing.T) {
	mw := NewClarificationMiddleware()
	state := &middleware.State{}
	resp := &middleware.Response{ToolCalls: []map[string]any{{
		"name": "ask_clarification",
		"input": map[string]any{
			"question": "Continue?",
			"options":  []any{"yes", "no"},
		},
	}}}
	if err := mw.After(context.Background(), state, resp); err != nil {
		t.Fatalf("after returned error: %v", err)
	}
	if state.Extra["interrupt"] != true {
		t.Fatalf("expected interrupt=true")
	}
	req, ok := state.Extra["clarification_request"].(ClarificationRequest)
	if !ok || req.Question != "Continue?" {
		t.Fatalf("unexpected clarification_request: %#v", state.Extra["clarification_request"])
	}
}

func TestClarificationMiddleware_WrapToolCall_InterceptsAskClarification(t *testing.T) {
	mw := NewClarificationMiddleware()
	state := &middleware.State{}
	called := false

	res, err := mw.WrapToolCall(
		context.Background(),
		state,
		&middleware.ToolCall{ID: "1", Name: "ask_clarification", Input: map[string]any{"question": "Need detail?", "options": []string{"a", "b"}}},
		func(ctx context.Context, toolCall *middleware.ToolCall) (*middleware.ToolResult, error) {
			called = true
			return &middleware.ToolResult{ID: toolCall.ID, Output: "unexpected"}, nil
		},
	)

	if err != nil {
		t.Fatalf("wrap_tool_call returned error: %v", err)
	}
	if called {
		t.Fatalf("expected handler not to be called for ask_clarification")
	}
	if res == nil || res.ID != "1" {
		t.Fatalf("unexpected result: %#v", res)
	}
	if state.Extra == nil || state.Extra["interrupt"] != true {
		t.Fatalf("expected interrupt=true")
	}
	req, ok := state.Extra["clarification_request"].(ClarificationRequest)
	if !ok || req.Question != "Need detail?" {
		t.Fatalf("unexpected clarification_request: %#v", state.Extra["clarification_request"])
	}
}

func TestClarificationMiddleware_WrapToolCall_PassThroughOtherTools(t *testing.T) {
	mw := NewClarificationMiddleware()
	state := &middleware.State{}
	called := false

	res, err := mw.WrapToolCall(
		context.Background(),
		state,
		&middleware.ToolCall{ID: "2", Name: "read_file", Input: map[string]any{"file_path": "/tmp/a"}},
		func(ctx context.Context, toolCall *middleware.ToolCall) (*middleware.ToolResult, error) {
			called = true
			return &middleware.ToolResult{ID: toolCall.ID, Output: "ok"}, nil
		},
	)

	if err != nil {
		t.Fatalf("wrap_tool_call returned error: %v", err)
	}
	if !called {
		t.Fatalf("expected handler to be called for non-clarification tool")
	}
	if res == nil || res.Output != "ok" {
		t.Fatalf("unexpected result: %#v", res)
	}
}
