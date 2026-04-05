// Package middleware provides an adapter to convert Middleware to adk.ChatModelAgentMiddleware.
//
// This adapter allows GoClaw's custom middleware system to leverage Eino's native
// ChatModelAgentMiddleware interface, gaining access to WrapModel and better
// context propagation capabilities.
package middleware

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// EinoMiddlewareAdapter adapts a Middleware to adk.ChatModelAgentMiddleware.
//
// This adapter maps the custom Middleware interface to Eino's native interface,
// enabling access to WrapModel and other advanced features while preserving
// the existing middleware logic.
type EinoMiddlewareAdapter struct {
	*adk.BaseChatModelAgentMiddleware
	mw Middleware
}

// NewEinoMiddlewareAdapter creates a new adapter for the given middleware.
func NewEinoMiddlewareAdapter(mw Middleware) *EinoMiddlewareAdapter {
	return &EinoMiddlewareAdapter{
		BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{},
		mw:                           mw,
	}
}

// BeforeAgent runs once at the start of an agent run.
// It converts adk.ChatModelAgentContext to State and calls the underlying middleware.
func (a *EinoMiddlewareAdapter) BeforeAgent(ctx context.Context, runCtx *adk.ChatModelAgentContext) (context.Context, *adk.ChatModelAgentContext, error) {
	state := extractStateFromContext(ctx)
	if state == nil {
		state = &State{}
	}

	if err := a.mw.BeforeAgent(ctx, state); err != nil {
		return ctx, runCtx, err
	}

	// Sync state back to context for subsequent hooks
	ctx = withMiddlewareState(ctx, state)
	return ctx, runCtx, nil
}

// BeforeModelRewriteState runs before each model invocation.
// It calls the underlying middleware's Before hook.
func (a *EinoMiddlewareAdapter) BeforeModelRewriteState(ctx context.Context, state *adk.ChatModelAgentState, mc *adk.ModelContext) (context.Context, *adk.ChatModelAgentState, error) {
	mwState := toMiddlewareStateFromADK(state)

	if err := a.mw.BeforeModel(ctx, mwState); err != nil {
		return ctx, state, err
	}

	// Apply state changes back to adk state
	applyMiddlewareStateToADK(mwState, state)

	// Store state in context for After hook
	ctx = withMiddlewareState(ctx, mwState)

	return ctx, state, nil
}

// AfterModelRewriteState runs after each model invocation.
// It calls the underlying middleware's After hook.
func (a *EinoMiddlewareAdapter) AfterModelRewriteState(ctx context.Context, state *adk.ChatModelAgentState, mc *adk.ModelContext) (context.Context, *adk.ChatModelAgentState, error) {
	mwState := extractStateFromContext(ctx)
	if mwState == nil {
		mwState = toMiddlewareStateFromADK(state)
	}

	resp := toMiddlewareResponseFromADK(state)

	if err := a.mw.AfterModel(ctx, mwState, resp); err != nil {
		// After errors are non-fatal, log them but continue
		// TODO: use proper logging
		_ = err
	}

	// Apply state changes back to adk state
	applyMiddlewareStateToADK(mwState, state)

	return ctx, state, nil
}

// AfterAgent runs once at the end of an agent run.
// Note: This is called automatically by the framework when the agent finishes.
func (a *EinoMiddlewareAdapter) AfterAgent(ctx context.Context, runCtx *adk.ChatModelAgentContext, state *adk.ChatModelAgentState) error {
	mwState := extractStateFromContext(ctx)
	if mwState == nil {
		mwState = toMiddlewareStateFromADK(state)
	}

	resp := toMiddlewareResponseFromADK(state)

	// AfterAgent errors are non-fatal
	_ = a.mw.AfterAgent(ctx, mwState, resp)

	return nil
}

