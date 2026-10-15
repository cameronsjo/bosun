package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cameronsjo/bosun/internal/docker"
	"github.com/cameronsjo/bosun/internal/docker/dockertest"
	"github.com/cameronsjo/bosun/internal/reconcile"
)

func targetStatusTestDaemon(t *testing.T, targets []reconcile.Target) (*Daemon, string) {
	t.Helper()
	if len(targets) > 1 {
		// ConfigForTarget inherits the flat project name when a target omits its
		// override. Give fixture siblings distinct Compose namespaces so target
		// layout validation reaches the daemon behavior under test.
		for i := range targets {
			if targets[i].ProjectName == "" {
				targets[i].ProjectName = targets[i].Name + "-stack"
			}
		}
	}
	root := t.TempDir()
	stateRoot := filepath.Join(root, "state")
	stagingRoot := filepath.Join(root, "staging")
	require.NoError(t, os.MkdirAll(stateRoot, 0o700))
	require.NoError(t, os.MkdirAll(stagingRoot, 0o700))
	stateFile := filepath.Join(stateRoot, "deploy-state.json")
	cfg := &reconcile.Config{
		RepoURL:     "https://example.test/repo.git",
		StateFile:   stateFile,
		StagingDir:  stagingRoot,
		LockFile:    filepath.Join(root, "reconcile.lock"),
		Targets:     targets,
		ProjectName: "bosun",
	}
	resolved, err := cfg.ResolveTargets()
	require.NoError(t, err)
	d := &Daemon{
		config:        &Config{APITimeout: time.Second, ReconcileConfig: cfg},
		targets:       resolved,
		targetRuntime: make(map[string]targetRuntimeStatus),
		stopLoops:     make(chan struct{}),
	}
	return d, stateRoot
}

func saveTargetState(t *testing.T, cfg *reconcile.Config, target reconcile.Target, state *reconcile.DeployState) string {
	t.Helper()
	targetCfg := cfg.ConfigForTarget(target)
	require.NoError(t, reconcile.SaveState(targetCfg.StateFile, state))
	return targetCfg.StateFile
}

func TestTargetHealthStatuses_UsesIndependentStateAndRuntime(t *testing.T) {
	d, _ := targetStatusTestDaemon(t, []reconcile.Target{
		{Name: "pi"},
		{Name: "nas", TargetHost: "root@nas"},
	})
	targets := d.targets
	piState := &reconcile.DeployState{
		LastDeployedCommit: "pi-commit",
		DeployedAt:         time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC),
		DriftCheckedAt:     time.Date(2026, 10, 14, 10, 1, 0, 0, time.UTC),
		DriftItems:         []reconcile.DriftItem{{Service: "traefik", Type: reconcile.DriftMissing}},
		AttemptCount:       1,
	}
	remoteCheckedAt := time.Date(2026, 10, 14, 9, 59, 0, 0, time.UTC)
	nasState := &reconcile.DeployState{LastDeployedCommit: "nas-commit", DriftCheckedAt: remoteCheckedAt}
	saveTargetState(t, d.config.ReconcileConfig, targets[0], piState)
	saveTargetState(t, d.config.ReconcileConfig, targets[1], nasState)
	d.recordTargetRuntime("pi", "drifted", "", time.Date(2026, 10, 14, 10, 2, 0, 0, time.UTC))
	d.recordTargetRuntime("nas", "unavailable", "remote target cannot be inspected by local Docker", time.Date(2026, 10, 14, 10, 2, 0, 0, time.UTC))

	statuses := d.targetHealthStatuses(targets)
	require.Len(t, statuses, 2)
	assert.Equal(t, []string{"pi", "nas"}, []string{statuses[0].Name, statuses[1].Name})
	assert.Equal(t, "pi-commit", statuses[0].LastDeployedCommit)
	assert.Equal(t, "drifted", statuses[0].Drift.Status)
	assert.Equal(t, 1, statuses[0].Drift.Items)
	assert.Equal(t, StatusDegraded, statuses[0].Status)
	assert.Equal(t, "nas-commit", statuses[1].LastDeployedCommit)
	assert.Equal(t, "unavailable", statuses[1].Drift.Status)
	assert.Equal(t, remoteCheckedAt, statuses[1].Drift.CheckedAt, "remote unavailability must retain the cached successful sample time")
	assert.Equal(t, StatusUnavailable, statuses[1].Status)
	assert.Contains(t, statuses[1].Message, "remote target")
}

