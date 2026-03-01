package daemon

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/cameronsjo/bosun/internal/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startSocketDaemonForClient starts a socket daemon and returns a ready Client.
func startSocketDaemonForClient(t *testing.T) (*Client, func()) {
	t.Helper()
	d, socketPath := startDaemonSocket(t)

	errCh := make(chan error, 1)
	go func() {
		errCh <- d.socketServer.Start()
	}()

	require.Eventually(t, func() bool {
		resp, err := httpGetOverSocket(socketPath, "/health")
		if err != nil {
			return false
		}
		resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 2*time.Second, 10*time.Millisecond, "socket server should be ready")

	client := NewClient(socketPath)

	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = d.socketServer.Shutdown(ctx)
	}

	return client, cleanup
}

func TestSocketClient_Health(t *testing.T) {
	client, cleanup := startSocketDaemonForClient(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	health, err := client.Health(ctx)
	require.NoError(t, err)
	assert.Equal(t, "healthy", health.Status)
}

func TestSocketClient_Status(t *testing.T) {
	client, cleanup := startSocketDaemonForClient(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	status, err := client.Status(ctx)
	require.NoError(t, err)
	assert.Equal(t, "idle", status.State)
}

func TestSocketClient_Trigger(t *testing.T) {
	client, cleanup := startSocketDaemonForClient(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.Trigger(ctx, "test", false)
	require.NoError(t, err)
	assert.Equal(t, "accepted", resp.Status)
}

func TestSocketClient_Config(t *testing.T) {
	client, cleanup := startSocketDaemonForClient(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg, err := client.Config(ctx)
	require.NoError(t, err)
	// Config should return the configured repo URL.
	assert.Equal(t, "https://github.com/test/repo", cfg.RepoURL)
}

func TestSocketClient_Ping(t *testing.T) {
	client, cleanup := startSocketDaemonForClient(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := client.Ping(ctx)
	require.NoError(t, err)
}

func TestTCPClient_Health(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir = evalSymlinks(t, tmpDir)
	token := "tcp-test-token"

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

	require.Eventually(t, func() bool {
		conn, err := net.DialTimeout("tcp", actualAddr, time.Second)
		if err != nil {
			return false
		}
		conn.Close()
		return true
	}, 2*time.Second, 10*time.Millisecond)

	client := NewTCPClient(actualAddr, token)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	health, err := client.Health(ctx)
	require.NoError(t, err)
	assert.Equal(t, "healthy", health.Status)
}

func TestTCPClient_WrongToken(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir = evalSymlinks(t, tmpDir)
	token := "correct-token-rt"

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

	require.Eventually(t, func() bool {
		conn, err := net.DialTimeout("tcp", actualAddr, time.Second)
		if err != nil {
			return false
		}
		conn.Close()
		return true
	}, 2*time.Second, 10*time.Millisecond)

	client := NewTCPClient(actualAddr, "wrong-token")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = client.Trigger(ctx, "test", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), fmt.Sprintf("%d", http.StatusUnauthorized))
}

func TestTCPClient_ConfigBlocked(t *testing.T) {
	// Config endpoint is blocked client-side for TCP clients.
	client := NewTCPClient("127.0.0.1:9999", "token")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.Config(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "security restriction")
}
