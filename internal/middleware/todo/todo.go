// Package todo implements TodoMiddleware for GoClaw.
//
// TodoMiddleware extends plan-mode behaviour by:
//  1. Injecting a write_todos pseudo-tool description into the system prompt
//     when PlanMode is active (so the model knows it can call write_todos).
//  2. Detecting context-loss: when State.Todos is non-empty but no write_todos
//     call is visible in the recent message history (e.g. it has been truncated
//     by SummarizationMiddleware), injecting a reminder message so the model
//     stays aware of its outstanding task list.
//  3. Parsing write_todos tool results from Response.ToolCalls (After) and
//     updating State.Todos accordingly.
//
// This mirrors DeerFlow's TodoMiddleware / TodoListMiddleware pattern.
package todo

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bookerbai/goclaw/internal/middleware"
)

// TodoStatus enumerates the allowed task lifecycle states.
type TodoStatus string

const (
	StatusPending    TodoStatus = "pending"
	StatusInProgress TodoStatus = "in_progress"
	StatusCompleted  TodoStatus = "completed"
	StatusFailed     TodoStatus = "failed"
)

// Todo represents a single task in the plan-mode task list.
type Todo struct {
	// ID is a stable identifier unique within the current run (e.g. "1", "2").
	ID string `json:"id"`

	// Subject is the short imperative description of the task.
	Subject string `json:"subject"`

	// Status is the current lifecycle state of the task.
	Status TodoStatus `json:"status"`
}

// writeToolsAnnotation is the pseudo-tool description injected into the system
// prompt when PlanMode is active. It tells the model how to call write_todos.
const writeToolsAnnotation = `## Available tool: write_todos

Use write_todos to create or update the task list for the current run.
Call it whenever tasks are created, started, completed, or failed.

Arguments (JSON):
  {
    "todos": [
      { "id": "<string>", "subject": "<short title>", "status": "pending|in_progress|completed|failed" }
    ]
  }

Rules:
- Always include ALL tasks, not just changed ones.
- Call write_todos immediately after changing any task status.
- Keep subjects concise (≤ 10 words).`

// reminderTemplate is used to inject a todo reminder when write_todos has
// scrolled out of the context window.
const reminderTemplate = `<system_reminder>
Your todo list from an earlier turn is no longer visible in the current context window,
but it is still active. Here is the current state:

%s

Continue tracking and updating this todo list as you work.
Call write_todos whenever the status of any item changes.
</system_reminder>`

// WriteTodosToolName is the stable name the model uses to call the pseudo-tool.
const WriteTodosToolName = "write_todos"

// TodoMiddleware manages the plan-mode task list and its visibility in context.
// It implements middleware.Middleware.
type TodoMiddleware struct {
	middleware.MiddlewareWrapper
}

// NewTodoMiddleware constructs a TodoMiddleware.
func NewTodoMiddleware() *TodoMiddleware { return &TodoMiddleware{} }

// Name implements middleware.Middleware.
func (t *TodoMiddleware) Name() string { return "TodoMiddleware" }

// BeforeModel runs before model invocation and handles two concerns:
//
// A. Tool injection (always when PlanMode is true):
//  1. If state.PlanMode is false, return nil (no-op).
//  2. Find or create the system message in state.Messages.
//  3. Append writeToolsAnnotation to the system message content so the model
//     knows write_todos is available.
//
// B. Context-loss reminder:
//  1. If state.Todos is empty, skip.
//  2. Check whether any recent message in state.Messages contains a
//     write_todos tool call (todosInMessages). If yes, skip.
//  3. Check whether a reminder message is already present (reminderInMessages).
//     If yes, skip (avoid duplicate injections).
//  4. Format the current Todos as a bullet list.
//  5. Inject a HumanMessage (role="human", name="todo_reminder") with the
//     formatted reminderTemplate content.
func (t *TodoMiddleware) BeforeModel(ctx context.Context, state *middleware.State) error {
	// --- Part A: tool annotation ---
	if state.PlanMode {
		// TODO: find system message and append writeToolsAnnotation.
		//       Create a system message if none exists.
		injectAnnotation(state)
	}

	// --- Part B: context-loss reminder ---
	if len(state.Todos) == 0 {
		return nil
	}

	if todosInMessages(state.Messages) {
		// write_todos call still visible — no reminder needed.
		return nil
	}

	if reminderInMessages(state.Messages) {
		// Reminder already present — avoid duplicate.
		return nil
	}

	// Inject reminder message.
	formatted := formatTodos(state.Todos)
	reminder := map[string]any{
		"role":    "human",
		"name":    "todo_reminder",
		"content": fmt.Sprintf(reminderTemplate, formatted),
	}
	state.Messages = append(state.Messages, reminder)

	return nil
}

