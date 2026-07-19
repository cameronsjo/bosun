package reconcile

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func dirHdr(name string) *tar.Header {
	return &tar.Header{Name: name, Typeflag: tar.TypeDir, Mode: 0o755}
}

// writeGzBytes gzip-compresses raw (non-tar) bytes to path.
func writeGzBytes(t *testing.T, path string, raw []byte) {
	t.Helper()
	f, err := os.Create(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	gz := gzip.NewWriter(f)
	_, werr := gz.Write(raw)
	require.NoError(t, werr)
	require.NoError(t, gz.Close())
}

func TestSafeExtractBackup_ErrorPaths(t *testing.T) {
	t.Run("missing archive file", func(t *testing.T) {
		_, _, err := safeExtractBackup(context.Background(), filepath.Join(t.TempDir(), "nope.tar.gz"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot open archive")
	})

	t.Run("not a gzip stream", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "plain.tar.gz")
		require.NoError(t, os.WriteFile(p, []byte("not gzip at all"), 0o644))
		_, _, err := safeExtractBackup(context.Background(), p)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot read gzip header")
	})

	t.Run("valid gzip but corrupt tar", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "corrupt.tar.gz")
		writeGzBytes(t, p, []byte("this is gzipped but is not a tar archive"))
		_, _, err := safeExtractBackup(context.Background(), p)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot read archive entry header")
	})

	t.Run("directory entry whose parent is a regular file (ENOTDIR)", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "a.tar.gz")
		writeGzTarArchiveHeaders(t, p, regHdr("a"), dirHdr("a/b"))
		_, _, err := safeExtractBackup(context.Background(), p)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot create dir")
	})

	t.Run("regular file whose parent is a regular file (ENOTDIR)", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "a.tar.gz")
		writeGzTarArchiveHeaders(t, p, regHdr("a"), regHdr("a/b"))
		_, _, err := safeExtractBackup(context.Background(), p)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot create parent for")
	})

	t.Run("regular file collides with an existing directory", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "a.tar.gz")
		writeGzTarArchiveHeaders(t, p, dirHdr("a"), regHdr("a"))
		_, _, err := safeExtractBackup(context.Background(), p)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot extract")
	})

	t.Run("decompression-bomb bound trips ErrBackupTooLarge", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "big.tar.gz")
		// A single 4 KiB entry against a 1 KiB budget overflows the cap.
		body := make([]byte, 4096)
		hdr := &tar.Header{Name: "big.bin", Mode: 0o644, Typeflag: tar.TypeReg, Size: int64(len(body))}
		f, err := os.Create(p)
		require.NoError(t, err)
		gz := gzip.NewWriter(f)
		tw := tar.NewWriter(gz)
		require.NoError(t, tw.WriteHeader(hdr))
		_, werr := tw.Write(body)
		require.NoError(t, werr)
		require.NoError(t, tw.Close())
		require.NoError(t, gz.Close())
		require.NoError(t, f.Close())

		_, _, err = safeExtractBackupBounded(context.Background(), p, 1024)
		require.ErrorIs(t, err, ErrBackupTooLarge)
	})

	t.Run("cancelled context aborts extraction", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "ok.tar.gz")
		writeGzTarArchiveHeaders(t, p, regHdr("a"), regHdr("b"))
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, _, err := safeExtractBackup(ctx, p)
		require.ErrorIs(t, err, context.Canceled)
	})

	t.Run("unknown entry type is skipped, extraction still succeeds", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "dev.tar.gz")
		writeGzTarArchiveHeaders(t, p,
			regHdr("compose/core.yml"),
			&tar.Header{Name: "compose/null", Typeflag: tar.TypeChar, Mode: 0o644, Devmajor: 1, Devminor: 3},
		)
		root, cleanup, err := safeExtractBackup(context.Background(), p)
		require.NoError(t, err)
		defer cleanup()
		assert.FileExists(t, filepath.Join(root, "compose/core.yml"))
		assert.NoFileExists(t, filepath.Join(root, "compose/null"), "a device entry must be skipped, not created")
	})
}

func TestWriteRegularEntry_OpenError(t *testing.T) {
	// dest under a nonexistent parent: os.OpenFile fails before any copy.
	dest := filepath.Join(t.TempDir(), "missing-parent", "file")
	_, err := writeRegularEntry(context.Background(), dest, nil, 1024)
	require.Error(t, err)
}

func TestWriteLinkEntry_CreateErrors(t *testing.T) {
	base := t.TempDir()

	t.Run("symlink create fails when the parent is missing", func(t *testing.T) {
		dest := filepath.Join(base, "missing", "link")
		err := writeLinkEntry(base, dest, &tar.Header{Typeflag: tar.TypeSymlink, Linkname: "target"})
		require.Error(t, err)
	})

	t.Run("hardlink create fails when the target is missing", func(t *testing.T) {
		dest := filepath.Join(base, "hardlink")
		err := writeLinkEntry(base, dest, &tar.Header{Typeflag: tar.TypeLink, Linkname: "does/not/exist"})
		require.Error(t, err)
	})
}
