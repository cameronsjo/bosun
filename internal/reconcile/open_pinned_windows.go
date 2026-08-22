//go:build windows

package reconcile

import (
	"os"

	"golang.org/x/sys/windows"
)

func openPinnedPath(path string) (*os.File, error) {
	return openPinnedWindows(path, windows.GENERIC_READ, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS)
}

func createPinnedFileExclusive(path string, _ os.FileMode) (*os.File, error) {
	return openPinnedWindows(path, windows.GENERIC_READ|windows.GENERIC_WRITE, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL)
}

func openPinnedWindows(path string, access, creation, attributes uint32) (*os.File, error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(
		pathPtr,
		access,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		creation,
		attributes,
		0,
	)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(handle), path), nil
}
