package reconcile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	assert.Equal(t, "infrastructure", cfg.InfraSubDir)
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
	assert.Equal(t, "/tmp/reconcile.lock", r.lockFile)
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

		secrets, err := r.decryptSecrets(nil)
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

		_, err := r.decryptSecrets(nil)
		assert.Error(t, err)
	})
}

func TestReconciler_RenderTemplates(t *testing.T) {
	t.Run("clears and creates staging directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		stagingDir := filepath.Join(tmpDir, "staging")
		repoDir := filepath.Join(tmpDir, "repo")
		infraDir := filepath.Join(repoDir, "infrastructure")

		require.NoError(t, os.MkdirAll(infraDir, 0755))

		cfg := &Config{
			RepoDir:    repoDir,
			StagingDir: stagingDir,
			InfraSubDir: "infrastructure",
		}
		r := NewReconciler(cfg)

		// Create old file in staging
		require.NoError(t, os.MkdirAll(stagingDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(stagingDir, "old.txt"), []byte("old"), 0644))

		secrets := map[string]any{}
		err := r.renderTemplates(nil, secrets)

		// May error if chezmoi not installed, but staging should be cleared
		if err != nil {
			t.Logf("renderTemplates error (expected if chezmoi not installed): %v", err)
		}

		// Old file should be removed
		assert.NoFileExists(t, filepath.Join(stagingDir, "old.txt"))
	})
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
