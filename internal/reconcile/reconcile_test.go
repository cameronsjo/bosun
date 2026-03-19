package reconcile

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

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

func TestWithDeployOps(t *testing.T) {
	cfg := DefaultConfig()
	deploy := NewDeployOps(false, "test-project")
	r := NewReconciler(cfg, WithDeployOps(deploy))
	assert.Equal(t, deploy, r.deploy)
}

func TestWithLockFile(t *testing.T) {
	cfg := DefaultConfig()
	r := NewReconciler(cfg, WithLockFile("/custom/lock/path"))
	assert.Equal(t, "/custom/lock/path", r.lockFile)
}

func TestWithAlerter(t *testing.T) {
	cfg := DefaultConfig()
	alerter := &mockAlertSender{}
	r := NewReconciler(cfg, WithAlerter(alerter))
	assert.Equal(t, alerter, r.alerter)
}

func TestWithDockerClient(t *testing.T) {
	cfg := DefaultConfig()
	mockAPI := newReconcileMockDockerAPI()
	client := docker.NewClientWithAPI(mockAPI)

	r := NewReconciler(cfg, WithDockerClient(client))
	assert.NotNil(t, r.dockerClientFn)
	assert.Equal(t, client, r.dockerClientFn())
}

func TestWithDockerClientFunc(t *testing.T) {
	cfg := DefaultConfig()
	called := false
	fn := func() *docker.Client {
		called = true
		return nil
	}

	r := NewReconciler(cfg, WithDockerClientFunc(fn))
	assert.NotNil(t, r.dockerClientFn)
	r.dockerClientFn()
	assert.True(t, called)
}

func TestSetRunOptions(t *testing.T) {
	cfg := DefaultConfig()
	r := NewReconciler(cfg)

	r.SetRunOptions("webhook:github", true)
	assert.Equal(t, "webhook:github", r.config.Source)
	assert.True(t, r.config.Force)
}

func TestCleanupStaging(t *testing.T) {
	t.Run("removes staging directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		stagingDir := filepath.Join(tmpDir, "staging")
		require.NoError(t, os.MkdirAll(filepath.Join(stagingDir, "compose"), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(stagingDir, "compose", "test.yml"), []byte("test"), 0644))

		cfg := &Config{StagingDir: stagingDir}
		r := NewReconciler(cfg)

		_ = r.cleanupStaging()
		assert.NoDirExists(t, stagingDir)
	})

	t.Run("no-op when staging dir does not exist", func(t *testing.T) {
		cfg := &Config{StagingDir: "/nonexistent/staging/dir"}
		r := NewReconciler(cfg)

		// Should not panic or error on missing dir.
		_ = r.cleanupStaging()
	})
}

// mockAlertSender implements AlertSender for testing.
type mockAlertSender struct {
	deploySuccessCalls     int
	deployFailureCalls     int
	deployRecoveryCalls    int
	unhealthyContainerCall int
	rollbackSuccessCalls   int
	rollbackFailureCalls   int
	lastErr                error
	lastSuccessServices    []string
	lastSuccessDuration    time.Duration
	lastFailureServices    []string
	lastFailureDuration    time.Duration
}

func (m *mockAlertSender) SendDeploySuccess(_ context.Context, _, _ string, services []string, duration time.Duration) error {
	m.deploySuccessCalls++
	m.lastSuccessServices = services
	m.lastSuccessDuration = duration
	return m.lastErr
}
func (m *mockAlertSender) SendDeployFailure(_ context.Context, _, _, _ string, services []string, duration time.Duration) error {
	m.deployFailureCalls++
	m.lastFailureServices = services
	m.lastFailureDuration = duration
	return m.lastErr
}
func (m *mockAlertSender) SendDeployRecovery(_ context.Context, _, _ string, _ int) error {
	m.deployRecoveryCalls++
	return m.lastErr
}
func (m *mockAlertSender) SendUnhealthyContainers(_ context.Context, _ string, _ []string) error {
	m.unhealthyContainerCall++
	return m.lastErr
}
func (m *mockAlertSender) SendRollbackSuccess(_ context.Context, _, _ string) error {
	m.rollbackSuccessCalls++
	return m.lastErr
}
func (m *mockAlertSender) SendRollbackFailure(_ context.Context, _, _ string) error {
	m.rollbackFailureCalls++
	return m.lastErr
}

func TestSendSuccessAlert(t *testing.T) {
	t.Run("no alerter is no-op", func(t *testing.T) {
		cfg := DefaultConfig()
		r := NewReconciler(cfg)
		r.sendSuccessAlert(context.Background()) // Should not panic
	})

	t.Run("calls alerter with target", func(t *testing.T) {
		alerter := &mockAlertSender{}
		cfg := &Config{TargetHost: "user@host", OnSuccess: true}
		r := NewReconciler(cfg, WithAlerter(alerter))
		r.lastCommit = "abc123"
		r.sendSuccessAlert(context.Background())
		assert.Equal(t, 1, alerter.deploySuccessCalls)
	})

	t.Run("uses local when target host is empty", func(t *testing.T) {
		alerter := &mockAlertSender{}
		cfg := &Config{OnSuccess: true}
		r := NewReconciler(cfg, WithAlerter(alerter))
		r.sendSuccessAlert(context.Background())
		assert.Equal(t, 1, alerter.deploySuccessCalls)
	})

	t.Run("alert error is logged not returned", func(t *testing.T) {
		alerter := &mockAlertSender{lastErr: fmt.Errorf("send failed")}
		cfg := &Config{OnSuccess: true}
		r := NewReconciler(cfg, WithAlerter(alerter))
		r.sendSuccessAlert(context.Background()) // Should not panic
		assert.Equal(t, 1, alerter.deploySuccessCalls)
	})

	t.Run("passes services and duration", func(t *testing.T) {
		alerter := &mockAlertSender{}
		cfg := &Config{OnSuccess: true}
		r := NewReconciler(cfg, WithAlerter(alerter))
		r.lastCommit = "abc123"
		r.runStartTime = time.Now().Add(-30 * time.Second)
		r.declaredServices = []DeclaredService{
			{Name: "traefik", Image: "traefik:v3"},
			{Name: "authelia", Image: "authelia:latest"},
		}
		r.sendSuccessAlert(context.Background())
		assert.Equal(t, 1, alerter.deploySuccessCalls)
		assert.Equal(t, []string{"traefik", "authelia"}, alerter.lastSuccessServices)
		assert.GreaterOrEqual(t, alerter.lastSuccessDuration, 29*time.Second)
		assert.Less(t, alerter.lastSuccessDuration, 31*time.Second)
	})

	t.Run("nil services when no declared services", func(t *testing.T) {
		alerter := &mockAlertSender{}
		cfg := &Config{OnSuccess: true}
		r := NewReconciler(cfg, WithAlerter(alerter))
		r.lastCommit = "abc123"
		r.runStartTime = time.Now()
		r.sendSuccessAlert(context.Background())
		assert.Equal(t, 1, alerter.deploySuccessCalls)
		assert.Nil(t, alerter.lastSuccessServices)
	})
}

func TestSendThrottledFailureAlert(t *testing.T) {
	t.Run("no alerter is no-op", func(t *testing.T) {
		cfg := DefaultConfig()
		r := NewReconciler(cfg)
		state := &DeployState{AttemptCount: 1}
		r.sendThrottledFailureAlert(context.Background(), state, "test error")
	})

	t.Run("sends on first attempt", func(t *testing.T) {
		tmpDir := t.TempDir()
		stateFile := filepath.Join(tmpDir, "state.json")

		alerter := &mockAlertSender{}
		cfg := &Config{StateFile: stateFile, OnFailure: true}
		r := NewReconciler(cfg, WithAlerter(alerter))
		r.lastCommit = "abc123"

		state := &DeployState{AttemptCount: 1, LastAlertedAttempt: 0}
		r.sendThrottledFailureAlert(context.Background(), state, "deploy failed")
		assert.Equal(t, 1, alerter.deployFailureCalls)
		assert.Equal(t, 1, state.LastAlertedAttempt)
	})

	t.Run("throttled on second attempt", func(t *testing.T) {
		alerter := &mockAlertSender{}
		cfg := &Config{OnFailure: true}
		r := NewReconciler(cfg, WithAlerter(alerter))

		state := &DeployState{AttemptCount: 2, LastAlertedAttempt: 1}
		r.sendThrottledFailureAlert(context.Background(), state, "deploy failed")
		assert.Equal(t, 0, alerter.deployFailureCalls, "should be throttled")
	})

	t.Run("passes services and duration", func(t *testing.T) {
		tmpDir := t.TempDir()
		stateFile := filepath.Join(tmpDir, "state.json")

		alerter := &mockAlertSender{}
		cfg := &Config{StateFile: stateFile, OnFailure: true}
		r := NewReconciler(cfg, WithAlerter(alerter))
		r.lastCommit = "abc123"
		r.runStartTime = time.Now().Add(-15 * time.Second)
		r.declaredServices = []DeclaredService{
			{Name: "nginx", Image: "nginx:latest"},
		}

		state := &DeployState{AttemptCount: 1, LastAlertedAttempt: 0}
		r.sendThrottledFailureAlert(context.Background(), state, "compose up failed")
		assert.Equal(t, 1, alerter.deployFailureCalls)
		assert.Equal(t, []string{"nginx"}, alerter.lastFailureServices)
		assert.GreaterOrEqual(t, alerter.lastFailureDuration, 14*time.Second)
		assert.Less(t, alerter.lastFailureDuration, 16*time.Second)
	})
}

func TestSendUnhealthyAlert(t *testing.T) {
	t.Run("no alerter is no-op", func(t *testing.T) {
		cfg := DefaultConfig()
		r := NewReconciler(cfg)
		r.sendUnhealthyAlert(context.Background(), []string{"web"})
	})

	t.Run("sends alert with containers", func(t *testing.T) {
		alerter := &mockAlertSender{}
		cfg := &Config{TargetHost: "user@host"}
		r := NewReconciler(cfg, WithAlerter(alerter))

		r.sendUnhealthyAlert(context.Background(), []string{"web", "api"})
		assert.Equal(t, 1, alerter.unhealthyContainerCall)
	})

	t.Run("alert error is logged not returned", func(t *testing.T) {
		alerter := &mockAlertSender{lastErr: fmt.Errorf("send failed")}
		cfg := &Config{}
		r := NewReconciler(cfg, WithAlerter(alerter))
		r.sendUnhealthyAlert(context.Background(), []string{"web"})
		assert.Equal(t, 1, alerter.unhealthyContainerCall)
	})
}

func TestSendRecoveryAlert(t *testing.T) {
	t.Run("no alerter is no-op", func(t *testing.T) {
		cfg := DefaultConfig()
		r := NewReconciler(cfg)
		r.sendRecoveryAlert(context.Background(), 3)
	})

	t.Run("sends recovery alert", func(t *testing.T) {
		alerter := &mockAlertSender{}
		cfg := &Config{TargetHost: "user@host", OnSuccess: true}
		r := NewReconciler(cfg, WithAlerter(alerter))
		r.lastCommit = "def456"

		r.sendRecoveryAlert(context.Background(), 5)
		assert.Equal(t, 1, alerter.deployRecoveryCalls)
	})

	t.Run("suppressed when OnSuccess is false", func(t *testing.T) {
		alerter := &mockAlertSender{}
		cfg := &Config{TargetHost: "user@host", OnSuccess: false}
		r := NewReconciler(cfg, WithAlerter(alerter))
		r.lastCommit = "def456"

		r.sendRecoveryAlert(context.Background(), 5)
		assert.Equal(t, 0, alerter.deployRecoveryCalls)
	})

	t.Run("alert error is logged not returned", func(t *testing.T) {
		alerter := &mockAlertSender{lastErr: fmt.Errorf("send failed")}
		cfg := &Config{OnSuccess: true}
		r := NewReconciler(cfg, WithAlerter(alerter))
		r.sendRecoveryAlert(context.Background(), 2)
		assert.Equal(t, 1, alerter.deployRecoveryCalls)
	})
}

// --- Mock implementations for Reconciler workflow tests ---

// mockGitOps implements GitOperations for testing.
type mockGitOps struct {
	syncChanged bool
	syncBefore  string
	syncAfter   string
	syncErr     error
	isRepoVal   bool
	diffFiles   []string
	diffErr     error
}

func (m *mockGitOps) Sync(_ context.Context) (bool, string, string, error) {
	return m.syncChanged, m.syncBefore, m.syncAfter, m.syncErr
}

func (m *mockGitOps) IsRepo(_ context.Context) bool {
	return m.isRepoVal
}

func (m *mockGitOps) DiffFiles(_ context.Context, _, _ string) ([]string, error) {
	return m.diffFiles, m.diffErr
}

// mockSecretsDecryptor implements SecretsDecryptor for testing.
type mockSecretsDecryptor struct {
	decryptResult map[string]any
	decryptErr    error
	checkAgeErr   error
}

func (m *mockSecretsDecryptor) DecryptFiles(_ context.Context, _ []string) (map[string]any, error) {
	return m.decryptResult, m.decryptErr
}

func (m *mockSecretsDecryptor) CheckAgeKey() error {
	return m.checkAgeErr
}

// --- Reconciler.verifyPostDeploy tests ---

