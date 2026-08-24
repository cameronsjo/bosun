package reconcile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// touchFile writes content to path with mtime set to t. Creates parent dirs.
func touchFile(t *testing.T, path, content string, mtime time.Time) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	require.NoError(t, os.Chtimes(path, mtime, mtime))
}

func TestVerifyDeployTarget_HealthyDeploy_Passes(t *testing.T) {
	// Layer 1.3, #214: every WrittenFiles entry exists at dst with fresh mtime.
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	startTime := time.Now().Add(-1 * time.Minute)

	// Source has content (non-empty), destination has matching fresh files.
	touchFile(t, filepath.Join(src, "a.yml"), "a", time.Now())
	touchFile(t, filepath.Join(src, "nested/b.yml"), "b", time.Now())
	touchFile(t, filepath.Join(dst, "a.yml"), "a", time.Now())
	touchFile(t, filepath.Join(dst, "nested/b.yml"), "b", time.Now())

	err := verifyDeployTarget(src, dst, []string{"a.yml", "nested/b.yml"}, startTime)
	assert.NoError(t, err)
}

// TestVerifyDeployTarget_ZeroWriteScenarios covers the invariant's empty-writes
// branch: when a sync records no writes against a non-empty source, the gate
// inspects the destination directly. A destination that already content-matches
// the source is a legitimate no-op (GH#330) and passes; a destination missing
// any source file is a #214 silent-sync failure and errors. setup builds the
// source/destination trees and returns the (src, dst) pair passed to the gate —
// dst is the destination directory for directory sources and the parent
// directory for single-file sources, matching how reconcile.go calls it.
func TestVerifyDeployTarget_ZeroWriteScenarios(t *testing.T) {
	stale := time.Now().Add(-7 * 24 * time.Hour)

	tests := []struct {
		name        string
		setup       func(t *testing.T, dir string) (src, dst string)
		wantErr     bool
		errContains []string
	}{
		{
			name: "dir source, destination content-matches → no-op passes",
			setup: func(t *testing.T, dir string) (string, string) {
				src, dst := filepath.Join(dir, "src"), filepath.Join(dir, "dst")
				touchFile(t, filepath.Join(src, "dnscrypt-proxy.toml"), "server_names = []\n", stale)
				touchFile(t, filepath.Join(dst, "dnscrypt-proxy.toml"), "server_names = []\n", stale)
				return src, dst
			},
		},
		{
			name: "dir source, nested files match → no-op passes",
			setup: func(t *testing.T, dir string) (string, string) {
				src, dst := filepath.Join(dir, "src"), filepath.Join(dir, "dst")
				touchFile(t, filepath.Join(src, "top.yml"), "a", stale)
				touchFile(t, filepath.Join(src, "sub", "nested.yml"), "b", stale)
				touchFile(t, filepath.Join(dst, "top.yml"), "a", stale)
				touchFile(t, filepath.Join(dst, "sub", "nested.yml"), "b", stale)
				return src, dst
			},
		},
		{
			name: "dir source, destination empty → silent failure errors",
			setup: func(t *testing.T, dir string) (string, string) {
				src, dst := filepath.Join(dir, "src"), filepath.Join(dir, "dst")
				touchFile(t, filepath.Join(src, "compose.yml"), "services:\n  foo: {}\n", time.Now())
				require.NoError(t, os.MkdirAll(dst, 0755)) // exists but empty — file missing.
				return src, dst
			},
			wantErr:     true,
			errContains: []string{"src=", "dst=", "compose.yml"},
		},
		{
			name: "dir source, one file missing → silent failure errors",
			setup: func(t *testing.T, dir string) (string, string) {
				src, dst := filepath.Join(dir, "src"), filepath.Join(dir, "dst")
				touchFile(t, filepath.Join(src, "present.yml"), "a", stale)
				touchFile(t, filepath.Join(src, "absent.yml"), "b", stale)
				touchFile(t, filepath.Join(dst, "present.yml"), "a", stale) // absent.yml NOT at dst.
				return src, dst
			},
			wantErr:     true,
			errContains: []string{"absent.yml"},
		},
		{
			// Existence-only would PASS this (the file is present); content-
			// equality catches the stale write the sync silently failed to replace.
			name: "dir source, destination has stale content → silent failure errors",
			setup: func(t *testing.T, dir string) (string, string) {
				src, dst := filepath.Join(dir, "src"), filepath.Join(dir, "dst")
				touchFile(t, filepath.Join(src, "config.toml"), "server = \"new\"\n", stale)
				touchFile(t, filepath.Join(dst, "config.toml"), "server = \"old\"\n", stale)
				return src, dst
			},
			wantErr:     true,
			errContains: []string{"config.toml"},
		},
		{
			name: "dir source, nested file has stale content → silent failure errors",
			setup: func(t *testing.T, dir string) (string, string) {
				src, dst := filepath.Join(dir, "src"), filepath.Join(dir, "dst")
				touchFile(t, filepath.Join(src, "top.yml"), "a", stale)
				touchFile(t, filepath.Join(src, "sub", "nested.yml"), "fresh", stale)
				touchFile(t, filepath.Join(dst, "top.yml"), "a", stale)
				touchFile(t, filepath.Join(dst, "sub", "nested.yml"), "stale", stale)
				return src, dst
			},
			wantErr:     true,
			errContains: []string{"nested.yml"},
		},
		{
			name: "file source, destination has stale content → silent failure errors",
			setup: func(t *testing.T, dir string) (string, string) {
				srcFile := filepath.Join(dir, "src", "single.yml")
				dstDir := filepath.Join(dir, "dst")
				touchFile(t, srcFile, "v2", stale)
				touchFile(t, filepath.Join(dstDir, "single.yml"), "v1", stale)
				return srcFile, dstDir
			},
			wantErr:     true,
			errContains: []string{"single.yml"},
		},
		{
			// Symlink-only source: the copy path never deploys symlinks, so the
			// invariant must impose no requirement and pass.
			name: "dir source with only a symlink → passes (symlink not deployed)",
			setup: func(t *testing.T, dir string) (string, string) {
				src, dst := filepath.Join(dir, "src"), filepath.Join(dir, "dst")
				require.NoError(t, os.MkdirAll(src, 0755))
				require.NoError(t, os.MkdirAll(dst, 0755))
				require.NoError(t, os.Symlink("/nonexistent/target", filepath.Join(src, "link.yml")))
				return src, dst
			},
		},
		{
			name: "file source, destination matches → no-op passes",
			setup: func(t *testing.T, dir string) (string, string) {
				srcFile := filepath.Join(dir, "src", "single.yml")
				dstDir := filepath.Join(dir, "dst")
				touchFile(t, srcFile, "v1", stale)
				touchFile(t, filepath.Join(dstDir, "single.yml"), "v1", stale)
				return srcFile, dstDir // dst is the parent dir for file targets.
			},
		},
		{
			name: "file source, destination empty → silent failure errors",
			setup: func(t *testing.T, dir string) (string, string) {
				srcFile := filepath.Join(dir, "src", "single.yml")
				dstDir := filepath.Join(dir, "dst")
				touchFile(t, srcFile, "v1", stale)
				require.NoError(t, os.MkdirAll(dstDir, 0755))
				return srcFile, dstDir
			},
			wantErr:     true,
			errContains: []string{"single.yml"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src, dst := tt.setup(t, t.TempDir())
			startTime := time.Now().Add(-1 * time.Minute)

			err := verifyDeployTarget(src, dst, nil, startTime)
			if !tt.wantErr {
				assert.NoError(t, err, "zero-write sync against a content-matched destination must pass")
				return
			}
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrDeployInvariantEmptyWrite)
			for _, want := range tt.errContains {
				assert.Contains(t, err.Error(), want)
			}
		})
	}
}

