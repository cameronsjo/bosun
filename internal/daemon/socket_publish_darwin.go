//go:build darwin

package daemon

import "golang.org/x/sys/unix"

// publishSocket atomically publishes stagedPath without replacing an entry
// created at socketPath after stale-socket cleanup.
func publishSocket(stagedPath, socketPath string) error {
	return unix.RenamexNp(stagedPath, socketPath, unix.RENAME_EXCL)
}
