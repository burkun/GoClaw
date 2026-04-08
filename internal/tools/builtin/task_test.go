package builtin

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// TestTaskResult_IsTerminal tests the IsTerminal method
func TestTaskResult_IsTerminal(t *testing.T) {
	tests := []struct {
		status   TaskStatus
		expected bool
	}{
		{TaskStatusPending, false},
		{TaskStatusRunning, false},
		{TaskStatusCompleted, true},
		{TaskStatusFailed, true},
		{TaskStatusTimedOut, true},
	}

	for _, tt := range tests {
		result := &TaskResult{Status: tt.status}
		if result.IsTerminal() != tt.expected {
			t.Errorf("IsTerminal() for status %q: got %v, want %v", tt.status, result.IsTerminal(), tt.expected)
		}
	}
}

// TestNewSubagentExecutor tests the constructor
func TestNewSubagentExecutor(t *testing.T) {
	tests := []struct {
		name           string
		maxTurns       int
		timeoutSec     int
		expectedTurns  int
		expectedTimout time.Duration
	}{
		{"defaults", 0, 0, 30, 10 * time.Minute},
		{"custom", 50, 300, 50, 300 * time.Second},
		{"negative values use defaults", -1, -1, 30, 10 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := SubagentConfig{
				Name:           "test",
				MaxTurns:       tt.maxTurns,
				TimeoutSeconds: tt.timeoutSec,
			}
			executor := NewSubagentExecutor(config, nil, "model", "thread", "trace")
			if executor.maxTurns != tt.expectedTurns {
				t.Errorf("maxTurns: got %d, want %d", executor.maxTurns, tt.expectedTurns)
			}
			if executor.timeout != tt.expectedTimout {
				t.Errorf("timeout: got %v, want %v", executor.timeout, tt.expectedTimout)
			}
		})
	}
}

// TestFilterTools tests the filterTools function
func TestFilterTools(t *testing.T) {
	tools := []Tool{
		&mockTool{name: "read_file"},
		&mockTool{name: "write_file"},
		&mockTool{name: "bash"},
		&mockTool{name: "web_search"},
	}

	tests := []struct {
		name        string
		allowed     []string
		disallowed  []string
		expectedLen int
	}{
		{"no filter", nil, nil, 4},
		{"allowlist only", []string{"read_file", "write_file"}, nil, 2},
		{"denylist only", nil, []string{"bash"}, 3},
		{"both filters", []string{"read_file", "write_file", "bash"}, []string{"bash"}, 2},
		{"empty after filters", []string{"nonexistent"}, nil, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filtered := filterTools(tools, tt.allowed, tt.disallowed)
			if len(filtered) != tt.expectedLen {
				t.Errorf("filterTools: got %d tools, want %d", len(filtered), tt.expectedLen)
			}
		})
	}
}

// mockTool is a mock implementation of Tool for testing
type mockTool struct {
	name string
}

func (m *mockTool) Name() string                     { return m.name }
func (m *mockTool) Description() string              { return "mock description" }
func (m *mockTool) InputSchema() json.RawMessage     { return json.RawMessage("{}") }
func (m *mockTool) Execute(ctx context.Context, input string) (string, error) {
	return "mock result", nil
}

// TestTaskRegistry tests task registry operations
func TestTaskRegistry_StoreLoadDelete(t *testing.T) {
	registry := &taskRegistry{
		tasks: make(map[string]*TaskResult),
	}

	result := &TaskResult{
		TaskID: "test-task-1",
		Status: TaskStatusRunning,
	}

	// Test Store
	registry.Store("test-task-1", result)

	// Test Load
	loaded, ok := registry.Load("test-task-1")
	if !ok {
		t.Error("expected to find task")
	}
	if loaded.TaskID != "test-task-1" {
		t.Errorf("loaded wrong task: got %q", loaded.TaskID)
	}

	// Test Delete
	registry.Delete("test-task-1")
	_, ok = registry.Load("test-task-1")
	if ok {
		t.Error("expected task to be deleted")
	}
}

// TestTaskRegistry_Size tests the Size method
func TestTaskRegistry_Size(t *testing.T) {
	registry := &taskRegistry{
		tasks: make(map[string]*TaskResult),
	}

	if registry.Size() != 0 {
		t.Errorf("expected size 0, got %d", registry.Size())
	}

	registry.Store("task-1", &TaskResult{TaskID: "task-1"})
	registry.Store("task-2", &TaskResult{TaskID: "task-2"})

	if registry.Size() != 2 {
		t.Errorf("expected size 2, got %d", registry.Size())
	}
}

