// Package docker provides a DockerSandbox that runs commands and file operations
// inside isolated Docker containers with resource limits and volume mounts.
//
// Each thread gets its own container (or reuses an existing one). Containers are
// identified by a deterministic name derived from the thread ID so that they can
// be looked up without maintaining external state across process restarts.
//
// Volume layout inside the container:
//
//	/mnt/user-data/workspace  (rw) – per-thread working directory
//	/mnt/user-data/uploads    (rw) – uploaded files
//	/mnt/user-data/outputs    (rw) – agent output artefacts
//	/mnt/skills               (ro) – optional skills directory
//
// The host path for these volumes is:
//
//	{WorkDir}/threads/{threadID}/user-data/{workspace|uploads|outputs}
package docker

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	dockerclient "github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"

	"github.com/bookerbai/goclaw/internal/sandbox"
)

// defaultContainerNamePrefix is prepended to thread IDs when no custom prefix is configured.
// Using deterministic names lets us find existing containers after process restarts.
const defaultContainerNamePrefix = "goclaw-sandbox-"

// containerStopTimeout is passed to ContainerStop when removing containers.
const containerStopTimeout = 10 * time.Second

// defaultContainerTTL is used when DockerConfig.ContainerTTL is zero.
const defaultContainerTTL = 10 * time.Minute

// DockerSandbox implements sandbox.Sandbox by executing operations inside a
// Docker container identified by containerID.
type DockerSandbox struct {
	id          string
	threadID    string
	containerID string
	baseDir     string // host-side base for this thread's user-data volumes
	client      *dockerclient.Client
	cfg         sandbox.SandboxConfig
	lastUsed    time.Time
	mu          sync.Mutex
}

// ID returns the sandbox identifier (equal to the container name).
func (s *DockerSandbox) ID() string {
	return s.id
}

func effectiveContainerPrefix(cfg sandbox.SandboxConfig) string {
	prefix := strings.TrimSpace(cfg.Docker.ContainerPrefix)
	if prefix == "" {
		return defaultContainerNamePrefix
	}
	if strings.HasSuffix(prefix, "-") {
		return prefix
	}
	return prefix + "-"
}

// containerName returns the deterministic Docker container name for this thread.
func containerName(cfg sandbox.SandboxConfig, threadID string) string {
	return effectiveContainerPrefix(cfg) + threadID
}

