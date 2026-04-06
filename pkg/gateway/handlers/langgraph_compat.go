// Package handlers provides HTTP handlers for the GoClaw gateway.
package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/cloudwego/eino/schema"

	"github.com/bookerbai/goclaw/internal/agent"
)

// LangGraphSSEEvent represents an SSE event in LangGraph SDK format.
// The LangGraph SDK expects events with specific structure that differs
// from GoClaw's native event format.
type LangGraphSSEEvent struct {
	// Event is the SSE event type (e.g., "metadata", "values", "messages", "error", "end").
	Event string `json:"event"`
	// Data is the JSON-encoded payload for this event.
	Data any `json:"data"`
}

// LangGraphMetadataEvent is sent as the first event in a stream.
type LangGraphMetadataEvent struct {
	RunID    string `json:"run_id"`
	ThreadID string `json:"thread_id"`
}

// LangGraphValuesEvent represents a full state snapshot.
// This is what the SDK receives when stream_mode includes "values".
type LangGraphValuesEvent struct {
	Messages []LangGraphMessage `json:"messages,omitempty"`
	Title    string             `json:"title,omitempty"`
	// Additional state fields can be added here.
}

// LangGraphMessage represents a message in LangGraph format.
// The SDK expects messages with specific structure including type, content, and additional_kwargs.
type LangGraphMessage struct {
	Type             string              `json:"type"`    // "human", "ai", "tool", "system"
	Content          any                 `json:"content"` // string or []ContentPart
	ID               string              `json:"id,omitempty"`
	Name             string              `json:"name,omitempty"`
	AdditionalKwargs map[string]any      `json:"additional_kwargs,omitempty"`
	ToolCalls        []LangGraphToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string              `json:"tool_call_id,omitempty"`
}

// LangGraphContentPart represents a content part for multimodal messages.
type LangGraphContentPart struct {
	Type     string `json:"type"` // "text", "image"
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
}

// LangGraphToolCall represents a tool call in LangGraph message format.
type LangGraphToolCall struct {
	ID   string         `json:"id"`
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
	Type string         `json:"type"` // always "tool_call"
}

// LangGraphCustomEvent represents a custom event (task_running, llm_retry, etc.).
type LangGraphCustomEvent struct {
	Type    string `json:"type"`
	TaskID  string `json:"task_id,omitempty"`
	Message string `json:"message,omitempty"`
}

// LangGraphErrorEvent represents an error in the stream.
type LangGraphErrorEvent struct {
	Message string `json:"message"`
	Name    string `json:"name,omitempty"`
}

// LangGraphEventConverter converts GoClaw native events to LangGraph SDK format.
type LangGraphEventConverter struct {
	threadID   string
	runID      string
	messages   []LangGraphMessage
	title      string
	streamMode string // "values", "messages", or "updates"
}

// NewLangGraphEventConverter creates a new converter for a streaming session.
func NewLangGraphEventConverter(threadID, runID, streamMode string) *LangGraphEventConverter {
	return &LangGraphEventConverter{
		threadID:   threadID,
		runID:      runID,
		messages:   make([]LangGraphMessage, 0),
		streamMode: streamMode,
	}
}

// ConvertMetadataEvent creates the initial metadata event.
func (c *LangGraphEventConverter) ConvertMetadataEvent() LangGraphSSEEvent {
	return LangGraphSSEEvent{
		Event: "metadata",
		Data: LangGraphMetadataEvent{
			RunID:    c.runID,
			ThreadID: c.threadID,
		},
	}
}

// Convert converts a GoClaw agent.Event to LangGraph SSE events.
// It may return multiple events for a single GoClaw event (e.g., message + tool_call).
func (c *LangGraphEventConverter) Convert(ev agent.Event) []LangGraphSSEEvent {
	switch ev.Type {
	case agent.EventMessageDelta:
		return c.convertMessageDelta(ev)
	case agent.EventToolEvent:
		return c.convertToolEvent(ev)
	case agent.EventTaskStarted, agent.EventTaskRunning, agent.EventTaskCompleted, agent.EventTaskFailed, agent.EventTaskTimedOut:
		return c.convertTaskEvent(ev)
	case agent.EventCompleted:
		return c.convertCompleted(ev)
	case agent.EventError:
		return c.convertError(ev)
	default:
		return nil
	}
}

