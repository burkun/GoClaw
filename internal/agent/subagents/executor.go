package subagents

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Status is the lifecycle state of a subagent task.
type Status string

const (
	StatusPending    Status = "pending"
	StatusQueued     Status = "queued"
	StatusInProgress Status = "in_progress"
	StatusCompleted  Status = "completed"
	StatusFailed     Status = "failed"
	StatusTimedOut   Status = "timed_out"
)

// TaskEvent represents a state transition event in a subagent task.
type TaskEvent struct {
	Status    Status    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
	Message   string    `json:"message,omitempty"`
}

// EventCallback is invoked when a task's status changes.
type EventCallback func(ctx context.Context, taskID string, event TaskEvent)

// TaskRequest is the input for a subagent task.
type TaskRequest struct {
	Description     string
	Prompt          string
	SubagentType    string
	MaxTurns        int
	Timeout         time.Duration
	ModelName       string
	SystemPrompt    string
	AllowedTools    []string
	DisallowedTools []string
	TraceID         string // Trace ID for logging correlation
	// State passed from parent agent
	ThreadID      string
	WorkspacePath string
	UploadsPath   string
	OutputsPath   string
}

// TaskResult is the observable result/state of a task.
type TaskResult struct {
	TaskID       string                   `json:"task_id"`
	TraceID      string                   `json:"trace_id,omitempty"`
	Description  string                   `json:"description,omitempty"`
	Prompt       string                   `json:"prompt"`
	SubagentType string                   `json:"subagent_type"`
	Status       Status                   `json:"status"`
	Output       string                   `json:"output,omitempty"`
	Error        string                   `json:"error,omitempty"`
	AIMessages   []map[string]interface{} `json:"ai_messages,omitempty"`
	CreatedAt    time.Time                `json:"created_at"`
	StartedAt    time.Time                `json:"started_at,omitempty"`
	FinishedAt   time.Time                `json:"finished_at,omitempty"`
}

// WorkerResult is the result returned by a worker function.
type WorkerResult struct {
	Output     string
	Error      error
	AIMessages []map[string]interface{}
}

// WorkerFunc executes a subagent task.
type WorkerFunc func(ctx context.Context, req TaskRequest) (string, error)

// WorkerFuncWithMessages executes a subagent task and returns messages.
type WorkerFuncWithMessages func(ctx context.Context, req TaskRequest) (WorkerResult, error)

// ExecutorConfig controls concurrency and timeout behaviour.
type ExecutorConfig struct {
	MaxConcurrent  int
	DefaultTimeout time.Duration
}

// Executor runs subagent tasks with bounded concurrency.
type Executor struct {
	cfg         ExecutorConfig
	sem         chan struct{}
	mu          sync.RWMutex
	tasks       map[string]*taskRecord
	callbacks   map[string]map[int]EventCallback
	callbackSeq int
}

type taskRecord struct {
	result TaskResult
	done   chan struct{}
}

var (
	defaultExecutorOnce sync.Once
	defaultExecutor     *Executor
)

// DefaultExecutor returns a process-wide executor for task tool usage.
func DefaultExecutor() *Executor {
	defaultExecutorOnce.Do(func() {
		defaultExecutor = NewExecutor(ExecutorConfig{})
	})
	return defaultExecutor
}

// NewExecutor creates an executor with sane defaults.
// MaxConcurrent is clamped to [2, 4] range.
func NewExecutor(cfg ExecutorConfig) *Executor {
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 3
	}
	// Clamp max_concurrent to [2, 4] range.
	const minConcurrent = 2
	const maxConcurrent = 4
	if cfg.MaxConcurrent < minConcurrent {
		cfg.MaxConcurrent = minConcurrent
	}
	if cfg.MaxConcurrent > maxConcurrent {
		cfg.MaxConcurrent = maxConcurrent
	}
	if cfg.DefaultTimeout <= 0 {
		cfg.DefaultTimeout = 15 * time.Minute
	}
	return &Executor{
		cfg:       cfg,
		sem:       make(chan struct{}, cfg.MaxConcurrent),
		tasks:     make(map[string]*taskRecord),
		callbacks: make(map[string]map[int]EventCallback),
	}
}