// virtualToContainerPath translates a virtual /mnt/user-data/... path into the
// equivalent path inside the container. For docker sandboxes the virtual paths
// are directly mounted so no translation is needed beyond a basic validation.
func virtualToContainerPath(virtualPath string) (string, error) {
	// TODO:
	//  1. Check virtualPath starts with sandbox.VirtualPathPrefix or "/mnt/skills".
	//  2. Reject ".." segments via rejectPathTraversal.
	//  3. Return virtualPath as-is (it maps directly to the container mount point).
	if err := rejectPathTraversal(virtualPath); err != nil {
		return "", err
	}
	if strings.HasPrefix(virtualPath, sandbox.VirtualPathPrefix) ||
		strings.HasPrefix(virtualPath, "/mnt/skills") {
		return virtualPath, nil
	}
	return "", fmt.Errorf("path %q is outside allowed virtual prefix", virtualPath)
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

// Execute runs a shell command inside the Docker container via docker exec.
// The container is started lazily if it exists but is not running.
func (s *DockerSandbox) Execute(ctx context.Context, command string) (sandbox.ExecuteResult, error) {
	s.mu.Lock()
	s.lastUsed = time.Now()
	s.mu.Unlock()

	if err := s.ensureContainerRunning(ctx); err != nil {
		return sandbox.ExecuteResult{Error: err}, nil
	}

	// Apply execution timeout (P2 alignment with DeerFlow's 600s default)
	timeout := s.cfg.ExecTimeout
	if timeout == 0 {
		timeout = 600 * time.Second // defaultExecTimeout aligned with DeerFlow
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	execCfg := types.ExecConfig{
		Cmd:          []string{"bash", "-c", command},
		AttachStdout: true,
		AttachStderr: true,
		WorkingDir:   sandbox.VirtualPathPrefix + "/workspace",
	}
	execID, err := s.client.ContainerExecCreate(execCtx, s.containerID, execCfg)
	if err != nil {
		return sandbox.ExecuteResult{Error: fmt.Errorf("exec create: %w", err)}, nil
	}

	resp, err := s.client.ContainerExecAttach(execCtx, execID.ID, types.ExecStartCheck{})
	if err != nil {
		return sandbox.ExecuteResult{Error: fmt.Errorf("exec attach: %w", err)}, nil
	}
	defer resp.Close()

	var stdoutBuf, stderrBuf bytes.Buffer
	// Use stdcopy to demultiplex Docker's multiplexed stream into stdout/stderr.
	if _, err := stdcopy.StdCopy(&stdoutBuf, &stderrBuf, resp.Reader); err != nil && err != io.EOF {
		// Non-fatal: return what we have.
		_ = err
	}

	inspectResult, err := s.client.ContainerExecInspect(execCtx, execID.ID)
	exitCode := 0
	if err == nil {
		exitCode = inspectResult.ExitCode
	}

	// Check for timeout
	if execCtx.Err() == context.DeadlineExceeded {
		return sandbox.ExecuteResult{
			Stdout:   stdoutBuf.String(),
			Stderr:   stderrBuf.String() + "\nCommand timed out",
			ExitCode: 124, // Standard timeout exit code
		}, nil
	}

	return sandbox.ExecuteResult{
		Stdout:   stdoutBuf.String(),
		Stderr:   stderrBuf.String(),
		ExitCode: exitCode,
	}, nil
}

// ReadFile reads a file from inside the container by copying it to stdout.
func (s *DockerSandbox) ReadFile(ctx context.Context, virtualPath string, startLine, endLine int) (string, error) {
	containerPath, err := virtualToContainerPath(virtualPath)
	if err != nil {
		return "", err
	}

	// Build command with optional line range (P0 fix)
	cmd := "cat " + shellQuote(containerPath)
	if startLine > 0 || endLine > 0 {
		// Use sed to extract line range
		if startLine < 0 {
			startLine = 0
		}
		if startLine > 0 {
			// sed is 1-based, Go is 0-based
			startLine++
		}
		if endLine > 0 {
			cmd = fmt.Sprintf("sed -n '%d,%dp' %s", startLine+1, endLine, shellQuote(containerPath))
		} else {
			cmd = fmt.Sprintf("sed -n '%d,$p' %s", startLine+1, shellQuote(containerPath))
		}
	}

	result, err := s.Execute(ctx, cmd)
	if err != nil {
		return "", err
	}
	if result.ExitCode != 0 {
		return "", fmt.Errorf("read file %q: %s", virtualPath, result.Stderr)
	}
	return result.Stdout, nil
}

// WriteFile writes content to a file inside the container.
// This method uses file locking to prevent concurrent write conflicts.
func (s *DockerSandbox) WriteFile(ctx context.Context, virtualPath string, content string, appendMode bool) error {
	return sandbox.WithFileLock(s.id, virtualPath, func() error {
		return s.writeFileLocked(ctx, virtualPath, content, appendMode)
	})
}

// writeFileLocked is the internal implementation of WriteFile without locking.
func (s *DockerSandbox) writeFileLocked(ctx context.Context, virtualPath string, content string, appendMode bool) error {
	containerPath, err := virtualToContainerPath(virtualPath)
	if err != nil {
		return err
	}
	dir := filepath.ToSlash(filepath.Dir(containerPath))
	redirect := ">"
	if appendMode {
		redirect = ">>"
	}
	// Use base64 encoding to safely handle arbitrary content including special characters and binary data.
	encoded := base64.StdEncoding.EncodeToString([]byte(content))
	cmd := fmt.Sprintf("mkdir -p %s && echo %s | base64 -d %s %s",
		shellQuote(dir), shellQuote(encoded), redirect, shellQuote(containerPath))
	result, execErr := s.Execute(ctx, cmd)
	if execErr != nil {
		return execErr
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("write file %q: %s", virtualPath, result.Stderr)
	}
	return nil
}

// ListDir lists files inside the container up to maxDepth levels deep.
func (s *DockerSandbox) ListDir(ctx context.Context, virtualPath string, maxDepth int) ([]sandbox.FileInfo, error) {
	// TODO:
	//  1. virtualToContainerPath.
	//  2. Execute find command: find {path} -maxdepth {maxDepth} -printf "%y %s %T@ %P\n".
	//  3. Parse each line to build []FileInfo with Path = virtualPath + "/" + relative.
	containerPath, err := virtualToContainerPath(virtualPath)
	if err != nil {
		return nil, err
	}
	if maxDepth <= 0 {
		maxDepth = 2
	}
	cmd := fmt.Sprintf(
		`find %s -maxdepth %d -mindepth 1 -printf "%%y\t%%s\t%%T@\t%%P\n" 2>/dev/null`,
		shellQuote(containerPath), maxDepth,
	)
	result, execErr := s.Execute(ctx, cmd)
	if execErr != nil {
		return nil, execErr
	}
	if result.ExitCode != 0 {
		return nil, fmt.Errorf("list dir %q: %s", virtualPath, result.Stderr)
	}

	var infos []sandbox.FileInfo
	// TODO: Parse result.Stdout lines into FileInfo structs.
	// Each line format: {type}\t{size}\t{epoch_seconds.ns}\t{relative_path}
	// where type is 'f' for file or 'd' for directory.
	for _, line := range strings.Split(strings.TrimSpace(result.Stdout), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 4)
		if len(parts) != 4 {
			continue
		}
		ftype, rel := parts[0], parts[3]
		isDir := ftype == "d"
		var size int64
		fmt.Sscanf(parts[1], "%d", &size)
		var epochSec float64
		fmt.Sscanf(parts[2], "%f", &epochSec)
		sec := int64(epochSec)
		nsec := int64((epochSec - float64(sec)) * 1e9)
		modTime := time.Unix(sec, nsec)
		infos = append(infos, sandbox.FileInfo{
			Name:    filepath.Base(rel),
			Path:    virtualPath + "/" + rel,
			Size:    size,
			IsDir:   isDir,
			ModTime: modTime,
		})
	}
	return infos, nil
}

// Glob finds files/directories matching pattern under virtualPath.
// This executes inside the Docker container to maintain sandbox isolation.
func (s *DockerSandbox) Glob(ctx context.Context, virtualPath string, pattern string, includeDirs bool, maxResults int) ([]string, bool, error) {
	containerPath, err := virtualToContainerPath(virtualPath)
	if err != nil {
		return nil, false, err
	}
	if maxResults <= 0 {
		maxResults = 200
	}

	// Build find command to execute inside container.
	// Use find with -name pattern for simple cases, or execute a shell loop for glob patterns.
	var cmd string
	if includeDirs {
		cmd = fmt.Sprintf("cd %s && find . -mindepth 1 -maxdepth 10 -print 2>/dev/null | head -n %d",
			shellQuote(containerPath), maxResults+1)
	} else {
		cmd = fmt.Sprintf("cd %s && find . -mindepth 1 -maxdepth 10 -type f -print 2>/dev/null | head -n %d",
			shellQuote(containerPath), maxResults+1)
	}

	result, execErr := s.Execute(ctx, cmd)
	if execErr != nil {
		return nil, false, execErr
	}
	if result.ExitCode != 0 {
		return nil, false, fmt.Errorf("glob %q: %s", virtualPath, result.Stderr)
	}

	// Parse results and apply pattern matching.
	lines := strings.Split(strings.TrimSpace(result.Stdout), "\n")
	matches := make([]string, 0, minIntDocker(32, maxResults))
	truncated := false

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Remove leading "./"
		rel := strings.TrimPrefix(line, "./")
		if !matchPatternDocker(rel, pattern) {
			continue
		}
		matches = append(matches, joinVirtualPath(virtualPath, rel))
		if len(matches) >= maxResults {
			truncated = true
			break
		}
	}

	sort.Strings(matches)
	return matches, truncated, nil
}

