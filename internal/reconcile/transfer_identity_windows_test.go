//go:build windows

package reconcile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlatformTransferFileIdentityWindows(t *testing.T) {
	root := t.TempDir()
	canonical := filepath.Join(root, "canonical")
	linked := filepath.Join(root, "linked")
	require.NoError(t, os.WriteFile(canonical, []byte("content"), 0o644))
	if err := os.Link(canonical, linked); err != nil {
		t.Skipf("hardlinks unavailable: %v", err)
	}

	canonicalInfo, err := os.Stat(canonical)
	require.NoError(t, err)
	linkedInfo, err := os.Stat(linked)
	require.NoError(t, err)
	canonicalID, canonicalOK := platformTransferFileIdentity(canonical, canonicalInfo)
	linkedID, linkedOK := platformTransferFileIdentity(linked, linkedInfo)

	assert.True(t, canonicalOK)
	assert.True(t, linkedOK)
	assert.Equal(t, canonicalID, linkedID)
}
