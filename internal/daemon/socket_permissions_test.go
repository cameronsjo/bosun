//go:build !windows

package daemon

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSocketServerValidatesAndRetainsMode(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		cfg := DefaultSocketConfig()
		assert.Equal(t, DefaultSocketPath, cfg.SocketPath)
		assert.Equal(t, os.FileMode(0o660), cfg.SocketMode)

		server, err := NewSocketServer(nil, nil)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o660), server.socketMode)
	})

	t.Run("configured mode reaches daemon socket server", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.SocketPath = filepath.Join(shortSocketTestDir(t), "bosun.sock")
		cfg.SocketMode = 0o600

		d, err := New(cfg)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), d.socketServer.socketMode)
	})

	t.Run("empty path is rejected", func(t *testing.T) {
		_, err := NewSocketServer(nil, &SocketConfig{SocketMode: 0o600})
		require.ErrorContains(t, err, "socket path is required")
	})

	t.Run("non-permission mode bits are rejected", func(t *testing.T) {
		mode := os.FileMode(0o600) | os.ModeSetuid
		_, err := NewSocketServer(nil, &SocketConfig{SocketPath: "bosun.sock", SocketMode: mode})
		require.ErrorContains(t, err, "only permission bits")

		cfg := DefaultConfig()
		cfg.SocketMode = mode
		require.ErrorContains(t, ValidateConfig(cfg), "invalid socket mode")
	})
}

func TestListenUnixSocketPublishesConfiguredMode(t *testing.T) {
	dir := shortSocketTestDir(t)
	socketPath := filepath.Join(dir, "bosun.sock")

	listener, owned, err := listenUnixSocket(socketPath, 0o600)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = listener.Close()
		_ = removeSocketIfSame(socketPath, owned)
	})

	info, err := os.Lstat(socketPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	assert.NotZero(t, info.Mode()&os.ModeSocket)
	assert.True(t, os.SameFile(owned, info))

	conn, err := net.DialTimeout("unix", socketPath, time.Second)
	require.NoError(t, err, "the atomically renamed socket remains connectable")
	require.NoError(t, conn.Close())

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "private staging directory must be removed")
	assert.Equal(t, "bosun.sock", entries[0].Name())
}

func TestSocketServerFirstObservableModeIsConfigured(t *testing.T) {
	dir := shortSocketTestDir(t)
	socketPath := filepath.Join(dir, "bosun.sock")
	server, err := NewSocketServer(nil, &SocketConfig{SocketPath: socketPath, SocketMode: 0o600})
	require.NoError(t, err)

	observed := make(chan os.FileMode, 1)
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		for {
			info, statErr := os.Lstat(socketPath)
			if statErr == nil {
				observed <- info.Mode().Perm()
				return
			}
			if !os.IsNotExist(statErr) {
				return
			}
		}
	}()

	startErr := make(chan error, 1)
	go func() { startErr <- server.Start() }()

	select {
	case mode := <-observed:
		assert.Equal(t, os.FileMode(0o600), mode, "final path must never expose a permissive initial mode")
	case err := <-startErr:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("socket was not published")
	}
	<-watchDone
	require.ErrorContains(t, server.Start(), "already starting or running")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, server.Shutdown(ctx))
	require.ErrorIs(t, <-startErr, http.ErrServerClosed)
	_, err = os.Lstat(socketPath)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestSocketListenerPathErrorsDoNotDeleteUnrelatedFile(t *testing.T) {
	dir := shortSocketTestDir(t)
	parentFile := filepath.Join(dir, "parent")
	socketPath := filepath.Join(parentFile, "bosun.sock")
	require.NoError(t, os.WriteFile(parentFile, []byte("keep"), 0o600))

	server, err := NewSocketServer(nil, &SocketConfig{SocketPath: socketPath, SocketMode: 0o600})
	require.NoError(t, err)
	require.ErrorContains(t, server.Start(), "failed to create socket directory")

	listener, _, err := listenUnixSocket(socketPath, 0o600)
	require.ErrorContains(t, err, "create private socket staging directory")
	assert.Nil(t, listener)
	require.Error(t, removeStaleSocket(socketPath))
	parentInfo, statErr := os.Lstat(parentFile)
	require.NoError(t, statErr)
	require.Error(t, removeSocketIfSame(socketPath, parentInfo))

	contents, readErr := os.ReadFile(parentFile)
	require.NoError(t, readErr)
	assert.Equal(t, "keep", string(contents))
}

