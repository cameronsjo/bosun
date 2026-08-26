//go:build !windows

package lock

import (
	"errors"
	"os"
	"syscall"
)

func platformLockOps() lockOps {
	return lockOps{
		lock: func(f *os.File) error {
			return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		},
		unlock: func(f *os.File) error {
			return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		},
		isContended: func(err error) bool {
			return errors.Is(err, syscall.EWOULDBLOCK)
		},
	}
}
