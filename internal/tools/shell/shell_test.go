package shell

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestValidateCommand_DangerousPath(t *testing.T) {
	err := validateCommand("cat /etc/passwd")
	if err == nil {
		t.Fatalf("expected dangerous path error")
	}
}

func TestValidateCommand_AllowedVirtualPath(t *testing.T) {
	err := validateCommand("cat /mnt/user-data/workspace/a.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBashToolExecute_Disabled(t *testing.T) {
	tool := NewBashTool(Config{Enabled: false})
	out, err := tool.Execute(context.Background(), `{"description":"x","command":"echo hi"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "disabled") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestBashToolExecute_Success(t *testing.T) {
	tool := NewBashTool(Config{Enabled: true, Timeout: 2 * time.Second})
	out, err := tool.Execute(context.Background(), `{"description":"x","command":"echo hello"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "hello") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestBashToolExecute_StdoutStderrSeparated(t *testing.T) {
	tool := NewBashTool(Config{Enabled: true, Timeout: 2 * time.Second})
	out, err := tool.Execute(context.Background(), `{"description":"x","command":"echo out; echo err >&2"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "out") {
		t.Fatalf("expected stdout content, got: %s", out)
	}
	if !strings.Contains(out, "Std Error:") || !strings.Contains(out, "err") {
		t.Fatalf("expected stderr section, got: %s", out)
	}
}

func TestBashToolExecute_NonZeroIncludesExitCode(t *testing.T) {
	tool := NewBashTool(Config{Enabled: true, Timeout: 2 * time.Second})
	out, err := tool.Execute(context.Background(), `{"description":"x","command":"echo boom >&2; exit 7"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Std Error:") || !strings.Contains(out, "boom") {
		t.Fatalf("expected stderr output, got: %s", out)
	}
	if !strings.Contains(out, "Exit Code: 7") {
		t.Fatalf("expected exit code in output, got: %s", out)
	}
}

func TestBashToolExecute_NoOutputMarker(t *testing.T) {
	tool := NewBashTool(Config{Enabled: true, Timeout: 2 * time.Second})
	out, err := tool.Execute(context.Background(), `{"description":"x","command":"true"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "(no output)" {
		t.Fatalf("expected no output marker, got: %s", out)
	}
}

func TestTruncateMiddle(t *testing.T) {
	in := strings.Repeat("a", 300)
	out := truncateMiddle(in, 120)
	if len(out) > 120 {
		t.Fatalf("expected truncated length <= 120, got %d", len(out))
	}
	if !strings.Contains(out, "middle truncated") {
		t.Fatalf("expected truncation marker, got %s", out)
	}
}

func TestValidateCommand_PipeInjection(t *testing.T) {
	// Pipe is allowed, but paths must still be validated
	// This should fail because /etc/passwd is a dangerous path
	err := validateCommand("cat /mnt/user-data/file.txt | cat /etc/passwd")
	if err == nil {
		t.Fatalf("expected dangerous path error")
	}
	if !strings.Contains(err.Error(), "dangerous") && !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("expected path error, got: %v", err)
	}

	// Pipe with safe paths should be allowed
	err = validateCommand("cat /mnt/user-data/file.txt | grep pattern")
	if err != nil {
		t.Fatalf("unexpected error for safe pipe command: %v", err)
	}
}

func TestValidateCommand_CommandSubstitution(t *testing.T) {
	// Test $() command substitution
	err := validateCommand("echo $(cat /etc/passwd)")
	if err == nil {
		t.Fatalf("expected command substitution error")
	}
	if !strings.Contains(err.Error(), "command substitution") {
		t.Fatalf("expected command substitution error, got: %v", err)
	}

	// Test backticks
	err = validateCommand("echo `cat /etc/passwd`")
	if err == nil {
		t.Fatalf("expected command substitution error")
	}
}

func TestValidateCommand_Redirection(t *testing.T) {
	// Redirection is allowed, but paths must still be validated
	// This should fail because /etc/passwd is a dangerous path
	err := validateCommand("cat < /etc/passwd")
	if err == nil {
		t.Fatalf("expected dangerous path error")
	}

	// Output redirection with dangerous path
	err = validateCommand("cat /mnt/user-data/file.txt > /tmp/out.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err) // /tmp is allowed
	}

	// Redirection with dangerous path
	err = validateCommand("cat /mnt/user-data/file.txt > /etc/bad.txt")
	if err == nil {
		t.Fatalf("expected dangerous path error")
	}
}

func TestValidateCommand_CommandChaining(t *testing.T) {
	// Command chaining is allowed, but paths must still be validated
	// This should fail because /etc/passwd is a dangerous path
	err := validateCommand("cat /mnt/user-data/file.txt; cat /etc/passwd")
	if err == nil {
		t.Fatalf("expected dangerous path error")
	}

	// Chaining with && and dangerous path
	err = validateCommand("cat /mnt/user-data/file.txt && cat /etc/passwd")
	if err == nil {
		t.Fatalf("expected dangerous path error")
	}

	// Chaining with || and dangerous path
	err = validateCommand("cat /mnt/user-data/file.txt || cat /etc/passwd")
	if err == nil {
		t.Fatalf("expected dangerous path error")
	}

	// Safe command chaining should be allowed
	err = validateCommand("echo hello; echo world")
	if err != nil {
		t.Fatalf("unexpected error for safe chaining: %v", err)
	}
}

func TestValidateCommand_EnvironmentVariable(t *testing.T) {
	// Test environment variable expansion that could be used to bypass
	// This should be allowed as long as the resolved path is safe
	err := validateCommand("cat $HOME/file.txt")
	if err == nil {
		t.Fatalf("expected error for environment variable")
	}

	// Test with dangerous path
	err = validateCommand("cat ${HOME}/file.txt")
	if err == nil {
		t.Fatalf("expected error for environment variable")
	}
}

func TestValidateCommand_SafeCommands(t *testing.T) {
	// These should pass validation
	tests := []string{
		"ls -la",
		"echo hello world",
		"cat /mnt/user-data/workspace/test.txt",
		"python script.py",
		"grep pattern /mnt/user-data/file.txt",
	}

	for _, cmd := range tests {
		err := validateCommand(cmd)
		if err != nil {
			t.Fatalf("unexpected error for safe command '%s': %v", cmd, err)
		}
	}
}

func TestExtractAbsolutePathTokens(t *testing.T) {
	tests := []struct {
		command  string
		expected []string
	}{
		{
			command:  "cat /etc/passwd",
			expected: []string{"/etc/passwd"},
		},
		{
			command:  "cat /mnt/user-data/file.txt | grep pattern",
			expected: []string{"/mnt/user-data/file.txt"},
		},
		{
			command:  "cat /mnt/user-data/in.txt > /tmp/out.txt",
			expected: []string{"/mnt/user-data/in.txt", "/tmp/out.txt"},
		},
		{
			command:  "cat /mnt/user-data/a.txt; cat /mnt/user-data/b.txt",
			expected: []string{"/mnt/user-data/a.txt", "/mnt/user-data/b.txt"},
		},
		{
			command:  "python --script=/mnt/user-data/test.py",
			expected: []string{"/mnt/user-data/test.py"},
		},
		{
			command:  "curl http://example.com/file.txt",
			expected: []string{}, // URLs should not be extracted as paths
		},
		{
			command:  "git clone ssh://git@github.com/repo.git",
			expected: []string{}, // SSH URLs should not be extracted
		},
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			paths := extractAbsolutePathTokens(tt.command)
			if len(paths) != len(tt.expected) {
				t.Fatalf("expected %d paths, got %d: %v", len(tt.expected), len(paths), paths)
			}
			for i, p := range paths {
				if p != tt.expected[i] {
					t.Fatalf("expected path[%d] = %s, got %s", i, tt.expected[i], p)
				}
			}
		})
	}
}
