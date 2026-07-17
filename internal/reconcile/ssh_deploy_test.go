package reconcile

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupSSHShim installs a fake `ssh` ahead of PATH that executes the remote
// command locally (`ssh [-o opt]... <host> <cmd...>` → `/bin/sh -c "<cmd...>"`),
// so DeployRemote's full pipeline — crash recovery, staging mkdir, tar-over-ssh
// extraction, and the retain-old swap — runs end-to-end against real local
// directories with no network. stdin flows through, so the tar pipe works.
// The shim skips the leading `-o KEY=VAL` host-key policy flags (and any other
// dash-prefixed option) before dropping the host, mirroring real ssh argv
// parsing; every path involved comes from t.TempDir().
func setupSSHShim(t *testing.T) {
	t.Helper()
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
		"exec /bin/sh -c \"$*\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ssh"), []byte(shim), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestDeployRemote_EndToEnd(t *testing.T) {
	t.Run("first deploy creates the target from the staged tree", func(t *testing.T) {
		setupSSHShim(t)
		base := t.TempDir()
		source := filepath.Join(base, "source")
		target := filepath.Join(base, "deploy", "compose")
		writeMarker(t, source, "marker", "v1")

		d := &DeployOps{}
		require.NoError(t, d.DeployRemote(context.Background(), source, "user@testhost", target))

		assert.Equal(t, "v1", readMarker(t, target, "marker"))
	})

	t.Run("redeploy replaces the target and leaves no residue", func(t *testing.T) {
		setupSSHShim(t)
		base := t.TempDir()
		source := filepath.Join(base, "source")
		parent := filepath.Join(base, "deploy")
		target := filepath.Join(parent, "compose")
		writeMarker(t, source, "marker", "v2")
		writeMarker(t, target, "marker", "v1")

		d := &DeployOps{}
		require.NoError(t, d.DeployRemote(context.Background(), source, "user@testhost", target))

		assert.Equal(t, "v2", readMarker(t, target, "marker"))
		entries, err := os.ReadDir(parent)
		require.NoError(t, err)
		require.Len(t, entries, 1, "no retained copies or staging temp dirs may survive a successful deploy")
		assert.Equal(t, "compose", entries[0].Name())
	})

	t.Run("interrupted prior swap self-heals then deploys", func(t *testing.T) {
		setupSSHShim(t)
		base := t.TempDir()
		source := filepath.Join(base, "source")
		parent := filepath.Join(base, "deploy")
		target := filepath.Join(parent, "compose")
		writeMarker(t, source, "marker", "new")
		// Simulate a crash mid-swap: target gone, retained copy stranded.
		writeMarker(t, target+bosunOldSuffix+"1111111111111111111", "marker", "stranded")

		d := &DeployOps{}
		require.NoError(t, d.DeployRemote(context.Background(), source, "user@testhost", target))

		assert.Equal(t, "new", readMarker(t, target, "marker"))
		entries, err := os.ReadDir(parent)
		require.NoError(t, err)
		require.Len(t, entries, 1, "the stranded retained copy must be promoted then replaced, not leaked")
	})

	t.Run("tar failure cleans the staging temp dir and preserves the target", func(t *testing.T) {
		setupSSHShim(t)
		base := t.TempDir()
		parent := filepath.Join(base, "deploy")
		target := filepath.Join(parent, "compose")
		writeMarker(t, target, "marker", "live")
		missingSource := filepath.Join(base, "does-not-exist")

		d := &DeployOps{}
		err := d.DeployRemote(context.Background(), missingSource, "user@testhost", target)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "tar")
		assert.Equal(t, "live", readMarker(t, target, "marker"), "a failed staging transfer must not touch the live target")
		entries, readErr := os.ReadDir(parent)
		require.NoError(t, readErr)
		require.Len(t, entries, 1, "staging temp dir must be cleaned up on failure")
	})

	t.Run("dry run performs no remote work", func(t *testing.T) {
		// No shim on purpose: any ssh invocation would fail loudly.
		base := t.TempDir()
		target := filepath.Join(base, "deploy", "compose")

		d := &DeployOps{DryRun: true}
		require.NoError(t, d.DeployRemote(context.Background(), filepath.Join(base, "source"), "user@testhost", target))
		assert.NoDirExists(t, target)
	})

	t.Run("invalid host is rejected before any work", func(t *testing.T) {
		d := &DeployOps{}
		err := d.DeployRemote(context.Background(), t.TempDir(), "-oProxyCommand=evil", "/tmp/x")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid SSH host")
	})
}

