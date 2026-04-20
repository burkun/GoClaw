package fs_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"goclaw/internal/tools/fs"
)

// newTestPaths creates a temporary per-thread directory structure and returns
// a PathMapping populated with the test paths. The returned cleanup function
// removes all created directories.
func newTestPaths(t *testing.T) (*fs.PathMapping, func()) {
	t.Helper()

	base := t.TempDir() // automatically cleaned up by testing framework

	workspace := filepath.Join(base, "workspace")
	uploads := filepath.Join(base, "uploads")
	outputs := filepath.Join(base, "outputs")

	for _, dir := range []string{workspace, uploads, outputs} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatalf("newTestPaths: MkdirAll %s: %v", dir, err)
		}
	}

	m := &fs.PathMapping{
		ThreadID:      "test-thread",
		WorkspacePath: workspace,
		UploadsPath:   uploads,
		OutputsPath:   outputs,
	}

	cleanup := func() {} // t.TempDir handles cleanup
	return m, cleanup
}

// writeHostFile is a test helper that writes content to a host file path
// (bypassing virtual-path resolution) so tests can set up fixture files.
func writeHostFile(t *testing.T, hostPath, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(hostPath), 0o750); err != nil {
		t.Fatalf("writeHostFile: MkdirAll: %v", err)
	}
	if err := os.WriteFile(hostPath, []byte(content), 0o644); err != nil {
		t.Fatalf("writeHostFile: WriteFile: %v", err)
	}
}

// readHostFile reads a file from a host path; used to verify WriteFileTool output.
func readHostFile(t *testing.T, hostPath string) string {
	t.Helper()
	data, err := os.ReadFile(hostPath)
	if err != nil {
		t.Fatalf("readHostFile: %v", err)
	}
	return string(data)
}

// makeInput encodes a map into a JSON string suitable for passing to Execute.
func makeInput(t *testing.T, fields map[string]any) string {
	t.Helper()
	data, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("makeInput: marshal: %v", err)
	}
	return string(data)
}

// ---------------------------------------------------------------------------
// ReadFileTool tests
// ---------------------------------------------------------------------------

// TestReadFile_Success verifies that ReadFileTool returns the correct file
// content when given a valid virtual path.
func TestReadFile_Success(t *testing.T) {
	paths, cleanup := newTestPaths(t)
	defer cleanup()

	tool := &fs.ReadFileTool{Paths: paths}

	// Write a fixture file to the workspace host path.
	hostFile := filepath.Join(paths.WorkspacePath, "hello.txt")
	writeHostFile(t, hostFile, "Hello, GoClaw!\n")

	input := makeInput(t, map[string]any{
		"description": "read hello.txt",
		"path":        "/mnt/user-data/workspace/hello.txt",
	})

	got, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}
	if got != "Hello, GoClaw!\n" {
		t.Errorf("Execute: got %q, want %q", got, "Hello, GoClaw!\n")
	}
}

// TestReadFile_NotFound verifies that ReadFileTool returns a user-facing error
// string (not a Go error) when the file does not exist.
func TestReadFile_NotFound(t *testing.T) {
	paths, cleanup := newTestPaths(t)
	defer cleanup()

	tool := &fs.ReadFileTool{Paths: paths}

	input := makeInput(t, map[string]any{
		"description": "read missing.txt",
		"path":        "/mnt/user-data/workspace/missing.txt",
	})

	got, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: unexpected Go error: %v", err)
	}
	if len(got) == 0 || got[:5] != "Error" {
		t.Errorf("Execute: expected error string, got %q", got)
	}
}

// TestReadFile_LineRange verifies that start_line/end_line restrict output to
// the requested line range.
func TestReadFile_LineRange(t *testing.T) {
	paths, cleanup := newTestPaths(t)
	defer cleanup()

	tool := &fs.ReadFileTool{Paths: paths}

	hostFile := filepath.Join(paths.WorkspacePath, "multiline.txt")
	writeHostFile(t, hostFile, "line1\nline2\nline3\nline4\nline5\n")

	startLine := 2
	endLine := 4
	input := makeInput(t, map[string]any{
		"description": "read lines 2-4",
		"path":        "/mnt/user-data/workspace/multiline.txt",
		"start_line":  startLine,
		"end_line":    endLine,
	})

	got, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}
	want := "line2\nline3\nline4"
	if got != want {
		t.Errorf("Execute: got %q, want %q", got, want)
	}
}

