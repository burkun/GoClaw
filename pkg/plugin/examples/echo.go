// Package example demonstrates how to create a GoClaw plugin.
// This example plugin provides a simple echo tool and logging middleware.
package example

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"goclaw/internal/middleware"
	"goclaw/internal/tools"
	"goclaw/pkg/plugin"
)

// EchoPlugin is an example plugin that demonstrates the plugin interface.
type EchoPlugin struct {
	*plugin.BasePlugin

	// Configuration
	prefix string
	suffix string
}

// New creates a new EchoPlugin instance.
func New() *EchoPlugin {
	return &EchoPlugin{
		BasePlugin: plugin.NewBasePlugin(
			"echo-plugin",
			"1.0.0",
			"An example plugin that provides an echo tool and logging middleware",
		),
		prefix: "[Echo] ",
		suffix: "",
	}
}

// Init initializes the plugin with configuration.
func (p *EchoPlugin) Init(ctx context.Context, config map[string]any) error {
	if prefix, ok := config["prefix"].(string); ok {
		p.prefix = prefix
	}
	if suffix, ok := config["suffix"].(string); ok {
		p.suffix = suffix
	}

	// Call base init (does nothing, but good practice)
	return p.BasePlugin.Init(ctx, config)
}

// Start starts the plugin.
func (p *EchoPlugin) Start(ctx context.Context) error {
	// Example: start a background goroutine or initialize resources
	fmt.Printf("EchoPlugin started with prefix=%q, suffix=%q\n", p.prefix, p.suffix)
	return p.BasePlugin.Start(ctx)
}

// Stop stops the plugin.
func (p *EchoPlugin) Stop(ctx context.Context) error {
	// Example: cleanup resources
	fmt.Println("EchoPlugin stopped")
	return p.BasePlugin.Stop(ctx)
}

// RegisterTools returns the tools provided by this plugin.
func (p *EchoPlugin) RegisterTools() []tools.Tool {
	return []tools.Tool{&echoTool{prefix: p.prefix, suffix: p.suffix}}
}

// RegisterMiddlewares returns the middlewares provided by this plugin.
func (p *EchoPlugin) RegisterMiddlewares() []middleware.Middleware {
	return []middleware.Middleware{&loggingMiddleware{}}
}

// echoTool is a simple tool that echoes back the input.
type echoTool struct {
	prefix string
	suffix string
}

func (t *echoTool) Name() string {
	return "echo"
}

func (t *echoTool) Description() string {
	return `Echo the input back to the user.

This is a simple demonstration tool that returns the input text with an optional prefix and suffix.
Use this tool when you want to test the plugin system or demonstrate tool invocation.`
}

func (t *echoTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"message": {
				"type": "string",
				"description": "The message to echo back"
			}
		},
		"required": ["message"]
	}`)
}

func (t *echoTool) Execute(ctx context.Context, input string) (string, error) {
	// Parse input
	var params struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	// Echo with prefix and suffix
	result := t.prefix + params.Message + t.suffix
	return result, nil
}

// loggingMiddleware is a simple middleware that logs agent lifecycle events.
type loggingMiddleware struct {
	middleware.MiddlewareWrapper
}

func (m *loggingMiddleware) Name() string {
	return "logging-middleware"
}

func (m *loggingMiddleware) BeforeAgent(ctx context.Context, state *middleware.State) error {
	fmt.Printf("[%s] BeforeAgent: thread=%s\n", time.Now().Format("15:04:05"), state.ThreadID)
	return m.MiddlewareWrapper.BeforeAgent(ctx, state)
}

func (m *loggingMiddleware) BeforeModel(ctx context.Context, state *middleware.State) error {
	fmt.Printf("[%s] BeforeModel: thread=%s, messages=%d\n",
		time.Now().Format("15:04:05"),
		state.ThreadID,
		len(state.Messages),
	)
	return m.MiddlewareWrapper.BeforeModel(ctx, state)
}

func (m *loggingMiddleware) AfterModel(ctx context.Context, state *middleware.State, response *middleware.Response) error {
	fmt.Printf("[%s] AfterModel: thread=%s, final_message_len=%d\n",
		time.Now().Format("15:04:05"),
		state.ThreadID,
		len(response.FinalMessage),
	)
	return m.MiddlewareWrapper.AfterModel(ctx, state, response)
}

func (m *loggingMiddleware) AfterAgent(ctx context.Context, state *middleware.State, response *middleware.Response) error {
	fmt.Printf("[%s] AfterAgent: thread=%s, error=%v\n",
		time.Now().Format("15:04:05"),
		state.ThreadID,
		response.Error,
	)
	return m.MiddlewareWrapper.AfterAgent(ctx, state, response)
}
