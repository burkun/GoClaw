package subagents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"

	"github.com/bookerbai/goclaw/internal/agent/subagents/builtins"
	"github.com/bookerbai/goclaw/internal/config"
	"github.com/bookerbai/goclaw/internal/models"
	toolruntime "github.com/bookerbai/goclaw/internal/tools"
)

// TaskToolName is the stable tool name used by the model.
const TaskToolName = "task"

// TaskToolConfig controls runtime behaviour for task tool.
type TaskToolConfig struct {
	Executor    *Executor
	Worker      WorkerFuncWithMessages
	WaitTimeout time.Duration
	// PollInterval is the interval between status polls during backend polling.
	PollInterval time.Duration
	// BackendPolling enables long-polling in the tool (DeerFlow style).
	BackendPolling bool
}

// TaskTool delegates a sub-task to the subagent executor.
type TaskTool struct {
	cfg TaskToolConfig
}

type taskToolInput struct {
	Description    string `json:"description"`
	Prompt         string `json:"prompt"`
	SubagentType   string `json:"subagent_type,omitempty"`
	MaxTurns       int    `json:"max_turns,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

type taskToolOutput struct {
	TaskID  string `json:"task_id"`
	Subject string `json:"subject,omitempty"`
	Status  Status `json:"status"`
	Output  string `json:"output,omitempty"`
	Error   string `json:"error,omitempty"`
	Message string `json:"message,omitempty"`
}

var (
	loadAppConfig     = config.GetAppConfig
	createChatModelFn = models.CreateChatModel
)

// NewTaskTool creates a task delegation tool.
func NewTaskTool(cfg TaskToolConfig) *TaskTool {
	if cfg.Executor == nil {
		cfg.Executor = DefaultExecutor()
	}
	if cfg.WaitTimeout <= 0 {
		cfg.WaitTimeout = 2 * time.Second
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 5 * time.Second
	}
	if cfg.Worker == nil {
		// Use the full agent worker with AI message collection
		cfg.Worker = AgentWorkerFuncWithMessages()
	}
	return &TaskTool{cfg: cfg}
}

func (t *TaskTool) Name() string {
	return TaskToolName
}

func (t *TaskTool) Description() string {
	return `Delegate a focused task to a subagent executor.
Use this for parallelizable work such as research, code scanning, and isolated analysis.
Returns a task id and the current/terminal status.`
}

func (t *TaskTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "required": ["description", "prompt"],
  "properties": {
    "description": {"type": "string", "description": "Short reason for delegation."},
    "prompt": {"type": "string", "description": "Subagent task prompt."},
    "subagent_type": {"type": "string", "description": "Subagent type (default: general-purpose)."},
    "max_turns": {"type": "integer", "description": "Optional max turns hint."},
    "timeout_seconds": {"type": "integer", "description": "Optional timeout override for this task."}
  }
}`)
}

// Execute submits a task and either waits briefly (legacy) or polls until completion (DeerFlow style).
func (t *TaskTool) Execute(ctx context.Context, input string) (string, error) {
	var in taskToolInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", fmt.Errorf("task tool: invalid input JSON: %w", err)
	}
	if strings.TrimSpace(in.Prompt) == "" {
		return "", fmt.Errorf("task tool: prompt is required")
	}

	req := TaskRequest{
		Description:  strings.TrimSpace(in.Description),
		Prompt:       strings.TrimSpace(in.Prompt),
		SubagentType: strings.TrimSpace(in.SubagentType),
		MaxTurns:     in.MaxTurns,
	}
	if in.TimeoutSeconds > 0 {
		req.Timeout = time.Duration(in.TimeoutSeconds) * time.Second
	}

	// Extract state from context (passed by parent agent)
	if vals := adk.GetSessionValues(ctx); vals != nil {
		if v, ok := vals["thread_id"].(string); ok {
			req.ThreadID = v
		}
		if v, ok := vals["workspace_path"].(string); ok {
			req.WorkspacePath = v
		}
		if v, ok := vals["uploads_path"].(string); ok {
			req.UploadsPath = v
		}
		if v, ok := vals["outputs_path"].(string); ok {
			req.OutputsPath = v
		}
	}

	// Validate and apply type config if available.
	if err := ValidateAndApplySubagentType(req.SubagentType, &req); err != nil {
		return "", fmt.Errorf("task tool: subagent type validation failed: %w", err)
	}

	// Submit task with AI message collection
	taskID, err := t.cfg.Executor.SubmitWithMessages(ctx, req, t.cfg.Worker)
	if err != nil {
		return "", fmt.Errorf("task tool: submit failed: %w", err)
	}

	subject := taskSubject(req)

	// Send task_started event
	SendTaskStarted(ctx, taskID, subject)

	// Choose polling mode
	if t.cfg.BackendPolling {
		// DeerFlow style: backend polls until completion and sends task_running events
		return t.executeWithBackendPolling(ctx, taskID, subject, req.Timeout)
	}

	// Legacy mode: brief wait then return current status
	return t.executeWithBriefWait(ctx, taskID, subject, req)
}

