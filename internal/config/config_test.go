package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cameronsjo/bosun/internal/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// evalSymlinks resolves symlinks for path comparison (macOS /var -> /private/var).
func evalSymlinks(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}
	return resolved
}

func TestFindRoot_WithBosunDir(t *testing.T) {
	tmpDir := evalSymlinks(t, t.TempDir())

	// Create bosun directory with docker-compose.yml
	bosunDir := filepath.Join(tmpDir, "bosun")
	require.NoError(t, os.MkdirAll(bosunDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(bosunDir, "docker-compose.yml"), []byte("version: '3'"), 0644))

	// Create subdirectory to search from
	subDir := filepath.Join(tmpDir, "sub", "deep")
	require.NoError(t, os.MkdirAll(subDir, 0755))

	// Change to subdirectory
	originalWd, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(originalWd) }()

	require.NoError(t, os.Chdir(subDir))

	// FindRoot should find the project root
	root, err := FindRoot()
	require.NoError(t, err)
	assert.Equal(t, tmpDir, root)
}

func TestFindRoot_WithManifestDir(t *testing.T) {
	tmpDir := evalSymlinks(t, t.TempDir())

	// Create manifest directory (without bosun)
	manifestDir := filepath.Join(tmpDir, "manifest")
	require.NoError(t, os.MkdirAll(manifestDir, 0755))

	// Create subdirectory to search from
	subDir := filepath.Join(tmpDir, "sub", "deep")
	require.NoError(t, os.MkdirAll(subDir, 0755))

	// Change to subdirectory
	originalWd, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(originalWd) }()

	require.NoError(t, os.Chdir(subDir))

	// FindRoot should find the project root
	root, err := FindRoot()
	require.NoError(t, err)
	assert.Equal(t, tmpDir, root)
}

func TestFindRoot_BosunDirWithoutComposeFile(t *testing.T) {
	tmpDir := evalSymlinks(t, t.TempDir())

	// Create bosun directory WITHOUT docker-compose.yml
	bosunDir := filepath.Join(tmpDir, "bosun")
	require.NoError(t, os.MkdirAll(bosunDir, 0755))

	// Also create manifest directory so we have a valid root
	manifestDir := filepath.Join(tmpDir, "manifest")
	require.NoError(t, os.MkdirAll(manifestDir, 0755))

	// Change to bosun directory
	originalWd, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(originalWd) }()

	require.NoError(t, os.Chdir(bosunDir))

	// FindRoot should find root via manifest directory
	root, err := FindRoot()
	require.NoError(t, err)
	assert.Equal(t, tmpDir, root)
}

func TestFindRoot_NoProjectRoot(t *testing.T) {
	// Use a temporary directory with no bosun or manifest dirs
	tmpDir := t.TempDir()

	// Change to temp directory
	originalWd, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(originalWd) }()

	require.NoError(t, os.Chdir(tmpDir))

	// FindRoot should return error
	_, err = FindRoot()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "project root not found")
}

func TestFindRoot_FromProjectRoot(t *testing.T) {
	tmpDir := evalSymlinks(t, t.TempDir())

	// Create manifest directory
	manifestDir := filepath.Join(tmpDir, "manifest")
	require.NoError(t, os.MkdirAll(manifestDir, 0755))

	// Change to project root itself
	originalWd, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(originalWd) }()

	require.NoError(t, os.Chdir(tmpDir))

	// FindRoot should find the project root
	root, err := FindRoot()
	require.NoError(t, err)
	assert.Equal(t, tmpDir, root)
}

