package control

import (
	"testing"
	"time"
)

func TestNewGuardrailRequest(t *testing.T) {
	req := NewGuardrailRequest("read_file", map[string]any{"path": "/tmp"}, "agent-1", "thread-1", true)

	if req.ToolName != "read_file" {
		t.Errorf("expected ToolName 'read_file', got %s", req.ToolName)
	}
	if req.AgentID != "agent-1" {
		t.Errorf("expected AgentID 'agent-1', got %s", req.AgentID)
	}
	if req.ThreadID != "thread-1" {
		t.Errorf("expected ThreadID 'thread-1', got %s", req.ThreadID)
	}
	if !req.IsSubagent {
		t.Error("expected IsSubagent to be true")
	}
	if req.Timestamp == "" {
		t.Error("expected Timestamp to be set")
	}
	// Verify timestamp is valid RFC3339
	_, err := time.Parse(time.RFC3339, req.Timestamp)
	if err != nil {
		t.Errorf("invalid timestamp format: %v", err)
	}
}

func TestDecisionAllowed(t *testing.T) {
	decision := DecisionAllowed()

	if !decision.Allow {
		t.Error("expected Allow to be true")
	}
	if len(decision.Reasons) != 1 {
		t.Fatalf("expected 1 reason, got %d", len(decision.Reasons))
	}
	if decision.Reasons[0].Code != ReasonAllowed {
		t.Errorf("expected code %s, got %s", ReasonAllowed, decision.Reasons[0].Code)
	}
}

func TestDecisionDenied(t *testing.T) {
	decision := DecisionDenied(ReasonToolNotAllowed, "test message")

	if decision.Allow {
		t.Error("expected Allow to be false")
	}
	if len(decision.Reasons) != 1 {
		t.Fatalf("expected 1 reason, got %d", len(decision.Reasons))
	}
	if decision.Reasons[0].Code != ReasonToolNotAllowed {
		t.Errorf("expected code %s, got %s", ReasonToolNotAllowed, decision.Reasons[0].Code)
	}
	if decision.Reasons[0].Message != "test message" {
		t.Errorf("expected message 'test message', got %s", decision.Reasons[0].Message)
	}
}

func TestDecisionDeniedWithPolicy(t *testing.T) {
	decision := DecisionDeniedWithPolicy(ReasonToolNotAllowed, "test message", "policy-123")

	if decision.Allow {
		t.Error("expected Allow to be false")
	}
	if decision.PolicyID != "policy-123" {
		t.Errorf("expected PolicyID 'policy-123', got %s", decision.PolicyID)
	}
}

func TestGuardrailReason(t *testing.T) {
	reason := GuardrailReason{
		Code:    ReasonBlockedPattern,
		Message: "pattern matched",
	}

	if reason.Code != ReasonBlockedPattern {
		t.Errorf("expected code %s, got %s", ReasonBlockedPattern, reason.Code)
	}
	if reason.Message != "pattern matched" {
		t.Errorf("expected message 'pattern matched', got %s", reason.Message)
	}
}

func TestGuardrailDecision_Metadata(t *testing.T) {
	decision := GuardrailDecision{
		Allow:    true,
		Reasons:  []GuardrailReason{{Code: ReasonAllowed}},
		PolicyID: "test-policy",
		Metadata: map[string]any{"key": "value"},
	}

	if decision.Metadata["key"] != "value" {
		t.Errorf("expected metadata key='value', got %v", decision.Metadata["key"])
	}
}

func TestGuardrailDecision_MultipleReasons(t *testing.T) {
	decision := GuardrailDecision{
		Allow: false,
		Reasons: []GuardrailReason{
			{Code: ReasonToolNotAllowed, Message: "tool denied"},
			{Code: ReasonLimitExceeded, Message: "limit reached"},
		},
	}

	if len(decision.Reasons) != 2 {
		t.Fatalf("expected 2 reasons, got %d", len(decision.Reasons))
	}
}

func TestReasonCodes(t *testing.T) {
	codes := []string{
		ReasonAllowed,
		ReasonToolNotAllowed,
		ReasonCommandNotAllowed,
		ReasonBlockedPattern,
		ReasonLimitExceeded,
		ReasonPassportSuspended,
		ReasonEvaluatorError,
	}

	for _, code := range codes {
		if code == "" {
			t.Errorf("reason code should not be empty")
		}
	}
}
