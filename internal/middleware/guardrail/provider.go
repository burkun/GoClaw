// Package guardrail implements GuardrailMiddleware for GoClaw.
//
// GuardrailMiddleware provides authorization checks before tool execution,
// implementing permit/deny/ask decision logic based on configurable policies.
// This implementation aligns with DeerFlow's GuardrailMiddleware and OAP standard.
package guardrail

import (
	"context"
	"time"
)

// GuardrailRequest is the context passed to the provider for each tool call.
// It mirrors DeerFlow's GuardrailRequest dataclass.
type GuardrailRequest struct {
	// ToolName is the name of the tool being invoked.
	ToolName string

	// ToolInput is the tool arguments (usually map[string]any).
	ToolInput map[string]any

	// AgentID is passed from config.passport (optional).
	AgentID string

	// ThreadID is the conversation identifier (optional).
	ThreadID string

	// IsSubagent indicates if this is a sub-agent request.
	IsSubagent bool

	// Timestamp is the ISO-8601 timestamp of the request.
	Timestamp string
}

// GuardrailReason is a structured reason for an allow/deny decision (OAP reason object).
type GuardrailReason struct {
	// Code is the OAP reason code (e.g., "oap.allowed", "oap.tool_not_allowed").
	Code string

	// Message is the human-readable explanation.
	Message string
}

// GuardrailDecision is the provider's allow/deny verdict (aligned with OAP Decision object).
type GuardrailDecision struct {
	// Allow indicates whether the tool call should proceed.
	Allow bool

	// Reasons contains structured explanations for the decision.
	Reasons []GuardrailReason

	// PolicyID is an optional identifier for the policy that matched.
	PolicyID string

	// Metadata contains additional provider-specific data.
	Metadata map[string]any
}

// GuardrailProvider is the contract for pluggable tool-call authorization.
// Any struct with these methods works - no base class required.
// Providers are configured via guardrails.provider in config.yaml.
type GuardrailProvider interface {
	// Name returns the provider identifier for logging.
	Name() string

	// Evaluate returns an authorization decision for the given request.
	Evaluate(ctx context.Context, request GuardrailRequest) (GuardrailDecision, error)
}

// OAP Reason Codes (standard codes from the OAP specification).
const (
	ReasonAllowed           = "oap.allowed"
	ReasonToolNotAllowed    = "oap.tool_not_allowed"
	ReasonCommandNotAllowed = "oap.command_not_allowed"
	ReasonBlockedPattern    = "oap.blocked_pattern"
	ReasonLimitExceeded     = "oap.limit_exceeded"
	ReasonPassportSuspended = "oap.passport_suspended"
	ReasonEvaluatorError    = "oap.evaluator_error"
)

// NewGuardrailRequest creates a GuardrailRequest from tool call parameters.
func NewGuardrailRequest(toolName string, toolInput map[string]any, agentID, threadID string, isSubagent bool) GuardrailRequest {
	return GuardrailRequest{
		ToolName:   toolName,
		ToolInput:  toolInput,
		AgentID:    agentID,
		ThreadID:   threadID,
		IsSubagent: isSubagent,
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
	}
}

// DecisionAllowed returns a simple allow decision.
func DecisionAllowed() GuardrailDecision {
	return GuardrailDecision{
		Allow:   true,
		Reasons: []GuardrailReason{{Code: ReasonAllowed}},
	}
}

// DecisionDenied returns a deny decision with a single reason.
func DecisionDenied(code, message string) GuardrailDecision {
	return GuardrailDecision{
		Allow:   false,
		Reasons: []GuardrailReason{{Code: code, Message: message}},
	}
}

// DecisionDeniedWithPolicy returns a deny decision with policy ID.
func DecisionDeniedWithPolicy(code, message, policyID string) GuardrailDecision {
	return GuardrailDecision{
		Allow:    false,
		Reasons:  []GuardrailReason{{Code: code, Message: message}},
		PolicyID: policyID,
	}
}
