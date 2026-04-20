package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"goclaw/internal/agent"
	"goclaw/internal/config"
	"goclaw/internal/threadstore"
	"goclaw/pkg/metrics"
)

// LangGraphHandler serves the LangGraph SDK compatible API endpoints.
// These endpoints are mounted under /api/langgraph/* and follow the
// LangGraph Platform API contract for compatibility with the @langchain/langgraph-sdk.
type LangGraphHandler struct {
	cfg    *config.AppConfig
	agent  agent.LeadAgent
	agents map[string]agent.LeadAgent // P1 fix: 支持多agent
	store  threadstore.Store          // Thread persistence for multi-turn context

	runsMu sync.RWMutex
	runs   map[string]lgRunHandle
}

type lgRunHandle struct {
	ThreadID     string
	RunID        string
	CheckpointID string
	AgentName    string // P1 fix: 记录使用的agent
	Cancel       context.CancelFunc
}

// NewLangGraphHandler creates a handler for LangGraph-compatible endpoints.
func NewLangGraphHandler(cfg *config.AppConfig, a agent.LeadAgent) *LangGraphHandler {
	store, _ := threadstore.NewFileStore("")
	return &LangGraphHandler{
		cfg:   cfg,
		agent: a,
		store: store,
		runs:  make(map[string]lgRunHandle),
	}
}

// NewLangGraphHandlerWithAgents creates a handler with multiple agents (P1 fix).
func NewLangGraphHandlerWithAgents(cfg *config.AppConfig, a agent.LeadAgent, agents map[string]agent.LeadAgent) *LangGraphHandler {
	store, _ := threadstore.NewFileStore("")
	return &LangGraphHandler{
		cfg:    cfg,
		agent:  a,
		agents: agents,
		store:  store,
		runs:   make(map[string]lgRunHandle),
	}
}

// NewLangGraphHandlerWithStore creates a handler with an explicit thread store.
func NewLangGraphHandlerWithStore(cfg *config.AppConfig, a agent.LeadAgent, store threadstore.Store) *LangGraphHandler {
	if store == nil {
		store, _ = threadstore.NewFileStore("")
	}
	return &LangGraphHandler{
		cfg:   cfg,
		agent: a,
		store: store,
		runs:  make(map[string]lgRunHandle),
	}
}

// GetAgent returns the agent by name (P1 fix).
func (h *LangGraphHandler) GetAgent(name string) agent.LeadAgent {
	if name == "" || h.agents == nil {
		return h.agent
	}

	if a, ok := h.agents[name]; ok {
		return a
	}

	return h.agent
}

// ---------------------------------------------------------------------------
// Request / Response types (LangGraph SDK format)
// ---------------------------------------------------------------------------

// LGCreateThreadRequest is the body for POST /threads.
type LGCreateThreadRequest struct {
	ThreadID string         `json:"thread_id,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// LGThread represents a thread in LangGraph format.
type LGThread struct {
	ThreadID  string         `json:"thread_id"`
	CreatedAt string         `json:"created_at"`
	UpdatedAt string         `json:"updated_at"`
	Status    string         `json:"status"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	Values    map[string]any `json:"values,omitempty"`
}

// LGThreadState represents thread state in LangGraph format.
type LGThreadState struct {
	Values       map[string]any `json:"values"`
	Next         []string       `json:"next"`
	CheckpointID string         `json:"checkpoint_id,omitempty"`
	CreatedAt    string         `json:"created_at,omitempty"`
	ParentID     string         `json:"parent_id,omitempty"`
}

