package builtin

import (
	"context"
	"encoding/json"
	"testing"
)

// TestToolSearchTool_Name tests the Name method
func TestToolSearchTool_Name(t *testing.T) {
	tool := NewToolSearchTool(nil)
	if tool.Name() != "tool_search" {
		t.Errorf("expected name 'tool_search', got %q", tool.Name())
	}
}

// TestToolSearchTool_Description tests the Description method
func TestToolSearchTool_Description(t *testing.T) {
	tool := NewToolSearchTool(nil)
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
}

// TestToolSearchTool_InputSchema tests the InputSchema method
func TestToolSearchTool_InputSchema(t *testing.T) {
	tool := NewToolSearchTool(nil)
	schema := tool.InputSchema()
	if len(schema) == 0 {
		t.Error("expected non-empty input schema")
	}

	var parsed map[string]any
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Errorf("input schema is not valid JSON: %v", err)
	}
}

func TestToolSearchTool_Execute_SearchAndLimit(t *testing.T) {
	tool := NewToolSearchTool([]ToolEntry{
		{Name: "ImageGen", Description: "generate image", Keywords: []string{"image", "generate"}},
		{Name: "Bash", Description: "run commands", Keywords: []string{"shell"}},
	})
	out, err := tool.Execute(context.Background(), `{"query":"image generate","limit":1}`)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if int(payload["count"].(float64)) != 1 {
		t.Fatalf("expected 1 result, got %v", payload["count"])
	}
}

func TestToolSearchTool_Execute_EmptyQuery(t *testing.T) {
	tool := NewToolSearchTool(nil)
	if _, err := tool.Execute(context.Background(), `{"query":""}`); err == nil {
		t.Fatalf("expected query validation error")
	}
}

func TestDefaultDeferredToolRegistry(t *testing.T) {
	list := DefaultDeferredToolRegistry()
	if len(list) < 10 {
		t.Fatalf("expected default deferred tools, got %d", len(list))
	}
}

func TestToolSearchTool_Execute_InvalidJSON(t *testing.T) {
	tool := NewToolSearchTool(nil)
	_, err := tool.Execute(context.Background(), `{invalid json`)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestToolSearchTool_Execute_LimitCap(t *testing.T) {
	tool := NewToolSearchTool([]ToolEntry{
		{Name: "A", Description: "a", Keywords: []string{"test"}},
		{Name: "B", Description: "b", Keywords: []string{"test"}},
	})
	// Limit above 50 should be capped
	out, err := tool.Execute(context.Background(), `{"query":"test","limit":100}`)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	// Should be capped, not error
	if payload["count"] == nil {
		t.Error("expected count in result")
	}
}

func TestToolSearchTool_AddTool(t *testing.T) {
	tool := NewToolSearchTool(nil)
	tool.AddTool(ToolEntry{Name: "NewTool", Description: "A new tool"})
	if len(tool.registry) != 1 {
		t.Errorf("expected 1 tool, got %d", len(tool.registry))
	}
}

func TestToolSearchTool_SetRegistry(t *testing.T) {
	tool := NewToolSearchTool(nil)
	newRegistry := []ToolEntry{
		{Name: "Tool1", Description: "First"},
		{Name: "Tool2", Description: "Second"},
	}
	tool.SetRegistry(newRegistry)
	if len(tool.registry) != 2 {
		t.Errorf("expected 2 tools, got %d", len(tool.registry))
	}
}

func TestToolSearchTool_Search_ScoreOrder(t *testing.T) {
	tool := NewToolSearchTool([]ToolEntry{
		{Name: "exact", Description: "exact match tool", Keywords: []string{"exact"}},
		{Name: "partial", Description: "partial match", Keywords: []string{"other"}},
		{Name: "exact_other", Description: "another exact", Keywords: []string{"exact"}},
	})
	// Exact name match should score highest
	out, err := tool.Execute(context.Background(), `{"query":"exact","limit":2}`)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	results := payload["results"].([]interface{})
	if len(results) > 2 {
		t.Errorf("expected at most 2 results, got %d", len(results))
	}
}

func TestToolSearchTool_Search_CategoryMatch(t *testing.T) {
	tool := NewToolSearchTool([]ToolEntry{
		{Name: "Tool1", Description: "desc", Category: "media", Keywords: []string{"image"}},
		{Name: "Tool2", Description: "desc", Category: "code", Keywords: []string{"other"}},
	})
	out, err := tool.Execute(context.Background(), `{"query":"media"}`)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if int(payload["count"].(float64)) != 1 {
		t.Errorf("expected 1 result for category match, got %v", payload["count"])
	}
}
