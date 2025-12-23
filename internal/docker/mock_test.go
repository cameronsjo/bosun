package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/system"
	"github.com/docker/go-connections/nat"
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
	PingFunc            func(ctx context.Context) (types.Ping, error)
	ContainerListFunc   func(ctx context.Context, options container.ListOptions) ([]container.Summary, error)
	ContainerInspectFunc func(ctx context.Context, containerID string) (container.InspectResponse, error)
	ContainerLogsFunc   func(ctx context.Context, ctr string, options container.LogsOptions) (io.ReadCloser, error)
	ContainerStartFunc  func(ctx context.Context, containerID string, options container.StartOptions) error
	ContainerRestartFunc func(ctx context.Context, containerID string, options container.StopOptions) error
	ContainerRemoveFunc func(ctx context.Context, containerID string, options container.RemoveOptions) error
	ContainerStatsFunc  func(ctx context.Context, containerID string, stream bool) (container.StatsResponseReader, error)
	DiskUsageFunc       func(ctx context.Context, options types.DiskUsageOptions) (types.DiskUsage, error)
	InfoFunc            func(ctx context.Context) (system.Info, error)
	CloseFunc           func() error

	// Call tracking
	PingCalls           int
	ContainerListCalls  int
	ContainerInspectCalls int
	ContainerLogsCalls  int
	ContainerStartCalls int
	ContainerRestartCalls int
	ContainerRemoveCalls int
	ContainerStatsCalls int
	DiskUsageCalls      int
	InfoCalls           int
	CloseCalls          int
}

// NewMockDockerAPI creates a new mock with default no-op implementations.
func NewMockDockerAPI() *MockDockerAPI {
	return &MockDockerAPI{}
}

// Ping implements DockerAPI.
func (m *MockDockerAPI) Ping(ctx context.Context) (types.Ping, error) {
	m.PingCalls++
	if m.PingFunc != nil {
		return m.PingFunc(ctx)
	}
	return types.Ping{APIVersion: "1.45"}, nil
}

// ContainerList implements DockerAPI.
func (m *MockDockerAPI) ContainerList(ctx context.Context, options container.ListOptions) ([]container.Summary, error) {
	m.ContainerListCalls++
	if m.ContainerListFunc != nil {
		return m.ContainerListFunc(ctx, options)
	}
	return []container.Summary{}, nil
}

// ContainerInspect implements DockerAPI.
func (m *MockDockerAPI) ContainerInspect(ctx context.Context, containerID string) (container.InspectResponse, error) {
	m.ContainerInspectCalls++
	if m.ContainerInspectFunc != nil {
		return m.ContainerInspectFunc(ctx, containerID)
	}
	return container.InspectResponse{}, nil
}

// ContainerLogs implements DockerAPI.
func (m *MockDockerAPI) ContainerLogs(ctx context.Context, containerName string, options container.LogsOptions) (io.ReadCloser, error) {
	m.ContainerLogsCalls++
	if m.ContainerLogsFunc != nil {
		return m.ContainerLogsFunc(ctx, containerName, options)
	}
	return io.NopCloser(bytes.NewReader([]byte{})), nil
}

// ContainerStart implements DockerAPI.
func (m *MockDockerAPI) ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error {
	m.ContainerStartCalls++
	if m.ContainerStartFunc != nil {
		return m.ContainerStartFunc(ctx, containerID, options)
	}
	return nil
}

// ContainerRestart implements DockerAPI.
func (m *MockDockerAPI) ContainerRestart(ctx context.Context, containerID string, options container.StopOptions) error {
	m.ContainerRestartCalls++
	if m.ContainerRestartFunc != nil {
		return m.ContainerRestartFunc(ctx, containerID, options)
	}
	return nil
}

// ContainerRemove implements DockerAPI.
func (m *MockDockerAPI) ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error {
	m.ContainerRemoveCalls++
	if m.ContainerRemoveFunc != nil {
		return m.ContainerRemoveFunc(ctx, containerID, options)
	}
	return nil
}

// ContainerStats implements DockerAPI.
func (m *MockDockerAPI) ContainerStats(ctx context.Context, containerID string, stream bool) (container.StatsResponseReader, error) {
	m.ContainerStatsCalls++
	if m.ContainerStatsFunc != nil {
		return m.ContainerStatsFunc(ctx, containerID, stream)
	}
	// Return empty stats
	stats := statsJSON{}
	data, _ := json.Marshal(stats)
	return container.StatsResponseReader{
		Body: io.NopCloser(bytes.NewReader(data)),
	}, nil
}

// DiskUsage implements DockerAPI.
func (m *MockDockerAPI) DiskUsage(ctx context.Context, options types.DiskUsageOptions) (types.DiskUsage, error) {
	m.DiskUsageCalls++
	if m.DiskUsageFunc != nil {
		return m.DiskUsageFunc(ctx, options)
	}
	return types.DiskUsage{}, nil
}

// Info implements DockerAPI.
func (m *MockDockerAPI) Info(ctx context.Context) (system.Info, error) {
	m.InfoCalls++
	if m.InfoFunc != nil {
		return m.InfoFunc(ctx)
	}
	return system.Info{}, nil
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
	m.ContainerRestartCalls = 0
	m.ContainerRemoveCalls = 0
	m.ContainerStatsCalls = 0
	m.DiskUsageCalls = 0
	m.InfoCalls = 0
	m.CloseCalls = 0
}

// Helper functions for creating test data

// makeTestContainer creates a test container with the given name and state.
func makeTestContainer(id, name, image, state string) container.Summary {
	return container.Summary{
		ID:      id + "0000000000000000", // Pad to make 12-char truncation work
		Names:   []string{"/" + name},
		Image:   image,
		State:   state,
		Status:  "Up 10 minutes",
		Created: 1700000000, // Fixed timestamp for testing
		Ports: []container.Port{
			{PublicPort: 8080, PrivatePort: 80, Type: "tcp"},
		},
	}
}

// makeTestContainerJSON creates a test ContainerJSON for inspection.
func makeTestContainerJSON(id, name, image, status string, running bool) container.InspectResponse {
	state := &container.State{
		Status:    status,
		Running:   running,
		StartedAt: "2024-01-01T00:00:00.000000000Z",
	}
	if !running {
		state.ExitCode = 0
	}

	return container.InspectResponse{
		ContainerJSONBase: &container.ContainerJSONBase{
			ID:      id + "0000000000000000",
			Name:    "/" + name,
			Created: "2024-01-01T00:00:00.000000000Z",
			State:   state,
			Driver:  "overlay2",
		},
		Config: &container.Config{
			Image:  image,
			Labels: map[string]string{"app": "test"},
			Env:    []string{"FOO=bar"},
		},
		NetworkSettings: &container.NetworkSettings{
			NetworkSettingsBase: container.NetworkSettingsBase{
				Ports: nat.PortMap{},
			},
			Networks: map[string]*network.EndpointSettings{
				"bridge": {},
			},
		},
		Mounts: []container.MountPoint{},
	}
}

// makeTestContainerJSONWithHealth creates a test ContainerJSON with health check.
func makeTestContainerJSONWithHealth(id, name, image, status, health string, running bool) container.InspectResponse {
	cj := makeTestContainerJSON(id, name, image, status, running)
	if health != "" {
		cj.State.Health = &container.Health{
			Status: health,
		}
	}
	return cj
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
