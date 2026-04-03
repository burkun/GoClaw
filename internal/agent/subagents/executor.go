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

// TaskRequest is the input for a subagent task.
type TaskRequest struct {
	Description  string
	Prompt       string
	SubagentType string
	MaxTurns     int
	Timeout      time.Duration
}

// TaskResult is the observable result/state of a task.
type TaskResult struct {
	TaskID       string    `json:"task_id"`
	Description  string    `json:"description,omitempty"`
	Prompt       string    `json:"prompt"`
	SubagentType string    `json:"subagent_type"`
	Status       Status    `json:"status"`
	Output       string    `json:"output,omitempty"`
	Error        string    `json:"error,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	StartedAt    time.Time `json:"started_at,omitempty"`
	FinishedAt   time.Time `json:"finished_at,omitempty"`
}

// WorkerFunc executes a subagent task.
type WorkerFunc func(ctx context.Context, req TaskRequest) (string, error)

// ExecutorConfig controls concurrency and timeout behaviour.
type ExecutorConfig struct {
	MaxConcurrent  int
	DefaultTimeout time.Duration
}

// Executor runs subagent tasks with bounded concurrency.
type Executor struct {
	cfg   ExecutorConfig
	sem   chan struct{}
	mu    sync.RWMutex
	tasks map[string]*taskRecord
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
func NewExecutor(cfg ExecutorConfig) *Executor {
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 3
	}
	if cfg.DefaultTimeout <= 0 {
		cfg.DefaultTimeout = 15 * time.Minute
	}
	return &Executor{
		cfg:   cfg,
		sem:   make(chan struct{}, cfg.MaxConcurrent),
		tasks: make(map[string]*taskRecord),
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
	now := time.Now()
	rec := &taskRecord{
		result: TaskResult{
			TaskID:       taskID,
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
}

func (e *Executor) finishTask(taskID string, st Status, output, errMsg string) {
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
	select {
	case <-rec.done:
		// already closed
	default:
		close(rec.done)
	}
	e.mu.Unlock()
}
