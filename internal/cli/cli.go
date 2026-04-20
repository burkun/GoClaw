// Package cli provides an interactive command-line interface for GoClaw.
// It implements a Claude Code-like terminal experience with:
// - Interactive multi-turn conversations
// - Streaming output with real-time token display
// - Tool execution visualization
// - Built-in commands (/help, /exit, /clear, etc.)
package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"goclaw/internal/agent"
	"goclaw/internal/config"
	"goclaw/internal/logging"

	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
)

// Version is the CLI version.
var Version = "dev"

// CLI holds the command-line interface state.
type CLI struct {
	cfg       *config.AppConfig
	agent     agent.LeadAgent
	session   *Session
	ui        *UI
	input     *InputHandler
	commands  *CommandRegistry
	output    io.Writer
	errOutput io.Writer

	// Options
	printMode       bool // Non-interactive mode
	outputFormat    string
	initialPrompt   string
	threadID        string
	continueThread  bool
	agentName       string
	thinkingEnabled bool
	planMode        bool
}

// Option configures the CLI.
type Option func(*CLI)

// WithPrintMode enables non-interactive print mode.
func WithPrintMode(enabled bool) Option {
	return func(c *CLI) { c.printMode = enabled }
}

// WithOutputFormat sets the output format (text, json, stream-json).
func WithOutputFormat(format string) Option {
	return func(c *CLI) { c.outputFormat = format }
}

// WithInitialPrompt sets the initial prompt to send.
func WithInitialPrompt(prompt string) Option {
	return func(c *CLI) { c.initialPrompt = prompt }
}

// WithThreadID sets the thread ID to continue.
func WithThreadID(threadID string) Option {
	return func(c *CLI) { c.threadID = threadID }
}

// WithContinueThread enables continuing the last thread.
func WithContinueThread(enabled bool) Option {
	return func(c *CLI) { c.continueThread = enabled }
}

// WithAgentName sets the agent to use.
func WithAgentName(name string) Option {
	return func(c *CLI) { c.agentName = name }
}

// WithThinkingEnabled enables/disables thinking mode.
func WithThinkingEnabled(enabled bool) Option {
	return func(c *CLI) { c.thinkingEnabled = enabled }
}

// WithPlanMode enables plan mode.
func WithPlanMode(enabled bool) Option {
	return func(c *CLI) { c.planMode = enabled }
}

// WithOutput sets the output writer.
func WithOutput(w io.Writer) Option {
	return func(c *CLI) { c.output = w }
}

// WithErrorOutput sets the error output writer.
func WithErrorOutput(w io.Writer) Option {
	return func(c *CLI) { c.errOutput = w }
}

// New creates a new CLI instance.
func New(cfg *config.AppConfig, agent agent.LeadAgent, opts ...Option) *CLI {
	c := &CLI{
		cfg:             cfg,
		agent:           agent,
		output:          os.Stdout,
		errOutput:       os.Stderr,
		outputFormat:    "text",
		thinkingEnabled: true,
	}

	for _, opt := range opts {
		opt(c)
	}

	c.ui = NewUI(c.output, c.errOutput, c.printMode)
	c.session = NewSession(cfg, c.threadID)
	c.input = NewInputHandler()
	c.commands = NewCommandRegistry()

	return c
}

// Run starts the CLI.
func (c *CLI) Run(ctx context.Context) error {
	// Set up signal handling
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Print mode: single query and exit
	if c.printMode {
		return c.runPrintMode(ctx)
	}

	// Interactive mode
	return c.runInteractiveMode(ctx)
}

