// Package sandbox defines the abstract interfaces for sandbox execution environments.
// Sandboxes provide isolated environments for executing commands and managing files,
// with virtual path translation to hide host filesystem details from agents.
package sandbox

import (
	"context"
	"sync"
	"time"
)

// VirtualPathPrefix is the virtual mount point agents see for user data.
// All agent-facing paths should start with this prefix.
const VirtualPathPrefix = "/mnt/user-data"

var (
	defaultProviderMu sync.RWMutex
	defaultProvider   SandboxProvider
)

// SetDefaultProvider stores the process-wide sandbox provider for tool wrappers.
func SetDefaultProvider(p SandboxProvider) {
	defaultProviderMu.Lock()
	defer defaultProviderMu.Unlock()
	defaultProvider = p
}

// DefaultProvider returns the process-wide sandbox provider.
func DefaultProvider() SandboxProvider {
	defaultProviderMu.RLock()
	defer defaultProviderMu.RUnlock()
	return defaultProvider
}

// SandboxType identifies which sandbox implementation to use.
type SandboxType string

const (
	// SandboxTypeLocal runs commands directly on the host filesystem,
	// restricted to per-thread directories. Bash execution is disabled by default.
	SandboxTypeLocal SandboxType = "local"

	// SandboxTypeDocker runs commands inside an isolated Docker container
	// with resource limits and volume mounts.
	SandboxTypeDocker SandboxType = "docker"
)

// SandboxConfig holds configuration for creating a sandbox provider.
type SandboxConfig struct {
	// Type selects the sandbox implementation.
	Type SandboxType `yaml:"type" json:"type"`

	// WorkDir is the base working directory on the host.
	// For local sandbox: base dir for per-thread subdirectories.
	// For docker sandbox: volume mount source on the host.
	WorkDir string `yaml:"work_dir" json:"work_dir"`

	// AllowedCommands is an allowlist of command prefixes permitted in local mode.
	// If empty, only the internal Go-implemented tools are available (no shell exec).
	AllowedCommands []string `yaml:"allowed_commands" json:"allowed_commands"`

	// DeniedCommands is a denylist of dangerous command prefixes that are always blocked,
	// evaluated before AllowedCommands.
	DeniedCommands []string `yaml:"denied_commands" json:"denied_commands"`

	// ExecTimeout is the maximum duration allowed for a single Execute call.
	// Defaults to 30s if zero.
	ExecTimeout time.Duration `yaml:"exec_timeout" json:"exec_timeout"`

	// Docker-specific options (ignored for local sandbox).
	Docker DockerConfig `yaml:"docker" json:"docker"`
}

// DockerVolumeMount describes one host-to-container bind mount.
type DockerVolumeMount struct {
	HostPath      string `yaml:"host_path" json:"host_path"`
	ContainerPath string `yaml:"container_path" json:"container_path"`
	ReadOnly      bool   `yaml:"read_only,omitempty" json:"read_only,omitempty"`
}

// DockerConfig holds Docker-specific sandbox configuration.
type DockerConfig struct {
	// Image is the Docker image to use for sandbox containers.
	Image string `yaml:"image" json:"image"`

	// ContainerPrefix is the container name prefix (thread id is appended).
	ContainerPrefix string `yaml:"container_prefix,omitempty" json:"container_prefix,omitempty"`

	// CPUQuota is the CPU quota in microseconds per 100ms period (e.g. 100000 = 1 CPU).
	// 0 means no limit.
	CPUQuota int64 `yaml:"cpu_quota" json:"cpu_quota"`

	// MemoryBytes is the memory limit in bytes. 0 means no limit.
	MemoryBytes int64 `yaml:"memory_bytes" json:"memory_bytes"`

	// NetworkDisabled disables all container networking when true.
	NetworkDisabled bool `yaml:"network_disabled" json:"network_disabled"`

	// ContainerTTL is how long an idle container is kept before being removed.
	// Defaults to 10 minutes if zero.
	ContainerTTL time.Duration `yaml:"container_ttl" json:"container_ttl"`

	// SkillsMountPath is the host path for the skills directory to mount read-only.
	// Empty means skills volume is not mounted.
	SkillsMountPath string `yaml:"skills_mount_path" json:"skills_mount_path"`

	// Mounts are additional bind mounts injected into docker sandboxes.
	Mounts []DockerVolumeMount `yaml:"mounts,omitempty" json:"mounts,omitempty"`

	// Environment variables injected into the container.
	Environment map[string]string `yaml:"environment,omitempty" json:"environment,omitempty"`
}

