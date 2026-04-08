package tool

import (
	"context"
	"testing"

	"github.com/bookerbai/goclaw/internal/middleware"
)

func TestDeferredToolFilterMiddleware_Name(t *testing.T) {
	mw := NewDeferredToolFilterMiddleware([]string{"ImageGen"})
	if mw.Name() != "DeferredToolFilterMiddleware" {
		t.Errorf("expected name 'DeferredToolFilterMiddleware', got %s", mw.Name())
	}
}

func TestDeferredToolFilterMiddleware_BeforeModel_NilState(t *testing.T) {
	mw := NewDeferredToolFilterMiddleware([]string{"ImageGen"})
	err := mw.BeforeModel(context.Background(), nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDeferredToolFilterMiddleware_BeforeModel_NilExtra(t *testing.T) {
	mw := NewDeferredToolFilterMiddleware([]string{"ImageGen"})
	state := &middleware.State{}
	err := mw.BeforeModel(context.Background(), state)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDeferredToolFilterMiddleware_BeforeModel_ToolSearchDisabled(t *testing.T) {
	mw := NewDeferredToolFilterMiddleware([]string{"ImageGen"})
	state := &middleware.State{
		Extra: map[string]any{
			"tool_search_enabled": false,
			"available_tools":     []string{"Read", "Write", "ImageGen"},
		},
	}
	err := mw.BeforeModel(context.Background(), state)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	// Tools should remain unchanged when tool_search is disabled
	tools := state.Extra["available_tools"].([]string)
	if len(tools) != 3 {
		t.Errorf("expected 3 tools, got %d", len(tools))
	}
}

func TestDeferredToolFilterMiddleware_BeforeModel_ToolSearchEnabled(t *testing.T) {
	mw := NewDeferredToolFilterMiddleware([]string{"ImageGen", "ImageEdit"})
	state := &middleware.State{
		Extra: map[string]any{
			"tool_search_enabled": true,
			"available_tools":     []string{"Read", "Write", "ImageGen", "ImageEdit", "Bash"},
		},
	}
	err := mw.BeforeModel(context.Background(), state)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	tools := state.Extra["available_tools"].([]string)
	if len(tools) != 3 {
		t.Errorf("expected 3 tools after filtering, got %d: %v", len(tools), tools)
	}
	for _, tool := range tools {
		if tool == "ImageGen" || tool == "ImageEdit" {
			t.Errorf("deferred tool %s should be filtered out", tool)
		}
	}
}

func TestDeferredToolFilterMiddleware_BeforeModel_EmptyTools(t *testing.T) {
	mw := NewDeferredToolFilterMiddleware([]string{"ImageGen"})
	state := &middleware.State{
		Extra: map[string]any{
			"tool_search_enabled": true,
			"available_tools":     []string{},
		},
	}
	err := mw.BeforeModel(context.Background(), state)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDeferredToolFilterMiddleware_BeforeModel_NoAvailableTools(t *testing.T) {
	mw := NewDeferredToolFilterMiddleware([]string{"ImageGen"})
	state := &middleware.State{
		Extra: map[string]any{
			"tool_search_enabled": true,
		},
	}
	err := mw.BeforeModel(context.Background(), state)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDeferredToolFilterMiddleware_BeforeModel_CaseInsensitive(t *testing.T) {
	mw := NewDeferredToolFilterMiddleware([]string{"IMAGEGEN", "imageedit"})
	state := &middleware.State{
		Extra: map[string]any{
			"tool_search_enabled": true,
			"available_tools":     []string{"Read", "imagegen", "ImageEdit", "Bash"},
		},
	}
	err := mw.BeforeModel(context.Background(), state)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	tools := state.Extra["available_tools"].([]string)
	for _, tool := range tools {
		if tool == "imagegen" || tool == "ImageEdit" {
			t.Errorf("deferred tool %s should be filtered out", tool)
		}
	}
}

func TestDeferredToolFilterMiddleware_BeforeModel_DeferredToolsSet(t *testing.T) {
	deferred := []string{"ImageGen", "ImageEdit"}
	mw := NewDeferredToolFilterMiddleware(deferred)
	state := &middleware.State{
		Extra: map[string]any{
			"tool_search_enabled": true,
			"available_tools":     []string{"Read"},
		},
	}
	err := mw.BeforeModel(context.Background(), state)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	stored := state.Extra["deferred_tools"].([]string)
	if len(stored) != 2 {
		t.Errorf("expected 2 deferred tools, got %d", len(stored))
	}
}

func TestDeferredToolFilterMiddleware_AfterModel(t *testing.T) {
	mw := NewDeferredToolFilterMiddleware([]string{"ImageGen"})
	err := mw.AfterModel(context.Background(), &middleware.State{}, &middleware.Response{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDefaultDeferredTools(t *testing.T) {
	tools := DefaultDeferredTools()
	if len(tools) == 0 {
		t.Error("expected non-empty default deferred tools list")
	}
	// Check for some expected tools
	expectedTools := []string{"ImageGen", "LSP", "NotebookEdit"}
	for _, expected := range expectedTools {
		found := false
		for _, tool := range tools {
			if tool == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %s in default deferred tools", expected)
		}
	}
}
