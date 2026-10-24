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

func TestComposeUpIsolated_OrphanPassSkippedWhenFileFailedWithoutRollback(t *testing.T) {
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

	// #729: a pass over first.yml and last.yml would remove failed.yml's
	// still-running containers as orphans, so no pass may run at all.
	_, statErr := os.Stat(capture)
	assert.True(t, os.IsNotExist(statErr), "no docker command may run: the orphan pass must be skipped")
	assert.Contains(t, summary.OrphanPassSkipped, "failed.yml")
	assert.Contains(t, partialDeployError(summary, len(files)).Error(), "orphan pass skipped",
		"the partial-deploy error feeds the failure alert and must name the skip")
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
	tmp := evalSymlinks(t, t.TempDir())
	good := filepath.Join(tmp, "compose", "good.yml")
	failed := filepath.Join(tmp, "compose", "failed.yml")
	require.NoError(t, os.MkdirAll(filepath.Dir(failed), 0o755))
	require.NoError(t, os.WriteFile(good, []byte("services: {}\n"), 0o644))
	require.NoError(t, os.WriteFile(failed, []byte("services: {}\n"), 0o644))
	backupDir := filepath.Join(tmp, "backups")
	backupName, err := NewDeployOps(false, "").Backup(context.Background(), backupDir, []string{failed})
	require.NoError(t, err)

	// Rollback succeeds; only the orphan pass fails.
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

	summary, err := d.ComposeUpIsolated(context.Background(), []string{good, failed}, filepath.Join(backupDir, backupName))

	require.NoError(t, err, "the existing orphan-pass error contract remains non-fatal")
	assert.Equal(t, map[string]int{good: 1, failed: 1}, attempts)
	require.Len(t, summary.Results, 2)
	assert.Equal(t, 1, summary.Succeeded)
	assert.Equal(t, 1, summary.Failed)
	assert.Equal(t, 1, summary.RolledBack)
	assert.Empty(t, summary.OrphanPassSkipped, "a verified rollback keeps the orphan pass")
	assert.ErrorIs(t, summary.Results[1].Err, phaseErr,
		"the non-fatal orphan failure must not replace the original phase-one error")

	captured, readErr := os.ReadFile(capture)
	require.NoError(t, readErr)
	lines := strings.Split(strings.TrimSpace(string(captured)), "\n")
	require.Len(t, lines, 2, "rollback then orphan pass")
	assert.Contains(t, lines[1], "--remove-orphans")
	assert.Contains(t, lines[1], "-f "+good)
	assert.NotContains(t, lines[1], "-f "+failed, "the failed new file must not enter the pass; its backup copy does")
}

// installOrphanPassDockerShim logs every docker call; the orphan pass (the
// call carrying --remove-orphans) exits exitCode and every other call exits 0.
func installOrphanPassDockerShim(t *testing.T, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	capture := filepath.Join(dir, "docker-args.log")
	shim := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> \"$BOSUN_TEST_DOCKER_CAPTURE\"\n" +
		"case \"$*\" in *--remove-orphans*) exit " + strconv.Itoa(exitCode) + " ;; esac\n" +
		"exit 0\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "docker"), []byte(shim), 0o755))
	t.Setenv("BOSUN_TEST_DOCKER_CAPTURE", capture)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return capture
}

func TestOrphanPassSkipReason(t *testing.T) {
	tests := []struct {
		name       string
		results    []ComposeFileResult
		backupRoot string
		wantSkip   string // substring; "" means the pass may run
	}{
		{name: "all succeeded", results: []ComposeFileResult{{File: "/c/a.yml", Success: true}}},
		{name: "unhealthy-only counts as success", results: []ComposeFileResult{{File: "/c/a.yml", Success: true, Err: ErrComposeUnhealthy}}},
		{name: "failure with verified rollback", results: []ComposeFileResult{{File: "/c/a.yml", Success: true}, {File: "/c/b.yml", RolledBack: true}}, backupRoot: "/tmp/x"},
		{name: "failure without rollback", results: []ComposeFileResult{{File: "/c/a.yml", Success: true}, {File: "/c/b.yml"}}, wantSkip: "b.yml"},
		{name: "rolled back but no backup tree is uncertain", results: []ComposeFileResult{{File: "/c/b.yml", RolledBack: true}}, wantSkip: "b.yml"},
		{name: "names every unrecovered file", results: []ComposeFileResult{{File: "/c/a.yml"}, {File: "/c/b.yml", RolledBack: true}, {File: "/c/c.yml"}}, backupRoot: "/tmp/x", wantSkip: "a.yml, c.yml"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := orphanPassSkipReason(tc.results, tc.backupRoot)
			if tc.wantSkip == "" {
				assert.Empty(t, got)
				return
			}
			assert.Contains(t, got, tc.wantSkip)
		})
	}
}

// installRunDockerShim puts a docker shim ahead of the harness one that logs
// to dockerLog. Rollback calls (`up` without --remove-orphans) exit
// rollbackExit; every other call exits 0.
func installRunDockerShim(t *testing.T, dockerLog string, rollbackExit int) {
	t.Helper()
	dir := t.TempDir()
	shim := "#!/bin/sh\necho \"$@\" >> '" + dockerLog + "'\n" +
		"case \"$*\" in *--remove-orphans*) exit 0 ;; *\" up \"*) exit " + strconv.Itoa(rollbackExit) + " ;; esac\nexit 0\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "docker"), []byte(shim), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// TestRun_OrphanPassSkippedAfterUnrecoveredFailure drives a full run (the
// previous deploy left NeedsRedeploy set) in which b.yml fails compose up. When
// its rollback also fails, b.yml's containers are still running and absent
// from every pass file, so `--remove-orphans` would remove them (#729): the
// pass must not run, and the run's error must say why. When the rollback
// succeeds, the pass runs as before (positive control).
func TestRun_OrphanPassSkippedAfterUnrecoveredFailure(t *testing.T) {
	tests := []struct {
		name         string
		rollbackExit int
		wantPass     bool
	}{
		{name: "rollback failed", rollbackExit: 1, wantPass: false},
		{name: "rollback succeeded", rollbackExit: 0, wantPass: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			prior := steadyPrior()
			prior.NeedsRedeploy = true
			r, calls, dockerLog, _ := changedOnlyHarness(t, "services:\n  svc-b:\n    image: alpine:OLD\n", prior, []string{"svc-a", "svc-b"})
			installRunDockerShim(t, dockerLog, tc.rollbackExit)
			r.deploy.composeUpFn = func(_ context.Context, files []string) error {
				*calls = append(*calls, files)
				if filepath.Base(files[0]) == "b.yml" {
					return errors.New("image pull failed")
				}
				return nil
			}

			err := r.Run(context.Background())

			require.Error(t, err, "a partial deploy is a failed deploy")
			assert.Equal(t, []string{"a.yml", "b.yml"}, upFileBases(*calls), "precondition: a full run")
			log := readDockerLog(t, dockerLog)
			assert.Contains(t, log, " up ", "precondition: the per-file rollback was attempted")
			if tc.wantPass {
				assert.Contains(t, log, "--remove-orphans", "a verified rollback keeps the orphan pass")
				assert.NotContains(t, err.Error(), "orphan pass skipped")
				return
			}
			assert.NotContains(t, log, "--remove-orphans",
				"the orphan pass must not run while b.yml failed and was not rolled back")
			assert.Contains(t, err.Error(), "orphan pass skipped", "the failure alert must name the skip")
			assert.Contains(t, err.Error(), "b.yml")
		})
	}
}
