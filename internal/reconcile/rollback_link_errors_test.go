package reconcile

import (
	"archive/tar"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSafeExtractBackupRejectsUnrealizableLinkEntries(t *testing.T) {
	for _, tt := range []struct {
		name    string
		headers []*tar.Header
		wantErr string
	}{
		{
			name: "link parent is a regular file",
			headers: []*tar.Header{
				regHdr("parent"),
				{Name: "parent/link", Typeflag: tar.TypeLink, Linkname: "missing"},
			},
			wantErr: "cannot create parent",
		},
		{
			name:    "hardlink target is missing",
			headers: []*tar.Header{{Name: "link", Typeflag: tar.TypeLink, Linkname: "missing"}},
			wantErr: "cannot extract hardlink",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			base := t.TempDir()
			temporary := filepath.Join(base, "extraction")
			require.NoError(t, os.Mkdir(temporary, 0o700))
			t.Setenv("TMPDIR", temporary)
			archive := filepath.Join(base, "archive.tar.gz")
			writeGzTarArchiveHeaders(t, archive, tt.headers...)

			root, cleanup, err := safeExtractBackup(context.Background(), archive)

			require.ErrorContains(t, err, tt.wantErr)
			assertFailedExtractionCleaned(t, temporary, root, cleanup)
		})
	}
}

func TestResolveWithinRootAcceptsRootDirectoryEntry(t *testing.T) {
	path, ok := resolveWithinRoot("/")
	assert.True(t, ok)
	assert.Equal(t, ".", path)
}
