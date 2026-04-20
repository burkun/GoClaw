// Package docker provides mock implementations for testing.
package docker

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
)

// MockDockerClient is a mock implementation of DockerClient for testing
type MockDockerClient struct {
	mu sync.RWMutex

	// Containers maps containerID -> container info
	Containers map[string]*MockContainer

	// ExecSessions maps execID -> exec info
	ExecSessions map[string]*MockExecSession

	// Call tracking
	ContainerCreateCalls      []ContainerCreateCall
	ContainerStartCalls       []string
	ContainerStopCalls        []string
	ContainerRemoveCalls      []string
	ContainerInspectCalls     []string
	ContainerExecCreateCalls  []ContainerExecCreateCall
	ContainerExecAttachCalls  []string
	ContainerExecInspectCalls []string

	// Error simulation
	ErrContainerCreate      error
	ErrContainerStart       error
	ErrContainerStop        error
	ErrContainerRemove      error
	ErrContainerInspect     error
	ErrContainerExecCreate  error
	ErrContainerExecAttach  error
	ErrContainerExecInspect error

	// ID counters for generating unique IDs
	containerIDCounter int
	execIDCounter      int

	// Hooks for test customization
	OnContainerInspect func(containerID string)
}

// MockContainer represents a mock container
type MockContainer struct {
	ID         string
	Name       string
	Config     *container.Config
	HostConfig *container.HostConfig
	Running    bool
	Created    time.Time
	State      *types.ContainerState
}

// MockExecSession represents a mock exec session
type MockExecSession struct {
	ID          string
	ContainerID string
	Config      types.ExecConfig
	ExitCode    int
	Stdout      string
	Stderr      string
	Running     bool
}

// ContainerCreateCall records a ContainerCreate call
type ContainerCreateCall struct {
	Config     *container.Config
	HostConfig *container.HostConfig
	Name       string
}

// ContainerExecCreateCall records a ContainerExecCreate call
type ContainerExecCreateCall struct {
	Container string
	Config    types.ExecConfig
}

// NewMockDockerClient creates a new mock Docker client
func NewMockDockerClient() *MockDockerClient {
	return &MockDockerClient{
		Containers:                make(map[string]*MockContainer),
		ExecSessions:              make(map[string]*MockExecSession),
		ContainerCreateCalls:      make([]ContainerCreateCall, 0),
		ContainerStartCalls:       make([]string, 0),
		ContainerStopCalls:        make([]string, 0),
		ContainerRemoveCalls:      make([]string, 0),
		ContainerInspectCalls:     make([]string, 0),
		ContainerExecCreateCalls:  make([]ContainerExecCreateCall, 0),
		ContainerExecAttachCalls:  make([]string, 0),
		ContainerExecInspectCalls: make([]string, 0),
	}
}

// ContainerCreate creates a new mock container
func (m *MockDockerClient) ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *v1.Platform, containerName string) (container.CreateResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.ErrContainerCreate != nil {
		return container.CreateResponse{}, m.ErrContainerCreate
	}

	m.containerIDCounter++
	id := containerName
	if id == "" {
		id = generateContainerID(m.containerIDCounter)
	}

	m.Containers[id] = &MockContainer{
		ID:         id,
		Name:       containerName,
		Config:     config,
		HostConfig: hostConfig,
		Running:    false,
		Created:    time.Now(),
		State: &types.ContainerState{
			Running: false,
			Status:  "created",
		},
	}

	m.ContainerCreateCalls = append(m.ContainerCreateCalls, ContainerCreateCall{
		Config:     config,
		HostConfig: hostConfig,
		Name:       containerName,
	})

	return container.CreateResponse{ID: id}, nil
}

// ContainerStart starts a mock container
func (m *MockDockerClient) ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.ErrContainerStart != nil {
		return m.ErrContainerStart
	}

	if container, exists := m.Containers[containerID]; exists {
		container.Running = true
		container.State.Running = true
		container.State.Status = "running"
	} else {
		return errors.New("container not found: " + containerID)
	}

	m.ContainerStartCalls = append(m.ContainerStartCalls, containerID)
	return nil
}

