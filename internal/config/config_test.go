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

	t.Run("parses exec hooks with command field", func(t *testing.T) {
		tmpDir := evalSymlinks(t, t.TempDir())

		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "manifest"), 0755))

		content := `post_sync_hooks:
  - paths: ["traefik/**"]
    action: exec
    container: traefik
    command: ["traefik", "reload"]
  - paths: ["nginx/**"]
    action: restart
    container: nginx
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
			Paths:     []string{"traefik/**"},
			Action:    "exec",
			Container: "traefik",
			Command:   []string{"traefik", "reload"},
		}, hooks[0])

		assert.Equal(t, reconcile.PostSyncHook{
			Paths:     []string{"nginx/**"},
			Action:    "restart",
			Container: "nginx",
		}, hooks[1])
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

func TestDeploySyncPathsFromConfig(t *testing.T) {
	t.Run("parses deploy_sync_paths from bosun.yaml", func(t *testing.T) {
		tmpDir := evalSymlinks(t, t.TempDir())
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "manifest"), 0755))

		content := `deploy_sync_paths:
  - "appdata/traefik"
  - "appdata/authelia"
  - "compose"
`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "bosun.yaml"), []byte(content), 0644))
		cfg := loadConfigFile(tmpDir)
		assert.Equal(t, []string{"appdata/traefik", "appdata/authelia", "compose"}, extractDeploySyncPaths(cfg))
	})

	t.Run("returns nil when not configured", func(t *testing.T) {
		tmpDir := evalSymlinks(t, t.TempDir())
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "manifest"), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "bosun.yaml"), []byte(""), 0644))

		cfg := loadConfigFile(tmpDir)
		assert.Empty(t, extractDeploySyncPaths(cfg))
	})
}

func TestDeploySyncExcludeFromConfig(t *testing.T) {
	t.Run("parses deploy_sync_exclude from bosun.yaml", func(t *testing.T) {
		tmpDir := evalSymlinks(t, t.TempDir())
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "manifest"), 0755))

		content := `deploy_sync_exclude:
  - "appdata/legacy-service"
`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "bosun.yaml"), []byte(content), 0644))
		cfg := loadConfigFile(tmpDir)
		assert.Equal(t, []string{"appdata/legacy-service"}, extractDeploySyncExclude(cfg))
	})

	t.Run("returns nil when not configured", func(t *testing.T) {
		tmpDir := evalSymlinks(t, t.TempDir())
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "manifest"), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "bosun.yaml"), []byte(""), 0644))

		cfg := loadConfigFile(tmpDir)
		assert.Empty(t, extractDeploySyncExclude(cfg))
	})
}

func TestLoadFrom_DeploySyncPaths(t *testing.T) {
	t.Run("loads deploy_sync_paths from directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		content := `deploy_sync_paths:
  - "appdata/**"
deploy_sync_exclude:
  - "appdata/deprecated"
`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "bosun.yaml"), []byte(content), 0644))

		cfg, err := LoadFrom(tmpDir)
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.Equal(t, []string{"appdata/**"}, cfg.DeploySyncPaths())
		assert.Equal(t, []string{"appdata/deprecated"}, cfg.DeploySyncExclude())
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

// TestLoad_FullYAMLIntegration creates a bosun.yaml with ALL sections populated
// and verifies every getter returns the expected value. This single test covers
// most zero-coverage getters: ProjectName, TunnelProvider, TunnelHostname,
// TunnelName, TunnelHealthEndpoint, GetTunnelConfig, GetAlertConfig.
func TestLoad_FullYAMLIntegration(t *testing.T) {
	tmpDir := evalSymlinks(t, t.TempDir())

	// Create manifest directory
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "manifest", "provisions"), 0755))

	content := `project_name: my-homelab
domain: home.example.com

infrastructure:
  containers:
    - traefik
    - authelia

tunnel:
  provider: cloudflare
  hostname: tunnel.example.com
  tunnel_name: my-tunnel
  health_endpoint: http://localhost:8080/health

alerts:
  discord_webhook_url: https://discord.example.com/hook
  sendgrid_api_key: sg-key
  sendgrid_from_email: noreply@example.com
  sendgrid_from_name: Bosun Alerts
  sendgrid_to_emails:
    - admin@example.com
  twilio_account_sid: AC123
  twilio_auth_token: token
  twilio_from_number: "+15551234567"
  twilio_to_numbers:
    - "+15559876543"
  on_success: true
  on_failure: true

post_sync_hooks:
  - paths: ["traefik/**"]
    action: restart
    container: traefik