func TestVerifyPostDeploy(t *testing.T) {
	t.Run("disabled when HealthCheckTimeout is zero", func(t *testing.T) {
		tmpDir := t.TempDir()
		stateFile := filepath.Join(tmpDir, "state.json")

		cfg := &Config{
			StateFile:          stateFile,
			HealthCheckTimeout: 0, // Disabled
		}
		r := NewReconciler(cfg)
		r.declaredServices = []DeclaredService{{Name: "web", Image: "nginx"}}

		mockAPI := newReconcileMockDockerAPI()
		client := docker.NewClientWithAPI(mockAPI)
		state := &DeployState{}

		err := r.verifyPostDeploy(context.Background(), state, client)
		assert.NoError(t, err)
		assert.True(t, state.HealthVerifiedAt.IsZero())
	})

	t.Run("all healthy returns nil", func(t *testing.T) {
		tmpDir := t.TempDir()
		stateFile := filepath.Join(tmpDir, "state.json")

		cfg := &Config{
			StateFile:           stateFile,
			ProjectName:         "test",
			HealthCheckTimeout:  10 * time.Second,
			HealthCheckInterval: 100 * time.Millisecond,
		}
		r := NewReconciler(cfg)
		r.declaredServices = []DeclaredService{{Name: "web", Image: "nginx:latest"}}

		mockAPI := newReconcileMockDockerAPI()
		mockAPI.containerListFunc = func(ctx context.Context, options container.ListOptions) ([]container.Summary, error) {
			return []container.Summary{
				{
					ID:    "abcdef123456abcdef",
					Names: []string{"/test-web-1"},
					Image: "nginx:latest",
					State: "running",
					Labels: map[string]string{
						"com.docker.compose.project": "test",
						"com.docker.compose.service": "web",
					},
				},
			}, nil
		}
		client := docker.NewClientWithAPI(mockAPI)
		state := &DeployState{}

		err := r.verifyPostDeploy(context.Background(), state, client)
		assert.NoError(t, err)
		assert.True(t, state.HealthVerificationPassed)
		assert.False(t, state.HealthVerifiedAt.IsZero())
	})

	t.Run("unhealthy timeout returns error", func(t *testing.T) {
		tmpDir := t.TempDir()
		stateFile := filepath.Join(tmpDir, "state.json")

		cfg := &Config{
			StateFile:           stateFile,
			ProjectName:         "test",
			HealthCheckTimeout:  300 * time.Millisecond,
			HealthCheckInterval: 50 * time.Millisecond,
		}
		r := NewReconciler(cfg)
		r.declaredServices = []DeclaredService{
			{Name: "web", Image: "nginx:latest"},
			{Name: "missing-svc", Image: "alpine:latest"},
		}

		mockAPI := newReconcileMockDockerAPI()
		mockAPI.containerListFunc = func(ctx context.Context, options container.ListOptions) ([]container.Summary, error) {
			return []container.Summary{
				{
					ID:    "abcdef123456abcdef",
					Names: []string{"/test-web-1"},
					Image: "nginx:latest",
					State: "running",
					Labels: map[string]string{
						"com.docker.compose.project": "test",
						"com.docker.compose.service": "web",
					},
				},
				// missing-svc not present = unhealthy
			}, nil
		}
		client := docker.NewClientWithAPI(mockAPI)
		state := &DeployState{}

		err := r.verifyPostDeploy(context.Background(), state, client)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "missing-svc")
		assert.False(t, state.HealthVerificationPassed)
		assert.False(t, state.HealthVerifiedAt.IsZero())
	})

	t.Run("context cancelled returns error", func(t *testing.T) {
		tmpDir := t.TempDir()
		stateFile := filepath.Join(tmpDir, "state.json")

		cfg := &Config{
			StateFile:           stateFile,
			HealthCheckTimeout:  10 * time.Second,
			HealthCheckInterval: 100 * time.Millisecond,
		}
		r := NewReconciler(cfg)
		r.declaredServices = []DeclaredService{{Name: "web", Image: "nginx"}}

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		mockAPI := newReconcileMockDockerAPI()
		client := docker.NewClientWithAPI(mockAPI)
		state := &DeployState{}

		err := r.verifyPostDeploy(ctx, state, client)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cancelled")
	})
}

// --- Reconciler.executePostSyncHooks tests ---

func TestReconcilerExecutePostSyncHooks(t *testing.T) {
	t.Run("first deploy skips hooks (empty previous commit)", func(t *testing.T) {
		cfg := &Config{
			PostSyncHooks: []PostSyncHook{
				{Container: "traefik", Paths: []string{"traefik/**"}, Action: "restart"},
			},
		}
		r := NewReconciler(cfg)
		r.dockerClientFn = func() *docker.Client { return nil }

		// Should not panic -- previous commit is empty
		r.executePostSyncHooks(context.Background(), "", "abc123", nil)
	})

	t.Run("no changed files skips hooks", func(t *testing.T) {
		gitOps := &mockGitOps{diffFiles: []string{}}
		cfg := &Config{
			PostSyncHooks: []PostSyncHook{
				{Container: "traefik", Paths: []string{"traefik/**"}, Action: "restart"},
			},
		}
		r := NewReconciler(cfg, WithGitOperations(gitOps))

		r.executePostSyncHooks(context.Background(), "aaa", "bbb", nil)
		// No crash, no hooks fired
	})

	t.Run("uses deploy result written files instead of git diff", func(t *testing.T) {
		restartCalled := false
		mockAPI := newReconcileMockDockerAPI()
		mockAPI.containerRestartFunc = func(ctx context.Context, cID string, opts container.StopOptions) error {
			restartCalled = true
			return nil
		}
		client := docker.NewClientWithAPI(mockAPI)

		cfg := &Config{
			PostSyncHooks: []PostSyncHook{
				{Container: "traefik", Paths: []string{"traefik/**"}, Action: "restart"},
			},
		}
		r := NewReconciler(cfg)
		r.dockerClientFn = func() *docker.Client { return client }

		result := &DeployResult{WrittenFiles: []string{"traefik/dynamic.yml"}}
		r.executePostSyncHooks(context.Background(), "aaa", "bbb", result)
		assert.True(t, restartCalled)
	})

	t.Run("docker client unavailable is non-fatal", func(t *testing.T) {
		gitOps := &mockGitOps{diffFiles: []string{"traefik/dynamic.yml"}}
		cfg := &Config{
			PostSyncHooks: []PostSyncHook{
				{Container: "traefik", Paths: []string{"traefik/**"}, Action: "restart"},
			},
		}
		r := NewReconciler(cfg, WithGitOperations(gitOps))
		r.dockerClientFn = func() *docker.Client { return nil }

		r.executePostSyncHooks(context.Background(), "aaa", "bbb", nil)
		// Should not panic
	})

	t.Run("diff failure fires all hooks unconditionally", func(t *testing.T) {
		restartCalled := false
		mockAPI := newReconcileMockDockerAPI()
		mockAPI.containerRestartFunc = func(ctx context.Context, cID string, opts container.StopOptions) error {
			restartCalled = true
			return nil
		}
		client := docker.NewClientWithAPI(mockAPI)

		gitOps := &mockGitOps{diffErr: fmt.Errorf("shallow clone")}
		cfg := &Config{
			PostSyncHooks: []PostSyncHook{
				{Container: "traefik", Paths: []string{"traefik/**"}, Action: "restart"},
			},
		}
		r := NewReconciler(cfg, WithGitOperations(gitOps))
		r.dockerClientFn = func() *docker.Client { return client }

		r.executePostSyncHooks(context.Background(), "aaa", "bbb", nil)
		assert.True(t, restartCalled)
	})
}

// --- Reconciler.reloadProjectConfig tests ---

func TestReloadProjectConfig(t *testing.T) {
	t.Run("nil reloader is no-op", func(t *testing.T) {
		cfg := &Config{}
		r := NewReconciler(cfg)
		r.reloadProjectConfig() // Should not panic
	})

	t.Run("reloader error keeps existing config", func(t *testing.T) {
		cfg := &Config{
			PostSyncHooks: []PostSyncHook{{Container: "orig"}},
			ConfigReloader: func(dir string) (*ReloadedConfig, error) {
				return nil, fmt.Errorf("parse error")
			},
		}
		r := NewReconciler(cfg)
		r.reloadProjectConfig()
		assert.Equal(t, "orig", r.config.PostSyncHooks[0].Container)
	})

	t.Run("reloader returns nil is no-op", func(t *testing.T) {
		cfg := &Config{
			ConfigReloader: func(dir string) (*ReloadedConfig, error) {
				return nil, nil
			},
		}
		r := NewReconciler(cfg)
		r.reloadProjectConfig()
	})

	t.Run("reloader updates hooks from repo", func(t *testing.T) {
		cfg := &Config{
			ConfigReloader: func(dir string) (*ReloadedConfig, error) {
				return &ReloadedConfig{
					PostSyncHooks: []PostSyncHook{{Container: "new-container"}},
				}, nil
			},
		}
		r := NewReconciler(cfg)
		r.reloadProjectConfig()
		require.Len(t, r.config.PostSyncHooks, 1)
		assert.Equal(t, "new-container", r.config.PostSyncHooks[0].Container)
	})

	t.Run("env override prevents hook reload", func(t *testing.T) {
		cfg := &Config{
			PostSyncHooksFromEnv: true,
			PostSyncHooks:        []PostSyncHook{{Container: "env-container"}},
			ConfigReloader: func(dir string) (*ReloadedConfig, error) {
				return &ReloadedConfig{
					PostSyncHooks: []PostSyncHook{{Container: "repo-container"}},
				}, nil
			},
		}
		r := NewReconciler(cfg)
		r.reloadProjectConfig()
		assert.Equal(t, "env-container", r.config.PostSyncHooks[0].Container)
	})

	t.Run("deploy paths reloaded from repo", func(t *testing.T) {
		cfg := &Config{
			ConfigReloader: func(dir string) (*ReloadedConfig, error) {
				return &ReloadedConfig{
					DeployPaths: []string{"infra/**"},
				}, nil
			},
		}
		r := NewReconciler(cfg)
		r.reloadProjectConfig()
		require.Len(t, r.config.DeployPaths, 1)
		assert.Equal(t, "infra/**", r.config.DeployPaths[0])
	})

	t.Run("settle delay reloaded from repo", func(t *testing.T) {
		cfg := &Config{
			ConfigReloader: func(dir string) (*ReloadedConfig, error) {
				return &ReloadedConfig{
					HookSettleDelay: 5 * time.Second,
				}, nil
			},
		}
		r := NewReconciler(cfg)
		r.reloadProjectConfig()
		assert.Equal(t, 5*time.Second, r.config.HookSettleDelay)
	})

	t.Run("empty reloaded config is no-op", func(t *testing.T) {
		cfg := &Config{
			PostSyncHooks: []PostSyncHook{{Container: "orig"}},
			ConfigReloader: func(dir string) (*ReloadedConfig, error) {
				return &ReloadedConfig{}, nil
			},
		}
		r := NewReconciler(cfg)
		r.reloadProjectConfig()
		assert.Equal(t, "orig", r.config.PostSyncHooks[0].Container)
	})

	t.Run("critical containers reloaded from repo", func(t *testing.T) {
		cfg := &Config{
			ConfigReloader: func(dir string) (*ReloadedConfig, error) {
				return &ReloadedConfig{
					CriticalContainers: []string{"traefik", "authelia"},
				}, nil
			},
		}
		r := NewReconciler(cfg)
		r.reloadProjectConfig()
		require.Len(t, r.config.CriticalContainers, 2)
		assert.Equal(t, "traefik", r.config.CriticalContainers[0])
		assert.Equal(t, "authelia", r.config.CriticalContainers[1])
	})

	t.Run("env override prevents critical containers reload", func(t *testing.T) {
		cfg := &Config{
			CriticalContainersFromEnv: true,
			CriticalContainers:        []string{"env-container"},
			ConfigReloader: func(dir string) (*ReloadedConfig, error) {
				return &ReloadedConfig{
					CriticalContainers: []string{"repo-container"},
				}, nil
			},
		}
		r := NewReconciler(cfg)
		r.reloadProjectConfig()
		require.Len(t, r.config.CriticalContainers, 1)
		assert.Equal(t, "env-container", r.config.CriticalContainers[0])
	})
}

// --- Reconciler.cleanupStaging additional tests ---

func TestCleanupStagingAdditional(t *testing.T) {
	t.Run("dry run is no-op", func(t *testing.T) {
		tmpDir := t.TempDir()
		stagingDir := filepath.Join(tmpDir, "staging")
		require.NoError(t, os.MkdirAll(stagingDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(stagingDir, "file.yml"), []byte("test"), 0644))

		cfg := &Config{StagingDir: stagingDir, DryRun: true}
		r := NewReconciler(cfg)

		err := r.cleanupStaging()
		require.NoError(t, err)
		assert.DirExists(t, stagingDir, "dry run should not remove staging")
	})

	t.Run("empty staging dir is no-op", func(t *testing.T) {
		cfg := &Config{StagingDir: ""}
		r := NewReconciler(cfg)

		err := r.cleanupStaging()
		require.NoError(t, err)
	})
}

// --- Reconciler.decryptSecrets tests ---

func TestDecryptSecrets(t *testing.T) {
	t.Run("empty secrets files returns empty map", func(t *testing.T) {
		cfg := &Config{SecretsFiles: []string{}}
		r := NewReconciler(cfg)

		secrets, err := r.decryptSecrets(context.Background())
		require.NoError(t, err)
		assert.Empty(t, secrets)
	})

	t.Run("missing secrets file returns error", func(t *testing.T) {
		cfg := &Config{
			RepoDir:      "/tmp/nonexistent-repo",
			SecretsFiles: []string{"secrets.yaml"},
		}
		r := NewReconciler(cfg)

		_, err := r.decryptSecrets(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "secrets file not found")
	})

	t.Run("successful decryption returns merged map", func(t *testing.T) {
		tmpDir := t.TempDir()
		// Create a dummy secrets file (the mock will return data regardless)
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "secrets.yaml"), []byte("dummy"), 0644))

		mockSops := &mockSecretsDecryptor{
			decryptResult: map[string]any{"key": "value"},
		}

		cfg := &Config{
			RepoDir:      tmpDir,
			SecretsFiles: []string{"secrets.yaml"},
		}
		r := NewReconciler(cfg, WithSecretsDecryptor(mockSops))

		secrets, err := r.decryptSecrets(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "value", secrets["key"])
	})

	t.Run("decryption failure propagates error", func(t *testing.T) {
		tmpDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "secrets.yaml"), []byte("dummy"), 0644))

		mockSops := &mockSecretsDecryptor{
			decryptErr: fmt.Errorf("age key not found"),
		}

		cfg := &Config{
			RepoDir:      tmpDir,
			SecretsFiles: []string{"secrets.yaml"},
		}
		r := NewReconciler(cfg, WithSecretsDecryptor(mockSops))

		_, err := r.decryptSecrets(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "age key not found")
	})
}

// --- Reconciler.renderTemplates tests ---