func TestSocketServerRefusesUnsafeStaleAndReplacementEntries(t *testing.T) {
	t.Run("regular file", func(t *testing.T) {
		dir := shortSocketTestDir(t)
		socketPath := filepath.Join(dir, "bosun.sock")
		require.NoError(t, os.WriteFile(socketPath, []byte("keep"), 0o600))
		server, err := NewSocketServer(nil, &SocketConfig{SocketPath: socketPath, SocketMode: 0o600})
		require.NoError(t, err)

		err = server.Start()
		require.ErrorContains(t, err, "refusing to remove non-socket")
		contents, readErr := os.ReadFile(socketPath)
		require.NoError(t, readErr)
		assert.Equal(t, "keep", string(contents))
	})

	t.Run("symlink", func(t *testing.T) {
		dir := shortSocketTestDir(t)
		target := filepath.Join(dir, "target")
		socketPath := filepath.Join(dir, "bosun.sock")
		require.NoError(t, os.WriteFile(target, []byte("keep"), 0o600))
		require.NoError(t, os.Symlink(target, socketPath))
		server, err := NewSocketServer(nil, &SocketConfig{SocketPath: socketPath, SocketMode: 0o600})
		require.NoError(t, err)

		err = server.Start()
		require.ErrorContains(t, err, "refusing to remove non-socket")
		contents, readErr := os.ReadFile(target)
		require.NoError(t, readErr)
		assert.Equal(t, "keep", string(contents))
	})

	t.Run("replacement during cleanup", func(t *testing.T) {
		dir := shortSocketTestDir(t)
		socketPath := filepath.Join(dir, "bosun.sock")
		listener, owned, err := listenUnixSocket(socketPath, 0o600)
		require.NoError(t, err)
		require.NoError(t, os.Remove(socketPath))
		require.NoError(t, os.WriteFile(socketPath, []byte("replacement"), 0o600))

		err = removeSocketIfSame(socketPath, owned)
		require.ErrorIs(t, err, errSocketPathReplaced)
		require.NoError(t, listener.Close(), "listener close must not unlink a replacement")
		contents, readErr := os.ReadFile(socketPath)
		require.NoError(t, readErr)
		assert.Equal(t, "replacement", string(contents))
	})
}

func TestRemoveSocketIfSameQuarantinesBeforeDeleting(t *testing.T) {
	dir := shortSocketTestDir(t)
	socketPath := filepath.Join(dir, "bosun.sock")
	listener, owned, err := listenUnixSocket(socketPath, 0o600)
	require.NoError(t, err)
	require.NoError(t, listener.Close())

	require.NoError(t, removeSocketIfSame(socketPath, owned))
	_, err = os.Lstat(socketPath)
	assert.ErrorIs(t, err, os.ErrNotExist)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries, "private cleanup quarantine must be removed after owned-socket deletion")
}

func TestRemoveSocketIfSamePreservesEntryWhenCleanupCannotBePrepared(t *testing.T) {
	dir := shortSocketTestDir(t)
	socketPath := filepath.Join(dir, "bosun.sock")
	listener, owned, err := listenUnixSocket(socketPath, 0o600)
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })

	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	err = removeSocketIfSame(socketPath, owned)
	require.ErrorContains(t, err, "create private socket cleanup directory")

	info, statErr := os.Lstat(socketPath)
	require.NoError(t, statErr)
	assert.True(t, os.SameFile(owned, info), "cleanup setup failure must leave the owned socket in place")
}

