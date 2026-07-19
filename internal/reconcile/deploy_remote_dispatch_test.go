package reconcile

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupGuardedSSHShim installs an `ssh` shim like setupSSHShim but no-ops any
// remote command touching /boot (the hardcoded Compose Manager path), so a
// non-dry-run remote deploy in tests never mutates outside its temp tree —
// notably when CI runs as root and `mkdir -p /boot/...` would otherwise succeed.
func setupGuardedSSHShim(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	shim := "#!/bin/sh\n" +
		"while [ $# -gt 0 ]; do\n" +
		"  case \"$1\" in\n" +
		"    -o) shift 2 ;;\n" +
		"    -*) shift ;;\n" +
		"    *) break ;;\n" +
		"  esac\n" +
		"done\n" +
		"shift\n" +
		"case \"$*\" in\n" +
		"  */boot/*) exit 0 ;;\n" +
		"esac\n" +
		"exec /bin/sh -c \"$*\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ssh"), []byte(shim), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// setupGuardedSCPShim installs an `scp` shim that no-ops when the destination is
// under /boot (Compose Manager), else performs a local copy.
func setupGuardedSCPShim(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	shim := "#!/bin/sh\n" +
		"while [ $# -gt 0 ]; do\n" +
		"  case \"$1\" in\n" +
		"    -o) shift 2 ;;\n" +
		"    -*) shift ;;\n" +
		"    *) break ;;\n" +
		"  esac\n" +
		"done\n" +
		"dst=\"${2#*:}\"\n" +
		"case \"$dst\" in\n" +
		"  /boot/*) exit 0 ;;\n" +
		"esac\n" +
		"exec cp \"$1\" \"$dst\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "scp"), []byte(shim), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// setupComposePSDockerShim installs a `docker` shim whose `compose up` fails but
// `compose ps` reports a running-but-unhealthy container (exit 0). ComposeUpRemote
// then classifies the failure as unhealthy-only and returns ErrComposeUnhealthy.
func setupComposePSDockerShim(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	shim := "#!/bin/sh\n" +
		"for a in \"$@\"; do\n" +
		"  if [ \"$a\" = \"ps\" ]; then\n" +
		"    echo '{\"Name\":\"web\",\"State\":\"running\",\"Health\":\"unhealthy\"}'\n" +
		"    exit 0\n" +
		"  fi\n" +
		"done\n" +
		"exit 1\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "docker"), []byte(shim), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// buildRemoteStaging creates a staging tree and returns (stagingDir, infraSubDir,
// remoteAppdata). It mirrors the fixed appdata/compose layout the reconciler syncs.
func buildRemoteStaging(t *testing.T) (string, string, string) {
	t.Helper()
	base := t.TempDir()
	stagingUnraid := filepath.Join(base, "staging", "unraid")
	for _, subdir := range []string{"appdata/traefik", "compose"} {
		require.NoError(t, os.MkdirAll(filepath.Join(stagingUnraid, subdir), 0o755))
	}
	writeMarker(t, filepath.Join(stagingUnraid, "appdata", "traefik"), "traefik.yml", "traefik-config")
	writeMarker(t, filepath.Join(stagingUnraid, "compose"), "core.yml", "compose-new")
	return filepath.Join(base, "staging"), "unraid", filepath.Join(base, "appdata")
}

func TestDeployRemoteDispatch_UnhealthyIsNonFatal(t *testing.T) {
	setupGuardedSSHShim(t)
	setupGuardedSCPShim(t)
	setupSha256sumShim(t)
	setupComposePSDockerShim(t) // compose up fails, ps reports unhealthy-only

	stagingDir, infra, remoteAppdata := buildRemoteStaging(t)
	cfg := &Config{
		TargetHost:        "user@testhost",
		StagingDir:        stagingDir,
		InfraSubDir:       infra,
		RemoteAppdataPath: remoteAppdata,
		DryRun:            false,
	}
	r := NewReconciler(cfg, WithDeployOps(&DeployOps{DryRun: false}))

	result, err := r.deployRemote(context.Background(), nil)
	require.NoError(t, err, "unhealthy-only remote compose up must be a warning, not a deploy failure")
	require.NotNil(t, result)
	assert.NotEmpty(t, result.ManagedFiles, "remote deploy seeds the managed-file manifest from the local staging walk")
	assert.Equal(t, "compose-new", readMarker(t, filepath.Join(remoteAppdata, "compose"), "core.yml"))
}

func TestDeployRemoteDispatch_RealFailureTriggersRollback(t *testing.T) {
	setupGuardedSSHShim(t)
	setupGuardedSCPShim(t)
	setupSha256sumShim(t)
	setupDockerShim(t, 1) // every docker invocation fails: up, classify ps, and the rollback's up

	stagingDir, infra, remoteAppdata := buildRemoteStaging(t)

	// Seed a verified backup anchor containing the CURRENT (good) compose dir,
	// then let the deploy overwrite it with staging content.
	composeDir := filepath.Join(remoteAppdata, "compose")
	writeMarker(t, composeDir, "core.yml", "compose-good")
	backupPath := backupComposeDir(t, &DeployOps{}, filepath.Join(remoteAppdata, "..", "backups"), composeDir)

	cfg := &Config{
		TargetHost:        "user@testhost",
		StagingDir:        stagingDir,
		InfraSubDir:       infra,
		RemoteAppdataPath: remoteAppdata,
		DryRun:            false,
	}
	r := NewReconciler(cfg, WithDeployOps(&DeployOps{DryRun: false}))
	r.lastBackupPath = backupPath

	result, err := r.deployRemote(context.Background(), nil)
	require.Error(t, err, "a real remote compose failure must surface as a deploy error")
	assert.Nil(t, result)
	// docker fails at every step, so the rollback's own compose up also fails.
	require.ErrorIs(t, err, ErrRollbackFailed)
}
