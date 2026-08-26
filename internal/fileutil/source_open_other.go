//go:build !darwin && !linux && !windows

package fileutil

import "os"

func openSourceFile(path string, _ bool) (*os.File, error) {
	return os.Open(path)
}
