// Package llmerror implements LLMErrorHandlingMiddleware for GoClaw.
//
// LLMErrorHandlingMiddleware catches tool execution errors and converts
// them into user-friendly ToolMessage responses, preventing agent crashes.
package llmerror

import (
	"context"
	"fmt"
	"strings"

	"github.com/bookerbai/goclaw/internal/middleware"
)

// LLMErrorHandlingMiddleware handles tool execution errors gracefully.
type LLMErrorHandlingMiddleware struct {
	middleware.MiddlewareWrapper
	// MaxRetries is the maximum number of retries for failed tool calls.
	MaxRetries int
}

// NewLLMErrorHandlingMiddleware constructs a LLMErrorHandlingMiddleware.
func NewLLMErrorHandlingMiddleware(maxRetries int) *LLMErrorHandlingMiddleware {
	if maxRetries <= 0 {
		maxRetries = 3
	}
	return &LLMErrorHandlingMiddleware{MaxRetries: maxRetries}
}

// Name implements middleware.Middleware.
func (m *LLMErrorHandlingMiddleware) Name() string { return "LLMErrorHandlingMiddleware" }

// Before is a no-op.
func (m *LLMErrorHandlingMiddleware) Before(_ context.Context, _ *middleware.State) error {
	return nil
}

// After converts tool errors into structured error messages.
func (m *LLMErrorHandlingMiddleware) After(_ context.Context, state *middleware.State, resp *middleware.Response) error {
	if resp == nil || len(resp.ToolCalls) == 0 {
		return nil
	}

	for i, tc := range resp.ToolCalls {
		errorMsg, hasError := tc["error"].(string)
		if !hasError || errorMsg == "" {
			continue
		}

		// Track retry count.
		toolID, _ := tc["id"].(string)
		retryKey := fmt.Sprintf("tool_retry_%s", toolID)
		retryCount := 0
		if state.Extra != nil {
			if count, ok := state.Extra[retryKey].(int); ok {
				retryCount = count
			}
		}

		// Convert error to user-friendly message.
		friendlyError := convertToFriendlyError(errorMsg)

		// Update tool call with error info.
		tc["output"] = fmt.Sprintf("Error: %s", friendlyError)
		tc["is_error"] = true
		tc["retry_count"] = retryCount

		if retryCount >= m.MaxRetries {
			tc["max_retries_exceeded"] = true
			tc["output"] = fmt.Sprintf("Error (max retries exceeded): %s", friendlyError)
		}

		resp.ToolCalls[i] = tc

		// Update retry count in state.
		if state.Extra == nil {
			state.Extra = map[string]any{}
		}
		state.Extra[retryKey] = retryCount + 1
	}

	return nil
}

func convertToFriendlyError(err string) string {
	lower := strings.ToLower(err)

	switch {
	case strings.Contains(lower, "permission denied"):
		return "Permission denied. Please check file/directory permissions."
	case strings.Contains(lower, "no such file"):
		return "File not found. Please verify the path exists."
	case strings.Contains(lower, "timeout"):
		return "Operation timed out. Please try again or simplify the request."
	case strings.Contains(lower, "connection refused"):
		return "Connection failed. Please check network connectivity."
	case strings.Contains(lower, "out of memory"):
		return "Insufficient memory. Please try with smaller input."
	case strings.Contains(lower, "rate limit"):
		return "Rate limit exceeded. Please wait before retrying."
	default:
		// Truncate long errors.
		if len(err) > 200 {
			return err[:200] + "..."
		}
		return err
	}
}

var _ middleware.Middleware = (*LLMErrorHandlingMiddleware)(nil)