func TestRenderTemplates(t *testing.T) {
	t.Run("renders templates to staging", func(t *testing.T) {
		tmpDir := t.TempDir()
		repoDir := filepath.Join(tmpDir, "repo")
		stagingDir := filepath.Join(tmpDir, "staging")

		// Create infra source with template
		infraDir := filepath.Join(repoDir, "unraid")
		require.NoError(t, os.MkdirAll(infraDir, 0755))

		tmplFile := filepath.Join(repoDir, "config.yaml.tmpl")
		require.NoError(t, os.WriteFile(tmplFile, []byte("host: {{ .host }}"), 0644))

		cfg := &Config{
			RepoDir:     repoDir,
			StagingDir:  stagingDir,
			InfraSubDir: ".",
		}
		r := NewReconciler(cfg)

		secrets := map[string]any{"host": "example.com"}
		err := r.renderTemplates(context.Background(), secrets)
		require.NoError(t, err)

		// Verify rendered template
		rendered, err := os.ReadFile(filepath.Join(stagingDir, "config.yaml"))
		require.NoError(t, err)
		assert.Equal(t, "host: example.com", string(rendered))
	})

	t.Run("clears staging before rendering", func(t *testing.T) {
		tmpDir := t.TempDir()
		repoDir := filepath.Join(tmpDir, "repo")
		stagingDir := filepath.Join(tmpDir, "staging")

		// Create pre-existing staging file
		require.NoError(t, os.MkdirAll(stagingDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(stagingDir, "old-file.yml"), []byte("old"), 0644))

		// Create infra source
		infraDir := filepath.Join(repoDir, "unraid")
		require.NoError(t, os.MkdirAll(infraDir, 0755))

		cfg := &Config{
			RepoDir:     repoDir,
			StagingDir:  stagingDir,
			InfraSubDir: ".",
		}
		r := NewReconciler(cfg)

		err := r.renderTemplates(context.Background(), map[string]any{})
		require.NoError(t, err)

		// Old file should be gone
		assert.NoFileExists(t, filepath.Join(stagingDir, "old-file.yml"))
	})
}

func TestRenderTemplatesFailure(t *testing.T) {
	t.Run("template parse error propagates", func(t *testing.T) {
		tmpDir := t.TempDir()
		repoDir := filepath.Join(tmpDir, "repo")
		stagingDir := filepath.Join(tmpDir, "staging")

		// Create infra dir
		infraDir := filepath.Join(repoDir, "unraid")
		require.NoError(t, os.MkdirAll(infraDir, 0755))

		// Create template with invalid syntax
		tmplFile := filepath.Join(repoDir, "config.yaml.tmpl")
		require.NoError(t, os.WriteFile(tmplFile, []byte("{{ .name | noSuchFunc }}"), 0644))

		cfg := &Config{
			RepoDir:     repoDir,
			StagingDir:  stagingDir,
			InfraSubDir: ".",
		}
		r := NewReconciler(cfg)

		err := r.renderTemplates(context.Background(), map[string]any{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to render templates")
	})
}

// --- Reconciler.isLocalMode / getTargetHost tests ---

func TestIsLocalMode(t *testing.T) {
	t.Run("remote host set returns false", func(t *testing.T) {
		cfg := &Config{TargetHost: "root@10.0.0.1"}
		r := NewReconciler(cfg)
		assert.False(t, r.isLocalMode())
	})

	t.Run("local appdata exists returns true", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := &Config{LocalAppdataPath: tmpDir}
		r := NewReconciler(cfg)
		assert.True(t, r.isLocalMode())
	})

	t.Run("local appdata missing returns false", func(t *testing.T) {
		cfg := &Config{LocalAppdataPath: "/nonexistent/path"}
		r := NewReconciler(cfg)
		assert.False(t, r.isLocalMode())
	})
}

func TestGetTargetHost(t *testing.T) {
	t.Run("explicit target host", func(t *testing.T) {
		cfg := &Config{TargetHost: "root@10.0.0.1"}
		r := NewReconciler(cfg)
		assert.Equal(t, "root@10.0.0.1", r.getTargetHost(nil))
	})

	t.Run("from secrets network unraid_ip", func(t *testing.T) {
		cfg := &Config{}
		r := NewReconciler(cfg)
		secrets := map[string]any{
			"network": map[string]any{
				"unraid_ip": "192.168.1.100",
			},
		}
		assert.Equal(t, "root@192.168.1.100", r.getTargetHost(secrets))
	})

	t.Run("no host available returns empty", func(t *testing.T) {
		cfg := &Config{}
		r := NewReconciler(cfg)
		assert.Equal(t, "", r.getTargetHost(map[string]any{}))
	})

	t.Run("nil secrets returns empty", func(t *testing.T) {
		cfg := &Config{}
		r := NewReconciler(cfg)
		assert.Equal(t, "", r.getTargetHost(nil))
	})
}

// --- Reconciler.doDeploy tests ---

func TestDoDeploy(t *testing.T) {
	t.Run("local mode dry run", func(t *testing.T) {
		tmpDir := t.TempDir()
		stagingDir := filepath.Join(tmpDir, "staging")
		appdataDir := filepath.Join(tmpDir, "appdata")
		require.NoError(t, os.MkdirAll(appdataDir, 0755))
		require.NoError(t, os.MkdirAll(stagingDir, 0755))

		cfg := &Config{
			DryRun:           true,
			StagingDir:       stagingDir,
			InfraSubDir:      ".",
			LocalAppdataPath: appdataDir,
		}
		r := NewReconciler(cfg)

		result, err := r.doDeploy(context.Background(), nil)
		require.NoError(t, err)
		assert.NotNil(t, result)
	})

	t.Run("remote mode returns nil result", func(t *testing.T) {
		// Remote deploy returns (nil, error) -- verify doDeploy routes correctly.
		// Use an empty secrets map so getTargetHost returns "" and deployRemote
		// fails immediately with a "no target host" error (no SSH involved).
		cfg := &Config{
			LocalAppdataPath: "/nonexistent/path", // Force remote mode (stat fails)
		}
		r := NewReconciler(cfg)

		result, err := r.doDeploy(context.Background(), map[string]any{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no target host")
		assert.Nil(t, result)
	})
}

// --- Reconciler.Run partial path tests ---

func TestReconcilerRun(t *testing.T) {
	t.Run("skip when already deployed same commit", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockFile := filepath.Join(tmpDir, "reconcile.lock")
		stateFile := filepath.Join(tmpDir, "state.json")

		gitOps := &mockGitOps{
			syncChanged: false,
			syncBefore:  "abc123",
			syncAfter:   "abc123",
		}

		// Pre-save state with same commit
		state := &DeployState{
			SchemaVersion:      2,
			LastDeployedCommit: "abc123",
		}
		require.NoError(t, SaveState(stateFile, state))

		cfg := &Config{
			LockFile:  lockFile,
			StateFile: stateFile,
		}
		r := NewReconciler(cfg, WithGitOperations(gitOps))

		err := r.Run(context.Background())
		require.NoError(t, err)
	})

	t.Run("circuit breaker blocks after max attempts", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockFile := filepath.Join(tmpDir, "reconcile.lock")
		stateFile := filepath.Join(tmpDir, "state.json")

		gitOps := &mockGitOps{
			syncChanged: false,
			syncBefore:  "abc123",
			syncAfter:   "abc123",
		}

		// Pre-save state with 3 failed attempts on same commit
		state := &DeployState{
			SchemaVersion:       2,
			LastDeployedCommit:  "oldcommit",
			LastAttemptedCommit: "abc123",
			AttemptCount:        3,
		}
		require.NoError(t, SaveState(stateFile, state))

		cfg := &Config{
			LockFile:  lockFile,
			StateFile: stateFile,
		}
		r := NewReconciler(cfg, WithGitOperations(gitOps))

		err := r.Run(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "circuit breaker")
	})

	t.Run("force overrides circuit breaker", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockFile := filepath.Join(tmpDir, "reconcile.lock")
		stateFile := filepath.Join(tmpDir, "state.json")
		repoDir := filepath.Join(tmpDir, "repo")
		stagingDir := filepath.Join(tmpDir, "staging")
		appdataDir := filepath.Join(tmpDir, "appdata")

		require.NoError(t, os.MkdirAll(appdataDir, 0755))

		gitOps := &mockGitOps{
			syncChanged: false,
			syncBefore:  "abc123",
			syncAfter:   "abc123",
		}

		// Pre-save state with 3 failed attempts on same commit
		state := &DeployState{
			SchemaVersion:       2,
			LastDeployedCommit:  "oldcommit",
			LastAttemptedCommit: "abc123",
			AttemptCount:        3,
		}
		require.NoError(t, SaveState(stateFile, state))

		// Create repo dir with infrastructure for rendering
		infraDir := filepath.Join(repoDir, "unraid")
		require.NoError(t, os.MkdirAll(infraDir, 0755))

		cfg := &Config{
			Force:            true,
			DryRun:           true, // Dry run to skip actual deployment
			LockFile:         lockFile,
			StateFile:        stateFile,
			RepoDir:          repoDir,
			StagingDir:       stagingDir,
			LocalAppdataPath: appdataDir,
			InfraSubDir:      ".",
		}
		r := NewReconciler(cfg, WithGitOperations(gitOps))

		err := r.Run(context.Background())
		require.NoError(t, err)
	})

	t.Run("git sync failure", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockFile := filepath.Join(tmpDir, "reconcile.lock")

		gitOps := &mockGitOps{
			syncErr: fmt.Errorf("authentication failed"),
		}

		cfg := &Config{
			LockFile: lockFile,
		}
		r := NewReconciler(cfg, WithGitOperations(gitOps))

		err := r.Run(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to sync repository")
	})

	t.Run("deploy paths skip non-matching changes", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockFile := filepath.Join(tmpDir, "reconcile.lock")
		stateFile := filepath.Join(tmpDir, "state.json")

		gitOps := &mockGitOps{
			syncChanged: true,
			syncBefore:  "aaa111",
			syncAfter:   "bbb222",
			diffFiles:   []string{"README.md", "docs/guide.md"},
		}

		cfg := &Config{
			LockFile:    lockFile,
			StateFile:   stateFile,
			DeployPaths: []string{"infra/**", "compose/**"},
		}
		r := NewReconciler(cfg, WithGitOperations(gitOps))

		err := r.Run(context.Background())
		require.NoError(t, err)

		// Verify state was saved with the commit to prevent re-evaluation
		saved := LoadState(stateFile)
		assert.Equal(t, "bbb222", saved.LastDeployedCommit)
	})
}

// --- Reconciler.createBackup tests ---

func TestCreateBackup(t *testing.T) {
	t.Run("local mode calls Backup", func(t *testing.T) {
		tmpDir := t.TempDir()
		backupDir := filepath.Join(tmpDir, "backups")
		stagingDir := filepath.Join(tmpDir, "staging")
		appdataDir := filepath.Join(tmpDir, "appdata")
		require.NoError(t, os.MkdirAll(appdataDir, 0755))
		// Create staging structure so discoverDeployTargets can scan it.
		require.NoError(t, os.MkdirAll(filepath.Join(stagingDir, "appdata", "traefik"), 0755))

		cfg := &Config{
			StagingDir:       stagingDir,
			InfraSubDir:      ".",
			BackupDir:        backupDir,
			LocalAppdataPath: appdataDir,
			BackupsToKeep:    3,
		}
		r := NewReconciler(cfg)

		// Backup will succeed (no files to back up returns name, no error)
		err := r.createBackup(context.Background(), nil)
		require.NoError(t, err)
		assert.NotEmpty(t, r.lastBackupPath)
	})
}

// --- Duration YAML edge cases ---

func TestDurationYAMLError(t *testing.T) {
	t.Run("unmarshal non-string YAML value", func(t *testing.T) {
		input := "delay:\n  nested: value\n"
		var out struct {
			Delay Duration `yaml:"delay"`
		}
		err := yaml.Unmarshal([]byte(input), &out)
		assert.Error(t, err)
	})

	t.Run("marshal zero duration", func(t *testing.T) {
		d := Duration{Duration: 0}
		val, err := d.MarshalYAML()
		require.NoError(t, err)
		assert.Nil(t, val)
	})

	t.Run("marshal non-zero duration", func(t *testing.T) {
		d := Duration{Duration: 3 * time.Second}
		val, err := d.MarshalYAML()
		require.NoError(t, err)
		assert.Equal(t, "3s", val)
	})
}

// --- ValidateSOPSFile additional tests ---