// WrapInvokableToolCall wraps tool calls with middleware logic.
// It chains through all WrapToolCall handlers from the adapted middleware.
func (a *EinoMiddlewareAdapter) WrapInvokableToolCall(ctx context.Context, endpoint adk.InvokableToolCallEndpoint, tCtx *adk.ToolContext) (adk.InvokableToolCallEndpoint, error) {
	wrapped := func(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
		// Convert to middleware types
		mwState := extractStateFromContext(ctx)
		if mwState == nil {
			mwState = &State{}
		}

		// Parse arguments JSON
		var args map[string]any
		if argumentsInJSON != "" {
			_ = json.Unmarshal([]byte(argumentsInJSON), &args)
		}
		if args == nil {
			args = map[string]any{}
		}

		toolCall := &ToolCall{
			ID:    tCtx.CallID,
			Name:  tCtx.Name,
			Input: args,
		}

		handler := func(callCtx context.Context, call *ToolCall) (*ToolResult, error) {
			result, err := endpoint(callCtx, argumentsInJSON, opts...)
			if err != nil {
				return &ToolResult{ID: call.ID, Error: err}, nil
			}
			return &ToolResult{ID: call.ID, Output: result}, nil
		}

		result, err := a.mw.WrapToolCall(ctx, mwState, toolCall, handler)
		if err != nil {
			return "", err
		}
		if result.Error != nil {
			return "", result.Error
		}

		if output, ok := result.Output.(string); ok {
			if msg, applied := applyCommandOutputToState(mwState, output); applied {
				return msg, nil
			}
			return output, nil
		}
		return middlewareToolOutputToString(result.Output), nil
	}

	return wrapped, nil
}

// WrapModel wraps the LLM model with custom behavior.
// If the middleware implements ModelWrapper interface, it calls WrapModel.
// Otherwise, it returns the model unchanged.
func (a *EinoMiddlewareAdapter) WrapModel(ctx context.Context, m model.BaseChatModel, mc *adk.ModelContext) (model.BaseChatModel, error) {
	// Check if middleware implements ModelWrapper interface
	type ModelWrapper interface {
		WrapModel(inner model.BaseChatModel) model.BaseChatModel
	}

	if wrapper, ok := a.mw.(ModelWrapper); ok {
		return wrapper.WrapModel(m), nil
	}

	return m, nil
}

// Helper functions for state conversion

func toMiddlewareStateFromADK(adkState *adk.ChatModelAgentState) *State {
	state := &State{
		Messages: make([]map[string]any, 0, len(adkState.Messages)),
	}

	for _, msg := range adkState.Messages {
		state.Messages = append(state.Messages, messageToMap(msg))
	}

	// Extract additional state from session values or extra
	// TODO: Implement full state extraction

	return state
}

func applyMiddlewareStateToADK(mwState *State, adkState *adk.ChatModelAgentState) {
	// Convert middleware state back to adk state
	// TODO: Implement full state synchronization
}

func toMiddlewareResponseFromADK(adkState *adk.ChatModelAgentState) *Response {
	resp := &Response{
		ToolCalls: make([]map[string]any, 0),
	}

	// Extract tool calls from the last message if it's an assistant message
	if len(adkState.Messages) > 0 {
		lastMsg := adkState.Messages[len(adkState.Messages)-1]
		if lastMsg.Role == schema.Assistant {
			resp.FinalMessage = lastMsg.Content
			// TODO: Extract tool calls from message
		}
	}

	return resp
}

func messageToMap(msg *schema.Message) map[string]any {
	m := map[string]any{
		"role":    string(msg.Role),
		"content": msg.Content,
	}
	if len(msg.ToolCalls) > 0 {
		toolCalls := make([]map[string]any, 0, len(msg.ToolCalls))
		for _, tc := range msg.ToolCalls {
			toolCalls = append(toolCalls, map[string]any{
				"id":        tc.ID,
				"name":      tc.Function.Name,
				"arguments": tc.Function.Arguments,
			})
		}
		m["tool_calls"] = toolCalls
	}
	return m
}

// Context key for storing middleware state
type middlewareStateKey struct{}

func withMiddlewareState(ctx context.Context, state *State) context.Context {
	return context.WithValue(ctx, middlewareStateKey{}, state)
}

func extractStateFromContext(ctx context.Context) *State {
	if state, ok := ctx.Value(middlewareStateKey{}).(*State); ok {
		return state
	}
	return nil
}

