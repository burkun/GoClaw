package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/bookerbai/goclaw/internal/agent"
	"github.com/bookerbai/goclaw/internal/config"
	"github.com/bookerbai/goclaw/internal/threadstore"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// collectSSEEvents reads all SSE frames from body until EOF and deserialises
// each "data: ..." line into an SSEEvent.
func collectSSEEvents(t *testing.T, body string) []SSEEvent {
	t.Helper()
	var events []SSEEvent
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		var ev SSEEvent
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			t.Fatalf("failed to parse SSE event JSON %q: %v", payload, err)
		}
		events = append(events, ev)
	}
	return events
}

// newRunRequest builds an httptest.Request for POST /api/threads/<id>/runs
// with the given JSON body.
func newRunRequest(t *testing.T, threadID, body string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/threads/"+threadID+"/runs",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

type mockLeadAgent struct {
	runErr    error
	resumeErr error
	events    []agent.Event
}

func (m *mockLeadAgent) Run(ctx context.Context, _ *agent.ThreadState, cfg agent.RunConfig) (<-chan agent.Event, error) {
	if m.runErr != nil {
		return nil, m.runErr
	}
	ch := make(chan agent.Event, len(m.events))
	go func() {
		defer close(ch)
		for _, ev := range m.events {
			select {
			case <-ctx.Done():
				ch <- agent.Event{Type: agent.EventError, ThreadID: cfg.ThreadID, Payload: agent.ErrorPayload{Code: agent.ErrorCodeContextCancelled, Message: ctx.Err().Error()}, Timestamp: time.Now().UnixMilli()}
				return
			case ch <- ev:
			}
		}
	}()
	return ch, nil
}

func (m *mockLeadAgent) Resume(ctx context.Context, _ *agent.ThreadState, cfg agent.RunConfig, checkpointID string) (<-chan agent.Event, error) {
	_ = checkpointID
	if m.resumeErr != nil {
		return nil, m.resumeErr
	}
	return m.Run(ctx, nil, cfg)
}

// ---------------------------------------------------------------------------
// Handler creation helper
// ---------------------------------------------------------------------------

// newTestHandler creates a ThreadsHandler with the given dependencies for testing.
func newTestHandler(cfg *config.AppConfig, agent agent.LeadAgent, store threadstore.Store) *ThreadsHandler {
	svc := NewThreadsService(cfg, agent, store)
	return NewThreadsHandlerWithService(svc)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestRunThread_SSETermination verifies that every call to RunThread produces
// a stream that ends with either a "completed" or "error" terminal event.
func TestRunThread_SSETermination(t *testing.T) {
	mock := &mockLeadAgent{events: []agent.Event{
		{Type: agent.EventMessageDelta, ThreadID: "thread-001", Payload: agent.MessageDeltaPayload{Content: "hello"}, Timestamp: time.Now().UnixMilli()},
		{Type: agent.EventCompleted, ThreadID: "thread-001", Payload: agent.CompletedPayload{FinalMessage: "done"}, Timestamp: time.Now().UnixMilli()},
	}}
	handler := newTestHandler(nil, mock, nil)

	req := newRunRequest(t, "thread-001", `{"input": "hello"}`)
	rr := httptest.NewRecorder()
	ginCtx, _ := newGinContext(rr, req, map[string]string{"thread_id": "thread-001"})
	handler.RunThread(ginCtx)

	events := collectSSEEvents(t, rr.Body.String())
	if len(events) == 0 {
		t.Fatal("expected at least one SSE event, got none")
	}
	last := events[len(events)-1]
	if last.Type != "completed" && last.Type != "error" {
		t.Errorf("last SSE event must be 'completed' or 'error', got %q", last.Type)
	}
}

// TestRunThread_errorEvent verifies that when the agent start fails the
// SSE stream contains an "error" event with a non-empty message payload.
func TestRunThread_errorEvent(t *testing.T) {
	handler := newTestHandler(nil, &mockLeadAgent{runErr: errors.New("boom")}, nil)

	req := newRunRequest(t, "thread-001", `{"input": "hello"}`)
	rr := httptest.NewRecorder()
	ginCtx, _ := newGinContext(rr, req, map[string]string{"thread_id": "thread-001"})
	handler.RunThread(ginCtx)

	events := collectSSEEvents(t, rr.Body.String())
	if len(events) == 0 {
		t.Fatal("expected at least one SSE event")
	}
	last := events[len(events)-1]
	if last.Type != "error" {
		t.Fatalf("expected terminal error event, got %q", last.Type)
	}
	payloadMap, ok := last.Payload.(map[string]interface{})
	if !ok {
		t.Fatalf("payload type mismatch: %T", last.Payload)
	}
	msg, _ := payloadMap["message"].(string)
	if strings.TrimSpace(msg) == "" {
		t.Fatal("expected non-empty error message")
	}
}

// ---------------------------------------------------------------------------
// Gin context helper
// ---------------------------------------------------------------------------

// newGinContext creates a *gin.Context backed by the given recorder and
// request, with URL params pre-populated from params.
func newGinContext(w *httptest.ResponseRecorder, r *http.Request, params map[string]string) (*gin.Context, bool) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(w)
	c.Request = r
	for k, v := range params {
		c.Params = append(c.Params, gin.Param{Key: k, Value: v})
	}
	return c, true
}

func TestCancelRun(t *testing.T) {
	h := newTestHandler(nil, nil, nil)

	cancelled := false
	h.svc.RegisterRun("run-1", "thread-1", "cp-1", func() {
		cancelled = true
	})

	req := httptest.NewRequest(http.MethodPost, "/api/threads/thread-1/runs/run-1/cancel", nil)
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"thread_id": "thread-1", "run_id": "run-1"})

	h.CancelRun(c)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rr.Code, rr.Body.String())
	}
	if !cancelled {
		t.Fatalf("expected cancel function to be called")
	}
	if _, ok := h.svc.GetRun("run-1"); !ok {
		// cancel does not auto remove, only signals cancellation.
		// keep this assertion to document current behavior.
		t.Fatalf("expected run to remain registered until run cleanup")
	}
}

