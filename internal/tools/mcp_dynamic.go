package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"goclaw/internal/config"
)

// MCPDynamicTool is a lightweight proxy tool generated from extensions MCP config.
// It provides a stable tool surface to the model before full MCP transports are implemented.
type MCPDynamicTool struct {
	serverName string
	serverCfg  config.MCPServerConfig
}

type mcpToolInput struct {
	ToolName  string         `json:"tool_name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

var (
	stdioMCPInvoker = invokeMCPStdio
	httpMCPInvoker  = invokeMCPHTTP
	sseMCPInvoker   = invokeMCPSSE
)

// NewMCPDynamicTool builds a dynamic MCP proxy tool for one configured server.
func NewMCPDynamicTool(serverName string, serverCfg config.MCPServerConfig) *MCPDynamicTool {
	return &MCPDynamicTool{serverName: strings.TrimSpace(serverName), serverCfg: serverCfg}
}

func (t *MCPDynamicTool) Name() string {
	return "mcp_" + sanitizeToolName(t.serverName) + "_call"
}

func (t *MCPDynamicTool) Description() string {
	transport := normalizeTransport(t.serverCfg.Type)
	desc := strings.TrimSpace(t.serverCfg.Description)
	if desc == "" {
		desc = "Dynamic MCP proxy tool"
	}
	endpoint := strings.TrimSpace(t.serverCfg.URL)
	if endpoint == "" {
		endpoint = strings.TrimSpace(t.serverCfg.Command)
	}
	if endpoint != "" {
		return fmt.Sprintf("%s. Server=%s, transport=%s, endpoint=%s", desc, t.serverName, transport, endpoint)
	}
	return fmt.Sprintf("%s. Server=%s, transport=%s", desc, t.serverName, transport)
}

func (t *MCPDynamicTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "tool_name": {"type": "string", "description": "目标 MCP 工具名"},
    "arguments": {"type": "object", "description": "透传给目标 MCP 工具的参数"}
  },
  "required": ["tool_name"]
}`)
}

func (t *MCPDynamicTool) Execute(ctx context.Context, input string) (string, error) {
	var in mcpToolInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", fmt.Errorf("mcp dynamic tool %q: invalid input json: %w", t.Name(), err)
	}
	if strings.TrimSpace(in.ToolName) == "" {
		return "", fmt.Errorf("mcp dynamic tool %q: tool_name is required", t.Name())
	}

	var (
		out string
		err error
	)
	switch normalizeTransport(t.serverCfg.Type) {
	case "stdio":
		out, err = stdioMCPInvoker(ctx, t.serverName, t.serverCfg, in)
	case "http":
		out, err = httpMCPInvoker(ctx, t.serverName, t.serverCfg, in)
	case "sse":
		out, err = sseMCPInvoker(ctx, t.serverName, t.serverCfg, in)
	default:
		return "", fmt.Errorf("mcp dynamic tool %q: unsupported transport %q", t.Name(), t.serverCfg.Type)
	}
	if err != nil {
		return "", fmt.Errorf("mcp dynamic tool %q: %w", t.Name(), err)
	}
	return out, nil
}

// BuildMCPDynamicTools creates dynamic proxy tools from enabled MCP server configs.
func BuildMCPDynamicTools(cfg *config.AppConfig) []Tool {
	if cfg == nil {
		return nil
	}
	enabled := cfg.Extensions.EnabledMCPServers()
	if len(enabled) == 0 {
		return nil
	}

	names := make([]string, 0, len(enabled))
	for name, srv := range enabled {
		if isSupportedTransport(srv.Type) {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	out := make([]Tool, 0, len(names))
	for _, name := range names {
		out = append(out, NewMCPDynamicTool(name, enabled[name]))
	}
	return out
}

func isSupportedTransport(t string) bool {
	switch normalizeTransport(t) {
	case "stdio", "sse", "http":
		return true
	default:
		return false
	}
}

func normalizeTransport(t string) string {
	return strings.ToLower(strings.TrimSpace(t))
}

func sanitizeToolName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return "server"
	}
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	name := strings.Trim(b.String(), "_")
	if name == "" {
		return "server"
	}
	for strings.Contains(name, "__") {
		name = strings.ReplaceAll(name, "__", "_")
	}
	return name
}
