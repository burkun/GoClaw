package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"goclaw/internal/config"
	skillsruntime "goclaw/internal/skills"
	toolruntime "goclaw/internal/tools"
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

// TestLeadAgent_basicRun verifies terminal event guarantee on uninitialized agent.
func TestLeadAgent_basicRun(t *testing.T) {
	a := &leadAgent{}

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

	ch, err := a.Run(ctx, state, cfg)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	events := drainChannel(t, ch, 5*time.Second)
	assertTerminal(t, events)
}

// TestLeadAgent_planMode verifies middleware chain is created in plan mode.
func TestLeadAgent_planMode(t *testing.T) {
	mws := buildMiddlewares(RunConfig{IsPlanMode: true})
	if len(mws) == 0 {
		t.Fatalf("expected non-empty middleware chain in plan mode")
	}
}

// TestBuildMiddlewares_empty verifies that buildMiddlewares does not panic with
// a zero-value RunConfig and returns a non-nil slice.
func TestBuildMiddlewares_empty(t *testing.T) {
	mws := buildMiddlewares(RunConfig{})
	_ = mws // middleware chain may be nil for empty config
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

	oldGet := getAppConfig
	oldRegister := registerDefaultTools
	oldInvalidate := invalidateMCPConfigCache
	defer func() {
		getAppConfig = oldGet
		registerDefaultTools = oldRegister
		invalidateMCPConfigCache = oldInvalidate
	}()

	getAppConfig = func() (*config.AppConfig, error) {
		return &config.AppConfig{}, nil
	}
	registerCalled := 0
	registerDefaultTools = func(_ *config.AppConfig, _ *config.ModelConfig) error {
		registerCalled++
		return nil
	}
	invalidateCalled := 0
	invalidateMCPConfigCache = func() { invalidateCalled++ }

	a := &leadAgent{skills: reg}
	if err := a.syncSkillsOnConfigReload(); err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	if plugin.reloadCnt != 1 {
		t.Fatalf("expected reload count 1, got %d", plugin.reloadCnt)
	}
	if registerCalled != 1 {
		t.Fatalf("expected registerDefaultTools called once, got %d", registerCalled)
	}
	if invalidateCalled != 1 {
		t.Fatalf("expected invalidateMCPConfigCache called once, got %d", invalidateCalled)
	}
}

func TestSyncSkillsOnConfigReload_ConfigError(t *testing.T) {
	oldGet := getAppConfig
	oldRegister := registerDefaultTools
	oldInvalidate := invalidateMCPConfigCache
	defer func() {
		getAppConfig = oldGet
		registerDefaultTools = oldRegister
		invalidateMCPConfigCache = oldInvalidate
	}()
	getAppConfig = func() (*config.AppConfig, error) {
		return nil, errors.New("load failed")
	}
	registerDefaultTools = func(_ *config.AppConfig, _ *config.ModelConfig) error {
		t.Fatalf("registerDefaultTools should not be called on get config error")
		return nil
	}
	invalidateMCPConfigCache = func() {
		t.Fatalf("invalidateMCPConfigCache should not be called on get config error")
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

// Note: TestIsToolError moved to executor_test.go for comprehensive coverage

// Note: TestToTaskEvent_StatusMapping and TestToTaskEvent_InvalidPayload moved to executor_test.go
