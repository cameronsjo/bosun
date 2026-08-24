package reconcile

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
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

	manifest, err := buildTransferManifest(context.Background(), root)
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

func TestBuildTransferManifest_FailsClosed(t *testing.T) {
	t.Run("cancelled context stops the snapshot", func(t *testing.T) {
		root := t.TempDir()
		writeMarker(t, root, "regular", strings.Repeat("x", 1024))
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := buildTransferManifest(ctx, root)

		require.ErrorIs(t, err, context.Canceled)
	})

	t.Run("fifo is unsupported", func(t *testing.T) {
		mkfifo, err := exec.LookPath("mkfifo")
		if err != nil {
			t.Skip("mkfifo is unavailable")
		}
		root := t.TempDir()
		require.NoError(t, exec.Command(mkfifo, filepath.Join(root, "named-pipe")).Run())

		_, err = buildTransferManifest(context.Background(), root)

		require.ErrorIs(t, err, ErrUnsupportedTransferEntry)
	})

	t.Run("unix socket is unsupported", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Unix sockets are unavailable")
		}
		root, err := os.MkdirTemp("/tmp", "bs-sock-")
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, os.RemoveAll(root)) })
		listener, err := net.Listen("unix", filepath.Join(root, "socket"))
		if err != nil {
			t.Skipf("Unix socket creation is unavailable: %v", err)
		}
		defer func() { require.NoError(t, listener.Close()) }()

		_, err = buildTransferManifest(context.Background(), root)

		require.ErrorIs(t, err, ErrUnsupportedTransferEntry)
	})
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
		manifest, err := buildTransferManifest(context.Background(), staged)
		require.NoError(t, err)
		require.NoError(t, d.verifyRemoteTransfer(context.Background(), "user@testhost", staged, manifest))
	})

	t.Run("rejects a missing entry", func(t *testing.T) {
		source := t.TempDir()
		writeMarker(t, source, "a", "a")
		writeMarker(t, source, "b", "b")
		manifest, err := buildTransferManifest(context.Background(), source)
		require.NoError(t, err)
		require.NoError(t, os.Remove(filepath.Join(source, "b")))
		err = d.verifyRemoteTransfer(context.Background(), "user@testhost", source, manifest)
		require.ErrorIs(t, err, ErrTransferIntegrity)
	})

	t.Run("rejects an unexpected entry", func(t *testing.T) {
		staged := t.TempDir()
		writeMarker(t, staged, "expected", "content")
		manifest, err := buildTransferManifest(context.Background(), staged)
		require.NoError(t, err)
		writeMarker(t, staged, "unexpected", "content")

		err = d.verifyRemoteTransfer(context.Background(), "user@testhost", staged, manifest)

		require.ErrorIs(t, err, ErrTransferIntegrity)
	})

	t.Run("rejects a failed entry walk even when its partial count matches", func(t *testing.T) {
		staged := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(staged, "expected-empty-dir"), 0o755))
		manifest, err := buildTransferManifest(context.Background(), staged)
		require.NoError(t, err)

		realFind, err := exec.LookPath("find")
		require.NoError(t, err)
		dir := t.TempDir()
		shim := "#!/bin/sh\n" + realFind + " \"$@\"\nexit 7\n"
		require.NoError(t, os.WriteFile(filepath.Join(dir, "find"), []byte(shim), 0o755))
		t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

		err = d.verifyRemoteTransfer(context.Background(), "user@testhost", staged, manifest)

		require.ErrorIs(t, err, ErrTransferIntegrity)
	})

	t.Run("rejects content and type changes", func(t *testing.T) {
		for _, mutate := range []struct {
			name string
			fn   func(t *testing.T, staged string)
		}{
			{name: "content", fn: func(t *testing.T, staged string) {
				require.NoError(t, os.WriteFile(filepath.Join(staged, "entry"), []byte("changed"), 0o644))
			}},
			{name: "type", fn: func(t *testing.T, staged string) {
				require.NoError(t, os.Remove(filepath.Join(staged, "entry")))
				require.NoError(t, os.Mkdir(filepath.Join(staged, "entry"), 0o755))
			}},
		} {
			t.Run(mutate.name, func(t *testing.T) {
				staged := t.TempDir()
				writeMarker(t, staged, "entry", "original")
				manifest, err := buildTransferManifest(context.Background(), staged)
				require.NoError(t, err)
				mutate.fn(t, staged)

				err = d.verifyRemoteTransfer(context.Background(), "user@testhost", staged, manifest)

				require.ErrorIs(t, err, ErrTransferIntegrity)
			})
		}
	})

	t.Run("rejects a changed symlink target", func(t *testing.T) {
		staged := t.TempDir()
		require.NoError(t, os.Symlink("first", filepath.Join(staged, "link")))
		manifest, err := buildTransferManifest(context.Background(), staged)
		require.NoError(t, err)
		require.NoError(t, os.Remove(filepath.Join(staged, "link")))
		require.NoError(t, os.Symlink("second", filepath.Join(staged, "link")))

		err = d.verifyRemoteTransfer(context.Background(), "user@testhost", staged, manifest)

		require.ErrorIs(t, err, ErrTransferIntegrity)
	})

	t.Run("rejects broken and unexpected hardlinks", func(t *testing.T) {
		t.Run("broken relationship", func(t *testing.T) {
			staged := t.TempDir()
			writeMarker(t, staged, "a", "same")
			require.NoError(t, os.Link(filepath.Join(staged, "a"), filepath.Join(staged, "b")))
			manifest, err := buildTransferManifest(context.Background(), staged)
			require.NoError(t, err)
			require.NoError(t, os.Remove(filepath.Join(staged, "b")))
			require.NoError(t, os.WriteFile(filepath.Join(staged, "b"), []byte("same"), 0o644))

			err = d.verifyRemoteTransfer(context.Background(), "user@testhost", staged, manifest)

			require.ErrorIs(t, err, ErrTransferIntegrity)
		})

		t.Run("unexpected relationship", func(t *testing.T) {
			staged := t.TempDir()
			writeMarker(t, staged, "a", "same")
			writeMarker(t, staged, "b", "same")
			manifest, err := buildTransferManifest(context.Background(), staged)
			require.NoError(t, err)
			require.NoError(t, os.Remove(filepath.Join(staged, "b")))
			require.NoError(t, os.Link(filepath.Join(staged, "a"), filepath.Join(staged, "b")))

			err = d.verifyRemoteTransfer(context.Background(), "user@testhost", staged, manifest)

			require.ErrorIs(t, err, ErrTransferIntegrity)
		})
	})
}

