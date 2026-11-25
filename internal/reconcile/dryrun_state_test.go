package reconcile

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dryRunHarness wires a Run() harness whose state file sits in its own
// subdirectory, so listing that directory shows state files only and never the
// lock file, staging tree, or backups.
type dryRunHarness struct {
	cfg     *Config
	alerter *mockAlertSender
}

// newRunHarness builds the Config for a Run() under test. With dryRun false it
// copies the shape that save_after_verify_test.go proves out: a persisting run
// whose deploy step is inert (the caller passes WithDeployOps(&DeployOps{DryRun: true})).
func newRunHarness(t *testing.T, dryRun bool) *dryRunHarness {
	t.Helper()
	tmp := t.TempDir()
	stateDir := filepath.Join(tmp, "state")
	repoDir := filepath.Join(tmp, "repo")
	appdata := filepath.Join(tmp, "appdata")
	require.NoError(t, os.MkdirAll(stateDir, 0o755))
	require.NoError(t, os.MkdirAll(repoDir, 0o755))
	require.NoError(t, os.MkdirAll(appdata, 0o755))

	infra := "."
	if !dryRun {
		infra = "unraid"
		require.NoError(t, os.MkdirAll(filepath.Join(repoDir, infra), 0o755))
	}
	cfg := &Config{
		DryRun:                  dryRun,
		DeployMode:              "local",
		AllowEmptyDeclaredState: true,
		SkipDeployInvariant:     !dryRun,
		LockFile:                privateLockPath(t, tmp, "reconcile.lock"),
		StateFile:               filepath.Join(stateDir, "state.json"),
		RepoDir:                 repoDir,
		StagingDir:              filepath.Join(tmp, "staging"),
		BackupDir:               filepath.Join(tmp, "backups"),
		LocalAppdataPath:        appdata,
		InfraSubDir:             infra,
		OnFailure:               true,
		OnSuccess:               true,
		OnRecovery:              true,
	}
	seedStubComposeService(t, cfg)
	return &dryRunHarness{cfg: cfg, alerter: &mockAlertSender{}}
}

func (h *dryRunHarness) seed(t *testing.T, state *DeployState) {
	t.Helper()
	if state != nil {
		require.NoError(t, SaveState(h.cfg.StateFile, state))
	}
}

// stateSnapshot is what a dry run must leave exactly as it found it: the state
// file's bytes (or its absence) and the names in its directory, which would
// show a leftover temp file from a write.
type stateSnapshot struct {
	present bool
	content []byte
	entries []string
}

func snapshotState(t *testing.T, stateFile string) stateSnapshot {
	t.Helper()
	var snap stateSnapshot
	content, err := os.ReadFile(stateFile)
	switch {
	case err == nil:
		snap.present = true
		snap.content = content
	case errors.Is(err, fs.ErrNotExist):
		// absent is a valid snapshot
	default:
		require.NoError(t, err)
	}
	entries, err := os.ReadDir(filepath.Dir(stateFile))
	require.NoError(t, err)
	for _, entry := range entries {
		snap.entries = append(snap.entries, entry.Name())
	}
	return snap
}

func gitSyncing(before, after string) GitOperations {
	return &mockGitOps{syncChanged: before != after, syncBefore: before, syncAfter: after}
}

type dryRunCase struct {
	name string
	// seed is the state file written before the run; nil leaves none.
	seed *DeployState
	// setup adjusts the Config and returns the run context and extra options.
	setup func(t *testing.T, h *dryRunHarness) (context.Context, []ReconcilerOption)

	wantErr           bool
	wantFailureAlerts int
	wantSuccessAlerts int
}

func runDryRunCases(t *testing.T, cases []dryRunCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newRunHarness(t, true)
			h.seed(t, tc.seed)
			ctx, opts := tc.setup(t, h)

			before := snapshotState(t, h.cfg.StateFile)
			r := NewReconciler(h.cfg, append([]ReconcilerOption{WithAlerter(h.alerter)}, opts...)...)
			err := r.Run(ctx)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, before, snapshotState(t, h.cfg.StateFile),
				"a dry run must leave the deploy state file and its directory exactly as found")
			assert.Equal(t, tc.wantFailureAlerts, h.alerter.deployFailureCalls, "failure alerts are unchanged by a dry run")
			assert.Equal(t, tc.wantSuccessAlerts, h.alerter.deploySuccessCalls, "success alerts are unchanged by a dry run")
			assert.Zero(t, h.alerter.deployRecoveryCalls, "a dry run sends no recovery alert")
		})
	}
}

