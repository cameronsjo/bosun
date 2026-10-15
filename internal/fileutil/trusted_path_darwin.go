//go:build darwin

package fileutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// macOS exposes /var and /tmp as documented system aliases. They are safe to
// canonicalize only when the aliases and their targets retain the expected
// root-owned layout. Arbitrary symlinks remain rejected by OpenPinnedRoot.
func canonicalizeTrustedPath(absPath string) (string, error) {
	for _, alias := range []struct {
		name   string
		target string
	}{
		{name: "/var", target: "/private/var"},
		{name: "/tmp", target: "/private/tmp"},
	} {
		if absPath != alias.name && !strings.HasPrefix(absPath, alias.name+string(filepath.Separator)) {
			continue
		}
		aliasInfo, err := os.Lstat(alias.name)
		if err != nil {
			return "", fmt.Errorf("inspect trusted system alias %q: %w", alias.name, err)
		}
		if aliasInfo.Mode()&os.ModeSymlink == 0 {
			return absPath, nil
		}
		aliasTarget, err := os.Readlink(alias.name)
		if err != nil {
			return "", fmt.Errorf("read trusted system alias %q: %w", alias.name, err)
		}
		if aliasTarget != alias.target {
			return "", fmt.Errorf("trusted system alias %q points to %q, expected %q", alias.name, aliasTarget, alias.target)
		}
		if err := requireRootOwnedSystemPath(alias.name, aliasInfo, false); err != nil {
			return "", err
		}
		targetInfo, err := os.Stat(alias.target)
		if err != nil {
			return "", fmt.Errorf("inspect trusted system alias target %q: %w", alias.target, err)
		}
		if err := requireRootOwnedSystemPath(alias.target, targetInfo, true); err != nil {
			return "", err
		}
		return alias.target + strings.TrimPrefix(absPath, alias.name), nil
	}
	return absPath, nil
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