// AdaptMiddlewares converts a slice of Middleware to adk.ChatModelAgentMiddleware.
// This is the main entry point for migrating from AgentMiddleware to ChatModelAgentMiddleware.
func AdaptMiddlewares(middlewares []Middleware) []adk.ChatModelAgentMiddleware {
	adapters := make([]adk.ChatModelAgentMiddleware, 0, len(middlewares))
	for _, mw := range middlewares {
		adapters = append(adapters, NewEinoMiddlewareAdapter(mw))
	}
	return adapters
}

// ComposeToolMiddleware creates a compose.ToolMiddleware from the adapted middleware chain.
// This combines all WrapToolCall handlers from the middleware chain.
func ComposeToolMiddleware(middlewares []Middleware) compose.ToolMiddleware {
	return compose.ToolMiddleware{
		Invokable: func(next compose.InvokableToolEndpoint) compose.InvokableToolEndpoint {
			return func(ctx context.Context, input *compose.ToolInput) (*compose.ToolOutput, error) {
				// Get state from context
				state := extractStateFromContext(ctx)
				if state == nil {
					state = &State{}
				}

				// Parse arguments JSON
				var args map[string]any
				if input.Arguments != "" {
					_ = json.Unmarshal([]byte(input.Arguments), &args)
				}
				if args == nil {
					args = map[string]any{}
				}

				// Create tool call representation
				toolCall := &ToolCall{
					ID:    input.CallID,
					Name:  input.Name,
					Input: args,
				}

				// Chain through all middleware WrapToolCall handlers
				handler := func(callCtx context.Context, call *ToolCall) (*ToolResult, error) {
					// Call the actual tool
					out, err := next(callCtx, input)
					if err != nil {
						return &ToolResult{ID: call.ID, Error: err}, nil
					}
					return &ToolResult{ID: call.ID, Output: out.Result}, nil
				}

				// Run the chain
				for i := len(middlewares) - 1; i >= 0; i-- {
					mw := middlewares[i]
					nextHandler := handler
					handler = func(callCtx context.Context, call *ToolCall) (*ToolResult, error) {
						return mw.WrapToolCall(callCtx, state, call, nextHandler)
					}
				}

				result, err := handler(ctx, toolCall)
				if err != nil {
					return nil, err
				}
				if result.Error != nil {
					return nil, result.Error
				}

				return &compose.ToolOutput{Result: middlewareToolOutputToString(result.Output)}, nil
			}
		},
	}
}

func applyCommandOutputToState(state *State, output string) (string, bool) {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return "", false
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return "", false
	}
	typ, _ := payload["type"].(string)
	if !strings.EqualFold(strings.TrimSpace(typ), "command") {
		return "", false
	}

	update, _ := payload["update"].(map[string]any)
	if update == nil {
		return "", false
	}

	if state != nil {
		pendingUpdates := map[string]any{}
		if artifacts, ok := update["artifacts"]; ok {
			switch vv := artifacts.(type) {
			case []string:
				pendingUpdates["artifacts"] = vv
			case []any:
				list := make([]string, 0, len(vv))
				for _, item := range vv {
					if s, ok := item.(string); ok {
						list = append(list, s)
					}
				}
				pendingUpdates["artifacts"] = list
			}
		}
		if viewedImages, ok := update["viewed_images"]; ok {
			pendingUpdates["viewed_images"] = viewedImages
		}
		if len(pendingUpdates) > 0 {
			ApplyReducers(state, pendingUpdates)
		}
	}

	message := "ok"
	if rawMessages, ok := update["messages"]; ok {
		switch msgs := rawMessages.(type) {
		case []any:
			for _, item := range msgs {
				if mm, ok := item.(map[string]any); ok {
					if content, ok := mm["content"].(string); ok && strings.TrimSpace(content) != "" {
						message = content
						break
					}
				}
			}
		case []map[string]any:
			for _, mm := range msgs {
				if content, ok := mm["content"].(string); ok && strings.TrimSpace(content) != "" {
					message = content
					break
				}
			}
		}
	}

	return message, true
}

func middlewareToolOutputToString(output any) string {
	if output == nil {
		return ""
	}
	if str, ok := output.(string); ok {
		return str
	}
	if bs, err := json.Marshal(output); err == nil {
		return string(bs)
	}
	return ""
}
