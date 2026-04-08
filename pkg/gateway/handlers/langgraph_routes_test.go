package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/bookerbai/goclaw/internal/agent"
	"github.com/bookerbai/goclaw/internal/config"
)

func TestLangGraphHandler_CreateThread(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewLangGraphHandler(&config.AppConfig{}, nil)
	router := gin.New()
	router.POST("/threads", h.CreateThread)

	req := httptest.NewRequest(http.MethodPost, "/threads", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, rr.Code, rr.Body.String())
	}

	// Verify response contains thread_id.
	if !strings.Contains(rr.Body.String(), "thread_id") {
		t.Errorf("expected response to contain thread_id, got: %s", rr.Body.String())
	}
}

func TestLangGraphHandler_CreateThread_WithID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewLangGraphHandler(&config.AppConfig{}, nil)
	router := gin.New()
	router.POST("/threads", h.CreateThread)

	req := httptest.NewRequest(http.MethodPost, "/threads", strings.NewReader(`{"thread_id": "custom-thread-123"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, rr.Code, rr.Body.String())
	}

	if !strings.Contains(rr.Body.String(), "custom-thread-123") {
		t.Errorf("expected response to contain custom-thread-123, got: %s", rr.Body.String())
	}
}

func TestLangGraphHandler_GetThread(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewLangGraphHandler(&config.AppConfig{}, nil)
	router := gin.New()
	router.GET("/threads/:thread_id", h.GetThread)

	req := httptest.NewRequest(http.MethodGet, "/threads/test-thread-123", nil)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
	}

	if !strings.Contains(rr.Body.String(), "test-thread-123") {
		t.Errorf("expected response to contain thread_id, got: %s", rr.Body.String())
	}
}

func TestLangGraphHandler_GetThread_MissingID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewLangGraphHandler(&config.AppConfig{}, nil)
	router := gin.New()
	router.GET("/threads/:thread_id", h.GetThread)

	req := httptest.NewRequest(http.MethodGet, "/threads/", nil)
	rr := httptest.NewRecorder()

	// Manually create context without thread_id
	c, _ := gin.CreateTestContext(rr)
	c.Request = req
	h.GetThread(c)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, rr.Code, rr.Body.String())
	}
}

func TestLangGraphHandler_DeleteThread(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewLangGraphHandler(&config.AppConfig{}, nil)
	router := gin.New()
	router.DELETE("/threads/:thread_id", h.DeleteThread)

	req := httptest.NewRequest(http.MethodDelete, "/threads/thread-to-delete", nil)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
	}

	if !strings.Contains(rr.Body.String(), "deleted") {
		t.Errorf("expected response to contain deleted, got: %s", rr.Body.String())
	}
}

func TestLangGraphHandler_SearchThreads(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewLangGraphHandler(&config.AppConfig{}, nil)
	router := gin.New()
	router.POST("/threads/search", h.SearchThreads)

	req := httptest.NewRequest(http.MethodPost, "/threads/search", strings.NewReader(`{"limit": 10}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
	}
}

func TestLangGraphHandler_GetAssistant(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewLangGraphHandler(&config.AppConfig{}, nil)
	router := gin.New()
	router.GET("/assistants/:assistant_id", h.GetAssistant)

	req := httptest.NewRequest(http.MethodGet, "/assistants/lead_agent", nil)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
	}

	if !strings.Contains(rr.Body.String(), "lead_agent") {
		t.Errorf("expected response to contain lead_agent, got: %s", rr.Body.String())
	}
}

func TestLangGraphHandler_GetAssistant_MissingID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewLangGraphHandler(&config.AppConfig{}, nil)
	rr := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rr)
	c.Request = httptest.NewRequest(http.MethodGet, "/assistants/", nil)
	h.GetAssistant(c)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, rr.Code, rr.Body.String())
	}
}

func TestLangGraphHandler_GetThreadState(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewLangGraphHandler(&config.AppConfig{}, nil)
	router := gin.New()
	router.GET("/threads/:thread_id/state", h.GetThreadState)

	req := httptest.NewRequest(http.MethodGet, "/threads/test-thread/state", nil)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
	}

	// Verify response contains values.
	if !strings.Contains(rr.Body.String(), "values") {
		t.Errorf("expected response to contain values, got: %s", rr.Body.String())
	}
}

func TestLangGraphHandler_UpdateThreadState(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewLangGraphHandler(&config.AppConfig{}, nil)
	router := gin.New()
	router.PATCH("/threads/:thread_id/state", h.UpdateThreadState)

	body := `{"values": {"title": "Test Thread"}}`
	req := httptest.NewRequest(http.MethodPatch, "/threads/test-thread/state", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
	}

	if !strings.Contains(rr.Body.String(), "checkpoint_id") {
		t.Errorf("expected response to contain checkpoint_id, got: %s", rr.Body.String())
	}
}

func TestLangGraphHandler_ListAssistants(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewLangGraphHandler(&config.AppConfig{}, nil)
	router := gin.New()
	router.GET("/assistants", h.ListAssistants)

	req := httptest.NewRequest(http.MethodGet, "/assistants", nil)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
	}
}

