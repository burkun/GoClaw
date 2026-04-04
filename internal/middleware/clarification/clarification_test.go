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