func TestLoad(t *testing.T) {
	tmpDir := evalSymlinks(t, t.TempDir())

	// Create manifest directory
	manifestDir := filepath.Join(tmpDir, "manifest")
	require.NoError(t, os.MkdirAll(manifestDir, 0755))

	// Create bosun directory with docker-compose.yml
	bosunDir := filepath.Join(tmpDir, "bosun")
	require.NoError(t, os.MkdirAll(bosunDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(bosunDir, "docker-compose.yml"), []byte("version: '3'"), 0644))

	// Change to project root
	originalWd, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(originalWd) }()

	require.NoError(t, os.Chdir(tmpDir))

	cfg, err := Load()
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Equal(t, tmpDir, cfg.Root)
	assert.Equal(t, filepath.Join(tmpDir, "manifest"), cfg.ManifestDir)
	assert.Equal(t, filepath.Join(tmpDir, "bosun", "docker-compose.yml"), cfg.ComposeFile)
	assert.Equal(t, filepath.Join(tmpDir, "manifest", ".bosun", "snapshots"), cfg.SnapshotsDir)
}

func TestLoad_NoProjectRoot(t *testing.T) {
	tmpDir := t.TempDir()

	// Change to temp directory (no project markers)
	originalWd, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(originalWd) }()

	require.NoError(t, os.Chdir(tmpDir))

	cfg, err := Load()
	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "project root not found")
}

func TestConfig_ProvisionsDir(t *testing.T) {
	cfg := &Config{
		ManifestDir: "/path/to/manifest",
	}

	assert.Equal(t, "/path/to/manifest/provisions", cfg.ProvisionsDir())
}

func TestConfig_ServicesDir(t *testing.T) {
	cfg := &Config{
		ManifestDir: "/path/to/manifest",
	}

	assert.Equal(t, "/path/to/manifest/services", cfg.ServicesDir())
}

func TestConfig_StacksDir(t *testing.T) {
	cfg := &Config{
		ManifestDir: "/path/to/manifest",
	}

	assert.Equal(t, "/path/to/manifest/stacks", cfg.StacksDir())
}

func TestConfig_OutputDir(t *testing.T) {
	cfg := &Config{
		ManifestDir: "/path/to/manifest",
	}

	assert.Equal(t, "/path/to/manifest/output", cfg.OutputDir())
}

func TestConfig_AllPathMethods(t *testing.T) {
	cfg := &Config{
		Root:            "/project",
		ManifestDir:    "/project/manifest",
		ComposeFile:    "/project/bosun/docker-compose.yml",
		SnapshotsDir:   "/project/manifest/.bosun/snapshots",
		infraContainers: []string{"traefik", "authelia", "gatus"},
	}

	// Verify all path methods return expected paths
	assert.Equal(t, "/project/manifest/provisions", cfg.ProvisionsDir())
	assert.Equal(t, "/project/manifest/services", cfg.ServicesDir())
	assert.Equal(t, "/project/manifest/stacks", cfg.StacksDir())
	assert.Equal(t, "/project/manifest/output", cfg.OutputDir())
}

func TestConfig_InfraContainers(t *testing.T) {
	t.Run("returns configured containers", func(t *testing.T) {
		cfg := &Config{
			infraContainers: []string{"custom1", "custom2"},
		}

		containers := cfg.InfraContainers()
		assert.Equal(t, []string{"custom1", "custom2"}, containers)
	})

	t.Run("returns empty slice when not configured", func(t *testing.T) {
		cfg := &Config{}

		containers := cfg.InfraContainers()
		assert.Empty(t, containers)
	})
}

