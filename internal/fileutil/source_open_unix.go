//go:build darwin || linux

package fileutil

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func openSourceFile(path string, followSymlinks bool) (*os.File, error) {
	flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NONBLOCK
	if !followSymlinks {
		flags |= unix.O_NOFOLLOW
	}

	fd, err := unix.Open(path, flags, 0)
	if err != nil {
		if !followSymlinks && errors.Is(err, unix.ELOOP) {
			return nil, ErrSymlinkSkipped
		}
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(fd), path), nil
}
