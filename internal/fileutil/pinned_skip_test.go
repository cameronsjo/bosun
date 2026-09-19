package fileutil

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupEscapingDestination builds a pinned root whose <service> directory has
// been replaced with a symlink to a tree the attacker owns, pre-loaded with a
// byte-identical copy of the rendered file. That is the shape that makes a
// by-path comparison answer "no change" while nothing exists inside the root.
func setupEscapingDestination(t *testing.T) (src, root, dst, outsideFile string) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	const rendered = "token: rendered"
	src = filepath.Join(base, "source.conf")
	require.NoError(t, os.WriteFile(src, []byte(rendered), 0o644))

	root = filepath.Join(base, "appdata")
	require.NoError(t, os.MkdirAll(root, 0o755))

	outside := filepath.Join(base, "outside")
	require.NoError(t, os.MkdirAll(outside, 0o755))
	outsideFile = filepath.Join(outside, "app.conf")
	require.NoError(t, os.WriteFile(outsideFile, []byte(rendered), 0o644))

	require.NoError(t, os.Symlink(outside, filepath.Join(root, "svc")))
	dst = filepath.Join(root, "svc", "app.conf")
	return src, root, dst, outsideFile
}

func TestCopyFileUnderRootIfChanged_RefusesEscapingDestination(t *testing.T) {
	src, root, dst, _ := setupEscapingDestination(t)

	changed, err := CopyFileUnderRootIfChanged(context.Background(), src, root, dst)

	require.Error(t, err, "an escaping destination must not be compared and silently skipped")
	assert.ErrorIs(t, err, errDestinationEscapesRoot)
	assert.False(t, changed)
	entry, lstatErr := os.Lstat(filepath.Join(root, "svc"))
	require.NoError(t, lstatErr)
	assert.NotZero(t, entry.Mode()&os.ModeSymlink, "the refusal must leave the swapped component untouched")
}

func TestPinnedDirCopyFileIfChangedDeferred_RefusesEscapingDestination(t *testing.T) {
	src, root, dst, _ := setupEscapingDestination(t)

	pinned := newPinnedDir(root)
	defer pinned.close()

	changed, verify, err := pinned.copyFileIfChangedDeferred(context.Background(), src, dst)

	require.Error(t, err, "an escaping destination must not be compared and silently skipped")
	assert.ErrorIs(t, err, errDestinationEscapesRoot)
	assert.False(t, changed)
	assert.Nil(t, verify)
}
