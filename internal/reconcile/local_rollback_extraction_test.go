package reconcile

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	bosunlog "github.com/cameronsjo/bosun/internal/log"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func archiveMemberName(path string) string {
	return strings.TrimPrefix(filepath.ToSlash(path), "/")
}

func invalidArchiveHeaders() []struct {
	name      string
	malicious *tar.Header
} {
	return []struct {
		name      string
		malicious *tar.Header
	}{
		{name: "member traversal", malicious: regHdr("../../escape")},
		{name: "absolute symlink", malicious: &tar.Header{Name: "compose/escape", Typeflag: tar.TypeSymlink, Linkname: "/tmp", Mode: 0o777}},
		{name: "escaping symlink", malicious: &tar.Header{Name: "compose/escape", Typeflag: tar.TypeSymlink, Linkname: "../../../escape", Mode: 0o777}},
		{name: "escaping hardlink", malicious: &tar.Header{Name: "compose/escape", Typeflag: tar.TypeLink, Linkname: "../../../escape", Mode: 0o644}},
	}
}

func writeInvalidBackupArchive(t *testing.T, backupPath, live string, malicious *tar.Header) {
	t.Helper()
	writeGzTarArchiveHeaders(t, filepath.Join(backupPath, "configs.tar.gz"),
		regHdr(archiveMemberName(live)),
		malicious,
	)
}

func installDockerContentObserver(t *testing.T, live string) string {
	t.Helper()
	dir := t.TempDir()
	capture := filepath.Join(dir, "docker-observation.log")
	shim := "#!/bin/sh\n" +
		"cat \"$BOSUN_TEST_LIVE_FILE\" > \"$BOSUN_TEST_DOCKER_CAPTURE\"\n" +
		"printf '\\n%s\\n' \"$*\" >> \"$BOSUN_TEST_DOCKER_CAPTURE\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "docker"), []byte(shim), 0o755))
	t.Setenv("BOSUN_TEST_LIVE_FILE", live)
	t.Setenv("BOSUN_TEST_DOCKER_CAPTURE", capture)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return capture
}

func TestRollbackFromBackupSet_ValidArchiveRestoresBeforeCompose(t *testing.T) {
	base := t.TempDir()
	liveRoot := filepath.Join(base, "appdata")
	live := writeLive(t, liveRoot, "compose/core.yml", "OLD")
	backupPath := filepath.Join(base, "backup")
	writeTestBackupArchive(t, backupPath, live)
	require.NoError(t, os.WriteFile(live, []byte("NEW"), 0o644))
	capture := installDockerContentObserver(t, live)

	d := &DeployOps{ProjectName: "bosun"}
	err := d.RollbackFromBackupSet(context.Background(), RollbackSet{
		Files:        []string{live},
		ComposeFiles: []string{live},
		Root:         liveRoot,
	}, backupPath)

	require.NoError(t, err)
	observed, readErr := os.ReadFile(capture)
	require.NoError(t, readErr)
	assert.True(t, strings.HasPrefix(string(observed), "OLD\n"),
		"compose must observe the restored bytes, proving it runs after extraction and live copy")
}

func TestRollbackFromBackupSet_InvalidArchiveHasNoLiveSideEffects(t *testing.T) {
	for _, tt := range invalidArchiveHeaders() {
		t.Run(tt.name, func(t *testing.T) {
			base := t.TempDir()
			liveRoot := filepath.Join(base, "appdata")
			live := writeLive(t, liveRoot, "compose/core.yml", "NEW")
			added := writeLive(t, liveRoot, "compose/added.yml", "ADDED")
			backupPath := filepath.Join(base, "backup")
			writeInvalidBackupArchive(t, backupPath, live, tt.malicious)
			capture := installOrphanPassDockerShim(t, 0)

			d := &DeployOps{ProjectName: "bosun"}
			err := d.RollbackFromBackupSet(context.Background(), RollbackSet{
				Files:         []string{live, added},
				ComposeFiles:  []string{live},
				Root:          liveRoot,
				DeleteMissing: true,
			}, backupPath)

			require.ErrorIs(t, err, errRollbackNotAttempted)
			assert.Equal(t, "NEW", read(t, live), "valid early archive content must not reach live state")
			assert.Equal(t, "ADDED", read(t, added), "failed extraction must not delete a backup-absent live file")
			assert.NoFileExists(t, capture, "failed extraction must not invoke compose")
		})
	}
}

