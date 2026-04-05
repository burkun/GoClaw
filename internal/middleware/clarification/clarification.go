// Package clarification implements ClarificationMiddleware for GoClaw.
//
// ClarificationMiddleware detects when the agent invokes ask_clarification
// and signals a flow interrupt so the run can yield control back to the
// user for additional input.
package clarification

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bookerbai/goclaw/internal/middleware"
)

// ClarificationMiddleware intercepts clarification requests.
type ClarificationMiddleware struct {
	middleware.MiddlewareWrapper
}

// NewClarificationMiddleware constructs a ClarificationMiddleware.
func NewClarificationMiddleware() *ClarificationMiddleware {
	return &ClarificationMiddleware{}
}

// Name implements middleware.Middleware.
func (m *ClarificationMiddleware) Name() string { return "ClarificationMiddleware" }

// Before is a no-op.
func (m *ClarificationMiddleware) Before(_ context.Context, _ *middleware.State) error {
	return nil
}

// WrapToolCall intercepts ask_clarification before execution and turns it into
// an immediate interrupt payload, so no external tool execution is needed.
func (m *ClarificationMiddleware) WrapToolCall(
	ctx context.Context,
	state *middleware.State,
	toolCall *middleware.ToolCall,
	handler middleware.ToolHandler,
) (*middleware.ToolResult, error) {
	if toolCall == nil || strings.ToLower(toolCall.Name) != "ask_clarification" {
		return handler(ctx, toolCall)
	}

	payload := ""
	if toolCall.Input != nil {
		if bs, err := json.Marshal(toolCall.Input); err == nil {
			payload = string(bs)
		}
	}
	if strings.TrimSpace(payload) == "" {
		payload = `{"question":"Please clarify your request."}`
	}

	var req ClarificationRequest
	if err := json.Unmarshal([]byte(payload), &req); err == nil && strings.TrimSpace(req.Question) != "" {
		if state != nil {
			if state.Extra == nil {
				state.Extra = map[string]any{}
			}
			state.Extra["clarification_request"] = req
			state.Extra["interrupt"] = true
		}
	} else if state != nil {
		if state.Extra == nil {
			state.Extra = map[string]any{}
		}
		state.Extra["clarification_request"] = payload
		state.Extra["interrupt"] = true
	}

	return &middleware.ToolResult{ID: toolCall.ID, Output: payload, Error: nil}, nil
}

// After checks if the response contains an ask_clarification tool call
// and sets state.Extra["clarification_request"] with the parsed output.
func (m *ClarificationMiddleware) After(_ context.Context, state *middleware.State, resp *middleware.Response) error {
	if resp == nil || len(resp.ToolCalls) == 0 {
		return nil
	}

	for _, tc := range resp.ToolCalls {
		name, _ := tc["name"].(string)
		if strings.ToLower(name) != "ask_clarification" {
			continue
		}

		output := toolCallPayload(tc)

		// Parse clarification request.
		var req ClarificationRequest
		if err := json.Unmarshal([]byte(output), &req); err == nil && req.Question != "" {
			if state.Extra == nil {
				state.Extra = map[string]any{}
			}
			state.Extra["clarification_request"] = req
			state.Extra["interrupt"] = true
			return nil
		}

		// Fallback: store raw output.
		if output != "" {
			if state.Extra == nil {
				state.Extra = map[string]any{}
			}
			state.Extra["clarification_request"] = output
			state.Extra["interrupt"] = true
		}
		return nil
	}

	return nil
}

func toolCallPayload(tc map[string]any) string {
	if tc == nil {
		return ""
	}
	if output, ok := tc["output"].(string); ok && output != "" {
		return output
	}
	for _, key := range []string{"args", "arguments", "input"} {
		if v, ok := tc[key]; ok {
			switch vv := v.(type) {
			case string:
				if strings.TrimSpace(vv) != "" {
					return vv
				}
			case map[string]any, []any:
				if bs, err := json.Marshal(vv); err == nil {
					return string(bs)
				}
			default:
				if vv != nil {
					return fmt.Sprint(vv)
				}
			}
		}
	}
	return ""
}

// ClarificationRequest mirrors the ask_clarification tool input.
type ClarificationRequest struct {
	Question string   `json:"question"`
	Options  []string `json:"options,omitempty"`
}

var _ middleware.Middleware = (*ClarificationMiddleware)(nil)
