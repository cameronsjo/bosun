//go:build !darwin && !linux && !windows

package fileutil

import "os"

// destinationOpenNonblock is zero where openSourceFile has no nonblocking path.
const destinationOpenNonblock = 0

func openSourceFile(path string, _ bool) (*os.File, error) {
	return os.Open(path)
}
