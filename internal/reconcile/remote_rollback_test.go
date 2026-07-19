package reconcile

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupDockerShim installs a fake `docker` ahead of PATH with a fixed exit code,
// so the SSH shim's local execution of remote `docker compose ...` resolves.
func setupDockerShim(t *testing.T, exitCode int) {
	t.Helper()
	dir := t.TempDir()
	shim := "#!/bin/sh\nexit " + itoa(exitCode) + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "docker"), []byte(shim), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

func TestArchiveNameWithinRoot(t *testing.T) {
	safe := []string{
		"appdata/compose/core.yml",
		"/mnt/user/appdata/x",
		"a/b/c",
		"file.yml",
		"./a",
		"a/../b", // resolves to b — still within root
		"",       // empty name (a bare directory entry)
		"/",      // strips to empty — root itself
	}
	for _, n := range safe {
		assert.True(t, archiveNameWithinRoot(n), "expected %q to be within-root", n)
	}
	unsafe := []string{
		"../etc/passwd",
		"../../x",
		"a/../../b",
		"/../escape",
	}
	for _, n := range unsafe {
		assert.False(t, archiveNameWithinRoot(n), "expected %q to be rejected", n)
	}
}

// writeGzTarArchive writes a gzip-compressed tar with the given member names
// (each a small regular file), used to exercise the path-traversal guard.
func writeGzTarArchive(t *testing.T, path string, names ...string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	f, err := os.Create(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, name := range names {
		body := []byte("data")
		require.NoError(t, tw.WriteHeader(&tar.Header{
			Name: name,
			Mode: 0o644,
			Size: int64(len(body)),
		}))
		_, werr := tw.Write(body)
		require.NoError(t, werr)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
}

func TestAssertArchiveEntriesWithinRoot(t *testing.T) {
	t.Run("accepts a benign archive", func(t *testing.T) {
		tarFile := filepath.Join(t.TempDir(), "ok.tar.gz")
		writeGzTarArchive(t, tarFile, "mnt/user/appdata/compose/core.yml", "mnt/user/appdata/x")
		require.NoError(t, assertArchiveEntriesWithinRoot(context.Background(), tarFile))
	})

	t.Run("rejects an archive with a traversal entry", func(t *testing.T) {
		tarFile := filepath.Join(t.TempDir(), "evil.tar.gz")
		writeGzTarArchive(t, tarFile, "good/file.yml", "../../etc/passwd")
		err := assertArchiveEntriesWithinRoot(context.Background(), tarFile)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "escapes extraction root")
	})
}

// backupComposeDir builds a real, verified backup of composeDir and returns the
// backup path (the directory holding configs.tar.gz).
func backupComposeDir(t *testing.T, d *DeployOps, backupDir, composeDir string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(backupDir, 0o755))
	name, err := d.Backup(context.Background(), backupDir, []string{composeDir})
	require.NoError(t, err)
	return filepath.Join(backupDir, name)
}

func TestRollbackRemoteCompose(t *testing.T) {
	deployErr := errors.New("remote compose exited 1")

	t.Run("restores the backup and re-applies it, returning ErrRollbackSucceeded", func(t *testing.T) {
		setupSSHShim(t)
		setupSha256sumShim(t)
		setupDockerShim(t, 0) // compose up after restore succeeds

		base := t.TempDir()
		composeDir := filepath.Join(base, "appdata", "compose")
		writeMarker(t, composeDir, "core.yml", "good-v1")
		backupPath := backupComposeDir(t, &DeployOps{}, filepath.Join(base, "backups"), composeDir)

		// Simulate the failed deploy having overwritten the live dir.
		writeMarker(t, composeDir, "core.yml", "bad-v2")

		d := &DeployOps{}
		err := d.RollbackRemoteCompose(context.Background(), "user@testhost", composeDir, backupPath, deployErr)
		require.ErrorIs(t, err, ErrRollbackSucceeded)
		assert.ErrorContains(t, err, "remote compose exited 1")
		assert.Equal(t, "good-v1", readMarker(t, composeDir, "core.yml"),
			"the backed-up compose dir must be restored to the live target")
	})

	t.Run("empty backup anchor returns ErrRollbackFailed", func(t *testing.T) {
		d := &DeployOps{}
		err := d.RollbackRemoteCompose(context.Background(), "user@testhost", "/appdata/compose", "", deployErr)
		require.ErrorIs(t, err, ErrRollbackFailed)
		assert.ErrorContains(t, err, "remote compose exited 1")
	})

	t.Run("unverifiable anchor returns ErrRollbackFailed", func(t *testing.T) {
		// A backup dir with no configs.tar.gz fails VerifyBackup.
		empty := t.TempDir()
		d := &DeployOps{}
		err := d.RollbackRemoteCompose(context.Background(), "user@testhost", "/appdata/compose", empty, deployErr)
		require.ErrorIs(t, err, ErrRollbackFailed)
	})

	t.Run("compose dir absent from the anchor returns ErrRollbackFailed", func(t *testing.T) {
		base := t.TempDir()
		composeDir := filepath.Join(base, "appdata", "compose")
		writeMarker(t, composeDir, "core.yml", "good")
		backupPath := backupComposeDir(t, &DeployOps{}, filepath.Join(base, "backups"), composeDir)

		d := &DeployOps{}
		// A different remoteComposeDir that was never backed up.
		err := d.RollbackRemoteCompose(context.Background(), "user@testhost", filepath.Join(base, "other", "compose"), backupPath, deployErr)
		require.ErrorIs(t, err, ErrRollbackFailed)
		assert.ErrorContains(t, err, "not found in anchor")
	})

	t.Run("compose up after restore fails returns ErrRollbackFailed", func(t *testing.T) {
		setupSSHShim(t)
		setupSha256sumShim(t)
		setupDockerShim(t, 1) // both the up and the ps classify fail

		base := t.TempDir()
		composeDir := filepath.Join(base, "appdata", "compose")
		writeMarker(t, composeDir, "core.yml", "good-v1")
		backupPath := backupComposeDir(t, &DeployOps{}, filepath.Join(base, "backups"), composeDir)
		writeMarker(t, composeDir, "core.yml", "bad-v2")

		d := &DeployOps{}
		err := d.RollbackRemoteCompose(context.Background(), "user@testhost", composeDir, backupPath, deployErr)
		require.ErrorIs(t, err, ErrRollbackFailed)
		// The restore itself still landed the backed-up tree before compose up failed.
		assert.Equal(t, "good-v1", readMarker(t, composeDir, "core.yml"))
	})
}
