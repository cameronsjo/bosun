package reconcile

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests exercise the #437 quoting of the raw-argv ssh sites (mkdir, mv,
// rm) by driving real remote commands through paths containing spaces and shell
// metacharacters. Without shellquote.Join the remote shell would split or
// evaluate those paths instead of passing each one as a single argument.

func TestEnsureRemoteDir_QuotesSpacedPath(t *testing.T) {
	setupSSHShim(t)
	base := t.TempDir()
	dir := filepath.Join(base, "app data", "nested config")

	d := &DeployOps{}
	require.NoError(t, d.EnsureRemoteDir(context.Background(), "user@testhost", dir))
	assert.DirExists(t, dir, "mkdir -p must receive the spaced path as a single argument")
}

func TestEnsureRemoteDir_QuotesShellMetacharacters(t *testing.T) {
	setupSSHShim(t)
	base := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(base, "config-prod"), 0o755))
	cases := []struct {
		name string
		path string
	}{
		{name: "semicolon", path: "config; false"},
		{name: "command substitution", path: "config-$(false)"},
		{name: "single quote", path: "config-'quoted'"},
		{name: "glob", path: "config-[prod]*"},
		{name: "ampersand", path: "config-& false"},
	}

	d := &DeployOps{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(base, tc.path)
			require.NoError(t, d.EnsureRemoteDir(context.Background(), "user@testhost", dir))
			assert.DirExists(t, dir, "mkdir -p must treat shell metacharacters as literal path bytes")
		})
	}
}

// The DeployRemoteFile tests use setupSSHShim alone: the single-file stream
// runs through `/bin/sh -c`, so remote shell evaluation of every path is
// modeled, which the old local-copy scp shim never did.

func TestDeployRemoteFile_QuotesSpacedPath(t *testing.T) {
	setupSSHShim(t)
	callLog := setupSCPTripwire(t)
	base := t.TempDir()
	source := filepath.Join(base, "config.yml")
	require.NoError(t, os.WriteFile(source, []byte("v1"), 0o644))
	target := filepath.Join(base, "remote dir", "config.yml")

	d := &DeployOps{}
	require.NoError(t, d.DeployRemoteFile(context.Background(), source, "user@testhost", target))

	got, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "v1", string(got), "the stream must target the spaced path as one argument")
	assertSCPNeverInvoked(t, callLog)
}

func TestDeployRemoteFile_QuotesShellMetacharacters(t *testing.T) {
	canary := filepath.Join(t.TempDir(), "canary")
	cases := []struct {
		name string
		file string
	}{
		{name: "mixed metacharacters", file: "config; false-$(false)-'quoted'-[prod]*.yml"},
		{name: "command substitution canary", file: `config-$(touch "$BOSUN_CANARY").yml`},
		{name: "backtick canary", file: "config-`touch \"$BOSUN_CANARY\"`.yml"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupSSHShim(t)
			callLog := setupSCPTripwire(t)
			t.Setenv("BOSUN_CANARY", canary)
			base := t.TempDir()
			source := filepath.Join(base, "config.yml")
			require.NoError(t, os.WriteFile(source, []byte("v1"), 0o644))
			target := filepath.Join(base, "remote", tc.file)

			d := &DeployOps{}
			require.NoError(t, d.DeployRemoteFile(context.Background(), source, "user@testhost", target))

			got, err := os.ReadFile(target)
			require.NoError(t, err)
			assert.Equal(t, "v1", string(got), "the stream must treat shell metacharacters as literal path bytes")
			assert.NoFileExists(t, canary, "no part of the remote path may be evaluated by the remote shell")
			assertSCPNeverInvoked(t, callLog)
		})
	}
}

func TestCleanupRemotePath_QuotesShellMetacharacters(t *testing.T) {
	setupSSHShim(t)
	target := filepath.Join(t.TempDir(), "staging; false-$(false)-'quoted'-[prod]*")
	require.NoError(t, os.WriteFile(target, []byte("partial"), 0o644))

	cleanupRemotePath(context.Background(), "user@testhost", target, "test", false)

	assert.NoFileExists(t, target, "rm must treat shell metacharacters as literal path bytes")
}

func TestDeployRemote_QuotesSpacedTarget(t *testing.T) {
	setupSSHShim(t)
	base := t.TempDir()
	source := filepath.Join(base, "source")
	target := filepath.Join(base, "deploy dir", "[prod]* compose")
	writeMarker(t, source, "core.yml", "v1")

	d := &DeployOps{}
	require.NoError(t, d.DeployRemote(context.Background(), source, "user@testhost", target))
	assert.Equal(t, "v1", readMarker(t, target, "core.yml"), "staging mkdir and swap must handle spaced paths")
}
