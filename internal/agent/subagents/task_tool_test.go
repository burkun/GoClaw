package subagents

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/bookerbai/goclaw/internal/config"
)

func TestTaskToolExecuteCompleted(t *testing.T) {
	exec := NewExecutor(ExecutorConfig{MaxConcurrent: 1, DefaultTimeout: time.Second})
	tool := NewTaskTool(TaskToolConfig{
		Executor:    exec,
		WaitTimeout: time.Second,
	})

	out, err := tool.Execute(context.Background(), `{"description":"run","prompt":"hello","subagent_type":"general-purpose"}`)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("output is not valid json: %v, out=%s", err, out)
	}

	if parsed["task_id"] == "" {
		t.Fatalf("expected task_id in output")
	}
	if parsed["status"] != string(StatusCompleted) {
		t.Fatalf("expected status completed, got %v", parsed["status"])
	}
}

func TestTaskToolExecuteRunningSnapshot(t *testing.T) {
	exec := NewExecutor(ExecutorConfig{MaxConcurrent: 1, DefaultTimeout: time.Second})
	tool := NewTaskTool(TaskToolConfig{
		Executor:    exec,
		WaitTimeout: 5 * time.Millisecond,
		Worker: func(ctx context.Context, req TaskRequest) (string, error) {
			_ = req
			select {
			case <-time.After(120 * time.Millisecond):
				return "done", nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		},
	})

	out, err := tool.Execute(context.Background(), `{"description":"run","prompt":"slow task"}`)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("output is not valid json: %v, out=%s", err, out)
	}

	if parsed["task_id"] == "" {
		t.Fatalf("expected task_id in output")
	}
	if parsed["message"] == nil {
		t.Fatalf("expected running message in output")
	}
	status, _ := parsed["status"].(string)
	if status != string(StatusQueued) && status != string(StatusInProgress) {
		t.Fatalf("expected status queued/in_progress, got %v", parsed["status"])
	}
}

func TestValidateAndApplySubagentType_Disabled(t *testing.T) {
	// Mock config with disabled type.
	oldLoadCfg := loadAppConfig
	defer func() { loadAppConfig = oldLoadCfg }()

	loadAppConfig = func() (*config.AppConfig, error) {
		return &config.AppConfig{
			Subagents: config.SubagentsConfig{
				Types: map[string]config.SubagentTypeConfig{
					"test-disabled": {Enabled: false},
				},
			},
		}, nil
	}

	req := &TaskRequest{Prompt: "test"}
	err := ValidateAndApplySubagentType("test-disabled", req)
	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("expected disabled error, got %v", err)
	}
}

func TestValidateAndApplySubagentType_TimeoutApplied(t *testing.T) {
	// Mock config with type that has timeout.
	oldLoadCfg := loadAppConfig
	defer func() { loadAppConfig = oldLoadCfg }()

	loadAppConfig = func() (*config.AppConfig, error) {
		return &config.AppConfig{
			Subagents: config.SubagentsConfig{
				Types: map[string]config.SubagentTypeConfig{
					"test-with-timeout": {
						Enabled:      true,
						TimeoutSecs:  42,
						Model:        "gpt-4o-mini",
						SystemPrompt: "you are subagent",
						AllowedTools: []string{"bash", "read_file"},
					},
				},
			},
		}, nil
	}

	req := &TaskRequest{Prompt: "test"}
	err := ValidateAndApplySubagentType("test-with-timeout", req)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if req.SubagentType != "test-with-timeout" {
		t.Fatalf("expected subagent type to be set, got %q", req.SubagentType)
	}
	if req.Timeout != 42*time.Second {
		t.Fatalf("expected timeout 42s, got %v", req.Timeout)
	}
	if req.ModelName != "gpt-4o-mini" {
		t.Fatalf("expected model override, got %q", req.ModelName)
	}
	if req.SystemPrompt != "you are subagent" {
		t.Fatalf("expected system prompt override, got %q", req.SystemPrompt)
	}
	if len(req.AllowedTools) != 2 || req.AllowedTools[0] != "bash" {
		t.Fatalf("expected allowed tools override, got %#v", req.AllowedTools)
	}

	prompt := buildSubagentPrompt(*req)
	if !strings.Contains(prompt, "Allowed tools: bash, read_file") {
		t.Fatalf("expected prompt to include allowed tools, got %q", prompt)
	}
}