// convertMessageDelta handles message_delta events.
func (c *LangGraphEventConverter) convertMessageDelta(ev agent.Event) []LangGraphSSEEvent {
	payload, ok := ev.Payload.(agent.MessageDeltaPayload)
	if !ok {
		return nil
	}

	// Append to messages array for values mode.
	msg := LangGraphMessage{
		Type:    "ai",
		Content: payload.Content,
		ID:      fmt.Sprintf("msg-%d", ev.Timestamp),
	}
	if payload.IsThinking {
		msg.AdditionalKwargs = map[string]any{"thinking": true}
	}

	// In "messages" stream mode, emit message chunks.
	if c.streamMode == "messages" {
		return []LangGraphSSEEvent{{
			Event: "messages",
			Data:  c.formatMessageTuple(msg, ev),
		}}
	}

	// In "values" mode, accumulate and emit full state.
	c.messages = append(c.messages, msg)
	return []LangGraphSSEEvent{{
		Event: "values",
		Data: LangGraphValuesEvent{
			Messages: c.messages,
			Title:    c.title,
		},
	}}
}

// convertToolEvent handles tool_event events.
func (c *LangGraphEventConverter) convertToolEvent(ev agent.Event) []LangGraphSSEEvent {
	payload, ok := ev.Payload.(agent.ToolEventPayload)
	if !ok {
		return nil
	}

	var events []LangGraphSSEEvent

	// If we have input, it's a tool call from the assistant.
	if payload.Input != "" {
		// Parse the input as JSON args.
		var args map[string]any
		_ = json.Unmarshal([]byte(payload.Input), &args)

		// Create an AI message with tool_call.
		aiMsg := LangGraphMessage{
			Type:    "ai",
			ID:      fmt.Sprintf("msg-toolcall-%d", ev.Timestamp),
			Content: "",
			ToolCalls: []LangGraphToolCall{{
				ID:   payload.CallID,
				Name: payload.ToolName,
				Args: args,
				Type: "tool_call",
			}},
		}
		c.messages = append(c.messages, aiMsg)

		events = append(events, LangGraphSSEEvent{
			Event: "values",
			Data: LangGraphValuesEvent{
				Messages: c.messages,
				Title:    c.title,
			},
		})
	}

	// If we have output, it's a tool result.
	if payload.Output != "" {
		toolMsg := LangGraphMessage{
			Type:       "tool",
			ID:         fmt.Sprintf("msg-toolresult-%d", ev.Timestamp),
			Content:    payload.Output,
			ToolCallID: payload.CallID,
			Name:       payload.ToolName,
		}
		if payload.IsError {
			toolMsg.AdditionalKwargs = map[string]any{"is_error": true}
		}
		c.messages = append(c.messages, toolMsg)

		events = append(events, LangGraphSSEEvent{
			Event: "values",
			Data: LangGraphValuesEvent{
				Messages: c.messages,
				Title:    c.title,
			},
		})
	}

	return events
}

// convertTaskEvent handles task_* events as custom events.
func (c *LangGraphEventConverter) convertTaskEvent(ev agent.Event) []LangGraphSSEEvent {
	// Handle both TaskPayload and map[string]any formats
	var taskID, description, message string
	var result, errorMsg string

	switch payload := ev.Payload.(type) {
	case agent.TaskPayload:
		taskID = payload.TaskID
		description = payload.Subject
	case map[string]any:
		if v, ok := payload["task_id"].(string); ok {
			taskID = v
		}
		if v, ok := payload["description"].(string); ok {
			description = v
		}
		if v, ok := payload["message"].(map[string]any); ok {
			if content, ok := v["content"].(string); ok {
				message = content
			}
		}
		if v, ok := payload["result"].(string); ok {
			result = v
		}
		if v, ok := payload["error"].(string); ok {
			errorMsg = v
		}
	default:
		return nil
	}

	data := LangGraphCustomEvent{
		Type:    string(ev.Type),
		TaskID:  taskID,
		Message: description,
	}

	// Add additional fields for different event types
	if ev.Type == agent.EventTaskRunning && message != "" {
		data.Message = message
	}
	if ev.Type == agent.EventTaskCompleted && result != "" {
		data.Message = result
	}
	if ev.Type == agent.EventTaskFailed && errorMsg != "" {
		data.Message = errorMsg
	}

	return []LangGraphSSEEvent{{
		Event: "custom",
		Data:  data,
	}}
}

