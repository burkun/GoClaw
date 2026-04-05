package audit

import (
	"context"
	"testing"

	"github.com/bookerbai/goclaw/internal/middleware"
)

type captureLogger struct {
	entries []AuditEntry
}

func (l *captureLogger) Log(entry AuditEntry) { l.entries = append(l.entries, entry) }

func TestSandboxAuditMiddleware_After_SandboxTool(t *testing.T) {
	logger := &captureLogger{}
	mw := NewSandboxAuditMiddleware(logger)
	state := &middleware.State{ThreadID: "thread-1"}
	resp := &middleware.Response{ToolCalls: []map[string]any{{
		"name":        "bash",
		"args":        map[string]any{"command": "ls -la"},
		"duration_ms": float64(12),
	}}}
	if err := mw.AfterModel(context.Background(), state, resp); err != nil {
		t.Fatalf("after returned error: %v", err)
	}
	if len(logger.entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(logger.entries))
	}
	if logger.entries[0].Operation != "execute" {
		t.Fatalf("unexpected op: %s", logger.entries[0].Operation)
	}
}

func TestSandboxAuditMiddleware_After_NonSandboxIgnored(t *testing.T) {
	logger := &captureLogger{}
	mw := NewSandboxAuditMiddleware(logger)
	resp := &middleware.Response{ToolCalls: []map[string]any{{"name": "web_search"}}}
	if err := mw.AfterModel(context.Background(), &middleware.State{}, resp); err != nil {
		t.Fatalf("after returned error: %v", err)
	}
	if len(logger.entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(logger.entries))
	}
}

func TestIsSandboxToolAndHelpers(t *testing.T) {
	if !isSandboxTool("Bash") || isSandboxTool("web_search") {
		t.Fatalf("sandbox tool classification mismatch")
	}
	if classifyOperation("read_file") != "read" {
		t.Fatalf("expected read operation")
	}
	if classifyOperation("write_file") != "write" {
		t.Fatalf("expected write operation")
	}
	if classifyOperation("bash") != "execute" {
		t.Fatalf("expected execute operation")
	}
	long := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	if got := truncateCommand(long); len(got) <= 100 {
		t.Fatalf("expected truncated command")
	}
}
