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
	"github.com/bookerbai/goclaw/internal/threadstore"
)

// ThreadsHandler serves /api/threads/:thread_id/* endpoints.
type ThreadsHandler struct {
	cfg   *config.AppConfig
	agent agent.LeadAgent
	store threadstore.Store

	runsMu sync.RWMutex
	runs   map[string]runHandle
}

type runHandle struct {
	ThreadID     string
	CheckpointID string
	Cancel       context.CancelFunc
}

// NewThreadsHandler creates a handler wired to the given agent.
func NewThreadsHandler(cfg *config.AppConfig, a agent.LeadAgent, store threadstore.Store) *ThreadsHandler {
	if store == nil {
		// Create default file store
		var err error
		store, err = threadstore.NewFileStore("")
		if err != nil {
			panic(fmt.Sprintf("failed to create thread store: %v", err))
		}
	}
	return &ThreadsHandler{cfg: cfg, agent: a, store: store, runs: make(map[string]runHandle)}
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
	// LastEventID is the last event ID received by the client for SSE resume.
	// If provided, the server will attempt to resume from this event.
	LastEventID string `json:"last_event_id,omitempty"`
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
	// EventID is a unique identifier for this event, used for SSE resume.
	EventID string `json:"event_id,omitempty"`
	// Payload holds event-specific data (delta text, tool args, error details, etc.).
	Payload any `json:"payload"`
	// Timestamp is the UTC Unix millisecond at which the event was generated.
	Timestamp int64 `json:"timestamp"`
}

// ---------------------------------------------------------------------------
// SSE helpers
// ---------------------------------------------------------------------------

