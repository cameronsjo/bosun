package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cameronsjo/bosun/internal/log"
	"github.com/cameronsjo/bosun/internal/reconcile"
)

// syncBuffer is a bytes.Buffer safe for the concurrent writes zerolog makes from
// the server goroutines Run starts.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) messages() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var messages []string
	for _, line := range bytes.Split(bytes.TrimSpace(b.buf.Bytes()), []byte("\n")) {
		var entry map[string]any
		if json.Unmarshal(line, &entry) != nil {
			continue
		}
		if msg, ok := entry["message"].(string); ok {
			messages = append(messages, msg)
		}
	}
	return messages
}

func captureDaemonLogs(t *testing.T) *syncBuffer {
	t.Helper()
	buf := &syncBuffer{}
	log.Init(&log.Options{Format: log.FormatJSON, Output: buf})
	t.Cleanup(func() { log.Init(&log.Options{}) })
	return buf
}

// socketOnlyDaemon builds a daemon that starts only the Unix socket server and
// never reconciles, so Run's server goroutines are the only log source.
func socketOnlyDaemon(t *testing.T, socketPath string) *Daemon {
	t.Helper()
	tmpDir := shortSocketDir(t)

	cfg := DefaultConfig()
	cfg.SocketPath = socketPath
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
	cfg.ReconcileConfig.LockFile = privateLockPath(t, tmpDir, "test.lock")
	cfg.ReconcileConfig.StateFile = filepath.Join(tmpDir, "state.json")

	d, err := New(cfg)
	require.NoError(t, err)
	return d
}

// TestRun_CleanShutdownLogsNoStartFailure pins that a graceful shutdown does not
// log http.ErrServerClosed as "Failed to start Unix socket server". The server
// goroutines are joined before Run returns, so the assertion needs no settle
// time: on the original code the error line is already in the buffer.
func TestRun_CleanShutdownLogsNoStartFailure(t *testing.T) {
	logs := captureDaemonLogs(t)
	socketPath := filepath.Join(shortSocketDir(t), "d.sock")
	d := socketOnlyDaemon(t, socketPath)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()

	require.Eventually(t, func() bool {
		_, err := os.Lstat(socketPath)
		return err == nil
	}, 5*time.Second, 10*time.Millisecond, "socket server never published its socket")

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err, "a clean cancel is not an error")
	case <-time.After(10 * time.Second):
		t.Fatal("Run() did not return after context cancellation")
	}

	messages := logs.messages()
	assert.Contains(t, messages, "Initiating graceful shutdown", "the capture must see the shutdown path, or the absence below proves nothing")
	assert.NotContains(t, messages, "Failed to start Unix socket server", "ErrServerClosed after Shutdown is not a start failure")
}

// TestRun_SocketBindFailureStillLogged is the positive control: a real start
// failure keeps logging and keeps failing Run.
func TestRun_SocketBindFailureStillLogged(t *testing.T) {
	logs := captureDaemonLogs(t)

	parent := filepath.Join(shortSocketDir(t), "not-a-dir")
	require.NoError(t, os.WriteFile(parent, []byte("file"), 0o600))
	d := socketOnlyDaemon(t, filepath.Join(parent, "d.sock"))

	done := make(chan error, 1)
	go func() { done <- d.Run(context.Background()) }()

	select {
	case err := <-done:
		require.Error(t, err)
		assert.ErrorContains(t, err, "socket server")
	case <-time.After(10 * time.Second):
		t.Fatal("Run() did not fail on an unbindable socket path")
	}

	assert.Contains(t, logs.messages(), "Failed to start Unix socket server")
}

func TestServerExitError(t *testing.T) {
	bindErr := errors.New("listen tcp 127.0.0.1:9090: bind: address already in use")

	tests := []struct {
		name    string
		err     error
		wantNil bool
		wantIs  error
	}{
		{name: "nil", err: nil, wantNil: true},
		{name: "server closed", err: http.ErrServerClosed, wantNil: true},
		{name: "wrapped server closed", err: fmt.Errorf("serve: %w", http.ErrServerClosed), wantNil: true},
		{name: "bind failure is named and wrapped", err: bindErr, wantIs: bindErr},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := serverExitError("TCP server", tt.err)
			if tt.wantNil {
				assert.NoError(t, got)
				return
			}
			require.Error(t, got)
			assert.ErrorIs(t, got, tt.wantIs)
			assert.ErrorContains(t, got, "TCP server: ")
		})
	}
}
