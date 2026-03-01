package reconcile

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cameronsjo/bosun/internal/docker"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	assert.Equal(t, "main", cfg.RepoBranch)
	assert.Equal(t, "/app/repo", cfg.RepoDir)
	assert.Equal(t, "/app/staging", cfg.StagingDir)
	assert.Equal(t, "/app/backups", cfg.BackupDir)
	assert.Equal(t, "/app/logs", cfg.LogDir)
	assert.Equal(t, "/mnt/appdata", cfg.LocalAppdataPath)
	assert.Equal(t, "/mnt/user/appdata", cfg.RemoteAppdataPath)
	assert.Equal(t, ".", cfg.InfraSubDir)
	assert.Equal(t, 5, cfg.BackupsToKeep)
}

func TestNewReconciler(t *testing.T) {
	cfg := &Config{
		RepoURL:    "https://github.com/test/repo.git",
		RepoBranch: "main",
		RepoDir:    "/tmp/repo",
		DryRun:     true,
	}

	r := NewReconciler(cfg)

	assert.NotNil(t, r)
	assert.Equal(t, cfg, r.config)
	assert.NotNil(t, r.git)
	assert.NotNil(t, r.sops)
	assert.NotNil(t, r.deploy)
	assert.Equal(t, DefaultLockFile, r.lockFile)
}

func TestReconciler_AcquireLock(t *testing.T) {
	t.Run("acquire lock successfully", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockFile := filepath.Join(tmpDir, "test.lock")

		cfg := DefaultConfig()
		r := NewReconciler(cfg)
		r.lockFile = lockFile

		err := r.acquireLock()
		require.NoError(t, err)
		assert.NotNil(t, r.lockFd)

		// Clean up
		r.releaseLock()
	})

	t.Run("fail to acquire already held lock", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockFile := filepath.Join(tmpDir, "test.lock")

		cfg := DefaultConfig()

		// First reconciler acquires lock
		r1 := NewReconciler(cfg)
		r1.lockFile = lockFile
		err := r1.acquireLock()
		require.NoError(t, err)

		// Second reconciler should fail
		r2 := NewReconciler(cfg)
		r2.lockFile = lockFile
		err = r2.acquireLock()
		assert.Error(t, err)

		// Clean up
		r1.releaseLock()
	})
}

func TestReconciler_ReleaseLock(t *testing.T) {
	t.Run("release held lock", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockFile := filepath.Join(tmpDir, "test.lock")

		cfg := DefaultConfig()
		r := NewReconciler(cfg)
		r.lockFile = lockFile

		err := r.acquireLock()
		require.NoError(t, err)

		r.releaseLock()
		assert.Nil(t, r.lockFd)

		// Should be able to acquire again
		err = r.acquireLock()
		require.NoError(t, err)
		r.releaseLock()
	})

	t.Run("release without holding lock", func(t *testing.T) {
		cfg := DefaultConfig()
		r := NewReconciler(cfg)

		// Should not panic
		r.releaseLock()
	})
}

func TestReconciler_IsLocalMode(t *testing.T) {
	t.Run("local mode with target host", func(t *testing.T) {
		cfg := &Config{
			TargetHost:       "user@host",
			LocalAppdataPath: "/non/existent/path",
		}
		r := NewReconciler(cfg)

		assert.False(t, r.isLocalMode())
	})

	t.Run("local mode with existing appdata", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := &Config{
			LocalAppdataPath: tmpDir,
		}
		r := NewReconciler(cfg)

		assert.True(t, r.isLocalMode())
	})

	t.Run("remote mode with non-existent appdata", func(t *testing.T) {
		cfg := &Config{
			LocalAppdataPath: "/non/existent/path",
		}
		r := NewReconciler(cfg)

		assert.False(t, r.isLocalMode())
	})
}

func TestReconciler_GetTargetHost(t *testing.T) {
	t.Run("explicit target host", func(t *testing.T) {
		cfg := &Config{
			TargetHost: "user@host",
		}
		r := NewReconciler(cfg)

		host := r.getTargetHost(nil)
		assert.Equal(t, "user@host", host)
	})

	t.Run("target host from secrets", func(t *testing.T) {
		cfg := &Config{}
		r := NewReconciler(cfg)

		secrets := map[string]any{
			"network": map[string]any{
				"unraid_ip": "192.168.1.100",
			},
		}

		host := r.getTargetHost(secrets)
		assert.Equal(t, "root@192.168.1.100", host)
	})

	t.Run("no target host available", func(t *testing.T) {
		cfg := &Config{}
		r := NewReconciler(cfg)

		host := r.getTargetHost(nil)
		assert.Empty(t, host)
	})

	t.Run("secrets with missing network", func(t *testing.T) {
		cfg := &Config{}
		r := NewReconciler(cfg)

		secrets := map[string]any{
			"other": "value",
		}

		host := r.getTargetHost(secrets)
		assert.Empty(t, host)
	})
}

func TestReconciler_DecryptSecrets(t *testing.T) {
	t.Run("empty secrets files list", func(t *testing.T) {
		cfg := &Config{
			SecretsFiles: []string{},
		}
		r := NewReconciler(cfg)

		// Initialize sops
		r.sops = NewSOPSOps()

		secrets, err := r.decryptSecrets(context.TODO())
		require.NoError(t, err)
		assert.Empty(t, secrets)
	})

	t.Run("non-existent secrets file", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := &Config{
			RepoDir:      tmpDir,
			SecretsFiles: []string{"non-existent.yaml"},
		}
		r := NewReconciler(cfg)
		r.sops = NewSOPSOps()

		_, err := r.decryptSecrets(context.TODO())
		assert.Error(t, err)
	})
}

func TestReconciler_RenderTemplates(t *testing.T) {
	t.Run("clears and creates staging directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		stagingDir := filepath.Join(tmpDir, "staging")
		repoDir := filepath.Join(tmpDir, "repo")

		// Create repo dir (infra is at root with InfraSubDir=".")
		require.NoError(t, os.MkdirAll(repoDir, 0755))

		cfg := &Config{
			RepoDir:     repoDir,
			StagingDir:  stagingDir,
			InfraSubDir: ".",
		}
		r := NewReconciler(cfg)

		// Create old file in staging
		require.NoError(t, os.MkdirAll(stagingDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(stagingDir, "old.txt"), []byte("old"), 0644))

		secrets := map[string]any{}
		err := r.renderTemplates(context.TODO(), secrets)

		// Template rendering uses native Go templates, should not error
		if err != nil {
			t.Logf("renderTemplates error: %v", err)
		}

		// Old file should be removed
		assert.NoFileExists(t, filepath.Join(stagingDir, "old.txt"))
	})
}

