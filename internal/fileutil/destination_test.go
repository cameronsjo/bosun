package fileutil

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// escapeFixture builds the layout a compromised container can arrange under a
// deploy destination: a destination subdirectory replaced by a symlink to a
// directory outside the destination.
type escapeFixture struct {
	src     string
	dst     string
	outside string
	// through is the destination path whose middle component is the symlink.
	through string
	// escaped is where a write that follows the symlink lands.
	escaped string
}

func newEscapeFixture(t *testing.T) escapeFixture {
	t.Helper()

	tmpDir := t.TempDir()
	fixture := escapeFixture{
		src:     filepath.Join(tmpDir, "staging", "app.yml"),
		dst:     filepath.Join(tmpDir, "appdata"),
		outside: filepath.Join(tmpDir, "outside"),
	}
	fixture.through = filepath.Join(fixture.dst, "svc", "app.yml")
	fixture.escaped = filepath.Join(fixture.outside, "app.yml")

	writeTestFile(t, fixture.src, "rendered config")
	require.NoError(t, os.MkdirAll(fixture.dst, 0o755))
	require.NoError(t, os.MkdirAll(fixture.outside, 0o755))
	require.NoError(t, os.Symlink(fixture.outside, filepath.Join(fixture.dst, "svc")))
	return fixture
}

// TestCopyFileUnderRoot_RefusesEscapeThroughSymlinkedDirectory is the
// single-file deploy half of the fix. The path-based control proves this
// filesystem really does let the write escape, so the pinned assertion that
// follows is not vacuous.
func TestCopyFileUnderRoot_RefusesEscapeThroughSymlinkedDirectory(t *testing.T) {
	t.Parallel()

	t.Run("path-based control escapes", func(t *testing.T) {
		t.Parallel()

		fixture := newEscapeFixture(t)

		require.NoError(t, CopyFile(context.Background(), fixture.src, fixture.through))
		assert.FileExists(t, fixture.escaped,
			"control: resolving the destination by path follows the symlink out of the deploy root")
	})

	t.Run("pinned copy refuses", func(t *testing.T) {
		t.Parallel()

		fixture := newEscapeFixture(t)

		err := CopyFileUnderRoot(context.Background(), fixture.src, fixture.dst, fixture.through)

		require.Error(t, err)
		assert.NoFileExists(t, fixture.escaped, "the pinned copy must not write outside the deploy root")
	})
}

func TestCopyFileUnderRootIfChanged_RefusesEscapeThroughSymlinkedDirectory(t *testing.T) {
	t.Parallel()

	t.Run("path-based control escapes", func(t *testing.T) {
		t.Parallel()

		fixture := newEscapeFixture(t)

		changed, err := CopyFileIfChanged(context.Background(), fixture.src, fixture.through)

		require.NoError(t, err)
		assert.True(t, changed)
		assert.FileExists(t, fixture.escaped,
			"control: the content-hash copy follows the symlink out of the deploy root too")
	})

	t.Run("pinned copy refuses", func(t *testing.T) {
		t.Parallel()

		fixture := newEscapeFixture(t)

		changed, err := CopyFileUnderRootIfChanged(context.Background(), fixture.src, fixture.dst, fixture.through)

		require.Error(t, err)
		assert.False(t, changed)
		assert.NoFileExists(t, fixture.escaped, "the pinned content-hash copy must not write outside the deploy root")
	})
}

func TestCopyFileUnderRoot_RefusesDestinationOutsideRoot(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	src := filepath.Join(tmpDir, "staging", "app.yml")
	writeTestFile(t, src, "rendered config")
	dst := filepath.Join(tmpDir, "elsewhere", "app.yml")

	err := CopyFileUnderRoot(context.Background(), src, filepath.Join(tmpDir, "appdata"), dst)

	require.ErrorIs(t, err, errDestinationEscapesRoot)
	assert.NoFileExists(t, dst, "a destination outside the root must fail closed, not fall back to a path copy")
}

