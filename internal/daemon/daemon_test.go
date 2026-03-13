package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cameronsjo/bosun/internal/alert"
	"github.com/cameronsjo/bosun/internal/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.SocketPath != "/var/run/bosun.sock" {
		t.Errorf("SocketPath = %q, want /var/run/bosun.sock", cfg.SocketPath)
	}
	if cfg.EnableTCP {
		t.Error("EnableTCP should be false by default")
	}
	if cfg.TCPAddr != "127.0.0.1:9090" {
		t.Errorf("TCPAddr = %q, want 127.0.0.1:9090", cfg.TCPAddr)
	}
	if cfg.Port != 8080 {
		t.Errorf("Port = %d, want 8080", cfg.Port)
	}
	if !cfg.EnableHTTP {
		t.Error("EnableHTTP should be true by default")
	}
	if cfg.PollInterval != time.Hour {
		t.Errorf("PollInterval = %v, want 1h", cfg.PollInterval)
	}
}

func TestDefaultConfig_DriftAlertCooldown(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.DriftAlertCooldown != time.Hour {
		t.Errorf("DriftAlertCooldown = %v, want 1h", cfg.DriftAlertCooldown)
	}
	if !cfg.DriftResolveAlerts {
		t.Error("DriftResolveAlerts should be true by default")
	}
	if !cfg.ContentHashSync {
		t.Error("ContentHashSync should be true by default")
	}
	if !cfg.RemoveOrphans {
		t.Error("RemoveOrphans should be true by default")
	}
}

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		wantErr bool
	}{
		{
			name: "valid config",
			cfg: &Config{
				Port: 8080,
				ReconcileConfig: &reconcile.Config{
					RepoURL: "https://github.com/example/repo",
				},
			},
			wantErr: false,
		},
		{
			name: "invalid port zero",
			cfg: &Config{
				Port: 0,
				ReconcileConfig: &reconcile.Config{
					RepoURL: "https://github.com/example/repo",
				},
			},
			wantErr: true,
		},
		{
			name: "invalid port too high",
			cfg: &Config{
				Port: 70000,
				ReconcileConfig: &reconcile.Config{
					RepoURL: "https://github.com/example/repo",
				},
			},
			wantErr: true,
		},
		{
			name: "missing repo URL",
			cfg: &Config{
				Port:            8080,
				ReconcileConfig: &reconcile.Config{},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateConfig(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestHealthStatus_JSON(t *testing.T) {
	status := HealthStatus{
		Status:        "healthy",
		Ready:         true,
		LastReconcile: time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
		Uptime:        5 * time.Minute,
	}

	data, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("Failed to marshal HealthStatus: %v", err)
	}

	var decoded HealthStatus
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal HealthStatus: %v", err)
	}

	if decoded.Status != status.Status {
		t.Errorf("Status = %q, want %q", decoded.Status, status.Status)
	}
	if decoded.Ready != status.Ready {
		t.Errorf("Ready = %v, want %v", decoded.Ready, status.Ready)
	}
}

func TestTriggerRequest_JSON(t *testing.T) {
	req := TriggerRequest{Source: "webhook"}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Failed to marshal TriggerRequest: %v", err)
	}

	var decoded TriggerRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal TriggerRequest: %v", err)
	}

	if decoded.Source != req.Source {
		t.Errorf("Source = %q, want %q", decoded.Source, req.Source)
	}
}

func TestTriggerResponse_JSON(t *testing.T) {
	resp := TriggerResponse{
		Status:  "accepted",
		Message: "Reconciliation triggered",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Failed to marshal TriggerResponse: %v", err)
	}

	var decoded TriggerResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal TriggerResponse: %v", err)
	}

	if decoded.Status != resp.Status {
		t.Errorf("Status = %q, want %q", decoded.Status, resp.Status)
	}
	if decoded.Message != resp.Message {
		t.Errorf("Message = %q, want %q", decoded.Message, resp.Message)
	}
}

func TestStatusResponse_JSON(t *testing.T) {
	now := time.Now()
	resp := StatusResponse{
		State:         "idle",
		LastReconcile: &now,
		LastError:     "some error",
		Uptime:        "1h30m",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Failed to marshal StatusResponse: %v", err)
	}

	var decoded StatusResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal StatusResponse: %v", err)
	}

	if decoded.State != resp.State {
		t.Errorf("State = %q, want %q", decoded.State, resp.State)
	}
	if decoded.LastError != resp.LastError {
		t.Errorf("LastError = %q, want %q", decoded.LastError, resp.LastError)
	}
}

func TestConfigResponse_JSON(t *testing.T) {
	resp := ConfigResponse{
		WebhookSecret: "secret123",
		PollInterval:  3600,
		RepoURL:       "https://github.com/example/repo",
		RepoBranch:    "main",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Failed to marshal ConfigResponse: %v", err)
	}

	var decoded ConfigResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal ConfigResponse: %v", err)
	}

	if decoded.WebhookSecret != resp.WebhookSecret {
		t.Errorf("WebhookSecret = %q, want %q", decoded.WebhookSecret, resp.WebhookSecret)
	}
	if decoded.RepoURL != resp.RepoURL {
		t.Errorf("RepoURL = %q, want %q", decoded.RepoURL, resp.RepoURL)
	}
}