func TestVerifyDeployTarget_EmptyWrittenFiles_Against_EmptySource_Passes(t *testing.T) {
	// Edge case: an empty source legitimately produces no writes. Don't fail.
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	startTime := time.Now().Add(-1 * time.Minute)

	require.NoError(t, os.MkdirAll(src, 0755))
	require.NoError(t, os.MkdirAll(dst, 0755))

	err := verifyDeployTarget(src, dst, nil, startTime)
	assert.NoError(t, err)
}

func TestVerifyDeployTarget_DirectoryOnlyWriteStillChecksRegularSourceFiles(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	startTime := time.Now().Add(-time.Minute)

	require.NoError(t, os.MkdirAll(filepath.Join(src, "empty"), 0755))
	touchFile(t, filepath.Join(src, "config.yml"), "expected", time.Now())
	require.NoError(t, os.MkdirAll(filepath.Join(dst, "empty"), 0755))
	// config.yml is intentionally absent. A directory-only WrittenFiles entry
	// must not suppress the zero-regular-file-write content invariant.

	err := verifyDeployTarget(src, dst, []string{"empty"}, startTime)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDeployInvariantEmptyWrite)
	assert.Contains(t, err.Error(), "config.yml")
}

func TestVerifyDeployTarget_WrittenDirectoryRequiresDirectoryDestination(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	startTime := time.Now().Add(-time.Minute)

	require.NoError(t, os.MkdirAll(filepath.Join(src, "empty"), 0755))
	touchFile(t, filepath.Join(dst, "empty"), "not a directory", time.Now())

	err := verifyDeployTarget(src, dst, []string{"empty"}, startTime)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDeployInvariantWrongType)
	assert.Contains(t, err.Error(), "want=directory")
}

