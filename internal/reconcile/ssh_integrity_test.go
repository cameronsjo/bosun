package reconcile

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupSha256sumShim makes `sha256sum` resolvable for the SSH shim's local
// execution. On Linux CI the real binary exists and this is a no-op; on macOS
// (no sha256sum) it installs a shim delegating to `shasum -a 256`, which shares
// the "<hex>  <path>" manifest format and the `-c -` stdin-checklist behavior.
func setupSha256sumShim(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("sha256sum"); err == nil {
		return
	}
	shasum, err := exec.LookPath("shasum")
	require.NoError(t, err, "need sha256sum or shasum on PATH to verify transfer integrity")
	dir := t.TempDir()
	shim := "#!/bin/sh\nexec " + shasum + " -a 256 \"$@\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sha256sum"), []byte(shim), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestBuildTransferChecksums(t *testing.T) {
	t.Run("deterministic sorted manifest in sha256sum format", func(t *testing.T) {
		root := t.TempDir()
		writeMarker(t, root, "b.txt", "beta")
		writeMarker(t, filepath.Join(root, "sub"), "a.txt", "alpha")

		first, err := buildTransferChecksums(root)
		require.NoError(t, err)
		second, err := buildTransferChecksums(root)
		require.NoError(t, err)
		assert.Equal(t, first, second, "manifest must be deterministic")

		lines := strings.Split(strings.TrimRight(first, "\n"), "\n")
		require.Len(t, lines, 2)
		assert.True(t, sortedAscending(lines), "manifest lines must be sorted for determinism")

		// Each line is "<64 hex>  <relpath>" and the hash matches the content.
		betaSum := sha256.Sum256([]byte("beta"))
		assert.Contains(t, first, hex.EncodeToString(betaSum[:])+"  b.txt")
		alphaSum := sha256.Sum256([]byte("alpha"))
		assert.Contains(t, first, hex.EncodeToString(alphaSum[:])+"  sub/a.txt")
		for _, ln := range lines {
			parts := strings.SplitN(ln, "  ", 2)
			require.Len(t, parts, 2, "line must be '<hex>  <path>': %q", ln)
			assert.Len(t, parts[0], 64, "sha-256 hex is 64 chars")
		}
	})

	t.Run("empty directory yields an empty manifest", func(t *testing.T) {
		manifest, err := buildTransferChecksums(t.TempDir())
		require.NoError(t, err)
		assert.Equal(t, "", manifest)
	})

	t.Run("rejects a filename containing a newline", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, "a\nb"), []byte("x"), 0o644))
		_, err := buildTransferChecksums(root)
		require.ErrorIs(t, err, ErrUnsafeChecksumPath)
	})

	t.Run("rejects a filename containing a backslash", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, "a\\b"), []byte("x"), 0o644))
		_, err := buildTransferChecksums(root)
		require.ErrorIs(t, err, ErrUnsafeChecksumPath)
	})

	t.Run("skips symlinks and irregular entries", func(t *testing.T) {
		root := t.TempDir()
		writeMarker(t, root, "real.txt", "real")
		require.NoError(t, os.Symlink(filepath.Join(root, "real.txt"), filepath.Join(root, "link.txt")))
		manifest, err := buildTransferChecksums(root)
		require.NoError(t, err)
		assert.Contains(t, manifest, "real.txt")
		assert.NotContains(t, manifest, "link.txt", "symlinks are not part of the deployed tar tree")
	})
}

func sortedAscending(s []string) bool {
	for i := 1; i < len(s); i++ {
		if s[i-1] > s[i] {
			return false
		}
	}
	return true
}