func TestReadFile_MaxChars_Configurable(t *testing.T) {
	paths, cleanup := newTestPaths(t)
	defer cleanup()

	tool := &fs.ReadFileTool{Paths: paths, MaxChars: 5}

	hostFile := filepath.Join(paths.WorkspacePath, "long.txt")
	writeHostFile(t, hostFile, "1234567890")

	input := makeInput(t, map[string]any{
		"description": "read long.txt",
		"path":        "/mnt/user-data/workspace/long.txt",
	})

	got, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}
	want := "12345\n... (truncated)"
	if got != want {
		t.Errorf("Execute: got %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// WriteFileTool tests
// ---------------------------------------------------------------------------

// TestWriteFile_Success verifies that WriteFileTool creates a file with the
// correct content and returns "OK".
func TestWriteFile_Success(t *testing.T) {
	paths, cleanup := newTestPaths(t)
	defer cleanup()

	tool := &fs.WriteFileTool{Paths: paths}

	input := makeInput(t, map[string]any{
		"description": "write output.txt",
		"path":        "/mnt/user-data/outputs/output.txt",
		"content":     "result data\n",
	})

	got, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}
	if got != "OK" {
		t.Errorf("Execute: got %q, want %q", got, "OK")
	}

	hostFile := filepath.Join(paths.OutputsPath, "output.txt")
	content := readHostFile(t, hostFile)
	if content != "result data\n" {
		t.Errorf("host file content: got %q, want %q", content, "result data\n")
	}
}

// TestWriteFile_Append verifies that WriteFileTool appends content when
// append=true.
func TestWriteFile_Append(t *testing.T) {
	paths, cleanup := newTestPaths(t)
	defer cleanup()

	tool := &fs.WriteFileTool{Paths: paths}

	hostFile := filepath.Join(paths.WorkspacePath, "log.txt")
	writeHostFile(t, hostFile, "first line\n")

	input := makeInput(t, map[string]any{
		"description": "append to log.txt",
		"path":        "/mnt/user-data/workspace/log.txt",
		"content":     "second line\n",
		"append":      true,
	})

	if _, err := tool.Execute(context.Background(), input); err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}

	content := readHostFile(t, hostFile)
	want := "first line\nsecond line\n"
	if content != want {
		t.Errorf("host file content: got %q, want %q", content, want)
	}
}

// ---------------------------------------------------------------------------
// Path traversal / security tests
// ---------------------------------------------------------------------------

// TestReadFile_PathTraversal_DotDot verifies that paths containing ".."
// segments are rejected with a permission error.
func TestReadFile_PathTraversal_DotDot(t *testing.T) {
	paths, cleanup := newTestPaths(t)
	defer cleanup()

	tool := &fs.ReadFileTool{Paths: paths}

	// Attempt to escape workspace via "../../../etc/passwd".
	input := makeInput(t, map[string]any{
		"description": "traversal attempt",
		"path":        "/mnt/user-data/workspace/../../../etc/passwd",
	})

	got, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: unexpected Go error: %v", err)
	}
	// The tool should return an "Error:" string, not the file content.
	if len(got) < 5 || got[:5] != "Error" {
		t.Errorf("Expected error string for traversal path, got: %q", got)
	}
}

// TestReadFile_OutsideVirtualRoot verifies that paths outside /mnt/user-data/
// are rejected even without ".." segments.
func TestReadFile_OutsideVirtualRoot(t *testing.T) {
	paths, cleanup := newTestPaths(t)
	defer cleanup()

	tool := &fs.ReadFileTool{Paths: paths}

	input := makeInput(t, map[string]any{
		"description": "outside root attempt",
		"path":        "/etc/hosts",
	})

	got, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: unexpected Go error: %v", err)
	}
	if len(got) < 5 || got[:5] != "Error" {
		t.Errorf("Expected error string for path outside virtual root, got: %q", got)
	}
}