func TestCreateVerifiedTransferArchive_FailsClosed(t *testing.T) {
	original := newLocalArchiveCommand
	t.Cleanup(func() { newLocalArchiveCommand = original })
	source := t.TempDir()
	writeMarker(t, source, "expected", "content")

	t.Run("exit-zero invalid archive fails extraction", func(t *testing.T) {
		newLocalArchiveCommand = func(ctx context.Context, sourceDir, archivePath string) *exec.Cmd {
			return exec.CommandContext(ctx, "sh", "-c", `printf not-a-tar > "$1"`, "sh", archivePath)
		}

		_, _, _, cleanup, err := createVerifiedTransferArchive(context.Background(), source)
		cleanup()

		require.Error(t, err)
		assert.ErrorContains(t, err, "verify transfer archive extraction")
	})

	t.Run("command diagnostics are bounded and control-safe", func(t *testing.T) {
		newLocalArchiveCommand = func(ctx context.Context, sourceDir, archivePath string) *exec.Cmd {
			return exec.CommandContext(ctx, "sh", "-c", `printf '%s\nline-two' "$1" >&2; exit 1`, "sh", strings.Repeat("x", commandDiagnosticMax+100))
		}

		_, _, _, cleanup, err := createVerifiedTransferArchive(context.Background(), source)
		cleanup()

		require.Error(t, err)
		assert.NotContains(t, err.Error(), "\n")
		assert.Contains(t, err.Error(), "…")
	})

	t.Run("source mutation during archive creation fails the snapshot comparison", func(t *testing.T) {
		newLocalArchiveCommand = func(ctx context.Context, sourceDir, archivePath string) *exec.Cmd {
			return exec.CommandContext(ctx, "sh", "-c", `printf changed > "$1/expected"; exec tar -C "$1" -cf "$2" .`, "sh", sourceDir, archivePath)
		}

		_, _, _, cleanup, err := createVerifiedTransferArchive(context.Background(), source)
		cleanup()

		require.ErrorIs(t, err, ErrTransferIntegrity)
	})
}

func TestCreateVerifiedTransferArchive_UsesPrivateDiscoverableWorkspace(t *testing.T) {
	source := t.TempDir()
	writeMarker(t, source, "expected", "content")

	archivePath, _, _, cleanup, err := createVerifiedTransferArchive(context.Background(), source)
	require.NoError(t, err)
	workspace := filepath.Dir(archivePath)
	t.Cleanup(cleanup)

	assert.True(t, strings.HasPrefix(filepath.Base(workspace), "bosun-deploy-"))
	if runtime.GOOS != "windows" {
		workspaceInfo, statErr := os.Stat(workspace)
		require.NoError(t, statErr)
		assert.Equal(t, os.FileMode(0o700), workspaceInfo.Mode().Perm())
		archiveInfo, statErr := os.Stat(archivePath)
		require.NoError(t, statErr)
		assert.Equal(t, os.FileMode(0o600), archiveInfo.Mode().Perm())
	}

	cleanup()
	assert.NoDirExists(t, workspace)
}

