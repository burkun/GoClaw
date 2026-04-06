package guardrail

import (
	"context"
	"testing"

	"github.com/bookerbai/goclaw/internal/middleware"
)

func TestGuardrailMiddleware_Before_Disabled(t *testing.T) {
	mw := NewGuardrailMiddleware(Config{Enabled: false})
	state := &middleware.State{Extra: map[string]any{"pending_tool_calls": []map[string]any{{"name": "bash"}}}}
	if err := mw.BeforeModel(context.Background(), state); err != nil {
		t.Fatalf("before returned error: %v", err)
	}
	tc := state.Extra["pending_tool_calls"].([]map[string]any)[0]
	if _, ok := tc["guardrail_decision"]; ok {
		t.Fatalf("expected no decision when disabled")
	}
}

func TestGuardrailMiddleware_Before_Decisions(t *testing.T) {
	mw := NewGuardrailMiddleware(Config{
		Enabled:         true,
		Policies:        []Policy{{ToolPattern: "bash", Decision: DecisionDeny, Reason: "unsafe"}, {ToolPattern: "web*", Decision: DecisionAsk, Reason: "approve"}},
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
