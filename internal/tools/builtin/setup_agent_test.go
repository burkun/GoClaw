package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestSetupAgentTool_Name tests the Name method
func TestSetupAgentTool_Name(t *testing.T) {
	tool := NewSetupAgentTool("")
	if tool.Name() != "setup_agent" {
		t.Errorf("expected name 'setup_agent', got %q", tool.Name())
	}
}

// TestSetupAgentTool_Description tests the Description method
func TestSetupAgentTool_Description(t *testing.T) {
	tool := NewSetupAgentTool("")
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
}

// TestSetupAgentTool_InputSchema tests the InputSchema method
func TestSetupAgentTool_InputSchema(t *testing.T) {
	tool := NewSetupAgentTool("")
	schema := tool.InputSchema()
	if len(schema) == 0 {
		t.Error("expected non-empty input schema")
	}
	// Verify it's valid JSON
	var parsed map[string]any
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Errorf("input schema is not valid JSON: %v", err)
	}
}

// TestSetupAgentTool_NewSetupAgentTool tests the constructor
func TestSetupAgentTool_NewSetupAgentTool(t *testing.T) {
	// Test with empty baseDir
	tool1 := NewSetupAgentTool("")
	if tool1.BaseDir != ".goclaw" {
		t.Errorf("expected default baseDir '.goclaw', got %q", tool1.BaseDir)
	}

	// Test with custom baseDir
	tool2 := NewSetupAgentTool("/custom/path")
	if tool2.BaseDir != "/custom/path" {
		t.Errorf("expected baseDir '/custom/path', got %q", tool2.BaseDir)
	}
}

// TestSetupAgentTool_Execute_Success tests successful agent creation
func TestSetupAgentTool_Execute_Success(t *testing.T) {
	tmp := t.TempDir()
	tool := NewSetupAgentTool(tmp)

	input := `{"agent_name":"test_agent","description":"Test agent for unit testing","soul":"You are a test agent."}`
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatalf("invalid result JSON: %v", err)
	}

	if payload["action"] != "setup_agent" {
		t.Errorf("expected action 'setup_agent', got %v", payload["action"])
	}
	if payload["agent_name"] != "test_agent" {
		t.Errorf("expected agent_name 'test_agent', got %v", payload["agent_name"])
	}

	// Verify files were created
	agentDir := filepath.Join(tmp, "agents", "test_agent")
	soulPath := filepath.Join(agentDir, "SOUL.md")
	configPath := filepath.Join(agentDir, "config.yaml")

	if _, err := os.Stat(soulPath); os.IsNotExist(err) {
		t.Error("SOUL.md was not created")
	}
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("config.yaml was not created")
	}

	// Verify SOUL.md content
	soulContent, err := os.ReadFile(soulPath)
	if err != nil {
		t.Fatalf("failed to read SOUL.md: %v", err)
	}
	if string(soulContent) != "You are a test agent." {
		t.Errorf("unexpected SOUL.md content: %q", string(soulContent))
	}
}

// TestSetupAgentTool_Execute_WithModel tests agent creation with model specified
func TestSetupAgentTool_Execute_WithModel(t *testing.T) {
	tmp := t.TempDir()
	tool := NewSetupAgentTool(tmp)

	input := `{"agent_name":"model_agent","description":"Agent with model","soul":"Test","model":"gpt-4"}`
	_, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	configPath := filepath.Join(tmp, "agents", "model_agent", "config.yaml")
	configContent, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config.yaml: %v", err)
	}
	if string(configContent) == "" {
		t.Error("config.yaml is empty")
	}
}

// TestSetupAgentTool_Execute_InvalidJSON tests error handling for invalid JSON
func TestSetupAgentTool_Execute_InvalidJSON(t *testing.T) {
	tool := NewSetupAgentTool("")
	_, err := tool.Execute(context.Background(), `{invalid json`)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// TestSetupAgentTool_Execute_EmptyAgentName tests validation for empty agent name
func TestSetupAgentTool_Execute_EmptyAgentName(t *testing.T) {
	tool := NewSetupAgentTool("")
	_, err := tool.Execute(context.Background(), `{"agent_name":"","description":"test","soul":"test"}`)
	if err == nil {
		t.Error("expected error for empty agent name")
	}
}

// TestSetupAgentTool_Execute_EmptyDescription tests validation for empty description
func TestSetupAgentTool_Execute_EmptyDescription(t *testing.T) {
	tool := NewSetupAgentTool("")
	_, err := tool.Execute(context.Background(), `{"agent_name":"test","description":"","soul":"test"}`)
	if err == nil {
		t.Error("expected error for empty description")
	}
}

// TestSetupAgentTool_Execute_EmptySoul tests validation for empty soul
func TestSetupAgentTool_Execute_EmptySoul(t *testing.T) {
	tool := NewSetupAgentTool("")
	_, err := tool.Execute(context.Background(), `{"agent_name":"test","description":"test","soul":""}`)
	if err == nil {
		t.Error("expected error for empty soul")
	}
}

// TestSetupAgentTool_Execute_InvalidAgentName tests validation for invalid agent names
func TestSetupAgentTool_Execute_InvalidAgentName(t *testing.T) {
	tool := NewSetupAgentTool("")

	invalidNames := []string{
		"test-agent", // hyphen not allowed
		"test.agent", // dot not allowed
		"test agent", // space not allowed
		"test@agent", // @ not allowed
	}

	for _, name := range invalidNames {
		input := `{"agent_name":"` + name + `","description":"test","soul":"test"}`
		_, err := tool.Execute(context.Background(), input)
		if err == nil {
			t.Errorf("expected error for agent name %q", name)
		}
	}
}

// TestSetupAgentTool_Execute_ValidAgentNames tests valid agent names
func TestSetupAgentTool_Execute_ValidAgentNames(t *testing.T) {
	tmp := t.TempDir()
	tool := NewSetupAgentTool(tmp)

	validNames := []string{
		"test_agent",
		"TestAgent",
		"test123",
		"TEST_AGENT_123",
	}

	for _, name := range validNames {
		input := `{"agent_name":"` + name + `","description":"test","soul":"test"}`
		_, err := tool.Execute(context.Background(), input)
		if err != nil {
			t.Errorf("unexpected error for agent name %q: %v", name, err)
		}
	}
}
