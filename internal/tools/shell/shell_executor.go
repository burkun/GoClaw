package shell

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// bashInput represents the input JSON structure for bash commands.
type bashInput struct {
	// Description is a brief rationale supplied by the model.
	Description string `json:"description"`
	// Command is the shell command to execute.
	Command string `json:"command"`
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
		// Ensure workspace directory exists before cd
		if err := os.MkdirAll(t.cfg.WorkspacePath, 0o755); err == nil {
			resolvedCmd = "cd " + shellQuote(t.cfg.WorkspacePath) + " && " + resolvedCmd
		}
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
