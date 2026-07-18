package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cameronsjo/bosun/internal/docker"
	"github.com/cameronsjo/bosun/internal/docker/dockertest"
	"github.com/cameronsjo/bosun/internal/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shortSocketDir creates a short temp directory for Unix socket tests.
// macOS limits socket paths to 104 bytes; t.TempDir() + EvalSymlinks
// produces paths over 100 chars, so we use os.MkdirTemp with a short prefix.
func shortSocketDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "bs")
	require.NoError(t, err)
	dir = evalSymlinks(t, dir)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// startDaemonSocket starts a daemon with a real Unix socket listener.
// Returns daemon, socket path, and a cancel func. Registers cleanup.
func startDaemonSocket(t *testing.T) (*Daemon, string) {
	t.Helper()
	tmpDir := shortSocketDir(t)

	socketPath := filepath.Join(tmpDir, "d.sock")

	cfg := DefaultConfig()
	cfg.SocketPath = socketPath
	cfg.EnableHTTP = false
	cfg.EnableTCP = false
	cfg.PollInterval = 0 // Disable polling
	cfg.DriftInterval = 0
	cfg.InitialDelay = 24 * time.Hour // Effectively disable initial reconcile
	cfg.ReconcileConfig = reconcile.DefaultConfig()
	cfg.ReconcileConfig.RepoURL = "https://github.com/test/repo"
	cfg.ReconcileConfig.DryRun = true
	cfg.ReconcileConfig.RepoDir = filepath.Join(tmpDir, "repo")
	cfg.ReconcileConfig.LockFile = filepath.Join(tmpDir, "test.lock")
	cfg.ReconcileConfig.StateFile = filepath.Join(tmpDir, "state.json")

	d, err := New(cfg)
	require.NoError(t, err)

	// Inject mock Docker client so health checks report docker as healthy.
	d.dockerClientOverride = docker.NewClientWithAPI(&dockertest.MockDockerAPI{})

	return d, socketPath
}

// httpGetOverSocket sends an HTTP GET to the given path over a Unix socket.
func httpGetOverSocket(socketPath, path string) (*http.Response, error) {
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", socketPath)
			},
		},
		Timeout: 5 * time.Second,
	}
	return client.Get("http://localhost" + path)
}

func TestSocketLifecycle_StartAcceptShutdown(t *testing.T) {
	d, socketPath := startDaemonSocket(t)

	// Start socket server in background.
	errCh := make(chan error, 1)
	go func() {
		errCh <- d.socketServer.Start()
	}()

	// Wait for socket file to appear.
	require.Eventually(t, func() bool {
		_, err := os.Stat(socketPath)
		return err == nil
	}, 2*time.Second, 10*time.Millisecond, "socket file should be created")

	// Send HTTP GET /health over Unix socket.
	resp, err := httpGetOverSocket(socketPath, "/health")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var health HealthStatus
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&health))
	assert.Equal(t, "healthy", health.Status)

	// Graceful shutdown.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, d.socketServer.Shutdown(ctx))

	// Socket file should be cleaned up.
	_, err = os.Stat(socketPath)
	assert.True(t, os.IsNotExist(err), "socket file should be removed after shutdown")

	// Server.Start returns http.ErrServerClosed on clean shutdown.
	select {
	case err := <-errCh:
		assert.ErrorIs(t, err, http.ErrServerClosed)
	case <-time.After(2 * time.Second):
		t.Fatal("Start() did not return after Shutdown()")
	}
}

