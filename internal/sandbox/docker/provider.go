// Package docker provides a DockerSandboxProvider that manages container lifecycle
// with warm-pool support, cross-process locking, and LRU eviction.
//
// This implementation mirrors DeerFlow's AioSandboxProvider architecture:
//   - In-process caching for fast repeated access
//   - Warm pool for released containers (no cold-start)
//   - Cross-process file locking for container creation
//   - Idle timeout management with background watchdog
//   - Replicas limit with LRU eviction
//   - Graceful shutdown
package docker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	dockerclient "github.com/docker/docker/client"

	"goclaw/internal/sandbox"
)

// Default docker client creation function - can be overridden in tests
var (
	defaultNewDockerClient = func() (DockerClient, error) {
		cli, err := dockerclient.NewClientWithOpts(
			dockerclient.FromEnv,
			dockerclient.WithAPIVersionNegotiation(),
		)
		if err != nil {
			return nil, err
		}
		return &dockerClientWrapper{
			client:        cli,
			isErrNotFound: dockerclient.IsErrNotFound,
		}, nil
	}
)

// Additional default configuration values for provider (specific to warm-pool)
const (
	defaultReplicas   = 3
	idleCheckInterval = 60 * time.Second
)

// DockerSandboxProvider implements sandbox.SandboxProvider with warm-pool support.
//
// Architecture (mirrors DeerFlow's AioSandboxProvider):
//   - _sandboxes: active sandboxes currently in use by threads
//   - _warm_pool: released sandboxes whose containers are still running
//   - _thread_sandboxes: maps thread_id -> sandbox_id
//   - _thread_locks: per-thread in-process locks
//   - _last_activity: tracks last use time for idle detection
//   - Cross-process file lock: prevents concurrent container creation
type DockerSandboxProvider struct {
	mu      sync.Mutex
	cfg     sandbox.SandboxConfig
	baseDir string // host root for thread volume directories
	client  DockerClient

	// Active sandboxes (in use by threads)
	sandboxes map[string]*DockerSandbox // sandbox_id -> sandbox

	// Warm pool: released sandboxes whose containers are still running
	// Maps sandbox_id -> release timestamp (for LRU eviction)
	warmPool map[string]time.Time

	// Thread mapping: tracks which sandbox belongs to which thread
	threadSandboxes map[string]string // thread_id -> sandbox_id

	// In-process locks per thread_id
	threadLocks map[string]*sync.Mutex

	// Last activity timestamp for idle detection
	lastActivity map[string]time.Time

	// Cross-process file lock for container creation
	cpLock *sandbox.CrossProcessFileLock

	// Replicas limit (max concurrent containers)
	replicas int

	// Idle timeout for automatic cleanup
	idleTimeout time.Duration

	// Background watchdog
	stopWatchdog context.CancelFunc
	watchdogWG   sync.WaitGroup

	// Shutdown flag
	shutdownCalled bool

	// newClient is an optional function to create a DockerClient (for testing)
	newClient func() (DockerClient, error)
}

// NewDockerSandboxProvider creates a DockerSandboxProvider with warm-pool support.
//
// Configuration options (from sandbox.DockerConfig):
//   - ContainerPrefix: container name prefix (default: "goclaw-sandbox-")
//   - ContainerTTL: idle timeout (default: 10 minutes, 0 to disable)
//   - Replicas: max concurrent containers (default: 3)
func NewDockerSandboxProvider(cfg sandbox.SandboxConfig, baseDir string) (*DockerSandboxProvider, error) {
	cli, err := newDefaultDockerClient()
	if err != nil {
		return nil, fmt.Errorf("create docker client: %w", err)
	}

	// Initialize cross-process file lock
	lockDir := filepath.Join(baseDir, ".locks")
	cpLock, err := sandbox.NewCrossProcessFileLock(lockDir)
	if err != nil {
		return nil, fmt.Errorf("create cross-process lock: %w", err)
	}

	// Parse configuration
	replicas := cfg.Docker.Replicas
	if replicas <= 0 {
		replicas = defaultReplicas
	}

	idleTimeout := cfg.Docker.ContainerTTL
	if idleTimeout <= 0 {
		idleTimeout = defaultContainerTTL
	}

	ctx, cancel := context.WithCancel(context.Background())
	p := &DockerSandboxProvider{
		cfg:             cfg,
		baseDir:         filepath.Clean(baseDir),
		client:          cli,
		sandboxes:       make(map[string]*DockerSandbox),
		warmPool:        make(map[string]time.Time),
		threadSandboxes: make(map[string]string),
		threadLocks:     make(map[string]*sync.Mutex),
		lastActivity:    make(map[string]time.Time),
		cpLock:          cpLock,
		replicas:        replicas,
		idleTimeout:     idleTimeout,
		stopWatchdog:    cancel,
		newClient:       newDefaultDockerClient,
	}

	// Start idle checker if enabled
	if idleTimeout > 0 {
		p.watchdogWG.Add(1)
		go p.runWatchdog(ctx)
	}

	return p, nil
}

