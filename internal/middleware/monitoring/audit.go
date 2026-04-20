// Package monitoring implements monitoring-related middleware for GoClaw.
//
// This package contains middlewares that monitor and audit operations,
// including sandbox auditing and token usage tracking.

package monitoring

import (
	"context"
	"regexp"
	"strings"
	"time"

	"goclaw/internal/logging"
	"goclaw/internal/middleware"
)

// AuditEntry represents a single audit log entry.
type AuditEntry struct {
	Timestamp  time.Time      `json:"timestamp"`
	ThreadID   string         `json:"thread_id"`
	ToolName   string         `json:"tool_name"`
	Operation  string         `json:"operation"`
	Target     string         `json:"target,omitempty"`
	Success    bool           `json:"success"`
	ErrorMsg   string         `json:"error_msg,omitempty"`
	DurationMs int64          `json:"duration_ms,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// AuditLogger defines the interface for audit logging.
type AuditLogger interface {
	Log(entry AuditEntry)
}

// DefaultAuditLogger logs using slog.
type DefaultAuditLogger struct{}

func (l *DefaultAuditLogger) Log(entry AuditEntry) {
	logging.Info("[SandboxAudit]",
		"thread", entry.ThreadID,
		"tool", entry.ToolName,
		"op", entry.Operation,
		"target", entry.Target,
		"success", entry.Success,
		"error", entry.ErrorMsg,
		"duration_ms", entry.DurationMs,
	)
}

// CommandRiskLevel represents the risk classification of a command.
type CommandRiskLevel string

const (
	RiskPass  CommandRiskLevel = "pass"  // Safe to execute
	RiskWarn  CommandRiskLevel = "warn"  // Medium-risk, add warning
	RiskBlock CommandRiskLevel = "block" // High-risk, block execution
)

// SandboxAuditMiddleware logs sandbox operations and intercepts risky commands.
type SandboxAuditMiddleware struct {
	middleware.MiddlewareWrapper
	logger             AuditLogger
	highRiskPatterns   []*regexp.Regexp
	mediumRiskPatterns []*regexp.Regexp
}

// NewSandboxAuditMiddleware constructs a SandboxAuditMiddleware.
func NewSandboxAuditMiddleware(logger AuditLogger) *SandboxAuditMiddleware {
	if logger == nil {
		logger = &DefaultAuditLogger{}
	}
	return &SandboxAuditMiddleware{
		logger: logger,
		highRiskPatterns: []*regexp.Regexp{
			// rm -rf / or rm -rf /*
			regexp.MustCompile(`rm\s+-[^\s]*r[^\s]*\s+(/\*?|~/?\*?)\s*$`),
			// curl | bash or wget | bash
			regexp.MustCompile(`(curl|wget).+\|\s*(ba)?sh`),
			// chmod 777
			regexp.MustCompile(`chmod\s+777`),
		},
		mediumRiskPatterns: []*regexp.Regexp{
			// pip install
			regexp.MustCompile(`pip\s+install`),
			// npm install -g
			regexp.MustCompile(`npm\s+install\s+-g`),
		},
	}
}

// Name implements middleware.Middleware.
func (m *SandboxAuditMiddleware) Name() string { return "SandboxAuditMiddleware" }

// WrapToolCall intercepts bash commands and enforces security policies.
// High-risk commands are blocked, medium-risk commands receive warnings.
func (m *SandboxAuditMiddleware) WrapToolCall(
	ctx context.Context,
	state *middleware.State,
	toolCall *middleware.ToolCall,
	handler middleware.ToolHandler,
) (*middleware.ToolResult, error) {
	// Only intercept bash tool
	if toolCall.Name != "bash" && toolCall.Name != "Bash" {
		return handler(ctx, toolCall)
	}

	// Extract command
	command, _ := toolCall.Input["command"].(string)
	if command == "" {
		return handler(ctx, toolCall)
	}

	// Classify command
	verdict := m.classifyCommand(command)
	threadID := ""
	if state != nil {
		threadID = state.ThreadID
	}

	// Log the audit
	m.logger.Log(AuditEntry{
		Timestamp: time.Now(),
		ThreadID:  threadID,
		ToolName:  toolCall.Name,
		Operation: "execute",
		Target:    truncateCommand(command),
		Success:   verdict != RiskBlock,
		Metadata:  map[string]any{"verdict": string(verdict)},
	})

	// Block high-risk commands
	if verdict == RiskBlock {
		return &middleware.ToolResult{
			ID: toolCall.ID,
			Output: map[string]any{
				"status":  "error",
				"content": "Command blocked: security violation detected. Choose an alternative approach.",
			},
			Error: nil,
		}, nil
	}

	// Execute the command
	result, err := handler(ctx, toolCall)
	if err != nil {
		return result, err
	}

	// Append warning for medium-risk commands
	if verdict == RiskWarn {
		if resultMap, ok := result.Output.(map[string]any); ok {
			if content, ok := resultMap["content"].(string); ok {
				resultMap["content"] = content + "\n\n⚠️ Warning: This is a medium-risk command. Proceed with caution."
			}
		}
	}

	return result, nil
}

func (m *SandboxAuditMiddleware) classifyCommand(command string) CommandRiskLevel {
	for _, pattern := range m.highRiskPatterns {
		if pattern.MatchString(command) {
			return RiskBlock
		}
	}
	for _, pattern := range m.mediumRiskPatterns {
		if pattern.MatchString(command) {
			return RiskWarn
		}
	}
	return RiskPass
}

func (m *SandboxAuditMiddleware) getThreadID(state *middleware.State) string {
	if state == nil {
		return "unknown"
	}
	return state.ThreadID
}

// AfterModel audits completed tool calls.
func (m *SandboxAuditMiddleware) AfterModel(_ context.Context, state *middleware.State, resp *middleware.Response) error {
	if resp == nil || len(resp.ToolCalls) == 0 {
		return nil
	}

	threadID := ""
	if state != nil {
		threadID = state.ThreadID
	}

	for _, tc := range resp.ToolCalls {
		toolName, _ := tc["name"].(string)
		if toolName == "" {
			continue
		}

		// Only audit sandbox-related tools.
		if !isSandboxTool(toolName) {
			continue
		}

		entry := AuditEntry{
			Timestamp: time.Now(),
			ThreadID:  threadID,
			ToolName:  toolName,
			Operation: classifyOperation(toolName),
			Success:   true,
		}

		// Extract target from arguments.
		if args, ok := tc["args"].(map[string]any); ok {
			if path, ok := args["path"].(string); ok {
				entry.Target = path
			} else if filePath, ok := args["file_path"].(string); ok {
				entry.Target = filePath
			} else if command, ok := args["command"].(string); ok {
				entry.Target = truncateCommand(command)
			}
		}

		// Check for errors.
		if errMsg, ok := tc["error"].(string); ok && errMsg != "" {
			entry.Success = false
			entry.ErrorMsg = errMsg
		}

		// Extract duration if available.
		if duration, ok := tc["duration_ms"].(float64); ok {
			entry.DurationMs = int64(duration)
		}

		m.logger.Log(entry)
	}

	return nil
}

func isSandboxTool(name string) bool {
	sandboxTools := map[string]bool{
		"bash":       true,
		"read_file":  true,
		"write_file": true,
		"edit_file":  true,
		"list_dir":   true,
		"glob":       true,
		"grep":       true,
		"Read":       true,
		"Write":      true,
		"Edit":       true,
		"MultiEdit":  true,
		"Bash":       true,
		"Glob":       true,
		"Grep":       true,
	}
	return sandboxTools[name]
}

func classifyOperation(toolName string) string {
	lower := strings.ToLower(toolName)
	switch {
	case strings.Contains(lower, "read"), strings.Contains(lower, "glob"), strings.Contains(lower, "grep"), strings.Contains(lower, "list"):
		return "read"
	case strings.Contains(lower, "write"), strings.Contains(lower, "edit"):
		return "write"
	case strings.Contains(lower, "bash"):
		return "execute"
	default:
		return "unknown"
	}
}

func truncateCommand(cmd string) string {
	if len(cmd) > 100 {
		return cmd[:100] + "..."
	}
	return cmd
}

var _ middleware.Middleware = (*SandboxAuditMiddleware)(nil)