func TestEnsureRemoteDir_EndToEnd(t *testing.T) {
	setupSSHShim(t)
	dir := filepath.Join(t.TempDir(), "nested", "path")

	d := &DeployOps{}
	require.NoError(t, d.EnsureRemoteDir(context.Background(), "user@testhost", dir))
	assert.DirExists(t, dir)
}

// setupSCPShim installs a fake `scp` ahead of PATH that performs a local copy
// (`scp [-o opt]... -q <src> <host:dst>` → `cp <src> <dst>`), skipping the
// leading host-key policy flags and stripping the `host:` prefix. Pairs with
// setupSSHShim so DeployRemoteFile's scp-then-ssh-mv flow runs locally.
func setupSCPShim(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	shim := "#!/bin/sh\n" +
		"while [ $# -gt 0 ]; do\n" +
		"  case \"$1\" in\n" +
		"    -o) shift 2 ;;\n" +
		"    -*) shift ;;\n" +
		"    *) break ;;\n" +
		"  esac\n" +
		"done\n" +
		"src=\"$1\"\n" +
		"dst=\"${2#*:}\"\n" +
		"exec cp \"$src\" \"$dst\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "scp"), []byte(shim), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestDeployRemoteFile_EndToEnd(t *testing.T) {
	setupSSHShim(t)
	setupSCPShim(t)
	base := t.TempDir()
	source := filepath.Join(base, "config.yml")
	require.NoError(t, os.WriteFile(source, []byte("v1"), 0o644))
	target := filepath.Join(base, "remote", "config.yml")

	d := &DeployOps{}
	require.NoError(t, d.DeployRemoteFile(context.Background(), source, "user@testhost", target))

	got, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "v1", string(got))
}

func TestCheckSSHConnectivity_EndToEnd(t *testing.T) {
	setupSSHShim(t)

	d := &DeployOps{}
	require.NoError(t, d.CheckSSHConnectivity(context.Background(), "user@testhost"))
}

// TestDeployRemoteFile_ScpFailureCleansTemp drives the scp-failure branch: a
// failing scp must clean up the remote temp file and surface the error. Covers
// the on-failure `ssh rm -f <tmp>` cleanup path.
func TestDeployRemoteFile_ScpFailureCleansTemp(t *testing.T) {
	setupSSHShim(t) // mkdir + cleanup rm succeed
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "scp"), []byte("#!/bin/sh\nexit 1\n"), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	base := t.TempDir()
	source := filepath.Join(base, "config.yml")
	require.NoError(t, os.WriteFile(source, []byte("v1"), 0o644))
	target := filepath.Join(base, "remote", "config.yml")

	d := &DeployOps{}
	err := d.DeployRemoteFile(context.Background(), source, "user@testhost", target)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scp failed")
}

// TestDeployRemoteFile_MoveFailureCleansTemp drives the atomic-move failure
// branch: scp lands the temp file, but the remote `mv` fails, so the temp file
// must be cleaned up and the error surfaced. Covers the post-move `ssh rm -f`.
func TestDeployRemoteFile_MoveFailureCleansTemp(t *testing.T) {
	setupSCPShim(t) // scp copies successfully
	// ssh shim that fails only when the remote command is `mv ...`.
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
		"  mv*) exit 1 ;;\n" +
		"esac\n" +
		"exec /bin/sh -c \"$*\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ssh"), []byte(shim), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	base := t.TempDir()
	source := filepath.Join(base, "config.yml")
	require.NoError(t, os.WriteFile(source, []byte("v1"), 0o644))
	target := filepath.Join(base, "remote", "config.yml")

	d := &DeployOps{}
	err := d.DeployRemoteFile(context.Background(), source, "user@testhost", target)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "atomic move failed")
}
