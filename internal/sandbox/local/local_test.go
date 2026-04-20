package local

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"goclaw/internal/sandbox"
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

// ---------------------------------------------------------------------------
// TestLocalSandbox_ID
// ---------------------------------------------------------------------------

func TestLocalSandbox_ID(t *testing.T) {
	sb, cleanup := newTestSandbox(t)
	defer cleanup()

	if sb.ID() != "local" {
		t.Errorf("expected ID to be 'local', got %q", sb.ID())
	}
}

// ---------------------------------------------------------------------------
// TestLocalSandbox_Glob
// ---------------------------------------------------------------------------

func TestLocalSandbox_Glob(t *testing.T) {
	sb, cleanup := newTestSandbox(t)
	defer cleanup()

	ctx := context.Background()

	// Create test files
	testFiles := []string{
		"/mnt/user-data/workspace/file1.txt",
		"/mnt/user-data/workspace/file2.txt",
		"/mnt/user-data/workspace/data.json",
		"/mnt/user-data/workspace/subdir/file3.txt",
	}

	for _, path := range testFiles {
		if err := sb.WriteFile(ctx, path, "content", false); err != nil {
			t.Fatalf("setup: WriteFile(%q): %v", path, err)
		}
	}

	t.Run("match txt files only", func(t *testing.T) {
		matches, truncated, err := sb.Glob(ctx, "/mnt/user-data/workspace", "*.txt", false, 100)
		if err != nil {
			t.Fatalf("Glob: %v", err)
		}
		if truncated {
			t.Error("expected truncated=false for small result set")
		}
		if len(matches) != 2 {
			t.Errorf("expected 2 txt files, got %d: %v", len(matches), matches)
		}
		for _, m := range matches {
			if !strings.HasSuffix(m, ".txt") {
				t.Errorf("unexpected match: %q", m)
			}
		}
	})

	t.Run("include directories", func(t *testing.T) {
		matches, _, err := sb.Glob(ctx, "/mnt/user-data/workspace", "subdir", true, 100)
		if err != nil {
			t.Fatalf("Glob: %v", err)
		}
		if len(matches) == 0 {
			t.Error("expected to find subdir")
		}
	})

	t.Run("glob double star pattern", func(t *testing.T) {
		matches, _, err := sb.Glob(ctx, "/mnt/user-data/workspace", "**/*.txt", false, 100)
		if err != nil {
			t.Fatalf("Glob: %v", err)
		}
		// **/*.txt matches files in subdirectories, not root level
		if len(matches) != 1 {
			t.Errorf("expected 1 txt file in subdir, got %d: %v", len(matches), matches)
		}
	})

	t.Run("maxResults limit", func(t *testing.T) {
		matches, truncated, err := sb.Glob(ctx, "/mnt/user-data/workspace", "*", false, 1)
		if err != nil {
			t.Fatalf("Glob: %v", err)
		}
		if !truncated {
			t.Error("expected truncated=true when maxResults limit hit")
		}
		if len(matches) != 1 {
			t.Errorf("expected 1 result due to limit, got %d", len(matches))
		}
	})
}

// ---------------------------------------------------------------------------
// TestLocalSandbox_Grep
// ---------------------------------------------------------------------------