func TestTargetHealthStatuses_ReportsOpenBreakerWithoutChangingSibling(t *testing.T) {
	d, _ := targetStatusTestDaemon(t, []reconcile.Target{
		{Name: "healthy"},
		{Name: "blocked"},
	})
	targets := d.targets
	saveTargetState(t, d.config.ReconcileConfig, targets[0], &reconcile.DeployState{LastDeployedCommit: "good"})
	saveTargetState(t, d.config.ReconcileConfig, targets[1], &reconcile.DeployState{
		LastDeployedCommit:  "old",
		LastAttemptedCommit: "new",
		AttemptCount:        reconcile.MaxAttempts,
	})

	statuses := d.targetHealthStatuses(targets)
	require.Len(t, statuses, 2)
	assert.Equal(t, StatusClosed, statuses[0].CircuitBreaker.Status)
	assert.Equal(t, StatusOpen, statuses[1].CircuitBreaker.Status)
	assert.Equal(t, StatusHealthy, statuses[0].Status)
	assert.Equal(t, StatusDegraded, statuses[1].Status)
	assert.Equal(t, "circuit breaker is open", statuses[1].Message)

	state := reconcile.LoadState(d.config.ReconcileConfig.ConfigForTarget(targets[0]).StateFile)
	assert.Equal(t, "good", state.LastDeployedCommit)
}

func TestRunDriftCheckForTarget_UsesTargetProjectAndLeavesSiblingStateUntouched(t *testing.T) {
	d, _ := targetStatusTestDaemon(t, []reconcile.Target{
		{Name: "alpha", ProjectName: "stack-alpha"},
		{Name: "beta", ProjectName: "stack-beta"},
	})
	d.config.APITimeout = time.Second
	d.config.ReconcileConfig.RestartBreakerEnabled = false
	targets := d.targets
	saveTargetState(t, d.config.ReconcileConfig, targets[0], &reconcile.DeployState{
		LastDeployedCommit: "alpha-commit",
		DeclaredServices:   []reconcile.DeclaredService{{Name: "web", Image: "nginx:latest"}},
	})
	betaState := &reconcile.DeployState{
		LastDeployedCommit: "beta-commit",
		DeclaredServices:   []reconcile.DeclaredService{{Name: "api", Image: "api:latest"}},
	}
	betaPath := saveTargetState(t, d.config.ReconcileConfig, targets[1], betaState)
	before, err := os.ReadFile(betaPath)
	require.NoError(t, err)

	mock := dockertest.NewMockDockerAPI()
	mock.ContainerListFunc = func(_ context.Context, _ client.ContainerListOptions) (client.ContainerListResult, error) {
		return client.ContainerListResult{Items: []container.Summary{
			dockertest.MakeTestContainer("alpha-web", "stack-alpha-web-1", "nginx:latest", "running", "stack-alpha", "web"),
			dockertest.MakeTestContainer("beta-api", "stack-beta-api-1", "api:latest", "running", "stack-beta", "api"),
		}}, nil
	}
	dockerClient := docker.NewClientWithAPI(mock)

	d.runDriftCheckForTarget(context.Background(), targets[0], dockerClient)

	after, err := os.ReadFile(betaPath)
	require.NoError(t, err)
	assert.Equal(t, before, after, "a drift check for alpha must not rewrite beta's state file")
	alpha := reconcile.LoadState(d.config.ReconcileConfig.ConfigForTarget(targets[0]).StateFile)
	assert.Empty(t, alpha.DriftItems, "alpha should match the alpha-labeled container")
	assert.Equal(t, 1, mock.ContainerListCalls)
}

