// Package guardrail implements GuardrailMiddleware for GoClaw.
//
// GuardrailMiddleware provides authorization checks before tool execution,
// implementing permit/deny/ask decision logic based on configurable policies.
package guardrail

import (
	"context"
	"strings"

	"github.com/bookerbai/goclaw/internal/middleware"
)

// Decision represents a guardrail authorization decision.
type Decision string

const (
	DecisionPermit Decision = "permit"
	DecisionDeny   Decision = "deny"
	DecisionAsk    Decision = "ask"
)

// Policy defines a guardrail policy rule.
type Policy struct {
	// ToolPattern is a glob pattern for matching tool names.
	ToolPattern string

	// Decision is the authorization result for matching tools.
	Decision Decision

	// Reason is an optional explanation for the decision.
	Reason string
}

// Config holds guardrail configuration.
type Config struct {
	// Enabled controls whether guardrail checks run.
	Enabled bool

	// Policies is the ordered list of policy rules.
	Policies []Policy

	// DefaultDecision is used when no policy matches.
	DefaultDecision Decision
}

// DefaultConfig returns a permissive default configuration.
func DefaultConfig() Config {
	return Config{
		Enabled:         false,
		DefaultDecision: DecisionPermit,
		Policies:        nil,
	}
}

// GuardrailMiddleware enforces authorization policies.
type GuardrailMiddleware struct {
	cfg Config
}

// NewGuardrailMiddleware constructs a GuardrailMiddleware.
func NewGuardrailMiddleware(cfg Config) *GuardrailMiddleware {
	return &GuardrailMiddleware{cfg: cfg}
}

// Name implements middleware.Middleware.
func (m *GuardrailMiddleware) Name() string { return "GuardrailMiddleware" }

// Before checks authorization for pending tool calls.
func (m *GuardrailMiddleware) Before(_ context.Context, state *middleware.State) error {
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

// After applies authorization checks to current response tool calls.
func (m *GuardrailMiddleware) After(_ context.Context, state *middleware.State, resp *middleware.Response) error {
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

func (m *GuardrailMiddleware) evaluate(toolName string) (Decision, string) {
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