// getThreadLock returns an in-process lock for the given thread_id.
func (p *DockerSandboxProvider) getThreadLock(threadID string) *sync.Mutex {
	p.mu.Lock()
	defer p.mu.Unlock()

	if lock, exists := p.threadLocks[threadID]; exists {
		return lock
	}

	lock := &sync.Mutex{}
	p.threadLocks[threadID] = lock
	return lock
}

// deterministicSandboxID generates a deterministic sandbox ID from a thread ID.
// This ensures all processes derive the same sandbox_id for a given thread,
// enabling cross-process sandbox discovery without shared memory.
func (p *DockerSandboxProvider) deterministicSandboxID(threadID string) string {
	// Use container name as deterministic ID
	return containerName(p.cfg, threadID)
}

// Acquire returns (or creates) a DockerSandbox for the given thread.
//
// For the same thread_id, this method will return the same sandbox_id
// across multiple turns, multiple processes.
//
// Thread-safe with both in-process and cross-process locking.
func (p *DockerSandboxProvider) Acquire(ctx context.Context, threadID string) (string, error) {
	if threadID == "" {
		return "", fmt.Errorf("thread_id is required")
	}

	threadLock := p.getThreadLock(threadID)
	threadLock.Lock()
	defer threadLock.Unlock()

	return p.acquireInternal(ctx, threadID)
}