// ExecuteResult holds the outcome of a command execution.
type ExecuteResult struct {
	// Stdout is the standard output captured from the command.
	Stdout string

	// Stderr is the standard error output captured from the command.
	Stderr string

	// ExitCode is the process exit code. 0 indicates success.
	ExitCode int

	// Error holds a non-nil value when execution failed due to a system-level
	// error (timeout, permission denied, sandbox not available, etc.), distinct
	// from a non-zero exit code returned by the command itself.
	Error error
}

// FileInfo describes a single filesystem entry returned by ListDir.
type FileInfo struct {
	// Name is the base filename (not the full path).
	Name string

	// Path is the full virtual path as seen by the agent (e.g. /mnt/user-data/workspace/foo.py).
	Path string

	// Size is the file size in bytes. 0 for directories.
	Size int64

	// IsDir indicates whether this entry is a directory.
	IsDir bool

	// ModTime is the last modification time.
	ModTime time.Time
}

// GrepMatch is a single line match returned by Sandbox.Grep.
type GrepMatch struct {
	// Path is the virtual path of the file containing the match.
	Path string
	// LineNumber is the 1-indexed line number in the file.
	LineNumber int
	// Line is the text content of the matched line.
	Line string
}

// Sandbox is the core interface every sandbox implementation must satisfy.
// All path arguments are virtual paths (e.g. /mnt/user-data/workspace/...).
// Implementations translate them to real host or container paths internally.
type Sandbox interface {
	// ID returns the unique identifier of this sandbox instance.
	ID() string

	// Execute runs a shell command inside the sandbox with the given context.
	// Returns an ExecuteResult regardless of exit code; only returns a non-nil
	// error in the ExecuteResult.Error field for system-level failures.
	Execute(ctx context.Context, command string) (ExecuteResult, error)

	// ReadFile reads the full contents of a file at the given virtual path.
	ReadFile(ctx context.Context, path string) (string, error)

	// WriteFile writes content to the file at the given virtual path.
	// If append is true, content is appended instead of overwriting.
	// Parent directories are created automatically.
	WriteFile(ctx context.Context, path string, content string, append bool) error

	// ListDir lists the direct children of a directory up to maxDepth levels deep.
	// maxDepth=1 lists only direct children; maxDepth=2 includes grandchildren, etc.
	ListDir(ctx context.Context, path string, maxDepth int) ([]FileInfo, error)

	// StrReplace replaces occurrences of oldStr with newStr in the file at path.
	// If replaceAll is false, only the first occurrence is replaced.
	// Returns an error if oldStr is not found.
	StrReplace(ctx context.Context, path string, oldStr string, newStr string, replaceAll bool) error

	// Glob finds paths matching pattern under path.
	// Returns (matches, truncated, error).
	Glob(ctx context.Context, path string, pattern string, includeDirs bool, maxResults int) ([]string, bool, error)

	// Grep searches lines matching pattern under path.
	// glob optionally restricts candidate files.
	// Returns (matches, truncated, error).
	Grep(ctx context.Context, path string, pattern string, glob string, literal bool, caseSensitive bool, maxResults int) ([]GrepMatch, bool, error)

	// UpdateFile writes binary content to the file at path.
	// Parent directories are created automatically.
	// This is the binary equivalent of WriteFile, used for uploading images, archives, etc.
	UpdateFile(ctx context.Context, path string, content []byte) error
}

// SandboxProvider manages the lifecycle of Sandbox instances.
// A single provider is typically a singleton shared across all goroutines.
type SandboxProvider interface {
	// Acquire returns the sandbox ID for the given thread, creating one if needed.
	// threadID is used to scope per-thread filesystem directories and containers.
	// It is safe to call concurrently; implementations must use internal locking.
	Acquire(ctx context.Context, threadID string) (sandboxID string, err error)

	// Get retrieves an existing sandbox by its ID. Returns nil if not found.
	Get(sandboxID string) Sandbox

	// Release signals that the caller no longer needs the sandbox.
	// Depending on the implementation this may be a no-op (local singleton)
	// or may stop/remove a container (docker).
	Release(ctx context.Context, sandboxID string) error

	// Shutdown tears down all active sandboxes and releases resources.
	// Should be called on application exit.
	Shutdown(ctx context.Context) error
}
