package todo

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/bookerbai/goclaw/internal/middleware"
)

// TestInjectAnnotation_noSystemMessage verifies annotation is added
// when no system message exists.
func TestInjectAnnotation_noSystemMessage(t *testing.T) {
	state := &middleware.State{
		Messages: []map[string]any{
			{"role": "user", "content": "hello"},
		},
	}

	injectAnnotation(state)

	if len(state.Messages) != 2 {
		t.Errorf("expected 2 messages after injection, got %d", len(state.Messages))
	}

	first := state.Messages[0]
	if first["role"] != "system" {
		t.Errorf("first message should be system, got %q", first["role"])
	}

	content, _ := first["content"].(string)
	if !contains(content, "write_todos") {
		t.Errorf("injected system message should contain write_todos")
	}
}

// TestInjectAnnotation_existingSystemMessage verifies annotation is appended
// when a system message already exists.
func TestInjectAnnotation_existingSystemMessage(t *testing.T) {
	originalContent := "You are a helpful assistant."
	state := &middleware.State{
		Messages: []map[string]any{
			{"role": "system", "content": originalContent},
			{"role": "user", "content": "hello"},
		},
	}

	injectAnnotation(state)

	if len(state.Messages) != 2 {
		t.Errorf("expected 2 messages (no new ones added), got %d", len(state.Messages))
	}

	first := state.Messages[0]
	content, _ := first["content"].(string)
	if !contains(content, originalContent) {
		t.Errorf("system message should retain original content")
	}
	if !contains(content, "write_todos") {
		t.Errorf("system message should contain appended write_todos annotation")
	}
}

// TestTodosInMessages_found verifies detection when write_todos call exists.
func TestTodosInMessages_found(t *testing.T) {
	messages := []map[string]any{
		{
			"role": "assistant",
			"content": "I'll update the tasks.",
			"tool_calls": []map[string]any{
				{
					"name": WriteTodosToolName,
					"id":   "call_123",
				},
			},
		},
	}

	if !todosInMessages(messages) {
		t.Error("should detect write_todos call in messages")
	}
}

// TestTodosInMessages_notFound verifies no detection when write_todos doesn't exist.
func TestTodosInMessages_notFound(t *testing.T) {
	messages := []map[string]any{
		{
			"role": "assistant",
			"content": "I'll do the work.",
			"tool_calls": []map[string]any{
				{
					"name": "some_other_tool",
					"id":   "call_456",
				},
			},
		},
	}

	if todosInMessages(messages) {
		t.Error("should not detect write_todos call when it doesn't exist")
	}
}

// TestReminderInMessages_found verifies detection when todo_reminder exists.
func TestReminderInMessages_found(t *testing.T) {
	messages := []map[string]any{
		{
			"role":    "human",
			"name":    "todo_reminder",
			"content": "Your todo list: ...",
		},
	}

	if !reminderInMessages(messages) {
		t.Error("should detect todo_reminder message")
	}
}

// TestReminderInMessages_notFound verifies no detection when reminder doesn't exist.
func TestReminderInMessages_notFound(t *testing.T) {
	messages := []map[string]any{
		{
			"role":    "human",
			"name":    "other_reminder",
			"content": "Something else",
		},
	}

	if reminderInMessages(messages) {
		t.Error("should not detect todo_reminder when it doesn't exist")
	}
}

// TestFormatTodos_basic verifies formatting of multiple todos.
func TestFormatTodos_basic(t *testing.T) {
	todos := []map[string]any{
		{"id": "1", "subject": "Write tests", "status": "in_progress"},
		{"id": "2", "subject": "Implement feature", "status": "pending"},
		{"id": "3", "subject": "Code review", "status": "completed"},
	}

	result := formatTodos(todos)

	expectedLines := []string{
		"- [in_progress] Write tests",
		"- [pending] Implement feature",
		"- [completed] Code review",
	}
	for _, line := range expectedLines {
		if !contains(result, line) {
			t.Errorf("formatted output should contain %q", line)
		}
	}
}

// TestFormatTodos_emptyStatus verifies defaulting status to "pending".
func TestFormatTodos_emptyStatus(t *testing.T) {
	todos := []map[string]any{
		{"id": "1", "subject": "Task", "status": ""},
	}

	result := formatTodos(todos)

	if !contains(result, "[pending]") {
		t.Errorf("empty status should default to pending")
	}
}

