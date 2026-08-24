package reconcile

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
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

type tarEntryFixture struct {
	header *tar.Header
	body   []byte
}

func buildRawTar(t *testing.T, entries ...tarEntryFixture) []byte {
	t.Helper()
	var raw bytes.Buffer
	tw := tar.NewWriter(&raw)
	for _, entry := range entries {
		header := *entry.header
		header.Size = int64(len(entry.body))
		require.NoError(t, tw.WriteHeader(&header))
		if len(entry.body) > 0 {
			_, err := tw.Write(entry.body)
			require.NoError(t, err)
		}
	}
	require.NoError(t, tw.Close())
	return raw.Bytes()
}

func assertFailedExtractionCleaned(t *testing.T, tmpRoot, root string, cleanup func()) {
	t.Helper()
	assert.Empty(t, root)
	require.NotNil(t, cleanup)
	cleanup()
	entries, err := os.ReadDir(tmpRoot)
	require.NoError(t, err)
	assert.Empty(t, entries, "failed extraction must remove its temporary tree")
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
		tmpRoot := t.TempDir()
		t.Setenv("TMPDIR", tmpRoot)
		p := filepath.Join(t.TempDir(), "corrupt.tar.gz")
		writeGzBytes(t, p, []byte("this is gzipped but is not a tar archive"))
		root, cleanup, err := safeExtractBackup(context.Background(), p)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot read archive entry header")
		assertFailedExtractionCleaned(t, tmpRoot, root, cleanup)
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
		tmpRoot := t.TempDir()
		t.Setenv("TMPDIR", tmpRoot)
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

		root, cleanup, err := safeExtractBackupBounded(context.Background(), p, 1024)
		require.ErrorIs(t, err, ErrBackupTooLarge)
		assert.Contains(t, err.Error(), "cannot extract",
			"regular-copy overflow must retain its archive-entry operation")
		assertFailedExtractionCleaned(t, tmpRoot, root, cleanup)
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
	_, err := writeRegularEntry(context.Background(), dest, nil)
	require.Error(t, err)
}

type cancelAfterFirstRead struct {
	cancel context.CancelFunc
	done   bool
}

func (r *cancelAfterFirstRead) Read(p []byte) (int, error) {
	if r.done {
		return 0, io.EOF
	}
	r.done = true
	n := copy(p, []byte("partial archive content"))
	r.cancel()
	return n, nil
}

func TestSafeExtractBackup_CancellationDuringRegularCopyCleansPartialTree(t *testing.T) {
	tmpRoot := t.TempDir()
	t.Setenv("TMPDIR", tmpRoot)
	tarFile := filepath.Join(t.TempDir(), "cancel.tar.gz")
	writeGzTarArchiveHeaders(t, tarFile, regHdr("compose/config.yml"))

	ctx, cancel := context.WithCancel(context.Background())
	root, cleanup, err := safeExtractBackupBoundedWithWriter(
		ctx,
		tarFile,
		MaxVerifyDecompressedBytes,
		func(ctx context.Context, dest string, _ io.Reader) (int64, error) {
			return writeRegularEntry(ctx, dest, &cancelAfterFirstRead{cancel: cancel})
		},
	)

	require.ErrorIs(t, err, context.Canceled)
	assertFailedExtractionCleaned(t, tmpRoot, root, cleanup)
}

func TestSafeExtractBackup_RejectsCorruptGzipTrailerAndCleans(t *testing.T) {
	base := t.TempDir()
	tarFile := filepath.Join(base, "corrupt-trailer.tar.gz")
	raw := buildRawTar(t, tarEntryFixture{header: regHdr("compose/core.yml"), body: []byte("data")})
	writeGzBytes(t, tarFile, raw)

	compressed, err := os.ReadFile(tarFile)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(compressed), 8)
	compressed[len(compressed)-8] ^= 0xff // corrupt the gzip CRC32 trailer
	require.NoError(t, os.WriteFile(tarFile, compressed, 0o644))
	tmpRoot := filepath.Join(base, "extract-tmp")
	require.NoError(t, os.MkdirAll(tmpRoot, 0o755))
	t.Setenv("TMPDIR", tmpRoot)

	root, cleanup, err := safeExtractBackup(context.Background(), tarFile)

	require.ErrorIs(t, err, gzip.ErrChecksum)
	assertFailedExtractionCleaned(t, tmpRoot, root, cleanup)
}

func TestSafeExtractBackup_WholeStreamBoundIncludesUnsupportedBody(t *testing.T) {
	base := t.TempDir()
	tarFile := filepath.Join(base, "unsupported-body.tar.gz")
	raw := buildRawTar(t,
		tarEntryFixture{
			header: &tar.Header{Name: "ignored.bin", Mode: 0o644, Typeflag: 'Z'},
			body:   bytes.Repeat([]byte("x"), 2048),
		},
	)
	writeGzBytes(t, tarFile, raw)
	tmpRoot := filepath.Join(base, "extract-tmp")
	require.NoError(t, os.MkdirAll(tmpRoot, 0o755))
	t.Setenv("TMPDIR", tmpRoot)

	root, cleanup, err := safeExtractBackupBounded(context.Background(), tarFile, 1024)

	require.ErrorIs(t, err, ErrBackupTooLarge)
	assert.Contains(t, err.Error(), "cannot read archive entry header",
		"tar.Next must surface the whole-stream overflow while discarding the unsupported body")
	assertFailedExtractionCleaned(t, tmpRoot, root, cleanup)
}