hook_settle_delay: "3s"

deploy_paths:
  - "infra/**"
  - "services/**"
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "bosun.yaml"), []byte(content), 0644))

	originalWd, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(originalWd) }()
	require.NoError(t, os.Chdir(tmpDir))

	cfg, err := Load()
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Project identity
	assert.Equal(t, "my-homelab", cfg.ProjectName())
	assert.Equal(t, "home.example.com", cfg.Domain())

	// Infrastructure
	assert.Equal(t, []string{"traefik", "authelia"}, cfg.InfraContainers())

	// Tunnel getters
	assert.Equal(t, "cloudflare", cfg.TunnelProvider())
	assert.Equal(t, "tunnel.example.com", cfg.TunnelHostname())
	assert.Equal(t, "my-tunnel", cfg.TunnelName())
	assert.Equal(t, "http://localhost:8080/health", cfg.TunnelHealthEndpoint())
	tunnelCfg := cfg.GetTunnelConfig()
	assert.Equal(t, TunnelConfig{
		Hostname:       "tunnel.example.com",
		TunnelName:     "my-tunnel",
		HealthEndpoint: "http://localhost:8080/health",
	}, tunnelCfg)

	// Alert config
	alertCfg := cfg.GetAlertConfig()
	assert.Equal(t, "https://discord.example.com/hook", alertCfg.DiscordWebhookURL)
	assert.Equal(t, "sg-key", alertCfg.SendGridAPIKey)
	assert.Equal(t, "noreply@example.com", alertCfg.SendGridFromEmail)
	assert.Equal(t, "Bosun Alerts", alertCfg.SendGridFromName)
	assert.Equal(t, []string{"admin@example.com"}, alertCfg.SendGridToEmails)
	assert.Equal(t, "AC123", alertCfg.TwilioAccountSID)
	assert.Equal(t, "token", alertCfg.TwilioAuthToken)
	assert.Equal(t, "+15551234567", alertCfg.TwilioFromNumber)
	assert.Equal(t, []string{"+15559876543"}, alertCfg.TwilioToNumbers)
	assert.True(t, alertCfg.OnSuccess)
	assert.True(t, alertCfg.OnFailure)

	// Hooks and deploy paths
	require.Len(t, cfg.PostSyncHooks(), 1)
	assert.Equal(t, "traefik", cfg.PostSyncHooks()[0].Container)
	assert.Equal(t, 3*time.Second, cfg.HookSettleDelay())
	assert.Equal(t, []string{"infra/**", "services/**"}, cfg.DeployPaths())

	// RemoveOrphans defaults to true when not set in config
	assert.True(t, cfg.RemoveOrphans())
}

func TestConfig_Format(t *testing.T) {
	t.Run("helm format with Chart.yaml in subdirectory", func(t *testing.T) {
		tmpDir := evalSymlinks(t, t.TempDir())

		// Create charts/myapp/Chart.yaml (Helm-aligned format)
		chartDir := filepath.Join(tmpDir, "charts", "myapp")
		require.NoError(t, os.MkdirAll(chartDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(chartDir, "Chart.yaml"), []byte("name: myapp"), 0644))

		cfg := &Config{Root: tmpDir, ManifestDir: filepath.Join(tmpDir, "manifest")}
		assert.Equal(t, "helm", cfg.Format())
	})

	t.Run("legacy format with provisions directory", func(t *testing.T) {
		tmpDir := evalSymlinks(t, t.TempDir())

		// Create manifest/provisions (legacy format)
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "manifest", "provisions"), 0755))

		cfg := &Config{Root: tmpDir, ManifestDir: filepath.Join(tmpDir, "manifest")}
		assert.Equal(t, "legacy", cfg.Format())
	})

	t.Run("unknown format when no charts or provisions", func(t *testing.T) {
		tmpDir := evalSymlinks(t, t.TempDir())

		cfg := &Config{Root: tmpDir, ManifestDir: filepath.Join(tmpDir, "manifest")}
		assert.Equal(t, "unknown", cfg.Format())
	})

	t.Run("charts/templates only does not match helm", func(t *testing.T) {
		tmpDir := evalSymlinks(t, t.TempDir())

		// Create charts/templates (should be skipped — not a service chart)
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "charts", "templates"), 0755))

		cfg := &Config{Root: tmpDir, ManifestDir: filepath.Join(tmpDir, "manifest")}
		assert.Equal(t, "unknown", cfg.Format())
	})

	t.Run("chart subdirectory without Chart.yaml does not match helm", func(t *testing.T) {
		tmpDir := evalSymlinks(t, t.TempDir())

		// Create charts/myapp but no Chart.yaml inside it
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "charts", "myapp"), 0755))

		cfg := &Config{Root: tmpDir, ManifestDir: filepath.Join(tmpDir, "manifest")}
		assert.Equal(t, "unknown", cfg.Format())
	})
}

