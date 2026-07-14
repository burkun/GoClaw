// Package middleware provides an adapter to convert GoClaw Middleware to Eino's
// ChatModelAgentMiddleware (v0.9+ interface-based handlers).
package middleware

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"goclaw/internal/logging"
)

// middlewareStateSessionKey is the key used to store middleware state in session values.
// This must match the key used in executor.go.
const middlewareStateSessionKey = "__middleware_state__"

// AdaptMiddlewares converts a slice of GoClaw Middleware to Eino ChatModelAgentMiddleware
// (v0.9+ Handlers). Each GoClaw middleware is wrapped in a middlewareAdapter that maps
// the legacy hook model to the new interface-based handler model.
func AdaptMiddlewares(middlewares []Middleware) []adk.ChatModelAgentMiddleware {
	result := make([]adk.ChatModelAgentMiddleware, 0, len(middlewares))
	for _, mw := range middlewares {
		result = append(result, newMiddlewareAdapter(mw))
	}
	return result
}

// middlewareAdapter wraps a GoClaw Middleware to implement adk.ChatModelAgentMiddleware.
type middlewareAdapter struct {
	*adk.TypedBaseChatModelAgentMiddleware[*schema.Message]
	mw Middleware
}

// newMiddlewareAdapter creates a ChatModelAgentMiddleware from a GoClaw Middleware.
func newMiddlewareAdapter(mw Middleware) adk.ChatModelAgentMiddleware {
	return &middlewareAdapter{
		TypedBaseChatModelAgentMiddleware: &adk.TypedBaseChatModelAgentMiddleware[*schema.Message]{},
		mw:                                mw,
	}
}

// BeforeAgent delegates to the GoClaw middleware's BeforeAgent hook.
// State is saved to session so subsequent hooks (BeforeModel, AfterModel) see mutations.
func (a *middlewareAdapter) BeforeAgent(ctx context.Context, runCtx *adk.ChatModelAgentContext) (context.Context, *adk.ChatModelAgentContext, error) {
	state := getOrCreateMiddlewareState(ctx)
	if err := a.mw.BeforeAgent(ctx, state); err != nil {
		return ctx, runCtx, err
	}
	saveMiddlewareStateToSession(ctx, state)
	return ctx, runCtx, nil
}

// AfterAgent delegates to the GoClaw middleware's AfterAgent hook.
// Messages are synced from agentState so middleware sees the final conversation state
// including tool results that completed after the last AfterModelRewriteState call.
func (a *middlewareAdapter) AfterAgent(ctx context.Context, agentState *adk.TypedChatModelAgentState[*schema.Message]) (context.Context, error) {
	state := getOrCreateMiddlewareState(ctx)
	if state == nil {
		return ctx, nil
	}
	// Sync messages from the final agent state (includes tool results).
	state.Messages = make([]map[string]any, 0, len(agentState.Messages))
	for _, msg := range agentState.Messages {
		state.Messages = append(state.Messages, messageToMap(msg))
	}
	saveMiddlewareStateToSession(ctx, state)

	resp := toResponseFromAgenticState(agentState)
	if err := a.mw.AfterAgent(ctx, state, resp); err != nil {
		logging.Warn("middleware AfterAgent error (non-fatal)",
			"middleware", a.mw.Name(), "error", err)
	}
	return ctx, nil
}

// BeforeModelRewriteState delegates to the GoClaw middleware's BeforeModel hook.
// Converts the agentic state to GoClaw State, calls BeforeModel, and writes changes back.
func (a *middlewareAdapter) BeforeModelRewriteState(
	ctx context.Context,
	state *adk.TypedChatModelAgentState[*schema.Message],
	mc *adk.TypedModelContext[*schema.Message],
) (context.Context, *adk.TypedChatModelAgentState[*schema.Message], error) {
	mwState := ensureMiddlewareState(ctx, state)
	if err := a.mw.BeforeModel(ctx, mwState); err != nil {
		return ctx, state, err
	}
	applyMiddlewareStateToAgentic(mwState, state)
	saveMiddlewareStateToSession(ctx, mwState)
	return ctx, state, nil
}