func TestRemoteHasSha256sum(t *testing.T) {
	t.Run("true when the remote has sha256sum", func(t *testing.T) {
		setupSSHShim(t)
		setupSha256sumShim(t)
		d := &DeployOps{}
		assert.True(t, d.remoteHasSha256sum(context.Background(), "user@testhost"))
	})

	t.Run("false for an option-injection host before any ssh exec", func(t *testing.T) {
		// No ssh shim: if the probe validated late it would exec ssh with the
		// poisoned option. validateHost must short-circuit to false first.
		d := &DeployOps{}
		assert.False(t, d.remoteHasSha256sum(context.Background(), "-oProxyCommand=touch /tmp/pwned"))
	})

	t.Run("false when command -v sha256sum fails on the remote", func(t *testing.T) {
		// SSH shim that reports sha256sum absent, passing every other command through.
		dir := t.TempDir()
		shim := "#!/bin/sh\n" +
			"while [ $# -gt 0 ]; do\n" +
			"  case \"$1\" in\n" +
			"    -o) shift 2 ;;\n" +
			"    -*) shift ;;\n" +
			"    *) break ;;\n" +
			"  esac\n" +
			"done\n" +
			"shift\n" +
			"case \"$*\" in\n" +
			"  *sha256sum*) exit 1 ;;\n" +
			"esac\n" +
			"exec /bin/sh -c \"$*\"\n"
		require.NoError(t, os.WriteFile(filepath.Join(dir, "ssh"), []byte(shim), 0o755))
		t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

		d := &DeployOps{}
		assert.False(t, d.remoteHasSha256sum(context.Background(), "user@testhost"))
	})
}

func TestVerifyRemoteTransfer(t *testing.T) {
	setupSSHShim(t)
	setupSha256sumShim(t)
	d := &DeployOps{}

	t.Run("passes when the staged tree matches the manifest", func(t *testing.T) {
		staged := t.TempDir()
		writeMarker(t, staged, "a.txt", "content-a")
		writeMarker(t, filepath.Join(staged, "sub"), "b.txt", "content-b")
		manifest, err := buildTransferChecksums(staged)
		require.NoError(t, err)

		require.NoError(t, d.verifyRemoteTransfer(context.Background(), "user@testhost", staged, manifest))
	})

	t.Run("fails with ErrTransferIntegrity on a content mismatch", func(t *testing.T) {
		source := t.TempDir()
		writeMarker(t, source, "a.txt", "the-real-content")
		manifest, err := buildTransferChecksums(source)
		require.NoError(t, err)

		// Staged copy has the same path but DIFFERENT bytes (a misdirected transfer).
		staged := t.TempDir()
		writeMarker(t, staged, "a.txt", "corrupted")

		err = d.verifyRemoteTransfer(context.Background(), "user@testhost", staged, manifest)
		require.ErrorIs(t, err, ErrTransferIntegrity)
	})

	t.Run("fails with ErrTransferIntegrity when a file is missing", func(t *testing.T) {
		source := t.TempDir()
		writeMarker(t, source, "a.txt", "present")
		writeMarker(t, source, "b.txt", "also-present")
		manifest, err := buildTransferChecksums(source)
		require.NoError(t, err)

		// Staged copy is truncated: b.txt never landed.
		staged := t.TempDir()
		writeMarker(t, staged, "a.txt", "present")

		err = d.verifyRemoteTransfer(context.Background(), "user@testhost", staged, manifest)
		require.ErrorIs(t, err, ErrTransferIntegrity)
	})
}

