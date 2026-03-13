// Package dockertest provides test utilities for the docker package.
// It exports a mock DockerAPI implementation usable from any test package.
package dockertest

import (
	"bytes"
	"context"
	"encoding/json"
	"io"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/system"

	"github.com/cameronsjo/bosun/internal/docker"
)

// MockDockerAPI is a mock implementation of docker.DockerAPI for testing.
// Each method has a function override and a call counter.
type MockDockerAPI struct {
	// Function overrides for each method.
	PingFunc             func(ctx context.Context) (types.Ping, error)
	ContainerListFunc    func(ctx context.Context, options container.ListOptions) ([]container.Summary, error)
	ContainerInspectFunc func(ctx context.Context, containerID string) (container.InspectResponse, error)
	ContainerLogsFunc    func(ctx context.Context, ctr string, options container.LogsOptions) (io.ReadCloser, error)
	ContainerStartFunc   func(ctx context.Context, containerID string, options container.StartOptions) error
	ContainerStopFunc    func(ctx context.Context, containerID string, options container.StopOptions) error
	ContainerRestartFunc func(ctx context.Context, containerID string, options container.StopOptions) error
	ContainerRemoveFunc  func(ctx context.Context, containerID string, options container.RemoveOptions) error
	ContainerStatsFunc   func(ctx context.Context, containerID string, stream bool) (container.StatsResponseReader, error)
	DiskUsageFunc        func(ctx context.Context, options types.DiskUsageOptions) (types.DiskUsage, error)
	InfoFunc             func(ctx context.Context) (system.Info, error)
	CloseFunc            func() error

	// Call tracking.
	PingCalls             int
	ContainerListCalls    int
	ContainerInspectCalls int
	ContainerLogsCalls    int
	ContainerStartCalls   int
	ContainerStopCalls    int
	ContainerRestartCalls int
	ContainerRemoveCalls  int
	ContainerStatsCalls   int
	DiskUsageCalls        int
	InfoCalls             int
	CloseCalls            int
}

// NewMockDockerAPI creates a new mock with default no-op implementations.
func NewMockDockerAPI() *MockDockerAPI {
	return &MockDockerAPI{}
}

// Ping implements docker.DockerAPI.
func (m *MockDockerAPI) Ping(ctx context.Context) (types.Ping, error) {
	m.PingCalls++
	if m.PingFunc != nil {
		return m.PingFunc(ctx)
	}
	return types.Ping{APIVersion: "1.45"}, nil
}

// ContainerList implements docker.DockerAPI.
func (m *MockDockerAPI) ContainerList(ctx context.Context, options container.ListOptions) ([]container.Summary, error) {
	m.ContainerListCalls++
	if m.ContainerListFunc != nil {
		return m.ContainerListFunc(ctx, options)
	}
	return []container.Summary{}, nil
}

// ContainerInspect implements docker.DockerAPI.
func (m *MockDockerAPI) ContainerInspect(ctx context.Context, containerID string) (container.InspectResponse, error) {
	m.ContainerInspectCalls++
	if m.ContainerInspectFunc != nil {
		return m.ContainerInspectFunc(ctx, containerID)
	}
	return container.InspectResponse{}, nil
}

// ContainerLogs implements docker.DockerAPI.
func (m *MockDockerAPI) ContainerLogs(ctx context.Context, containerName string, options container.LogsOptions) (io.ReadCloser, error) {
	m.ContainerLogsCalls++
	if m.ContainerLogsFunc != nil {
		return m.ContainerLogsFunc(ctx, containerName, options)
	}
	return io.NopCloser(bytes.NewReader([]byte{})), nil
}

// ContainerStart implements docker.DockerAPI.
func (m *MockDockerAPI) ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error {
	m.ContainerStartCalls++
	if m.ContainerStartFunc != nil {
		return m.ContainerStartFunc(ctx, containerID, options)
	}
	return nil
}

