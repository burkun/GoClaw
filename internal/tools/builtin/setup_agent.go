// Package builtin implements built-in tools for agent flow control.
package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tools "goclaw/internal/tools"
	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// SetupAgentTool (P2 fix)
// ---------------------------------------------------------------------------

// SetupAgentTool creates a custom agent by writing its configuration and SOUL.md
// to the file system. This mirrors DeerFlow's Agent Creator flow.
type SetupAgentTool struct {
	// BaseDir is the base directory for agent configurations.
	BaseDir string
}

// NewSetupAgentTool creates a SetupAgentTool.
func NewSetupAgentTool(baseDir string) *SetupAgentTool {
	if baseDir == "" {
		baseDir = ".goclaw"
	}
	return &SetupAgentTool{BaseDir: baseDir}
}

type setupAgentInput struct {
	AgentName   string `json:"agent_name"`
	Description string `json:"description"`
	Soul        string `json:"soul"`
	Model       string `json:"model,omitempty"`
}

func (t *SetupAgentTool) Name() string { return "setup_agent" }

func (t *SetupAgentTool) Description() string {
	return `Create a new custom agent with the specified configuration.
This tool writes the agent's SOUL.md (personality/instructions) and config.yaml to disk.
After creation, the agent can be invoked using the 'agent_name' parameter in subsequent requests.`
}

func (t *SetupAgentTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "required": ["agent_name", "description", "soul"],
  "properties": {
    "agent_name": {"type": "string", "description": "Unique name for the new agent (alphanumeric and underscore only)."},
    "description": {"type": "string", "description": "Brief description of what this agent does."},
    "soul": {"type": "string", "description": "The agent's personality, instructions, and behavior guidelines (SOUL.md content)."},
    "model": {"type": "string", "description": "Optional model name to use for this agent (defaults to system default)."}
  }
}`)
}

// Execute creates the agent directory and writes configuration files.
func (t *SetupAgentTool) Execute(_ context.Context, input string) (string, error) {
	var in setupAgentInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", fmt.Errorf("setup_agent: invalid input JSON: %w", err)
	}

	// Validate agent name
	name := strings.TrimSpace(in.AgentName)
	if name == "" {
		return "", fmt.Errorf("setup_agent: agent_name is required")
	}
	// Only allow alphanumeric and underscore
	for _, r := range name {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_') {
			return "", fmt.Errorf("setup_agent: agent_name must contain only alphanumeric characters and underscores")
		}
	}

	description := strings.TrimSpace(in.Description)
	if description == "" {
		return "", fmt.Errorf("setup_agent: description is required")
	}

	soul := strings.TrimSpace(in.Soul)
	if soul == "" {
		return "", fmt.Errorf("setup_agent: soul is required")
	}

	// Create agent directory
	agentDir := filepath.Join(t.BaseDir, "agents", strings.ToLower(name))
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		return "", fmt.Errorf("setup_agent: create agent directory: %w", err)
	}

	// Write SOUL.md
	soulPath := filepath.Join(agentDir, "SOUL.md")
	if err := os.WriteFile(soulPath, []byte(soul), 0o644); err != nil {
		return "", fmt.Errorf("setup_agent: write SOUL.md: %w", err)
	}

	// Write config.yaml
	config := map[string]any{
		"name":        name,
		"description": description,
	}
	if in.Model != "" {
		config["model"] = strings.TrimSpace(in.Model)
	}

	configData, err := yaml.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("setup_agent: marshal config: %w", err)
	}

	configPath := filepath.Join(agentDir, "config.yaml")
	if err := os.WriteFile(configPath, configData, 0o644); err != nil {
		return "", fmt.Errorf("setup_agent: write config.yaml: %w", err)
	}

	result := map[string]any{
		"action":     "setup_agent",
		"agent_name": name,
		"message":    fmt.Sprintf("Agent '%s' created successfully at %s", name, agentDir),
	}
	b, _ := json.Marshal(result)
	return string(b), nil
}

var _ tools.Tool = (*SetupAgentTool)(nil)
