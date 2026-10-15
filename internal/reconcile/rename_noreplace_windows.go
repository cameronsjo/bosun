//go:build windows

package reconcile

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func renameNoReplace(oldPath, newPath string) error {
	oldPtr, err := windows.UTF16PtrFromString(oldPath)
	if err != nil {
		return err
	}
	newPtr, err := windows.UTF16PtrFromString(newPath)
	if err != nil {
		return err
	}
	return windows.MoveFile(oldPtr, newPtr)
}

func renameNoReplaceAt(oldDir *os.File, oldName string, newDir *os.File, newName string) error {
	return renameNoReplace(filepath.Join(oldDir.Name(), oldName), filepath.Join(newDir.Name(), newName))
}
