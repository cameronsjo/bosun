package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cameronsjo/bosun/internal/reconcile"
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
		tmpDir, _ = filepath.EvalSymlinks(tmpDir)

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

		origDir, _ := os.Getwd()
		defer func() { _ = os.Chdir(origDir) }()
		_ = os.Chdir(tmpDir)

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
		tmpDir, _ = filepath.EvalSymlinks(tmpDir)

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

		origDir, _ := os.Getwd()
		defer func() { _ = os.Chdir(origDir) }()
		_ = os.Chdir(tmpDir)

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
		tmpDir, _ = filepath.EvalSymlinks(tmpDir)

		origDir, _ := os.Getwd()
		defer func() { _ = os.Chdir(origDir) }()
		_ = os.Chdir(tmpDir)

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
