package fileutil

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type directoryCopyFunc func(src, dst string) ([]string, error)

type directoryCopyCase struct {
	name string
	copy directoryCopyFunc
}

func directoryCopyCases() []directoryCopyCase {
	return []directoryCopyCase{
		{
			name: "CopyDir",
			copy: func(src, dst string) ([]string, error) {
				return nil, CopyDir(src, dst)
			},
		},
		{name: "CopyDirIfChanged", copy: CopyDirIfChanged},
	}
}

func writeContainmentTestFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func TestDirectoryCopiesRejectEqualPathBeforeMutation(t *testing.T) {
	t.Parallel()

	for _, copier := range directoryCopyCases() {
		t.Run(copier.name, func(t *testing.T) {
			t.Parallel()

			source := filepath.Join(t.TempDir(), "source")
			writeContainmentTestFile(t, filepath.Join(source, "sentinel.txt"), "preserve me")

			written, err := copier.copy(source, source)

			require.ErrorIs(t, err, errCopyDestinationWithinSource)
			assert.Empty(t, written)
			content, readErr := os.ReadFile(filepath.Join(source, "sentinel.txt"))
			require.NoError(t, readErr)
			assert.Equal(t, "preserve me", string(content))
		})
	}
}

func TestDirectoryCopiesRejectDescendantBeforeMutation(t *testing.T) {
	t.Parallel()

	for _, copier := range directoryCopyCases() {
		t.Run(copier.name, func(t *testing.T) {
			t.Parallel()

			source := filepath.Join(t.TempDir(), "source")
			writeContainmentTestFile(t, filepath.Join(source, "source.txt"), "source")
			destination := filepath.Join(source, "destination")

			written, err := copier.copy(source, destination)

			require.ErrorIs(t, err, errCopyDestinationWithinSource)
			assert.Empty(t, written)
			assert.NoDirExists(t, destination)
		})
	}
}

func TestDirectoryCopiesPreserveExistingDescendantOnRejection(t *testing.T) {
	t.Parallel()

	for _, copier := range directoryCopyCases() {
		t.Run(copier.name, func(t *testing.T) {
			t.Parallel()

			source := filepath.Join(t.TempDir(), "source")
			writeContainmentTestFile(t, filepath.Join(source, "source.txt"), "source")
			destination := filepath.Join(source, "destination")
			writeContainmentTestFile(t, filepath.Join(destination, "sentinel.txt"), "preserve me")

			written, err := copier.copy(source, destination)

			require.ErrorIs(t, err, errCopyDestinationWithinSource)
			assert.Empty(t, written)
			content, readErr := os.ReadFile(filepath.Join(destination, "sentinel.txt"))
			require.NoError(t, readErr)
			assert.Equal(t, "preserve me", string(content))
			assert.NoFileExists(t, filepath.Join(destination, "source.txt"))
		})
	}
}

func TestDirectoryCopiesResolveSymlinkedDestinationParents(t *testing.T) {
	t.Parallel()

	for _, copier := range directoryCopyCases() {
		t.Run(copier.name, func(t *testing.T) {
			t.Parallel()

			tmpDir := t.TempDir()
			source := filepath.Join(tmpDir, "source")
			writeContainmentTestFile(t, filepath.Join(source, "source.txt"), "source")
			alias := filepath.Join(tmpDir, "source-alias")
			if err := os.Symlink(source, alias); err != nil {
				t.Skipf("create source symlink: %v", err)
			}
			destination := filepath.Join(alias, "destination")

			written, err := copier.copy(source, destination)

			require.ErrorIs(t, err, errCopyDestinationWithinSource)
			assert.Empty(t, written)
			assert.NoDirExists(t, filepath.Join(source, "destination"))
		})
	}
}

func TestDirectoryCopiesResolveSymlinkedSources(t *testing.T) {
	t.Parallel()

	for _, copier := range directoryCopyCases() {
		t.Run(copier.name, func(t *testing.T) {
			t.Parallel()

			tmpDir := t.TempDir()
			source := filepath.Join(tmpDir, "source")
			writeContainmentTestFile(t, filepath.Join(source, "source.txt"), "source")
			alias := filepath.Join(tmpDir, "source-alias")
			if err := os.Symlink(source, alias); err != nil {
				t.Skipf("create source symlink: %v", err)
			}
			destination := filepath.Join(source, "destination")

			written, err := copier.copy(alias, destination)

			require.ErrorIs(t, err, errCopyDestinationWithinSource)
			assert.Empty(t, written)
			assert.NoDirExists(t, destination)
		})
	}
}