func TestLoadInfraContainers(t *testing.T) {
	t.Run("loads from .bosun/config.yml", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create .bosun/config.yml
		bosunDir := filepath.Join(tmpDir, ".bosun")
		require.NoError(t, os.MkdirAll(bosunDir, 0755))

		content := `infrastructure:
  containers:
    - nginx
    - redis
    - postgres
`
		require.NoError(t, os.WriteFile(filepath.Join(bosunDir, "config.yml"), []byte(content), 0644))

		containers := extractInfraContainers(loadConfigFile(tmpDir))
		assert.Equal(t, []string{"nginx", "redis", "postgres"}, containers)
	})

	t.Run("loads from bosun.yml", func(t *testing.T) {
		tmpDir := t.TempDir()

		content := `infrastructure:
  containers:
    - custom1
    - custom2
`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "bosun.yml"), []byte(content), 0644))

		containers := extractInfraContainers(loadConfigFile(tmpDir))
		assert.Equal(t, []string{"custom1", "custom2"}, containers)
	})

	t.Run("prefers bosun.yml over .bosun/config.yml", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create both files — bosun.yml takes priority since loadConfigFile
		// searches bosun.yaml > bosun.yml > .bosun/config.yml consistently
		// for all config sections.
		bosunDir := filepath.Join(tmpDir, ".bosun")
		require.NoError(t, os.MkdirAll(bosunDir, 0755))

		content1 := `infrastructure:
  containers:
    - from-bosun-dir
`
		require.NoError(t, os.WriteFile(filepath.Join(bosunDir, "config.yml"), []byte(content1), 0644))

		content2 := `infrastructure:
  containers:
    - from-root
`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "bosun.yml"), []byte(content2), 0644))

		containers := extractInfraContainers(loadConfigFile(tmpDir))
		assert.Equal(t, []string{"from-root"}, containers)
	})

	t.Run("returns defaults when no config file", func(t *testing.T) {
		tmpDir := t.TempDir()

		containers := extractInfraContainers(loadConfigFile(tmpDir))
		assert.Equal(t, defaultInfraContainers, containers)
	})

	t.Run("returns defaults when config has empty containers", func(t *testing.T) {
		tmpDir := t.TempDir()

		content := `infrastructure:
  containers: []
`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "bosun.yml"), []byte(content), 0644))

		containers := extractInfraContainers(loadConfigFile(tmpDir))
		assert.Equal(t, defaultInfraContainers, containers)
	})

	t.Run("returns defaults when config is malformed", func(t *testing.T) {
		tmpDir := t.TempDir()

		content := `not: valid: yaml:
:::
`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "bosun.yml"), []byte(content), 0644))

		containers := extractInfraContainers(loadConfigFile(tmpDir))
		assert.Equal(t, defaultInfraContainers, containers)
	})

	t.Run("returns defaults when infrastructure section missing", func(t *testing.T) {
		tmpDir := t.TempDir()

		content := `something_else:
  key: value
`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "bosun.yml"), []byte(content), 0644))

		containers := extractInfraContainers(loadConfigFile(tmpDir))
		assert.Equal(t, defaultInfraContainers, containers)
	})
}

func TestLoad_WithInfraContainers(t *testing.T) {
	tmpDir := evalSymlinks(t, t.TempDir())

	// Create manifest directory
	manifestDir := filepath.Join(tmpDir, "manifest")
	require.NoError(t, os.MkdirAll(manifestDir, 0755))

	// Create bosun directory with docker-compose.yml
	bosunDir := filepath.Join(tmpDir, "bosun")
	require.NoError(t, os.MkdirAll(bosunDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(bosunDir, "docker-compose.yml"), []byte("version: '3'"), 0644))

	// Create .bosun/config.yml with custom containers
	dotBosunDir := filepath.Join(tmpDir, ".bosun")
	require.NoError(t, os.MkdirAll(dotBosunDir, 0755))
	content := `infrastructure:
  containers:
    - custom-proxy
    - custom-auth
`
	require.NoError(t, os.WriteFile(filepath.Join(dotBosunDir, "config.yml"), []byte(content), 0644))

	// Change to project root
	originalWd, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(originalWd) }()

	require.NoError(t, os.Chdir(tmpDir))

	cfg, err := Load()
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Verify infra containers are loaded
	assert.Equal(t, []string{"custom-proxy", "custom-auth"}, cfg.InfraContainers())
}

func TestFindRoot_BosunPreferredOverManifest(t *testing.T) {
	tmpDir := evalSymlinks(t, t.TempDir())

	// Create both bosun directory (with compose) and manifest directory
	bosunDir := filepath.Join(tmpDir, "bosun")
	require.NoError(t, os.MkdirAll(bosunDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(bosunDir, "docker-compose.yml"), []byte("version: '3'"), 0644))

	manifestDir := filepath.Join(tmpDir, "manifest")
	require.NoError(t, os.MkdirAll(manifestDir, 0755))

	// Change to project root
	originalWd, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(originalWd) }()

	require.NoError(t, os.Chdir(tmpDir))

	// FindRoot should find the project root (bosun checked first)
	root, err := FindRoot()
	require.NoError(t, err)
	assert.Equal(t, tmpDir, root)
}

