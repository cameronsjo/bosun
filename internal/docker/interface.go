// Package docker provides Docker SDK client and operations.
package docker

import (
	"context"

	"github.com/moby/moby/client"
)

// DockerAPI defines the interface for Docker client operations.
// This interface enables mocking for unit tests without requiring a running Docker daemon.
//
// The method set mirrors the moby split-module client (github.com/moby/moby/client)
// so that *client.Client satisfies it directly. Every method takes an options struct
// and returns a *Result wrapper, matching the moby client conventions.
type DockerAPI interface {
	// Ping tests the connection to the Docker daemon.
	Ping(ctx context.Context, options client.PingOptions) (client.PingResult, error)

	// ContainerList returns a list of containers.
	ContainerList(ctx context.Context, options client.ContainerListOptions) (client.ContainerListResult, error)

	// ContainerInspect returns detailed information about a container.
	ContainerInspect(ctx context.Context, containerID string, options client.ContainerInspectOptions) (client.ContainerInspectResult, error)

	// ContainerLogs returns logs from a container.
	ContainerLogs(ctx context.Context, containerID string, options client.ContainerLogsOptions) (client.ContainerLogsResult, error)

	// ContainerStart starts a stopped container.
	ContainerStart(ctx context.Context, containerID string, options client.ContainerStartOptions) (client.ContainerStartResult, error)

	// ContainerStop stops a container with a grace period.
	ContainerStop(ctx context.Context, containerID string, options client.ContainerStopOptions) (client.ContainerStopResult, error)

	// ContainerRestart restarts a container.
	ContainerRestart(ctx context.Context, containerID string, options client.ContainerRestartOptions) (client.ContainerRestartResult, error)

	// ContainerRemove removes a container.
	ContainerRemove(ctx context.Context, containerID string, options client.ContainerRemoveOptions) (client.ContainerRemoveResult, error)

	// ContainerStats returns container resource usage statistics.
	ContainerStats(ctx context.Context, containerID string, options client.ContainerStatsOptions) (client.ContainerStatsResult, error)

	// ExecCreate creates an exec instance in a container.
	ExecCreate(ctx context.Context, containerID string, options client.ExecCreateOptions) (client.ExecCreateResult, error)

	// ExecAttach attaches to an exec instance and returns a connection.
	ExecAttach(ctx context.Context, execID string, options client.ExecAttachOptions) (client.ExecAttachResult, error)

	// ExecInspect returns information about an exec instance.
	ExecInspect(ctx context.Context, execID string, options client.ExecInspectOptions) (client.ExecInspectResult, error)

	// DiskUsage returns Docker system disk usage information.
	DiskUsage(ctx context.Context, options client.DiskUsageOptions) (client.DiskUsageResult, error)

	// Info returns system-wide information about the Docker daemon.
	Info(ctx context.Context, options client.InfoOptions) (client.SystemInfoResult, error)

	// Close closes the client connection.
	Close() error
}

// Verify that the moby client satisfies our interface at compile time.
// This keeps DockerAPI in sync with the upstream client signatures.
var _ DockerAPI = (*client.Client)(nil)

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
