package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"goclaw/internal/config"
)

func TestMCPEndToEnd(t *testing.T) {
	cfg, err := config.GetAppConfig()
	if err != nil {
		t.Skipf("Config not available: %v", err)
	}

	enabled := cfg.Extensions.EnabledMCPServers()
	if len(enabled) == 0 {
		t.Skip("No MCP servers configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Test discovery
	client := NewMCPDiscoveryClient(cfg)
	discovered, err := client.DiscoverAllTools(ctx)
	if err != nil {
		t.Fatalf("Discovery error: %v", err)
	}

	t.Logf("Discovered %d tools", len(discovered))
	for _, tool := range discovered {
		t.Logf("  - %s (from %s)", tool.FullName(), tool.ServerName)
	}

	if len(discovered) == 0 {
		t.Fatal("Expected to discover tools but got none")
	}

	// Find a read_text_file or read_file tool
	var readTool *DiscoveredMCPTool
	for i, tool := range discovered {
		if tool.ToolName == "read_text_file" || tool.ToolName == "read_file" {
			readTool = &discovered[i]
			break
		}
	}
	if readTool == nil {
		t.Skip("No read_text_file or read_file tool found")
	}

	// Test tool invocation
	tool := NewMCPSpecificTool(*readTool)
	t.Logf("Testing tool: %s", tool.Name())

	invokeCtx, invokeCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer invokeCancel()

	// Use the first tool with a simple test input
	testInput := map[string]any{"path": "/tmp/mcp-test/test.txt"}
	inputJSON, _ := json.Marshal(testInput)

	result, err := tool.Execute(invokeCtx, string(inputJSON))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	t.Logf("Result: %s", result)

	if result == "" {
		t.Fatal("Expected non-empty result")
	}
}
