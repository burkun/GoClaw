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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
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
func (t *BashTool) Execute(ctx context.Context, input string) (string, error) {
	if !t.cfg.Enabled {
		return "Error: bash tool is disabled. Set sandbox.allow_host_bash=true in config to enable.", nil
	}

	var in bashInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", fmt.Errorf("bash: invalid input JSON: %w", err)
	}
	if strings.TrimSpace(in.Command) == "" {
		return "", fmt.Errorf("bash: command is required")
	}

	if err := validateCommand(in.Command); err != nil {
		return fmt.Sprintf("Error: %s", err.Error()), nil
	}

	resolvedCmd := in.Command
	if t.cfg.VirtualToHostReplacer != nil {
		mapped, err := t.cfg.VirtualToHostReplacer(resolvedCmd)
		if err != nil {
			return fmt.Sprintf("Error: %v", err), nil
		}
		resolvedCmd = mapped
	}
	if strings.TrimSpace(t.cfg.WorkspacePath) != "" {
		resolvedCmd = "cd " + shellQuote(t.cfg.WorkspacePath) + " && " + resolvedCmd
	}

	timeout := t.effectiveTimeout(ctx)
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "bash", "-c", resolvedCmd)
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	err := cmd.Run()

	formatOutput := func(includeExitCode bool, exitCode int) string {
		out := strings.TrimRight(stdoutBuf.String(), "\n")
		errOut := strings.TrimRight(stderrBuf.String(), "\n")
		var b strings.Builder
		if out != "" {
			b.WriteString(out)
		}
		if errOut != "" {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString("Std Error:\n")
			b.WriteString(errOut)
		}
		if includeExitCode {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(fmt.Sprintf("Exit Code: %d", exitCode))
		}
		if b.Len() == 0 {
			return "(no output)"
		}
		return b.String()
	}

	if runCtx.Err() == context.DeadlineExceeded {
		output := truncateMiddle(formatOutput(false, 0), t.cfg.MaxOutputChars)
		if strings.TrimSpace(output) == "" || output == "(no output)" {
			return fmt.Sprintf("Error: command timed out after %s", timeout), nil
		}
		return fmt.Sprintf("Error: command timed out after %s\n%s", timeout, output), nil
	}
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			output := truncateMiddle(formatOutput(true, exitErr.ExitCode()), t.cfg.MaxOutputChars)
			return output, nil
		}
		return "", fmt.Errorf("bash: execute command failed: %w", err)
	}
	return truncateMiddle(formatOutput(false, 0), t.cfg.MaxOutputChars), nil
}

// validateCommand checks the command against the dangerous-path denylist and
// rejects any absolute paths not covered by allowedSystemPathPrefixes or the
// virtual /mnt/user-data/ prefix.
func validateCommand(command string) error {
	for _, p := range extractAbsolutePathTokens(command) {
		if p == "/mnt/user-data" || strings.HasPrefix(p, "/mnt/user-data/") {
			continue
		}
		if hasAnyPrefix(p, allowedSystemPathPrefixes) {
			continue
		}
		if hasAnyPrefix(p, dangerousPathPrefixes) {
			return fmt.Errorf("permission denied: path not allowed: %s", p)
		}
		return fmt.Errorf("permission denied: absolute path not allowed: %s", p)
	}
	return nil
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

func hasAnyPrefix(v string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(v, p) {
			return true
		}
	}
	return false
}

func extractAbsolutePathTokens(command string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0)
	for i := 0; i < len(command); i++ {
		if command[i] != '/' {
			continue
		}
		if i > 0 {
			prev := command[i-1]
			if isPathChar(prev) || prev == ':' {
				continue
			}
		}
		j := i
		for j < len(command) && !isPathDelimiter(command[j]) {
			j++
		}
		token := command[i:j]
		if len(token) <= 1 {
			continue
		}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		out = append(out, token)
	}
	return out
}

func isPathChar(c byte) bool {
	return (c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') ||
		c == '_' || c == '-' || c == '.'
}

func isPathDelimiter(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\r', '"', '\'', '`', ';', '&', '|', '<', '>', '(', ')':
		return true
	default:
		return false
	}
}
