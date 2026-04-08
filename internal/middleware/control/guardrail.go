// Package control implements control-flow middleware for GoClaw.
//
// This package contains middlewares that control agent behavior and enforce limits,
// including loop detection, guardrails, and subagent limits.
package control

import (
	"context"
	"fmt"
	"strings"

	"github.com/bookerbai/goclaw/internal/logging"
	"github.com/bookerbai/goclaw/internal/middleware"
	"github.com/bookerbai/goclaw/pkg/errors"
)

// GuardrailDecision represents a guardrail authorization decision (legacy, kept for compatibility).
type GuardrailDecisionType string

const (
	DecisionPermit GuardrailDecisionType = "permit"
	DecisionDeny   GuardrailDecisionType = "deny"
	DecisionAsk    GuardrailDecisionType = "ask"
)

// GuardrailPolicy defines a guardrail policy rule (legacy, kept for compatibility).
type GuardrailPolicy struct {
	// ToolPattern is a glob pattern for matching tool names.
	ToolPattern string

	// Decision is the authorization result for matching tools.
	Decision GuardrailDecisionType

	// Reason is an optional explanation for the decision.
	Reason string
}

// GuardrailConfig holds guardrail configuration.
type GuardrailConfig struct {
	// Enabled controls whether guardrail checks run.
	Enabled bool

	// FailClosed controls behavior when provider errors.
	// If true (default), deny on error. If false, allow on error.
	FailClosed bool

	// Passport is passed to provider as request.agent_id.
	Passport string

	// Provider is the GuardrailProvider implementation.
	Provider GuardrailProvider

	// Policies is the ordered list of policy rules (legacy, for backward compatibility).
	Policies []GuardrailPolicy

	// DefaultDecision is used when no policy matches (legacy).
	DefaultDecision GuardrailDecisionType
}

// DefaultGuardrailConfig returns a permissive default configuration.
func DefaultGuardrailConfig() GuardrailConfig {
	return GuardrailConfig{
		Enabled:         false,
		FailClosed:      true,
		DefaultDecision: DecisionPermit,
		Policies:        nil,
	}
}

// GuardrailMiddleware enforces authorization policies.
// It implements the Middleware interface with full WrapToolCall support.
type GuardrailMiddleware struct {
	middleware.MiddlewareWrapper
	cfg GuardrailConfig
}

// NewGuardrailMiddleware constructs a GuardrailMiddleware.
func NewGuardrailMiddleware(cfg GuardrailConfig) *GuardrailMiddleware {
	// Ensure FailClosed defaults to true for safety.
	if cfg.Enabled && !cfg.FailClosed {
		logging.Warn("guardrail: fail_closed is false, provider errors will allow tool calls")
	}
	return &GuardrailMiddleware{cfg: cfg}
}

// Name implements middleware.Middleware.
func (m *GuardrailMiddleware) Name() string { return "GuardrailMiddleware" }

// BeforeModel checks authorization for pending tool calls (legacy support).
// This is kept for backward compatibility with code that uses pending_tool_calls.
func (m *GuardrailMiddleware) BeforeModel(_ context.Context, state *middleware.State) error {
	if !m.cfg.Enabled || state == nil || state.Extra == nil {
		return nil
	}
	pendingTools, ok := state.Extra["pending_tool_calls"].([]map[string]any)
	if !ok || len(pendingTools) == 0 {
		return nil
	}
	m.applyDecisions(pendingTools)
	state.Extra["pending_tool_calls"] = pendingTools
	return nil
}

// AfterModel applies authorization checks to current response tool calls (legacy support).
func (m *GuardrailMiddleware) AfterModel(_ context.Context, state *middleware.State, resp *middleware.Response) error {
	if !m.cfg.Enabled || resp == nil || len(resp.ToolCalls) == 0 {
		return nil
	}
	m.applyDecisions(resp.ToolCalls)
	if state != nil {
		if state.Extra == nil {
			state.Extra = map[string]any{}
		}
		state.Extra["pending_tool_calls"] = resp.ToolCalls
	}
	return nil
}

// WrapToolCall intercepts tool calls and evaluates them against the guardrail provider.
// This is the primary interception point for OAP-compliant authorization.
// If the tool is denied, it returns a ToolResult with an error message instead of executing the tool.
func (m *GuardrailMiddleware) WrapToolCall(ctx context.Context, state *middleware.State, toolCall *middleware.ToolCall, handler middleware.ToolHandler) (*middleware.ToolResult, error) {
	// If guardrail is disabled, pass through to the actual tool handler.
	if !m.cfg.Enabled {
		return handler(ctx, toolCall)
	}

	// Build the guardrail request.
	req := m.buildRequest(state, toolCall)

	// Evaluate against the provider.
	decision, err := m.evaluateWithProvider(ctx, req)
	if err != nil {
		// Provider error - handle based on fail_closed setting.
		logging.Error("guardrail: provider evaluation error", "tool", toolCall.Name, "error", err)
		if m.cfg.FailClosed {
			// Fail closed: deny the call.
			return m.buildDeniedResult(toolCall, GuardrailDecision{
				Allow: false,
				Reasons: []GuardrailReason{{
					Code:    ReasonEvaluatorError,
					Message: "guardrail provider error (fail-closed)",
				}},
			}), nil
		}
		// Fail open: allow the call with a warning.
		logging.Warn("guardrail: fail_open is set, allowing tool call despite provider error", "tool", toolCall.Name)
		return handler(ctx, toolCall)
	}

	// If allowed, execute the actual tool handler.
	if decision.Allow {
		logging.Debug("guardrail: tool call allowed", "tool", toolCall.Name, "policy", decision.PolicyID)
		return handler(ctx, toolCall)
	}

	// Denied: return an error ToolResult without executing the tool.
	logging.Warn("guardrail: tool call denied",
		"tool", toolCall.Name,
		"policy", decision.PolicyID,
		"code", m.firstReasonCode(decision),
	)
	return m.buildDeniedResult(toolCall, decision), nil
}