// TestWriteFile_PathTraversal verifies that WriteFileTool also rejects
// path-traversal attempts.
func TestWriteFile_PathTraversal(t *testing.T) {
	paths, cleanup := newTestPaths(t)
	defer cleanup()

	tool := &fs.WriteFileTool{Paths: paths}

	input := makeInput(t, map[string]any{
		"description": "traversal write attempt",
		"path":        "/mnt/user-data/workspace/../../evil.txt",
		"content":     "should not be written",
	})

	got, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: unexpected Go error: %v", err)
	}
	if len(got) < 5 || got[:5] != "Error" {
		t.Errorf("Expected error string for traversal path, got: %q", got)
	}

	// Verify that no file was written outside the allowed roots.
	escapedPath := filepath.Join(paths.WorkspacePath, "..", "..", "evil.txt")
	if _, statErr := os.Stat(escapedPath); statErr == nil {
		t.Errorf("File was written outside allowed root: %s", escapedPath)
	}
}

// TestResolveVirtualPath_Mapping verifies that ResolveVirtualPath correctly
// maps each virtual sub-path to the corresponding host directory.
func TestResolveVirtualPath_Mapping(t *testing.T) {
	paths, cleanup := newTestPaths(t)
	defer cleanup()

	cases := []struct {
		virtual string
		wantDir string
	}{
		{"/mnt/user-data/workspace/foo.txt", paths.WorkspacePath},
		{"/mnt/user-data/uploads/bar.txt", paths.UploadsPath},
		{"/mnt/user-data/outputs/baz.txt", paths.OutputsPath},
	}

	for _, tc := range cases {
		got, err := fs.ResolveVirtualPath(tc.virtual, paths)
		if err != nil {
			t.Errorf("ResolveVirtualPath(%q): unexpected error: %v", tc.virtual, err)
			continue
		}
		if !filepath.HasPrefix(got, tc.wantDir) {
			t.Errorf("ResolveVirtualPath(%q): got %q, expected path under %q", tc.virtual, got, tc.wantDir)
		}
	}
}

func TestWriteFile_ConcurrentAppend_NoDataLoss(t *testing.T) {
	paths, cleanup := newTestPaths(t)
	defer cleanup()

	tool := &fs.WriteFileTool{Paths: paths}
	hostFile := filepath.Join(paths.WorkspacePath, "concurrent.log")

	const workers = 20
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		i := i
		go func() {
			defer wg.Done()
			input := makeInput(t, map[string]any{
				"description": "append concurrent line",
				"path":        "/mnt/user-data/workspace/concurrent.log",
				"content":     fmt.Sprintf("line-%02d\n", i),
				"append":      true,
			})
			got, err := tool.Execute(context.Background(), input)
			if err != nil {
				t.Errorf("Execute: unexpected Go error: %v", err)
				return
			}
			if got != "OK" {
				t.Errorf("Execute: got %q, want OK", got)
			}
		}()
	}
	wg.Wait()

	content := readHostFile(t, hostFile)
	lines := strings.Split(strings.TrimSpace(content), "\n")
	if len(lines) != workers {
		t.Fatalf("expected %d lines, got %d; content=%q", workers, len(lines), content)
	}
}

// ---------------------------------------------------------------------------
// ReadFileTool method tests
// ---------------------------------------------------------------------------

func TestReadFileTool_Name(t *testing.T) {
	tool := &fs.ReadFileTool{}
	if tool.Name() != "read_file" {
		t.Errorf("expected name 'read_file', got %q", tool.Name())
	}
}

func TestReadFileTool_Description(t *testing.T) {
	tool := &fs.ReadFileTool{}
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
}

func TestReadFileTool_InputSchema(t *testing.T) {
	tool := &fs.ReadFileTool{}
	schema := tool.InputSchema()
	if len(schema) == 0 {
		t.Error("expected non-empty input schema")
	}
}

// ---------------------------------------------------------------------------
// WriteFileTool method tests
// ---------------------------------------------------------------------------

func TestWriteFileTool_Name(t *testing.T) {
	tool := &fs.WriteFileTool{}
	if tool.Name() != "write_file" {
		t.Errorf("expected name 'write_file', got %q", tool.Name())
	}
}

func TestWriteFileTool_Description(t *testing.T) {
	tool := &fs.WriteFileTool{}
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
}

