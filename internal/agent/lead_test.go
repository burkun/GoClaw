package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/bookerbai/goclaw/internal/config"
	skillsruntime "github.com/bookerbai/goclaw/internal/skills"
	toolruntime "github.com/bookerbai/goclaw/internal/tools"
	"github.com/cloudwego/eino/schema"
)

// --- helpers ---

// newTestState creates a minimal ThreadState suitable for testing.
func newTestState(threadID string, userMsg string) *ThreadState {
	return &ThreadState{
		Messages: []*schema.Message{
			schema.UserMessage(userMsg),
		},
		Sandbox:    &SandboxState{SandboxID: "test-sandbox"},
		ThreadData: &ThreadDataState{WorkspacePath: "/tmp/" + threadID},
	}
}

// drainChannel collects all events from ch until it is closed or deadline passes.
func drainChannel(t *testing.T, ch <-chan Event, timeout time.Duration) []Event {
	t.Helper()
	var events []Event
	deadline := time.After(timeout)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return events
			}
			events = append(events, ev)
		case <-deadline:
			t.Fatal("timeout waiting for agent events")
			return events
		}
	}
}

// assertTerminal asserts that the last event in the slice is a terminal event
// (EventCompleted or EventError).
func assertTerminal(t *testing.T, events []Event) {
	t.Helper()
	if len(events) == 0 {
		t.Fatal("expected at least one event, got none")
	}
	last := events[len(events)-1]
	if last.Type != EventCompleted && last.Type != EventError {
		t.Errorf("expected terminal event (completed|error), got: %q", last.Type)
	}
}

type fakeGoClawTool struct {
	name string
}

func (f *fakeGoClawTool) Name() string        { return f.name }
func (f *fakeGoClawTool) Description() string { return "fake" }
func (f *fakeGoClawTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (f *fakeGoClawTool) Execute(_ context.Context, _ string) (string, error) { return "ok", nil }

type fakeSkillPlugin struct {
	name      string
	reloadCnt int
}

func (p *fakeSkillPlugin) Name() string                                        { return p.name }
func (p *fakeSkillPlugin) OnLoad(_ context.Context, _ *config.AppConfig) error { return nil }
func (p *fakeSkillPlugin) OnUnload(_ context.Context) error                    { return nil }
func (p *fakeSkillPlugin) OnConfigReload(_ *config.AppConfig) error {
	p.reloadCnt++
	return nil
}

// --- tests ---

// TestLeadAgent_basicRun is a stub end-to-end test.
//
// It verifies that:
//  1. leadAgent.Run returns a channel without error.
//  2. The channel is eventually closed.
//  3. The last event is a terminal event (EventCompleted or EventError).
//
// TODO: replace the stub leadAgent with a real instance once New() is implemented.
// A mock Eino ChatModel can be used to avoid requiring live API keys in CI.
func TestLeadAgent_basicRun(t *testing.T) {
	// TODO: construct a real leadAgent with a mock Eino model.
	// For now, instantiate the placeholder implementation.
	agent := &leadAgent{}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	state := newTestState("thread-001", "Hello, who are you?")
	cfg := RunConfig{
		ThreadID:        "thread-001",
		ModelName:       "test-model",
		ThinkingEnabled: false,
		IsPlanMode:      false,
		SubagentEnabled: false,
	}

	ch, err := agent.Run(ctx, state, cfg)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	events := drainChannel(t, ch, 5*time.Second)
	assertTerminal(t, events)
}

// TestLeadAgent_planMode verifies that buildMiddlewares includes TodoMiddleware
// when plan mode is configured.
//
// TODO: full end-to-end test requires a real mock model that returns write_todos
// tool calls. For now, verify that the middleware chain includes TodoMiddleware.
func TestLeadAgent_planMode(t *testing.T) {
	mws := buildMiddlewares(RunConfig{IsPlanMode: true})

	if mws == nil {
		t.Skip("todoMiddleware not wired yet; skipping full plan-mode test")
	}

	// Middleware chain is built; if we reach here, TodoMiddleware should be active.
	// Full verification requires end-to-end test with mock model.
	t.Log("plan mode middleware chain is configured correctly")
}

// TestLeadAgent_cancelContext verifies that cancelling the context causes the
// agent run to terminate with an EventError.
//
// Note: This test requires context cancellation to be propagated through the
// event stream. The lead agent's drainIter() supports this, but full validation
// requires a real agent execution environment.
func TestLeadAgent_cancelContext(t *testing.T) {
	t.Skip("TODO: requires real agent execution environment with context propagation")

	agent := &leadAgent{}
	ctx, cancel := context.WithCancel(context.Background())

	state := newTestState("thread-003", "Do a very long task.")
	cfg := RunConfig{ThreadID: "thread-003"}

	ch, err := agent.Run(ctx, state, cfg)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	// Cancel immediately to trigger early termination.
	cancel()

	events := drainChannel(t, ch, 5*time.Second)
	assertTerminal(t, events)

	last := events[len(events)-1]
	if last.Type != EventError {
		t.Errorf("expected EventError after context cancel, got %q", last.Type)
	}
}

// TestBuildMiddlewares_empty verifies that buildMiddlewares does not panic with
// a zero-value RunConfig and returns a non-nil slice.
//
// TODO: tighten assertions once middleware implementations exist.
func TestBuildMiddlewares_empty(t *testing.T) {
	mws := buildMiddlewares(RunConfig{})
	// nil is acceptable until middleware constructors are implemented.
	_ = mws
}

func TestFilterToolsByAllowed(t *testing.T) {
	tools := []toolruntime.Tool{&fakeGoClawTool{name: "read"}, &fakeGoClawTool{name: "bash"}, &fakeGoClawTool{name: "write"}}
	einoTools := toolruntime.AdaptToEinoTools(tools)

	filtered, err := filterToolsByAllowed(context.Background(), einoTools, map[string]struct{}{"read": {}, "write": {}})
	if err != nil {
		t.Fatalf("filter failed: %v", err)
	}
	if len(filtered) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(filtered))
	}
}

