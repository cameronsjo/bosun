//go:build !windows

package daemon

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
)

// listenUnixSocket binds the socket behind a private directory, applies the
// requested mode, and only then atomically publishes it at socketPath. This
// avoids both a permissive final-path window and process-global umask changes.
func listenUnixSocket(socketPath string, socketMode os.FileMode) (net.Listener, os.FileInfo, error) {
	socketDir := filepath.Dir(socketPath)
	stagingDir, err := os.MkdirTemp(socketDir, ".")
	if err != nil {
		return nil, nil, fmt.Errorf("create private socket staging directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(stagingDir) }()
	// MkdirTemp applies the process umask. Explicitly restore owner access while
	// retaining a private directory, without mutating the process-global umask.
	if err := os.Chmod(stagingDir, 0o700); err != nil {
		return nil, nil, fmt.Errorf("secure socket staging directory: %w", err)
	}

	stagingPath := filepath.Join(stagingDir, "s")
	listener, err := net.Listen("unix", stagingPath)
	if err != nil {
		return nil, nil, fmt.Errorf("bind staged Unix socket: %w", err)
	}
	keepListener := false
	defer func() {
		if !keepListener {
			_ = listener.Close()
		}
	}()
	if unixListener, ok := listener.(*net.UnixListener); ok {
		// The socket is renamed after bind, so the listener's original path is
		// no longer authoritative. SocketServer performs inode-checked cleanup.
		unixListener.SetUnlinkOnClose(false)
	}

	if err := os.Chmod(stagingPath, socketMode); err != nil {
		return nil, nil, fmt.Errorf("set staged Unix socket mode to %04o: %w", socketMode.Perm(), err)
	}
	stagedInfo, err := os.Lstat(stagingPath)
	if err != nil {
		return nil, nil, fmt.Errorf("inspect staged Unix socket: %w", err)
	}
	if stagedInfo.Mode()&os.ModeSocket == 0 {
		return nil, nil, fmt.Errorf("staged Unix socket path is not a socket: %s", stagingPath)
	}
	if stagedInfo.Mode().Perm() != socketMode.Perm() {
		return nil, nil, fmt.Errorf("staged Unix socket mode is %04o, expected %04o", stagedInfo.Mode().Perm(), socketMode.Perm())
	}

	if err := publishSocket(stagingPath, socketPath); err != nil {
		return nil, nil, fmt.Errorf("publish Unix socket atomically: %w", err)
	}
	publishedInfo, err := os.Lstat(socketPath)
	if err != nil {
		cleanupErr := removeSocketIfSame(socketPath, stagedInfo)
		return nil, nil, errors.Join(fmt.Errorf("inspect published Unix socket: %w", err), cleanupErr)
	}
	if !os.SameFile(stagedInfo, publishedInfo) {
		_ = removeSocketIfSame(socketPath, stagedInfo)
		return nil, nil, errSocketPathReplaced
	}

	keepListener = true
	return listener, publishedInfo, nil
}

func removeStaleSocket(socketPath string) error {
	info, err := os.Lstat(socketPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("refusing to remove non-socket entry at %s", socketPath)
	}
	return os.Remove(socketPath)
}

func removeSocketIfSame(socketPath string, owned os.FileInfo) error {
	current, err := os.Lstat(socketPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect socket during cleanup: %w", err)
	}
	if !os.SameFile(current, owned) {
		return errSocketPathReplaced
	}
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove owned socket: %w", err)
	}
	return nil
}
