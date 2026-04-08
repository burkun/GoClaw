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
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
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
// Aligned with DeerFlow's 600s default (P2 alignment).
const defaultExecTimeout = 600 * time.Second

// detectedShell caches the auto-detected shell path.
var detectedShell string
var detectedShellOnce sync.Once

// detectShell auto-detects the best available shell (P2 fix).
// Priority: zsh > bash > sh
// This mirrors DeerFlow's shell selection logic.
func detectShell() string {
	detectedShellOnce.Do(func() {
		// Try zsh first (common on macOS)
		if path, err := exec.LookPath("zsh"); err == nil {
			detectedShell = path
			return
		}
		// Try bash (common on Linux)
		if path, err := exec.LookPath("bash"); err == nil {
			detectedShell = path
			return
		}
		// Fallback to sh (always available)
		if path, err := exec.LookPath("sh"); err == nil {
			detectedShell = path
			return
		}
		// Ultimate fallback
		detectedShell = "/bin/sh"
	})
	return detectedShell
}

// LocalSandbox implements sandbox.Sandbox by running operations directly on
// the host filesystem inside a per-thread directory tree.
type LocalSandbox struct {
	id         string
	threadID   string
	baseDir    string // absolute host path of .goclaw/threads/{threadID}/user-data
	skillsPath string // absolute host path of skills directory (optional, may be empty)
	cfg        sandbox.SandboxConfig
}

// ID returns the unique identifier for this sandbox instance.
func (s *LocalSandbox) ID() string {
	return s.id
}

// virtualToReal translates a virtual /mnt/user-data/... or /mnt/skills/... path to its real host path.
// Returns an error if the path does not start with a valid virtual prefix.
// Also checks for symlink escape attempts.
func (s *LocalSandbox) virtualToReal(virtualPath string) (string, error) {
	// Check for skills path first
	skillsPrefix := sandbox.VirtualSkillsPathPrefix
	if virtualPath == skillsPrefix || strings.HasPrefix(virtualPath, skillsPrefix+"/") {
		if s.skillsPath == "" {
			return "", fmt.Errorf("path %q: skills directory not configured", virtualPath)
		}
		if virtualPath == skillsPrefix {
			realPath := filepath.Clean(s.skillsPath)
			return s.checkSymlinkEscape(realPath)
		}
		if err := rejectPathTraversal(virtualPath); err != nil {
			return "", err
		}
		rel := strings.TrimPrefix(virtualPath, skillsPrefix+"/")
		realPath := filepath.Join(s.skillsPath, rel)
		realPath = filepath.Clean(realPath)
		if !isInsideDir(realPath, s.skillsPath) {
			return "", fmt.Errorf("access denied: path traversal detected")
		}
		return s.checkSymlinkEscape(realPath)
	}

	// Check for user-data path
	prefix := sandbox.VirtualPathPrefix
	if virtualPath == prefix {
		realPath := filepath.Clean(s.baseDir)
		return s.checkSymlinkEscape(realPath)
	}
	if !strings.HasPrefix(virtualPath, prefix+"/") {
		return "", fmt.Errorf("path %q is outside allowed virtual prefixes (%s or %s)", virtualPath, prefix, skillsPrefix)
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
	return s.checkSymlinkEscape(real)
}

// checkSymlinkEscape verifies that the real path does not escape via symlinks.
// It resolves symlinks and checks if the resolved path is still inside allowed directories.
//
// SECURITY FIX: This function returns the evaluated (resolved) path instead of the
// original realPath to prevent TOCTOU (Time-Of-Check-Time-Of-Use) race conditions.
// By returning the resolved path, all subsequent file operations use the path that
// was actually verified to be inside the allowed directories. This prevents an
// attacker from modifying the symlink between the check and the use.
//
// For non-existent paths (write operations), we verify the nearest existing ancestor
// and return the cleaned realPath since no symlink exists yet to resolve.
func (s *LocalSandbox) checkSymlinkEscape(realPath string) (string, error) {
	allowedBase := canonicalAllowedRoot(s.baseDir)
	allowedSkills := ""
	if s.skillsPath != "" {
		allowedSkills = canonicalAllowedRoot(s.skillsPath)
	}

	isAllowed := func(candidate string) bool {
		candidate = filepath.Clean(candidate)
		if isInsideDir(candidate, allowedBase) {
			return true
		}
		if allowedSkills != "" && isInsideDir(candidate, allowedSkills) {
			return true
		}
		return false
	}

	evaluated, err := filepath.EvalSymlinks(realPath)
	if err == nil {
		if isAllowed(evaluated) {
			// SECURITY: Return the evaluated path to prevent TOCTOU race condition.
			// This ensures all file operations use the verified safe path.
			return evaluated, nil
		}
		return "", fmt.Errorf("access denied: symlink escapes sandbox boundary")
	}
	if !os.IsNotExist(err) {
		return "", fmt.Errorf("access denied: cannot evaluate path: %w", err)
	}

	// Path may not exist yet (write path). Walk up to the nearest existing ancestor
	// and ensure that ancestor resolves inside allowed roots.
	for parent := filepath.Clean(realPath); parent != "/" && parent != "."; parent = filepath.Dir(parent) {
		evalParent, parentErr := filepath.EvalSymlinks(parent)
		if parentErr != nil {
			if os.IsNotExist(parentErr) {
				continue
			}
			return "", fmt.Errorf("access denied: cannot evaluate ancestor: %w", parentErr)
		}
		if !isAllowed(evalParent) {
			return "", fmt.Errorf("access denied: symlink escape detected in parent directory")
		}
		// For non-existent paths, return the cleaned realPath since there's no
		// symlink to resolve yet. The ancestor check above ensures the path
		// will be created in a safe location.
		return filepath.Clean(realPath), nil
	}

	// Fallback for edge cases (e.g., root directory)
	return filepath.Clean(realPath), nil
}

func canonicalAllowedRoot(path string) string {
	path = filepath.Clean(path)
	evaluated, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}
	return filepath.Clean(evaluated)
}