func TestDirectoryCopiesRejectUnresolvableRootsBeforeMutation(t *testing.T) {
	t.Parallel()

	for _, copier := range directoryCopyCases() {
		for _, root := range []string{"source", "destination"} {
			t.Run(copier.name+"/"+root, func(t *testing.T) {
				t.Parallel()

				tmpDir := t.TempDir()
				source := filepath.Join(tmpDir, "source")
				destination := filepath.Join(tmpDir, "destination")
				brokenTarget := filepath.Join(tmpDir, "missing")
				if root == "source" {
					if err := os.Symlink(brokenTarget, source); err != nil {
						t.Skipf("create source symlink: %v", err)
					}
				} else {
					writeContainmentTestFile(t, filepath.Join(source, "source.txt"), "source")
					if err := os.Symlink(brokenTarget, destination); err != nil {
						t.Skipf("create destination symlink: %v", err)
					}
				}

				written, err := copier.copy(source, destination)

				require.ErrorContains(t, err, "resolve copy "+root)
				assert.Empty(t, written)
				if root == "source" {
					assert.NoFileExists(t, destination)
				} else {
					linkTarget, readErr := os.Readlink(destination)
					require.NoError(t, readErr)
					assert.Equal(t, brokenTarget, linkTarget)
				}
			})
		}
	}
}

func TestCanonicalPathForContainmentRejectsNonDirectoryAncestor(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Windows reports a non-directory ancestor as path-not-found")
	}

	blocker := filepath.Join(t.TempDir(), "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("not a directory"), 0o644))

	_, err := canonicalPathForContainment(filepath.Join(blocker, "child"))

	require.Error(t, err)
	assert.ErrorContains(t, err, "inspect path")
}

func TestDestinationHasSourceAncestorWithStatPropagatesErrors(t *testing.T) {
	t.Parallel()

	source := filepath.Join(t.TempDir(), "source")
	require.NoError(t, os.Mkdir(source, 0o755))
	sourceInfo, err := os.Stat(source)
	require.NoError(t, err)
	injectedErr := errors.New("injected stat failure")

	tests := []struct {
		name string
		stat func(string) (os.FileInfo, error)
		want string
	}{
		{
			name: "source",
			stat: func(string) (os.FileInfo, error) {
				return nil, injectedErr
			},
			want: "inspect source",
		},
		{
			name: "destination ancestor",
			stat: func(path string) (os.FileInfo, error) {
				if path == source {
					return sourceInfo, nil
				}
				return nil, injectedErr
			},
			want: "inspect destination ancestor",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			overlaps, err := destinationHasSourceAncestorWithStat(source, source+"-destination", tt.stat)

			require.ErrorIs(t, err, injectedErr)
			assert.ErrorContains(t, err, tt.want)
			assert.False(t, overlaps)
		})
	}
}

func TestDirectoryCopiesRejectCaseInsensitiveContainment(t *testing.T) {
	t.Parallel()

	for _, copier := range directoryCopyCases() {
		for _, shape := range []string{"equal", "descendant"} {
			t.Run(copier.name+"/"+shape, func(t *testing.T) {
				t.Parallel()

				tmpDir := t.TempDir()
				source := filepath.Join(tmpDir, "Source")
				sentinel := filepath.Join(source, "sentinel.txt")
				writeContainmentTestFile(t, sentinel, "preserve me")
				alias := filepath.Join(tmpDir, "source")
				sourceInfo, err := os.Stat(source)
				require.NoError(t, err)
				aliasInfo, err := os.Stat(alias)
				if err != nil || !os.SameFile(sourceInfo, aliasInfo) {
					t.Skip("filesystem is case-sensitive")
				}

				destination := alias
				if shape == "descendant" {
					destination = filepath.Join(alias, "destination")
				}
				written, err := copier.copy(source, destination)

				require.ErrorIs(t, err, errCopyDestinationWithinSource)
				assert.Empty(t, written)
				content, readErr := os.ReadFile(sentinel)
				require.NoError(t, readErr)
				assert.Equal(t, "preserve me", string(content))
				if shape == "descendant" {
					assert.NoDirExists(t, filepath.Join(source, "destination"))
				}
			})
		}
	}
}

func TestDirectoryCopiesUseComponentAwareContainment(t *testing.T) {
	t.Parallel()

	for _, copier := range directoryCopyCases() {
		t.Run(copier.name, func(t *testing.T) {
			t.Parallel()

			tmpDir := t.TempDir()
			source := filepath.Join(tmpDir, "app")
			destination := filepath.Join(tmpDir, "application")
			writeContainmentTestFile(t, filepath.Join(source, "source.txt"), "source")

			written, err := copier.copy(source, destination)

			require.NoError(t, err)
			if copier.name == "CopyDirIfChanged" {
				assert.Equal(t, []string{"source.txt"}, written)
			}
			content, readErr := os.ReadFile(filepath.Join(destination, "source.txt"))
			require.NoError(t, readErr)
			assert.Equal(t, "source", string(content))
		})
	}
}