// Submit enqueues a task and starts background execution.
func (e *Executor) Submit(ctx context.Context, req TaskRequest, worker WorkerFunc) (string, error) {
	if worker == nil {
		return "", fmt.Errorf("subagents: worker is required")
	}
	if req.Prompt == "" {
		return "", fmt.Errorf("subagents: prompt is required")
	}
	if req.SubagentType == "" {
		req.SubagentType = "general-purpose"
	}
	if req.Timeout <= 0 {
		req.Timeout = e.cfg.DefaultTimeout
	}

	taskID := uuid.NewString()
	traceID := generateTraceID()
	now := time.Now()
	rec := &taskRecord{
		result: TaskResult{
			TaskID:       taskID,
			TraceID:      traceID,
			Description:  req.Description,
			Prompt:       req.Prompt,
			SubagentType: req.SubagentType,
			Status:       StatusPending,
			CreatedAt:    now,
		},
		done: make(chan struct{}),
	}

	e.mu.Lock()
	e.tasks[taskID] = rec
	e.mu.Unlock()

	e.setStatus(taskID, StatusQueued, "", "")

	go e.runTask(ctx, taskID, req, worker)
	return taskID, nil
}

// SubmitWithMessages enqueues a task with a worker that returns AI messages.
func (e *Executor) SubmitWithMessages(ctx context.Context, req TaskRequest, worker WorkerFuncWithMessages) (string, error) {
	if worker == nil {
		return "", fmt.Errorf("subagents: worker is required")
	}
	if req.Prompt == "" {
		return "", fmt.Errorf("subagents: prompt is required")
	}
	if req.SubagentType == "" {
		req.SubagentType = "general-purpose"
	}
	if req.Timeout <= 0 {
		req.Timeout = e.cfg.DefaultTimeout
	}

	taskID := uuid.NewString()
	traceID := generateTraceID()
	now := time.Now()
	rec := &taskRecord{
		result: TaskResult{
			TaskID:       taskID,
			TraceID:      traceID,
			Description:  req.Description,
			Prompt:       req.Prompt,
			SubagentType: req.SubagentType,
			Status:       StatusPending,
			CreatedAt:    now,
		},
		done: make(chan struct{}),
	}

	e.mu.Lock()
	e.tasks[taskID] = rec
	e.mu.Unlock()

	e.setStatus(taskID, StatusQueued, "", "")

	go e.runTaskWithMessages(ctx, taskID, req, worker)
	return taskID, nil
}

func (e *Executor) runTask(ctx context.Context, taskID string, req TaskRequest, worker WorkerFunc) {
	// Acquire concurrency slot.
	select {
	case e.sem <- struct{}{}:
		defer func() { <-e.sem }()
	case <-ctx.Done():
		e.finishTask(taskID, StatusFailed, "", ctx.Err().Error())
		return
	}

	e.markStarted(taskID)

	runCtx, cancel := context.WithTimeout(ctx, req.Timeout)
	defer cancel()

	output, err := worker(runCtx, req)
	if err != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			e.finishTask(taskID, StatusTimedOut, output, runCtx.Err().Error())
			return
		}
		e.finishTask(taskID, StatusFailed, output, err.Error())
		return
	}

	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		e.finishTask(taskID, StatusTimedOut, output, runCtx.Err().Error())
		return
	}

	e.finishTask(taskID, StatusCompleted, output, "")
}

func (e *Executor) runTaskWithMessages(ctx context.Context, taskID string, req TaskRequest, worker WorkerFuncWithMessages) {
	// Acquire concurrency slot.
	select {
	case e.sem <- struct{}{}:
		defer func() { <-e.sem }()
	case <-ctx.Done():
		e.finishTaskWithMessages(taskID, StatusFailed, "", ctx.Err().Error(), nil)
		return
	}

	// Get trace ID from task record
	e.mu.RLock()
	rec, ok := e.tasks[taskID]
	if ok && rec.result.TraceID != "" {
		req.TraceID = rec.result.TraceID
	}
	e.mu.RUnlock()

	e.markStarted(taskID)

	runCtx, cancel := context.WithTimeout(ctx, req.Timeout)
	defer cancel()

	result, err := worker(runCtx, req)
	if err != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			e.finishTaskWithMessages(taskID, StatusTimedOut, result.Output, runCtx.Err().Error(), result.AIMessages)
			return
		}
		e.finishTaskWithMessages(taskID, StatusFailed, result.Output, err.Error(), result.AIMessages)
		return
	}

	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		e.finishTaskWithMessages(taskID, StatusTimedOut, result.Output, runCtx.Err().Error(), result.AIMessages)
		return
	}

	e.finishTaskWithMessages(taskID, StatusCompleted, result.Output, "", result.AIMessages)
}