func TestRollbackFromBackupSet_OuterCancellationDoesNotSuppressRollback(t *testing.T) {
	base := t.TempDir()
	liveRoot := filepath.Join(base, "appdata")
	live := writeLive(t, liveRoot, "svc/config.yml", "OLD")
	backupPath := filepath.Join(base, "backup")
	writeTestBackupArchive(t, backupPath, live)
	require.NoError(t, os.WriteFile(live, []byte("NEW"), 0o644))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := (&DeployOps{}).RollbackFromBackupSet(ctx, RollbackSet{Files: []string{live}, Root: liveRoot}, backupPath)

	require.NoError(t, err)
	assert.Equal(t, "OLD", read(t, live))
}

func TestRollbackFromBackupSet_IndependentDeadlineCleansBeforeLiveUse(t *testing.T) {
	base := t.TempDir()
	liveRoot := filepath.Join(base, "appdata")
	live := writeLive(t, liveRoot, "svc/config.yml", "NEW")
	backupPath := filepath.Join(base, "backup")
	writeGzTarArchiveHeaders(t, filepath.Join(backupPath, "configs.tar.gz"), regHdr(archiveMemberName(live)))
	tmpRoot := filepath.Join(base, "extract-tmp")
	require.NoError(t, os.MkdirAll(tmpRoot, 0o755))
	t.Setenv("TMPDIR", tmpRoot)

	err := (&DeployOps{ComposeUpTimeout: time.Nanosecond}).RollbackFromBackupSet(
		context.Background(), RollbackSet{Files: []string{live}, Root: liveRoot}, backupPath)

	require.ErrorIs(t, err, errRollbackNotAttempted)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Equal(t, "NEW", read(t, live))
	entries, readErr := os.ReadDir(tmpRoot)
	require.NoError(t, readErr)
	assert.Empty(t, entries, "independent extraction deadline must clean its temporary tree")
}

func TestRollbackFromBackupSet_ExtractionErrorPreservesTypedCause(t *testing.T) {
	err := (&DeployOps{}).RollbackFromBackupSet(context.Background(), RollbackSet{}, t.TempDir())

	require.ErrorIs(t, err, errRollbackNotAttempted)
	var pathErr *os.PathError
	require.ErrorAs(t, err, &pathErr)
	assert.Equal(t, "open", pathErr.Op)
}

func TestComposeUpIsolated_InvalidArchiveHasNoBackupComposeOrOrphanUse(t *testing.T) {
	for _, tt := range invalidArchiveHeaders() {
		t.Run(tt.name, func(t *testing.T) {
			base := t.TempDir()
			composeFile := writeLive(t, base, "compose/core.yml", "NEW")
			backupPath := filepath.Join(base, "backup")
			writeInvalidBackupArchive(t, backupPath, composeFile, tt.malicious)
			capture := installOrphanPassDockerShim(t, 0)
			phaseErr := errors.New("phase compose failed")
			d := &DeployOps{
				ProjectName:   "bosun",
				RemoveOrphans: true,
				composeUpFn: func(context.Context, []string) error {
					return phaseErr
				},
			}

			summary, err := d.ComposeUpIsolated(context.Background(), []string{composeFile}, backupPath)

			require.ErrorIs(t, err, phaseErr)
			require.Len(t, summary.Results, 1)
			assert.ErrorIs(t, summary.Results[0].Err, phaseErr)
			assert.False(t, summary.Results[0].RolledBack)
			assert.NoFileExists(t, capture, "invalid archive must reach neither rollback compose nor orphan pass")
		})
	}
}

