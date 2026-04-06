// Package builtin implements built-in tools for GoClaw.
//
// TaskTool provides subagent delegation capabilities, allowing the lead agent
// to spawn specialized subagents for complex multi-step tasks. This mirrors
// DeerFlow's task_tool functionality with:
//   - SubagentExecutor for managing subagent lifecycle
//   - Background execution with async/await patterns
//   - Real-time status polling and streaming updates
//   - Task result tracking with status (pending/running/completed/failed/timed_out)
//   - Thread pool for concurrent subagent execution
//   - Timeout and cancellation support
package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bookerbai/goclaw/internal/logging"
)

// TaskStatus represents the current state of a subagent task.
// Mirrors DeerFlow's SubagentStatus enum.
type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusTimedOut  TaskStatus = "timed_out"
)

// TaskResult holds the outcome of a subagent execution.
// Mirrors DeerFlow's SubagentResult dataclass.
type TaskResult struct {
	// TaskID is the unique identifier for this execution.
	TaskID string `json:"task_id"`
	// TraceID is for distributed tracing (links parent and subagent logs).
	TraceID string `json:"trace_id"`
	// Status is the current status of the execution.
	Status TaskStatus `json:"status"`
	// Result is the final result message (if completed).
	Result string `json:"result,omitempty"`
	// Error is the error message (if failed).
	Error string `json:"error,omitempty"`
	// StartedAt is when execution started.
	StartedAt *time.Time `json:"started_at,omitempty"`
	// CompletedAt is when execution completed.
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	// AIMessages is the list of AI messages generated during execution.
	AIMessages []map[string]any `json:"ai_messages,omitempty"`
}

// IsTerminal returns true if the task has reached a terminal state.
func (r *TaskResult) IsTerminal() bool {
	return r.Status == TaskStatusCompleted ||
		r.Status == TaskStatusFailed ||
		r.Status == TaskStatusTimedOut
}

// SubagentConfig holds configuration for a subagent type.
// Mirrors DeerFlow's SubagentConfig.
type SubagentConfig struct {
	// Name is the subagent type name.
	Name string
	// SystemPrompt is the system prompt for the subagent.
	SystemPrompt string
	// MaxTurns is the maximum number of turns.
	MaxTurns int
	// TimeoutSeconds is the execution timeout.
	TimeoutSeconds int
	// Model is the model to use ("inherit" to use parent's model).
	Model string
	// Tools is the allowlist of tool names (nil = all tools).
	Tools []string
	// DisallowedTools is the denylist of tool names.
	DisallowedTools []string
}

// SubagentExecutor manages the execution of a subagent.
// Mirrors DeerFlow's SubagentExecutor class.
type SubagentExecutor struct {
	Config      SubagentConfig
	Tools       []Tool
	ParentModel string
	ThreadID    string
	TraceID     string
	maxTurns    int
	timeout     time.Duration
}

// Tool is the interface for tools that can be passed to subagents.
type Tool interface {
	Name() string
	Description() string
	InputSchema() json.RawMessage
	Execute(ctx context.Context, input string) (string, error)
}

// NewSubagentExecutor creates a new SubagentExecutor.
func NewSubagentExecutor(config SubagentConfig, tools []Tool, parentModel, threadID, traceID string) *SubagentExecutor {
	maxTurns := config.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 30
	}

	timeout := time.Duration(config.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}

	return &SubagentExecutor{
		Config:      config,
		Tools:       filterTools(tools, config.Tools, config.DisallowedTools),
		ParentModel: parentModel,
		ThreadID:    threadID,
		TraceID:     traceID,
		maxTurns:    maxTurns,
		timeout:     timeout,
	}
}

// filterTools filters tools based on allowlist and denylist.
func filterTools(tools []Tool, allowed, disallowed []string) []Tool {
	filtered := make([]Tool, 0, len(tools))

	allowedSet := make(map[string]bool)
	if len(allowed) > 0 {
		for _, name := range allowed {
			allowedSet[name] = true
		}
	}

	disallowedSet := make(map[string]bool)
	for _, name := range disallowed {
		disallowedSet[name] = true
	}

	for _, tool := range tools {
		name := tool.Name()

		// Apply denylist
		if disallowedSet[name] {
			continue
		}

		// Apply allowlist if specified
		if len(allowedSet) > 0 && !allowedSet[name] {
			continue
		}

		filtered = append(filtered, tool)
	}

	return filtered
}

