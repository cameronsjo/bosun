package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cameronsjo/bosun/internal/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// #391: ResolveTargets now returns (nil, error) for a multi-target config that
// carries the reserved "default" name (previously silently dropped the
// target). executeReconcile must fail the cycle loud — not panic ranging over
// a nil slice — while a resolvable list still reaches the per-target loop.
// Both cases must record cycle state (lastReconcile/lastError).
func TestExecuteReconcile_TargetResolution(t *testing.T) {
	tests := []struct {
		name            string
		targets         []reconcile.Target
		wantErr         bool
		wantErrContains string
	}{
		{
			name: "multi-target config with reserved default aborts the cycle loud",
			targets: []reconcile.Target{
				{Name: "unraid", TargetHost: "user@unraid"},
				{Name: "default", ProjectName: "homelab"},
			},
			wantErr:         true,
			wantErrContains: "default",
		},
		{
			// Empty targets synthesize the implicit default — a resolvable,
			// zero-error case. DryRun + a fake repo means r.Run() itself may
			// fail; the point under test is that targetsErr is nil and the
			// cycle reaches the per-target loop at all.
			name:    "resolvable target list still runs the per-target loop",
			targets: nil,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, _ := newTestDaemon(t)
			d.config.ReconcileConfig.Targets = tt.targets

			err := d.executeReconcile(context.Background(), "test", false)

			if tt.wantErr {
				assert.Error(t, err, "a target-resolution failure must abort the reconcile cycle")
				assert.Contains(t, err.Error(), tt.wantErrContains, "the propagated error must name the offending target")
			}

			d.stateMu.RLock()
			lastReconcile := d.lastReconcile
			lastErr := d.lastError
			d.stateMu.RUnlock()

			assert.False(t, lastReconcile.IsZero(), "cycle state must be recorded whether or not resolution fails")
			if tt.wantErr {
				assert.Error(t, lastErr, "lastError must reflect the aborted cycle")
			}
		})
	}
}

func TestExecuteReconcile_TargetFailureRetainsEvidenceAndSiblingCleans(t *testing.T) {
	d, _ := newTestDaemon(t)
	baseDir := evalSymlinks(t, t.TempDir())
	repoDir := filepath.Join(baseDir, "repo")
	firstStaging := filepath.Join(baseDir, "staging", "first")
	secondStaging := filepath.Join(baseDir, "staging", "second")
	firstAppdata := filepath.Join(baseDir, "appdata", "first")
	secondAppdata := filepath.Join(baseDir, "appdata", "second")
	require.NoError(t, os.MkdirAll(filepath.Join(repoDir, "appdata", "svc"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "appdata", "svc", "secret.yml"), []byte("secret: rendered"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(repoDir, "compose"), 0o755))
	require.NoError(t, os.MkdirAll(firstAppdata, 0o755))
	require.NoError(t, os.MkdirAll(secondAppdata, 0o755))

	badStateParent := filepath.Join(baseDir, "bad-state-parent")
	require.NoError(t, os.Mkdir(badStateParent, 0o500))
	t.Cleanup(func() { require.NoError(t, os.Chmod(badStateParent, 0o700)) })
	firstState := filepath.Join(badStateParent, "state.json")
	secondState := filepath.Join(baseDir, "second-state.json")

	cfg := d.config.ReconcileConfig
	cfg.DryRun = false
	cfg.DeployMode = "local"
	cfg.AllowEmptyDeclaredState = true
	cfg.SkipDeployInvariant = true
	cfg.RepoDir = repoDir
	cfg.StagingDir = filepath.Join(baseDir, "staging")
	cfg.StateFile = filepath.Join(baseDir, "base-state.json")
	cfg.LocalAppdataPath = filepath.Join(baseDir, "appdata")
	cfg.LockFile = filepath.Join(baseDir, "reconcile.lock")
	cfg.BackupDir = filepath.Join(baseDir, "backups")
	cfg.InfraSubDir = "."
	cfg.Targets = []reconcile.Target{
		{Name: "first", ProjectName: "first", StateFile: firstState, StagingDir: firstStaging, LocalAppdataPath: firstAppdata},
		{Name: "second", ProjectName: "second", StateFile: secondState, StagingDir: secondStaging, LocalAppdataPath: secondAppdata},
	}
	d.reconcileOpts = append(d.reconcileOpts,
		reconcile.WithGitOperations(daemonSuccessGitOps{}),
		reconcile.WithDeployOps(&reconcile.DeployOps{DryRun: true}),
	)

	err := d.executeReconcile(context.Background(), "test", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "target first")
	assert.DirExists(t, firstStaging, "failed target retains its rendered evidence")
	assert.NoDirExists(t, secondStaging, "successful sibling removes only its own staging slot")
	assert.Equal(t, "abc123", reconcile.LoadState(secondState).LastDeployedCommit)

	info, statErr := os.Stat(filepath.Join(firstStaging, "appdata", "svc", "secret.yml"))
	require.NoError(t, statErr)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}
