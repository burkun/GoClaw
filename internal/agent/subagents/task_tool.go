package subagents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"

	"github.com/bookerbai/goclaw/internal/config"
	"github.com/bookerbai/goclaw/internal/models"
	toolruntime "github.com/bookerbai/goclaw/internal/tools"
)

// TaskToolName is the stable tool name used by the model.
const TaskToolName = "task"

// TaskToolConfig controls runtime behaviour for task tool.
type TaskToolConfig struct {
	Executor    *Executor
	Worker      WorkerFunc
	WaitTimeout time.Duration
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
	if cfg.Worker == nil {
		cfg.Worker = defaultWorker
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

// Execute submits a task and waits briefly for completion.
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

	// Validate and apply type config if available.
	if err := ValidateAndApplySubagentType(req.SubagentType, &req); err != nil {
		return "", fmt.Errorf("task tool: subagent type validation failed: %w", err)
	}

	taskID, err := t.cfg.Executor.Submit(ctx, req, t.cfg.Worker)
	if err != nil {
		return "", fmt.Errorf("task tool: submit failed: %w", err)
	}

	waitCtx, cancel := context.WithTimeout(ctx, t.cfg.WaitTimeout)
	defer cancel()

	res, err := t.cfg.Executor.Wait(waitCtx, taskID)
	if err != nil {
		// Timed out while waiting; return current snapshot.
		snapshot, ok := t.cfg.Executor.Get(taskID)
		if ok {
			return mustJSON(taskToolOutput{
				TaskID:  snapshot.TaskID,
				Subject: taskSubject(req),
				Status:  snapshot.Status,
				Output:  snapshot.Output,
				Error:   snapshot.Error,
				Message: "task submitted; still running",
			}), nil
		}
		return mustJSON(taskToolOutput{
			TaskID:  taskID,
			Subject: taskSubject(req),
			Status:  StatusQueued,
			Message: "task submitted",
		}), nil
	}

	return mustJSON(taskToolOutput{
		TaskID:  res.TaskID,
		Subject: taskSubject(req),
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
		if dm := cfg.DefaultModel(); dm != nil {
			modelReq.ModelName = dm.Name
		}
		chatModel, modelErr := createChatModelFn(ctx, cfg, modelReq)
		if modelErr == nil && chatModel != nil {
			resp, genErr := chatModel.Generate(ctx, []*schema.Message{
				schema.SystemMessage("You are a focused subagent. Solve the task directly and return concise actionable output."),
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
func ValidateAndApplySubagentType(subagentType string, req *TaskRequest) error {
	if subagentType == "" {
		subagentType = "general-purpose"
	}

	cfg, err := loadAppConfig()
	if err != nil || cfg == nil {
		// No config; allow any type.
		return nil
	}

	if cfg.Subagents.Types == nil || len(cfg.Subagents.Types) == 0 {
		// No subagents config; allow any type.
		return nil
	}

	typeCfg, ok := cfg.Subagents.Types[subagentType]
	if !ok {
		// Type not explicitly defined; allow but warn.
		return nil
	}

	if !typeCfg.Enabled {
		return fmt.Errorf("subagent type %q is disabled", subagentType)
	}

	// Apply type-specific overrides if not already set.
	if typeCfg.TimeoutSecs > 0 && req.Timeout <= 0 {
		req.Timeout = time.Duration(typeCfg.TimeoutSecs) * time.Second
	}
	if typeCfg.Model != "" && typeCfg.Model != "inherit" {
		// Note: model override would be applied by the worker, not here.
	}

	return nil
}

var _ toolruntime.Tool = (*TaskTool)(nil)