func TestVerifyDeployTarget_StaleDestination_Errors(t *testing.T) {
	// Layer 1.3, #214: the freshrss-shape failure — CopyDirIfChanged claimed
	// to write the file (or we recorded it as written), but the destination's
	// mtime is older than the deploy start time. The write silently failed.
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")

	touchFile(t, filepath.Join(src, "config.yml"), "fresh", time.Now())

	// Destination mtime is 13 days stale (matches the GH#214 field report).
	staleTime := time.Now().Add(-13 * 24 * time.Hour)
	touchFile(t, filepath.Join(dst, "config.yml"), "old-content", staleTime)

	// Reconcile started "now" (well after the stale destination was touched).
	startTime := time.Now().Add(-1 * time.Minute)

	err := verifyDeployTarget(src, dst, []string{"config.yml"}, startTime)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDeployInvariantStaleMtime)
	assert.Contains(t, err.Error(), "config.yml")
}

func TestVerifyDeployTarget_MissingDestination_Errors(t *testing.T) {
	// Layer 1.3, #214: WrittenFiles records a path that doesn't exist on disk.
	// Either the write was rolled back, the FS dropped it, or the bookkeeping
	// is wrong. Surface it.
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")

	touchFile(t, filepath.Join(src, "a.yml"), "a", time.Now())
	require.NoError(t, os.MkdirAll(dst, 0755))
	// dst/a.yml intentionally not created.

	startTime := time.Now().Add(-1 * time.Minute)
	err := verifyDeployTarget(src, dst, []string{"a.yml"}, startTime)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDeployInvariantMissingFile)
	assert.Contains(t, err.Error(), "a.yml")
}

func TestVerifyDeployTarget_MtimeExactlyEqualToStartTime_Passes(t *testing.T) {
	// Boundary: mt.Before(st) is strict less-than, so mtime == startTime
	// (after second-truncation) must pass. Captures the invariant edge case
	// where a write completes in the same second the reconcile began.
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")

	exact := time.Now().Truncate(time.Second)
	touchFile(t, filepath.Join(src, "a.yml"), "a", exact)
	touchFile(t, filepath.Join(dst, "a.yml"), "a", exact)

	err := verifyDeployTarget(src, dst, []string{"a.yml"}, exact)
	assert.NoError(t, err, "mtime exactly equal to startTime must pass (Before is strict)")
}

func TestVerifyDeployTarget_ZeroByteSourceFile_Healthy(t *testing.T) {
	// A 0-byte file in src and dst is legitimate (empty config, empty .gitkeep).
	// The mtime check should still apply but size should not be a signal.
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")

	now := time.Now()
	touchFile(t, filepath.Join(src, "empty.yml"), "", now)
	touchFile(t, filepath.Join(dst, "empty.yml"), "", now)

	startTime := now.Add(-1 * time.Minute)
	err := verifyDeployTarget(src, dst, []string{"empty.yml"}, startTime)
	assert.NoError(t, err)
}

