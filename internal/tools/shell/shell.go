// Package shell implements the bash shell execution tool for GoClaw.
//
// # Security model
//
// The shell tool is disabled in local-sandbox mode by default, matching
// DeerFlow's "sandbox.allow_host_bash" opt-in. When enabled it enforces:
//
//  1. Denylist check – rejects commands that reference dangerous paths or
//     system binaries outside a small allow-list.
//  2. Virtual path replacement – /mnt/user-data/* paths are translated to
//     per-thread host paths before the command is executed.
//  3. Timeout enforcement – every execution is wrapped in a context with a
//     configurable deadline (default 60 s).
//  4. Working directory – the command is prefixed with `cd <workspace> &&`
//     so relative paths are anchored to the thread workspace.
//
// The tool must NOT be treated as a secure sandbox boundary; it is an
// opt-in convenience for trusted local environments.
package shell

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// DefaultTimeout is the maximum duration a single bash command may run.
// Aligned with DeerFlow's 600s timeout.
const DefaultTimeout = 600 * time.Second

// DefaultMaxOutputChars is the maximum length of the command output returned
// to the model. Longer output is middle-truncated (head + tail preserved).
const DefaultMaxOutputChars = 20_000

// dangerousPathPrefixes lists host path prefixes that must never appear in a
// command submitted to the local bash tool. These are not exhaustive; the list
// is a best-effort guard against common accidental misuse.
var dangerousPathPrefixes = []string{
	"/etc/",
	"/root/",
	"/home/",
	"/private/",
	"/var/",
	"/Library/",
	"/System/",
	"/proc/",
	"/sys/",
}

// allowedSystemPathPrefixes lists system paths that ARE safe to reference in
// commands (executables, devices, etc.).
var allowedSystemPathPrefixes = []string{
	"/bin/",
	"/usr/bin/",
	"/usr/sbin/",
	"/sbin/",
	"/opt/homebrew/bin/",
	"/dev/",
	"/tmp/",
}

// Config holds runtime configuration for the BashTool.
type Config struct {
	// Enabled controls whether the bash tool may execute commands.
	// Default is false (disabled) to match DeerFlow's local-sandbox default.
	Enabled bool
	// Timeout is the per-command execution deadline.
	// Zero value uses DefaultTimeout.
	Timeout time.Duration
	// MaxOutputChars caps the output length returned to the model.
	// Zero value uses DefaultMaxOutputChars.
	MaxOutputChars int
	// WorkspacePath is the thread-local working directory.
	// The command is prefixed with "cd <WorkspacePath> && " when non-empty.
	WorkspacePath string
	// VirtualToHostReplacer translates /mnt/user-data/* virtual paths in the
	// command string to real host paths. If nil, no replacement is performed.
	VirtualToHostReplacer func(cmd string) (string, error)
}

// BashTool executes bash commands in a thread-local working directory.
// Implements tools.Tool.
type BashTool struct {
	cfg Config
}

// NewBashTool creates a BashTool with the given configuration.
func NewBashTool(cfg Config) *BashTool {
	if cfg.Timeout == 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.MaxOutputChars == 0 {
		cfg.MaxOutputChars = DefaultMaxOutputChars
	}
	return &BashTool{cfg: cfg}
}

// Name returns the tool name.
func (t *BashTool) Name() string { return "bash" }

// Description returns the tool description.
func (t *BashTool) Description() string {
	return `Execute a bash command in a Linux environment.
- Use python to run Python code.
- Prefer a thread-local virtual environment in /mnt/user-data/workspace/.venv.
- Always use absolute virtual paths (under /mnt/user-data/) for files and directories.
- The tool is disabled in local-sandbox mode unless explicitly enabled.`
}

// InputSchema returns the JSON schema for the tool input.
func (t *BashTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "required": ["description", "command"],
  "properties": {
    "description": {"type": "string", "description": "Explain why you are running this command."},
    "command":     {"type": "string", "description": "The bash command to execute."}
  }
}`)
}

// truncateMiddle performs a middle-truncation of output, preserving equal
// portions of the head and tail. The returned string is at most maxChars long.
func truncateMiddle(output string, maxChars int) string {
	if maxChars <= 0 || len(output) <= maxChars {
		return output
	}
	skipped := len(output) - maxChars
	marker := fmt.Sprintf("\n... [middle truncated: %d chars skipped] ...\n", skipped)
	if len(marker) >= maxChars {
		return output[:maxChars]
	}
	kept := maxChars - len(marker)
	headLen := kept / 2
	tailLen := kept - headLen
	return output[:headLen] + marker + output[len(output)-tailLen:]
}

// effectiveTimeout returns the timeout to use for a command execution,
// preferring ctx's existing deadline when it is earlier than t.cfg.Timeout.
func (t *BashTool) effectiveTimeout(ctx context.Context) time.Duration {
	timeout := t.cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining > 0 && remaining < timeout {
			return remaining
		}
	}
	return timeout
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
