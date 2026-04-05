// Package fs implements file-system tools for the GoClaw agent harness.
//
// # Virtual path system
//
// Agent-visible paths always use the /mnt/user-data/ prefix, which is
// translated to the actual per-thread host directory at runtime. This mirrors
// DeerFlow's sandbox path abstraction so the agent never sees host paths.
//
//	Virtual                          Host (example)
//	/mnt/user-data/workspace/  →  .deer-flow/threads/{id}/user-data/workspace/
//	/mnt/user-data/uploads/    →  .deer-flow/threads/{id}/user-data/uploads/
//	/mnt/user-data/outputs/    →  .deer-flow/threads/{id}/user-data/outputs/
//
// The four tools exposed by this package are:
//
//	read_file  – read text content from a file, optionally limiting to a line range
//	write_file – write (or append) text content to a file
//	edit_file  – replace an exact substring in a file (str_replace semantic)
//	list_dir   – list directory contents up to 2 levels deep
package fs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// VirtualPathPrefix is the root of all virtual paths that the agent uses.
const VirtualPathPrefix = "/mnt/user-data"

// PathMapping holds the resolved host-side paths for a single agent thread.
// Populate one of these from your thread/sandbox state and pass it to each
// tool via Execute's input JSON or by constructing the tool with PathMapping
// directly (the tools embed it via ThreadPaths).
type PathMapping struct {
	// ThreadID is the logical thread identifier owning these paths.
	ThreadID string
	// WorkspacePath is the host path that corresponds to /mnt/user-data/workspace.
	WorkspacePath string
	// UploadsPath is the host path that corresponds to /mnt/user-data/uploads.
	UploadsPath string
	// OutputsPath is the host path that corresponds to /mnt/user-data/outputs.
	OutputsPath string
}

type fileOpLockKey struct {
	threadID string
	hostPath string
}

var (
	fileOperationLocks      = map[fileOpLockKey]*sync.Mutex{}
	fileOperationLocksGuard sync.Mutex
)

func getFileOperationLock(threadID string, hostPath string) *sync.Mutex {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		threadID = "default"
	}
	key := fileOpLockKey{threadID: threadID, hostPath: filepath.Clean(hostPath)}
	fileOperationLocksGuard.Lock()
	defer fileOperationLocksGuard.Unlock()
	if l, ok := fileOperationLocks[key]; ok {
		return l
	}
	l := &sync.Mutex{}
	fileOperationLocks[key] = l
	return l
}

// ResolveVirtualPath translates a virtual /mnt/user-data/* path to its host
// equivalent using the given PathMapping. Returns an error when the path is
// outside the allowed virtual roots or contains path-traversal segments.
//
// Mapping order (longest-prefix first):
//
//	/mnt/user-data/workspace → m.WorkspacePath
//	/mnt/user-data/uploads   → m.UploadsPath
//	/mnt/user-data/outputs   → m.OutputsPath
//	/mnt/user-data           → common parent of the three dirs (if they share one)
func ResolveVirtualPath(virtualPath string, m *PathMapping) (string, error) {
	// 1. Block ".." segments.
	if err := rejectPathTraversal(virtualPath); err != nil {
		return "", err
	}

	// 2. Build ordered mapping pairs (longest-prefix first).
	type mapping struct {
		virtualPrefix string
		hostBase      string
	}
	mappings := []mapping{
		{VirtualPathPrefix + "/workspace", m.WorkspacePath},
		{VirtualPathPrefix + "/uploads", m.UploadsPath},
		{VirtualPathPrefix + "/outputs", m.OutputsPath},
	}

	// 3. Iterate and match.
	for _, mp := range mappings {
		if mp.hostBase == "" {
			continue
		}
		if virtualPath == mp.virtualPrefix {
			return mp.hostBase, nil
		}
		if strings.HasPrefix(virtualPath, mp.virtualPrefix+"/") {
			remainder := strings.TrimPrefix(virtualPath, mp.virtualPrefix+"/")
			resolved := filepath.Join(mp.hostBase, remainder)
			if err := validateResolvedPath(resolved, m); err != nil {
				return "", err
			}
			return resolved, nil
		}
	}

	// 4. No prefix matched.
	return "", fmt.Errorf("permission denied: path must be under %s", VirtualPathPrefix)
}

