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
func listenUnixSocket(socketPath string, socketMode os.FileMode) (net.Listener, *socketOwnership, error) {
	socketDir := filepath.Dir(socketPath)
	stagingDir, err := os.MkdirTemp(socketDir, ".")
	if err != nil {
		return nil, nil, fmt.Errorf("create private socket staging directory: %w", err)
	}
	keepStaging := false
	defer func() {
		if !keepStaging {
			_ = os.RemoveAll(stagingDir)
		}
	}()
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

	// Retain a private hard link for the listener lifetime. os.SameFile compares
	// device and inode, but an unlinked inode can be reused immediately (observed
	// on Linux). The anchor pins this inode, making later identity checks reliable.
	anchorPath := filepath.Join(stagingDir, "owner")
	if err := os.Link(stagingPath, anchorPath); err != nil {
		return nil, nil, fmt.Errorf("anchor staged Unix socket ownership: %w", err)
	}
	anchorInfo, err := os.Lstat(anchorPath)
	if err != nil {
		return nil, nil, fmt.Errorf("inspect Unix socket ownership anchor: %w", err)
	}
	owned := &socketOwnership{file: anchorInfo, anchorPath: anchorPath}

	if err := publishSocket(stagingPath, socketPath); err != nil {
		return nil, nil, fmt.Errorf("publish Unix socket atomically: %w", err)
	}
	publishedInfo, err := os.Lstat(socketPath)
	if err != nil {
		cleanupErr := removeSocketIfSame(socketPath, owned)
		return nil, nil, errors.Join(fmt.Errorf("inspect published Unix socket: %w", err), cleanupErr)
	}
	if !sameOwnedEntry(owned.file, publishedInfo) {
		_ = removeSocketIfSame(socketPath, owned)
		return nil, nil, errSocketPathReplaced
	}

	keepListener = true
	keepStaging = true
	owned.file = publishedInfo
	return listener, owned, nil
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
	return removeSocketIfSame(socketPath, &socketOwnership{file: info})
}

func removeSocketIfSame(socketPath string, owned *socketOwnership) error {
	return removeSocketIfSameWithHooks(socketPath, owned, nil, nil)
}

// removeSocketIfSameWithHooks exposes the two filesystem race boundaries to
// deterministic fault tests. Production always passes nil hooks.
func removeSocketIfSameWithHooks(
	socketPath string,
	owned *socketOwnership,
	beforeQuarantine func(string),
	afterQuarantine func(string),
) error {
	if owned == nil || owned.file == nil {
		return nil
	}
	releaseAnchor := false
	defer func() {
		if releaseAnchor && owned.anchorPath != "" {
			_ = os.RemoveAll(filepath.Dir(owned.anchorPath))
		}
	}()
	current, err := os.Lstat(socketPath)
	if os.IsNotExist(err) {
		releaseAnchor = true
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect socket during cleanup: %w", err)
	}
	if !sameOwnedEntry(owned.file, current) {
		releaseAnchor = true
		return errSocketPathReplaced
	}

	// A second Lstat followed by Remove would still have a check/use race: an
	// attacker could replace the name after SameFile and have that replacement
	// unlinked. Atomically move the current name into a private quarantine,
	// verify the inode that actually moved, and only then delete it.
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
	if err := os.Chmod(quarantineDir, 0o700); err != nil {
		return fmt.Errorf("secure socket cleanup directory: %w", err)
	}
	if beforeQuarantine != nil {
		beforeQuarantine(quarantineDir)
	}

	quarantinedPath := filepath.Join(quarantineDir, "entry")
	if err := os.Rename(socketPath, quarantinedPath); err != nil {
		if os.IsNotExist(err) {
			releaseAnchor = true
			return nil
		}
		return fmt.Errorf("quarantine socket during cleanup: %w", err)
	}
	releaseAnchor = true
	if afterQuarantine != nil {
		afterQuarantine(quarantinedPath)
	}
	quarantined, err := os.Lstat(quarantinedPath)
	if err != nil {
		removeQuarantine = false
		return fmt.Errorf("inspect quarantined socket entry preserved at %s: %w", quarantinedPath, err)
	}
	if !sameOwnedEntry(owned.file, quarantined) {
		if restoreErr := publishSocket(quarantinedPath, socketPath); restoreErr != nil {
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

func sameOwnedEntry(expected, actual os.FileInfo) bool {
	return expected != nil && actual != nil &&
		os.SameFile(expected, actual) && expected.Mode().Type() == actual.Mode().Type()
}
