//go:build !darwin && !linux

package fileutil

// Other platforms do not have the macOS /var and /tmp aliases. Their ordinary
// absolute paths are traversed descriptor-by-descriptor by OpenPinnedRoot.
func canonicalizeTrustedPath(absPath string) (string, error) {
	return absPath, nil
}
