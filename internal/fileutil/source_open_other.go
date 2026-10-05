//go:build !darwin && !linux && !windows

package fileutil

import "os"

// destinationOpenNonblock is zero where openSourceFile has no nonblocking path.
const destinationOpenNonblock = 0

// openSourceFile has no O_NOFOLLOW here, so a no-follow open checks the entry
// with Lstat first. That leaves a check-then-open window these platforms
// accept; darwin, linux and windows refuse the link in the open itself.
func openSourceFile(path string, followSymlinks bool) (*os.File, error) {
	if !followSymlinks {
		if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return nil, ErrSymlinkSkipped
		}
	}
	return os.Open(path)
}
