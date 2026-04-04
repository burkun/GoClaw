// Package audit implements SandboxAuditMiddleware for GoClaw.
//
// SandboxAuditMiddleware logs sandbox operations for security auditing,
// tracking file operations, command executions, and resource usage.
package audit

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/bookerbai/goclaw/internal/middleware"
)

// AuditEntry represents a single audit log entry.
type AuditEntry struct {
	Timestamp   time.Time      `json:"timestamp"`
	ThreadID    string         `json:"thread_id"`
	ToolName    string         `json:"tool_name"`
	Operation   string         `json:"operation"`
	Target      string         `json:"target,omitempty"`
	Success     bool           `json:"success"`
	ErrorMsg    string         `json:"error_msg,omitempty"`
	DurationMs  int64          `json:"duration_ms,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// AuditLogger defines the interface for audit logging.
type AuditLogger interface {
	Log(entry AuditEntry)
}

// DefaultAuditLogger logs to standard logger.
type DefaultAuditLogger struct{}

func (l *DefaultAuditLogger) Log(entry AuditEntry) {
	log.Printf("[SandboxAudit] thread=%s tool=%s op=%s target=%s success=%v err=%s",
		entry.ThreadID, entry.ToolName, entry.Operation, entry.Target, entry.Success, entry.ErrorMsg)
}

// SandboxAuditMiddleware logs sandbox operations.
type SandboxAuditMiddleware struct {
	middleware.MiddlewareWrapper
	logger AuditLogger
}

// NewSandboxAuditMiddleware constructs a SandboxAuditMiddleware.
func NewSandboxAuditMiddleware(logger AuditLogger) *SandboxAuditMiddleware {
	if logger == nil {
		logger = &DefaultAuditLogger{}
	}
	return &SandboxAuditMiddleware{logger: logger}
}

// Name implements middleware.Middleware.
func (m *SandboxAuditMiddleware) Name() string { return "SandboxAuditMiddleware" }

// Before is a no-op.
func (m *SandboxAuditMiddleware) Before(_ context.Context, _ *middleware.State) error {
	return nil
}

// After audits completed tool calls.
func (m *SandboxAuditMiddleware) After(_ context.Context, state *middleware.State, resp *middleware.Response) error {
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