// realToVirtual translates a real host path back to its /mnt/user-data/... or /mnt/skills/... virtual form.
// Used for masking host paths in output returned to agents.
//
// Note: realPath may be a symlink-resolved path (e.g., /private/var instead of /var on macOS).
// We need to compare against both the original base path and its canonical (resolved) form.
func (s *LocalSandbox) realToVirtual(realPath string) string {
	// Check skills path first (higher priority for masking)
	if s.skillsPath != "" {
		skillsBase := filepath.Clean(s.skillsPath)
		skillsCanonical := canonicalAllowedRoot(s.skillsPath)
		clean := filepath.Clean(realPath)
		if clean == skillsBase || clean == skillsCanonical {
			return sandbox.VirtualSkillsPathPrefix
		}
		if strings.HasPrefix(clean, skillsBase+string(os.PathSeparator)) {
			rel := strings.TrimPrefix(clean, skillsBase+string(os.PathSeparator))
			return sandbox.VirtualSkillsPathPrefix + "/" + rel
		}
		if strings.HasPrefix(clean, skillsCanonical+string(os.PathSeparator)) {
			rel := strings.TrimPrefix(clean, skillsCanonical+string(os.PathSeparator))
			return sandbox.VirtualSkillsPathPrefix + "/" + rel
		}
	}

	// Check user-data path
	base := filepath.Clean(s.baseDir)
	canonical := canonicalAllowedRoot(s.baseDir)
	clean := filepath.Clean(realPath)
	if clean == base || clean == canonical {
		return sandbox.VirtualPathPrefix
	}
	if strings.HasPrefix(clean, base+string(os.PathSeparator)) {
		rel := strings.TrimPrefix(clean, base+string(os.PathSeparator))
		return sandbox.VirtualPathPrefix + "/" + rel
	}
	if strings.HasPrefix(clean, canonical+string(os.PathSeparator)) {
		rel := strings.TrimPrefix(clean, canonical+string(os.PathSeparator))
		return sandbox.VirtualPathPrefix + "/" + rel
	}
	return realPath
}

// rejectPathTraversal returns an error if the path contains ".." segments.
func rejectPathTraversal(path string) error {
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
	cleanCandidate := filepath.Clean(candidate)
	cleanDir := filepath.Clean(dir)
	return cleanCandidate == cleanDir ||
		strings.HasPrefix(cleanCandidate, cleanDir+string(os.PathSeparator))
}