func TestValidateSOPSFileAdditional(t *testing.T) {
	t.Run("valid SOPS file", func(t *testing.T) {
		tmpDir := t.TempDir()
		sopsFile := filepath.Join(tmpDir, "secrets.yaml")
		content := "key: ENC[AES256_GCM,...]\nsops:\n  age:\n  - recipient: age1...\n"
		require.NoError(t, os.WriteFile(sopsFile, []byte(content), 0644))

		err := ValidateSOPSFile(sopsFile)
		require.NoError(t, err)
	})

	t.Run("file without sops key", func(t *testing.T) {
		tmpDir := t.TempDir()
		plainFile := filepath.Join(tmpDir, "plain.yaml")
		require.NoError(t, os.WriteFile(plainFile, []byte("key: value\n"), 0644))

		err := ValidateSOPSFile(plainFile)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrNotSOPSFile)
	})

	t.Run("non-existent file", func(t *testing.T) {
		err := ValidateSOPSFile("/nonexistent/secrets.yaml")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("invalid YAML", func(t *testing.T) {
		tmpDir := t.TempDir()
		badFile := filepath.Join(tmpDir, "bad.yaml")
		require.NoError(t, os.WriteFile(badFile, []byte("[invalid: yaml: {{{"), 0644))

		err := ValidateSOPSFile(badFile)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid YAML")
	})
}

// --- VerifyContainerHealth dry-run test ---

func TestVerifyContainerHealthDryRun(t *testing.T) {
	deploy := NewDeployOps(true, "test")
	err := deploy.VerifyContainerHealth(context.Background(), "compose.yml")
	require.NoError(t, err, "dry run should short-circuit and return nil")
}

// --- ComposeUpWithRollback tests ---

func TestComposeUpWithRollback(t *testing.T) {
	t.Run("delegates to ComposeUpMultipleWithRollback", func(t *testing.T) {
		// This primarily tests the delegation path; ComposeUpMultiple
		// will fail because docker compose is not running test containers.
		deploy := NewDeployOps(false, "test")
		err := deploy.ComposeUpWithRollback(context.Background(), "/nonexistent/compose.yml", "")
		require.Error(t, err)
	})
}

func TestComposeUpMultipleWithRollback(t *testing.T) {
	t.Run("no backup available returns wrapped error", func(t *testing.T) {
		deploy := NewDeployOps(false, "test")
		err := deploy.ComposeUpMultipleWithRollback(context.Background(), []string{"/nonexistent.yml"}, "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no backup available for rollback")
	})

	t.Run("backup dir with no matching files", func(t *testing.T) {
		tmpDir := t.TempDir()
		backupDir := filepath.Join(tmpDir, "backup")
		require.NoError(t, os.MkdirAll(backupDir, 0755))

		deploy := NewDeployOps(false, "test")
		err := deploy.ComposeUpMultipleWithRollback(context.Background(), []string{"/nonexistent.yml"}, backupDir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no backup files found for rollback")
	})
}

// --- DeployResult tests ---

func TestDeployResult(t *testing.T) {
	t.Run("AddWritten appends files", func(t *testing.T) {
		result := &DeployResult{}
		result.AddWritten("file1.yml", "file2.yml")
		result.AddWritten("file3.yml")
		assert.Len(t, result.WrittenFiles, 3)
	})
}

// --- sendThrottledFailureAlert error paths ---

func TestSendThrottledFailureAlertErrorPaths(t *testing.T) {
	t.Run("send error is logged and returns early without saving state", func(t *testing.T) {
		tmpDir := t.TempDir()
		stateFile := filepath.Join(tmpDir, "state.json")

		alerter := &mockAlertSender{lastErr: fmt.Errorf("network error")}
		cfg := &Config{StateFile: stateFile, OnFailure: true}
		r := NewReconciler(cfg, WithAlerter(alerter))
		r.lastCommit = "abc123"

		state := &DeployState{AttemptCount: 1, LastAlertedAttempt: 0}
		r.sendThrottledFailureAlert(context.Background(), state, "deploy failed")

		assert.Equal(t, 1, alerter.deployFailureCalls)
		// LastAlertedAttempt should NOT be updated on send failure
		assert.Equal(t, 0, state.LastAlertedAttempt)
	})

	t.Run("save state error after successful send", func(t *testing.T) {
		// Use a non-writable state file path to trigger save error
		stateFile := "/nonexistent/dir/state.json"

		alerter := &mockAlertSender{}
		cfg := &Config{StateFile: stateFile, OnFailure: true}
		r := NewReconciler(cfg, WithAlerter(alerter))
		r.lastCommit = "abc123"

		state := &DeployState{AttemptCount: 1, LastAlertedAttempt: 0}
		r.sendThrottledFailureAlert(context.Background(), state, "deploy failed")

		assert.Equal(t, 1, alerter.deployFailureCalls)
		// State is updated in memory even if file save fails
		assert.Equal(t, 1, state.LastAlertedAttempt)
	})

	t.Run("uses local target when host is empty", func(t *testing.T) {
		tmpDir := t.TempDir()
		stateFile := filepath.Join(tmpDir, "state.json")

		alerter := &mockAlertSender{}
		cfg := &Config{StateFile: stateFile, OnFailure: true} // No TargetHost
		r := NewReconciler(cfg, WithAlerter(alerter))

		state := &DeployState{AttemptCount: 1, LastAlertedAttempt: 0}
		r.sendThrottledFailureAlert(context.Background(), state, "fail")

		assert.Equal(t, 1, alerter.deployFailureCalls)
	})
}

// --- deployLocal full path tests ---

func TestDeployLocalFullPath(t *testing.T) {
	t.Run("successful local deployment with content hash sync", func(t *testing.T) {
		tmpDir := t.TempDir()
		stagingDir := filepath.Join(tmpDir, "staging")
		appdataDir := filepath.Join(tmpDir, "appdata")

		// Create the staging structure that deployLocal expects
		stagingUnraid := filepath.Join(stagingDir, "unraid")
		require.NoError(t, os.MkdirAll(filepath.Join(stagingUnraid, "appdata", "traefik"), 0755))
		require.NoError(t, os.MkdirAll(filepath.Join(stagingUnraid, "appdata", "agentgateway"), 0755))
		require.NoError(t, os.MkdirAll(filepath.Join(stagingUnraid, "appdata", "authelia"), 0755))
		require.NoError(t, os.MkdirAll(filepath.Join(stagingUnraid, "appdata", "gatus"), 0755))
		require.NoError(t, os.MkdirAll(filepath.Join(stagingUnraid, "appdata", "tailscale-gateway"), 0755))
		require.NoError(t, os.MkdirAll(filepath.Join(stagingUnraid, "compose"), 0755))

		// Write config files in staging
		require.NoError(t, os.WriteFile(filepath.Join(stagingUnraid, "appdata", "traefik", "traefik.yml"), []byte("entryPoints:\n  web:\n    address: ':80'"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(stagingUnraid, "appdata", "agentgateway", "config.yaml"), []byte("port: 8080"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(stagingUnraid, "appdata", "authelia", "configuration.yml"), []byte("server:\n  host: 0.0.0.0"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(stagingUnraid, "appdata", "gatus", "config.yaml"), []byte("endpoints: []"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(stagingUnraid, "appdata", "tailscale-gateway", "serve.json"), []byte("{}"), 0644))

		// Create appdata dir (triggers local mode)
		require.NoError(t, os.MkdirAll(appdataDir, 0755))

		// Create deploy ops with content hash sync enabled + dry run
		// (dry run to avoid compose up but still exercise file sync)
		deploy := &DeployOps{DryRun: false, ProjectName: "test", ContentHashSync: true}

		cfg := &Config{
			DryRun:           true, // DryRun on reconciler skips compose up
			StagingDir:       stagingDir,
			InfraSubDir:      "unraid",
			LocalAppdataPath: appdataDir,
		}
		r := NewReconciler(cfg, WithDeployOps(deploy))

		result, err := r.deployLocal(context.Background())
		require.NoError(t, err)
		assert.NotNil(t, result)
	})

	t.Run("non-dry-run local deployment syncs files and signals", func(t *testing.T) {
		tmpDir := t.TempDir()
		stagingDir := filepath.Join(tmpDir, "staging")
		appdataDir := filepath.Join(tmpDir, "appdata")

		// Create the full staging structure that deployLocal expects
		stagingUnraid := filepath.Join(stagingDir, "unraid")
		require.NoError(t, os.MkdirAll(filepath.Join(stagingUnraid, "appdata", "traefik"), 0755))
		require.NoError(t, os.MkdirAll(filepath.Join(stagingUnraid, "appdata", "agentgateway"), 0755))
		require.NoError(t, os.MkdirAll(filepath.Join(stagingUnraid, "appdata", "authelia"), 0755))
		require.NoError(t, os.MkdirAll(filepath.Join(stagingUnraid, "appdata", "gatus"), 0755))
		require.NoError(t, os.MkdirAll(filepath.Join(stagingUnraid, "appdata", "tailscale-gateway"), 0755))
		// Intentionally empty compose dir -> exercises "No compose files found" branch
		require.NoError(t, os.MkdirAll(filepath.Join(stagingUnraid, "compose"), 0755))

		// Write config files
		require.NoError(t, os.WriteFile(filepath.Join(stagingUnraid, "appdata", "traefik", "traefik.yml"), []byte("entryPoints:\n  web:\n    address: ':80'"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(stagingUnraid, "appdata", "agentgateway", "config.yaml"), []byte("port: 8080"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(stagingUnraid, "appdata", "authelia", "configuration.yml"), []byte("server:\n  host: 0.0.0.0"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(stagingUnraid, "appdata", "gatus", "config.yaml"), []byte("endpoints: []"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(stagingUnraid, "appdata", "tailscale-gateway", "serve.json"), []byte("{}"), 0644))

		// Create appdata dir (triggers local mode)
		require.NoError(t, os.MkdirAll(appdataDir, 0755))

		// DryRun=false on config but using ContentHashSync to avoid compose up
		deploy := &DeployOps{DryRun: false, ProjectName: "test", ContentHashSync: true}

		cfg := &Config{
			DryRun:           false, // Non-dry-run to exercise compose path
			StagingDir:       stagingDir,
			InfraSubDir:      "unraid",
			LocalAppdataPath: appdataDir,
		}
		r := NewReconciler(cfg, WithDeployOps(deploy))

		result, err := r.deployLocal(context.Background())
		require.NoError(t, err)
		assert.NotNil(t, result)

		// Verify files were synced to appdata
		assert.FileExists(t, filepath.Join(appdataDir, "traefik", "traefik.yml"))
		assert.FileExists(t, filepath.Join(appdataDir, "agentgateway", "config.yaml"))
		assert.FileExists(t, filepath.Join(appdataDir, "authelia", "configuration.yml"))
		assert.FileExists(t, filepath.Join(appdataDir, "gatus", "config.yaml"))
	})

	t.Run("deploy local with compose files invokes compose up", func(t *testing.T) {
		tmpDir := t.TempDir()
		stagingDir := filepath.Join(tmpDir, "staging")
		appdataDir := filepath.Join(tmpDir, "appdata")

		// Create the staging structure
		stagingUnraid := filepath.Join(stagingDir, "unraid")
		require.NoError(t, os.MkdirAll(filepath.Join(stagingUnraid, "appdata", "traefik"), 0755))
		require.NoError(t, os.MkdirAll(filepath.Join(stagingUnraid, "appdata", "agentgateway"), 0755))
		require.NoError(t, os.MkdirAll(filepath.Join(stagingUnraid, "appdata", "authelia"), 0755))
		require.NoError(t, os.MkdirAll(filepath.Join(stagingUnraid, "appdata", "gatus"), 0755))
		require.NoError(t, os.MkdirAll(filepath.Join(stagingUnraid, "appdata", "tailscale-gateway"), 0755))
		require.NoError(t, os.MkdirAll(filepath.Join(stagingUnraid, "compose"), 0755))

		// Write config files
		require.NoError(t, os.WriteFile(filepath.Join(stagingUnraid, "appdata", "traefik", "traefik.yml"), []byte("key: value"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(stagingUnraid, "appdata", "agentgateway", "config.yaml"), []byte("key: value"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(stagingUnraid, "appdata", "authelia", "configuration.yml"), []byte("key: value"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(stagingUnraid, "appdata", "gatus", "config.yaml"), []byte("key: value"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(stagingUnraid, "appdata", "tailscale-gateway", "serve.json"), []byte("{}"), 0644))

		// Write a compose file so the compose-up branch is hit
		require.NoError(t, os.WriteFile(filepath.Join(stagingUnraid, "compose", "stack.yml"), []byte("version: '3'\nservices: {}"), 0644))

		require.NoError(t, os.MkdirAll(appdataDir, 0755))

		deploy := &DeployOps{DryRun: false, ProjectName: "test", ContentHashSync: true}

		cfg := &Config{
			DryRun:           false,
			StagingDir:       stagingDir,
			InfraSubDir:      "unraid",
			LocalAppdataPath: appdataDir,
		}
		r := NewReconciler(cfg, WithDeployOps(deploy))

		// This will fail at compose up (docker not available in test) but
		// exercises the compose file discovery path and error classification
		result, err := r.deployLocal(context.Background())

		// compose up will fail since docker isn't running in test
		if err != nil {
			// Should be a compose-related error (all files failed in isolated mode)
			assert.Contains(t, err.Error(), "all compose files failed to deploy")
		} else {
			// If docker compose happens to succeed, that's fine too
			assert.NotNil(t, result)
		}
	})

	t.Run("deploy local with missing staging dir returns error", func(t *testing.T) {
		tmpDir := t.TempDir()
		stagingDir := filepath.Join(tmpDir, "staging")
		appdataDir := filepath.Join(tmpDir, "appdata")

		// Don't create staging structure -> discovery will fail on missing dir
		require.NoError(t, os.MkdirAll(appdataDir, 0755))

		deploy := &DeployOps{DryRun: false, ContentHashSync: true}

		cfg := &Config{
			StagingDir:       stagingDir,
			InfraSubDir:      "unraid",
			LocalAppdataPath: appdataDir,
		}
		r := NewReconciler(cfg, WithDeployOps(deploy))

		_, err := r.deployLocal(context.Background())
		require.Error(t, err, "should fail when staging dir doesn't exist")
		assert.Contains(t, err.Error(), "discover deploy targets")
	})
}

// --- DeployLocal content-hash mode tests ---

func TestDeployOps_DeployLocalContentHash(t *testing.T) {
	t.Run("content hash mode syncs files", func(t *testing.T) {
		tmpDir := t.TempDir()
		sourceDir := filepath.Join(tmpDir, "source")
		targetDir := filepath.Join(tmpDir, "target")

		require.NoError(t, os.MkdirAll(sourceDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "config.yml"), []byte("key: value"), 0644))

		deploy := &DeployOps{ContentHashSync: true}
		result := &DeployResult{}

		err := deploy.DeployLocal(context.Background(), sourceDir, targetDir, result)
		require.NoError(t, err)

		// Verify file was copied to target
		content, err := os.ReadFile(filepath.Join(targetDir, "config.yml"))
		require.NoError(t, err)
		assert.Equal(t, "key: value", string(content))

		// Files should appear in result
		assert.NotEmpty(t, result.WrittenFiles)
	})

	t.Run("content hash mode skips unchanged files", func(t *testing.T) {
		tmpDir := t.TempDir()
		sourceDir := filepath.Join(tmpDir, "source")
		targetDir := filepath.Join(tmpDir, "target")

		require.NoError(t, os.MkdirAll(sourceDir, 0755))
		require.NoError(t, os.MkdirAll(targetDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "config.yml"), []byte("key: value"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(targetDir, "config.yml"), []byte("key: value"), 0644))

		deploy := &DeployOps{ContentHashSync: true}
		result := &DeployResult{}

		err := deploy.DeployLocal(context.Background(), sourceDir, targetDir, result)
		require.NoError(t, err)

		// No files should be written since content is the same
		assert.Empty(t, result.WrittenFiles)
	})

	t.Run("standard mode uses atomic rename", func(t *testing.T) {
		tmpDir := t.TempDir()
		sourceDir := filepath.Join(tmpDir, "source")
		targetDir := filepath.Join(tmpDir, "target")

		require.NoError(t, os.MkdirAll(sourceDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "file.txt"), []byte("hello"), 0644))

		deploy := &DeployOps{ContentHashSync: false}
		result := &DeployResult{}

		err := deploy.DeployLocal(context.Background(), sourceDir, targetDir, result)
		require.NoError(t, err)

		// Verify file exists in target
		content, err := os.ReadFile(filepath.Join(targetDir, "file.txt"))
		require.NoError(t, err)
		assert.Equal(t, "hello", string(content))
	})

	t.Run("dry run mode skips deployment", func(t *testing.T) {
		deploy := &DeployOps{DryRun: true}
		result := &DeployResult{}

		err := deploy.DeployLocal(context.Background(), "/nonexistent/source", "/nonexistent/target", result)
		require.NoError(t, err)
		assert.Empty(t, result.WrittenFiles)
	})

	t.Run("non-existent source returns error", func(t *testing.T) {
		deploy := &DeployOps{}
		result := &DeployResult{}

		err := deploy.DeployLocal(context.Background(), "/nonexistent/source", "/tmp/target", result)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "source directory")
	})

	t.Run("source is not a directory returns error", func(t *testing.T) {
		tmpDir := t.TempDir()
		sourceFile := filepath.Join(tmpDir, "not-a-dir.txt")
		require.NoError(t, os.WriteFile(sourceFile, []byte("content"), 0644))

		deploy := &DeployOps{}
		result := &DeployResult{}

		err := deploy.DeployLocal(context.Background(), sourceFile, filepath.Join(tmpDir, "target"), result)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not a directory")
	})

	t.Run("context cancelled during content hash sync", func(t *testing.T) {
		tmpDir := t.TempDir()
		sourceDir := filepath.Join(tmpDir, "source")
		targetDir := filepath.Join(tmpDir, "target")

		require.NoError(t, os.MkdirAll(sourceDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "file.txt"), []byte("data"), 0644))

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		deploy := &DeployOps{ContentHashSync: true}
		result := &DeployResult{}

		err := deploy.DeployLocal(ctx, sourceDir, targetDir, result)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})
}

// --- DeployLocalFile content-hash mode tests ---

func TestDeployOps_DeployLocalFileContentHash(t *testing.T) {
	t.Run("content hash mode writes changed file", func(t *testing.T) {
		tmpDir := t.TempDir()
		sourceFile := filepath.Join(tmpDir, "source.yml")
		targetFile := filepath.Join(tmpDir, "out", "target.yml")

		require.NoError(t, os.WriteFile(sourceFile, []byte("key: value"), 0644))

		deploy := &DeployOps{ContentHashSync: true}
		result := &DeployResult{}

		err := deploy.DeployLocalFile(context.Background(), sourceFile, targetFile, result)
		require.NoError(t, err)

		content, err := os.ReadFile(targetFile)
		require.NoError(t, err)
		assert.Equal(t, "key: value", string(content))
		assert.Contains(t, result.WrittenFiles, targetFile)
	})

	t.Run("content hash mode skips unchanged file", func(t *testing.T) {
		tmpDir := t.TempDir()
		sourceFile := filepath.Join(tmpDir, "source.yml")
		targetFile := filepath.Join(tmpDir, "target.yml")

		require.NoError(t, os.WriteFile(sourceFile, []byte("same content"), 0644))
		require.NoError(t, os.WriteFile(targetFile, []byte("same content"), 0644))

		deploy := &DeployOps{ContentHashSync: true}
		result := &DeployResult{}

		err := deploy.DeployLocalFile(context.Background(), sourceFile, targetFile, result)
		require.NoError(t, err)
		assert.Empty(t, result.WrittenFiles)
	})

	t.Run("non-hash mode copies file directly", func(t *testing.T) {
		tmpDir := t.TempDir()
		sourceFile := filepath.Join(tmpDir, "source.yml")
		targetFile := filepath.Join(tmpDir, "out", "target.yml")

		require.NoError(t, os.WriteFile(sourceFile, []byte("data"), 0644))

		deploy := &DeployOps{ContentHashSync: false}
		result := &DeployResult{}

		err := deploy.DeployLocalFile(context.Background(), sourceFile, targetFile, result)
		require.NoError(t, err)

		content, err := os.ReadFile(targetFile)
		require.NoError(t, err)
		assert.Equal(t, "data", string(content))
	})

	t.Run("dry run skips", func(t *testing.T) {
		deploy := &DeployOps{DryRun: true}

		err := deploy.DeployLocalFile(context.Background(), "/nonexistent", "/nonexistent", nil)
		require.NoError(t, err)
	})

	t.Run("cancelled context returns error", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		deploy := &DeployOps{}
		err := deploy.DeployLocalFile(ctx, "/nonexistent", "/nonexistent", nil)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("missing source file returns error", func(t *testing.T) {
		deploy := &DeployOps{ContentHashSync: true}

		err := deploy.DeployLocalFile(context.Background(), "/nonexistent/src.yml", "/tmp/target.yml", nil)
		require.Error(t, err)
	})
}

// --- ComposeUpMultiple tests ---

func TestComposeUpMultiple(t *testing.T) {
	t.Run("dry run returns nil", func(t *testing.T) {
		deploy := NewDeployOps(true, "test")
		err := deploy.ComposeUpMultiple(context.Background(), []string{"compose.yml"})
		require.NoError(t, err)
	})

	t.Run("empty files returns nil", func(t *testing.T) {
		deploy := NewDeployOps(false, "test")
		err := deploy.ComposeUpMultiple(context.Background(), []string{})
		require.NoError(t, err)
	})
}

// --- SignalContainer tests ---

func TestSignalContainer(t *testing.T) {
	t.Run("dry run returns nil", func(t *testing.T) {
		deploy := NewDeployOps(true, "test")
		err := deploy.SignalContainer(context.Background(), "mycontainer", "SIGHUP")
		require.NoError(t, err)
	})

	t.Run("invalid container name returns error", func(t *testing.T) {
		deploy := NewDeployOps(false, "test")
		err := deploy.SignalContainer(context.Background(), "", "SIGHUP")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid container name")
	})

	t.Run("invalid signal returns error", func(t *testing.T) {
		deploy := NewDeployOps(false, "test")
		err := deploy.SignalContainer(context.Background(), "mycontainer", "INVALID_SIGNAL")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid signal")
	})
}

// --- Full Run success path ---

func TestReconcilerRunFullSuccess(t *testing.T) {
	t.Run("full pipeline with dry run deploys successfully", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockFile := filepath.Join(tmpDir, "reconcile.lock")
		stateFile := filepath.Join(tmpDir, "state.json")
		repoDir := filepath.Join(tmpDir, "repo")
		stagingDir := filepath.Join(tmpDir, "staging")
		appdataDir := filepath.Join(tmpDir, "appdata")

		require.NoError(t, os.MkdirAll(appdataDir, 0755))

		// Create repo with infra dir and template
		infraDir := filepath.Join(repoDir, "unraid")
		require.NoError(t, os.MkdirAll(infraDir, 0755))
		require.NoError(t, os.WriteFile(
			filepath.Join(repoDir, "config.yaml.tmpl"),
			[]byte("host: {{ .host }}"),
			0644,
		))

		gitOps := &mockGitOps{
			syncChanged: true,
			syncBefore:  "aaa111",
			syncAfter:   "bbb222",
		}

		mockSops := &mockSecretsDecryptor{
			decryptResult: map[string]any{"host": "example.com"},
		}

		cfg := &Config{
			DryRun:           true,
			LockFile:         lockFile,
			StateFile:        stateFile,
			RepoDir:          repoDir,
			StagingDir:       stagingDir,
			LocalAppdataPath: appdataDir,
			InfraSubDir:      ".",
			SecretsFiles:     []string{},
		}
		r := NewReconciler(cfg,
			WithGitOperations(gitOps),
			WithSecretsDecryptor(mockSops),
		)

		err := r.Run(context.Background())
		require.NoError(t, err)

		// Verify state was saved with the deployed commit
		saved := LoadState(stateFile)
		assert.Equal(t, "bbb222", saved.LastDeployedCommit)
		assert.Equal(t, 0, saved.AttemptCount)
	})

	t.Run("state mismatch triggers re-deploy", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockFile := filepath.Join(tmpDir, "reconcile.lock")
		stateFile := filepath.Join(tmpDir, "state.json")
		repoDir := filepath.Join(tmpDir, "repo")
		stagingDir := filepath.Join(tmpDir, "staging")
		appdataDir := filepath.Join(tmpDir, "appdata")

		require.NoError(t, os.MkdirAll(appdataDir, 0755))

		infraDir := filepath.Join(repoDir, "unraid")
		require.NoError(t, os.MkdirAll(infraDir, 0755))

		// Pre-save state with different deployed commit
		state := &DeployState{
			SchemaVersion:      2,
			LastDeployedCommit: "oldcommit",
		}
		require.NoError(t, SaveState(stateFile, state))

		gitOps := &mockGitOps{
			syncChanged: false, // No git change, but state mismatch
			syncBefore:  "newcommit",
			syncAfter:   "newcommit",
		}

		cfg := &Config{
			DryRun:           true,
			LockFile:         lockFile,
			StateFile:        stateFile,
			RepoDir:          repoDir,
			StagingDir:       stagingDir,
			LocalAppdataPath: appdataDir,
			InfraSubDir:      ".",
			SecretsFiles:     []string{},
		}
		r := NewReconciler(cfg, WithGitOperations(gitOps))

		err := r.Run(context.Background())
		require.NoError(t, err)

		saved := LoadState(stateFile)
		assert.Equal(t, "newcommit", saved.LastDeployedCommit)
	})

	t.Run("decrypt failure sends throttled alert and returns error", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockFile := filepath.Join(tmpDir, "reconcile.lock")
		stateFile := filepath.Join(tmpDir, "state.json")
		repoDir := filepath.Join(tmpDir, "repo")
		appdataDir := filepath.Join(tmpDir, "appdata")

		require.NoError(t, os.MkdirAll(appdataDir, 0755))

		infraDir := filepath.Join(repoDir, "unraid")
		require.NoError(t, os.MkdirAll(infraDir, 0755))

		// Create a dummy secrets file so the file-exists check passes
		require.NoError(t, os.WriteFile(filepath.Join(repoDir, "secrets.yaml"), []byte("dummy"), 0644))

		gitOps := &mockGitOps{
			syncChanged: true,
			syncBefore:  "aaa",
			syncAfter:   "bbb",
		}

		mockSops := &mockSecretsDecryptor{
			decryptErr: fmt.Errorf("age key not available"),
		}

		alerter := &mockAlertSender{}

		cfg := &Config{
			LockFile:         lockFile,
			StateFile:        stateFile,
			RepoDir:          repoDir,
			StagingDir:       filepath.Join(tmpDir, "staging"),
			LocalAppdataPath: appdataDir,
			InfraSubDir:      ".",
			SecretsFiles:     []string{"secrets.yaml"},
			OnFailure:        true,
		}
		r := NewReconciler(cfg,
			WithGitOperations(gitOps),
			WithSecretsDecryptor(mockSops),
			WithAlerter(alerter),
		)

		err := r.Run(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decrypt secrets")
		assert.Equal(t, 1, alerter.deployFailureCalls)
	})

	t.Run("template failure sends alert and returns error", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockFile := filepath.Join(tmpDir, "reconcile.lock")
		stateFile := filepath.Join(tmpDir, "state.json")
		repoDir := filepath.Join(tmpDir, "repo")
		appdataDir := filepath.Join(tmpDir, "appdata")

		require.NoError(t, os.MkdirAll(appdataDir, 0755))

		// Create repo with broken template
		infraDir := filepath.Join(repoDir, "unraid")
		require.NoError(t, os.MkdirAll(infraDir, 0755))
		require.NoError(t, os.WriteFile(
			filepath.Join(repoDir, "config.yaml.tmpl"),
			[]byte("{{ .x | badFunc }}"), // invalid template
			0644,
		))

		gitOps := &mockGitOps{
			syncChanged: true,
			syncBefore:  "aaa111",
			syncAfter:   "bbb222",
		}

		alerter := &mockAlertSender{}

		cfg := &Config{
			LockFile:         lockFile,
			StateFile:        stateFile,
			RepoDir:          repoDir,
			StagingDir:       filepath.Join(tmpDir, "staging"),
			LocalAppdataPath: appdataDir,
			InfraSubDir:      ".",
			SecretsFiles:     []string{},
			OnFailure:        true,
		}
		r := NewReconciler(cfg,
			WithGitOperations(gitOps),
			WithAlerter(alerter),
		)

		err := r.Run(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to render templates")
		assert.Equal(t, 1, alerter.deployFailureCalls)
	})

	t.Run("full non-dry-run local deploy with backup and cleanup", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockFile := filepath.Join(tmpDir, "reconcile.lock")
		stateFile := filepath.Join(tmpDir, "state.json")
		repoDir := filepath.Join(tmpDir, "repo")
		stagingDir := filepath.Join(tmpDir, "staging")
		appdataDir := filepath.Join(tmpDir, "appdata")
		backupDir := filepath.Join(tmpDir, "backups")

		require.NoError(t, os.MkdirAll(appdataDir, 0755))

		// Create infra source with full directory structure that deployLocal expects.
		// RenderDirectory copies non-template files from repoDir/unraid/ -> staging/unraid/,
		// then deployLocal syncs staging/unraid/appdata/* -> appdata/*.
		infraDir := filepath.Join(repoDir, "unraid")
		require.NoError(t, os.MkdirAll(filepath.Join(infraDir, "appdata", "traefik"), 0755))
		require.NoError(t, os.MkdirAll(filepath.Join(infraDir, "appdata", "agentgateway"), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(infraDir, "appdata", "agentgateway", "config.yaml"), []byte("port: 8080"), 0644))
		require.NoError(t, os.MkdirAll(filepath.Join(infraDir, "appdata", "authelia"), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(infraDir, "appdata", "authelia", "configuration.yml"), []byte("server:\n  port: 9091"), 0644))
		require.NoError(t, os.MkdirAll(filepath.Join(infraDir, "appdata", "gatus"), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(infraDir, "appdata", "gatus", "config.yaml"), []byte("endpoints: []"), 0644))
		require.NoError(t, os.MkdirAll(filepath.Join(infraDir, "appdata", "tailscale-gateway"), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(infraDir, "appdata", "tailscale-gateway", "serve.json"), []byte("{}"), 0644))
		require.NoError(t, os.MkdirAll(filepath.Join(infraDir, "compose"), 0755))

		gitOps := &mockGitOps{
			syncChanged: true,
			syncBefore:  "aaa111",
			syncAfter:   "bbb222",
		}

		// Deploy ops that use content hash sync and skip compose up
		deploy := &DeployOps{DryRun: false, ProjectName: "test", ContentHashSync: true}

		cfg := &Config{
			DryRun:           false, // Non-dry-run to exercise backup + cleanup
			LockFile:         lockFile,
			StateFile:        stateFile,
			RepoDir:          repoDir,
			StagingDir:       stagingDir,
			LocalAppdataPath: appdataDir,
			BackupDir:        backupDir,
			BackupsToKeep:    3,
			InfraSubDir:      ".",
			SecretsFiles:     []string{},
		}
		r := NewReconciler(cfg,
			WithGitOperations(gitOps),
			WithDeployOps(deploy),
		)

		err := r.Run(context.Background())
		require.NoError(t, err)

		// Verify state was saved
		saved := LoadState(stateFile)
		assert.Equal(t, "bbb222", saved.LastDeployedCommit)
		assert.Equal(t, 0, saved.AttemptCount)
		assert.Equal(t, 1, saved.DeployCount)

		// Staging should be cleaned up
		_, statErr := os.Stat(stagingDir)
		assert.True(t, os.IsNotExist(statErr), "staging dir should be removed after deploy")
	})

	t.Run("deploy failure sends alert and returns error", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockFile := filepath.Join(tmpDir, "reconcile.lock")
		stateFile := filepath.Join(tmpDir, "state.json")
		repoDir := filepath.Join(tmpDir, "repo")
		stagingDir := filepath.Join(tmpDir, "staging")

		// Create infra source
		infraDir := filepath.Join(repoDir, "unraid")
		require.NoError(t, os.MkdirAll(infraDir, 0755))

		gitOps := &mockGitOps{
			syncChanged: true,
			syncBefore:  "aaa111",
			syncAfter:   "bbb222",
		}

		alerter := &mockAlertSender{}

		cfg := &Config{
			DryRun:           false,
			LockFile:         lockFile,
			StateFile:        stateFile,
			RepoDir:          repoDir,
			StagingDir:       stagingDir,
			LocalAppdataPath: "/nonexistent/appdata", // Force remote mode
			InfraSubDir:      ".",
			SecretsFiles:     []string{},
			OnFailure:        true,
		}
		r := NewReconciler(cfg,
			WithGitOperations(gitOps),
			WithAlerter(alerter),
		)

		err := r.Run(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "deployment failed")
		assert.Equal(t, 1, alerter.deployFailureCalls)
	})

	t.Run("recovery alert sent after previous failure", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockFile := filepath.Join(tmpDir, "reconcile.lock")
		stateFile := filepath.Join(tmpDir, "state.json")
		repoDir := filepath.Join(tmpDir, "repo")
		stagingDir := filepath.Join(tmpDir, "staging")
		appdataDir := filepath.Join(tmpDir, "appdata")

		require.NoError(t, os.MkdirAll(appdataDir, 0755))

		infraDir := filepath.Join(repoDir, "unraid")
		require.NoError(t, os.MkdirAll(infraDir, 0755))

		// Pre-save state with previous failure on same commit
		state := &DeployState{
			SchemaVersion:       2,
			LastDeployedCommit:  "oldcommit",
			LastAttemptedCommit: "bbb222",
			AttemptCount:        2,
		}
		require.NoError(t, SaveState(stateFile, state))

		gitOps := &mockGitOps{
			syncChanged: true,
			syncBefore:  "aaa111",
			syncAfter:   "bbb222",
		}

		alerter := &mockAlertSender{}

		cfg := &Config{
			DryRun:           true,
			LockFile:         lockFile,
			StateFile:        stateFile,
			RepoDir:          repoDir,
			StagingDir:       stagingDir,
			LocalAppdataPath: appdataDir,
			InfraSubDir:      ".",
			SecretsFiles:     []string{},
			OnFailure:        true,
			OnSuccess:        true,
		}
		r := NewReconciler(cfg,
			WithGitOperations(gitOps),
			WithAlerter(alerter),
		)

		err := r.Run(context.Background())
		require.NoError(t, err)

		// Recovery alert should be sent (AttemptCount was 3 before success)
		assert.Equal(t, 1, alerter.deployRecoveryCalls)
		assert.Equal(t, 1, alerter.deploySuccessCalls)
	})
}

// --- OnFailure / OnSuccess gate tests ---

func TestSyncRepoFailureSendsAlert(t *testing.T) {
	t.Run("syncRepo failure triggers failure alert when OnFailure=true", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockFile := filepath.Join(tmpDir, "reconcile.lock")
		stateFile := filepath.Join(tmpDir, "state.json")

		gitOps := &mockGitOps{
			syncErr: fmt.Errorf("network timeout"),
		}
		alerter := &mockAlertSender{}

		cfg := &Config{
			LockFile:  lockFile,
			StateFile: stateFile,
			OnFailure: true,
		}
		r := NewReconciler(cfg,
			WithGitOperations(gitOps),
			WithAlerter(alerter),
		)

		err := r.Run(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to sync repository")
		assert.Equal(t, 1, alerter.deployFailureCalls)

		// State should have been saved with attempt tracking.
		state := LoadState(stateFile)
		assert.Equal(t, 1, state.AttemptCount)
	})

	t.Run("syncRepo failure does NOT trigger alert when OnFailure=false", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockFile := filepath.Join(tmpDir, "reconcile.lock")
		stateFile := filepath.Join(tmpDir, "state.json")

		gitOps := &mockGitOps{
			syncErr: fmt.Errorf("network timeout"),
		}
		alerter := &mockAlertSender{}

		cfg := &Config{
			LockFile:  lockFile,
			StateFile: stateFile,
			OnFailure: false,
		}
		r := NewReconciler(cfg,
			WithGitOperations(gitOps),
			WithAlerter(alerter),
		)

		err := r.Run(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to sync repository")
		assert.Equal(t, 0, alerter.deployFailureCalls)
	})
}

func TestOnSuccessGate(t *testing.T) {
	t.Run("success alert suppressed when OnSuccess=false", func(t *testing.T) {
		alerter := &mockAlertSender{}
		cfg := &Config{OnSuccess: false}
		r := NewReconciler(cfg, WithAlerter(alerter))
		r.lastCommit = "abc123"
		r.sendSuccessAlert(context.Background())
		assert.Equal(t, 0, alerter.deploySuccessCalls)
	})
}

func TestOnFailureGate(t *testing.T) {
	t.Run("failure alert suppressed when OnFailure=false for decrypt failure", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockFile := filepath.Join(tmpDir, "reconcile.lock")
		stateFile := filepath.Join(tmpDir, "state.json")
		repoDir := filepath.Join(tmpDir, "repo")
		appdataDir := filepath.Join(tmpDir, "appdata")

		require.NoError(t, os.MkdirAll(appdataDir, 0755))

		infraDir := filepath.Join(repoDir, "unraid")
		require.NoError(t, os.MkdirAll(infraDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(repoDir, "secrets.yaml"), []byte("dummy"), 0644))

		gitOps := &mockGitOps{
			syncChanged: true,
			syncBefore:  "aaa",
			syncAfter:   "bbb",
		}
		mockSops := &mockSecretsDecryptor{
			decryptErr: fmt.Errorf("age key not available"),
		}
		alerter := &mockAlertSender{}

		cfg := &Config{
			LockFile:         lockFile,
			StateFile:        stateFile,
			RepoDir:          repoDir,
			StagingDir:       filepath.Join(tmpDir, "staging"),
			LocalAppdataPath: appdataDir,
			InfraSubDir:      ".",
			SecretsFiles:     []string{"secrets.yaml"},
			OnFailure:        false,
		}
		r := NewReconciler(cfg,
			WithGitOperations(gitOps),
			WithSecretsDecryptor(mockSops),
			WithAlerter(alerter),
		)

		err := r.Run(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decrypt secrets")
		assert.Equal(t, 0, alerter.deployFailureCalls)
	})

	t.Run("failure alert suppressed when OnFailure=false for template failure", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockFile := filepath.Join(tmpDir, "reconcile.lock")
		stateFile := filepath.Join(tmpDir, "state.json")
		repoDir := filepath.Join(tmpDir, "repo")
		appdataDir := filepath.Join(tmpDir, "appdata")

		require.NoError(t, os.MkdirAll(appdataDir, 0755))

		infraDir := filepath.Join(repoDir, "unraid")
		require.NoError(t, os.MkdirAll(infraDir, 0755))
		require.NoError(t, os.WriteFile(
			filepath.Join(repoDir, "config.yaml.tmpl"),
			[]byte("{{ .x | badFunc }}"),
			0644,
		))

		gitOps := &mockGitOps{
			syncChanged: true,
			syncBefore:  "aaa111",
			syncAfter:   "bbb222",
		}
		alerter := &mockAlertSender{}

		cfg := &Config{
			LockFile:         lockFile,
			StateFile:        stateFile,
			RepoDir:          repoDir,
			StagingDir:       filepath.Join(tmpDir, "staging"),
			LocalAppdataPath: appdataDir,
			InfraSubDir:      ".",
			SecretsFiles:     []string{},
			OnFailure:        false,
		}
		r := NewReconciler(cfg,
			WithGitOperations(gitOps),
			WithAlerter(alerter),
		)

		err := r.Run(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to render templates")
		assert.Equal(t, 0, alerter.deployFailureCalls)
	})

	t.Run("failure alert suppressed when OnFailure=false for deploy failure", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockFile := filepath.Join(tmpDir, "reconcile.lock")
		stateFile := filepath.Join(tmpDir, "state.json")
		repoDir := filepath.Join(tmpDir, "repo")
		stagingDir := filepath.Join(tmpDir, "staging")

		infraDir := filepath.Join(repoDir, "unraid")
		require.NoError(t, os.MkdirAll(infraDir, 0755))

		gitOps := &mockGitOps{
			syncChanged: true,
			syncBefore:  "aaa111",
			syncAfter:   "bbb222",
		}
		alerter := &mockAlertSender{}

		cfg := &Config{
			DryRun:           false,
			LockFile:         lockFile,
			StateFile:        stateFile,
			RepoDir:          repoDir,
			StagingDir:       stagingDir,
			LocalAppdataPath: "/nonexistent/appdata",
			InfraSubDir:      ".",
			SecretsFiles:     []string{},
			OnFailure:        false,
		}
		r := NewReconciler(cfg,
			WithGitOperations(gitOps),
			WithAlerter(alerter),
		)

		err := r.Run(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "deployment failed")
		assert.Equal(t, 0, alerter.deployFailureCalls)
	})
}

func TestDefaultConfigOnFailureDefault(t *testing.T) {
	cfg := DefaultConfig()
	assert.True(t, cfg.OnFailure, "OnFailure should default to true")
	assert.False(t, cfg.OnSuccess, "OnSuccess should default to false")
}

func TestReloadProjectConfig_AlertGates(t *testing.T) {
	t.Run("reload updates OnFailure and OnSuccess from repo config", func(t *testing.T) {
		onFailure := false
		onSuccess := true
		cfg := &Config{
			OnFailure: true,
			OnSuccess: false,
			ConfigReloader: func(string) (*ReloadedConfig, error) {
				return &ReloadedConfig{
					OnFailure: &onFailure,
					OnSuccess: &onSuccess,
				}, nil
			},
		}
		r := NewReconciler(cfg)

		r.reloadProjectConfig()

		assert.False(t, r.config.OnFailure, "OnFailure should be updated to false from repo config")
		assert.True(t, r.config.OnSuccess, "OnSuccess should be updated to true from repo config")
	})

	t.Run("nil values in ReloadedConfig preserve existing config", func(t *testing.T) {
		cfg := &Config{
			OnFailure: true,
			OnSuccess: false,
			ConfigReloader: func(string) (*ReloadedConfig, error) {
				return &ReloadedConfig{}, nil
			},
		}
		r := NewReconciler(cfg)

		r.reloadProjectConfig()

		assert.True(t, r.config.OnFailure, "OnFailure should remain true when not reloaded")
		assert.False(t, r.config.OnSuccess, "OnSuccess should remain false when not reloaded")
	})
}

// --- MinLen tests ---

func TestMinLen(t *testing.T) {
	tests := []struct {
		name string
		s    string
		n    int
		want int
	}{
		{"string longer than n", "abcdefgh", 4, 4},
		{"string shorter than n", "ab", 4, 2},
		{"string equal to n", "abcd", 4, 4},
		{"empty string", "", 4, 0},
		{"zero n", "abc", 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, MinLen(tt.s, tt.n))
		})
	}
}

func TestRunLockContention(t *testing.T) {
	tmpDir := t.TempDir()
	lockFile := filepath.Join(tmpDir, "reconcile.lock")
	stateFile := filepath.Join(tmpDir, "state.json")
	repoDir := filepath.Join(tmpDir, "repo")
	stagingDir := filepath.Join(tmpDir, "staging")

	require.NoError(t, os.MkdirAll(repoDir, 0755))

	gitOps := &mockGitOps{syncChanged: true, syncBefore: "a", syncAfter: "b"}

	cfg := &Config{
		DryRun:     true,
		LockFile:   lockFile,
		StateFile:  stateFile,
		RepoDir:    repoDir,
		StagingDir: stagingDir,
	}

	// Acquire lock manually first.
	r1 := NewReconciler(cfg, WithGitOperations(gitOps))
	require.NoError(t, r1.acquireLock())
	defer r1.releaseLock()

	// Second reconciler should fail to acquire lock.
	r2 := NewReconciler(cfg, WithGitOperations(gitOps))
	err := r2.Run(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to acquire lock")
}

func TestDeployLocalSyncErrors(t *testing.T) {
	t.Run("missing staging subdirectory returns discovery error", func(t *testing.T) {
		tmpDir := t.TempDir()
		stagingDir := filepath.Join(tmpDir, "staging")
		appdataDir := filepath.Join(tmpDir, "appdata")
		require.NoError(t, os.MkdirAll(appdataDir, 0755))
		// Don't create the staging subdir — discovery will fail.

		deploy := &DeployOps{DryRun: false, ContentHashSync: true}
		cfg := &Config{
			DryRun:           false,
			StagingDir:       stagingDir,
			InfraSubDir:      "unraid",
			LocalAppdataPath: appdataDir,
		}
		r := NewReconciler(cfg, WithDeployOps(deploy))

		_, err := r.deployLocal(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "discover deploy targets")
	})

	t.Run("compose file present triggers compose up error", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockFile := filepath.Join(tmpDir, "reconcile.lock")
		stateFile := filepath.Join(tmpDir, "state.json")
		repoDir := filepath.Join(tmpDir, "repo")
		stagingDir := filepath.Join(tmpDir, "staging")
		appdataDir := filepath.Join(tmpDir, "appdata")

		require.NoError(t, os.MkdirAll(appdataDir, 0755))

		// Create infra structure directly in repo root (InfraSubDir: ".").
		require.NoError(t, os.MkdirAll(filepath.Join(repoDir, "appdata", "traefik"), 0755))
		require.NoError(t, os.MkdirAll(filepath.Join(repoDir, "appdata", "agentgateway"), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(repoDir, "appdata", "agentgateway", "config.yaml"), []byte("port: 8080"), 0644))
		require.NoError(t, os.MkdirAll(filepath.Join(repoDir, "appdata", "authelia"), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(repoDir, "appdata", "authelia", "configuration.yml"), []byte("server: {}"), 0644))
		require.NoError(t, os.MkdirAll(filepath.Join(repoDir, "appdata", "gatus"), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(repoDir, "appdata", "gatus", "config.yaml"), []byte("endpoints: []"), 0644))
		require.NoError(t, os.MkdirAll(filepath.Join(repoDir, "appdata", "tailscale-gateway"), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(repoDir, "appdata", "tailscale-gateway", "serve.json"), []byte("{}"), 0644))
		require.NoError(t, os.MkdirAll(filepath.Join(repoDir, "compose"), 0755))
		// This compose file triggers the compose-up path in deployLocal (non-dry-run).
		require.NoError(t, os.WriteFile(filepath.Join(repoDir, "compose", "docker-compose.yml"), []byte("version: '3'"), 0644))

		gitOps := &mockGitOps{syncChanged: true, syncBefore: "a", syncAfter: "b"}
		deploy := &DeployOps{DryRun: false, ProjectName: "test", ContentHashSync: true}

		cfg := &Config{
			DryRun:           false,
			LockFile:         lockFile,
			StateFile:        stateFile,
			RepoDir:          repoDir,
			StagingDir:       stagingDir,
			LocalAppdataPath: appdataDir,
			InfraSubDir:      ".",
			SecretsFiles:     []string{},
		}
		r := NewReconciler(cfg, WithGitOperations(gitOps), WithDeployOps(deploy))

		// Will fail at compose-up because docker isn't available in tests.
		err := r.Run(context.Background())
		require.Error(t, err)
		// The error comes from compose-up: all files failed in isolated mode.
		assert.Contains(t, err.Error(), "all compose files failed to deploy")
	})
}

func TestRunSaveStateErrorInAttemptTracking(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping permission test when running as root")
	}

	// Put the state file in a read-only directory so SaveState fails during
	// attempt tracking (line 335 of reconcile.go).
	tmpDir := t.TempDir()
	lockFile := filepath.Join(tmpDir, "reconcile.lock")
	repoDir := filepath.Join(tmpDir, "repo")
	stagingDir := filepath.Join(tmpDir, "staging")
	appdataDir := filepath.Join(tmpDir, "appdata")

	require.NoError(t, os.MkdirAll(appdataDir, 0755))

	// Create infra structure directly in repo root (InfraSubDir: ".").
	require.NoError(t, os.MkdirAll(filepath.Join(repoDir, "appdata", "traefik"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(repoDir, "appdata", "agentgateway"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "appdata", "agentgateway", "config.yaml"), []byte("port: 8080"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(repoDir, "appdata", "authelia"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "appdata", "authelia", "configuration.yml"), []byte("server: {}"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(repoDir, "appdata", "gatus"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "appdata", "gatus", "config.yaml"), []byte("endpoints: []"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(repoDir, "appdata", "tailscale-gateway"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "appdata", "tailscale-gateway", "serve.json"), []byte("{}"), 0644))
	require.NoError(t, os.MkdirAll(filepath.Join(repoDir, "compose"), 0755))

	// State dir is read-only, so SaveState calls will fail.
	stateDir := filepath.Join(tmpDir, "state-ro")
	require.NoError(t, os.MkdirAll(stateDir, 0755))
	stateFile := filepath.Join(stateDir, "state.json")
	require.NoError(t, os.Chmod(stateDir, 0555))
	t.Cleanup(func() { _ = os.Chmod(stateDir, 0755) })

	gitOps := &mockGitOps{syncChanged: true, syncBefore: "a", syncAfter: "b"}
	deploy := &DeployOps{DryRun: true, ContentHashSync: true}

	cfg := &Config{
		DryRun:           true,
		LockFile:         lockFile,
		StateFile:        stateFile,
		RepoDir:          repoDir,
		StagingDir:       stagingDir,
		LocalAppdataPath: appdataDir,
		InfraSubDir:      ".",
		SecretsFiles:     []string{},
	}
	r := NewReconciler(cfg, WithGitOperations(gitOps), WithDeployOps(deploy))

	// Run should fail because the pre-deploy NeedsRedeploy marker cannot be persisted.
	// Without the safety net, a partial deploy failure would not be retried.
	err := r.Run(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to persist pre-deploy redeploy marker")
}

// --- Health Gate pipeline integration tests ---

func TestRunHealthGate_SkipsWhenNoCriticalContainers(t *testing.T) {
	cfg := &Config{}
	r := NewReconciler(cfg)
	state := &DeployState{}

	err := r.runHealthGate(context.Background(), state)
	require.NoError(t, err)
}

func TestRunHealthGate_SkipsWhenDryRun(t *testing.T) {
	cfg := &Config{
		DryRun:             true,
		CriticalContainers: []string{"traefik"},
	}
	r := NewReconciler(cfg)
	state := &DeployState{}

	err := r.runHealthGate(context.Background(), state)
	require.NoError(t, err)
}

func TestRunHealthGate_SkipsForRemoteDeploy(t *testing.T) {
	cfg := &Config{
		TargetHost:         "user@remote",
		CriticalContainers: []string{"traefik"},
	}
	r := NewReconciler(cfg)
	state := &DeployState{}

	err := r.runHealthGate(context.Background(), state)
	require.NoError(t, err)
}

func TestRunHealthGate_SkipsWhenNoDockerClient(t *testing.T) {
	cfg := &Config{
		CriticalContainers: []string{"traefik"},
		LocalAppdataPath:   t.TempDir(), // Ensures isLocalMode() returns true.
	}
	r := NewReconciler(cfg)
	state := &DeployState{}

	err := r.runHealthGate(context.Background(), state)
	require.NoError(t, err)
}

func TestRunHealthGate_PassesWhenAllHealthy(t *testing.T) {
	mockAPI := newReconcileMockDockerAPI()
	mockAPI.containerInspectFunc = func(_ context.Context, name string) (container.InspectResponse, error) {
		return makeInspectResponse(name, "running", &container.Health{Status: "healthy"}), nil
	}
	client := docker.NewClientWithAPI(mockAPI)

	cfg := &Config{
		CriticalContainers: []string{"traefik", "authelia"},
		HealthGateTimeout:  5 * time.Second,
		LocalAppdataPath:   t.TempDir(), // Ensures isLocalMode() returns true.
	}
	r := NewReconciler(cfg, WithDockerClient(client))
	state := &DeployState{}

	err := r.runHealthGate(context.Background(), state)
	require.NoError(t, err)
}

func TestRunHealthGate_FailsWhenUnhealthy(t *testing.T) {
	mockAPI := newReconcileMockDockerAPI()
	mockAPI.containerInspectFunc = func(_ context.Context, name string) (container.InspectResponse, error) {
		if name == "authelia" {
			return makeInspectResponse(name, "running", &container.Health{Status: "unhealthy"}), nil
		}
		return makeInspectResponse(name, "running", &container.Health{Status: "healthy"}), nil
	}
	client := docker.NewClientWithAPI(mockAPI)

	cfg := &Config{
		CriticalContainers: []string{"traefik", "authelia"},
		HealthGateTimeout:  1 * time.Second,
		LocalAppdataPath:   t.TempDir(), // Ensures isLocalMode() returns true.
	}
	r := NewReconciler(cfg, WithDockerClient(client))
	state := &DeployState{}

	err := r.runHealthGate(context.Background(), state)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "authelia")
}

func TestDeployRemoteErrorPropagation(t *testing.T) {
	t.Run("no target host returns error", func(t *testing.T) {
		cfg := &Config{
			TargetHost:       "",
			LocalAppdataPath: "/nonexistent/path",
		}
		r := NewReconciler(cfg)

		err := r.deployRemote(context.Background(), map[string]any{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no target host")
	})

	t.Run("first sync failure returns error", func(t *testing.T) {
		// deployRemote calls DeployRemote (tar-over-SSH) as the first SSH op.
		// Without a real SSH target, this fails. Verify the error propagates.
		cfg := &Config{
			TargetHost:        "user@invalid-host-that-does-not-exist",
			LocalAppdataPath:  "/nonexistent/path",
			StagingDir:        t.TempDir(),
			RemoteAppdataPath: "/mnt/user/appdata",
		}
		deploy := &DeployOps{DryRun: false}
		r := NewReconciler(cfg, WithDeployOps(deploy))

		err := r.deployRemote(context.Background(), nil)
		require.Error(t, err)
		// Should NOT contain "Deployment complete" in any wrapped error.
	})

	t.Run("signal failure does not cause deploy error", func(t *testing.T) {
		// With DryRun on the deployer, all SSH ops return nil.
		// The reconciler config DryRun=false so the compose-up/signal block runs,
		// but the deploy methods skip execution due to deployer's dry run.
		tmpDir := t.TempDir()
		stagingUnraid := filepath.Join(tmpDir, "unraid")
		// Create all required staging directories so file syncs succeed in dry run.
		for _, subdir := range []string{
			"appdata/traefik",
			"appdata/agentgateway",
			"appdata/authelia",
			"appdata/gatus",
			"appdata/tailscale-gateway",
			"compose",
		} {
			require.NoError(t, os.MkdirAll(filepath.Join(stagingUnraid, subdir), 0755))
		}
		// Create required config files so DeployRemoteFile doesn't fail on open.
		for _, file := range []string{
			"appdata/agentgateway/config.yaml",
			"appdata/authelia/configuration.yml",
			"appdata/gatus/config.yaml",
			"appdata/tailscale-gateway/serve.json",
			"compose/core.yml",
		} {
			require.NoError(t, os.WriteFile(filepath.Join(stagingUnraid, file), []byte("test"), 0644))
		}

		cfg := &Config{
			TargetHost:        "user@testhost",
			LocalAppdataPath:  "/nonexistent/path",
			StagingDir:        tmpDir,
			InfraSubDir:       "unraid",
			RemoteAppdataPath: "/mnt/user/appdata",
			DryRun:            false,
		}
		// DryRun on deployer makes all SSH calls return nil, including ComposeUpRemote.
		deploy := &DeployOps{DryRun: true}
		r := NewReconciler(cfg, WithDeployOps(deploy))

		err := r.deployRemote(context.Background(), nil)
		// All SSH ops are dry-run no-ops, compose up and signal both "succeed".
		require.NoError(t, err)
	})
}

func TestRunRemoteDeployFailure_StateNotUpdated(t *testing.T) {
	// When deployRemote returns an error, Run() should NOT update LastDeployedCommit.
	tmpDir := t.TempDir()
	lockFile := filepath.Join(tmpDir, "reconcile.lock")
	stateFile := filepath.Join(tmpDir, "state.json")

	gitOps := &mockGitOps{syncChanged: true, syncBefore: "old-commit", syncAfter: "new-commit"}

	cfg := &Config{
		DryRun:            false,
		Force:             false,
		LockFile:          lockFile,
		StateFile:         stateFile,
		RepoDir:           filepath.Join(tmpDir, "repo"),
		StagingDir:        filepath.Join(tmpDir, "staging"),
		LocalAppdataPath:  "/nonexistent/path", // Force remote mode.
		TargetHost:        "user@invalid-host",
		RemoteAppdataPath: "/mnt/user/appdata",
		InfraSubDir:       ".",
		SecretsFiles:      []string{},
	}

	deploy := &DeployOps{DryRun: false}
	r := NewReconciler(cfg, WithGitOperations(gitOps), WithDeployOps(deploy))

	err := r.Run(context.Background())
	require.Error(t, err)

	// Verify state file was NOT updated with the new commit.
	state := LoadState(stateFile)
	assert.NotEqual(t, "new-commit", state.LastDeployedCommit,
		"LastDeployedCommit should NOT be updated when deploy fails")
}

// --- Multi-target tests ---

func TestTarget_IsDefault(t *testing.T) {
	tests := []struct {
		name     string
		target   Target
		expected bool
	}{
		{"default target", Target{Name: DefaultTargetName}, true},
		{"named target", Target{Name: "unraid"}, false},
		{"empty name", Target{Name: ""}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.target.IsDefault())
		})
	}
}

func TestResolveTargets_ImplicitDefault(t *testing.T) {
	cfg := &Config{
		TargetHost:         "user@pi",
		LocalAppdataPath:   "/mnt/appdata",
		RemoteAppdataPath:  "/mnt/user/appdata",
		ProjectName:        "homelab",
		StateFile:          "/var/lib/bosun/deploy-state.json",
		StagingDir:         "/app/staging",
		CriticalContainers: []string{"traefik"},
		PostSyncHooks:      []PostSyncHook{{Container: "traefik", Paths: []string{"*.toml"}}},
		DeploySyncPaths:    []string{"compose/**"},
		DeploySyncExclude:  []string{"*.bak"},
	}

	targets := cfg.ResolveTargets()
	require.Len(t, targets, 1)

	def := targets[0]
	assert.Equal(t, DefaultTargetName, def.Name)
	assert.True(t, def.IsDefault())
	assert.Equal(t, "user@pi", def.TargetHost)
	assert.Equal(t, "/mnt/appdata", def.LocalAppdataPath)
	assert.Equal(t, "/mnt/user/appdata", def.RemoteAppdataPath)
	assert.Equal(t, "homelab", def.ProjectName)
	assert.Equal(t, "/var/lib/bosun/deploy-state.json", def.StateFile)
	assert.Equal(t, "/app/staging", def.StagingDir)
	assert.Equal(t, []string{"traefik"}, def.CriticalContainers)
	assert.Len(t, def.PostSyncHooks, 1)
	assert.Equal(t, []string{"compose/**"}, def.DeploySyncPaths)
	assert.Equal(t, []string{"*.bak"}, def.DeploySyncExclude)
}

func TestResolveTargets_ExplicitTargets(t *testing.T) {
	cfg := &Config{
		TargetHost: "should-be-ignored",
		Targets: []Target{
			{Name: "unraid", TargetHost: "user@unraid"},
			{Name: "pi", TargetHost: "user@pi"},
		},
	}

	targets := cfg.ResolveTargets()
	require.Len(t, targets, 2)
	assert.Equal(t, "unraid", targets[0].Name)
	assert.Equal(t, "user@unraid", targets[0].TargetHost)
	assert.Equal(t, "pi", targets[1].Name)
	assert.Equal(t, "user@pi", targets[1].TargetHost)
}

func TestTargetStateFile(t *testing.T) {
	tests := []struct {
		name     string
		baseDir  string
		target   Target
		expected string
	}{
		{
			"default target uses legacy path",
			"/var/lib/bosun",
			Target{Name: DefaultTargetName},
			"/var/lib/bosun/deploy-state.json",
		},
		{
			"named target uses name suffix",
			"/var/lib/bosun",
			Target{Name: "unraid"},
			"/var/lib/bosun/deploy-state-unraid.json",
		},
		{
			"custom state file override",
			"/var/lib/bosun",
			Target{Name: "pi", StateFile: "/custom/state.json"},
			"/custom/state.json",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, TargetStateFile(tt.baseDir, tt.target))
		})
	}
}

func TestTargetStagingDir(t *testing.T) {
	tests := []struct {
		name     string
		baseDir  string
		target   Target
		expected string
	}{
		{
			"default target uses base dir",
			"/app/staging",
			Target{Name: DefaultTargetName},
			"/app/staging",
		},
		{
			"named target uses subdirectory",
			"/app/staging",
			Target{Name: "pi"},
			"/app/staging/pi",
		},
		{
			"custom staging dir override",
			"/app/staging",
			Target{Name: "unraid", StagingDir: "/custom/staging"},
			"/custom/staging",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, TargetStagingDir(tt.baseDir, tt.target))
		})
	}
}

func TestTargetLockFile(t *testing.T) {
	tests := []struct {
		name     string
		baseDir  string
		target   Target
		expected string
	}{
		{
			"default target uses legacy lock",
			"/var/run/bosun",
			Target{Name: DefaultTargetName},
			"/var/run/bosun/reconcile.lock",
		},
		{
			"named target uses name suffix",
			"/var/run/bosun",
			Target{Name: "unraid"},
			"/var/run/bosun/reconcile-unraid.lock",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, TargetLockFile(tt.baseDir, tt.target))
		})
	}
}

