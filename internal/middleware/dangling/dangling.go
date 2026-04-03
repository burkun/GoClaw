// Package dangling implements DanglingToolCallMiddleware which repairs
// incomplete tool calls from a previous turn so the model can continue.
package dangling

import (
	"context"

	"github.com/bookerbai/goclaw/internal/middleware"
)

// DanglingToolCallMiddleware detects incomplete (dangling) tool calls at the
// end of state.Messages and inserts placeholder tool_message responses so the
// model does not get stuck waiting for a tool response that never comes.
type DanglingToolCallMiddleware struct{}

// New creates a DanglingToolCallMiddleware.
func New() *DanglingToolCallMiddleware {
	return &DanglingToolCallMiddleware{}
}

// Name implements middleware.Middleware.
func (m *DanglingToolCallMiddleware) Name() string {
	return "DanglingToolCallMiddleware"
}

// Before scans the tail of Messages for assistant messages with tool_calls
// that lack corresponding tool_message responses and inserts placeholders.
func (m *DanglingToolCallMiddleware) Before(_ context.Context, state *middleware.State) error {
	if len(state.Messages) == 0 {
		return nil
	}

	// Find last assistant message.
	var lastAssistantIdx int = -1
	for i := len(state.Messages) - 1; i >= 0; i-- {
		role, _ := state.Messages[i]["role"].(string)
		if role == "assistant" {
			lastAssistantIdx = i
			break
		}
	}
	if lastAssistantIdx == -1 {
		return nil
	}

	lastMsg := state.Messages[lastAssistantIdx]
	toolCalls, ok := lastMsg["tool_calls"].([]map[string]any)
	if !ok || len(toolCalls) == 0 {
		return nil
	}

	// Collect IDs of tool_calls.
	callIDs := make(map[string]bool)
	for _, tc := range toolCalls {
		if id, ok := tc["id"].(string); ok && id != "" {
			callIDs[id] = true
		}
	}

	// Check which have matching tool_message after the assistant message.
	for i := lastAssistantIdx + 1; i < len(state.Messages); i++ {
		msg := state.Messages[i]
		role, _ := msg["role"].(string)
		if role != "tool" {
			continue
		}
		tcID, _ := msg["tool_call_id"].(string)
		delete(callIDs, tcID)
	}

	// Insert placeholder for any remaining dangling calls.
	for id := range callIDs {
		placeholder := map[string]any{
			"role":         "tool",
			"tool_call_id": id,
			"content":      "[Interrupted: tool call did not complete in previous run]",
		}
		state.Messages = append(state.Messages, placeholder)
	}
	return nil
}

// After is a no-op.
func (m *DanglingToolCallMiddleware) After(_ context.Context, _ *middleware.State, _ *middleware.Response) error {
	return nil
}

var _ middleware.Middleware = (*DanglingToolCallMiddleware)(nil)
