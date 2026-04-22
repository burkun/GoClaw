// Package agent defines the lead agent and its event types for GoClaw.
package agent

// EventType identifies the kind of event emitted by the lead agent during a run.
type EventType string

const (
	// EventMessageDelta is emitted for each incremental token/chunk from the assistant.
	EventMessageDelta EventType = "message_delta"

	// EventToolEvent is emitted when a tool is called or returns a result.
	EventToolEvent EventType = "tool_event"

	// EventCompleted is the terminal success event for a run.
	EventCompleted EventType = "completed"

	// EventError is the terminal failure event for a run.
	EventError EventType = "error"

	// EventTaskStarted is emitted when a subagent task starts.
	EventTaskStarted EventType = "task_started"

	// EventTaskRunning is emitted when a subagent task has progress (new AI message).
	EventTaskRunning EventType = "task_running"

	// EventTaskCompleted is emitted when a subagent task finishes successfully.
	EventTaskCompleted EventType = "task_completed"

	// EventTaskFailed is emitted when a subagent task fails.
	EventTaskFailed EventType = "task_failed"

	// EventTaskTimedOut is emitted when a subagent task times out.
	EventTaskTimedOut EventType = "task_timed_out"

	// EventTitle is emitted when the conversation title is generated.
	EventTitle EventType = "title"
)

// Event is the envelope sent over the channel returned by LeadAgent.Run.
// Every event carries the thread ID, optional run ID, and a typed payload.
type Event struct {
	// Type identifies the kind of event.
	Type EventType `json:"type"`

	// ThreadID is the conversation/thread identifier this event belongs to.
	ThreadID string `json:"thread_id"`

	// RunID identifies the current run instance (set by handler/agent layer).
	RunID string `json:"run_id,omitempty"`

	// Payload holds the event-specific data. Callers should type-assert based on Type.
	Payload any `json:"payload"`

	// Timestamp is the wall-clock time when the event was created (Unix milliseconds).
	Timestamp int64 `json:"timestamp"`
}

// --- Payload types ---

// MessageDeltaPayload carries an incremental text chunk from the model.
type MessageDeltaPayload struct {
	// Content is the partial text content of the assistant message.
	Content string `json:"content"`
	// IsThinking indicates whether this delta is from an internal reasoning step.
	IsThinking bool `json:"is_thinking,omitempty"`
}

// ToolEventPayload carries information about a tool invocation.
type ToolEventPayload struct {
	// CallID is the unique identifier for this tool call (matches tool result).
	CallID string `json:"call_id"`
	// ToolName is the name of the tool being invoked.
	ToolName string `json:"tool_name"`
	// Input is the JSON-encoded arguments passed to the tool.
	Input string `json:"input,omitempty"`
	// Output is the JSON-encoded result returned by the tool.
	// Empty while the tool is still running.
	Output string `json:"output,omitempty"`
	// IsError indicates the tool returned an error result.
	IsError bool `json:"is_error,omitempty"`
}

// CompletedPayload carries the final outcome of a successful run.
type CompletedPayload struct {
	// FinalMessage is the last assistant message produced.
	FinalMessage string `json:"final_message"`
	// Title is the auto-generated conversation title.
	Title string `json:"title,omitempty"`
}

// TitlePayload carries the title when it's generated.
type TitlePayload struct {
	// Title is the auto-generated conversation title.
	Title string `json:"title"`
}

// ErrorPayload carries diagnostic information when a run fails.
type ErrorPayload struct {
	// Code is a stable error code prefix (e.g. "agent/model_error").
	Code string `json:"code"`
	// Message is a human-readable description.
	Message string `json:"message"`
}

// TaskPayload carries state for plan-mode task tracking events.
type TaskPayload struct {
	// TaskID is the unique identifier of the task within the current run.
	TaskID string `json:"task_id"`
	// Subject is the short title of the task.
	Subject string `json:"subject"`
	// Status is one of "pending", "in_progress", "completed", "failed".
	Status string `json:"status"`
}
