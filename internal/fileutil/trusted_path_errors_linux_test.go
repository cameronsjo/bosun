//go:build linux

package fileutil

import (
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type trustedPathFileInfo struct {
	mode os.FileMode
	sys  any
}

func (i trustedPathFileInfo) Name() string       { return "trusted-path-fixture" }
func (i trustedPathFileInfo) Size() int64        { return 0 }
func (i trustedPathFileInfo) Mode() os.FileMode  { return i.mode }
func (i trustedPathFileInfo) ModTime() time.Time { return time.Time{} }
func (i trustedPathFileInfo) IsDir() bool        { return i.mode.IsDir() }
func (i trustedPathFileInfo) Sys() any           { return i.sys }

func trustedPathInfo(mode os.FileMode, uid uint32) os.FileInfo {
	return trustedPathFileInfo{mode: mode, sys: &syscall.Stat_t{Uid: uid}}
}

func TestNormalizeTrustedAliasTarget(t *testing.T) {
	tests := []struct {
		name, aliasName, target, want string
	}{
		{name: "relative target", aliasName: "/var/run", target: "../run", want: "/run"},
		{name: "absolute target", aliasName: "/var/run", target: "/run", want: "/run"},
		{name: "nested relative target", aliasName: "/var/run", target: "../../private/run", want: "/private/run"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, normalizeTrustedAliasTarget(tt.aliasName, tt.target))
		})
	}
}

func TestRequireRootOwnedSystemPathRejectsUnsafeMetadata(t *testing.T) {
	foreignUID := uint32(os.Geteuid()) + 1
	tests := []struct {
		name      string
		path      string
		info      os.FileInfo
		directory bool
		wantErr   string
	}{
		{name: "directory must be a directory", path: "/run", info: trustedPathInfo(0o755, 0), directory: true, wantErr: "not a directory"},
		{name: "owner metadata unavailable", path: "/run", info: trustedPathFileInfo{mode: os.ModeDir | 0o755}, directory: true, wantErr: "not root-owned"},
		{name: "foreign owner", path: "/run", info: trustedPathInfo(os.ModeDir|0o755, foreignUID), directory: true, wantErr: "not root-owned"},
		{name: "unsafe writable directory", path: "/run", info: trustedPathInfo(os.ModeDir|0o775, 0), directory: true, wantErr: "unsafe writable permissions"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := requireRootOwnedSystemPath(tt.path, tt.info, tt.directory)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestRequireRootOwnedSystemPathAcceptsRootOwnedFilesAndStickyDirectories(t *testing.T) {
	require.NoError(t, requireRootOwnedSystemPath("/run/file", trustedPathInfo(0o600, 0), false))
	require.NoError(t, requireRootOwnedSystemPath("/tmp", trustedPathInfo(os.ModeDir|os.ModeSticky|0o1777, 0), true))
}
