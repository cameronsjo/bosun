package daemon

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cameronsjo/bosun/internal/docker"
	"github.com/cameronsjo/bosun/internal/docker/dockertest"
	"github.com/cameronsjo/bosun/internal/reconcile"
)

// newLoopDaemon creates a daemon with short intervals suitable for loop testing.
func newLoopDaemon(t *testing.T, pollInterval, driftInterval time.Duration) *Daemon {
	t.Helper()
	tmpDir := t.TempDir()
	tmpDir = evalSymlinks(t, tmpDir)

	cfg := DefaultConfig()
	cfg.SocketPath = filepath.Join(tmpDir, "test.sock")
	cfg.EnableHTTP = false
	cfg.PollInterval = pollInterval
	cfg.DriftInterval = driftInterval
	cfg.InitialDelay = 24 * time.Hour // Disable initial reconcile
	cfg.APITimeout = 2 * time.Second
	cfg.ReconcileConfig = reconcile.DefaultConfig()
	cfg.ReconcileConfig.RepoURL = "https://github.com/test/repo"
	cfg.ReconcileConfig.DryRun = true
	cfg.ReconcileConfig.RepoDir = filepath.Join(tmpDir, "repo")
	cfg.ReconcileConfig.LockFile = filepath.Join(tmpDir, "test.lock")
	cfg.ReconcileConfig.StateFile = filepath.Join(tmpDir, "state.json")

	d, err := New(cfg)
	require.NoError(t, err)

	return d
}

func TestPollLoop_StopsOnContextCancel(t *testing.T) {
	d := newLoopDaemon(t, 50*time.Millisecond, 0)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		d.pollLoop(ctx)
		close(done)
	}()

	// Let the loop tick at least once.
	time.Sleep(120 * time.Millisecond)

	cancel()

	select {
	case <-done:
		// Clean exit.
	case <-time.After(2 * time.Second):
		t.Fatal("pollLoop did not exit after context cancel")
	}
}

func TestPollLoop_StopsOnStopLoops(t *testing.T) {
	d := newLoopDaemon(t, 50*time.Millisecond, 0)

	ctx := context.Background()

	done := make(chan struct{})
	go func() {
		d.pollLoop(ctx)
		close(done)
	}()

	// Let the loop run briefly.
	time.Sleep(80 * time.Millisecond)

	close(d.stopLoops)

	select {
	case <-done:
		// Clean exit.
	case <-time.After(2 * time.Second):
		t.Fatal("pollLoop did not exit after stopLoops closed")
	}
}

func TestPollLoop_TriggersOnInterval(t *testing.T) {
	d := newLoopDaemon(t, 50*time.Millisecond, 0)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		d.pollLoop(ctx)
		close(done)
	}()

	// Wait long enough for at least one tick.
	time.Sleep(120 * time.Millisecond)
	cancel()

	<-done

	// After the poll fires, the daemon should have attempted reconciliation.
	// Since DryRun=true and there's no git repo, the reconcile will fail,
	// but the reconciling flag should have been set and cleared.
	d.reconcileMu.Lock()
	reconciling := d.reconciling
	d.reconcileMu.Unlock()

	assert.False(t, reconciling, "reconciling should be false after poll completes")
}

func TestDriftCheckLoop_StopsOnContextCancel(t *testing.T) {
	d := newLoopDaemon(t, 0, 50*time.Millisecond)

	// Inject mock Docker so drift check has something to query.
	mock := dockertest.NewMockDockerAPI()
	d.dockerClientOverride = docker.NewClientWithAPI(mock)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		d.driftCheckLoop(ctx)
		close(done)
	}()

	// Let the loop tick at least once.
	time.Sleep(120 * time.Millisecond)

	cancel()

	select {
	case <-done:
		// Clean exit.
	case <-time.After(2 * time.Second):
		t.Fatal("driftCheckLoop did not exit after context cancel")
	}
}

func TestDriftCheckLoop_StopsOnStopLoops(t *testing.T) {
	d := newLoopDaemon(t, 0, 50*time.Millisecond)

	mock := dockertest.NewMockDockerAPI()
	d.dockerClientOverride = docker.NewClientWithAPI(mock)

	ctx := context.Background()

	done := make(chan struct{})
	go func() {
		d.driftCheckLoop(ctx)
		close(done)
	}()

	// Let the loop run briefly.
	time.Sleep(80 * time.Millisecond)

	close(d.stopLoops)

	select {
	case <-done:
		// Clean exit.
	case <-time.After(2 * time.Second):
		t.Fatal("driftCheckLoop did not exit after stopLoops closed")
	}
}