// acquireInternal is the internal sandbox acquisition with two-layer consistency.
//
// Layer 1: In-process cache (fastest, covers same-process repeated access)
// Layer 2: Backend discovery (covers containers started by other processes)
// Layer 3: Cross-process lock + create
func (p *DockerSandboxProvider) acquireInternal(ctx context.Context, threadID string) (string, error) {
	sandboxID := p.deterministicSandboxID(threadID)

	// ── Layer 1: In-process cache (fast path) ──
	p.mu.Lock()
	if existingID, ok := p.threadSandboxes[threadID]; ok {
		if sb, exists := p.sandboxes[existingID]; exists {
			sb.mu.Lock()
			sb.lastUsed = time.Now()
			sb.mu.Unlock()
			p.lastActivity[existingID] = time.Now()
			p.mu.Unlock()
			return existingID, nil
		}
		// Stale mapping, clean it up
		delete(p.threadSandboxes, threadID)
	}

	// ── Layer 1.5: Warm pool (container still running, no cold-start) ──
	if releaseTime, inWarmPool := p.warmPool[sandboxID]; inWarmPool {
		delete(p.warmPool, sandboxID)
		threadBaseDir := filepath.Join(p.baseDir, "threads", threadID, "user-data")
		sb := &DockerSandbox{
			id:          sandboxID,
			threadID:    threadID,
			containerID: sandboxID,
			baseDir:     threadBaseDir,
			client:      p.client,
			cfg:         p.cfg,
			lastUsed:    time.Now(),
		}
		p.sandboxes[sandboxID] = sb
		p.threadSandboxes[threadID] = sandboxID
		p.lastActivity[sandboxID] = time.Now()
		p.mu.Unlock()

		_ = releaseTime // Used for logging if needed
		return sandboxID, nil
	}
	p.mu.Unlock()

	// ── Layer 2: Cross-process lock for container creation ──
	// Use file lock so that two processes racing to create the same sandbox
	// for the same thread_id serialize here.
	lockFilePath := filepath.Join(p.baseDir, "threads", threadID, sandboxID+".lock")
	if err := os.MkdirAll(filepath.Dir(lockFilePath), 0755); err != nil {
		return "", fmt.Errorf("create lock file directory: %w", err)
	}

	unlock, err := p.cpLock.Acquire(ctx, lockFilePath)
	if err != nil {
		return "", fmt.Errorf("acquire cross-process lock: %w", err)
	}
	defer unlock()

	// Re-check caches under the lock (another thread/process may have won the race)
	p.mu.Lock()
	if existingID, ok := p.threadSandboxes[threadID]; ok {
		if sb, exists := p.sandboxes[existingID]; exists {
			sb.mu.Lock()
			sb.lastUsed = time.Now()
			sb.mu.Unlock()
			p.lastActivity[existingID] = time.Now()
			p.mu.Unlock()
			return existingID, nil
		}
	}
	if _, inWarmPool := p.warmPool[sandboxID]; inWarmPool {
		delete(p.warmPool, sandboxID)
		threadBaseDir := filepath.Join(p.baseDir, "threads", threadID, "user-data")
		sb := &DockerSandbox{
			id:          sandboxID,
			threadID:    threadID,
			containerID: sandboxID,
			baseDir:     threadBaseDir,
			client:      p.client,
			cfg:         p.cfg,
			lastUsed:    time.Now(),
		}
		p.sandboxes[sandboxID] = sb
		p.threadSandboxes[threadID] = sandboxID
		p.lastActivity[sandboxID] = time.Now()
		p.mu.Unlock()
		return sandboxID, nil
	}
	p.mu.Unlock()

	// Container discovery: another process may have created the container
	inspect, err := p.client.ContainerInspect(ctx, sandboxID)
	if err == nil && inspect.State != nil && inspect.State.Running {
		// Container exists and is running - reclaim it
		threadBaseDir := filepath.Join(p.baseDir, "threads", threadID, "user-data")
		sb := &DockerSandbox{
			id:          sandboxID,
			threadID:    threadID,
			containerID: inspect.ID,
			baseDir:     threadBaseDir,
			client:      p.client,
			cfg:         p.cfg,
			lastUsed:    time.Now(),
		}
		p.mu.Lock()
		p.sandboxes[sandboxID] = sb
		p.threadSandboxes[threadID] = sandboxID
		p.lastActivity[sandboxID] = time.Now()
		p.mu.Unlock()
		return sandboxID, nil
	}

	// Create new sandbox
	return p.createSandbox(ctx, threadID, sandboxID)
}

// createSandbox creates a new Docker container for the sandbox.
//
// Enforces replicas limit by evicting from warm pool if needed.
func (p *DockerSandboxProvider) createSandbox(ctx context.Context, threadID, sandboxID string) (string, error) {
	// Enforce replicas: only warm-pool containers count toward eviction budget.
	// Active sandboxes are in use by live threads and must not be forcibly stopped.
	p.mu.Lock()
	total := len(p.sandboxes) + len(p.warmPool)
	p.mu.Unlock()

	if total >= p.replicas {
		evicted := p.evictOldestWarm()
		if evicted != "" {
			// Log eviction
			_ = evicted
		} else {
			// All slots are occupied by active sandboxes — proceed anyway and log.
			// The replicas limit is a soft cap; we never forcibly stop a container
			// that is actively serving a thread.
		}
	}

	// Create volume directories
	threadBaseDir := filepath.Join(p.baseDir, "threads", threadID, "user-data")
	for _, sub := range []string{"workspace", "uploads", "outputs"} {
		if err := os.MkdirAll(filepath.Join(threadBaseDir, sub), 0755); err != nil {
			return "", fmt.Errorf("create sandbox dir %s: %w", sub, err)
		}
	}

	// Create sandbox instance (container is created lazily on first use)
	sb := &DockerSandbox{
		id:          sandboxID,
		threadID:    threadID,
		containerID: sandboxID,
		baseDir:     threadBaseDir,
		client:      p.client,
		cfg:         p.cfg,
		lastUsed:    time.Now(),
	}

	p.mu.Lock()
	p.sandboxes[sandboxID] = sb
	p.threadSandboxes[threadID] = sandboxID
	p.lastActivity[sandboxID] = time.Now()
	p.mu.Unlock()

	return sandboxID, nil
}

