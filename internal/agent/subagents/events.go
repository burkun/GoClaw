package subagents

import (
	"context"
	"time"

	"github.com/cloudwego/eino/adk"
)

// TaskEventData represents the data for a task event.
type TaskEventData struct {
	Type          string         `json:"type"`
	TaskID        string         `json:"task_id"`
	Timestamp     int64          `json:"timestamp"`
	Description   string         `json:"description,omitempty"`
	Message       map[string]any `json:"message,omitempty"`
	MessageIndex  int            `json:"message_index,omitempty"`
	TotalMessages int            `json:"total_messages,omitempty"`
	Result        string         `json:"result,omitempty"`
	Error         string         `json:"error,omitempty"`
}

// sendTaskEvent sends a custom task event to the SSE stream.
// This function uses Eino's adk.SendEvent with a CustomizedAction to emit
// task lifecycle events (task_started, task_running, task_completed, etc.).
func sendTaskEvent(ctx context.Context, eventType string, taskID string, data map[string]any) {
	if data == nil {
		data = make(map[string]any)
	}
	data["task_id"] = taskID
	data["type"] = eventType
	data["timestamp"] = time.Now().UnixMilli()

	event := &adk.AgentEvent{
		Action: &adk.AgentAction{
			CustomizedAction: data,
		},
	}

	// Silently ignore error if not in agent context
	_ = adk.SendEvent(ctx, event)
}

// SendTaskStarted sends a task_started event.
func SendTaskStarted(ctx context.Context, taskID, description string) {
	sendTaskEvent(ctx, "task_started", taskID, map[string]any{
		"description": description,
	})
}

// SendTaskRunning sends a task_running event with AI message.
func SendTaskRunning(ctx context.Context, taskID string, message map[string]any, messageIndex, totalMessages int) {
	sendTaskEvent(ctx, "task_running", taskID, map[string]any{
		"message":        message,
		"message_index":  messageIndex,
		"total_messages": totalMessages,
	})
}

// SendTaskCompleted sends a task_completed event.
func SendTaskCompleted(ctx context.Context, taskID, output string) {
	sendTaskEvent(ctx, "task_completed", taskID, map[string]any{
		"result": output,
	})
}

// SendTaskFailed sends a task_failed event.
func SendTaskFailed(ctx context.Context, taskID, errorMsg string) {
	sendTaskEvent(ctx, "task_failed", taskID, map[string]any{
		"error": errorMsg,
	})
}

// SendTaskTimedOut sends a task_timed_out event.
func SendTaskTimedOut(ctx context.Context, taskID string) {
	sendTaskEvent(ctx, "task_timed_out", taskID, nil)
}
