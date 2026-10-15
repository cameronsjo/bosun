package reconcile

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/cameronsjo/bosun/internal/fileutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMkdirAllUnderRootRejectsUnsafeInputs(t *testing.T) {
	t.Run("cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := mkdirAllUnderRoot(ctx, t.TempDir(), filepath.Join(t.TempDir(), "target"), 0o755)

		require.ErrorIs(t, err, context.Canceled)
	})

	t.Run("outside deploy root", func(t *testing.T) {
		base := t.TempDir()
		root := filepath.Join(base, "root")
		outside := filepath.Join(base, "outside")
		require.NoError(t, os.Mkdir(root, 0o755))

		err := mkdirAllUnderRoot(context.Background(), root, outside, 0o755)

		require.ErrorContains(t, err, "outside deploy root")
	})

	t.Run("root path is not a directory", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "root-file")
		require.NoError(t, os.WriteFile(root, []byte("block"), 0o600))

		err := mkdirAllUnderRoot(context.Background(), root, root, 0o755)

		require.ErrorContains(t, err, "create deploy root")
	})

	t.Run("nested root cannot be pinned", func(t *testing.T) {
		base := t.TempDir()
		root := filepath.Join(base, "root-file")
		require.NoError(t, os.WriteFile(root, []byte("block"), 0o600))

		err := mkdirAllUnderRoot(context.Background(), root, filepath.Join(root, "child"), 0o755)

		require.ErrorContains(t, err, "pin deploy root")
	})
}

func TestRenameNoReplacePinnedRejectsUntrustedParentsAndNames(t *testing.T) {
	require.ErrorContains(t, renameNoReplacePinned(nil, "old", nil, "new"), "no trusted parent")

	parent, err := os.Open(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { _ = parent.Close() })

	for _, tt := range []struct {
		name    string
		oldPath string
		newPath string
	}{
		{name: "old dot", oldPath: ".", newPath: "new"},
		{name: "new dot", oldPath: "old", newPath: "."},
		{name: "old separator", oldPath: string(filepath.Separator), newPath: "new"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := renameNoReplacePinned(parent, tt.oldPath, parent, tt.newPath)

			require.ErrorContains(t, err, "invalid basename")
		})
	}
}