func TestLocalSandbox_Grep(t *testing.T) {
	sb, cleanup := newTestSandbox(t)
	defer cleanup()

	ctx := context.Background()

	// Create test files with searchable content
	files := map[string]string{
		"/mnt/user-data/workspace/file1.txt": "hello world\nfoo bar\nHELLO AGAIN",
		"/mnt/user-data/workspace/file2.txt": "another hello\nworld peace",
		"/mnt/user-data/workspace/data.log":  "ERROR: something failed\nINFO: hello there",
	}

	for path, content := range files {
		if err := sb.WriteFile(ctx, path, content, false); err != nil {
			t.Fatalf("setup: WriteFile(%q): %v", path, err)
		}
	}

	t.Run("literal search case-sensitive", func(t *testing.T) {
		matches, truncated, err := sb.Grep(ctx, "/mnt/user-data/workspace", "hello", "", true, true, 100)
		if err != nil {
			t.Fatalf("Grep: %v", err)
		}
		if truncated {
			t.Error("expected truncated=false for small result set")
		}
		// file1.txt has "hello world" (line 1)
		// file2.txt has "another hello" (line 1)
		// data.log has "hello there" (line 2)
		if len(matches) != 3 {
			t.Errorf("expected 3 matches (lowercase 'hello'), got %d", len(matches))
		}
	})

	t.Run("literal search case-insensitive", func(t *testing.T) {
		matches, _, err := sb.Grep(ctx, "/mnt/user-data/workspace", "hello", "", true, false, 100)
		if err != nil {
			t.Fatalf("Grep: %v", err)
		}
		// case-insensitive 'hello' matches:
		// file1.txt: "hello world" and "HELLO AGAIN"
		// file2.txt: "another hello"
		// data.log: "hello there"
		if len(matches) != 4 {
			t.Errorf("expected 4 matches (case-insensitive), got %d", len(matches))
		}
	})

	t.Run("regex search", func(t *testing.T) {
		matches, _, err := sb.Grep(ctx, "/mnt/user-data/workspace", "h.*o", "", false, true, 100)
		if err != nil {
			t.Fatalf("Grep: %v", err)
		}
		if len(matches) == 0 {
			t.Error("expected regex pattern to match")
		}
	})

	t.Run("with glob filter", func(t *testing.T) {
		matches, _, err := sb.Grep(ctx, "/mnt/user-data/workspace", "hello", "*.txt", true, false, 100)
		if err != nil {
			t.Fatalf("Grep: %v", err)
		}
		for _, m := range matches {
			if !strings.HasSuffix(m.Path, ".txt") {
				t.Errorf("expected only .txt files, got %q", m.Path)
			}
		}
	})

	t.Run("maxResults limit", func(t *testing.T) {
		matches, truncated, err := sb.Grep(ctx, "/mnt/user-data/workspace", "hello", "", true, false, 1)
		if err != nil {
			t.Fatalf("Grep: %v", err)
		}
		if !truncated {
			t.Error("expected truncated=true when maxResults limit hit")
		}
		if len(matches) != 1 {
			t.Errorf("expected 1 result due to limit, got %d", len(matches))
		}
	})
}

// ---------------------------------------------------------------------------
// TestLocalSandbox_UpdateFile
// ---------------------------------------------------------------------------