// rejectPathTraversal returns an error if path contains ".." segments.
// This is the first line of defence against directory-traversal attacks.
func rejectPathTraversal(path string) error {
	// Normalize to forward slashes and split on "/".
	normalized := strings.ReplaceAll(path, "\\", "/")
	segments := strings.Split(normalized, "/")
	for _, seg := range segments {
		if seg == ".." {
			return fmt.Errorf("permission denied: path traversal detected: %s", path)
		}
	}
	return nil
}

// validateResolvedPath verifies that resolved falls within one of the allowed
// per-thread host roots (workspace, uploads, outputs). Returns PermissionError
// if the resolved path escapes those roots after filepath.Clean/Abs evaluation.
func validateResolvedPath(resolved string, m *PathMapping) error {
	absResolved, err := filepath.Abs(resolved)
	if err != nil {
		return fmt.Errorf("permission denied: cannot resolve path: %s", resolved)
	}

	roots := []string{m.WorkspacePath, m.UploadsPath, m.OutputsPath}
	for _, root := range roots {
		if root == "" {
			continue
		}
		absRoot, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		// Check if resolved path equals root or is under root.
		if absResolved == absRoot || strings.HasPrefix(absResolved, absRoot+string(os.PathSeparator)) {
			return nil
		}
	}
	return fmt.Errorf("permission denied: path escapes allowed roots: %s", resolved)
}

// maskHostPaths replaces host-side absolute paths in output with their virtual
// equivalents to prevent the agent from observing actual host directory layouts.
// This is the output-sanitization counterpart of ResolveVirtualPath.
func maskHostPaths(output string, m *PathMapping) string {
	// Build actual→virtual mapping pairs (longest host path first for correct replacement).
	type mapping struct {
		hostPath    string
		virtualPath string
	}
	mappings := []mapping{
		{m.WorkspacePath, VirtualPathPrefix + "/workspace"},
		{m.UploadsPath, VirtualPathPrefix + "/uploads"},
		{m.OutputsPath, VirtualPathPrefix + "/outputs"},
	}

	result := output
	for _, mp := range mappings {
		if mp.hostPath != "" {
			result = strings.ReplaceAll(result, mp.hostPath, mp.virtualPath)
		}
	}
	return result
}

// ensureDir creates dir and all necessary parents. No-op if dir already exists.
func ensureDir(dir string) error {
	return os.MkdirAll(dir, 0o750)
}

// ---------------------------------------------------------------------------
// ReadFileTool
// ---------------------------------------------------------------------------

const DefaultReadFileMaxChars = 50000

// ReadFileTool reads a text file at a virtual path and returns its content.
// Optionally restricts output to a line range [StartLine, EndLine] (1-indexed,
// inclusive) to avoid flooding the model's context with very large files.
//
// Implements tools.Tool.
type ReadFileTool struct {
	// Paths holds the per-thread path mapping injected by the sandbox layer.
	Paths *PathMapping
	// MaxChars is the maximum number of characters returned.
	// Values <= 0 use DefaultReadFileMaxChars.
	MaxChars int
}

// readFileInput is the JSON-decoded input for ReadFileTool.
type readFileInput struct {
	// Description is a brief rationale for why the file is being read.
	Description string `json:"description"`
	// Path is the virtual absolute path to the file (under /mnt/user-data/).
	Path string `json:"path"`
	// StartLine is the 1-indexed first line to return (optional).
	StartLine *int `json:"start_line,omitempty"`
	// EndLine is the 1-indexed last line to return (optional).
	EndLine *int `json:"end_line,omitempty"`
}

func (t *ReadFileTool) Name() string { return "read_file" }

func (t *ReadFileTool) Description() string {
	return `Read the contents of a text file. Use this to examine source code, ` +
		`configuration files, logs, or any text-based file.
Path must be an absolute virtual path under /mnt/user-data/.
Use start_line and end_line to read a specific line range (1-indexed, inclusive).`
}

