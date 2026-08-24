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

// writeGzTarArchiveHeaders writes a gzip-compressed tar from explicit headers.
// A header with a non-empty Linkname and TypeSymlink/TypeLink writes a link;
// otherwise a small regular-file body is written for TypeReg entries.
func writeGzTarArchiveHeaders(t *testing.T, path string, hdrs ...*tar.Header) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	f, err := os.Create(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, hdr := range hdrs {
		body := []byte("data")
		if hdr.Typeflag == tar.TypeReg && hdr.Size == 0 {
			hdr.Size = int64(len(body))
		}
		require.NoError(t, tw.WriteHeader(hdr))
		if hdr.Typeflag == tar.TypeReg {
			_, werr := tw.Write(body[:hdr.Size])
			require.NoError(t, werr)
		}
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
}

func regHdr(name string) *tar.Header {
	return &tar.Header{Name: name, Mode: 0o644, Typeflag: tar.TypeReg}
}

func TestSafeExtractBackup(t *testing.T) {
	t.Run("extracts a benign archive under the root", func(t *testing.T) {
		tarFile := filepath.Join(t.TempDir(), "ok.tar.gz")
		writeGzTarArchiveHeaders(t, tarFile,
			regHdr("mnt/user/appdata/compose/core.yml"),
			regHdr("mnt/user/appdata/x"),
		)
		root, cleanup, err := safeExtractBackup(context.Background(), tarFile)
		require.NoError(t, err)
		defer cleanup()
		assert.FileExists(t, filepath.Join(root, "mnt/user/appdata/compose/core.yml"))
		assert.FileExists(t, filepath.Join(root, "mnt/user/appdata/x"))
	})

	t.Run("rejects a name that traverses out of the root", func(t *testing.T) {
		tarFile := filepath.Join(t.TempDir(), "evil.tar.gz")
		writeGzTarArchiveHeaders(t, tarFile, regHdr("good/file.yml"), regHdr("../../etc/passwd"))
		_, _, err := safeExtractBackup(context.Background(), tarFile)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "escapes extraction root")
	})

	t.Run("rejects an absolute symlink target (write-through escape)", func(t *testing.T) {
		tarFile := filepath.Join(t.TempDir(), "symlink-abs.tar.gz")
		writeGzTarArchiveHeaders(t, tarFile,
			&tar.Header{Name: "compose/evil", Typeflag: tar.TypeSymlink, Linkname: "/etc/cron.d", Mode: 0o777},
		)
		_, _, err := safeExtractBackup(context.Background(), tarFile)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "symlink target escapes extraction root")
	})

	t.Run("rejects a climbing symlink target", func(t *testing.T) {
		tarFile := filepath.Join(t.TempDir(), "symlink-climb.tar.gz")
		writeGzTarArchiveHeaders(t, tarFile,
			&tar.Header{Name: "compose/evil", Typeflag: tar.TypeSymlink, Linkname: "../../../../etc", Mode: 0o777},
		)
		_, _, err := safeExtractBackup(context.Background(), tarFile)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "symlink target escapes extraction root")
	})

	t.Run("rejects a hardlink target that escapes the root", func(t *testing.T) {
		tarFile := filepath.Join(t.TempDir(), "hardlink.tar.gz")
		writeGzTarArchiveHeaders(t, tarFile,
			&tar.Header{Name: "compose/evil", Typeflag: tar.TypeLink, Linkname: "../../../../etc/passwd"},
		)
		_, _, err := safeExtractBackup(context.Background(), tarFile)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "hardlink target escapes extraction root")
	})

	t.Run("allows a within-root symlink", func(t *testing.T) {
		tarFile := filepath.Join(t.TempDir(), "symlink-ok.tar.gz")
		writeGzTarArchiveHeaders(t, tarFile,
			regHdr("compose/real.yml"),
			&tar.Header{Name: "compose/link.yml", Typeflag: tar.TypeSymlink, Linkname: "real.yml", Mode: 0o777},
		)
		root, cleanup, err := safeExtractBackup(context.Background(), tarFile)
		require.NoError(t, err)
		defer cleanup()
		target, lerr := os.Readlink(filepath.Join(root, "compose/link.yml"))
		require.NoError(t, lerr)
		assert.Equal(t, "real.yml", target)
	})

	t.Run("blocks the symlink-then-write-through escape end to end", func(t *testing.T) {
		// An escaping symlink followed by a file written through it: the symlink
		// is rejected before creation, so the through-write can never land outside.
		tarFile := filepath.Join(t.TempDir(), "escape.tar.gz")
		writeGzTarArchiveHeaders(t, tarFile,
			&tar.Header{Name: "compose/escape", Typeflag: tar.TypeSymlink, Linkname: "/tmp", Mode: 0o777},
			regHdr("compose/escape/payload"),
		)
		_, _, err := safeExtractBackup(context.Background(), tarFile)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "symlink target escapes extraction root")
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

	t.Run("re-push failing integrity returns ErrRollbackFailed", func(t *testing.T) {
		// The anchor verifies and extracts fine, but the restored tree's re-push
		// itself fails the SHA-256 transfer check — rollback must surface that as
		// ErrRollbackFailed rather than silently trusting a corrupt restore.
		setupSha256sumShim(t)
		base := t.TempDir()
		composeDir := filepath.Join(base, "appdata", "compose")
		writeMarker(t, composeDir, "core.yml", "good-v1")
		backupPath := backupComposeDir(t, &DeployOps{}, filepath.Join(base, "backups"), composeDir)

		// SSH shim that corrupts the staged tree after each tar extraction, so the
		// re-push's integrity verification always fails.
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
			"  *'cat > '*'.deploy-tmp-'*'.tar'*)\n" +
			"    /bin/sh -c \"$*\"\n" +
			"    tmp=$(echo \"$*\" | sed 's/.*-C \\([^ ]*\\) -xf.*/\\1/')\n" +
			"    find \"$tmp\" -type f -exec sh -c 'echo tampered >> \"$1\"' _ {} \\;\n" +
			"    ;;\n" +
			"  *) exec /bin/sh -c \"$*\" ;;\n" +
			"esac\n"
		require.NoError(t, os.WriteFile(filepath.Join(dir, "ssh"), []byte(shim), 0o755))
		t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

		d := &DeployOps{}
		err := d.RollbackRemoteCompose(context.Background(), "user@testhost", composeDir, backupPath, deployErr)
		require.ErrorIs(t, err, ErrRollbackFailed)
		assert.ErrorContains(t, err, "re-deploy restored compose dir")
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