func TestReconciler_ReloadProjectConfig(t *testing.T) {
	t.Run("updates hooks when not from env", func(t *testing.T) {
		cfg := &Config{
			PostSyncHooks: []PostSyncHook{
				{Paths: []string{"old/**"}, Action: "restart", Container: "old"},
			},
		}
		cfg.ConfigReloader = func(dir string) (*ReloadedConfig, error) {
			return &ReloadedConfig{
				PostSyncHooks: []PostSyncHook{
					{Paths: []string{"new/**"}, Action: "restart", Container: "new"},
				},
			}, nil
		}
		r := NewReconciler(cfg)

		r.reloadProjectConfig()

		require.Len(t, r.config.PostSyncHooks, 1)
		assert.Equal(t, "new", r.config.PostSyncHooks[0].Container)
	})

	t.Run("skips hooks when from env", func(t *testing.T) {
		cfg := &Config{
			PostSyncHooks: []PostSyncHook{
				{Paths: []string{"env/**"}, Action: "restart", Container: "env-hook"},
			},
			PostSyncHooksFromEnv: true,
		}
		cfg.ConfigReloader = func(dir string) (*ReloadedConfig, error) {
			return &ReloadedConfig{
				PostSyncHooks: []PostSyncHook{
					{Paths: []string{"repo/**"}, Action: "restart", Container: "repo-hook"},
				},
			}, nil
		}
		r := NewReconciler(cfg)

		r.reloadProjectConfig()

		require.Len(t, r.config.PostSyncHooks, 1)
		assert.Equal(t, "env-hook", r.config.PostSyncHooks[0].Container)
	})

	t.Run("updates settle delay when not from env", func(t *testing.T) {
		cfg := &Config{
			HookSettleDelay: 0,
		}
		cfg.ConfigReloader = func(dir string) (*ReloadedConfig, error) {
			return &ReloadedConfig{
				HookSettleDelay: 5 * time.Second,
			}, nil
		}
		r := NewReconciler(cfg)

		r.reloadProjectConfig()

		assert.Equal(t, 5*time.Second, r.config.HookSettleDelay)
	})

	t.Run("skips settle delay when from env", func(t *testing.T) {
		cfg := &Config{
			HookSettleDelay:        2 * time.Second,
			HookSettleDelayFromEnv: true,
		}
		cfg.ConfigReloader = func(dir string) (*ReloadedConfig, error) {
			return &ReloadedConfig{
				HookSettleDelay: 10 * time.Second,
			}, nil
		}
		r := NewReconciler(cfg)

		r.reloadProjectConfig()

		assert.Equal(t, 2*time.Second, r.config.HookSettleDelay)
	})

	t.Run("keeps config on parse error", func(t *testing.T) {
		cfg := &Config{
			PostSyncHooks: []PostSyncHook{
				{Paths: []string{"keep/**"}, Action: "restart", Container: "keep"},
			},
		}
		cfg.ConfigReloader = func(dir string) (*ReloadedConfig, error) {
			return nil, fmt.Errorf("YAML parse error")
		}
		r := NewReconciler(cfg)

		r.reloadProjectConfig()

		require.Len(t, r.config.PostSyncHooks, 1)
		assert.Equal(t, "keep", r.config.PostSyncHooks[0].Container)
	})

	t.Run("no-op when reloader is nil", func(t *testing.T) {
		cfg := &Config{
			PostSyncHooks: []PostSyncHook{
				{Paths: []string{"unchanged/**"}, Action: "restart", Container: "unchanged"},
			},
		}
		r := NewReconciler(cfg)

		r.reloadProjectConfig()

		require.Len(t, r.config.PostSyncHooks, 1)
		assert.Equal(t, "unchanged", r.config.PostSyncHooks[0].Container)
	})

	t.Run("no-op when repo has no config", func(t *testing.T) {
		cfg := &Config{
			PostSyncHooks: []PostSyncHook{
				{Paths: []string{"existing/**"}, Action: "restart", Container: "existing"},
			},
		}
		cfg.ConfigReloader = func(dir string) (*ReloadedConfig, error) {
			return &ReloadedConfig{}, nil
		}
		r := NewReconciler(cfg)

		r.reloadProjectConfig()

		require.Len(t, r.config.PostSyncHooks, 1)
		assert.Equal(t, "existing", r.config.PostSyncHooks[0].Container)
	})
}

func TestExecutePostSyncHooks_DiffFilesError_FiresAllHooks(t *testing.T) {
	// Simulates the shallow clone scenario: DiffFiles fails because the previous
	// commit is not in the shallow history. This is the root cause of GitHub #55.
	cfg := &Config{
		PostSyncHooks: []PostSyncHook{
			{Paths: []string{"**"}, Action: "restart", Container: "traefik"},
		},
	}

	diffErr := fmt.Errorf("resolve from-commit abc12345: object not found")
	mockGitOps := &mockGitWithDiff{
		diffFiles: nil,
		diffErr:   diffErr,
	}

	dockerCalled := false
	r := NewReconciler(cfg, WithGitOperations(mockGitOps))
	r.dockerClientFn = func() *docker.Client {
		dockerCalled = true
		return nil // Would be a real client in production
	}

	// previousCommit is non-empty (second deploy), currentCommit is the new tip
	r.executePostSyncHooks(context.Background(), "abc1234567890", "def9876543210", nil)

	// BUG: DiffFiles fails on shallow clone, hooks are silently skipped.
	// After the fix, the function should fall back to treating all files as changed
	// and the docker client function should be called.
	assert.True(t, dockerCalled, "expected docker client to be called when DiffFiles fails (hooks should still fire)")
}

func TestExecutePostSyncHooks_WrittenFiles_MatchesHooks(t *testing.T) {
	// When content-hash sync provides WrittenFiles, hooks should match against those.
	cfg := &Config{
		PostSyncHooks: []PostSyncHook{
			{Paths: []string{"**"}, Action: "restart", Container: "traefik"},
		},
	}

	mockGitOps := &mockGitWithDiff{}
	dockerCalled := false
	r := NewReconciler(cfg, WithGitOperations(mockGitOps))
	r.dockerClientFn = func() *docker.Client {
		dockerCalled = true
		return nil
	}

	deployResult := &DeployResult{
		WrittenFiles: []string{"conf.d/router.yml", "traefik.yml"},
	}

	r.executePostSyncHooks(context.Background(), "abc1234567890", "def9876543210", deployResult)

	assert.True(t, dockerCalled, "expected docker client to be called when WrittenFiles are present")
}

func TestExecutePostSyncHooks_EmptyPreviousCommit_Skips(t *testing.T) {
	// First deploy: previousCommit is empty, hooks should be skipped (correct behavior).
	cfg := &Config{
		PostSyncHooks: []PostSyncHook{
			{Paths: []string{"**"}, Action: "restart", Container: "traefik"},
		},
	}

	mockGitOps := &mockGitWithDiff{}
	dockerCalled := false
	r := NewReconciler(cfg, WithGitOperations(mockGitOps))
	r.dockerClientFn = func() *docker.Client {
		dockerCalled = true
		return nil
	}

	r.executePostSyncHooks(context.Background(), "", "def9876543210", nil)

	assert.False(t, dockerCalled, "hooks should be skipped on first deploy (empty previousCommit)")
}

// mockGitWithDiff extends the basic mock with controllable DiffFiles behavior.
type mockGitWithDiff struct {
	syncChanged bool
	syncBefore  string
	syncAfter   string
	syncErr     error
	diffFiles   []string
	diffErr     error
}

func (m *mockGitWithDiff) Sync(_ context.Context) (bool, string, string, error) {
	return m.syncChanged, m.syncBefore, m.syncAfter, m.syncErr
}
func (m *mockGitWithDiff) IsRepo(_ context.Context) bool { return true }
func (m *mockGitWithDiff) DiffFiles(_ context.Context, _, _ string) ([]string, error) {
	return m.diffFiles, m.diffErr
}

