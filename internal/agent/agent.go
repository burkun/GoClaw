package agent

import (
	"context"

	einoruntime "goclaw/internal/eino"
	skillsruntime "goclaw/internal/skills"

	"github.com/cloudwego/eino/adk"
	lctool "github.com/cloudwego/eino/components/tool"
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
	middlewares []adk.AgentMiddleware
	runner      *einoruntime.Runner
	skills      *skillsruntime.Registry
}
