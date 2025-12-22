// Package docker provides Docker SDK client and operations.
package docker

import (
	"context"
	"io"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/system"
)

// DockerAPI defines the interface for Docker client operations.
// This interface enables mocking for unit tests without requiring a running Docker daemon.
type DockerAPI interface {
	// Ping tests the connection to the Docker daemon.
	Ping(ctx context.Context) (types.Ping, error)

	// ContainerList returns a list of containers.
	ContainerList(ctx context.Context, options container.ListOptions) ([]types.Container, error)

	// ContainerInspect returns detailed information about a container.
	ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error)

	// ContainerLogs returns logs from a container.
	ContainerLogs(ctx context.Context, container string, options container.LogsOptions) (io.ReadCloser, error)

	// ContainerStart starts a stopped container.
	ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error

	// ContainerRestart restarts a container.
	ContainerRestart(ctx context.Context, containerID string, options container.StopOptions) error

	// ContainerRemove removes a container.
	ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error

	// ContainerStats returns container resource usage statistics.
	ContainerStats(ctx context.Context, containerID string, stream bool) (container.StatsResponseReader, error)

	// DiskUsage returns Docker system disk usage information.
	DiskUsage(ctx context.Context, options types.DiskUsageOptions) (types.DiskUsage, error)

	// Info returns system-wide information about the Docker daemon.
	Info(ctx context.Context) (system.Info, error)

	// Close closes the client connection.
	Close() error
}

// Verify that the Docker SDK client implements our interface.
// This ensures compile-time verification that our interface stays in sync.
var _ DockerAPI = (dockerAPIAdapter)(nil)

// dockerAPIAdapter adapts the Docker SDK client to our interface.
// The SDK client methods have the same signatures, so this is a type alias.
type dockerAPIAdapter interface {
	Ping(ctx context.Context) (types.Ping, error)
	ContainerList(ctx context.Context, options container.ListOptions) ([]types.Container, error)
	ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error)
	ContainerLogs(ctx context.Context, container string, options container.LogsOptions) (io.ReadCloser, error)
	ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error
	ContainerRestart(ctx context.Context, containerID string, options container.StopOptions) error
	ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error
	ContainerStats(ctx context.Context, containerID string, stream bool) (container.StatsResponseReader, error)
	DiskUsage(ctx context.Context, options types.DiskUsageOptions) (types.DiskUsage, error)
	Info(ctx context.Context) (system.Info, error)
	Close() error
}

// TestableClient is a variant of Client that accepts a DockerAPI interface.
// This allows injecting mocks for testing.
type TestableClient struct {
	api DockerAPI
}

// NewTestableClient creates a new testable Docker client with the given API implementation.
func NewTestableClient(api DockerAPI) *TestableClient {
	return &TestableClient{api: api}
}

// API returns the underlying Docker API for advanced operations.
func (c *TestableClient) API() DockerAPI {
	return c.api
}

// Close closes the Docker client connection.
func (c *TestableClient) Close() error {
	if c.api != nil {
		return c.api.Close()
	}
	return nil
}
