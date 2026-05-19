package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cameronsjo/bosun/internal/config"
)

func TestDoctorCmd_Help(t *testing.T) {
	t.Run("doctor --help", func(t *testing.T) {
		output, err := executeCmd(t, "doctor", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "diagnostic")
		assert.Contains(t, output, "Docker")
	})
}

func TestDoctorCmd_Aliases(t *testing.T) {
	t.Run("checkup alias", func(t *testing.T) {
		_, err := executeCmd(t, "checkup", "--help")
		assert.NoError(t, err)
	})
}

func TestCheckGit(t *testing.T) {
	// Git is typically installed in test environments
	t.Run("git installed", func(t *testing.T) {
		result := checkGit()
		// Git should be installed on any dev machine running tests
		// If not, this is a warning that the test environment is unusual
		assert.True(t, result.Passed == 1 || result.Failed == 1,
			"checkGit should return exactly one passed or failed")
		assert.Equal(t, 0, result.Warned)
	})
}

func TestCheckProjectRoot(t *testing.T) {
	t.Run("with valid config", func(t *testing.T) {
		cfg := &config.Config{
			Root: "/some/path",
		}
		result := checkProjectRoot(cfg, nil)
		assert.Equal(t, 1, result.Passed)
		assert.Equal(t, 0, result.Failed)
		assert.Equal(t, 0, result.Warned)
	})

	t.Run("with nil config and no error", func(t *testing.T) {
		result := checkProjectRoot(nil, nil)
		assert.Equal(t, 0, result.Passed)
		assert.Equal(t, 0, result.Failed)
		assert.Equal(t, 1, result.Warned)
	})

	t.Run("with nil config and YAML parse error", func(t *testing.T) {
		tmpDir := t.TempDir()
		// Write a syntactically invalid bosun.yaml
		bosunYAML := filepath.Join(tmpDir, "bosun.yaml")
		require.NoError(t, os.WriteFile(bosunYAML, []byte("key: [unclosed"), 0644))

		// Also create a manifest dir so FindRoot anchors here
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "manifest"), 0755))

		originalWd, err := os.Getwd()
		require.NoError(t, err)
		defer func() { _ = os.Chdir(originalWd) }()
		require.NoError(t, os.Chdir(tmpDir))

		_, loadErr := config.Load()
		require.Error(t, loadErr)

		result := checkProjectRoot(nil, loadErr)
		// YAML parse errors must be surfaced as failures, not generic warnings.
		assert.Equal(t, 1, result.Failed, "YAML parse error should be a failure")
		assert.Equal(t, 0, result.Warned)
		assert.Equal(t, 0, result.Passed)
	})
}

func TestCheckAgeKey(t *testing.T) {
	t.Run("with SOPS_AGE_KEY_FILE set to existing file", func(t *testing.T) {
		tmpDir := t.TempDir()
		keyFile := filepath.Join(tmpDir, "keys.txt")
		require.NoError(t, os.WriteFile(keyFile, []byte("test key"), 0600))

		t.Setenv("SOPS_AGE_KEY_FILE", keyFile)
		result := checkAgeKey()
		assert.Equal(t, 1, result.Passed)
		assert.Equal(t, 0, result.Failed)
		assert.Equal(t, 0, result.Warned)
	})

	t.Run("with SOPS_AGE_KEY_FILE set to non-existent file", func(t *testing.T) {
		t.Setenv("SOPS_AGE_KEY_FILE", "/non/existent/path/keys.txt")
		result := checkAgeKey()
		assert.Equal(t, 0, result.Passed)
		assert.Equal(t, 0, result.Failed)
		assert.Equal(t, 1, result.Warned)
	})
}

func TestCheckManifestDirectory(t *testing.T) {
	t.Run("with nil config", func(t *testing.T) {
		result := checkManifestDirectory(nil)
		assert.Equal(t, 0, result.Passed)
		assert.Equal(t, 0, result.Failed)
		assert.Equal(t, 0, result.Warned)
	})

	t.Run("with manifest directory present", func(t *testing.T) {
		tmpDir := t.TempDir()
		manifestDir := filepath.Join(tmpDir, "manifest")
		require.NoError(t, os.MkdirAll(manifestDir, 0755))

		cfg := &config.Config{
			ManifestDir: manifestDir,
		}
		result := checkManifestDirectory(cfg)
		assert.Equal(t, 1, result.Passed)
		assert.Equal(t, 0, result.Failed)
		assert.Equal(t, 0, result.Warned)
	})

	t.Run("with non-existent manifest directory", func(t *testing.T) {
		cfg := &config.Config{
			ManifestDir: "/non/existent/manifest",
		}
		result := checkManifestDirectory(cfg)
		assert.Equal(t, 0, result.Passed)
		assert.Equal(t, 0, result.Failed)
		assert.Equal(t, 1, result.Warned)
	})
}

func TestCheckWebhook(t *testing.T) {
	// Note: This test checks behavior when webhook is not running
	// In a typical test environment, the webhook will not be running
	t.Run("webhook not responding", func(t *testing.T) {
		result := checkWebhook()
		// Should warn when webhook is not responding
		assert.Equal(t, 0, result.Passed)
		assert.Equal(t, 0, result.Failed)
		assert.Equal(t, 1, result.Warned)
	})
}

func TestCheckDockerCompose(t *testing.T) {
	// Docker Compose v2 is typically installed in test environments with Docker
	t.Run("docker compose check", func(t *testing.T) {
		result := checkDockerCompose()
		// Should return exactly one passed or failed (not warned)
		assert.True(t, result.Passed == 1 || result.Failed == 1,
			"checkDockerCompose should return exactly one passed or failed")
		assert.Equal(t, 0, result.Warned)
	})
}