func TestWriteFileTool_InputSchema(t *testing.T) {
	tool := &fs.WriteFileTool{}
	schema := tool.InputSchema()
	if len(schema) == 0 {
		t.Error("expected non-empty input schema")
	}
}

// ---------------------------------------------------------------------------
// EditFileTool tests
// ---------------------------------------------------------------------------

func TestEditFileTool_Name(t *testing.T) {
	tool := &fs.EditFileTool{}
	if tool.Name() != "edit_file" {
		t.Errorf("expected name 'edit_file', got %q", tool.Name())
	}
}

func TestEditFileTool_Description(t *testing.T) {
	tool := &fs.EditFileTool{}
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
}

func TestEditFileTool_InputSchema(t *testing.T) {
	tool := &fs.EditFileTool{}
	schema := tool.InputSchema()
	if len(schema) == 0 {
		t.Error("expected non-empty input schema")
	}
}

func TestEditFileTool_Execute_Success(t *testing.T) {
	paths, cleanup := newTestPaths(t)
	defer cleanup()

	tool := &fs.EditFileTool{Paths: paths}

	// Write initial file
	hostFile := filepath.Join(paths.WorkspacePath, "edit.txt")
	writeHostFile(t, hostFile, "Hello World\n")

	input := makeInput(t, map[string]any{
		"description": "edit file",
		"path":        "/mnt/user-data/workspace/edit.txt",
		"old_str":     "World",
		"new_str":     "GoClaw",
	})

	got, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}
	if got != "OK" {
		t.Errorf("Execute: got %q, want OK", got)
	}

	// Verify content
	content := readHostFile(t, hostFile)
	if content != "Hello GoClaw\n" {
		t.Errorf("content: got %q, want %q", content, "Hello GoClaw\n")
	}
}

func TestEditFileTool_Execute_ReplaceAll(t *testing.T) {
	paths, cleanup := newTestPaths(t)
	defer cleanup()

	tool := &fs.EditFileTool{Paths: paths}

	hostFile := filepath.Join(paths.WorkspacePath, "replace.txt")
	writeHostFile(t, hostFile, "foo foo foo\n")

	input := makeInput(t, map[string]any{
		"description": "replace all",
		"path":        "/mnt/user-data/workspace/replace.txt",
		"old_str":     "foo",
		"new_str":     "bar",
		"replace_all": true,
	})

	got, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}
	if got != "OK" {
		t.Errorf("Execute: got %q, want OK", got)
	}

	content := readHostFile(t, hostFile)
	if content != "bar bar bar\n" {
		t.Errorf("content: got %q", content)
	}
}

func TestEditFileTool_Execute_OldStrNotFound(t *testing.T) {
	paths, cleanup := newTestPaths(t)
	defer cleanup()

	tool := &fs.EditFileTool{Paths: paths}

	hostFile := filepath.Join(paths.WorkspacePath, "notfound.txt")
	writeHostFile(t, hostFile, "Hello World\n")

	input := makeInput(t, map[string]any{
		"description": "edit file",
		"path":        "/mnt/user-data/workspace/notfound.txt",
		"old_str":     "NotExist",
		"new_str":     "New",
	})

	got, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}
	if len(got) < 5 || got[:5] != "Error" {
		t.Errorf("expected error string, got %q", got)
	}
}

func TestEditFileTool_Execute_MultipleWithoutReplaceAll(t *testing.T) {
	paths, cleanup := newTestPaths(t)
	defer cleanup()

	tool := &fs.EditFileTool{Paths: paths}

	hostFile := filepath.Join(paths.WorkspacePath, "multiple.txt")
	writeHostFile(t, hostFile, "foo foo foo\n")

	input := makeInput(t, map[string]any{
		"description": "edit file",
		"path":        "/mnt/user-data/workspace/multiple.txt",
		"old_str":     "foo",
		"new_str":     "bar",
	})

	got, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}
	if len(got) < 5 || got[:5] != "Error" {
		t.Errorf("expected error string for multiple matches, got %q", got)
	}
}