// ContainerStop stops a mock container
func (m *MockDockerClient) ContainerStop(ctx context.Context, containerID string, options container.StopOptions) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.ErrContainerStop != nil {
		return m.ErrContainerStop
	}

	if container, exists := m.Containers[containerID]; exists {
		container.Running = false
		container.State.Running = false
		container.State.Status = "exited"
	}

	m.ContainerStopCalls = append(m.ContainerStopCalls, containerID)
	return nil
}

// ContainerRemove removes a mock container
func (m *MockDockerClient) ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.ErrContainerRemove != nil {
		return m.ErrContainerRemove
	}

	delete(m.Containers, containerID)
	m.ContainerRemoveCalls = append(m.ContainerRemoveCalls, containerID)
	return nil
}

// ContainerInspect returns container info
func (m *MockDockerClient) ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.ErrContainerInspect != nil {
		return types.ContainerJSON{}, m.ErrContainerInspect
	}

	// Call hook if set
	if m.OnContainerInspect != nil {
		m.mu.Unlock()
		m.OnContainerInspect(containerID)
		m.mu.Lock()
	}

	if mc, exists := m.Containers[containerID]; exists {
		m.ContainerInspectCalls = append(m.ContainerInspectCalls, containerID)

		// Build ContainerJSON with proper embedded struct initialization
		base := &types.ContainerJSONBase{
			ID:   mc.ID,
			Name: "/" + mc.Name,
			State: &types.ContainerState{
				Running: mc.Running,
				Status:  mc.State.Status,
			},
			HostConfig: &container.HostConfig{},
		}

		containerJSON := types.ContainerJSON{
			ContainerJSONBase: base,
		}

		if mc.Config != nil {
			containerJSON.Config = mc.Config
		}

		return containerJSON, nil
	}

	return types.ContainerJSON{}, errors.New("container not found: " + containerID)
}

// ContainerExecCreate creates a mock exec session
func (m *MockDockerClient) ContainerExecCreate(ctx context.Context, container string, config types.ExecConfig) (types.IDResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.ErrContainerExecCreate != nil {
		return types.IDResponse{}, m.ErrContainerExecCreate
	}

	if _, exists := m.Containers[container]; !exists {
		return types.IDResponse{}, errors.New("container not found: " + container)
	}

	m.execIDCounter++
	execID := generateExecID(m.execIDCounter)

	m.ExecSessions[execID] = &MockExecSession{
		ID:          execID,
		ContainerID: container,
		Config:      config,
		ExitCode:    0,
		Running:     false,
	}

	m.ContainerExecCreateCalls = append(m.ContainerExecCreateCalls, ContainerExecCreateCall{
		Container: container,
		Config:    config,
	})

	return types.IDResponse{ID: execID}, nil
}

// ContainerExecAttach attaches to a mock exec session
func (m *MockDockerClient) ContainerExecAttach(ctx context.Context, execID string, config types.ExecStartCheck) (types.HijackedResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.ErrContainerExecAttach != nil {
		return types.HijackedResponse{}, m.ErrContainerExecAttach
	}

	session, exists := m.ExecSessions[execID]
	if !exists {
		return types.HijackedResponse{}, errors.New("exec session not found: " + execID)
	}

	session.Running = true
	m.ContainerExecAttachCalls = append(m.ContainerExecAttachCalls, execID)

	// Create a hijacked response with the mock output
	// The mock output is piped through to simulate exec output
	pr, pw := io.Pipe()
	go func() {
		pw.Write([]byte(session.Stdout))
		pw.Close()
	}()

	hijacked := types.NewHijackedResponse(&mockConn{}, "application/vnd.docker.raw-stream")
	hijacked.Reader = bufio.NewReader(pr)
	return hijacked, nil
}

// ContainerExecInspect returns exec session info
func (m *MockDockerClient) ContainerExecInspect(ctx context.Context, execID string) (types.ContainerExecInspect, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.ErrContainerExecInspect != nil {
		return types.ContainerExecInspect{}, m.ErrContainerExecInspect
	}

	session, exists := m.ExecSessions[execID]
	if !exists {
		return types.ContainerExecInspect{}, errors.New("exec session not found: " + execID)
	}

	session.Running = false
	m.ContainerExecInspectCalls = append(m.ContainerExecInspectCalls, execID)

	return types.ContainerExecInspect{
		ExecID:      execID,
		ContainerID: session.ContainerID,
		Running:     false,
		ExitCode:    session.ExitCode,
	}, nil
}

