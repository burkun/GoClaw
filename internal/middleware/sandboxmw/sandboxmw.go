// Package sandboxmw implements SandboxMiddleware which acquires a sandbox
// instance for the current thread and stores it in state.Extra["sandbox"].
package sandboxmw

import (
	"context"
	"fmt"

	"github.com/bookerbai/goclaw/internal/middleware"
	"github.com/bookerbai/goclaw/internal/sandbox"
)

// SandboxMiddleware acquires a sandbox on Before and stores it in state.Extra["sandbox"].
type SandboxMiddleware struct {
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

// Before acquires or retrieves a sandbox for the thread.
func (m *SandboxMiddleware) Before(ctx context.Context, state *middleware.State) error {
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
	return nil
}

// After is a no-op; sandbox release is handled by provider TTL or explicit shutdown.
func (m *SandboxMiddleware) After(_ context.Context, _ *middleware.State, _ *middleware.Response) error {
	return nil
}

var _ middleware.Middleware = (*SandboxMiddleware)(nil)