// convertCompleted handles the completed terminal event.
func (c *LangGraphEventConverter) convertCompleted(ev agent.Event) []LangGraphSSEEvent {
	// Emit final values snapshot then end event.
	events := []LangGraphSSEEvent{
		{
			Event: "values",
			Data: LangGraphValuesEvent{
				Messages: c.messages,
				Title:    c.title,
			},
		},
		{
			Event: "end",
			Data: map[string]any{
				"run_id":    c.runID,
				"thread_id": c.threadID,
			},
		},
	}
	return events
}

// convertError handles error terminal event.
func (c *LangGraphEventConverter) convertError(ev agent.Event) []LangGraphSSEEvent {
	payload, ok := ev.Payload.(agent.ErrorPayload)
	if !ok {
		// Fallback for unknown payload format.
		return []LangGraphSSEEvent{{
			Event: "error",
			Data: LangGraphErrorEvent{
				Message: "unknown error",
			},
		}}
	}

	return []LangGraphSSEEvent{{
		Event: "error",
		Data: LangGraphErrorEvent{
			Message: payload.Message,
			Name:    payload.Code,
		},
	}}
}

// formatMessageTuple formats a message for "messages" stream mode.
// In this mode, the SDK expects a tuple-like structure.
func (c *LangGraphEventConverter) formatMessageTuple(msg LangGraphMessage, ev agent.Event) any {
	return []any{msg, map[string]any{
		"timestamp": ev.Timestamp,
		"run_id":    c.runID,
	}}
}

// WriteLangGraphSSE writes a LangGraph SSE event to the writer.
// Format: `event: <event_type>\ndata: <json>\n\n`
func WriteLangGraphSSE(w io.Writer, ev LangGraphSSEEvent) error {
	data, err := json.Marshal(ev.Data)
	if err != nil {
		return fmt.Errorf("marshal SSE data: %w", err)
	}

	// Write event type line.
	if _, err := fmt.Fprintf(w, "event: %s\n", ev.Event); err != nil {
		return fmt.Errorf("write SSE event type: %w", err)
	}

	// Write data line.
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return fmt.Errorf("write SSE data: %w", err)
	}

	return nil
}

// WriteSSEHeartbeat writes an SSE heartbeat (comment) to keep the connection alive.
// Format: `: heartbeat\n\n`
// This is an SSE comment that browsers ignore but keeps the connection active.
func WriteSSEHeartbeat(w io.Writer) error {
	if _, err := fmt.Fprint(w, ": heartbeat\n\n"); err != nil {
		return fmt.Errorf("write SSE heartbeat: %w", err)
	}
	return nil
}

// HeartbeatInterval is the default interval between heartbeat messages.
// Aligned with DeerFlow's 15 second interval.
const HeartbeatInterval = 15 * time.Second

// ConvertSchemaMessageToLangGraph converts a schema.Message to LangGraph message format.
func ConvertSchemaMessageToLangGraph(msg *schema.Message) LangGraphMessage {
	if msg == nil {
		return LangGraphMessage{}
	}

	lgMsg := LangGraphMessage{
		Content: msg.Content,
		ID:      fmt.Sprintf("msg-%d", time.Now().UnixMilli()),
		Name:    msg.Name,
	}

	// Map role types.
	switch msg.Role {
	case schema.User:
		lgMsg.Type = "human"
	case schema.Assistant:
		lgMsg.Type = "ai"
	case schema.Tool:
		lgMsg.Type = "tool"
		lgMsg.ToolCallID = msg.ToolCallID
		lgMsg.Name = msg.ToolName
	case schema.System:
		lgMsg.Type = "system"
	default:
		lgMsg.Type = string(msg.Role)
	}

	// Convert tool calls.
	if len(msg.ToolCalls) > 0 {
		lgMsg.ToolCalls = make([]LangGraphToolCall, len(msg.ToolCalls))
		for i, tc := range msg.ToolCalls {
			var args map[string]any
			_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
			lgMsg.ToolCalls[i] = LangGraphToolCall{
				ID:   tc.ID,
				Name: tc.Function.Name,
				Args: args,
				Type: "tool_call",
			}
		}
	}

	return lgMsg
}