func TestEditFileTool_Execute_FileNotFound(t *testing.T) {
	paths, cleanup := newTestPaths(t)
	defer cleanup()

	tool := &fs.EditFileTool{Paths: paths}

	input := makeInput(t, map[string]any{
		"description": "edit missing file",
		"path":        "/mnt/user-data/workspace/missing.txt",
		"old_str":     "old",
		"new_str":     "new",
	})

	got, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}
	if len(got) < 5 || got[:5] != "Error" {
		t.Errorf("expected error string, got %q", got)
	}
}

func TestEditFileTool_Execute_InvalidJSON(t *testing.T) {
	tool := &fs.EditFileTool{}
	_, err := tool.Execute(context.Background(), `{invalid json`)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestEditFileTool_Execute_PathTraversal(t *testing.T) {
	paths, cleanup := newTestPaths(t)
	defer cleanup()

	tool := &fs.EditFileTool{Paths: paths}

	input := makeInput(t, map[string]any{
		"description": "traversal attempt",
		"path":        "/mnt/user-data/workspace/../../etc/passwd",
		"old_str":     "old",
		"new_str":     "new",
	})

	got, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}
	if len(got) < 5 || got[:5] != "Error" {
		t.Errorf("expected error string for path traversal, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// ListDirTool tests
// ---------------------------------------------------------------------------

func TestListDirTool_Name(t *testing.T) {
	tool := &fs.ListDirTool{}
	if tool.Name() != "list_dir" {
		t.Errorf("expected name 'list_dir', got %q", tool.Name())
	}
}

func TestListDirTool_Description(t *testing.T) {
	tool := &fs.ListDirTool{}
	if tool.Description() == "" {
		t.Error("expected non-empty description")
	}
}

func TestListDirTool_InputSchema(t *testing.T) {
	tool := &fs.ListDirTool{}
	schema := tool.InputSchema()
	if len(schema) == 0 {
		t.Error("expected non-empty input schema")
	}
}

func TestListDirTool_Execute_Success(t *testing.T) {
	paths, cleanup := newTestPaths(t)
	defer cleanup()

	tool := &fs.ListDirTool{Paths: paths}

	// Create test structure
	writeHostFile(t, filepath.Join(paths.WorkspacePath, "file1.txt"), "content1")
	writeHostFile(t, filepath.Join(paths.WorkspacePath, "file2.txt"), "content2")
	os.MkdirAll(filepath.Join(paths.WorkspacePath, "subdir"), 0o755)
	writeHostFile(t, filepath.Join(paths.WorkspacePath, "subdir", "file3.txt"), "content3")

	input := makeInput(t, map[string]any{
		"description": "list workspace",
		"path":        "/mnt/user-data/workspace",
	})

	got, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}

	// Check output contains expected items
	if !strings.Contains(got, "file1.txt") {
		t.Error("expected output to contain file1.txt")
	}
	if !strings.Contains(got, "file2.txt") {
		t.Error("expected output to contain file2.txt")
	}
	if !strings.Contains(got, "subdir/") {
		t.Error("expected output to contain subdir/")
	}
}

func TestListDirTool_Execute_EmptyDirectory(t *testing.T) {
	paths, cleanup := newTestPaths(t)
	defer cleanup()

	tool := &fs.ListDirTool{Paths: paths}

	input := makeInput(t, map[string]any{
		"description": "list empty workspace",
		"path":        "/mnt/user-data/workspace",
	})

	got, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}
	if got != "(empty)" {
		t.Errorf("expected '(empty)', got %q", got)
	}
}

func TestListDirTool_Execute_DirectoryNotFound(t *testing.T) {
	paths, cleanup := newTestPaths(t)
	defer cleanup()

	tool := &fs.ListDirTool{Paths: paths}

	input := makeInput(t, map[string]any{
		"description": "list missing directory",
		"path":        "/mnt/user-data/workspace/notexist",
	})

	got, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}
	if len(got) < 5 || got[:5] != "Error" {
		t.Errorf("expected error string, got %q", got)
	}
}

