//go:build !linux && !darwin && !windows

package reconcile

import (
	"fmt"
	"os"
)

func renameNoReplace(oldPath, newPath string) error {
	return fmt.Errorf("atomic no-replace rename is unsupported on this platform: %s -> %s", oldPath, newPath)
}

func renameNoReplaceAt(oldDir *os.File, oldName string, newDir *os.File, newName string) error {
	return fmt.Errorf("descriptor-relative atomic no-replace rename is unsupported on this platform: %s/%s -> %s/%s", oldDir.Name(), oldName, newDir.Name(), newName)
}
