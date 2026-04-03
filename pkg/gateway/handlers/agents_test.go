package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bookerbai/goclaw/internal/config"
)

func TestAgentsHandler_ListAgents_Empty(t *testing.T) {
	h := NewAgentsHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
	rr := httptest.NewRecorder()
	ctx, _ := newGinContext(rr, req, nil)

	h.ListAgents(ctx)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	list := resp["agents"].([]any)
	if len(list) != 0 {
		t.Errorf("expected empty list, got %v", list)
	}
}

func TestAgentsHandler_GetAgent_NotFound(t *testing.T) {
	h := NewAgentsHandler(&config.AppConfig{Agents: map[string]config.AgentConfig{}})
	req := httptest.NewRequest(http.MethodGet, "/api/agents/unknown", nil)
	rr := httptest.NewRecorder()
	ctx, _ := newGinContext(rr, req, map[string]string{"name": "unknown"})

	h.GetAgent(ctx)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

func TestAgentsHandler_UpdateAgent(t *testing.T) {
	cfg := &config.AppConfig{Agents: map[string]config.AgentConfig{
		"demo": {Enabled: false, Model: "gpt-4"},
	}}
	h := NewAgentsHandler(cfg)

	body := `{"enabled": true, "model": "gpt-4o"}`
	req := httptest.NewRequest(http.MethodPut, "/api/agents/demo", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	ctx, _ := newGinContext(rr, req, map[string]string{"name": "demo"})

	h.UpdateAgent(ctx)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if !cfg.Agents["demo"].Enabled || cfg.Agents["demo"].Model != "gpt-4o" {
		t.Error("expected agent to be updated")
	}
}
