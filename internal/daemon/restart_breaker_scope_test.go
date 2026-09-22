package daemon

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cameronsjo/bosun/internal/docker/dockertest"
	"github.com/cameronsjo/bosun/internal/reconcile"
)

// restartLoopInspect answers every inspect with a restart count far above the
// default threshold, so any container the breaker samples would trip.
func restartLoopInspect(_ context.Context, name string, _ client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	return client.ContainerInspectResult{
		Container: container.InspectResponse{
			ID:           strings.Repeat("a", 32) + name,
			Name:         "/" + name,
			RestartCount: 12,
			State: &container.State{
				Status:    "running",
				StartedAt: "2026-03-13T10:00:00Z",
			},
			Config: &container.Config{
				Image:  "api:latest",
				Labels: map[string]string{},
				Env:    []string{},
			},
			NetworkSettings: &container.NetworkSettings{
				Networks: map[string]*network.EndpointSettings{},
			},
		},
	}, nil
}

// TestDaemonRunDriftCheck_RestartBreakerStopsOnlyItsOwnProject drives the real
// daemon drift check against a Docker host that carries a foreign-project
// container, and pins three things at once:
//
//   - the breaker stops the container in the daemon's own compose project
//     (which needs the resolved target project name to reach it),
//   - it never stops the foreign-project container, and
//   - the read-only drift comparison still sees the whole host, so narrowing
//     the drift scope is not smuggled in with the breaker fix.
func TestDaemonRunDriftCheck_RestartBreakerStopsOnlyItsOwnProject(t *testing.T) {
	var stopped []string
	mock := dockertest.NewMockDockerAPI()
	mock.ContainerListFunc = func(_ context.Context, _ client.ContainerListOptions) (client.ContainerListResult, error) {
		return client.ContainerListResult{Items: []container.Summary{
			dockertest.MakeTestContainer("aaa111", "bosun-proj-api-1", "api:latest", "running", "bosun-proj", "api"),
			dockertest.MakeTestContainer("bbb222", "other-cache-1", "cache:latest", "running", "other-project", "cache"),
		}}, nil
	}
	mock.ContainerInspectFunc = restartLoopInspect
	mock.ContainerStopFunc = func(_ context.Context, name string, _ client.ContainerStopOptions) (client.ContainerStopResult, error) {
		stopped = append(stopped, name)
		return client.ContainerStopResult{}, nil
	}

	d := newDockerDaemon(t, mock)
	// A lone `default` target is how a single-target install carries its
	// compose project name; the base config's ProjectName stays empty, exactly
	// as the daemon's own env path leaves it.
	d.config.ReconcileConfig.Targets = []reconcile.Target{{Name: "default", ProjectName: "bosun-proj"}}
	require.Empty(t, d.config.ReconcileConfig.ProjectName, "the daemon must not need the base project name")

	stateFile := d.config.ReconcileConfig.StateFile
	require.NoError(t, reconcile.SaveState(stateFile, &reconcile.DeployState{
		LastDeployedCommit: "abc123",
		DeclaredServices: []reconcile.DeclaredService{
			{Name: "api", Image: "api:latest"},
			{Name: "cache", Image: "cache:latest"},
		},
		RestartTracking: map[string]reconcile.RestartTrackingEntry{
			"api":   {RestartCount: 0},
			"cache": {RestartCount: 0},
		},
	}))

	d.runDriftCheck(context.Background())
	d.wg.Wait()

	assert.Equal(t, []string{"bosun-proj-api-1"}, stopped,
		"the breaker stops its own project's restart loop and nothing else")
	assert.NotContains(t, stopped, "other-cache-1",
		"a container from another compose project is never bosun's to stop")

	loaded := reconcile.LoadState(stateFile)
	assert.True(t, loaded.RestartTracking["api"].Tripped, "the in-project service is tripped")
	assert.False(t, loaded.RestartTracking["cache"].Tripped, "a foreign container may not trip the breaker")

	// Drift is a read-only comparison and keeps its host-wide scope: the
	// foreign "cache" container still answers for the declared cache service,
	// so no missing-service drift is reported for it.
	for _, item := range loaded.DriftItems {
		assert.NotEqual(t, "cache", item.Service,
			"the drift check's comparison scope must be unchanged by the breaker fix")
	}
}

func TestDaemonRestartBreakerProjectName(t *testing.T) {
	t.Run("resolves a lone target's project name", func(t *testing.T) {
		d := newDockerDaemon(t, dockertest.NewMockDockerAPI())
		d.config.ReconcileConfig.Targets = []reconcile.Target{{Name: "default", ProjectName: "homelab"}}

		assert.Equal(t, "homelab", d.restartBreakerProjectName())
	})

	t.Run("falls back to the local bosun.yaml root project_name", func(t *testing.T) {
		d := newDockerDaemon(t, dockertest.NewMockDockerAPI())
		d.config.ProjectNameFromFile = "from-file"

		assert.Equal(t, "from-file", d.restartBreakerProjectName())
	})

	t.Run("resolves nothing when no source supplies a project name", func(t *testing.T) {
		d := newDockerDaemon(t, dockertest.NewMockDockerAPI())

		assert.Empty(t, d.restartBreakerProjectName(),
			"an unresolved scope disables the breaker instead of widening it")
	})
}

func TestWarnRestartBreakerScopePosture(t *testing.T) {
	t.Run("announces a breaker that has become a no-op", func(t *testing.T) {
		d := newDockerDaemon(t, dockertest.NewMockDockerAPI())
		d.config.ReconcileConfig.RestartBreakerEnabled = true

		var output bytes.Buffer
		d.warnRestartBreakerScopePosture(zerolog.New(&output))

		logs := output.String()
		assert.Contains(t, logs, `"level":"warn"`)
		assert.Contains(t, logs, "Restart circuit breaker is DISABLED")
	})

	t.Run("stays quiet when a scope resolves", func(t *testing.T) {
		d := newDockerDaemon(t, dockertest.NewMockDockerAPI())
		d.config.ReconcileConfig.RestartBreakerEnabled = true
		d.config.ReconcileConfig.Targets = []reconcile.Target{{Name: "default", ProjectName: "homelab"}}

		var output bytes.Buffer
		d.warnRestartBreakerScopePosture(zerolog.New(&output))

		assert.Empty(t, output.String())
	})

	t.Run("stays quiet when the breaker is disabled", func(t *testing.T) {
		d := newDockerDaemon(t, dockertest.NewMockDockerAPI())
		d.config.ReconcileConfig.RestartBreakerEnabled = false

		var output bytes.Buffer
		d.warnRestartBreakerScopePosture(zerolog.New(&output))

		assert.Empty(t, output.String())
	})
}