func TestFindRoot_DeepNesting(t *testing.T) {
	tmpDir := evalSymlinks(t, t.TempDir())

	// Create manifest directory at root
	manifestDir := filepath.Join(tmpDir, "manifest")
	require.NoError(t, os.MkdirAll(manifestDir, 0755))

	// Create deeply nested subdirectory
	deepDir := filepath.Join(tmpDir, "a", "b", "c", "d", "e", "f")
	require.NoError(t, os.MkdirAll(deepDir, 0755))

	// Change to deep directory
	originalWd, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(originalWd) }()

	require.NoError(t, os.Chdir(deepDir))

	// FindRoot should still find the project root
	root, err := FindRoot()
	require.NoError(t, err)
	assert.Equal(t, tmpDir, root)
}

func TestPostSyncHooksFromConfig(t *testing.T) {
	t.Run("parses hooks from bosun.yaml", func(t *testing.T) {
		tmpDir := evalSymlinks(t, t.TempDir())

		// Create manifest directory (needed for FindRoot)
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "manifest"), 0755))

		content := `post_sync_hooks:
  - paths: ["traefik/conf.d/**"]
    action: restart
    container: traefik
  - paths: ["authelia/config.yml"]
    action: restart
    container: authelia
`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "bosun.yaml"), []byte(content), 0644))

		originalWd, err := os.Getwd()
		require.NoError(t, err)
		defer func() { _ = os.Chdir(originalWd) }()
		require.NoError(t, os.Chdir(tmpDir))

		cfg, err := Load()
		require.NoError(t, err)

		hooks := cfg.PostSyncHooks()
		require.Len(t, hooks, 2)

		assert.Equal(t, reconcile.PostSyncHook{
			Paths:     []string{"traefik/conf.d/**"},
			Action:    "restart",
			Container: "traefik",
		}, hooks[0])

		assert.Equal(t, reconcile.PostSyncHook{
			Paths:     []string{"authelia/config.yml"},
			Action:    "restart",
			Container: "authelia",
		}, hooks[1])
	})

	t.Run("returns nil when no hooks configured", func(t *testing.T) {
		tmpDir := evalSymlinks(t, t.TempDir())

		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "manifest"), 0755))

		content := `infrastructure:
  containers:
    - traefik
`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "bosun.yaml"), []byte(content), 0644))

		originalWd, err := os.Getwd()
		require.NoError(t, err)
		defer func() { _ = os.Chdir(originalWd) }()
		require.NoError(t, os.Chdir(tmpDir))

		cfg, err := Load()
		require.NoError(t, err)
		assert.Empty(t, cfg.PostSyncHooks())
	})

	t.Run("returns nil when no config file exists", func(t *testing.T) {
		tmpDir := evalSymlinks(t, t.TempDir())

		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "manifest"), 0755))

		originalWd, err := os.Getwd()
		require.NoError(t, err)
		defer func() { _ = os.Chdir(originalWd) }()
		require.NoError(t, os.Chdir(tmpDir))

		cfg, err := Load()
		require.NoError(t, err)
		assert.Empty(t, cfg.PostSyncHooks())
	})

	t.Run("parses hooks with delay field", func(t *testing.T) {
		tmpDir := evalSymlinks(t, t.TempDir())

		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "manifest"), 0755))

		content := `post_sync_hooks:
  - paths: ["traefik/conf.d/**"]
    action: restart
    container: traefik
    delay: "5s"
  - paths: ["gatus/config.yaml"]
    action: restart
    container: gatus
`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "bosun.yaml"), []byte(content), 0644))

		originalWd, err := os.Getwd()
		require.NoError(t, err)
		defer func() { _ = os.Chdir(originalWd) }()
		require.NoError(t, os.Chdir(tmpDir))

		cfg, err := Load()
		require.NoError(t, err)

		hooks := cfg.PostSyncHooks()
		require.Len(t, hooks, 2)
		assert.Equal(t, 5*time.Second, hooks[0].Delay.Duration)
		assert.Equal(t, time.Duration(0), hooks[1].Delay.Duration)
	})
}