func TestVerifyDeployTarget_SubSecondMtime_Tolerated(t *testing.T) {
	// FAT/FUSE filesystems have second-resolution mtime. A write completing
	// within the same wall-clock second as startTime must not be flagged as
	// stale by sub-second drift. verifyDeployTarget truncates both sides to
	// the second for the comparison.
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")

	now := time.Now().Truncate(time.Second)
	startTime := now.Add(800 * time.Millisecond) // 0.8s into the current second
	mtime := now                                 // file written at second-boundary

	touchFile(t, filepath.Join(src, "a.yml"), "a", now)
	touchFile(t, filepath.Join(dst, "a.yml"), "a", mtime)

	err := verifyDeployTarget(src, dst, []string{"a.yml"}, startTime)
	assert.NoError(t, err, "sub-second mtime drift should not trip the invariant")
}

// TestVerifyDeployTarget_SrcIsRegularFile exercises the single-file target
// shape: src is a file path (not a directory), dst is its parent directory,
// writtenRel is [filepath.Base(srcFile)] per DeployLocalFile's bookkeeping.
func TestVerifyDeployTarget_SrcIsRegularFile_HealthyPasses(t *testing.T) {
	dir := t.TempDir()
	srcFile := filepath.Join(dir, "src", "single.yml")
	dstDir := filepath.Join(dir, "dst")
	dstFile := filepath.Join(dstDir, "single.yml")

	touchFile(t, srcFile, "v1", time.Now())
	touchFile(t, dstFile, "v1", time.Now())

	startTime := time.Now().Add(-1 * time.Minute)
	err := verifyDeployTarget(srcFile, dstDir, []string{"single.yml"}, startTime)
	assert.NoError(t, err)
}

