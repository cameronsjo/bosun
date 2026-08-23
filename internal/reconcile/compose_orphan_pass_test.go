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
	args := string(orphanArgs)
	assert.Contains(t, args, "/compose/first.yml")
	assert.NotContains(t, args, "/compose/failed.yml",
		"a phase-one failure without rollback must not be attempted by the orphan pass")
	assert.Contains(t, args, "/compose/last.yml")
	assert.Less(t, strings.Index(args, "/compose/first.yml"), strings.Index(args, "/compose/last.yml"),
		"successful files must retain input order")
	assert.Contains(t, args, "--remove-orphans")
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
