package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cameronsjo/bosun/internal/reconcile"
)

func TestRunFullDryRunWithConfig_PreservesConfiguredState(t *testing.T) {
	repoDir := newFullDryRunTestRepo(t)
	baseDir := t.TempDir()
	stateDir := filepath.Join(baseDir, "state")
	stateFile := filepath.Join(stateDir, reconcile.DefaultStateFile)
	require.NoError(t, os.MkdirAll(stateDir, 0o755))
	seed := []byte("{\n  \"last_deployed_commit\": \"previous-commit\",\n  \"attempt_count\": 2\n}\n")
	require.NoError(t, os.WriteFile(stateFile, seed, 0o600))

	cfg := fullDryRunTestConfig(t, repoDir, stateFile)
	scratchRoot := t.TempDir()
	t.Setenv("TMPDIR", scratchRoot)
	require.NoError(t, runFullDryRunWithConfig(cfg))

	assert.FileExists(t, filepath.Join(cfg.StagingDir, "compose", "docker-compose.yml"),
		"successful full validation must render the local repository")
	matches, err := filepath.Glob(filepath.Join(scratchRoot, "bosun-reconcile-dry-run-state-*"))
	require.NoError(t, err)
	assert.Empty(t, matches, "successful full validation must remove its scratch state directory")
	got, err := os.ReadFile(stateFile)
	require.NoError(t, err)
	assert.Equal(t, seed, got, "full validation must not mutate the configured deploy state")
}

func TestRunFullDryRunWithConfig_CleansScratchStateOnFailure(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("TMPDIR", tmpDir)
	baseDir := t.TempDir()
	stateDir := filepath.Join(baseDir, "state")
	stateFile := filepath.Join(stateDir, reconcile.DefaultStateFile)
	require.NoError(t, os.MkdirAll(stateDir, 0o755))
	seed := []byte("{\"last_deployed_commit\":\"previous-commit\",\"attempt_count\":2}\n")
	require.NoError(t, os.WriteFile(stateFile, seed, 0o600))

	cfg := fullDryRunTestConfig(t, filepath.Join(baseDir, "missing-repo"), stateFile)
	err := runFullDryRunWithConfig(cfg)
	require.Error(t, err)

	matches, err := filepath.Glob(filepath.Join(tmpDir, "bosun-reconcile-dry-run-state-*"))
	require.NoError(t, err)
	assert.Empty(t, matches, "failed full validation must remove its scratch state directory")
	got, err := os.ReadFile(stateFile)
	require.NoError(t, err)
	assert.Equal(t, seed, got, "failed full validation must not mutate the configured deploy state")
}

func TestRunFullDryRunWithConfig_ReportsScratchStateCreationFailure(t *testing.T) {
	baseDir := t.TempDir()
	stateDir := filepath.Join(baseDir, "state")
	stateFile := filepath.Join(stateDir, reconcile.DefaultStateFile)
	require.NoError(t, os.MkdirAll(stateDir, 0o755))
	seed := []byte("{\"last_deployed_commit\":\"previous-commit\",\"attempt_count\":2}\n")
	require.NoError(t, os.WriteFile(stateFile, seed, 0o600))

	tmpFile := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(tmpFile, []byte("occupied"), 0o600))
	cfg := fullDryRunTestConfig(t, filepath.Join(baseDir, "missing-repo"), stateFile)
	t.Setenv("TMPDIR", tmpFile)

	err := runFullDryRunWithConfig(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create temporary dry-run state directory")
	assert.NoDirExists(t, cfg.RepoDir, "state preparation must fail before Git creates a clone")
	got, readErr := os.ReadFile(stateFile)
	require.NoError(t, readErr)
	assert.Equal(t, seed, got, "failed scratch setup must not mutate the configured deploy state")
}

func fullDryRunTestConfig(t *testing.T, repoDir, stateFile string) *reconcile.Config {
	t.Helper()
	baseDir := t.TempDir()
	appdataDir := filepath.Join(baseDir, "appdata")
	require.NoError(t, os.MkdirAll(appdataDir, 0o755))

	cfg := reconcile.DefaultConfig()
	cfg.RepoURL = repoDir
	cfg.RepoBranch = "master"
	cfg.RepoDir = filepath.Join(baseDir, "clone")
	cfg.StagingDir = filepath.Join(baseDir, "staging")
	cfg.BackupDir = filepath.Join(baseDir, "backups")
	cfg.LogDir = filepath.Join(baseDir, "logs")
	// Keep the final lock directory private; Unix lock acquisition requires
	// a daemon-owned 0700 directory.
	cfg.LockFile = filepath.Join(baseDir, "lock", "reconcile.lock")
	cfg.StateFile = stateFile
	cfg.LocalAppdataPath = appdataDir
	cfg.DeployMode = "local"
	cfg.DryRun = true
	cfg.OnFailure = false
	cfg.OnRecovery = false
	return cfg
}

func newFullDryRunTestRepo(t *testing.T) string {
	t.Helper()
	repoDir := filepath.Join(t.TempDir(), "repo")
	repo, err := git.PlainInit(repoDir, false)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(repoDir, "compose"), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(repoDir, "compose", "docker-compose.yml"),
		[]byte("services:\n  hello:\n    image: alpine:3\n"),
		0o644,
	))

	worktree, err := repo.Worktree()
	require.NoError(t, err)
	_, err = worktree.Add("compose/docker-compose.yml")
	require.NoError(t, err)
	_, err = worktree.Commit("initial compose", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Bosun Test",
			Email: "bosun-test@example.com",
			When:  time.Now(),
		},
	})
	require.NoError(t, err)
	return repoDir
}