func (t *ReadFileTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "required": ["description", "path"],
  "properties": {
    "description": {"type": "string", "description": "Explain why you are reading this file."},
    "path":        {"type": "string", "description": "Absolute virtual path to the file."},
    "start_line":  {"type": "integer", "description": "First line to read (1-indexed, inclusive)."},
    "end_line":    {"type": "integer", "description": "Last line to read (1-indexed, inclusive)."}
  }
}`)
}

// Execute reads the file at the virtual path and returns its content.
//
// TODO: implementation steps
//  1. json.Unmarshal input into readFileInput; return error on bad JSON.
//  2. Call ResolveVirtualPath(in.Path, t.Paths) to get the host path.
//  3. Call os.ReadFile(hostPath) to read content.
//  4. If in.StartLine and in.EndLine are set, split by "\n" and slice
//     [StartLine-1 : EndLine].
//  5. Apply a max-chars head-truncation guard (default 50 000 chars).
//  6. Return the (possibly truncated) content string.
//  7. On FileNotFoundError → return "Error: file not found: <virtual path>".
//  8. On PermissionError   → return "Error: permission denied: <virtual path>".
func (t *ReadFileTool) Execute(_ context.Context, input string) (string, error) {
	var in readFileInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", fmt.Errorf("read_file: invalid input JSON: %w", err)
	}

	// Resolve virtual path to host path.
	hostPath, err := ResolveVirtualPath(in.Path, t.Paths)
	if err != nil {
		return fmt.Sprintf("Error: %v", err), nil
	}

	// Read file content.
	data, err := os.ReadFile(hostPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Sprintf("Error: file not found: %s", in.Path), nil
		}
		if os.IsPermission(err) {
			return fmt.Sprintf("Error: permission denied: %s", in.Path), nil
		}
		return fmt.Sprintf("Error: %v", err), nil
	}

	content := string(data)

	// Apply line range if specified.
	if in.StartLine != nil && in.EndLine != nil {
		lines := strings.Split(content, "\n")
		start := *in.StartLine - 1 // Convert to 0-indexed
		end := *in.EndLine

		if start < 0 {
			start = 0
		}
		if end > len(lines) {
			end = len(lines)
		}
		if start >= len(lines) {
			return "", nil
		}

		content = strings.Join(lines[start:end], "\n")
	}

	// Apply max-chars truncation guard.
	maxChars := t.MaxChars
	if maxChars <= 0 {
		maxChars = DefaultReadFileMaxChars
	}
	if len(content) > maxChars {
		content = content[:maxChars] + "\n... (truncated)"
	}

	return content, nil
}

// ---------------------------------------------------------------------------
// WriteFileTool
// ---------------------------------------------------------------------------

// WriteFileTool writes or appends text content to a file at a virtual path.
// Parent directories are created automatically when they do not exist.
//
// Implements tools.Tool.
type WriteFileTool struct {
	Paths *PathMapping
}

type writeFileInput struct {
	Description string `json:"description"`
	Path        string `json:"path"`
	Content     string `json:"content"`
	// Append controls whether content is appended instead of overwriting.
	Append bool `json:"append,omitempty"`
}

func (t *WriteFileTool) Name() string { return "write_file" }

func (t *WriteFileTool) Description() string {
	return `Write text content to a file. Parent directories are created automatically.
Path must be an absolute virtual path under /mnt/user-data/.
Set append=true to append to an existing file instead of overwriting it.`
}

func (t *WriteFileTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "required": ["description", "path", "content"],
  "properties": {
    "description": {"type": "string", "description": "Explain why you are writing this file."},
    "path":        {"type": "string", "description": "Absolute virtual path to the file."},
    "content":     {"type": "string", "description": "Text content to write."},
    "append":      {"type": "boolean", "description": "Append instead of overwrite (default false)."}
  }
}`)
}

