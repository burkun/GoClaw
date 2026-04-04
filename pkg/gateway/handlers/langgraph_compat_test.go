package handlers

import (
	"testing"

	"github.com/bookerbai/goclaw/internal/agent"
)

func TestLangGraphEventConverter_Metadata(t *testing.T) {
	converter := NewLangGraphEventConverter("thread-123", "run-456", "values")

	ev := converter.ConvertMetadataEvent()
	if ev.Event != "metadata" {
		t.Fatalf("expected event type 'metadata', got %q", ev.Event)
	}

	data, ok := ev.Data.(LangGraphMetadataEvent)
	if !ok {
		t.Fatalf("expected LangGraphMetadataEvent, got %T", ev.Data)
	}

	if data.ThreadID != "thread-123" {
		t.Errorf("expected thread_id 'thread-123', got %q", data.ThreadID)
	}
	if data.RunID != "run-456" {
		t.Errorf("expected run_id 'run-456', got %q", data.RunID)
	}
}

func TestLangGraphEventConverter_MessageDelta(t *testing.T) {
	converter := NewLangGraphEventConverter("thread-1", "run-1", "values")

	ev := agent.Event{
		Type:      agent.EventMessageDelta,
		ThreadID:  "thread-1",
		Timestamp: 1000,
		Payload: agent.MessageDeltaPayload{
			Content:     "Hello",
			IsThinking:  false,
		},
	}

	events := converter.Convert(ev)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	if events[0].Event != "values" {
		t.Errorf("expected event type 'values', got %q", events[0].Event)
	}

	data, ok := events[0].Data.(LangGraphValuesEvent)
	if !ok {
		t.Fatalf("expected LangGraphValuesEvent, got %T", events[0].Data)
	}

	if len(data.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(data.Messages))
	}

	if data.Messages[0].Content != "Hello" {
		t.Errorf("expected content 'Hello', got %q", data.Messages[0].Content)
	}
	if data.Messages[0].Type != "ai" {
		t.Errorf("expected type 'ai', got %q", data.Messages[0].Type)
	}
}

func TestLangGraphEventConverter_ToolEvent(t *testing.T) {
	converter := NewLangGraphEventConverter("thread-1", "run-1", "values")

	// Test tool call.
	ev := agent.Event{
		Type:      agent.EventToolEvent,
		ThreadID:  "thread-1",
		Timestamp: 1000,
		Payload: agent.ToolEventPayload{
			CallID:   "call-1",
			ToolName: "bash",
			Input:    `{"command": "ls"}`,
		},
	}

	events := converter.Convert(ev)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	data, ok := events[0].Data.(LangGraphValuesEvent)
	if !ok {
		t.Fatalf("expected LangGraphValuesEvent, got %T", events[0].Data)
	}

	if len(data.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(data.Messages))
	}

	if len(data.Messages[0].ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(data.Messages[0].ToolCalls))
	}

	if data.Messages[0].ToolCalls[0].Name != "bash" {
		t.Errorf("expected tool name 'bash', got %q", data.Messages[0].ToolCalls[0].Name)
	}

	// Test tool result.
	ev2 := agent.Event{
		Type:      agent.EventToolEvent,
		ThreadID:  "thread-1",
		Timestamp: 2000,
		Payload: agent.ToolEventPayload{
			CallID:   "call-1",
			ToolName: "bash",
			Output:   "file1.txt\nfile2.txt",
		},
	}

	events2 := converter.Convert(ev2)
	if len(events2) != 1 {
		t.Fatalf("expected 1 event for tool result, got %d", len(events2))
	}

	data2, ok := events2[0].Data.(LangGraphValuesEvent)
	if !ok {
		t.Fatalf("expected LangGraphValuesEvent, got %T", events2[0].Data)
	}

	// Should have 2 messages now (tool call + tool result).
	if len(data2.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(data2.Messages))
	}

	// Last message should be tool result.
	lastMsg := data2.Messages[1]
	if lastMsg.Type != "tool" {
		t.Errorf("expected type 'tool', got %q", lastMsg.Type)
	}
	if lastMsg.ToolCallID != "call-1" {
		t.Errorf("expected tool_call_id 'call-1', got %q", lastMsg.ToolCallID)
	}
}

func TestLangGraphEventConverter_TaskEvent(t *testing.T) {
	converter := NewLangGraphEventConverter("thread-1", "run-1", "values")

	ev := agent.Event{
		Type:      agent.EventTaskRunning,
		ThreadID:  "thread-1",
		Timestamp: 1000,
		Payload: agent.TaskPayload{
			TaskID:  "task-1",
			Subject: "Do something",
			Status:  "in_progress",
		},
	}

	events := converter.Convert(ev)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	if events[0].Event != "custom" {
		t.Errorf("expected event type 'custom', got %q", events[0].Event)
	}

	data, ok := events[0].Data.(LangGraphCustomEvent)
	if !ok {
		t.Fatalf("expected LangGraphCustomEvent, got %T", events[0].Data)
	}

	if data.Type != "task_running" {
		t.Errorf("expected type 'task_running', got %q", data.Type)
	}
	if data.TaskID != "task-1" {
		t.Errorf("expected task_id 'task-1', got %q", data.TaskID)
	}
}

func TestLangGraphEventConverter_Error(t *testing.T) {
	converter := NewLangGraphEventConverter("thread-1", "run-1", "values")

	ev := agent.Event{
		Type:      agent.EventError,
		ThreadID:  "thread-1",
		Timestamp: 1000,
		Payload: agent.ErrorPayload{
			Code:    "agent/model_error",
			Message: "Rate limit exceeded",
		},
	}

	events := converter.Convert(ev)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	if events[0].Event != "error" {
		t.Errorf("expected event type 'error', got %q", events[0].Event)
	}

	data, ok := events[0].Data.(LangGraphErrorEvent)
	if !ok {
		t.Fatalf("expected LangGraphErrorEvent, got %T", events[0].Data)
	}

	if data.Message != "Rate limit exceeded" {
		t.Errorf("expected message 'Rate limit exceeded', got %q", data.Message)
	}
}

func TestLangGraphEventConverter_Completed(t *testing.T) {
	converter := NewLangGraphEventConverter("thread-1", "run-1", "values")

	ev := agent.Event{
		Type:      agent.EventCompleted,
		ThreadID:  "thread-1",
		Timestamp: 1000,
		Payload: agent.CompletedPayload{
			FinalMessage: "Done",
		},
	}

	events := converter.Convert(ev)
	if len(events) != 2 {
		t.Fatalf("expected 2 events (values + end), got %d", len(events))
	}

	if events[0].Event != "values" {
		t.Errorf("expected first event type 'values', got %q", events[0].Event)
	}
	if events[1].Event != "end" {
		t.Errorf("expected second event type 'end', got %q", events[1].Event)
	}
}

func TestLangGraphEventConverter_MessagesMode(t *testing.T) {
	// In "messages" mode, message deltas should be emitted as "messages" events.
	converter := NewLangGraphEventConverter("thread-1", "run-1", "messages")

	ev := agent.Event{
		Type:      agent.EventMessageDelta,
		ThreadID:  "thread-1",
		Timestamp: 1000,
		Payload: agent.MessageDeltaPayload{
			Content: "Hello",
		},
	}

	events := converter.Convert(ev)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	// In messages mode, the event type should be "messages", not "values".
	if events[0].Event != "messages" {
		t.Errorf("expected event type 'messages', got %q", events[0].Event)
	}
}
