package tool

import (
	"context"
	"errors"
	"testing"

	"goclaw/internal/middleware"
)

func TestWrapToolCall_ConvertsHandlerErrorToStructuredResult(t *testing.T) {
	mw := NewToolErrorHandlingMiddleware()
	res, err := mw.WrapToolCall(
		context.Background(),
		&middleware.State{},
		&middleware.ToolCall{ID: "1", Name: "read_file"},
		func(ctx context.Context, toolCall *middleware.ToolCall) (*middleware.ToolResult, error) {
			return nil, errors.New("boom")
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res == nil {
		t.Fatalf("expected result")
	}
	out, ok := res.Output.(map[string]any)
	if !ok {
		t.Fatalf("expected map output, got %T", res.Output)
	}
	if out["status"] != "error" {
		t.Fatalf("expected status=error, got %#v", out["status"])
	}
	if out["error_type"] != "tool_execution_error" {
		t.Fatalf("expected structured error_type, got %#v", out["error_type"])
	}
	if out["tool_name"] != "read_file" {
		t.Fatalf("expected tool_name=read_file, got %#v", out["tool_name"])
	}
	if res.Error != nil {
		t.Fatalf("expected cleared result.Error, got %v", res.Error)
	}
}

func TestWrapToolCall_ConvertsResultErrorToStructuredResult(t *testing.T) {
	mw := NewToolErrorHandlingMiddleware()
	res, err := mw.WrapToolCall(
		context.Background(),
		&middleware.State{},
		&middleware.ToolCall{ID: "2", Name: "grep"},
		func(ctx context.Context, toolCall *middleware.ToolCall) (*middleware.ToolResult, error) {
			return &middleware.ToolResult{ID: toolCall.ID, Error: errors.New("inner error")}, nil
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res == nil {
		t.Fatalf("expected result")
	}
	out, ok := res.Output.(map[string]any)
	if !ok {
		t.Fatalf("expected map output, got %T", res.Output)
	}
	if out["error_message"] != "inner error" {
		t.Fatalf("expected propagated inner error, got %#v", out["error_message"])
	}
}

func TestWrapToolCall_PassThroughSuccess(t *testing.T) {
	mw := NewToolErrorHandlingMiddleware()
	res, err := mw.WrapToolCall(
		context.Background(),
		&middleware.State{},
		&middleware.ToolCall{ID: "3", Name: "ls"},
		func(ctx context.Context, toolCall *middleware.ToolCall) (*middleware.ToolResult, error) {
			return &middleware.ToolResult{ID: toolCall.ID, Output: "ok"}, nil
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res == nil || res.Output != "ok" {
		t.Fatalf("expected passthrough success, got %#v", res)
	}
}