func TestConfigFromEnv(t *testing.T) {
	t.Run("default values when no env vars", func(t *testing.T) {
		cfg := ConfigFromEnv()

		if cfg.SocketPath != "/var/run/bosun.sock" {
			t.Errorf("SocketPath = %q, want /var/run/bosun.sock", cfg.SocketPath)
		}
		if cfg.Port != 8080 {
			t.Errorf("Port = %d, want 8080", cfg.Port)
		}
		if !cfg.EnableHTTP {
			t.Error("EnableHTTP should be true by default")
		}
		if cfg.EnableTCP {
			t.Error("EnableTCP should be false by default")
		}
	})

	t.Run("BOSUN_SOCKET_PATH overrides default", func(t *testing.T) {
		t.Setenv("BOSUN_SOCKET_PATH", "/tmp/custom.sock")

		cfg := ConfigFromEnv()

		if cfg.SocketPath != "/tmp/custom.sock" {
			t.Errorf("SocketPath = %q, want /tmp/custom.sock", cfg.SocketPath)
		}
	})

	t.Run("PORT sets http port", func(t *testing.T) {
		t.Setenv("PORT", "9000")

		cfg := ConfigFromEnv()

		if cfg.Port != 9000 {
			t.Errorf("Port = %d, want 9000", cfg.Port)
		}
	})

	t.Run("WEBHOOK_PORT overrides PORT", func(t *testing.T) {
		t.Setenv("PORT", "9000")
		t.Setenv("WEBHOOK_PORT", "9999")

		cfg := ConfigFromEnv()

		if cfg.Port != 9999 {
			t.Errorf("Port = %d, want 9999", cfg.Port)
		}
	})

	t.Run("BOSUN_DISABLE_HTTP disables HTTP server", func(t *testing.T) {
		t.Setenv("BOSUN_DISABLE_HTTP", "true")

		cfg := ConfigFromEnv()

		if cfg.EnableHTTP {
			t.Error("EnableHTTP should be false when BOSUN_DISABLE_HTTP=true")
		}
	})

	t.Run("BOSUN_ENABLE_TCP enables TCP server", func(t *testing.T) {
		t.Setenv("BOSUN_ENABLE_TCP", "true")

		cfg := ConfigFromEnv()

		if !cfg.EnableTCP {
			t.Error("EnableTCP should be true when BOSUN_ENABLE_TCP=true")
		}
	})

	t.Run("BOSUN_TCP_ADDR sets TCP address", func(t *testing.T) {
		t.Setenv("BOSUN_TCP_ADDR", "0.0.0.0:9999")

		cfg := ConfigFromEnv()

		if cfg.TCPAddr != "0.0.0.0:9999" {
			t.Errorf("TCPAddr = %q, want 0.0.0.0:9999", cfg.TCPAddr)
		}
	})

	t.Run("BOSUN_BEARER_TOKEN sets bearer token", func(t *testing.T) {
		t.Setenv("BOSUN_BEARER_TOKEN", "secret-token")

		cfg := ConfigFromEnv()

		if cfg.BearerToken != "secret-token" {
			t.Errorf("BearerToken = %q, want secret-token", cfg.BearerToken)
		}
	})

	t.Run("GITHUB_WEBHOOK_SECRET overrides WEBHOOK_SECRET", func(t *testing.T) {
		t.Setenv("WEBHOOK_SECRET", "generic-secret")
		t.Setenv("GITHUB_WEBHOOK_SECRET", "github-secret")

		cfg := ConfigFromEnv()

		if cfg.WebhookSecret != "github-secret" {
			t.Errorf("WebhookSecret = %q, want github-secret", cfg.WebhookSecret)
		}
	})

	t.Run("POLL_INTERVAL in seconds", func(t *testing.T) {
		t.Setenv("POLL_INTERVAL", "300")

		cfg := ConfigFromEnv()

		if cfg.PollInterval != 300*time.Second {
			t.Errorf("PollInterval = %v, want 5m0s", cfg.PollInterval)
		}
	})

	t.Run("BOSUN_POLL_INTERVAL overrides POLL_INTERVAL", func(t *testing.T) {
		t.Setenv("POLL_INTERVAL", "300")
		t.Setenv("BOSUN_POLL_INTERVAL", "600")

		cfg := ConfigFromEnv()

		if cfg.PollInterval != 600*time.Second {
			t.Errorf("PollInterval = %v, want 10m0s", cfg.PollInterval)
		}
	})

	t.Run("BOSUN_REPO_URL overrides REPO_URL", func(t *testing.T) {
		t.Setenv("REPO_URL", "https://github.com/old/repo")
		t.Setenv("BOSUN_REPO_URL", "https://github.com/new/repo")

		cfg := ConfigFromEnv()

		if cfg.ReconcileConfig.RepoURL != "https://github.com/new/repo" {
			t.Errorf("RepoURL = %q, want https://github.com/new/repo", cfg.ReconcileConfig.RepoURL)
		}
	})

	t.Run("BOSUN_REPO_BRANCH overrides REPO_BRANCH", func(t *testing.T) {
		t.Setenv("REPO_BRANCH", "develop")
		t.Setenv("BOSUN_REPO_BRANCH", "production")

		cfg := ConfigFromEnv()

		if cfg.ReconcileConfig.RepoBranch != "production" {
			t.Errorf("RepoBranch = %q, want production", cfg.ReconcileConfig.RepoBranch)
		}
	})

	t.Run("DEPLOY_TARGET sets target host", func(t *testing.T) {
		t.Setenv("DEPLOY_TARGET", "server.local")

		cfg := ConfigFromEnv()

		if cfg.ReconcileConfig.TargetHost != "server.local" {
			t.Errorf("TargetHost = %q, want server.local", cfg.ReconcileConfig.TargetHost)
		}
	})

	t.Run("BOSUN_SECRETS_FILE overrides SECRETS_FILES", func(t *testing.T) {
		t.Setenv("SECRETS_FILES", "old.yaml")
		t.Setenv("BOSUN_SECRETS_FILE", "new.yaml, another.yaml")

		cfg := ConfigFromEnv()

		want := []string{"new.yaml", "another.yaml"}
		got := cfg.ReconcileConfig.SecretsFiles
		if len(got) != len(want) {
			t.Errorf("SecretsFiles len = %d, want %d", len(got), len(want))
			return
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("SecretsFiles[%d] = %q, want %q", i, got[i], want[i])
			}
		}
	})

	t.Run("DRY_RUN sets dry run mode", func(t *testing.T) {
		t.Setenv("DRY_RUN", "true")

		cfg := ConfigFromEnv()

		if !cfg.ReconcileConfig.DryRun {
			t.Error("DryRun should be true when DRY_RUN=true")
		}
	})

	t.Run("invalid port ignored", func(t *testing.T) {
		t.Setenv("PORT", "not-a-number")

		cfg := ConfigFromEnv()

		// Should use default
		if cfg.Port != 8080 {
			t.Errorf("Port = %d, want 8080 (default)", cfg.Port)
		}
	})

	t.Run("invalid poll interval ignored", func(t *testing.T) {
		t.Setenv("POLL_INTERVAL", "not-a-number")

		cfg := ConfigFromEnv()

		// Should use default
		if cfg.PollInterval != time.Hour {
			t.Errorf("PollInterval = %v, want 1h (default)", cfg.PollInterval)
		}
	})
}

