package reconcile

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/cameronsjo/bosun/internal/docker"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeInspectResponse builds a minimal valid InspectResponse for health gate tests.
func makeInspectResponse(name, state string, health *container.Health) container.InspectResponse {
	return container.InspectResponse{
		ContainerJSONBase: &container.ContainerJSONBase{
			ID:      "abc1234567890000",
			Name:    "/" + name,
			Created: "2024-01-01T00:00:00.000000000Z",
			State: &container.State{
				Status:    state,
				Running:   state == "running",
				StartedAt: "2024-01-01T00:00:00.000000000Z",
				Health:    health,
			},
			Driver: "overlay2",
		},
		Config: &container.Config{
			Image:  name + ":latest",
			Labels: map[string]string{},
			Env:    []string{},
		},
		NetworkSettings: &container.NetworkSettings{
			NetworkSettingsBase: container.NetworkSettingsBase{ //nolint:staticcheck
				Ports: nat.PortMap{},
			},
			Networks: map[string]*network.EndpointSettings{},
		},
	}
}

func TestCheckCriticalContainerHealth_AllHealthy(t *testing.T) {
	mockAPI := newReconcileMockDockerAPI()
	mockAPI.containerInspectFunc = func(_ context.Context, name string) (container.InspectResponse, error) {
		return makeInspectResponse(name, "running", &container.Health{Status: "healthy"}), nil
	}
	client := docker.NewClientWithAPI(mockAPI)

	err := CheckCriticalContainerHealth(
		context.Background(), client,
		[]string{"traefik", "authelia"},
		5*time.Second,
	)
	require.NoError(t, err)
}

func TestCheckCriticalContainerHealth_OneUnhealthy(t *testing.T) {
	mockAPI := newReconcileMockDockerAPI()
	mockAPI.containerInspectFunc = func(_ context.Context, name string) (container.InspectResponse, error) {
		if name == "authelia" {
			return makeInspectResponse(name, "running", &container.Health{
				Status:        "unhealthy",
				FailingStreak: 3,
				Log:           []*container.HealthcheckResult{{ExitCode: 1, Output: "connection refused"}},
			}), nil
		}
		return makeInspectResponse(name, "running", &container.Health{Status: "healthy"}), nil
	}
	client := docker.NewClientWithAPI(mockAPI)

	err := CheckCriticalContainerHealth(
		context.Background(), client,
		[]string{"traefik", "authelia"},
		1*time.Second,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "authelia")
	assert.Contains(t, err.Error(), "unhealthy")
	assert.NotContains(t, err.Error(), "traefik")
}

func TestCheckCriticalContainerHealth_MissingContainer(t *testing.T) {
	mockAPI := newReconcileMockDockerAPI()
	mockAPI.containerInspectFunc = func(_ context.Context, name string) (container.InspectResponse, error) {
		if name == "authelia" {
			return container.InspectResponse{}, fmt.Errorf("Error: No such container: authelia")
		}
		return makeInspectResponse(name, "running", &container.Health{Status: "healthy"}), nil
	}
	client := docker.NewClientWithAPI(mockAPI)

	err := CheckCriticalContainerHealth(
		context.Background(), client,
		[]string{"traefik", "authelia"},
		1*time.Second,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "authelia")
	assert.Contains(t, err.Error(), "missing")
}

func TestCheckCriticalContainerHealth_NoHealthcheckPasses(t *testing.T) {
	mockAPI := newReconcileMockDockerAPI()
	mockAPI.containerInspectFunc = func(_ context.Context, name string) (container.InspectResponse, error) {
		// Running container with no healthcheck (nil Health field).
		return makeInspectResponse(name, "running", nil), nil
	}
	client := docker.NewClientWithAPI(mockAPI)

	err := CheckCriticalContainerHealth(
		context.Background(), client,
		[]string{"traefik"},
		5*time.Second,
	)
	require.NoError(t, err)
}

func TestCheckCriticalContainerHealth_TimeoutWithEventualSuccess(t *testing.T) {
	callCount := 0
	mockAPI := newReconcileMockDockerAPI()
	mockAPI.containerInspectFunc = func(_ context.Context, name string) (container.InspectResponse, error) {
		callCount++
		status := "starting"
		if callCount >= 2 {
			status = "healthy"
		}
		return makeInspectResponse(name, "running", &container.Health{Status: status}), nil
	}
	client := docker.NewClientWithAPI(mockAPI)

	err := CheckCriticalContainerHealth(
		context.Background(), client,
		[]string{"authelia"},
		30*time.Second,
	)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, callCount, 2)
}

func TestCheckCriticalContainerHealth_EmptyListSkips(t *testing.T) {
	err := CheckCriticalContainerHealth(
		context.Background(), nil,
		[]string{},
		5*time.Second,
	)
	require.NoError(t, err)
}

func TestCheckCriticalContainerHealth_NotRunning(t *testing.T) {
	mockAPI := newReconcileMockDockerAPI()
	mockAPI.containerInspectFunc = func(_ context.Context, name string) (container.InspectResponse, error) {
		return makeInspectResponse(name, "exited", nil), nil
	}
	client := docker.NewClientWithAPI(mockAPI)

	err := CheckCriticalContainerHealth(
		context.Background(), client,
		[]string{"authelia"},
		1*time.Second,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not_running")
}

func TestCheckCriticalContainerHealth_ContextCancelled(t *testing.T) {
	mockAPI := newReconcileMockDockerAPI()
	mockAPI.containerInspectFunc = func(_ context.Context, name string) (container.InspectResponse, error) {
		return makeInspectResponse(name, "running", &container.Health{Status: "starting"}), nil
	}
	client := docker.NewClientWithAPI(mockAPI)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := CheckCriticalContainerHealth(
		ctx, client,
		[]string{"authelia"},
		30*time.Second,
	)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}
