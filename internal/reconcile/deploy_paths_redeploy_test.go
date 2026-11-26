package reconcile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A prior deploy that synced files and then failed leaves NeedsRedeploy set with
// LastDeployedCommit still at the last good commit. A later commit that touches
// no deploy_paths file must not be treated as "nothing to deploy": the diff base
// is the last good commit, so a commit that reverts the failed deploy's changes
// shows no relevant path while the failed deploy's files are still on disk.
func TestRun_DeployPathsNeedsRedeployForcesPipeline(t *testing.T) {
	type outcome struct {
		err      error
		diffs    [][2]string
		decrypts int
		state    *DeployState
	}
	run := func(t *testing.T, needsRedeploy bool) outcome {
		t.Helper()
		h := newRunHarness(t, false)
		h.cfg.DeployPaths = NewConfigField([]string{"unraid/**"})
		h.cfg.SecretsFiles = []string{"secrets.yaml"}
		require.NoError(t, os.WriteFile(filepath.Join(h.cfg.RepoDir, "secrets.yaml"), []byte("placeholder: true\n"), 0o600))
		h.seed(t, &DeployState{LastDeployedCommit: "aaa111", DeployCount: 1, NeedsRedeploy: needsRedeploy})

		git := &mockGitWithDiff{
			syncChanged: true, syncBefore: "bbb222", syncAfter: "ccc333",
			diffFiles: []string{"docs/README.md"},
		}
		decrypts := 0
		dec := callbackDecryptor{decrypt: func(context.Context) (map[string]any, error) {
			decrypts++
			return nil, errors.New("injected decrypt failure")
		}}

		r := NewReconciler(h.cfg, WithGitOperations(git), WithSecretsDecryptor(dec))
		err := r.Run(context.Background())
		return outcome{err: err, diffs: git.diffCalledWith, decrypts: decrypts, state: LoadState(h.cfg.StateFile)}
	}

	t.Run("a pending redeploy bypasses the gate and keeps its marker when the pipeline fails", func(t *testing.T) {
		got := run(t, true)

		require.Empty(t, got.diffs, "the gate must not even diff while a redeploy is pending")
		require.ErrorContains(t, got.err, "failed to decrypt secrets")
		assert.Equal(t, 1, got.decrypts, "the pipeline ran past the gate")
		assert.True(t, got.state.NeedsRedeploy, "a failed pipeline must not clear the redeploy marker")
		assert.Equal(t, "aaa111", got.state.LastDeployedCommit)
	})

	t.Run("without a pending redeploy the gate still skips (control)", func(t *testing.T) {
		got := run(t, false)

		require.NoError(t, got.err)
		assert.Equal(t, [][2]string{{"aaa111", "ccc333"}}, got.diffs)
		assert.Zero(t, got.decrypts)
		assert.Equal(t, "ccc333", got.state.LastDeployedCommit)
	})
}