func TestConfigForTarget_DefaultTarget(t *testing.T) {
	base := DefaultConfig()
	base.TargetHost = "user@original"
	base.ProjectName = "original-project"

	target := Target{
		Name:              DefaultTargetName,
		TargetHost:        "user@original",
		LocalAppdataPath:  "/mnt/appdata",
		RemoteAppdataPath: "/mnt/user/appdata",
		ProjectName:       "original-project",
	}

	cfg := base.ConfigForTarget(target)

	// Default target preserves the base paths (no subdirectory nesting).
	assert.Equal(t, base.StagingDir, cfg.StagingDir, "default target should use base staging dir")
	assert.Equal(t, filepath.Join(filepath.Dir(base.StateFile), DefaultStateFile), cfg.StateFile, "default target should use legacy state file")
	assert.Equal(t, filepath.Join(DefaultLockDir, "reconcile.lock"), cfg.LockFile, "default target should use legacy lock file")
	assert.Equal(t, "user@original", cfg.TargetHost)
	assert.Equal(t, "original-project", cfg.ProjectName)
}

func TestConfigForTarget_NamedTarget(t *testing.T) {
	base := DefaultConfig()

	target := Target{
		Name:               "unraid",
		TargetHost:         "user@unraid",
		LocalAppdataPath:   "/mnt/custom/appdata",
		RemoteAppdataPath:  "/mnt/user/custom/appdata",
		ProjectName:        "homelab-unraid",
		CriticalContainers: []string{"traefik"},
		PostSyncHooks:      []PostSyncHook{{Container: "traefik", Paths: []string{"*.toml"}}},
		DeploySyncPaths:    []string{"compose/**"},
		DeploySyncExclude:  []string{"*.bak"},
	}

	cfg := base.ConfigForTarget(target)

	// Named target gets subdirectory paths.
	assert.Equal(t, filepath.Join(base.StagingDir, "unraid"), cfg.StagingDir)
	assert.Equal(t, filepath.Join(filepath.Dir(base.StateFile), "deploy-state-unraid.json"), cfg.StateFile)
	assert.Equal(t, filepath.Join(DefaultLockDir, "reconcile-unraid.lock"), cfg.LockFile)
	assert.Equal(t, "user@unraid", cfg.TargetHost)
	assert.Equal(t, "/mnt/custom/appdata", cfg.LocalAppdataPath)
	assert.Equal(t, "/mnt/user/custom/appdata", cfg.RemoteAppdataPath)
	assert.Equal(t, "homelab-unraid", cfg.ProjectName)
	assert.Equal(t, []string{"traefik"}, cfg.CriticalContainers)
	require.Len(t, cfg.PostSyncHooks, 1)
	assert.Equal(t, "traefik", cfg.PostSyncHooks[0].Container)
	assert.Equal(t, []string{"compose/**"}, cfg.DeploySyncPaths)
	assert.Equal(t, []string{"*.bak"}, cfg.DeploySyncExclude)

	// Base config should be unmodified.
	assert.Equal(t, DefaultConfig().StagingDir, base.StagingDir)
}