func TestSocketLifecycle_StaleSocketCleanup(t *testing.T) {
	d, socketPath := startDaemonSocket(t)

	// Create a stale socket file.
	require.NoError(t, os.MkdirAll(filepath.Dir(socketPath), 0755))
	require.NoError(t, os.WriteFile(socketPath, []byte("stale"), 0600))

	// Start should succeed, removing the stale file.
	errCh := make(chan error, 1)
	go func() {
		errCh <- d.socketServer.Start()
	}()

	// Wait for the socket to be functional.
	require.Eventually(t, func() bool {
		resp, err := httpGetOverSocket(socketPath, "/health")
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 2*time.Second, 10*time.Millisecond, "server should start despite stale socket")

	// Clean shutdown.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, d.socketServer.Shutdown(ctx))
}

func TestTCPLifecycle_StartAcceptShutdown(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir = evalSymlinks(t, tmpDir)
	token := "test-bearer-token"

	cfg := DefaultConfig()
	cfg.SocketPath = filepath.Join(tmpDir, "test.sock")
	cfg.EnableHTTP = false
	cfg.EnableTCP = true
	cfg.TCPAddr = "127.0.0.1:0" // OS picks a free port
	cfg.BearerToken = token
	cfg.PollInterval = 0
	cfg.DriftInterval = 0
	cfg.InitialDelay = 24 * time.Hour
	cfg.ReconcileConfig = reconcile.DefaultConfig()
	cfg.ReconcileConfig.RepoURL = "https://github.com/test/repo"
	cfg.ReconcileConfig.DryRun = true
	cfg.ReconcileConfig.RepoDir = filepath.Join(tmpDir, "repo")
	cfg.ReconcileConfig.LockFile = filepath.Join(tmpDir, "test.lock")
	cfg.ReconcileConfig.StateFile = filepath.Join(tmpDir, "state.json")

	d, err := New(cfg)
	require.NoError(t, err)

	// Inject mock Docker client so health checks report docker as healthy.
	d.dockerClientOverride = docker.NewClientWithAPI(&dockertest.MockDockerAPI{})

	// Pre-bind listener so we can get the actual port.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	actualAddr := listener.Addr().String()
	d.tcpServer.listener = listener

	// Start TCP server in background.
	errCh := make(chan error, 1)
	go func() {
		errCh <- d.tcpServer.httpServer.Serve(listener)
	}()

	// Wait for server to be ready; surface startup errors immediately.
	require.Eventually(t, func() bool {
		select {
		case err := <-errCh:
			require.NoError(t, err, "TCP server failed to start")
			return false
		default:
		}
		conn, err := net.DialTimeout("tcp", actualAddr, time.Second)
		if err != nil {
			return false
		}
		_ = conn.Close()
		return true
	}, 2*time.Second, 10*time.Millisecond, "TCP server should be listening")

	// Health is public (no auth required).
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://%s/health", actualAddr))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Graceful shutdown.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, d.tcpServer.Shutdown(ctx))
}

func TestTCPLifecycle_AuthRequired(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir = evalSymlinks(t, tmpDir)
	token := "correct-token"

	cfg := DefaultConfig()
	cfg.SocketPath = filepath.Join(tmpDir, "test.sock")
	cfg.EnableHTTP = false
	cfg.EnableTCP = true
	cfg.TCPAddr = "127.0.0.1:0"
	cfg.BearerToken = token
	cfg.PollInterval = 0
	cfg.DriftInterval = 0
	cfg.InitialDelay = 24 * time.Hour
	cfg.ReconcileConfig = reconcile.DefaultConfig()
	cfg.ReconcileConfig.RepoURL = "https://github.com/test/repo"
	cfg.ReconcileConfig.DryRun = true
	cfg.ReconcileConfig.RepoDir = filepath.Join(tmpDir, "repo")
	cfg.ReconcileConfig.LockFile = filepath.Join(tmpDir, "test.lock")
	cfg.ReconcileConfig.StateFile = filepath.Join(tmpDir, "state.json")

	d, err := New(cfg)
	require.NoError(t, err)

	// Pre-bind listener.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	actualAddr := listener.Addr().String()
	d.tcpServer.listener = listener

	go func() { _ = d.tcpServer.httpServer.Serve(listener) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = d.tcpServer.Shutdown(ctx)
	})

	// Wait for server to be ready.
	require.Eventually(t, func() bool {
		conn, err := net.DialTimeout("tcp", actualAddr, time.Second)
		if err != nil {
			return false
		}
		_ = conn.Close()
		return true
	}, 2*time.Second, 10*time.Millisecond)

	client := &http.Client{Timeout: 5 * time.Second}

	t.Run("no token returns 401", func(t *testing.T) {
		resp, err := client.Get(fmt.Sprintf("http://%s/status", actualAddr))
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("wrong token returns 401", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s/status", actualAddr), nil)
		req.Header.Set("Authorization", "Bearer wrong-token")
		resp, err := client.Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("correct token returns 200", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s/status", actualAddr), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}

// TestDaemonRun_RecoversPanicInSynchronousBody is a #364 review follow-up:
// Run's original top-level "defer sentrypkg.Recover()" swallowed a panic
// raised in Run's own synchronous body -- before any goroutine is even
// spawned -- and returned nil. The caller's ui.Fatal never fired, the
// process exited 0, and a supervisor keyed on a nonzero exit code would
// never restart a daemon that never actually came up. A zero-value Daemon
// (nil config) panics immediately at the first synchronous call
// (warnWebhookAuthPosture dereferences d.config), standing in for any
// startup-time panic before the goroutines further down are ever spawned.
func TestDaemonRun_RecoversPanicInSynchronousBody(t *testing.T) {
	d := &Daemon{}

	err := d.Run(context.Background())

	require.Error(t, err, "a panic in Run's synchronous body must surface as a non-nil error, not exit silently as nil")
	assert.Contains(t, err.Error(), "panicked")
}

func TestDaemonRun_CancelledContext(t *testing.T) {
	tmpDir := shortSocketDir(t)

	cfg := DefaultConfig()
	cfg.SocketPath = filepath.Join(tmpDir, "d.sock")
	cfg.EnableHTTP = false
	cfg.EnableTCP = false
	cfg.PollInterval = 0
	cfg.DriftInterval = 0
	cfg.InitialDelay = 24 * time.Hour
	cfg.ShutdownTimeout = 2 * time.Second
	cfg.ReconcileConfig = reconcile.DefaultConfig()
	cfg.ReconcileConfig.RepoURL = "https://github.com/test/repo"
	cfg.ReconcileConfig.DryRun = true
	cfg.ReconcileConfig.RepoDir = filepath.Join(tmpDir, "repo")
	cfg.ReconcileConfig.LockFile = filepath.Join(tmpDir, "test.lock")
	cfg.ReconcileConfig.StateFile = filepath.Join(tmpDir, "state.json")

	d, err := New(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- d.Run(ctx)
	}()

	// Give Run a moment to start servers.
	time.Sleep(100 * time.Millisecond)

	// Cancel immediately.
	cancel()

	// Run should return within ShutdownTimeout.
	select {
	case err := <-done:
		// nil is fine — clean shutdown from context cancellation.
		_ = err
	case <-time.After(10 * time.Second):
		t.Fatal("Run() did not return after context cancellation")
	}
}

// TestDaemonRun_InitialReconcileFailure_StaysNotReady is the regression test
// for #346: the initial-reconcile goroutine used to call setReady(true)
// unconditionally, so /ready reported healthy even after the very first
// reconcile failed outright. There is no reachable git repo here, so the
// initial reconcile is guaranteed to fail; IsReady() must stay false.
func TestDaemonRun_InitialReconcileFailure_StaysNotReady(t *testing.T) {
	tmpDir := shortSocketDir(t)

	cfg := DefaultConfig()
	cfg.SocketPath = filepath.Join(tmpDir, "d.sock")
	cfg.EnableHTTP = false
	cfg.EnableTCP = false
	cfg.PollInterval = 0
	cfg.DriftInterval = 0
	cfg.InitialDelay = 10 * time.Millisecond
	cfg.ShutdownTimeout = 2 * time.Second
	cfg.ReconcileConfig = reconcile.DefaultConfig()
	cfg.ReconcileConfig.RepoURL = "https://github.com/test/repo"
	cfg.ReconcileConfig.DryRun = true
	cfg.ReconcileConfig.RepoDir = filepath.Join(tmpDir, "repo")
	cfg.ReconcileConfig.LockFile = filepath.Join(tmpDir, "test.lock")
	cfg.ReconcileConfig.StateFile = filepath.Join(tmpDir, "state.json")

	d, err := New(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- d.Run(ctx)
	}()

	require.Eventually(t, func() bool {
		_, lastErr := d.LastReconcile()
		return lastErr != nil
	}, 10*time.Second, 20*time.Millisecond, "initial reconcile did not complete with an error")

	assert.False(t, d.IsReady(), "daemon must not report ready after a failed initial reconcile")

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not return after context cancellation")
	}
}

// daemonSuccessGitOps is a reconcile.GitOperations stub that always reports a
// clean, up-to-date sync, letting a test drive a full successful reconcile
// pipeline without needing a reachable remote repository.
type daemonSuccessGitOps struct{}

func (daemonSuccessGitOps) Sync(context.Context) (bool, string, string, error) {
	return false, "abc123", "abc123", nil
}

func (daemonSuccessGitOps) IsRepo(context.Context) bool { return true }

func (daemonSuccessGitOps) DiffFiles(context.Context, string, string) ([]string, error) {
	return nil, nil
}

// TestDaemonRun_LaterReconcileSucceeds_BecomesReady is the follow-up
// regression test for #346: readiness must reflect "has ever successfully
// reconciled", not just "the initial boot reconcile succeeded". After a
// failed initial reconcile leaves the daemon not-ready, a later reconcile
// (poll/webhook/manual trigger) that succeeds must flip IsReady() to true --
// setReady(true) has to live at a single choke point every trigger path runs
// through (TriggerReconcile), not only in the initial-reconcile goroutine.
func TestDaemonRun_LaterReconcileSucceeds_BecomesReady(t *testing.T) {
	tmpDir := shortSocketDir(t)

	cfg := DefaultConfig()
	cfg.SocketPath = filepath.Join(tmpDir, "d.sock")
	cfg.EnableHTTP = false
	cfg.EnableTCP = false
	cfg.PollInterval = 0
	cfg.DriftInterval = 0
	cfg.InitialDelay = 10 * time.Millisecond
	cfg.ShutdownTimeout = 2 * time.Second
	cfg.ReconcileConfig = reconcile.DefaultConfig()
	cfg.ReconcileConfig.RepoURL = "https://github.com/test/repo"
	cfg.ReconcileConfig.DryRun = true
	cfg.ReconcileConfig.AllowEmptyDeclaredState = true
	cfg.ReconcileConfig.RepoDir = filepath.Join(tmpDir, "repo")
	cfg.ReconcileConfig.StagingDir = filepath.Join(tmpDir, "staging")
	cfg.ReconcileConfig.LocalAppdataPath = filepath.Join(tmpDir, "appdata")
	cfg.ReconcileConfig.InfraSubDir = "unraid"
	cfg.ReconcileConfig.LockFile = filepath.Join(tmpDir, "test.lock")
	cfg.ReconcileConfig.StateFile = filepath.Join(tmpDir, "state.json")
	require.NoError(t, os.MkdirAll(cfg.ReconcileConfig.LocalAppdataPath, 0755))

	d, err := New(cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- d.Run(ctx)
	}()

	// Phase 1: the initial reconcile fails against the unreachable repo, same
	// as TestDaemonRun_InitialReconcileFailure_StaysNotReady.
	require.Eventually(t, func() bool {
		_, lastErr := d.LastReconcile()
		return lastErr != nil
	}, 10*time.Second, 20*time.Millisecond, "initial reconcile did not complete with an error")
	assert.False(t, d.IsReady(), "must not be ready after the failed initial reconcile")

	// Phase 2: seed the compose fixture the pipeline needs past the git-sync
	// step, inject a GitOps stub that always succeeds, and drive a later
	// reconcile directly -- simulating an operator fixing connectivity and a
	// subsequent poll/webhook landing.
	composeDir := filepath.Join(cfg.ReconcileConfig.RepoDir, cfg.ReconcileConfig.InfraSubDir, "compose")
	require.NoError(t, os.MkdirAll(composeDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(composeDir, "stub.yml"),
		[]byte("services:\n  stub:\n    image: alpine:latest\n"), 0644))

	d.reconcileOpts = append(d.reconcileOpts, reconcile.WithGitOperations(daemonSuccessGitOps{}))
	require.NoError(t, d.TriggerReconcile(ctx, "manual-followup", false))

	assert.True(t, d.IsReady(), "must become ready once a later reconcile succeeds, even after a failed initial reconcile")

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not return after context cancellation")
	}
}
