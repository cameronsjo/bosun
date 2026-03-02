package reconcile

import (
	"bytes"
	"context"
	"io"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/system"
)

// reconcileMockDockerAPI implements docker.DockerAPI for reconcile package tests.
// Only the methods needed by reconcile are given real function overrides;
// the rest return safe defaults.
type reconcileMockDockerAPI struct {
	containerRestartFunc func(ctx context.Context, containerID string, options container.StopOptions) error
	containerInspectFunc func(ctx context.Context, containerID string) (container.InspectResponse, error)
	containerListFunc    func(ctx context.Context, options container.ListOptions) ([]container.Summary, error)
}

func newReconcileMockDockerAPI() *reconcileMockDockerAPI {
	return &reconcileMockDockerAPI{}
}

func (m *reconcileMockDockerAPI) Ping(ctx context.Context) (types.Ping, error) {
	return types.Ping{APIVersion: "1.45"}, nil
}

func (m *reconcileMockDockerAPI) ContainerList(ctx context.Context, options container.ListOptions) ([]container.Summary, error) {
	if m.containerListFunc != nil {
		return m.containerListFunc(ctx, options)
	}
	return []container.Summary{}, nil
}

func (m *reconcileMockDockerAPI) ContainerInspect(ctx context.Context, containerID string) (container.InspectResponse, error) {
	if m.containerInspectFunc != nil {
		return m.containerInspectFunc(ctx, containerID)
	}
	return container.InspectResponse{}, nil
}

func (m *reconcileMockDockerAPI) ContainerLogs(_ context.Context, _ string, _ container.LogsOptions) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader([]byte{})), nil
}

func (m *reconcileMockDockerAPI) ContainerStart(_ context.Context, _ string, _ container.StartOptions) error {
	return nil
}

func (m *reconcileMockDockerAPI) ContainerRestart(ctx context.Context, containerID string, options container.StopOptions) error {
	if m.containerRestartFunc != nil {
		return m.containerRestartFunc(ctx, containerID, options)
	}
	return nil
}

func (m *reconcileMockDockerAPI) ContainerRemove(_ context.Context, _ string, _ container.RemoveOptions) error {
	return nil
}

func (m *reconcileMockDockerAPI) ContainerStats(_ context.Context, _ string, _ bool) (container.StatsResponseReader, error) {
	return container.StatsResponseReader{
		Body: io.NopCloser(bytes.NewReader([]byte("{}"))),
	}, nil
}

func (m *reconcileMockDockerAPI) DiskUsage(_ context.Context, _ types.DiskUsageOptions) (types.DiskUsage, error) {
	return types.DiskUsage{}, nil
}

func (m *reconcileMockDockerAPI) Info(_ context.Context) (system.Info, error) {
	return system.Info{}, nil
}

func (m *reconcileMockDockerAPI) Close() error {
	return nil
}
