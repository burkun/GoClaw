// Package loop implements LoopDetectionMiddleware which detects repeated
// identical tool calls and injects a system reminder to break the loop.
package loop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/bookerbai/goclaw/internal/middleware"
)

// Config holds configuration for LoopDetectionMiddleware.
type Config struct {
	// MaxRepeats is the number of consecutive identical tool calls allowed
	// before injecting a break-loop reminder. Default 3.
	MaxRepeats int
}

// DefaultConfig returns reasonable defaults.
func DefaultConfig() Config {
	return Config{MaxRepeats: 3}
}

// LoopDetectionMiddleware detects repeated identical tool calls.
type LoopDetectionMiddleware struct {
	cfg Config
}

// New creates a LoopDetectionMiddleware.
func New(cfg Config) *LoopDetectionMiddleware {
	if cfg.MaxRepeats <= 0 {
		cfg.MaxRepeats = 3
	}
	return &LoopDetectionMiddleware{cfg: cfg}
}

// Name implements middleware.Middleware.
func (m *LoopDetectionMiddleware) Name() string {
	return "LoopDetectionMiddleware"
}

// Before is a no-op; detection happens in After.
func (m *LoopDetectionMiddleware) Before(_ context.Context, _ *middleware.State) error {
	return nil
}

// After checks the most recent tool calls from Response and inserts a system
// reminder if the same call has been repeated cfg.MaxRepeats times.
func (m *LoopDetectionMiddleware) After(_ context.Context, state *middleware.State, resp *middleware.Response) error {
	if len(resp.ToolCalls) == 0 {
		return nil
	}

	// Compute hashes for the last N tool calls in state.Messages (assistant messages).
	hashes := collectToolCallHashes(state.Messages, m.cfg.MaxRepeats+1)
	if len(hashes) < m.cfg.MaxRepeats {
		return nil
	}

	// Check if the last MaxRepeats hashes are identical.
	lastHash := hashes[len(hashes)-1]
	repeatCount := 0
	for i := len(hashes) - 1; i >= 0; i-- {
		if hashes[i] == lastHash {
			repeatCount++
		} else {
			break
		}
	}

	if repeatCount >= m.cfg.MaxRepeats {
		reminder := map[string]any{
			"role":    "system",
			"name":    "loop_detection",
			"content": "You appear to be repeating the same tool call. Please try a different approach or provide a final answer.",
		}
		state.Messages = append(state.Messages, reminder)
	}
	return nil
}

// collectToolCallHashes scans messages for assistant messages with tool_calls
// and returns a slice of their content hashes (up to limit entries).
// Uses full SHA256 hash (not truncated) to avoid collisions.
func collectToolCallHashes(messages []map[string]any, limit int) []string {
	var hashes []string
	for i := len(messages) - 1; i >= 0 && len(hashes) < limit; i-- {
		msg := messages[i]
		role, _ := msg["role"].(string)
		if role != "assistant" {
			continue
		}
		tcs, ok := msg["tool_calls"].([]map[string]any)
		if !ok || len(tcs) == 0 {
			continue
		}
		b, _ := json.Marshal(tcs)
		sum := sha256.Sum256(b)
		hashes = append([]string{hex.EncodeToString(sum[:])}, hashes...)
	}
	return hashes
}

var _ middleware.Middleware = (*LoopDetectionMiddleware)(nil)