// Execute writes content to the resolved host path.
//
// TODO: implementation steps
//  1. json.Unmarshal input into writeFileInput.
//  2. Call ResolveVirtualPath(in.Path, t.Paths).
//  3. Call ensureDir(filepath.Dir(hostPath)).
//  4. If in.Append, open with os.O_APPEND|os.O_CREATE|os.O_WRONLY;
//     otherwise use os.WriteFile with mode 0o644.
//  5. On success return "OK".
//  6. Map OS errors to user-facing strings (PermissionError, IsADirectory, etc.).
func (t *WriteFileTool) Execute(_ context.Context, input string) (string, error) {
	var in writeFileInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", fmt.Errorf("write_file: invalid input JSON: %w", err)
	}

	// Resolve virtual path to host path.
	hostPath, err := ResolveVirtualPath(in.Path, t.Paths)
	if err != nil {
		return fmt.Sprintf("Error: %v", err), nil
	}

	lock := getFileOperationLock(pathThreadID(t.Paths), hostPath)
	lock.Lock()
	defer lock.Unlock()

	// Ensure parent directory exists.
	if err := ensureDir(filepath.Dir(hostPath)); err != nil {
		return fmt.Sprintf("Error: cannot create directory: %v", err), nil
	}

	if in.Append {
		// Append mode.
		f, err := os.OpenFile(hostPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			if os.IsPermission(err) {
				return fmt.Sprintf("Error: permission denied: %s", in.Path), nil
			}
			return fmt.Sprintf("Error: %v", err), nil
		}
		defer f.Close()
		if _, err := f.WriteString(in.Content); err != nil {
			return fmt.Sprintf("Error: %v", err), nil
		}
	} else {
		// Overwrite mode.
		if err := os.WriteFile(hostPath, []byte(in.Content), 0o644); err != nil {
			if os.IsPermission(err) {
				return fmt.Sprintf("Error: permission denied: %s", in.Path), nil
			}
			return fmt.Sprintf("Error: %v", err), nil
		}
	}

	return "OK", nil
}

// ---------------------------------------------------------------------------
// EditFileTool (str_replace semantic)
// ---------------------------------------------------------------------------

// EditFileTool performs an exact-substring replacement inside a file.
// When ReplaceAll is false (default) the old string must appear exactly once;
// this prevents accidental mass edits of autogenerated files.
//
// Implements tools.Tool.
type EditFileTool struct {
	Paths *PathMapping
}

type editFileInput struct {
	Description string `json:"description"`
	Path        string `json:"path"`
	OldStr      string `json:"old_str"`
	NewStr      string `json:"new_str"`
	// ReplaceAll replaces every occurrence when true; default replaces only the first.
	ReplaceAll bool `json:"replace_all,omitempty"`
}

func (t *EditFileTool) Name() string { return "edit_file" }

func (t *EditFileTool) Description() string {
	return `Replace a substring in a file with another substring (str_replace semantic).
When replace_all is false (default), old_str must appear exactly once in the file.
Path must be an absolute virtual path under /mnt/user-data/.`
}

func (t *EditFileTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "required": ["description", "path", "old_str", "new_str"],
  "properties": {
    "description": {"type": "string"},
    "path":        {"type": "string", "description": "Absolute virtual path to the file."},
    "old_str":     {"type": "string", "description": "Exact substring to replace."},
    "new_str":     {"type": "string", "description": "Replacement string."},
    "replace_all": {"type": "boolean", "description": "Replace all occurrences (default false)."}
  }
}`)
}

// Execute performs the str_replace operation on the file.
//
// TODO: implementation steps
//  1. json.Unmarshal input.
//  2. ResolveVirtualPath.
//  3. os.ReadFile to get current content.
//  4. If in.OldStr not in content → return error "old_str not found in file".
//  5. If !in.ReplaceAll and strings.Count(content, in.OldStr) > 1 → return
//     error "old_str appears N times; use replace_all=true or make old_str unique".
//  6. strings.Replace(content, in.OldStr, in.NewStr, replaceCount).
//  7. os.WriteFile the updated content.
//  8. Return "OK".
func (t *EditFileTool) Execute(_ context.Context, input string) (string, error) {
	var in editFileInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", fmt.Errorf("edit_file: invalid input JSON: %w", err)
	}

	// Resolve virtual path to host path.
	hostPath, err := ResolveVirtualPath(in.Path, t.Paths)
	if err != nil {
		return fmt.Sprintf("Error: %v", err), nil
	}

	lock := getFileOperationLock(pathThreadID(t.Paths), hostPath)
	lock.Lock()
	defer lock.Unlock()

	// Read current file content.
	data, err := os.ReadFile(hostPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Sprintf("Error: file not found: %s", in.Path), nil
		}
		if os.IsPermission(err) {
			return fmt.Sprintf("Error: permission denied: %s", in.Path), nil
		}
		return fmt.Sprintf("Error: %v", err), nil
	}

	content := string(data)

	// Check if old_str exists in content.
	count := strings.Count(content, in.OldStr)
	if count == 0 {
		return "Error: old_str not found in file", nil
	}

	// If not replace_all, old_str must appear exactly once.
	if !in.ReplaceAll && count > 1 {
		return fmt.Sprintf("Error: old_str appears %d times; use replace_all=true or make old_str unique", count), nil
	}

	// Perform replacement.
	var newContent string
	if in.ReplaceAll {
		newContent = strings.ReplaceAll(content, in.OldStr, in.NewStr)
	} else {
		newContent = strings.Replace(content, in.OldStr, in.NewStr, 1)
	}

	// Write updated content.
	if err := os.WriteFile(hostPath, []byte(newContent), 0o644); err != nil {
		if os.IsPermission(err) {
			return fmt.Sprintf("Error: permission denied: %s", in.Path), nil
		}
		return fmt.Sprintf("Error: %v", err), nil
	}

	return "OK", nil
}

func pathThreadID(paths *PathMapping) string {
	if paths == nil {
		return "default"
	}
	if strings.TrimSpace(paths.ThreadID) == "" {
		return "default"
	}
	return strings.TrimSpace(paths.ThreadID)
}

// ---------------------------------------------------------------------------
// ListDirTool
// ---------------------------------------------------------------------------

// ListDirTool lists directory contents up to 2 levels deep in tree format.
// This mirrors DeerFlow's `ls` tool.
//
// Implements tools.Tool.
type ListDirTool struct {
	Paths *PathMapping
}

type listDirInput struct {
	Description string `json:"description"`
	Path        string `json:"path"`
}

func (t *ListDirTool) Name() string { return "list_dir" }

func (t *ListDirTool) Description() string {
	return `List the contents of a directory up to 2 levels deep in tree format.
