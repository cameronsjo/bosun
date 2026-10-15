//go:build darwin

package reconcile

import (
	"os"

	"golang.org/x/sys/unix"
)

func renameNoReplace(oldPath, newPath string) error {
	return unix.RenamexNp(oldPath, newPath, unix.RENAME_EXCL)
}

func renameNoReplaceAt(oldDir *os.File, oldName string, newDir *os.File, newName string) error {
	return unix.RenameatxNp(int(oldDir.Fd()), oldName, int(newDir.Fd()), newName, unix.RENAME_EXCL)
}
