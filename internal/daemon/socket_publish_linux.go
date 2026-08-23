//go:build linux

package daemon

import "golang.org/x/sys/unix"

// publishSocket atomically publishes stagedPath without replacing an entry
// created at socketPath after stale-socket cleanup.
func publishSocket(stagedPath, socketPath string) error {
	return unix.Renameat2(unix.AT_FDCWD, stagedPath, unix.AT_FDCWD, socketPath, unix.RENAME_NOREPLACE)
}
