// Package control implements control-flow middleware for GoClaw.
//
// This package contains middlewares that control agent behavior and enforce limits,
// including loop detection, guardrails, and subagent limits.
package control

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/bookerbai/goclaw/internal/middleware"
)

// LoopDetectionConfig holds configuration for LoopDetectionMiddleware.
type LoopDetectionConfig struct {
	// MaxRepeats is the number of consecutive identical tool calls allowed
	// before injecting a break-loop reminder. Default 3.
	MaxRepeats int
	// AlternatingMinCycles controls alternating pattern detection threshold.
	// Example: A->B->A->B is 2 cycles.
	AlternatingMinCycles int
}

// DefaultLoopDetectionConfig returns reasonable defaults.
func DefaultLoopDetectionConfig() LoopDetectionConfig {
	return LoopDetectionConfig{MaxRepeats: 3, AlternatingMinCycles: 2}
}

// LoopDetectionMiddleware detects repeated identical tool calls.
type LoopDetectionMiddleware struct {
	middleware.MiddlewareWrapper
	cfg LoopDetectionConfig
}

// NewLoopDetectionMiddleware creates a LoopDetectionMiddleware.
func NewLoopDetectionMiddleware(cfg LoopDetectionConfig) *LoopDetectionMiddleware {
	if cfg.MaxRepeats <= 0 {
		cfg.MaxRepeats = 3
	}
	if cfg.AlternatingMinCycles <= 0 {
		cfg.AlternatingMinCycles = 2
	}
	return &LoopDetectionMiddleware{cfg: cfg}
}

// Name implements middleware.Middleware.
func (m *LoopDetectionMiddleware) Name() string {
	return "LoopDetectionMiddleware"
}

// BeforeModel is a no-op; detection happens in AfterModel.
func (m *LoopDetectionMiddleware) BeforeModel(_ context.Context, _ *middleware.State) error {
	return nil
}

// AfterModel checks the most recent tool calls from Response and inserts a system
// reminder if the same call has been repeated cfg.MaxRepeats times.
func (m *LoopDetectionMiddleware) AfterModel(_ context.Context, state *middleware.State, resp *middleware.Response) error {
	if len(resp.ToolCalls) == 0 {
		return nil
	}

	limit := m.cfg.MaxRepeats + 1
	altLimit := m.cfg.AlternatingMinCycles*2 + 1
	if altLimit > limit {
		limit = altLimit
	}
	// Compute hashes for the last N tool calls in state.Messages (assistant messages).
	hashes := collectToolCallHashes(state.Messages, limit)
	if len(hashes) == 0 {
		return nil
	}

	if hasConsecutiveLoop(hashes, m.cfg.MaxRepeats) {
		state.Messages = append(state.Messages, loopReminder("You appear to be repeating the same tool call. Please try a different approach or provide a final answer."))
		return nil
	}

	if hasAlternatingLoop(hashes, m.cfg.AlternatingMinCycles) {
		state.Messages = append(state.Messages, loopReminder("You appear to be alternating between the same tool calls. Please try a different strategy or provide a final answer."))
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
		tcs := normalizeToolCalls(parseToolCalls(msg["tool_calls"]))
		if len(tcs) == 0 {
			continue
		}
		b, _ := json.Marshal(tcs)
		sum := sha256.Sum256(b)
		hashes = append([]string{hex.EncodeToString(sum[:])}, hashes...)
	}
	return hashes
}

func hasConsecutiveLoop(hashes []string, maxRepeats int) bool {
	if maxRepeats <= 0 || len(hashes) < maxRepeats {
		return false
	}
	lastHash := hashes[len(hashes)-1]
	repeatCount := 0
	for i := len(hashes) - 1; i >= 0; i-- {
		if hashes[i] == lastHash {
			repeatCount++
		} else {
			break
		}
	}
	return repeatCount >= maxRepeats
}

func hasAlternatingLoop(hashes []string, minCycles int) bool {
	windowSize := minCycles * 2
	if minCycles <= 1 || len(hashes) < windowSize {
		return false
	}
	window := hashes[len(hashes)-windowSize:]
	a, b := window[0], window[1]
	if a == b {
		return false
	}
	for i, h := range window {
		if i%2 == 0 && h != a {
			return false
		}
		if i%2 == 1 && h != b {
			return false
		}
	}
	return true
}

func parseToolCalls(raw any) []map[string]any {
	switch v := raw.(type) {
	case []map[string]any:
		return v
	case []any:
		out := make([]map[string]any, 0, len(v))
		for _, item := range v {
			if tc, ok := item.(map[string]any); ok {
				out = append(out, tc)
			}
		}
		return out
	default:
		return nil
	}
}

func normalizeToolCalls(calls []map[string]any) []map[string]any {
	if len(calls) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(calls))
	for _, tc := range calls {
		n := make(map[string]any, len(tc))
		for k, v := range tc {
			if k == "arguments" {
				n[k] = normalizeArguments(v)
				continue
			}
			n[k] = v
		}
		out = append(out, n)
	}
	return out
}

func normalizeArguments(raw any) any {
	switch v := raw.(type) {
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return v
		}
		var parsed any
		if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
			if b, err := json.Marshal(parsed); err == nil {
				return string(b)
			}
		}
		return v
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return v
		}
		var parsed any
		if err := json.Unmarshal(b, &parsed); err != nil {
			return v
		}
		canonical, err := json.Marshal(parsed)
		if err != nil {
			return v
		}
		return string(canonical)
	}
}

func loopReminder(content string) map[string]any {
	return map[string]any{
		"role":    "system",
		"name":    "loop_detection",
		"content": content,
	}
}

var _ middleware.Middleware = (*LoopDetectionMiddleware)(nil)
