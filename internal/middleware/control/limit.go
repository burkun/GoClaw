// Package control implements control-flow middleware for GoClaw.
//
// This package contains middlewares that control agent behavior and enforce limits,
// including loop detection, guardrails, and subagent limits.
package control

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/bookerbai/goclaw/internal/middleware"
)

// ErrSubagentLimitReached is returned when the global subagent limit is hit.
var ErrSubagentLimitReached = errors.New("subagent concurrency limit reached")

// SubagentLimitConfig holds configuration for SubagentLimitMiddleware.
type SubagentLimitConfig struct {
	// MaxConcurrent is the global limit for concurrent subagent executions.
	MaxConcurrent int
}

// DefaultSubagentLimitConfig returns reasonable defaults.
func DefaultSubagentLimitConfig() SubagentLimitConfig {
	return SubagentLimitConfig{MaxConcurrent: 3}
}

// SubagentLimitMiddleware tracks active subagent count and rejects new runs
// when the limit is exceeded.
type SubagentLimitMiddleware struct {
	middleware.MiddlewareWrapper
	cfg     SubagentLimitConfig
	current *int64
}

// NewSubagentLimitMiddleware creates a SubagentLimitMiddleware with the given config.
// The counter is shared across all runs; pass the same instance to all chains.
func NewSubagentLimitMiddleware(cfg SubagentLimitConfig) *SubagentLimitMiddleware {
	var counter int64
	return &SubagentLimitMiddleware{cfg: cfg, current: &counter}
}

// Name implements middleware.Middleware.
func (m *SubagentLimitMiddleware) Name() string {
	return "SubagentLimitMiddleware"
}

// BeforeModel increments counter and rejects if limit exceeded.
func (m *SubagentLimitMiddleware) BeforeModel(_ context.Context, state *middleware.State) error {
	if state.Extra == nil {
		state.Extra = map[string]any{}
	}
	// Per-turn counter for truncating excessive task tool calls.
	state.Extra["task_tool_calls_count"] = 0

	// Only apply active-run counter to subagent runs (indicated by extra flag).
	isSubagent, _ := state.Extra["is_subagent"].(bool)
	if !isSubagent {
		return nil
	}

	cur := atomic.AddInt64(m.current, 1)
	if int(cur) > m.cfg.MaxConcurrent {
		atomic.AddInt64(m.current, -1)
		return ErrSubagentLimitReached
	}
	return nil
}

// AfterModel decrements counter for subagent runs.
func (m *SubagentLimitMiddleware) AfterModel(_ context.Context, state *middleware.State, _ *middleware.Response) error {
	isSubagent, _ := state.Extra["is_subagent"].(bool)
	if isSubagent {
		atomic.AddInt64(m.current, -1)
	}
	return nil
}

// WrapToolCall truncates excessive task tool calls in a single run.
// For task calls beyond MaxConcurrent, it returns a synthetic success output
// without executing the underlying tool.
func (m *SubagentLimitMiddleware) WrapToolCall(ctx context.Context, state *middleware.State, toolCall *middleware.ToolCall, handler middleware.ToolHandler) (*middleware.ToolResult, error) {
	if toolCall == nil || toolCall.Name != "task" {
		return handler(ctx, toolCall)
	}
	if state.Extra == nil {
		state.Extra = map[string]any{}
	}
	count, _ := state.Extra["task_tool_calls_count"].(int)
	if count >= m.cfg.MaxConcurrent {
		return &middleware.ToolResult{
			ID: toolCall.ID,
			Output: fmt.Sprintf(
				"subagent task call skipped: exceeded max_concurrent limit (%d)",
				m.cfg.MaxConcurrent,
			),
		}, nil
	}
	state.Extra["task_tool_calls_count"] = count + 1
	return handler(ctx, toolCall)
}

// Current returns the current active subagent count.
func (m *SubagentLimitMiddleware) Current() int64 {
	return atomic.LoadInt64(m.current)
}

var _ middleware.Middleware = (*SubagentLimitMiddleware)(nil)