// TestCopyDirIfChanged_PinnedOpsRefuseSwappedSubdirectory models the finding's
// race: the walk creates a destination subdirectory, that subdirectory is
// swapped for a symlink, and the file copy that follows must not write through
// it. The path-based control performs the same sequence and does escape.
func TestCopyDirIfChanged_PinnedOpsRefuseSwappedSubdirectory(t *testing.T) {
	t.Parallel()

	swapAfterMkdir := func(t *testing.T, fixture escapeFixture, ops destinationDirOps) error {
		t.Helper()

		subDir := filepath.Join(fixture.dst, "svc")
		require.NoError(t, os.Remove(subDir), "start from a real directory, as the walk would create it")
		created, err := ops.mkdirIfMissing(subDir, 0o755)
		require.NoError(t, err)
		require.True(t, created)

		// The window the finding describes: the checked directory becomes a
		// symlink before the copy resolves the destination again.
		require.NoError(t, os.Remove(subDir))
		require.NoError(t, os.Symlink(fixture.outside, subDir))

		_, _, copyErr := ops.copyFile(context.Background(), fixture.src, fixture.through)
		return copyErr
	}

	t.Run("path-based control escapes", func(t *testing.T) {
		t.Parallel()

		fixture := newEscapeFixture(t)
		ops := pathDirOps(copyFileIfChangedDeferredWithoutDirSync, syncDestinationDir).withDefaults()

		require.NoError(t, swapAfterMkdir(t, fixture, ops))
		assert.FileExists(t, fixture.escaped,
			"control: the path-based walk writes through a directory swapped after its check")
	})

	t.Run("pinned ops refuse", func(t *testing.T) {
		t.Parallel()

		fixture := newEscapeFixture(t)
		pinned := newPinnedDir(fixture.dst)
		defer pinned.close()

		err := swapAfterMkdir(t, fixture, pinned.dirOps().withDefaults())

		require.Error(t, err)
		assert.NoFileExists(t, fixture.escaped, "the pinned walk must not write outside the deploy root")
	})
}

// TestCopyDirIfChanged_LeavesNoDestinationWhenWalkFailsImmediately keeps the
// pinned root lazy: the destination is created when the walk reaches it, never
// before.
func TestCopyDirIfChanged_LeavesNoDestinationWhenWalkFailsImmediately(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	src := filepath.Join(tmpDir, "missing-source")
	dst := filepath.Join(tmpDir, "destination")

	written, err := CopyDirIfChanged(context.Background(), src, dst)

	require.Error(t, err)
	assert.Empty(t, written)
	assert.NoDirExists(t, dst, "a walk that never reaches the source root must leave no destination behind")
}

func TestRootCreateTemp_MatchesCreateTempContract(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	root, err := os.OpenRoot(tmpDir)
	require.NoError(t, err)
	defer func() { _ = root.Close() }()

	t.Run("creates a private file inside the root", func(t *testing.T) {
		file, name, err := rootCreateTemp(root, ".", ".tmp-*")
		require.NoError(t, err)
		defer func() { _ = file.Close() }()

		assert.Equal(t, name, filepath.Base(name), "the returned name must be usable with the root's own methods")
		info, err := root.Lstat(name)
		require.NoError(t, err)
		assert.Equal(t, fs.FileMode(0o600), info.Mode().Perm())
	})

	t.Run("returns distinct names", func(t *testing.T) {
		first, firstName, err := rootCreateTemp(root, ".", ".tmp-*")
		require.NoError(t, err)
		defer func() { _ = first.Close() }()
		second, secondName, err := rootCreateTemp(root, ".", ".tmp-*")
		require.NoError(t, err)
		defer func() { _ = second.Close() }()

		assert.NotEqual(t, firstName, secondName)
	})

	t.Run("refuses a pattern with a path separator", func(t *testing.T) {
		_, _, err := rootCreateTemp(root, ".", "../escape-*")

		require.ErrorIs(t, err, errTempPatternHasSeparator)
	})
}

func TestDestinationName_FailsClosed(t *testing.T) {
	t.Parallel()

	root := filepath.Join("/", "mnt", "user", "appdata")

	t.Run("accepts a path under the root", func(t *testing.T) {
		name, err := destinationName(root, filepath.Join(root, "svc", "app.yml"))

		require.NoError(t, err)
		assert.Equal(t, filepath.Join("svc", "app.yml"), name)
	})

	t.Run("accepts the root itself", func(t *testing.T) {
		name, err := destinationName(root, root)

		require.NoError(t, err)
		assert.Equal(t, ".", name)
	})

	t.Run("refuses a sibling of the root", func(t *testing.T) {
		_, err := destinationName(root, filepath.Join("/", "mnt", "user", "other", "app.yml"))

		require.ErrorIs(t, err, errDestinationEscapesRoot)
	})

	t.Run("refuses a traversal out of the root", func(t *testing.T) {
		_, err := destinationName(root, filepath.Join(root, "..", "app.yml"))

		require.ErrorIs(t, err, errDestinationEscapesRoot)
	})
}
