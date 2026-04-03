// Package local provides a LocalSandbox that runs commands and file operations
// directly on the host filesystem, confined to per-thread directory trees under
// .goclaw/threads/{threadID}/user-data/.
//
// Virtual path mapping:
//
//	/mnt/user-data/workspace  ->  .goclaw/threads/{threadID}/user-data/workspace
//	/mnt/user-data/uploads    ->  .goclaw/threads/{threadID}/user-data/uploads
//	/mnt/user-data/outputs    ->  .goclaw/threads/{threadID}/user-data/outputs
//
// The sandbox is safe against path traversal attacks ("../") in all file
// operations. Shell execution is intentionally kept minimal and gated behind
// an explicit allowlist; bash is disabled by default.
package local

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/bookerbai/goclaw/internal/sandbox"
)

// defaultDeniedCommands is the built-in denylist of dangerous command prefixes
// that are always blocked regardless of allowlist configuration.
var defaultDeniedCommands = []string{
	"rm -rf /",
	"rm -rf ~",
	"mkfs",
	"dd if=/dev/zero",
	":(){ :|:& };:", // fork bomb
	"sudo",
	"su ",
	"chmod 777 /",
	"chown -R",
}

// defaultExecTimeout is used when SandboxConfig.ExecTimeout is zero.
const defaultExecTimeout = 30 * time.Second

// LocalSandbox implements sandbox.Sandbox by running operations directly on
// the host filesystem inside a per-thread directory tree.
type LocalSandbox struct {
	id       string
	threadID string
	baseDir  string  // absolute host path of .goclaw/threads/{threadID}/user-data
	cfg      sandbox.SandboxConfig
}

// ID returns the unique identifier for this sandbox instance.
func (s *LocalSandbox) ID() string {
	return s.id
}

// virtualToReal translates a virtual /mnt/user-data/... path to its real host path.
// Returns an error if the path does not start with VirtualPathPrefix.
func (s *LocalSandbox) virtualToReal(virtualPath string) (string, error) {
	// TODO:
	//  1. Check the path starts with sandbox.VirtualPathPrefix; return error if not.
	//  2. Strip the prefix, join with s.baseDir.
	//  3. Call rejectPathTraversal on the virtual path first.
	//  4. filepath.Clean the result and re-check it stays inside s.baseDir via
	//     strings.HasPrefix after filepath.Clean(s.baseDir)+string(os.PathSeparator).
	//  5. Return cleaned real path.
	prefix := sandbox.VirtualPathPrefix
	if virtualPath == prefix {
		return filepath.Clean(s.baseDir), nil
	}
	if !strings.HasPrefix(virtualPath, prefix+"/") {
		return "", fmt.Errorf("path %q is outside allowed virtual prefix %s", virtualPath, prefix)
	}
	if err := rejectPathTraversal(virtualPath); err != nil {
		return "", err
	}
	rel := strings.TrimPrefix(virtualPath, prefix+"/")
	real := filepath.Join(s.baseDir, rel)
	real = filepath.Clean(real)
	if !isInsideDir(real, s.baseDir) {
		return "", fmt.Errorf("access denied: path traversal detected")
	}
	return real, nil
}

// realToVirtual translates a real host path back to its /mnt/user-data/... virtual form.
// Used for masking host paths in output returned to agents.
func (s *LocalSandbox) realToVirtual(realPath string) string {
	// TODO:
	//  1. filepath.Clean both realPath and s.baseDir.
	//  2. If realPath == s.baseDir return sandbox.VirtualPathPrefix.
	//  3. If realPath has s.baseDir+"/" prefix, replace it with sandbox.VirtualPathPrefix+"/".
	//  4. Otherwise return realPath unchanged (it is not a user-data path).
	base := filepath.Clean(s.baseDir)
	clean := filepath.Clean(realPath)
	if clean == base {
		return sandbox.VirtualPathPrefix
	}
	if strings.HasPrefix(clean, base+string(os.PathSeparator)) {
		rel := strings.TrimPrefix(clean, base+string(os.PathSeparator))
		return sandbox.VirtualPathPrefix + "/" + rel
	}
	return realPath
}

