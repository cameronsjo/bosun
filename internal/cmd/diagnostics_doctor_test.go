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
		result := checkProjectRoot(cfg)
		assert.Equal(t, 1, result.Passed)
		assert.Equal(t, 0, result.Failed)
		assert.Equal(t, 0, result.Warned)
	})

	t.Run("with nil config", func(t *testing.T) {
		result := checkProjectRoot(nil)
		assert.Equal(t, 0, result.Passed)
		assert.Equal(t, 0, result.Failed)
		assert.Equal(t, 1, result.Warned)
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

// TestDoctorCmd_HelpShowsChecks tests doctor help mentions diagnostic checks.
func TestDoctorCmd_HelpShowsChecks(t *testing.T) {
	t.Run("doctor help shows expected checks", func(t *testing.T) {
		output, err := executeCmd(t, "doctor", "--help")
		assert.NoError(t, err)
		assert.Contains(t, output, "Docker")
		assert.Contains(t, output, "diagnostic")
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
