//go:build aix || darwin || dragonfly || freebsd || illumos || linux || netbsd || openbsd || solaris

package reconcile

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildTransferManifest_LargeUniqueTreeUsesIdentityIndex(t *testing.T) {
	const fileCount = 512
	root := t.TempDir()
	for i := range fileCount {
		writeMarker(t, root, fmt.Sprintf("file-%04d", i), fmt.Sprintf("content-%d", i))
	}

	hashCalls := 0
	fallbackScans := 0
	comparisons := 0
	ops := defaultTransferManifestOps()
	ops.observeFallbackScan = func() { fallbackScans++ }
	originalHash := ops.hashFile
	ops.hashFile = func(ctx context.Context, path string) ([sha256.Size]byte, error) {
		hashCalls++
		return originalHash(ctx, path)
	}
	originalSameFile := ops.sameFile
	ops.sameFile = func(a, b fs.FileInfo) bool {
		comparisons++
		return originalSameFile(a, b)
	}

	manifest, err := buildTransferManifestWithOps(context.Background(), root, ops)

	require.NoError(t, err)
	assert.Len(t, manifest, fileCount)
	assert.Equal(t, fileCount, hashCalls)
	assert.Zero(t, fallbackScans, "an all-indexed unique tree never enters the fallback scan")
	assert.Zero(t, comparisons)
}

func TestBuildTransferManifest_HardlinkHeavyTreeHashesInodeOnce(t *testing.T) {
	const linkCount = 512
	root := t.TempDir()
	canonical := filepath.Join(root, "file-0000")
	require.NoError(t, os.WriteFile(canonical, []byte("shared content"), 0o644))
	for i := 1; i < linkCount; i++ {
		require.NoError(t, os.Link(canonical, filepath.Join(root, fmt.Sprintf("file-%04d", i))))
	}

	hashCalls := 0
	fallbackScans := 0
	comparisons := 0
	ops := defaultTransferManifestOps()
	ops.observeFallbackScan = func() { fallbackScans++ }
	originalHash := ops.hashFile
	ops.hashFile = func(ctx context.Context, path string) ([sha256.Size]byte, error) {
		hashCalls++
		return originalHash(ctx, path)
	}
	originalSameFile := ops.sameFile
	ops.sameFile = func(a, b fs.FileInfo) bool {
		comparisons++
		return originalSameFile(a, b)
	}

	manifest, err := buildTransferManifestWithOps(context.Background(), root, ops)

	require.NoError(t, err)
	require.Len(t, manifest, linkCount)
	assert.Equal(t, 1, hashCalls, "one inode is hashed once regardless of link count")
	assert.Zero(t, fallbackScans, "indexed hardlinks never enter the fallback scan")
	assert.Zero(t, comparisons)
	for i, entry := range manifest {
		assert.Equal(t, manifest[0].SHA256, entry.SHA256)
		if i == 0 {
			assert.Empty(t, entry.HardlinkTo)
		} else {
			assert.Equal(t, manifest[0].Path, entry.HardlinkTo)
		}
	}
}

func TestBuildTransferManifest_MixedIdentityFallbackIndexesCanonicalAndStopsScanning(t *testing.T) {
	root := t.TempDir()
	canonical := filepath.Join(root, "file-0000")
	linked := filepath.Join(root, "file-0001")
	require.NoError(t, os.WriteFile(canonical, []byte("shared content"), 0o644))
	require.NoError(t, os.Link(canonical, linked))
	writeMarker(t, root, "file-0002", "unique content")

	hashCalls := 0
	fallbackScans := 0
	comparisons := 0
	ops := defaultTransferManifestOps()
	ops.observeFallbackScan = func() { fallbackScans++ }
	ops.identity = func(path string, info fs.FileInfo) (transferFileIdentity, bool) {
		identity, known := platformTransferFileIdentity(path, info)
		if filepath.Base(path) == "file-0000" {
			return transferFileIdentity{}, false
		}
		return identity, known
	}
	originalHash := ops.hashFile
	ops.hashFile = func(ctx context.Context, path string) ([sha256.Size]byte, error) {
		hashCalls++
		return originalHash(ctx, path)
	}
	originalSameFile := ops.sameFile
	ops.sameFile = func(a, b fs.FileInfo) bool {
		comparisons++
		return originalSameFile(a, b)
	}

	manifest, err := buildTransferManifestWithOps(context.Background(), root, ops)

	require.NoError(t, err)
	require.Len(t, manifest, 3)
	assert.Equal(t, 2, hashCalls, "the recovered hardlink identity reuses its canonical hash")
	assert.Equal(t, 1, fallbackScans, "matching the sole unindexed canonical empties the fallback set")
	assert.Equal(t, 1, comparisons)
	assert.Equal(t, "file-0000", manifest[1].HardlinkTo)
	assert.Equal(t, manifest[0].SHA256, manifest[1].SHA256)
	assert.Empty(t, manifest[2].HardlinkTo)
}

func TestBuildTransferManifest_FallbackChecksCancellationInsideScan(t *testing.T) {
	const fileCount = 64
	root := t.TempDir()
	for i := range fileCount {
		writeMarker(t, root, fmt.Sprintf("file-%04d", i), fmt.Sprintf("content-%d", i))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	comparisons := 0
	ops := defaultTransferManifestOps()
	ops.identity = func(string, fs.FileInfo) (transferFileIdentity, bool) {
		return transferFileIdentity{}, false
	}
	ops.sameFile = func(fs.FileInfo, fs.FileInfo) bool {
		comparisons++
		if comparisons == 25 {
			cancel()
		}
		return false
	}

	_, err := buildTransferManifestWithOps(ctx, root, ops)

	require.ErrorIs(t, err, context.Canceled)
	assert.LessOrEqual(t, comparisons, 25, "fallback stops before scanning another prior entry")
}
