package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/bookerbai/goclaw/internal/agent"
	"github.com/bookerbai/goclaw/internal/config"
)

// ThreadsHandler serves /api/threads/:thread_id/* endpoints.
type ThreadsHandler struct {
	cfg   *config.AppConfig
	agent agent.LeadAgent

	runsMu sync.RWMutex
	runs   map[string]runHandle
}

type runHandle struct {
	ThreadID     string
	CheckpointID string
	Cancel       context.CancelFunc
}

// NewThreadsHandler creates a handler wired to the given agent.
func NewThreadsHandler(cfg *config.AppConfig, a agent.LeadAgent) *ThreadsHandler {
	return &ThreadsHandler{cfg: cfg, agent: a, runs: make(map[string]runHandle)}
}

// ---------------------------------------------------------------------------
// Request / response types
// ---------------------------------------------------------------------------

// RunThreadRequest is the JSON body accepted by POST /api/threads/:thread_id/runs.
type RunThreadRequest struct {
	// Input is the user message or structured input for the agent.
	Input any `json:"input"`
	// Config holds optional run-level overrides (model, tools, etc.).
	Config map[string]any `json:"config,omitempty"`
	// Metadata is passed through to the agent's ThreadState for custom use.
	Metadata map[string]any `json:"metadata,omitempty"`
	// CheckpointID resumes from an existing checkpoint when provided.
	CheckpointID string `json:"checkpoint_id,omitempty"`
}

// SSEEvent is the unified envelope for all Server-Sent Events.
// Every event sent over the stream uses this structure.
type SSEEvent struct {
	// Type classifies the event: "message_delta", "tool_event", "completed", or "error".
	Type string `json:"type"`
	// ThreadID echoes the thread identifier from the request path.
	ThreadID string `json:"thread_id"`
	// RunID identifies this run instance.
	RunID string `json:"run_id,omitempty"`
	// CheckpointID is used for interruption persistence and resume.
	CheckpointID string `json:"checkpoint_id,omitempty"`
	// Payload holds event-specific data (delta text, tool args, error details, etc.).
	Payload any `json:"payload"`
	// Timestamp is the UTC Unix millisecond at which the event was generated.
	Timestamp int64 `json:"timestamp"`
}

// ---------------------------------------------------------------------------
// SSE helpers
// ---------------------------------------------------------------------------

// writeSSE serialises event as an SSE data-only frame and flushes it.
// Format:
//
//	data: <json>\n\n
//
// The caller must have already set the response headers for text/event-stream.
func writeSSE(w io.Writer, event SSEEvent) error {
	b, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("writeSSE marshal: %w", err)
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
		return fmt.Errorf("writeSSE write: %w", err)
	}
	return nil
}

// sseNow returns the current UTC time as Unix milliseconds.
func sseNow() int64 {
	return time.Now().UnixMilli()
}

// ---------------------------------------------------------------------------
// Handler
// ---------------------------------------------------------------------------

