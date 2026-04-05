package local

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bookerbai/goclaw/internal/sandbox"
)

// newTestSandbox creates a LocalSandbox backed by a temporary directory.
// The returned cleanup function removes the temporary directory.
func newTestSandbox(t *testing.T) (*LocalSandbox, func()) {
	t.Helper()
	tmpDir := t.TempDir()
	baseDir := filepath.Join(tmpDir, "threads", "test-thread-001", "user-data")
	for _, sub := range []string{"workspace", "uploads", "outputs"} {
		if err := os.MkdirAll(filepath.Join(baseDir, sub), 0o755); err != nil {
			t.Fatalf("setup: create %s dir: %v", sub, err)
		}
	}
	sb := &LocalSandbox{
		id:       "local",
		threadID: "test-thread-001",
		baseDir:  baseDir,
		cfg: sandbox.SandboxConfig{
			Type:            sandbox.SandboxTypeLocal,
			AllowedCommands: []string{"echo", "ls", "cat", "python3", "python", "exit"},
			ExecTimeout:     5 * time.Second,
		},
	}
	return sb, func() { os.RemoveAll(tmpDir) }
}

// ---------------------------------------------------------------------------
// TestLocalSandbox_pathEscape
// ---------------------------------------------------------------------------

// TestLocalSandbox_pathEscape verifies that path traversal attempts are
// rejected before any filesystem access occurs. All variants MUST return an
// error – any successful return would indicate a security regression.
func TestLocalSandbox_pathEscape(t *testing.T) {
	sb, cleanup := newTestSandbox(t)
	defer cleanup()

	ctx := context.Background()

	escapePaths := []struct {
		name string
		path string
	}{
		{
			name: "dot-dot in middle of path",
			path: "/mnt/user-data/workspace/../../etc/passwd",
		},
		{
			name: "dot-dot at start of relative segment",
			path: "/mnt/user-data/../../../etc/shadow",
		},
		{
			name: "encoded dot-dot (backslash variant)",
			path: `/mnt/user-data/workspace\..\..\..\Windows\System32`,
		},
		{
			name: "absolute path outside prefix",
			path: "/etc/passwd",
		},
		{
			name: "empty path",
			path: "",
		},
	}

	for _, tc := range escapePaths {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// ReadFile must reject the path.
			_, err := sb.ReadFile(ctx, tc.path, 0, 0)
			if err == nil {
				t.Errorf("ReadFile(%q): expected error for path traversal, got nil", tc.path)
			}

			// WriteFile must also reject it.
			err = sb.WriteFile(ctx, tc.path, "pwned", false)
			if err == nil {
				t.Errorf("WriteFile(%q): expected error for path traversal, got nil", tc.path)
			}

			// ListDir must also reject it.
			_, err = sb.ListDir(ctx, tc.path, 1)
			if err == nil {
				t.Errorf("ListDir(%q): expected error for path traversal, got nil", tc.path)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestLocalSandbox_execute
// ---------------------------------------------------------------------------

// TestLocalSandbox_execute checks basic command execution: stdout capture,
// exit code propagation, denylist enforcement, and allowlist enforcement.
func TestLocalSandbox_execute(t *testing.T) {
	sb, cleanup := newTestSandbox(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("echo returns stdout", func(t *testing.T) {
		result, err := sb.Execute(ctx, "echo hello-goclaw")
		if err != nil {
			t.Fatalf("Execute: unexpected system error: %v", err)
		}
		if result.Error != nil {
			t.Fatalf("Execute: ExecuteResult.Error = %v", result.Error)
		}
		if !strings.Contains(result.Stdout, "hello-goclaw") {
			t.Errorf("expected stdout to contain %q, got %q", "hello-goclaw", result.Stdout)
		}
		if result.ExitCode != 0 {
			t.Errorf("expected exit code 0, got %d", result.ExitCode)
		}
	})

	t.Run("non-zero exit code is captured", func(t *testing.T) {
		result, err := sb.Execute(ctx, "exit 42")
		if err != nil {
			t.Fatalf("Execute: unexpected system error: %v", err)
		}
		if result.ExitCode != 42 {
			t.Errorf("expected exit code 42, got %d", result.ExitCode)
		}
	})

	t.Run("denylist command is rejected", func(t *testing.T) {
		// "sudo" is in the built-in denylist.
		result, err := sb.Execute(ctx, "sudo whoami")
		if err != nil {
			t.Fatalf("Execute: unexpected system error: %v", err)
		}
		// Must not succeed (ExitCode != 0 or Error set).
		if result.ExitCode == 0 && result.Error == nil {
			t.Error("expected denylist command to be rejected, but it succeeded")
		}
	})

	t.Run("command not in allowlist is rejected", func(t *testing.T) {
		// "curl" is not in the test sandbox's AllowedCommands list.
		result, err := sb.Execute(ctx, "curl http://example.com")
		if err != nil {
			t.Fatalf("Execute: unexpected system error: %v", err)
		}
		if result.ExitCode == 0 && result.Error == nil {
			t.Error("expected non-allowlisted command to be rejected, but it succeeded")
		}
	})
}

// ---------------------------------------------------------------------------
// TestLocalSandbox_readWrite
// ---------------------------------------------------------------------------

// TestLocalSandbox_readWrite verifies round-trip write → read, append mode,
// and StrReplace on files inside the virtual filesystem.
func TestLocalSandbox_readWrite(t *testing.T) {
	sb, cleanup := newTestSandbox(t)
	defer cleanup()

	ctx := context.Background()
	virtualPath := "/mnt/user-data/workspace/test.txt"

	t.Run("write and read round-trip", func(t *testing.T) {
		content := "hello from goclaw\n"
		if err := sb.WriteFile(ctx, virtualPath, content, false); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		got, err := sb.ReadFile(ctx, virtualPath, 0, 0)
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		if got != content {
			t.Errorf("content mismatch:\n  want %q\n  got  %q", content, got)
		}
	})

	t.Run("append mode", func(t *testing.T) {
		first := "line1\n"
		second := "line2\n"
		if err := sb.WriteFile(ctx, virtualPath, first, false); err != nil {
			t.Fatalf("WriteFile (overwrite): %v", err)
		}
		if err := sb.WriteFile(ctx, virtualPath, second, true); err != nil {
			t.Fatalf("WriteFile (append): %v", err)
		}
		got, err := sb.ReadFile(ctx, virtualPath, 0, 0)
		if err != nil {
			t.Fatalf("ReadFile after append: %v", err)
		}
		if !strings.Contains(got, first) || !strings.Contains(got, second) {
			t.Errorf("expected appended content, got %q", got)
		}
	})

	t.Run("overwrite clears previous content", func(t *testing.T) {
		if err := sb.WriteFile(ctx, virtualPath, "old content\n", false); err != nil {
			t.Fatalf("WriteFile (old): %v", err)
		}
		if err := sb.WriteFile(ctx, virtualPath, "new content\n", false); err != nil {
			t.Fatalf("WriteFile (new): %v", err)
		}
		got, err := sb.ReadFile(ctx, virtualPath, 0, 0)
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		if strings.Contains(got, "old content") {
			t.Errorf("overwrite failed: old content still present in %q", got)
		}
		if !strings.Contains(got, "new content") {
			t.Errorf("overwrite failed: new content not found in %q", got)
		}
	})

	t.Run("str_replace success", func(t *testing.T) {
		if err := sb.WriteFile(ctx, virtualPath, "foo bar baz\n", false); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		if err := sb.StrReplace(ctx, virtualPath, "bar", "qux", false); err != nil {
			t.Fatalf("StrReplace: %v", err)
		}
		got, err := sb.ReadFile(ctx, virtualPath, 0, 0)
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		if !strings.Contains(got, "qux") || strings.Contains(got, "bar") {
			t.Errorf("StrReplace result unexpected: %q", got)
		}
	})

	t.Run("str_replace returns error when old string not found", func(t *testing.T) {
		if err := sb.WriteFile(ctx, virtualPath, "hello world\n", false); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		err := sb.StrReplace(ctx, virtualPath, "nonexistent", "replacement", false)
		if err == nil {
			t.Error("expected error when old string not found, got nil")
		}
	})

	t.Run("list dir returns written file", func(t *testing.T) {
		if err := sb.WriteFile(ctx, "/mnt/user-data/workspace/list_test.txt", "data", false); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		infos, err := sb.ListDir(ctx, "/mnt/user-data/workspace", 1)
		if err != nil {
			t.Fatalf("ListDir: %v", err)
		}
		var found bool
		for _, fi := range infos {
			if fi.Name == "list_test.txt" {
				found = true
				if fi.IsDir {
					t.Errorf("list_test.txt should not be a directory")
				}
				if !strings.HasPrefix(fi.Path, "/mnt/user-data") {
					t.Errorf("virtual path should start with /mnt/user-data, got %q", fi.Path)
				}
			}
		}
		if !found {
			t.Error("list_test.txt not found in ListDir results")
		}
	})
}
