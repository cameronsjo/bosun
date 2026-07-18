package reconcile

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net"

	"github.com/moby/moby/client"
)

// reconcileMockDockerAPI implements docker.DockerAPI for reconcile package tests.
// Only the methods needed by reconcile are given real function overrides;
// the rest return safe defaults.
type reconcileMockDockerAPI struct {
	containerRestartFunc     func(ctx context.Context, containerID string, options client.ContainerRestartOptions) (client.ContainerRestartResult, error)
	containerInspectFunc     func(ctx context.Context, containerID string, options client.ContainerInspectOptions) (client.ContainerInspectResult, error)
	containerListFunc        func(ctx context.Context, options client.ContainerListOptions) (client.ContainerListResult, error)
	containerExecCreateFunc  func(ctx context.Context, containerID string, options client.ExecCreateOptions) (client.ExecCreateResult, error)
	containerExecAttachFunc  func(ctx context.Context, execID string, options client.ExecAttachOptions) (client.ExecAttachResult, error)
	containerExecInspectFunc func(ctx context.Context, execID string, options client.ExecInspectOptions) (client.ExecInspectResult, error)
}

func newReconcileMockDockerAPI() *reconcileMockDockerAPI {
	return &reconcileMockDockerAPI{}
}

func (m *reconcileMockDockerAPI) Ping(ctx context.Context, options client.PingOptions) (client.PingResult, error) {
	return client.PingResult{APIVersion: "1.45"}, nil
}

func (m *reconcileMockDockerAPI) ContainerList(ctx context.Context, options client.ContainerListOptions) (client.ContainerListResult, error) {
	if m.containerListFunc != nil {
		return m.containerListFunc(ctx, options)
	}
	return client.ContainerListResult{}, nil
}

func (m *reconcileMockDockerAPI) ContainerInspect(ctx context.Context, containerID string, options client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	if m.containerInspectFunc != nil {
		return m.containerInspectFunc(ctx, containerID, options)
	}
	return client.ContainerInspectResult{}, nil
}

func (m *reconcileMockDockerAPI) ContainerLogs(_ context.Context, _ string, _ client.ContainerLogsOptions) (client.ContainerLogsResult, error) {
	return io.NopCloser(bytes.NewReader([]byte{})), nil
}

func (m *reconcileMockDockerAPI) ContainerStart(_ context.Context, _ string, _ client.ContainerStartOptions) (client.ContainerStartResult, error) {
	return client.ContainerStartResult{}, nil
}

func (m *reconcileMockDockerAPI) ContainerStop(_ context.Context, _ string, _ client.ContainerStopOptions) (client.ContainerStopResult, error) {
	return client.ContainerStopResult{}, nil
}

func (m *reconcileMockDockerAPI) ContainerRestart(ctx context.Context, containerID string, options client.ContainerRestartOptions) (client.ContainerRestartResult, error) {
	if m.containerRestartFunc != nil {
		return m.containerRestartFunc(ctx, containerID, options)
	}
	return client.ContainerRestartResult{}, nil
}

func (m *reconcileMockDockerAPI) ContainerRemove(_ context.Context, _ string, _ client.ContainerRemoveOptions) (client.ContainerRemoveResult, error) {
	return client.ContainerRemoveResult{}, nil
}

func (m *reconcileMockDockerAPI) ExecCreate(ctx context.Context, containerID string, options client.ExecCreateOptions) (client.ExecCreateResult, error) {
	if m.containerExecCreateFunc != nil {
		return m.containerExecCreateFunc(ctx, containerID, options)
	}
	return client.ExecCreateResult{ID: "mock-exec-id"}, nil
}

func (m *reconcileMockDockerAPI) ExecAttach(ctx context.Context, execID string, options client.ExecAttachOptions) (client.ExecAttachResult, error) {
	if m.containerExecAttachFunc != nil {
		return m.containerExecAttachFunc(ctx, execID, options)
	}
	return client.ExecAttachResult{HijackedResponse: mockHijackedResponse()}, nil
}

// mockHijackedResponse creates a HijackedResponse with a valid net.Conn
// so that Close() does not panic.
func mockHijackedResponse() client.HijackedResponse {
	server, pipeClient := net.Pipe()
	_ = server.Close()
	return client.HijackedResponse{
		Conn:   pipeClient,
		Reader: bufio.NewReader(bytes.NewReader([]byte{})),
	}
}

func (m *reconcileMockDockerAPI) ExecInspect(ctx context.Context, execID string, options client.ExecInspectOptions) (client.ExecInspectResult, error) {
	if m.containerExecInspectFunc != nil {
		return m.containerExecInspectFunc(ctx, execID, options)
	}
	return client.ExecInspectResult{ExitCode: 0}, nil
}

func (m *reconcileMockDockerAPI) ContainerStats(_ context.Context, _ string, _ client.ContainerStatsOptions) (client.ContainerStatsResult, error) {
	return client.ContainerStatsResult{
		Body: io.NopCloser(bytes.NewReader([]byte("{}"))),
	}, nil
}

func (m *reconcileMockDockerAPI) DiskUsage(_ context.Context, _ client.DiskUsageOptions) (client.DiskUsageResult, error) {
	return client.DiskUsageResult{}, nil
}

func (m *reconcileMockDockerAPI) Info(_ context.Context, _ client.InfoOptions) (client.SystemInfoResult, error) {
	return client.SystemInfoResult{}, nil
}

func (m *reconcileMockDockerAPI) Close() error {
	return nil
}
