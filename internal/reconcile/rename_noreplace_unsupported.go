//go:build !linux && !darwin && !windows

package reconcile

import "fmt"

func renameNoReplace(oldPath, newPath string) error {
	return fmt.Errorf("atomic no-replace rename is unsupported on this platform: %s -> %s", oldPath, newPath)
}
