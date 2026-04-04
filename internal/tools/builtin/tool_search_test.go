package builtin

import (
	"context"
	"encoding/json"
	"testing"
)

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