// AfterModelRewriteState delegates to the GoClaw middleware's AfterModel hook.
func (a *middlewareAdapter) AfterModelRewriteState(
	ctx context.Context,
	state *adk.TypedChatModelAgentState[*schema.Message],
	mc *adk.TypedModelContext[*schema.Message],
) (context.Context, *adk.TypedChatModelAgentState[*schema.Message], error) {
	mwState := ensureMiddlewareState(ctx, state)
	resp := toResponseFromAgenticState(state)
	if err := a.mw.AfterModel(ctx, mwState, resp); err != nil {
		logging.Warn("middleware AfterModel error (non-fatal)",
			"middleware", a.mw.Name(), "error", err)
	}
	saveMiddlewareStateToSession(ctx, mwState)
	return ctx, state, nil
}

// WrapInvokableToolCall wraps tool execution with the GoClaw middleware's WrapToolCall hook.
func (a *middlewareAdapter) WrapInvokableToolCall(
	ctx context.Context,
	endpoint adk.InvokableToolCallEndpoint,
	tCtx *adk.ToolContext,
) (adk.InvokableToolCallEndpoint, error) {
	return func(callCtx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
		state := getOrCreateMiddlewareState(callCtx)
		if state == nil {
			state = &State{}
		}

		// Build GoClaw ToolCall from the new endpoint's JSON args + ToolContext.
		toolCall := buildToolCall(tCtx, argumentsInJSON)

		// Inner handler: call the next endpoint in the chain.
		handler := func(_ context.Context, _ *ToolCall) (*ToolResult, error) {
			out, err := endpoint(callCtx, argumentsInJSON, opts...)
			if err != nil {
				return &ToolResult{ID: tCtx.CallID, Error: err}, nil
			}
			return &ToolResult{ID: tCtx.CallID, Output: out}, nil
		}

		result, err := a.mw.WrapToolCall(callCtx, state, toolCall, handler)
		if err != nil {
			return "", err
		}
		if result.Error != nil {
			return "", result.Error
		}
		return toolResultToString(result.Output), nil
	}, nil
}

// WrapModel delegates to the middleware's WrapModel hook if it has one.
// The base implementation passes through unchanged.

// --- Helper functions ---

func getOrCreateMiddlewareState(ctx context.Context) *State {
	vals := adk.GetSessionValues(ctx)
	if vals != nil {
		if cached, ok := vals[middlewareStateSessionKey].(*State); ok && cached != nil {
			return cached
		}
	}
	return &State{Extra: map[string]any{}}
}

// ensureMiddlewareState syncs messages from agentic state into the middleware state.
func ensureMiddlewareState(ctx context.Context, agentState *adk.TypedChatModelAgentState[*schema.Message]) *State {
	mwState := getOrCreateMiddlewareState(ctx)
	mwState.Messages = make([]map[string]any, 0, len(agentState.Messages))
	for _, msg := range agentState.Messages {
		mwState.Messages = append(mwState.Messages, messageToMap(msg))
	}

	// Recover Extra fields from session values (scene, agent_name, etc.)
	vals := adk.GetSessionValues(ctx)
	if vals != nil {
		if scene, ok := vals["scene"].(string); ok && scene != "" {
			if mwState.Extra == nil {
				mwState.Extra = map[string]any{}
			}
			mwState.Extra["scene"] = scene
		}
		if agentName, ok := vals["agent_name"].(string); ok && agentName != "" {
			if mwState.Extra == nil {
				mwState.Extra = map[string]any{}
			}
			mwState.Extra["agent_name"] = agentName
		}
	}

	return mwState
}

func saveMiddlewareStateToSession(ctx context.Context, state *State) {
	if state == nil {
		return
	}
	vals := adk.GetSessionValues(ctx)
	if vals == nil {
		return
	}
	vals[middlewareStateSessionKey] = state
}