// evictOldestWarm destroys the oldest container in the warm pool to free capacity.
//
// Returns the evicted sandbox_id, or empty string if warm pool is empty.
func (p *DockerSandboxProvider) evictOldestWarm() string {
	p.mu.Lock()
	if len(p.warmPool) == 0 {
		p.mu.Unlock()
		return ""
	}

	// Find oldest entry
	var oldestID string
	var oldestTime time.Time
	for id, t := range p.warmPool {
		if oldestID == "" || t.Before(oldestTime) {
			oldestID = id
			oldestTime = t
		}
	}
	delete(p.warmPool, oldestID)
	p.mu.Unlock()

	// Destroy the container
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := p.destroySandbox(ctx, oldestID); err != nil {
		// Log error but don't fail
		_ = err
		return ""
	}

	return oldestID
}

// Get retrieves a sandbox by ID. Updates last activity timestamp.
func (p *DockerSandboxProvider) Get(sandboxID string) sandbox.Sandbox {
	p.mu.Lock()
	defer p.mu.Unlock()

	if sb, exists := p.sandboxes[sandboxID]; exists {
		p.lastActivity[sandboxID] = time.Now()
		sb.mu.Lock()
		sb.lastUsed = time.Now()
		sb.mu.Unlock()
		return sb
	}
	return nil
}

// Release moves the sandbox to the warm pool instead of destroying it.
//
// The container keeps running for quick reuse, avoiding cold-start latency.
// This mirrors DeerFlow's release() behavior.
func (p *DockerSandboxProvider) Release(ctx context.Context, sandboxID string) error {
	p.mu.Lock()

	// Check if sandbox exists in active pool
	_, inActive := p.sandboxes[sandboxID]
	if !inActive {
		p.mu.Unlock()
		return nil
	}

	// Remove from active pool
	delete(p.sandboxes, sandboxID)

	// Remove thread mapping(s) for this sandbox
	var threadsToRemove []string
	for tid, sid := range p.threadSandboxes {
		if sid == sandboxID {
			threadsToRemove = append(threadsToRemove, tid)
		}
	}
	for _, tid := range threadsToRemove {
		delete(p.threadSandboxes, tid)
	}

	// Remove from last activity tracking
	delete(p.lastActivity, sandboxID)

	// Move to warm pool - container keeps running
	if _, exists := p.warmPool[sandboxID]; !exists {
		p.warmPool[sandboxID] = time.Now()
	}
	p.mu.Unlock()

	return nil
}

// Destroy stops and removes the container for the given sandbox ID.
//
// This is the forceful cleanup method used for eviction and shutdown.
func (p *DockerSandboxProvider) Destroy(ctx context.Context, sandboxID string) error {
	return p.destroySandbox(ctx, sandboxID)
}