// Grep searches matching lines under virtualPath.
// This executes inside the Docker container to maintain sandbox isolation.
func (s *DockerSandbox) Grep(ctx context.Context, virtualPath string, pattern string, glob string, literal bool, caseSensitive bool, maxResults int) ([]sandbox.GrepMatch, bool, error) {
	containerPath, err := virtualToContainerPath(virtualPath)
	if err != nil {
		return nil, false, err
	}
	if maxResults <= 0 {
		maxResults = 100
	}

	// Build grep command to execute inside container.
	// Use grep with -r for recursive search, -n for line numbers.
	var grepOpts string
	if literal {
		grepOpts += " -F"
	}
	if !caseSensitive {
		grepOpts += " -i"
	}

	var cmd string
	if strings.TrimSpace(glob) != "" {
		// Use --include for glob filtering.
		cmd = fmt.Sprintf("cd %s && grep -r%s -n --include=%s -e %s . 2>/dev/null | head -n %d",
			shellQuote(containerPath), grepOpts, shellQuote(glob), shellQuote(pattern), maxResults+1)
	} else {
		cmd = fmt.Sprintf("cd %s && grep -r%s -n -e %s . 2>/dev/null | head -n %d",
			shellQuote(containerPath), grepOpts, shellQuote(pattern), maxResults+1)
	}

	result, execErr := s.Execute(ctx, cmd)
	if execErr != nil {
		return nil, false, execErr
	}
	// Exit code 1 means no matches, which is OK.
	if result.ExitCode != 0 && result.ExitCode != 1 {
		return nil, false, fmt.Errorf("grep %q: %s", virtualPath, result.Stderr)
	}

	// Parse results: format is "./path/to/file:line_number:content"
	lines := strings.Split(strings.TrimSpace(result.Stdout), "\n")
	matches := make([]sandbox.GrepMatch, 0, minIntDocker(16, maxResults))
	truncated := false

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Parse "./path/to/file:line_number:content"
		// Handle the case where content may contain ":"
		if !strings.HasPrefix(line, "./") {
			continue
		}

		// Find first two colons
		firstColon := strings.Index(line, ":")
		if firstColon < 0 {
			continue
		}
		secondColon := strings.Index(line[firstColon+1:], ":")
		if secondColon < 0 {
			continue
		}
		secondColon += firstColon + 1

		relPath := line[2:firstColon]
		lineNumStr := line[firstColon+1 : secondColon]
		content := line[secondColon+1:]

		var lineNum int
		if _, err := fmt.Sscanf(lineNumStr, "%d", &lineNum); err != nil {
			continue
		}

		vp := joinVirtualPath(virtualPath, relPath)
		matches = append(matches, sandbox.GrepMatch{
			Path:       vp,
			LineNumber: lineNum,
			Line:       content,
		})

		if len(matches) >= maxResults {
			truncated = true
			break
		}
	}

	return matches, truncated, nil
}