// applyMiddlewareStateToAgentic writes back modified middleware state (e.g. modified
// system prompt from SceneMiddleware, injected facts from MemoryMiddleware) to the
// agentic state that will be passed to the model.
func applyMiddlewareStateToAgentic(mwState *State, agentState *adk.TypedChatModelAgentState[*schema.Message]) {
	if mwState == nil || agentState == nil {
		return
	}
	agentState.Messages = make([]*schema.Message, 0, len(mwState.Messages))
	for _, msg := range mwState.Messages {
		agentState.Messages = append(agentState.Messages, mapToMessage(msg))
	}
}

// toResponseFromAgenticState builds a GoClaw Response from the agentic state's last message.
func toResponseFromAgenticState(agentState *adk.TypedChatModelAgentState[*schema.Message]) *Response {
	resp := &Response{
		ToolCalls: make([]map[string]any, 0),
	}
	if len(agentState.Messages) > 0 {
		lastMsg := agentState.Messages[len(agentState.Messages)-1]
		if lastMsg.Role == schema.Assistant {
			resp.FinalMessage = lastMsg.Content
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
	return resp
}

// buildToolCall constructs a GoClaw ToolCall from a ToolContext and JSON arguments string.
func buildToolCall(tCtx *adk.ToolContext, argumentsInJSON string) *ToolCall {
	var args map[string]any
	if strings.TrimSpace(argumentsInJSON) != "" {
		_ = json.Unmarshal([]byte(argumentsInJSON), &args)
	}
	if args == nil {
		args = map[string]any{}
	}
	name := ""
	callID := ""
	if tCtx != nil {
		name = tCtx.Name
		callID = tCtx.CallID
	}
	return &ToolCall{
		ID:    callID,
		Name:  name,
		Input: args,
	}
}

// toolResultToString converts a tool result to its string representation.
func toolResultToString(output any) string {
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

// --- Legacy message conversion helpers (kept for state sync) ---

func messageToMap(msg *schema.Message) map[string]any {
	m := map[string]any{
		"role":    string(msg.Role),
		"content": msg.Content,
	}
	if msg.ToolCallID != "" {
		m["tool_call_id"] = msg.ToolCallID
	}
	if msg.ToolName != "" {
		m["tool_name"] = msg.ToolName
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
	if msg.ReasoningContent != "" {
		m["reasoning_content"] = msg.ReasoningContent
	}
	return m
}

func mapToMessage(m map[string]any) *schema.Message {
	role, _ := m["role"].(string)
	content, _ := m["content"].(string)
	msg := &schema.Message{
		Role:    schema.RoleType(role),
		Content: content,
	}
	// Restore tool_call_id and tool_name for tool role messages.
	if toolCallID, ok := m["tool_call_id"].(string); ok {
		msg.ToolCallID = toolCallID
	}
	if toolName, ok := m["tool_name"].(string); ok {
		msg.ToolName = toolName
	}
	// Restore tool_calls for assistant messages.
	if tcs, ok := m["tool_calls"]; ok {
		switch v := tcs.(type) {
		case []map[string]any:
			msg.ToolCalls = make([]schema.ToolCall, 0, len(v))
			for _, tc := range v {
				id, _ := tc["id"].(string)
				name, _ := tc["name"].(string)
				args, _ := tc["arguments"].(string)
				msg.ToolCalls = append(msg.ToolCalls, schema.ToolCall{
					ID:       id,
					Function: schema.FunctionCall{Name: name, Arguments: args},
				})
			}
		case []any:
			msg.ToolCalls = make([]schema.ToolCall, 0, len(v))
			for _, item := range v {
				if tcm, ok := item.(map[string]any); ok {
					id, _ := tcm["id"].(string)
					name, _ := tcm["name"].(string)
					args, _ := tcm["arguments"].(string)
					msg.ToolCalls = append(msg.ToolCalls, schema.ToolCall{
						ID:       id,
						Function: schema.FunctionCall{Name: name, Arguments: args},
					})
				}
			}
		}
	}
	// Restore reasoning content.
	if reasoning, ok := m["reasoning_content"].(string); ok {
		msg.ReasoningContent = reasoning
	}
	return msg
}

// Ensure we import model to avoid unused import when WrapModel defaults to passthrough.
var _ model.BaseModel[*schema.Message]