func TestCancelRun_NotFound(t *testing.T) {
	h := newTestHandler(nil, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/threads/thread-1/runs/run-x/cancel", nil)
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"thread_id": "thread-1", "run_id": "run-x"})

	h.CancelRun(c)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

// compile-time guard: keep context imported for future fake-agent tests.
var _ = context.Background

// ---------------------------------------------------------------------------
// Thread CRUD / state / history tests
// ---------------------------------------------------------------------------

func TestCreateThread(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := threadstore.NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	h := newTestHandler(nil, nil, store)
	body := `{"thread_id": "thread-abc", "metadata": {"key": "value"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/threads", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, nil)
	h.CreateThread(c)
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d, body=%s", rr.Code, rr.Body.String())
	}
}

func TestGetThread(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := threadstore.NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	h := newTestHandler(nil, nil, store)

	// First create the thread
	body := `{"thread_id": "thread-xyz"}`
	createReq := httptest.NewRequest(http.MethodPost, "/api/threads", strings.NewReader(body))
	createReq.Header.Set("Content-Type", "application/json")
	createRR := httptest.NewRecorder()
	createCtx, _ := newGinContext(createRR, createReq, nil)
	h.CreateThread(createCtx)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("failed to create thread: expected 201, got %d, body=%s", createRR.Code, createRR.Body.String())
	}

	// Now get the thread
	req := httptest.NewRequest(http.MethodGet, "/api/threads/thread-xyz", nil)
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"thread_id": "thread-xyz"})
	h.GetThread(c)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestDeleteThread(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := threadstore.NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	h := newTestHandler(nil, nil, store)

	// First create the thread with a unique ID
	body := `{"thread_id": "thread-to-delete"}`
	createReq := httptest.NewRequest(http.MethodPost, "/api/threads", strings.NewReader(body))
	createReq.Header.Set("Content-Type", "application/json")
	createRR := httptest.NewRecorder()
	createCtx, _ := newGinContext(createRR, createReq, nil)
	h.CreateThread(createCtx)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("failed to create thread: expected 201, got %d, body=%s", createRR.Code, createRR.Body.String())
	}

	// Now delete the thread
	req := httptest.NewRequest(http.MethodDelete, "/api/threads/thread-to-delete", nil)
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"thread_id": "thread-to-delete"})
	h.DeleteThread(c)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestGetThreadState(t *testing.T) {
	h := newTestHandler(nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/threads/thread-xyz/state", nil)
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"thread_id": "thread-xyz"})
	h.GetThreadState(c)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestGetThreadHistory(t *testing.T) {
	h := newTestHandler(nil, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/threads/thread-xyz/history", nil)
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"thread_id": "thread-xyz"})
	h.GetThreadHistory(c)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestListRuns(t *testing.T) {
	h := newTestHandler(nil, nil, nil)
	h.svc.RegisterRun("run-1", "thread-xyz", "cp-1", func() {})
	req := httptest.NewRequest(http.MethodGet, "/api/threads/thread-xyz/runs", nil)
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"thread_id": "thread-xyz"})
	h.ListRuns(c)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "run-1") {
		t.Fatalf("expected run-1 in response, got %s", rr.Body.String())
	}
}

func TestGetRun(t *testing.T) {
	h := newTestHandler(nil, nil, nil)
	h.svc.RegisterRun("run-2", "thread-xyz", "cp-2", func() {})
	req := httptest.NewRequest(http.MethodGet, "/api/threads/thread-xyz/runs/run-2", nil)
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"thread_id": "thread-xyz", "run_id": "run-2"})
	h.GetRun(c)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestGetRun_NotFound(t *testing.T) {
	h := newTestHandler(nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/threads/thread-xyz/runs/run-missing", nil)
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"thread_id": "thread-xyz", "run_id": "run-missing"})
	h.GetRun(c)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestRunThread_ContentLocationHeader(t *testing.T) {
	mock := &mockLeadAgent{events: []agent.Event{
		{Type: agent.EventCompleted, ThreadID: "thread-loc", Payload: agent.CompletedPayload{FinalMessage: "done"}, Timestamp: time.Now().UnixMilli()},
	}}
	h := newTestHandler(nil, mock, nil)
	req := newRunRequest(t, "thread-loc", `{"input": "hi"}`)
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"thread_id": "thread-loc"})
	h.RunThread(c)
	loc := rr.Header().Get("Content-Location")
	if !strings.Contains(loc, "/api/threads/thread-loc/runs/") {
		t.Fatalf("expected Content-Location header, got %q", loc)
	}
}

func TestRunThread_MissingThreadID(t *testing.T) {
	h := newTestHandler(nil, nil, nil)
	req := newRunRequest(t, "", `{"input": "hi"}`)
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"thread_id": ""})
	h.RunThread(c)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestRunThread_InvalidJSON(t *testing.T) {
	h := newTestHandler(nil, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/threads/thread-1/runs", strings.NewReader("invalid json"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"thread_id": "thread-1"})
	h.RunThread(c)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestRunThread_NilAgent(t *testing.T) {
	h := newTestHandler(nil, nil, nil)
	req := newRunRequest(t, "thread-1", `{"input": "hi"}`)
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"thread_id": "thread-1"})
	h.RunThread(c)
	events := collectSSEEvents(t, rr.Body.String())
	if len(events) == 0 {
		t.Fatal("expected SSE event")
	}
	if events[0].Type != "error" {
		t.Fatalf("expected error event, got %s", events[0].Type)
	}
}

func TestRunThread_ResumeWithCheckpoint(t *testing.T) {
	mock := &mockLeadAgent{events: []agent.Event{
		{Type: agent.EventCompleted, ThreadID: "thread-resume", Payload: agent.CompletedPayload{FinalMessage: "resumed"}, Timestamp: time.Now().UnixMilli()},
	}}
	h := newTestHandler(nil, mock, nil)
	req := newRunRequest(t, "thread-resume", `{"input": "hi", "checkpoint_id": "cp-123"}`)
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"thread_id": "thread-resume"})
	h.RunThread(c)
	events := collectSSEEvents(t, rr.Body.String())
	if len(events) == 0 {
		t.Fatal("expected SSE event")
	}
	if events[len(events)-1].Type != "completed" {
		t.Fatalf("expected completed event, got %s", events[len(events)-1].Type)
	}
}

func TestRunThread_SSEHeaders(t *testing.T) {
	mock := &mockLeadAgent{events: []agent.Event{
		{Type: agent.EventCompleted, ThreadID: "thread-hdr", Payload: agent.CompletedPayload{FinalMessage: "done"}, Timestamp: time.Now().UnixMilli()},
	}}
	h := newTestHandler(nil, mock, nil)
	req := newRunRequest(t, "thread-hdr", `{"input": "hi"}`)
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"thread_id": "thread-hdr"})
	h.RunThread(c)
	if ct := rr.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("expected Content-Type text/event-stream, got %s", ct)
	}
	if cc := rr.Header().Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("expected Cache-Control no-cache, got %s", cc)
	}
	if conn := rr.Header().Get("Connection"); conn != "keep-alive" {
		t.Fatalf("expected Connection keep-alive, got %s", conn)
	}
}

func TestRunThread_LastEventIDHeader(t *testing.T) {
	mock := &mockLeadAgent{events: []agent.Event{
		{Type: agent.EventCompleted, ThreadID: "thread-last", Payload: agent.CompletedPayload{FinalMessage: "done"}, Timestamp: time.Now().UnixMilli()},
	}}
	h := newTestHandler(nil, mock, nil)
	req := newRunRequest(t, "thread-last", `{"input": "hi"}`)
	req.Header.Set("Last-Event-ID", "event-123")
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"thread_id": "thread-last"})
	h.RunThread(c)
	if resume := rr.Header().Get("X-Resume-From-Event"); resume != "event-123" {
		t.Fatalf("expected X-Resume-From-Event header, got %s", resume)
	}
}

func TestSearchThreads(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := threadstore.NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	h := newTestHandler(nil, nil, store)

	// Create test threads
	for i := 1; i <= 3; i++ {
		body := fmt.Sprintf(`{"thread_id": "thread-%d", "metadata": {"key": "value%d"}}`, i, i)
		req := httptest.NewRequest(http.MethodPost, "/api/threads", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		c, _ := newGinContext(rr, req, nil)
		h.CreateThread(c)
		if rr.Code != http.StatusCreated {
			t.Fatalf("failed to create thread: %d", rr.Code)
		}
	}

	// Search with empty query
	searchBody := `{}`
	req := httptest.NewRequest(http.MethodPost, "/api/threads/search", strings.NewReader(searchBody))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, nil)
	h.SearchThreads(c)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	threads, ok := resp["threads"].([]interface{})
	if !ok {
		t.Fatal("expected threads array")
	}
	if len(threads) != 3 {
		t.Fatalf("expected 3 threads, got %d", len(threads))
	}
}

func TestPatchThread(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := threadstore.NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	h := newTestHandler(nil, nil, store)

	// Create thread first
	body := `{"thread_id": "thread-patch", "metadata": {"key": "value"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/threads", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, nil)
	h.CreateThread(c)
	if rr.Code != http.StatusCreated {
		t.Fatalf("failed to create thread: %d", rr.Code)
	}

	// Patch thread
	patchBody := `{"metadata": {"new_key": "new_value"}}`
	req = httptest.NewRequest(http.MethodPatch, "/api/threads/thread-patch", strings.NewReader(patchBody))
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	c, _ = newGinContext(rr, req, map[string]string{"thread_id": "thread-patch"})
	h.PatchThread(c)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rr.Code, rr.Body.String())
	}
}

