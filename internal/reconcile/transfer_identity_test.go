package reconcile

import (
	"context"
	"crypto/sha256"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildTransferManifest_IdentityFallbackPreservesCorrectness(t *testing.T) {
	t.Run("unique files are hashed independently", func(t *testing.T) {
		root := t.TempDir()
		writeMarker(t, root, "first", "same content")
		writeMarker(t, root, "second", "same content")

		hashCalls := 0
		ops := defaultTransferManifestOps()
		ops.identity = func(string, fs.FileInfo) (transferFileIdentity, bool) {
			return transferFileIdentity{}, false
		}
		originalHash := ops.hashFile
		ops.hashFile = func(ctx context.Context, path string) ([sha256.Size]byte, error) {
			hashCalls++
			return originalHash(ctx, path)
		}

		manifest, err := buildTransferManifestWithOps(context.Background(), root, ops)

		require.NoError(t, err)
		require.Len(t, manifest, 2)
		assert.Empty(t, manifest[0].HardlinkTo)
		assert.Empty(t, manifest[1].HardlinkTo)
		assert.Equal(t, 2, hashCalls, "equal content without shared identity is not a hardlink")
	})

	t.Run("hardlinks reuse their canonical hash", func(t *testing.T) {
		root := t.TempDir()
		writeMarker(t, root, "canonical", "content")
		if err := os.Link(filepath.Join(root, "canonical"), filepath.Join(root, "linked")); err != nil {
			t.Skipf("hardlinks unavailable: %v", err)
		}

		hashCalls := 0
		ops := defaultTransferManifestOps()
		ops.identity = func(string, fs.FileInfo) (transferFileIdentity, bool) {
			return transferFileIdentity{}, false
		}
		originalHash := ops.hashFile
		ops.hashFile = func(ctx context.Context, path string) ([sha256.Size]byte, error) {
			hashCalls++
			return originalHash(ctx, path)
		}

		manifest, err := buildTransferManifestWithOps(context.Background(), root, ops)

		require.NoError(t, err)
		require.Len(t, manifest, 2)
		assert.Equal(t, 1, hashCalls)
		assert.Empty(t, manifest[0].HardlinkTo)
		assert.Equal(t, manifest[0].Path, manifest[1].HardlinkTo)
		assert.Equal(t, manifest[0].SHA256, manifest[1].SHA256)
	})
}
