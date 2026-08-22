//go:build !windows

package reconcile

import "os"

func openPinnedPath(path string) (*os.File, error) {
	return os.Open(path)
}