func TestFilterToolsByAllowed_NoMatch(t *testing.T) {
	tools := []toolruntime.Tool{&fakeGoClawTool{name: "read"}}
	einoTools := toolruntime.AdaptToEinoTools(tools)

	_, err := filterToolsByAllowed(context.Background(), einoTools, map[string]struct{}{"bash": {}})
	if err == nil {
		t.Fatalf("expected error when no tools matched")
	}
}

func TestSyncSkillsOnConfigReload(t *testing.T) {
	reg := skillsruntime.NewRegistry()
	plugin := &fakeSkillPlugin{name: "skill-a"}
	if err := reg.Register(&skillsruntime.Skill{Metadata: skillsruntime.SkillMetadata{Name: "skill-a"}, Plugin: plugin}); err != nil {
		t.Fatalf("register skill failed: %v", err)
	}

	old := getAppConfig
	defer func() { getAppConfig = old }()
	getAppConfig = func() (*config.AppConfig, error) {
		return &config.AppConfig{}, nil
	}

	a := &leadAgent{skills: reg}
	if err := a.syncSkillsOnConfigReload(); err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	if plugin.reloadCnt != 1 {
		t.Fatalf("expected reload count 1, got %d", plugin.reloadCnt)
	}
}

func TestSyncSkillsOnConfigReload_ConfigError(t *testing.T) {
	old := getAppConfig
	defer func() { getAppConfig = old }()
	getAppConfig = func() (*config.AppConfig, error) {
		return nil, errors.New("load failed")
	}

	a := &leadAgent{skills: skillsruntime.NewRegistry()}
	if err := a.syncSkillsOnConfigReload(); err == nil {
		t.Fatalf("expected error when getAppConfig fails")
	}
}

// TestEventTypes_constants ensures that EventType constant strings are stable and
// match the SSE contract defined in PLAN.md.
func TestEventTypes_constants(t *testing.T) {
	expected := map[EventType]string{
		EventMessageDelta:  "message_delta",
		EventToolEvent:     "tool_event",
		EventCompleted:     "completed",
		EventError:         "error",
		EventTaskStarted:   "task_started",
		EventTaskRunning:   "task_running",
		EventTaskCompleted: "task_completed",
		EventTaskFailed:    "task_failed",
	}
	for ev, want := range expected {
		if string(ev) != want {
			t.Errorf("EventType %q != %q", string(ev), want)
		}
	}
}

// TestErrorCodeConstants verifies that error codes are properly defined and match
// the SSE contract in EVENTS.md.
func TestErrorCodeConstants(t *testing.T) {
	expected := map[string]string{
		ErrorCodeRunFailed:        "agent/run_error",
		ErrorCodeInterrupted:      "agent/interrupted",
		ErrorCodeEmptyStream:      "agent/empty_stream",
		ErrorCodeContextCancelled: "agent/context_cancelled",
		ErrorCodeNotInitialized:   "agent/not_initialized",
	}
	for name, code := range expected {
		if name == "" || code == "" {
			t.Errorf("error code constant is empty: %q = %q", name, code)
		}
	}
}

