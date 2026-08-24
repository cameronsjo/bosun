package daemon

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
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

func TestDaemonRunDriftCheck_SkipsWhenReconciling(t *testing.T) {
	mock := dockertest.NewMockDockerAPI()
	d := newDockerDaemon(t, mock)

	// Pretend a reconcile is in progress.
	d.reconcileMu.Lock()
	d.reconciling = true
	d.reconcileMu.Unlock()

	// Run drift check — should skip because reconciling is true.
	d.runDriftCheck(context.Background())

	assert.Equal(t, 0, mock.ContainerListCalls, "Docker should not be called while reconciling")

	// Clean up.
	d.reconcileMu.Lock()
	d.reconciling = false
	d.reconcileMu.Unlock()
}

func TestDaemonRunDriftCheck_UpdatesStateFile(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir = evalSymlinks(t, tmpDir)
	stateFile := filepath.Join(tmpDir, "state.json")

	// Pre-populate state with declared services so drift check runs.
	state := &reconcile.DeployState{
		LastDeployedCommit: "abc123",
		DeclaredServices: []reconcile.DeclaredService{
			{Name: "web", Image: "nginx:latest"},
			{Name: "api", Image: "myapp:v1"},
		},
	}
	if err := reconcile.SaveState(stateFile, state); err != nil {
		t.Fatalf("Failed to write state: %v", err)
	}

	// Build daemon with proper config.
	cfg := DefaultConfig()
	cfg.SocketPath = filepath.Join(tmpDir, "test.sock")
	cfg.EnableHTTP = false
	cfg.PollInterval = 0
	cfg.DriftInterval = 5 * time.Minute
	cfg.APITimeout = 5 * time.Second
	cfg.ReconcileConfig = reconcile.DefaultConfig()
	cfg.ReconcileConfig.RepoURL = "https://github.com/test/repo"
	cfg.ReconcileConfig.DryRun = true
	cfg.ReconcileConfig.RepoDir = filepath.Join(tmpDir, "repo")
	cfg.ReconcileConfig.LockFile = filepath.Join(tmpDir, "test.lock")
	cfg.ReconcileConfig.StateFile = stateFile
	cfg.ReconcileConfig.ProjectName = "myapp"

	d, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create daemon: %v", err)
	}

	// Inject Docker mock — returns empty containers (no matching services running).
	d.dockerClientOverride = docker.NewClientWithAPI(dockertest.NewMockDockerAPI())

	// Run drift check.
	d.runDriftCheck(context.Background())

	// Reload state and verify drift was recorded.
	updatedState := reconcile.LoadState(stateFile)
	assert.False(t, updatedState.DriftCheckedAt.IsZero(), "DriftCheckedAt should be set after drift check")
}

func TestDaemonRunDriftCheck_NoStateFile(t *testing.T) {
	mock := dockertest.NewMockDockerAPI()

	tmpDir := t.TempDir()
	tmpDir = evalSymlinks(t, tmpDir)

	cfg := DefaultConfig()
	cfg.SocketPath = filepath.Join(tmpDir, "test.sock")
	cfg.EnableHTTP = false
	cfg.PollInterval = 0
	cfg.DriftInterval = 5 * time.Minute
	cfg.APITimeout = 5 * time.Second
	cfg.ReconcileConfig = reconcile.DefaultConfig()
	cfg.ReconcileConfig.RepoURL = "https://github.com/test/repo"
	cfg.ReconcileConfig.DryRun = true
	cfg.ReconcileConfig.RepoDir = filepath.Join(tmpDir, "repo")
	cfg.ReconcileConfig.LockFile = filepath.Join(tmpDir, "test.lock")
	cfg.ReconcileConfig.StateFile = "" // No state file
	cfg.ReconcileConfig.ProjectName = "myapp"

	d, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create daemon: %v", err)
	}
	d.dockerClientOverride = docker.NewClientWithAPI(mock)

	// Should return early without panicking — stateFile is empty.
	d.runDriftCheck(context.Background())

	// Docker should never be called.
	assert.Equal(t, 0, mock.ContainerListCalls)
}

