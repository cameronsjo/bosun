package reconcile

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recoverySkipFixture builds a reconciler whose next run will end cleanly
// without deploying, starting from a state that owes a retraction.
type recoverySkipFixture struct {
	reconciler *Reconciler
	alerter    *mockAlertSender
	stateFile  string
}

func newRecoverySkipFixture(t *testing.T, git GitOperations, seed *DeployState, deployPaths []string) *recoverySkipFixture {
	t.Helper()

	tmpDir := t.TempDir()
	stateFile := filepath.Join(tmpDir, "state.json")
	repoDir := filepath.Join(tmpDir, "repo")
	require.NoError(t, os.MkdirAll(filepath.Join(repoDir, "unraid"), 0o755))
	appdataDir := filepath.Join(tmpDir, "appdata")
	require.NoError(t, os.MkdirAll(appdataDir, 0o755))
	require.NoError(t, SaveState(stateFile, seed))

	alerter := &mockAlertSender{}
	cfg := &Config{
		DryRun:                  true,
		AllowEmptyDeclaredState: true,
		LockFile:                filepath.Join(tmpDir, "reconcile.lock"),
		StateFile:               stateFile,
		RepoDir:                 repoDir,
		StagingDir:              filepath.Join(tmpDir, "staging"),
		LocalAppdataPath:        appdataDir,
		InfraSubDir:             ".",
		SecretsFiles:            []string{},
		OnFailure:               true,
		OnSuccess:               false, // the incident's actual setting
		OnRecovery:              true,
	}
	if deployPaths != nil {
		cfg.DeployPaths = NewConfigField(deployPaths)
	}
	seedStubComposeService(t, cfg)

	return &recoverySkipFixture{
		reconciler: NewReconciler(cfg, WithGitOperations(git), WithAlerter(alerter)),
		alerter:    alerter,
		stateFile:  stateFile,
	}
}

// TestRecoveryFiresOnDocsOnlySkip drives the 2026-09-08 incident's exact
// sequence: a sync failure that alerted, then a success whose changed files are
// all outside deploy_paths, so the run takes the path-aware skip branch.
//
// Before this change that branch returned before the recovery dispatch site AND
// zeroed LastAlertedAttempt on the way out, so the retraction was not merely
// delayed -- the evidence that one was owed was destroyed. A single failure
// followed by a docs-only recovery could never retract, whatever the config
// said. Note OnSuccess is false here, as it was in production.
func TestRecoveryFiresOnDocsOnlySkip(t *testing.T) {
	git := &mockGitWithDiff{
		syncChanged: true,
		syncBefore:  "aaa111",
		syncAfter:   "bbb222",
		diffFiles:   []string{"docs/plans/some-plan.md", "CLAUDE.md"},
	}

	f := newRecoverySkipFixture(t, git, &DeployState{
		SchemaVersion:       2,
		LastDeployedCommit:  "aaa111",
		LastAttemptedCommit: "aaa111",
		AttemptCount:        1,
		LastAlertedAttempt:  1,
	}, []string{"unraid/**", "manifests/**"})

	require.NoError(t, f.reconciler.Run(context.Background()))

	assert.Equal(t, 1, f.alerter.deployRecoveryCalls,
		"a clean run must retract even when it deploys nothing")
	assert.Equal(t, 0, f.alerter.deploySuccessCalls,
		"no deploy happened, so no success alert -- and OnSuccess is false anyway")

	state := LoadState(f.stateFile)
	require.NotNil(t, state)
	assert.Equal(t, 0, state.LastAlertedAttempt, "failure tracking clears after a delivered retraction")
	assert.Equal(t, "bbb222", state.LastDeployedCommit)
}

// TestRecoveryDoesNotRepeatOnDocsOnlySkip guards the inverse failure: a
// retraction that fires on every subsequent quiet cycle.
func TestRecoveryDoesNotRepeatOnDocsOnlySkip(t *testing.T) {
	git := &mockGitWithDiff{
		syncChanged: true,
		syncBefore:  "aaa111",
		syncAfter:   "bbb222",
		diffFiles:   []string{"README.md"},
	}

	f := newRecoverySkipFixture(t, git, &DeployState{
		SchemaVersion:       2,
		LastDeployedCommit:  "aaa111",
		LastAttemptedCommit: "aaa111",
		AttemptCount:        1,
		LastAlertedAttempt:  1,
	}, []string{"unraid/**"})

	require.NoError(t, f.reconciler.Run(context.Background()))
	require.Equal(t, 1, f.alerter.deployRecoveryCalls)

	// Second quiet cycle, nothing further owed.
	git.syncBefore = "bbb222"
	git.syncAfter = "ccc333"
	require.NoError(t, f.reconciler.Run(context.Background()))
	assert.Equal(t, 1, f.alerter.deployRecoveryCalls,
		"a second clean run must not re-send the retraction")
}

// TestNoRecoveryWithoutAPriorAlert pins the predicate. A recorded failure that
// never crossed an alert threshold owes nothing, so AttemptCount alone is the
// wrong thing to branch on.
func TestNoRecoveryWithoutAPriorAlert(t *testing.T) {
	git := &mockGitWithDiff{
		syncChanged: true,
		syncBefore:  "aaa111",
		syncAfter:   "bbb222",
		diffFiles:   []string{"docs/notes.md"},
	}

	f := newRecoverySkipFixture(t, git, &DeployState{
		SchemaVersion:       2,
		LastDeployedCommit:  "aaa111",
		LastAttemptedCommit: "aaa111",
		AttemptCount:        2,
		LastAlertedAttempt:  0, // never alerted
	}, []string{"unraid/**"})

	require.NoError(t, f.reconciler.Run(context.Background()))
	assert.Equal(t, 0, f.alerter.deployRecoveryCalls,
		"nothing was ever alerted, so nothing is owed a retraction")
}

// TestRecoveryFiresOnAlreadyDeployedSkip covers the branch the design calls out
// as the one that turns a missing alert into a repeating one.
//
// This branch fails differently from the docs-only skip: it returns before
// dispatch but never zeroes LastAlertedAttempt, so dispatching here without
// clearing it would re-alert on every subsequent quiet cycle. Both halves are
// asserted -- fires once, then stops.
func TestRecoveryFiresOnAlreadyDeployedSkip(t *testing.T) {
	// syncAfter equals LastDeployedCommit, so shouldSkipDeploy takes the
	// already-deployed branch rather than the deploy-paths branch.
	git := &mockGitWithDiff{
		syncChanged: false,
		syncBefore:  "aaa111",
		syncAfter:   "aaa111",
	}

	f := newRecoverySkipFixture(t, git, &DeployState{
		SchemaVersion:       2,
		LastDeployedCommit:  "aaa111",
		LastAttemptedCommit: "aaa111",
		AttemptCount:        1,
		LastAlertedAttempt:  1,
	}, nil)

	require.NoError(t, f.reconciler.Run(context.Background()))
	assert.Equal(t, 1, f.alerter.deployRecoveryCalls,
		"the already-deployed skip must retract too; it returned before dispatch entirely")

	state := LoadState(f.stateFile)
	require.NotNil(t, state)
	assert.Equal(t, 0, state.LastAlertedAttempt,
		"this branch never cleared LastAlertedAttempt on its own; leaving it set re-alerts forever")

	require.NoError(t, f.reconciler.Run(context.Background()))
	assert.Equal(t, 1, f.alerter.deployRecoveryCalls,
		"a second already-deployed cycle must not re-send the retraction")
}
