package llmerror

import (
	"context"
	"strings"
	"testing"

	"github.com/bookerbai/goclaw/internal/middleware"
)

func TestLLMErrorHandlingMiddleware_After_WithError(t *testing.T) {
	mw := NewLLMErrorHandlingMiddleware(3)
	state := &middleware.State{Extra: map[string]any{}}
	resp := &middleware.Response{ToolCalls: []map[string]any{{"id": "1", "name": "bash", "error": "permission denied"}}}
	if err := mw.AfterModel(context.Background(), state, resp); err != nil {
		t.Fatalf("after returned error: %v", err)
	}
	tc := resp.ToolCalls[0]
	if tc["is_error"] != true {
		t.Fatalf("expected is_error=true")
	}
	out, _ := tc["output"].(string)
	if !strings.Contains(out, "Permission denied") {
		t.Fatalf("unexpected output: %q", out)
	}
	if state.Extra["tool_retry_1"].(int) != 1 {
		t.Fatalf("expected retry counter incremented")
	}
}

func TestLLMErrorHandlingMiddleware_After_MaxRetries(t *testing.T) {
	mw := NewLLMErrorHandlingMiddleware(1)
	state := &middleware.State{Extra: map[string]any{"tool_retry_1": 1}}
	resp := &middleware.Response{ToolCalls: []map[string]any{{"id": "1", "name": "bash", "error": "timeout"}}}
	if err := mw.AfterModel(context.Background(), state, resp); err != nil {
		t.Fatalf("after returned error: %v", err)
	}
	tc := resp.ToolCalls[0]
	if tc["max_retries_exceeded"] != true {
		t.Fatalf("expected max_retries_exceeded=true")
	}
}

func TestConvertToFriendlyError(t *testing.T) {
	if got := convertToFriendlyError("connection refused by host"); !strings.Contains(got, "Connection failed") {
		t.Fatalf("unexpected mapping: %q", got)
	}
	longErr := strings.Repeat("x", 300)
	if got := convertToFriendlyError(longErr); len(got) <= 200 {
		t.Fatalf("expected truncation with ellipsis")
	}
}
