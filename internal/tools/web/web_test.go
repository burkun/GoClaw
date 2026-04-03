package web

import (
	"context"
	"strings"
	"testing"
)

func TestWebSearchToolExecute_MissingAPIKey(t *testing.T) {
	tool := NewWebSearchTool(WebToolConfig{TavilyAPIKey: ""})
	out, err := tool.Execute(context.Background(), `{"query":"golang"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "TAVILY_API_KEY") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestWebFetchToolExecute_InvalidURL(t *testing.T) {
	tool := NewWebFetchTool(WebToolConfig{})
	out, err := tool.Execute(context.Background(), `{"url":"example.com"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "invalid URL") {
		t.Fatalf("unexpected output: %s", out)
	}
}
