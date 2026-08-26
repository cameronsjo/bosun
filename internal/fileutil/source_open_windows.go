//go:build windows

package fileutil

import (
	"os"

	"golang.org/x/sys/windows"
)

func openSourceFile(path string, followSymlinks bool) (*os.File, error) {
	flags := os.O_RDONLY
	if !followSymlinks {
		flags |= windows.O_FILE_FLAG_OPEN_REPARSE_POINT
	}
	return os.OpenFile(path, flags, 0)
}