func TestConfig_HelmDirGetters(t *testing.T) {
	cfg := &Config{Root: "/project"}

	assert.Equal(t, "/project/charts", cfg.ChartsDir())
	assert.Equal(t, "/project/charts/templates", cfg.TemplatesDir())
	assert.Equal(t, "/project/stacks", cfg.HelmStacksDir())
}

func TestConfig_ProvisionsDir_Explicit(t *testing.T) {
	cfg := &Config{
		ManifestDir:   "/project/manifest",
		provisionsDir: "/project/custom-provisions",
	}
	assert.Equal(t, "/project/custom-provisions", cfg.ProvisionsDir())
}

func TestConfig_TunnelDefaultProvider(t *testing.T) {
	// When no tunnel provider is configured, Load defaults to "tailscale"
	tmpDir := evalSymlinks(t, t.TempDir())
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "manifest"), 0755))

	// Empty config
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "bosun.yaml"), []byte("domain: test.com\n"), 0644))

	originalWd, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(originalWd) }()
	require.NoError(t, os.Chdir(tmpDir))

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "tailscale", cfg.TunnelProvider())
}

func TestLoad_ManifestsPluralFallback(t *testing.T) {
	tmpDir := evalSymlinks(t, t.TempDir())

	// Create manifests/ (plural) instead of manifest/ (singular)
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "manifests"), 0755))

	originalWd, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(originalWd) }()
	require.NoError(t, os.Chdir(tmpDir))

	cfg, err := Load()
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// ManifestDir should use manifests/ (plural)
	assert.Equal(t, filepath.Join(tmpDir, "manifests"), cfg.ManifestDir)
}

func TestLoad_CustomManifestDir(t *testing.T) {
	tmpDir := evalSymlinks(t, t.TempDir())

	// Create both a default manifest/ and a custom one
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "manifest"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "my-manifests"), 0755))

	content := `manifest_dir: my-manifests
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "bosun.yaml"), []byte(content), 0644))

	originalWd, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(originalWd) }()
	require.NoError(t, os.Chdir(tmpDir))

	cfg, err := Load()
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Equal(t, filepath.Join(tmpDir, "my-manifests"), cfg.ManifestDir)
}

func TestLoad_CustomProvisionsDir(t *testing.T) {
	tmpDir := evalSymlinks(t, t.TempDir())

	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "manifest"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "custom-provisions"), 0755))

	content := `provisions_dir: custom-provisions
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "bosun.yaml"), []byte(content), 0644))

	originalWd, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(originalWd) }()
	require.NoError(t, os.Chdir(tmpDir))

	cfg, err := Load()
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Equal(t, filepath.Join(tmpDir, "custom-provisions"), cfg.ProvisionsDir())
}

func TestLoad_DefaultProjectName(t *testing.T) {
	tmpDir := evalSymlinks(t, t.TempDir())

	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "manifest"), 0755))
	// No config file — project name defaults to directory basename

	originalWd, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(originalWd) }()
	require.NoError(t, os.Chdir(tmpDir))

	cfg, err := Load()
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Equal(t, filepath.Base(tmpDir), cfg.ProjectName())
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

func TestDriftAlertDebounceFromConfig(t *testing.T) {
	t.Run("parses duration string from bosun.yaml", func(t *testing.T) {
		tmpDir := evalSymlinks(t, t.TempDir())

		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "manifest"), 0755))

		content := `drift_alert_debounce: "5m"
`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "bosun.yaml"), []byte(content), 0644))

		originalWd, err := os.Getwd()
		require.NoError(t, err)
		defer func() { _ = os.Chdir(originalWd) }()
		require.NoError(t, os.Chdir(tmpDir))

		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, 5*time.Minute, cfg.DriftAlertDebounce())
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
		assert.Equal(t, time.Duration(0), cfg.DriftAlertDebounce())
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
		assert.Equal(t, time.Duration(0), cfg.DriftAlertDebounce())
	})
}

