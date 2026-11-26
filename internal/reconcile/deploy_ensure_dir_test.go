package reconcile

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// installCommandLogSSHShim puts an `ssh` first on PATH that appends each remote
// command line to a log and exits 0 without running it. It returns the log path.
func installCommandLogSSHShim(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	commandLog := filepath.Join(dir, "ssh-commands")
	t.Setenv("SSH_COMMAND_LOG", commandLog)
	shim := "#!/bin/sh\n" +
		"while [ $# -gt 0 ]; do\n" +
		"  case \"$1\" in\n" +
		"    -o) shift 2 ;;\n" +
		"    -*) shift ;;\n" +
		"    *) break ;;\n" +
		"  esac\n" +
		"done\n" +
		"shift\n" +
		"printf '%s\\n' \"$*\" >> \"$SSH_COMMAND_LOG\"\n" +
		"exit 0\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ssh"), []byte(shim), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return commandLog
}

// TestDeployRemote_SingleFileEnsuresParentOnce pins that a single-file target
// costs one `mkdir -p` of its parent directory, not two: DeployRemoteFile
// already ensures it, so the caller must not ensure it first.
func TestDeployRemote_SingleFileEnsuresParentOnce(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the ssh shim is a POSIX shell script")
	}
	base := t.TempDir()
	stagingDir := filepath.Join(base, "staging")
	stagingAppdata := filepath.Join(stagingDir, "unraid", "appdata")
	require.NoError(t, os.MkdirAll(stagingAppdata, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(stagingAppdata, "service.yml"), []byte("enabled: true"), 0o644))

	remoteAppdata := filepath.Join(base, "remote-appdata")
	commandLog := installCommandLogSSHShim(t)
	r := NewReconciler(&Config{
		TargetHost:        "user@testhost",
		DryRun:            false,
		StagingDir:        stagingDir,
		InfraSubDir:       "unraid",
		RemoteAppdataPath: remoteAppdata,
	}, WithDeployOps(&DeployOps{}))

	result, err := r.deployRemote(context.Background(), nil)

	require.NoError(t, err)
	require.NotNil(t, result)
	data, err := os.ReadFile(commandLog)
	require.NoError(t, err)
	ensureParent := 0
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "mkdir -p "+remoteAppdata {
			ensureParent++
		}
	}
	assert.Equal(t, 1, ensureParent, "the parent directory must be ensured exactly once; commands:\n%s", data)
}
