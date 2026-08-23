package reconcile

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupSelectiveMkdirFailureSSHShim(t *testing.T, failPath string) string {
	t.Helper()
	dir := t.TempDir()
	failureLog := filepath.Join(dir, "mkdir-failures.log")
	t.Setenv("SSH_FAIL_MKDIR_PATH", failPath)
	t.Setenv("SSH_MKDIR_FAILURE_LOG", failureLog)
	shim := "#!/bin/sh\n" +
		"while [ $# -gt 0 ]; do\n" +
		"  case \"$1\" in\n" +
		"    -o) shift 2 ;;\n" +
		"    -*) shift ;;\n" +
		"    *) break ;;\n" +
		"  esac\n" +
		"done\n" +
		"shift\n" +
		"if [ \"$*\" = \"mkdir -p $SSH_FAIL_MKDIR_PATH\" ]; then\n" +
		"  printf '%s\\n' \"$*\" >> \"$SSH_MKDIR_FAILURE_LOG\"\n" +
		"  echo 'permission denied' >&2\n" +
		"  exit 1\n" +
		"fi\n" +
		"case \"$*\" in\n" +
		"  */boot/*) exit 0 ;;\n" +
		"esac\n" +
		"exec /bin/sh -c \"$*\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ssh"), []byte(shim), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return failureLog
}

func mkdirFailureCount(t *testing.T, failureLog string) int {
	t.Helper()
	data, err := os.ReadFile(failureLog)
	require.NoError(t, err)
	return len(strings.Split(strings.TrimSpace(string(data)), "\n"))
}

func TestEnsureRemoteDir_DryRunSkipsSSH(t *testing.T) {
	failureLog := setupSelectiveMkdirFailureSSHShim(t, "/remote/appdata")

	err := (&DeployOps{DryRun: true}).EnsureRemoteDir(context.Background(), "user@testhost", "/remote/appdata")
	require.NoError(t, err)
	_, statErr := os.Stat(failureLog)
	assert.ErrorIs(t, statErr, os.ErrNotExist, "dry-run directory preparation must not execute ssh")
}

func TestDeployLocal_PropagatesSingleFileParentMkdirError(t *testing.T) {
	base := t.TempDir()
	stagingDir := filepath.Join(base, "staging")
	stagingAppdata := filepath.Join(stagingDir, "unraid", "appdata")
	require.NoError(t, os.MkdirAll(stagingAppdata, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(stagingAppdata, "service.yml"), []byte("enabled: true"), 0o644))

	blocker := filepath.Join(base, "not-a-directory")
	require.NoError(t, os.WriteFile(blocker, []byte("block"), 0o644))
	appdataDir := filepath.Join(blocker, "appdata")
	r := NewReconciler(&Config{
		DryRun:           false,
		StagingDir:       stagingDir,
		InfraSubDir:      "unraid",
		LocalAppdataPath: appdataDir,
	}, WithDeployOps(&DeployOps{}))

	result, err := r.deployLocal(context.Background(), nil)
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "create local deploy directory \""+appdataDir+"\"")
}

func TestDeployLocal_PropagatesComposeMkdirError(t *testing.T) {
	base := t.TempDir()
	stagingDir := filepath.Join(base, "staging")
	composeStaging := filepath.Join(stagingDir, "unraid", "compose")
	require.NoError(t, os.MkdirAll(composeStaging, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(composeStaging, "core.yml"), []byte("services: {}"), 0o644))

	appdataDir := filepath.Join(base, "appdata")
	require.NoError(t, os.MkdirAll(appdataDir, 0o755))
	composeTarget := filepath.Join(appdataDir, "compose")
	require.NoError(t, os.WriteFile(composeTarget, []byte("block"), 0o644))
	r := NewReconciler(&Config{
		DryRun:           false,
		StagingDir:       stagingDir,
		InfraSubDir:      "unraid",
		LocalAppdataPath: appdataDir,
	}, WithDeployOps(&DeployOps{}))

	result, err := r.deployLocal(context.Background(), nil)
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "create local compose directory \""+composeTarget+"\"")
}