// Close closes the mock client
func (m *MockDockerClient) Close() error {
	return nil
}

// IsErrNotFound returns true if the error is a "not found" error
func (m *MockDockerClient) IsErrNotFound(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "not found")
}

// SetExecOutput sets the output for an exec session
func (m *MockDockerClient) SetExecOutput(execID string, stdout, stderr string, exitCode int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if session, exists := m.ExecSessions[execID]; exists {
		session.Stdout = stdout
		session.Stderr = stderr
		session.ExitCode = exitCode
	}
}

// SetContainerRunning sets a container as running (useful for setup)
func (m *MockDockerClient) SetContainerRunning(containerID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if container, exists := m.Containers[containerID]; exists {
		container.Running = true
		container.State.Running = true
		container.State.Status = "running"
	}
}

// AddMockContainer adds a pre-configured mock container
func (m *MockDockerClient) AddMockContainer(id string, running bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	status := "created"
	if running {
		status = "running"
	}

	m.Containers[id] = &MockContainer{
		ID:      id,
		Name:    id,
		Running: running,
		Created: time.Now(),
		State: &types.ContainerState{
			Running: running,
			Status:  status,
		},
	}
}

// Reset clears all state (useful between tests)
func (m *MockDockerClient) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Containers = make(map[string]*MockContainer)
	m.ExecSessions = make(map[string]*MockExecSession)
	m.ContainerCreateCalls = make([]ContainerCreateCall, 0)
	m.ContainerStartCalls = make([]string, 0)
	m.ContainerStopCalls = make([]string, 0)
	m.ContainerRemoveCalls = make([]string, 0)
	m.ContainerInspectCalls = make([]string, 0)
	m.ContainerExecCreateCalls = make([]ContainerExecCreateCall, 0)
	m.ContainerExecAttachCalls = make([]string, 0)
	m.ContainerExecInspectCalls = make([]string, 0)
	m.containerIDCounter = 0
	m.execIDCounter = 0

	// Reset errors
	m.ErrContainerCreate = nil
	m.ErrContainerStart = nil
	m.ErrContainerStop = nil
	m.ErrContainerRemove = nil
	m.ErrContainerInspect = nil
	m.ErrContainerExecCreate = nil
	m.ErrContainerExecAttach = nil
	m.ErrContainerExecInspect = nil
}

// mockExecReader implements io.ReadCloser for exec output
type mockExecReader struct {
	stdout   string
	stderr   string
	readPos  int
	finished bool
}

func (r *mockExecReader) Read(p []byte) (n int, err error) {
	if r.finished {
		return 0, io.EOF
	}

	// Simple implementation: return stdout then EOF
	// In real Docker, this uses stdcopy protocol with headers
	if r.readPos < len(r.stdout) {
		n = copy(p, r.stdout[r.readPos:])
		r.readPos += n
		return n, nil
	}

	r.finished = true
	return 0, io.EOF
}

func (r *mockExecReader) Close() error {
	return nil
}

// mockAddr implements net.Addr for testing
type mockAddr struct{}

func (a mockAddr) Network() string { return "tcp" }
func (a mockAddr) String() string  { return "127.0.0.1:0" }

// mockConn implements net.Conn for testing
type mockConn struct{}

func (c *mockConn) Read(b []byte) (n int, err error)   { return 0, io.EOF }
func (c *mockConn) Write(b []byte) (n int, err error)  { return len(b), nil }
func (c *mockConn) Close() error                       { return nil }
func (c *mockConn) LocalAddr() net.Addr                { return mockAddr{} }
func (c *mockConn) RemoteAddr() net.Addr               { return mockAddr{} }
func (c *mockConn) SetDeadline(t time.Time) error      { return nil }
func (c *mockConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *mockConn) SetWriteDeadline(t time.Time) error { return nil }

// Helper functions
func generateContainerID(counter int) string {
	return "mock-container-" + string(rune('a'+counter%26)) + string(rune('0'+counter%10))
}

func generateExecID(counter int) string {
	return "mock-exec-" + string(rune('a'+counter%26)) + string(rune('0'+counter%10))
}

// Ensure MockDockerClient implements DockerClient
var _ DockerClient = (*MockDockerClient)(nil)
