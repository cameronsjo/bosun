//go:build windows

package snapshot

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// checkDiskSpace checks if there's enough disk space available.
func checkDiskSpace(dir string, requiredBytes int64) error {
	dirPtr, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return fmt.Errorf("failed to encode path: %w", err)
	}

	var freeBytesAvailable uint64
	err = windows.GetDiskFreeSpaceEx(
		dirPtr,
		(*uint64)(unsafe.Pointer(&freeBytesAvailable)),
		nil,
		nil,
	)
	if err != nil {
		return fmt.Errorf("failed to check disk space: %w", err)
	}

	if int64(freeBytesAvailable) < requiredBytes {
		return fmt.Errorf("need %d bytes, only %d available", requiredBytes, freeBytesAvailable)
	}
	return nil
}