// TestRun_DryRun_LeavesDeployStateUntouched drives Run() to each exit that used
// to persist state and asserts the state file is byte-identical afterwards.
// Every row fails on the original code: that code saved state at the exit the
// row names.
func TestRun_DryRun_LeavesDeployStateUntouched(t *testing.T) {
	runDryRunCases(t, []dryRunCase{
		{
			// Original: attempt marker, NeedsRedeploy marker, then LastDeployedCommit and DeployCount.
			name: "full success on a new commit",
			seed: &DeployState{LastDeployedCommit: "aaa111", DeployCount: 1},
			setup: func(t *testing.T, h *dryRunHarness) (context.Context, []ReconcilerOption) {
				return context.Background(), []ReconcilerOption{WithGitOperations(gitSyncing("aaa111", "bbb222"))}
			},
			wantSuccessAlerts: 1,
		},
		{
			// Original: creates the state file.
			name: "no prior state file",
			seed: nil,
			setup: func(t *testing.T, h *dryRunHarness) (context.Context, []ReconcilerOption) {
				return context.Background(), []ReconcilerOption{WithGitOperations(gitSyncing("", "bbb222"))}
			},
			wantSuccessAlerts: 1,
		},
		{
			// Original: the already-deployed skip resets the breaker counters.
			name: "already-deployed skip with stale breaker counters",
			seed: &DeployState{LastDeployedCommit: "aaa111", LastAttemptedCommit: "x", AttemptCount: 2},
			setup: func(t *testing.T, h *dryRunHarness) (context.Context, []ReconcilerOption) {
				return context.Background(), []ReconcilerOption{WithGitOperations(gitSyncing("aaa111", "aaa111"))}
			},
		},
		{
			// Original: the path-aware skip records LastDeployedCommit.
			name: "path-aware skip",
			seed: &DeployState{LastDeployedCommit: "aaa111", DeployCount: 1},
			setup: func(t *testing.T, h *dryRunHarness) (context.Context, []ReconcilerOption) {
				h.cfg.DeployPaths = NewConfigField([]string{"unraid/**"})
				git := &mockGitWithDiff{
					syncChanged: true, syncBefore: "aaa111", syncAfter: "bbb222",
					diffFiles: []string{"docs/README.md"},
				}
				return context.Background(), []ReconcilerOption{WithGitOperations(git)}
			},
		},
		{
			// Original: recordSyncFailureAttempt saves AttemptCount 1.
			name: "sync failure",
			seed: &DeployState{LastDeployedCommit: "aaa111"},
			setup: func(t *testing.T, h *dryRunHarness) (context.Context, []ReconcilerOption) {
				git := &mockGitOps{syncErr: errors.New("injected sync failure")}
				return context.Background(), []ReconcilerOption{WithGitOperations(git)}
			},
			wantErr:           true,
			wantFailureAlerts: 1,
		},
		{
			// Original: the attempt save, then the alert-throttle save in sendThrottledFailureAlert.
			name: "decrypt failure with an alerter",
			seed: &DeployState{LastDeployedCommit: "aaa111"},
			setup: func(t *testing.T, h *dryRunHarness) (context.Context, []ReconcilerOption) {
				require.NoError(t, os.WriteFile(filepath.Join(h.cfg.RepoDir, "secrets.yaml"), []byte("placeholder: true\n"), 0o600))
				h.cfg.SecretsFiles = []string{"secrets.yaml"}
				return context.Background(), []ReconcilerOption{
					WithGitOperations(gitSyncing("aaa111", "bbb222")),
					WithSecretsDecryptor(&mockSecretsDecryptor{decryptErr: errors.New("age key not available")}),
				}
			},
			wantErr:           true,
			wantFailureAlerts: 1,
		},
		{
			// Original: recordSyncFailureAttempt saves, then finalizeAttempt saves the interrupted outcome.
			// The git mock cancels the caller context and returns context.Canceled, which is what
			// isPropagatedCallerCancellation requires; attemptSnapshot is set before the sync error is handled.
			name: "caller cancellation during sync",
			seed: &DeployState{LastDeployedCommit: "aaa111"},
			setup: func(t *testing.T, h *dryRunHarness) (context.Context, []ReconcilerOption) {
				ctx, cancel := context.WithCancel(context.Background())
				t.Cleanup(cancel)
				git := callbackGitOps{sync: func(context.Context) (bool, string, string, error) {
					cancel()
					return false, "", "", context.Canceled
				}}
				return ctx, []ReconcilerOption{WithGitOperations(git)}
			},
			wantErr: true,
			// The interruption alert is a failure-class alert and stays on a dry run.
			wantFailureAlerts: 1,
		},
		{
			// Original: the recover handler in Run() calls recordSyncFailureAttempt, which saves.
			name: "panic during sync",
			seed: &DeployState{LastDeployedCommit: "aaa111"},
			setup: func(t *testing.T, h *dryRunHarness) (context.Context, []ReconcilerOption) {
				return context.Background(), []ReconcilerOption{WithGitOperations(panicSyncGitOps{})}
			},
			wantErr:           true,
			wantFailureAlerts: 1,
		},
	})
}