func TestPatchThread_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := threadstore.NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	h := newTestHandler(nil, nil, store)

	patchBody := `{"metadata": {"key": "value"}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/threads/nonexistent", strings.NewReader(patchBody))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"thread_id": "nonexistent"})
	h.PatchThread(c)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestPatchThread_MissingThreadID(t *testing.T) {
	h := newTestHandler(nil, nil, nil)
	patchBody := `{"metadata": {"key": "value"}}`
	req := httptest.NewRequest(http.MethodPatch, "/api/threads/", strings.NewReader(patchBody))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"thread_id": ""})
	h.PatchThread(c)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestUpdateThreadState(t *testing.T) {
	h := newTestHandler(nil, nil, nil)
	body := `{"values": {"key": "value"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/threads/thread-1/state", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"thread_id": "thread-1"})
	h.UpdateThreadState(c)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp ThreadStateResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp.ChannelValues["key"] != "value" {
		t.Fatalf("expected key=value, got %v", resp.ChannelValues)
	}
}

func TestUpdateThreadState_MissingThreadID(t *testing.T) {
	h := newTestHandler(nil, nil, nil)
	body := `{"values": {"key": "value"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/threads//state", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"thread_id": ""})
	h.UpdateThreadState(c)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestGetThread_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := threadstore.NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	h := newTestHandler(nil, nil, store)
	req := httptest.NewRequest(http.MethodGet, "/api/threads/nonexistent", nil)
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"thread_id": "nonexistent"})
	h.GetThread(c)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestCreateThread_AutoID(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := threadstore.NewFileStore(tmpDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	h := newTestHandler(nil, nil, store)
	body := `{}` // No thread_id provided
	req := httptest.NewRequest(http.MethodPost, "/api/threads", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, nil)
	h.CreateThread(c)
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d, body=%s", rr.Code, rr.Body.String())
	}
	var resp ThreadMetadata
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp.ThreadID == "" {
		t.Fatal("expected auto-generated thread_id")
	}
}

func TestCancelRun_MissingThreadID(t *testing.T) {
	h := newTestHandler(nil, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/threads//runs/run-1/cancel", nil)
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"thread_id": "", "run_id": "run-1"})
	h.CancelRun(c)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestCancelRun_ThreadIDMismatch(t *testing.T) {
	h := newTestHandler(nil, nil, nil)
	h.svc.RegisterRun("run-1", "thread-1", "cp-1", func() {})
	req := httptest.NewRequest(http.MethodPost, "/api/threads/thread-2/runs/run-1/cancel", nil)
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"thread_id": "thread-2", "run_id": "run-1"})
	h.CancelRun(c)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestNewThreadsHandler(t *testing.T) {
	h := NewThreadsHandler(nil, nil, nil)
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
}

func TestDeleteThread_Error(t *testing.T) {
	h := newTestHandler(nil, nil, nil)
	req := httptest.NewRequest(http.MethodDelete, "/api/threads/nonexistent", nil)
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"thread_id": "nonexistent"})
	h.DeleteThread(c)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestGetThreadState_MissingThreadID(t *testing.T) {
	h := newTestHandler(nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/threads//state", nil)
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"thread_id": ""})
	h.GetThreadState(c)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestGetThreadHistory_MissingThreadID(t *testing.T) {
	h := newTestHandler(nil, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/threads//history", nil)
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"thread_id": ""})
	h.GetThreadHistory(c)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestListRuns_MissingThreadID(t *testing.T) {
	h := newTestHandler(nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/threads//runs", nil)
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"thread_id": ""})
	h.ListRuns(c)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestSearchThreads_Error(t *testing.T) {
	h := newTestHandler(nil, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/threads/search", strings.NewReader("invalid"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, nil)
	h.SearchThreads(c)
	// Should still return OK with empty result
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}
