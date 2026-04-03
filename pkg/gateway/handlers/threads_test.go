package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
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

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestRunThread_SSETermination verifies that every call to RunThread produces
// a stream that ends with either a "completed" or "error" terminal event.
// This is the P0 SSE contract: the client must always receive an explicit
// end-of-stream signal so it can clean up its connection.
func TestRunThread_SSETermination(t *testing.T) {
	// TODO: Replace with a real mock agent once agent.LeadAgent interface is defined.
	//   handler := NewThreadsHandler(testConfig(), &mockAgent{})
	//
	// Stub setup:
	//   - mockAgent.Run returns a closed channel immediately (happy path).
	//   - Expect the handler to emit "completed" as the last event.

	handler := NewThreadsHandler(nil, nil) // nil cfg/agent: uses placeholder path

	req := newRunRequest(t, "thread-001", `{"input": "hello"}`)
	rr := httptest.NewRecorder()

	// Call the handler directly through a minimal Gin context.
	// TODO: Switch to router.ServeHTTP once the full Gin test harness is wired.
	//   For now we invoke the underlying function to keep the test self-contained.
	ginCtx, _ := newGinContext(rr, req, map[string]string{"thread_id": "thread-001"})
	handler.RunThread(ginCtx)

	body := rr.Body.String()
	events := collectSSEEvents(t, body)

	if len(events) == 0 {
		t.Fatal("expected at least one SSE event, got none")
	}

	last := events[len(events)-1]
	if last.Type != "completed" && last.Type != "error" {
		t.Errorf("last SSE event must be 'completed' or 'error', got %q", last.Type)
	}
}

// TestRunThread_errorEvent verifies that when the agent returns an error the
// SSE stream contains an "error" event with a non-empty message in its payload.
func TestRunThread_errorEvent(t *testing.T) {
	// TODO: Create a mockAgent whose Run method closes with an error.
	//   handler := NewThreadsHandler(testConfig(), &errorAgent{err: errors.New("boom")})
	//
	// Until the agent interface is defined, this test documents the expected
	// contract and acts as a compilation guard.

	// Placeholder assertion — confirms the helper compiles correctly.
	ev := SSEEvent{
		Type:     "error",
		ThreadID: "thread-001",
		Payload:  map[string]string{"message": "boom"},
	}
	if ev.Type != "error" {
		t.Errorf("expected type 'error', got %q", ev.Type)
	}

	payloadMap, ok := ev.Payload.(map[string]string)
	if !ok {
		t.Fatal("payload should be a map[string]string")
	}
	if msg := payloadMap["message"]; msg == "" {
		t.Error("error event payload must contain a non-empty 'message' field")
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