// LGRunRequest is the body for POST /threads/{id}/runs/stream.
type LGRunRequest struct {
	AssistantID     string         `json:"assistant_id"`
	Input           any            `json:"input,omitempty"`
	Config          map[string]any `json:"config,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
	StreamMode      any            `json:"stream_mode,omitempty"` // string or []string
	StreamSubgraphs bool           `json:"stream_subgraphs,omitempty"`
	StreamResumable bool           `json:"stream_resumable,omitempty"`
	CheckpointID    string         `json:"checkpoint_id,omitempty"`
	InterruptBefore any            `json:"interrupt_before,omitempty"`
	InterruptAfter  any            `json:"interrupt_after,omitempty"`
}

// LGSearchRequest is the body for POST /threads/search.
type LGSearchRequest struct {
	Limit    int    `json:"limit,omitempty"`
	Offset   int    `json:"offset,omitempty"`
	Query    string `json:"query,omitempty"`
	Status   string `json:"status,omitempty"`
	SortBy   string `json:"sort_by,omitempty"`
	SortDesc bool   `json:"sort_desc,omitempty"`
}

// ---------------------------------------------------------------------------
// Thread CRUD endpoints
// ---------------------------------------------------------------------------

// CreateThread handles POST /threads.
func (h *LangGraphHandler) CreateThread(c *gin.Context) {
	var req LGCreateThreadRequest
	if err := c.ShouldBindJSON(&req); err != nil && err.Error() != "EOF" {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	threadID := strings.TrimSpace(req.ThreadID)
	if threadID == "" {
		threadID = uuid.NewString()
	}

	now := time.Now().UTC().Format(time.RFC3339)
	thread := LGThread{
		ThreadID:  threadID,
		CreatedAt: now,
		UpdatedAt: now,
		Status:    "idle",
		Metadata:  req.Metadata,
	}

	c.JSON(http.StatusCreated, thread)
}

// GetThread handles GET /threads/{thread_id}.
func (h *LangGraphHandler) GetThread(c *gin.Context) {
	threadID := c.Param("thread_id")
	if threadID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "thread_id is required"})
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	thread := LGThread{
		ThreadID:  threadID,
		CreatedAt: now,
		UpdatedAt: now,
		Status:    "idle",
	}

	c.JSON(http.StatusOK, thread)
}

// DeleteThread handles DELETE /threads/{thread_id}.
func (h *LangGraphHandler) DeleteThread(c *gin.Context) {
	threadID := c.Param("thread_id")
	if threadID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "thread_id is required"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"thread_id": threadID, "deleted": true})
}

// SearchThreads handles POST /threads/search.
func (h *LangGraphHandler) SearchThreads(c *gin.Context) {
	var req LGSearchRequest
	_ = c.ShouldBindJSON(&req)

	// Minimal implementation: return empty list.
	// Full implementation would query a persistent store.
	c.JSON(http.StatusOK, []LGThread{})
}

// GetThreadState handles GET /threads/{thread_id}/state.
func (h *LangGraphHandler) GetThreadState(c *gin.Context) {
	threadID := c.Param("thread_id")
	if threadID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "thread_id is required"})
		return
	}

	state := LGThreadState{
		Values: map[string]any{
			"messages": []any{},
		},
		Next: []string{},
	}

	c.JSON(http.StatusOK, state)
}

// UpdateThreadState handles PATCH /threads/{thread_id}/state.
func (h *LangGraphHandler) UpdateThreadState(c *gin.Context) {
	threadID := c.Param("thread_id")
	if threadID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "thread_id is required"})
		return
	}

	var req struct {
		Values map[string]any `json:"values"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	state := LGThreadState{
		Values:       req.Values,
		Next:         []string{},
		CheckpointID: uuid.NewString(),
	}

	c.JSON(http.StatusOK, state)
}

// ---------------------------------------------------------------------------
// Runs endpoints
// ---------------------------------------------------------------------------

