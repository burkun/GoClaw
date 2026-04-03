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
	Description string   `json:"description"`
	Question    string   `json:"question"`
	Options     []string `json:"options,omitempty"`
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

	result := map[string]any{
		"action":   "clarify",
		"question": in.Question,
	}
	if len(in.Options) > 0 {
		result["options"] = in.Options
	}
	b, _ := json.Marshal(result)
	return string(b), nil
}

var _ tools.Tool = (*ClarificationTool)(nil)
