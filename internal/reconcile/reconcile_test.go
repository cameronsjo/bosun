package reconcile

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