func TestRunDriftCheckForTarget_NamedBlankProjectUsesComposeNamespace(t *testing.T) {
	d, _ := targetStatusTestDaemon(t, []reconcile.Target{{Name: "alpha"}})
	d.config.ReconcileConfig.ProjectName = ""
	d.config.ReconcileConfig.RestartBreakerEnabled = true
	d.config.ReconcileConfig.RestartThreshold = 5
	target := d.targets[0]
	target.ProjectName = ""
	saveTargetState(t, d.config.ReconcileConfig, target, &reconcile.DeployState{
		LastDeployedCommit: "alpha-commit",
		DeclaredServices:   []reconcile.DeclaredService{{Name: "web", Image: "nginx:latest"}},
		RestartTracking: map[string]reconcile.RestartTrackingEntry{
			"api": {
				ContainerID:          "other-api-1-",
				RestartCount:         0,
				BaselineRestartCount: 0,
				BaselineAt:           time.Now().Add(-time.Minute),
				CheckedAt:            time.Now().Add(-time.Minute),
			},
		},
	})

	mock := dockertest.NewMockDockerAPI()
	mock.ContainerListFunc = func(ctx context.Context, _ client.ContainerListOptions) (client.ContainerListResult, error) {
		assert.NoError(t, ctx.Err())
		return client.ContainerListResult{Items: []container.Summary{
			dockertest.MakeTestContainer("compose-web", "compose-web-1", "nginx:latest", "running", "compose", "web"),
			// This restart-looping container belongs to an unrelated Compose
			// project and must not be inspected or stopped for alpha.
			dockertest.MakeTestContainer("unrelated-api", "other-api-1", "api:latest", "running", "other", "api"),
		}}, nil
	}
	mock.ContainerInspectFunc = func(ctx context.Context, id string, _ client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
		assert.NoError(t, ctx.Err())
		restartCount := 0
		if id == "other-api-1" {
			restartCount = 5
		}
		return client.ContainerInspectResult{Container: container.InspectResponse{
			ID:              id + "-long-id",
			Name:            "/" + id,
			Config:          &container.Config{Image: "nginx:latest"},
			State:           &container.State{Status: container.StateRunning},
			RestartCount:    restartCount,
			NetworkSettings: &container.NetworkSettings{},
		}}, nil
	}

	d.runDriftCheckForTarget(context.Background(), target, docker.NewClientWithAPI(mock))

	state := reconcile.LoadState(d.configForTarget(target).StateFile)
	assert.Empty(t, state.DriftItems, "the Compose-derived namespace should match the declared web service")
	assert.Equal(t, 1, mock.ContainerInspectCalls, "the unrelated project must not be inspected")
	assert.Equal(t, 0, mock.ContainerStopCalls, "the unrelated restart loop must not be stopped")
}

func TestRunDriftCheckForTarget_UnresolvedImplicitProjectFailsClosed(t *testing.T) {
	d, _ := targetStatusTestDaemon(t, []reconcile.Target{{Name: reconcile.DefaultTargetName}})
	d.config.ReconcileConfig.ProjectName = ""
	d.config.ReconcileConfig.RestartBreakerEnabled = true
	d.config.ReconcileConfig.RestartThreshold = 5
	target := d.targets[0]
	target.ProjectName = ""
	priorTracking := map[string]reconcile.RestartTrackingEntry{
		"web": {
			RestartCount:         0,
			BaselineRestartCount: 0,
			BaselineAt:           time.Now().Add(-time.Minute),
			CheckedAt:            time.Now().Add(-time.Minute),
		},
	}
	statePath := saveTargetState(t, d.config.ReconcileConfig, target, &reconcile.DeployState{
		LastDeployedCommit: "default-commit",
		DeclaredServices:   []reconcile.DeclaredService{{Name: "web", Image: "nginx:latest"}},
		RestartTracking:    priorTracking,
	})
	persistedPrior := reconcile.LoadState(statePath)

	mock := dockertest.NewMockDockerAPI()
	mock.ContainerListFunc = func(_ context.Context, _ client.ContainerListOptions) (client.ContainerListResult, error) {
		return client.ContainerListResult{
			Items: []container.Summary{
				dockertest.MakeTestContainer("foreign-web", "foreign-web-1", "nginx:latest", "running", "foreign", "web"),
			},
		}, nil
	}
	mock.ContainerInspectFunc = func(_ context.Context, _ string, _ client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
		t.Fatal("an unresolved implicit target must not inspect containers")
		return client.ContainerInspectResult{}, nil
	}

	d.runDriftCheckForTarget(context.Background(), target, docker.NewClientWithAPI(mock))

	state := reconcile.LoadState(statePath)
	assert.Equal(t, persistedPrior.RestartTracking["web"], state.RestartTracking["web"], "unresolved scope must preserve target restart tracking")
	assert.Equal(t, 0, mock.ContainerInspectCalls, "unresolved scope must not inspect host containers")
	assert.Equal(t, 0, mock.ContainerStopCalls, "unresolved scope must not stop host containers")
}