// rejectPathTraversal returns an error if the path contains ".." segments.
func rejectPathTraversal(path string) error {
	// TODO:
	//  1. Replace backslashes with forward slashes for normalisation.
	//  2. Split on "/" and look for ".." segments.
	//  3. Return a PermissionError if any ".." segment is found.
	normalised := strings.ReplaceAll(path, "\\", "/")
	for _, seg := range strings.Split(normalised, "/") {
		if seg == ".." {
			return fmt.Errorf("access denied: path traversal detected")
		}
	}
	return nil
}

// isInsideDir reports whether candidate is inside (or equal to) the dir root.
// Both paths must be absolute and filepath.Clean'd.
func isInsideDir(candidate, dir string) bool {
	// TODO:
	//  1. Ensure dir ends without trailing slash, append os.PathSeparator.
	//  2. Return candidate == dir || strings.HasPrefix(candidate, dir+sep).
	cleanDir := filepath.Clean(dir)
	return candidate == cleanDir ||
		strings.HasPrefix(candidate, cleanDir+string(os.PathSeparator))
}

// isDeniedCommand checks whether the given command matches a denylist entry.
func isDeniedCommand(command string, denied []string) bool {
	// TODO:
	//  1. Trim leading whitespace from command.
	//  2. For each pattern in denied, check strings.HasPrefix(command, pattern).
	//  3. Return true if any match is found.
	trimmed := strings.TrimSpace(command)
	for _, pattern := range denied {
		if strings.HasPrefix(trimmed, pattern) {
			return true
		}
	}
	return false
}

// isAllowedCommand checks whether command is permitted by the allowlist.
// If allowedCommands is empty, no shell exec is permitted.
func isAllowedCommand(command string, allowed []string) bool {
	// TODO:
	//  1. If allowed is nil/empty, return false (exec disabled).
	//  2. Trim leading whitespace from command.
	//  3. Extract the first token (command name) by splitting on whitespace.
	//  4. Return true if any allowlist entry equals the first token.
	if len(allowed) == 0 {
		return false
	}
	trimmed := strings.TrimSpace(command)
	parts := strings.Fields(trimmed)
	if len(parts) == 0 {
		return false
	}
	cmdName := parts[0]
	for _, a := range allowed {
		if a == cmdName {
			return true
		}
	}
	return false
}

// Execute runs a shell command inside the sandbox.
// The command is validated against the denylist and allowlist before execution.
// The working directory is set to the thread's workspace directory.
func (s *LocalSandbox) Execute(ctx context.Context, command string) (sandbox.ExecuteResult, error) {
	// TODO:
	//  1. Check isDeniedCommand; return PermissionError result if matched.
	//  2. Check isAllowedCommand; return PermissionError result if not allowed.
	//  3. Compute exec timeout: use s.cfg.ExecTimeout or defaultExecTimeout.
	//  4. Create a context with the timeout, derived from the input ctx.
	//  5. Build exec.CommandContext("bash", "-c", command).
	//  6. Set Cmd.Dir to s.baseDir+"/workspace".
	//  7. Set a restricted Cmd.Env (PATH, HOME, no sensitive vars).
	//  8. Capture stdout and stderr via bytes.Buffer.
	//  9. Run the command and collect ExitCode from exec.ExitError if needed.
	// 10. Return ExecuteResult with Stdout, Stderr, ExitCode.
	denied := append(defaultDeniedCommands, s.cfg.DeniedCommands...)
	if isDeniedCommand(command, denied) {
		return sandbox.ExecuteResult{
			ExitCode: 1,
			Error:    fmt.Errorf("permission denied: command is in the denylist"),
		}, nil
	}
	if !isAllowedCommand(command, s.cfg.AllowedCommands) {
		return sandbox.ExecuteResult{
			ExitCode: 1,
			Error:    fmt.Errorf("permission denied: shell exec is disabled; command not in allowlist"),
		}, nil
	}

	timeout := s.cfg.ExecTimeout
	if timeout == 0 {
		timeout = defaultExecTimeout
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	workspaceDir := filepath.Join(s.baseDir, "workspace")
	// Ensure workspace directory exists.
	// TODO: use os.MkdirAll to create workspaceDir before running cmd.
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		return sandbox.ExecuteResult{Error: fmt.Errorf("failed to create workspace dir: %w", err)}, nil
	}

	cmd := exec.CommandContext(execCtx, "bash", "-c", command)
	cmd.Dir = workspaceDir
	// Provide a restricted environment to avoid leaking host secrets.
	cmd.Env = []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=" + workspaceDir,
		"TMPDIR=" + workspaceDir,
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	runErr := cmd.Run()
	result := sandbox.ExecuteResult{
		Stdout: stdoutBuf.String(),
		Stderr: stderrBuf.String(),
	}
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = 1
			result.Error = runErr
		}
	}
	return result, nil
}