// AfterModel parses write_todos tool results from response.ToolCalls and updates
// state.Todos to reflect the new task list.
//
// Implementation steps:
//  1. Iterate over response.ToolCalls.
//  2. For each call where name == WriteTodosToolName:
//     a. Parse the "input" JSON field into a struct with a "todos" array.
//     b. Convert each entry into a Todo.
//     c. Replace state.Todos with the new list.
//     d. Break (only the last write_todos call matters).
func (t *TodoMiddleware) AfterModel(ctx context.Context, state *middleware.State, response *middleware.Response) error {
	for _, call := range response.ToolCalls {
		name, _ := call["name"].(string)
		if name != WriteTodosToolName {
			continue
		}

		// Parse the input JSON.
		inputStr, _ := call["input"].(string)
		if inputStr == "" {
			break
		}

		var payload struct {
			Todos []map[string]any `json:"todos"`
		}
		if err := json.Unmarshal([]byte(inputStr), &payload); err != nil {
			// Parsing failed; skip this call.
			break
		}

		// Convert to state.Todos format.
		newTodos := make([]map[string]any, 0, len(payload.Todos))
		for _, t := range payload.Todos {
			newTodos = append(newTodos, t)
		}
		state.Todos = newTodos
		break
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

// injectAnnotation appends writeToolsAnnotation to the first system message.
// If no system message exists, one is prepended.
func injectAnnotation(state *middleware.State) {
	for i, msg := range state.Messages {
		if msg["role"] == "system" {
			existing, _ := msg["content"].(string)
			state.Messages[i]["content"] = existing + "\n\n" + writeToolsAnnotation
			return
		}
	}
	sysMsg := map[string]any{
		"role":    "system",
		"content": writeToolsAnnotation,
	}
	state.Messages = append([]map[string]any{sysMsg}, state.Messages...)
}

// todosInMessages returns true if any message in the list contains a
// write_todos tool call, indicating the call is still within the context window.
func todosInMessages(messages []map[string]any) bool {
	for _, msg := range messages {
		if msg["role"] != "assistant" {
			continue
		}
		calls, ok := msg["tool_calls"].([]map[string]any)
		if !ok {
			continue
		}
		for _, call := range calls {
			if call["name"] == WriteTodosToolName {
				return true
			}
		}
	}
	return false
}

// reminderInMessages returns true if a todo_reminder message is already present.
func reminderInMessages(messages []map[string]any) bool {
	for _, msg := range messages {
		if msg["role"] == "human" && msg["name"] == "todo_reminder" {
			return true
		}
	}
	return false
}

// formatTodos returns a human-readable bullet list of the current todos.
// Example output:
//
//   - [pending] Write unit tests
//   - [in_progress] Implement API handler
func formatTodos(todos []map[string]any) string {
	var lines []string
	for _, todo := range todos {
		status, _ := todo["status"].(string)
		subject, _ := todo["subject"].(string)
		if status == "" {
			status = string(StatusPending)
		}
		lines = append(lines, fmt.Sprintf("- [%s] %s", status, subject))
	}
	return strings.Join(lines, "\n")
}