func TestDeployLocal_DryRunDoesNotCreateTargetDirectories(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, stagingRoot string)
	}{
		{
			name: "single file target",
			setup: func(t *testing.T, stagingRoot string) {
				appdata := filepath.Join(stagingRoot, "appdata")
				require.NoError(t, os.MkdirAll(appdata, 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(appdata, "service.yml"), []byte("enabled: true"), 0o644))
			},
		},
		{
			name: "compose directory target",
			setup: func(t *testing.T, stagingRoot string) {
				compose := filepath.Join(stagingRoot, "compose")
				require.NoError(t, os.MkdirAll(compose, 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(compose, "core.yml"), []byte("services: {}"), 0o644))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := t.TempDir()
			stagingDir := filepath.Join(base, "staging")
			tt.setup(t, filepath.Join(stagingDir, "unraid"))
			appdataDir := filepath.Join(base, "appdata")
			r := NewReconciler(&Config{
				DryRun:           true,
				StagingDir:       stagingDir,
				InfraSubDir:      "unraid",
				LocalAppdataPath: appdataDir,
			})

			result, err := r.deployLocal(context.Background(), nil)
			require.NoError(t, err)
			require.NotNil(t, result)
			_, statErr := os.Stat(appdataDir)
			assert.ErrorIs(t, statErr, os.ErrNotExist, "dry run must leave destination directories absent")
		})
	}
}

func TestDeployRemote_PropagatesSingleFileParentMkdirError(t *testing.T) {
	base := t.TempDir()
	stagingDir := filepath.Join(base, "staging")
	stagingAppdata := filepath.Join(stagingDir, "unraid", "appdata")
	require.NoError(t, os.MkdirAll(stagingAppdata, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(stagingAppdata, "service.yml"), []byte("enabled: true"), 0o644))

	remoteAppdata := filepath.Join(base, "remote-appdata")
	failureLog := setupSelectiveMkdirFailureSSHShim(t, remoteAppdata)
	r := NewReconciler(&Config{
		TargetHost:        "user@testhost",
		DryRun:            false,
		StagingDir:        stagingDir,
		InfraSubDir:       "unraid",
		RemoteAppdataPath: remoteAppdata,
	}, WithDeployOps(&DeployOps{}))

	result, err := r.deployRemote(context.Background(), nil)
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "ensure remote deploy directory \""+remoteAppdata+"\"")
	assert.Contains(t, err.Error(), "permission denied")
	assert.Equal(t, 1, mkdirFailureCount(t, failureLog), "mandatory mkdir failure must abort before file deployment retries it")
}

func TestDeployRemote_PropagatesComposeMkdirError(t *testing.T) {
	base := t.TempDir()
	stagingDir := filepath.Join(base, "staging")
	composeStaging := filepath.Join(stagingDir, "unraid", "compose")
	require.NoError(t, os.MkdirAll(composeStaging, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(composeStaging, "core.yml"), []byte("services: {}"), 0o644))

	remoteAppdata := filepath.Join(base, "remote-appdata")
	composeTarget := filepath.Join(remoteAppdata, "compose")
	setupGuardedSCPShim(t)
	setupSelectiveMkdirFailureSSHShim(t, composeTarget)
	r := NewReconciler(&Config{
		TargetHost:        "user@testhost",
		DryRun:            false,
		StagingDir:        stagingDir,
		InfraSubDir:       "unraid",
		RemoteAppdataPath: remoteAppdata,
	}, WithDeployOps(&DeployOps{}))

	result, err := r.deployRemote(context.Background(), nil)
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "ensure remote compose directory \""+composeTarget+"\"")
	assert.Contains(t, err.Error(), "permission denied")
}

func TestDeployRemote_ComposeManagerMkdirErrorWarnsAndSkipsOptionalSync(t *testing.T) {
	base := t.TempDir()
	stagingDir := filepath.Join(base, "staging")
	composeStaging := filepath.Join(stagingDir, "unraid", "compose")
	require.NoError(t, os.MkdirAll(composeStaging, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(composeStaging, "core.yml"), []byte("services: {}"), 0o644))

	const composeManagerDir = "/boot/config/plugins/compose.manager/projects/core"
	remoteAppdata := filepath.Join(base, "remote-appdata")
	setupGuardedSCPShim(t)
	failureLog := setupSelectiveMkdirFailureSSHShim(t, composeManagerDir)
	r := NewReconciler(&Config{
		TargetHost:        "user@testhost",
		DryRun:            true,
		StagingDir:        stagingDir,
		InfraSubDir:       "unraid",
		RemoteAppdataPath: remoteAppdata,
	}, WithDeployOps(&DeployOps{}))

	result, err := r.deployRemote(context.Background(), nil)
	require.NoError(t, err, "the optional Compose Manager mirror must not fail the primary deployment")
	require.NotNil(t, result)
	assert.Equal(t, "services: {}", readMarker(t, filepath.Join(remoteAppdata, "compose"), "core.yml"))
	assert.Equal(t, 1, mkdirFailureCount(t, failureLog), "optional mkdir failure must skip DeployRemoteFile instead of retrying the same mkdir")
}
