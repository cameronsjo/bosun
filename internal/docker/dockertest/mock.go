// Package dockertest provides test utilities for the docker package.
// It exports a mock DockerAPI implementation usable from any test package.
package dockertest

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"

	"github.com/cameronsjo/bosun/internal/docker"
)

// MockDockerAPI is a mock implementation of docker.DockerAPI for testing.
// Each method has a function override and a call counter.
type MockDockerAPI struct {
	// Function overrides for each method.
	PingFunc             func(ctx context.Context, options client.PingOptions) (client.PingResult, error)
	ContainerListFunc    func(ctx context.Context, options client.ContainerListOptions) (client.ContainerListResult, error)
	ContainerInspectFunc func(ctx context.Context, containerID string, options client.ContainerInspectOptions) (client.ContainerInspectResult, error)
	ContainerLogsFunc    func(ctx context.Context, containerID string, options client.ContainerLogsOptions) (client.ContainerLogsResult, error)
	ContainerStartFunc   func(ctx context.Context, containerID string, options client.ContainerStartOptions) (client.ContainerStartResult, error)
	ContainerStopFunc    func(ctx context.Context, containerID string, options client.ContainerStopOptions) (client.ContainerStopResult, error)
	ContainerRestartFunc func(ctx context.Context, containerID string, options client.ContainerRestartOptions) (client.ContainerRestartResult, error)
	ContainerRemoveFunc  func(ctx context.Context, containerID string, options client.ContainerRemoveOptions) (client.ContainerRemoveResult, error)
	ContainerStatsFunc   func(ctx context.Context, containerID string, options client.ContainerStatsOptions) (client.ContainerStatsResult, error)
	ExecCreateFunc       func(ctx context.Context, containerID string, options client.ExecCreateOptions) (client.ExecCreateResult, error)
	ExecAttachFunc       func(ctx context.Context, execID string, options client.ExecAttachOptions) (client.ExecAttachResult, error)
	ExecInspectFunc      func(ctx context.Context, execID string, options client.ExecInspectOptions) (client.ExecInspectResult, error)
	DiskUsageFunc        func(ctx context.Context, options client.DiskUsageOptions) (client.DiskUsageResult, error)
	InfoFunc             func(ctx context.Context, options client.InfoOptions) (client.SystemInfoResult, error)
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
	ExecCreateCalls       int
	ExecAttachCalls       int
	ExecInspectCalls      int
	DiskUsageCalls        int
	InfoCalls             int
	CloseCalls            int
}

// NewMockDockerAPI creates a new mock with default no-op implementations.
func NewMockDockerAPI() *MockDockerAPI {
	return &MockDockerAPI{}
}

// Ping implements docker.DockerAPI.
func (m *MockDockerAPI) Ping(ctx context.Context, options client.PingOptions) (client.PingResult, error) {
	m.PingCalls++
	if m.PingFunc != nil {
		return m.PingFunc(ctx, options)
	}
	return client.PingResult{APIVersion: "1.45"}, nil
}

// ContainerList implements docker.DockerAPI.
func (m *MockDockerAPI) ContainerList(ctx context.Context, options client.ContainerListOptions) (client.ContainerListResult, error) {
	m.ContainerListCalls++
	if m.ContainerListFunc != nil {
		return m.ContainerListFunc(ctx, options)
	}
	return client.ContainerListResult{Items: []container.Summary{}}, nil
}

// ContainerInspect implements docker.DockerAPI.
func (m *MockDockerAPI) ContainerInspect(ctx context.Context, containerID string, options client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	m.ContainerInspectCalls++
	if m.ContainerInspectFunc != nil {
		return m.ContainerInspectFunc(ctx, containerID, options)
	}
	return client.ContainerInspectResult{}, nil
}

// ContainerLogs implements docker.DockerAPI.
func (m *MockDockerAPI) ContainerLogs(ctx context.Context, containerName string, options client.ContainerLogsOptions) (client.ContainerLogsResult, error) {
	m.ContainerLogsCalls++
	if m.ContainerLogsFunc != nil {
		return m.ContainerLogsFunc(ctx, containerName, options)
	}
	return io.NopCloser(bytes.NewReader([]byte{})), nil
}

// ContainerStart implements docker.DockerAPI.
func (m *MockDockerAPI) ContainerStart(ctx context.Context, containerID string, options client.ContainerStartOptions) (client.ContainerStartResult, error) {
	m.ContainerStartCalls++
	if m.ContainerStartFunc != nil {
		return m.ContainerStartFunc(ctx, containerID, options)
	}
	return client.ContainerStartResult{}, nil
}

