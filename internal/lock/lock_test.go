package lock

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	lock := New("/tmp/test", "provision")
	assert.Equal(t, filepath.Join("/tmp/test", ".bosun", "locks", "provision.lock"), lock.path)
}

func TestLock_AcquireRelease(t *testing.T) {
	tmpDir := t.TempDir()
	lock := New(tmpDir, "test")
	lockPath := filepath.Join(tmpDir, ".bosun", "locks", "test.lock")

	require.NoError(t, lock.Acquire())

	lockedInfo, err := os.Stat(lockPath)
	require.NoError(t, err)
	pid, err := os.ReadFile(lockPath)
	require.NoError(t, err)
	assert.Equal(t, strconv.Itoa(os.Getpid())+"\n", string(pid))

	require.NoError(t, lock.Release())

	releasedInfo, err := os.Stat(lockPath)
	require.NoError(t, err)
	assert.True(t, os.SameFile(lockedInfo, releasedInfo), "release must preserve the lock inode")

	next := New(tmpDir, "test")
	require.NoError(t, next.Acquire())
	t.Cleanup(func() { require.NoError(t, next.Release()) })
	reacquiredInfo, err := os.Stat(lockPath)
	require.NoError(t, err)
	assert.True(t, os.SameFile(lockedInfo, reacquiredInfo), "reacquire must use the same lock inode")
}

func TestLock_Contention(t *testing.T) {
	tmpDir := t.TempDir()
	lock1 := New(tmpDir, "test")
	lock2 := New(tmpDir, "test")

	require.NoError(t, lock1.Acquire())
	t.Cleanup(func() { require.NoError(t, lock1.Release()) })

	err := lock2.Acquire()
	assert.EqualError(t, err, "another test operation is already running")
}

func TestLock_SameInstanceReacquirePreservesHandle(t *testing.T) {
	tmpDir := t.TempDir()
	lock := New(tmpDir, "test")
	require.NoError(t, lock.Acquire())
	heldFile := lock.file

	err := lock.Acquire()
	assert.EqualError(t, err, "another test operation is already running")
	assert.Same(t, heldFile, lock.file)
	require.NoError(t, lock.Release())

	competitor := New(tmpDir, "test")
	require.NoError(t, competitor.Acquire())
	require.NoError(t, competitor.Release())
}

func TestLock_ReleaseWithoutAcquire(t *testing.T) {
	tmpDir := t.TempDir()
	lock := New(tmpDir, "test")

	require.NoError(t, lock.Release())
}

func TestLock_AcquireFilesystemErrors(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(t *testing.T) *Lock
		wantError string
	}{
		{
			name: "create lock directory",
			setup: func(t *testing.T) *Lock {
				manifestPath := filepath.Join(t.TempDir(), "manifest-file")
				require.NoError(t, os.WriteFile(manifestPath, []byte("not a directory"), 0600))
				return New(manifestPath, "test")
			},
			wantError: "create lock directory",
		},
		{
			name: "open lock file",
			setup: func(t *testing.T) *Lock {
				lock := New(t.TempDir(), "test")
				require.NoError(t, os.MkdirAll(lock.path, 0700))
				return lock
			},
			wantError: "open lock file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.setup(t).Acquire()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantError)
		})
	}
}

func TestLock_UnexpectedPlatformErrors(t *testing.T) {
	t.Run("acquire preserves cause and closes handle", func(t *testing.T) {
		lock := New(t.TempDir(), "test")
		injectedErr := errors.New("injected acquire failure")
		lock.ops.lock = func(*os.File) error { return injectedErr }

		err := lock.Acquire()
		require.Error(t, err)
		assert.ErrorIs(t, err, injectedErr)
		assert.Nil(t, lock.file)
	})

	t.Run("release preserves cause and clears handle", func(t *testing.T) {
		tmpDir := t.TempDir()
		lock := New(tmpDir, "test")
		require.NoError(t, lock.Acquire())
		injectedErr := errors.New("injected release failure")
		lock.ops.unlock = func(*os.File) error { return injectedErr }

		err := lock.Release()
		require.Error(t, err)
		assert.ErrorIs(t, err, injectedErr)
		assert.Nil(t, lock.file)

		competitor := New(tmpDir, "test")
		require.NoError(t, competitor.Acquire(), "closing the handle must release the OS lock")
		require.NoError(t, competitor.Release())
	})
}

func TestWithLock(t *testing.T) {
	callbackErr := errors.New("callback failure")
	releaseErr := errors.New("release failure")
	tests := []struct {
		name              string
		callbackErr       error
		releaseErr        error
		wantCallbackCause bool
		wantReleaseCause  bool
	}{
		{name: "success"},
		{name: "callback error", callbackErr: callbackErr, wantCallbackCause: true},
		{name: "release error", releaseErr: releaseErr, wantReleaseCause: true},
		{name: "callback and release errors", callbackErr: callbackErr, releaseErr: releaseErr, wantCallbackCause: true, wantReleaseCause: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lock := New(t.TempDir(), "test")
			if tt.releaseErr != nil {
				lock.ops.unlock = func(*os.File) error { return tt.releaseErr }
			}
			executed := false
			err := withLock(lock, func() error {
				executed = true
				return tt.callbackErr
			})

			assert.True(t, executed)
			assert.Equal(t, tt.wantCallbackCause, errors.Is(err, callbackErr))
			assert.Equal(t, tt.wantReleaseCause, errors.Is(err, releaseErr))
		})
	}
}

func TestWithLock_Blocked(t *testing.T) {
	tmpDir := t.TempDir()
	lock := New(tmpDir, "test")

	require.NoError(t, lock.Acquire())
	t.Cleanup(func() { require.NoError(t, lock.Release()) })

	executed := false
	err := WithLock(tmpDir, "test", func() error {
		executed = true
		return nil
	})
	assert.EqualError(t, err, "another test operation is already running")
	assert.False(t, executed)
}

func TestLock_CrossProcessContention(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tmpDir := t.TempDir()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLockHelperProcess$")
	cmd.Env = append(os.Environ(), "BOSUN_LOCK_HELPER_DIR="+tmpDir)
	stdin, err := cmd.StdinPipe()
	require.NoError(t, err)
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	cmd.Stderr = os.Stderr
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
	})

	ready := make(chan error, 1)
	go func() {
		line := make([]byte, len("LOCKED\n"))
		_, readErr := io.ReadFull(stdout, line)
		if readErr == nil && string(line) != "LOCKED\n" {
			readErr = fmt.Errorf("unexpected helper output %q", line)
		}
		ready <- readErr
	}()
	select {
	case err := <-ready:
		require.NoError(t, err)
	case <-ctx.Done():
		require.Fail(t, "lock helper did not become ready")
	}

	contender := New(tmpDir, "test")
	assert.EqualError(t, contender.Acquire(), "another test operation is already running")

	require.NoError(t, stdin.Close())
	require.NoError(t, cmd.Wait())
}

func TestLockHelperProcess(t *testing.T) {
	tmpDir := os.Getenv("BOSUN_LOCK_HELPER_DIR")
	if tmpDir == "" {
		t.Skip("helper process only")
	}

	lock := New(tmpDir, "test")
	require.NoError(t, lock.Acquire())
	defer func() { require.NoError(t, lock.Release()) }()
	_, err := fmt.Fprintln(os.Stdout, "LOCKED")
	require.NoError(t, err)
	_, err = io.Copy(io.Discard, os.Stdin)
	require.NoError(t, err)
}