// runPrintMode executes a single query and prints the result.
func (c *CLI) runPrintMode(ctx context.Context) error {
	if c.initialPrompt == "" {
		return fmt.Errorf("no prompt provided in print mode")
	}

	// Ensure thread ID
	threadID := c.threadID
	if threadID == "" {
		threadID = uuid.NewString()
	}

	runCfg := agent.RunConfig{
		ThreadID:        threadID,
		ThinkingEnabled: c.thinkingEnabled,
		IsPlanMode:      c.planMode,
		AgentName:       c.agentName,
	}

	state := &agent.ThreadState{
		Messages: []*schema.Message{},
	}

	// Add user message
	state.Messages = append(state.Messages, schema.UserMessage(c.initialPrompt))

	// Run agent
	eventCh, err := c.agent.Run(ctx, state, runCfg)
	if err != nil {
		return fmt.Errorf("agent run failed: %w", err)
	}

	// Process events
	var finalMessage string
	for event := range eventCh {
		switch event.Type {
		case agent.EventMessageDelta:
			if delta, ok := event.Payload.(agent.MessageDeltaPayload); ok {
				if c.outputFormat == "text" {
					fmt.Fprint(c.output, delta.Content)
				}
				finalMessage += delta.Content
			}
		case agent.EventCompleted:
			if c.outputFormat == "json" {
				// Output as JSON
				fmt.Fprintf(c.output, `{"status":"completed","message":%q}%s`, finalMessage, "\n")
			}
		case agent.EventError:
			if errPayload, ok := event.Payload.(agent.ErrorPayload); ok {
				if c.outputFormat == "json" {
					fmt.Fprintf(c.output, `{"status":"error","code":%q,"message":%q}%s`, errPayload.Code, errPayload.Message, "\n")
				} else {
					fmt.Fprintf(c.errOutput, "Error: %s\n", errPayload.Message)
				}
				return fmt.Errorf("%s: %s", errPayload.Code, errPayload.Message)
			}
		}
	}

	if c.outputFormat == "text" {
		fmt.Fprintln(c.output)
	}

	return nil
}

// runInteractiveMode starts the interactive REPL.
func (c *CLI) runInteractiveMode(ctx context.Context) error {
	// Print welcome
	c.ui.PrintWelcome()

	// Load or create thread
	if c.threadID != "" {
		if err := c.session.LoadThread(c.threadID); err != nil {
			logging.Warn("failed to load thread", "thread_id", c.threadID, "error", err)
		}
	} else if c.continueThread {
		if err := c.session.LoadLastThread(); err != nil {
			logging.Debug("no previous thread to continue", "error", err)
		}
	}

	// Main loop
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		// Read input
		input, err := c.input.ReadLine(c.session.threadID)
		if err != nil {
			if err == io.EOF {
				c.ui.Println("\nGoodbye!")
				return nil
			}
			return fmt.Errorf("read input: %w", err)
		}

		// Skip empty input
		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}

		// Handle commands
		if strings.HasPrefix(input, "/") {
			if handled, exit := c.handleCommand(input); exit {
				return nil
			} else if handled {
				continue
			}
		}

		// Send message to agent
		if err := c.sendMessage(ctx, input); err != nil {
			c.ui.PrintError(err)
			continue
		}
	}
}

// handleCommand processes a slash command.
func (c *CLI) handleCommand(input string) (handled bool, exit bool) {
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return false, false
	}

	cmd := strings.ToLower(parts[0])
	args := parts[1:]

	switch cmd {
	case "/exit", "/quit", "/q":
		c.ui.Println("Goodbye!")
		return true, true

	case "/help", "/?":
		c.printHelp()
		return true, false

	case "/clear", "/cls":
		c.ui.Clear()
		c.ui.PrintWelcome()
		return true, false

	case "/new":
		c.session = NewSession(c.cfg, "")
		c.ui.Println("Started new conversation.")
		return true, false

	case "/thread":
		if len(args) > 0 {
			threadID := args[0]
			if err := c.session.LoadThread(threadID); err != nil {
				c.ui.PrintError(fmt.Errorf("failed to load thread: %w", err))
			} else {
				c.ui.Printf("Switched to thread: %s\n", threadID)
			}
		} else {
			c.ui.Printf("Current thread: %s\n", c.session.threadID)
		}
		return true, false

	case "/model", "/m":
		if len(args) > 0 {
			// Set model
			c.session.modelName = args[0]
			c.ui.Printf("Model set to: %s\n", args[0])
		} else {
			modelName := "default"
			if c.cfg != nil && c.cfg.DefaultModel() != nil {
				modelName = c.cfg.DefaultModel().Name
			}
			c.ui.Printf("Current model: %s\n", modelName)
		}
		return true, false

	case "/think", "/thinking":
		c.thinkingEnabled = !c.thinkingEnabled
		status := "disabled"
		if c.thinkingEnabled {
			status = "enabled"
		}
		c.ui.Printf("Thinking mode: %s\n", status)
		return true, false

	case "/plan":
		c.planMode = !c.planMode
		status := "disabled"
		if c.planMode {
			status = "enabled"
		}
		c.ui.Printf("Plan mode: %s\n", status)
		return true, false

	default:
		c.ui.Printf("Unknown command: %s. Type /help for available commands.\n", cmd)
		return true, false
	}
}