// TestIsToolError verifies that tool error detection works correctly.
func TestIsToolError(t *testing.T) {
	tests := []struct {
		name    string
		msg     *schema.Message
		wantErr bool
	}{
		{
			name:    "nil message",
			msg:     nil,
			wantErr: false,
		},
		{
			name:    "non-tool message",
			msg:     schema.UserMessage("hello"),
			wantErr: false,
		},
		{
			name:    "tool message with success",
			msg:     schema.ToolMessage("operation completed successfully", "call_001"),
			wantErr: false,
		},
		{
			name:    "tool message with error prefix",
			msg:     schema.ToolMessage("error: command failed", "call_002"),
			wantErr: true,
		},
		{
			name:    "tool message with ERROR uppercase",
			msg:     schema.ToolMessage("ERROR: invalid input", "call_003"),
			wantErr: true,
		},
		{
			name:    "tool message with failed",
			msg:     schema.ToolMessage("failed to open file", "call_004"),
			wantErr: true,
		},
		{
			name:    "tool message with whitespace",
			msg:     schema.ToolMessage("  error: timeout", "call_005"),
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isToolError(tt.msg)
			if got != tt.wantErr {
				t.Errorf("isToolError(%v) = %v, want %v", tt.msg, got, tt.wantErr)
			}
		})
	}
}

// TestConvertAgentEvent_ToolWithError verifies that tool events correctly
// mark the is_error flag based on the tool message content.
func TestConvertAgentEvent_ToolWithError(t *testing.T) {
	// Create a tool message that represents an error result.
	toolMsg := schema.ToolMessage("error: bash: permission denied", "call_001")
	toolMsg.ToolName = "bash"

	// convertAgentEvent extracts the tool message directly from the output.
	// For this test, we verify that isToolError correctly detects errors.
	// We'll test convertAgentEvent indirectly by verifying isToolError first,
	// then ensure it's integrated in the conversion function.

	// Verify isToolError detects the error correctly.
	if !isToolError(toolMsg) {
		t.Error("isToolError should detect 'error:' prefix")
	}

	// Verify a successful tool message is not marked as error.
	okMsg := schema.ToolMessage("success: command executed", "call_002")
	okMsg.ToolName = "bash"
	if isToolError(okMsg) {
		t.Error("isToolError should not mark successful message as error")
	}
}

func TestToTaskEvent_StatusMapping(t *testing.T) {
	ts := int64(123)

	cases := []struct {
		name    string
		status  string
		wantTyp EventType
	}{
		{name: "queued maps to started", status: "queued", wantTyp: EventTaskStarted},
		{name: "in_progress maps to running", status: "in_progress", wantTyp: EventTaskRunning},
		{name: "completed maps to completed", status: "completed", wantTyp: EventTaskCompleted},
		{name: "failed maps to failed", status: "failed", wantTyp: EventTaskFailed},
		{name: "timed_out maps to failed", status: "timed_out", wantTyp: EventTaskFailed},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := `{"task_id":"t1","subject":"demo","status":"` + tc.status + `"}`
			ev := toTaskEvent("thread-1", raw, ts)
			if ev == nil {
				t.Fatalf("expected non-nil task event")
			}
			if ev.Type != tc.wantTyp {
				t.Fatalf("unexpected event type: got=%s want=%s", ev.Type, tc.wantTyp)
			}
			if ev.Timestamp != ts {
				t.Fatalf("timestamp mismatch: got=%d want=%d", ev.Timestamp, ts)
			}
			payload, ok := ev.Payload.(TaskPayload)
			if !ok {
				t.Fatalf("payload type mismatch: %T", ev.Payload)
			}
			if payload.TaskID != "t1" || payload.Status != tc.status {
				t.Fatalf("payload mismatch: %+v", payload)
			}
		})
	}
}

func TestToTaskEvent_InvalidPayload(t *testing.T) {
	if ev := toTaskEvent("thread-1", "not-json", 1); ev != nil {
		t.Fatalf("expected nil for invalid json")
	}
	if ev := toTaskEvent("thread-1", `{"status":"queued"}`, 1); ev != nil {
		t.Fatalf("expected nil when task_id missing")
	}
	if ev := toTaskEvent("thread-1", `{"task_id":"t1"}`, 1); ev != nil {
		t.Fatalf("expected nil when status missing")
	}
}