// RunThread handles POST /api/threads/:thread_id/runs.
//
// It streams the agent's response as SSE events.  Regardless of success or
// failure the stream is always terminated with either a "completed" or an
// "error" event — this is the termination guarantee required by the P0 contract.
//
// SSE headers set:
//
//	Content-Type:  text/event-stream
//	Cache-Control: no-cache
//	X-Accel-Buffering: no   (disables nginx proxy buffering)
//	Connection: keep-alive
func (h *ThreadsHandler) RunThread(c *gin.Context) {
	threadID := c.Param("thread_id")
	if threadID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "thread_id is required"})
		return
	}

	var req RunThreadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	runID := uuid.NewString()
	checkpointID := strings.TrimSpace(req.CheckpointID)
	if checkpointID == "" {
		checkpointID = runID
	}

	// Set SSE response headers before writing any body bytes.
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no") // prevents nginx from buffering the stream

	w := c.Writer

	// Termination guarantee: ensure every code path writes a completed/error event.
	var terminalEventWritten bool
	defer func() {
		if !terminalEventWritten {
			// This path is reached on unexpected panics after recovery; emit an error
			// event so the client knows the stream ended abnormally.
			_ = writeSSE(w, SSEEvent{
				Type:         "error",
				ThreadID:     threadID,
				RunID:        runID,
				CheckpointID: checkpointID,
				Payload:      map[string]string{"message": "internal server error"},
				Timestamp:    sseNow(),
			})
			w.Flush()
		}
	}()

	// Prepare agent ThreadState from request input.
	state := &agent.ThreadState{
		Messages: make([]*schema.Message, 0),
	}

	// Prepare run configuration.
	modelName := "gpt-4"
	if h.cfg != nil && h.cfg.DefaultModel() != nil {
		modelName = h.cfg.DefaultModel().Name
	}
	cfg := agent.RunConfig{
		ThreadID:        threadID,
		ModelName:       modelName,
		SubagentEnabled: true,
		CheckpointID:    checkpointID,
		AgentName:       "lead_agent",
	}

	// Check agent is initialized.
	if h.agent == nil {
		_ = writeSSE(w, SSEEvent{
			Type:         "error",
			ThreadID:     threadID,
			RunID:        runID,
			CheckpointID: checkpointID,
			Payload:      map[string]string{"message": agent.ErrorCodeNotInitialized},
			Timestamp:    sseNow(),
		})
		w.Flush()
		terminalEventWritten = true
		return
	}

	runCtx, cancel := context.WithCancel(c.Request.Context())
	h.registerRun(runID, runHandle{ThreadID: threadID, CheckpointID: checkpointID, Cancel: cancel})
	defer h.unregisterRun(runID)

	// Run or resume the agent and stream events.
	var (
		eventChan <-chan agent.Event
		err       error
	)
	if strings.TrimSpace(req.CheckpointID) != "" {
		eventChan, err = h.agent.Resume(runCtx, state, cfg, checkpointID)
	} else {
		eventChan, err = h.agent.Run(runCtx, state, cfg)
	}
	if err != nil {
		_ = writeSSE(w, SSEEvent{
			Type:         "error",
			ThreadID:     threadID,
			RunID:        runID,
			CheckpointID: checkpointID,
			Payload:      map[string]string{"message": fmt.Sprintf("failed to start agent: %v", err)},
			Timestamp:    sseNow(),
		})
		w.Flush()
		terminalEventWritten = true
		return
	}

	// Consume agent events and stream them to client.
	for ev := range eventChan {
		sseEv := SSEEvent{
			Type:         string(ev.Type),
			ThreadID:     ev.ThreadID,
			RunID:        runID,
			CheckpointID: checkpointID,
			Payload:      ev.Payload,
			Timestamp:    ev.Timestamp,
		}

		if err := writeSSE(w, sseEv); err != nil {
			// Write error; stream is already broken, just return.
			return
		}
		w.Flush()

		// Mark stream termination on completed/error event.
		if ev.Type == agent.EventCompleted || ev.Type == agent.EventError {
			terminalEventWritten = true
		}
	}

	// Defensive: if loop exited without terminal event, emit error.
	if !terminalEventWritten {
		_ = writeSSE(w, SSEEvent{
			Type:         "error",
			ThreadID:     threadID,
			RunID:        runID,
			CheckpointID: checkpointID,
			Payload:      map[string]string{"message": "agent event stream closed without terminal event"},
			Timestamp:    sseNow(),
		})
		w.Flush()
		terminalEventWritten = true
	}
}

// CancelRun handles POST /api/threads/:thread_id/runs/:run_id/cancel.
func (h *ThreadsHandler) CancelRun(c *gin.Context) {
	threadID := c.Param("thread_id")
	runID := c.Param("run_id")
	if threadID == "" || runID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "thread_id and run_id are required"})
		return
	}

	run, ok := h.getRun(runID)
	if !ok || run.ThreadID != threadID {
		c.JSON(http.StatusNotFound, gin.H{"error": "run not found"})
		return
	}
	if run.Cancel != nil {
		run.Cancel()
	}

	c.JSON(http.StatusOK, gin.H{
		"thread_id":     threadID,
		"run_id":        runID,
		"checkpoint_id": run.CheckpointID,
		"status":        "cancelling",
	})
}

func (h *ThreadsHandler) registerRun(runID string, run runHandle) {
	h.runsMu.Lock()
	defer h.runsMu.Unlock()
	h.runs[runID] = run
}

func (h *ThreadsHandler) unregisterRun(runID string) {
	h.runsMu.Lock()
	defer h.runsMu.Unlock()
	delete(h.runs, runID)
}

func (h *ThreadsHandler) getRun(runID string) (runHandle, bool) {
	h.runsMu.RLock()
	defer h.runsMu.RUnlock()
	r, ok := h.runs[runID]
	return r, ok
}