// executeWithBackendPolling polls until task completion, sending task_running events.
// Includes max_poll_count safety net to prevent infinite polling.
func (t *TaskTool) executeWithBackendPolling(ctx context.Context, taskID, subject string, timeout time.Duration) (string, error) {
	lastMessageCount := 0
	lastStatus := StatusPending
	pollCount := 0

	// Calculate max poll count based on timeout and poll interval.
	// Add buffer of 60 seconds for safety.
	maxPollCount := int((timeout + 60*time.Second) / t.cfg.PollInterval)
	if maxPollCount < 10 {
		maxPollCount = 10 // Minimum safety net
	}

	for {
		pollCount++

		// Safety net: prevent infinite polling
		if pollCount > maxPollCount {
			SendTaskFailed(ctx, taskID, fmt.Sprintf("polling exceeded max_poll_count (%d)", maxPollCount))
			return mustJSON(taskToolOutput{
				TaskID:  taskID,
				Subject: subject,
				Status:  StatusFailed,
				Error:   fmt.Sprintf("task polling exceeded maximum allowed polls (%d), possible deadlock", maxPollCount),
			}), nil
		}

		// Check context cancellation
		select {
		case <-ctx.Done():
			return mustJSON(taskToolOutput{
				TaskID:  taskID,
				Subject: subject,
				Status:  StatusFailed,
				Error:   ctx.Err().Error(),
			}), nil
		default:
		}

		// Get current result
		result, ok := t.cfg.Executor.Get(taskID)
		if !ok {
			SendTaskFailed(ctx, taskID, "task disappeared")
			return mustJSON(taskToolOutput{
				TaskID:  taskID,
				Subject: subject,
				Status:  StatusFailed,
				Error:   "task disappeared from executor",
			}), nil
		}

		// Send task_running events for new AI messages
		if len(result.AIMessages) > lastMessageCount {
			for i := lastMessageCount; i < len(result.AIMessages); i++ {
				SendTaskRunning(ctx, taskID, result.AIMessages[i], i+1, len(result.AIMessages))
			}
			lastMessageCount = len(result.AIMessages)
		}

		// Check for terminal status
		switch result.Status {
		case StatusCompleted:
			SendTaskCompleted(ctx, taskID, result.Output)
			return mustJSON(taskToolOutput{
				TaskID:  taskID,
				Subject: subject,
				Status:  StatusCompleted,
				Output:  result.Output,
			}), nil
		case StatusFailed:
			SendTaskFailed(ctx, taskID, result.Error)
			return mustJSON(taskToolOutput{
				TaskID:  taskID,
				Subject: subject,
				Status:  StatusFailed,
				Error:   result.Error,
			}), nil
		case StatusTimedOut:
			SendTaskTimedOut(ctx, taskID)
			return mustJSON(taskToolOutput{
				TaskID:  taskID,
				Subject: subject,
				Status:  StatusTimedOut,
				Error:   result.Error,
			}), nil
		}

		// Log status changes
		if result.Status != lastStatus {
			lastStatus = result.Status
		}

		// Wait before next poll
		select {
		case <-ctx.Done():
			return mustJSON(taskToolOutput{
				TaskID:  taskID,
				Subject: subject,
				Status:  StatusFailed,
				Error:   ctx.Err().Error(),
			}), nil
		case <-time.After(t.cfg.PollInterval):
		}
	}
}

// executeWithBriefWait waits briefly then returns current status (legacy mode).
func (t *TaskTool) executeWithBriefWait(ctx context.Context, taskID, subject string, req TaskRequest) (string, error) {
	eventCh := make(chan TaskEvent, 4)
	unsubscribe := t.cfg.Executor.Subscribe(taskID, func(_ context.Context, _ string, ev TaskEvent) {
		select {
		case eventCh <- ev:
		default:
		}
	})
	defer unsubscribe()

	waitCtx, cancel := context.WithTimeout(ctx, t.cfg.WaitTimeout)
	defer cancel()

	res, err := t.cfg.Executor.Wait(waitCtx, taskID)
	if err != nil {
		// Timed out while waiting; return current snapshot.
		snapshot, ok := t.cfg.Executor.Get(taskID)
		if ok {
			status := snapshot.Status
			// Prefer latest streamed event status when available.
			for {
				select {
				case ev := <-eventCh:
					status = ev.Status
				default:
					goto doneDrain
				}
			}
		doneDrain:
			if status == StatusPending {
				status = StatusQueued
			}
			return mustJSON(taskToolOutput{
				TaskID:  snapshot.TaskID,
				Subject: subject,
				Status:  status,
				Output:  snapshot.Output,
				Error:   snapshot.Error,
				Message: "task submitted; still running",
			}), nil
		}
		return mustJSON(taskToolOutput{
			TaskID:  taskID,
			Subject: subject,
			Status:  StatusQueued,
			Message: "task submitted",
		}), nil
	}

	// Determine event type and send
	switch res.Status {
	case StatusCompleted:
		SendTaskCompleted(ctx, taskID, res.Output)
	case StatusFailed:
		SendTaskFailed(ctx, taskID, res.Error)
	case StatusTimedOut:
		SendTaskTimedOut(ctx, taskID)
	}

	return mustJSON(taskToolOutput{
		TaskID:  res.TaskID,
		Subject: subject,
		Status:  res.Status,
		Output:  res.Output,
		Error:   res.Error,
	}), nil
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return `{"status":"failed","error":"marshal output failed"}`
	}
	return string(b)
}