// StrReplace replaces a string inside a file in the container.
// This method uses file locking to prevent concurrent write conflicts.
func (s *DockerSandbox) StrReplace(ctx context.Context, virtualPath string, oldStr string, newStr string, replaceAll bool) error {
	return sandbox.WithFileLock(s.id, virtualPath, func() error {
		return s.strReplaceLocked(ctx, virtualPath, oldStr, newStr, replaceAll)
	})
}

// strReplaceLocked is the internal implementation of StrReplace without locking.
func (s *DockerSandbox) strReplaceLocked(ctx context.Context, virtualPath string, oldStr string, newStr string, replaceAll bool) error {
	content, err := s.ReadFile(ctx, virtualPath, 0, 0) // Read entire file for str_replace
	if err != nil {
		return err
	}
	if !strings.Contains(content, oldStr) {
		return fmt.Errorf("str_replace: string to replace not found in %q", virtualPath)
	}
	n := 1
	if replaceAll {
		n = -1
	}
	newContent := strings.Replace(content, oldStr, newStr, n)
	return s.writeFileLocked(ctx, virtualPath, newContent, false)
}

// UpdateFile writes binary content to a file inside the container.
// Parent directories are created automatically.
// This method uses file locking to prevent concurrent write conflicts.
func (s *DockerSandbox) UpdateFile(ctx context.Context, virtualPath string, content []byte) error {
	return sandbox.WithFileLock(s.id, virtualPath, func() error {
		return s.updateFileLocked(ctx, virtualPath, content)
	})
}

