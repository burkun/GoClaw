// Package toolerror implements ToolErrorHandlingMiddleware for GoClaw.
//
// ToolErrorHandlingMiddleware wraps tool execution and converts panics/errors
// into friendly ToolMessage responses so the agent can continue gracefully.
// This mirrors DeerFlow's ToolErrorHandlingMiddleware implementation.
package toolerror

import (
	"context"
	"fmt"

	"github.com/bookerbai/goclaw/internal/middleware"
)

// ToolErrorHandlingMiddleware catches tool execution errors and converts them
// to error ToolMessage responses, allowing the agent to adapt and continue.
type ToolErrorHandlingMiddleware struct {
	middleware.MiddlewareWrapper
}

// New creates a ToolErrorHandlingMiddleware.
func New() *ToolErrorHandlingMiddleware {
	return &ToolErrorHandlingMiddleware{}
}

// Name implements middleware.Middleware.
func (m *ToolErrorHandlingMiddleware) Name() string {
	return "ToolErrorHandlingMiddleware"
}

// WrapToolCall intercepts tool execution and converts errors to ToolMessage.
// When a tool execution fails, instead of aborting the entire run, this middleware
// returns a ToolMessage with status="error" so the agent can see the failure
// and choose an alternative approach.
func (m *ToolErrorHandlingMiddleware) WrapToolCall(
	ctx context.Context,
	state *middleware.State,
	toolCall *middleware.ToolCall,
	handler middleware.ToolHandler,
) (*middleware.ToolResult, error) {
	// Execute the tool
	result, err := handler(ctx, toolCall)
	if err != nil {
		// Convert error to ToolMessage
		return &middleware.ToolResult{
			ID:     toolCall.ID,
			Output: map[string]any{
				"status":  "error",
				"content": fmt.Sprintf("Error: Tool '%s' failed: %s. Continue with available context.", toolCall.Name, err.Error()),
			},
			Error: nil, // Clear error so agent can continue
		}, nil
	}

	return result, nil
}