// TestDestinationSatisfiesSource exercises the helper directly, including
// branches verifyDeployTarget reaches only obliquely: the missing-source
// short-circuit, content-equality (present-but-differing), symlink skipping
// (Lstat semantics), and the walk stat-error path. The helper returns
// (sawFiles, mismatch, err): sawFiles is whether the source held any regular
// file; mismatch is the first absent-or-differing destination path ("" when
// satisfied).
func TestDestinationSatisfiesSource(t *testing.T) {
	t.Run("missing source: no files, no mismatch", func(t *testing.T) {
		dir := t.TempDir()
		sawFiles, mismatch, err := destinationSatisfiesSource(filepath.Join(dir, "nope"), dir)
		require.NoError(t, err)
		assert.False(t, sawFiles)
		assert.Empty(t, mismatch)
	})

	t.Run("dir source all present and content-equal: satisfied", func(t *testing.T) {
		dir := t.TempDir()
		src, dst := filepath.Join(dir, "src"), filepath.Join(dir, "dst")
		touchFile(t, filepath.Join(src, "a.yml"), "a", time.Now())
		touchFile(t, filepath.Join(src, "sub", "b.yml"), "b", time.Now())
		touchFile(t, filepath.Join(dst, "a.yml"), "a", time.Now())
		touchFile(t, filepath.Join(dst, "sub", "b.yml"), "b", time.Now())
		sawFiles, mismatch, err := destinationSatisfiesSource(src, dst)
		require.NoError(t, err)
		assert.True(t, sawFiles)
		assert.Empty(t, mismatch)
	})

	t.Run("dir source missing file: mismatch names destination path", func(t *testing.T) {
		dir := t.TempDir()
		src, dst := filepath.Join(dir, "src"), filepath.Join(dir, "dst")
		touchFile(t, filepath.Join(src, "a.yml"), "a", time.Now())
		require.NoError(t, os.MkdirAll(dst, 0755))
		sawFiles, mismatch, err := destinationSatisfiesSource(src, dst)
		require.NoError(t, err)
		assert.True(t, sawFiles)
		assert.Equal(t, filepath.Join(dst, "a.yml"), mismatch)
	})

	t.Run("dir source present but differing content: mismatch names destination path", func(t *testing.T) {
		dir := t.TempDir()
		src, dst := filepath.Join(dir, "src"), filepath.Join(dir, "dst")
		touchFile(t, filepath.Join(src, "a.yml"), "fresh", time.Now())
		touchFile(t, filepath.Join(dst, "a.yml"), "stale", time.Now())
		sawFiles, mismatch, err := destinationSatisfiesSource(src, dst)
		require.NoError(t, err)
		assert.True(t, sawFiles)
		assert.Equal(t, filepath.Join(dst, "a.yml"), mismatch)
	})

	t.Run("file source: equal satisfied, differing and absent mismatch", func(t *testing.T) {
		dir := t.TempDir()
		srcFile := filepath.Join(dir, "single.yml")
		touchFile(t, srcFile, "v", time.Now())

		dstEqual := filepath.Join(dir, "equal")
		touchFile(t, filepath.Join(dstEqual, "single.yml"), "v", time.Now())
		sawFiles, mismatch, err := destinationSatisfiesSource(srcFile, dstEqual)
		require.NoError(t, err)
		assert.True(t, sawFiles)
		assert.Empty(t, mismatch)

		dstDiffer := filepath.Join(dir, "differ")
		touchFile(t, filepath.Join(dstDiffer, "single.yml"), "other", time.Now())
		sawFiles, mismatch, err = destinationSatisfiesSource(srcFile, dstDiffer)
		require.NoError(t, err)
		assert.True(t, sawFiles)
		assert.Equal(t, filepath.Join(dstDiffer, "single.yml"), mismatch)

		dstAbsent := filepath.Join(dir, "absent")
		require.NoError(t, os.MkdirAll(dstAbsent, 0755))
		sawFiles, mismatch, err = destinationSatisfiesSource(srcFile, dstAbsent)
		require.NoError(t, err)
		assert.True(t, sawFiles)
		assert.Equal(t, filepath.Join(dstAbsent, "single.yml"), mismatch)
	})

	t.Run("symlink source file: no requirement (not deployed)", func(t *testing.T) {
		dir := t.TempDir()
		srcLink := filepath.Join(dir, "link.yml")
		require.NoError(t, os.Symlink("/nonexistent/target", srcLink))
		sawFiles, mismatch, err := destinationSatisfiesSource(srcLink, filepath.Join(dir, "dst"))
		require.NoError(t, err)
		assert.False(t, sawFiles, "a symlink is not a regular file, so it imposes no requirement")
		assert.Empty(t, mismatch)
	})

	t.Run("dir source: symlinks skipped, regular files still checked", func(t *testing.T) {
		dir := t.TempDir()
		src, dst := filepath.Join(dir, "src"), filepath.Join(dir, "dst")
		touchFile(t, filepath.Join(src, "real.yml"), "x", time.Now())
		require.NoError(t, os.Symlink("/nonexistent/target", filepath.Join(src, "link.yml")))
		touchFile(t, filepath.Join(dst, "real.yml"), "x", time.Now())
		// dst has no link.yml — but the source symlink is skipped, so this is satisfied.
		sawFiles, mismatch, err := destinationSatisfiesSource(src, dst)
		require.NoError(t, err)
		assert.True(t, sawFiles)
		assert.Empty(t, mismatch, "the source symlink must not be required at the destination")
	})

	t.Run("dir source with only symlinks: no regular files seen", func(t *testing.T) {
		dir := t.TempDir()
		src, dst := filepath.Join(dir, "src"), filepath.Join(dir, "dst")
		require.NoError(t, os.MkdirAll(src, 0755))
		require.NoError(t, os.Symlink("/nonexistent/target", filepath.Join(src, "link.yml")))
		sawFiles, mismatch, err := destinationSatisfiesSource(src, dst)
		require.NoError(t, err)
		assert.False(t, sawFiles)
		assert.Empty(t, mismatch)
	})

	t.Run("unreadable source subtree surfaces walk error", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("running as root bypasses directory permissions")
		}
		dir := t.TempDir()
		src := filepath.Join(dir, "src")
		touchFile(t, filepath.Join(src, "sub", "a.yml"), "a", time.Now())
		// Make the subdir untraversable so WalkDir's child walk errors.
		require.NoError(t, os.Chmod(filepath.Join(src, "sub"), 0000))
		t.Cleanup(func() { _ = os.Chmod(filepath.Join(src, "sub"), 0755) })

		_, _, err := destinationSatisfiesSource(src, filepath.Join(dir, "dst"))
		require.Error(t, err)
	})

	t.Run("lstat error (non-directory parent) surfaces, not treated as missing", func(t *testing.T) {
		dir := t.TempDir()
		// A regular file used as a path's parent component yields ENOTDIR from
		// Lstat — a real I/O error, distinct from fs.ErrNotExist (which would
		// short-circuit to "no files, no mismatch").
		notADir := filepath.Join(dir, "notadir")
		touchFile(t, notADir, "x", time.Now())
		sawFiles, mismatch, err := destinationSatisfiesSource(filepath.Join(notADir, "child"), filepath.Join(dir, "dst"))
		require.Error(t, err)
		assert.False(t, sawFiles)
		assert.Empty(t, mismatch)
	})

	t.Run("unreadable regular file in dir source surfaces hash error", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("running as root bypasses file permissions")
		}
		dir := t.TempDir()
		src, dst := filepath.Join(dir, "src"), filepath.Join(dir, "dst")
		touchFile(t, filepath.Join(src, "a.yml"), "a", time.Now())
		require.NoError(t, os.Chmod(filepath.Join(src, "a.yml"), 0000))
		t.Cleanup(func() { _ = os.Chmod(filepath.Join(src, "a.yml"), 0644) })

		// Walk reaches the regular file, then FileHash(src) fails to open it.
		_, _, err := destinationSatisfiesSource(src, dst)
		require.Error(t, err)
	})

	t.Run("unreadable single-file source surfaces hash error", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("running as root bypasses file permissions")
		}
		dir := t.TempDir()
		srcFile := filepath.Join(dir, "single.yml")
		touchFile(t, srcFile, "v", time.Now())
		require.NoError(t, os.Chmod(srcFile, 0000))
		t.Cleanup(func() { _ = os.Chmod(srcFile, 0644) })

		sawFiles, _, err := destinationSatisfiesSource(srcFile, filepath.Join(dir, "dst"))
		require.Error(t, err)
		assert.True(t, sawFiles, "a regular (if unreadable) file still counts as seen")
	})
}