// updateFileLocked is the internal implementation of UpdateFile without locking.
func (s *DockerSandbox) updateFileLocked(ctx context.Context, virtualPath string, content []byte) error {
	containerPath, err := virtualToContainerPath(virtualPath)
	if err != nil {
		return err
	}
	dir := filepath.ToSlash(filepath.Dir(containerPath))
	// Use base64 encoding to safely handle binary data.
	encoded := base64.StdEncoding.EncodeToString(content)
	cmd := fmt.Sprintf("mkdir -p %s && echo %s | base64 -d > %s",
		shellQuote(dir), shellQuote(encoded), shellQuote(containerPath))
	result, execErr := s.Execute(ctx, cmd)
	if execErr != nil {
		return execErr
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("update file %q: %s", virtualPath, result.Stderr)
	}
	return nil
}

func (s *DockerSandbox) virtualToHostPath(virtualPath string) (string, error) {
	if err := rejectPathTraversal(virtualPath); err != nil {
		return "", err
	}
	prefix := sandbox.VirtualPathPrefix
	if virtualPath == prefix {
		return filepath.Clean(s.baseDir), nil
	}
	if !strings.HasPrefix(virtualPath, prefix+"/") {
		return "", fmt.Errorf("path %q is outside allowed virtual prefix %s", virtualPath, prefix)
	}
	rel := strings.TrimPrefix(virtualPath, prefix+"/")
	host := filepath.Join(s.baseDir, rel)
	host = filepath.Clean(host)
	base := filepath.Clean(s.baseDir)
	if host != base && !strings.HasPrefix(host, base+string(os.PathSeparator)) {
		return "", fmt.Errorf("access denied: path traversal detected")
	}
	return host, nil
}

func joinVirtualPath(rootVirtual string, rel string) string {
	root := strings.TrimSuffix(filepath.ToSlash(rootVirtual), "/")
	r := strings.TrimPrefix(filepath.ToSlash(rel), "./")
	r = strings.TrimPrefix(r, "/")
	if r == "" {
		return root
	}
	return root + "/" + r
}

func matchPatternDocker(relPath, pattern string) bool {
	rel := filepath.ToSlash(relPath)
	p := filepath.ToSlash(strings.TrimSpace(pattern))
	if p == "" {
		return true
	}
	if strings.Contains(p, "**") {
		re := globToRegexpDocker(p)
		return re.MatchString(rel)
	}
	ok, err := filepath.Match(p, rel)
	if err != nil {
		return false
	}
	return ok
}

func globToRegexpDocker(pattern string) *regexp.Regexp {
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

func minIntDocker(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func buildContainerEnv(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		if strings.TrimSpace(k) == "" {
			continue
		}
		// Resolve $ENV syntax (P1 fix)
		resolvedValue := resolveEnvValue(env[k])
		out = append(out, fmt.Sprintf("%s=%s", k, resolvedValue))
	}
	return out
}

// resolveEnvValue resolves $ENV_VAR and $ENV_VAR:default syntax.
// - $ENV_VAR → value from environment, or empty string if not set
// - $ENV_VAR:default → value from environment, or "default" if not set
// This mirrors DeerFlow's environment variable resolution behavior.
func resolveEnvValue(value string) string {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "$") {
		return value
	}

	// Remove $ prefix
	varPart := strings.TrimPrefix(value, "$")

	// Check for default value syntax: $VAR:default
	var varName, defaultVal string
	if colonIdx := strings.Index(varPart, ":"); colonIdx >= 0 {
		varName = strings.TrimSpace(varPart[:colonIdx])
		defaultVal = varPart[colonIdx+1:]
	} else {
		varName = strings.TrimSpace(varPart)
	}

	if varName == "" {
		return value
	}

	// Look up environment variable
	if envVal, ok := os.LookupEnv(varName); ok {
		return envVal
	}

	// Return default value (empty if no default specified)
	return defaultVal
}

// ensureContainerRunning starts the container if it is stopped, or creates it
// if it does not exist yet.
func (s *DockerSandbox) ensureContainerRunning(ctx context.Context) error {
	inspect, err := s.client.ContainerInspect(ctx, s.containerID)
	if err != nil {
		if dockerclient.IsErrNotFound(err) {
			return s.createContainer(ctx)
		}
		return fmt.Errorf("inspect container %q: %w", s.containerID, err)
	}
	// If container exists but is not running, start it.
	if !inspect.State.Running {
		if err := s.client.ContainerStart(ctx, s.containerID, container.StartOptions{}); err != nil {
			return fmt.Errorf("start existing container %q: %w", s.containerID, err)
		}
	}
	return nil
}

