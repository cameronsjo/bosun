package reconcile

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	logpkg "github.com/cameronsjo/bosun/internal/log"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func backupDirectoryNames(t *testing.T, backupDir string) []string {
	t.Helper()

	entries, err := os.ReadDir(backupDir)
	require.NoError(t, err)

	var names []string
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "backup-") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names
}

func TestReconciler_BackupRetentionN1RunsOnlyAfterSuccess(t *testing.T) {
	deployFailure := errors.New("injected compose failure")

	tests := []struct {
		name               string
		composeErr         error
		breakCleanup       bool
		cancelCleanup      bool
		wantRunErr         string
		wantBackups        int
		wantPriorPreserved bool
		wantDeployedCommit string
		wantSignals        int
		wantCleanupWarning bool
	}{
		{
			name:               "success prunes prior backup after deploy verification",
			wantBackups:        1,
			wantPriorPreserved: false,
			wantDeployedCommit: "new-commit",
			wantSignals:        1,
		},
		{
			name:               "deploy failure preserves prior backup",
			composeErr:         deployFailure,
			wantRunErr:         "deployment failed",
			wantBackups:        2,
			wantPriorPreserved: true,
		},
		{
			name:               "cleanup failure is non-fatal and preserves both backups",
			breakCleanup:       true,
			wantBackups:        2,
			wantPriorPreserved: true,
			wantDeployedCommit: "new-commit",
			wantSignals:        1,
		},
		{
			name:               "cleanup cancellation on final verification preserves both backups",
			cancelCleanup:      true,
			wantBackups:        2,
			wantPriorPreserved: true,
			wantDeployedCommit: "new-commit",
			wantSignals:        1,
			wantCleanupWarning: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			logger := zerolog.New(&logs).Level(zerolog.WarnLevel)
			runCtx := logpkg.WithContext(context.Background(), &logger)
			runCtx, cancel := context.WithCancel(runCtx)
			t.Cleanup(cancel)

			root := evalSymlinks(t, t.TempDir())
			repoDir := filepath.Join(root, "repo")
			stagingDir := filepath.Join(root, "staging")
			appdataDir := filepath.Join(root, "appdata")
			backupDir := filepath.Join(root, "backups")
			stateFile := filepath.Join(root, "state.json")

			cfg := &Config{
				LockFile:         filepath.Join(root, "reconcile.lock"),
				StateFile:        stateFile,
				RepoDir:          repoDir,
				StagingDir:       stagingDir,
				LocalAppdataPath: appdataDir,
				BackupDir:        backupDir,
				BackupsToKeep:    1,
				InfraSubDir:      ".",
				SecretsFiles:     []string{},
			}
			seedStubComposeService(t, cfg)

			liveComposeDir := filepath.Join(appdataDir, "compose")
			require.NoError(t, os.MkdirAll(liveComposeDir, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(liveComposeDir, "stub.yml"), []byte("services:\n  stub:\n    image: alpine:old\n"), 0o644))

			priorBackup := makeBackupDir(t, backupDir, "backup-20000101-000000", true)
			blockedBackupDir := backupDir + ".blocked"
			cleanupBlocked := false
			signalCalls := 0
			verifyCalls := 0

			deploy := &DeployOps{
				DryRun:          false,
				ProjectName:     "test",
				ContentHashSync: true,
				composeUpFn: func(_ context.Context, _ []string) error {
					assert.DirExists(t, priorBackup, "the prior known-good must survive until deploy runs")
					require.Len(t, backupDirectoryNames(t, backupDir), 2, "retention must not run before compose succeeds")

					if tt.breakCleanup {
						require.NoError(t, os.Rename(backupDir, blockedBackupDir))
						require.NoError(t, os.WriteFile(backupDir, []byte("blocks cleanup ReadDir"), 0o600))
						cleanupBlocked = true
					}
					return tt.composeErr
				},
				signalContainerFn: func(_ context.Context, containerName, signal string) error {
					signalCalls++
					assert.Equal(t, "agentgateway", containerName)
					assert.Equal(t, "SIGHUP", signal)
					return nil
				},
			}
			if tt.cancelCleanup {
				deploy.verifyBackupFn = func(_ context.Context, _ string) error {
					verifyCalls++
					if verifyCalls == 2 {
						cancel()
					}
					return nil
				}
			}

			r := NewReconciler(cfg,
				WithGitOperations(&mockGitOps{syncChanged: true, syncBefore: "old-commit", syncAfter: "new-commit"}),
				WithDeployOps(deploy),
			)

			runErr := r.Run(runCtx)
			if cleanupBlocked {
				require.NoError(t, os.Remove(backupDir))
				require.NoError(t, os.Rename(blockedBackupDir, backupDir))
			}

			if tt.wantRunErr == "" {
				require.NoError(t, runErr)
			} else {
				require.Error(t, runErr)
				assert.ErrorContains(t, runErr, tt.wantRunErr)
			}

			assert.Len(t, backupDirectoryNames(t, backupDir), tt.wantBackups)
			if tt.wantPriorPreserved {
				assert.DirExists(t, priorBackup)
			} else {
				assert.NoDirExists(t, priorBackup)
			}
			assert.Equal(t, tt.wantDeployedCommit, LoadState(stateFile).LastDeployedCommit)
			assert.Equal(t, tt.wantSignals, signalCalls)
			if tt.cancelCleanup {
				assert.Equal(t, 2, verifyCalls, "cancellation must occur after the final candidate verification")
			}
			if tt.wantCleanupWarning {
				assert.Contains(t, logs.String(), `"error":"context canceled"`)
				assert.Contains(t, logs.String(), `"message":"Failed to cleanup old backups after successful deploy"`)
			}
		})
	}
}