// ReadFile reads the full text content of the file at virtualPath.
func (s *LocalSandbox) ReadFile(ctx context.Context, virtualPath string) (string, error) {
	// TODO:
	//  1. Translate virtualPath to real path via virtualToReal.
	//  2. os.ReadFile the real path.
	//  3. Return string(content), nil or wrap the error.
	realPath, err := s.virtualToReal(virtualPath)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(realPath)
	if err != nil {
		return "", fmt.Errorf("read file %q: %w", virtualPath, err)
	}
	return string(data), nil
}

// WriteFile writes content to the file at virtualPath.
// If append is true, content is appended; otherwise the file is overwritten.
// Parent directories are created automatically.
func (s *LocalSandbox) WriteFile(ctx context.Context, virtualPath string, content string, appendMode bool) error {
	// TODO:
	//  1. Translate virtualPath to real path via virtualToReal.
	//  2. os.MkdirAll for the parent directory.
	//  3. If appendMode, open with O_APPEND|O_CREATE|O_WRONLY; otherwise O_CREATE|O_TRUNC|O_WRONLY.
	//  4. Write content, close file.
	realPath, err := s.virtualToReal(virtualPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(realPath), 0o755); err != nil {
		return fmt.Errorf("create parent dirs for %q: %w", virtualPath, err)
	}
	flag := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	if appendMode {
		flag = os.O_CREATE | os.O_WRONLY | os.O_APPEND
	}
	f, err := os.OpenFile(realPath, flag, 0o644)
	if err != nil {
		return fmt.Errorf("open file %q: %w", virtualPath, err)
	}
	defer f.Close()
	if _, err := io.WriteString(f, content); err != nil {
		return fmt.Errorf("write file %q: %w", virtualPath, err)
	}
	return nil
}

