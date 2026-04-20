package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"goclaw/internal/config"
)

func TestMCPHandler_GetConfig_Empty(t *testing.T) {
	tmp := t.TempDir()
	extPath := filepath.Join(tmp, "extensions_config.json")

	h := NewMCPHandler(&config.AppConfig{ExtensionsRef: config.ExtensionsConfigRef{ConfigPath: extPath}})
	req := httptest.NewRequest(http.MethodGet, "/api/mcp/config", nil)
	rr := httptest.NewRecorder()
	ctx, _ := newGinContext(rr, req, nil)

	h.GetConfig(ctx)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp["mcp_servers"] == nil {
		t.Error("expected mcp_servers key")
	}
}

func TestMCPHandler_UpdateConfig(t *testing.T) {
	tmp := t.TempDir()
	extPath := filepath.Join(tmp, "extensions_config.json")
	_ = os.WriteFile(extPath, []byte(`{"mcp_servers":{}}`), 0o644)
	cfg := &config.AppConfig{ExtensionsRef: config.ExtensionsConfigRef{ConfigPath: extPath}}
	h := NewMCPHandler(cfg)

	body := `{"mcp_servers":{"demo":{"transport":"stdio","command":"demo-server"}}}`
	req := httptest.NewRequest(http.MethodPut, "/api/mcp/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	ctx, _ := newGinContext(rr, req, nil)

	h.UpdateConfig(ctx)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	data, _ := os.ReadFile(extPath)
	if !strings.Contains(string(data), "demo-server") {
		t.Error("expected updated file to contain demo-server")
	}
	if cfg.Extensions.MCPServers["demo"].Command != "demo-server" {
		t.Fatalf("expected in-memory cfg sync after update")
	}
}
