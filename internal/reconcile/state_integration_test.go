package reconcile

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockGit is a test GitOperations that returns controlled values.
type mockGit struct {
	changed bool
	before  string
	after   string
	err     error
}

func (m *mockGit) Sync(_ context.Context) (bool, string, string, error) {
	return m.changed, m.before, m.after, m.err
}

func (m *mockGit) IsRepo(_ context.Context) bool { return true }

// mockSOPS is a test SecretsDecryptor that returns empty secrets.
type mockSOPS struct{ err error }

func (m *mockSOPS) DecryptFiles(_ context.Context, _ []string) (map[string]any, error) {
	return map[string]any{}, m.err
}
func (m *mockSOPS) CheckAgeKey() error { return nil }

// newTestReconciler creates a Reconciler wired with mocks and temp directories.
// Uses local mode with DryRun so the pipeline completes without actual deploys.
func newTestReconciler(t *testing.T, git *mockGit) (*Reconciler, string) {
	t.Helper()
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "deploy-state.json")
	lockFile := filepath.Join(dir, "reconcile.lock")
	stagingDir := filepath.Join(dir, "staging")
	repoDir := filepath.Join(dir, "repo")
	appdataDir := filepath.Join(dir, "appdata")

	// Create minimal directories.
	require.NoError(t, os.MkdirAll(repoDir, 0755))
	require.NoError(t, os.MkdirAll(appdataDir, 0755))

	cfg := &Config{
		RepoDir:          repoDir,
		StagingDir:       stagingDir,
		BackupDir:        filepath.Join(dir, "backups"),
		LogDir:           filepath.Join(dir, "logs"),
		LockFile:         lockFile,
		StateFile:        stateFile,
		LocalAppdataPath: appdataDir, // Existing dir triggers local mode
		InfraSubDir:      ".",
		DryRun:           true, // Prevents actual file deployment
	}

	r := NewReconciler(cfg,
		WithGitOperations(git),
		WithSecretsDecryptor(&mockSOPS{}),
	)

	return r, stateFile
}

func TestRun_InterruptedDeploy_RerunsOnNextTrigger(t *testing.T) {
	// Simulate: first run pulled commit-A but was interrupted (state file absent).
	// Second run: git shows no changes (already at commit-A).
	// Expected: pipeline runs because state file doesn't record commit-A as deployed.

	git := &mockGit{changed: false, before: "commit-A", after: "commit-A"}
	r, stateFile := newTestReconciler(t, git)

	// No state file exists (simulating interrupted first run).
	assert.NoFileExists(t, stateFile)

	// Run should proceed (dry run, so it won't fail on deploy).
	err := r.Run(context.Background())
	require.NoError(t, err)

	// State file should now record commit-A as deployed.
	state := LoadState(stateFile)
	assert.Equal(t, "commit-A", state.LastDeployedCommit)
}

func TestRun_SuccessfulDeploy_SkipsNextRun(t *testing.T) {
	git := &mockGit{changed: false, before: "commit-A", after: "commit-A"}
	r, stateFile := newTestReconciler(t, git)

	// First run succeeds.
	err := r.Run(context.Background())
	require.NoError(t, err)

	// Verify state was written.
	state := LoadState(stateFile)
	assert.Equal(t, "commit-A", state.LastDeployedCommit)

	// Second run with same commit should skip.
	err = r.Run(context.Background())
	require.NoError(t, err)

	// State should remain unchanged.
	state = LoadState(stateFile)
	assert.Equal(t, "commit-A", state.LastDeployedCommit)
}

func TestRun_ForceFlag_OverridesStateMatch(t *testing.T) {
	git := &mockGit{changed: false, before: "commit-A", after: "commit-A"}
	r, stateFile := newTestReconciler(t, git)

	// Write existing state matching current commit.
	require.NoError(t, SaveState(stateFile, &DeployState{
		LastDeployedCommit: "commit-A",
	}))

	// With force=true, should still run.
	r.config.Force = true
	err := r.Run(context.Background())
	require.NoError(t, err)
}