func TestComposeUpIsolated_OuterCancellationDoesNotSuppressExtraction(t *testing.T) {
	base := t.TempDir()
	composeFile := writeLive(t, base, "compose/core.yml", "services: {}\n")
	backupPath := filepath.Join(base, "backup")
	writeTestBackupArchive(t, backupPath, composeFile)
	setupDockerShim(t, 0)
	phaseErr := errors.New("phase compose failed")
	d := &DeployOps{composeUpFn: func(context.Context, []string) error { return phaseErr }}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	summary, err := d.ComposeUpIsolated(ctx, []string{composeFile}, backupPath)

	require.ErrorIs(t, err, phaseErr)
	require.Len(t, summary.Results, 1)
	assert.True(t, summary.Results[0].RolledBack)
}

func TestComposeUpIsolated_IndependentDeadlineCleansBeforeBackupUse(t *testing.T) {
	base := t.TempDir()
	composeFile := writeLive(t, base, "compose/core.yml", "services: {}\n")
	backupPath := filepath.Join(base, "backup")
	writeGzTarArchiveHeaders(t, filepath.Join(backupPath, "configs.tar.gz"), regHdr(archiveMemberName(composeFile)))
	tmpRoot := filepath.Join(base, "extract-tmp")
	require.NoError(t, os.MkdirAll(tmpRoot, 0o755))
	t.Setenv("TMPDIR", tmpRoot)
	capture := installOrphanPassDockerShim(t, 0)
	phaseErr := errors.New("phase compose failed")
	d := &DeployOps{
		ComposeUpTimeout: time.Nanosecond,
		RemoveOrphans:    true,
		composeUpFn: func(context.Context, []string) error {
			return phaseErr
		},
	}

	summary, err := d.ComposeUpIsolated(context.Background(), []string{composeFile}, backupPath)

	require.ErrorIs(t, err, phaseErr)
	require.Len(t, summary.Results, 1)
	assert.False(t, summary.Results[0].RolledBack)
	assert.NoFileExists(t, capture)
	entries, readErr := os.ReadDir(tmpRoot)
	require.NoError(t, readErr)
	assert.Empty(t, entries, "independent extraction deadline must clean its temporary tree")
}

func TestComposeUpIsolated_ExtractionFailureIsLoggedAndMemoized(t *testing.T) {
	phaseErrOne := errors.New("first phase failure")
	phaseErrTwo := errors.New("second phase failure")
	extractErr := errors.New("archive validation failed")
	var logs bytes.Buffer
	logger := zerolog.New(&logs).Level(zerolog.WarnLevel)
	ctx := bosunlog.WithContext(context.Background(), &logger)
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	extractCalls := 0
	d := &DeployOps{
		ComposeUpTimeout: time.Minute,
		composeUpFn: func(_ context.Context, files []string) error {
			if files[0] == "/compose/one.yml" {
				return phaseErrOne
			}
			return phaseErrTwo
		},
		extractBackupFn: func(extractCtx context.Context, tarFile string) (string, func(), error) {
			extractCalls++
			assert.Equal(t, "/backup/configs.tar.gz", tarFile)
			assert.NoError(t, extractCtx.Err(), "outer cancellation must not reach the extraction context")
			_, hasDeadline := extractCtx.Deadline()
			assert.True(t, hasDeadline, "extraction context must retain its independent bound")
			return "", func() {}, extractErr
		},
	}

	summary, err := d.ComposeUpIsolated(ctx, []string{"/compose/one.yml", "/compose/two.yml"}, "/backup")

	require.Error(t, err)
	assert.ErrorIs(t, err, phaseErrOne)
	assert.ErrorIs(t, err, phaseErrTwo)
	assert.Equal(t, 1, extractCalls, "multiple failures must share one lazy extraction attempt")
	require.Len(t, summary.Results, 2)
	assert.ErrorIs(t, summary.Results[0].Err, phaseErrOne)
	assert.ErrorIs(t, summary.Results[1].Err, phaseErrTwo)
	assert.False(t, summary.Results[0].RolledBack)
	assert.False(t, summary.Results[1].RolledBack)
	assert.Contains(t, logs.String(), extractErr.Error(), "the extraction cause must remain operator-visible")
}