func TestLoadFrom_DriftAlertDebounce(t *testing.T) {
	t.Run("loads drift_alert_debounce from directory", func(t *testing.T) {
		tmpDir := t.TempDir()

		content := `drift_alert_debounce: "10m"
`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "bosun.yaml"), []byte(content), 0644))

		cfg, err := LoadFrom(tmpDir)
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.Equal(t, 10*time.Minute, cfg.DriftAlertDebounce())
	})

	t.Run("returns zero when no drift_alert_debounce", func(t *testing.T) {
		tmpDir := t.TempDir()

		cfg, err := LoadFrom(tmpDir)
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.Equal(t, time.Duration(0), cfg.DriftAlertDebounce())
	})
}

func TestRemoveOrphansFromConfig(t *testing.T) {
	t.Run("defaults to true when not configured", func(t *testing.T) {
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
		assert.True(t, cfg.RemoveOrphans())
	})

	t.Run("defaults to true when no config file", func(t *testing.T) {
		tmpDir := evalSymlinks(t, t.TempDir())

		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "manifest"), 0755))

		originalWd, err := os.Getwd()
		require.NoError(t, err)
		defer func() { _ = os.Chdir(originalWd) }()
		require.NoError(t, os.Chdir(tmpDir))

		cfg, err := Load()
		require.NoError(t, err)
		assert.True(t, cfg.RemoveOrphans())
	})

	t.Run("parses false from bosun.yaml", func(t *testing.T) {
		tmpDir := evalSymlinks(t, t.TempDir())

		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "manifest"), 0755))

		content := `remove_orphans: false
`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "bosun.yaml"), []byte(content), 0644))

		originalWd, err := os.Getwd()
		require.NoError(t, err)
		defer func() { _ = os.Chdir(originalWd) }()
		require.NoError(t, os.Chdir(tmpDir))

		cfg, err := Load()
		require.NoError(t, err)
		assert.False(t, cfg.RemoveOrphans())
	})

	t.Run("parses true from bosun.yaml", func(t *testing.T) {
		tmpDir := evalSymlinks(t, t.TempDir())

		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "manifest"), 0755))

		content := `remove_orphans: true
`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "bosun.yaml"), []byte(content), 0644))

		originalWd, err := os.Getwd()
		require.NoError(t, err)
		defer func() { _ = os.Chdir(originalWd) }()
		require.NoError(t, os.Chdir(tmpDir))

		cfg, err := Load()
		require.NoError(t, err)
		assert.True(t, cfg.RemoveOrphans())
	})
}

func TestLoadFrom_RemoveOrphans(t *testing.T) {
	t.Run("loads false from directory", func(t *testing.T) {
		tmpDir := t.TempDir()

		content := `remove_orphans: false
`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "bosun.yaml"), []byte(content), 0644))

		cfg, err := LoadFrom(tmpDir)
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.False(t, cfg.RemoveOrphans())
	})

	t.Run("defaults to true when not set", func(t *testing.T) {
		tmpDir := t.TempDir()

		cfg, err := LoadFrom(tmpDir)
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.True(t, cfg.RemoveOrphans())
	})
}

func TestExtractRemoveOrphans(t *testing.T) {
	t.Run("returns true when nil", func(t *testing.T) {
		cfg := configFile{}
		assert.True(t, extractRemoveOrphans(cfg))
	})

	t.Run("returns false when explicitly false", func(t *testing.T) {
		f := false
		cfg := configFile{RemoveOrphans: &f}
		assert.False(t, extractRemoveOrphans(cfg))
	})

	t.Run("returns true when explicitly true", func(t *testing.T) {
		tr := true
		cfg := configFile{RemoveOrphans: &tr}
		assert.True(t, extractRemoveOrphans(cfg))
	})
}

func TestCriticalContainersFromConfig(t *testing.T) {
	t.Run("parses critical_containers from bosun.yaml", func(t *testing.T) {
		tmpDir := evalSymlinks(t, t.TempDir())

		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "manifest"), 0755))

		content := `critical_containers:
  - "traefik"
  - "authelia"
`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "bosun.yaml"), []byte(content), 0644))

		originalWd, err := os.Getwd()
		require.NoError(t, err)
		defer func() { _ = os.Chdir(originalWd) }()
		require.NoError(t, os.Chdir(tmpDir))

		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, []string{"traefik", "authelia"}, cfg.CriticalContainers())
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
		assert.Empty(t, cfg.CriticalContainers())
	})
}

func TestLoadFrom_CriticalContainers(t *testing.T) {
	t.Run("loads critical_containers from directory", func(t *testing.T) {
		tmpDir := t.TempDir()

		content := `critical_containers:
  - "traefik"
  - "authelia"
`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "bosun.yaml"), []byte(content), 0644))

		cfg, err := LoadFrom(tmpDir)
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.Equal(t, []string{"traefik", "authelia"}, cfg.CriticalContainers())
	})

	t.Run("returns empty when not configured", func(t *testing.T) {
		tmpDir := t.TempDir()

		cfg, err := LoadFrom(tmpDir)
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.Empty(t, cfg.CriticalContainers())
	})
}