func TestNewLangGraphHandlerWithAgents(t *testing.T) {
	cfg := &config.AppConfig{}
	agents := map[string]agent.LeadAgent{
		"test_agent": nil,
	}
	h := NewLangGraphHandlerWithAgents(cfg, nil, agents)
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
	if h.agents == nil {
		t.Fatal("expected non-nil agents map")
	}
}

func TestLangGraphHandler_GetAgent(t *testing.T) {
	cfg := &config.AppConfig{}
	h := NewLangGraphHandler(cfg, nil)

	// Test with empty name - should return default agent
	a := h.GetAgent("")
	if a != nil {
		t.Error("expected nil agent for empty name")
	}
}

func TestLangGraphHandler_GetAgent_ByName(t *testing.T) {
	mockAgent := &mockLGAgent{}
	agents := map[string]agent.LeadAgent{
		"custom_agent": mockAgent,
	}
	h := NewLangGraphHandlerWithAgents(nil, nil, agents)

	// Test with existing agent name
	a := h.GetAgent("custom_agent")
	if a == nil {
		t.Error("expected non-nil agent for custom_agent")
	}

	// Test with non-existing agent name - should return default
	a = h.GetAgent("nonexistent")
	if a != nil {
		t.Error("expected nil agent for nonexistent name")
	}
}

type mockLGAgent struct{}

func (m *mockLGAgent) Run(ctx context.Context, state *agent.ThreadState, cfg agent.RunConfig) (<-chan agent.Event, error) {
	ch := make(chan agent.Event)
	close(ch)
	return ch, nil
}

func (m *mockLGAgent) Resume(ctx context.Context, state *agent.ThreadState, cfg agent.RunConfig, checkpointID string) (<-chan agent.Event, error) {
	ch := make(chan agent.Event)
	close(ch)
	return ch, nil
}

func TestLangGraphHandler_CancelRun(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewLangGraphHandler(&config.AppConfig{}, nil)
	h.registerRun("run-1", lgRunHandle{
		ThreadID: "thread-1",
		RunID:    "run-1",
	})

	router := gin.New()
	router.POST("/threads/:thread_id/runs/:run_id/cancel", h.CancelRun)

	req := httptest.NewRequest(http.MethodPost, "/threads/thread-1/runs/run-1/cancel", nil)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
	}
}

func TestLangGraphHandler_CancelRun_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewLangGraphHandler(&config.AppConfig{}, nil)

	router := gin.New()
	router.POST("/threads/:thread_id/runs/:run_id/cancel", h.CancelRun)

	req := httptest.NewRequest(http.MethodPost, "/threads/thread-1/runs/nonexistent/cancel", nil)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d: %s", http.StatusNotFound, rr.Code, rr.Body.String())
	}
}

func TestLangGraphHandler_GetRun(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewLangGraphHandler(&config.AppConfig{}, nil)
	h.registerRun("run-2", lgRunHandle{
		ThreadID:     "thread-2",
		RunID:        "run-2",
		CheckpointID: "cp-2",
	})

	router := gin.New()
	router.GET("/threads/:thread_id/runs/:run_id", h.GetRun)

	req := httptest.NewRequest(http.MethodGet, "/threads/thread-2/runs/run-2", nil)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
	}
}

func TestLangGraphHandler_GetRun_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewLangGraphHandler(&config.AppConfig{}, nil)

	router := gin.New()
	router.GET("/threads/:thread_id/runs/:run_id", h.GetRun)

	req := httptest.NewRequest(http.MethodGet, "/threads/thread-2/runs/nonexistent", nil)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d: %s", http.StatusNotFound, rr.Code, rr.Body.String())
	}
}

func TestLangGraphHandler_ListRuns(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewLangGraphHandler(&config.AppConfig{}, nil)
	h.registerRun("run-3", lgRunHandle{
		ThreadID: "thread-3",
		RunID:    "run-3",
	})

	router := gin.New()
	router.GET("/threads/:thread_id/runs", h.ListRuns)

	req := httptest.NewRequest(http.MethodGet, "/threads/thread-3/runs", nil)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
	}
}

func TestLangGraphHandler_WaitForRun(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewLangGraphHandler(&config.AppConfig{}, nil)
	router := gin.New()
	router.GET("/threads/:thread_id/runs/:run_id/join", h.WaitForRun)

	req := httptest.NewRequest(http.MethodGet, "/threads/thread-1/runs/run-1/join", nil)
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, rr.Code, rr.Body.String())
	}
}

