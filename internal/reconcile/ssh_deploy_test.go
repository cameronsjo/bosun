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
// command locally (`ssh <host> <cmd...>` → `/bin/sh -c "<cmd...>"`), so
// DeployRemote's full pipeline — crash recovery, staging mkdir, tar-over-ssh
// extraction, and the retain-old swap — runs end-to-end against real local
// directories with no network. stdin flows through, so the tar pipe works.
// The shell interpreter mirrors real ssh semantics (the remote shell parses
// the joined argv); every path involved comes from t.TempDir().
func setupSSHShim(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	shim := "#!/bin/sh\nshift\nexec /bin/sh -c \"$*\"\n"
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
