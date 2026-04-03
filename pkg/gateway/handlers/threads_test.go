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
	handler := NewThreadsHandler(nil, mock)

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
	handler := NewThreadsHandler(nil, &mockLeadAgent{runErr: errors.New("boom")})

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
	h := NewThreadsHandler(nil, nil)

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
	h := NewThreadsHandler(nil, nil)
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