func TestReloadProjectConfig_DeployPaths(t *testing.T) {
	t.Run("updates deploy_paths when not from env", func(t *testing.T) {
		cfg := &Config{}
		cfg.ConfigReloader = func(dir string) (*ReloadedConfig, error) {
			return &ReloadedConfig{
				DeployPaths: []string{"unraid/**", "infra/**"},
			}, nil
		}
		r := NewReconciler(cfg)

		r.reloadProjectConfig()

		assert.Equal(t, []string{"unraid/**", "infra/**"}, r.config.DeployPaths)
	})

	t.Run("skips deploy_paths when from env", func(t *testing.T) {
		cfg := &Config{
			DeployPaths:        []string{"env/**"},
			DeployPathsFromEnv: true,
		}
		cfg.ConfigReloader = func(dir string) (*ReloadedConfig, error) {
			return &ReloadedConfig{
				DeployPaths: []string{"repo/**"},
			}, nil
		}
		r := NewReconciler(cfg)

		r.reloadProjectConfig()

		assert.Equal(t, []string{"env/**"}, r.config.DeployPaths)
	})
}

func TestRun_DeployPathsSkip(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile := filepath.Join(tmpDir, "state.json")

	cfg := &Config{
		RepoDir:     tmpDir,
		LockFile:    filepath.Join(tmpDir, "test.lock"),
		StateFile:   stateFile,
		DeployPaths: []string{"unraid/**"},
	}

	mockGit := &mockGitWithDiff{
		syncChanged: true,
		syncBefore:  "aaa111",
		syncAfter:   "bbb222",
		diffFiles:   []string{"docs/README.md", ".beads/issues/task-1.jsonl"},
	}

	r := NewReconciler(cfg,
		WithGitOperations(mockGit),
		WithSecretsDecryptor(&mockSOPS{}),
	)

	err := r.Run(context.Background())
	require.NoError(t, err)

	// Verify state was updated with skipped commit.
	state := LoadState(stateFile)
	assert.Equal(t, "bbb222", state.LastDeployedCommit)
	assert.Equal(t, 0, state.DeployCount, "deploy count should not be incremented for skipped commits")
}

func TestRun_DeployPathsMatch(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile := filepath.Join(tmpDir, "state.json")

	cfg := &Config{
		RepoDir:     tmpDir,
		LockFile:    filepath.Join(tmpDir, "test.lock"),
		StateFile:   stateFile,
		StagingDir:  filepath.Join(tmpDir, "staging"),
		DeployPaths: []string{"unraid/**"},
		DryRun:      true,
	}

	mockGit := &mockGitWithDiff{
		syncChanged: true,
		syncBefore:  "aaa111",
		syncAfter:   "bbb222",
		diffFiles:   []string{"unraid/compose/core.yml", "docs/README.md"},
	}

	r := NewReconciler(cfg,
		WithGitOperations(mockGit),
		WithSecretsDecryptor(&mockSOPS{}),
	)

	// This will fail at decrypt/render stage because we don't have a full repo,
	// but the key test is that it did NOT skip — it proceeded past the path check.
	err := r.Run(context.Background())

	// The error should be from a later pipeline stage, not path-aware skip.
	// In dry-run with no secrets, it should succeed through to rendering.
	// State should show it was attempted (not skip-deployed).
	state := LoadState(stateFile)
	if err == nil {
		// If it succeeded (dry-run path), deploy count should be incremented.
		assert.Equal(t, 1, state.DeployCount)
	} else {
		// If it failed at a later stage, that's fine — the point is it didn't skip.
		assert.Equal(t, "bbb222", state.LastAttemptedCommit)
	}
}

func TestRun_DeployPathsDiffFails(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile := filepath.Join(tmpDir, "state.json")

	cfg := &Config{
		RepoDir:     tmpDir,
		LockFile:    filepath.Join(tmpDir, "test.lock"),
		StateFile:   stateFile,
		StagingDir:  filepath.Join(tmpDir, "staging"),
		DeployPaths: []string{"unraid/**"},
		DryRun:      true,
	}

	mockGit := &mockGitWithDiff{
		syncChanged: true,
		syncBefore:  "aaa111",
		syncAfter:   "bbb222",
		diffErr:     fmt.Errorf("object not found"),
	}

	r := NewReconciler(cfg,
		WithGitOperations(mockGit),
		WithSecretsDecryptor(&mockSOPS{}),
	)

	// DiffFiles fails, so it should fall through to full deploy (not skip).
	_ = r.Run(context.Background())

	state := LoadState(stateFile)
	// The pipeline should have been attempted (not skip-deployed).
	assert.Equal(t, "bbb222", state.LastAttemptedCommit)
}

func TestRun_DeployPathsForceOverride(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile := filepath.Join(tmpDir, "state.json")

	cfg := &Config{
		RepoDir:     tmpDir,
		LockFile:    filepath.Join(tmpDir, "test.lock"),
		StateFile:   stateFile,
		StagingDir:  filepath.Join(tmpDir, "staging"),
		DeployPaths: []string{"unraid/**"},
		Force:       true,
		DryRun:      true,
	}

	mockGit := &mockGitWithDiff{
		syncChanged: true,
		syncBefore:  "aaa111",
		syncAfter:   "bbb222",
		diffFiles:   []string{"docs/README.md"}, // Non-matching files
	}

	r := NewReconciler(cfg,
		WithGitOperations(mockGit),
		WithSecretsDecryptor(&mockSOPS{}),
	)

	// With --force, path check should be bypassed entirely.
	_ = r.Run(context.Background())

	state := LoadState(stateFile)
	// Pipeline should have run (attempt tracked, not skip-deployed).
	assert.Equal(t, "bbb222", state.LastAttemptedCommit)
}

func TestConfig_Validation(t *testing.T) {
	t.Run("config fields", func(t *testing.T) {
		cfg := &Config{
			RepoURL:           "https://github.com/test/repo.git",
			RepoBranch:        "develop",
			RepoDir:           "/custom/repo",
			StagingDir:        "/custom/staging",
			BackupDir:         "/custom/backups",
			LogDir:            "/custom/logs",
			TargetHost:        "user@remote",
			LocalAppdataPath:  "/local/appdata",
			RemoteAppdataPath: "/remote/appdata",
			DryRun:            true,
			Force:             true,
			SecretsFiles:      []string{"secrets1.yaml", "secrets2.yaml"},
			InfraSubDir:       "infra",
			BackupsToKeep:     10,
		}

		assert.Equal(t, "https://github.com/test/repo.git", cfg.RepoURL)
		assert.Equal(t, "develop", cfg.RepoBranch)
		assert.Equal(t, "/custom/repo", cfg.RepoDir)
		assert.Equal(t, "/custom/staging", cfg.StagingDir)
		assert.Equal(t, "/custom/backups", cfg.BackupDir)
		assert.Equal(t, "/custom/logs", cfg.LogDir)
		assert.Equal(t, "user@remote", cfg.TargetHost)
		assert.Equal(t, "/local/appdata", cfg.LocalAppdataPath)
		assert.Equal(t, "/remote/appdata", cfg.RemoteAppdataPath)
		assert.True(t, cfg.DryRun)
		assert.True(t, cfg.Force)
		assert.Len(t, cfg.SecretsFiles, 2)
		assert.Equal(t, "infra", cfg.InfraSubDir)
		assert.Equal(t, 10, cfg.BackupsToKeep)
	})
}

