// Package dangling implements DanglingToolCallMiddleware which repairs
// incomplete tool calls from a previous turn so the model can continue.
//
// ARCHITECTURE NOTE (P1 alignment):
// DeerFlow uses wrap_model_call hook to insert ToolMessage right after AIMessage.
// GoClaw's current middleware architecture doesn't have WrapModel hook, so we use
// Before hook instead. The functional behavior is equivalent - we find the last
// assistant message and insert placeholder tool messages at the correct position.
// Future enhancement: add WrapModel to middleware.Middleware interface for
// precise model-call interception.
package dangling

import (
	"context"

	"github.com/bookerbai/goclaw/internal/middleware"
)

// DanglingToolCallMiddleware detects incomplete (dangling) tool calls at the
// end of state.Messages and inserts placeholder tool_message responses so the
// model does not get stuck waiting for a tool response that never comes.
type DanglingToolCallMiddleware struct {
	middleware.MiddlewareWrapper
}

// New creates a DanglingToolCallMiddleware.
func New() *DanglingToolCallMiddleware {
	return &DanglingToolCallMiddleware{}
}

// Name implements middleware.Middleware.
func (m *DanglingToolCallMiddleware) Name() string {
	return "DanglingToolCallMiddleware"
}

// BeforeModel scans the tail of Messages for assistant messages with tool_calls
// that lack corresponding tool_message responses and inserts placeholders.
func (m *DanglingToolCallMiddleware) BeforeModel(_ context.Context, state *middleware.State) error {
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

	// Insert placeholder for any remaining dangling calls right after the assistant message.
	if len(callIDs) > 0 {
		placeholders := make([]map[string]any, 0, len(callIDs))
		for id := range callIDs {
			placeholders = append(placeholders, map[string]any{
				"role":         "tool",
				"tool_call_id": id,
				"content":      "[Interrupted: tool call did not complete in previous run]",
			})
		}
		// Insert placeholders right after the last assistant message.
		insertPos := lastAssistantIdx + 1
		state.Messages = append(
			state.Messages[:insertPos],
			append(placeholders, state.Messages[insertPos:]...)...,
		)
	}
	return nil
}

// AfterModel is a no-op.
func (m *DanglingToolCallMiddleware) AfterModel(_ context.Context, _ *middleware.State, _ *middleware.Response) error {
	return nil
}

var _ middleware.Middleware = (*DanglingToolCallMiddleware)(nil)
