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

func TestAgentsHandler_CreateAndDeleteAgent(t *testing.T) {
	cfg := &config.AppConfig{Agents: map[string]config.AgentConfig{}}
	h := NewAgentsHandler(cfg)

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