// --- Phase 2A: Alert sender mock and tests ---

// alertCall captures the arguments passed to a mock alert sender method.
type alertCall struct {
	commit        string
	target        string
	reason        string
	containers    []string
	priorFailures int
	backupName    string
}

// mockAlertSender implements the AlertSender interface for testing.
type mockAlertSender struct {
	mu    sync.Mutex
	calls map[string][]alertCall
	err   error
}

func newMockAlertSender() *mockAlertSender {
	return &mockAlertSender{calls: make(map[string][]alertCall)}
}

func (m *mockAlertSender) SendDeploySuccess(_ context.Context, commit, target string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls["SendDeploySuccess"] = append(m.calls["SendDeploySuccess"], alertCall{commit: commit, target: target})
	return m.err
}

func (m *mockAlertSender) SendDeployFailure(_ context.Context, commit, target, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls["SendDeployFailure"] = append(m.calls["SendDeployFailure"], alertCall{commit: commit, target: target, reason: reason})
	return m.err
}

func (m *mockAlertSender) SendDeployRecovery(_ context.Context, commit, target string, priorFailures int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls["SendDeployRecovery"] = append(m.calls["SendDeployRecovery"], alertCall{commit: commit, target: target, priorFailures: priorFailures})
	return m.err
}

func (m *mockAlertSender) SendUnhealthyContainers(_ context.Context, target string, containers []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls["SendUnhealthyContainers"] = append(m.calls["SendUnhealthyContainers"], alertCall{target: target, containers: containers})
	return m.err
}

func (m *mockAlertSender) SendRollbackSuccess(_ context.Context, target, backupName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls["SendRollbackSuccess"] = append(m.calls["SendRollbackSuccess"], alertCall{target: target, backupName: backupName})
	return m.err
}

func (m *mockAlertSender) SendRollbackFailure(_ context.Context, target, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls["SendRollbackFailure"] = append(m.calls["SendRollbackFailure"], alertCall{target: target, reason: reason})
	return m.err
}

func (m *mockAlertSender) getCalls(method string) []alertCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls[method]
}

// newTestReconcilerWithAlerter creates a Reconciler with a mock alert sender for testing.
func newTestReconcilerWithAlerter(t *testing.T, alerter AlertSender) (*Reconciler, string) {
	t.Helper()
	tmpDir := t.TempDir()
	cfg := &Config{
		RepoDir:    tmpDir,
		StagingDir: filepath.Join(tmpDir, "staging"),
		StateFile:  filepath.Join(tmpDir, "state.json"),
		LockFile:   filepath.Join(tmpDir, "test.lock"),
		RepoBranch: "main",
	}
	r := NewReconciler(cfg,
		WithGitOperations(&mockGitWithDiff{}),
		WithSecretsDecryptor(&mockSOPS{}),
		WithAlerter(alerter),
	)
	return r, tmpDir
}

func TestSendSuccessAlert(t *testing.T) {
	t.Run("nil alerter does not panic", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := &Config{
			RepoBranch: "main",
			LockFile:   filepath.Join(tmpDir, "lock"),
		}
		r := NewReconciler(cfg)
		r.alerter = nil
		r.lastCommit = "abc123"

		// Must not panic.
		r.sendSuccessAlert(context.Background())
	})

	t.Run("sends with commit and target", func(t *testing.T) {
		alerter := newMockAlertSender()
		r, _ := newTestReconcilerWithAlerter(t, alerter)
		r.lastCommit = "abc123"
		r.config.TargetHost = "root@host"

		r.sendSuccessAlert(context.Background())

		calls := alerter.getCalls("SendDeploySuccess")
		require.Len(t, calls, 1)
		assert.Equal(t, "abc123", calls[0].commit)
		assert.Equal(t, "root@host", calls[0].target)
	})

	t.Run("target defaults to local when TargetHost is empty", func(t *testing.T) {
		alerter := newMockAlertSender()
		r, _ := newTestReconcilerWithAlerter(t, alerter)
		r.lastCommit = "def456"
		r.config.TargetHost = ""

		r.sendSuccessAlert(context.Background())

		calls := alerter.getCalls("SendDeploySuccess")
		require.Len(t, calls, 1)
		assert.Equal(t, "local", calls[0].target)
	})

	t.Run("target uses TargetHost when set", func(t *testing.T) {
		alerter := newMockAlertSender()
		r, _ := newTestReconcilerWithAlerter(t, alerter)
		r.lastCommit = "ghi789"
		r.config.TargetHost = "deploy@prod"

		r.sendSuccessAlert(context.Background())

		calls := alerter.getCalls("SendDeploySuccess")
		require.Len(t, calls, 1)
		assert.Equal(t, "deploy@prod", calls[0].target)
	})
}

func TestSendThrottledFailureAlert(t *testing.T) {
	t.Run("nil alerter does not panic", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := &Config{
			RepoBranch: "main",
			LockFile:   filepath.Join(tmpDir, "lock"),
			StateFile:  filepath.Join(tmpDir, "state.json"),
		}
		r := NewReconciler(cfg)
		r.alerter = nil
		state := &DeployState{AttemptCount: 1, LastAlertedAttempt: 0}

		// Must not panic.
		r.sendThrottledFailureAlert(context.Background(), state, "test failure")
	})

	t.Run("skips when ShouldAlert returns false", func(t *testing.T) {
		alerter := newMockAlertSender()
		r, _ := newTestReconcilerWithAlerter(t, alerter)
		r.lastCommit = "abc123"

		// Attempt 2 with lastAlerted=1: ShouldAlert(2,1) should be false
		// (thresholds are 1,3,10,30 - attempt 2 is not in the schedule).
		state := &DeployState{AttemptCount: 2, LastAlertedAttempt: 1}
		r.sendThrottledFailureAlert(context.Background(), state, "some failure")

		calls := alerter.getCalls("SendDeployFailure")
		assert.Empty(t, calls)
	})

	t.Run("sends when ShouldAlert returns true", func(t *testing.T) {
		alerter := newMockAlertSender()
		r, _ := newTestReconcilerWithAlerter(t, alerter)
		r.lastCommit = "abc123"
		r.config.TargetHost = ""

		// Attempt 1 with lastAlerted=0: ShouldAlert(1,0) is true.
		state := &DeployState{AttemptCount: 1, LastAlertedAttempt: 0}
		r.sendThrottledFailureAlert(context.Background(), state, "decrypt failed")

		calls := alerter.getCalls("SendDeployFailure")
		require.Len(t, calls, 1)
		assert.Equal(t, "abc123", calls[0].commit)
		assert.Equal(t, "local", calls[0].target)
		assert.Equal(t, "decrypt failed", calls[0].reason)
	})

	t.Run("updates state LastAlertedAttempt after sending", func(t *testing.T) {
		alerter := newMockAlertSender()
		r, _ := newTestReconcilerWithAlerter(t, alerter)
		r.lastCommit = "abc123"

		state := &DeployState{AttemptCount: 1, LastAlertedAttempt: 0}
		r.sendThrottledFailureAlert(context.Background(), state, "some error")

		assert.Equal(t, 1, state.LastAlertedAttempt)
	})
}