func TestDeployLocalManagedPropagatesPinnedStalePruneFailure(t *testing.T) {
	base := evalSymlinks(t, t.TempDir())
	sourceDir := filepath.Join(base, "source")
	deployRoot := filepath.Join(base, "appdata")
	targetDir := filepath.Join(deployRoot, "service")
	require.NoError(t, os.MkdirAll(sourceDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "present.yml"), []byte("new"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(targetDir, "nested"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(targetDir, "nested", "old.yml"), []byte("old"), 0o644))

	deploy := &DeployOps{
		ContentHashSync: true,
		copyDirIfChangedFn: func(_ context.Context, src, _ string) ([]string, error) {
			// The source was valid during transition discovery. Introduce a
			// regular-file parent before stale pruning so its ENOTDIR result
			// exercises the failure and rollback boundary.
			return nil, os.WriteFile(filepath.Join(src, "nested"), []byte("replacement parent"), 0o600)
		},
	}
	result := &DeployResult{}
	err := deploy.deployLocalManaged(context.Background(), sourceDir, targetDir, deployRoot, result, map[string]bool{"nested/old.yml": true})

	require.ErrorContains(t, err, "remove stale files")
	assert.FileExists(t, filepath.Join(targetDir, "nested", "old.yml"))
	assert.Empty(t, result.WrittenFiles)
}

func TestValidateManagedDirectoryRejectsEmptyDirectory(t *testing.T) {
	dir := t.TempDir()

	_, err := validateManagedDirectory(context.Background(), dir, "config", nil)

	require.ErrorContains(t, err, "has no managed descendants")
}

func TestCleanupPrivateStageRequiresPinnedParent(t *testing.T) {
	stage := filepath.Join(t.TempDir(), "stage")
	require.NoError(t, os.Mkdir(stage, 0o700))
	handle, err := os.Open(stage)
	require.NoError(t, err)
	info, err := handle.Stat()
	require.NoError(t, err)
	t.Cleanup(func() { _ = handle.Close() })

	item := &managedTypeTransition{privatePath: stage, privateHandle: handle, privateInfo: info}
	err = item.cleanupPrivateStage()

	require.ErrorContains(t, err, "missing pinned parent")
	assert.DirExists(t, stage)
}

func TestOpenPinnedDirectoryAndCreatePinnedChildDirectoryPropagateParentFailures(t *testing.T) {
	parentFile := filepath.Join(t.TempDir(), "parent-file")
	require.NoError(t, os.WriteFile(parentFile, []byte("block"), 0o600))

	_, _, err := openPinnedDirectory(parentFile)
	require.Error(t, err)

	_, _, _, err = createPinnedChildDirectory(filepath.Join(parentFile, "child"), nil)
	require.Error(t, err)

	parent := t.TempDir()
	child := filepath.Join(parent, "child")
	require.NoError(t, os.Mkdir(child, 0o700))
	_, _, _, err = createPinnedChildDirectory(child, nil)
	require.Error(t, err)

	_, _, _, err = createPinnedChildDirectory("", nil)
	require.ErrorContains(t, err, "invalid child name")
}

func TestRemovePinnedTreeReportsMissingCleanupParent(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "managed.txt")
	require.NoError(t, os.WriteFile(path, []byte("managed"), 0o600))
	handle, err := os.Open(path)
	require.NoError(t, err)
	info, err := handle.Stat()
	require.NoError(t, err)
	t.Cleanup(func() { _ = handle.Close() })
	fingerprint, err := fingerprintPinnedPath(path, handle, info)
	require.NoError(t, err)
	parent, err := os.Open(base)
	require.NoError(t, err)
	t.Cleanup(func() { _ = parent.Close() })

	err = removePinnedTree(path, filepath.Join(base, "missing", "cleanup"), handle, info, fingerprint, "managed file", removalHooks{}, parent)

	require.ErrorContains(t, err, "pin cleanup namespace parent")
	assert.FileExists(t, path)
}

func TestPinRemovalTreeRejectsUnopenableDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permission behavior is not portable on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("permission failure is not observable when tests run as root")
	}
	path := filepath.Join(t.TempDir(), "managed")
	require.NoError(t, os.Mkdir(path, 0o700))
	handle, err := os.Open(path)
	require.NoError(t, err)
	defer func() { _ = handle.Close() }()
	info, err := handle.Stat()
	require.NoError(t, err)
	require.NoError(t, os.Chmod(path, 0))
	t.Cleanup(func() { _ = os.Chmod(path, 0o700) })

	_, err = pinRemovalTree(path, handle, info)

	require.ErrorContains(t, err, "pin removal root")
}

func TestRemovePinnedTreeRejectsInvalidCleanupName(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "managed.txt")
	require.NoError(t, os.WriteFile(path, []byte("managed"), 0o600))
	handle, err := os.Open(path)
	require.NoError(t, err)
	info, err := handle.Stat()
	require.NoError(t, err)
	t.Cleanup(func() { _ = handle.Close() })
	fingerprint, err := fingerprintPinnedPath(path, handle, info)
	require.NoError(t, err)

	err = removePinnedTree(path, "", handle, info, fingerprint, "managed file", removalHooks{})

	require.ErrorContains(t, err, "invalid cleanup namespace name")
	assert.FileExists(t, path)
}

