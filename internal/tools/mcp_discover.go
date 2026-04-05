// Package tools provides MCP tool discovery functionality.
//
// This implements the DeerFlow-style MCP tool discovery where each MCP server
// is queried for its tools via tools/list, and each tool is exposed as a
// separate DynamicTool to the agent.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bookerbai/goclaw/internal/config"
)

// MCPToolDefinition represents a single tool from MCP tools/list.
type MCPToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
}

// MCPToolListResult is the result of tools/list method.
type MCPToolListResult struct {
	Tools []MCPToolDefinition `json:"tools"`
}

// DiscoveredMCPTool is a fully qualified tool with server prefix.
type DiscoveredMCPTool struct {
	ServerName  string
	ToolName    string
	Description string
	InputSchema json.RawMessage
	ServerCfg   config.MCPServerConfig
}

// FullName returns the prefixed tool name (server_toolname).
func (t *DiscoveredMCPTool) FullName() string {
	return sanitizeToolName(t.ServerName) + "_" + sanitizeToolName(t.ToolName)
}

// MCPDiscoveryClient discovers tools from MCP servers.
type MCPDiscoveryClient struct {
	cfg *config.AppConfig
}

// NewMCPDiscoveryClient creates a new MCP tool discovery client.
func NewMCPDiscoveryClient(cfg *config.AppConfig) *MCPDiscoveryClient {
	return &MCPDiscoveryClient{cfg: cfg}
}

// DiscoverAllTools queries all enabled MCP servers and returns discovered tools.
func (c *MCPDiscoveryClient) DiscoverAllTools(ctx context.Context) ([]DiscoveredMCPTool, error) {
	if c.cfg == nil {
		return nil, nil
	}

	enabled := c.cfg.Extensions.EnabledMCPServers()
	if len(enabled) == 0 {
		return nil, nil
	}

	var allTools []DiscoveredMCPTool
	for serverName, serverCfg := range enabled {
		if !isSupportedTransport(serverCfg.Type) {
			continue
		}
		tools, err := c.discoverServerTools(ctx, serverName, serverCfg)
		if err != nil {
			// Log error but continue with other servers.
			fmt.Printf("[MCP Discovery] Failed to discover tools from %s: %v\n", serverName, err)
			continue
		}
		allTools = append(allTools, tools...)
	}

	// Sort by full name for stable ordering.
	sort.Slice(allTools, func(i, j int) bool {
		return allTools[i].FullName() < allTools[j].FullName()
	})

	return allTools, nil
}

// discoverServerTools queries a single MCP server for its tools.
func (c *MCPDiscoveryClient) discoverServerTools(ctx context.Context, serverName string, serverCfg config.MCPServerConfig) ([]DiscoveredMCPTool, error) {
	transport := normalizeTransport(serverCfg.Type)

	switch transport {
	case "stdio":
		return c.discoverStdioTools(ctx, serverName, serverCfg)
	case "sse", "http":
		return c.discoverHTTPtools(ctx, serverName, serverCfg)
	default:
		return nil, fmt.Errorf("unsupported transport: %s", transport)
	}
}

// discoverStdioTools discovers tools from an STDIO MCP server.
func (c *MCPDiscoveryClient) discoverStdioTools(ctx context.Context, serverName string, serverCfg config.MCPServerConfig) ([]DiscoveredMCPTool, error) {
	// Use a short timeout for discovery.
	discoverCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Invoke tools/list method via STDIO bridge.
	result, err := invokeMCPStdioRaw(discoverCtx, serverName, serverCfg, "tools/list", nil)
	if err != nil {
		return nil, fmt.Errorf("tools/list failed: %w", err)
	}

	var listResult MCPToolListResult
	if err := json.Unmarshal([]byte(result), &listResult); err != nil {
		return nil, fmt.Errorf("parse tools/list result: %w", err)
	}

	tools := make([]DiscoveredMCPTool, 0, len(listResult.Tools))
	for _, tool := range listResult.Tools {
		tools = append(tools, DiscoveredMCPTool{
			ServerName:  serverName,
			ToolName:    tool.Name,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
			ServerCfg:   serverCfg,
		})
	}

	return tools, nil
}

