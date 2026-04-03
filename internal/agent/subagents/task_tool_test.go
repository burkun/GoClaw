package subagents

import (
	"context"
	"encoding/json"
	"testing"
	"time"
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
