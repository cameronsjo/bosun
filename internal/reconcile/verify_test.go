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

func TestVerifyDeployTarget_EmptyWrittenFiles_Against_NonEmptySource_Errors(t *testing.T) {
	// Layer 1.3, #214: this is the silent-success signature — source has files
	// but CopyDirIfChanged returned no writes. Either render produced nothing
	// useful or hashes matched stale destination content. Either way, fail.
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	startTime := time.Now().Add(-1 * time.Minute)

	touchFile(t, filepath.Join(src, "compose.yml"), "services:\n  foo: {}\n", time.Now())
	require.NoError(t, os.MkdirAll(dst, 0755))

	err := verifyDeployTarget(src, dst, nil, startTime)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDeployInvariantEmptyWrite)
	assert.Contains(t, err.Error(), "src=")
	assert.Contains(t, err.Error(), "dst=")
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

func TestVerifyDeployTarget_SrcIsRegularFile_EmptyWritten_Errors(t *testing.T) {
	dir := t.TempDir()
	srcFile := filepath.Join(dir, "src", "single.yml")
	dstDir := filepath.Join(dir, "dst")

	touchFile(t, srcFile, "v1", time.Now())
	require.NoError(t, os.MkdirAll(dstDir, 0755))

	startTime := time.Now().Add(-1 * time.Minute)
	err := verifyDeployTarget(srcFile, dstDir, nil, startTime)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDeployInvariantEmptyWrite)
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

// silentSyncReproduction is the GH#214 shape: src and dst contain identical
// bytes, so CopyDirIfChanged hashes the two and records zero writes despite
// the deploy "succeeding."
func TestDeployLocal_SilentEmptyWrite_FailsInvariant(t *testing.T) {
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

	_, err := r.deployLocal(context.Background())
	require.Error(t, err, "deployLocal should fail when sync writes nothing against a non-empty source")
	assert.ErrorIs(t, err, ErrDeployInvariantEmptyWrite)
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

	result, err := r.deployLocal(context.Background())
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

	result, err := r.deployLocal(context.Background())
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.NotEmpty(t, result.WrittenFiles, "expected at least one file to be written")
}