// discoverHTTPtools discovers tools from an HTTP/SSE MCP server.
func (c *MCPDiscoveryClient) discoverHTTPtools(ctx context.Context, serverName string, serverCfg config.MCPServerConfig) ([]DiscoveredMCPTool, error) {
	// For now, use the same HTTP invocation as stdio for SSE discovery.
	// This could be enhanced with proper SSE support.
	discoverCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Try HTTP POST for tools/list.
	result, err := invokeMCPHTTPRaw(discoverCtx, serverName, serverCfg, "tools/list", nil)
	if err != nil {
		return nil, fmt.Errorf("tools/list failed: %w", err)
	}

	var listResult MCPToolListResult
	if err := json.Unmarshal([]byte(result), &listResult); err != nil {
		return nil, fmt.Errorf("parse tools/list result: %w", err)
	}

	tools := make([]DiscoveredMCPTool, 0, len(listResult.Tools))
	for _, tool := range listResult.Tools {
		tools = append(tools, DiscoveredMCPTool{
			ServerName:  serverName,
			ToolName:    tool.Name,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
			ServerCfg:   serverCfg,
		})
	}

	return tools, nil
}

// MCPSpecificTool wraps a discovered MCP tool as a Tool implementation.
type MCPSpecificTool struct {
	def DiscoveredMCPTool
}

// NewMCPSpecificTool creates a Tool from a discovered MCP tool definition.
func NewMCPSpecificTool(def DiscoveredMCPTool) *MCPSpecificTool {
	return &MCPSpecificTool{def: def}
}

// Name returns the full prefixed tool name.
func (t *MCPSpecificTool) Name() string {
	return t.def.FullName()
}

// Description returns the tool description.
func (t *MCPSpecificTool) Description() string {
	desc := strings.TrimSpace(t.def.Description)
	if desc == "" {
		desc = fmt.Sprintf("MCP tool %s from server %s", t.def.ToolName, t.def.ServerName)
	}
	transport := normalizeTransport(t.def.ServerCfg.Type)
	return fmt.Sprintf("%s (MCP server: %s, transport: %s)", desc, t.def.ServerName, transport)
}

// InputSchema returns the tool's JSON schema.
func (t *MCPSpecificTool) InputSchema() json.RawMessage {
	if len(t.def.InputSchema) > 0 {
		return t.def.InputSchema
	}
	// Return generic object schema if none provided.
	return json.RawMessage(`{"type":"object","properties":{}}`)
}

// Execute invokes the MCP tool with the given input.
func (t *MCPSpecificTool) Execute(ctx context.Context, input string) (string, error) {
	// Parse input to ensure it's valid JSON.
	var args map[string]any
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		return "", fmt.Errorf("mcp tool %s: invalid input JSON: %w", t.Name(), err)
	}

	// Build the tool call input.
	toolInput := mcpToolInput{
		ToolName:  t.def.ToolName,
		Arguments: args,
	}

	// Route to appropriate transport.
	switch normalizeTransport(t.def.ServerCfg.Type) {
	case "stdio":
		return invokeMCPStdio(ctx, t.def.ServerName, t.def.ServerCfg, toolInput)
	case "http":
		return invokeMCPHTTP(ctx, t.def.ServerName, t.def.ServerCfg, toolInput)
	case "sse":
		return invokeMCPSSE(ctx, t.def.ServerName, t.def.ServerCfg, toolInput)
	default:
		return "", fmt.Errorf("mcp tool %s: unsupported transport %s", t.Name(), t.def.ServerCfg.Type)
	}
}

// BuildDiscoveredMCPTools discovers tools from all enabled MCP servers and returns them as Tools.
func BuildDiscoveredMCPTools(cfg *config.AppConfig) []Tool {
	if cfg == nil {
		return nil
	}

	client := NewMCPDiscoveryClient(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	discovered, err := client.DiscoverAllTools(ctx)
	if err != nil {
		fmt.Printf("[MCP Discovery] Error discovering tools: %v\n", err)
		return nil
	}

	tools := make([]Tool, 0, len(discovered))
	for _, def := range discovered {
		tools = append(tools, NewMCPSpecificTool(def))
	}

	return tools
}

// Ensure MCPSpecificTool implements Tool interface.
var _ Tool = (*MCPSpecificTool)(nil)