func TestSendRecoveryAlert(t *testing.T) {
	t.Run("nil alerter does not panic", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := &Config{
			RepoBranch: "main",
			LockFile:   filepath.Join(tmpDir, "lock"),
		}
		r := NewReconciler(cfg)
		r.alerter = nil

		// Must not panic.
		r.sendRecoveryAlert(context.Background(), 3)
	})

	t.Run("sends with priorFailures count", func(t *testing.T) {
		alerter := newMockAlertSender()
		r, _ := newTestReconcilerWithAlerter(t, alerter)
		r.lastCommit = "fix123"
		r.config.TargetHost = "root@box"

		r.sendRecoveryAlert(context.Background(), 5)

		calls := alerter.getCalls("SendDeployRecovery")
		require.Len(t, calls, 1)
		assert.Equal(t, "fix123", calls[0].commit)
		assert.Equal(t, "root@box", calls[0].target)
		assert.Equal(t, 5, calls[0].priorFailures)
	})

	t.Run("target defaults to local", func(t *testing.T) {
		alerter := newMockAlertSender()
		r, _ := newTestReconcilerWithAlerter(t, alerter)
		r.lastCommit = "fix456"
		r.config.TargetHost = ""

		r.sendRecoveryAlert(context.Background(), 2)

		calls := alerter.getCalls("SendDeployRecovery")
		require.Len(t, calls, 1)
		assert.Equal(t, "local", calls[0].target)
	})
}

func TestSendUnhealthyAlert(t *testing.T) {
	t.Run("nil alerter does not panic", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := &Config{
			RepoBranch: "main",
			LockFile:   filepath.Join(tmpDir, "lock"),
		}
		r := NewReconciler(cfg)
		r.alerter = nil

		// Must not panic.
		r.sendUnhealthyAlert(context.Background(), []string{"traefik", "authelia"})
	})

	t.Run("sends container list", func(t *testing.T) {
		alerter := newMockAlertSender()
		r, _ := newTestReconcilerWithAlerter(t, alerter)
		r.config.TargetHost = "root@host"

		containers := []string{"traefik", "authelia", "gatus"}
		r.sendUnhealthyAlert(context.Background(), containers)

		calls := alerter.getCalls("SendUnhealthyContainers")
		require.Len(t, calls, 1)
		assert.Equal(t, "root@host", calls[0].target)
		assert.Equal(t, containers, calls[0].containers)
	})

	t.Run("target defaults to local", func(t *testing.T) {
		alerter := newMockAlertSender()
		r, _ := newTestReconcilerWithAlerter(t, alerter)
		r.config.TargetHost = ""

		r.sendUnhealthyAlert(context.Background(), []string{"nginx"})

		calls := alerter.getCalls("SendUnhealthyContainers")
		require.Len(t, calls, 1)
		assert.Equal(t, "local", calls[0].target)
	})
}

// --- Phase 2D: Functional options tests ---

func TestWithAlerter(t *testing.T) {
	cfg := &Config{RepoBranch: "main", LockFile: filepath.Join(t.TempDir(), "lock")}
	alerter := newMockAlertSender()
	r := NewReconciler(cfg, WithAlerter(alerter))
	assert.NotNil(t, r.alerter)
}

func TestWithDeployOps(t *testing.T) {
	cfg := &Config{RepoBranch: "main", LockFile: filepath.Join(t.TempDir(), "lock")}
	deploy := &DeployOps{DryRun: true, ProjectName: "test-project"}
	r := NewReconciler(cfg, WithDeployOps(deploy))
	assert.True(t, r.deploy.DryRun)
	assert.Equal(t, "test-project", r.deploy.ProjectName)
}

func TestWithLockFile(t *testing.T) {
	cfg := &Config{RepoBranch: "main", LockFile: filepath.Join(t.TempDir(), "lock")}
	r := NewReconciler(cfg, WithLockFile("/custom/lock"))
	assert.Equal(t, "/custom/lock", r.lockFile)
}

func TestWithDockerClient(t *testing.T) {
	cfg := &Config{RepoBranch: "main", LockFile: filepath.Join(t.TempDir(), "lock")}
	r := NewReconciler(cfg, WithDockerClient(nil))
	assert.NotNil(t, r.dockerClientFn)
}

func TestWithDockerClientFunc(t *testing.T) {
	cfg := &Config{RepoBranch: "main", LockFile: filepath.Join(t.TempDir(), "lock")}
	called := false
	r := NewReconciler(cfg, WithDockerClientFunc(func() *docker.Client {
		called = true
		return nil
	}))
	assert.NotNil(t, r.dockerClientFn)
	r.dockerClientFn()
	assert.True(t, called)
}

func TestSetRunOptions(t *testing.T) {
	cfg := &Config{RepoBranch: "main", LockFile: filepath.Join(t.TempDir(), "lock")}
	r := NewReconciler(cfg)
	r.SetRunOptions("webhook:github", true)
	assert.Equal(t, "webhook:github", r.config.Source)
	assert.True(t, r.config.Force)
}

func TestWithGitOperations(t *testing.T) {
	cfg := &Config{RepoBranch: "main", LockFile: filepath.Join(t.TempDir(), "lock")}
	mock := &mockGitWithDiff{syncAfter: "test-commit"}
	r := NewReconciler(cfg, WithGitOperations(mock))
	assert.NotNil(t, r.git)
	// Verify it's our mock by calling Sync.
	_, _, after, _ := r.git.Sync(context.Background())
	assert.Equal(t, "test-commit", after)
}

func TestWithSecretsDecryptor(t *testing.T) {
	cfg := &Config{RepoBranch: "main", LockFile: filepath.Join(t.TempDir(), "lock")}
	mock := &mockSOPS{}
	r := NewReconciler(cfg, WithSecretsDecryptor(mock))
	assert.NotNil(t, r.sops)
}

// --- Cleanup staging tests ---

func TestCleanupStaging(t *testing.T) {
	t.Run("removes staging directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		stagingDir := filepath.Join(tmpDir, "staging")
		require.NoError(t, os.MkdirAll(stagingDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(stagingDir, "file.txt"), []byte("data"), 0644))

		cfg := &Config{StagingDir: stagingDir}
		r := NewReconciler(cfg)

		err := r.cleanupStaging()
		require.NoError(t, err)
		assert.NoDirExists(t, stagingDir)
	})

	t.Run("no-op in dry run", func(t *testing.T) {
		tmpDir := t.TempDir()
		stagingDir := filepath.Join(tmpDir, "staging")
		require.NoError(t, os.MkdirAll(stagingDir, 0755))

		cfg := &Config{StagingDir: stagingDir, DryRun: true}
		r := NewReconciler(cfg)

		err := r.cleanupStaging()
		require.NoError(t, err)
		assert.DirExists(t, stagingDir)
	})

	t.Run("no-op when staging dir is empty string", func(t *testing.T) {
		cfg := &Config{StagingDir: ""}
		r := NewReconciler(cfg)

		err := r.cleanupStaging()
		require.NoError(t, err)
	})

	t.Run("no error when staging dir does not exist", func(t *testing.T) {
		cfg := &Config{StagingDir: filepath.Join(t.TempDir(), "nonexistent")}
		r := NewReconciler(cfg)

		err := r.cleanupStaging()
		require.NoError(t, err)
	})
}

