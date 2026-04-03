package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bookerbai/goclaw/internal/config"
)

func TestInvokeMCPHTTP(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req mcpEnvelope
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode req failed: %v", err)
		}

		if req.Method == "notifications/initialized" {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		if req.ID == nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"missing id"}`))
			return
		}

		switch req.Method {
		case "initialize":
			_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"capabilities":{}}}`, *req.ID)
		case "tools/list":
			_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"tools":[{"name":"search_docs"}]}}`, *req.ID)
		case "tools/call":
			_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"result":{"isError":false,"content":[{"type":"text","text":"done"}]}}`, *req.ID)
		default:
			_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"error":{"code":-32601,"message":"method not found"}}`, *req.ID)
		}
	}))
	defer ts.Close()

	out, err := invokeMCPHTTP(context.Background(), "remote-http", config.MCPServerConfig{Enabled: true, Type: "http", URL: ts.URL}, mcpToolInput{ToolName: "search_docs"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "done" {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestParseRPCResponse(t *testing.T) {
	out, err := parseRPCResponse([]byte(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(out), `"ok":true`) {
		t.Fatalf("unexpected result: %s", string(out))
	}

	if _, err := parseRPCResponse([]byte(`{"jsonrpc":"2.0","id":2,"result":{}}`), 1); err == nil {
		t.Fatalf("expected id mismatch error")
	}
}

func TestParseSSEEndpointEvent(t *testing.T) {
	if got := parseSSEEndpointEvent(`{"endpoint":"/mcp"}`); got != "/mcp" {
		t.Fatalf("unexpected endpoint parse: %q", got)
	}
	if got := parseSSEEndpointEvent("https://example.com/mcp"); got != "https://example.com/mcp" {
		t.Fatalf("unexpected raw endpoint parse: %q", got)
	}
}
