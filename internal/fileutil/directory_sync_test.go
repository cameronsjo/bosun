package fileutil

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCopyFileWithOps_InvokesConfiguredSyncOnce(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	src := filepath.Join(tmpDir, "source.txt")
	dst := filepath.Join(tmpDir, "nested", "destination.txt")
	require.NoError(t, os.WriteFile(src, []byte("payload"), 0o644))

	var synced []string
	err := copyFileWithOps(context.Background(), src, dst, (*os.File).Chmod, func(dir string) error {
		synced = append(synced, dir)
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Dir(dst)}, synced)
}

func TestCopyDirWithOps_BatchesDestinationParentSyncs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		files    []string
		wantDirs []string
	}{
		{
			name:     "multiple files in one parent",
			files:    []string{"a.txt", "b.txt", "c.txt"},
			wantDirs: []string{"."},
		},
		{
			name:     "files in distinct nested parents",
			files:    []string{"a.txt", filepath.Join("alpha", "b.txt"), filepath.Join("zeta", "c.txt")},
			wantDirs: []string{".", "alpha", "zeta"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tmpDir := t.TempDir()
			srcDir := filepath.Join(tmpDir, "source")
			dstDir := filepath.Join(tmpDir, "destination")
			for _, rel := range tt.files {
				writeTestFile(t, filepath.Join(srcDir, rel), rel)
			}

			var synced []string
			err := copyDirWithOps(context.Background(), srcDir, dstDir, copyFileWithoutDirSyncContext, func(dir string) error {
				rel, relErr := filepath.Rel(dstDir, dir)
				require.NoError(t, relErr)
				synced = append(synced, rel)
				return nil
			})

			require.NoError(t, err)
			assert.Equal(t, tt.wantDirs, synced)
			for _, rel := range tt.files {
				assert.FileExists(t, filepath.Join(dstDir, rel))
			}
		})
	}
}

func TestCopyDirIfChangedWithOps_SyncsOnlyChangedParents(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		changed     []string
		unchanged   []string
		wantDirs    []string
		wantWritten []string
	}{
		{
			name:        "multiple changed files in one parent",
			changed:     []string{"a.txt", "b.txt"},
			wantDirs:    []string{"."},
			wantWritten: []string{"a.txt", "b.txt"},
		},
		{
			name:        "changed files in different parents",
			changed:     []string{"a.txt", filepath.Join("nested", "b.txt")},
			wantDirs:    []string{".", "nested"},
			wantWritten: []string{"a.txt", "nested", filepath.Join("nested", "b.txt")},
		},
		{
			name:        "unchanged-only parent adds no sync",
			changed:     []string{"changed.txt"},
			unchanged:   []string{filepath.Join("stable", "same.txt")},
			wantDirs:    []string{"."},
			wantWritten: []string{"changed.txt"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tmpDir := t.TempDir()
			srcDir := filepath.Join(tmpDir, "source")
			dstDir := filepath.Join(tmpDir, "destination")
			for _, rel := range tt.changed {
				writeTestFile(t, filepath.Join(srcDir, rel), "new "+rel)
			}
			for _, rel := range tt.unchanged {
				writeTestFile(t, filepath.Join(srcDir, rel), "same")
				writeTestFile(t, filepath.Join(dstDir, rel), "same")
			}

			var synced []string
			written, err := copyDirIfChangedWithOps(context.Background(),
				srcDir,
				dstDir,
				copyFileIfChangedDeferredWithoutDirSync,
				func(dir string) error {
					rel, relErr := filepath.Rel(dstDir, dir)
					require.NoError(t, relErr)
					synced = append(synced, rel)
					return nil
				},
			)

			require.NoError(t, err)
			assert.Equal(t, tt.wantDirs, synced)
			assert.Equal(t, tt.wantWritten, written)
		})
	}
}

