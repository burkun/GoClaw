package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/bookerbai/goclaw/internal/agent"
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
// Tests
// ---------------------------------------------------------------------------

// TestRunThread_SSETermination verifies that every call to RunThread produces
// a stream that ends with either a "completed" or "error" terminal event.
func TestRunThread_SSETermination(t *testing.T) {
	mock := &mockLeadAgent{events: []agent.Event{
		{Type: agent.EventMessageDelta, ThreadID: "thread-001", Payload: agent.MessageDeltaPayload{Content: "hello"}, Timestamp: time.Now().UnixMilli()},
		{Type: agent.EventCompleted, ThreadID: "thread-001", Payload: agent.CompletedPayload{FinalMessage: "done"}, Timestamp: time.Now().UnixMilli()},
	}}
	handler := NewThreadsHandler(nil, mock, nil)

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
	handler := NewThreadsHandler(nil, &mockLeadAgent{runErr: errors.New("boom")}, nil)

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
	h := NewThreadsHandler(nil, nil, nil)

	cancelled := false
	h.registerRun("run-1", runHandle{
		ThreadID:     "thread-1",
		CheckpointID: "cp-1",
		Cancel: func() {
			cancelled = true
		},
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
	if _, ok := h.getRun("run-1"); !ok {
		// cancel does not auto remove, only signals cancellation.
		// keep this assertion to document current behavior.
		t.Fatalf("expected run to remain registered until run cleanup")
	}
}

func TestCancelRun_NotFound(t *testing.T) {
	h := NewThreadsHandler(nil, nil, nil)
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
	h := NewThreadsHandler(nil, nil, store)
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
	h := NewThreadsHandler(nil, nil, store)

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
	h := NewThreadsHandler(nil, nil, store)

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
	h := NewThreadsHandler(nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/threads/thread-xyz/state", nil)
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"thread_id": "thread-xyz"})
	h.GetThreadState(c)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestGetThreadHistory(t *testing.T) {
	h := NewThreadsHandler(nil, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/threads/thread-xyz/history", nil)
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"thread_id": "thread-xyz"})
	h.GetThreadHistory(c)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestListRuns(t *testing.T) {
	h := NewThreadsHandler(nil, nil, nil)
	h.registerRun("run-1", runHandle{ThreadID: "thread-xyz"})
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
	h := NewThreadsHandler(nil, nil, nil)
	h.registerRun("run-2", runHandle{ThreadID: "thread-xyz"})
	req := httptest.NewRequest(http.MethodGet, "/api/threads/thread-xyz/runs/run-2", nil)
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"thread_id": "thread-xyz", "run_id": "run-2"})
	h.GetRun(c)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestGetRun_NotFound(t *testing.T) {
	h := NewThreadsHandler(nil, nil, nil)
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
	h := NewThreadsHandler(nil, mock, nil)
	req := newRunRequest(t, "thread-loc", `{"input": "hi"}`)
	rr := httptest.NewRecorder()
	c, _ := newGinContext(rr, req, map[string]string{"thread_id": "thread-loc"})
	h.RunThread(c)
	loc := rr.Header().Get("Content-Location")
	if !strings.Contains(loc, "/api/threads/thread-loc/runs/") {
		t.Fatalf("expected Content-Location header, got %q", loc)
	}
}