// Get returns a snapshot of a task result.
func (e *Executor) Get(taskID string) (TaskResult, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	rec, ok := e.tasks[taskID]
	if !ok {
		return TaskResult{}, false
	}
	return rec.result, true
}

// List returns all task snapshots sorted by create time.
func (e *Executor) List() []TaskResult {
	e.mu.RLock()
	out := make([]TaskResult, 0, len(e.tasks))
	for _, rec := range e.tasks {
		out = append(out, rec.result)
	}
	e.mu.RUnlock()

	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

// Wait blocks until the task is finished or ctx is done.
func (e *Executor) Wait(ctx context.Context, taskID string) (TaskResult, error) {
	e.mu.RLock()
	rec, ok := e.tasks[taskID]
	e.mu.RUnlock()
	if !ok {
		return TaskResult{}, fmt.Errorf("subagents: task %s not found", taskID)
	}

	select {
	case <-rec.done:
		res, _ := e.Get(taskID)
		return res, nil
	case <-ctx.Done():
		return TaskResult{}, ctx.Err()
	}
}

// Cleanup removes a finished task from in-memory storage.
func (e *Executor) Cleanup(taskID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	rec, ok := e.tasks[taskID]
	if !ok {
		return
	}
	select {
	case <-rec.done:
		delete(e.tasks, taskID)
	default:
		// still running; keep it
	}
}

func (e *Executor) setStatus(taskID string, st Status, output, errMsg string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	rec, ok := e.tasks[taskID]
	if !ok {
		return
	}
	rec.result.Status = st
	if output != "" {
		rec.result.Output = output
	}
	if errMsg != "" {
		rec.result.Error = errMsg
	}
	// Dispatch event to subscribed callbacks.
	e.dispatchEventLocked(taskID, TaskEvent{Status: st, Timestamp: time.Now(), Message: errMsg})
}

func (e *Executor) markStarted(taskID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	rec, ok := e.tasks[taskID]
	if !ok {
		return
	}
	rec.result.Status = StatusInProgress
	rec.result.StartedAt = time.Now()
	e.dispatchEventLocked(taskID, TaskEvent{Status: StatusInProgress, Timestamp: rec.result.StartedAt})
}

func (e *Executor) finishTask(taskID string, st Status, output, errMsg string) {
	e.finishTaskWithMessages(taskID, st, output, errMsg, nil)
}

func (e *Executor) finishTaskWithMessages(taskID string, st Status, output, errMsg string, aiMessages []map[string]interface{}) {
	e.mu.Lock()
	rec, ok := e.tasks[taskID]
	if !ok {
		e.mu.Unlock()
		return
	}
	rec.result.Status = st
	rec.result.Output = output
	rec.result.Error = errMsg
	rec.result.FinishedAt = time.Now()
	if len(aiMessages) > 0 {
		rec.result.AIMessages = aiMessages
	}
	select {
	case <-rec.done:
		// already closed
	default:
		close(rec.done)
	}
	// Dispatch final event before unlock.
	e.dispatchEventLocked(taskID, TaskEvent{Status: st, Timestamp: time.Now(), Message: errMsg})
	e.mu.Unlock()
}

// Subscribe registers a callback to be invoked on status changes for a given taskID.
// Returns a function to unsubscribe.
func (e *Executor) Subscribe(taskID string, cb EventCallback) func() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.callbackSeq++
	id := e.callbackSeq
	if e.callbacks[taskID] == nil {
		e.callbacks[taskID] = make(map[int]EventCallback)
	}
	e.callbacks[taskID][id] = cb
	return func() {
		e.mu.Lock()
		defer e.mu.Unlock()
		if e.callbacks[taskID] == nil {
			return
		}
		delete(e.callbacks[taskID], id)
		if len(e.callbacks[taskID]) == 0 {
			delete(e.callbacks, taskID)
		}
	}
}

// dispatchEventLocked calls all registered callbacks for taskID.
// Must be called while e.mu is held.
func (e *Executor) dispatchEventLocked(taskID string, event TaskEvent) {
	cbMap, ok := e.callbacks[taskID]
	if !ok || len(cbMap) == 0 {
		return
	}
	callbacks := make([]EventCallback, 0, len(cbMap))
	for _, cb := range cbMap {
		callbacks = append(callbacks, cb)
	}
	// Non-blocking dispatch to avoid deadlock.
	for _, cb := range callbacks {
		go cb(context.Background(), taskID, event)
	}
}

// generateTraceID creates an 8-character trace ID for logging correlation.
func generateTraceID() string {
	return uuid.NewString()[:8]
}
