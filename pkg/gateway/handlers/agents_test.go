package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"goclaw/internal/config"
	"gopkg.in/yaml.v3"
)

func newPersistedAgentsHandler(t *testing.T, cfg *config.AppConfig) *AgentsHandler {
	t.Helper()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "config.yaml")
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("GOCLAW_CONFIG_PATH", path)
	return NewAgentsHandler(cfg)
}

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

func TestAgentsHandler_ListAgents_WithAgents(t *testing.T) {
	cfg := &config.AppConfig{Agents: map[string]config.AgentConfig{
		"agent1": {Enabled: true, Model: "gpt-4", Description: "Agent 1"},
		"agent2": {Enabled: false, Model: "gpt-3.5", Description: "Agent 2"},
	}}
	h := NewAgentsHandler(cfg)
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
	if len(list) != 2 {
		t.Errorf("expected 2 agents, got %d", len(list))
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

func TestAgentsHandler_GetAgent_Found(t *testing.T) {
	cfg := &config.AppConfig{Agents: map[string]config.AgentConfig{
		"demo": {Enabled: true, Model: "gpt-4", Description: "Demo agent"},
	}}
	h := NewAgentsHandler(cfg)
	req := httptest.NewRequest(http.MethodGet, "/api/agents/demo", nil)
	rr := httptest.NewRecorder()
	ctx, _ := newGinContext(rr, req, map[string]string{"name": "demo"})

	h.GetAgent(ctx)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestAgentsHandler_UpdateAgent(t *testing.T) {
	cfg := &config.AppConfig{Agents: map[string]config.AgentConfig{
		"demo": {Enabled: false, Model: "gpt-4"},
	}}
	h := newPersistedAgentsHandler(t, cfg)

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

func TestAgentsHandler_UpdateAgent_NotFound(t *testing.T) {
	cfg := &config.AppConfig{Agents: map[string]config.AgentConfig{}}
	h := newPersistedAgentsHandler(t, cfg)

	body := `{"enabled": true}`
	req := httptest.NewRequest(http.MethodPut, "/api/agents/nonexistent", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	ctx, _ := newGinContext(rr, req, map[string]string{"name": "nonexistent"})

	h.UpdateAgent(ctx)

	// UpdateAgent may create agent if not found (based on implementation)
	if rr.Code != http.StatusOK && rr.Code != http.StatusNotFound {
		t.Fatalf("expected 200 or 404, got %d", rr.Code)
	}
}

func TestAgentsHandler_CreateAndDeleteAgent(t *testing.T) {
	cfg := &config.AppConfig{Agents: map[string]config.AgentConfig{}}
	h := newPersistedAgentsHandler(t, cfg)

	createBody := `{"name":"worker-1","enabled":true,"model":"gpt-4o"}`
	createReq := httptest.NewRequest(http.MethodPost, "/api/agents", strings.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createRR := httptest.NewRecorder()
	createCtx, _ := newGinContext(createRR, createReq, nil)
	h.CreateAgent(createCtx)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", createRR.Code, createRR.Body.String())
	}
	if _, ok := cfg.Agents["worker-1"]; !ok {
		t.Fatalf("expected created agent in config")
	}

	delReq := httptest.NewRequest(http.MethodDelete, "/api/agents/worker-1", nil)
	delRR := httptest.NewRecorder()
	delCtx, _ := newGinContext(delRR, delReq, map[string]string{"name": "worker-1"})
	h.DeleteAgent(delCtx)
	if delRR.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", delRR.Code)
	}
	if _, ok := cfg.Agents["worker-1"]; ok {
		t.Fatalf("expected agent deleted")
	}
}

func TestAgentsHandler_CreateAgent_InvalidName(t *testing.T) {
	cfg := &config.AppConfig{Agents: map[string]config.AgentConfig{}}
	h := NewAgentsHandler(cfg)

	body := `{"name":"..","enabled":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/agents", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	ctx, _ := newGinContext(rr, req, nil)

	h.CreateAgent(ctx)

	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", rr.Code)
	}
}

func TestAgentsHandler_CreateAgent_Duplicate(t *testing.T) {
	cfg := &config.AppConfig{Agents: map[string]config.AgentConfig{
		"existing": {Enabled: true},
	}}
	h := NewAgentsHandler(cfg)

	body := `{"name":"existing","enabled":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/agents", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	ctx, _ := newGinContext(rr, req, nil)

	h.CreateAgent(ctx)

	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", rr.Code)
	}
}

func TestAgentsHandler_DeleteAgent_NotFound(t *testing.T) {
	cfg := &config.AppConfig{Agents: map[string]config.AgentConfig{}}
	h := newPersistedAgentsHandler(t, cfg)

	req := httptest.NewRequest(http.MethodDelete, "/api/agents/nonexistent", nil)
	rr := httptest.NewRecorder()
	ctx, _ := newGinContext(rr, req, map[string]string{"name": "nonexistent"})

	h.DeleteAgent(ctx)

	// DeleteAgent returns 200 even if not found
	if rr.Code != http.StatusOK && rr.Code != http.StatusNotFound {
		t.Fatalf("expected 200 or 404, got %d", rr.Code)
	}
}

func TestAgentsHandler_CheckAgentName(t *testing.T) {
	cfg := &config.AppConfig{Agents: map[string]config.AgentConfig{"demo": {Enabled: true}}}
	h := NewAgentsHandler(cfg)

	okReq := httptest.NewRequest(http.MethodGet, "/api/agents/check?name=new_agent", nil)
	okRR := httptest.NewRecorder()
	okCtx, _ := newGinContext(okRR, okReq, nil)
	h.CheckAgentName(okCtx)
	if okRR.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", okRR.Code)
	}

	takenReq := httptest.NewRequest(http.MethodGet, "/api/agents/check?name=demo", nil)
	takenRR := httptest.NewRecorder()
	takenCtx, _ := newGinContext(takenRR, takenReq, nil)
	h.CheckAgentName(takenCtx)
	if takenRR.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", takenRR.Code)
	}

	badReq := httptest.NewRequest(http.MethodGet, "/api/agents/check?name=..", nil)
	badRR := httptest.NewRecorder()
	badCtx, _ := newGinContext(badRR, badReq, nil)
	h.CheckAgentName(badCtx)
	if badRR.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", badRR.Code)
	}
}

func TestAgentsHandler_CheckAgentName_MissingName(t *testing.T) {
	h := NewAgentsHandler(nil)

	req := httptest.NewRequest(http.MethodGet, "/api/agents/check", nil)
	rr := httptest.NewRecorder()
	ctx, _ := newGinContext(rr, req, nil)

	h.CheckAgentName(ctx)

	// CheckAgentName returns 422 for missing name (treated as invalid)
	if rr.Code != http.StatusUnprocessableEntity && rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 422 or 400, got %d", rr.Code)
	}
}