func TestCopyDirIfChangedWithOps_SyncsEveryParentBeforeVerification(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	srcDir := filepath.Join(tmpDir, "source")
	dstDir := filepath.Join(tmpDir, "destination")
	writeTestFile(t, filepath.Join(srcDir, "a.txt"), "a")
	writeTestFile(t, filepath.Join(srcDir, "nested", "b.txt"), "b")

	var events []string
	written, err := copyDirIfChangedWithOps(context.Background(),
		srcDir,
		dstDir,
		func(_ context.Context, src, dst string) (bool, postWriteVerification, error) {
			return copyFileIfChangedDeferredWithCopy(context.Background(),
				src,
				dst,
				func(path string) ([sha256.Size]byte, error) {
					rel, relErr := filepath.Rel(dstDir, path)
					require.NoError(t, relErr)
					events = append(events, "verify "+rel)
					return FileHash(path)
				},
				copyFileWithoutDirSyncContext,
			)
		},
		func(dir string) error {
			rel, relErr := filepath.Rel(dstDir, dir)
			require.NoError(t, relErr)
			events = append(events, "sync "+rel)
			return nil
		},
	)

	require.NoError(t, err)
	assert.Equal(t, []string{
		"sync .",
		"sync nested",
		"verify a.txt",
		"verify " + filepath.Join("nested", "b.txt"),
	}, events)
	assert.Equal(t, []string{"a.txt", "nested", filepath.Join("nested", "b.txt")}, written)
}

func TestCopyDirIfChangedWithOps_SyncFailurePreservesChangeSet(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	srcDir := filepath.Join(tmpDir, "source")
	dstDir := filepath.Join(tmpDir, "destination")
	writeTestFile(t, filepath.Join(srcDir, "a.txt"), "a")
	writeTestFile(t, filepath.Join(srcDir, "b.txt"), "b")

	syncErr := errors.New("injected directory sync failure")
	written, err := copyDirIfChangedWithOps(context.Background(),
		srcDir,
		dstDir,
		copyFileIfChangedDeferredWithoutDirSync,
		func(string) error { return syncErr },
	)

	require.ErrorIs(t, err, syncErr)
	assert.ErrorContains(t, err, "sync destination directory")
	assert.Equal(t, []string{"a.txt", "b.txt"}, written,
		"renamed files remain part of the change set when the later batch sync fails")
	assert.FileExists(t, filepath.Join(dstDir, "a.txt"))
	assert.FileExists(t, filepath.Join(dstDir, "b.txt"))
}

func TestCopyDirIfChangedWithOps_FlushesPriorParentsAfterCopyFailure(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	srcDir := filepath.Join(tmpDir, "source")
	dstDir := filepath.Join(tmpDir, "destination")
	writeTestFile(t, filepath.Join(srcDir, "a-good.txt"), "good")
	writeTestFile(t, filepath.Join(srcDir, "b-bad.txt"), "bad")
	writeTestFile(t, filepath.Join(srcDir, "c-unvisited.txt"), "unvisited")

	copyErr := errors.New("injected copy failure")
	flushErr := errors.New("injected flush failure")
	var synced []string
	written, err := copyDirIfChangedWithOps(context.Background(),
		srcDir,
		dstDir,
		func(_ context.Context, src, dst string) (bool, postWriteVerification, error) {
			if filepath.Base(src) == "b-bad.txt" {
				return false, nil, copyErr
			}
			if err := copyFileWithoutDirSync(src, dst); err != nil {
				return false, nil, err
			}
			return true, nil, nil
		},
		func(dir string) error {
			synced = append(synced, dir)
			return flushErr
		},
	)

	require.Error(t, err)
	assert.ErrorIs(t, err, copyErr, "the primary walk/copy error must be preserved")
	assert.ErrorIs(t, err, flushErr, "the deferred durability failure must also be exposed")
	assert.Equal(t, []string{"a-good.txt"}, written)
	assert.Equal(t, []string{dstDir}, synced)
	assert.FileExists(t, filepath.Join(dstDir, "a-good.txt"))
	assert.NoFileExists(t, filepath.Join(dstDir, "c-unvisited.txt"))
}