// defaultWorker executes a focused single-turn subagent task.
// It tries to use the configured chat model first, then falls back to a
// deterministic local summarizer when model creation/inference is unavailable.
func defaultWorker(ctx context.Context, req TaskRequest) (string, error) {
	if strings.Contains(strings.ToLower(req.Prompt), "force_fail") {
		return "", fmt.Errorf("subagent forced failure")
	}

	cfg, err := loadAppConfig()
	if err == nil && cfg != nil {
		modelReq := models.CreateRequest{}
		if strings.TrimSpace(req.ModelName) != "" {
			modelReq.ModelName = strings.TrimSpace(req.ModelName)
		} else if dm := cfg.DefaultModel(); dm != nil {
			modelReq.ModelName = dm.Name
		}
		systemPrompt := "You are a focused subagent. Solve the task directly and return concise actionable output."
		if strings.TrimSpace(req.SystemPrompt) != "" {
			systemPrompt = strings.TrimSpace(req.SystemPrompt)
		}
		chatModel, modelErr := createChatModelFn(ctx, cfg, modelReq)
		if modelErr == nil && chatModel != nil {
			resp, genErr := chatModel.Generate(ctx, []*schema.Message{
				schema.SystemMessage(systemPrompt),
				schema.UserMessage(buildSubagentPrompt(req)),
			})
			if genErr == nil && resp != nil {
				out := strings.TrimSpace(resp.Content)
				if out != "" {
					return out, nil
				}
			}
		}
	}

	return fallbackSubagentOutput(req), nil
}

func defaultString(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

func taskSubject(req TaskRequest) string {
	s := strings.TrimSpace(req.Description)
	if s == "" {
		s = strings.TrimSpace(req.Prompt)
	}
	if len(s) > 80 {
		return s[:80]
	}
	return s
}

func buildSubagentPrompt(req TaskRequest) string {
	parts := []string{
		"Subagent type: " + defaultString(req.SubagentType, "general-purpose"),
		"Task prompt:\n" + strings.TrimSpace(req.Prompt),
	}
	if strings.TrimSpace(req.Description) != "" {
		parts = append(parts, "Description: "+strings.TrimSpace(req.Description))
	}
	if req.MaxTurns > 0 {
		parts = append(parts, fmt.Sprintf("Max turns hint: %d", req.MaxTurns))
	}
	if len(req.AllowedTools) > 0 {
		parts = append(parts, "Allowed tools: "+strings.Join(req.AllowedTools, ", "))
	}
	return strings.Join(parts, "\n\n")
}

func fallbackSubagentOutput(req TaskRequest) string {
	subject := taskSubject(req)
	if subject == "" {
		subject = "delegated task"
	}
	return fmt.Sprintf("subagent[%s] completed: %s", defaultString(req.SubagentType, "general-purpose"), subject)
}

// ValidateAndApplySubagentType checks if the type is registered and applies its config.
// It merges builtin defaults with config.yaml overrides.
func ValidateAndApplySubagentType(subagentType string, req *TaskRequest) error {
	if subagentType == "" {
		subagentType = "general-purpose"
	}
	req.SubagentType = subagentType

	cfg, err := loadAppConfig()
	if err != nil {
		cfg = nil
	}

	// Get effective config (builtin + config.yaml overrides)
	typeCfg := builtins.GetEffectiveConfig(subagentType, cfg)

	if !typeCfg.Enabled {
		return fmt.Errorf("subagent type %q is disabled", subagentType)
	}

	// Apply type-specific overrides if not already set.
	if typeCfg.TimeoutSecs > 0 && req.Timeout <= 0 {
		req.Timeout = time.Duration(typeCfg.TimeoutSecs) * time.Second
	}
	if typeCfg.MaxTurns > 0 && req.MaxTurns <= 0 {
		req.MaxTurns = typeCfg.MaxTurns
	}
	if strings.TrimSpace(typeCfg.Model) != "" && !strings.EqualFold(strings.TrimSpace(typeCfg.Model), "inherit") && strings.TrimSpace(req.ModelName) == "" {
		req.ModelName = strings.TrimSpace(typeCfg.Model)
	}
	if strings.TrimSpace(typeCfg.SystemPrompt) != "" && strings.TrimSpace(req.SystemPrompt) == "" {
		req.SystemPrompt = strings.TrimSpace(typeCfg.SystemPrompt)
	}
	if len(typeCfg.AllowedTools) > 0 && len(req.AllowedTools) == 0 {
		req.AllowedTools = append([]string{}, typeCfg.AllowedTools...)
	}
	if len(typeCfg.DisallowedTools) > 0 && len(req.DisallowedTools) == 0 {
		req.DisallowedTools = append([]string{}, typeCfg.DisallowedTools...)
	}

	return nil
}

var _ toolruntime.Tool = (*TaskTool)(nil)
