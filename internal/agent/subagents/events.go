package subagents

import (
	"context"
	"sync"
	"time"
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

// taskEventKey is the context key for collecting task events.
type taskEventKey struct{}

// TaskEventCollector collects task events during subagent execution.
type TaskEventCollector struct {
	mu     sync.Mutex
	events []TaskEventData
}

// NewTaskEventCollector creates a new collector.
func NewTaskEventCollector() *TaskEventCollector {
	return &TaskEventCollector{
		events: make([]TaskEventData, 0),
	}
}

// AddEvent adds a task event.
func (c *TaskEventCollector) AddEvent(data TaskEventData) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, data)
}

// Events returns all collected events.
func (c *TaskEventCollector) Events() []TaskEventData {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]TaskEventData, len(c.events))
	copy(out, c.events)
	return out
}

// WithTaskEventCollector attaches a TaskEventCollector to the context.
func WithTaskEventCollector(ctx context.Context, collector *TaskEventCollector) context.Context {
	return context.WithValue(ctx, taskEventKey{}, collector)
}

// getTaskEventCollector retrieves the collector from context.
func getTaskEventCollector(ctx context.Context) *TaskEventCollector {
	if c, ok := ctx.Value(taskEventKey{}).(*TaskEventCollector); ok {
		return c
	}
	return nil
}

// sendTaskEvent records a task event via context collector.
func sendTaskEvent(ctx context.Context, eventType string, taskID string, data map[string]any) {
	collector := getTaskEventCollector(ctx)
	if collector == nil {
		return
	}

	if data == nil {
		data = make(map[string]any)
	}
	data["task_id"] = taskID
	data["type"] = eventType
	data["timestamp"] = time.Now().UnixMilli()

	collector.AddEvent(TaskEventData{
		Type:      eventType,
		TaskID:    taskID,
		Timestamp: data["timestamp"].(int64),
	})
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