// Execute runs the subagent synchronously and returns the result.
func (e *SubagentExecutor) Execute(ctx context.Context, task string) (*TaskResult, error) {
	result := &TaskResult{
		TaskID:    generateTaskID(),
		TraceID:   e.TraceID,
		Status:    TaskStatusRunning,
		StartedAt: timePtr(time.Now()),
	}

	// Apply timeout
	execCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	// Execute the subagent
	err := e.runSubagent(execCtx, task, result)
	if err != nil {
		if execCtx.Err() == context.DeadlineExceeded {
			result.Status = TaskStatusTimedOut
			result.Error = fmt.Sprintf("Task timed out after %v", e.timeout)
		} else {
			result.Status = TaskStatusFailed
			result.Error = err.Error()
		}
		now := time.Now()
		result.CompletedAt = &now
		return result, nil
	}

	result.Status = TaskStatusCompleted
	now := time.Now()
	result.CompletedAt = &now
	return result, nil
}

// ExecuteAsync starts the subagent execution in the background.
// Returns the task ID for polling.
func (e *SubagentExecutor) ExecuteAsync(task string) string {
	result := &TaskResult{
		TaskID:    generateTaskID(),
		TraceID:   e.TraceID,
		Status:    TaskStatusPending,
		StartedAt: timePtr(time.Now()),
	}

	// Store in global task registry
	globalTaskRegistry.Store(result.TaskID, result)

	// Submit to thread pool for background execution
	taskPool.Submit(func() {
		result.Status = TaskStatusRunning
		globalTaskRegistry.Store(result.TaskID, result)

		ctx, cancel := context.WithTimeout(context.Background(), e.timeout)
		defer cancel()

		err := e.runSubagent(ctx, task, result)
		if err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				result.Status = TaskStatusTimedOut
				result.Error = fmt.Sprintf("Task timed out after %v", e.timeout)
			} else {
				result.Status = TaskStatusFailed
				result.Error = err.Error()
			}
		} else {
			result.Status = TaskStatusCompleted
		}

		now := time.Now()
		result.CompletedAt = &now
		globalTaskRegistry.Store(result.TaskID, result)
	})

	return result.TaskID
}

// runSubagent is the core subagent execution logic.
// This is a simplified implementation that should be extended based on the agent framework used.
func (e *SubagentExecutor) runSubagent(ctx context.Context, task string, result *TaskResult) error {
	// NOTE: This is a placeholder implementation.
	// A full implementation would:
	// 1. Create an agent instance with the configured model
	// 2. Set up the initial state with the task
	// 3. Stream execution results
	// 4. Collect AI messages
	// 5. Return the final result
	//
	// For now, return a placeholder result
	result.Result = fmt.Sprintf("Subagent '%s' executed task: %s (placeholder implementation)", e.Config.Name, task)
	return nil
}

// ---------------------------------------------------------------------------
// Global Task Registry (mirrors DeerFlow's _background_tasks)
// ---------------------------------------------------------------------------

// taskRegistry is the global storage for background task results.
type taskRegistry struct {
	mu    sync.RWMutex
	tasks map[string]*TaskResult
	wg    sync.WaitGroup // tracks cleanup goroutine
}

// defaultTaskTTL is the default time-to-live for completed tasks.
const defaultTaskTTL = 30 * time.Minute

// cleanupInterval is the interval between automatic cleanup runs.
const cleanupInterval = 5 * time.Minute

var globalTaskRegistry = &taskRegistry{
	tasks: make(map[string]*TaskResult),
}

// StartTaskRegistryCleanup starts a background goroutine that periodically
// removes completed tasks older than the TTL. Call this once at application start.
// Returns a stop function to halt the cleanup goroutine.
func StartTaskRegistryCleanup() func() {
	return globalTaskRegistry.StartCleanup(defaultTaskTTL)
}

// StartCleanup starts a background cleanup goroutine with the given TTL.
func (r *taskRegistry) StartCleanup(ttl time.Duration) func() {
	stop := make(chan struct{})
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		ticker := time.NewTicker(cleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				r.Cleanup(ttl)
			}
		}
	}()
	return func() {
		close(stop)
		r.wg.Wait()
	}
}

// Store stores a task result.
func (r *taskRegistry) Store(taskID string, result *TaskResult) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tasks[taskID] = result
}

// Load retrieves a task result.
func (r *taskRegistry) Load(taskID string) (*TaskResult, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result, ok := r.tasks[taskID]
	return result, ok
}

// Delete removes a task result.
func (r *taskRegistry) Delete(taskID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tasks, taskID)
}

