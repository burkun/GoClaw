// Package builtin implements built-in tools for agent flow control.
package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	tools "github.com/bookerbai/goclaw/internal/tools"
)

// ---------------------------------------------------------------------------
// ClarificationTool
// ---------------------------------------------------------------------------

// ClarificationTool allows the agent to request additional information from the user
// before proceeding. It interrupts the current run and returns a structured clarification request.
// Implements tools.Tool.
type ClarificationTool struct{}

// NewClarificationTool creates a ClarificationTool.
func NewClarificationTool() *ClarificationTool {
	return &ClarificationTool{}
}

type clarificationInput struct {
	Description       string   `json:"description"`
	Question          string   `json:"question"`
	ClarificationType string   `json:"clarification_type,omitempty"`
	Options           []string `json:"options,omitempty"`
}

// validClarificationTypes defines the allowed clarification types (P0 fix).
var validClarificationTypes = map[string]bool{
	"missing_info":          true,
	"ambiguous_requirement": true,
	"approach_choice":       true,
	"risk_confirmation":     true,
	"suggestion":            true,
}

func (t *ClarificationTool) Name() string { return "ask_clarification" }

func (t *ClarificationTool) Description() string {
	return `Request clarification from the user when you need more information to proceed.
Use this when instructions are ambiguous or you have multiple valid approaches.
The run will pause until the user responds.`
}

func (t *ClarificationTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "required": ["description", "question"],
  "properties": {
    "description": {"type": "string", "description": "Why you need clarification."},
    "question":    {"type": "string", "description": "The question to ask the user."},
    "clarification_type": {
      "type": "string",
      "enum": ["missing_info", "ambiguous_requirement", "approach_choice", "risk_confirmation", "suggestion"],
      "description": "The type of clarification needed."
    },
    "options":     {"type": "array", "items": {"type": "string"}, "description": "Optional suggested answers."}
  }
}`)
}

// Execute returns a structured clarification request that the runtime can intercept.
func (t *ClarificationTool) Execute(_ context.Context, input string) (string, error) {
	var in clarificationInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", fmt.Errorf("ask_clarification: invalid input JSON: %w", err)
	}
	if strings.TrimSpace(in.Question) == "" {
		return "", fmt.Errorf("ask_clarification: question is required")
	}

	// Validate clarification_type if provided (P0 fix)
	clarificationType := in.ClarificationType
	if clarificationType != "" && !validClarificationTypes[clarificationType] {
		return "", fmt.Errorf("ask_clarification: invalid clarification_type %q, must be one of: missing_info, ambiguous_requirement, approach_choice, risk_confirmation, suggestion", clarificationType)
	}

	result := map[string]any{
		"action":   "clarify",
		"question": in.Question,
	}
	if clarificationType != "" {
		result["clarification_type"] = clarificationType
	}
	if len(in.Options) > 0 {
		result["options"] = in.Options
	}
	b, _ := json.Marshal(result)
	return string(b), nil
}

var _ tools.Tool = (*ClarificationTool)(nil)
