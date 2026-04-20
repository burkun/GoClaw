// Package monitoring implements monitoring-related middleware for GoClaw.
//
// This package contains middlewares that monitor and audit operations,
// including sandbox auditing and token usage tracking.
package monitoring

import (
	"context"

	"goclaw/internal/logging"
	"goclaw/internal/middleware"
)

// TokenUsageMiddleware logs token usage from model response usage_metadata.
type TokenUsageMiddleware struct {
	middleware.MiddlewareWrapper
}

// NewTokenUsageMiddleware creates a TokenUsageMiddleware.
func NewTokenUsageMiddleware() *TokenUsageMiddleware {
	return &TokenUsageMiddleware{}
}

// Name implements middleware.Middleware.
func (m *TokenUsageMiddleware) Name() string {
	return "TokenUsageMiddleware"
}

// AfterModel runs after the agent model invocation and logs token usage.
// It extracts usage_metadata from the last message (if it's an AI message)
// and logs input_tokens, output_tokens, and total_tokens.
func (m *TokenUsageMiddleware) AfterModel(ctx context.Context, state *middleware.State, resp *middleware.Response) error {
	if len(state.Messages) == 0 {
		return nil
	}

	lastMsg := state.Messages[len(state.Messages)-1]

	// Check if this is an assistant (AI) message
	role, ok := lastMsg["role"].(string)
	if !ok || role != "assistant" {
		return nil
	}

	// Extract usage_metadata
	usage, ok := lastMsg["usage_metadata"].(map[string]any)
	if !ok {
		return nil
	}

	// Extract token counts (may be int, int64, or float64 from JSON)
	inputTokens := extractInt(usage, "input_tokens")
	outputTokens := extractInt(usage, "output_tokens")
	totalTokens := extractInt(usage, "total_tokens")

	// Log token usage
	logging.Info("LLM token usage",
		"input", inputTokens,
		"output", outputTokens,
		"total", totalTokens,
	)

	return nil
}

// extractInt safely extracts an integer value from a map, handling
// different numeric types that may result from JSON unmarshaling.
func extractInt(m map[string]any, key string) int {
	if m == nil {
		return 0
	}
	val, ok := m[key]
	if !ok {
		return 0
	}

	switch v := val.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case uint64:
		return int(v)
	default:
		return 0
	}
}
