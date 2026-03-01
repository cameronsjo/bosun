package daemon

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

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
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)
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
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

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