// StreamRun handles POST /threads/{thread_id}/runs/stream.
// This is the core endpoint that the LangGraph SDK uses for streaming agent responses.
func (h *LangGraphHandler) StreamRun(c *gin.Context) {
	threadID := c.Param("thread_id")
	if threadID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "thread_id is required"})
		return
	}

	var req LGRunRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	runID := uuid.NewString()

	// Determine stream mode.
	streamMode := "values"
	switch v := req.StreamMode.(type) {
	case string:
		if v != "" {
			streamMode = v
		}
	case []any:
		if len(v) > 0 {
			if s, ok := v[0].(string); ok {
				streamMode = s
			}
		}
	}

	// Set SSE headers.
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	w := c.Writer

	// Create converter for this stream.
	converter := NewLangGraphEventConverter(threadID, runID, streamMode)

	// Write metadata event first.
	if err := WriteLangGraphSSE(w, converter.ConvertMetadataEvent()); err != nil {
		return
	}
	w.Flush()

	// Check agent is initialized and select based on request.
	// Priority: query param > header > config.context > default "lead_agent"
	agentName := c.Query("agent")
	if agentName == "" {
		agentName = c.GetHeader("X-Agent-Name")
	}
	if agentName == "" {
		if ctx, ok := req.Config["context"].(map[string]any); ok {
			if v, ok := ctx["agent_name"].(string); ok && v != "" {
				agentName = v
			}
		}
	}
	if agentName == "" {
		agentName = "lead_agent"
	}

	selectedAgent := h.GetAgent(agentName)
	if selectedAgent == nil {
		_ = WriteLangGraphSSE(w, LangGraphSSEEvent{
			Event: "error",
			Data:  LangGraphErrorEvent{Message: fmt.Sprintf("agent %q not found", agentName)},
		})
		w.Flush()
		return
	}

	// Prepare agent state from request input.
	state := &agent.ThreadState{
		Messages: make([]*schema.Message, 0),
	}

	// Load historical messages from thread store for multi-turn context.
	if h.store != nil {
		if existingState, err := h.store.GetState(threadID); err == nil && existingState != nil {
			for _, mr := range existingState.Messages {
				var msg *schema.Message
				switch mr.Role {
				case "human":
					msg = schema.UserMessage(mr.Content)
				case "assistant", "ai":
					msg = schema.AssistantMessage(mr.Content, nil)
				case "system":
					msg = schema.SystemMessage(mr.Content)
				case "tool":
					msg = schema.ToolMessage(mr.Content, "")
				default:
					msg = schema.UserMessage(mr.Content)
				}
				if msg != nil {
					state.Messages = append(state.Messages, msg)
				}
			}
		}
	}

	// Extract messages from input if present.
	if input, ok := req.Input.(map[string]any); ok {
		if msgs, ok := input["messages"].([]any); ok {
			for _, m := range msgs {
				if msgMap, ok := m.(map[string]any); ok {
					if msg := parseLangGraphMessage(msgMap); msg != nil {
						state.Messages = append(state.Messages, msg)
					}
				}
			}
		}
	}

	// Prepare run configuration.
	modelName := "gpt-4"
	if h.cfg != nil && h.cfg.DefaultModel() != nil {
		modelName = h.cfg.DefaultModel().Name
	}

	// Extract context from request config.
	thinkEnabled := true
	planMode := false
	subagentEnabled := true
	if ctx, ok := req.Config["context"].(map[string]any); ok {
		if v, ok := ctx["thinking_enabled"].(bool); ok {
			thinkEnabled = v
		}
		if v, ok := ctx["is_plan_mode"].(bool); ok {
			planMode = v
		}
		if v, ok := ctx["subagent_enabled"].(bool); ok {
			subagentEnabled = v
		}
	}

	cfg := agent.RunConfig{
		ThreadID:        threadID,
		ModelName:       modelName,
		ThinkingEnabled: thinkEnabled,
		IsPlanMode:      planMode,
		SubagentEnabled: subagentEnabled,
		CheckpointID:    req.CheckpointID,
		AgentName:       agentName,
		RunID:           runID,
	}

	// Run the agent.
	runCtx, cancel := context.WithCancel(c.Request.Context())

	// Record metrics: increment active runs.
	metrics.SetActiveRuns(float64(len(h.runs) + 1))
	defer metrics.SetActiveRuns(float64(len(h.runs)))

	// Track agent run duration for metrics.
	runStartTime := time.Now()
	var runStatus string

	h.registerRun(runID, lgRunHandle{
		ThreadID:     threadID,
		RunID:        runID,
		CheckpointID: req.CheckpointID,
		AgentName:    agentName,
		Cancel:       cancel,
	})
	defer h.unregisterRun(runID)

	// Ensure metrics are recorded on exit.
	defer func() {
		runDuration := time.Since(runStartTime)
		if runStatus == "" {
			runStatus = "success"
		}
		metrics.RecordAgentRun(agentName, runDuration, runStatus)
	}()

	var eventChan <-chan agent.Event
	var err error

	if strings.TrimSpace(req.CheckpointID) != "" {
		eventChan, err = selectedAgent.Resume(runCtx, state, cfg, req.CheckpointID)
	} else {
		eventChan, err = selectedAgent.Run(runCtx, state, cfg)
	}

	if err != nil {
		_ = WriteLangGraphSSE(w, LangGraphSSEEvent{
			Event: "error",
			Data:  LangGraphErrorEvent{Message: fmt.Sprintf("failed to start agent: %v", err)},
		})
		w.Flush()
		return
	}

	// Stream events with heartbeat support.
	// Use a ticker to send heartbeat messages when no events are received.
	heartbeatTicker := time.NewTicker(HeartbeatInterval)
	defer heartbeatTicker.Stop()