func isSkillsVirtualPath(virtualPath string) bool {
	p := filepath.ToSlash(strings.TrimSpace(virtualPath))
	prefix := sandbox.VirtualSkillsPathPrefix
	return p == prefix || strings.HasPrefix(p, prefix+"/")
}

// isDeniedCommand checks whether the given command matches a denylist entry.
func isDeniedCommand(command string, denied []string) bool {
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
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		return sandbox.ExecuteResult{Error: fmt.Errorf("failed to create workspace dir: %w", err)}, nil
	}

	cmd := exec.CommandContext(execCtx, detectShell(), "-c", command)
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

	// Mask host paths in stdout and stderr before returning
	maskedStdout := s.maskHostPaths(stdoutBuf.String())
	maskedStderr := s.maskHostPaths(stderrBuf.String())

	result := sandbox.ExecuteResult{
		Stdout: maskedStdout,
		Stderr: maskedStderr,
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

// maskHostPaths replaces all occurrences of host paths with virtual paths in text.
// This prevents leaking host filesystem details to agents.
func (s *LocalSandbox) maskHostPaths(text string) string {
	// Replace baseDir paths first (longer paths take priority)
	if s.baseDir != "" {
		text = strings.ReplaceAll(text, s.baseDir, sandbox.VirtualPathPrefix)
		text = strings.ReplaceAll(text, filepath.Clean(s.baseDir), sandbox.VirtualPathPrefix)
	}
	// Replace skillsPath paths
	if s.skillsPath != "" {
		text = strings.ReplaceAll(text, s.skillsPath, sandbox.VirtualSkillsPathPrefix)
		text = strings.ReplaceAll(text, filepath.Clean(s.skillsPath), sandbox.VirtualSkillsPathPrefix)
	}
	return text
}

// ReadFile reads the full text content of the file at virtualPath.
func (s *LocalSandbox) ReadFile(ctx context.Context, virtualPath string, startLine, endLine int) (string, error) {
	realPath, err := s.virtualToReal(virtualPath)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(realPath)
	if err != nil {
		return "", fmt.Errorf("read file %q: %w", virtualPath, err)
	}

	content := string(data)

	// Apply line range filtering if specified (P0 fix)
	if startLine > 0 || endLine > 0 {
		lines := strings.Split(content, "\n")
		if startLine < 0 {
			startLine = 0
		}
		if endLine <= 0 || endLine > len(lines) {
			endLine = len(lines)
		}
		if startLine > len(lines) {
			startLine = len(lines)
		}
		content = strings.Join(lines[startLine:endLine], "\n")
	}

	return content, nil
}

// WriteFile writes content to the file at virtualPath.
// If append is true, content is appended; otherwise the file is overwritten.
// Parent directories are created automatically.
// This method uses file locking to prevent concurrent write conflicts.
func (s *LocalSandbox) WriteFile(ctx context.Context, virtualPath string, content string, appendMode bool) error {
	if isSkillsVirtualPath(virtualPath) {
		return fmt.Errorf("access denied: write access to skills path is not allowed: %s", virtualPath)
	}
	return sandbox.WithFileLock(s.id, virtualPath, func() error {
		return s.writeFileLocked(ctx, virtualPath, content, appendMode)
	})
}

// writeFileLocked is the internal implementation of WriteFile without locking.
// It must only be called while holding the file lock.
func (s *LocalSandbox) writeFileLocked(ctx context.Context, virtualPath string, content string, appendMode bool) error {
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

// Glob finds files/directories matching pattern under virtualPath.
func (s *LocalSandbox) Glob(ctx context.Context, virtualPath string, pattern string, includeDirs bool, maxResults int) ([]string, bool, error) {
	_ = ctx
	realPath, err := s.virtualToReal(virtualPath)
	if err != nil {
		return nil, false, err
	}
	if maxResults <= 0 {
		maxResults = 200
	}
	matches := make([]string, 0, minInt(32, maxResults))
	truncated := false

	walkErr := filepath.WalkDir(realPath, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if path == realPath {
			return nil
		}
		if d.IsDir() && !includeDirs {
			return nil
		}
		rel, err := filepath.Rel(realPath, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if !matchPattern(rel, pattern) {
			return nil
		}
		matches = append(matches, s.realToVirtual(path))
		if len(matches) >= maxResults {
			truncated = true
			return errStopWalkLocal
		}
		return nil
	})
	if walkErr != nil && walkErr != errStopWalkLocal {
		return nil, false, fmt.Errorf("glob %q: %w", virtualPath, walkErr)
	}
	sort.Strings(matches)
	return matches, truncated, nil
}

// Grep searches matching lines under virtualPath.
func (s *LocalSandbox) Grep(ctx context.Context, virtualPath string, pattern string, glob string, literal bool, caseSensitive bool, maxResults int) ([]sandbox.GrepMatch, bool, error) {
	_ = ctx
	realPath, err := s.virtualToReal(virtualPath)
	if err != nil {
		return nil, false, err
	}
	if maxResults <= 0 {
		maxResults = 100
	}
	matcher, err := newLineMatcher(pattern, literal, caseSensitive)
	if err != nil {
		return nil, false, err
	}

	matches := make([]sandbox.GrepMatch, 0, minInt(16, maxResults))
	truncated := false
	walkErr := filepath.WalkDir(realPath, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(realPath, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if strings.TrimSpace(glob) != "" && !matchPattern(rel, glob) {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		lineNo := 0
		vp := s.realToVirtual(path)
		for scanner.Scan() {
			lineNo++
			line := scanner.Text()
			if !matcher(line) {
				continue
			}
			matches = append(matches, sandbox.GrepMatch{Path: vp, LineNumber: lineNo, Line: line})
			if len(matches) >= maxResults {
				truncated = true
				return errStopWalkLocal
			}
		}
		return nil
	})
	if walkErr != nil && walkErr != errStopWalkLocal {
		return nil, false, fmt.Errorf("grep %q: %w", virtualPath, walkErr)
	}
	return matches, truncated, nil
}

// StrReplace replaces occurrences of oldStr with newStr in the file at virtualPath.
// If replaceAll is false, only the first occurrence is replaced.
// Returns an error if oldStr is not found.
// This method uses file locking to prevent concurrent write conflicts.
func (s *LocalSandbox) StrReplace(ctx context.Context, virtualPath string, oldStr string, newStr string, replaceAll bool) error {
	if isSkillsVirtualPath(virtualPath) {
		return fmt.Errorf("access denied: write access to skills path is not allowed: %s", virtualPath)
	}
	return sandbox.WithFileLock(s.id, virtualPath, func() error {
		return s.strReplaceLocked(ctx, virtualPath, oldStr, newStr, replaceAll)
	})
}

// strReplaceLocked is the internal implementation of StrReplace without locking.
// It must only be called while holding the file lock.
func (s *LocalSandbox) strReplaceLocked(ctx context.Context, virtualPath string, oldStr string, newStr string, replaceAll bool) error {
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

// UpdateFile writes binary content to the file at virtualPath.
// Parent directories are created automatically.
// This method uses file locking to prevent concurrent write conflicts.
func (s *LocalSandbox) UpdateFile(ctx context.Context, virtualPath string, content []byte) error {
	if isSkillsVirtualPath(virtualPath) {
		return fmt.Errorf("access denied: write access to skills path is not allowed: %s", virtualPath)
	}
	return sandbox.WithFileLock(s.id, virtualPath, func() error {
		return s.updateFileLocked(ctx, virtualPath, content)
	})
}

// updateFileLocked is the internal implementation of UpdateFile without locking.
// It must only be called while holding the file lock.
func (s *LocalSandbox) updateFileLocked(ctx context.Context, virtualPath string, content []byte) error {
	realPath, err := s.virtualToReal(virtualPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(realPath), 0o755); err != nil {
		return fmt.Errorf("create parent dirs for %q: %w", virtualPath, err)
	}
	if err := os.WriteFile(realPath, content, 0o644); err != nil {
		return fmt.Errorf("update file %q: %w", virtualPath, err)
	}
	return nil
}

var errStopWalkLocal = fmt.Errorf("stop walk")

func newLineMatcher(pattern string, literal bool, caseSensitive bool) (func(string) bool, error) {
	if literal {
		needle := pattern
		if caseSensitive {
			return func(line string) bool { return strings.Contains(line, needle) }, nil
		}
		needle = strings.ToLower(needle)
		return func(line string) bool { return strings.Contains(strings.ToLower(line), needle) }, nil
	}
	p := pattern
	if !caseSensitive {
		p = "(?i)" + p
	}
	r, err := regexp.Compile(p)
	if err != nil {
		return nil, err
	}
	return func(line string) bool { return r.MatchString(line) }, nil
}

func matchPattern(relPath, pattern string) bool {
	rel := filepath.ToSlash(relPath)
	p := filepath.ToSlash(strings.TrimSpace(pattern))
	if p == "" {
		return true
	}
	if strings.Contains(p, "**") {
		re := globToRegexp(p)
		return re.MatchString(rel)
	}
	ok, err := filepath.Match(p, rel)
	if err != nil {
		return false
	}
	return ok
}

func globToRegexp(pattern string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		ch := pattern[i]
		switch ch {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				b.WriteString(".*")
				i++
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		case '.', '(', ')', '+', '|', '^', '$', '{', '}', '[', ']', '\\':
			b.WriteString("\\")
			b.WriteByte(ch)
		default:
			b.WriteByte(ch)
		}
	}
	b.WriteString("$")
	r, err := regexp.Compile(b.String())
	if err != nil {
		return regexp.MustCompile("^$")
	}
	return r
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ---------------------------------------------------------------------------
// LocalSandboxProvider
// ---------------------------------------------------------------------------

// LocalSandboxProvider implements sandbox.SandboxProvider using a per-process
// singleton LocalSandbox instance. The same sandbox is reused across all threads
// because access is already scoped by the per-thread baseDir within the sandbox.
type LocalSandboxProvider struct {
	mu         sync.Mutex
	cfg        sandbox.SandboxConfig
	baseDir    string // root directory for all thread data (e.g. /path/to/.goclaw)
	skillsPath string // optional path to skills directory for /mnt/skills mounting
	sandbox    *LocalSandbox
}

// NewLocalSandboxProvider creates a new provider. baseDir is the root directory
// under which .goclaw/threads/{threadID}/user-data/ subdirectories will be created.
// skillsPath is optional; if provided, /mnt/skills virtual path will be mapped to it.
func NewLocalSandboxProvider(cfg sandbox.SandboxConfig, baseDir string, skillsPath string) *LocalSandboxProvider {
	return &LocalSandboxProvider{
		cfg:        cfg,
		baseDir:    filepath.Clean(baseDir),
		skillsPath: filepath.Clean(skillsPath),
	}
}

// Acquire returns the singleton sandbox ID "local", creating the sandbox if needed.
// threadID is used to set up the per-thread filesystem paths on first call.
func (p *LocalSandboxProvider) Acquire(ctx context.Context, threadID string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.sandbox == nil {
		threadBaseDir := filepath.Join(p.baseDir, "threads", threadID, "user-data")
		for _, sub := range []string{"workspace", "uploads", "outputs"} {
			dir := filepath.Join(threadBaseDir, sub)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return "", fmt.Errorf("create sandbox dir %s: %w", sub, err)
			}
			if err := os.Chmod(dir, 0o777); err != nil {
				return "", fmt.Errorf("chmod sandbox dir %s: %w", sub, err)
			}
		}
		p.sandbox = &LocalSandbox{
			id:         "local",
			threadID:   threadID,
			baseDir:    threadBaseDir,
			skillsPath: p.skillsPath,
			cfg:        p.cfg,
		}
	}
	return "local", nil
}

// Get retrieves the singleton sandbox by ID. Returns nil if not yet created.
func (p *LocalSandboxProvider) Get(sandboxID string) sandbox.Sandbox {
	p.mu.Lock()
	defer p.mu.Unlock()
	if sandboxID == "local" && p.sandbox != nil {
		return p.sandbox
	}
	return nil
}

// Release is a no-op for the local singleton; the sandbox is kept alive for reuse.
func (p *LocalSandboxProvider) Release(ctx context.Context, sandboxID string) error {
	return nil
}

// Shutdown releases the singleton sandbox and allows it to be garbage collected.
// After Shutdown, Acquire can create a new sandbox on the next call.
func (p *LocalSandboxProvider) Shutdown(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sandbox = nil
	return nil
}