func TestLocalSandbox_UpdateFile(t *testing.T) {
	sb, cleanup := newTestSandbox(t)
	defer cleanup()

	ctx := context.Background()
	virtualPath := "/mnt/user-data/workspace/binary.dat"

	t.Run("write and read binary content", func(t *testing.T) {
		binaryContent := []byte{0x00, 0x01, 0x02, 0xFF, 0xFE, 0xFD}
		if err := sb.UpdateFile(ctx, virtualPath, binaryContent); err != nil {
			t.Fatalf("UpdateFile: %v", err)
		}

		// Read back using ReadFile
		got, err := sb.ReadFile(ctx, virtualPath, 0, 0)
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		if string(binaryContent) != got {
			t.Errorf("content mismatch:\n  want %v\n  got  %q", binaryContent, got)
		}
	})

	t.Run("overwrite binary content", func(t *testing.T) {
		newContent := []byte{0xAA, 0xBB, 0xCC}
		if err := sb.UpdateFile(ctx, virtualPath, newContent); err != nil {
			t.Fatalf("UpdateFile (overwrite): %v", err)
		}

		got, err := sb.ReadFile(ctx, virtualPath, 0, 0)
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		if string(newContent) != got {
			t.Errorf("overwrite failed:\n  want %v\n  got  %q", newContent, got)
		}
	})

	t.Run("reject skills path", func(t *testing.T) {
		// Create a sandbox with skillsPath
		tmpDir := t.TempDir()
		skillsDir := filepath.Join(tmpDir, "skills")
		if err := os.MkdirAll(skillsDir, 0o755); err != nil {
			t.Fatalf("setup: create skills dir: %v", err)
		}

		sbWithSkills := &LocalSandbox{
			id:         "local",
			threadID:   "test-thread",
			baseDir:    filepath.Join(tmpDir, "user-data"),
			skillsPath: skillsDir,
			cfg:        sandbox.SandboxConfig{Type: sandbox.SandboxTypeLocal},
		}

		err := sbWithSkills.UpdateFile(ctx, "/mnt/skills/file.txt", []byte("test"))
		if err == nil {
			t.Error("expected error when writing to skills path")
		}
		if !strings.Contains(err.Error(), "write access to skills path is not allowed") {
			t.Errorf("unexpected error message: %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// TestLocalSandbox_ReadFileLineRange
// ---------------------------------------------------------------------------

func TestLocalSandbox_ReadFileLineRange(t *testing.T) {
	sb, cleanup := newTestSandbox(t)
	defer cleanup()

	ctx := context.Background()
	virtualPath := "/mnt/user-data/workspace/lines.txt"

	// Create file with multiple lines
	content := "line1\nline2\nline3\nline4\nline5\n"
	if err := sb.WriteFile(ctx, virtualPath, content, false); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	t.Run("read full file", func(t *testing.T) {
		got, err := sb.ReadFile(ctx, virtualPath, 0, 0)
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		if got != content {
			t.Errorf("expected full content, got %q", got)
		}
	})

	t.Run("read first 2 lines", func(t *testing.T) {
		got, err := sb.ReadFile(ctx, virtualPath, 0, 2)
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		// strings.Join doesn't preserve trailing newline
		expected := "line1\nline2"
		if got != expected {
			t.Errorf("expected %q, got %q", expected, got)
		}
	})

	t.Run("read middle lines", func(t *testing.T) {
		got, err := sb.ReadFile(ctx, virtualPath, 1, 3)
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		// strings.Join doesn't preserve trailing newline
		expected := "line2\nline3"
		if got != expected {
			t.Errorf("expected %q, got %q", expected, got)
		}
	})

	t.Run("read last lines", func(t *testing.T) {
		got, err := sb.ReadFile(ctx, virtualPath, 3, 10)
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		// strings.Split("line1\nline2\nline3\nline4\nline5\n", "\n") gives
		// ["line1", "line2", "line3", "line4", "line5", ""]
		// Taking lines[3:10] gives ["line4", "line5", ""]
		// strings.Join adds back the newlines
		expected := "line4\nline5\n"
		if got != expected {
			t.Errorf("expected %q, got %q", expected, got)
		}
	})

	t.Run("startLine beyond file length", func(t *testing.T) {
		got, err := sb.ReadFile(ctx, virtualPath, 100, 200)
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		if got != "" {
			t.Errorf("expected empty string, got %q", got)
		}
	})

	t.Run("negative startLine", func(t *testing.T) {
		got, err := sb.ReadFile(ctx, virtualPath, -5, 3)
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		// strings.Join doesn't preserve trailing newline
		expected := "line1\nline2\nline3"
		if got != expected {
			t.Errorf("expected %q, got %q", expected, got)
		}
	})
}

// ---------------------------------------------------------------------------
// TestLocalSandbox_SymlinkSecurity
// ---------------------------------------------------------------------------

func TestLocalSandbox_SymlinkSecurity(t *testing.T) {
	sb, cleanup := newTestSandbox(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("symlink escape is blocked", func(t *testing.T) {
		// Create a symlink that points outside the sandbox
		realBaseDir := sb.baseDir
		outsideFile := filepath.Join(filepath.Dir(realBaseDir), "outside.txt")
		if err := os.WriteFile(outsideFile, []byte("secret"), 0o644); err != nil {
			t.Fatalf("setup: create outside file: %v", err)
		}

		// Create symlink inside sandbox pointing outside
		linkPath := filepath.Join(realBaseDir, "workspace", "escape_link")
		if err := os.Symlink(outsideFile, linkPath); err != nil {
			t.Fatalf("setup: create symlink: %v", err)
		}

		// Try to read through the symlink
		_, err := sb.ReadFile(ctx, "/mnt/user-data/workspace/escape_link", 0, 0)
		if err == nil {
			t.Error("expected error when reading symlink that escapes sandbox")
		}
		if !strings.Contains(err.Error(), "symlink") && !strings.Contains(err.Error(), "access denied") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("symlink inside sandbox is allowed", func(t *testing.T) {
		// Create a file inside sandbox
		if err := sb.WriteFile(ctx, "/mnt/user-data/workspace/target.txt", "content", false); err != nil {
			t.Fatalf("setup: WriteFile: %v", err)
		}

		// Create symlink to file inside sandbox
		realBaseDir := sb.baseDir
		targetFile := filepath.Join(realBaseDir, "workspace", "target.txt")
		linkPath := filepath.Join(realBaseDir, "workspace", "internal_link")
		if err := os.Symlink(targetFile, linkPath); err != nil {
			t.Fatalf("setup: create symlink: %v", err)
		}

		// Reading through the symlink should work
		content, err := sb.ReadFile(ctx, "/mnt/user-data/workspace/internal_link", 0, 0)
		if err != nil {
			t.Errorf("unexpected error reading internal symlink: %v", err)
		}
		if content != "content" {
			t.Errorf("unexpected content: %q", content)
		}
	})
}

// ---------------------------------------------------------------------------
// TestLocalSandbox_SkillsPath
// ---------------------------------------------------------------------------

func TestLocalSandbox_SkillsPath(t *testing.T) {
	tmpDir := t.TempDir()

	// Create skills directory
	skillsDir := filepath.Join(tmpDir, "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatalf("setup: create skills dir: %v", err)
	}

	// Create a skill file
	skillFile := filepath.Join(skillsDir, "skill1.txt")
	if err := os.WriteFile(skillFile, []byte("skill content"), 0o644); err != nil {
		t.Fatalf("setup: create skill file: %v", err)
	}

	// Create sandbox with skillsPath
	baseDir := filepath.Join(tmpDir, "threads", "test-thread", "user-data")
	for _, sub := range []string{"workspace", "uploads", "outputs"} {
		if err := os.MkdirAll(filepath.Join(baseDir, sub), 0o755); err != nil {
			t.Fatalf("setup: create %s dir: %v", sub, err)
		}
	}

	sb := &LocalSandbox{
		id:         "local",
		threadID:   "test-thread",
		baseDir:    baseDir,
		skillsPath: skillsDir,
		cfg:        sandbox.SandboxConfig{Type: sandbox.SandboxTypeLocal},
	}

	ctx := context.Background()

	t.Run("read from skills path", func(t *testing.T) {
		content, err := sb.ReadFile(ctx, "/mnt/skills/skill1.txt", 0, 0)
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		if content != "skill content" {
			t.Errorf("unexpected content: %q", content)
		}
	})

	t.Run("write to skills path is rejected", func(t *testing.T) {
		err := sb.WriteFile(ctx, "/mnt/skills/new.txt", "test", false)
		if err == nil {
			t.Error("expected error when writing to skills path")
		}
		if !strings.Contains(err.Error(), "write access to skills path is not allowed") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("str_replace on skills path is rejected", func(t *testing.T) {
		err := sb.StrReplace(ctx, "/mnt/skills/skill1.txt", "skill", "test", false)
		if err == nil {
			t.Error("expected error when modifying skills path")
		}
	})

	t.Run("list skills directory", func(t *testing.T) {
		infos, err := sb.ListDir(ctx, "/mnt/skills", 1)
		if err != nil {
			t.Fatalf("ListDir: %v", err)
		}
		if len(infos) == 0 {
			t.Error("expected to find files in skills directory")
		}
	})

	t.Run("skills path traversal is blocked", func(t *testing.T) {
		_, err := sb.ReadFile(ctx, "/mnt/skills/../etc/passwd", 0, 0)
		if err == nil {
			t.Error("expected error for path traversal")
		}
	})
}

// ---------------------------------------------------------------------------
// TestLocalSandbox_VirtualToReal
// ---------------------------------------------------------------------------

func TestLocalSandbox_VirtualToReal(t *testing.T) {
	sb, cleanup := newTestSandbox(t)
	defer cleanup()

	t.Run("valid user-data path", func(t *testing.T) {
		realPath, err := sb.virtualToReal("/mnt/user-data/workspace/file.txt")
		if err != nil {
			t.Fatalf("virtualToReal: %v", err)
		}
		expectedSuffix := filepath.Join("user-data", "workspace", "file.txt")
		if !strings.HasSuffix(realPath, expectedSuffix) {
			t.Errorf("expected path to end with %q, got %q", expectedSuffix, realPath)
		}
	})

	t.Run("path traversal is rejected", func(t *testing.T) {
		_, err := sb.virtualToReal("/mnt/user-data/workspace/../../../etc/passwd")
		if err == nil {
			t.Error("expected error for path traversal")
		}
	})

	t.Run("path outside allowed prefixes", func(t *testing.T) {
		_, err := sb.virtualToReal("/etc/passwd")
		if err == nil {
			t.Error("expected error for path outside allowed prefixes")
		}
	})

	t.Run("root user-data path", func(t *testing.T) {
		realPath, err := sb.virtualToReal("/mnt/user-data")
		if err != nil {
			t.Fatalf("virtualToReal: %v", err)
		}
		// On macOS, /var is a symlink to /private/var, so we need to compare
		// the evaluated (canonical) paths
		expectedBase, err := filepath.EvalSymlinks(sb.baseDir)
		if err != nil {
			t.Fatalf("filepath.EvalSymlinks: %v", err)
		}
		if realPath != expectedBase {
			t.Errorf("expected %q, got %q", expectedBase, realPath)
		}
	})
}

// ---------------------------------------------------------------------------
// TestLocalSandbox_ErrorHandling
// ---------------------------------------------------------------------------

func TestLocalSandbox_ErrorHandling(t *testing.T) {
	sb, cleanup := newTestSandbox(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("read non-existent file", func(t *testing.T) {
		_, err := sb.ReadFile(ctx, "/mnt/user-data/workspace/nonexistent.txt", 0, 0)
		if err == nil {
			t.Error("expected error when reading non-existent file")
		}
	})

	t.Run("write creates parent directories", func(t *testing.T) {
		// Write to a path with non-existent parent directories
		err := sb.WriteFile(ctx, "/mnt/user-data/workspace/deep/nested/dir/file.txt", "content", false)
		if err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		// Verify the file was created
		content, err := sb.ReadFile(ctx, "/mnt/user-data/workspace/deep/nested/dir/file.txt", 0, 0)
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		if content != "content" {
			t.Errorf("unexpected content: %q", content)
		}
	})

	t.Run("str_replace on non-existent file", func(t *testing.T) {
		err := sb.StrReplace(ctx, "/mnt/user-data/workspace/nonexistent.txt", "old", "new", false)
		if err == nil {
			t.Error("expected error when str_replace on non-existent file")
		}
	})

	t.Run("list non-existent directory", func(t *testing.T) {
		// ListDir uses WalkDir which returns nil when the directory doesn't exist
		// (it calls the callback with an error, but if the callback returns nil,
		// WalkDir also returns nil)
		infos, err := sb.ListDir(ctx, "/mnt/user-data/workspace/nonexistent", 1)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if len(infos) != 0 {
			t.Errorf("expected empty results, got %d items", len(infos))
		}
	})

	t.Run("glob on non-existent directory", func(t *testing.T) {
		// Glob uses WalkDir which returns nil when the directory doesn't exist
		matches, truncated, err := sb.Glob(ctx, "/mnt/user-data/workspace/nonexistent", "*.txt", false, 100)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if len(matches) != 0 {
			t.Errorf("expected empty results, got %d matches", len(matches))
		}
		if truncated {
			t.Error("expected truncated=false for empty result set")
		}
	})

	t.Run("grep on non-existent directory", func(t *testing.T) {
		// Grep uses WalkDir which returns nil when the directory doesn't exist
		matches, truncated, err := sb.Grep(ctx, "/mnt/user-data/workspace/nonexistent", "pattern", "", true, true, 100)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if len(matches) != 0 {
			t.Errorf("expected empty results, got %d matches", len(matches))
		}
		if truncated {
			t.Error("expected truncated=false for empty result set")
		}
	})
}

// ---------------------------------------------------------------------------
// TestLocalSandboxProvider
// ---------------------------------------------------------------------------

func TestLocalSandboxProvider(t *testing.T) {
	tmpDir := t.TempDir()
	baseDir := filepath.Join(tmpDir, ".goclaw")
	skillsDir := filepath.Join(tmpDir, "skills")

	// Create skills directory with a file
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatalf("setup: create skills dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, "skill.txt"), []byte("skill"), 0o644); err != nil {
		t.Fatalf("setup: create skill file: %v", err)
	}

	cfg := sandbox.SandboxConfig{
		Type:            sandbox.SandboxTypeLocal,
		AllowedCommands: []string{"echo", "ls"},
		ExecTimeout:     5 * time.Second,
	}

	provider := NewLocalSandboxProvider(cfg, baseDir, skillsDir)

	t.Run("Acquire creates sandbox", func(t *testing.T) {
		ctx := context.Background()
		sandboxID, err := provider.Acquire(ctx, "test-thread-001")
		if err != nil {
			t.Fatalf("Acquire: %v", err)
		}
		if sandboxID != "local" {
			t.Errorf("expected sandboxID 'local', got %q", sandboxID)
		}

		// Verify directories were created
		threadDir := filepath.Join(baseDir, "threads", "test-thread-001", "user-data")
		for _, sub := range []string{"workspace", "uploads", "outputs"} {
			subDir := filepath.Join(threadDir, sub)
			if _, err := os.Stat(subDir); os.IsNotExist(err) {
				t.Errorf("directory %q was not created", subDir)
			}
		}
	})

	t.Run("Get returns sandbox", func(t *testing.T) {
		sb := provider.Get("local")
		if sb == nil {
			t.Fatal("Get returned nil")
		}
		if sb.ID() != "local" {
			t.Errorf("expected ID 'local', got %q", sb.ID())
		}
	})

	t.Run("Get returns nil for unknown ID", func(t *testing.T) {
		sb := provider.Get("unknown-id")
		if sb != nil {
			t.Error("expected nil for unknown sandbox ID")
		}
	})

	t.Run("Acquire returns existing sandbox", func(t *testing.T) {
		ctx := context.Background()
		sandboxID, err := provider.Acquire(ctx, "test-thread-002")
		if err != nil {
			t.Fatalf("Acquire: %v", err)
		}
		if sandboxID != "local" {
			t.Errorf("expected sandboxID 'local', got %q", sandboxID)
		}

		// Should return the same sandbox instance
		sb1 := provider.Get("local")
		sb2 := provider.Get("local")
		if sb1 != sb2 {
			t.Error("expected same sandbox instance")
		}
	})

	t.Run("Release is no-op", func(t *testing.T) {
		ctx := context.Background()
		err := provider.Release(ctx, "local")
		if err != nil {
			t.Errorf("Release returned error: %v", err)
		}

		// Sandbox should still be accessible
		sb := provider.Get("local")
		if sb == nil {
			t.Error("sandbox should still be accessible after Release")
		}
	})

	t.Run("Shutdown clears sandbox", func(t *testing.T) {
		ctx := context.Background()
		err := provider.Shutdown(ctx)
		if err != nil {
			t.Fatalf("Shutdown: %v", err)
		}

		// Sandbox should no longer be accessible
		sb := provider.Get("local")
		if sb != nil {
			t.Error("expected nil after Shutdown")
		}

		// Can create a new sandbox after shutdown
		_, err = provider.Acquire(ctx, "test-thread-003")
		if err != nil {
			t.Fatalf("Acquire after Shutdown: %v", err)
		}
		sb = provider.Get("local")
		if sb == nil {
			t.Error("expected new sandbox after Acquire")
		}
	})
}

// ---------------------------------------------------------------------------
// TestLocalSandbox_ExecuteEdgeCases
// ---------------------------------------------------------------------------

func TestLocalSandbox_ExecuteEdgeCases(t *testing.T) {
	sb, cleanup := newTestSandbox(t)
	defer cleanup()

	ctx := context.Background()

	t.Run("command with stderr", func(t *testing.T) {
		result, err := sb.Execute(ctx, "echo error >&2")
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if !strings.Contains(result.Stderr, "error") {
			t.Errorf("expected stderr to contain 'error', got %q", result.Stderr)
		}
	})

	t.Run("command timeout", func(t *testing.T) {
		// Create sandbox with short timeout
		tmpDir := t.TempDir()
		baseDir := filepath.Join(tmpDir, "user-data")
		if err := os.MkdirAll(filepath.Join(baseDir, "workspace"), 0o755); err != nil {
			t.Fatalf("setup: %v", err)
		}

		shortTimeoutSb := &LocalSandbox{
			id:       "local",
			threadID: "test",
			baseDir:  baseDir,
			cfg: sandbox.SandboxConfig{
				Type:            sandbox.SandboxTypeLocal,
				AllowedCommands: []string{"sleep"},
				ExecTimeout:     1 * time.Second,
			},
		}

		result, err := shortTimeoutSb.Execute(ctx, "sleep 10")
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		// Command should be killed by timeout
		if result.ExitCode == 0 {
			t.Error("expected non-zero exit code for timeout")
		}
	})

	t.Run("empty allowlist blocks all commands", func(t *testing.T) {
		tmpDir := t.TempDir()
		baseDir := filepath.Join(tmpDir, "user-data")
		if err := os.MkdirAll(filepath.Join(baseDir, "workspace"), 0o755); err != nil {
			t.Fatalf("setup: %v", err)
		}

		noAllowlistSb := &LocalSandbox{
			id:       "local",
			threadID: "test",
			baseDir:  baseDir,
			cfg: sandbox.SandboxConfig{
				Type:            sandbox.SandboxTypeLocal,
				AllowedCommands: []string{}, // Empty allowlist
			},
		}

		result, err := noAllowlistSb.Execute(ctx, "echo test")
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if result.ExitCode == 0 && result.Error == nil {
			t.Error("expected command to be blocked with empty allowlist")
		}
	})

	t.Run("custom denied commands", func(t *testing.T) {
		tmpDir := t.TempDir()
		baseDir := filepath.Join(tmpDir, "user-data")
		if err := os.MkdirAll(filepath.Join(baseDir, "workspace"), 0o755); err != nil {
			t.Fatalf("setup: %v", err)
		}

		customDeniedSb := &LocalSandbox{
			id:       "local",
			threadID: "test",
			baseDir:  baseDir,
			cfg: sandbox.SandboxConfig{
				Type:            sandbox.SandboxTypeLocal,
				AllowedCommands: []string{"mycommand"},
				DeniedCommands:  []string{"mycommand"},
			},
		}

		result, err := customDeniedSb.Execute(ctx, "mycommand")
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if result.ExitCode == 0 && result.Error == nil {
			t.Error("expected custom denied command to be blocked")
		}
	})

	t.Run("host paths are masked in output", func(t *testing.T) {
		// Execute a command that would output the real path
		result, err := sb.Execute(ctx, "pwd")
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		// The output should contain virtual path, not real host path
		if strings.Contains(result.Stdout, sb.baseDir) {
			t.Errorf("host path leaked in stdout: %q", result.Stdout)
		}
		if strings.Contains(result.Stdout, "/mnt/user-data") {
			// This is expected - virtual path should be present
		} else {
			t.Logf("pwd output (may not contain /mnt/user-data on this system): %q", result.Stdout)
		}
	})
}