func TestSplitAndTrim(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "simple list",
			input: "a,b,c",
			want:  []string{"a", "b", "c"},
		},
		{
			name:  "with spaces",
			input: " a , b , c ",
			want:  []string{"a", "b", "c"},
		},
		{
			name:  "empty parts",
			input: "a,,b",
			want:  []string{"a", "b"},
		},
		{
			name:  "single item",
			input: "a",
			want:  []string{"a"},
		},
		{
			name:  "empty string",
			input: "",
			want:  []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitAndTrim(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("splitAndTrim() len = %d, want %d", len(got), len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("splitAndTrim()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestWidgetData(t *testing.T) {
	t.Run("fresh state returns zeroes", func(t *testing.T) {
		stateFile := filepath.Join(t.TempDir(), "state.json")
		d := &Daemon{
			config: &Config{
				ReconcileConfig: &reconcile.Config{
					StateFile: stateFile,
				},
			},
		}

		data := d.WidgetData()

		if data["deploys_total"] != 0 {
			t.Errorf("deploys_total = %v, want 0", data["deploys_total"])
		}
		if data["last_deploy"] != "" {
			t.Errorf("last_deploy = %v, want empty", data["last_deploy"])
		}
		if data["status"] != "ok" {
			t.Errorf("status = %v, want ok", data["status"])
		}
		if data["git_sha"] != "" {
			t.Errorf("git_sha = %v, want empty", data["git_sha"])
		}
	})

	t.Run("with deploy history", func(t *testing.T) {
		stateFile := filepath.Join(t.TempDir(), "state.json")
		state := &reconcile.DeployState{
			LastDeployedCommit: "abc1234def5678",
			DeployedAt:         time.Date(2026, 2, 15, 12, 0, 0, 0, time.UTC),
			DeployCount:        42,
		}
		if err := reconcile.SaveState(stateFile, state); err != nil {
			t.Fatalf("Failed to write state: %v", err)
		}

		d := &Daemon{
			config: &Config{
				ReconcileConfig: &reconcile.Config{
					StateFile: stateFile,
				},
			},
		}

		data := d.WidgetData()

		if data["deploys_total"] != 42 {
			t.Errorf("deploys_total = %v, want 42", data["deploys_total"])
		}
		if data["last_deploy"] != "2026-02-15T12:00:00Z" {
			t.Errorf("last_deploy = %v, want 2026-02-15T12:00:00Z", data["last_deploy"])
		}
		if data["status"] != "ok" {
			t.Errorf("status = %v, want ok", data["status"])
		}
		if data["git_sha"] != "abc1234" {
			t.Errorf("git_sha = %v, want abc1234", data["git_sha"])
		}
	})

	t.Run("error status when last reconcile failed", func(t *testing.T) {
		stateFile := filepath.Join(t.TempDir(), "state.json")
		d := &Daemon{
			config: &Config{
				ReconcileConfig: &reconcile.Config{
					StateFile: stateFile,
				},
			},
			lastError: fmt.Errorf("deployment failed"),
		}

		data := d.WidgetData()

		if data["status"] != "error" {
			t.Errorf("status = %v, want error", data["status"])
		}
	})
}

func TestConfigFromEnv_PostSyncHooksFromConfig(t *testing.T) {
	t.Run("loads hooks from bosun.yaml", func(t *testing.T) {
		tmpDir := t.TempDir()
		// macOS: /var -> /private/var symlink resolution.
		tmpDir = evalSymlinks(t, tmpDir)

		yamlContent := `manifest_dir: manifest
post_sync_hooks:
  - paths: ["traefik/conf.d/**"]
    action: restart
    container: traefik
`
		if err := os.WriteFile(filepath.Join(tmpDir, "bosun.yaml"), []byte(yamlContent), 0o644); err != nil {
			t.Fatalf("Failed to write bosun.yaml: %v", err)
		}
		if err := os.MkdirAll(filepath.Join(tmpDir, "manifest"), 0o755); err != nil {
			t.Fatalf("Failed to create manifest dir: %v", err)
		}

		origDir, err := os.Getwd()
		require.NoError(t, err)
		defer func() { _ = os.Chdir(origDir) }()
		require.NoError(t, os.Chdir(tmpDir))

		cfg := ConfigFromEnv()

		hooks := cfg.ReconcileConfig.PostSyncHooks
		if len(hooks) != 1 {
			t.Fatalf("PostSyncHooks len = %d, want 1", len(hooks))
		}
		if hooks[0].Container != "traefik" {
			t.Errorf("PostSyncHooks[0].Container = %q, want traefik", hooks[0].Container)
		}
		if hooks[0].Action != "restart" {
			t.Errorf("PostSyncHooks[0].Action = %q, want restart", hooks[0].Action)
		}
	})

	t.Run("env var overrides bosun.yaml hooks", func(t *testing.T) {
		tmpDir := t.TempDir()
		tmpDir = evalSymlinks(t, tmpDir)

		yamlContent := `manifest_dir: manifest
post_sync_hooks:
  - paths: ["traefik/conf.d/**"]
    action: restart
    container: traefik
`
		if err := os.WriteFile(filepath.Join(tmpDir, "bosun.yaml"), []byte(yamlContent), 0o644); err != nil {
			t.Fatalf("Failed to write bosun.yaml: %v", err)
		}
		if err := os.MkdirAll(filepath.Join(tmpDir, "manifest"), 0o755); err != nil {
			t.Fatalf("Failed to create manifest dir: %v", err)
		}

		origDir, err := os.Getwd()
		require.NoError(t, err)
		defer func() { _ = os.Chdir(origDir) }()
		require.NoError(t, os.Chdir(tmpDir))

		envHooks := []reconcile.PostSyncHook{{
			Paths:     []string{"authelia/**"},
			Action:    "restart",
			Container: "authelia",
		}}
		envJSON, _ := json.Marshal(envHooks)
		t.Setenv("BOSUN_POST_SYNC_HOOKS", string(envJSON))

		cfg := ConfigFromEnv()

		hooks := cfg.ReconcileConfig.PostSyncHooks
		if len(hooks) != 1 {
			t.Fatalf("PostSyncHooks len = %d, want 1", len(hooks))
		}
		if hooks[0].Container != "authelia" {
			t.Errorf("PostSyncHooks[0].Container = %q, want authelia (env var should override config)", hooks[0].Container)
		}
	})

	t.Run("no config file uses env var only", func(t *testing.T) {
		tmpDir := t.TempDir()
		tmpDir = evalSymlinks(t, tmpDir)

		origDir, err := os.Getwd()
		require.NoError(t, err)
		defer func() { _ = os.Chdir(origDir) }()
		require.NoError(t, os.Chdir(tmpDir))

		envHooks := []reconcile.PostSyncHook{{
			Paths:     []string{"nginx/**"},
			Action:    "restart",
			Container: "nginx",
		}}
		envJSON, _ := json.Marshal(envHooks)
		t.Setenv("BOSUN_POST_SYNC_HOOKS", string(envJSON))

		cfg := ConfigFromEnv()

		hooks := cfg.ReconcileConfig.PostSyncHooks
		if len(hooks) != 1 {
			t.Fatalf("PostSyncHooks len = %d, want 1", len(hooks))
		}
		if hooks[0].Container != "nginx" {
			t.Errorf("PostSyncHooks[0].Container = %q, want nginx", hooks[0].Container)
		}
	})
}

func TestConfigFromEnv_HookSettleDelay(t *testing.T) {
	t.Run("parses Go duration string", func(t *testing.T) {
		t.Setenv("BOSUN_HOOK_SETTLE_DELAY", "2s")

		cfg := ConfigFromEnv()

		if cfg.ReconcileConfig.HookSettleDelay != 2*time.Second {
			t.Errorf("HookSettleDelay = %v, want 2s", cfg.ReconcileConfig.HookSettleDelay)
		}
	})

	t.Run("parses bare seconds", func(t *testing.T) {
		t.Setenv("BOSUN_HOOK_SETTLE_DELAY", "5")

		cfg := ConfigFromEnv()

		if cfg.ReconcileConfig.HookSettleDelay != 5*time.Second {
			t.Errorf("HookSettleDelay = %v, want 5s", cfg.ReconcileConfig.HookSettleDelay)
		}
	})

	t.Run("defaults to zero when not set", func(t *testing.T) {
		cfg := ConfigFromEnv()

		if cfg.ReconcileConfig.HookSettleDelay != 0 {
			t.Errorf("HookSettleDelay = %v, want 0", cfg.ReconcileConfig.HookSettleDelay)
		}
	})

	t.Run("invalid value ignored", func(t *testing.T) {
		t.Setenv("BOSUN_HOOK_SETTLE_DELAY", "not-a-duration")

		cfg := ConfigFromEnv()

		if cfg.ReconcileConfig.HookSettleDelay != 0 {
			t.Errorf("HookSettleDelay = %v, want 0 (default)", cfg.ReconcileConfig.HookSettleDelay)
		}
	})
}

func TestConfigFromEnv_HooksWithDelay(t *testing.T) {
	t.Run("parses hooks JSON with delay field", func(t *testing.T) {
		envHooks := `[{"paths":["traefik/conf.d/**"],"action":"restart","container":"traefik","delay":"5s"}]`
		t.Setenv("BOSUN_POST_SYNC_HOOKS", envHooks)

		cfg := ConfigFromEnv()

		hooks := cfg.ReconcileConfig.PostSyncHooks
		if len(hooks) != 1 {
			t.Fatalf("PostSyncHooks len = %d, want 1", len(hooks))
		}
		if hooks[0].Container != "traefik" {
			t.Errorf("PostSyncHooks[0].Container = %q, want traefik", hooks[0].Container)
		}
		if hooks[0].Delay.Duration != 5*time.Second {
			t.Errorf("PostSyncHooks[0].Delay = %v, want 5s", hooks[0].Delay.Duration)
		}
	})

	t.Run("hooks without delay default to zero", func(t *testing.T) {
		envHooks := `[{"paths":["gatus/**"],"action":"restart","container":"gatus"}]`
		t.Setenv("BOSUN_POST_SYNC_HOOKS", envHooks)

		cfg := ConfigFromEnv()

		hooks := cfg.ReconcileConfig.PostSyncHooks
		if len(hooks) != 1 {
			t.Fatalf("PostSyncHooks len = %d, want 1", len(hooks))
		}
		if hooks[0].Delay.Duration != 0 {
			t.Errorf("PostSyncHooks[0].Delay = %v, want 0", hooks[0].Delay.Duration)
		}
	})
}

func TestConfigFromEnv_InfraDir(t *testing.T) {
	// Save and restore environment
	orig := os.Getenv("BOSUN_INFRA_DIR")
	defer func() {
		if orig != "" {
			_ = os.Setenv("BOSUN_INFRA_DIR", orig)
		} else {
			_ = os.Unsetenv("BOSUN_INFRA_DIR")
		}
	}()

	t.Run("uses default when not set", func(t *testing.T) {
		_ = os.Unsetenv("BOSUN_INFRA_DIR")
		cfg := ConfigFromEnv()
		if cfg.ReconcileConfig.InfraSubDir != "." {
			t.Errorf("InfraSubDir = %q, want .", cfg.ReconcileConfig.InfraSubDir)
		}
	})

	t.Run("uses env var when set", func(t *testing.T) {
		_ = os.Setenv("BOSUN_INFRA_DIR", "unraid")
		cfg := ConfigFromEnv()
		if cfg.ReconcileConfig.InfraSubDir != "unraid" {
			t.Errorf("InfraSubDir = %q, want unraid", cfg.ReconcileConfig.InfraSubDir)
		}
	})
}

func TestConfigFromEnv_DriftAlertCooldown(t *testing.T) {
	t.Run("parses Go duration string", func(t *testing.T) {
		t.Setenv("BOSUN_DRIFT_ALERT_COOLDOWN", "30m")

		cfg := ConfigFromEnv()

		if cfg.DriftAlertCooldown != 30*time.Minute {
			t.Errorf("DriftAlertCooldown = %v, want 30m", cfg.DriftAlertCooldown)
		}
	})

	t.Run("parses bare seconds", func(t *testing.T) {
		t.Setenv("BOSUN_DRIFT_ALERT_COOLDOWN", "3600")

		cfg := ConfigFromEnv()

		if cfg.DriftAlertCooldown != time.Hour {
			t.Errorf("DriftAlertCooldown = %v, want 1h", cfg.DriftAlertCooldown)
		}
	})

	t.Run("defaults to 1h when not set", func(t *testing.T) {
		cfg := ConfigFromEnv()

		if cfg.DriftAlertCooldown != time.Hour {
			t.Errorf("DriftAlertCooldown = %v, want 1h (default)", cfg.DriftAlertCooldown)
		}
	})

	t.Run("invalid value keeps default", func(t *testing.T) {
		t.Setenv("BOSUN_DRIFT_ALERT_COOLDOWN", "not-a-duration")

		cfg := ConfigFromEnv()

		if cfg.DriftAlertCooldown != time.Hour {
			t.Errorf("DriftAlertCooldown = %v, want 1h (default)", cfg.DriftAlertCooldown)
		}
	})
}

func TestConfigFromEnv_DriftAlertDebounce(t *testing.T) {
	t.Run("parses Go duration string", func(t *testing.T) {
		t.Setenv("BOSUN_DRIFT_ALERT_DEBOUNCE", "5m")

		cfg := ConfigFromEnv()

		assert.Equal(t, 5*time.Minute, cfg.DriftAlertDebounce)
	})

	t.Run("parses bare seconds", func(t *testing.T) {
		t.Setenv("BOSUN_DRIFT_ALERT_DEBOUNCE", "300")

		cfg := ConfigFromEnv()

		assert.Equal(t, 300*time.Second, cfg.DriftAlertDebounce)
	})

	t.Run("defaults to 0 when not set", func(t *testing.T) {
		cfg := ConfigFromEnv()

		assert.Equal(t, time.Duration(0), cfg.DriftAlertDebounce)
	})

	t.Run("invalid value keeps default 0", func(t *testing.T) {
		t.Setenv("BOSUN_DRIFT_ALERT_DEBOUNCE", "not-a-duration")

		cfg := ConfigFromEnv()

		assert.Equal(t, time.Duration(0), cfg.DriftAlertDebounce)
	})
}

func TestDefaultConfig_DriftAlertDebounce(t *testing.T) {
	cfg := DefaultConfig()

	assert.Equal(t, time.Duration(0), cfg.DriftAlertDebounce, "DriftAlertDebounce should default to 0 (disabled)")
}

func TestConfigFromEnv_DriftAlertDebounce_EnvZeroOverridesConfig(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir = evalSymlinks(t, tmpDir)

	yamlContent := `manifest_dir: manifest
drift_alert_debounce: "10m"
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "bosun.yaml"), []byte(yamlContent), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "manifest"), 0o755))

	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(origDir) }()
	require.NoError(t, os.Chdir(tmpDir))

	// Explicitly set env var to "0" to disable debouncing.
	t.Setenv("BOSUN_DRIFT_ALERT_DEBOUNCE", "0")

	cfg := ConfigFromEnv()

	assert.Equal(t, time.Duration(0), cfg.DriftAlertDebounce,
		"env var set to '0' should disable debounce, not fall through to config file value")
}

func TestConfigFromEnv_DriftResolveAlerts(t *testing.T) {
	t.Run("defaults to true when not set", func(t *testing.T) {
		cfg := ConfigFromEnv()

		if !cfg.DriftResolveAlerts {
			t.Error("DriftResolveAlerts should be true by default")
		}
	})

	t.Run("false disables resolve alerts", func(t *testing.T) {
		t.Setenv("BOSUN_DRIFT_RESOLVE_ALERTS", "false")

		cfg := ConfigFromEnv()

		if cfg.DriftResolveAlerts {
			t.Error("DriftResolveAlerts should be false when set to 'false'")
		}
	})

	t.Run("0 disables resolve alerts", func(t *testing.T) {
		t.Setenv("BOSUN_DRIFT_RESOLVE_ALERTS", "0")

		cfg := ConfigFromEnv()

		if cfg.DriftResolveAlerts {
			t.Error("DriftResolveAlerts should be false when set to '0'")
		}
	})

	t.Run("true keeps resolve alerts enabled", func(t *testing.T) {
		t.Setenv("BOSUN_DRIFT_RESOLVE_ALERTS", "true")

		cfg := ConfigFromEnv()

		if !cfg.DriftResolveAlerts {
			t.Error("DriftResolveAlerts should be true when set to 'true'")
		}
	})
}

func TestConfigFromEnv_EnvOverrideFlags(t *testing.T) {
	t.Run("PostSyncHooksFromEnv set when env var present", func(t *testing.T) {
		envHooks := `[{"paths":["traefik/**"],"action":"restart","container":"traefik"}]`
		t.Setenv("BOSUN_POST_SYNC_HOOKS", envHooks)

		cfg := ConfigFromEnv()

		if !cfg.ReconcileConfig.PostSyncHooksFromEnv {
			t.Error("PostSyncHooksFromEnv should be true when BOSUN_POST_SYNC_HOOKS is set")
		}
	})

	t.Run("PostSyncHooksFromEnv false when env var absent", func(t *testing.T) {
		cfg := ConfigFromEnv()

		if cfg.ReconcileConfig.PostSyncHooksFromEnv {
			t.Error("PostSyncHooksFromEnv should be false when BOSUN_POST_SYNC_HOOKS is not set")
		}
	})

	t.Run("HookSettleDelayFromEnv set when env var present", func(t *testing.T) {
		t.Setenv("BOSUN_HOOK_SETTLE_DELAY", "3s")

		cfg := ConfigFromEnv()

		if !cfg.ReconcileConfig.HookSettleDelayFromEnv {
			t.Error("HookSettleDelayFromEnv should be true when BOSUN_HOOK_SETTLE_DELAY is set")
		}
	})

	t.Run("HookSettleDelayFromEnv false when env var absent", func(t *testing.T) {
		cfg := ConfigFromEnv()

		if cfg.ReconcileConfig.HookSettleDelayFromEnv {
			t.Error("HookSettleDelayFromEnv should be false when BOSUN_HOOK_SETTLE_DELAY is not set")
		}
	})

	t.Run("ConfigReloader is wired", func(t *testing.T) {
		cfg := ConfigFromEnv()

		if cfg.ReconcileConfig.ConfigReloader == nil {
			t.Error("ConfigReloader should be set by ConfigFromEnv")
		}
	})
}

func TestConfigFromEnv_ContentHashSync(t *testing.T) {
	t.Run("default true", func(t *testing.T) {
		cfg := ConfigFromEnv()

		if !cfg.ContentHashSync {
			t.Error("ContentHashSync should be true by default")
		}
		if cfg.ReconcileConfig == nil || !cfg.ReconcileConfig.ContentHashSync {
			t.Error("ReconcileConfig.ContentHashSync should be true by default")
		}
	})

	t.Run("disabled with false", func(t *testing.T) {
		t.Setenv("BOSUN_CONTENT_HASH_SYNC", "false")

		cfg := ConfigFromEnv()

		if cfg.ContentHashSync {
			t.Error("ContentHashSync should be false when set to 'false'")
		}
		if cfg.ReconcileConfig != nil && cfg.ReconcileConfig.ContentHashSync {
			t.Error("ReconcileConfig.ContentHashSync should be false")
		}
	})

	t.Run("disabled with 0", func(t *testing.T) {
		t.Setenv("BOSUN_CONTENT_HASH_SYNC", "0")

		cfg := ConfigFromEnv()

		if cfg.ContentHashSync {
			t.Error("ContentHashSync should be false when set to '0'")
		}
	})

	t.Run("enabled with true", func(t *testing.T) {
		t.Setenv("BOSUN_CONTENT_HASH_SYNC", "true")

		cfg := ConfigFromEnv()

		if !cfg.ContentHashSync {
			t.Error("ContentHashSync should be true when set to 'true'")
		}
	})
}

func TestConfigFromEnv_RemoveOrphans(t *testing.T) {
	t.Run("default true", func(t *testing.T) {
		cfg := ConfigFromEnv()

		if !cfg.RemoveOrphans {
			t.Error("RemoveOrphans should be true by default")
		}
		if cfg.ReconcileConfig == nil || !cfg.ReconcileConfig.RemoveOrphans {
			t.Error("ReconcileConfig.RemoveOrphans should be true by default")
		}
	})

	t.Run("disabled with false", func(t *testing.T) {
		t.Setenv("BOSUN_REMOVE_ORPHANS", "false")

		cfg := ConfigFromEnv()

		if cfg.RemoveOrphans {
			t.Error("RemoveOrphans should be false when set to 'false'")
		}
		if cfg.ReconcileConfig != nil && cfg.ReconcileConfig.RemoveOrphans {
			t.Error("ReconcileConfig.RemoveOrphans should be false")
		}
	})

	t.Run("disabled with 0", func(t *testing.T) {
		t.Setenv("BOSUN_REMOVE_ORPHANS", "0")

		cfg := ConfigFromEnv()

		if cfg.RemoveOrphans {
			t.Error("RemoveOrphans should be false when set to '0'")
		}
	})

	t.Run("enabled with true", func(t *testing.T) {
		t.Setenv("BOSUN_REMOVE_ORPHANS", "true")

		cfg := ConfigFromEnv()

		if !cfg.RemoveOrphans {
			t.Error("RemoveOrphans should be true when set to 'true'")
		}
	})
}

func TestConfigFromEnv_ComposeUpTimeout(t *testing.T) {
	t.Run("default not set (zero, uses deploy package default)", func(t *testing.T) {
		cfg := ConfigFromEnv()
		require.NotNil(t, cfg.ReconcileConfig)
		assert.Zero(t, cfg.ReconcileConfig.ComposeUpTimeout)
	})

	t.Run("parses Go duration string", func(t *testing.T) {
		t.Setenv("BOSUN_COMPOSE_UP_TIMEOUT", "30m")
		cfg := ConfigFromEnv()
		require.NotNil(t, cfg.ReconcileConfig)
		assert.Equal(t, 30*time.Minute, cfg.ReconcileConfig.ComposeUpTimeout)
	})

	t.Run("parses plain seconds", func(t *testing.T) {
		t.Setenv("BOSUN_COMPOSE_UP_TIMEOUT", "1800")
		cfg := ConfigFromEnv()
		require.NotNil(t, cfg.ReconcileConfig)
		assert.Equal(t, 30*time.Minute, cfg.ReconcileConfig.ComposeUpTimeout)
	})

	t.Run("invalid value is skipped", func(t *testing.T) {
		t.Setenv("BOSUN_COMPOSE_UP_TIMEOUT", "not-a-duration")
		cfg := ConfigFromEnv()
		require.NotNil(t, cfg.ReconcileConfig)
		assert.Zero(t, cfg.ReconcileConfig.ComposeUpTimeout)
	})
}

func TestConfigFromEnv_HealthCheckTimeout(t *testing.T) {
	t.Run("default uses reconcile package default", func(t *testing.T) {
		cfg := ConfigFromEnv()
		require.NotNil(t, cfg.ReconcileConfig)
		assert.Equal(t, 60*time.Second, cfg.ReconcileConfig.HealthCheckTimeout)
	})

	t.Run("parses Go duration string", func(t *testing.T) {
		t.Setenv("BOSUN_HEALTH_CHECK_TIMEOUT", "2m")
		cfg := ConfigFromEnv()
		require.NotNil(t, cfg.ReconcileConfig)
		assert.Equal(t, 2*time.Minute, cfg.ReconcileConfig.HealthCheckTimeout)
	})

	t.Run("parses plain seconds", func(t *testing.T) {
		t.Setenv("BOSUN_HEALTH_CHECK_TIMEOUT", "120")
		cfg := ConfigFromEnv()
		require.NotNil(t, cfg.ReconcileConfig)
		assert.Equal(t, 2*time.Minute, cfg.ReconcileConfig.HealthCheckTimeout)
	})

	t.Run("invalid value retains default", func(t *testing.T) {
		t.Setenv("BOSUN_HEALTH_CHECK_TIMEOUT", "not-a-duration")
		cfg := ConfigFromEnv()
		require.NotNil(t, cfg.ReconcileConfig)
		assert.Equal(t, 60*time.Second, cfg.ReconcileConfig.HealthCheckTimeout)
	})
}

func TestConfigFromEnv_HealthCheckInterval(t *testing.T) {
	t.Run("default uses reconcile package default", func(t *testing.T) {
		cfg := ConfigFromEnv()
		require.NotNil(t, cfg.ReconcileConfig)
		assert.Equal(t, 5*time.Second, cfg.ReconcileConfig.HealthCheckInterval)
	})

	t.Run("parses Go duration string", func(t *testing.T) {
		t.Setenv("BOSUN_HEALTH_CHECK_INTERVAL", "10s")
		cfg := ConfigFromEnv()
		require.NotNil(t, cfg.ReconcileConfig)
		assert.Equal(t, 10*time.Second, cfg.ReconcileConfig.HealthCheckInterval)
	})

	t.Run("parses plain seconds", func(t *testing.T) {
		t.Setenv("BOSUN_HEALTH_CHECK_INTERVAL", "10")
		cfg := ConfigFromEnv()
		require.NotNil(t, cfg.ReconcileConfig)
		assert.Equal(t, 10*time.Second, cfg.ReconcileConfig.HealthCheckInterval)
	})

	t.Run("invalid value retains default", func(t *testing.T) {
		t.Setenv("BOSUN_HEALTH_CHECK_INTERVAL", "not-a-duration")
		cfg := ConfigFromEnv()
		require.NotNil(t, cfg.ReconcileConfig)
		assert.Equal(t, 5*time.Second, cfg.ReconcileConfig.HealthCheckInterval)
	})
}

// ---------------------------------------------------------------------------
// Phase 1C: TriggerReconcile Concurrency
// ---------------------------------------------------------------------------

// newConcurrencyDaemon creates a daemon suitable for concurrency tests.
// DryRun=true and no real git repo, so reconcile will fail fast at
// acquireLock or syncRepo -- that's fine, we're testing the envelope.
func newConcurrencyDaemon(t *testing.T) *Daemon {
	t.Helper()
	tmpDir := t.TempDir()
	tmpDir = evalSymlinks(t, tmpDir)

	cfg := DefaultConfig()
	cfg.EnableHTTP = false
	cfg.ReconcileConfig = reconcile.DefaultConfig()
	cfg.ReconcileConfig.RepoURL = "https://github.com/test/repo"
	cfg.ReconcileConfig.DryRun = true
	cfg.ReconcileConfig.RepoDir = filepath.Join(tmpDir, "repo")
	cfg.ReconcileConfig.LockFile = filepath.Join(tmpDir, "test.lock")
	cfg.ReconcileConfig.StateFile = filepath.Join(tmpDir, "state.json")
	cfg.SocketPath = filepath.Join(tmpDir, "test.sock")

	d, err := New(cfg)
	require.NoError(t, err)
	return d
}

func TestTriggerReconcile_SingleTrigger(t *testing.T) {
	d := newConcurrencyDaemon(t)
	ctx := context.Background()

	// The reconcile will fail because there's no git repo -- that's expected.
	// We're testing that the trigger envelope completes without hanging.
	err := d.TriggerReconcile(ctx, "test", false)

	// Error is expected (no git repo), but the call should complete.
	// The important thing is it doesn't hang or panic.
	_ = err

	// Verify reconciling flag is cleared after completion.
	d.reconcileMu.Lock()
	assert.False(t, d.reconciling, "reconciling should be false after completion")
	d.reconcileMu.Unlock()
}

func TestTriggerReconcile_ConcurrentTriggers(t *testing.T) {
	d := newConcurrencyDaemon(t)
	ctx := context.Background()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		_ = d.TriggerReconcile(ctx, "trigger1", false)
	}()

	// Small delay to let first trigger start reconciling.
	time.Sleep(10 * time.Millisecond)

	go func() {
		defer wg.Done()
		_ = d.TriggerReconcile(ctx, "trigger2", false)
	}()

	wg.Wait()

	// Verify clean state after both complete.
	d.reconcileMu.Lock()
	assert.False(t, d.reconciling, "reconciling should be false after all triggers complete")
	assert.False(t, d.pendingTrigger, "pendingTrigger should be false after processing")
	d.reconcileMu.Unlock()
}

func TestTriggerReconcile_ForceStickiness(t *testing.T) {
	d := newConcurrencyDaemon(t)
	ctx := context.Background()

	// Manually lock the reconcile so we can inject pending triggers.
	d.reconcileMu.Lock()
	d.reconciling = true
	d.reconcileMu.Unlock()

	// First trigger: non-force.
	err1 := d.TriggerReconcile(ctx, "trigger-noforce", false)
	assert.NoError(t, err1, "queued trigger should return nil")

	// Second trigger: force.
	err2 := d.TriggerReconcile(ctx, "trigger-force", true)
	assert.NoError(t, err2, "queued trigger should return nil")

	// Verify force is sticky: once set, stays set.
	d.reconcileMu.Lock()
	assert.True(t, d.pendingTrigger, "pendingTrigger should be true")
	assert.True(t, d.triggerForce, "triggerForce should be sticky (true)")
	assert.Equal(t, "trigger-force", d.triggerSource, "source should be from latest trigger")

	// Clean up: reset state so daemon doesn't hang.
	d.reconciling = false
	d.pendingTrigger = false
	d.triggerForce = false
	d.reconcileMu.Unlock()
}

func TestTriggerReconcile_ContextCancellation(t *testing.T) {
	d := newConcurrencyDaemon(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	// Should complete without hanging even with cancelled context.
	err := d.TriggerReconcile(ctx, "cancelled", false)
	// May or may not error -- the point is it doesn't hang.
	_ = err

	d.reconcileMu.Lock()
	assert.False(t, d.reconciling, "reconciling should be false after cancellation")
	d.reconcileMu.Unlock()
}

// ---------------------------------------------------------------------------
// Phase 1D: State Accessors
// ---------------------------------------------------------------------------

func TestHealthStatus_Accessors(t *testing.T) {
	t.Run("healthy by default", func(t *testing.T) {
		tmpDir := t.TempDir()
		tmpDir = evalSymlinks(t, tmpDir)

		d := &Daemon{
			config: &Config{
				ReconcileConfig: &reconcile.Config{
					StateFile: filepath.Join(tmpDir, "state.json"),
				},
			},
			stopLoops: make(chan struct{}),
		}

		status := d.HealthStatus()
		assert.Equal(t, "healthy", status.Status)
	})

	t.Run("degraded after setting lastError", func(t *testing.T) {
		tmpDir := t.TempDir()
		tmpDir = evalSymlinks(t, tmpDir)

		d := &Daemon{
			config: &Config{
				ReconcileConfig: &reconcile.Config{
					StateFile: filepath.Join(tmpDir, "state.json"),
				},
			},
			lastError: errors.New("deploy failed"),
			stopLoops: make(chan struct{}),
		}

		status := d.HealthStatus()
		assert.Equal(t, "degraded", status.Status)
		assert.Equal(t, "deploy failed", status.LastError)
	})
}

func TestIsReady_SetReady(t *testing.T) {
	d := &Daemon{
		config:    DefaultConfig(),
		stopLoops: make(chan struct{}),
	}

	assert.False(t, d.IsReady(), "should start not ready")

	d.setReady(true)
	assert.True(t, d.IsReady(), "should be ready after setReady(true)")

	d.setReady(false)
	assert.False(t, d.IsReady(), "should be not ready after setReady(false)")
}

func TestLastReconcile(t *testing.T) {
	d := &Daemon{
		config:    DefaultConfig(),
		stopLoops: make(chan struct{}),
	}

	t.Run("starts zero", func(t *testing.T) {
		lastTime, lastErr := d.LastReconcile()
		assert.True(t, lastTime.IsZero(), "lastReconcile should be zero initially")
		assert.NoError(t, lastErr, "lastError should be nil initially")
	})

	t.Run("returns correct values after setting", func(t *testing.T) {
		now := time.Now()
		testErr := errors.New("test error")

		d.stateMu.Lock()
		d.lastReconcile = now
		d.lastError = testErr
		d.stateMu.Unlock()

		lastTime, lastErr := d.LastReconcile()
		assert.Equal(t, now, lastTime)
		assert.Equal(t, testErr, lastErr)
	})
}

func TestVersionOrDev(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty returns dev", input: "", want: "dev"},
		{name: "version returns version", input: "1.0.0", want: "1.0.0"},
		{name: "pre-release version", input: "0.16.0-rc1", want: "0.16.0-rc1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, versionOrDev(tt.input))
		})
	}
}

func TestWidgetData_Structure(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir = evalSymlinks(t, tmpDir)

	d := &Daemon{
		config: &Config{
			ReconcileConfig: &reconcile.Config{
				StateFile: filepath.Join(tmpDir, "state.json"),
			},
		},
		stopLoops: make(chan struct{}),
	}

	data := d.WidgetData()

	// Verify expected keys are present.
	assert.Contains(t, data, "deploys_total")
	assert.Contains(t, data, "last_deploy")
	assert.Contains(t, data, "status")
	assert.Contains(t, data, "git_sha")

	// Fresh state should have zero/empty values.
	assert.Equal(t, 0, data["deploys_total"])
	assert.Equal(t, "", data["last_deploy"])
	assert.Equal(t, "ok", data["status"])
	assert.Equal(t, "", data["git_sha"])
}

// ---------------------------------------------------------------------------
// Alert Wrapper Tests
// ---------------------------------------------------------------------------

// testAlertProvider implements alert.Provider for test assertions.
type testAlertProvider struct {
	alerts []*alert.Alert
}

func (p *testAlertProvider) Name() string                                { return "test" }
func (p *testAlertProvider) IsConfigured() bool                          { return true }
func (p *testAlertProvider) Send(_ context.Context, a *alert.Alert) error {
	p.alerts = append(p.alerts, a)
	return nil
}

func newAlertDaemon(t *testing.T, provider *testAlertProvider) *Daemon {
	t.Helper()
	tmpDir := t.TempDir()
	tmpDir = evalSymlinks(t, tmpDir)

	mgr := alert.NewManager()
	mgr.AddProvider(provider)

	cfg := DefaultConfig()
	cfg.EnableHTTP = false
	cfg.AlertManager = mgr
	cfg.ReconcileConfig = reconcile.DefaultConfig()
	cfg.ReconcileConfig.RepoURL = "https://github.com/test/repo"
	cfg.ReconcileConfig.DryRun = true
	cfg.ReconcileConfig.RepoDir = filepath.Join(tmpDir, "repo")
	cfg.ReconcileConfig.LockFile = filepath.Join(tmpDir, "test.lock")
	cfg.ReconcileConfig.StateFile = filepath.Join(tmpDir, "state.json")
	cfg.SocketPath = filepath.Join(tmpDir, "test.sock")

	d, err := New(cfg)
	require.NoError(t, err)
	return d
}

func TestSendDriftAlert(t *testing.T) {
	t.Run("report with missing and unhealthy items sends alert", func(t *testing.T) {
		provider := &testAlertProvider{}
		d := newAlertDaemon(t, provider)

		report := &reconcile.DriftReport{
			CheckedAt: time.Now(),
			Items: []reconcile.DriftItem{
				{Service: "api", Type: reconcile.DriftMissing},
				{Service: "web", Type: reconcile.DriftUnhealthy},
				{Service: "redis", Type: reconcile.DriftImageMismatch}, // should be excluded from alert text
			},
		}

		d.sendDriftAlert(context.Background(), report)

		require.Len(t, provider.alerts, 1)
		assert.Equal(t, "Drift Detected", provider.alerts[0].Title)
		assert.Contains(t, provider.alerts[0].Message, "api (missing)")
		assert.Contains(t, provider.alerts[0].Message, "web (unhealthy)")
		// image_mismatch is filtered out of the drift items list
		assert.NotContains(t, provider.alerts[0].Message, "redis")
	})

	t.Run("report with only image mismatch sends alert with empty items", func(t *testing.T) {
		provider := &testAlertProvider{}
		d := newAlertDaemon(t, provider)

		report := &reconcile.DriftReport{
			CheckedAt: time.Now(),
			Items: []reconcile.DriftItem{
				{Service: "redis", Type: reconcile.DriftImageMismatch},
			},
		}

		d.sendDriftAlert(context.Background(), report)

		require.Len(t, provider.alerts, 1)
		// Alert is still sent but with empty drift items (just the target)
		assert.Equal(t, "Drift Detected", provider.alerts[0].Title)
	})

	t.Run("no alerter configured panics", func(t *testing.T) {
		tmpDir := t.TempDir()
		tmpDir = evalSymlinks(t, tmpDir)

		d := &Daemon{
			config: &Config{
				ReconcileConfig: &reconcile.Config{
					StateFile: filepath.Join(tmpDir, "state.json"),
				},
			},
			alerter:   nil,
			stopLoops: make(chan struct{}),
		}

		// This would panic if sendDriftAlert didn't handle nil alerter.
		// The function calls d.alerter.SendDriftDetected — nil dereference check.
		assert.Panics(t, func() {
			d.sendDriftAlert(context.Background(), &reconcile.DriftReport{
				Items: []reconcile.DriftItem{{Service: "x", Type: reconcile.DriftMissing}},
			})
		}, "sendDriftAlert with nil alerter panics because the guard is in the caller")
	})
}

func TestSendDriftResolvedAlert(t *testing.T) {
	t.Run("resolved keys sends alert with target", func(t *testing.T) {
		provider := &testAlertProvider{}
		d := newAlertDaemon(t, provider)

		d.sendDriftResolvedAlert(context.Background(), []string{"api:missing", "web:unhealthy"})

		require.Len(t, provider.alerts, 1)
		assert.Equal(t, "Drift Resolved", provider.alerts[0].Title)
		assert.Contains(t, provider.alerts[0].Message, "local") // default target
		assert.Contains(t, provider.alerts[0].Message, "api:missing")
		assert.Contains(t, provider.alerts[0].Message, "web:unhealthy")
	})

	t.Run("custom TargetHost uses it instead of local", func(t *testing.T) {
		provider := &testAlertProvider{}
		d := newAlertDaemon(t, provider)
		d.config.ReconcileConfig.TargetHost = "unraid.local"

		d.sendDriftResolvedAlert(context.Background(), []string{"api:missing"})

		require.Len(t, provider.alerts, 1)
		assert.Contains(t, provider.alerts[0].Message, "unraid.local")
		assert.NotContains(t, provider.alerts[0].Message, "local,") // shouldn't be "local" fallback
	})

	t.Run("no alerter configured panics", func(t *testing.T) {
		tmpDir := t.TempDir()
		tmpDir = evalSymlinks(t, tmpDir)

		d := &Daemon{
			config: &Config{
				ReconcileConfig: &reconcile.Config{
					StateFile: filepath.Join(tmpDir, "state.json"),
				},
			},
			alerter:   nil,
			stopLoops: make(chan struct{}),
		}

		assert.Panics(t, func() {
			d.sendDriftResolvedAlert(context.Background(), []string{"api:missing"})
		}, "sendDriftResolvedAlert with nil alerter panics because the guard is in the caller")
	})
}

// ---------------------------------------------------------------------------
// Drift Debounce Integration Tests
// ---------------------------------------------------------------------------

func TestRunDriftCheck_DebounceDisabled(t *testing.T) {
	// When debounce is 0 (default), alerts fire immediately on first detection.
	provider := &testAlertProvider{}
	d := newAlertDaemon(t, provider)
	d.config.DriftAlertDebounce = 0
	d.config.DriftInterval = 5 * time.Minute

	stateFile := d.config.ReconcileConfig.StateFile

	// Seed state with declared services and drift items (simulating a drift check result).
	state := &reconcile.DeployState{
		LastDeployedCommit: "abc123",
		DeclaredServices: []reconcile.DeclaredService{
			{Name: "traefik", Image: "traefik:v3"},
		},
		DriftCheckedAt: time.Now(),
		DriftItems: []reconcile.DriftItem{
			{Service: "traefik", Type: reconcile.DriftUnhealthy},
		},
		DriftAlertedItems: map[string]time.Time{
			"traefik:unhealthy": time.Now().Add(-2 * time.Hour), // past cooldown
		},
	}
	require.NoError(t, reconcile.SaveState(stateFile, state))

	// Verify debounce map is nil initially (disabled).
	loaded := reconcile.LoadState(stateFile)
	assert.Nil(t, loaded.DriftDebounceItems)
}

func TestRunDriftCheck_DebounceStatePersistence(t *testing.T) {
	// Verify debounce items round-trip through state file.
	dir := t.TempDir()
	dir = evalSymlinks(t, dir)
	stateFile := filepath.Join(dir, "state.json")

	now := time.Now().Truncate(time.Second)
	state := &reconcile.DeployState{
		LastDeployedCommit: "abc123",
		DriftDebounceItems: map[string]time.Time{
			"traefik:unhealthy": now.Add(-2 * time.Minute),
			"authelia:missing":  now.Add(-1 * time.Minute),
		},
	}
	require.NoError(t, reconcile.SaveState(stateFile, state))

	loaded := reconcile.LoadState(stateFile)
	require.Len(t, loaded.DriftDebounceItems, 2)
	assert.WithinDuration(t,
		state.DriftDebounceItems["traefik:unhealthy"],
		loaded.DriftDebounceItems["traefik:unhealthy"],
		time.Second,
	)
}

func TestFilterDebounced_ZeroBypassesBehavior(t *testing.T) {
	// Verify zero debounce passes all items through (backwards compat).
	items := []reconcile.DriftItem{
		{Service: "traefik", Type: reconcile.DriftUnhealthy},
		{Service: "authelia", Type: reconcile.DriftMissing},
	}
	debounceMap := map[string]time.Time{}

	result := reconcile.FilterDebounced(items, debounceMap, 0)

	assert.Len(t, result, 2, "zero debounce should pass all items")
	assert.Empty(t, debounceMap, "zero debounce should not modify debounce map")
}

func TestDriftDebounce_E2E_PersistBeyondWindow(t *testing.T) {
	// Simulates: drift appears -> persists past debounce window -> alert fires -> resolves -> resolution alert fires.
	debounce := 5 * time.Minute

	// Cycle 1: drift detected, item enters debounce.
	items := []reconcile.DriftItem{
		{Service: "traefik", Type: reconcile.DriftUnhealthy},
	}
	debounceMap := map[string]time.Time{}

	result := reconcile.FilterDebounced(items, debounceMap, debounce)
	assert.Empty(t, result, "cycle 1: item should be in debounce")
	assert.Contains(t, debounceMap, "traefik:unhealthy")

	// Cycle 2: simulate time passing past the window by backdating first-seen.
	debounceMap["traefik:unhealthy"] = time.Now().Add(-6 * time.Minute)
	result = reconcile.FilterDebounced(items, debounceMap, debounce)
	require.Len(t, result, 1, "cycle 2: item should graduate")
	assert.Empty(t, debounceMap, "graduated item removed from debounce map")

	// Now the graduated item enters dedup layer.
	alertedItems := map[string]time.Time{}
	alertItems, resolvedKeys := reconcile.ShouldAlertDrift(result, alertedItems, time.Hour)
	assert.Len(t, alertItems, 1, "should trigger alert after graduation")
	assert.Empty(t, resolvedKeys)

	// Record alert.
	alertedItems["traefik:unhealthy"] = time.Now()

	// Cycle 3: drift resolves.
	emptyItems := []reconcile.DriftItem{}
	_, resolvedKeys = reconcile.ShouldAlertDrift(emptyItems, alertedItems, time.Hour)
	assert.Len(t, resolvedKeys, 1, "should detect resolution")
	assert.Equal(t, "traefik:unhealthy", resolvedKeys[0])
}

func TestDriftDebounce_E2E_ResolveBeforeWindow(t *testing.T) {
	// Simulates: drift appears -> resolves before debounce window -> no alerts.
	debounce := 5 * time.Minute

	// Cycle 1: drift detected, item enters debounce.
	items := []reconcile.DriftItem{
		{Service: "traefik", Type: reconcile.DriftUnhealthy},
	}
	debounceMap := map[string]time.Time{}

	result := reconcile.FilterDebounced(items, debounceMap, debounce)
	assert.Empty(t, result, "item should be in debounce")

	// Cycle 2: drift resolves (empty items list).
	emptyItems := []reconcile.DriftItem{}
	result = reconcile.FilterDebounced(emptyItems, debounceMap, debounce)
	assert.Empty(t, result, "no items should graduate")
	assert.Empty(t, debounceMap, "resolved item should be removed from debounce map")

	// No alerts should have been triggered at any point.
}

func TestDriftDebounce_E2E_PersistWithDedupCooldown(t *testing.T) {
	// Simulates: drift persists -> alert fires -> repeat check within cooldown -> dedup suppresses.
	debounce := 5 * time.Minute
	cooldown := time.Hour

	// Cycle 1: drift enters debounce.
	items := []reconcile.DriftItem{
		{Service: "traefik", Type: reconcile.DriftUnhealthy},
	}
	debounceMap := map[string]time.Time{}

	reconcile.FilterDebounced(items, debounceMap, debounce)

	// Cycle 2: graduate past debounce window.
	debounceMap["traefik:unhealthy"] = time.Now().Add(-6 * time.Minute)
	graduated := reconcile.FilterDebounced(items, debounceMap, debounce)
	require.Len(t, graduated, 1)

	// First alert fires.
	alertedItems := map[string]time.Time{}
	alertItems, _ := reconcile.ShouldAlertDrift(graduated, alertedItems, cooldown)
	assert.Len(t, alertItems, 1)
	alertedItems["traefik:unhealthy"] = time.Now()

	// Cycle 3: drift still present, no debounce items (already graduated).
	// Since debounce is 0 for already-graduated items, items pass through.
	graduated2 := reconcile.FilterDebounced(items, debounceMap, debounce)
	// Item is new to debounce again since it was removed on graduation.
	// But it enters debounce again since debounce map is empty.
	// Actually, the item re-enters debounce. But in the real daemon,
	// once graduated, the item is tracked by DriftAlertedItems, not debounce.
	// The dedup layer handles repeat suppression.

	// In a real daemon flow, the graduated items would go through dedup.
	// Let's simulate the dedup directly.
	alertItems2, _ := reconcile.ShouldAlertDrift(items, alertedItems, cooldown)
	assert.Empty(t, alertItems2, "dedup should suppress within cooldown")

	// After cooldown expires.
	alertedItems["traefik:unhealthy"] = time.Now().Add(-2 * time.Hour)
	alertItems3, _ := reconcile.ShouldAlertDrift(items, alertedItems, cooldown)
	assert.Len(t, alertItems3, 1, "should re-alert after cooldown expires")

	_ = graduated2
}

func TestParseDurationOrSeconds(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    time.Duration
		wantOK  bool
	}{
		{name: "Go duration 30s", input: "30s", want: 30 * time.Second, wantOK: true},
		{name: "Go duration 5m", input: "5m", want: 5 * time.Minute, wantOK: true},
		{name: "bare seconds", input: "300", want: 300 * time.Second, wantOK: true},
		{name: "invalid string", input: "not-a-number", want: 0, wantOK: false},
		{name: "empty string", input: "", want: 0, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseDurationOrSeconds(tt.input)
			assert.Equal(t, tt.wantOK, ok)
			if ok {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}
