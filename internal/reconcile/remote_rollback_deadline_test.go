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

// installRollbackHangingComposeSSHShim runs every remote command locally, like
// setupSSHShim, except `docker compose ... up`, which it replaces with a sleep.
// The shim `exec`s the sleep so killing the ssh process ends the hang.
func installRollbackHangingComposeSSHShim(t *testing.T, sleepSeconds int) {
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
		"  *'docker compose'*' up '*) exec sleep " + itoa(sleepSeconds) + " ;;\n" +
		"esac\n" +
		"exec /bin/sh -c \"$*\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ssh"), []byte(shim), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestRollbackRemoteCompose_Deadlines(t *testing.T) {
	deployErr := errors.New("remote compose exited 1")

	// The caller's ctx is often the reason compose up failed (cycle timeout or
	// daemon shutdown). Rollback must still restore the anchor, as the local
	// RollbackFromBackupSet does through deadline.CleanupContext.
	t.Run("canceled caller ctx still restores the backup", func(t *testing.T) {
		setupSSHShim(t)
		setupSha256sumShim(t)
		setupDockerShim(t, 0)

		base := t.TempDir()
		composeDir := filepath.Join(base, "appdata", "compose")
		writeMarker(t, composeDir, "core.yml", "good-v1")
		backupPath := backupComposeDir(t, &DeployOps{}, filepath.Join(base, "backups"), composeDir)
		writeMarker(t, composeDir, "core.yml", "bad-v2")

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		d := &DeployOps{}
		err := d.RollbackRemoteCompose(ctx, "user@testhost", composeDir, backupPath, deployErr)
		require.ErrorIs(t, err, ErrRollbackSucceeded, "a canceled caller ctx must not abort the restore: %v", err)
		assert.Equal(t, "good-v1", readMarker(t, composeDir, "core.yml"))
	})

	// Detached from cancellation does not mean unbounded: a hung compose up
	// during rollback ends at ComposeUpTimeout.
	t.Run("rollback compose up is bounded by ComposeUpTimeout", func(t *testing.T) {
		installRollbackHangingComposeSSHShim(t, 15)
		setupSha256sumShim(t)
		setupDockerShim(t, 0)

		base := t.TempDir()
		composeDir := filepath.Join(base, "appdata", "compose")
		writeMarker(t, composeDir, "core.yml", "good-v1")
		backupPath := backupComposeDir(t, &DeployOps{}, filepath.Join(base, "backups"), composeDir)
		writeMarker(t, composeDir, "core.yml", "bad-v2")

		d := &DeployOps{ComposeUpTimeout: 3 * time.Second}
		start := time.Now()
		err := d.RollbackRemoteCompose(context.Background(), "user@testhost", composeDir, backupPath, deployErr)
		elapsed := time.Since(start)

		require.ErrorIs(t, err, ErrRollbackFailed, "a rollback compose up that outlives ComposeUpTimeout must fail: %v", err)
		assert.Less(t, elapsed, 10*time.Second, "rollback must be cut off at ComposeUpTimeout")
	})
}
