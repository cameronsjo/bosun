//go:build !darwin && !linux && !windows

package fileutil

import "os"

func openSourceFile(path string, _ bool) (*os.File, error) {
	return os.Open(path)
}

func rootOpenFlags(_ bool) int { return os.O_RDONLY }

func isRootSymlinkError(error) bool { return false }
