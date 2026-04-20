// Package middleware provides an adapter to convert Middleware to adk.AgentMiddleware.
package middleware

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"goclaw/internal/logging"
)

// AdaptMiddlewares converts a slice of Middleware to adk.AgentMiddleware.
// The new Eino API uses AgentMiddleware with BeforeChatModel/AfterChatModel/WrapToolCall hooks.
func AdaptMiddlewares(middlewares []Middleware) []adk.AgentMiddleware {
	result := make([]adk.AgentMiddleware, 0, len(middlewares))
	for _, mw := range middlewares {
		result = append(result, adaptMiddleware(mw))
	}
	return result
}

// adaptMiddleware converts a single Middleware to adk.AgentMiddleware.
func adaptMiddleware(mw Middleware) adk.AgentMiddleware {
	return adk.AgentMiddleware{
		BeforeChatModel: func(ctx context.Context, state *adk.ChatModelAgentState) error {
			mwState := toMiddlewareState(state)
			if err := mw.BeforeModel(ctx, mwState); err != nil {
				return err
			}
			// Write back modified state to adk state
			applyMiddlewareStateToAdk(mwState, state)
			return nil
		},
		AfterChatModel: func(ctx context.Context, state *adk.ChatModelAgentState) error {
			mwState := toMiddlewareState(state)
			resp := toMiddlewareResponse(state)

			if err := mw.AfterModel(ctx, mwState, resp); err != nil {
				logging.Warn("middleware AfterModel error (non-fatal)",
					"middleware", mw.Name(),
					"error", err,
				)
			}
			return nil
		},
		WrapToolCall: composeToolMiddleware(mw),
	}
}

// composeToolMiddleware creates a compose.ToolMiddleware from a single middleware.
func composeToolMiddleware(mw Middleware) compose.ToolMiddleware {
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

				// Wrap handler
				handler := func(callCtx context.Context, call *ToolCall) (*ToolResult, error) {
					out, err := next(callCtx, input)
					if err != nil {
						return &ToolResult{ID: call.ID, Error: err}, nil
					}
					return &ToolResult{ID: call.ID, Output: out.Result}, nil
				}

				result, err := mw.WrapToolCall(ctx, state, toolCall, handler)
				if err != nil {
					return nil, err
				}
				if result.Error != nil {
					return nil, result.Error
				}

				return &compose.ToolOutput{Result: middlewareToolOutputToString(result.Output)}, nil
			}
		},
		Streamable: func(next compose.StreamableToolEndpoint) compose.StreamableToolEndpoint {
			return func(ctx context.Context, input *compose.ToolInput) (*compose.StreamToolOutput, error) {
				// For streaming, we just pass through (middleware wrap is for sync calls)
				return next(ctx, input)
			}
		},
	}
}

// ComposeToolMiddleware creates a compose.ToolMiddleware from all middlewares.
func ComposeToolMiddleware(middlewares []Middleware) compose.ToolMiddleware {
	if len(middlewares) == 0 {
		return compose.ToolMiddleware{}
	}

	// Build chain from all WrapToolCall handlers
	return compose.ToolMiddleware{
		Invokable: func(next compose.InvokableToolEndpoint) compose.InvokableToolEndpoint {
			handler := func(ctx context.Context, input *compose.ToolInput) (*compose.ToolOutput, error) {
				return next(ctx, input)
			}

			// Chain middlewares in reverse order
			for i := len(middlewares) - 1; i >= 0; i-- {
				mw := middlewares[i]
				innerHandler := handler
				handler = func(ctx context.Context, input *compose.ToolInput) (*compose.ToolOutput, error) {
					state := extractStateFromContext(ctx)
					if state == nil {
						state = &State{}
					}

					var args map[string]any
					if input.Arguments != "" {
						_ = json.Unmarshal([]byte(input.Arguments), &args)
					}
					if args == nil {
						args = map[string]any{}
					}

					toolCall := &ToolCall{
						ID:    input.CallID,
						Name:  input.Name,
						Input: args,
					}

					inner := func(callCtx context.Context, call *ToolCall) (*ToolResult, error) {
						out, err := innerHandler(callCtx, input)
						if err != nil {
							return &ToolResult{ID: call.ID, Error: err}, nil
						}
						return &ToolResult{ID: call.ID, Output: out.Result}, nil
					}

					result, err := mw.WrapToolCall(ctx, state, toolCall, inner)
					if err != nil {
						return nil, err
					}
					if result.Error != nil {
						return nil, result.Error
					}

					return &compose.ToolOutput{Result: middlewareToolOutputToString(result.Output)}, nil
				}
			}

			return handler
		},
	}
}

// Helper functions

func toMiddlewareState(adkState *adk.ChatModelAgentState) *State {
	state := &State{
		Messages: make([]map[string]any, 0, len(adkState.Messages)),
	}

	for _, msg := range adkState.Messages {
		state.Messages = append(state.Messages, messageToMap(msg))
	}

	return state
}

func toMiddlewareResponse(adkState *adk.ChatModelAgentState) *Response {
	resp := &Response{
		ToolCalls: make([]map[string]any, 0),
	}

	if len(adkState.Messages) > 0 {
		lastMsg := adkState.Messages[len(adkState.Messages)-1]
		if lastMsg.Role == schema.Assistant {
			resp.FinalMessage = lastMsg.Content
			if len(lastMsg.ToolCalls) > 0 {
				for _, tc := range lastMsg.ToolCalls {
					resp.ToolCalls = append(resp.ToolCalls, map[string]any{
						"id":       tc.ID,
						"name":     tc.Function.Name,
						"input":    tc.Function.Arguments,
						"response": "",
					})
				}
			}
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

// applyMiddlewareStateToAdk writes back modified middleware state to adk state.
// This is critical for memory injection which modifies Messages.
func applyMiddlewareStateToAdk(mwState *State, adkState *adk.ChatModelAgentState) {
	if mwState == nil || adkState == nil {
		return
	}

	// Convert modified messages back to adk state
	adkState.Messages = make([]*schema.Message, 0, len(mwState.Messages))
	for _, msg := range mwState.Messages {
		role, _ := msg["role"].(string)
		content, _ := msg["content"].(string)

		var schemaRole schema.RoleType
		switch role {
		case "system":
			schemaRole = schema.System
		case "user", "human":
			schemaRole = schema.User
		case "assistant", "ai":
			schemaRole = schema.Assistant
		case "tool":
			schemaRole = schema.Tool
		default:
			schemaRole = schema.User
		}

		adkState.Messages = append(adkState.Messages, &schema.Message{
			Role:    schemaRole,
			Content: content,
		})
	}
}

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