func TestListDirTool_Execute_InvalidJSON(t *testing.T) {
	tool := &fs.ListDirTool{}
	_, err := tool.Execute(context.Background(), `{invalid json`)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestListDirTool_Execute_PathTraversal(t *testing.T) {
	paths, cleanup := newTestPaths(t)
	defer cleanup()

	tool := &fs.ListDirTool{Paths: paths}

	input := makeInput(t, map[string]any{
		"description": "traversal attempt",
		"path":        "/mnt/user-data/workspace/../../etc",
	})

	got, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}
	if len(got) < 5 || got[:5] != "Error" {
		t.Errorf("expected error string for path traversal, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// maskHostPaths tests
// ---------------------------------------------------------------------------

func TestMaskHostPaths(t *testing.T) {
	// Verify the VirtualPathPrefix constant
	if fs.VirtualPathPrefix != "/mnt/user-data" {
		t.Errorf("VirtualPathPrefix: got %q, want /mnt/user-data", fs.VirtualPathPrefix)
	}
}

// ---------------------------------------------------------------------------
// pathThreadID tests
// ---------------------------------------------------------------------------

func TestPathThreadID(t *testing.T) {
	// Test through WriteFileTool behavior
	paths1, cleanup1 := newTestPaths(t)
	defer cleanup1()
	paths1.ThreadID = "custom-thread"

	tool := &fs.WriteFileTool{Paths: paths1}
	input := makeInput(t, map[string]any{
		"description": "test",
		"path":        "/mnt/user-data/workspace/test.txt",
		"content":     "test",
	})

	_, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Errorf("Execute failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Additional ReadFileTool tests
// ---------------------------------------------------------------------------

func TestReadFileTool_Execute_InvalidJSON(t *testing.T) {
	tool := &fs.ReadFileTool{}
	_, err := tool.Execute(context.Background(), `{invalid json`)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestReadFileTool_Execute_PermissionDenied(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping as root")
	}

	paths, cleanup := newTestPaths(t)
	defer cleanup()

	tool := &fs.ReadFileTool{Paths: paths}

	// Create a file with no read permissions
	hostFile := filepath.Join(paths.WorkspacePath, "noperm.txt")
	writeHostFile(t, hostFile, "secret")
	os.Chmod(hostFile, 0o000)

	input := makeInput(t, map[string]any{
		"description": "read no perm",
		"path":        "/mnt/user-data/workspace/noperm.txt",
	})

	got, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}
	if len(got) < 5 || got[:5] != "Error" {
		t.Errorf("expected error string, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// Additional WriteFileTool tests
// ---------------------------------------------------------------------------

func TestWriteFileTool_Execute_InvalidJSON(t *testing.T) {
	tool := &fs.WriteFileTool{}
	_, err := tool.Execute(context.Background(), `{invalid json`)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestWriteFileTool_Execute_CreateDirectories(t *testing.T) {
	paths, cleanup := newTestPaths(t)
	defer cleanup()

	tool := &fs.WriteFileTool{Paths: paths}

	input := makeInput(t, map[string]any{
		"description": "write nested",
		"path":        "/mnt/user-data/workspace/nested/deep/file.txt",
		"content":     "nested content",
	})

	got, err := tool.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("Execute: unexpected error: %v", err)
	}
	if got != "OK" {
		t.Errorf("Execute: got %q, want OK", got)
	}

	// Verify file was created
	hostFile := filepath.Join(paths.WorkspacePath, "nested", "deep", "file.txt")
	content := readHostFile(t, hostFile)
	if content != "nested content" {
		t.Errorf("content: got %q", content)
	}
}

// ---------------------------------------------------------------------------
// validateResolvedPath edge cases
// ---------------------------------------------------------------------------

func TestResolveVirtualPath_EdgeCases(t *testing.T) {
	paths, cleanup := newTestPaths(t)
	defer cleanup()

	// Test exact path match (no trailing slash)
	got, err := fs.ResolveVirtualPath("/mnt/user-data/workspace", paths)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if got != paths.WorkspacePath {
		t.Errorf("got %q, want %q", got, paths.WorkspacePath)
	}

	// Test uploads
	got, err = fs.ResolveVirtualPath("/mnt/user-data/uploads/test.txt", paths)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !filepath.HasPrefix(got, paths.UploadsPath) {
		t.Errorf("got %q, expected under %q", got, paths.UploadsPath)
	}

	// Test outputs
	got, err = fs.ResolveVirtualPath("/mnt/user-data/outputs/test.txt", paths)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !filepath.HasPrefix(got, paths.OutputsPath) {
		t.Errorf("got %q, expected under %q", got, paths.OutputsPath)
	}
}