func TestRunDriftCheckForTarget_RestartBreakerMutatesOnlySelectedTarget(t *testing.T) {
	d, _ := targetStatusTestDaemon(t, []reconcile.Target{
		{Name: "alpha", ProjectName: "stack-alpha"},
		{Name: "beta", ProjectName: "stack-beta"},
	})
	d.config.APITimeout = time.Second
	d.config.ReconcileConfig.RestartThreshold = 5
	d.config.ReconcileConfig.RestartBreakerEnabled = true
	targets := d.targets
	// docker.Client.Inspect exposes the first 12 characters of the daemon's
	// inspect ID; keep the persisted baseline aligned with that representation.
	alphaID := "stack-alpha-"
	saveTargetState(t, d.config.ReconcileConfig, targets[0], &reconcile.DeployState{
		LastDeployedCommit: "alpha-commit",
		DeclaredServices:   []reconcile.DeclaredService{{Name: "web", Image: "nginx:latest"}},
		RestartTracking: map[string]reconcile.RestartTrackingEntry{
			"web": {
				ContainerID:          alphaID,
				RestartCount:         0,
				BaselineRestartCount: 0,
				BaselineAt:           time.Now().Add(-time.Minute),
				CheckedAt:            time.Now().Add(-time.Minute),
			},
		},
	})
	betaPath := saveTargetState(t, d.config.ReconcileConfig, targets[1], &reconcile.DeployState{
		LastDeployedCommit: "beta-commit",
		DeclaredServices:   []reconcile.DeclaredService{{Name: "web", Image: "nginx:latest"}},
	})
	before, err := os.ReadFile(betaPath)
	require.NoError(t, err)

	mock := dockertest.NewMockDockerAPI()
	mock.ContainerListFunc = func(_ context.Context, _ client.ContainerListOptions) (client.ContainerListResult, error) {
		return client.ContainerListResult{Items: []container.Summary{
			dockertest.MakeTestContainer(alphaID, "stack-alpha-web-1", "nginx:latest", "running", "stack-alpha", "web"),
			dockertest.MakeTestContainer("beta-container", "stack-beta-web-1", "nginx:latest", "running", "stack-beta", "web"),
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

	alpha := reconcile.LoadState(d.config.ReconcileConfig.ConfigForTarget(targets[0]).StateFile)
	require.Contains(t, alpha.RestartTracking, "web")
	assert.True(t, alpha.RestartTracking["web"].Tripped)
	assert.Equal(t, 1, mock.ContainerStopCalls, "only alpha's restart-looping container should be stopped")
	after, err := os.ReadFile(betaPath)
	require.NoError(t, err)
	assert.Equal(t, before, after, "the sibling target state must remain byte-for-byte unchanged")
}

func TestRunDriftCheck_AllRemoteTargetsSkipDockerAndPreserveState(t *testing.T) {
	d, _ := targetStatusTestDaemon(t, []reconcile.Target{
		{Name: "nas", TargetHost: "root@nas"},
		{Name: "pi", TargetHost: "root@pi"},
	})
	targets := d.targets
	paths := make([]string, 0, len(targets))
	before := make([][]byte, 0, len(targets))
	for _, target := range targets {
		path := saveTargetState(t, d.config.ReconcileConfig, target, &reconcile.DeployState{LastDeployedCommit: target.Name + "-commit"})
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		paths = append(paths, path)
		before = append(before, data)
	}
	mock := dockertest.NewMockDockerAPI()
	d.dockerClientOverride = docker.NewClientWithAPI(mock)

	d.runDriftCheck(context.Background())

	assert.Equal(t, 0, mock.ContainerListCalls, "remote periodic drift must not inspect the local Docker daemon")
	for i, path := range paths {
		after, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, before[i], after, "remote target %q state must remain unchanged", targets[i].Name)
		status, ok := d.targetRuntimeStatus(targets[i].Name)
		require.True(t, ok)
		assert.Equal(t, "unavailable", status.driftStatus)
	}
}

func TestRunDriftCheck_DisabledBaseStateSkipsNamedLocalAndRemoteTargets(t *testing.T) {
	root := t.TempDir()
	cfg := &reconcile.Config{
		RepoURL:    "https://example.test/repo.git",
		StateFile:  "",
		StagingDir: filepath.Join(root, "staging"),
		LockFile:   filepath.Join(root, "reconcile.lock"),
		Targets: []reconcile.Target{
			{Name: "local", ProjectName: "local-stack"},
			{Name: "remote", TargetHost: "root@remote", ProjectName: "remote-stack"},
		},
	}
	targets, err := cfg.ResolveTargets()
	require.NoError(t, err)
	d := &Daemon{
		config:          &Config{ReconcileConfig: cfg},
		targets:         targets,
		targetsResolved: true,
		targetRuntime:   make(map[string]targetRuntimeStatus),
		stopLoops:       make(chan struct{}),
	}
	mock := dockertest.NewMockDockerAPI()
	d.dockerClientOverride = docker.NewClientWithAPI(mock)

	d.runDriftCheck(context.Background())

	assert.Equal(t, 0, mock.ContainerListCalls, "an empty base state file disables drift before Docker inspection")
	assert.Empty(t, d.targetRuntime, "disabled drift must not create per-target runtime state")
	for _, target := range targets {
		assert.NoFileExists(t, d.configForTarget(target).StateFile, "disabled drift must not create derived target state")
	}
}

func TestTargetHealthStatuses_PreservesCachedTimestampOnLocalDriftFailure(t *testing.T) {
	d, _ := targetStatusTestDaemon(t, []reconcile.Target{{Name: "pi", ProjectName: "pi-stack"}})
	target := d.targets[0]
	cachedCheckedAt := time.Date(2026, 10, 14, 10, 4, 0, 0, time.UTC)
	saveTargetState(t, d.config.ReconcileConfig, target, &reconcile.DeployState{
		LastDeployedCommit: "pi-commit",
		DriftCheckedAt:     cachedCheckedAt,
		DeclaredServices:   []reconcile.DeclaredService{{Name: "web", Image: "nginx:latest"}},
	})
	mock := dockertest.NewMockDockerAPI()
	mock.ContainerListFunc = func(ctx context.Context, _ client.ContainerListOptions) (client.ContainerListResult, error) {
		assert.NoError(t, ctx.Err())
		return client.ContainerListResult{}, errors.New("docker list unavailable")
	}

	d.runDriftCheckForTarget(context.Background(), target, docker.NewClientWithAPI(mock))

	statuses := d.targetHealthStatuses(d.targets)
	require.Len(t, statuses, 1)
	assert.Equal(t, StatusUnavailable, statuses[0].Drift.Status)
	assert.Equal(t, cachedCheckedAt, statuses[0].Drift.CheckedAt, "a failed live sample must retain the cached successful timestamp")
}

func TestRunDriftCheck_FanoutUsesIndependentLocalNamespacesAndStateFiles(t *testing.T) {
	d, _ := targetStatusTestDaemon(t, []reconcile.Target{
		{Name: "alpha", ProjectName: "stack-alpha"},
		{Name: "beta", ProjectName: "stack-beta"},
	})
	d.config.ReconcileConfig.RestartBreakerEnabled = false
	targets := d.targets
	for _, target := range targets {
		saveTargetState(t, d.config.ReconcileConfig, target, &reconcile.DeployState{
			LastDeployedCommit: target.Name + "-commit",
			DeclaredServices:   []reconcile.DeclaredService{{Name: "web", Image: "nginx:latest"}},
		})
	}
	mock := dockertest.NewMockDockerAPI()
	mock.ContainerListFunc = func(ctx context.Context, _ client.ContainerListOptions) (client.ContainerListResult, error) {
		assert.NoError(t, ctx.Err())
		return client.ContainerListResult{Items: []container.Summary{
			dockertest.MakeTestContainer("alpha-web", "stack-alpha-web-1", "nginx:latest", "running", "stack-alpha", "web"),
			dockertest.MakeTestContainer("beta-web", "stack-beta-web-1", "nginx:latest", "running", "stack-beta", "web"),
		}}, nil
	}
	d.dockerClientOverride = docker.NewClientWithAPI(mock)

	d.runDriftCheck(context.Background())

	paths := make([]string, 0, len(targets))
	for _, target := range targets {
		targetCfg := d.configForTarget(target)
		paths = append(paths, targetCfg.StateFile)
		state := reconcile.LoadState(targetCfg.StateFile)
		assert.False(t, state.DriftCheckedAt.IsZero(), "target %q should receive its own drift result", target.Name)
	}
	assert.NotEqual(t, paths[0], paths[1], "target fanout must use independent state files")
	assert.Equal(t, 2, mock.ContainerListCalls, "each local target is inspected independently")
}

func TestConfigForTarget_PreservesHotReloadedProjectName(t *testing.T) {
	d, _ := targetStatusTestDaemon(t, []reconcile.Target{{Name: "pi", ProjectName: "startup-stack"}})
	target := d.targets[0]
	assert.Equal(t, "startup-stack", d.configForTarget(target).ProjectName)

	// This models ConfigReloader changing the effective project name during a
	// reconcile before its result is recorded for the next cycle.
	d.recordTargetProjectName(target.Name, "reloaded-stack")
	assert.Equal(t, "reloaded-stack", d.configForTarget(target).ProjectName)
	d.recordTargetRuntime(target.Name, "drifted", "", time.Now())
	runtimeStatus, ok := d.targetRuntimeStatus(target.Name)
	require.True(t, ok)
	assert.Equal(t, "reloaded-stack", runtimeStatus.projectName, "drift recording must merge, not erase the hot-reloaded project")
}

func TestHealthStatus_SingleNamedTargetIncludesTargetSubsystem(t *testing.T) {
	d, _ := targetStatusTestDaemon(t, []reconcile.Target{{Name: "nas", TargetHost: "root@nas", ProjectName: "nas-stack"}})
	saveTargetState(t, d.config.ReconcileConfig, d.targets[0], &reconcile.DeployState{LastDeployedCommit: "nas-commit"})

	status := d.HealthStatus()
	assert.Equal(t, StatusHealthy, status.Status, "remote target unavailability is detail, not daemon liveness")
	require.Len(t, status.Targets, 1)
	assert.Equal(t, "nas", status.Targets[0].Name)
	assert.Contains(t, status.Subsystems, "targets")
	assert.Equal(t, StatusUnavailable, status.Targets[0].Status)
}

func TestHealthHTTPKeepsTargetDetailsOutOfLiveness(t *testing.T) {
	t.Run("remote target is unavailable in detail but healthy at top level", func(t *testing.T) {
		d, _ := targetStatusTestDaemon(t, []reconcile.Target{{Name: "nas", TargetHost: "root@nas", ProjectName: "nas-stack"}})
		s := &Server{daemon: d}

		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		w := httptest.NewRecorder()
		s.handleHealth(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		status := requireBoundedHealthResponse(t, w.Body.Bytes())
		assert.Equal(t, StatusHealthy, status.Status)
		assert.Equal(t, StatusUnavailable, d.HealthStatus().Targets[0].Status)
	})

	t.Run("local drift is unavailable to liveness but remains detailed", func(t *testing.T) {
		d, _ := targetStatusTestDaemon(t, []reconcile.Target{{Name: "pi", ProjectName: "pi-stack"}})
		saveTargetState(t, d.config.ReconcileConfig, d.targets[0], &reconcile.DeployState{
			LastDeployedCommit: "pi-commit",
			DriftItems:         []reconcile.DriftItem{{Service: "web", Type: reconcile.DriftMissing}},
		})
		d.dockerClientOverride = docker.NewClientWithAPI(dockertest.NewMockDockerAPI())
		s := &Server{daemon: d}

		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		w := httptest.NewRecorder()
		s.handleHealth(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		status := requireBoundedHealthResponse(t, w.Body.Bytes())
		assert.Equal(t, StatusHealthy, status.Status)
		assert.Equal(t, StatusDegraded, d.HealthStatus().Targets[0].Status)
	})

	t.Run("real Docker fault still fails liveness", func(t *testing.T) {
		d, _ := targetStatusTestDaemon(t, []reconcile.Target{{Name: "pi", ProjectName: "pi-stack"}})
		d.dockerErr = errors.New("docker daemon not reachable")
		d.dockerOnce.Do(func() {})
		s := &Server{daemon: d}

		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		w := httptest.NewRecorder()
		s.handleHealth(w, req)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
		status := requireBoundedHealthResponse(t, w.Body.Bytes())
		assert.Equal(t, StatusUnhealthy, status.Status)
	})
}

func TestWidgetDataAggregatesLocalAndRemoteTargetStates(t *testing.T) {
	d, _ := targetStatusTestDaemon(t, []reconcile.Target{
		{Name: "alpha", ProjectName: "alpha-stack"},
		{Name: "beta", ProjectName: "beta-stack"},
		{Name: "nas", TargetHost: "root@nas", ProjectName: "nas-stack"},
	})
	alphaDeployed := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	betaDeployed := alphaDeployed.Add(time.Hour)
	remoteDeployed := betaDeployed.Add(time.Hour)
	saveTargetState(t, d.config.ReconcileConfig, d.targets[0], &reconcile.DeployState{
		DeployCount:        2,
		LastDeployedCommit: "alpha-commit-long",
		DeployedAt:         alphaDeployed,
	})
	saveTargetState(t, d.config.ReconcileConfig, d.targets[1], &reconcile.DeployState{
		DeployCount:        3,
		LastDeployedCommit: "beta-commit-long",
		DeployedAt:         betaDeployed,
	})
	saveTargetState(t, d.config.ReconcileConfig, d.targets[2], &reconcile.DeployState{
		DeployCount:        99,
		LastDeployedCommit: "remote-commit",
		DeployedAt:         remoteDeployed,
	})

	data := d.WidgetData()
	assert.Equal(t, 104, data["deploys_total"], "widget totals include cached state for remote targets")
	assert.Equal(t, remoteDeployed.Format(time.RFC3339), data["last_deploy"])
	assert.Equal(t, "remote-", data["git_sha"])
	assert.Len(t, data, 4, "Homepage widget schema remains flat")
}

func TestWidgetDataMissingTargetStateDoesNotFallBackToBaseState(t *testing.T) {
	d, _ := targetStatusTestDaemon(t, []reconcile.Target{{Name: "nas", ProjectName: "nas-stack"}})
	baseState := &reconcile.DeployState{
		DeployCount:        99,
		LastDeployedCommit: "base-commit",
		DeployedAt:         time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC),
	}
	require.NoError(t, reconcile.SaveState(d.config.ReconcileConfig.StateFile, baseState))

	data := d.WidgetData()
	assert.Equal(t, 0, data["deploys_total"], "a missing target state must not read the legacy base state")
	assert.Equal(t, "", data["last_deploy"])
	assert.Equal(t, "", data["git_sha"])
}

func TestWidgetDataInvalidNamedTopologyDoesNotFallBackToBaseState(t *testing.T) {
	root := t.TempDir()
	stateFile := filepath.Join(root, "base-state.json")
	require.NoError(t, reconcile.SaveState(stateFile, &reconcile.DeployState{
		DeployCount:        99,
		LastDeployedCommit: "base-commit",
		DeployedAt:         time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC),
	}))

	d := &Daemon{
		config: &Config{ReconcileConfig: &reconcile.Config{
			StateFile: stateFile,
			Targets: []reconcile.Target{
				{Name: "alpha", ProjectName: "alpha-stack"},
				{Name: "alpha", ProjectName: "alpha-other-stack"},
			},
		}},
	}

	data := d.WidgetData()
	assert.Equal(t, "error", data["status"])
	assert.Equal(t, 0, data["deploys_total"], "invalid named topology must not fall back to the base state")
	assert.Equal(t, "", data["last_deploy"])
	assert.Equal(t, "", data["git_sha"])
}

func TestWidgetDataPartialDefaultSurvivesCachedImplicitValidationError(t *testing.T) {
	root := t.TempDir()
	stateFile := filepath.Join(root, "base-state.json")
	require.NoError(t, reconcile.SaveState(stateFile, &reconcile.DeployState{
		DeployCount:        7,
		LastDeployedCommit: "legacy-commit",
		DeployedAt:         time.Date(2026, 10, 14, 13, 0, 0, 0, time.UTC),
	}))

	d := &Daemon{config: &Config{ReconcileConfig: &reconcile.Config{StateFile: stateFile}}}
	_, err := d.targetSnapshot()
	require.Error(t, err, "partial implicit-default config should fail operational target validation")

	data := d.WidgetData()
	assert.Equal(t, "ok", data["status"])
	assert.Equal(t, 7, data["deploys_total"])
	assert.Equal(t, "2026-10-14T13:00:00Z", data["last_deploy"])
	assert.Equal(t, "legacy-", data["git_sha"])
}

func TestRunDriftCheck_BaseStateFileDisablesTargetFanout(t *testing.T) {
	d, _ := targetStatusTestDaemon(t, []reconcile.Target{
		{Name: "local", ProjectName: "local-stack"},
		{Name: "remote", TargetHost: "root@remote", ProjectName: "remote-stack"},
	})
	d.config.ReconcileConfig.StateFile = ""
	mock := dockertest.NewMockDockerAPI()
	d.dockerClientOverride = docker.NewClientWithAPI(mock)

	d.runDriftCheck(context.Background())

	assert.Equal(t, 0, mock.ContainerListCalls, "the explicit base-state opt-out must skip Docker before target fanout")
	assert.Equal(t, 0, mock.PingCalls, "the explicit base-state opt-out must skip Docker before target fanout")
}

func TestTargetHealthStatuses_UsesExplicitDefaultStateOverride(t *testing.T) {
	d, _ := targetStatusTestDaemon(t, []reconcile.Target{{
		Name:      "default",
		StateFile: filepath.Join(t.TempDir(), "overridden-state.json"),
	}})
	state := &reconcile.DeployState{LastDeployedCommit: "override-commit"}
	path := saveTargetState(t, d.config.ReconcileConfig, d.targets[0], state)
	assert.NotEqual(t, d.config.ReconcileConfig.StateFile, path)

	statuses := d.targetHealthStatuses(d.targets)
	require.Len(t, statuses, 1)
	assert.Equal(t, "override-commit", statuses[0].LastDeployedCommit)
}

func TestTargetSnapshot_IsStableAfterConfigMutation(t *testing.T) {
	d, _ := targetStatusTestDaemon(t, []reconcile.Target{{Name: "pi"}, {Name: "nas"}})
	d.config.ReconcileConfig.Targets[0].Name = "changed"
	d.config.ReconcileConfig.Targets = []reconcile.Target{{Name: "other"}}

	targets, err := d.targetSnapshot()
	require.NoError(t, err)
	assert.Equal(t, []string{"pi", "nas"}, []string{targets[0].Name, targets[1].Name})
}

func TestDetailedTargetStatuses_OmitsImplicitDefault(t *testing.T) {
	d, _ := targetStatusTestDaemon(t, nil)
	assert.Empty(t, d.detailedTargetStatuses())
}

func TestPublicHealthResponse_OmitsTargetDetails(t *testing.T) {
	status := HealthStatus{
		Status: StatusDegraded,
		Ready:  true,
		Targets: []TargetHealthStatus{{
			Name:               "secret-target",
			LastDeployedCommit: "secret-commit",
			Message:            "/srv/private",
		}},
	}
	body, err := json.Marshal(publicHealthResponse(status))
	require.NoError(t, err)
	assert.NotContains(t, string(body), "secret-target")
	assert.NotContains(t, string(body), "secret-commit")
	assert.NotContains(t, string(body), "private")
}

func TestSocketStatus_DefaultShapeDoesNotPingDocker(t *testing.T) {
	d, _ := targetStatusTestDaemon(t, nil)
	mock := dockertest.NewMockDockerAPI()
	d.dockerClientOverride = docker.NewClientWithAPI(mock)
	server := &SocketServer{daemon: d}

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	w := httptest.NewRecorder()
	server.handleStatus(w, req)

	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &fields))
	assert.ElementsMatch(t, []string{"state", "uptime"}, mapKeys(fields))
	assert.Equal(t, 0, mock.PingCalls, "operator socket status must use cached target state and avoid a Docker health ping")
}

func TestAPIStatus_IncludesOrderedTargetState(t *testing.T) {
	d, _ := targetStatusTestDaemon(t, []reconcile.Target{{Name: "alpha"}, {Name: "beta"}})
	for _, target := range d.targets {
		saveTargetState(t, d.config.ReconcileConfig, target, &reconcile.DeployState{
			LastDeployedCommit: target.Name + "-commit",
		})
	}
	mock := dockertest.NewMockDockerAPI()
	d.dockerClientOverride = docker.NewClientWithAPI(mock)

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()
	d.handleAPIStatus(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var response APIStatusResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&response))
	require.Len(t, response.Targets, 2)
	assert.Equal(t, []string{"alpha", "beta"}, []string{response.Targets[0].Name, response.Targets[1].Name})
	assert.Equal(t, "alpha-commit", response.Targets[0].LastDeployedCommit)
	assert.Equal(t, 1, mock.PingCalls, "API status should perform one live Docker health check")
}
