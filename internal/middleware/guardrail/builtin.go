// Package guardrail provides built-in guardrail providers for GoClaw.
package guardrail

import (
	"context"
	"fmt"
)

// AllowlistProvider is a simple allowlist/denylist provider with no external dependencies.
// It mirrors DeerFlow's AllowlistProvider in guardrails/builtin.py.
type AllowlistProvider struct {
	allowedTools map[string]struct{}
	deniedTools  map[string]struct{}
}

// AllowlistProviderConfig holds configuration for AllowlistProvider.
type AllowlistProviderConfig struct {
	// AllowedTools is the set of tools that are permitted.
	// If non-empty, only these tools are allowed.
	AllowedTools []string

	// DeniedTools is the set of tools that are blocked.
	DeniedTools []string
}

// NewAllowlistProvider creates a new AllowlistProvider from the given configuration.
func NewAllowlistProvider(cfg AllowlistProviderConfig) *AllowlistProvider {
	p := &AllowlistProvider{
		allowedTools: make(map[string]struct{}),
		deniedTools:  make(map[string]struct{}),
	}

	for _, t := range cfg.AllowedTools {
		p.allowedTools[t] = struct{}{}
	}
	for _, t := range cfg.DeniedTools {
		p.deniedTools[t] = struct{}{}
	}

	return p
}

// Name implements GuardrailProvider.
func (p *AllowlistProvider) Name() string {
	return "allowlist"
}

// Evaluate implements GuardrailProvider.
func (p *AllowlistProvider) Evaluate(_ context.Context, request GuardrailRequest) (GuardrailDecision, error) {
	// If allowlist is defined, check against it first.
	if len(p.allowedTools) > 0 {
		if _, ok := p.allowedTools[request.ToolName]; !ok {
			return DecisionDenied(ReasonToolNotAllowed,
				fmt.Sprintf("tool '%s' not in allowlist", request.ToolName)), nil
		}
	}

	// Check denylist.
	if _, ok := p.deniedTools[request.ToolName]; ok {
		return DecisionDenied(ReasonToolNotAllowed,
			fmt.Sprintf("tool '%s' is denied", request.ToolName)), nil
	}

	return DecisionAllowed(), nil
}