func TestMinLen(t *testing.T) {
	tests := []struct {
		name     string
		s        string
		n        int
		expected int
	}{
		{"string shorter than n", "abc", 5, 3},
		{"string equal to n", "abcde", 5, 5},
		{"string longer than n", "abcdefgh", 5, 5},
		{"empty string", "", 5, 0},
		{"n is zero", "abc", 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MinLen(tt.s, tt.n)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// --- Alert error path tests (cover the warn-on-failure branches) ---

func TestSendSuccessAlert_ErrorLogsWarning(t *testing.T) {
	alerter := newMockAlertSender()
	alerter.err = fmt.Errorf("notification service unavailable")
	r, _ := newTestReconcilerWithAlerter(t, alerter)
	r.lastCommit = "abc123"

	// Should not panic even when alerter returns error.
	r.sendSuccessAlert(context.Background())

	calls := alerter.getCalls("SendDeploySuccess")
	require.Len(t, calls, 1)
}

func TestSendRecoveryAlert_ErrorLogsWarning(t *testing.T) {
	alerter := newMockAlertSender()
	alerter.err = fmt.Errorf("notification service unavailable")
	r, _ := newTestReconcilerWithAlerter(t, alerter)
	r.lastCommit = "fix123"

	// Should not panic even when alerter returns error.
	r.sendRecoveryAlert(context.Background(), 3)

	calls := alerter.getCalls("SendDeployRecovery")
	require.Len(t, calls, 1)
}

func TestSendUnhealthyAlert_ErrorLogsWarning(t *testing.T) {
	alerter := newMockAlertSender()
	alerter.err = fmt.Errorf("notification service unavailable")
	r, _ := newTestReconcilerWithAlerter(t, alerter)

	// Should not panic even when alerter returns error.
	r.sendUnhealthyAlert(context.Background(), []string{"traefik"})

	calls := alerter.getCalls("SendUnhealthyContainers")
	require.Len(t, calls, 1)
}

func TestSendThrottledFailureAlert_ErrorLogsWarning(t *testing.T) {
	alerter := newMockAlertSender()
	alerter.err = fmt.Errorf("notification service unavailable")
	r, _ := newTestReconcilerWithAlerter(t, alerter)
	r.lastCommit = "abc123"

	state := &DeployState{AttemptCount: 1, LastAlertedAttempt: 0}
	r.sendThrottledFailureAlert(context.Background(), state, "test error")

	// Alert was attempted but failed, so LastAlertedAttempt should NOT be updated.
	assert.Equal(t, 0, state.LastAlertedAttempt)

	calls := alerter.getCalls("SendDeployFailure")
	require.Len(t, calls, 1)
}

func TestRun_GitSyncError(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &Config{
		RepoDir:    tmpDir,
		LockFile:   filepath.Join(tmpDir, "test.lock"),
		StateFile:  filepath.Join(tmpDir, "state.json"),
		RepoBranch: "main",
	}

	mockGit := &mockGitWithDiff{
		syncErr: fmt.Errorf("authentication failed"),
	}

	r := NewReconciler(cfg,
		WithGitOperations(mockGit),
		WithSecretsDecryptor(&mockSOPS{}),
	)

	err := r.Run(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to sync repository")
}

func TestRun_AlreadyDeployedSkips(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile := filepath.Join(tmpDir, "state.json")

	// Pre-seed state with the same commit already deployed.
	require.NoError(t, SaveState(stateFile, &DeployState{
		LastDeployedCommit: "abc123",
	}))

	cfg := &Config{
		RepoDir:    tmpDir,
		LockFile:   filepath.Join(tmpDir, "test.lock"),
		StateFile:  stateFile,
		RepoBranch: "main",
	}

	mockGit := &mockGitWithDiff{
		syncChanged: false,
		syncBefore:  "abc123",
		syncAfter:   "abc123",
	}

	r := NewReconciler(cfg,
		WithGitOperations(mockGit),
		WithSecretsDecryptor(&mockSOPS{}),
	)

	err := r.Run(context.Background())
	require.NoError(t, err)

	// State should remain unchanged -- no deploy happened.
	state := LoadState(stateFile)
	assert.Equal(t, "abc123", state.LastDeployedCommit)
	assert.Equal(t, 0, state.DeployCount)
}

func TestRun_CircuitBreakerSendsAlert(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile := filepath.Join(tmpDir, "state.json")

	require.NoError(t, SaveState(stateFile, &DeployState{
		LastAttemptedCommit: "bad-commit",
		AttemptCount:        MaxAttempts,
	}))

	alerter := newMockAlertSender()
	cfg := &Config{
		RepoDir:    tmpDir,
		LockFile:   filepath.Join(tmpDir, "test.lock"),
		StateFile:  stateFile,
		RepoBranch: "main",
	}

	mockGit := &mockGitWithDiff{
		syncChanged: false,
		syncBefore:  "bad-commit",
		syncAfter:   "bad-commit",
	}

	r := NewReconciler(cfg,
		WithGitOperations(mockGit),
		WithSecretsDecryptor(&mockSOPS{}),
		WithAlerter(alerter),
	)

	err := r.Run(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circuit breaker")

	// Alert should have been sent.
	calls := alerter.getCalls("SendDeployFailure")
	require.Len(t, calls, 1)
	assert.Contains(t, calls[0].reason, "circuit breaker")
}

func TestRun_DecryptSecretsError(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile := filepath.Join(tmpDir, "state.json")

	cfg := &Config{
		RepoDir:      tmpDir,
		LockFile:     filepath.Join(tmpDir, "test.lock"),
		StateFile:    stateFile,
		SecretsFiles: []string{"secrets.yaml"},
		RepoBranch:   "main",
	}

	mockGit := &mockGitWithDiff{
		syncChanged: true,
		syncBefore:  "aaa",
		syncAfter:   "bbb",
	}

	mockSops := &mockSOPS{err: fmt.Errorf("decryption key not found")}

	alerter := newMockAlertSender()
	r := NewReconciler(cfg,
		WithGitOperations(mockGit),
		WithSecretsDecryptor(mockSops),
		WithAlerter(alerter),
	)

	err := r.Run(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decrypt secrets")

	// Failure alert should fire.
	calls := alerter.getCalls("SendDeployFailure")
	require.Len(t, calls, 1)
}

func TestRun_RecoveryAlertSentAfterPreviousFailures(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile := filepath.Join(tmpDir, "state.json")
	repoDir := filepath.Join(tmpDir, "repo")
	appdataDir := filepath.Join(tmpDir, "appdata")
	require.NoError(t, os.MkdirAll(repoDir, 0755))
	require.NoError(t, os.MkdirAll(appdataDir, 0755))

	// Pre-seed state with 2 attempts on this commit (second attempt succeeds).
	require.NoError(t, SaveState(stateFile, &DeployState{
		LastAttemptedCommit: "fix-commit",
		AttemptCount:        2,
	}))

	cfg := &Config{
		RepoDir:          repoDir,
		LockFile:         filepath.Join(tmpDir, "test.lock"),
		StateFile:        stateFile,
		StagingDir:       filepath.Join(tmpDir, "staging"),
		BackupDir:        filepath.Join(tmpDir, "backups"),
		LocalAppdataPath: appdataDir,
		InfraSubDir:      ".",
		DryRun:           true,
		RepoBranch:       "main",
	}

	mockGit := &mockGitWithDiff{
		syncChanged: false,
		syncBefore:  "fix-commit",
		syncAfter:   "fix-commit",
	}

	alerter := newMockAlertSender()
	r := NewReconciler(cfg,
		WithGitOperations(mockGit),
		WithSecretsDecryptor(&mockSOPS{}),
		WithAlerter(alerter),
	)

	err := r.Run(context.Background())
	require.NoError(t, err)

	// Recovery alert should be sent because AttemptCount was > 1.
	calls := alerter.getCalls("SendDeployRecovery")
	require.Len(t, calls, 1)
	assert.Equal(t, 2, calls[0].priorFailures)
}

func TestExecutePostSyncHooks_NoChangedFiles_Skips(t *testing.T) {
	cfg := &Config{
		PostSyncHooks: []PostSyncHook{
			{Paths: []string{"**"}, Action: "restart", Container: "traefik"},
		},
	}

	mockGitOps := &mockGitWithDiff{
		diffFiles: []string{}, // Empty diff -- no changed files.
	}
	dockerCalled := false
	r := NewReconciler(cfg, WithGitOperations(mockGitOps))
	r.dockerClientFn = func() *docker.Client {
		dockerCalled = true
		return nil
	}

	r.executePostSyncHooks(context.Background(), "abc123", "def456", nil)

	assert.False(t, dockerCalled, "hooks should be skipped when no files changed")
}

func TestExecutePostSyncHooks_NoMatchingHooks_Skips(t *testing.T) {
	cfg := &Config{
		PostSyncHooks: []PostSyncHook{
			{Paths: []string{"traefik/**"}, Action: "restart", Container: "traefik"},
		},
	}

	mockGitOps := &mockGitWithDiff{
		diffFiles: []string{"docs/README.md"}, // Changed files don't match hooks.
	}
	dockerCalled := false
	r := NewReconciler(cfg, WithGitOperations(mockGitOps))
	r.dockerClientFn = func() *docker.Client {
		dockerCalled = true
		return nil
	}

	r.executePostSyncHooks(context.Background(), "abc123", "def456", nil)

	assert.False(t, dockerCalled, "docker client should not be called when no hooks match")
}

func TestNewReconciler_DefaultLockFile(t *testing.T) {
	cfg := &Config{RepoBranch: "main"}
	r := NewReconciler(cfg)
	assert.Equal(t, DefaultLockFile, r.lockFile)
}

func TestNewReconciler_CustomLockFile(t *testing.T) {
	cfg := &Config{RepoBranch: "main", LockFile: "/custom/path/lock"}
	r := NewReconciler(cfg)
	assert.Equal(t, "/custom/path/lock", r.lockFile)
}

func TestDeployLocal_DryRun(t *testing.T) {
	tmpDir := t.TempDir()
	stagingDir := filepath.Join(tmpDir, "staging")
	appdataDir := filepath.Join(tmpDir, "appdata")

	// Create staging structure matching what the pipeline produces.
	unraidDir := filepath.Join(stagingDir, "unraid")
	require.NoError(t, os.MkdirAll(filepath.Join(unraidDir, "appdata", "traefik"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(unraidDir, "appdata", "agentgateway"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(unraidDir, "appdata", "authelia"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(unraidDir, "appdata", "gatus"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(unraidDir, "appdata", "tailscale-gateway"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(unraidDir, "compose"), 0755))

	// Write minimal files.
	require.NoError(t, os.WriteFile(filepath.Join(unraidDir, "appdata", "traefik", "traefik.yml"), []byte("entryPoints: {}"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(unraidDir, "appdata", "agentgateway", "config.yaml"), []byte("{}"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(unraidDir, "appdata", "authelia", "configuration.yml"), []byte("{}"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(unraidDir, "appdata", "gatus", "config.yaml"), []byte("{}"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(unraidDir, "appdata", "tailscale-gateway", "serve.json"), []byte("{}"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(unraidDir, "compose", "core.yml"), []byte("services: {}"), 0644))

	// Create appdata dir so isLocalMode returns true.
	require.NoError(t, os.MkdirAll(appdataDir, 0755))

	cfg := &Config{
		StagingDir:       stagingDir,
		LocalAppdataPath: appdataDir,
		DryRun:           true,
	}
	r := NewReconciler(cfg)

	result, err := r.deployLocal(context.Background())
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestDeployLocal_FullPipeline(t *testing.T) {
	tmpDir := t.TempDir()
	stagingDir := filepath.Join(tmpDir, "staging")
	appdataDir := filepath.Join(tmpDir, "appdata")

	// Create staging structure.
	unraidDir := filepath.Join(stagingDir, "unraid")
	require.NoError(t, os.MkdirAll(filepath.Join(unraidDir, "appdata", "traefik"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(unraidDir, "appdata", "agentgateway"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(unraidDir, "appdata", "authelia"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(unraidDir, "appdata", "gatus"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(unraidDir, "appdata", "tailscale-gateway"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(unraidDir, "compose"), 0755))

	require.NoError(t, os.WriteFile(filepath.Join(unraidDir, "appdata", "traefik", "traefik.yml"), []byte("entryPoints: {}"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(unraidDir, "appdata", "agentgateway", "config.yaml"), []byte("{}"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(unraidDir, "appdata", "authelia", "configuration.yml"), []byte("{}"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(unraidDir, "appdata", "gatus", "config.yaml"), []byte("{}"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(unraidDir, "appdata", "tailscale-gateway", "serve.json"), []byte("{}"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(unraidDir, "compose", "core.yml"), []byte("services:\n  web:\n    image: nginx:latest"), 0644))

	require.NoError(t, os.MkdirAll(appdataDir, 0755))

	cfg := &Config{
		StagingDir:       stagingDir,
		LocalAppdataPath: appdataDir,
		DryRun:           false,
	}
	d := NewDeployOps(false, "")
	d.ContentHashSync = true
	r := NewReconciler(cfg, WithDeployOps(d))

	result, err := r.deployLocal(context.Background())
	// It will fail at ComposeUpMultipleWithRollback since docker isn't available,
	// but all the DeployLocal/DeployLocalFile calls should succeed.
	if err != nil {
		// Expected: fails at compose up stage.
		assert.Contains(t, err.Error(), "service reload")
	} else {
		assert.NotNil(t, result)
	}

	// Verify files were deployed to appdata.
	assert.FileExists(t, filepath.Join(appdataDir, "traefik", "traefik.yml"))
	assert.FileExists(t, filepath.Join(appdataDir, "agentgateway", "config.yaml"))
	assert.FileExists(t, filepath.Join(appdataDir, "authelia", "configuration.yml"))
	assert.FileExists(t, filepath.Join(appdataDir, "gatus", "config.yaml"))
}

func TestDecryptSecrets_MultipleFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create fake secrets files (they exist but won't decrypt without SOPS keys).
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "secret1.yaml"), []byte("key: val"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "secret2.yaml"), []byte("key: val"), 0644))

	cfg := &Config{
		RepoDir:      tmpDir,
		SecretsFiles: []string{"secret1.yaml", "secret2.yaml"},
	}
	r := NewReconciler(cfg, WithSecretsDecryptor(&mockSOPS{}))

	secrets, err := r.decryptSecrets(context.TODO())
	require.NoError(t, err)
	assert.NotNil(t, secrets)
}

func TestSaveState_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "state.json")

	state := &DeployState{
		LastDeployedCommit:  "abc123",
		LastAttemptedCommit: "abc123",
		AttemptCount:        0,
		DeployCount:         5,
		Source:              "webhook",
	}

	require.NoError(t, SaveState(path, state))

	loaded := LoadState(path)
	assert.Equal(t, "abc123", loaded.LastDeployedCommit)
	assert.Equal(t, 5, loaded.DeployCount)
	assert.Equal(t, "webhook", loaded.Source)
}

func TestLoadState_CorruptJSON(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "state.json")

	require.NoError(t, os.WriteFile(path, []byte("not valid json{{{"), 0644))

	state := LoadState(path)
	// Should return zero state on corrupt data.
	assert.Equal(t, 0, state.DeployCount)
	assert.Empty(t, state.LastDeployedCommit)
}

func TestRenderTemplates_WithInfraSubDir(t *testing.T) {
	tmpDir := t.TempDir()
	stagingDir := filepath.Join(tmpDir, "staging")
	repoDir := filepath.Join(tmpDir, "repo")

	// Create repo with infra subdirectory.
	infraDir := filepath.Join(repoDir, "infra")
	require.NoError(t, os.MkdirAll(filepath.Join(infraDir, "unraid", "compose"), 0755))
	require.NoError(t, os.WriteFile(
		filepath.Join(infraDir, "unraid", "compose", "core.yml.tmpl"),
		[]byte(`services:
  web:
    image: nginx:{{ .version }}
`),
		0644,
	))

	cfg := &Config{
		RepoDir:     repoDir,
		StagingDir:  stagingDir,
		InfraSubDir: "infra",
	}
	r := NewReconciler(cfg)

	secrets := map[string]any{"version": "1.25"}
	err := r.renderTemplates(context.TODO(), secrets)
	require.NoError(t, err)

	// Rendered file should exist in staging.
	rendered, err := os.ReadFile(filepath.Join(stagingDir, "unraid", "compose", "core.yml"))
	require.NoError(t, err)
	assert.Contains(t, string(rendered), "nginx:1.25")
}

func TestRenderTemplates_NonExistentInfra(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &Config{
		RepoDir:     filepath.Join(tmpDir, "repo"),
		StagingDir:  filepath.Join(tmpDir, "staging"),
		InfraSubDir: "nonexistent",
	}
	r := NewReconciler(cfg)

	err := r.renderTemplates(context.TODO(), map[string]any{})
	assert.Error(t, err)
}

func TestCreateBackup_LocalMode(t *testing.T) {
	tmpDir := t.TempDir()
	backupDir := filepath.Join(tmpDir, "backups")
	appdataDir := filepath.Join(tmpDir, "appdata")

	// Create appdata structure that the backup expects.
	require.NoError(t, os.MkdirAll(filepath.Join(appdataDir, "traefik"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(appdataDir, "traefik", "traefik.yml"), []byte("config"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(appdataDir, "authelia"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(appdataDir, "authelia", "configuration.yml"), []byte("config"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(appdataDir, "agentgateway"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(appdataDir, "agentgateway", "config.yaml"), []byte("config"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(appdataDir, "gatus"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(appdataDir, "gatus", "config.yaml"), []byte("config"), 0644))

	cfg := &Config{
		LocalAppdataPath: appdataDir,
		BackupDir:        backupDir,
		BackupsToKeep:    3,
	}
	r := NewReconciler(cfg)

	err := r.createBackup(context.Background(), nil)
	require.NoError(t, err)

	// Backup should have been created.
	assert.NotEmpty(t, r.lastBackupPath)
	assert.DirExists(t, r.lastBackupPath)
}

func TestWithDockerClient_Nil(t *testing.T) {
	cfg := &Config{RepoBranch: "main"}
	r := NewReconciler(cfg, WithDockerClient(nil))

	// The function wrapper should return nil when called.
	assert.NotNil(t, r.dockerClientFn)
	assert.Nil(t, r.dockerClientFn())
}

func TestDecryptSecrets_SuccessfulDecrypt(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a file that the mock SOPS can "decrypt".
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "secrets.yaml"), []byte("encrypted: true"), 0644))

	// Mock SOPS that returns specific secrets.
	mockSops := &mockSOPSWithSecrets{
		secrets: map[string]any{
			"network": map[string]any{
				"unraid_ip": "192.168.1.100",
			},
		},
	}

	cfg := &Config{
		RepoDir:      tmpDir,
		SecretsFiles: []string{"secrets.yaml"},
	}
	r := NewReconciler(cfg, WithSecretsDecryptor(mockSops))

	secrets, err := r.decryptSecrets(context.TODO())
	require.NoError(t, err)
	assert.NotNil(t, secrets)

	network, ok := secrets["network"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "192.168.1.100", network["unraid_ip"])
}

// mockSOPSWithSecrets returns configurable secrets.
type mockSOPSWithSecrets struct {
	secrets map[string]any
	err     error
}

func (m *mockSOPSWithSecrets) DecryptFiles(_ context.Context, _ []string) (map[string]any, error) {
	return m.secrets, m.err
}

func (m *mockSOPSWithSecrets) CheckAgeKey() error { return nil }

func TestReloadProjectConfig_NilReturned(t *testing.T) {
	cfg := &Config{
		PostSyncHooks: []PostSyncHook{
			{Paths: []string{"keep/**"}, Action: "restart", Container: "keep"},
		},
	}
	cfg.ConfigReloader = func(dir string) (*ReloadedConfig, error) {
		return nil, nil
	}
	r := NewReconciler(cfg)

	r.reloadProjectConfig()

	// Config should be unchanged.
	require.Len(t, r.config.PostSyncHooks, 1)
	assert.Equal(t, "keep", r.config.PostSyncHooks[0].Container)
}

func TestSaveState_NonExistentDir(t *testing.T) {
	// Saving to a directory that doesn't exist should fail.
	path := filepath.Join("/nonexistent", "deep", "path", "state.json")
	err := SaveState(path, &DeployState{LastDeployedCommit: "abc"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create temp state file")
}

func TestSaveState_CompleteFields(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "state.json")

	now := time.Now().Truncate(time.Second)
	state := &DeployState{
		LastDeployedCommit:  "commit-123",
		DeployedAt:          now,
		Source:              "webhook:github",
		LastAttemptedCommit: "commit-123",
		AttemptCount:        0,
		LastAlertedAttempt:  0,
		DeployCount:         42,
		DeclaredServices: []DeclaredService{
			{Name: "web", Image: "nginx:latest"},
		},
		DriftItems: []DriftItem{
			{Service: "api", Type: DriftMissing},
		},
	}

	require.NoError(t, SaveState(path, state))

	loaded := LoadState(path)
	assert.Equal(t, "commit-123", loaded.LastDeployedCommit)
	assert.Equal(t, 42, loaded.DeployCount)
	assert.Equal(t, "webhook:github", loaded.Source)
	assert.Len(t, loaded.DeclaredServices, 1)
	assert.Len(t, loaded.DriftItems, 1)
}

func TestCleanupStaging_StagingDirNotSet(t *testing.T) {
	cfg := &Config{StagingDir: ""}
	r := NewReconciler(cfg)
	err := r.cleanupStaging()
	assert.NoError(t, err)
}