func TestLoadFrom(t *testing.T) {
	t.Run("loads hooks from directory", func(t *testing.T) {
		tmpDir := t.TempDir()

		content := `post_sync_hooks:
  - paths: ["traefik/conf.d/**"]
    action: restart
    container: traefik
hook_settle_delay: "3s"
`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "bosun.yaml"), []byte(content), 0644))

		cfg, err := LoadFrom(tmpDir)
		require.NoError(t, err)
		require.NotNil(t, cfg)

		hooks := cfg.PostSyncHooks()
		require.Len(t, hooks, 1)
		assert.Equal(t, "traefik", hooks[0].Container)
		assert.Equal(t, 3*time.Second, cfg.HookSettleDelay())
	})

	t.Run("returns empty config when no config file", func(t *testing.T) {
		tmpDir := t.TempDir()

		cfg, err := LoadFrom(tmpDir)
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.Empty(t, cfg.PostSyncHooks())
		assert.Equal(t, time.Duration(0), cfg.HookSettleDelay())
	})

	t.Run("returns empty config on malformed YAML", func(t *testing.T) {
		tmpDir := t.TempDir()

		content := `not: valid: yaml:
:::
`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "bosun.yaml"), []byte(content), 0644))

		cfg, err := LoadFrom(tmpDir)
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.Empty(t, cfg.PostSyncHooks())
	})
}

func TestDeployPathsFromConfig(t *testing.T) {
	t.Run("parses deploy_paths from bosun.yaml", func(t *testing.T) {
		tmpDir := evalSymlinks(t, t.TempDir())

		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "manifest"), 0755))

		content := `deploy_paths:
  - "unraid/**"
  - "infrastructure/**"
`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "bosun.yaml"), []byte(content), 0644))

		originalWd, err := os.Getwd()
		require.NoError(t, err)
		defer func() { _ = os.Chdir(originalWd) }()
		require.NoError(t, os.Chdir(tmpDir))

		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, []string{"unraid/**", "infrastructure/**"}, cfg.DeployPaths())
	})

	t.Run("returns nil when not configured", func(t *testing.T) {
		tmpDir := evalSymlinks(t, t.TempDir())

		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "manifest"), 0755))

		content := `infrastructure:
  containers:
    - traefik
`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "bosun.yaml"), []byte(content), 0644))

		originalWd, err := os.Getwd()
		require.NoError(t, err)
		defer func() { _ = os.Chdir(originalWd) }()
		require.NoError(t, os.Chdir(tmpDir))

		cfg, err := Load()
		require.NoError(t, err)
		assert.Empty(t, cfg.DeployPaths())
	})

	t.Run("returns nil when no config file", func(t *testing.T) {
		tmpDir := evalSymlinks(t, t.TempDir())

		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "manifest"), 0755))

		originalWd, err := os.Getwd()
		require.NoError(t, err)
		defer func() { _ = os.Chdir(originalWd) }()
		require.NoError(t, os.Chdir(tmpDir))

		cfg, err := Load()
		require.NoError(t, err)
		assert.Empty(t, cfg.DeployPaths())
	})
}

func TestLoadFrom_DeployPaths(t *testing.T) {
	t.Run("loads deploy_paths from directory", func(t *testing.T) {
		tmpDir := t.TempDir()

		content := `deploy_paths:
  - "unraid/**"
  - "infra/**"
`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "bosun.yaml"), []byte(content), 0644))

		cfg, err := LoadFrom(tmpDir)
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.Equal(t, []string{"unraid/**", "infra/**"}, cfg.DeployPaths())
	})

	t.Run("returns empty when no deploy_paths", func(t *testing.T) {
		tmpDir := t.TempDir()

		cfg, err := LoadFrom(tmpDir)
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.Empty(t, cfg.DeployPaths())
	})
}