func TestRun_CircuitBreaker_TripsAfterMaxAttempts(t *testing.T) {
	// Simulate a commit that always fails decryption.
	git := &mockGit{changed: false, before: "bad-commit", after: "bad-commit"}

	dir := t.TempDir()
	stateFile := filepath.Join(dir, "deploy-state.json")

	// Pre-seed state with MaxAttempts failures on this commit.
	require.NoError(t, SaveState(stateFile, &DeployState{
		LastAttemptedCommit: "bad-commit",
		AttemptCount:        MaxAttempts,
	}))

	cfg := &Config{
		RepoDir:          filepath.Join(dir, "repo"),
		StagingDir:       filepath.Join(dir, "staging"),
		BackupDir:        filepath.Join(dir, "backups"),
		LogDir:           filepath.Join(dir, "logs"),
		LockFile:         filepath.Join(dir, "reconcile.lock"),
		StateFile:        stateFile,
		LocalAppdataPath: filepath.Join(dir, "appdata"),
		InfraSubDir:      ".",
	}
	require.NoError(t, os.MkdirAll(cfg.RepoDir, 0755))
	require.NoError(t, os.MkdirAll(cfg.LocalAppdataPath, 0755))

	r := NewReconciler(cfg,
		WithGitOperations(git),
		WithSecretsDecryptor(&mockSOPS{}),
	)

	// Should fail with circuit breaker error.
	err := r.Run(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circuit breaker")
}

func TestRun_ForceFlag_OverridesCircuitBreaker(t *testing.T) {
	git := &mockGit{changed: false, before: "bad-commit", after: "bad-commit"}

	dir := t.TempDir()
	stateFile := filepath.Join(dir, "deploy-state.json")

	// Pre-seed state with MaxAttempts failures.
	require.NoError(t, SaveState(stateFile, &DeployState{
		LastAttemptedCommit: "bad-commit",
		AttemptCount:        MaxAttempts,
	}))

	cfg := &Config{
		RepoDir:          filepath.Join(dir, "repo"),
		StagingDir:       filepath.Join(dir, "staging"),
		BackupDir:        filepath.Join(dir, "backups"),
		LogDir:           filepath.Join(dir, "logs"),
		LockFile:         filepath.Join(dir, "reconcile.lock"),
		StateFile:        stateFile,
		LocalAppdataPath: filepath.Join(dir, "appdata"),
		InfraSubDir:      ".",
		Force:            true,
		DryRun:           true,
	}
	require.NoError(t, os.MkdirAll(cfg.RepoDir, 0755))
	require.NoError(t, os.MkdirAll(cfg.LocalAppdataPath, 0755))

	r := NewReconciler(cfg,
		WithGitOperations(git),
		WithSecretsDecryptor(&mockSOPS{}),
	)

	// Force should override circuit breaker.
	err := r.Run(context.Background())
	require.NoError(t, err)
}

func TestRun_NewCommit_ResetsAttemptCount(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "deploy-state.json")

	// Pre-seed with failures on old commit.
	require.NoError(t, SaveState(stateFile, &DeployState{
		LastAttemptedCommit: "old-commit",
		AttemptCount:        MaxAttempts,
	}))

	// New commit arrives.
	git := &mockGit{changed: true, before: "old-commit", after: "new-commit"}

	cfg := &Config{
		RepoDir:          filepath.Join(dir, "repo"),
		StagingDir:       filepath.Join(dir, "staging"),
		BackupDir:        filepath.Join(dir, "backups"),
		LogDir:           filepath.Join(dir, "logs"),
		LockFile:         filepath.Join(dir, "reconcile.lock"),
		StateFile:        stateFile,
		LocalAppdataPath: filepath.Join(dir, "appdata"),
		InfraSubDir:      ".",
		DryRun:           true,
	}
	require.NoError(t, os.MkdirAll(cfg.RepoDir, 0755))
	require.NoError(t, os.MkdirAll(cfg.LocalAppdataPath, 0755))

	r := NewReconciler(cfg,
		WithGitOperations(git),
		WithSecretsDecryptor(&mockSOPS{}),
	)

	err := r.Run(context.Background())
	require.NoError(t, err)

	// Attempt count should be reset, new commit deployed.
	state := LoadState(stateFile)
	assert.Equal(t, "new-commit", state.LastDeployedCommit)
	assert.Equal(t, 0, state.AttemptCount)
}
