package reconcile

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupSha256sumShim(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("sha256sum"); err == nil {
		return
	}
	shasum, err := exec.LookPath("shasum")
	require.NoError(t, err)
	dir := t.TempDir()
	shim := "#!/bin/sh\nexec " + shasum + " -a 256 \"$@\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sha256sum"), []byte(shim), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestBuildTransferManifest(t *testing.T) {
	root := t.TempDir()
	writeMarker(t, root, "regular", "content")
	require.NoError(t, os.Mkdir(filepath.Join(root, "empty-dir"), 0o755))
	require.NoError(t, os.Symlink("regular\n", filepath.Join(root, "link\n")))
	require.NoError(t, os.Link(filepath.Join(root, "regular"), filepath.Join(root, "hardlink")))

	manifest, err := buildTransferManifest(root)
	require.NoError(t, err)
	require.Len(t, manifest, 4)
	byPath := make(map[string]transferEntry, len(manifest))
	for _, entry := range manifest {
		byPath[entry.Path] = entry
	}
	assert.Equal(t, byte('d'), byPath["empty-dir"].Kind)
	assert.Equal(t, byte('l'), byPath["link\n"].Kind)
	assert.Equal(t, "regular\n", byPath["link\n"].LinkTarget)
	assert.Equal(t, byte('f'), byPath["regular"].Kind)
	assert.NotEmpty(t, byPath["regular"].SHA256)
	assert.True(t, byPath["hardlink"].HardlinkTo == "regular" || byPath["regular"].HardlinkTo == "hardlink")
}

func TestVerifyRemoteTransfer(t *testing.T) {
	setupSSHShim(t)
	d := &DeployOps{}

	t.Run("accepts an empty valid tree", func(t *testing.T) {
		require.NoError(t, d.verifyRemoteTransfer(context.Background(), "user@testhost", t.TempDir(), nil))
	})

	t.Run("handles control characters symlinks and hardlinks", func(t *testing.T) {
		staged := t.TempDir()
		writeMarker(t, staged, "line\nname\\file", "content")
		require.NoError(t, os.Symlink("line\nname\\file", filepath.Join(staged, "sym\nlink")))
		require.NoError(t, os.Link(filepath.Join(staged, "line\nname\\file"), filepath.Join(staged, "hard link")))
		manifest, err := buildTransferManifest(staged)
		require.NoError(t, err)
		require.NoError(t, d.verifyRemoteTransfer(context.Background(), "user@testhost", staged, manifest))
	})

	t.Run("rejects a missing entry", func(t *testing.T) {
		source := t.TempDir()
		writeMarker(t, source, "a", "a")
		writeMarker(t, source, "b", "b")
		manifest, err := buildTransferManifest(source)
		require.NoError(t, err)
		require.NoError(t, os.Remove(filepath.Join(source, "b")))
		err = d.verifyRemoteTransfer(context.Background(), "user@testhost", source, manifest)
		require.ErrorIs(t, err, ErrTransferIntegrity)
	})
}

func TestDeployRemote_ExitZeroIncompleteLocalArchiveCannotReplaceTarget(t *testing.T) {
	setupSSHShim(t)
	original := newLocalArchiveCommand
	t.Cleanup(func() { newLocalArchiveCommand = original })
	newLocalArchiveCommand = func(ctx context.Context, sourceDir, archivePath string) *exec.Cmd {
		return exec.CommandContext(ctx, "tar", "-C", sourceDir, "-cf", archivePath, "kept")
	}
	base := t.TempDir()
	source := filepath.Join(base, "source")
	target := filepath.Join(base, "deploy", "compose")
	writeMarker(t, source, "kept", "one")
	writeMarker(t, source, "omitted", "two")
	writeMarker(t, target, "live", "old")

	err := (&DeployOps{}).DeployRemote(context.Background(), source, "user@testhost", target)
	require.ErrorIs(t, err, ErrTransferIntegrity)
	assert.Equal(t, "old", readMarker(t, target, "live"))
	assert.NoFileExists(t, filepath.Join(target, "kept"))
}