func TestLoadConfig_Domain(t *testing.T) {
	t.Run("loads domain from bosun.yaml", func(t *testing.T) {
		tmpDir := evalSymlinks(t, t.TempDir())

		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "manifest"), 0755))

		content := `domain: example.com
`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "bosun.yaml"), []byte(content), 0644))

		originalWd, err := os.Getwd()
		require.NoError(t, err)
		defer func() { _ = os.Chdir(originalWd) }()
		require.NoError(t, os.Chdir(tmpDir))

		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, "example.com", cfg.Domain())
	})

	t.Run("returns empty string when not configured", func(t *testing.T) {
		tmpDir := evalSymlinks(t, t.TempDir())

		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "manifest"), 0755))

		content := `infrastructure:
  containers:
    - traefik
`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "bosun.yaml"), []byte(content), 0644))

		originalWd, err := os.Getwd()
		require.NoError(t, err)
		defer func() { _ = os.Chdir(originalWd) }()
		require.NoError(t, os.Chdir(tmpDir))

		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, "", cfg.Domain())
	})

	t.Run("returns empty string when no config file", func(t *testing.T) {
		tmpDir := evalSymlinks(t, t.TempDir())

		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "manifest"), 0755))

		originalWd, err := os.Getwd()
		require.NoError(t, err)
		defer func() { _ = os.Chdir(originalWd) }()
		require.NoError(t, os.Chdir(tmpDir))

		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, "", cfg.Domain())
	})
}

func TestLoadFrom_Domain(t *testing.T) {
	t.Run("loads domain from directory", func(t *testing.T) {
		tmpDir := t.TempDir()

		content := `domain: homelab.example.com
`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "bosun.yaml"), []byte(content), 0644))

		cfg, err := LoadFrom(tmpDir)
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.Equal(t, "homelab.example.com", cfg.Domain())
	})

	t.Run("returns empty when no domain", func(t *testing.T) {
		tmpDir := t.TempDir()

		cfg, err := LoadFrom(tmpDir)
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.Equal(t, "", cfg.Domain())
	})
}

func TestHookSettleDelayFromConfig(t *testing.T) {
	t.Run("parses duration string from bosun.yaml", func(t *testing.T) {
		tmpDir := evalSymlinks(t, t.TempDir())

		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "manifest"), 0755))

		content := `hook_settle_delay: "2s"
`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "bosun.yaml"), []byte(content), 0644))

		originalWd, err := os.Getwd()
		require.NoError(t, err)
		defer func() { _ = os.Chdir(originalWd) }()
		require.NoError(t, os.Chdir(tmpDir))

		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, 2*time.Second, cfg.HookSettleDelay())
	})

	t.Run("parses bare seconds from bosun.yaml", func(t *testing.T) {
		tmpDir := evalSymlinks(t, t.TempDir())

		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "manifest"), 0755))

		content := `hook_settle_delay: "5"
`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "bosun.yaml"), []byte(content), 0644))

		originalWd, err := os.Getwd()
		require.NoError(t, err)
		defer func() { _ = os.Chdir(originalWd) }()
		require.NoError(t, os.Chdir(tmpDir))

		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, 5*time.Second, cfg.HookSettleDelay())
	})

	t.Run("defaults to zero when not configured", func(t *testing.T) {
		tmpDir := evalSymlinks(t, t.TempDir())

		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "manifest"), 0755))

		content := `infrastructure:
  containers:
    - traefik
`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "bosun.yaml"), []byte(content), 0644))

		originalWd, err := os.Getwd()
		require.NoError(t, err)
		defer func() { _ = os.Chdir(originalWd) }()
		require.NoError(t, os.Chdir(tmpDir))

		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, time.Duration(0), cfg.HookSettleDelay())
	})

	t.Run("defaults to zero when no config file", func(t *testing.T) {
		tmpDir := evalSymlinks(t, t.TempDir())

		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "manifest"), 0755))

		originalWd, err := os.Getwd()
		require.NoError(t, err)
		defer func() { _ = os.Chdir(originalWd) }()
		require.NoError(t, os.Chdir(tmpDir))

		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, time.Duration(0), cfg.HookSettleDelay())
	})
}
