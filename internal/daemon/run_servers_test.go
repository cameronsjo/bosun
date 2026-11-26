package daemon

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cameronsjo/bosun/internal/reconcile"
)

// loopbackAddr returns a 127.0.0.1 address that was free a moment ago.
func loopbackAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())
	return addr
}

// occupiedLoopbackAddr returns an address something is already listening on, so
// a server that binds it fails with "address already in use".
func occupiedLoopbackAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	return ln.Addr().String()
}

func portOf(t *testing.T, addr string) int {
	t.Helper()
	_, portStr, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	var port int
	_, err = fmt.Sscanf(portStr, "%d", &port)
	require.NoError(t, err)
	return port
}

// allServersDaemon builds a daemon with the Unix socket, TCP and HTTP servers
// all enabled on loopback, and no reconcile or background loops.
func allServersDaemon(t *testing.T, tcpAddr, httpAddr string) *Daemon {
	t.Helper()
	tmpDir := shortSocketDir(t)

	cfg := DefaultConfig()
	cfg.SocketPath = filepath.Join(tmpDir, "d.sock")
	cfg.EnableTCP = true
	cfg.TCPAddr = tcpAddr
	cfg.BearerToken = "tok"
	cfg.EnableHTTP = true
	cfg.ListenAddr = "127.0.0.1"
	cfg.Port = portOf(t, httpAddr)
	cfg.PollInterval = 0
	cfg.DriftInterval = 0
	cfg.InitialDelay = 24 * time.Hour
	cfg.ShutdownTimeout = 2 * time.Second
	cfg.ReconcileConfig = reconcile.DefaultConfig()
	cfg.ReconcileConfig.RepoURL = "https://github.com/test/repo"
	cfg.ReconcileConfig.DryRun = true
	cfg.ReconcileConfig.RepoDir = filepath.Join(tmpDir, "repo")
	cfg.ReconcileConfig.LockFile = privateLockPath(t, tmpDir, "test.lock")
	cfg.ReconcileConfig.StateFile = filepath.Join(tmpDir, "state.json")

	d, err := New(cfg)
	require.NoError(t, err)
	return d
}

// healthUp reports whether the server at url answers HTTP at all. A degraded
// status (503, no reconcile has run) still means the server is serving.
func healthUp(url string) bool {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := (&http.Client{Timeout: time.Second}).Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return true
}

// TestRun_TCPAndHTTPServers_CleanShutdown starts all three servers, confirms the
// TCP and HTTP ones serve, then cancels. A graceful stop is not an error and
// logs no start failure for any server.
func TestRun_TCPAndHTTPServers_CleanShutdown(t *testing.T) {
	logs := captureDaemonLogs(t)
	tcpAddr, httpAddr := loopbackAddr(t), loopbackAddr(t)
	d := allServersDaemon(t, tcpAddr, httpAddr)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()

	require.Eventually(t, func() bool {
		return healthUp("http://"+tcpAddr+"/health") && healthUp("http://"+httpAddr+"/health")
	}, 5*time.Second, 20*time.Millisecond, "TCP and HTTP servers never came up")

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("Run() did not return after context cancellation")
	}

	messages := logs.messages()
	assert.Contains(t, messages, "Initiating graceful shutdown")
	assert.NotContains(t, messages, "Failed to start Unix socket server")
	assert.NotContains(t, messages, "Failed to start TCP server")
	assert.NotContains(t, messages, "Failed to start HTTP server")
}

// TestRun_ServerBindFailure_NamesTheServer is the positive control for the
// ErrServerClosed filter: a real bind failure on the TCP or HTTP server still
// logs and still fails Run, naming the server.
func TestRun_ServerBindFailure_NamesTheServer(t *testing.T) {
	tests := []struct {
		name       string
		occupy     func(t *testing.T) (tcpAddr, httpAddr string)
		wantErr    string
		wantLogMsg string
	}{
		{
			name: "TCP address in use",
			occupy: func(t *testing.T) (string, string) {
				return occupiedLoopbackAddr(t), loopbackAddr(t)
			},
			wantErr:    "TCP server",
			wantLogMsg: "Failed to start TCP server",
		},
		{
			name: "HTTP port in use",
			occupy: func(t *testing.T) (string, string) {
				return loopbackAddr(t), occupiedLoopbackAddr(t)
			},
			wantErr:    "HTTP server",
			wantLogMsg: "Failed to start HTTP server",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := captureDaemonLogs(t)
			tcpAddr, httpAddr := tt.occupy(t)
			d := allServersDaemon(t, tcpAddr, httpAddr)

			done := make(chan error, 1)
			go func() { done <- d.Run(context.Background()) }()

			select {
			case err := <-done:
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.wantErr)
			case <-time.After(10 * time.Second):
				t.Fatal("Run() did not fail on a server that cannot bind")
			}

			assert.Contains(t, logs.messages(), tt.wantLogMsg)
		})
	}
}