func TestConfigForTarget_PartialOverrides(t *testing.T) {
	base := DefaultConfig()
	base.CriticalContainers = []string{"global-container"}
	base.PostSyncHooks = []PostSyncHook{{Container: "global", Paths: []string{"*"}}}

	// Target with no overrides — inherits from base.
	target := Target{
		Name:       "pi",
		TargetHost: "user@pi",
	}

	cfg := base.ConfigForTarget(target)
	assert.Equal(t, []string{"global-container"}, cfg.CriticalContainers, "should inherit from base when target has none")
	assert.Len(t, cfg.PostSyncHooks, 1, "should inherit from base when target has none")
	assert.Equal(t, base.LocalAppdataPath, cfg.LocalAppdataPath, "should keep base when target is empty")
	assert.Equal(t, base.RemoteAppdataPath, cfg.RemoteAppdataPath, "should keep base when target is empty")
}

func TestConfigForTarget_ExplicitEmptySliceOverrides(t *testing.T) {
	base := DefaultConfig()
	base.CriticalContainers = []string{"global-container"}
	base.PostSyncHooks = []PostSyncHook{{Container: "global", Paths: []string{"*"}}}
	base.DeploySyncPaths = []string{"infra/**"}

	// Target with explicit empty slices — should opt out of base defaults.
	target := Target{
		Name:               "minimal",
		TargetHost:         "user@minimal",
		CriticalContainers: []string{},
		PostSyncHooks:      []PostSyncHook{},
		DeploySyncPaths:    []string{},
	}

	cfg := base.ConfigForTarget(target)
	assert.Empty(t, cfg.CriticalContainers, "explicit empty should override base, not inherit")
	assert.Empty(t, cfg.PostSyncHooks, "explicit empty should override base, not inherit")
	assert.Empty(t, cfg.DeploySyncPaths, "explicit empty should override base, not inherit")
}

