//go:build !windows

package snapshot

import (
	"fmt"
	"syscall"
)

// checkDiskSpace checks if there's enough disk space available.
func checkDiskSpace(dir string, requiredBytes int64) error {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(dir, &stat); err != nil {
		return fmt.Errorf("failed to check disk space: %w", err)
	}

	available := int64(stat.Bavail) * int64(stat.Bsize)
	if available < requiredBytes {
		return fmt.Errorf("need %d bytes, only %d available", requiredBytes, available)
	}
	return nil
}