func TestDeployRemote_PartialExtractionCannotReplaceTarget(t *testing.T) {
	setupSha256sumShim(t)
	base := t.TempDir()
	source := filepath.Join(base, "source")
	parent := filepath.Join(base, "deploy")
	target := filepath.Join(parent, "compose")
	writeMarker(t, source, "kept", "new-one")
	writeMarker(t, source, "omitted", "new-two")
	writeMarker(t, target, "live", "old")

	dir := t.TempDir()
	shim := "#!/bin/sh\n" +
		"while [ $# -gt 0 ]; do case \"$1\" in -o) shift 2 ;; -*) shift ;; *) break ;; esac; done\n" +
		"shift\n" +
		"case \"$*\" in\n" +
		"  *'cat > '*'.deploy-tmp-'*'.tar'*) /bin/sh -c \"$*\"; tmp=$(echo \"$*\" | sed 's/.*tar -C \\([^ ]*\\) -xf.*/\\1/'); rm -f \"$tmp/omitted\"; exit 0 ;;\n" +
		"  *) exec /bin/sh -c \"$*\" ;;\n" +
		"esac\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ssh"), []byte(shim), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	err := (&DeployOps{}).DeployRemote(context.Background(), source, "user@testhost", target)
	require.ErrorIs(t, err, ErrTransferIntegrity)
	assert.Equal(t, "old", readMarker(t, target, "live"))
	assert.NoFileExists(t, filepath.Join(target, "kept"))
	entries, readErr := os.ReadDir(parent)
	require.NoError(t, readErr)
	require.Len(t, entries, 1)
	assert.Equal(t, "compose", entries[0].Name())
}

func TestDeployRemote_ValidEmptyAndSpecialTreesPromote(t *testing.T) {
	t.Run("empty source is a valid complete tree", func(t *testing.T) {
		setupSSHShim(t)
		base := t.TempDir()
		source := filepath.Join(base, "source")
		target := filepath.Join(base, "deploy", "compose")
		require.NoError(t, os.MkdirAll(source, 0o755))
		writeMarker(t, target, "stale", "remove-me")
		require.NoError(t, (&DeployOps{}).DeployRemote(context.Background(), source, "user@testhost", target))
		entries, err := os.ReadDir(target)
		require.NoError(t, err)
		assert.Empty(t, entries)
	})

	t.Run("directories symlinks hardlinks and control names survive", func(t *testing.T) {
		setupSSHShim(t)
		base := t.TempDir()
		source := filepath.Join(base, "source")
		target := filepath.Join(base, "deploy", "compose")
		writeMarker(t, source, "line\nname\\file", "content")
		writeMarker(t, source, ".bosun-transfer.tar", "legitimate config")
		require.NoError(t, os.Mkdir(filepath.Join(source, "empty-dir"), 0o755))
		require.NoError(t, os.Symlink("line\nname\\file", filepath.Join(source, "sym\nlink")))
		require.NoError(t, os.Link(filepath.Join(source, "line\nname\\file"), filepath.Join(source, "hard link")))
		require.NoError(t, (&DeployOps{}).DeployRemote(context.Background(), source, "user@testhost", target))
		assert.DirExists(t, filepath.Join(target, "empty-dir"))
		link, err := os.Readlink(filepath.Join(target, "sym\nlink"))
		require.NoError(t, err)
		assert.Equal(t, "line\nname\\file", link)
		left, err := os.Stat(filepath.Join(target, "line\nname\\file"))
		require.NoError(t, err)
		right, err := os.Stat(filepath.Join(target, "hard link"))
		require.NoError(t, err)
		assert.True(t, os.SameFile(left, right))
		assert.Equal(t, "legitimate config", readMarker(t, target, ".bosun-transfer.tar"))
	})
}

func TestDeployRemote_IntegrityCapabilityMissingFailsClosed(t *testing.T) {
	dir := t.TempDir()
	shim := "#!/bin/sh\nwhile [ $# -gt 0 ]; do case \"$1\" in -o) shift 2 ;; -*) shift ;; *) break ;; esac; done\nshift\ncase \"$*\" in *'command -v sha256sum'*) exit 1 ;; esac\nexec /bin/sh -c \"$*\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ssh"), []byte(shim), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	base := t.TempDir()
	source := filepath.Join(base, "source")
	target := filepath.Join(base, "deploy", "compose")
	writeMarker(t, source, "new", "new")
	writeMarker(t, target, "live", "old")
	err := (&DeployOps{}).DeployRemote(context.Background(), source, "user@testhost", target)
	require.ErrorIs(t, err, ErrTransferIntegrity)
	assert.ErrorContains(t, err, "ssh extract failed")
	assert.Equal(t, "old", readMarker(t, target, "live"))
}