func TestCheckSOPS(t *testing.T) {
	t.Run("sops check", func(t *testing.T) {
		result := checkSOPS()
		// Should return exactly one passed or warned
		assert.True(t, result.Passed == 1 || result.Warned == 1,
			"checkSOPS should return exactly one passed or warned")
		assert.Equal(t, 0, result.Failed)
	})
}

func TestCheckTraefikConfig(t *testing.T) {
	t.Run("no traefik service", func(t *testing.T) {
		// When no Traefik service is found, checkTraefikConfig should return empty result
		result := checkTraefikConfig(nil)
		assert.Equal(t, 0, result.Passed)
		assert.Equal(t, 0, result.Failed)
		assert.Equal(t, 0, result.Warned)
	})

	t.Run("with well-configured traefik", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create compose file with well-configured Traefik
		composePath := filepath.Join(tmpDir, "docker-compose.yml")
		content := `services:
  traefik:
    image: traefik:v3.2
    command:
      - "--entrypoints.web.http.redirections.entrypoint.to=websecure"
      - "--providers.docker.exposedbydefault=false"
    volumes:
      - ./conf.d:/etc/traefik/conf.d
`
		require.NoError(t, os.WriteFile(composePath, []byte(content), 0644))

		// Create dynamic config directory with security headers
		confDir := filepath.Join(tmpDir, "conf.d")
		require.NoError(t, os.MkdirAll(confDir, 0755))
		middleware := `http:
  middlewares:
    secure-defaults:
      headers:
        stsSeconds: 31536000
`
		require.NoError(t, os.WriteFile(filepath.Join(confDir, "middlewares.yml"), []byte(middleware), 0644))

		// Create manifest dir to make config.Load work
		manifestDir := filepath.Join(tmpDir, "manifest")
		require.NoError(t, os.MkdirAll(manifestDir, 0755))

		cfg := &config.Config{
			Root:        tmpDir,
			ManifestDir: manifestDir,
		}

		result := checkTraefikConfig(cfg)
		// HTTPS redirect: pass, exposedByDefault: pass, security headers: pass, docker socket: pass (no socket mount)
		assert.Equal(t, 4, result.Passed)
		assert.Equal(t, 0, result.Warned)
	})

	t.Run("with minimal traefik", func(t *testing.T) {
		tmpDir := t.TempDir()

		composePath := filepath.Join(tmpDir, "docker-compose.yml")
		content := `services:
  traefik:
    image: traefik:v3.2
    command:
      - "--api.dashboard=true"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
`
		require.NoError(t, os.WriteFile(composePath, []byte(content), 0644))

		manifestDir := filepath.Join(tmpDir, "manifest")
		require.NoError(t, os.MkdirAll(manifestDir, 0755))

		cfg := &config.Config{
			Root:        tmpDir,
			ManifestDir: manifestDir,
		}

		result := checkTraefikConfig(cfg)
		// All 4 checks should warn: HTTPS, exposedByDefault, headers, docker socket
		assert.Equal(t, 0, result.Passed)
		assert.Equal(t, 4, result.Warned)
	})
}

func TestCheckStateDir(t *testing.T) {
	t.Run("writable custom dir via BOSUN_STATE_DIR", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("BOSUN_STATE_DIR", tmpDir)
		result := checkStateDir()
		assert.Equal(t, 1, result.Passed)
		assert.Equal(t, 0, result.Failed)
	})

	t.Run("non-writable dir fails", func(t *testing.T) {
		// Point to a path under a non-existent root that cannot be created.
		t.Setenv("BOSUN_STATE_DIR", "/proc/bosun-cannot-create-this")
		result := checkStateDir()
		assert.Equal(t, 1, result.Failed)
		assert.Equal(t, 0, result.Passed)
	})
}

func TestCheckSocketDir(t *testing.T) {
	t.Run("writable custom socket path via BOSUN_SOCKET_PATH", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("BOSUN_SOCKET_PATH", filepath.Join(tmpDir, "bosun.sock"))
		result := checkSocketDir()
		assert.Equal(t, 1, result.Passed)
		assert.Equal(t, 0, result.Failed)
	})

	t.Run("non-writable socket dir fails", func(t *testing.T) {
		t.Setenv("BOSUN_SOCKET_PATH", "/proc/bosun-cannot-create-this/bosun.sock")
		result := checkSocketDir()
		assert.Equal(t, 1, result.Failed)
		assert.Equal(t, 0, result.Passed)
	})
}

func TestWebhookAddr(t *testing.T) {
	t.Run("defaults to localhost:8080", func(t *testing.T) {
		t.Setenv("BOSUN_TCP_ADDR", "")
		assert.Equal(t, "localhost:8080", webhookAddr())
	})

	t.Run("reads BOSUN_TCP_ADDR", func(t *testing.T) {
		t.Setenv("BOSUN_TCP_ADDR", "192.168.1.10:9090")
		assert.Equal(t, "192.168.1.10:9090", webhookAddr())
	})
}

func TestCheckWebhook_UsesEnvAddr(t *testing.T) {
	// Point at a port that is definitely not listening to confirm the address
	// is respected (the check should warn, not panic or use a wrong address).
	t.Setenv("BOSUN_TCP_ADDR", "127.0.0.1:19999")
	result := checkWebhook()
	assert.Equal(t, 0, result.Passed)
	assert.Equal(t, 1, result.Warned)
	assert.Equal(t, 0, result.Failed)
}