func TestLangGraphHandler_StreamRunStandalone(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockAgent := &mockLGAgent{}
	h := NewLangGraphHandler(nil, mockAgent)
	router := gin.New()
	router.POST("/runs/stream", h.StreamRunStandalone)

	body := `{"input": {"messages": []}}`
	req := httptest.NewRequest(http.MethodPost, "/runs/stream", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	router.ServeHTTP(rr, req)

	// Verify the endpoint was reached (may return SSE or error based on agent state)
	if rr.Code != http.StatusOK && rr.Code != http.StatusBadRequest {
		t.Errorf("expected status 200 or 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestParseLangGraphMessage_Human(t *testing.T) {
	m := map[string]any{
		"type":    "human",
		"content": "Hello world",
	}
	msg := parseLangGraphMessage(m)
	if msg == nil {
		t.Fatal("expected non-nil message")
	}
}

func TestParseLangGraphMessage_AI(t *testing.T) {
	m := map[string]any{
		"type":    "ai",
		"content": "Hi there",
		"name":    "assistant",
	}
	msg := parseLangGraphMessage(m)
	if msg == nil {
		t.Fatal("expected non-nil message")
	}
}

func TestParseLangGraphMessage_AIWithToolCalls(t *testing.T) {
	m := map[string]any{
		"type":    "ai",
		"content": "",
		"tool_calls": []any{
			map[string]any{
				"id":   "call-1",
				"name": "bash",
				"args": `{"command": "ls"}`,
			},
		},
	}
	msg := parseLangGraphMessage(m)
	if msg == nil {
		t.Fatal("expected non-nil message")
	}
}

func TestParseLangGraphMessage_Tool(t *testing.T) {
	m := map[string]any{
		"type":         "tool",
		"content":      "result",
		"tool_call_id": "call-1",
		"name":         "bash",
	}
	msg := parseLangGraphMessage(m)
	if msg == nil {
		t.Fatal("expected non-nil message")
	}
}

func TestParseLangGraphMessage_System(t *testing.T) {
	m := map[string]any{
		"type":    "system",
		"content": "You are helpful",
	}
	msg := parseLangGraphMessage(m)
	if msg == nil {
		t.Fatal("expected non-nil message")
	}
}

func TestParseLangGraphMessage_UnknownType(t *testing.T) {
	m := map[string]any{
		"type":    "unknown",
		"content": "test",
	}
	msg := parseLangGraphMessage(m)
	if msg == nil {
		t.Fatal("expected non-nil message")
	}
}

func TestLangGraphHandler_GetThreadState_MissingID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewLangGraphHandler(nil, nil)
	rr := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rr)
	c.Request = httptest.NewRequest(http.MethodGet, "/threads//state", nil)
	h.GetThreadState(c)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestLangGraphHandler_UpdateThreadState_MissingID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewLangGraphHandler(nil, nil)
	rr := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rr)
	c.Request = httptest.NewRequest(http.MethodPatch, "/threads//state", strings.NewReader(`{}`))
	c.Request.Header.Set("Content-Type", "application/json")
	h.UpdateThreadState(c)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestLangGraphHandler_UpdateThreadState_InvalidBody(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewLangGraphHandler(nil, nil)
	rr := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rr)
	c.Params = gin.Params{{Key: "thread_id", Value: "test-thread"}}
	c.Request = httptest.NewRequest(http.MethodPatch, "/threads/test-thread/state", strings.NewReader("invalid"))
	c.Request.Header.Set("Content-Type", "application/json")
	h.UpdateThreadState(c)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestLangGraphHandler_CreateThread_InvalidBody(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewLangGraphHandler(nil, nil)
	rr := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rr)
	c.Request = httptest.NewRequest(http.MethodPost, "/threads", strings.NewReader("invalid"))
	c.Request.Header.Set("Content-Type", "application/json")
	h.CreateThread(c)

	// Invalid body returns 400
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestLangGraphHandler_CancelRun_MissingParams(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewLangGraphHandler(nil, nil)
	rr := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rr)
	c.Request = httptest.NewRequest(http.MethodPost, "/threads//runs//cancel", nil)
	h.CancelRun(c)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestLangGraphHandler_GetRun_MissingParams(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewLangGraphHandler(nil, nil)
	rr := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rr)
	c.Request = httptest.NewRequest(http.MethodGet, "/threads//runs/", nil)
	h.GetRun(c)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestLangGraphHandler_ListRuns_MissingThreadID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewLangGraphHandler(nil, nil)
	rr := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rr)
	c.Request = httptest.NewRequest(http.MethodGet, "/threads//runs", nil)
	h.ListRuns(c)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestLangGraphHandler_WaitForRun_MissingParams(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewLangGraphHandler(nil, nil)
	rr := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rr)
	c.Request = httptest.NewRequest(http.MethodGet, "/threads//runs//join", nil)
	h.WaitForRun(c)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestLangGraphHandler_RunManagement(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := NewLangGraphHandler(nil, nil)

	// Test register/unregister/get run
	runID := "test-run-mgmt"
	h.registerRun(runID, lgRunHandle{
		ThreadID: "thread-1",
		RunID:    runID,
	})

	// Get run
	run, ok := h.getRun(runID)
	if !ok {
		t.Fatal("expected to find run")
	}
	if run.ThreadID != "thread-1" {
		t.Fatalf("expected thread-1, got %s", run.ThreadID)
	}

	// Unregister run
	h.unregisterRun(runID)
	_, ok = h.getRun(runID)
	if ok {
		t.Fatal("expected run to be unregistered")
	}
}
