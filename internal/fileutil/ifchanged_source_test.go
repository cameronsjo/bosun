package fileutil_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cameronsjo/bosun/internal/fileutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// realTempDir returns t.TempDir() with symlinks resolved (macOS /var is a
// link to /private/var).
func realTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	return dir
}

// ifChangedCopier runs one of the two public content-hash copy entry points
// with dst under root.
type ifChangedCopier struct {
	name string
	run  func(ctx context.Context, src, root, dst string) (bool, error)
}

func ifChangedCopiers() []ifChangedCopier {
	return []ifChangedCopier{
		{
			name: "CopyFileIfChanged",
			run: func(ctx context.Context, src, _, dst string) (bool, error) {
				return fileutil.CopyFileIfChanged(ctx, src, dst)
			},
		},
		{
			name: "CopyFileUnderRootIfChanged",
			run: func(ctx context.Context, src, root, dst string) (bool, error) {
				return fileutil.CopyFileUnderRootIfChanged(ctx, src, root, dst)
			},
		},
	}
}

// TestCopyFileIfChangedSkipsLinkedSourcesWithoutFollowing pins that a symlink
// source is the documented benign skip, (false, nil), whatever the link points
// at. The content-hash path used to hash through the link before the copy
// refused it, so a dangling link or a link to a directory failed the deploy
// instead of being skipped (cameronsjo/bosun#802).
func TestCopyFileIfChangedSkipsLinkedSourcesWithoutFollowing(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		target func(t *testing.T, dir string) string
	}{
		{
			name: "dangling link",
			target: func(_ *testing.T, dir string) string {
				return filepath.Join(dir, "does-not-exist")
			},
		},
		{
			name: "link to a directory",
			target: func(t *testing.T, dir string) string {
				target := filepath.Join(dir, "a-directory")
				require.NoError(t, os.Mkdir(target, 0o755))
				return target
			},
		},
	}

	for _, copier := range ifChangedCopiers() {
		for _, tc := range cases {
			t.Run(copier.name+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				dir := realTempDir(t)
				src := filepath.Join(dir, "link")
				require.NoError(t, os.Symlink(tc.target(t, dir), src))
				root := filepath.Join(dir, "root")
				require.NoError(t, os.Mkdir(root, 0o755))
				dst := filepath.Join(root, "dst.txt")

				changed, err := copier.run(context.Background(), src, root, dst)

				require.NoError(t, err, "a symlink source must be skipped, not followed")
				assert.False(t, changed)
				assert.NoFileExists(t, dst)
			})
		}
	}
}

// TestCopyFileIfChangedSkipsLinkToEqualContent is a regression guard: a link
// whose target matches the destination stays a no-write skip and leaves the
// destination untouched.
func TestCopyFileIfChangedSkipsLinkToEqualContent(t *testing.T) {
	t.Parallel()

	for _, copier := range ifChangedCopiers() {
		t.Run(copier.name, func(t *testing.T) {
			t.Parallel()
			dir := realTempDir(t)
			target := filepath.Join(dir, "target.txt")
			require.NoError(t, os.WriteFile(target, []byte("same"), 0o644))
			src := filepath.Join(dir, "link")
			require.NoError(t, os.Symlink(target, src))
			root := filepath.Join(dir, "root")
			require.NoError(t, os.Mkdir(root, 0o755))
			dst := filepath.Join(root, "dst.txt")
			require.NoError(t, os.WriteFile(dst, []byte("same"), 0o600))

			changed, err := copier.run(context.Background(), src, root, dst)

			require.NoError(t, err)
			assert.False(t, changed)
			info, statErr := os.Lstat(dst)
			require.NoError(t, statErr)
			assert.True(t, info.Mode().IsRegular())
			assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "the destination must not be rewritten")
		})
	}
}

// TestCopyFileUnderRootIfChangedLinkedSourceCreatesNoRoot pins that a refused
// source is refused before any destination mutation. The pinned variant used
// to create the missing destination root first and only then let the copy
// refuse the symlink.
func TestCopyFileUnderRootIfChangedLinkedSourceCreatesNoRoot(t *testing.T) {
	t.Parallel()

	dir := realTempDir(t)
	target := filepath.Join(dir, "target.txt")
	require.NoError(t, os.WriteFile(target, []byte("payload"), 0o644))
	src := filepath.Join(dir, "link")
	require.NoError(t, os.Symlink(target, src))
	root := filepath.Join(dir, "absent-root")
	dst := filepath.Join(root, "dst.txt")

	changed, err := fileutil.CopyFileUnderRootIfChanged(context.Background(), src, root, dst)

	require.NoError(t, err)
	assert.False(t, changed)
	_, statErr := os.Lstat(root)
	assert.ErrorIs(t, statErr, os.ErrNotExist, "a skipped source must not create the destination root")
}

// TestCopyFileUnderRootIfChangedDirectorySourceCreatesNoRoot pins that a
// directory source is refused as an unsupported type before the missing
// destination root is created.
func TestCopyFileUnderRootIfChangedDirectorySourceCreatesNoRoot(t *testing.T) {
	t.Parallel()

	dir := realTempDir(t)
	src := filepath.Join(dir, "a-directory")
	require.NoError(t, os.Mkdir(src, 0o755))
	root := filepath.Join(dir, "absent-root")

	changed, err := fileutil.CopyFileUnderRootIfChanged(context.Background(), src, root, filepath.Join(root, "dst.txt"))

	require.ErrorIs(t, err, fileutil.ErrUnsupportedFileType)
	assert.False(t, changed)
	_, statErr := os.Lstat(root)
	assert.ErrorIs(t, statErr, os.ErrNotExist, "a refused source must not create the destination root")
}

// TestCopyFileUnderRootRefusesSourceBeforeCreatingRoot pins the pinned plain
// copy (CopyFileUnderRoot, the standard-mode single-file deploy) to CopyFile's
// source contract with no destination mutation: a symlinked source returns
// ErrSymlinkSkipped and a directory source ErrUnsupportedFileType, and neither
// creates the missing root.
func TestCopyFileUnderRootRefusesSourceBeforeCreatingRoot(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		source  func(t *testing.T, dir string) string
		wantErr error
	}{
		{
			name: "symlink to a regular file",
			source: func(t *testing.T, dir string) string {
				target := filepath.Join(dir, "target.txt")
				require.NoError(t, os.WriteFile(target, []byte("payload"), 0o644))
				link := filepath.Join(dir, "link")
				require.NoError(t, os.Symlink(target, link))
				return link
			},
			wantErr: fileutil.ErrSymlinkSkipped,
		},
		{
			name: "directory",
			source: func(t *testing.T, dir string) string {
				src := filepath.Join(dir, "a-directory")
				require.NoError(t, os.Mkdir(src, 0o755))
				return src
			},
			wantErr: fileutil.ErrUnsupportedFileType,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := realTempDir(t)
			src := tc.source(t, dir)
			root := filepath.Join(dir, "absent-root")

			err := fileutil.CopyFileUnderRoot(context.Background(), src, root, filepath.Join(root, "dst.txt"))

			require.ErrorIs(t, err, tc.wantErr)
			_, statErr := os.Lstat(root)
			assert.ErrorIs(t, statErr, os.ErrNotExist, "a refused source must not create the destination root")
		})
	}
}
