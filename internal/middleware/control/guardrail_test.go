package control

import (
	"context"
	"errors"
	"testing"

	"github.com/bookerbai/goclaw/internal/middleware"
)

// mockProvider is a mock GuardrailProvider for testing
type mockProvider struct {
	decision GuardrailDecision
	err      error
}

func (m *mockProvider) Name() string { return "mock" }

func (m *mockProvider) Evaluate(ctx context.Context, request GuardrailRequest) (GuardrailDecision, error) {
	return m.decision, m.err
}

func TestGuardrailMiddleware_Name(t *testing.T) {
	mw := NewGuardrailMiddleware(GuardrailConfig{})
	if mw.Name() != "GuardrailMiddleware" {
		t.Errorf("expected name 'GuardrailMiddleware', got %s", mw.Name())
	}
}

func TestGuardrailMiddleware_Before_Disabled(t *testing.T) {
	mw := NewGuardrailMiddleware(GuardrailConfig{Enabled: false})
	state := &middleware.State{Extra: map[string]any{"pending_tool_calls": []map[string]any{{"name": "bash"}}}}
	if err := mw.BeforeModel(context.Background(), state); err != nil {
		t.Fatalf("before returned error: %v", err)
	}
	tc := state.Extra["pending_tool_calls"].([]map[string]any)[0]
	if _, ok := tc["guardrail_decision"]; ok {
		t.Fatalf("expected no decision when disabled")
	}
}