Path must be an absolute virtual path under /mnt/user-data/.`
}

func (t *ListDirTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "required": ["description", "path"],
  "properties": {
    "description": {"type": "string", "description": "Explain why you are listing this directory."},
    "path":        {"type": "string", "description": "Absolute virtual path to the directory."}
  }
}`)
}

// Execute lists the directory at the virtual path.
//
// TODO: implementation steps
//  1. json.Unmarshal input.
//  2. ResolveVirtualPath.
//  3. os.ReadDir(hostPath) to get top-level entries.
//  4. For each entry that is itself a directory, os.ReadDir one level deeper.
//  5. Format output as an indented tree (e.g. "dir/\n  file.txt").
//  6. Return "(empty)" when the directory contains no entries.
//  7. Map FileNotFoundError, PermissionError to user-facing error strings.
//  8. maskHostPaths on the output before returning.
func (t *ListDirTool) Execute(_ context.Context, input string) (string, error) {
	var in listDirInput
	if err := json.Unmarshal([]byte(input), &in); err != nil {
		return "", fmt.Errorf("list_dir: invalid input JSON: %w", err)
	}

	// Resolve virtual path to host path.
	hostPath, err := ResolveVirtualPath(in.Path, t.Paths)
	if err != nil {
		return fmt.Sprintf("Error: %v", err), nil
	}

	// Read directory entries.
	entries, err := os.ReadDir(hostPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Sprintf("Error: directory not found: %s", in.Path), nil
		}
		if os.IsPermission(err) {
			return fmt.Sprintf("Error: permission denied: %s", in.Path), nil
		}
		return fmt.Sprintf("Error: %v", err), nil
	}

	if len(entries) == 0 {
		return "(empty)", nil
	}

	// Build tree output (up to 2 levels deep).
	var sb strings.Builder
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			sb.WriteString(name + "/\n")
			// List one level deeper.
			subPath := filepath.Join(hostPath, name)
			subEntries, err := os.ReadDir(subPath)
			if err == nil {
				for _, subEntry := range subEntries {
					subName := subEntry.Name()
					if subEntry.IsDir() {
						sb.WriteString("  " + subName + "/\n")
					} else {
						sb.WriteString("  " + subName + "\n")
					}
				}
			}
		} else {
			sb.WriteString(name + "\n")
		}
	}

	output := strings.TrimSuffix(sb.String(), "\n")
	return maskHostPaths(output, t.Paths), nil
}

// Ensure the fs package compiles even without a consumer importing these symbols.
var (
	_ = filepath.Join
	_ = strings.HasPrefix
	_ = ensureDir
	_ = maskHostPaths
)