func TestFingerprintPinnedPathRejectsOutsideNamespaceAndChangedNamespace(t *testing.T) {
	t.Run("cannot pin unreadable directory root", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("directory permission behavior is not portable on Windows")
		}
		if os.Getuid() == 0 {
			t.Skip("permission failure is not observable when tests run as root")
		}
		path := filepath.Join(t.TempDir(), "directory")
		require.NoError(t, os.Mkdir(path, 0o700))
		handle, err := os.Open(path)
		require.NoError(t, err)
		defer func() { _ = handle.Close() }()
		info, err := handle.Stat()
		require.NoError(t, err)
		require.NoError(t, os.Chmod(path, 0))
		t.Cleanup(func() { _ = os.Chmod(path, 0o700) })

		_, err = fingerprintPinnedPath(path, handle, info)

		require.ErrorContains(t, err, "pin fingerprint root")
	})

	t.Run("closed root", func(t *testing.T) {
		path := t.TempDir()
		handle, err := os.Open(path)
		require.NoError(t, err)
		defer func() { _ = handle.Close() }()
		info, err := handle.Stat()
		require.NoError(t, err)
		root, err := fileutil.OpenPinnedRoot(path, false)
		require.NoError(t, err)
		require.NoError(t, root.Close())

		_, err = fingerprintPinnedPathAtRoot(path, path, handle, info, root)

		require.ErrorContains(t, err, "stat fingerprint root")
	})

	t.Run("outside pinned root", func(t *testing.T) {
		base := t.TempDir()
		path := filepath.Join(base, "directory")
		require.NoError(t, os.Mkdir(path, 0o700))
		handle, err := os.Open(path)
		require.NoError(t, err)
		defer func() { _ = handle.Close() }()
		info, err := handle.Stat()
		require.NoError(t, err)
		root, err := fileutil.OpenPinnedRoot(path, false)
		require.NoError(t, err)
		defer func() { _ = root.Close() }()

		_, err = fingerprintPinnedPathAtRoot(path, filepath.Join(base, "other"), handle, info, root)

		require.ErrorContains(t, err, "outside pinned root")
	})

	t.Run("namespace changes after walk", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("directory mode changes are not portable on Windows")
		}
		path := t.TempDir()
		handle, err := os.Open(path)
		require.NoError(t, err)
		defer func() { _ = handle.Close() }()
		info, err := handle.Stat()
		require.NoError(t, err)
		root, err := fileutil.OpenPinnedRoot(path, false)
		require.NoError(t, err)
		defer func() { _ = root.Close() }()

		_, err = fingerprintPinnedPathAtRootWithHooks(path, path, handle, info, root, &fingerprintWalkHooks{
			afterWalk: func() { require.NoError(t, os.Chmod(path, 0o755)) },
		})

		require.ErrorContains(t, err, "fingerprint namespace changed")
	})
}

func TestManagedTypeTransitionsRollbackReportsDescriptorRenameFailures(t *testing.T) {
	t.Run("replacement quarantine collision", func(t *testing.T) {
		transitions, _ := newFileToDirTransition(t)
		item := transitions.items[0]
		require.NoError(t, transitions.Promote(context.Background()))
		require.NoError(t, os.WriteFile(item.newPath, []byte("collision"), 0o600))

		err := transitions.Rollback(os.ErrInvalid)

		require.ErrorContains(t, err, "quarantine failed replacement")
	})

	t.Run("closed parent while restoring original", func(t *testing.T) {
		transitions, _ := newFileToDirTransition(t)
		item := transitions.items[0]
		require.NoError(t, renameNoReplacePinned(item.parentHandle, item.targetPath, item.parentHandle, item.oldPath))
		item.quarantined = true
		require.NoError(t, item.parentHandle.Close())

		err := transitions.Rollback(os.ErrInvalid)

		require.ErrorContains(t, err, "restore original destination")
	})
}