// destroySandbox is the internal implementation of sandbox destruction.
func (p *DockerSandboxProvider) destroySandbox(ctx context.Context, sandboxID string) error {
	p.mu.Lock()

	// Remove from active pool if present
	sb, inActive := p.sandboxes[sandboxID]
	if inActive {
		delete(p.sandboxes, sandboxID)
		var threadsToRemove []string
		for tid, sid := range p.threadSandboxes {
			if sid == sandboxID {
				threadsToRemove = append(threadsToRemove, tid)
			}
		}
		for _, tid := range threadsToRemove {
			delete(p.threadSandboxes, tid)
		}
	}

	// Remove from warm pool if present
	_, inWarm := p.warmPool[sandboxID]
	if inWarm {
		delete(p.warmPool, sandboxID)
	}

	// Remove from last activity tracking
	delete(p.lastActivity, sandboxID)

	p.mu.Unlock()

	if !inActive && !inWarm {
		return nil
	}

	// Stop and remove the container
	containerID := sandboxID
	if sb != nil {
		containerID = sb.containerID
	}

	stopTimeout := int(containerStopTimeout.Seconds())
	if err := p.client.ContainerStop(ctx, containerID, container.StopOptions{Timeout: &stopTimeout}); err != nil {
		// Log but don't fail – container might already be gone
		_ = err
	}

	if err := p.client.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true}); err != nil {
		return fmt.Errorf("remove container %q: %w", sandboxID, err)
	}

	return nil
}

// Shutdown tears down all active sandboxes and warm pool containers.
//
// Thread-safe and idempotent.
func (p *DockerSandboxProvider) Shutdown(ctx context.Context) error {
	p.mu.Lock()
	if p.shutdownCalled {
		p.mu.Unlock()
		return nil
	}
	p.shutdownCalled = true

	// Collect IDs to destroy
	activeIDs := make([]string, 0, len(p.sandboxes))
	for id := range p.sandboxes {
		activeIDs = append(activeIDs, id)
	}
	warmIDs := make([]string, 0, len(p.warmPool))
	for id := range p.warmPool {
		warmIDs = append(warmIDs, id)
	}

	// Clear pools
	p.sandboxes = make(map[string]*DockerSandbox)
	p.warmPool = make(map[string]time.Time)
	p.threadSandboxes = make(map[string]string)
	p.lastActivity = make(map[string]time.Time)
	p.mu.Unlock()

	// Stop watchdog
	if p.stopWatchdog != nil {
		p.stopWatchdog()
		p.watchdogWG.Wait()
	}

	var firstErr error

	// Destroy active sandboxes
	for _, id := range activeIDs {
		if err := p.destroySandbox(ctx, id); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	// Destroy warm pool sandboxes
	for _, id := range warmIDs {
		if err := p.destroySandbox(ctx, id); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	_ = p.client.Close()
	return firstErr
}

// runWatchdog periodically evicts containers that have been idle longer than idleTimeout.
func (p *DockerSandboxProvider) runWatchdog(ctx context.Context) {
	defer p.watchdogWG.Done()

	ticker := time.NewTicker(idleCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.cleanupIdleSandboxes(ctx)
		}
	}
}

// cleanupIdleSandboxes destroys sandboxes that have exceeded the idle timeout.
func (p *DockerSandboxProvider) cleanupIdleSandboxes(ctx context.Context) {
	now := time.Now()

	// Check warm pool first (easiest to evict)
	p.mu.Lock()
	var warmEvict []string
	for id, releaseTime := range p.warmPool {
		if now.Sub(releaseTime) > p.idleTimeout {
			warmEvict = append(warmEvict, id)
			delete(p.warmPool, id)
		}
	}
	p.mu.Unlock()

	for _, id := range warmEvict {
		_ = p.destroySandbox(ctx, id)
	}

	// Check active pool
	p.mu.Lock()
	var activeEvict []string
	for id, lastUsed := range p.lastActivity {
		if now.Sub(lastUsed) > p.idleTimeout {
			// Only evict if still in active pool (not warm)
			if _, inActive := p.sandboxes[id]; inActive {
				activeEvict = append(activeEvict, id)
			}
		}
	}
	p.mu.Unlock()

	for _, id := range activeEvict {
		_ = p.destroySandbox(ctx, id)
	}
}

// newDefaultDockerClient creates a new real Docker client
func newDefaultDockerClient() (DockerClient, error) {
	return defaultNewDockerClient()
}

// Ensure DockerSandboxProvider satisfies sandbox.SandboxProvider at compile time.
var _ sandbox.SandboxProvider = (*DockerSandboxProvider)(nil)
