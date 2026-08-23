//go:build windows

package daemon

import (
	"fmt"
	"net"
	"os"
)

// Windows does not enforce Unix permission bits on AF_UNIX socket paths.
// Keep the configured mode visible to os.Stat where supported, while access
// control remains the platform ACL and the daemon's request authorization.
func listenUnixSocket(socketPath string, socketMode os.FileMode) (net.Listener, os.FileInfo, error) {
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, nil, err
	}
	if unixListener, ok := listener.(*net.UnixListener); ok {
		// Keep shutdown cleanup ownership-checked. The default close behavior can
		// unlink a replacement created after publication.
		unixListener.SetUnlinkOnClose(false)
	}
	if err := os.Chmod(socketPath, socketMode); err != nil {
		_ = listener.Close()
		return nil, nil, fmt.Errorf("set Unix socket mode: %w", err)
	}
	info, err := os.Lstat(socketPath)
	if err != nil {
		_ = listener.Close()
		return nil, nil, fmt.Errorf("inspect Unix socket: %w", err)
	}
	return listener, info, nil
}

func removeStaleSocket(socketPath string) error {
	info, err := os.Lstat(socketPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
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
