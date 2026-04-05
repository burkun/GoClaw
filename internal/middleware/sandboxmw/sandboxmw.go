// Package sandboxmw implements SandboxMiddleware which acquires a sandbox
// instance for the current thread and stores it in state.Extra["sandbox"].
//
// Lifecycle:
// - BeforeAgent: Acquires sandbox for the thread
// - AfterAgent: Releases sandbox (if provider supports release)
//
// This mirrors DeerFlow's SandboxMiddleware which acquires in before_agent
// and releases in after_agent.
package sandboxmw

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/bookerbai/goclaw/internal/middleware"
	"github.com/bookerbai/goclaw/internal/sandbox"
)

// SandboxMiddleware acquires a sandbox on BeforeAgent and releases it on AfterAgent.
type SandboxMiddleware struct {
	middleware.MiddlewareWrapper
	provider sandbox.SandboxProvider
}

// New creates a SandboxMiddleware with the given provider.
func New(provider sandbox.SandboxProvider) *SandboxMiddleware {
	return &SandboxMiddleware{provider: provider}
}

// Name implements middleware.Middleware.
func (m *SandboxMiddleware) Name() string {
	return "SandboxMiddleware"
}

// BeforeAgent acquires a sandbox for the thread at the start of the agent run.
// This is called once per agent run, before any model invocations.
func (m *SandboxMiddleware) BeforeAgent(ctx context.Context, state *middleware.State) error {
	if m.provider == nil {
		return nil
	}

	sandboxID, err := m.provider.Acquire(ctx, state.ThreadID)
	if err != nil {
		return fmt.Errorf("sandboxmw: acquire failed: %w", err)
	}

	sb := m.provider.Get(sandboxID)
	if sb == nil {
		return fmt.Errorf("sandboxmw: sandbox %s not found after acquire", sandboxID)
	}

	if state.Extra == nil {
		state.Extra = make(map[string]any)
	}
	state.Extra["sandbox"] = sb
	state.Extra["sandbox_id"] = sandboxID
	slog.Debug("sandboxmw: acquired sandbox", "sandbox_id", sandboxID, "thread_id", state.ThreadID)
	return nil
}

// AfterAgent releases the sandbox at the end of the agent run.
// This is called once per agent run, after all model invocations.
func (m *SandboxMiddleware) AfterAgent(_ context.Context, state *middleware.State, _ *middleware.Response) error {
	if m.provider == nil || state == nil || state.Extra == nil {
		return nil
	}

	sandboxID, ok := state.Extra["sandbox_id"].(string)
	if !ok || sandboxID == "" {
		return nil
	}

	// Release the sandbox if the provider supports it.
	// Note: Some providers use TTL-based release instead.
	if releaser, ok := m.provider.(interface{ Release(ctx context.Context, sandboxID string) error }); ok {
		if err := releaser.Release(context.Background(), sandboxID); err != nil {
			slog.Warn("sandboxmw: release failed", "sandbox_id", sandboxID, "error", err)
		} else {
			slog.Debug("sandboxmw: released sandbox", "sandbox_id", sandboxID)
		}
	}

	delete(state.Extra, "sandbox")
	delete(state.Extra, "sandbox_id")
	return nil
}

var _ middleware.Middleware = (*SandboxMiddleware)(nil)
