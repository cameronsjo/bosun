//go:build windows

package daemon

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// Windows does not enforce Unix permission bits on AF_UNIX socket paths.
// Keep the configured mode visible to os.Stat where supported, while access
// control remains the platform ACL and the daemon's request authorization.
func listenUnixSocket(socketPath string, socketMode os.FileMode) (net.Listener, *socketOwnership, error) {
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
	return listener, &socketOwnership{file: info}, nil
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
	return removeSocketIfSame(socketPath, &socketOwnership{file: info})
}

func removeSocketIfSame(socketPath string, owned *socketOwnership) error {
	if owned == nil || owned.file == nil {
		return nil
	}
	current, err := os.Lstat(socketPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect socket during cleanup: %w", err)
	}
	if !os.SameFile(current, owned.file) || current.Mode().Type() != owned.file.Mode().Type() {
		return errSocketPathReplaced
	}

	quarantineDir, err := os.MkdirTemp(filepath.Dir(socketPath), ".bosun-socket-cleanup-")
	if err != nil {
		return fmt.Errorf("create private socket cleanup directory: %w", err)
	}
	removeQuarantine := true
	defer func() {
		if removeQuarantine {
			_ = os.RemoveAll(quarantineDir)
		}
	}()
	quarantinedPath := filepath.Join(quarantineDir, "entry")
	if err := moveFileNoReplace(socketPath, quarantinedPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("quarantine socket during cleanup: %w", err)
	}
	quarantined, err := os.Lstat(quarantinedPath)
	if err != nil {
		removeQuarantine = false
		return fmt.Errorf("inspect quarantined socket entry preserved at %s: %w", quarantinedPath, err)
	}
	if !os.SameFile(quarantined, owned.file) || quarantined.Mode().Type() != owned.file.Mode().Type() {
		if restoreErr := moveFileNoReplace(quarantinedPath, socketPath); restoreErr != nil {
			removeQuarantine = false
			return errors.Join(
				errSocketPathReplaced,
				fmt.Errorf("restore replacement entry (preserved at %s): %w", quarantinedPath, restoreErr),
			)
		}
		return errSocketPathReplaced
	}
	if err := os.Remove(quarantinedPath); err != nil && !os.IsNotExist(err) {
		removeQuarantine = false
		return fmt.Errorf("remove quarantined owned socket preserved at %s: %w", quarantinedPath, err)
	}
	return nil
}

// moveFileNoReplace uses MoveFile rather than os.Rename, whose Windows
// implementation replaces an existing destination.
func moveFileNoReplace(oldPath, newPath string) error {
	oldPtr, err := windows.UTF16PtrFromString(oldPath)
	if err != nil {
		return err
	}
	newPtr, err := windows.UTF16PtrFromString(newPath)
	if err != nil {
		return err
	}
	return windows.MoveFile(oldPtr, newPtr)
}