func TestDeployRemote_IntegrityVerification(t *testing.T) {
	t.Run("verified transfer promotes the staged tree", func(t *testing.T) {
		setupSSHShim(t)
		setupSha256sumShim(t)
		base := t.TempDir()
		source := filepath.Join(base, "source")
		target := filepath.Join(base, "deploy", "compose")
		writeMarker(t, source, "core.yml", "services: {}")
		writeMarker(t, filepath.Join(source, "nested"), "extra.yml", "more")

		d := &DeployOps{}
		require.NoError(t, d.DeployRemote(context.Background(), source, "user@testhost", target, true))
		assert.Equal(t, "services: {}", readMarker(t, target, "core.yml"))
		assert.Equal(t, "more", readMarker(t, filepath.Join(target, "nested"), "extra.yml"))
	})

	t.Run("empty source skips verification and still deploys", func(t *testing.T) {
		setupSSHShim(t)
		setupSha256sumShim(t)
		base := t.TempDir()
		source := filepath.Join(base, "source")
		require.NoError(t, os.MkdirAll(source, 0o755)) // no regular files -> empty manifest
		target := filepath.Join(base, "deploy", "compose")

		d := &DeployOps{}
		require.NoError(t, d.DeployRemote(context.Background(), source, "user@testhost", target, true))
		assert.DirExists(t, target)
	})

	t.Run("corrupted transfer fails integrity, retries with a fresh tmpDir, and preserves the live target", func(t *testing.T) {
		setupSha256sumShim(t)
		base := t.TempDir()
		source := filepath.Join(base, "source")
		parent := filepath.Join(base, "deploy")
		target := filepath.Join(parent, "compose")
		writeMarker(t, source, "core.yml", "the-good-config")
		writeMarker(t, target, "core.yml", "live-config") // must survive

		// A counter proves the corruption ran once per attempt (fresh tmpDir each).
		counter := filepath.Join(base, "extract-count")

		// SSH shim that corrupts the staged tree right after each tar extraction,
		// so `sha256sum -c` always fails. Every other remote command passes through.
		dir := t.TempDir()
		shim := "#!/bin/sh\n" +
			"while [ $# -gt 0 ]; do\n" +
			"  case \"$1\" in\n" +
			"    -o) shift 2 ;;\n" +
			"    -*) shift ;;\n" +
			"    *) break ;;\n" +
			"  esac\n" +
			"done\n" +
			"shift\n" +
			"case \"$*\" in\n" +
			"  *'-xf -'*)\n" +
			"    /bin/sh -c \"$*\"\n" +
			"    tmp=$(echo \"$*\" | sed 's/.*-C \\([^ ]*\\) -xf.*/\\1/')\n" +
			"    find \"$tmp\" -type f -exec sh -c 'echo tampered >> \"$1\"' _ {} \\;\n" +
			"    echo x >> " + counter + "\n" +
			"    ;;\n" +
			"  *) exec /bin/sh -c \"$*\" ;;\n" +
			"esac\n"
		require.NoError(t, os.WriteFile(filepath.Join(dir, "ssh"), []byte(shim), 0o755))
		t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

		d := &DeployOps{}
		err := d.DeployRemote(context.Background(), source, "user@testhost", target, true)
		require.ErrorIs(t, err, ErrTransferIntegrity)

		// Live target untouched — a failed integrity check never swaps.
		assert.Equal(t, "live-config", readMarker(t, target, "core.yml"))

		// One extract+corrupt per attempt: proves retry ran with a fresh tmpDir
		// each pass (DefaultMaxRetries attempts).
		countBytes, readErr := os.ReadFile(counter)
		require.NoError(t, readErr)
		assert.Equal(t, DefaultMaxRetries, strings.Count(string(countBytes), "x"))

		// No staging temp dir leaked: each failed attempt cleaned up after itself.
		entries, readErr := os.ReadDir(parent)
		require.NoError(t, readErr)
		require.Len(t, entries, 1)
		assert.Equal(t, "compose", entries[0].Name())
	})

	t.Run("an unsafe source filename hard-fails before any transfer", func(t *testing.T) {
		setupSSHShim(t)
		setupSha256sumShim(t)
		base := t.TempDir()
		source := filepath.Join(base, "source")
		require.NoError(t, os.MkdirAll(source, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(source, "a\nb.yml"), []byte("x"), 0o644))
		target := filepath.Join(base, "deploy", "compose")

		d := &DeployOps{}
		err := d.DeployRemote(context.Background(), source, "user@testhost", target, true)
		require.ErrorIs(t, err, ErrUnsafeChecksumPath)
		assert.Contains(t, err.Error(), "build transfer checksums")
		assert.NoDirExists(t, target, "the deploy must abort before swapping anything into the live target")
	})

	t.Run("verifyChecksums=false skips verification entirely (degrade path)", func(t *testing.T) {
		setupSSHShim(t) // NO sha256sum shim installed
		base := t.TempDir()
		source := filepath.Join(base, "source")
		target := filepath.Join(base, "deploy", "compose")
		writeMarker(t, source, "core.yml", "v1")

		d := &DeployOps{}
		// With verification off, sha256sum is never invoked, so the deploy
		// succeeds even on a host without it.
		require.NoError(t, d.DeployRemote(context.Background(), source, "user@testhost", target, false))
		assert.Equal(t, "v1", readMarker(t, target, "core.yml"))
	})
}
