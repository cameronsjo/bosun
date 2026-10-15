//go:build linux

package fileutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Linux distributions commonly expose /var/run as the documented /run alias.
// Canonicalize only that exact system alias after validating its root-owned
// target. Arbitrary symlinks remain rejected by OpenPinnedRoot.
func canonicalizeTrustedPath(absPath string) (string, error) {
	const aliasName = "/var/run"
	const aliasTarget = "/run"
	if absPath != aliasName && !strings.HasPrefix(absPath, aliasName+string(filepath.Separator)) {
		return absPath, nil
	}
	aliasInfo, err := os.Lstat(aliasName)
	if err != nil {
		return "", fmt.Errorf("inspect trusted system alias %q: %w", aliasName, err)
	}
	if aliasInfo.Mode()&os.ModeSymlink == 0 {
		return absPath, nil
	}
	linkTarget, err := os.Readlink(aliasName)
	if err != nil {
		return "", fmt.Errorf("read trusted system alias %q: %w", aliasName, err)
	}
	if normalized := normalizeTrustedAliasTarget(aliasName, linkTarget); normalized != aliasTarget {
		return "", fmt.Errorf("trusted system alias %q points to %q, expected %q", aliasName, linkTarget, aliasTarget)
	}
	if err := requireRootOwnedSystemPath(aliasName, aliasInfo, false); err != nil {
		return "", err
	}
	targetInfo, err := os.Stat(aliasTarget)
	if err != nil {
		return "", fmt.Errorf("inspect trusted system alias target %q: %w", aliasTarget, err)
	}
	if err := requireRootOwnedSystemPath(aliasTarget, targetInfo, true); err != nil {
		return "", err
	}
	return aliasTarget + strings.TrimPrefix(absPath, aliasName), nil
}

func normalizeTrustedAliasTarget(aliasName, linkTarget string) string {
	if filepath.IsAbs(linkTarget) {
		return filepath.Clean(linkTarget)
	}
	return filepath.Clean(filepath.Join(filepath.Dir(aliasName), linkTarget))
}

func requireRootOwnedSystemPath(path string, info os.FileInfo, directory bool) error {
	if directory && !info.IsDir() {
		return fmt.Errorf("trusted system path %q is not a directory", path)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 {
		return fmt.Errorf("trusted system path %q is not root-owned", path)
	}
	if directory && info.Mode().Perm()&0o022 != 0 && info.Mode()&os.ModeSticky == 0 {
		return fmt.Errorf("trusted system path %q has unsafe writable permissions %#o", path, info.Mode().Perm())
	}
	return nil
}
