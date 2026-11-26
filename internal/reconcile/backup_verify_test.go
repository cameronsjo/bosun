package reconcile

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeGoTarGz writes a gzip-compressed tar holding one regular file per name,
// using Go's archive packages so the fixture needs no system tar binary.
func writeGoTarGz(t *testing.T, dst string, names ...string) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, name := range names {
		body := []byte("content of " + name)
		require.NoError(t, tw.WriteHeader(&tar.Header{
			Name:     name,
			Mode:     0o644,
			Size:     int64(len(body)),
			Typeflag: tar.TypeReg,
		}))
		_, err := tw.Write(body)
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	require.NoError(t, os.WriteFile(dst, buf.Bytes(), 0o644))
}

// writeGzipBytes writes raw bytes as a gzip stream: valid gzip, no valid tar.
func writeGzipBytes(t *testing.T, dst string, payload []byte) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	_, err := gz.Write(payload)
	require.NoError(t, err)
	require.NoError(t, gz.Close())
	require.NoError(t, os.WriteFile(dst, buf.Bytes(), 0o644))
}

// installTarTripwire puts a `tar` first on PATH that records every call and
// fails. It returns the call log; the file exists only if tar was invoked.
func installTarTripwire(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	callLog := filepath.Join(dir, "tar-calls")
	t.Setenv("TAR_TRIPWIRE_LOG", callLog)
	shim := "#!/bin/sh\necho \"$*\" >> \"$TAR_TRIPWIRE_LOG\"\necho 'tar: tripwire' >&2\nexit 99\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tar"), []byte(shim), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return callLog
}

// TestVerifyBackup_DoesNotExecTar pins that backup verification reads the
// archive in-process: one gzip+tar pass, no `tar -tzf` subprocess and no second
// decompression. The tripwire tar fails every call, so a verifier that still
// shells out rejects a valid archive.
func TestVerifyBackup_DoesNotExecTar(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the tar tripwire is a POSIX shell script")
	}
	callLog := installTarTripwire(t)

	backupPath := filepath.Join(t.TempDir(), "backup")
	require.NoError(t, os.MkdirAll(backupPath, 0o755))
	writeGoTarGz(t, filepath.Join(backupPath, "configs.tar.gz"), "conf/app.yml")

	err := NewDeployOps(false, "").VerifyBackup(context.Background(), backupPath)

	require.NoError(t, err, "a valid archive must verify without any tar subprocess")
	assert.NoFileExists(t, callLog, "verification must not invoke the tar binary")
}

// TestVerifyBackup_InProcessScan pins the verdict and message for each archive
// shape the in-process pass replaces the tar listing for. Messages stay stable:
// docs/workflows.md lists "backup archive is corrupted" as an operator-facing
// error.
func TestVerifyBackup_InProcessScan(t *testing.T) {
	tests := []struct {
		name    string
		write   func(t *testing.T, tarFile string)
		wantErr string // empty means success
	}{
		{
			name:  "valid archive",
			write: func(t *testing.T, tarFile string) { writeGoTarGz(t, tarFile, "conf/app.yml", "conf/other.yml") },
		},
		{
			name:    "gzip of an empty tar has no entries",
			write:   func(t *testing.T, tarFile string) { writeGoTarGz(t, tarFile) },
			wantErr: "contains no files",
		},
		{
			name: "not gzip",
			write: func(t *testing.T, tarFile string) {
				require.NoError(t, os.WriteFile(tarFile, []byte("not a tar file"), 0o644))
			},
			wantErr: "corrupted",
		},
		{
			name: "gzip with a damaged tar header",
			write: func(t *testing.T, tarFile string) {
				writeGzipBytes(t, tarFile, bytes.Repeat([]byte("x"), 512))
			},
			wantErr: "corrupted",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backupPath := filepath.Join(t.TempDir(), "backup")
			require.NoError(t, os.MkdirAll(backupPath, 0o755))
			tt.write(t, filepath.Join(backupPath, "configs.tar.gz"))

			err := NewDeployOps(false, "").VerifyBackup(context.Background(), backupPath)

			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// TestVerifyBackup_UnreadableArchive covers the open failure the in-process pass
// now owns: the archive exists and is non-empty but cannot be opened.
func TestVerifyBackup_UnreadableArchive(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod access failures are not portable to Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file permissions")
	}
	backupPath := filepath.Join(t.TempDir(), "backup")
	require.NoError(t, os.MkdirAll(backupPath, 0o755))
	tarFile := filepath.Join(backupPath, "configs.tar.gz")
	writeGoTarGz(t, tarFile, "conf/app.yml")
	require.NoError(t, os.Chmod(tarFile, 0o000))
	t.Cleanup(func() { _ = os.Chmod(tarFile, 0o644) })

	err := NewDeployOps(false, "").VerifyBackup(context.Background(), backupPath)

	require.Error(t, err)
}

// TestVerifyBackup_ReportsBombAndCancelDistinctly pins that the budget and the
// caller's context stay visible to errors.Is through the verifier.
func TestVerifyBackup_ReportsBombAndCancelDistinctly(t *testing.T) {
	backupPath := filepath.Join(t.TempDir(), "backup")
	require.NoError(t, os.MkdirAll(backupPath, 0o755))
	tarFile := filepath.Join(backupPath, "configs.tar.gz")
	writeGoTarGz(t, tarFile, "conf/app.yml")

	t.Run("cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := NewDeployOps(false, "").VerifyBackup(ctx, backupPath)
		require.Error(t, err)
		assert.True(t, errors.Is(err, context.Canceled), "got: %v", err)
	})

	t.Run("byte budget", func(t *testing.T) {
		err := verifyArchiveIntegrity(context.Background(), tarFile, 1)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrBackupTooLarge)
	})
}