func TestConfigForTarget_NilSliceInherits(t *testing.T) {
	base := DefaultConfig()
	base.CriticalContainers = []string{"global-container"}

	// Target with nil (unset) — should inherit from base.
	target := Target{
		Name:       "inherit",
		TargetHost: "user@inherit",
	}

	cfg := base.ConfigForTarget(target)
	assert.Equal(t, []string{"global-container"}, cfg.CriticalContainers, "nil should inherit from base")
}

func TestConfigForTarget_LockFilePreservesCustomDir(t *testing.T) {
	base := DefaultConfig()
	base.LockFile = "/custom/locks/reconcile.lock"

	target := Target{Name: "nas"}

	cfg := base.ConfigForTarget(target)
	assert.Contains(t, cfg.LockFile, "/custom/locks/", "per-target lock should use base config's lock directory")
	assert.Contains(t, cfg.LockFile, "nas", "per-target lock should include target name")
}

func TestValidateTargetName(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{"unraid", false},
		{"pi-4", false},
		{"nas_backup", false},
		{DefaultTargetName, false},
		{"../../etc", true},
		{"/tmp/evil", true},
		{"", true},
		{"has spaces", true},
		{"has.dots", true},
		{"-starts-with-dash", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTargetName(tt.name)
			if tt.wantErr {
				assert.Error(t, err, "name %q should be rejected", tt.name)
			} else {
				assert.NoError(t, err, "name %q should be accepted", tt.name)
			}
		})
	}
}

