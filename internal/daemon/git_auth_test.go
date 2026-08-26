package daemon

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cameronsjo/bosun/internal/config"
	"github.com/cameronsjo/bosun/internal/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateConfigGitAuthentication(t *testing.T) {
	newConfig := func(repoURL string) *Config {
		cfg := DefaultConfig()
		cfg.ReconcileConfig = reconcile.DefaultConfig()
		cfg.ReconcileConfig.RepoURL = repoURL
		return cfg
	}

	t.Run("paired HTTPS credentials are accepted", func(t *testing.T) {
		t.Setenv("BOSUN_GIT_USERNAME", "user")
		t.Setenv("BOSUN_GIT_TOKEN", "token")
		require.NoError(t, ValidateConfig(newConfig("https://example.com/repo.git")))
	})

	t.Run("partial credentials fail startup validation", func(t *testing.T) {
		t.Setenv("BOSUN_GIT_USERNAME", "configured-user")
		t.Setenv("BOSUN_GIT_TOKEN", "")
		err := ValidateConfig(newConfig("https://example.com/repo.git"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "BOSUN_GIT_TOKEN")
		assert.NotContains(t, err.Error(), "configured-user")
	})

	t.Run("userinfo fails startup validation", func(t *testing.T) {
		t.Setenv("BOSUN_GIT_USERNAME", "")
		t.Setenv("BOSUN_GIT_TOKEN", "")
		err := ValidateConfig(newConfig("https://embedded:password@example.com/repo.git"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "userinfo")
		assert.NotContains(t, err.Error(), "embedded")
		assert.NotContains(t, err.Error(), "password")
	})

	t.Run("invalid SSH key mount fails startup validation without panic", func(t *testing.T) {
		t.Setenv("BOSUN_GIT_USERNAME", "")
		t.Setenv("BOSUN_GIT_TOKEN", "")
		t.Setenv("SSH_AUTH_SOCK", "")
		keyPath := filepath.Join(t.TempDir(), "deploy-key")
		require.NoError(t, os.Mkdir(keyPath, 0700))
		t.Setenv("BOSUN_SSH_KEY", keyPath)
		t.Setenv("HOME", t.TempDir())

		err := ValidateConfig(newConfig("git@example.com:owner/repo.git"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "BOSUN_SSH_KEY")
		assert.Contains(t, err.Error(), "not a regular file")
		assert.Contains(t, err.Error(), keyPath)
	})

	t.Run("Age identity failure precedes SSH authentication when secrets are configured", func(t *testing.T) {
		t.Setenv("BOSUN_GIT_USERNAME", "")
		t.Setenv("BOSUN_GIT_TOKEN", "")
		t.Setenv("SSH_AUTH_SOCK", "")
		t.Setenv("BOSUN_SSH_KEY", "")
		agePath := filepath.Join(t.TempDir(), "age-key.txt")
		require.NoError(t, os.Mkdir(agePath, 0o700))
		t.Setenv("SOPS_AGE_KEY", "")
		t.Setenv("SOPS_AGE_KEY_FILE", agePath)
		t.Setenv("HOME", t.TempDir())

		cfg := newConfig("operator@example.com:owner/repo.git")
		cfg.ReconcileConfig.SecretsFiles = []string{"secrets/prod.sops.yaml"}
		err := ValidateConfig(cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "SOPS_AGE_KEY_FILE")
		assert.Contains(t, err.Error(), "not a regular file")
		assert.NotContains(t, err.Error(), "SSH authentication is unavailable")
	})

	t.Run("Age identity is not required without secrets files", func(t *testing.T) {
		t.Setenv("BOSUN_GIT_USERNAME", "")
		t.Setenv("BOSUN_GIT_TOKEN", "")
		t.Setenv("SOPS_AGE_KEY", "")
		t.Setenv("SOPS_AGE_KEY_FILE", t.TempDir())
		require.NoError(t, ValidateConfig(newConfig("https://example.com/repo.git")))
	})
}

func TestDaemonRunRejectsGitAuthenticationBeforeStartup(t *testing.T) {
	t.Setenv("BOSUN_GIT_USERNAME", "configured-user")
	t.Setenv("BOSUN_GIT_TOKEN", "")

	cfg := DefaultConfig()
	cfg.EnableHTTP = false
	cfg.EnableTCP = false
	cfg.SocketPath = filepath.Join(t.TempDir(), "daemon.sock")
	cfg.ReconcileConfig = reconcile.DefaultConfig()
	cfg.ReconcileConfig.RepoURL = "https://example.com/repo.git"
	d, err := New(cfg)
	require.NoError(t, err)

	err = d.Run(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "BOSUN_GIT_TOKEN")
	assert.NotContains(t, err.Error(), "configured-user")
	_, statErr := os.Stat(cfg.SocketPath)
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestDaemonRunRejectsInvalidSSHKeyBeforeStartup(t *testing.T) {
	t.Setenv("BOSUN_GIT_USERNAME", "")
	t.Setenv("BOSUN_GIT_TOKEN", "")
	t.Setenv("SSH_AUTH_SOCK", "")
	keyPath := filepath.Join(t.TempDir(), "deploy-key")
	require.NoError(t, os.Mkdir(keyPath, 0700))
	t.Setenv("BOSUN_SSH_KEY", keyPath)
	t.Setenv("HOME", t.TempDir())

	cfg := DefaultConfig()
	cfg.EnableHTTP = false
	cfg.EnableTCP = false
	cfg.SocketPath = filepath.Join(t.TempDir(), "daemon.sock")
	cfg.ReconcileConfig = reconcile.DefaultConfig()
	cfg.ReconcileConfig.RepoURL = "git@example.com:owner/repo.git"
	d, err := New(cfg)
	require.NoError(t, err)

	err = d.Run(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "BOSUN_SSH_KEY")
	assert.Contains(t, err.Error(), "not a regular file")
	assert.Contains(t, err.Error(), keyPath)
	_, statErr := os.Stat(cfg.SocketPath)
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestDaemonRunRejectsInvalidAgeIdentityBeforeListeners(t *testing.T) {
	t.Setenv("BOSUN_GIT_USERNAME", "")
	t.Setenv("BOSUN_GIT_TOKEN", "")
	t.Setenv("SOPS_AGE_KEY", "")
	agePath := filepath.Join(t.TempDir(), "age-key.txt")
	require.NoError(t, os.Mkdir(agePath, 0o700))
	t.Setenv("SOPS_AGE_KEY_FILE", agePath)

	cfg := DefaultConfig()
	cfg.EnableHTTP = false
	cfg.EnableTCP = false
	cfg.SocketPath = filepath.Join(t.TempDir(), "daemon.sock")
	cfg.ReconcileConfig = reconcile.DefaultConfig()
	cfg.ReconcileConfig.RepoURL = "https://example.com/repo.git"
	cfg.ReconcileConfig.SecretsFiles = []string{"secrets/prod.sops.yaml"}
	d, err := New(cfg)
	require.NoError(t, err)

	err = d.Run(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SOPS_AGE_KEY_FILE")
	assert.Contains(t, err.Error(), "not a regular file")
	_, statErr := os.Stat(cfg.SocketPath)
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestConfigFromEnvGitAuthenticationPrecedenceAndAliases(t *testing.T) {
	t.Setenv("BOSUN_REPO_URL", "https://primary.example/repo.git")
	t.Setenv("REPO_URL", "http://shadowed.example/repo.git")
	t.Setenv("BOSUN_GIT_USERNAME", "bosun-user")
	t.Setenv("BOSUN_GIT_TOKEN", "bosun-token")
	t.Setenv("GIT_USERNAME", "legacy-user")
	t.Setenv("GIT_TOKEN", "legacy-token")

	cfg := ConfigFromEnv()
	require.NotNil(t, cfg.ReconcileConfig)
	assert.Equal(t, "https://primary.example/repo.git", cfg.ReconcileConfig.RepoURL)
	require.NoError(t, ValidateConfig(cfg))

	t.Setenv("BOSUN_GIT_TOKEN", "")
	err := ValidateConfig(ConfigFromEnv())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "BOSUN_GIT_TOKEN")
	assert.NotContains(t, err.Error(), "legacy-token")

	assert.Equal(t, "https://primary.example/repo.git", config.BosunEnv("REPO_URL"))
}

func TestDaemonGitCredentialResponseRedaction(t *testing.T) {
	const username = "response-user+secret@example.com"
	const token = "response-token:/?secret"
	t.Setenv("BOSUN_GIT_USERNAME", username)
	t.Setenv("BOSUN_GIT_TOKEN", token)

	ss, d := newTestSocketServer(t)
	d.config.ReconcileConfig.RepoURL = "https://embedded%40user:embedded%3Atoken@example.com/repo.git"
	basic := base64.StdEncoding.EncodeToString([]byte(username + ":" + token))
	d.lastError = fmt.Errorf("git failed user=%s token=%s query=%s Basic %s url=%s",
		username, token, url.QueryEscape(token), basic, d.config.ReconcileConfig.RepoURL)

	tests := []struct {
		name    string
		target  string
		handler http.HandlerFunc
	}{
		{name: "config", target: "/config", handler: ss.handleConfig},
		{name: "status", target: "/status", handler: ss.handleStatus},
		{name: "health", target: "/health", handler: ss.handleHealth},
		{name: "api status", target: "/api/status", handler: d.handleAPIStatus},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.target, nil)
			w := httptest.NewRecorder()
			tt.handler(w, req)
			body := w.Body.String()
			for _, forbidden := range []string{
				username,
				token,
				url.QueryEscape(token),
				basic,
				"embedded%40user",
				"embedded%3Atoken",
			} {
				assert.NotContains(t, body, forbidden)
			}
			if tt.name == "config" {
				assert.Contains(t, body, "https://example.com/repo.git")
			}
		})
	}
}

func TestGitCredentialsAreNotSerialized(t *testing.T) {
	const username = "serialization-user"
	const token = "serialization-token"
	t.Setenv("BOSUN_GIT_USERNAME", username)
	t.Setenv("BOSUN_GIT_TOKEN", token)

	cfgType := reflect.TypeOf(*reconcile.DefaultConfig())
	reloadType := reflect.TypeOf(reconcile.ReloadedConfig{})
	for _, candidate := range []reflect.Type{cfgType, reloadType} {
		for i := 0; i < candidate.NumField(); i++ {
			name := strings.ToLower(candidate.Field(i).Name)
			assert.NotContains(t, name, "gitusername")
			assert.NotContains(t, name, "gittoken")
		}
	}

	stateJSON, err := json.Marshal(&reconcile.DeployState{})
	require.NoError(t, err)
	responseJSON, err := json.Marshal(buildConfigResponse(&Config{ReconcileConfig: reconcile.DefaultConfig()}))
	require.NoError(t, err)
	serialized := strings.Join([]string{string(stateJSON), string(responseJSON)}, "\n")
	assert.NotContains(t, serialized, username)
	assert.NotContains(t, serialized, token)
}
