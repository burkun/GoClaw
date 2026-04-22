// Package handlers provides HTTP handlers for the GoClaw gateway.
package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/cloudwego/eino/schema"

	"goclaw/internal/agent"
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
	CreatedAt        int64               `json:"created_at,omitempty"`
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
	case agent.EventTitle:
		return c.convertTitleEvent(ev)
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

	// In "messages" stream mode, emit message chunks.
	if c.streamMode == "messages" {
		msg := LangGraphMessage{
			Type:    "ai",
			Content: payload.Content,
			ID:      fmt.Sprintf("msg-%d", ev.Timestamp),
		}
		if payload.IsThinking {
			msg.AdditionalKwargs = map[string]any{
				"reasoning_content": payload.Content,
			}
			msg.Content = ""
		}
		return []LangGraphSSEEvent{{
			Event: "messages",
			Data:  c.formatMessageTuple(msg, ev),
		}}
	}

	// In "values" mode, accumulate content into the last AI message.
	// This provides a proper streaming experience where the AI message
	// content grows incrementally.

	// Check if we have an AI message to append to
	var lastAIMsg *LangGraphMessage
	if len(c.messages) > 0 {
		lastMsg := &c.messages[len(c.messages)-1]
		if lastMsg.Type == "ai" && len(lastMsg.ToolCalls) == 0 {
			lastAIMsg = lastMsg
		}
	}

	if payload.IsThinking {
		// Handle thinking/reasoning content
		if lastAIMsg == nil {
			// Create new AI message for thinking
			lastAIMsg = &LangGraphMessage{
				Type:             "ai",
				Content:          "",
				ID:               fmt.Sprintf("msg-%d", ev.Timestamp),
				AdditionalKwargs: map[string]any{"reasoning_content": payload.Content},
			}
			c.messages = append(c.messages, *lastAIMsg)
		} else {
			// Append to existing thinking content
			if lastAIMsg.AdditionalKwargs == nil {
				lastAIMsg.AdditionalKwargs = map[string]any{}
			}
			existing, _ := lastAIMsg.AdditionalKwargs["reasoning_content"].(string)
			lastAIMsg.AdditionalKwargs["reasoning_content"] = existing + payload.Content
		}
	} else {
		// Handle regular content
		if lastAIMsg == nil {
			// Create new AI message
			lastAIMsg = &LangGraphMessage{
				Type:    "ai",
				Content: payload.Content,
				ID:      fmt.Sprintf("msg-%d", ev.Timestamp),
			}
			c.messages = append(c.messages, *lastAIMsg)
		} else {
			// Append to existing content
			existing, _ := lastAIMsg.Content.(string)
			lastAIMsg.Content = existing + payload.Content
		}
	}

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

		// Check if the last message is an AI message we can attach tool_calls to.
		// This avoids creating duplicate AI messages.
		var aiMsg *LangGraphMessage
		if len(c.messages) > 0 {
			lastMsg := &c.messages[len(c.messages)-1]
			if lastMsg.Type == "ai" && lastMsg.Content == "" && len(lastMsg.ToolCalls) == 0 {
				// Reuse the empty AI message
				lastMsg.ToolCalls = []LangGraphToolCall{{
					ID:   payload.CallID,
					Name: payload.ToolName,
					Args: args,
					Type: "tool_call",
				}}
				aiMsg = lastMsg
			}
		}

		if aiMsg == nil {
			// Create a new AI message with tool_call.
			aiMsg = &LangGraphMessage{
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
			c.messages = append(c.messages, *aiMsg)
		}

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

// convertTitleEvent handles title events.
// It updates the converter's title and emits a values event with the title.
func (c *LangGraphEventConverter) convertTitleEvent(ev agent.Event) []LangGraphSSEEvent {
	payload, ok := ev.Payload.(agent.TitlePayload)
	if !ok {
		return nil
	}

	c.title = payload.Title

	// Emit values event with the title
	return []LangGraphSSEEvent{{
		Event: "values",
		Data: LangGraphValuesEvent{
			Messages: c.messages,
			Title:    c.title,
		},
	}}
}

// convertCompleted handles the completed terminal event.
func (c *LangGraphEventConverter) convertCompleted(ev agent.Event) []LangGraphSSEEvent {
	// Get title from CompletedPayload if not already set
	title := c.title
	if title == "" {
		if payload, ok := ev.Payload.(agent.CompletedPayload); ok && payload.Title != "" {
			title = payload.Title
			c.title = title
		}
	}

	// Emit final values snapshot then end event.
	events := []LangGraphSSEEvent{
		{
			Event: "values",
			Data: LangGraphValuesEvent{
				Messages: c.messages,
				Title:    title,
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
// It sends both an error event and an end event to properly terminate the SSE stream.
func (c *LangGraphEventConverter) convertError(ev agent.Event) []LangGraphSSEEvent {
	payload, ok := ev.Payload.(agent.ErrorPayload)
	var message string
	var code string
	if ok {
		message = payload.Message
		code = payload.Code
	} else {
		message = "unknown error"
	}

	return []LangGraphSSEEvent{
		{
			Event: "error",
			Data: LangGraphErrorEvent{
				Message: message,
				Name:    code,
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