func TestCopyDirIfChangedWithOps_JoinsCopyFlushAndVerificationFailures(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	srcDir := filepath.Join(tmpDir, "source")
	dstDir := filepath.Join(tmpDir, "destination")
	writeTestFile(t, filepath.Join(srcDir, "a-good.txt"), "good")
	writeTestFile(t, filepath.Join(srcDir, "b-bad.txt"), "bad")

	copyErr := errors.New("injected copy failure")
	flushErr := errors.New("injected flush failure")
	verifyErr := errors.New("injected verification failure")
	var events []string
	written, err := copyDirIfChangedWithOps(context.Background(),
		srcDir,
		dstDir,
		func(_ context.Context, src, dst string) (bool, postWriteVerification, error) {
			if filepath.Base(src) == "b-bad.txt" {
				return false, nil, copyErr
			}
			require.NoError(t, copyFileWithoutDirSync(src, dst))
			return true, func() error {
				events = append(events, "verify")
				return verifyErr
			}, nil
		},
		func(string) error {
			events = append(events, "sync")
			return flushErr
		},
	)

	require.Error(t, err)
	assert.ErrorIs(t, err, copyErr)
	assert.ErrorIs(t, err, flushErr)
	assert.ErrorIs(t, err, verifyErr)
	assert.Equal(t, []string{"sync", "verify"}, events)
	assert.Equal(t, []string{"a-good.txt"}, written)
}

func TestCopyDirIfChangedWithOps_ReportsDeferredVerificationFailure(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	srcDir := filepath.Join(tmpDir, "source")
	dstDir := filepath.Join(tmpDir, "destination")
	writeTestFile(t, filepath.Join(srcDir, "config.yml"), "new")

	readbackErr := errors.New("injected readback failure")
	written, err := copyDirIfChangedWithOps(context.Background(),
		srcDir,
		dstDir,
		func(_ context.Context, src, dst string) (bool, postWriteVerification, error) {
			return copyFileIfChangedDeferredWithCopy(context.Background(),
				src,
				dst,
				func(string) ([sha256.Size]byte, error) {
					return [sha256.Size]byte{}, readbackErr
				},
				copyFileWithoutDirSyncContext,
			)
		},
		syncDestinationDir,
	)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPostWriteVerification)
	assert.ErrorIs(t, err, readbackErr)
	assert.Equal(t, []string{"config.yml"}, written)
	assert.FileExists(t, filepath.Join(dstDir, "config.yml"))
}

func TestRunPostWriteVerifications_AttemptsEveryCallback(t *testing.T) {
	t.Parallel()

	firstErr := errors.New("first verification failed")
	lastErr := errors.New("last verification failed")
	var calls []string
	err := runPostWriteVerifications([]postWriteVerification{
		func() error {
			calls = append(calls, "first")
			return firstErr
		},
		func() error {
			calls = append(calls, "middle")
			return nil
		},
		func() error {
			calls = append(calls, "last")
			return lastErr
		},
	})

	assert.Equal(t, []string{"first", "middle", "last"}, calls)
	assert.ErrorIs(t, err, firstErr)
	assert.ErrorIs(t, err, lastErr)
}

func TestCopyDirWithOps_FlushesPriorParentsAfterCopyFailure(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	srcDir := filepath.Join(tmpDir, "source")
	dstDir := filepath.Join(tmpDir, "destination")
	writeTestFile(t, filepath.Join(srcDir, "a-good.txt"), "good")
	writeTestFile(t, filepath.Join(srcDir, "b-bad.txt"), "bad")

	copyErr := errors.New("injected copy failure")
	var synced []string
	err := copyDirWithOps(context.Background(),
		srcDir,
		dstDir,
		func(_ context.Context, src, dst string) error {
			if filepath.Base(src) == "b-bad.txt" {
				return copyErr
			}
			return copyFileWithoutDirSync(src, dst)
		},
		func(dir string) error {
			synced = append(synced, dir)
			return nil
		},
	)

	require.ErrorIs(t, err, copyErr)
	assert.Equal(t, copyErr, err, "a successful deferred flush must preserve the primary error value")
	assert.Equal(t, []string{dstDir}, synced)
	assert.FileExists(t, filepath.Join(dstDir, "a-good.txt"))
}

func TestSyncParentDirs_AttemptsEveryParentInDeterministicOrder(t *testing.T) {
	t.Parallel()

	parents := map[string]struct{}{
		"/zeta":  {},
		"/alpha": {},
		"/mid":   {},
	}
	firstErr := errors.New("first sync failed")
	lastErr := errors.New("last sync failed")
	var synced []string
	err := syncParentDirs(parents, func(dir string) error {
		synced = append(synced, dir)
		switch dir {
		case "/alpha":
			return firstErr
		case "/zeta":
			return lastErr
		default:
			return nil
		}
	})

	assert.Equal(t, []string{"/alpha", "/mid", "/zeta"}, synced)
	assert.ErrorIs(t, err, firstErr)
	assert.ErrorIs(t, err, lastErr)
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}