func TestBoundedCommandBuffer(t *testing.T) {
	var buffer boundedCommandBuffer
	payload := strings.Repeat("x", commandOutputByteMax+100)
	n, err := buffer.Write([]byte(payload))
	require.NoError(t, err)
	assert.Equal(t, len(payload), n)
	assert.Less(t, len(buffer.String()), len(payload))
	assert.True(t, strings.HasSuffix(buffer.String(), "…"))
}

func TestDeployRemote_ExistingStageCollisionIsNeverReusedOrDeleted(t *testing.T) {
	setupSSHShim(t)
	original := newRemoteStageID
	t.Cleanup(func() { newRemoteStageID = original })
	newRemoteStageID = func() (string, error) { return "collision", nil }

	base := t.TempDir()
	source := filepath.Join(base, "source")
	parent := filepath.Join(base, "deploy")
	target := filepath.Join(parent, "compose")
	stageRoot := path.Join(parent, ".deploy-tmp-collision")
	writeMarker(t, source, "new", "new")
	writeMarker(t, target, "live", "old")
	writeMarker(t, stageRoot, "attacker-owned", "preserve")

	err := (&DeployOps{}).DeployRemote(context.Background(), source, "user@testhost", target)

	require.Error(t, err)
	assert.ErrorContains(t, err, "create remote temp dir")
	assert.Equal(t, "old", readMarker(t, target, "live"))
	assert.Equal(t, "preserve", readMarker(t, stageRoot, "attacker-owned"), "a colliding namespace was not created by this deploy and must not be reused or cleaned")
}

func TestDeployRemote_StageIdentifierFailurePreservesTarget(t *testing.T) {
	setupSSHShim(t)
	original := newRemoteStageID
	t.Cleanup(func() { newRemoteStageID = original })
	stageErr := errors.New("entropy unavailable")
	newRemoteStageID = func() (string, error) { return "", stageErr }

	base := t.TempDir()
	source := filepath.Join(base, "source")
	target := filepath.Join(base, "deploy", "compose")
	writeMarker(t, source, "new", "new")
	writeMarker(t, target, "live", "old")

	err := (&DeployOps{}).DeployRemote(context.Background(), source, "user@testhost", target)

	require.ErrorIs(t, err, stageErr)
	assert.Equal(t, "old", readMarker(t, target, "live"))
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
		"  *'cat > '*'.deploy-tmp-'*'/archive.tar'*) /bin/sh -c \"$*\"; tmp=$(echo \"$*\" | sed 's/.*tar -C \\([^ ]*\\) -xf.*/\\1/'); rm -f \"$tmp/omitted\"; exit 0 ;;\n" +
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
		writeMarker(t, source, "-leading 'quote' [glob]*", "special")
		writeMarker(t, source, ".bosun-transfer.tar", "legitimate config")
		require.NoError(t, os.Mkdir(filepath.Join(source, "empty-dir"), 0o755))
		require.NoError(t, os.Symlink("line\nname\\file", filepath.Join(source, "sym\nlink")))
		require.NoError(t, os.Symlink("cycle-b", filepath.Join(source, "cycle-a")))
		require.NoError(t, os.Symlink("cycle-a", filepath.Join(source, "cycle-b")))
		require.NoError(t, os.Symlink("../../outside", filepath.Join(source, "escape-link")))
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
		assert.Equal(t, "special", readMarker(t, target, "-leading 'quote' [glob]*"))
		escapeTarget, err := os.Readlink(filepath.Join(target, "escape-link"))
		require.NoError(t, err)
		assert.Equal(t, "../../outside", escapeTarget)
		assert.Equal(t, "legitimate config", readMarker(t, target, ".bosun-transfer.tar"))
	})
}

func TestDeployRemote_IntegrityCapabilityMissingFailsClosed(t *testing.T) {
	counter := filepath.Join(t.TempDir(), "attempts")
	dir := t.TempDir()
	shim := "#!/bin/sh\nwhile [ $# -gt 0 ]; do case \"$1\" in -o) shift 2 ;; -*) shift ;; *) break ;; esac; done\nshift\ncase \"$*\" in *'command -v sha256sum'*) echo x >> " + counter + "; exit 78 ;; esac\nexec /bin/sh -c \"$*\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ssh"), []byte(shim), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	base := t.TempDir()
	source := filepath.Join(base, "source")
	target := filepath.Join(base, "deploy", "compose")
	writeMarker(t, source, "new", "new")
	writeMarker(t, target, "live", "old")
	err := (&DeployOps{}).DeployRemote(context.Background(), source, "user@testhost", target)
	require.ErrorIs(t, err, ErrRemoteIntegrityUnavailable)
	assert.NotErrorIs(t, err, ErrTransferIntegrity)
	attempts, readErr := os.ReadFile(counter)
	require.NoError(t, readErr)
	assert.Equal(t, 1, strings.Count(string(attempts), "x"), "a missing remote capability is not retryable")
	assert.Equal(t, "old", readMarker(t, target, "live"))
}
