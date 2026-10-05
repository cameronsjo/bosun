package reconcile

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMkdirAllUnderRoot(t *testing.T) {
	setup := func(t *testing.T) (base, root string) {
		t.Helper()
		base, err := filepath.EvalSymlinks(t.TempDir())
		require.NoError(t, err)
		return base, filepath.Join(base, "appdata")
	}

	t.Run("creates the root and the nested target", func(t *testing.T) {
		_, root := setup(t)
		target := filepath.Join(root, "svc", "conf")

		require.NoError(t, mkdirAllUnderRoot(context.Background(), root, target, 0o755))

		assert.DirExists(t, target)
	})

	t.Run("target equal to root creates only the root", func(t *testing.T) {
		_, root := setup(t)

		require.NoError(t, mkdirAllUnderRoot(context.Background(), root, root, 0o755))

		assert.DirExists(t, root)
	})

	t.Run("refuses a target outside the root without creating it", func(t *testing.T) {
		base, root := setup(t)
		outside := filepath.Join(base, "outside", "svc")

		err := mkdirAllUnderRoot(context.Background(), root, outside, 0o755)

		require.ErrorContains(t, err, "outside deploy root")
		assert.NoDirExists(t, root)
		assert.NoDirExists(t, filepath.Join(base, "outside"))
	})

	// The service directory under appdata is container-writable. A container
	// that swaps it for a symlink to a tree it chose must not get bosun, as
	// root, to create directories there.
	t.Run("refuses a swapped component that escapes the root", func(t *testing.T) {
		base, root := setup(t)
		require.NoError(t, os.MkdirAll(root, 0o755))
		elsewhere := filepath.Join(base, "elsewhere")
		require.NoError(t, os.MkdirAll(elsewhere, 0o755))
		require.NoError(t, os.Symlink(elsewhere, filepath.Join(root, "svc")))

		err := mkdirAllUnderRoot(context.Background(), root, filepath.Join(root, "svc", "conf"), 0o755)

		require.Error(t, err)
		assert.NoDirExists(t, filepath.Join(elsewhere, "conf"))
	})

	t.Run("reports a root that cannot be created", func(t *testing.T) {
		_, root := setup(t)
		require.NoError(t, os.WriteFile(root, []byte("not a directory"), 0o644))

		err := mkdirAllUnderRoot(context.Background(), root, filepath.Join(root, "svc"), 0o755)

		require.ErrorContains(t, err, "create deploy root")
	})

	t.Run("rejects a relative target against an absolute root", func(t *testing.T) {
		_, root := setup(t)

		err := mkdirAllUnderRoot(context.Background(), root, "svc", 0o755)

		require.Error(t, err)
		assert.NoDirExists(t, root)
	})

	t.Run("honors a cancelled context before touching disk", func(t *testing.T) {
		_, root := setup(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := mkdirAllUnderRoot(ctx, root, filepath.Join(root, "svc"), 0o755)

		require.ErrorIs(t, err, context.Canceled)
		assert.NoDirExists(t, root)
	})
}

// Pins the production wiring, not just the helper: with no localFS seam
// injected, deployLocalManaged must create its target through the root pinned
// at appdata. A path-based MkdirAll would follow the swapped service directory
// and create the target in the tree it points at.
func TestDeployLocalManaged_CreatesTargetThroughPinnedRoot(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	source := filepath.Join(base, "staging", "svc", "conf")
	require.NoError(t, os.MkdirAll(source, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(source, "app.conf"), []byte("token: rendered"), 0o644))

	appdata := filepath.Join(base, "appdata")
	require.NoError(t, os.MkdirAll(appdata, 0o755))
	outside := filepath.Join(base, "outside")
	require.NoError(t, os.MkdirAll(outside, 0o755))
	require.NoError(t, os.Symlink(outside, filepath.Join(appdata, "svc")))

	ops := &DeployOps{ContentHashSync: true}
	err = ops.deployLocalManaged(context.Background(), source, filepath.Join(appdata, "svc", "conf"), appdata, nil, nil)

	require.ErrorContains(t, err, "create target directory")
	assert.NoDirExists(t, filepath.Join(outside, "conf"), "no directory may be created through the swapped component")
}