func TestGuardrailMiddleware_Before_NilState(t *testing.T) {
	mw := NewGuardrailMiddleware(GuardrailConfig{Enabled: true})
	err := mw.BeforeModel(context.Background(), nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGuardrailMiddleware_Before_NilExtra(t *testing.T) {
	mw := NewGuardrailMiddleware(GuardrailConfig{Enabled: true})
	state := &middleware.State{}
	err := mw.BeforeModel(context.Background(), state)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGuardrailMiddleware_Before_NoPendingTools(t *testing.T) {
	mw := NewGuardrailMiddleware(GuardrailConfig{Enabled: true})
	state := &middleware.State{Extra: map[string]any{}}
	err := mw.BeforeModel(context.Background(), state)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGuardrailMiddleware_Before_Decisions(t *testing.T) {
	mw := NewGuardrailMiddleware(GuardrailConfig{
		Enabled:         true,
		Policies:        []GuardrailPolicy{{ToolPattern: "bash", Decision: DecisionDeny, Reason: "unsafe"}, {ToolPattern: "web*", Decision: DecisionAsk, Reason: "approve"}},
		DefaultDecision: DecisionPermit,
	})
	state := &middleware.State{Extra: map[string]any{"pending_tool_calls": []map[string]any{{"name": "bash"}, {"name": "web_search"}, {"name": "read_file"}}}}
	if err := mw.BeforeModel(context.Background(), state); err != nil {
		t.Fatalf("before returned error: %v", err)
	}
	calls := state.Extra["pending_tool_calls"].([]map[string]any)
	if calls[0]["guardrail_decision"] != "deny" || calls[0]["blocked"] != true {
		t.Fatalf("bash decision mismatch: %#v", calls[0])
	}
	if calls[1]["guardrail_decision"] != "ask" || calls[1]["requires_approval"] != true {
		t.Fatalf("web decision mismatch: %#v", calls[1])
	}
	if calls[2]["guardrail_decision"] != "permit" {
		t.Fatalf("default decision mismatch: %#v", calls[2])
	}
}

func TestGuardrailMiddleware_After_Disabled(t *testing.T) {
	mw := NewGuardrailMiddleware(GuardrailConfig{Enabled: false})
	state := &middleware.State{}
	resp := &middleware.Response{ToolCalls: []map[string]any{{"name": "bash"}}}
	err := mw.AfterModel(context.Background(), state, resp)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGuardrailMiddleware_After_NilResponse(t *testing.T) {
	mw := NewGuardrailMiddleware(GuardrailConfig{Enabled: true})
	err := mw.AfterModel(context.Background(), &middleware.State{}, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGuardrailMiddleware_After_EmptyToolCalls(t *testing.T) {
	mw := NewGuardrailMiddleware(GuardrailConfig{Enabled: true})
	err := mw.AfterModel(context.Background(), &middleware.State{}, &middleware.Response{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGuardrailMiddleware_After_Decisions(t *testing.T) {
	mw := NewGuardrailMiddleware(GuardrailConfig{
		Enabled:         true,
		Policies:        []GuardrailPolicy{{ToolPattern: "bash", Decision: DecisionDeny, Reason: "unsafe"}},
		DefaultDecision: DecisionPermit,
	})
	state := &middleware.State{}
	resp := &middleware.Response{ToolCalls: []map[string]any{{"name": "bash"}}}
	err := mw.AfterModel(context.Background(), state, resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state.Extra == nil {
		t.Fatal("expected Extra to be set")
	}
	pending, ok := state.Extra["pending_tool_calls"].([]map[string]any)
	if !ok {
		t.Fatal("expected pending_tool_calls")
	}
	if pending[0]["guardrail_decision"] != "deny" {
		t.Errorf("expected deny, got %v", pending[0]["guardrail_decision"])
	}
}

func TestGuardrailMiddleware_WrapToolCall_Disabled(t *testing.T) {
	mw := NewGuardrailMiddleware(GuardrailConfig{Enabled: false})
	state := &middleware.State{ThreadID: "thread-1"}
	toolCall := &middleware.ToolCall{ID: "1", Name: "bash", Input: map[string]any{}}

	called := false
	result, err := mw.WrapToolCall(context.Background(), state, toolCall, func(ctx context.Context, tc *middleware.ToolCall) (*middleware.ToolResult, error) {
		called = true
		return &middleware.ToolResult{ID: tc.ID, Output: "ok"}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected handler to be called")
	}
	if result.Output != "ok" {
		t.Errorf("expected output 'ok', got %v", result.Output)
	}
}

func TestGuardrailMiddleware_WrapToolCall_Allowed(t *testing.T) {
	mw := NewGuardrailMiddleware(GuardrailConfig{
		Enabled: true,
		Provider: &mockProvider{
			decision: DecisionAllowed(),
		},
	})
	state := &middleware.State{ThreadID: "thread-1"}
	toolCall := &middleware.ToolCall{ID: "1", Name: "read_file", Input: map[string]any{"path": "/tmp"}}

	called := false
	result, err := mw.WrapToolCall(context.Background(), state, toolCall, func(ctx context.Context, tc *middleware.ToolCall) (*middleware.ToolResult, error) {
		called = true
		return &middleware.ToolResult{ID: tc.ID, Output: "content"}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected handler to be called")
	}
	if result.Output != "content" {
		t.Errorf("expected output 'content', got %v", result.Output)
	}
}

func TestGuardrailMiddleware_WrapToolCall_Denied(t *testing.T) {
	mw := NewGuardrailMiddleware(GuardrailConfig{
		Enabled: true,
		Provider: &mockProvider{
			decision: DecisionDenied(ReasonToolNotAllowed, "tool not allowed"),
		},
	})
	state := &middleware.State{ThreadID: "thread-1"}
	toolCall := &middleware.ToolCall{ID: "1", Name: "bash", Input: map[string]any{}}

	called := false
	result, err := mw.WrapToolCall(context.Background(), state, toolCall, func(ctx context.Context, tc *middleware.ToolCall) (*middleware.ToolResult, error) {
		called = true
		return &middleware.ToolResult{ID: tc.ID, Output: "should not run"}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Error("expected handler NOT to be called")
	}
	if result.Error == nil {
		t.Error("expected error in result")
	}
}

func TestGuardrailMiddleware_WrapToolCall_ProviderError_FailClosed(t *testing.T) {
	mw := NewGuardrailMiddleware(GuardrailConfig{
		Enabled:    true,
		FailClosed: true,
		Provider: &mockProvider{
			err: errors.New("provider error"),
		},
	})
	state := &middleware.State{ThreadID: "thread-1"}
	toolCall := &middleware.ToolCall{ID: "1", Name: "bash", Input: map[string]any{}}

	called := false
	result, err := mw.WrapToolCall(context.Background(), state, toolCall, func(ctx context.Context, tc *middleware.ToolCall) (*middleware.ToolResult, error) {
		called = true
		return &middleware.ToolResult{ID: tc.ID, Output: "should not run"}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Error("expected handler NOT to be called")
	}
	if result.Error == nil {
		t.Error("expected error in result")
	}
}

func TestGuardrailMiddleware_WrapToolCall_ProviderError_FailOpen(t *testing.T) {
	mw := NewGuardrailMiddleware(GuardrailConfig{
		Enabled:    true,
		FailClosed: false,
		Provider: &mockProvider{
			err: errors.New("provider error"),
		},
	})
	state := &middleware.State{ThreadID: "thread-1"}
	toolCall := &middleware.ToolCall{ID: "1", Name: "bash", Input: map[string]any{}}

	called := false
	result, err := mw.WrapToolCall(context.Background(), state, toolCall, func(ctx context.Context, tc *middleware.ToolCall) (*middleware.ToolResult, error) {
		called = true
		return &middleware.ToolResult{ID: tc.ID, Output: "ok"}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected handler to be called (fail-open)")
	}
	if result.Output != "ok" {
		t.Errorf("expected output 'ok', got %v", result.Output)
	}
}

func TestGuardrailMiddleware_WrapToolCall_LegacyFallback(t *testing.T) {
	mw := NewGuardrailMiddleware(GuardrailConfig{
		Enabled: true,
		Policies: []GuardrailPolicy{
			{ToolPattern: "bash", Decision: DecisionDeny, Reason: "unsafe"},
		},
	})
	state := &middleware.State{ThreadID: "thread-1"}
	toolCall := &middleware.ToolCall{ID: "1", Name: "bash", Input: map[string]any{}}

	result, err := mw.WrapToolCall(context.Background(), state, toolCall, func(ctx context.Context, tc *middleware.ToolCall) (*middleware.ToolResult, error) {
		return &middleware.ToolResult{ID: tc.ID, Output: "should not run"}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error == nil {
		t.Error("expected error in result")
	}
}

func TestGuardrailMiddleware_BuildRequest(t *testing.T) {
	mw := NewGuardrailMiddleware(GuardrailConfig{
		Passport: "agent-123",
	})
	state := &middleware.State{
		ThreadID: "thread-1",
		Extra:    map[string]any{"is_subagent": true},
	}
	toolCall := &middleware.ToolCall{
		ID:   "call-1",
		Name: "read_file",
		Input: map[string]any{
			"path": "/tmp/file.txt",
		},
	}

	req := mw.buildRequest(state, toolCall)

	if req.ToolName != "read_file" {
		t.Errorf("expected ToolName 'read_file', got %s", req.ToolName)
	}
	if req.AgentID != "agent-123" {
		t.Errorf("expected AgentID 'agent-123', got %s", req.AgentID)
	}
	if req.ThreadID != "thread-1" {
		t.Errorf("expected ThreadID 'thread-1', got %s", req.ThreadID)
	}
	if !req.IsSubagent {
		t.Error("expected IsSubagent to be true")
	}
}

func TestGuardrailMiddleware_BuildRequest_NilState(t *testing.T) {
	mw := NewGuardrailMiddleware(GuardrailConfig{
		Passport: "agent-123",
	})
	toolCall := &middleware.ToolCall{
		ID:     "call-1",
		Name:   "read_file",
		Input:  map[string]any{},
	}

	req := mw.buildRequest(nil, toolCall)

	if req.ThreadID != "" {
		t.Errorf("expected empty ThreadID, got %s", req.ThreadID)
	}
	if req.IsSubagent {
		t.Error("expected IsSubagent to be false")
	}
}

func TestGuardrailMiddleware_FirstReasonCode(t *testing.T) {
	mw := &GuardrailMiddleware{}

	decision := GuardrailDecision{
		Reasons: []GuardrailReason{
			{Code: ReasonToolNotAllowed, Message: "test"},
		},
	}
	if mw.firstReasonCode(decision) != ReasonToolNotAllowed {
		t.Errorf("expected %s, got %s", ReasonToolNotAllowed, mw.firstReasonCode(decision))
	}

	emptyDecision := GuardrailDecision{}
	if mw.firstReasonCode(emptyDecision) != "unknown" {
		t.Errorf("expected 'unknown', got %s", mw.firstReasonCode(emptyDecision))
	}
}

func TestGuardrailMiddleware_DefaultConfig(t *testing.T) {
	cfg := DefaultGuardrailConfig()
	if cfg.Enabled {
		t.Error("expected Enabled to be false")
	}
	if !cfg.FailClosed {
		t.Error("expected FailClosed to be true")
	}
	if cfg.DefaultDecision != DecisionPermit {
		t.Errorf("expected DefaultDecision 'permit', got %s", cfg.DefaultDecision)
	}
}

func TestMatchPattern(t *testing.T) {
	if !matchPattern("*", "bash") {
		t.Fatalf("* should match")
	}
	if !matchPattern("bash*", "bash_exec") {
		t.Fatalf("prefix glob should match")
	}
	if !matchPattern("*file", "read_file") {
		t.Fatalf("suffix glob should match")
	}
	if !matchPattern("*edit*", "multi_edit") {
		t.Fatalf("contains glob should match")
	}
	if matchPattern("bash", "grep") {
		t.Fatalf("exact mismatch should not match")
	}
}

func TestMatchPattern_CaseInsensitive(t *testing.T) {
	if !matchPattern("BASH", "bash") {
		t.Error("expected case-insensitive match")
	}
	if !matchPattern("bash", "BASH") {
		t.Error("expected case-insensitive match")
	}
}

func TestMatchPattern_Exact(t *testing.T) {
	if !matchPattern("bash", "bash") {
		t.Error("expected exact match")
	}
	if matchPattern("bash", "bash_exec") {
		t.Error("expected no match for partial")
	}
}