// TestRun_DryRun_WithholdsRecoveryAlerts covers the recovery-class alerts. A dry
// run cannot record that a retraction was delivered, so it would repeat on every
// run; it sends none. Every row fails on the original code, which sent the
// recovery and cleared the owed marker in state.
func TestRun_DryRun_WithholdsRecoveryAlerts(t *testing.T) {
	alerted := &DeployState{
		SchemaVersion:       2,
		LastDeployedCommit:  "aaa111",
		LastAttemptedCommit: "aaa111",
		AttemptCount:        1,
		LastAlertedAttempt:  1,
	}
	runDryRunCases(t, []dryRunCase{
		{
			name: "failure retraction on a docs-only path-aware skip",
			seed: alerted,
			setup: func(t *testing.T, h *dryRunHarness) (context.Context, []ReconcilerOption) {
				h.cfg.DeployPaths = NewConfigField([]string{"unraid/**"})
				git := &mockGitWithDiff{
					syncChanged: true, syncBefore: "aaa111", syncAfter: "bbb222",
					diffFiles: []string{"docs/notes.md"},
				}
				return context.Background(), []ReconcilerOption{WithGitOperations(git)}
			},
		},
		{
			name: "failure retraction on an already-deployed skip",
			seed: alerted,
			setup: func(t *testing.T, h *dryRunHarness) (context.Context, []ReconcilerOption) {
				return context.Background(), []ReconcilerOption{WithGitOperations(gitSyncing("aaa111", "aaa111"))}
			},
		},
		{
			name: "failure retraction after a full dry-run success",
			seed: &DeployState{
				SchemaVersion:       2,
				LastDeployedCommit:  "oldcommit",
				LastAttemptedCommit: "bbb222",
				AttemptCount:        2,
				LastAlertedAttempt:  1,
			},
			setup: func(t *testing.T, h *dryRunHarness) (context.Context, []ReconcilerOption) {
				return context.Background(), []ReconcilerOption{WithGitOperations(gitSyncing("aaa111", "bbb222"))}
			},
			// The success alert is not recovery-class and stays.
			wantSuccessAlerts: 1,
		},
		{
			name: "interruption retraction after a full dry-run success",
			seed: &DeployState{
				SchemaVersion:      2,
				LastDeployedCommit: "aaa111",
				LastAttemptOutcome: &LastAttemptOutcome{
					Outcome:   attemptOutcomeInterrupted,
					Commit:    "bbb222",
					Timestamp: time.Now().UTC(),
					Alerted:   true,
				},
			},
			setup: func(t *testing.T, h *dryRunHarness) (context.Context, []ReconcilerOption) {
				return context.Background(), []ReconcilerOption{WithGitOperations(gitSyncing("aaa111", "bbb222"))}
			},
			wantSuccessAlerts: 1,
		},
	})
}