func TestRemoveSocketIfSameHandlesFilesystemRaces(t *testing.T) {
	t.Run("path disappears before quarantine", func(t *testing.T) {
		dir := shortSocketTestDir(t)
		socketPath := filepath.Join(dir, "bosun.sock")
		listener, owned, err := listenUnixSocket(socketPath, 0o600)
		require.NoError(t, err)
		require.NoError(t, listener.Close())

		err = removeSocketIfSameWithHooks(socketPath, owned, func(string) {
			require.NoError(t, os.Remove(socketPath))
		}, nil)
		require.NoError(t, err)
	})

	t.Run("rename failure leaves socket", func(t *testing.T) {
		dir := shortSocketTestDir(t)
		socketPath := filepath.Join(dir, "bosun.sock")
		listener, owned, err := listenUnixSocket(socketPath, 0o600)
		require.NoError(t, err)
		t.Cleanup(func() { _ = listener.Close() })

		err = removeSocketIfSameWithHooks(socketPath, owned, func(string) {
			require.NoError(t, os.Chmod(dir, 0o500))
		}, nil)
		require.ErrorContains(t, err, "quarantine socket during cleanup")
		require.NoError(t, os.Chmod(dir, 0o700))
		_, statErr := os.Lstat(socketPath)
		require.NoError(t, statErr)
	})

	t.Run("replacement is restored", func(t *testing.T) {
		dir := shortSocketTestDir(t)
		socketPath := filepath.Join(dir, "bosun.sock")
		listener, owned, err := listenUnixSocket(socketPath, 0o600)
		require.NoError(t, err)
		require.NoError(t, listener.Close())

		err = removeSocketIfSameWithHooks(socketPath, owned, func(string) {
			require.NoError(t, os.Remove(socketPath))
			require.NoError(t, os.WriteFile(socketPath, []byte("replacement"), 0o600))
		}, nil)
		require.ErrorIs(t, err, errSocketPathReplaced)
		contents, readErr := os.ReadFile(socketPath)
		require.NoError(t, readErr)
		assert.Equal(t, "replacement", string(contents))
	})

	t.Run("replacement remains quarantined when final path races", func(t *testing.T) {
		dir := shortSocketTestDir(t)
		socketPath := filepath.Join(dir, "bosun.sock")
		listener, owned, err := listenUnixSocket(socketPath, 0o600)
		require.NoError(t, err)
		require.NoError(t, listener.Close())
		var quarantinedPath string

		err = removeSocketIfSameWithHooks(socketPath, owned, func(string) {
			require.NoError(t, os.Remove(socketPath))
			require.NoError(t, os.WriteFile(socketPath, []byte("first replacement"), 0o600))
		}, func(path string) {
			quarantinedPath = path
			require.NoError(t, os.WriteFile(socketPath, []byte("racing replacement"), 0o600))
		})
		require.ErrorIs(t, err, errSocketPathReplaced)
		contents, readErr := os.ReadFile(socketPath)
		require.NoError(t, readErr)
		assert.Equal(t, "racing replacement", string(contents))
		contents, readErr = os.ReadFile(quarantinedPath)
		require.NoError(t, readErr)
		assert.Equal(t, "first replacement", string(contents))
		require.NoError(t, os.RemoveAll(filepath.Dir(quarantinedPath)))
	})

	t.Run("uninspectable quarantine is retained", func(t *testing.T) {
		dir := shortSocketTestDir(t)
		socketPath := filepath.Join(dir, "bosun.sock")
		listener, owned, err := listenUnixSocket(socketPath, 0o600)
		require.NoError(t, err)
		require.NoError(t, listener.Close())
		var quarantinedPath string

		err = removeSocketIfSameWithHooks(socketPath, owned, nil, func(path string) {
			quarantinedPath = path
			require.NoError(t, os.Remove(path))
		})
		require.ErrorContains(t, err, "inspect quarantined socket entry")
		_, statErr := os.Stat(filepath.Dir(quarantinedPath))
		require.NoError(t, statErr, "uncertain quarantine must be retained for inspection")
		require.NoError(t, os.RemoveAll(filepath.Dir(quarantinedPath)))
	})

	t.Run("delete failure retains owned quarantine", func(t *testing.T) {
		dir := shortSocketTestDir(t)
		socketPath := filepath.Join(dir, "bosun.sock")
		listener, owned, err := listenUnixSocket(socketPath, 0o600)
		require.NoError(t, err)
		require.NoError(t, listener.Close())
		var quarantinedPath string

		err = removeSocketIfSameWithHooks(socketPath, owned, nil, func(path string) {
			quarantinedPath = path
			require.NoError(t, os.Chmod(filepath.Dir(path), 0o500))
		})
		require.ErrorContains(t, err, "remove quarantined owned socket")
		require.NoError(t, os.Chmod(filepath.Dir(quarantinedPath), 0o700))
		_, statErr := os.Lstat(quarantinedPath)
		require.NoError(t, statErr)
		require.NoError(t, os.RemoveAll(filepath.Dir(quarantinedPath)))
	})
}

