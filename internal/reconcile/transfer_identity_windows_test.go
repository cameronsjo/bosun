//go:build windows

package reconcile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlatformTransferFileIdentityWindows_HardlinksShareIdentity(t *testing.T) {
	root := t.TempDir()
	canonical := filepath.Join(root, "canonical")
	linked := filepath.Join(root, "linked")
	distinct := filepath.Join(root, "distinct")
	require.NoError(t, os.WriteFile(canonical, []byte("content"), 0o644))
	require.NoError(t, os.WriteFile(distinct, []byte("content"), 0o644))
	if err := os.Link(canonical, linked); err != nil {
		t.Skipf("hardlinks unavailable: %v", err)
	}

	canonicalInfo, err := os.Stat(canonical)
	require.NoError(t, err)
	linkedInfo, err := os.Stat(linked)
	require.NoError(t, err)
	distinctInfo, err := os.Stat(distinct)
	require.NoError(t, err)
	canonicalID, canonicalOK := platformTransferFileIdentity(canonical, canonicalInfo)
	linkedID, linkedOK := platformTransferFileIdentity(linked, linkedInfo)
	distinctID, distinctOK := platformTransferFileIdentity(distinct, distinctInfo)

	assert.True(t, canonicalOK)
	assert.True(t, linkedOK)
	assert.True(t, distinctOK)
	assert.Equal(t, canonicalID, linkedID)
	assert.NotEqual(t, canonicalID, distinctID)
}