// ContainerStop implements docker.DockerAPI.
func (m *MockDockerAPI) ContainerStop(ctx context.Context, containerID string, options client.ContainerStopOptions) (client.ContainerStopResult, error) {
	m.ContainerStopCalls++
	if m.ContainerStopFunc != nil {
		return m.ContainerStopFunc(ctx, containerID, options)
	}
	return client.ContainerStopResult{}, nil
}

// ContainerRestart implements docker.DockerAPI.
func (m *MockDockerAPI) ContainerRestart(ctx context.Context, containerID string, options client.ContainerRestartOptions) (client.ContainerRestartResult, error) {
	m.ContainerRestartCalls++
	if m.ContainerRestartFunc != nil {
		return m.ContainerRestartFunc(ctx, containerID, options)
	}
	return client.ContainerRestartResult{}, nil
}

// ContainerRemove implements docker.DockerAPI.
func (m *MockDockerAPI) ContainerRemove(ctx context.Context, containerID string, options client.ContainerRemoveOptions) (client.ContainerRemoveResult, error) {
	m.ContainerRemoveCalls++
	if m.ContainerRemoveFunc != nil {
		return m.ContainerRemoveFunc(ctx, containerID, options)
	}
	return client.ContainerRemoveResult{}, nil
}

// ContainerStats implements docker.DockerAPI.
func (m *MockDockerAPI) ContainerStats(ctx context.Context, containerID string, options client.ContainerStatsOptions) (client.ContainerStatsResult, error) {
	m.ContainerStatsCalls++
	if m.ContainerStatsFunc != nil {
		return m.ContainerStatsFunc(ctx, containerID, options)
	}
	// Return empty stats JSON.
	data, _ := json.Marshal(map[string]any{
		"cpu_stats":    map[string]any{"cpu_usage": map[string]any{"total_usage": 0}, "system_cpu_usage": 0},
		"precpu_stats": map[string]any{"cpu_usage": map[string]any{"total_usage": 0}, "system_cpu_usage": 0},
		"memory_stats": map[string]any{"usage": 0, "limit": 0},
	})
	return client.ContainerStatsResult{
		Body: io.NopCloser(bytes.NewReader(data)),
	}, nil
}

// ExecCreate implements docker.DockerAPI.
func (m *MockDockerAPI) ExecCreate(ctx context.Context, containerID string, options client.ExecCreateOptions) (client.ExecCreateResult, error) {
	m.ExecCreateCalls++
	if m.ExecCreateFunc != nil {
		return m.ExecCreateFunc(ctx, containerID, options)
	}
	return client.ExecCreateResult{ID: "mock-exec-id"}, nil
}

// ExecAttach implements docker.DockerAPI.
func (m *MockDockerAPI) ExecAttach(ctx context.Context, execID string, options client.ExecAttachOptions) (client.ExecAttachResult, error) {
	m.ExecAttachCalls++
	if m.ExecAttachFunc != nil {
		return m.ExecAttachFunc(ctx, execID, options)
	}
	return client.ExecAttachResult{HijackedResponse: mockHijackedResponse()}, nil
}

// mockHijackedResponse creates a HijackedResponse with a valid net.Conn.
func mockHijackedResponse() client.HijackedResponse {
	server, pipeClient := net.Pipe()
	_ = server.Close()
	return client.HijackedResponse{
		Conn:   pipeClient,
		Reader: bufio.NewReader(bytes.NewReader([]byte{})),
	}
}

// ExecInspect implements docker.DockerAPI.
func (m *MockDockerAPI) ExecInspect(ctx context.Context, execID string, options client.ExecInspectOptions) (client.ExecInspectResult, error) {
	m.ExecInspectCalls++
	if m.ExecInspectFunc != nil {
		return m.ExecInspectFunc(ctx, execID, options)
	}
	return client.ExecInspectResult{ExitCode: 0}, nil
}

// DiskUsage implements docker.DockerAPI.
func (m *MockDockerAPI) DiskUsage(ctx context.Context, options client.DiskUsageOptions) (client.DiskUsageResult, error) {
	m.DiskUsageCalls++
	if m.DiskUsageFunc != nil {
		return m.DiskUsageFunc(ctx, options)
	}
	return client.DiskUsageResult{}, nil
}

// Info implements docker.DockerAPI.
func (m *MockDockerAPI) Info(ctx context.Context, options client.InfoOptions) (client.SystemInfoResult, error) {
	m.InfoCalls++
	if m.InfoFunc != nil {
		return m.InfoFunc(ctx, options)
	}
	return client.SystemInfoResult{}, nil
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
	m.ExecCreateCalls = 0
	m.ExecAttachCalls = 0
	m.ExecInspectCalls = 0
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
		State:   container.ContainerState(state),
		Status:  status,
		Created: 1700000000,
		Labels:  labels,
		Ports: []container.PortSummary{
			{PublicPort: 8080, PrivatePort: 80, Type: "tcp"},
		},
	}
}
