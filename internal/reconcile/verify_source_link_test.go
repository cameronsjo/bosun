package reconcile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cameronsjo/bosun/internal/fileutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFileEqual_SymlinkSourceRefused pins that the deploy invariant's content
// comparison never reads a source through a symlink. Callers select regular
// files by Lstat, so a link here means the entry was swapped after that check;
// comparing through it would judge the destination against content the copy
// path never deploys (cameronsjo/bosun#802).
func TestFileEqual_SymlinkSourceRefused(t *testing.T) {
	dir := evalSymlinks(t, t.TempDir())
	target := filepath.Join(dir, "target.txt")
	require.NoError(t, os.WriteFile(target, []byte("same"), 0o644))
	src := filepath.Join(dir, "link")
	require.NoError(t, os.Symlink(target, src))
	dst := filepath.Join(dir, "dst.txt")
	require.NoError(t, os.WriteFile(dst, []byte("same"), 0o644))

	equal, err := fileEqual(src, dst)

	require.ErrorIs(t, err, fileutil.ErrSymlinkSkipped)
	assert.False(t, equal)
}