// TestRun_DryRun_DeployPathsGate replaces four older tests that asserted the
// deploy_paths gate through persisted attempt state. A dry run persists none, so
// the gate is observed through the diff calls and whether the pipeline rendered.
func TestRun_DryRun_DeployPathsGate(t *testing.T) {
	tests := []struct {
		name         string
		seed         *DeployState
		force        bool
		diff         *mockGitWithDiff
		wantRendered bool
		wantDiff     [][2]string
	}{
		{
			name: "a matching file proceeds",
			seed: &DeployState{LastDeployedCommit: "aaa111", DeployCount: 1},
			diff: &mockGitWithDiff{
				syncChanged: true, syncBefore: "aaa111", syncAfter: "bbb222",
				diffFiles: []string{"unraid/compose/core.yml", "docs/README.md"},
			},
			wantRendered: true,
			wantDiff:     [][2]string{{"aaa111", "bbb222"}},
		},
		{
			name: "an unavailable diff base proceeds",
			seed: &DeployState{LastDeployedCommit: "aaa111", DeployCount: 1},
			diff: &mockGitWithDiff{
				syncChanged: true, syncBefore: "aaa111", syncAfter: "bbb222",
				diffErr: fmt.Errorf("%w: object not found", ErrCommitUnavailable),
			},
			wantRendered: true,
			wantDiff:     [][2]string{{"aaa111", "bbb222"}},
		},
		{
			name: "a generic diff error proceeds",
			seed: &DeployState{LastDeployedCommit: "aaa111", DeployCount: 1},
			diff: &mockGitWithDiff{
				syncChanged: true, syncBefore: "aaa111", syncAfter: "bbb222",
				diffErr: errors.New("object database unavailable"),
			},
			wantRendered: true,
			wantDiff:     [][2]string{{"aaa111", "bbb222"}},
		},
		{
			name:  "force bypasses the gate",
			seed:  &DeployState{LastDeployedCommit: "aaa111", DeployCount: 1},
			force: true,
			diff: &mockGitWithDiff{
				syncChanged: true, syncBefore: "aaa111", syncAfter: "bbb222",
				diffFiles: []string{"docs/README.md"},
			},
			wantRendered: true,
		},
		{
			name: "a first deploy has no diff base",
			diff: &mockGitWithDiff{
				syncChanged: true, syncBefore: "", syncAfter: "bbb222",
				diffFiles: []string{"docs/README.md"},
			},
			wantRendered: true,
		},
		{
			name: "only non-matching files skip",
			seed: &DeployState{LastDeployedCommit: "aaa111", DeployCount: 1},
			diff: &mockGitWithDiff{
				syncChanged: true, syncBefore: "aaa111", syncAfter: "bbb222",
				diffFiles: []string{"docs/README.md"},
			},
			wantRendered: false,
			wantDiff:     [][2]string{{"aaa111", "bbb222"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newRunHarness(t, true)
			h.cfg.DeployPaths = NewConfigField([]string{"unraid/**"})
			h.cfg.Force = tt.force
			h.seed(t, tt.seed)

			before := snapshotState(t, h.cfg.StateFile)
			r := NewReconciler(h.cfg, WithGitOperations(tt.diff), WithSecretsDecryptor(&mockSOPS{}))
			require.NoError(t, r.Run(context.Background()))

			assert.Equal(t, tt.wantDiff, tt.diff.diffCalledWith)
			if tt.wantRendered {
				assert.DirExists(t, h.cfg.StagingDir, "the pipeline rendered, so the gate did not skip")
			} else {
				assert.NoDirExists(t, h.cfg.StagingDir, "the gate skipped before rendering")
			}
			assert.Equal(t, before, snapshotState(t, h.cfg.StateFile))
		})
	}
}

// TestRun_DryRun_StillSyncsDecryptsAndRenders is a guard, not a regression test:
// it passes on the original code too. It pins that making a dry run
// state-neutral did not turn it into an early return.
func TestRun_DryRun_StillSyncsDecryptsAndRenders(t *testing.T) {
	h := newRunHarness(t, true)
	require.NoError(t, os.WriteFile(filepath.Join(h.cfg.RepoDir, "secrets.yaml"), []byte("placeholder: true\n"), 0o600))
	h.cfg.SecretsFiles = []string{"secrets.yaml"}

	syncs, decrypts := 0, 0
	git := callbackGitOps{sync: func(context.Context) (bool, string, string, error) {
		syncs++
		return true, "aaa111", "bbb222", nil
	}}
	dec := callbackDecryptor{decrypt: func(context.Context) (map[string]any, error) {
		decrypts++
		return map[string]any{}, nil
	}}

	r := NewReconciler(h.cfg, WithGitOperations(git), WithSecretsDecryptor(dec))
	require.NoError(t, r.Run(context.Background()))

	assert.Equal(t, 1, syncs, "git sync still runs on a dry run")
	assert.Equal(t, 1, decrypts, "secret decryption still runs on a dry run")
	assertPrivateStagingTree(t, h.cfg.StagingDir)
}

// TestRun_DryRun_StillSendsFailureAndSuccessAlerts is a guard, not a regression
// test: it passes on the original code too. It pins the decision that
// --dry-run does not imply --no-alerts (Standalone Reconcile Alert Suppression).
func TestRun_DryRun_StillSendsFailureAndSuccessAlerts(t *testing.T) {
	t.Run("a failing dry run alerts", func(t *testing.T) {
		h := newRunHarness(t, true)
		r := NewReconciler(h.cfg,
			WithGitOperations(&mockGitOps{syncErr: errors.New("injected sync failure")}),
			WithAlerter(h.alerter),
		)
		require.Error(t, r.Run(context.Background()))
		assert.Equal(t, 1, h.alerter.deployFailureCalls)
	})

	t.Run("a succeeding dry run alerts", func(t *testing.T) {
		h := newRunHarness(t, true)
		r := NewReconciler(h.cfg, WithGitOperations(gitSyncing("aaa111", "bbb222")), WithAlerter(h.alerter))
		require.NoError(t, r.Run(context.Background()))
		assert.Equal(t, 1, h.alerter.deploySuccessCalls)
	})
}

// TestRun_RecoveryAlertSentAfterDeploy keeps the coverage the dry-run subtest
// "recovery alert sent after previous failure" gave the deploy-path retraction
// (reconcile.go's retractFailureAlert before the success save). It runs a
// persisting Run() with an inert deploy step, because a dry run no longer sends
// recovery alerts.
func TestRun_RecoveryAlertSentAfterDeploy(t *testing.T) {
	h := newRunHarness(t, false)
	// LastAlertedAttempt is what real state carries: failure alerts fire at
	// attempt 1, and it is the predicate for "a retraction is owed".
	h.seed(t, &DeployState{
		SchemaVersion:       2,
		LastDeployedCommit:  "oldcommit",
		LastAttemptedCommit: "bbb222",
		AttemptCount:        2,
		LastAlertedAttempt:  1,
	})

	r := NewReconciler(h.cfg,
		WithGitOperations(gitSyncing("aaa111", "bbb222")),
		WithSecretsDecryptor(&mockSOPS{}),
		WithDeployOps(&DeployOps{DryRun: true}),
		WithAlerter(h.alerter),
	)
	require.NoError(t, r.Run(context.Background()))

	assert.Equal(t, 1, h.alerter.deployRecoveryCalls)
	assert.Equal(t, 1, h.alerter.deploySuccessCalls)
	state := LoadState(h.cfg.StateFile)
	assert.Equal(t, "bbb222", state.LastDeployedCommit)
	assert.Zero(t, state.LastAlertedAttempt, "a delivered retraction clears the owed marker")
}

// TestPersistState covers both arms of the single state-write choke point and
// its error propagation. SaveState requires the parent directory to exist, so a
// missing one is the injected write failure.
func TestPersistState(t *testing.T) {
	tests := []struct {
		name       string
		dryRun     bool
		parentDir  bool
		wantErr    bool
		wantExists bool
	}{
		{name: "a real run writes the file", dryRun: false, parentDir: true, wantExists: true},
		{name: "a real run reports a failed write", dryRun: false, parentDir: false, wantErr: true},
		{name: "a dry run writes nothing", dryRun: true, parentDir: true},
		{name: "a dry run ignores an unwritable path", dryRun: true, parentDir: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			stateDir := filepath.Join(dir, "state")
			if tt.parentDir {
				require.NoError(t, os.MkdirAll(stateDir, 0o755))
			}
			stateFile := filepath.Join(stateDir, "state.json")
			r := &Reconciler{config: &Config{StateFile: stateFile, DryRun: tt.dryRun}}

			err := r.persistState(&DeployState{LastDeployedCommit: "aaa111"})

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			_, statErr := os.Stat(stateFile)
			assert.Equal(t, tt.wantExists, statErr == nil, "state file existence")
		})
	}
}