// ListDir lists files and directories up to maxDepth levels deep under virtualPath.
func (s *LocalSandbox) ListDir(ctx context.Context, virtualPath string, maxDepth int) ([]sandbox.FileInfo, error) {
	// TODO:
	//  1. Translate virtualPath to real path.
	//  2. Walk the directory up to maxDepth levels.
	//  3. For each entry, compute its virtual path using realToVirtual.
	//  4. Build a FileInfo from os.FileInfo and the virtual path.
	//  5. Return the slice.
	realPath, err := s.virtualToReal(virtualPath)
	if err != nil {
		return nil, err
	}
	if maxDepth <= 0 {
		maxDepth = 2
	}

	var results []sandbox.FileInfo
	rootDepth := strings.Count(filepath.Clean(realPath), string(os.PathSeparator))

	walkErr := filepath.WalkDir(realPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		currentDepth := strings.Count(filepath.Clean(path), string(os.PathSeparator)) - rootDepth
		if currentDepth > maxDepth {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if path == realPath {
			return nil // skip root itself
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}
		var size int64
		if !d.IsDir() {
			size = info.Size()
		}
		results = append(results, sandbox.FileInfo{
			Name:    d.Name(),
			Path:    s.realToVirtual(path),
			Size:    size,
			IsDir:   d.IsDir(),
			ModTime: info.ModTime(),
		})
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("list dir %q: %w", virtualPath, walkErr)
	}
	return results, nil
}

// StrReplace replaces occurrences of oldStr with newStr in the file at virtualPath.
func (s *LocalSandbox) StrReplace(ctx context.Context, virtualPath string, oldStr string, newStr string, replaceAll bool) error {
	// TODO:
	//  1. Translate virtualPath to real path.
	//  2. ReadFile content.
	//  3. Check oldStr is present; return error if not found.
	//  4. strings.Replace(content, oldStr, newStr, n) where n=-1 if replaceAll else 1.
	//  5. WriteFile the new content (no append).
	realPath, err := s.virtualToReal(virtualPath)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(realPath)
	if err != nil {
		return fmt.Errorf("str_replace read %q: %w", virtualPath, err)
	}
	content := string(data)
	if !strings.Contains(content, oldStr) {
		return fmt.Errorf("str_replace: string to replace not found in %q", virtualPath)
	}
	n := 1
	if replaceAll {
		n = -1
	}
	newContent := strings.Replace(content, oldStr, newStr, n)
	return os.WriteFile(realPath, []byte(newContent), 0o644)
}

// ---------------------------------------------------------------------------
// LocalSandboxProvider
// ---------------------------------------------------------------------------

// LocalSandboxProvider implements sandbox.SandboxProvider using a per-process
// singleton LocalSandbox instance. The same sandbox is reused across all threads
// because access is already scoped by the per-thread baseDir within the sandbox.
type LocalSandboxProvider struct {
	mu      sync.Mutex
	cfg     sandbox.SandboxConfig
	baseDir string // root directory for all thread data (e.g. /path/to/.goclaw)
	sandbox *LocalSandbox
}

// NewLocalSandboxProvider creates a new provider. baseDir is the root directory
// under which .goclaw/threads/{threadID}/user-data/ subdirectories will be created.
func NewLocalSandboxProvider(cfg sandbox.SandboxConfig, baseDir string) *LocalSandboxProvider {
	return &LocalSandboxProvider{
		cfg:     cfg,
		baseDir: filepath.Clean(baseDir),
	}
}

// Acquire returns the singleton sandbox ID "local", creating the sandbox if needed.
// threadID is used to set up the per-thread filesystem paths on first call.
func (p *LocalSandboxProvider) Acquire(ctx context.Context, threadID string) (string, error) {
	// TODO:
	//  1. Lock p.mu.
	//  2. If p.sandbox == nil, construct threadBaseDir = filepath.Join(p.baseDir,
	//     "threads", threadID, "user-data"), os.MkdirAll the subdirs
	//     (workspace, uploads, outputs).
	//  3. Create a new LocalSandbox with id="local", threadID, and baseDir set to threadBaseDir.
	//  4. Assign to p.sandbox.
	//  5. Unlock and return "local", nil.
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.sandbox == nil {
		threadBaseDir := filepath.Join(p.baseDir, "threads", threadID, "user-data")
		for _, sub := range []string{"workspace", "uploads", "outputs"} {
			if err := os.MkdirAll(filepath.Join(threadBaseDir, sub), 0o755); err != nil {
				return "", fmt.Errorf("create sandbox dir %s: %w", sub, err)
			}
		}
		p.sandbox = &LocalSandbox{
			id:       "local",
			threadID: threadID,
			baseDir:  threadBaseDir,
			cfg:      p.cfg,
		}
	}
	return "local", nil
}

// Get retrieves the singleton sandbox by ID. Returns nil if not yet created.
func (p *LocalSandboxProvider) Get(sandboxID string) sandbox.Sandbox {
	// TODO:
	//  1. Lock p.mu, read p.sandbox, unlock.
	//  2. If sandboxID == "local" and p.sandbox != nil, return p.sandbox.
	//  3. Otherwise return nil.
	p.mu.Lock()
	defer p.mu.Unlock()
	if sandboxID == "local" && p.sandbox != nil {
		return p.sandbox
	}
	return nil
}

// Release is a no-op for the local singleton; the sandbox is kept alive for reuse.
func (p *LocalSandboxProvider) Release(ctx context.Context, sandboxID string) error {
	// TODO: No-op – local sandbox is intentionally not torn down between turns.
	// Cleanup happens only in Shutdown.
	return nil
}

// Shutdown releases the singleton sandbox and allows it to be garbage collected.
// After Shutdown, Acquire can create a new sandbox on the next call.
func (p *LocalSandboxProvider) Shutdown(ctx context.Context) error {
	// TODO:
	//  1. Lock p.mu.
	//  2. Set p.sandbox = nil.
	//  3. Unlock and return nil.
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sandbox = nil
	return nil
}
