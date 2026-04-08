// Package docker provides Docker client interface for sandbox operations.
// This file defines the DockerClient interface that abstracts Docker API calls
// to enable mock-based testing without requiring a real Docker daemon.
package docker

import (
	"context"
	"io"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
)

// DockerClient defines the interface for Docker API operations used by DockerSandbox.
// This interface abstracts the docker client to enable mock-based unit testing.
type DockerClient interface {
	// ContainerCreate creates a new container with the given configuration
	ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *v1.Platform, containerName string) (container.CreateResponse, error)

	// ContainerStart starts a container
	ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error

	// ContainerStop stops a container
	ContainerStop(ctx context.Context, containerID string, options container.StopOptions) error

	// ContainerRemove removes a container
	ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error

	// ContainerInspect returns detailed information about a container
	ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error)

	// ContainerExecCreate creates a new exec configuration to run an exec process
	ContainerExecCreate(ctx context.Context, container string, config types.ExecConfig) (types.IDResponse, error)

	// ContainerExecAttach attaches to an exec command already running in a container
	ContainerExecAttach(ctx context.Context, execID string, config types.ExecStartCheck) (types.HijackedResponse, error)

	// ContainerExecInspect returns information about a specific exec process
	ContainerExecInspect(ctx context.Context, execID string) (types.ContainerExecInspect, error)

	// Close closes the Docker client connection
	Close() error

	// IsErrNotFound returns true if the error is a "not found" error
	IsErrNotFound(err error) bool
}

// dockerClientWrapper wraps the real docker client to implement DockerClient interface
type dockerClientWrapper struct {
	client interface {
		ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *v1.Platform, containerName string) (container.CreateResponse, error)
		ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error
		ContainerStop(ctx context.Context, containerID string, options container.StopOptions) error
		ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error
		ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error)
		ContainerExecCreate(ctx context.Context, container string, config types.ExecConfig) (types.IDResponse, error)
		ContainerExecAttach(ctx context.Context, execID string, config types.ExecStartCheck) (types.HijackedResponse, error)
		ContainerExecInspect(ctx context.Context, execID string) (types.ContainerExecInspect, error)
		Close() error
	}
	isErrNotFound func(err error) bool
}

func (w *dockerClientWrapper) ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *v1.Platform, containerName string) (container.CreateResponse, error) {
	return w.client.ContainerCreate(ctx, config, hostConfig, networkingConfig, platform, containerName)
}

func (w *dockerClientWrapper) ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error {
	return w.client.ContainerStart(ctx, containerID, options)
}

func (w *dockerClientWrapper) ContainerStop(ctx context.Context, containerID string, options container.StopOptions) error {
	return w.client.ContainerStop(ctx, containerID, options)
}

func (w *dockerClientWrapper) ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error {
	return w.client.ContainerRemove(ctx, containerID, options)
}

func (w *dockerClientWrapper) ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error) {
	return w.client.ContainerInspect(ctx, containerID)
}

func (w *dockerClientWrapper) ContainerExecCreate(ctx context.Context, container string, config types.ExecConfig) (types.IDResponse, error) {
	return w.client.ContainerExecCreate(ctx, container, config)
}

func (w *dockerClientWrapper) ContainerExecAttach(ctx context.Context, execID string, config types.ExecStartCheck) (types.HijackedResponse, error) {
	return w.client.ContainerExecAttach(ctx, execID, config)
}

func (w *dockerClientWrapper) ContainerExecInspect(ctx context.Context, execID string) (types.ContainerExecInspect, error) {
	return w.client.ContainerExecInspect(ctx, execID)
}

func (w *dockerClientWrapper) Close() error {
	return w.client.Close()
}

func (w *dockerClientWrapper) IsErrNotFound(err error) bool {
	if w.isErrNotFound != nil {
		return w.isErrNotFound(err)
	}
	return false
}

// mockHijackedResponse implements io.ReadCloser for mocking HijackedResponse
type mockHijackedResponse struct {
	reader io.ReadCloser
}

func (m *mockHijackedResponse) Read(p []byte) (n int, err error) {
	return m.reader.Read(p)
}

func (m *mockHijackedResponse) Close() error {
	return m.reader.Close()
}

// Ensure dockerClientWrapper implements DockerClient
var _ DockerClient = (*dockerClientWrapper)(nil)
