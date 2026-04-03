package eino

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/adk"
)

// EventStream 封装 Eino AsyncIterator，供上层统一读取事件流。
type EventStream struct {
	iter *adk.AsyncIterator[*adk.AgentEvent]
}

// Next 获取下一条事件；当返回 ok=false 表示流结束。
func (s *EventStream) Next() (*adk.AgentEvent, bool) {
	if s == nil || s.iter == nil {
		return nil, false
	}
	return s.iter.Next()
}

// RunnerConfig 是 GoClaw 对 Eino RunnerConfig 的轻量封装。
type RunnerConfig struct {
	Agent           adk.Agent
	EnableStreaming bool
	CheckPointStore adk.CheckPointStore
}

// Runner 是 GoClaw 对 Eino Runner 的适配器。
type Runner struct {
	inner *adk.Runner
}

// NewRunner 创建新的 Runner 适配器。
func NewRunner(ctx context.Context, cfg RunnerConfig) (*Runner, error) {
	if cfg.Agent == nil {
		return nil, fmt.Errorf("eino runner: agent is required")
	}

	inner := adk.NewRunner(ctx, adk.RunnerConfig{
		Agent:           cfg.Agent,
		EnableStreaming: cfg.EnableStreaming,
		CheckPointStore: cfg.CheckPointStore,
	})

	return &Runner{inner: inner}, nil
}

// Run 执行一次完整运行。
func (r *Runner) Run(ctx context.Context, messages []Message, opts ...AgentRunOption) *EventStream {
	if r == nil || r.inner == nil {
		return &EventStream{}
	}
	return &EventStream{iter: r.inner.Run(ctx, messages, opts...)}
}

// Query 使用单条用户输入触发运行。
func (r *Runner) Query(ctx context.Context, query string, opts ...AgentRunOption) *EventStream {
	if r == nil || r.inner == nil {
		return &EventStream{}
	}
	return &EventStream{iter: r.inner.Query(ctx, query, opts...)}
}

// Resume 从 checkpoint 恢复。
func (r *Runner) Resume(ctx context.Context, checkpointID string, opts ...AgentRunOption) (*EventStream, error) {
	if r == nil || r.inner == nil {
		return nil, fmt.Errorf("eino runner: not initialized")
	}

	iter, err := r.inner.Resume(ctx, checkpointID, opts...)
	if err != nil {
		return nil, err
	}
	return &EventStream{iter: iter}, nil
}

// ResumeWithParams 从 checkpoint 恢复并注入目标恢复参数。
func (r *Runner) ResumeWithParams(ctx context.Context, checkpointID string, params *ResumeParams, opts ...AgentRunOption) (*EventStream, error) {
	if r == nil || r.inner == nil {
		return nil, fmt.Errorf("eino runner: not initialized")
	}

	iter, err := r.inner.ResumeWithParams(ctx, checkpointID, params, opts...)
	if err != nil {
		return nil, err
	}
	return &EventStream{iter: iter}, nil
}
