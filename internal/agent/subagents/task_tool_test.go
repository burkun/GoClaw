package subagents

import (
	"context"
	"encoding/json"
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
	if err == nil || !contains(err.Error(), "disabled") {
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
						Enabled:     true,
						TimeoutSecs: 42,
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
	if req.Timeout != 42*time.Second {
		t.Fatalf("expected timeout 42s, got %v", req.Timeout)
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
