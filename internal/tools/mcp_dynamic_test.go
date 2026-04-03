package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/bookerbai/goclaw/internal/config"
)

func TestBuildMCPDynamicTools(t *testing.T) {
	cfg := &config.AppConfig{
		Extensions: config.ExtensionsConfig{
			MCPServers: map[string]config.MCPServerConfig{
				"beta-server": {
					Enabled: true,
					Type:    "http",
					URL:     "https://mcp.example.com",
				},
				"alpha-server": {
					Enabled: true,
					Type:    "stdio",
					Command: "uvx",
				},
				"disabled-server": {
					Enabled: false,
					Type:    "sse",
					URL:     "https://mcp.disabled.example.com/sse",
				},
				"unsupported-server": {
					Enabled: true,
					Type:    "ws",
				},
			},
		},
	}

	tools := BuildMCPDynamicTools(cfg)
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}

	if tools[0].Name() != "mcp_alpha_server_call" {
		t.Fatalf("unexpected first tool name: %s", tools[0].Name())
	}
	if tools[1].Name() != "mcp_beta_server_call" {
		t.Fatalf("unexpected second tool name: %s", tools[1].Name())
	}
}

func TestMCPDynamicTool_ExecuteValidation(t *testing.T) {
	tool := NewMCPDynamicTool("remote-1", config.MCPServerConfig{Enabled: true, Type: "http", URL: "https://mcp.example.com"})

	if _, err := tool.Execute(context.Background(), `{"arguments":{}}`); err == nil || !strings.Contains(err.Error(), "tool_name is required") {
		t.Fatalf("expected tool_name required error, got %v", err)
	}
}

func TestMCPDynamicTool_ExecuteStdio(t *testing.T) {
	tool := NewMCPDynamicTool("remote-1", config.MCPServerConfig{Enabled: true, Type: "stdio", Command: "fake"})

	old := stdioMCPInvoker
	defer func() { stdioMCPInvoker = old }()

	var gotServer string
	var gotTool string
	stdioMCPInvoker = func(_ context.Context, serverName string, _ config.MCPServerConfig, in mcpToolInput) (string, error) {
		gotServer = serverName
		gotTool = in.ToolName
		return "ok-from-mcp", nil
	}

	out, err := tool.Execute(context.Background(), `{"tool_name":"search_docs","arguments":{"q":"go"}}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "ok-from-mcp" {
		t.Fatalf("unexpected output: %s", out)
	}
	if gotServer != "remote-1" || gotTool != "search_docs" {
		t.Fatalf("unexpected invoker args: server=%s tool=%s", gotServer, gotTool)
	}
}

func TestMCPDynamicTool_ExecuteStdioError(t *testing.T) {
	tool := NewMCPDynamicTool("remote-1", config.MCPServerConfig{Enabled: true, Type: "stdio", Command: "fake"})

	old := stdioMCPInvoker
	defer func() { stdioMCPInvoker = old }()
	stdioMCPInvoker = func(_ context.Context, _ string, _ config.MCPServerConfig, _ mcpToolInput) (string, error) {
		return "", errors.New("bridge failed")
	}

	_, err := tool.Execute(context.Background(), `{"tool_name":"search_docs"}`)
	if err == nil || !strings.Contains(err.Error(), "bridge failed") {
		t.Fatalf("expected wrapped bridge error, got %v", err)
	}
}

func TestMCPDynamicTool_ExecuteHTTP(t *testing.T) {
	tool := NewMCPDynamicTool("remote-http", config.MCPServerConfig{Enabled: true, Type: "http", URL: "https://mcp.example.com"})

	old := httpMCPInvoker
	defer func() { httpMCPInvoker = old }()

	httpMCPInvoker = func(_ context.Context, serverName string, _ config.MCPServerConfig, in mcpToolInput) (string, error) {
		if serverName != "remote-http" || in.ToolName != "search_docs" {
			t.Fatalf("unexpected call args: server=%s tool=%s", serverName, in.ToolName)
		}
		return "ok-http", nil
	}

	out, err := tool.Execute(context.Background(), `{"tool_name":"search_docs"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "ok-http" {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestMCPDynamicTool_ExecuteSSE(t *testing.T) {
	tool := NewMCPDynamicTool("remote-sse", config.MCPServerConfig{Enabled: true, Type: "sse", URL: "https://mcp.example.com/sse"})

	old := sseMCPInvoker
	defer func() { sseMCPInvoker = old }()

	sseMCPInvoker = func(_ context.Context, serverName string, _ config.MCPServerConfig, in mcpToolInput) (string, error) {
		if serverName != "remote-sse" || in.ToolName != "search_docs" {
			t.Fatalf("unexpected call args: server=%s tool=%s", serverName, in.ToolName)
		}
		return "ok-sse", nil
	}

	out, err := tool.Execute(context.Background(), `{"tool_name":"search_docs"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "ok-sse" {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestMCPDynamicTool_InputSchema(t *testing.T) {
	tool := NewMCPDynamicTool("remote", config.MCPServerConfig{Enabled: true, Type: "sse"})

	var schema map[string]any
	if err := json.Unmarshal(tool.InputSchema(), &schema); err != nil {
		t.Fatalf("schema is invalid json: %v", err)
	}
	required, ok := schema["required"].([]any)
	if !ok || len(required) == 0 || required[0] != "tool_name" {
		t.Fatalf("unexpected required field: %#v", schema["required"])
	}
}
