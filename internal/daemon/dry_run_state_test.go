package daemon

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/cameronsjo/bosun/internal/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExecuteReconcile_DryRunLeavesDeployStateUntouched pins that a daemon
// started with --dry-run does not rewrite the production deploy state file. The
// one-shot CLI hides this behind a temporary copy of the state; the daemon has
// no such copy, so Reconciler.Run itself must not persist on a dry run.
//
// It fails on the original code: the sync failure made recordSyncFailureAttempt
// save AttemptCount 2 into the seeded state file.
func TestExecuteReconcile_DryRunLeavesDeployStateUntouched(t *testing.T) {
	d := newConcurrencyDaemon(t)
	require.True(t, d.config.ReconcileConfig.DryRun, "the fixture must be a dry-run daemon")
	stateFile := d.config.ReconcileConfig.StateFile

	require.NoError(t, reconcile.SaveState(stateFile, &reconcile.DeployState{
		LastDeployedCommit:  "old",
		LastAttemptedCommit: "old",
		AttemptCount:        1,
	}))
	before, err := os.ReadFile(stateFile)
	require.NoError(t, err)

	d.reconcileOpts = append(d.reconcileOpts, reconcile.WithGitOperations(&cycleGitOps{
		sync: func(context.Context, int) (bool, string, string, error) {
			return false, "old", "new", errors.New("git unavailable")
		},
	}))

	require.Error(t, d.executeReconcile(context.Background(), "test", false))

	after, err := os.ReadFile(stateFile)
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after), "a dry-run daemon must leave the deploy state file as it found it")
}
