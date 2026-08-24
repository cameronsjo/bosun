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
// rm) by driving real remote commands through paths containing spaces. Without
// shellquote.Join the remote shell would word-split the path and the command
// would target the wrong file, so a spaced path succeeding is the proof.

func TestEnsureRemoteDir_QuotesSpacedPath(t *testing.T) {
	setupSSHShim(t)
	base := t.TempDir()
	dir := filepath.Join(base, "app data", "nested config")

	d := &DeployOps{}
	require.NoError(t, d.EnsureRemoteDir(context.Background(), "user@testhost", dir))
	assert.DirExists(t, dir, "mkdir -p must receive the spaced path as a single argument")
}

func TestDeployRemoteFile_QuotesSpacedPath(t *testing.T) {
	setupSSHShim(t)
	setupSCPShim(t)
	base := t.TempDir()
	source := filepath.Join(base, "config.yml")
	require.NoError(t, os.WriteFile(source, []byte("v1"), 0o644))
	target := filepath.Join(base, "remote dir", "config.yml")

	d := &DeployOps{}
	require.NoError(t, d.DeployRemoteFile(context.Background(), source, "user@testhost", target))

	got, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "v1", string(got), "the atomic mv must target the spaced path as one argument")
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