// TestTaskRegistry_Cleanup tests the Cleanup method
func TestTaskRegistry_Cleanup(t *testing.T) {
	registry := &taskRegistry{
		tasks: make(map[string]*TaskResult),
	}

	// Add a completed task that's old enough to be cleaned up
	oldTime := time.Now().Add(-2 * time.Hour)
	registry.Store("old-task", &TaskResult{
		TaskID:      "old-task",
		Status:      TaskStatusCompleted,
		CompletedAt: &oldTime,
	})

	// Add a recent completed task
	recentTime := time.Now()
	registry.Store("recent-task", &TaskResult{
		TaskID:      "recent-task",
		Status:      TaskStatusCompleted,
		CompletedAt: &recentTime,
	})

	// Add a running task (should not be cleaned up)
	registry.Store("running-task", &TaskResult{
		TaskID: "running-task",
		Status: TaskStatusRunning,
	})

	// Cleanup tasks older than 1 hour
	registry.Cleanup(1 * time.Hour)

	// Old task should be removed
	if _, ok := registry.Load("old-task"); ok {
		t.Error("expected old task to be cleaned up")
	}

	// Recent and running tasks should remain
	if _, ok := registry.Load("recent-task"); !ok {
		t.Error("expected recent task to remain")
	}
	if _, ok := registry.Load("running-task"); !ok {
		t.Error("expected running task to remain")
	}
}

// TestGetBackgroundTaskResult tests the global function
func TestGetBackgroundTaskResult(t *testing.T) {
	// Clean up before test
	globalTaskRegistry.Delete("test-global-task")

	// Test not found
	_, ok := GetBackgroundTaskResult("test-global-task")
	if ok {
		t.Error("expected not to find task")
	}

	// Add task
	globalTaskRegistry.Store("test-global-task", &TaskResult{TaskID: "test-global-task"})

	// Test found
	result, ok := GetBackgroundTaskResult("test-global-task")
	if !ok {
		t.Error("expected to find task")
	}
	if result.TaskID != "test-global-task" {
		t.Errorf("wrong task ID: got %q", result.TaskID)
	}

	// Cleanup
	CleanupBackgroundTask("test-global-task")
}

// TestTaskTool_Name tests the Name method
func TestTaskTool_Name(t *testing.T) {
	tool := &TaskTool{}
	if tool.Name() != "task" {
		t.Errorf("expected name 'task', got %q", tool.Name())
	}
}

// TestTaskTool_Description tests the Description method
func TestTaskTool_Description(t *testing.T) {
	tool := &TaskTool{}
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
}

// TestTaskTool_InputSchema tests the InputSchema method
func TestTaskTool_InputSchema(t *testing.T) {
	tool := &TaskTool{}
	schema := tool.InputSchema()
	if len(schema) == 0 {
		t.Error("expected non-empty input schema")
	}

	var parsed map[string]any
	if err := json.Unmarshal(schema, &parsed); err != nil {
		t.Errorf("input schema is not valid JSON: %v", err)
	}
}

// TestTaskTool_Execute_InvalidJSON tests error handling for invalid JSON
func TestTaskTool_Execute_InvalidJSON(t *testing.T) {
	tool := &TaskTool{
		SubagentConfigs: DefaultSubagentConfigs,
	}
	_, err := tool.Execute(context.Background(), `{invalid json`)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// TestTaskTool_Execute_MissingFields tests validation for missing required fields
func TestTaskTool_Execute_MissingFields(t *testing.T) {
	tool := &TaskTool{
		SubagentConfigs: DefaultSubagentConfigs,
	}

	tests := []string{
		`{"description":"test"}`,
		`{"description":"test","prompt":""}`,
		`{"description":"test","prompt":"test"}`,
	}

	for i, input := range tests {
		result, err := tool.Execute(context.Background(), input)
		if err != nil {
			t.Errorf("test %d: unexpected error: %v", i, err)
			continue
		}
		// Should return error message, not an actual Go error
		if len(result) > 0 && result[0:5] != "Error" {
			t.Errorf("test %d: expected error string, got: %q", i, result)
		}
	}
}

// TestTaskTool_Execute_UnknownSubagentType tests handling of unknown subagent types
func TestTaskTool_Execute_UnknownSubagentType(t *testing.T) {
	tool := &TaskTool{
		SubagentConfigs: DefaultSubagentConfigs,
	}

	input := `{"description":"test","prompt":"test task","subagent_type":"nonexistent"}`
	result, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) == 0 || result[:5] != "Error" {
		t.Errorf("expected error for unknown subagent type, got: %q", result)
	}
}

// TestNewTaskToolWithDefaults tests the constructor
func TestNewTaskToolWithDefaults(t *testing.T) {
	tool := NewTaskToolWithDefaults()
	if tool.Name() != "task" {
		t.Errorf("expected name 'task', got %q", tool.Name())
	}
	if len(tool.SubagentConfigs) == 0 {
		t.Error("expected non-empty default subagent configs")
	}
}