func TestListenUnixSocketCleansStagingAfterPublishError(t *testing.T) {
	dir := shortSocketTestDir(t)
	socketPath := filepath.Join(dir, strings.Repeat("x", 256))
	listener, _, err := listenUnixSocket(socketPath, 0o600)
	require.Error(t, err)
	assert.Nil(t, listener)

	entries, readErr := os.ReadDir(dir)
	require.NoError(t, readErr)
	assert.Empty(t, entries, "listener and private staging directory must be cleaned after failure")
}

func TestListenUnixSocketCleansStagingAfterBindError(t *testing.T) {
	dir := shortSocketTestDir(t)
	// Darwin and Linux cap AF_UNIX paths near 104/108 bytes. The filesystem
	// directory itself is valid, but the private staged socket path is not.
	longDir := filepath.Join(dir, strings.Repeat("x", 90))
	require.NoError(t, os.Mkdir(longDir, 0o700))

	listener, _, err := listenUnixSocket(filepath.Join(longDir, "bosun.sock"), 0o600)
	require.ErrorContains(t, err, "bind staged Unix socket")
	assert.Nil(t, listener)

	entries, readErr := os.ReadDir(longDir)
	require.NoError(t, readErr)
	assert.Empty(t, entries, "private staging directory must be cleaned after bind failure")
}

func TestPublishSocketDoesNotReplaceRacingEntry(t *testing.T) {
	dir := shortSocketTestDir(t)
	stagedPath := filepath.Join(dir, "staged")
	socketPath := filepath.Join(dir, "bosun.sock")
	require.NoError(t, os.WriteFile(stagedPath, []byte("staged"), 0o600))
	require.NoError(t, os.WriteFile(socketPath, []byte("replacement"), 0o600))

	err := publishSocket(stagedPath, socketPath)
	require.Error(t, err)
	contents, readErr := os.ReadFile(socketPath)
	require.NoError(t, readErr)
	assert.Equal(t, "replacement", string(contents))
	_, statErr := os.Lstat(stagedPath)
	assert.NoError(t, statErr, "failed publication must leave the private staged entry for deferred cleanup")
}

func TestListenUnixSocketConcurrentModes(t *testing.T) {
	dir := shortSocketTestDir(t)
	modes := []os.FileMode{0o000, 0o600, 0o620, 0o640, 0o660}
	var wg sync.WaitGroup
	errs := make(chan error, len(modes))
	for i, mode := range modes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			path := filepath.Join(dir, "bosun-"+string(rune('a'+i))+".sock")
			listener, owned, err := listenUnixSocket(path, mode)
			if err != nil {
				errs <- err
				return
			}
			defer func() {
				_ = listener.Close()
				_ = removeSocketIfSame(path, owned)
			}()
			info, err := os.Lstat(path)
			if err != nil {
				errs <- err
				return
			}
			if info.Mode().Perm() != mode {
				errs <- errors.New("published socket mode mismatch")
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
}

func shortSocketTestDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "bosun-socket-test-")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(dir)) })
	return dir
}
