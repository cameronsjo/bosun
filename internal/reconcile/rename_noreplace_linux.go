//go:build linux

package reconcile

import (
	"os"

	"golang.org/x/sys/unix"
)

func renameNoReplace(oldPath, newPath string) error {
	return unix.Renameat2(unix.AT_FDCWD, oldPath, unix.AT_FDCWD, newPath, unix.RENAME_NOREPLACE)
}

func renameNoReplaceAt(oldDir *os.File, oldName string, newDir *os.File, newName string) error {
	return unix.Renameat2(int(oldDir.Fd()), oldName, int(newDir.Fd()), newName, unix.RENAME_NOREPLACE)
}
