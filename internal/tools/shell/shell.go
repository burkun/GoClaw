// Package shell implements the bash shell execution tool for GoClaw.
//
// Security model
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
const DefaultTimeout = 60 * time.Second

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

type bashInput struct {
	// Description is a brief rationale supplied by the model.
	Description string `json:"description"`
	// Command is the shell command to execute.
	Command string `json:"command"`
}

func (t *BashTool) Name() string { return "bash" }

func (t *BashTool) Description() string {
	return `Execute a bash command in a Linux environment.
- Use python to run Python code.
- Prefer a thread-local virtual environment in /mnt/user-data/workspace/.venv.
- Always use absolute virtual paths (under /mnt/user-data/) for files and directories.
- The tool is disabled in local-sandbox mode unless explicitly enabled.`
}

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

// Execute runs the bash command with security checks and timeout enforcement.
//
// TODO: implementation steps
//  1. If !t.cfg.Enabled → return "Error: bash tool is disabled in local-sandbox mode."
//  2. json.Unmarshal input into bashInput.
//  3. Call validateCommand(in.Command) to enforce denylist rules.
//  4. If t.cfg.VirtualToHostReplacer != nil, call it to resolve virtual paths.
//  5. If t.cfg.WorkspacePath != "", prepend "cd <quoted-workspace> && " to command.
//  6. Create a context with t.cfg.Timeout deadline using context.WithTimeout.
//  7. Use exec.CommandContext(ctx, "bash", "-c", resolvedCmd).CombinedOutput().
//  8. Call truncateMiddle(output, t.cfg.MaxOutputChars) on the raw output.
//  9. Return the (possibly truncated) string; forward non-zero exit codes as
//     content, not as Go errors, so the model can observe the failure.
func (t *BashTool) Execute(ctx context.Context, input string) (string, error) {
	if !t.cfg.Enabled {
		return "Error: bash tool is disabled. Set sandbox.allow_host_bash=true in config to enable.", nil
	}

	var in bashInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", fmt.Errorf("bash: invalid input JSON: %w", err)
	}

	if err := validateCommand(in.Command); err != nil {
		return fmt.Sprintf("Error: %s", err.Error()), nil
	}

	// TODO: implement steps 4-9 from the doc comment above.
	_ = ctx
	return "", fmt.Errorf("bash: not implemented")
}

// validateCommand checks the command against the dangerous-path denylist and
// rejects any absolute paths not covered by allowedSystemPathPrefixes or the
// virtual /mnt/user-data/ prefix.
//
// TODO: implementation steps
//  1. Extract all absolute path tokens from command using a simple regex
//     (same approach as DeerFlow: r"(?<![:\w])(?<!:/)/(?:[^\s"'`;&|<>()]+)").
//  2. For each token:
//     a. If it starts with /mnt/user-data/ → allowed (virtual path).
//     b. If it starts with an allowedSystemPathPrefixes entry → allowed.
//     c. If it starts with a dangerousPathPrefixes entry → return error.
//     d. Otherwise → return error (unknown absolute path).
//  3. Return nil if all tokens pass.
func validateCommand(command string) error {
	// TODO: implement – see doc comment above.
	_ = command
	return nil
}

// truncateMiddle performs a middle-truncation of output, preserving equal
// portions of the head and tail. The returned string is at most maxChars long.
//
// TODO: implementation steps
//  1. If len(output) <= maxChars, return output unchanged.
//  2. Compute kept = maxChars - len(marker).
//  3. head = output[:kept/2]; tail = output[len(output)-kept/2:].
//  4. Return head + marker + tail where marker =
//     "\n... [middle truncated: N chars skipped] ...\n".
func truncateMiddle(output string, maxChars int) string {
	// TODO: implement – see doc comment above.
	if maxChars <= 0 || len(output) <= maxChars {
		return output
	}
	_ = strings.NewReplacer // placeholder import usage
	return output[:maxChars]
}

// effectiveTimeout returns the timeout to use for a command execution,
// preferring ctx's existing deadline when it is earlier than t.cfg.Timeout.
func (t *BashTool) effectiveTimeout(ctx context.Context) time.Duration {
	// TODO: compare ctx.Deadline() with t.cfg.Timeout and return the shorter.
	_ = ctx
	return t.cfg.Timeout
}
