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
	_ = state

	// Execute the tool
	result, err := handler(ctx, toolCall)
	if err != nil {
		return buildErrorToolResult(toolCall, err), nil
	}
	if result != nil && result.Error != nil {
		return buildErrorToolResult(toolCall, result.Error), nil
	}

	return result, nil
}

func buildErrorToolResult(toolCall *middleware.ToolCall, err error) *middleware.ToolResult {
	toolID := ""
	toolName := "unknown"
	if toolCall != nil {
		toolID = toolCall.ID
		if toolCall.Name != "" {
			toolName = toolCall.Name
		}
	}
	msg := fmt.Sprintf("Error: Tool '%s' failed: %s. Continue with available context.", toolName, err.Error())
	return &middleware.ToolResult{
		ID: toolID,
		Output: map[string]any{
			"status":        "error",
			"content":       msg,
			"error_type":    "tool_execution_error",
			"tool_name":     toolName,
			"error_message": err.Error(),
		},
		Error: nil,
	}
}
