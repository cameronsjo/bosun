//go:build windows

package lock

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func platformLockOps() lockOps {
	return lockOps{
		lock: func(f *os.File) error {
			return windows.LockFileEx(
				windows.Handle(f.Fd()),
				windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
				0,
				1,
				0,
				&windows.Overlapped{},
			)
		},
		unlock: func(f *os.File) error {
			return windows.UnlockFileEx(
				windows.Handle(f.Fd()),
				0,
				1,
				0,
				&windows.Overlapped{},
			)
		},
		isContended: func(err error) bool {
			return errors.Is(err, windows.ERROR_LOCK_VIOLATION)
		},
	}
}