streamLoop:
	for {
		select {
		case ev, ok := <-eventChan:
			if !ok {
				break streamLoop
			}
			events := converter.Convert(ev)
			for _, sseEv := range events {
				if err := WriteLangGraphSSE(w, sseEv); err != nil {
					return
				}
			}
			w.Flush()
			// Reset heartbeat timer after sending events
			heartbeatTicker.Reset(HeartbeatInterval)

		case <-heartbeatTicker.C:
			// Send heartbeat to keep connection alive
			if err := WriteSSEHeartbeat(w); err != nil {
				return
			}
			w.Flush()

		case <-runCtx.Done():
			break streamLoop
		}
	}

	// Persist thread state for multi-turn context.
	h.saveThreadMessages(threadID, state.Messages, converter.messages)
}

// StreamRunStandalone handles POST /runs/stream (without thread_id).
// Creates a new thread internally.
func (h *LangGraphHandler) StreamRunStandalone(c *gin.Context) {
	var req LGRunRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Create a new thread.
	threadID := uuid.NewString()

	// Forward to StreamRun with the new thread_id.
	c.Params = append(c.Params, gin.Param{Key: "thread_id", Value: threadID})
	h.StreamRun(c)
}

// CancelRun handles POST /threads/{thread_id}/runs/{run_id}/cancel.
func (h *LangGraphHandler) CancelRun(c *gin.Context) {
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
		"thread_id": threadID,
		"run_id":    runID,
		"status":    "cancelling",
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (h *LangGraphHandler) registerRun(runID string, run lgRunHandle) {
	h.runsMu.Lock()
	defer h.runsMu.Unlock()
	h.runs[runID] = run
}

func (h *LangGraphHandler) unregisterRun(runID string) {
	h.runsMu.Lock()
	defer h.runsMu.Unlock()
	delete(h.runs, runID)
}

func (h *LangGraphHandler) getRun(runID string) (lgRunHandle, bool) {
	h.runsMu.RLock()
	defer h.runsMu.RUnlock()
	r, ok := h.runs[runID]
	return r, ok
}

// saveThreadMessages persists the combined message history to thread store.
func (h *LangGraphHandler) saveThreadMessages(threadID string, initialMsgs []*schema.Message, converterMsgs []LangGraphMessage) {
	if h.store == nil {
		return
	}

	// Build combined message list: initial messages + new messages from converter
	allMsgs := make([]threadstore.MessageRecord, 0)

	// Add initial messages
	for _, m := range initialMsgs {
		role := "human"
		switch m.Role {
		case schema.Assistant:
			role = "assistant"
		case schema.System:
			role = "system"
		case schema.Tool:
			role = "tool"
		}
		allMsgs = append(allMsgs, threadstore.MessageRecord{
			Role:      role,
			Content:   m.Content,
			CreatedAt: time.Now().UnixMilli(),
		})
	}

	// Add new messages from converter (AI responses and tool results)
	for _, m := range converterMsgs {
		content := ""
		switch v := m.Content.(type) {
		case string:
			content = v
		}
		if content == "" {
			continue
		}
		allMsgs = append(allMsgs, threadstore.MessageRecord{
			Role:      m.Type,
			Content:   content,
			CreatedAt: time.Now().UnixMilli(),
		})
	}

	// Create or update thread state
	now := time.Now().UnixMilli()
	threadState := &threadstore.ThreadState{
		ThreadID:  threadID,
		Status:    "idle",
		Messages:  allMsgs,
		UpdatedAt: now,
	}

	// Check if thread exists, create metadata if not
	if existingMeta, _ := h.store.Get(threadID); existingMeta == nil {
		_ = h.store.Create(&threadstore.ThreadMetadata{
			ThreadID:  threadID,
			Status:    "idle",
			CreatedAt: now,
			UpdatedAt: now,
		})
	}

	_ = h.store.SaveState(threadState)
}

// parseContent extracts text content from various formats.
// LangGraph SDK may send content as:
// - string: "hello"
// - array: [{"type":"text","text":"hello"}]
// - array with multiple parts: [{"type":"text","text":"hello"},{"type":"image_url","image_url":{...}}]
func parseContent(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var parts []string
		for _, item := range v {
			if itemMap, ok := item.(map[string]any); ok {
				if text, ok := itemMap["text"].(string); ok && text != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

// parseLangGraphMessage converts a LangGraph message map to schema.Message.
func parseLangGraphMessage(m map[string]any) *schema.Message {
	role, _ := m["type"].(string)
	content := parseContent(m["content"])

	switch role {
	case "human":
		return schema.UserMessage(content)
	case "ai":
		// Parse tool calls if present.
		var toolCalls []schema.ToolCall
		if tcs, ok := m["tool_calls"].([]any); ok {
			for _, tc := range tcs {
				if tcMap, ok := tc.(map[string]any); ok {
					id, _ := tcMap["id"].(string)
					name, _ := tcMap["name"].(string)
					argsJSON, _ := tcMap["args"].(string)
					if argsJSON == "" {
						argsBytes, _ := json.Marshal(tcMap["args"])
						argsJSON = string(argsBytes)
					}
					toolCalls = append(toolCalls, schema.ToolCall{
						ID:   id,
						Type: "tool_call",
						Function: schema.FunctionCall{
							Name:      name,
							Arguments: argsJSON,
						},
					})
				}
			}
		}
		msg := schema.AssistantMessage(content, toolCalls)
		if name, ok := m["name"].(string); ok {
			msg.Name = name
		}
		return msg
	case "tool":
		toolCallID, _ := m["tool_call_id"].(string)
		name, _ := m["name"].(string)
		return schema.ToolMessage(content, toolCallID, schema.WithToolName(name))
	case "system":
		return schema.SystemMessage(content)
	default:
		return schema.UserMessage(content)
	}
}

// WaitForRun handles GET /threads/{thread_id}/runs/{run_id}/join (for non-streaming clients).
func (h *LangGraphHandler) WaitForRun(c *gin.Context) {
	threadID := c.Param("thread_id")
	runID := c.Param("run_id")
	if threadID == "" || runID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "thread_id and run_id are required"})
		return
	}

	// Minimal implementation: return run status.
	c.JSON(http.StatusOK, gin.H{
		"thread_id": threadID,
		"run_id":    runID,
		"status":    "pending",
	})
}

// GetRun handles GET /threads/{thread_id}/runs/{run_id}.
func (h *LangGraphHandler) GetRun(c *gin.Context) {
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

	c.JSON(http.StatusOK, gin.H{
		"thread_id":     threadID,
		"run_id":        runID,
		"status":        "running",
		"checkpoint_id": run.CheckpointID,
	})
}

// ListRuns handles GET /threads/{thread_id}/runs.
func (h *LangGraphHandler) ListRuns(c *gin.Context) {
	threadID := c.Param("thread_id")
	if threadID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "thread_id is required"})
		return
	}

	h.runsMu.RLock()
	defer h.runsMu.RUnlock()

	runs := make([]map[string]any, 0)
	for runID, run := range h.runs {
		if run.ThreadID == threadID {
			runs = append(runs, map[string]any{
				"run_id": runID,
				"status": "running",
			})
		}
	}

	c.JSON(http.StatusOK, runs)
}

// ---------------------------------------------------------------------------
// Assistants endpoints (stub for SDK compatibility)
// ---------------------------------------------------------------------------

// GetAssistant handles GET /assistants/{assistant_id}.
// LangGraph SDK expects this to exist for the assistant_id parameter.
func (h *LangGraphHandler) GetAssistant(c *gin.Context) {
	assistantID := c.Param("assistant_id")
	if assistantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "assistant_id is required"})
		return
	}

	// Return a stub assistant for "lead_agent".
	c.JSON(http.StatusOK, gin.H{
		"assistant_id": assistantID,
		"name":         "lead_agent",
		"graph_id":     "lead_agent",
		"config":       map[string]any{},
	})
}

// ListAssistants handles GET /assistants.
func (h *LangGraphHandler) ListAssistants(c *gin.Context) {
	c.JSON(http.StatusOK, []gin.H{
		{
			"assistant_id": "lead_agent",
			"name":         "Lead Agent",
			"graph_id":     "lead_agent",
		},
	})
}