func TestRemoveStaleFilesUnderRootRejectsInvalidSourceAndTarget(t *testing.T) {
	base := evalSymlinks(t, t.TempDir())
	deployRoot := filepath.Join(base, "appdata")
	targetDir := filepath.Join(deployRoot, "service")
	sourceDir := filepath.Join(base, "source")
	require.NoError(t, os.MkdirAll(targetDir, 0o755))
	require.NoError(t, os.MkdirAll(sourceDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "present.yml"), []byte("present"), 0o644))
	root, err := fileutil.OpenPinnedRoot(deployRoot, false)
	require.NoError(t, err)
	defer func() { _ = root.Close() }()

	t.Run("target escapes deploy root", func(t *testing.T) {
		err := removeStaleFilesUnderRoot(context.Background(), sourceDir, filepath.Join(base, "outside"), deployRoot, root, nil, map[string]bool{"old.yml": true})

		require.ErrorContains(t, err, "escapes deploy root")
	})

	t.Run("source path is not traversable", func(t *testing.T) {
		require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "nested"), []byte("file parent"), 0o600))
		require.NoError(t, os.MkdirAll(filepath.Join(targetDir, "nested"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(targetDir, "nested", "old.yml"), []byte("old"), 0o600))

		err := removeStaleFilesUnderRoot(context.Background(), sourceDir, targetDir, deployRoot, root, nil, map[string]bool{"nested/old.yml": true})

		require.ErrorContains(t, err, "stat source path nested/old.yml")
	})

	t.Run("cancellation reaches pinned walk", func(t *testing.T) {
		ctx := &cancelOnErrCheckContext{Context: context.Background(), cancelOn: 3}
		err := removeStaleFilesUnderRoot(ctx, sourceDir, targetDir, deployRoot, root, nil, map[string]bool{"old.yml": true})

		require.ErrorIs(t, err, context.Canceled)
	})
}

func TestRemoveStaleFilesUnderRootReportsRemovalFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permission behavior is not portable on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("permission failure is not observable when tests run as root")
	}
	base := evalSymlinks(t, t.TempDir())
	deployRoot := filepath.Join(base, "appdata")
	targetDir := filepath.Join(deployRoot, "service")
	sourceDir := filepath.Join(base, "source")
	locked := filepath.Join(targetDir, "locked")
	require.NoError(t, os.MkdirAll(locked, 0o755))
	require.NoError(t, os.MkdirAll(sourceDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "present.yml"), []byte("present"), 0o644))
	stale := filepath.Join(locked, "old.yml")
	require.NoError(t, os.WriteFile(stale, []byte("old"), 0o600))
	require.NoError(t, os.Chmod(locked, 0o555))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	root, err := fileutil.OpenPinnedRoot(deployRoot, false)
	require.NoError(t, err)
	defer func() { _ = root.Close() }()

	err = removeStaleFilesUnderRoot(context.Background(), sourceDir, targetDir, deployRoot, root, nil, map[string]bool{"locked/old.yml": true})

	require.ErrorContains(t, err, "stale file(s) could not be removed")
	assert.FileExists(t, stale)
}

func TestRemoveStaleFilesUnderRootReportsSourceInspectionFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permission behavior is not portable on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("permission failure is not observable when tests run as root")
	}
	base := evalSymlinks(t, t.TempDir())
	deployRoot := filepath.Join(base, "appdata")
	targetDir := filepath.Join(deployRoot, "service")
	sourceDir := filepath.Join(base, "source")
	require.NoError(t, os.MkdirAll(targetDir, 0o755))
	require.NoError(t, os.Mkdir(sourceDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "present.yml"), []byte("present"), 0o644))
	require.NoError(t, os.Chmod(sourceDir, 0))
	t.Cleanup(func() { _ = os.Chmod(sourceDir, 0o700) })
	root, err := fileutil.OpenPinnedRoot(deployRoot, false)
	require.NoError(t, err)
	defer func() { _ = root.Close() }()

	err = removeStaleFilesUnderRoot(context.Background(), sourceDir, targetDir, deployRoot, root, nil, map[string]bool{"old.yml": true})

	require.ErrorContains(t, err, "inspect source")
}