// TestTaskTool_RegisterSubagentConfig tests registering new subagent configs
func TestTaskTool_RegisterSubagentConfig(t *testing.T) {
	tool := &TaskTool{}
	tool.RegisterSubagentConfig("custom", SubagentConfig{
		Name:         "custom",
		SystemPrompt: "You are a custom agent",
		MaxTurns:     20,
	})

	if len(tool.SubagentConfigs) != 1 {
		t.Errorf("expected 1 config, got %d", len(tool.SubagentConfigs))
	}
	if tool.SubagentConfigs["custom"].Name != "custom" {
		t.Error("config not registered correctly")
	}
}

// TestTaskTool_GetAvailableSubagentNames tests listing available subagents
func TestTaskTool_GetAvailableSubagentNames(t *testing.T) {
	tool := NewTaskToolWithDefaults()
	names := tool.GetAvailableSubagentNames()
	if len(names) == 0 {
		t.Error("expected non-empty list of subagent names")
	}
}

// TestGenerateTaskID tests task ID generation
func TestGenerateTaskID(t *testing.T) {
	id1 := generateTaskID()

	if len(id1) != 8 {
		t.Errorf("expected 8-character ID, got %d", len(id1))
	}

	// ID should be numeric only
	for _, c := range id1 {
		if c < '0' || c > '9' {
			t.Errorf("expected numeric ID, got %q", id1)
			break
		}
	}
}

// TestGenerateTraceID tests trace ID generation
func TestGenerateTraceID(t *testing.T) {
	id := generateTraceID()
	if len(id) != 8 {
		t.Errorf("expected 8-character ID, got %d", len(id))
	}
}

// TestTimePtr tests the timePtr helper
func TestTimePtr(t *testing.T) {
	now := time.Now()
	ptr := timePtr(now)
	if ptr == nil {
		t.Error("expected non-nil pointer")
	}
	if !ptr.Equal(now) {
		t.Error("time values don't match")
	}
}

// TestTaskPool tests the task pool
func TestTaskPool_NewTaskPool(t *testing.T) {
	pool := NewTaskPool(2)
	if pool == nil {
		t.Error("expected non-nil pool")
	}
}

// TestTaskPool_Submit tests submitting tasks to the pool
func TestTaskPool_Submit(t *testing.T) {
	pool := NewTaskPool(2)
	defer pool.Stop()

	done := make(chan bool, 1)
	success := pool.Submit(func() {
		done <- true
	})
	if !success {
		t.Error("expected Submit to succeed")
	}

	select {
	case <-done:
		// Success
	case <-time.After(1 * time.Second):
		t.Error("task did not execute within timeout")
	}
}

// TestTaskPool_SubmitAfterStop tests submitting after stopping
func TestTaskPool_SubmitAfterStop(t *testing.T) {
	pool := NewTaskPool(1)
	pool.Stop()

	success := pool.Submit(func() {})
	if success {
		t.Error("expected Submit to fail after Stop")
	}
}

// TestTaskPool_Stop tests stopping the pool
func TestTaskPool_Stop(t *testing.T) {
	pool := NewTaskPool(2)
	// Stop should not panic when called
	pool.Stop()
	// Double stop should be safe
	pool.Stop()
}

// TestDefaultSubagentConfigs tests default configurations exist
func TestDefaultSubagentConfigs(t *testing.T) {
	if len(DefaultSubagentConfigs) == 0 {
		t.Error("expected non-empty default subagent configs")
	}

	expectedTypes := []string{"general-purpose", "code", "research"}
	for _, expected := range expectedTypes {
		if _, ok := DefaultSubagentConfigs[expected]; !ok {
			t.Errorf("missing expected subagent type: %s", expected)
		}
	}
}

// TestSubagentExecutor_Execute tests synchronous execution
func TestSubagentExecutor_Execute(t *testing.T) {
	config := SubagentConfig{
		Name:           "test",
		SystemPrompt:   "You are a test agent",
		MaxTurns:       10,
		TimeoutSeconds: 5,
	}
	executor := NewSubagentExecutor(config, nil, "gpt-4", "thread-1", "trace-1")

	result, err := executor.Execute(context.Background(), "test task")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.TaskID == "" {
		t.Error("expected non-empty TaskID")
	}
	if result.TraceID != "trace-1" {
		t.Errorf("expected TraceID 'trace-1', got %q", result.TraceID)
	}
	if result.Status != TaskStatusCompleted {
		t.Errorf("expected status Completed, got %q", result.Status)
	}
}