// createContainer creates a new Docker container for this sandbox.
func (s *DockerSandbox) createContainer(ctx context.Context) error {
	// TODO:
	//  1. Prepare host-side volume directories (os.MkdirAll workspace/uploads/outputs).
	//  2. Build []mount.Mount for workspace, uploads, outputs (bind mounts, rw),
	//     and optionally skills (bind mount, ro).
	//  3. Build container.Config{Image, Env, WorkingDir, ...}.
	//  4. Build container.HostConfig{Resources: {CPUQuota, Memory}, Mounts, NetworkMode}.
	//  5. client.ContainerCreate(ctx, &config, &hostConfig, nil, nil, containerName).
	//  6. client.ContainerStart.
	//  7. Store returned container ID in s.containerID.
	workspacePath := filepath.Join(s.baseDir, "workspace")
	uploadsPath := filepath.Join(s.baseDir, "uploads")
	outputsPath := filepath.Join(s.baseDir, "outputs")

	for _, dir := range []string{workspacePath, uploadsPath, outputsPath} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create volume dir %q: %w", dir, err)
		}
	}

	mounts := []mount.Mount{
		{Type: mount.TypeBind, Source: workspacePath, Target: sandbox.VirtualPathPrefix + "/workspace"},
		{Type: mount.TypeBind, Source: uploadsPath, Target: sandbox.VirtualPathPrefix + "/uploads"},
		{Type: mount.TypeBind, Source: outputsPath, Target: sandbox.VirtualPathPrefix + "/outputs"},
	}
	if s.cfg.Docker.SkillsMountPath != "" {
		mounts = append(mounts, mount.Mount{
			Type:     mount.TypeBind,
			Source:   s.cfg.Docker.SkillsMountPath,
			Target:   "/mnt/skills",
			ReadOnly: true,
		})
	}
	for _, vm := range s.cfg.Docker.Mounts {
		hostPath := filepath.Clean(strings.TrimSpace(vm.HostPath))
		containerPath := filepath.ToSlash(strings.TrimSpace(vm.ContainerPath))
		if hostPath == "" || containerPath == "" {
			continue
		}
		if err := os.MkdirAll(hostPath, 0o755); err != nil {
			return fmt.Errorf("create mount dir %q: %w", hostPath, err)
		}
		mounts = append(mounts, mount.Mount{
			Type:     mount.TypeBind,
			Source:   hostPath,
			Target:   containerPath,
			ReadOnly: vm.ReadOnly,
		})
	}

	dockerCfg := &container.Config{
		Image:      s.cfg.Docker.Image,
		WorkingDir: sandbox.VirtualPathPrefix + "/workspace",
		Tty:        false,
		// Keep the container alive with a no-op command.
		Cmd: []string{"sleep", "infinity"},
		Env: buildContainerEnv(s.cfg.Docker.Environment),
	}
	resources := container.Resources{}
	if s.cfg.Docker.CPUQuota > 0 {
		resources.CPUQuota = s.cfg.Docker.CPUQuota
		resources.CPUPeriod = 100000
	}
	if s.cfg.Docker.MemoryBytes > 0 {
		resources.Memory = s.cfg.Docker.MemoryBytes
	}
	hostCfg := &container.HostConfig{
		Mounts:    mounts,
		Resources: resources,
	}
	if s.cfg.Docker.NetworkDisabled {
		hostCfg.NetworkMode = "none"
	}

	name := s.id
	resp, err := s.client.ContainerCreate(ctx, dockerCfg, hostCfg, nil, nil, name)
	if err != nil {
		return fmt.Errorf("create container %q: %w", name, err)
	}
	s.containerID = resp.ID

	if err := s.client.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return fmt.Errorf("start container %q: %w", name, err)
	}
	return nil
}

// shellQuote wraps a string in single quotes for safe shell interpolation.
// Interior single quotes are escaped.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// DockerSandboxProvider is defined in provider.go
