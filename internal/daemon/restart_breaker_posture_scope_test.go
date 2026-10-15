package daemon

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cameronsjo/bosun/internal/docker"
	"github.com/cameronsjo/bosun/internal/docker/dockertest"
	"github.com/cameronsjo/bosun/internal/reconcile"
)

// TestRestartBreakerPostureMatchesRuntimeScope_RootProjectName pins bosun#719:
// a root-level project_name in the local bosun.yaml is posture input only, so
// before a reconcile reloads it from the repository the implicit default
// target's breaker scope is empty and the breaker stops nothing. The startup
// posture report must agree with what the breaker does in that window: if it
// stays silent, the breaker must act on a restart loop in that project; if the
// breaker does not act, the warning must fire.
func TestRestartBreakerPostureMatchesRuntimeScope_RootProjectName(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "bosun.yaml"), []byte("project_name: homelab\n"), 0o644))
	originalWd, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.Chdir(originalWd)) })
	require.NoError(t, os.Chdir(root))

	t.Setenv("BOSUN_TARGETS", "")
	t.Setenv("BOSUN_RESTART_BREAKER", "true")
	t.Setenv("BOSUN_RESTART_THRESHOLD", "5")

	cfg := ConfigFromEnv()
	require.Equal(t, "homelab", cfg.ProjectNameFromFile, "fixture must supply a root-level project_name")
	rcfg := cfg.ReconcileConfig
	require.True(t, rcfg.RestartBreakerEnabled)
	rcfg.RepoURL = "https://example.test/repo.git"
	rcfg.StateFile = filepath.Join(root, "state", "deploy-state.json")
	rcfg.StagingDir = filepath.Join(root, "staging")
	rcfg.LockFile = filepath.Join(root, "reconcile.lock")
	require.NoError(t, os.MkdirAll(filepath.Dir(rcfg.StateFile), 0o700))
	require.NoError(t, os.MkdirAll(rcfg.StagingDir, 0o700))
	cfg.APITimeout = time.Second

	targets, err := rcfg.ResolveTargets()
	require.NoError(t, err)
	require.Len(t, targets, 1)
	require.True(t, targets[0].IsDefault())

	d := &Daemon{
		config:        cfg,
		targets:       targets,
		targetRuntime: make(map[string]targetRuntimeStatus),
		stopLoops:     make(chan struct{}),
	}

	var output bytes.Buffer
	d.warnRestartBreakerScopePosture(zerolog.New(&output), targets)
	warned := output.Len() > 0

	// Startup state: no reconcile has run, so no repo reload has set a name.
	containerID := "homelab-web-"
	saveTargetState(t, rcfg, targets[0], &reconcile.DeployState{
		LastDeployedCommit: "default-commit",
		DeclaredServices:   []reconcile.DeclaredService{{Name: "web", Image: "nginx:latest"}},
		RestartTracking: map[string]reconcile.RestartTrackingEntry{
			"web": {
				ContainerID:          containerID,
				RestartCount:         0,
				BaselineRestartCount: 0,
				BaselineAt:           time.Now().Add(-time.Minute),
				CheckedAt:            time.Now().Add(-time.Minute),
			},
		},
	})
	mock := dockertest.NewMockDockerAPI()
	mock.ContainerListFunc = func(_ context.Context, _ client.ContainerListOptions) (client.ContainerListResult, error) {
		return client.ContainerListResult{Items: []container.Summary{
			dockertest.MakeTestContainer(containerID, "homelab-web-1", "nginx:latest", "running", "homelab", "web"),
		}}, nil
	}
	mock.ContainerInspectFunc = func(_ context.Context, id string, _ client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
		return client.ContainerInspectResult{Container: container.InspectResponse{
			ID:              id + "-long-id",
			Name:            "/" + id,
			Config:          &container.Config{Image: "nginx:latest"},
			State:           &container.State{Status: container.StateRunning},
			RestartCount:    5,
			NetworkSettings: &container.NetworkSettings{},
		}}, nil
	}

	d.runDriftCheckForTarget(context.Background(), targets[0], docker.NewClientWithAPI(mock))
	breakerActed := mock.ContainerStopCalls > 0

	assert.Equal(t, !breakerActed, warned,
		"startup posture must match the breaker: warned=%v, breaker stopped the looping container=%v, warning output=%q",
		warned, breakerActed, output.String())
	if warned {
		assert.Contains(t, output.String(), "project_name")
	}
}