func TestExtractCriticalContainers(t *testing.T) {
	t.Run("returns containers from config", func(t *testing.T) {
		cfg := configFile{CriticalContainers: []string{"traefik", "authelia"}}
		assert.Equal(t, []string{"traefik", "authelia"}, extractCriticalContainers(cfg))
	})

	t.Run("returns nil when empty", func(t *testing.T) {
		cfg := configFile{}
		assert.Nil(t, extractCriticalContainers(cfg))
	})
}

func TestShutdownTimeoutFromConfig(t *testing.T) {
	t.Run("defaults to 30s when not configured", func(t *testing.T) {
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
		assert.Equal(t, 30*time.Second, cfg.ShutdownTimeout())
	})

	t.Run("parses duration from bosun.yaml", func(t *testing.T) {
		tmpDir := evalSymlinks(t, t.TempDir())

		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "manifest"), 0755))

		content := `shutdown_timeout: 120s
`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "bosun.yaml"), []byte(content), 0644))

		originalWd, err := os.Getwd()
		require.NoError(t, err)
		defer func() { _ = os.Chdir(originalWd) }()
		require.NoError(t, os.Chdir(tmpDir))

		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, 120*time.Second, cfg.ShutdownTimeout())
	})

	t.Run("respects BOSUN_STOP_TIMEOUT env var", func(t *testing.T) {
		tmpDir := evalSymlinks(t, t.TempDir())

		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "manifest"), 0755))

		// No shutdown_timeout in YAML
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "bosun.yaml"), []byte(""), 0644))

		t.Setenv("BOSUN_STOP_TIMEOUT", "90s")

		originalWd, err := os.Getwd()
		require.NoError(t, err)
		defer func() { _ = os.Chdir(originalWd) }()
		require.NoError(t, os.Chdir(tmpDir))

		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, 90*time.Second, cfg.ShutdownTimeout())
	})

	t.Run("YAML takes precedence over env var", func(t *testing.T) {
		tmpDir := evalSymlinks(t, t.TempDir())

		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "manifest"), 0755))

		content := `shutdown_timeout: 60s
`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "bosun.yaml"), []byte(content), 0644))

		t.Setenv("BOSUN_STOP_TIMEOUT", "90s")

		originalWd, err := os.Getwd()
		require.NoError(t, err)
		defer func() { _ = os.Chdir(originalWd) }()
		require.NoError(t, os.Chdir(tmpDir))

		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, 60*time.Second, cfg.ShutdownTimeout())
	})
}

func TestLoadFrom_ShutdownTimeout(t *testing.T) {
	t.Run("loads shutdown_timeout from directory", func(t *testing.T) {
		tmpDir := t.TempDir()

		content := `shutdown_timeout: 45s
`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "bosun.yaml"), []byte(content), 0644))

		cfg, err := LoadFrom(tmpDir)
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.Equal(t, 45*time.Second, cfg.ShutdownTimeout())
	})

	t.Run("defaults to 30s when not set", func(t *testing.T) {
		tmpDir := t.TempDir()

		cfg, err := LoadFrom(tmpDir)
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.Equal(t, 30*time.Second, cfg.ShutdownTimeout())
	})
}

func TestExtractShutdownTimeout(t *testing.T) {
	t.Run("returns default when zero", func(t *testing.T) {
		cfg := configFile{}
		assert.Equal(t, 30*time.Second, extractShutdownTimeout(cfg))
	})

	t.Run("returns configured duration", func(t *testing.T) {
		cfg := configFile{ShutdownTimeout: reconcile.Duration{Duration: 120 * time.Second}}
		assert.Equal(t, 120*time.Second, extractShutdownTimeout(cfg))
	})

	t.Run("respects env var when config is zero", func(t *testing.T) {
		t.Setenv("BOSUN_STOP_TIMEOUT", "45s")
		cfg := configFile{}
		assert.Equal(t, 45*time.Second, extractShutdownTimeout(cfg))
	})

	t.Run("ignores invalid env var", func(t *testing.T) {
		t.Setenv("BOSUN_STOP_TIMEOUT", "not-a-duration")
		cfg := configFile{}
		assert.Equal(t, 30*time.Second, extractShutdownTimeout(cfg))
	})
}