// printHelp displays available commands.
func (c *CLI) printHelp() {
	help := `
GoClaw CLI - Interactive AI Agent

Commands:
  /help, /?      Show this help message
  /exit, /quit   Exit the CLI
  /clear         Clear the screen
  /new           Start a new conversation
  /thread [id]   Show or switch thread
  /model [name]  Show or set model
  /think         Toggle thinking mode
  /plan          Toggle plan mode

Keyboard Shortcuts:
  Ctrl+C         Cancel current operation
  Ctrl+D         Exit the CLI
  Enter          Send message
  Shift+Enter    New line (multiline)

Version: ` + Version + `
`
	c.ui.Print(help)
}

// sendMessage sends a message to the agent and streams the response.
func (c *CLI) sendMessage(ctx context.Context, content string) error {
	// Ensure thread ID
	if c.session.threadID == "" {
		c.session.threadID = uuid.NewString()
	}

	runCfg := agent.RunConfig{
		ThreadID:        c.session.threadID,
		ThinkingEnabled: c.thinkingEnabled,
		IsPlanMode:      c.planMode,
		AgentName:       c.agentName,
	}

	// Build state from session history
	state := &agent.ThreadState{
		Messages: c.session.GetMessages(),
	}

	// Add user message
	state.Messages = append(state.Messages, schema.UserMessage(content))

	// Show user message
	c.ui.PrintUserMessage(content)

	// Run agent
	eventCh, err := c.agent.Run(ctx, state, runCfg)
	if err != nil {
		return fmt.Errorf("agent run failed: %w", err)
	}

	// Process events
	var assistantMessage string
	c.ui.StartAssistantMessage()
	defer c.ui.EndAssistantMessage()

	for event := range eventCh {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		switch event.Type {
		case agent.EventMessageDelta:
			if delta, ok := event.Payload.(agent.MessageDeltaPayload); ok {
				if delta.IsThinking {
					c.ui.PrintThinking(delta.Content)
				} else {
					c.ui.PrintAssistantDelta(delta.Content)
				}
				assistantMessage += delta.Content
			}

		case agent.EventToolEvent:
			if tool, ok := event.Payload.(agent.ToolEventPayload); ok {
				if tool.Output == "" {
					c.ui.PrintToolCall(tool.ToolName, tool.Input)
				} else {
					c.ui.PrintToolResult(tool.ToolName, tool.Output, tool.IsError)
				}
			}

		case agent.EventTaskStarted, agent.EventTaskRunning,
			agent.EventTaskCompleted, agent.EventTaskFailed, agent.EventTaskTimedOut:
			if task, ok := event.Payload.(agent.TaskPayload); ok {
				c.ui.PrintTaskEvent(string(event.Type), task.TaskID, task.Subject, task.Status)
			}

		case agent.EventCompleted:
			// Save to session
			c.session.AddMessage("human", content)
			c.session.AddMessage("assistant", assistantMessage)

		case agent.EventError:
			if errPayload, ok := event.Payload.(agent.ErrorPayload); ok {
				c.ui.PrintError(fmt.Errorf("%s: %s", errPayload.Code, errPayload.Message))
				return fmt.Errorf("%s: %s", errPayload.Code, errPayload.Message)
			}
		}
	}

	return nil
}