// writeSSE serialises event as an SSE frame with optional event ID and flushes it.
// Format:
//
//	id: <event_id>\n (if EventID is set)
//	data: <json>\n\n
//
// The caller must have already set the response headers for text/event-stream.
func writeSSE(w io.Writer, event SSEEvent) error {
	// Write event ID if present (for SSE resume support)
	if event.EventID != "" {
		if _, err := fmt.Fprintf(w, "id: %s\n", event.EventID); err != nil {
			return fmt.Errorf("writeSSE id: %w", err)
		}
	}
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
	// Content-Location header for SDK run metadata extraction.
	c.Header("Content-Location", fmt.Sprintf("/api/threads/%s/runs/%s/stream?thread_id=%s&run_id=%s", threadID, runID, threadID, runID))

	// Check for Last-Event-ID header for SSE resume (RFC 8895)
	lastEventID := c.GetHeader("Last-Event-ID")
	if lastEventID != "" {
		// Log resume attempt for debugging
		// In a full implementation, we would use this to resume from the specific event
		// For now, we just acknowledge it and continue from checkpoint_id
		c.Header("X-Resume-From-Event", lastEventID)
	}

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
		RunID:           runID,
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
	eventCounter := 0
	for ev := range eventChan {
		eventCounter++
		// Generate unique event ID for SSE resume support
		eventID := fmt.Sprintf("%s-%d", runID, eventCounter)
		sseEv := SSEEvent{
			Type:         string(ev.Type),
			ThreadID:     ev.ThreadID,
			RunID:        runID,
			CheckpointID: checkpointID,
			EventID:      eventID,
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

// ---------------------------------------------------------------------------
// Thread CRUD, State, History, and Runs list endpoints
// ---------------------------------------------------------------------------

// ThreadMetadata contains basic thread information.
type ThreadMetadata struct {
	ThreadID  string         `json:"thread_id"`
	Title     string         `json:"title,omitempty"`
	Status    string         `json:"status"`
	CreatedAt int64          `json:"created_at"`
	UpdatedAt int64          `json:"updated_at"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	Values    map[string]any `json:"values,omitempty"`
}

// ThreadStateResponse contains the current thread state from checkpoint.
type ThreadStateResponse struct {
	ChannelValues map[string]any `json:"channel_values"`
	CheckpointID  string         `json:"checkpoint_id,omitempty"`
	Next          []string       `json:"next,omitempty"`
	Tasks         []any          `json:"tasks,omitempty"`
}

// HistoryEntry represents a single checkpoint history entry.
type HistoryEntry struct {
	CheckpointID string `json:"checkpoint_id"`
	Timestamp    int64  `json:"timestamp"`
	ParentID     string `json:"parent_id,omitempty"`
}

// RunEntry represents a run record for list runs.
type RunEntry struct {
	RunID     string `json:"run_id"`
	ThreadID  string `json:"thread_id"`
	Status    string `json:"status"`
	CreatedAt int64  `json:"created_at"`
}

// CreateThread handles POST /api/threads.
func (h *ThreadsHandler) CreateThread(c *gin.Context) {
	var req struct {
		ThreadID string         `json:"thread_id"`
		Metadata map[string]any `json:"metadata,omitempty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		NewValidationError("invalid request body").Render(c, http.StatusBadRequest)
		return
	}
	threadID := strings.TrimSpace(req.ThreadID)
	if threadID == "" {
		threadID = uuid.NewString()
	}

	meta := &threadstore.ThreadMetadata{
		ThreadID: threadID,
		Status:   "idle",
		Metadata: req.Metadata,
	}

	if err := h.store.Create(meta); err != nil {
		NewAPIError("create_thread_failed", err.Error()).Render(c, http.StatusInternalServerError)
		return
	}

	c.JSON(http.StatusCreated, ThreadMetadata{
		ThreadID:  meta.ThreadID,
		Title:     meta.Title,
		Status:    meta.Status,
		CreatedAt: meta.CreatedAt,
		UpdatedAt: meta.UpdatedAt,
		Metadata:  meta.Metadata,
	})
}

// SearchThreads handles POST /api/threads/search.
func (h *ThreadsHandler) SearchThreads(c *gin.Context) {
	var query threadstore.SearchQuery
	if err := c.ShouldBindJSON(&query); err != nil {
		// Allow empty body
		query = threadstore.SearchQuery{}
	}

	results, total, err := h.store.Search(query)
	if err != nil {
		NewAPIError("search_failed", err.Error()).Render(c, http.StatusInternalServerError)
		return
	}

	// Convert to response format
	threads := make([]ThreadMetadata, len(results))
	for i, t := range results {
		threads[i] = ThreadMetadata{
			ThreadID:  t.ThreadID,
			Title:     t.Title,
			Status:    t.Status,
			CreatedAt: t.CreatedAt,
			UpdatedAt: t.UpdatedAt,
			Metadata:  t.Metadata,
		}
	}

	c.JSON(http.StatusOK, gin.H{"threads": threads, "total": total})
}

// GetThread handles GET /api/threads/:thread_id.
func (h *ThreadsHandler) GetThread(c *gin.Context) {
	threadID := c.Param("thread_id")
	if threadID == "" {
		NewValidationError("thread_id is required").Render(c, http.StatusBadRequest)
		return
	}

	meta, err := h.store.Get(threadID)
	if err != nil {
		NewNotFoundError("thread not found").Render(c, http.StatusNotFound)
		return
	}

	c.JSON(http.StatusOK, ThreadMetadata{
		ThreadID:  meta.ThreadID,
		Title:     meta.Title,
		Status:    meta.Status,
		CreatedAt: meta.CreatedAt,
		UpdatedAt: meta.UpdatedAt,
		Metadata:  meta.Metadata,
	})
}

// PatchThread handles PATCH /api/threads/:thread_id.
func (h *ThreadsHandler) PatchThread(c *gin.Context) {
	threadID := c.Param("thread_id")
	if threadID == "" {
		NewValidationError("thread_id is required").Render(c, http.StatusBadRequest)
		return
	}

	var req struct {
		Metadata map[string]any `json:"metadata,omitempty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		NewValidationError("invalid request body").Render(c, http.StatusBadRequest)
		return
	}

	// Get existing metadata
	existing, err := h.store.Get(threadID)
	if err != nil {
		NewNotFoundError("thread not found").Render(c, http.StatusNotFound)
		return
	}

	// Merge metadata
	if req.Metadata != nil {
		if existing.Metadata == nil {
			existing.Metadata = make(map[string]any)
		}
		for k, v := range req.Metadata {
			existing.Metadata[k] = v
		}
	}

	if err := h.store.Update(threadID, existing); err != nil {
		NewAPIError("update_failed", err.Error()).Render(c, http.StatusInternalServerError)
		return
	}

	c.JSON(http.StatusOK, ThreadMetadata{
		ThreadID:  existing.ThreadID,
		Title:     existing.Title,
		Status:    existing.Status,
		CreatedAt: existing.CreatedAt,
		UpdatedAt: existing.UpdatedAt,
		Metadata:  existing.Metadata,
	})
}

// DeleteThread handles DELETE /api/threads/:thread_id.
func (h *ThreadsHandler) DeleteThread(c *gin.Context) {
	threadID := c.Param("thread_id")
	if threadID == "" {
		NewValidationError("thread_id is required").Render(c, http.StatusBadRequest)
		return
	}

	if err := h.store.Delete(threadID); err != nil {
		NewNotFoundError("thread not found").Render(c, http.StatusNotFound)
		return
	}

	c.JSON(http.StatusOK, gin.H{"thread_id": threadID, "deleted": true})
}

// GetThreadState handles GET /api/threads/:thread_id/state.
func (h *ThreadsHandler) GetThreadState(c *gin.Context) {
	threadID := c.Param("thread_id")
	if threadID == "" {
		NewValidationError("thread_id is required").Render(c, http.StatusBadRequest)
		return
	}
	// Minimal implementation: return empty state (no checkpoint store integration yet).
	resp := ThreadStateResponse{
		ChannelValues: map[string]any{},
	}
	c.JSON(http.StatusOK, resp)
}

// UpdateThreadState handles POST /api/threads/:thread_id/state.
func (h *ThreadsHandler) UpdateThreadState(c *gin.Context) {
	threadID := c.Param("thread_id")
	if threadID == "" {
		NewValidationError("thread_id is required").Render(c, http.StatusBadRequest)
		return
	}
	var req struct {
		Values map[string]any `json:"values"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		NewValidationError("invalid request body").Render(c, http.StatusBadRequest)
		return
	}
	resp := ThreadStateResponse{
		ChannelValues: req.Values,
		CheckpointID:  uuid.NewString(),
	}
	c.JSON(http.StatusOK, resp)
}

// GetThreadHistory handles POST /api/threads/:thread_id/history.
func (h *ThreadsHandler) GetThreadHistory(c *gin.Context) {
	threadID := c.Param("thread_id")
	if threadID == "" {
		NewValidationError("thread_id is required").Render(c, http.StatusBadRequest)
		return
	}
	// Minimal implementation: return empty list (no persistent history yet).
	c.JSON(http.StatusOK, gin.H{"thread_id": threadID, "history": []HistoryEntry{}})
}

// ListRuns handles GET /api/threads/:thread_id/runs.
func (h *ThreadsHandler) ListRuns(c *gin.Context) {
	threadID := c.Param("thread_id")
	if threadID == "" {
		NewValidationError("thread_id is required").Render(c, http.StatusBadRequest)
		return
	}
	// Return currently in-memory runs for this thread.
	h.runsMu.RLock()
	defer h.runsMu.RUnlock()
	entries := make([]RunEntry, 0)
	for runID, rh := range h.runs {
		if rh.ThreadID == threadID {
			entries = append(entries, RunEntry{
				RunID:     runID,
				ThreadID:  threadID,
				Status:    "running",
				CreatedAt: time.Now().UnixMilli(),
			})
		}
	}
	c.JSON(http.StatusOK, gin.H{"runs": entries})
}

// GetRun handles GET /api/threads/:thread_id/runs/:run_id.
func (h *ThreadsHandler) GetRun(c *gin.Context) {
	threadID := c.Param("thread_id")
	runID := c.Param("run_id")
	if threadID == "" || runID == "" {
		NewValidationError("thread_id and run_id are required").Render(c, http.StatusBadRequest)
		return
	}
	rh, ok := h.getRun(runID)
	if !ok || rh.ThreadID != threadID {
		NewNotFoundError("run not found").Render(c, http.StatusNotFound)
		return
	}
	c.JSON(http.StatusOK, RunEntry{
		RunID:     runID,
		ThreadID:  threadID,
		Status:    "running",
		CreatedAt: time.Now().UnixMilli(),
	})
}