// Cleanup removes completed tasks older than the given duration.
func (r *taskRegistry) Cleanup(maxAge time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	for id, result := range r.tasks {
		if result.IsTerminal() && result.CompletedAt != nil {
			if now.Sub(*result.CompletedAt) > maxAge {
				delete(r.tasks, id)
			}
		}
	}
}

// Size returns the current number of tasks in the registry.
func (r *taskRegistry) Size() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tasks)
}

// GetBackgroundTaskResult retrieves a task result from the global registry.
// Mirrors DeerFlow's get_background_task_result function.
func GetBackgroundTaskResult(taskID string) (*TaskResult, bool) {
	return globalTaskRegistry.Load(taskID)
}

// CleanupBackgroundTask removes a task from the global registry.
// Mirrors DeerFlow's cleanup_background_task function.
func CleanupBackgroundTask(taskID string) {
	globalTaskRegistry.Delete(taskID)
}

// ---------------------------------------------------------------------------
// Thread Pool for Background Execution
// ---------------------------------------------------------------------------

// taskPool is the global thread pool for subagent execution.
// Mirrors DeerFlow's _execution_pool.
var taskPool = NewTaskPool(3)

// TaskPool manages a pool of goroutines for executing tasks.
type TaskPool struct {
	wg      sync.WaitGroup
	work    chan func()
	stop    chan struct{}
	stopped bool
	mu      sync.Mutex
}

// NewTaskPool creates a new task pool with the given number of workers.
func NewTaskPool(numWorkers int) *TaskPool {
	pool := &TaskPool{
		work: make(chan func()),
		stop: make(chan struct{}),
	}

	for i := 0; i < numWorkers; i++ {
		pool.wg.Add(1)
		go pool.worker()
	}

	return pool
}

// worker is the goroutine that processes tasks.
func (p *TaskPool) worker() {
	defer p.wg.Done()

	for {
		select {
		case <-p.stop:
			return
		case task, ok := <-p.work:
			if !ok {
				return
			}
			task()
		}
	}
}

// Submit submits a task to the pool.
func (p *TaskPool) Submit(task func()) bool {
	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		return false
	}
	p.mu.Unlock()

	select {
	case p.work <- task:
		return true
	case <-p.stop:
		return false
	}
}

// Stop stops the pool and waits for all workers to finish.
func (p *TaskPool) Stop() {
	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		return
	}
	p.stopped = true
	close(p.stop)
	p.mu.Unlock()

	p.wg.Wait()
}

// ---------------------------------------------------------------------------
// TaskTool
// ---------------------------------------------------------------------------

// TaskTool delegates work to a specialized subagent.
//
// Subagents help:
// - Preserve context by keeping exploration and implementation separate
// - Handle complex multi-step tasks autonomously
// - Execute commands or operations in isolated contexts
//
// Available subagent types depend on configuration:
// - "general-purpose": A capable agent for complex, multi-step tasks
// - "code": Specialized for code analysis and modification
// - "research": Specialized for information gathering
//
// Implements tools.Tool.
type TaskTool struct {
	// SubagentConfigs maps subagent type names to their configurations.
	SubagentConfigs map[string]SubagentConfig
	// ToolRegistry provides access to available tools.
	ToolRegistry func() []Tool
	// ParentModel is the model used by the parent agent.
	ParentModel string
	// ThreadID is the current thread ID.
	ThreadID string
	// TraceID is for distributed tracing.
	TraceID string
}

// taskInput is the JSON-decoded input for TaskTool.
type taskInput struct {
	// Description is a short (3-5 word) description of the task for logging/display.
	Description string `json:"description"`
	// Prompt is the task description for the subagent. Be specific and clear.
	Prompt string `json:"prompt"`
	// SubagentType is the type of subagent to use.
	SubagentType string `json:"subagent_type"`
	// MaxTurns is the optional maximum number of agent turns.
	MaxTurns *int `json:"max_turns,omitempty"`
}

// Name returns the tool name.
func (t *TaskTool) Name() string { return "task" }

// Description returns the tool description.
func (t *TaskTool) Description() string {
	return `Delegate a task to a specialized subagent that runs in its own context.

Subagents help you:
- Preserve context by keeping exploration and implementation separate
- Handle complex multi-step tasks autonomously
- Execute commands or operations in isolated contexts

Available subagent types depend on configuration:
- "general-purpose": A capable agent for complex, multi-step tasks that require
  both exploration and action. Use when the task requires complex reasoning,
  multiple dependent steps, or would benefit from isolated context.
- "code": Specialized for code analysis, refactoring, and modification tasks.
- "research": Specialized for information gathering and research tasks.

When to use this tool:
- Complex tasks requiring multiple steps or tools
- Tasks that produce verbose output
- When you want to isolate context from the main conversation
- Parallel research or exploration tasks

When NOT to use this tool:
- Simple, single-step operations (use tools directly)
- Tasks requiring user interaction or clarification

The subagent executes in the background and this tool polls for completion,
streaming status updates (task_started, task_running, task_completed, task_failed)
to provide real-time feedback.`
}

