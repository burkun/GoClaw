package agent

import (
	"context"

	"github.com/cloudwego/eino/adk"
	lctool "github.com/cloudwego/eino/components/tool"
	einoruntime "github.com/bookerbai/goclaw/internal/eino"
	skillsruntime "github.com/bookerbai/goclaw/internal/skills"
)

// LeadAgent defines the interface for a lead agent.
type LeadAgent interface {
	Run(ctx context.Context, state *ThreadState, cfg RunConfig) (<-chan Event, error)
	Resume(ctx context.Context, state *ThreadState, cfg RunConfig, checkpointID string) (<-chan Event, error)
}

// leadAgent is the main agent implementation.
type leadAgent struct {
	einoAgent   adk.Agent
	tools       []lctool.BaseTool
	middlewares []adk.ChatModelAgentMiddleware
	runner      *einoruntime.Runner
	skills      *skillsruntime.Registry
}

