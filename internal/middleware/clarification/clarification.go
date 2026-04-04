// Package clarification implements ClarificationMiddleware for GoClaw.
//
// ClarificationMiddleware detects when the agent invokes ask_clarification
// and signals a flow interrupt so the run can yield control back to the
// user for additional input.
package clarification

import (
	"context"
	"encoding/json"
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

		output, _ := tc["output"].(string)
		if output == "" {
			// Try to get from arguments if output not yet populated.
			if args, ok := tc["args"].(string); ok && args != "" {
				output = args
			}
		}

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

// ClarificationRequest mirrors the ask_clarification tool input.
type ClarificationRequest struct {
	Question string   `json:"question"`
	Options  []string `json:"options,omitempty"`
}

var _ middleware.Middleware = (*ClarificationMiddleware)(nil)