func TestAgentsHandler_GetUserProfile(t *testing.T) {
	h := NewAgentsHandler(&config.AppConfig{})
	req := httptest.NewRequest(http.MethodGet, "/api/user-profile", nil)
	rr := httptest.NewRecorder()
	ctx, _ := newGinContext(rr, req, nil)
	h.GetUserProfile(ctx)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestNewAgentsHandler(t *testing.T) {
	h := NewAgentsHandler(nil)
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
}

func TestIsValidAgentName(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
	}{
		{"valid_agent", true},
		{"ValidAgent123", true},
		{"a", false}, // too short
		{"1invalid", false}, // must start with letter
		{"", false},
		{"..", false},
		{"agent-with-dash", true},
		{"agent_with_underscore", true},
	}
	for _, tc := range tests {
		result := isValidAgentName(tc.name)
		if result != tc.expected {
			t.Errorf("isValidAgentName(%q) = %v, expected %v", tc.name, result, tc.expected)
		}
	}
}

func TestGetAgentDir(t *testing.T) {
	dir := getAgentDir("test_agent")
	if !strings.Contains(dir, "test_agent") {
		t.Errorf("expected agent dir to contain 'test_agent', got %s", dir)
	}
}

func TestLoadAgentConfig(t *testing.T) {
	// Test loading non-existent config
	_, err := loadAgentConfig("nonexistent_agent_" + uuid.NewString()[:8])
	if err == nil {
		t.Error("expected error for non-existent agent config")
	}
}