// buildRequest creates a GuardrailRequest from the tool call and state.
func (m *GuardrailMiddleware) buildRequest(state *middleware.State, toolCall *middleware.ToolCall) GuardrailRequest {
	threadID := ""
	isSubagent := false
	if state != nil {
		threadID = state.ThreadID
		if state.Extra != nil {
			if sub, ok := state.Extra["is_subagent"].(bool); ok {
				isSubagent = sub
			}
		}
	}

	return GuardrailRequest{
		ToolName:   toolCall.Name,
		ToolInput:  toolCall.Input,
		AgentID:    m.cfg.Passport,
		ThreadID:   threadID,
		IsSubagent: isSubagent,
		Timestamp: strings.ReplaceAll(strings.ReplaceAll(
			strings.Split(fmt.Sprintf("%v", toolCall), " ")[0], "[", ""), "]", ""),
	}
}

// evaluateWithProvider evaluates the request against the configured provider.
func (m *GuardrailMiddleware) evaluateWithProvider(ctx context.Context, req GuardrailRequest) (GuardrailDecision, error) {
	// If a modern provider is configured, use it.
	if m.cfg.Provider != nil {
		return m.cfg.Provider.Evaluate(ctx, req)
	}

	// Fall back to legacy policy evaluation for backward compatibility.
	return m.evaluateLegacy(req.ToolName), nil
}

// evaluateLegacy evaluates using the legacy policy list.
func (m *GuardrailMiddleware) evaluateLegacy(toolName string) GuardrailDecision {
	for _, policy := range m.cfg.Policies {
		if matchPattern(policy.ToolPattern, toolName) {
			if policy.Decision == DecisionDeny {
				return DecisionDenied(ReasonToolNotAllowed, policy.Reason)
			}
			return DecisionAllowed()
		}
	}

	// Use default decision.
	if m.cfg.DefaultDecision == DecisionDeny {
		return DecisionDenied(ReasonToolNotAllowed, "blocked by default guardrail policy")
	}
	return DecisionAllowed()
}

// buildDeniedResult creates a ToolResult that contains the denial message.
// The agent sees this as if the tool returned an error, allowing it to adapt.
func (m *GuardrailMiddleware) buildDeniedResult(toolCall *middleware.ToolCall, decision GuardrailDecision) *middleware.ToolResult {
	reasonText := "blocked by guardrail policy"
	reasonCode := ReasonToolNotAllowed
	if len(decision.Reasons) > 0 {
		reasonText = decision.Reasons[0].Message
		reasonCode = decision.Reasons[0].Code
	}

	// Build a user-friendly error message that the agent can understand.
	// This mirrors DeerFlow's _build_denied_message pattern.
	message := fmt.Sprintf("Guardrail denied: tool '%s' was blocked (%s). Reason: %s. Choose an alternative approach.",
		toolCall.Name, reasonCode, reasonText)

	return &middleware.ToolResult{
		ID:     toolCall.ID,
		Output: message,
		Error:  errors.PermissionError(fmt.Sprintf("guardrail: %s: %s", reasonCode, reasonText)),
	}
}

// firstReasonCode returns the code of the first reason, or a default.
func (m *GuardrailMiddleware) firstReasonCode(decision GuardrailDecision) string {
	if len(decision.Reasons) > 0 {
		return decision.Reasons[0].Code
	}
	return "unknown"
}

// applyDecisions applies legacy decisions to tool calls (for backward compatibility).
func (m *GuardrailMiddleware) applyDecisions(toolCalls []map[string]any) {
	for i, tc := range toolCalls {
		toolName, _ := tc["name"].(string)
		if toolName == "" {
			continue
		}
		decision, reason := m.evaluate(toolName)
		tc["guardrail_decision"] = string(decision)
		tc["guardrail_reason"] = reason
		if decision == DecisionDeny {
			tc["blocked"] = true
			tc["block_reason"] = reason
		} else if decision == DecisionAsk {
			tc["requires_approval"] = true
		}
		toolCalls[i] = tc
	}
}

// evaluate is the legacy evaluation function for backward compatibility.
func (m *GuardrailMiddleware) evaluate(toolName string) (GuardrailDecisionType, string) {
	for _, policy := range m.cfg.Policies {
		if matchPattern(policy.ToolPattern, toolName) {
			return policy.Decision, policy.Reason
		}
	}
	return m.cfg.DefaultDecision, ""
}

func matchPattern(pattern, name string) bool {
	// Simple glob matching: * matches any sequence.
	pattern = strings.ToLower(pattern)
	name = strings.ToLower(name)

	if pattern == "*" {
		return true
	}

	if strings.HasPrefix(pattern, "*") && strings.HasSuffix(pattern, "*") {
		return strings.Contains(name, strings.Trim(pattern, "*"))
	}

	if strings.HasPrefix(pattern, "*") {
		return strings.HasSuffix(name, strings.TrimPrefix(pattern, "*"))
	}

	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(name, strings.TrimSuffix(pattern, "*"))
	}

	return pattern == name
}

var _ middleware.Middleware = (*GuardrailMiddleware)(nil)