// TestVerifyDeployTarget_CompareError_Surfaces covers the empty-writes branch's
// error path: when destinationSatisfiesSource cannot compare (an I/O failure
// hashing the source), verifyDeployTarget wraps it as a "compare source" error
// — distinct from the silent-sync sentinel, since the gate could not reach a
// verdict.
func TestVerifyDeployTarget_CompareError_Surfaces(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses file permissions")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	touchFile(t, filepath.Join(src, "a.yml"), "a", time.Now())
	require.NoError(t, os.Chmod(filepath.Join(src, "a.yml"), 0000))
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(src, "a.yml"), 0644) })

	err := verifyDeployTarget(src, dst, nil, time.Now().Add(-time.Minute))
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrDeployInvariantEmptyWrite, "an I/O comparison failure is not a silent-sync verdict")
	assert.Contains(t, err.Error(), "compare source")
}

func TestDirHasRegularFiles(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, root string) string
		want  bool
	}{
		{
			name: "empty directory returns false",
			setup: func(t *testing.T, root string) string {
				p := filepath.Join(root, "empty")
				require.NoError(t, os.MkdirAll(p, 0755))
				return p
			},
			want: false,
		},
		{
			name: "directory with regular file returns true",
			setup: func(t *testing.T, root string) string {
				p := filepath.Join(root, "has-file")
				touchFile(t, filepath.Join(p, "a.txt"), "x", time.Now())
				return p
			},
			want: true,
		},
		{
			name: "nested regular file returns true",
			setup: func(t *testing.T, root string) string {
				p := filepath.Join(root, "nested")
				touchFile(t, filepath.Join(p, "deep/deeper/a.txt"), "x", time.Now())
				return p
			},
			want: true,
		},
		{
			name: "only empty subdirectories returns false",
			setup: func(t *testing.T, root string) string {
				p := filepath.Join(root, "only-dirs")
				require.NoError(t, os.MkdirAll(filepath.Join(p, "a", "b", "c"), 0755))
				return p
			},
			want: false,
		},
		{
			name: "single regular file (not directory) returns true",
			setup: func(t *testing.T, root string) string {
				p := filepath.Join(root, "file.yml")
				touchFile(t, p, "x", time.Now())
				return p
			},
			want: true,
		},
		{
			name: "missing path returns false, nil",
			setup: func(t *testing.T, root string) string {
				return filepath.Join(root, "does-not-exist")
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			path := tt.setup(t, root)

			got, err := dirHasRegularFiles(path)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// Smoke test that the sentinel errors are distinct so callers can branch on them.
func TestDeployInvariantSentinels_AreDistinct(t *testing.T) {
	assert.False(t, errors.Is(ErrDeployInvariantEmptyWrite, ErrDeployInvariantStaleMtime))
	assert.False(t, errors.Is(ErrDeployInvariantEmptyWrite, ErrDeployInvariantMissingFile))
	assert.False(t, errors.Is(ErrDeployInvariantStaleMtime, ErrDeployInvariantMissingFile))
}

// --- Integration tests: invariant wiring through deployLocal ---

// noopComposeUp lets deployLocal tests run without a Docker daemon.
func noopComposeUp(_ context.Context, _ []string) error { return nil }

// deployLocalFixture lays out the staging + appdata tree shared by the
// deployLocal integration tests. Returns the two root paths so callers can
// vary the seeded content per test. cfg.DryRun must be false for invariants
// to run.
type deployLocalFixture struct {
	stagingDir, appdataDir string
	freshrssStaging        string // staging/unraid/appdata/freshrss
	freshrssAppdata        string // appdata/freshrss
	composeStaging         string // staging/unraid/compose
	composeAppdata         string // appdata/compose
}

func newDeployLocalFixture(t *testing.T) deployLocalFixture {
	t.Helper()
	tmpDir := t.TempDir()
	stagingDir := filepath.Join(tmpDir, "staging")
	appdataDir := filepath.Join(tmpDir, "appdata")
	return deployLocalFixture{
		stagingDir:      stagingDir,
		appdataDir:      appdataDir,
		freshrssStaging: filepath.Join(stagingDir, "unraid", "appdata", "freshrss"),
		freshrssAppdata: filepath.Join(appdataDir, "freshrss"),
		composeStaging:  filepath.Join(stagingDir, "unraid", "compose"),
		composeAppdata:  filepath.Join(appdataDir, "compose"),
	}
}

func newDeployLocalDeploy() *DeployOps {
	return &DeployOps{
		DryRun:          false,
		ProjectName:     "test",
		ContentHashSync: true,
		composeUpFn:     noopComposeUp,
	}
}

// TestDeployLocal_NoOpSync_PassesInvariant is the GH#330 shape: staging and
// appdata contain identical bytes, so CopyDirIfChanged hashes the two and
// records zero writes. The destination already content-matches the source, so
// this is a legitimate no-op and the deploy must succeed — not abort on the
// empty-write invariant. (Previously this exact scenario was asserted to fail
// under #214's overly aggressive invariant, which caused the GH#330 outage.)
func TestDeployLocal_NoOpSync_PassesInvariant(t *testing.T) {
	fx := newDeployLocalFixture(t)
	now := time.Now()
	touchFile(t, filepath.Join(fx.freshrssStaging, "config.yml"), "identical", now)
	touchFile(t, filepath.Join(fx.freshrssAppdata, "config.yml"), "identical", now)
	touchFile(t, filepath.Join(fx.composeStaging, "stub.yml"), "services:\n  stub: {}\n", now)
	touchFile(t, filepath.Join(fx.composeAppdata, "stub.yml"), "services:\n  stub: {}\n", now)

	cfg := &Config{
		StagingDir:       fx.stagingDir,
		InfraSubDir:      "unraid",
		LocalAppdataPath: fx.appdataDir,
	}
	r := NewReconciler(cfg, WithDeployOps(newDeployLocalDeploy()))

	result, err := r.deployLocal(context.Background(), nil)
	require.NoError(t, err, "a no-op sync against a content-matched destination must not trip the invariant")
	assert.NotNil(t, result)
}

// TestDeployLocal_StandardMode_PassesInvariant is the B1 shape: standard
// (non-content-hash) deploy nuke-and-replaces the target and records ZERO
// writes (DeployLocal never calls AddWritten in this mode). The invariant must
// not infer a silent-sync failure from the empty write list — it inspects the
// destination directly and passes because the freshly copied files are
// content-equal to the source. This locks in that the content-equality invariant
// covers both deploy strategies, not just content-hash sync.
func TestDeployLocal_StandardMode_PassesInvariant(t *testing.T) {
	fx := newDeployLocalFixture(t)
	now := time.Now()
	// Differing dst content forces standard mode to actually overwrite; it still
	// records no writes, so the invariant relies on post-deploy content equality.
	touchFile(t, filepath.Join(fx.freshrssStaging, "config.yml"), "new-content", now)
	touchFile(t, filepath.Join(fx.freshrssAppdata, "config.yml"), "old-content", now.Add(-24*time.Hour))
	touchFile(t, filepath.Join(fx.composeStaging, "stub.yml"), "services:\n  stub: {}\n", now)
	require.NoError(t, os.MkdirAll(fx.composeAppdata, 0755))

	cfg := &Config{
		StagingDir:       fx.stagingDir,
		InfraSubDir:      "unraid",
		LocalAppdataPath: fx.appdataDir,
	}
	// Standard mode: ContentHashSync disabled.
	standard := &DeployOps{DryRun: false, ProjectName: "test", ContentHashSync: false, composeUpFn: noopComposeUp}
	r := NewReconciler(cfg, WithDeployOps(standard))

	result, err := r.deployLocal(context.Background(), nil)
	require.NoError(t, err, "standard-mode deploy records zero writes but leaves dst content-equal; invariant must pass")
	require.NotNil(t, result)
	assert.Empty(t, result.WrittenFiles, "standard mode does not populate WrittenFiles")

	// Confirm the deploy actually landed the new content (not just that the gate passed).
	got, err := os.ReadFile(filepath.Join(fx.freshrssAppdata, "config.yml"))
	require.NoError(t, err)
	assert.Equal(t, "new-content", string(got))
}

func TestDeployLocal_SkipDeployInvariant_BypassesCheck(t *testing.T) {
	fx := newDeployLocalFixture(t)
	now := time.Now()
	touchFile(t, filepath.Join(fx.freshrssStaging, "config.yml"), "identical", now)
	touchFile(t, filepath.Join(fx.freshrssAppdata, "config.yml"), "identical", now)
	touchFile(t, filepath.Join(fx.composeStaging, "stub.yml"), "services:\n  stub: {}\n", now)
	touchFile(t, filepath.Join(fx.composeAppdata, "stub.yml"), "services:\n  stub: {}\n", now)

	cfg := &Config{
		SkipDeployInvariant: true,
		StagingDir:          fx.stagingDir,
		InfraSubDir:         "unraid",
		LocalAppdataPath:    fx.appdataDir,
	}
	r := NewReconciler(cfg, WithDeployOps(newDeployLocalDeploy()))

	result, err := r.deployLocal(context.Background(), nil)
	require.NoError(t, err, "SkipDeployInvariant=true should bypass the silent-write check")
	assert.NotNil(t, result)
}

func TestDeployLocal_HealthyDeploy_PassesInvariant(t *testing.T) {
	fx := newDeployLocalFixture(t)
	// Different src vs dst forces a real write.
	touchFile(t, filepath.Join(fx.freshrssStaging, "config.yml"), "new-content", time.Now())
	touchFile(t, filepath.Join(fx.freshrssAppdata, "config.yml"), "old-content", time.Now().Add(-24*time.Hour))
	touchFile(t, filepath.Join(fx.composeStaging, "stub.yml"), "services:\n  stub: {}\n", time.Now())
	require.NoError(t, os.MkdirAll(fx.composeAppdata, 0755))

	cfg := &Config{
		StagingDir:       fx.stagingDir,
		InfraSubDir:      "unraid",
		LocalAppdataPath: fx.appdataDir,
	}
	r := NewReconciler(cfg, WithDeployOps(newDeployLocalDeploy()))

	result, err := r.deployLocal(context.Background(), nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.NotEmpty(t, result.WrittenFiles, "expected at least one file to be written")
}