// ContainerStop implements docker.DockerAPI.
func (m *MockDockerAPI) ContainerStop(ctx context.Context, containerID string, options container.StopOptions) error {
	m.ContainerStopCalls++
	if m.ContainerStopFunc != nil {
		return m.ContainerStopFunc(ctx, containerID, options)
	}
	return nil
}

// ContainerRestart implements docker.DockerAPI.
func (m *MockDockerAPI) ContainerRestart(ctx context.Context, containerID string, options container.StopOptions) error {
	m.ContainerRestartCalls++
	if m.ContainerRestartFunc != nil {
		return m.ContainerRestartFunc(ctx, containerID, options)
	}
	return nil
}

// ContainerRemove implements docker.DockerAPI.
func (m *MockDockerAPI) ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error {
	m.ContainerRemoveCalls++
	if m.ContainerRemoveFunc != nil {
		return m.ContainerRemoveFunc(ctx, containerID, options)
	}
	return nil
}

// ContainerStats implements docker.DockerAPI.
func (m *MockDockerAPI) ContainerStats(ctx context.Context, containerID string, stream bool) (container.StatsResponseReader, error) {
	m.ContainerStatsCalls++
	if m.ContainerStatsFunc != nil {
		return m.ContainerStatsFunc(ctx, containerID, stream)
	}
	// Return empty stats JSON.
	data, _ := json.Marshal(map[string]any{
		"cpu_stats":    map[string]any{"cpu_usage": map[string]any{"total_usage": 0}, "system_cpu_usage": 0},
		"precpu_stats": map[string]any{"cpu_usage": map[string]any{"total_usage": 0}, "system_cpu_usage": 0},
		"memory_stats": map[string]any{"usage": 0, "limit": 0},
	})
	return container.StatsResponseReader{
		Body: io.NopCloser(bytes.NewReader(data)),
	}, nil
}

// DiskUsage implements docker.DockerAPI.
func (m *MockDockerAPI) DiskUsage(ctx context.Context, options types.DiskUsageOptions) (types.DiskUsage, error) {
	m.DiskUsageCalls++
	if m.DiskUsageFunc != nil {
		return m.DiskUsageFunc(ctx, options)
	}
	return types.DiskUsage{}, nil
}

// Info implements docker.DockerAPI.
func (m *MockDockerAPI) Info(ctx context.Context) (system.Info, error) {
	m.InfoCalls++
	if m.InfoFunc != nil {
		return m.InfoFunc(ctx)
	}
	return system.Info{}, nil
}

// Close implements docker.DockerAPI.
func (m *MockDockerAPI) Close() error {
	m.CloseCalls++
	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	return nil
}

// Reset resets all call counters.
func (m *MockDockerAPI) Reset() {
	m.PingCalls = 0
	m.ContainerListCalls = 0
	m.ContainerInspectCalls = 0
	m.ContainerLogsCalls = 0
	m.ContainerStartCalls = 0
	m.ContainerStopCalls = 0
	m.ContainerRestartCalls = 0
	m.ContainerRemoveCalls = 0
	m.ContainerStatsCalls = 0
	m.DiskUsageCalls = 0
	m.InfoCalls = 0
	m.CloseCalls = 0
}

// Verify MockDockerAPI implements docker.DockerAPI at compile time.
var _ docker.DockerAPI = (*MockDockerAPI)(nil)

// MakeTestContainer creates a container.Summary with compose labels for testing.
func MakeTestContainer(id, name, image, state, project, service string) container.Summary {
	labels := map[string]string{}
	if project != "" {
		labels["com.docker.compose.project"] = project
	}
	if service != "" {
		labels["com.docker.compose.service"] = service
	}

	status := "Up 10 minutes"
	return container.Summary{
		ID:      id + "0000000000000000",
		Names:   []string{"/" + name},
		Image:   image,
		State:   state,
		Status:  status,
		Created: 1700000000,
		Labels:  labels,
		Ports: []container.Port{
			{PublicPort: 8080, PrivatePort: 80, Type: "tcp"},
		},
	}
}