func TestDaemonRunDriftCheck_SelfHealBudgetPersistsAndExhaustsOnce(t *testing.T) {
	provider := &testAlertProvider{}
	d := newAlertDaemon(t, provider)
	d.dockerClientOverride = docker.NewClientWithAPI(dockertest.NewMockDockerAPI())
	d.config.ReconcileConfig.RestartBreakerEnabled = false
	d.config.DriftSelfHeal = reconcile.NewConfigField(true)
	d.config.DriftSelfHealCooldown = reconcile.NewConfigField(time.Nanosecond)
	d.config.MaxSelfHealAttempts = 2
	var triggerCount atomic.Int32
	d.triggerReconcileFn = func(context.Context, string, bool) error {
		triggerCount.Add(1)
		return nil
	}

	stateFile := d.config.ReconcileConfig.StateFile
	restartEntry := reconcile.RestartTrackingEntry{
		RestartCount: 7,
		ContainerID:  "restart-breaker-identity",
		Tripped:      true,
	}
	require.NoError(t, reconcile.SaveState(stateFile, &reconcile.DeployState{
		LastDeployedCommit: "abc123",
		DeclaredServices: []reconcile.DeclaredService{
			{Name: "api", Image: "api:latest"},
		},
		RestartTracking: map[string]reconcile.RestartTrackingEntry{"api": restartEntry},
	}))

	for attempt := 1; attempt <= 2; attempt++ {
		d.runDriftCheck(context.Background())
		d.wg.Wait() // no background reconcile may outlive the temp directory
		loaded := reconcile.LoadState(stateFile)
		require.NotNil(t, loaded.DriftSelfHeal)
		assert.Equal(t, attempt, loaded.DriftSelfHeal.Attempts)
		assert.False(t, loaded.DriftSelfHeal.Exhausted)
		assert.Equal(t, restartEntry, loaded.RestartTracking["api"])
	}

	d.runDriftCheck(context.Background())
	d.wg.Wait()
	loaded := reconcile.LoadState(stateFile)
	require.NotNil(t, loaded.DriftSelfHeal)
	assert.Equal(t, 2, loaded.DriftSelfHeal.Attempts)
	assert.True(t, loaded.DriftSelfHeal.Exhausted)
	assert.True(t, loaded.DriftSelfHeal.ExhaustedAlerted)

	d.runDriftCheck(context.Background())
	d.wg.Wait()
	var exhaustionAlerts int
	for _, got := range provider.alerts {
		if got.Source == "self-heal-exhausted" {
			exhaustionAlerts++
		}
	}
	assert.Equal(t, 1, exhaustionAlerts)
	assert.Equal(t, int32(2), triggerCount.Load(), "only the configured number of reconciliations may be triggered")
}

func TestDaemonRunDriftCheck_ConcurrentCallsSerializeStateUpdates(t *testing.T) {
	mock := dockertest.NewMockDockerAPI()
	d := newDockerDaemon(t, mock)
	d.config.ReconcileConfig.RestartBreakerEnabled = false
	stateFile := d.config.ReconcileConfig.StateFile
	require.NoError(t, reconcile.SaveState(stateFile, &reconcile.DeployState{
		LastDeployedCommit: "abc123",
		DeclaredServices: []reconcile.DeclaredService{
			{Name: "api", Image: "api:latest"},
		},
	}))

	const callers = 8
	var wg sync.WaitGroup
	wg.Add(callers)
	for range callers {
		go func() {
			defer wg.Done()
			d.runDriftCheck(context.Background())
		}()
	}
	wg.Wait()

	assert.Equal(t, callers, mock.ContainerListCalls)
	loaded := reconcile.LoadState(stateFile)
	assert.False(t, loaded.DriftCheckedAt.IsZero())
}

func TestDaemonRunDriftCheck_BlocksReconcileAdmissionForStateCycle(t *testing.T) {
	enteredDocker := make(chan struct{})
	releaseDocker := make(chan struct{})
	mock := dockertest.NewMockDockerAPI()
	mock.ContainerListFunc = func(ctx context.Context, _ client.ContainerListOptions) (client.ContainerListResult, error) {
		close(enteredDocker)
		select {
		case <-releaseDocker:
			return client.ContainerListResult{Items: []container.Summary{}}, nil
		case <-ctx.Done():
			return client.ContainerListResult{}, ctx.Err()
		}
	}
	d := newDockerDaemon(t, mock)
	d.config.ReconcileConfig.RestartBreakerEnabled = false
	stateFile := d.config.ReconcileConfig.StateFile
	require.NoError(t, reconcile.SaveState(stateFile, &reconcile.DeployState{
		LastDeployedCommit: "abc123",
		DeclaredServices: []reconcile.DeclaredService{
			{Name: "api", Image: "api:latest"},
		},
	}))

	done := make(chan struct{})
	go func() {
		defer close(done)
		d.runDriftCheck(context.Background())
	}()
	<-enteredDocker

	if d.reconcileMu.TryLock() {
		d.reconcileMu.Unlock()
		close(releaseDocker)
		<-done
		t.Fatal("reconcile admission was not held across the drift state cycle")
	}
	close(releaseDocker)
	<-done
}

func TestDaemonRunDriftCheck_ShutdownDoesNotConsumeSelfHealAttempt(t *testing.T) {
	d := newDockerDaemon(t, dockertest.NewMockDockerAPI())
	d.config.ReconcileConfig.RestartBreakerEnabled = false
	d.config.DriftSelfHeal = reconcile.NewConfigField(true)
	d.config.DriftSelfHealCooldown = reconcile.NewConfigField(time.Nanosecond)
	d.config.MaxSelfHealAttempts = 1
	stateFile := d.config.ReconcileConfig.StateFile
	require.NoError(t, reconcile.SaveState(stateFile, &reconcile.DeployState{
		LastDeployedCommit: "abc123",
		DeclaredServices: []reconcile.DeclaredService{
			{Name: "api", Image: "api:latest"},
		},
	}))

	d.cancelLifecycle()
	d.runDriftCheck(context.Background())

	loaded := reconcile.LoadState(stateFile)
	assert.Nil(t, loaded.DriftSelfHeal, "shutdown-rejected work is not a self-heal attempt")
	assert.NotEmpty(t, loaded.DriftItems, "ordinary drift state updates must still persist")
}
