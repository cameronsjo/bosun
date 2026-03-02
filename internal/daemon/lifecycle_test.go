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
	t.Cleanup(func() { os.RemoveAll(dir) })
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
	defer resp.Body.Close()
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
		resp.Body.Close()
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
		conn.Close()
		return true
	}, 2*time.Second, 10*time.Millisecond, "TCP server should be listening")

	// Health is public (no auth required).
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://%s/health", actualAddr))
	require.NoError(t, err)
	defer resp.Body.Close()
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
		conn.Close()
		return true
	}, 2*time.Second, 10*time.Millisecond)

	client := &http.Client{Timeout: 5 * time.Second}

	t.Run("no token returns 401", func(t *testing.T) {
		resp, err := client.Get(fmt.Sprintf("http://%s/status", actualAddr))
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("wrong token returns 401", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s/status", actualAddr), nil)
		req.Header.Set("Authorization", "Bearer wrong-token")
		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("correct token returns 200", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s/status", actualAddr), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
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
