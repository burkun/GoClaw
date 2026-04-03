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