// InputSchema returns the JSON schema for the tool input.
func (t *TaskTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "required": ["description", "prompt", "subagent_type"],
  "properties": {
    "description": {
      "type": "string",
      "description": "A short (3-5 word) description of the task for logging/display. ALWAYS PROVIDE THIS PARAMETER FIRST."
    },
    "prompt": {
      "type": "string",
      "description": "The task description for the subagent. Be specific and clear about what needs to be done. ALWAYS PROVIDE THIS PARAMETER SECOND."
    },
    "subagent_type": {
      "type": "string",
      "description": "The type of subagent to use. ALWAYS PROVIDE THIS PARAMETER THIRD."
    },
    "max_turns": {
      "type": "integer",
      "description": "Optional maximum number of agent turns. Defaults to the subagent's configured maximum."
    }
  }
}`)
}

// Execute runs the subagent with the given task.
// This implementation mirrors DeerFlow's task_tool with background execution and polling.
func (t *TaskTool) Execute(ctx context.Context, input string) (string, error) {
	var in taskInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", fmt.Errorf("task: invalid input JSON: %w", err)
	}

	// Validate required fields
	in.Description = strings.TrimSpace(in.Description)
	in.Prompt = strings.TrimSpace(in.Prompt)
	in.SubagentType = strings.TrimSpace(in.SubagentType)

	if in.Description == "" {
		return "Error: description is required (a short 3-5 word task description)", nil
	}
	if in.Prompt == "" {
		return "Error: prompt is required (the task description for the subagent)", nil
	}
	if in.SubagentType == "" {
		return "Error: subagent_type is required", nil
	}

	// Get subagent configuration
	config, ok := t.SubagentConfigs[in.SubagentType]
	if !ok {
		available := make([]string, 0, len(t.SubagentConfigs))
		for name := range t.SubagentConfigs {
			available = append(available, name)
		}
		return fmt.Sprintf("Error: Unknown subagent type '%s'. Available: %v", in.SubagentType, available), nil
	}

	// Apply max turns override if provided
	if in.MaxTurns != nil && *in.MaxTurns > 0 {
		config.MaxTurns = *in.MaxTurns
	}

	// Get available tools
	var tools []Tool
	if t.ToolRegistry != nil {
		tools = t.ToolRegistry()
	}

	// Create executor
	traceID := t.TraceID
	if traceID == "" {
		traceID = generateTraceID()
	}

	executor := NewSubagentExecutor(config, tools, t.ParentModel, t.ThreadID, traceID)

	// Start background execution
	taskID := executor.ExecuteAsync(in.Prompt)

	// Poll for task completion (mirrors DeerFlow's polling loop)
	return t.pollForCompletion(ctx, taskID, in.Description, config.TimeoutSeconds)
}

// pollForCompletion polls the background task until completion or timeout.
// Mirrors DeerFlow's polling loop in task_tool.
func (t *TaskTool) pollForCompletion(ctx context.Context, taskID, description string, timeoutSeconds int) (string, error) {
	// Polling interval: 5 seconds
	pollInterval := 5 * time.Second

	// Max poll count: timeout + 60s buffer, in 5s intervals
	maxPollCount := (timeoutSeconds + 60) / 5
	if maxPollCount <= 0 {
		maxPollCount = 120 // Default: 10 minutes
	}

	pollCount := 0
	lastStatus := TaskStatusPending
	lastMessageCount := 0

	// Send task_started event
	logging.Info("[TaskTool] task started", "task_id", taskID)

	for {
		result, ok := GetBackgroundTaskResult(taskID)
		if !ok {
			CleanupBackgroundTask(taskID)
			return fmt.Sprintf("Error: Task %s disappeared from background tasks", taskID), nil
		}

		// Log status changes
		if result.Status != lastStatus {
			logging.Info("[TaskTool] task status changed",
				"task_id", taskID,
				"from", lastStatus,
				"to", result.Status)
			lastStatus = result.Status
		}

		// Check for new AI messages and send task_running events
		currentMessageCount := len(result.AIMessages)
		if currentMessageCount > lastMessageCount {
			for i := lastMessageCount; i < currentMessageCount; i++ {
				logging.Debug("[TaskTool] task running: new message", "task_id", taskID)
			}
			lastMessageCount = currentMessageCount
		}

		// Check if task completed, failed, or timed out
		switch result.Status {
		case TaskStatusCompleted:
			logging.Info("[TaskTool] task completed", "task_id", taskID, "result", result.Result)
			CleanupBackgroundTask(taskID)
			return fmt.Sprintf("Task Succeeded. Result: %s", result.Result), nil

		case TaskStatusFailed:
			logging.Warn("[TaskTool] task failed", "task_id", taskID, "error", result.Error)
			CleanupBackgroundTask(taskID)
			return fmt.Sprintf("Task failed. Error: %s", result.Error), nil

		case TaskStatusTimedOut:
			logging.Warn("[TaskTool] task timed out", "task_id", taskID, "error", result.Error)
			CleanupBackgroundTask(taskID)
			return fmt.Sprintf("Task timed out. Error: %s", result.Error), nil
		}

		// Still running, wait before next poll
		select {
		case <-ctx.Done():
			// Context cancelled, schedule deferred cleanup
			go t.deferredCleanup(taskID, maxPollCount)
			return "", ctx.Err()
		case <-time.After(pollInterval):
			pollCount++
		}

		// Polling timeout as safety net
		if pollCount > maxPollCount {
			timeoutMinutes := timeoutSeconds / 60
			logging.Warn("[TaskTool] task polling timed out",
				"task_id", taskID,
				"timeout_minutes", timeoutMinutes)
			return fmt.Sprintf("Task polling timed out after %d minutes. This may indicate the background task is stuck.", timeoutMinutes), nil
		}
	}
}

// deferredCleanup waits for the task to complete and then cleans up.
func (t *TaskTool) deferredCleanup(taskID string, maxPolls int) {
	pollCount := 0
	for {
		result, ok := GetBackgroundTaskResult(taskID)
		if !ok {
			return
		}

		if result.IsTerminal() {
			CleanupBackgroundTask(taskID)
			return
		}

		if pollCount > maxPolls {
			return
		}

		time.Sleep(5 * time.Second)
		pollCount++
	}
}

// ---------------------------------------------------------------------------
// Helper Functions
// ---------------------------------------------------------------------------

func generateTaskID() string {
	// Generate a short unique ID (8 characters)
	return fmt.Sprintf("%08d", time.Now().UnixNano()%100000000)
}

func generateTraceID() string {
	// Generate a short trace ID (8 characters)
	return fmt.Sprintf("%08d", time.Now().UnixNano()%100000000)
}

func timePtr(t time.Time) *time.Time {
	return &t
}

// ---------------------------------------------------------------------------
// Default Subagent Configurations
// ---------------------------------------------------------------------------

// DefaultSubagentConfigs provides default subagent configurations.
// These should be customized based on the deployment.
var DefaultSubagentConfigs = map[string]SubagentConfig{
	"general-purpose": {
		Name:           "general-purpose",
		SystemPrompt:   "You are a capable general-purpose agent. Help the user with their task.",
		MaxTurns:       30,
		TimeoutSeconds: 600,
		Model:          "inherit",
	},
	"code": {
		Name:           "code",
		SystemPrompt:   "You are a code specialist. Analyze, refactor, and modify code as requested.",
		MaxTurns:       30,
		TimeoutSeconds: 600,
		Model:          "inherit",
	},
	"research": {
		Name:           "research",
		SystemPrompt:   "You are a research specialist. Gather and synthesize information.",
		MaxTurns:       30,
		TimeoutSeconds: 600,
		Model:          "inherit",
	},
}

// NewTaskToolWithDefaults creates a TaskTool with default configurations.
func NewTaskToolWithDefaults() *TaskTool {
	return &TaskTool{
		SubagentConfigs: DefaultSubagentConfigs,
		ToolRegistry:    nil, // Should be set by caller
		ParentModel:     "",
		ThreadID:        "",
		TraceID:         "",
	}
}

// RegisterSubagentConfig registers a subagent configuration.
func (t *TaskTool) RegisterSubagentConfig(name string, config SubagentConfig) {
	if t.SubagentConfigs == nil {
		t.SubagentConfigs = make(map[string]SubagentConfig)
	}
	t.SubagentConfigs[name] = config
}

// GetAvailableSubagentNames returns the list of available subagent types.
func (t *TaskTool) GetAvailableSubagentNames() []string {
	names := make([]string, 0, len(t.SubagentConfigs))
	for name := range t.SubagentConfigs {
		names = append(names, name)
	}
	return names
}