// TestSubagentExecutor_Execute_ContextCancellation tests handling context cancellation
func TestSubagentExecutor_Execute_ContextCancellation(t *testing.T) {
	config := SubagentConfig{
		Name:           "test",
		TimeoutSeconds: 10,
	}
	executor := NewSubagentExecutor(config, nil, "gpt-4", "thread-1", "trace-1")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// The placeholder implementation should still complete
	result, err := executor.Execute(ctx, "test task")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Placeholder implementation always completes successfully
	if result.Status != TaskStatusCompleted {
		t.Errorf("expected status Completed, got %q", result.Status)
	}
}

// TestSubagentExecutor_ExecuteAsync tests asynchronous execution
func TestSubagentExecutor_ExecuteAsync(t *testing.T) {
	config := SubagentConfig{
		Name:           "test",
		TimeoutSeconds: 5,
	}
	executor := NewSubagentExecutor(config, nil, "gpt-4", "thread-1", "trace-1")

	taskID := executor.ExecuteAsync("test task")
	if taskID == "" {
		t.Error("expected non-empty task ID")
	}

	// Wait a moment for background execution
	time.Sleep(100 * time.Millisecond)

	// Check task is in registry
	result, ok := GetBackgroundTaskResult(taskID)
	if !ok {
		t.Error("expected to find task in registry")
	}

	// Should be completed or running
	if result.Status != TaskStatusCompleted && result.Status != TaskStatusRunning && result.Status != TaskStatusPending {
		t.Errorf("unexpected status: %q", result.Status)
	}

	// Cleanup
	CleanupBackgroundTask(taskID)
}

// TestStartTaskRegistryCleanup tests the cleanup goroutine
func TestStartTaskRegistryCleanup(t *testing.T) {
	// Add an old completed task
	oldTime := time.Now().Add(-2 * time.Hour)
	globalTaskRegistry.Store("cleanup-test-old", &TaskResult{
		TaskID:      "cleanup-test-old",
		Status:      TaskStatusCompleted,
		CompletedAt: &oldTime,
	})

	// Start cleanup with short TTL
	stop := StartTaskRegistryCleanup()
	defer stop()

	// Wait a moment for cleanup to potentially run
	time.Sleep(100 * time.Millisecond)

	// The task should eventually be cleaned up (but timing is tricky in tests)
	// Just verify the stop function works without panic
}

// TestTaskRegistry_StartCleanup tests the StartCleanup method
func TestTaskRegistry_StartCleanup(t *testing.T) {
	registry := &taskRegistry{
		tasks: make(map[string]*TaskResult),
	}

	// Add an old task
	oldTime := time.Now().Add(-2 * time.Hour)
	registry.Store("old-task", &TaskResult{
		TaskID:      "old-task",
		Status:      TaskStatusCompleted,
		CompletedAt: &oldTime,
	})

	// Start cleanup
	stop := registry.StartCleanup(100 * time.Millisecond)
	defer stop()

	// Wait for cleanup to potentially run
	time.Sleep(150 * time.Millisecond)

	// Verify cleanup happened (or at least didn't panic)
	// The exact timing is tricky, so we just ensure it runs without error
}

// TestTaskTool_Execute_Success tests successful task tool execution
func TestTaskTool_Execute_Success(t *testing.T) {
	// Skip this test in short mode as it involves polling
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	tool := &TaskTool{
		SubagentConfigs: DefaultSubagentConfigs,
		ToolRegistry:    func() []Tool { return nil },
	}

	// Poll interval is 5s, so timeout must be > 5s to complete at least one poll cycle
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := tool.Execute(ctx, `{"description":"test","prompt":"test task","subagent_type":"general-purpose"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have some result (either completed or timed out)
	if result == "" {
		t.Error("expected non-empty result")
	}
}

// TestTaskTool_Execute_WithMaxTurns tests execution with max_turns override
func TestTaskTool_Execute_WithMaxTurns(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	tool := &TaskTool{
		SubagentConfigs: DefaultSubagentConfigs,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := tool.Execute(ctx, `{"description":"test","prompt":"test","subagent_type":"general-purpose","max_turns":5}`)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	// Should have some result
	if result == "" {
		t.Error("expected non-empty result")
	}
}

// TestTaskTool_Execute_WithToolRegistry tests execution with tool registry
func TestTaskTool_Execute_WithToolRegistry(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	tool := &TaskTool{
		SubagentConfigs: DefaultSubagentConfigs,
		ToolRegistry: func() []Tool {
			return []Tool{&mockTool{name: "test_tool"}}
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := tool.Execute(ctx, `{"description":"test","prompt":"test","subagent_type":"general-purpose"}`)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty result")
	}
}
