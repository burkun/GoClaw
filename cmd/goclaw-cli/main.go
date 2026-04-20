// GoClaw CLI - Interactive command-line interface for GoClaw AI Agent.
//
// Usage:
//
//	goclaw-cli                    # Start interactive session
//	goclaw-cli "your prompt"      # Start with initial prompt
//	goclaw-cli -p "your prompt"   # Print mode (non-interactive)
//	goclaw-cli --thread <id>      # Continue specific thread
//	goclaw-cli --continue         # Continue last thread
//
// Run 'goclaw-cli --help' for more options.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"goclaw/internal/agent"
	"goclaw/internal/agentconfig"
	"goclaw/internal/cli"
	"goclaw/internal/config"
	"goclaw/internal/logging"
)

var (
	// Build-time variables
	Version   = "dev"
	GitCommit = "unknown"
)

func main() {
	// Parse flags
	fs := flag.NewFlagSet("goclaw-cli", flag.ExitOnError)

	// Mode flags
	printMode := fs.Bool("p", false, "Print mode (non-interactive)")
	printModeLong := fs.Bool("print", false, "Print mode (non-interactive)")

	// Output flags
	outputFormat := fs.String("output-format", "text", "Output format: text, json, stream-json")
	jsonOutput := fs.Bool("json", false, "JSON output (shorthand for --output-format json)")

	// Thread flags
	threadID := fs.String("thread", "", "Thread ID to continue")
	continueThread := fs.Bool("continue", false, "Continue last thread")

	// Agent flags
	agentName := fs.String("agent", "", "Agent name to use")

	// Mode toggles
	noThinking := fs.Bool("no-thinking", false, "Disable thinking mode")
	planMode := fs.Bool("plan", false, "Enable plan mode")

	// Utility flags
	showVersion := fs.Bool("version", false, "Show version")
	showHelp := fs.Bool("help", false, "Show help")

	// Parse
	if err := fs.Parse(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing flags: %v\n", err)
		os.Exit(1)
	}

	// Handle --help
	if *showHelp {
		printHelp()
		os.Exit(0)
	}

	// Handle --version
	if *showVersion {
		fmt.Printf("goclaw-cli version %s (commit: %s)\n", Version, GitCommit)
		os.Exit(0)
	}

	// Get initial prompt (remaining args or first arg if not a flag)
	args := fs.Args()
	var initialPrompt string
	if len(args) > 0 {
		initialPrompt = strings.Join(args, " ")
	}

	// Initialize logging
	logging.Init("info")

	// Load config
	cfg, err := config.GetAppConfig()
	if err != nil {
		logging.Warn("failed to load config, using defaults", "error", err)
	}

	// Update log level from config
	if cfg != nil && cfg.LogLevel != "" {
		logging.SetLevel(cfg.LogLevel)
	}

	// Create or load agent
	var leadAgent agent.LeadAgent
	agentToUse := strings.TrimSpace(*agentName)
	if agentToUse == "" {
		agentToUse = "default"
	}

	// Try to load from agent configs
	agentLoader := agentconfig.DefaultLoader
	_, loadErr := agentLoader.LoadConfig(agentToUse)
	if loadErr == nil {
		leadAgent, err = agent.NewWithName(context.Background(), agentToUse)
	} else {
		// Fall back to default agent
		leadAgent, err = agent.New(context.Background())
	}

	if err != nil {
		logging.Error("failed to initialize agent", "error", err)
		os.Exit(1)
	}

	// Determine output format
	format := *outputFormat
	if *jsonOutput {
		format = "json"
	}

	// Determine print mode
	isPrintMode := *printMode || *printModeLong

	// Build CLI options
	opts := []cli.Option{
		cli.WithPrintMode(isPrintMode),
		cli.WithOutputFormat(format),
		cli.WithInitialPrompt(initialPrompt),
		cli.WithThreadID(*threadID),
		cli.WithContinueThread(*continueThread),
		cli.WithAgentName(agentToUse),
		cli.WithThinkingEnabled(!*noThinking),
		cli.WithPlanMode(*planMode),
		cli.WithOutput(os.Stdout),
		cli.WithErrorOutput(os.Stderr),
	}

	// Create CLI
	c := cli.New(cfg, leadAgent, opts...)

	// Set up context with signal handling
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Run CLI
	if err := c.Run(ctx); err != nil {
		logging.Error("CLI error", "error", err)
		os.Exit(1)
	}
}

func printHelp() {
	fmt.Printf(`goclaw-cli - Interactive AI Agent CLI

USAGE:
    goclaw-cli [OPTIONS] [PROMPT]

MODES:
    Interactive (default):
        goclaw-cli                    Start interactive session
        goclaw-cli "your prompt"      Start with initial prompt

    Print mode (non-interactive):
        goclaw-cli -p "your prompt"   Execute and print result
        goclaw-cli --print "prompt"   Same as -p

OPTIONS:
    -p, --print           Print mode (non-interactive)
    --output-format FMT   Output format: text, json, stream-json
    --json                JSON output (shorthand for --output-format json)

    --thread ID           Continue specific thread
    --continue            Continue last thread

    --agent NAME          Agent to use (default: default)
    --no-thinking         Disable thinking mode
    --plan                Enable plan mode

    --version             Show version
    --help                Show this help

EXAMPLES:
    # Interactive session
    goclaw-cli

    # Quick query
    goclaw-cli "explain this code"

    # Print mode with JSON output
    goclaw-cli -p --json "what is Go?"

    # Continue previous conversation
    goclaw-cli --continue

    # Use specific thread
    goclaw-cli --thread abc123 "continue from here"

INTERACTIVE COMMANDS:
    /help                 Show available commands
    /exit, /quit          Exit the CLI
    /clear                Clear the screen
    /new                  Start new conversation
    /thread [ID]          Show or switch thread
    /model [NAME]         Show or set model
    /think                Toggle thinking mode
    /plan                 Toggle plan mode

Version: %s
`, Version)
}
