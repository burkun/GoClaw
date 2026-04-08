package handlers

import (
	"bytes"
	"testing"
)

func TestWriteSSE(t *testing.T) {
	var buf bytes.Buffer
	event := SSEEvent{
		Type:         "message_delta",
		ThreadID:     "thread-1",
		RunID:        "run-1",
		CheckpointID: "cp-1",
		Payload:      map[string]string{"content": "hello"},
		Timestamp:    1234567890,
	}

	err := writeSSE(&buf, event)
	if err != nil {
		t.Fatalf("writeSSE failed: %v", err)
	}

	output := buf.String()
	if !bytes.Contains([]byte(output), []byte("data: ")) {
		t.Errorf("expected SSE data prefix, got %s", output)
	}
	if !bytes.Contains([]byte(output), []byte("message_delta")) {
		t.Errorf("expected event type in output, got %s", output)
	}
}

func TestWriteSSE_WithEventID(t *testing.T) {
	var buf bytes.Buffer
	event := SSEEvent{
		Type:         "completed",
		ThreadID:     "thread-1",
		RunID:        "run-1",
		CheckpointID: "cp-1",
		EventID:      "event-123",
		Payload:      map[string]string{"message": "done"},
		Timestamp:    1234567890,
	}

	err := writeSSE(&buf, event)
	if err != nil {
		t.Fatalf("writeSSE failed: %v", err)
	}

	output := buf.String()
	if !bytes.Contains([]byte(output), []byte("id: event-123")) {
		t.Errorf("expected event ID in output, got %s", output)
	}
	if !bytes.Contains([]byte(output), []byte("data: ")) {
		t.Errorf("expected SSE data prefix, got %s", output)
	}
}

func TestSSENow(t *testing.T) {
	ts := sseNow()
	if ts == 0 {
		t.Error("expected non-zero timestamp")
	}
}
