//go:build linux

package fileutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCanonicalTrustedPath_LinuxVarRunAlias(t *testing.T) {
	const alias = "/var/run"
	const target = "/run"
	const suffix = "/bosun/reconcile.lock"

	aliasInfo, err := os.Lstat(alias)
	if err != nil || aliasInfo.Mode()&os.ModeSymlink == 0 {
		t.Skipf("standard Linux %s alias is unavailable", alias)
	}
	linkTarget, err := os.Readlink(alias)
	require.NoError(t, err)
	if filepath.Clean(filepath.Join(filepath.Dir(alias), linkTarget)) != target {
		t.Skipf("non-standard Linux %s alias target %q", alias, linkTarget)
	}

	got, err := CanonicalTrustedPath(alias + suffix)
	require.NoError(t, err)
	assert.Equal(t, target+suffix, got)
}
