// Package tool implements tool-related middleware for GoClaw.
//
// This package contains middlewares that handle tool filtering and error handling.
package tool

import (
	"context"
	"strings"

	"goclaw/internal/middleware"
)

// DeferredToolFilterMiddleware filters deferred tools from model bindings.
type DeferredToolFilterMiddleware struct {
	middleware.MiddlewareWrapper
	// DeferredToolNames is the list of tool names to hide when tool_search is enabled.
	DeferredToolNames []string
}

// NewDeferredToolFilterMiddleware constructs a DeferredToolFilterMiddleware.
func NewDeferredToolFilterMiddleware(deferredNames []string) *DeferredToolFilterMiddleware {
	return &DeferredToolFilterMiddleware{DeferredToolNames: deferredNames}
}

// Name implements middleware.Middleware.
func (m *DeferredToolFilterMiddleware) Name() string { return "DeferredToolFilterMiddleware" }

// BeforeModel filters deferred tools from available tools when tool_search is enabled.
func (m *DeferredToolFilterMiddleware) BeforeModel(_ context.Context, state *middleware.State) error {
	if state == nil || state.Extra == nil {
		return nil
	}

	// Check if tool_search is enabled.
	toolSearchEnabled, _ := state.Extra["tool_search_enabled"].(bool)
	if !toolSearchEnabled {
		return nil
	}

	// Get available tools from state.
	availableTools, ok := state.Extra["available_tools"].([]string)
	if !ok || len(availableTools) == 0 {
		return nil
	}

	// Filter out deferred tools.
	deferredSet := make(map[string]bool)
	for _, name := range m.DeferredToolNames {
		deferredSet[strings.ToLower(name)] = true
	}

	filtered := make([]string, 0, len(availableTools))
	for _, tool := range availableTools {
		if !deferredSet[strings.ToLower(tool)] {
			filtered = append(filtered, tool)
		}
	}

	state.Extra["available_tools"] = filtered
	state.Extra["deferred_tools"] = m.DeferredToolNames

	return nil
}

// AfterModel is a no-op.
func (m *DeferredToolFilterMiddleware) AfterModel(_ context.Context, _ *middleware.State, _ *middleware.Response) error {
	return nil
}

// DefaultDeferredTools returns the standard list of deferred tools.
func DefaultDeferredTools() []string {
	return []string{
		"ImageGen",
		"ImageEdit",
		"NotebookEdit",
		"LSP",
		"CronCreate",
		"CronDelete",
		"CronList",
		"EnterWorktree",
		"LeaveWorktree",
		"TeamCreate",
		"TeamDelete",
	}
}

var _ middleware.Middleware = (*DeferredToolFilterMiddleware)(nil)