func TestBoundedContextReader_CancelsWhileTarNextDiscardsUnsupportedBody(t *testing.T) {
	raw := buildRawTar(t,
		tarEntryFixture{
			header: &tar.Header{Name: "ignored.bin", Mode: 0o644, Typeflag: 'Z'},
			body:   bytes.Repeat([]byte("x"), 2048),
		},
		tarEntryFixture{header: regHdr("compose/next.yml"), body: []byte("next")},
	)
	ctx, cancel := context.WithCancel(context.Background())
	stream := &boundedContextReader{ctx: ctx, reader: bytes.NewReader(raw), remaining: int64(len(raw))}
	tr := tar.NewReader(stream)

	hdr, err := tr.Next()
	require.NoError(t, err)
	require.Equal(t, byte('Z'), hdr.Typeflag)
	cancel()
	_, err = tr.Next() // tar.Next must discard ignored.bin before reading next.yml.

	require.ErrorIs(t, err, context.Canceled)
}

func TestSafeExtractBackup_WholeStreamBoundaryCountsHeadersPaddingAndEndMarkers(t *testing.T) {
	base := t.TempDir()
	tarFile := filepath.Join(base, "boundary.tar.gz")
	raw := buildRawTar(t, tarEntryFixture{header: regHdr("one-byte"), body: []byte("x")})
	require.Greater(t, len(raw), 1, "raw tar size must include structural bytes beyond the file body")
	writeGzBytes(t, tarFile, raw)

	root, cleanup, err := safeExtractBackupBounded(context.Background(), tarFile, int64(len(raw)))
	require.NoError(t, err, "the exact whole-stream boundary must succeed")
	assert.FileExists(t, filepath.Join(root, "one-byte"))
	cleanup()
	assert.NoDirExists(t, root)

	tmpRoot := filepath.Join(base, "extract-tmp")
	require.NoError(t, os.MkdirAll(tmpRoot, 0o755))
	t.Setenv("TMPDIR", tmpRoot)
	root, cleanup, err = safeExtractBackupBounded(context.Background(), tarFile, int64(len(raw)-1))
	require.ErrorIs(t, err, ErrBackupTooLarge,
		"one byte below the raw tar size must fail because headers, padding, and end markers count")
	assertFailedExtractionCleaned(t, tmpRoot, root, cleanup)
}

func TestSafeExtractBackup_TrailingDrainPropagatesOverflowAndCleans(t *testing.T) {
	base := t.TempDir()
	tarFile := filepath.Join(base, "trailing-overflow.tar.gz")
	raw := buildRawTar(t, tarEntryFixture{header: regHdr("compose/core.yml"), body: []byte("data")})
	raw = append(raw, bytes.Repeat([]byte("trailing"), 32)...)
	writeGzBytes(t, tarFile, raw)
	tmpRoot := filepath.Join(base, "extract-tmp")
	require.NoError(t, os.MkdirAll(tmpRoot, 0o755))
	t.Setenv("TMPDIR", tmpRoot)

	root, cleanup, err := safeExtractBackupBounded(context.Background(), tarFile, int64(len(raw)-1))

	require.ErrorIs(t, err, ErrBackupTooLarge,
		"overflow returned with trailing drain bytes must remain classifiable")
	assert.Contains(t, err.Error(), "cannot finish archive stream")
	assertFailedExtractionCleaned(t, tmpRoot, root, cleanup)
}

func TestSafeExtractBackup_ValidationFailuresCleanPartialTree(t *testing.T) {
	tests := []struct {
		name      string
		malicious *tar.Header
	}{
		{name: "member traversal", malicious: regHdr("../../escape")},
		{name: "absolute symlink", malicious: &tar.Header{Name: "compose/escape", Typeflag: tar.TypeSymlink, Linkname: "/tmp", Mode: 0o777}},
		{name: "escaping symlink", malicious: &tar.Header{Name: "compose/escape", Typeflag: tar.TypeSymlink, Linkname: "../../../escape", Mode: 0o777}},
		{name: "escaping hardlink", malicious: &tar.Header{Name: "compose/escape", Typeflag: tar.TypeLink, Linkname: "../../../escape", Mode: 0o644}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpRoot := t.TempDir()
			t.Setenv("TMPDIR", tmpRoot)
			tarFile := filepath.Join(t.TempDir(), "invalid.tar.gz")
			writeGzTarArchiveHeaders(t, tarFile, regHdr("compose/early.yml"), tt.malicious)

			root, cleanup, err := safeExtractBackup(context.Background(), tarFile)
			require.Error(t, err)
			assertFailedExtractionCleaned(t, tmpRoot, root, cleanup)
		})
	}
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
