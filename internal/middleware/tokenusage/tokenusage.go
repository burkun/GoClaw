// Package tokenusage implements TokenUsageMiddleware for GoClaw.
//
// TokenUsageMiddleware logs LLM token usage (input/output/total tokens)
// from the usage_metadata field of AI messages after each model call.
// This mirrors DeerFlow's TokenUsageMiddleware implementation.
package tokenusage

import (
	"context"
	"log/slog"

	"github.com/bookerbai/goclaw/internal/middleware"
)

// TokenUsageMiddleware logs token usage from model response usage_metadata.
type TokenUsageMiddleware struct {
	middleware.MiddlewareWrapper
}

// New creates a TokenUsageMiddleware.
func New() *TokenUsageMiddleware {
	return &TokenUsageMiddleware{}
}

// Name implements middleware.Middleware.
func (m *TokenUsageMiddleware) Name() string {
	return "TokenUsageMiddleware"
}

// After runs after the agent model invocation and logs token usage.
// It extracts usage_metadata from the last message (if it's an AI message)
// and logs input_tokens, output_tokens, and total_tokens.
func (m *TokenUsageMiddleware) After(ctx context.Context, state *middleware.State, resp *middleware.Response) error {
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
	slog.Info("LLM token usage",
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
