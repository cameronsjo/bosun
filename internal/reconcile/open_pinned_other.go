//go:build !windows

package reconcile

import "os"

func openPinnedPath(path string) (*os.File, error) {
	return os.Open(path)
}

func createPinnedFileExclusive(path string, mode os.FileMode) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, mode)
}