func TestResolveTargets_SkipsInvalidNames(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Targets = []Target{
		{Name: "good-target", TargetHost: "user@good"},
		{Name: "../../evil", TargetHost: "user@evil"},
		{Name: "also-good", TargetHost: "user@also"},
	}

	targets := cfg.ResolveTargets()
	assert.Len(t, targets, 2, "should skip the invalid target")
	assert.Equal(t, "good-target", targets[0].Name)
	assert.Equal(t, "also-good", targets[1].Name)
}

func TestConfigForTarget_DefaultTargetPreservesExactPaths(t *testing.T) {
	base := DefaultConfig()
	base.LockFile = "/tmp/custom.lock"
	base.StateFile = "/tmp/custom-state.json"
	base.StagingDir = "/tmp/custom-staging"

	target := Target{Name: DefaultTargetName}

	cfg := base.ConfigForTarget(target)
	assert.Equal(t, "/tmp/custom.lock", cfg.LockFile, "default target should preserve exact lock path")
	assert.Equal(t, "/tmp/custom-state.json", cfg.StateFile, "default target should preserve exact state path")
	assert.Equal(t, "/tmp/custom-staging", cfg.StagingDir, "default target should preserve exact staging path")
}

func TestMergeTargetSecrets(t *testing.T) {
	t.Run("scoped override replaces shared key", func(t *testing.T) {
		secrets := map[string]any{
			"db_password": "shared",
			"api_key":     "shared-api",
			"targets": map[string]any{
				"unraid": map[string]any{
					"db_password": "secret1",
				},
			},
		}

		merged := MergeTargetSecrets(secrets, "unraid")
		assert.Equal(t, "secret1", merged["db_password"])
		assert.Equal(t, "shared-api", merged["api_key"], "non-overridden keys preserved")
	})

	t.Run("no scope returns original", func(t *testing.T) {
		secrets := map[string]any{"db_password": "shared"}
		merged := MergeTargetSecrets(secrets, "")
		assert.Equal(t, secrets, merged)
	})

	t.Run("nil secrets returns nil", func(t *testing.T) {
		merged := MergeTargetSecrets(nil, "unraid")
		assert.Nil(t, merged)
	})

	t.Run("no targets key in secrets", func(t *testing.T) {
		secrets := map[string]any{"db_password": "shared"}
		merged := MergeTargetSecrets(secrets, "unraid")
		assert.Equal(t, "shared", merged["db_password"])
	})

	t.Run("scope not found in targets", func(t *testing.T) {
		secrets := map[string]any{
			"db_password": "shared",
			"targets": map[string]any{
				"pi": map[string]any{"db_password": "pi-secret"},
			},
		}
		merged := MergeTargetSecrets(secrets, "unraid")
		assert.Equal(t, "shared", merged["db_password"])
	})

	t.Run("does not mutate original", func(t *testing.T) {
		secrets := map[string]any{
			"db_password": "shared",
			"targets": map[string]any{
				"unraid": map[string]any{"db_password": "secret1"},
			},
		}

		merged := MergeTargetSecrets(secrets, "unraid")
		assert.Equal(t, "secret1", merged["db_password"])
		assert.Equal(t, "shared", secrets["db_password"], "original map should be unchanged")
	})

	t.Run("multiple scoped keys", func(t *testing.T) {
		secrets := map[string]any{
			"db_password":  "shared",
			"db_host":      "shared-host",
			"other_secret": "keep-me",
			"targets": map[string]any{
				"pi": map[string]any{
					"db_password": "pi-pw",
					"db_host":     "pi-host",
				},
			},
		}

		merged := MergeTargetSecrets(secrets, "pi")
		assert.Equal(t, "pi-pw", merged["db_password"])
		assert.Equal(t, "pi-host", merged["db_host"])
		assert.Equal(t, "keep-me", merged["other_secret"])
	})
}
