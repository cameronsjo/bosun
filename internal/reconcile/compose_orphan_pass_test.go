package reconcile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComposeUpIsolated_OrphanPassSkipsFailedWithoutRollback(t *testing.T) {
	capture := installOrphanPassDockerShim(t, 0)
	phaseErr := errors.New("unreachable image")
	var phaseCalls []string
	d := &DeployOps{
		RemoveOrphans: true,
		ProjectName:   "bosun",
		composeUpFn: func(_ context.Context, files []string) error {
			require.Len(t, files, 1)
			phaseCalls = append(phaseCalls, files[0])
			if strings.Contains(files[0], "failed") {
				return phaseErr
			}
			return nil
		},
	}
	files := []string{
		"/compose/first.yml",
		"/compose/failed.yml",
		"/compose/last.yml",
	}

	summary, err := d.ComposeUpIsolated(context.Background(), files, "")

	require.NoError(t, err)
	assert.Equal(t, files, phaseCalls, "phase one must attempt each file exactly once in input order")
	require.Len(t, summary.Results, 3)
	assert.Equal(t, 2, summary.Succeeded)
	assert.Equal(t, 1, summary.Failed)
	assert.ErrorIs(t, summary.Results[1].Err, phaseErr)

	orphanArgs, err := os.ReadFile(capture)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"compose", "-p", "bosun",
		"-f", "/compose/first.yml",
		"-f", "/compose/last.yml",
		"up", "-d", "--remove-orphans",
	}, strings.Fields(string(orphanArgs)),
		"the orphan pass must contain only eligible files in phase-one order")
}

func TestComposeUpIsolated_OrphanPassIncludesVerifiedRollback(t *testing.T) {
	tmp := evalSymlinks(t, t.TempDir())
	composeFile := filepath.Join(tmp, "compose", "failed.yml")
	require.NoError(t, os.MkdirAll(filepath.Dir(composeFile), 0o755))
	require.NoError(t, os.WriteFile(composeFile, []byte("services: {}\n"), 0o644))

	backupDir := filepath.Join(tmp, "backups")
	backupName, err := NewDeployOps(false, "").Backup(context.Background(), backupDir, []string{composeFile})
	require.NoError(t, err)
	backupPath := filepath.Join(backupDir, backupName)

	capture := installOrphanPassDockerShim(t, 0)
	phaseErr := errors.New("invalid compose file")
	phaseAttempts := 0
	d := &DeployOps{
		RemoveOrphans: true,
		ProjectName:   "bosun",
		composeUpFn: func(_ context.Context, files []string) error {
			phaseAttempts++
			require.Equal(t, []string{composeFile}, files)
			return phaseErr
		},
	}

	summary, err := d.ComposeUpIsolated(context.Background(), []string{composeFile}, backupPath)

	require.Error(t, err, "an all-failed phase one remains fatal even after verified rollback")
	assert.Equal(t, 1, phaseAttempts, "phase one must not retry the failed input")
	require.Len(t, summary.Results, 1)
	assert.Equal(t, 0, summary.Succeeded)
	assert.Equal(t, 1, summary.Failed)
	assert.Equal(t, 1, summary.RolledBack)
	assert.ErrorIs(t, summary.Results[0].Err, phaseErr)

	captured, readErr := os.ReadFile(capture)
	require.NoError(t, readErr)
	lines := strings.Split(strings.TrimSpace(string(captured)), "\n")
	require.Len(t, lines, 2, "verified rollback must be followed by an orphan pass")
	rollbackArgs := strings.Fields(lines[0])
	orphanArgs := strings.Fields(lines[1])
	require.Len(t, rollbackArgs, 7)
	assert.Equal(t, []string{"compose", "-p", "bosun", "-f"}, rollbackArgs[:4])
	assert.NotEqual(t, composeFile, rollbackArgs[4], "rollback must use the extracted backup copy")
	assert.Equal(t, []string{"up", "-d"}, rollbackArgs[5:])
	assert.Equal(t, append(append([]string(nil), rollbackArgs...), "--remove-orphans"), orphanArgs,
		"orphan pass must use the same verified backup path as rollback")
}

func TestComposeUpIsolated_OrphanPassErrorPreservesPhaseOneResults(t *testing.T) {
	capture := installOrphanPassDockerShim(t, 1)
	phaseErr := errors.New("invalid compose file")
	attempts := make(map[string]int)
	d := &DeployOps{
		RemoveOrphans: true,
		composeUpFn: func(_ context.Context, files []string) error {
			require.Len(t, files, 1)
			attempts[files[0]]++
			if strings.Contains(files[0], "failed") {
				return phaseErr
			}
			return nil
		},
	}

	summary, err := d.ComposeUpIsolated(context.Background(), []string{
		"/compose/good.yml",
		"/compose/failed.yml",
	}, "")

	require.NoError(t, err, "the existing orphan-pass error contract remains non-fatal")
	assert.Equal(t, map[string]int{
		"/compose/good.yml":   1,
		"/compose/failed.yml": 1,
	}, attempts)
	require.Len(t, summary.Results, 2)
	assert.Equal(t, 1, summary.Succeeded)
	assert.Equal(t, 1, summary.Failed)
	assert.ErrorIs(t, summary.Results[1].Err, phaseErr,
		"the non-fatal orphan failure must not replace the original phase-one error")

	orphanArgs, readErr := os.ReadFile(capture)
	require.NoError(t, readErr)
	assert.Contains(t, string(orphanArgs), "/compose/good.yml")
	assert.NotContains(t, string(orphanArgs), "/compose/failed.yml")
}

func installOrphanPassDockerShim(t *testing.T, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	capture := filepath.Join(dir, "docker-args.log")
	shim := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> \"$BOSUN_TEST_DOCKER_CAPTURE\"\n" +
		"exit " + strconv.Itoa(exitCode) + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "docker"), []byte(shim), 0o755))
	t.Setenv("BOSUN_TEST_DOCKER_CAPTURE", capture)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return capture
}