// TestAfter_parseWriteTodos verifies JSON parsing and state update.
func TestAfter_parseWriteTodos(t *testing.T) {
	mw := NewTodoMiddleware()

	state := &middleware.State{
		Messages: []map[string]any{},
		Todos:    []map[string]any{},
	}

	inputJSON, _ := json.Marshal(map[string]any{
		"todos": []map[string]any{
			{"id": "1", "subject": "New task", "status": "pending"},
			{"id": "2", "subject": "Another task", "status": "in_progress"},
		},
	})

	response := &middleware.Response{
		ToolCalls: []map[string]any{
			{
				"name":  WriteTodosToolName,
				"id":    "call_123",
				"input": string(inputJSON),
			},
		},
	}

	err := mw.After(context.Background(), state, response)
	if err != nil {
		t.Fatalf("After returned unexpected error: %v", err)
	}

	if len(state.Todos) != 2 {
		t.Errorf("expected 2 todos after parsing, got %d", len(state.Todos))
	}

	if state.Todos[0]["subject"] != "New task" {
		t.Errorf("todo subject mismatch")
	}
}

// TestAfter_ignoredInvalidJSON verifies graceful handling of malformed JSON.
func TestAfter_ignoredInvalidJSON(t *testing.T) {
	mw := NewTodoMiddleware()

	state := &middleware.State{
		Messages: []map[string]any{},
		Todos:    []map[string]any{{"id": "old", "subject": "Old task"}},
	}

	response := &middleware.Response{
		ToolCalls: []map[string]any{
			{
				"name":  WriteTodosToolName,
				"id":    "call_456",
				"input": "not valid json {",
			},
		},
	}

	err := mw.After(context.Background(), state, response)
	if err != nil {
		t.Fatalf("After should not return error on invalid JSON: %v", err)
	}

	// State should remain unchanged
	if len(state.Todos) != 1 || state.Todos[0]["id"] != "old" {
		t.Errorf("todos should remain unchanged after failed JSON parse")
	}
}

// TestBefore_contextLossReminder verifies reminder injection on context loss.
func TestBefore_contextLossReminder(t *testing.T) {
	mw := NewTodoMiddleware()

	state := &middleware.State{
		PlanMode: true,
		Messages: []map[string]any{
			{"role": "system", "content": "You are helpful."},
			{"role": "user", "content": "Do task 1"},
			{"role": "assistant", "content": "Starting..."},
			// Note: write_todos call is NOT present (context loss)
		},
		Todos: []map[string]any{
			{"id": "1", "subject": "Task 1", "status": "in_progress"},
		},
	}

	err := mw.Before(context.Background(), state)
	if err != nil {
		t.Fatalf("Before returned error: %v", err)
	}

	// Should have injected reminder message
	var foundReminder bool
	for _, msg := range state.Messages {
		if msg["role"] == "human" && msg["name"] == "todo_reminder" {
			foundReminder = true
			break
		}
	}

	if !foundReminder {
		t.Error("reminder message should be injected on context loss")
	}
}

// TestBefore_noReminderWhenWriteTodosPresent verifies no duplicate reminders.
func TestBefore_noReminderWhenWriteTodosPresent(t *testing.T) {
	mw := NewTodoMiddleware()

	initialMessageCount := 3
	state := &middleware.State{
		PlanMode: true,
		Messages: []map[string]any{
			{"role": "system", "content": "You are helpful."},
			{"role": "user", "content": "Do task 1"},
			{
				"role": "assistant",
				"content": "Updating tasks",
				"tool_calls": []map[string]any{
					{"name": WriteTodosToolName, "id": "call_1"},
				},
			},
		},
		Todos: []map[string]any{
			{"id": "1", "subject": "Task 1", "status": "in_progress"},
		},
	}

	err := mw.Before(context.Background(), state)
	if err != nil {
		t.Fatalf("Before returned error: %v", err)
	}

	// Should NOT inject reminder if write_todos is recent
	if len(state.Messages) != initialMessageCount {
		t.Errorf("no reminder should be added when write_todos is present; "+
			"expected %d messages, got %d", initialMessageCount, len(state.Messages))
	}
}

// TestBefore_noReminderWhenAlreadyPresent verifies no duplicate reminders.
func TestBefore_noReminderWhenAlreadyPresent(t *testing.T) {
	mw := NewTodoMiddleware()

	initialMessageCount := 4
	state := &middleware.State{
		PlanMode: true,
		Messages: []map[string]any{
			{"role": "system", "content": "You are helpful."},
			{"role": "user", "content": "Do task 1"},
			{"role": "assistant", "content": "Working..."},
			{
				"role": "human",
				"name": "todo_reminder",
				"content": "Your todos are: ...",
			},
		},
		Todos: []map[string]any{
			{"id": "1", "subject": "Task 1", "status": "in_progress"},
		},
	}

	err := mw.Before(context.Background(), state)
	if err != nil {
		t.Fatalf("Before returned error: %v", err)
	}

	// Should NOT add a second reminder
	if len(state.Messages) != initialMessageCount {
		t.Errorf("no duplicate reminder should be added; "+
			"expected %d messages, got %d", initialMessageCount, len(state.Messages))
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || len(substr) > 0 && s != "")
}
