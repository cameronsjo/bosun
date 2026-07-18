package docker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

// Common test errors.
var (
	errMockPing      = errors.New("mock: ping failed")
	errMockList      = errors.New("mock: container list failed")
	errMockInspect   = errors.New("mock: container inspect failed")
	errMockLogs      = errors.New("mock: container logs failed")
	errMockStart     = errors.New("mock: container start failed")
	errMockRestart   = errors.New("mock: container restart failed")
	errMockRemove    = errors.New("mock: container remove failed")
	errMockStats     = errors.New("mock: container stats failed")
	errMockDiskUsage = errors.New("mock: disk usage failed")
	errMockInfo      = errors.New("mock: info failed")
)

// MockDockerAPI is a mock implementation of DockerAPI for testing.
type MockDockerAPI struct {
	// Function overrides for each method
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

	// Call tracking
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

// Ping implements DockerAPI.
func (m *MockDockerAPI) Ping(ctx context.Context, options client.PingOptions) (client.PingResult, error) {
	m.PingCalls++
	if m.PingFunc != nil {
		return m.PingFunc(ctx, options)
	}
	return client.PingResult{APIVersion: "1.45"}, nil
}

// ContainerList implements DockerAPI.
func (m *MockDockerAPI) ContainerList(ctx context.Context, options client.ContainerListOptions) (client.ContainerListResult, error) {
	m.ContainerListCalls++
	if m.ContainerListFunc != nil {
		return m.ContainerListFunc(ctx, options)
	}
	return client.ContainerListResult{Items: []container.Summary{}}, nil
}

// ContainerInspect implements DockerAPI.
func (m *MockDockerAPI) ContainerInspect(ctx context.Context, containerID string, options client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	m.ContainerInspectCalls++
	if m.ContainerInspectFunc != nil {
		return m.ContainerInspectFunc(ctx, containerID, options)
	}
	return client.ContainerInspectResult{}, nil
}

// ContainerLogs implements DockerAPI.
func (m *MockDockerAPI) ContainerLogs(ctx context.Context, containerName string, options client.ContainerLogsOptions) (client.ContainerLogsResult, error) {
	m.ContainerLogsCalls++
	if m.ContainerLogsFunc != nil {
		return m.ContainerLogsFunc(ctx, containerName, options)
	}
	return io.NopCloser(bytes.NewReader([]byte{})), nil
}

// ContainerStart implements DockerAPI.
func (m *MockDockerAPI) ContainerStart(ctx context.Context, containerID string, options client.ContainerStartOptions) (client.ContainerStartResult, error) {
	m.ContainerStartCalls++
	if m.ContainerStartFunc != nil {
		return m.ContainerStartFunc(ctx, containerID, options)
	}
	return client.ContainerStartResult{}, nil
}

// ContainerStop implements DockerAPI.
func (m *MockDockerAPI) ContainerStop(ctx context.Context, containerID string, options client.ContainerStopOptions) (client.ContainerStopResult, error) {
	m.ContainerStopCalls++
	if m.ContainerStopFunc != nil {
		return m.ContainerStopFunc(ctx, containerID, options)
	}
	return client.ContainerStopResult{}, nil
}

// ContainerRestart implements DockerAPI.
func (m *MockDockerAPI) ContainerRestart(ctx context.Context, containerID string, options client.ContainerRestartOptions) (client.ContainerRestartResult, error) {
	m.ContainerRestartCalls++
	if m.ContainerRestartFunc != nil {
		return m.ContainerRestartFunc(ctx, containerID, options)
	}
	return client.ContainerRestartResult{}, nil
}

// ContainerRemove implements DockerAPI.
func (m *MockDockerAPI) ContainerRemove(ctx context.Context, containerID string, options client.ContainerRemoveOptions) (client.ContainerRemoveResult, error) {
	m.ContainerRemoveCalls++
	if m.ContainerRemoveFunc != nil {
		return m.ContainerRemoveFunc(ctx, containerID, options)
	}
	return client.ContainerRemoveResult{}, nil
}

// ExecCreate implements DockerAPI.
func (m *MockDockerAPI) ExecCreate(ctx context.Context, containerID string, options client.ExecCreateOptions) (client.ExecCreateResult, error) {
	m.ExecCreateCalls++
	if m.ExecCreateFunc != nil {
		return m.ExecCreateFunc(ctx, containerID, options)
	}
	return client.ExecCreateResult{ID: "mock-exec-id"}, nil
}

// ExecAttach implements DockerAPI.
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

// ExecInspect implements DockerAPI.
func (m *MockDockerAPI) ExecInspect(ctx context.Context, execID string, options client.ExecInspectOptions) (client.ExecInspectResult, error) {
	m.ExecInspectCalls++
	if m.ExecInspectFunc != nil {
		return m.ExecInspectFunc(ctx, execID, options)
	}
	return client.ExecInspectResult{ExitCode: 0}, nil
}

// ContainerStats implements DockerAPI.
func (m *MockDockerAPI) ContainerStats(ctx context.Context, containerID string, options client.ContainerStatsOptions) (client.ContainerStatsResult, error) {
	m.ContainerStatsCalls++
	if m.ContainerStatsFunc != nil {
		return m.ContainerStatsFunc(ctx, containerID, options)
	}
	// Return empty stats
	stats := statsJSON{}
	data, _ := json.Marshal(stats)
	return client.ContainerStatsResult{
		Body: io.NopCloser(bytes.NewReader(data)),
	}, nil
}

// DiskUsage implements DockerAPI.
func (m *MockDockerAPI) DiskUsage(ctx context.Context, options client.DiskUsageOptions) (client.DiskUsageResult, error) {
	m.DiskUsageCalls++
	if m.DiskUsageFunc != nil {
		return m.DiskUsageFunc(ctx, options)
	}
	return client.DiskUsageResult{}, nil
}

// Info implements DockerAPI.
func (m *MockDockerAPI) Info(ctx context.Context, options client.InfoOptions) (client.SystemInfoResult, error) {
	m.InfoCalls++
	if m.InfoFunc != nil {
		return m.InfoFunc(ctx, options)
	}
	return client.SystemInfoResult{}, nil
}

// Close implements DockerAPI.
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

// Helper functions for creating test data

// makeTestContainer creates a test container with the given name and state.
func makeTestContainer(id, name, image, state string) container.Summary {
	return makeTestContainerWithHealth(id, name, image, state, "")
}

// makeTestContainerWithHealth creates a test container.Summary with optional health status in the Status string.
func makeTestContainerWithHealth(id, name, image, state, health string) container.Summary {
	return makeTestContainerFull(id, name, image, state, health, nil)
}

// makeTestContainerFull creates a test container.Summary with all optional fields.
func makeTestContainerFull(id, name, image, state, health string, labels map[string]string) container.Summary {
	status := "Up 10 minutes"
	if health != "" {
		status = "Up 10 minutes (" + health + ")"
	}
	return container.Summary{
		ID:      id + "0000000000000000", // Pad to make 12-char truncation work
		Names:   []string{"/" + name},
		Image:   image,
		State:   container.ContainerState(state),
		Status:  status,
		Created: 1700000000, // Fixed timestamp for testing
		Labels:  labels,
		Ports: []container.PortSummary{
			{PublicPort: 8080, PrivatePort: 80, Type: "tcp"},
		},
	}
}

// makeTestContainerJSON creates a test ContainerInspectResult for inspection.
func makeTestContainerJSON(id, name, image, status string, running bool) client.ContainerInspectResult {
	state := &container.State{
		Status:    container.ContainerState(status),
		Running:   running,
		StartedAt: "2024-01-01T00:00:00.000000000Z",
	}
	if !running {
		state.ExitCode = 0
	}

	return client.ContainerInspectResult{
		Container: container.InspectResponse{
			ID:      id + "0000000000000000",
			Name:    "/" + name,
			Created: "2024-01-01T00:00:00.000000000Z",
			State:   state,
			Driver:  "overlay2",
			Config: &container.Config{
				Image:  image,
				Labels: map[string]string{"app": "test"},
				Env:    []string{"FOO=bar"},
			},
			NetworkSettings: &container.NetworkSettings{
				Ports: network.PortMap{},
				Networks: map[string]*network.EndpointSettings{
					"bridge": {},
				},
			},
			Mounts: []container.MountPoint{},
		},
	}
}

// makeStatsJSON creates test stats JSON data.
func makeStatsJSON(cpuTotal, cpuSystem, preCPUTotal, preSystem, memUsage, memLimit uint64, cpuCount int) []byte {
	stats := map[string]any{
		"cpu_stats": map[string]any{
			"cpu_usage": map[string]any{
				"total_usage":  cpuTotal,
				"percpu_usage": make([]uint64, cpuCount),
			},
			"system_cpu_usage": cpuSystem,
		},
		"precpu_stats": map[string]any{
			"cpu_usage": map[string]any{
				"total_usage": preCPUTotal,
			},
			"system_cpu_usage": preSystem,
		},
		"memory_stats": map[string]any{
			"usage": memUsage,
			"limit": memLimit,
		},
	}
	data, _ := json.Marshal(stats)
	return data
}

// Verify MockDockerAPI implements DockerAPI.
var _ DockerAPI = (*MockDockerAPI)(nil)
