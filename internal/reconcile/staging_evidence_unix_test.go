//go:build !windows

package reconcile

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProtectOrDeleteStagingRejectsIrregularEntry(t *testing.T) {
	root := filepath.Join(evalSymlinks(t, t.TempDir()), "staging")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, syscall.Mkfifo(filepath.Join(root, "secret-pipe"), 0o644))

	outcome, err := protectOrDeleteStaging(context.Background(), "unraid", root, defaultStagingEvidenceOps(), "test")
	require.NoError(t, err)
	assert.Equal(t, "discarded", outcome)
	assert.NoDirExists(t, root)
}
