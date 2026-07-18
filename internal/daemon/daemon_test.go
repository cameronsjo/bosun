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

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"

	"github.com/cameronsjo/bosun/internal/alert"
	"github.com/cameronsjo/bosun/internal/docker"
	"github.com/cameronsjo/bosun/internal/docker/dockertest"
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
		{
			name: "valid drift_ignore rules",
			cfg: &Config{
				Port: 8080,
				ReconcileConfig: &reconcile.Config{
					RepoURL: "https://github.com/example/repo",
					DriftIgnore: reconcile.NewConfigField([]reconcile.DriftIgnoreRule{
						{Service: "traefik", Type: "unhealthy"},
					}),
				},
			},
			wantErr: false,
		},
		{
			name: "unknown drift_ignore type fails startup",
			cfg: &Config{
				Port: 8080,
				ReconcileConfig: &reconcile.Config{
					RepoURL: "https://github.com/example/repo",
					DriftIgnore: reconcile.NewConfigField([]reconcile.DriftIgnoreRule{
						{Service: "traefik", Type: "stopped"},
					}),
				},
			},
			wantErr: true,
		},
		{
			name: "invalid drift_ignore glob fails startup",
			cfg: &Config{
				Port: 8080,
				ReconcileConfig: &reconcile.Config{
					RepoURL: "https://github.com/example/repo",
					DriftIgnore: reconcile.NewConfigField([]reconcile.DriftIgnoreRule{
						{Service: "[unclosed", Type: "unhealthy"},
					}),
				},
			},
			wantErr: true,
		},
		{
			name: "total-suppression drift_ignore rule only warns, does not fail startup",
			cfg: &Config{
				Port: 8080,
				ReconcileConfig: &reconcile.Config{
					RepoURL: "https://github.com/example/repo",
					DriftIgnore: reconcile.NewConfigField([]reconcile.DriftIgnoreRule{
						{Service: "*", Type: "*"},
					}),
				},
			},
			wantErr: false,
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
		Subsystems: map[string]SubsystemStatus{
			"docker":          {Status: "healthy"},
			"git":             {Status: "healthy"},
			"reconciler":      {Status: "healthy"},
			"circuit_breaker": {Status: "closed"},
		},
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
	assert.Len(t, decoded.Subsystems, 4)
	assert.Equal(t, "healthy", decoded.Subsystems["docker"].Status)
	assert.Equal(t, "closed", decoded.Subsystems["circuit_breaker"].Status)
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

	t.Run("deploy-sync invariants default to strict", func(t *testing.T) {
		cfg := ConfigFromEnv()

		if cfg.ReconcileConfig.AllowEmptyDeclaredState {
			t.Error("AllowEmptyDeclaredState should default to false")
		}
		if cfg.ReconcileConfig.SkipDeployInvariant {
			t.Error("SkipDeployInvariant should default to false")
		}
	})

	t.Run("BOSUN_ALLOW_EMPTY_DECLARED_STATE=true opts out of declared-state gate", func(t *testing.T) {
		t.Setenv("BOSUN_ALLOW_EMPTY_DECLARED_STATE", "true")

		cfg := ConfigFromEnv()

		if !cfg.ReconcileConfig.AllowEmptyDeclaredState {
			t.Error("AllowEmptyDeclaredState should be true when env var is 'true'")
		}
	})

	t.Run("BOSUN_SKIP_DEPLOY_INVARIANT=true disables mtime invariant", func(t *testing.T) {
		t.Setenv("BOSUN_SKIP_DEPLOY_INVARIANT", "true")

		cfg := ConfigFromEnv()

		if !cfg.ReconcileConfig.SkipDeployInvariant {
			t.Error("SkipDeployInvariant should be true when env var is 'true'")
		}
	})

	t.Run("invariant env vars use strict lowercase 'true' match", func(t *testing.T) {
		cases := []string{"TRUE", "True", "yes", "1", "on", "enabled"}
		for _, v := range cases {
			v := v
			t.Run(v, func(t *testing.T) {
				t.Setenv("BOSUN_ALLOW_EMPTY_DECLARED_STATE", v)
				t.Setenv("BOSUN_SKIP_DEPLOY_INVARIANT", v)
				cfg := ConfigFromEnv()
				if cfg.ReconcileConfig.AllowEmptyDeclaredState {
					t.Errorf("BOSUN_ALLOW_EMPTY_DECLARED_STATE=%q should not enable override (strict %q match)", v, "true")
				}
				if cfg.ReconcileConfig.SkipDeployInvariant {
					t.Errorf("BOSUN_SKIP_DEPLOY_INVARIANT=%q should not enable override (strict %q match)", v, "true")
				}
			})
		}
	})
}

func TestConfigFromEnv_BOSUNTargets(t *testing.T) {
	t.Run("valid BOSUN_TARGETS JSON is parsed and stored", func(t *testing.T) {
		t.Setenv("BOSUN_TARGETS", `[{"name":"unraid","target_host":"user@unraid","project_name":"homelab","remote_appdata_path":"/mnt/user/appdata"}]`)

		cfg := ConfigFromEnv()

		require.Len(t, cfg.ReconcileConfig.Targets, 1)
		assert.Equal(t, "unraid", cfg.ReconcileConfig.Targets[0].Name)
		assert.Equal(t, "homelab", cfg.ReconcileConfig.Targets[0].ProjectName)
		assert.Equal(t, "/mnt/user/appdata", cfg.ReconcileConfig.Targets[0].RemoteAppdataPath)
	})

	t.Run("invalid project_name in one entry is cleared; other fields preserved", func(t *testing.T) {
		t.Setenv("BOSUN_TARGETS", `[{"name":"evil","target_host":"user@host","project_name":"evil; rm -rf /","remote_appdata_path":"/mnt/appdata"}]`)

		cfg := ConfigFromEnv()

		require.Len(t, cfg.ReconcileConfig.Targets, 1)
		assert.Equal(t, "", cfg.ReconcileConfig.Targets[0].ProjectName, "invalid project_name must be cleared")
		assert.Equal(t, "/mnt/appdata", cfg.ReconcileConfig.Targets[0].RemoteAppdataPath, "valid remote_appdata_path must be preserved")
	})

	t.Run("invalid remote_appdata_path is cleared; project_name preserved", func(t *testing.T) {
		t.Setenv("BOSUN_TARGETS", `[{"name":"badpath","target_host":"user@host","project_name":"myproject","remote_appdata_path":"/mnt;rm -rf /"}]`)

		cfg := ConfigFromEnv()

		require.Len(t, cfg.ReconcileConfig.Targets, 1)
		assert.Equal(t, "myproject", cfg.ReconcileConfig.Targets[0].ProjectName, "valid project_name must be preserved")
		assert.Equal(t, "", cfg.ReconcileConfig.Targets[0].RemoteAppdataPath, "invalid remote_appdata_path must be cleared")
	})

	t.Run("multiple entries — only bad fields cleared, clean entries untouched", func(t *testing.T) {
		t.Setenv("BOSUN_TARGETS", `[
			{"name":"clean","target_host":"user@host1","project_name":"goodproject","remote_appdata_path":"/mnt/appdata"},
			{"name":"bad","target_host":"user@host2","project_name":"evil$(id)","remote_appdata_path":"/mnt/appdata"}
		]`)

		cfg := ConfigFromEnv()

		require.Len(t, cfg.ReconcileConfig.Targets, 2)
		assert.Equal(t, "goodproject", cfg.ReconcileConfig.Targets[0].ProjectName, "clean entry must be preserved")
		assert.Equal(t, "", cfg.ReconcileConfig.Targets[1].ProjectName, "invalid entry must be cleared")
	})

	t.Run("malformed JSON is ignored", func(t *testing.T) {
		t.Setenv("BOSUN_TARGETS", `not-valid-json`)

		cfg := ConfigFromEnv()

		assert.Empty(t, cfg.ReconcileConfig.Targets, "malformed JSON must result in empty targets")
	})

	t.Run("empty BOSUN_TARGETS env var is a no-op", func(t *testing.T) {
		// No BOSUN_TARGETS set — targets remain empty.
		cfg := ConfigFromEnv()

		assert.Empty(t, cfg.ReconcileConfig.Targets)
	})

	t.Run("explicit empty array clears targets", func(t *testing.T) {
		t.Setenv("BOSUN_TARGETS", `[]`)

		cfg := ConfigFromEnv()

		assert.Empty(t, cfg.ReconcileConfig.Targets, "explicit empty array must set empty targets slice")
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

		hooks := cfg.ReconcileConfig.PostSyncHooks.Value
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

		hooks := cfg.ReconcileConfig.PostSyncHooks.Value
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

		hooks := cfg.ReconcileConfig.PostSyncHooks.Value
		if len(hooks) != 1 {
			t.Fatalf("PostSyncHooks len = %d, want 1", len(hooks))
		}
		if hooks[0].Container != "nginx" {
			t.Errorf("PostSyncHooks[0].Container = %q, want nginx", hooks[0].Container)
		}
	})
}

func TestConfigFromEnv_HookSettleDelay(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		setEnv   bool
		expected time.Duration
	}{
		{name: "parses Go duration string", envValue: "2s", setEnv: true, expected: 2 * time.Second},
		{name: "parses bare seconds", envValue: "5", setEnv: true, expected: 5 * time.Second},
		{name: "defaults to zero when not set", setEnv: false, expected: 0},
		{name: "invalid value ignored", envValue: "not-a-duration", setEnv: true, expected: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("BOSUN_HOOK_SETTLE_DELAY", "")
			require.NoError(t, os.Unsetenv("BOSUN_HOOK_SETTLE_DELAY"))
			if tt.setEnv {
				t.Setenv("BOSUN_HOOK_SETTLE_DELAY", tt.envValue)
			}

			cfg := ConfigFromEnv()

			assert.Equal(t, tt.expected, cfg.ReconcileConfig.HookSettleDelay.Value)
		})
	}
}

func TestConfigFromEnv_HooksWithDelay(t *testing.T) {
	t.Run("parses hooks JSON with delay field", func(t *testing.T) {
		envHooks := `[{"paths":["traefik/conf.d/**"],"action":"restart","container":"traefik","delay":"5s"}]`
		t.Setenv("BOSUN_POST_SYNC_HOOKS", envHooks)

		cfg := ConfigFromEnv()

		hooks := cfg.ReconcileConfig.PostSyncHooks.Value
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

		hooks := cfg.ReconcileConfig.PostSyncHooks.Value
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

		assert.Equal(t, 5*time.Minute, cfg.DriftAlertDebounce.Value)
	})

	t.Run("parses bare seconds", func(t *testing.T) {
		t.Setenv("BOSUN_DRIFT_ALERT_DEBOUNCE", "300")

		cfg := ConfigFromEnv()

		assert.Equal(t, 300*time.Second, cfg.DriftAlertDebounce.Value)
	})

	t.Run("defaults to 0 when not set", func(t *testing.T) {
		cfg := ConfigFromEnv()

		assert.Equal(t, time.Duration(0), cfg.DriftAlertDebounce.Value)
	})

	t.Run("invalid value keeps default 0", func(t *testing.T) {
		t.Setenv("BOSUN_DRIFT_ALERT_DEBOUNCE", "not-a-duration")

		cfg := ConfigFromEnv()

		assert.Equal(t, time.Duration(0), cfg.DriftAlertDebounce.Value)
	})
}

func TestDefaultConfig_DriftAlertDebounce(t *testing.T) {
	cfg := DefaultConfig()

	assert.Equal(t, time.Duration(0), cfg.DriftAlertDebounce.Value, "DriftAlertDebounce should default to 0 (disabled)")
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

	assert.Equal(t, time.Duration(0), cfg.DriftAlertDebounce.Value,
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
	t.Run("PostSyncHooks source is env when env var present", func(t *testing.T) {
		t.Setenv("BOSUN_POST_SYNC_HOOKS", `[{"paths":["traefik/**"],"action":"restart","container":"traefik"}]`)

		cfg := ConfigFromEnv()
		assert.True(t, cfg.ReconcileConfig.PostSyncHooks.FromEnv())
	})

	t.Run("PostSyncHooks source is not env when env var absent", func(t *testing.T) {
		cfg := ConfigFromEnv()
		assert.False(t, cfg.ReconcileConfig.PostSyncHooks.FromEnv())
	})

	t.Run("HookSettleDelay source is env when env var present", func(t *testing.T) {
		t.Setenv("BOSUN_HOOK_SETTLE_DELAY", "3s")

		cfg := ConfigFromEnv()
		assert.True(t, cfg.ReconcileConfig.HookSettleDelay.FromEnv())
	})

	t.Run("HookSettleDelay source is not env when env var absent", func(t *testing.T) {
		cfg := ConfigFromEnv()
		assert.False(t, cfg.ReconcileConfig.HookSettleDelay.FromEnv())
	})

	t.Run("ConfigReloader is wired", func(t *testing.T) {
		cfg := ConfigFromEnv()
		require.NotNil(t, cfg.ReconcileConfig.ConfigReloader)
	})

	t.Run("CriticalContainersFromEnv set when env var present", func(t *testing.T) {
		t.Setenv("BOSUN_CRITICAL_CONTAINERS", `["traefik","authelia"]`)

		cfg := ConfigFromEnv()
		assert.True(t, cfg.ReconcileConfig.CriticalContainers.FromEnv())
	})

	t.Run("CriticalContainers source is not env when env var absent", func(t *testing.T) {
		cfg := ConfigFromEnv()
		assert.False(t, cfg.ReconcileConfig.CriticalContainers.FromEnv())
	})

	t.Run("DeploySyncPaths source is env when env var present", func(t *testing.T) {
		t.Setenv("BOSUN_DEPLOY_SYNC_PATHS", `["appdata/traefik","compose"]`)

		cfg := ConfigFromEnv()

		if !cfg.ReconcileConfig.DeploySyncPaths.FromEnv() {
			t.Error("DeploySyncPaths.Source should be SourceEnv when BOSUN_DEPLOY_SYNC_PATHS is set")
		}
	})

	t.Run("DeploySyncPaths source is not env when env var absent", func(t *testing.T) {
		cfg := ConfigFromEnv()

		if cfg.ReconcileConfig.DeploySyncPaths.FromEnv() {
			t.Error("DeploySyncPaths.Source should not be SourceEnv when BOSUN_DEPLOY_SYNC_PATHS is not set")
		}
	})

	t.Run("DeploySyncExclude source is env when env var present", func(t *testing.T) {
		t.Setenv("BOSUN_DEPLOY_SYNC_EXCLUDE", `["appdata/legacy"]`)

		cfg := ConfigFromEnv()

		if !cfg.ReconcileConfig.DeploySyncExclude.FromEnv() {
			t.Error("DeploySyncExclude.Source should be SourceEnv when BOSUN_DEPLOY_SYNC_EXCLUDE is set")
		}
	})

	t.Run("DeploySyncExclude source is not env when env var absent", func(t *testing.T) {
		cfg := ConfigFromEnv()

		if cfg.ReconcileConfig.DeploySyncExclude.FromEnv() {
			t.Error("DeploySyncExclude.Source should not be SourceEnv when BOSUN_DEPLOY_SYNC_EXCLUDE is not set")
		}
	})
}

func TestConfigFromEnv_ConfigReloaderCallable(t *testing.T) {
	cfg := ConfigFromEnv()
	require.NotNil(t, cfg.ReconcileConfig.ConfigReloader)

	// Create a temp dir with a minimal bosun.yaml so the reloader can parse it.
	tmpDir := t.TempDir()
	yamlContent := "post_sync_hooks:\n  - paths: [\"traefik/**\"]\n    action: restart\n    container: traefik\nhook_settle_delay: 3s\n"
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "bosun.yaml"), []byte(yamlContent), 0o644))

	reloaded, err := cfg.ReconcileConfig.ConfigReloader(tmpDir)
	require.NoError(t, err)
	require.NotNil(t, reloaded)
	assert.Len(t, reloaded.PostSyncHooks, 1)
	assert.Equal(t, "traefik", reloaded.PostSyncHooks[0].Container)
	require.NotNil(t, reloaded.HookSettleDelay)
	assert.Equal(t, 3*time.Second, *reloaded.HookSettleDelay)
}

func TestConfigFromEnv_CriticalContainers(t *testing.T) {
	t.Run("parses JSON array", func(t *testing.T) {
		t.Setenv("BOSUN_CRITICAL_CONTAINERS", `["traefik","authelia"]`)

		cfg := ConfigFromEnv()

		containers := cfg.ReconcileConfig.CriticalContainers.Value
		if len(containers) != 2 {
			t.Fatalf("expected 2 critical containers, got %d", len(containers))
		}
		if containers[0] != "traefik" || containers[1] != "authelia" {
			t.Errorf("unexpected containers: %v", containers)
		}
	})

	t.Run("invalid JSON ignored", func(t *testing.T) {
		t.Setenv("BOSUN_CRITICAL_CONTAINERS", "not-json")

		cfg := ConfigFromEnv()

		if len(cfg.ReconcileConfig.CriticalContainers.Value) != 0 {
			t.Errorf("expected empty containers, got %v", cfg.ReconcileConfig.CriticalContainers.Value)
		}
	})

	t.Run("empty array", func(t *testing.T) {
		t.Setenv("BOSUN_CRITICAL_CONTAINERS", `[]`)

		cfg := ConfigFromEnv()

		if len(cfg.ReconcileConfig.CriticalContainers.Value) != 0 {
			t.Errorf("expected empty containers, got %v", cfg.ReconcileConfig.CriticalContainers.Value)
		}
	})
}

func TestConfigFromEnv_DeploySyncPaths(t *testing.T) {
	t.Run("parses JSON array", func(t *testing.T) {
		t.Setenv("BOSUN_DEPLOY_SYNC_PATHS", `["appdata/traefik","compose"]`)

		cfg := ConfigFromEnv()

		paths := cfg.ReconcileConfig.DeploySyncPaths.Value
		if len(paths) != 2 {
			t.Fatalf("expected 2 deploy sync paths, got %d", len(paths))
		}
		if paths[0] != "appdata/traefik" || paths[1] != "compose" {
			t.Errorf("unexpected paths: %v", paths)
		}
	})

	t.Run("invalid JSON ignored", func(t *testing.T) {
		t.Setenv("BOSUN_DEPLOY_SYNC_PATHS", "not-json")

		cfg := ConfigFromEnv()

		if len(cfg.ReconcileConfig.DeploySyncPaths.Value) != 0 {
			t.Errorf("expected empty paths, got %v", cfg.ReconcileConfig.DeploySyncPaths.Value)
		}
	})
}

func TestConfigFromEnv_DeploySyncExclude(t *testing.T) {
	t.Run("parses JSON array", func(t *testing.T) {
		t.Setenv("BOSUN_DEPLOY_SYNC_EXCLUDE", `["appdata/legacy"]`)

		cfg := ConfigFromEnv()

		paths := cfg.ReconcileConfig.DeploySyncExclude.Value
		if len(paths) != 1 {
			t.Fatalf("expected 1 deploy sync exclude, got %d", len(paths))
		}
		if paths[0] != "appdata/legacy" {
			t.Errorf("unexpected paths: %v", paths)
		}
	})

	t.Run("invalid JSON ignored", func(t *testing.T) {
		t.Setenv("BOSUN_DEPLOY_SYNC_EXCLUDE", "not-json")

		cfg := ConfigFromEnv()

		if len(cfg.ReconcileConfig.DeploySyncExclude.Value) != 0 {
			t.Errorf("expected empty paths, got %v", cfg.ReconcileConfig.DeploySyncExclude.Value)
		}
	})
}

func TestConfigFromEnv_HealthGateTimeout(t *testing.T) {
	t.Run("parses Go duration string", func(t *testing.T) {
		t.Setenv("BOSUN_HEALTH_GATE_TIMEOUT", "90s")

		cfg := ConfigFromEnv()

		if cfg.ReconcileConfig.HealthGateTimeout != 90*time.Second {
			t.Errorf("HealthGateTimeout = %v, want 90s", cfg.ReconcileConfig.HealthGateTimeout)
		}
	})

	t.Run("parses bare seconds", func(t *testing.T) {
		t.Setenv("BOSUN_HEALTH_GATE_TIMEOUT", "120")

		cfg := ConfigFromEnv()

		if cfg.ReconcileConfig.HealthGateTimeout != 120*time.Second {
			t.Errorf("HealthGateTimeout = %v, want 120s", cfg.ReconcileConfig.HealthGateTimeout)
		}
	})

	t.Run("default 60s when not set", func(t *testing.T) {
		cfg := ConfigFromEnv()

		if cfg.ReconcileConfig.HealthGateTimeout != 60*time.Second {
			t.Errorf("HealthGateTimeout = %v, want 60s (default)", cfg.ReconcileConfig.HealthGateTimeout)
		}
	})

	t.Run("invalid value ignored", func(t *testing.T) {
		t.Setenv("BOSUN_HEALTH_GATE_TIMEOUT", "not-a-duration")

		cfg := ConfigFromEnv()

		// Should keep default (60s from DefaultConfig)
		if cfg.ReconcileConfig.HealthGateTimeout != 60*time.Second {
			t.Errorf("HealthGateTimeout = %v, want 60s (default)", cfg.ReconcileConfig.HealthGateTimeout)
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
		if cfg.ReconcileConfig == nil || !cfg.ReconcileConfig.RemoveOrphans.Value {
			t.Error("ReconcileConfig.RemoveOrphans should be true by default")
		}
	})

	t.Run("disabled with false", func(t *testing.T) {
		t.Setenv("BOSUN_REMOVE_ORPHANS", "false")

		cfg := ConfigFromEnv()

		if cfg.RemoveOrphans {
			t.Error("RemoveOrphans should be false when set to 'false'")
		}
		if cfg.ReconcileConfig != nil && cfg.ReconcileConfig.RemoveOrphans.Value {
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

	t.Run("non-positive value is skipped", func(t *testing.T) {
		t.Setenv("BOSUN_COMPOSE_UP_TIMEOUT", "-5m")
		cfg := ConfigFromEnv()
		require.NotNil(t, cfg.ReconcileConfig)
		assert.Zero(t, cfg.ReconcileConfig.ComposeUpTimeout)
	})
}

func TestConfigFromEnv_BackupTimeout(t *testing.T) {
	// Unlike ComposeUpTimeout, BackupTimeout is set in reconcile.DefaultConfig()
	// (spec: default 5m), so unset/invalid env values fall back to that default
	// rather than zero.
	t.Run("default is DefaultBackupTimeout when unset", func(t *testing.T) {
		cfg := ConfigFromEnv()
		require.NotNil(t, cfg.ReconcileConfig)
		assert.Equal(t, reconcile.DefaultBackupTimeout, cfg.ReconcileConfig.BackupTimeout)
	})

	t.Run("parses Go duration string", func(t *testing.T) {
		t.Setenv("BOSUN_BACKUP_TIMEOUT", "10m")
		cfg := ConfigFromEnv()
		require.NotNil(t, cfg.ReconcileConfig)
		assert.Equal(t, 10*time.Minute, cfg.ReconcileConfig.BackupTimeout)
	})

	t.Run("parses plain seconds", func(t *testing.T) {
		t.Setenv("BOSUN_BACKUP_TIMEOUT", "120")
		cfg := ConfigFromEnv()
		require.NotNil(t, cfg.ReconcileConfig)
		assert.Equal(t, 2*time.Minute, cfg.ReconcileConfig.BackupTimeout)
	})

	t.Run("invalid value falls back to default", func(t *testing.T) {
		t.Setenv("BOSUN_BACKUP_TIMEOUT", "not-a-duration")
		cfg := ConfigFromEnv()
		require.NotNil(t, cfg.ReconcileConfig)
		assert.Equal(t, reconcile.DefaultBackupTimeout, cfg.ReconcileConfig.BackupTimeout)
	})

	t.Run("non-positive value falls back to default", func(t *testing.T) {
		t.Setenv("BOSUN_BACKUP_TIMEOUT", "-5m")
		cfg := ConfigFromEnv()
		require.NotNil(t, cfg.ReconcileConfig)
		assert.Equal(t, reconcile.DefaultBackupTimeout, cfg.ReconcileConfig.BackupTimeout)
	})

	t.Run("zero value falls back to default", func(t *testing.T) {
		t.Setenv("BOSUN_BACKUP_TIMEOUT", "0")
		cfg := ConfigFromEnv()
		require.NotNil(t, cfg.ReconcileConfig)
		assert.Equal(t, reconcile.DefaultBackupTimeout, cfg.ReconcileConfig.BackupTimeout)
	})
}

func TestConfigFromEnv_ReconcileTimeout(t *testing.T) {
	// Unset/invalid/non-positive values must all fall back to the 10m default
	// (DefaultConfig) rather than reach context.WithTimeout as zero — a zero
	// timeout yields an already-expired context and fails every reconcile
	// instantly (#419).
	t.Run("default is 10m when unset", func(t *testing.T) {
		cfg := ConfigFromEnv()
		assert.Equal(t, 10*time.Minute, cfg.ReconcileTimeout)
	})

	t.Run("parses Go duration string", func(t *testing.T) {
		t.Setenv("BOSUN_RECONCILE_TIMEOUT", "20m")
		cfg := ConfigFromEnv()
		assert.Equal(t, 20*time.Minute, cfg.ReconcileTimeout)
	})

	t.Run("parses plain seconds", func(t *testing.T) {
		t.Setenv("BOSUN_RECONCILE_TIMEOUT", "1200")
		cfg := ConfigFromEnv()
		assert.Equal(t, 20*time.Minute, cfg.ReconcileTimeout)
	})

	t.Run("invalid value falls back to default", func(t *testing.T) {
		t.Setenv("BOSUN_RECONCILE_TIMEOUT", "not-a-duration")
		cfg := ConfigFromEnv()
		assert.Equal(t, 10*time.Minute, cfg.ReconcileTimeout)
	})

	t.Run("negative value falls back to default", func(t *testing.T) {
		t.Setenv("BOSUN_RECONCILE_TIMEOUT", "-5m")
		cfg := ConfigFromEnv()
		assert.Equal(t, 10*time.Minute, cfg.ReconcileTimeout)
	})

	t.Run("zero value falls back to default instead of yielding an expired context", func(t *testing.T) {
		t.Setenv("BOSUN_RECONCILE_TIMEOUT", "0")
		cfg := ConfigFromEnv()
		assert.Equal(t, 10*time.Minute, cfg.ReconcileTimeout)
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

	t.Run("negative value retains default", func(t *testing.T) {
		t.Setenv("BOSUN_HEALTH_CHECK_TIMEOUT", "-30s")
		cfg := ConfigFromEnv()
		require.NotNil(t, cfg.ReconcileConfig)
		assert.Equal(t, 60*time.Second, cfg.ReconcileConfig.HealthCheckTimeout)
	})

	t.Run("zero disables health check", func(t *testing.T) {
		t.Setenv("BOSUN_HEALTH_CHECK_TIMEOUT", "0")
		cfg := ConfigFromEnv()
		require.NotNil(t, cfg.ReconcileConfig)
		assert.Zero(t, cfg.ReconcileConfig.HealthCheckTimeout)
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

	t.Run("zero value retains default", func(t *testing.T) {
		t.Setenv("BOSUN_HEALTH_CHECK_INTERVAL", "0")
		cfg := ConfigFromEnv()
		require.NotNil(t, cfg.ReconcileConfig)
		assert.Equal(t, 5*time.Second, cfg.ReconcileConfig.HealthCheckInterval)
	})

	t.Run("negative value retains default", func(t *testing.T) {
		t.Setenv("BOSUN_HEALTH_CHECK_INTERVAL", "-1s")
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

// blockingGitOps is a reconcile.GitOperations stub whose Sync blocks until the
// context is done, simulating a wedged git sync so tests can prove a trigger
// path is bounded by ReconcileTimeout rather than hanging forever.
type blockingGitOps struct{}

func (blockingGitOps) Sync(ctx context.Context) (bool, string, string, error) {
	<-ctx.Done()
	return false, "", "", ctx.Err()
}

func (blockingGitOps) IsRepo(context.Context) bool { return true }

func (blockingGitOps) DiffFiles(context.Context, string, string) ([]string, error) {
	return nil, nil
}

// TestTriggerReconcile_BoundedByReconcileTimeout proves that startup, poll, and
// drift-self-heal triggers -- which call TriggerReconcile with the bare daemon
// context instead of pre-wrapping it like the webhook/socket/tcp/api sites do --
// still get unwedged by ReconcileTimeout rather than blocking d.reconciling forever.
func TestTriggerReconcile_BoundedByReconcileTimeout(t *testing.T) {
	d := newConcurrencyDaemon(t)
	d.config.ReconcileTimeout = 100 * time.Millisecond
	d.reconcileOpts = append(d.reconcileOpts, reconcile.WithGitOperations(blockingGitOps{}))

	ctx := context.Background()
	start := time.Now()
	err := d.TriggerReconcile(ctx, "startup", false)
	elapsed := time.Since(start)

	require.Error(t, err, "a blocked sync should surface a timeout/cancellation error")
	assert.Less(t, elapsed, 2*time.Second, "TriggerReconcile should return once ReconcileTimeout elapses, not hang")

	d.reconcileMu.Lock()
	assert.False(t, d.reconciling, "reconciling should clear after the timeout unwedges the run")
	d.reconcileMu.Unlock()
}

// panicGitOps is a reconcile.GitOperations stub whose Sync panics,
// simulating a defect anywhere in the reconcile pipeline so tests can prove
// the daemon recovers gracefully instead of crashing (#364).
type panicGitOps struct{}

func (panicGitOps) Sync(context.Context) (bool, string, string, error) {
	panic("simulated reconcile panic")
}

func (panicGitOps) IsRepo(context.Context) bool { return true }

func (panicGitOps) DiffFiles(context.Context, string, string) ([]string, error) {
	return nil, nil
}

// TestTriggerReconcile_RecoversFromPanic is the #364 regression: a panic
// anywhere in the reconcile pipeline (originally observed as an unhandled
// panic during Reconciler.Run(), taking the whole daemon process down with
// exit code 2) must be recovered, logged as an ordinary error, and must not
// leave the daemon wedged -- a later trigger must still be able to run and
// succeed, so the circuit breaker keeps governing retries exactly as it
// would after any other reconcile failure.
func TestTriggerReconcile_RecoversFromPanic(t *testing.T) {
	d := newConcurrencyDaemon(t)
	d.reconcileOpts = append(d.reconcileOpts, reconcile.WithGitOperations(panicGitOps{}))

	ctx := context.Background()
	err := d.TriggerReconcile(ctx, "test", false)

	require.Error(t, err, "a recovered panic must surface as an ordinary error, not crash the test process")
	assert.Contains(t, err.Error(), "panic")

	d.reconcileMu.Lock()
	assert.False(t, d.reconciling, "reconciling must clear after a recovered panic, not stay wedged forever")
	assert.False(t, d.pendingTrigger, "pendingTrigger must clear after a recovered panic")
	d.reconcileMu.Unlock()

	_, lastErr := d.LastReconcile()
	require.Error(t, lastErr, "LastReconcile must reflect the panic as the daemon's last error")

	// The daemon must not be permanently wedged: swap in a GitOps stub that
	// succeeds and confirm a subsequent trigger can still run to completion
	// (DryRun with no repo on disk fails at a later pipeline step, but the
	// point is it actually RUNS rather than queuing forever behind a stuck
	// d.reconciling=true).
	d.reconcileOpts = d.reconcileOpts[:len(d.reconcileOpts)-1]
	d.reconcileOpts = append(d.reconcileOpts, reconcile.WithGitOperations(daemonSuccessGitOps{}))
	err = d.TriggerReconcile(ctx, "followup", false)
	_ = err // may still fail for unrelated reasons (no staging dir, etc.) -- reaching here at all is the point

	d.reconcileMu.Lock()
	assert.False(t, d.reconciling, "reconciling should clear after the follow-up trigger completes")
	d.reconcileMu.Unlock()
}

// ---------------------------------------------------------------------------
// Phase 1D: State Accessors
// ---------------------------------------------------------------------------

func TestHealthStatus_Accessors(t *testing.T) {
	t.Run("healthy with all subsystems ok", func(t *testing.T) {
		tmpDir := t.TempDir()
		tmpDir = evalSymlinks(t, tmpDir)

		mockAPI := &dockertest.MockDockerAPI{}
		mockClient := docker.NewClientWithAPI(mockAPI)

		d := &Daemon{
			config: &Config{
				ReconcileConfig: &reconcile.Config{
					RepoURL:   "https://github.com/test/repo",
					StateFile: filepath.Join(tmpDir, "state.json"),
				},
			},
			dockerClientOverride: mockClient,
			stopLoops:            make(chan struct{}),
		}

		status := d.HealthStatus()
		assert.Equal(t, "healthy", status.Status)
		require.NotNil(t, status.Subsystems)
		assert.Equal(t, "healthy", status.Subsystems["docker"].Status)
		assert.Equal(t, "healthy", status.Subsystems["git"].Status)
		assert.Equal(t, "healthy", status.Subsystems["reconciler"].Status)
		assert.Equal(t, "closed", status.Subsystems["circuit_breaker"].Status)
	})

	t.Run("degraded after setting lastError", func(t *testing.T) {
		tmpDir := t.TempDir()
		tmpDir = evalSymlinks(t, tmpDir)

		mockAPI := &dockertest.MockDockerAPI{}
		mockClient := docker.NewClientWithAPI(mockAPI)

		d := &Daemon{
			config: &Config{
				ReconcileConfig: &reconcile.Config{
					RepoURL:   "https://github.com/test/repo",
					StateFile: filepath.Join(tmpDir, "state.json"),
				},
			},
			dockerClientOverride: mockClient,
			lastError:            errors.New("deploy failed"),
			stopLoops:            make(chan struct{}),
		}

		status := d.HealthStatus()
		assert.Equal(t, "degraded", status.Status)
		assert.Equal(t, "deploy failed", status.LastError)
		assert.Equal(t, "degraded", status.Subsystems["reconciler"].Status)
		assert.Equal(t, "deploy failed", status.Subsystems["reconciler"].Message)
	})

	t.Run("unhealthy when docker unavailable", func(t *testing.T) {
		tmpDir := t.TempDir()
		tmpDir = evalSymlinks(t, tmpDir)

		// Simulate docker failure by pre-populating the sync.Once result with an error.
		d := &Daemon{
			config: &Config{
				ReconcileConfig: &reconcile.Config{
					RepoURL:   "https://github.com/test/repo",
					StateFile: filepath.Join(tmpDir, "state.json"),
				},
			},
			dockerErr: fmt.Errorf("docker daemon not reachable"),
			stopLoops: make(chan struct{}),
		}
		// Force sync.Once to be "done" so DockerClient() returns the error.
		d.dockerOnce.Do(func() {})

		status := d.HealthStatus()
		assert.Equal(t, "unhealthy", status.Status)
		assert.Equal(t, "unhealthy", status.Subsystems["docker"].Status)
		assert.NotEmpty(t, status.Subsystems["docker"].Message)
	})

	t.Run("degraded when git repo not configured", func(t *testing.T) {
		tmpDir := t.TempDir()
		tmpDir = evalSymlinks(t, tmpDir)

		mockAPI := &dockertest.MockDockerAPI{}
		mockClient := docker.NewClientWithAPI(mockAPI)

		d := &Daemon{
			config: &Config{
				ReconcileConfig: &reconcile.Config{
					StateFile: filepath.Join(tmpDir, "state.json"),
				},
			},
			dockerClientOverride: mockClient,
			stopLoops:            make(chan struct{}),
		}

		status := d.HealthStatus()
		assert.Equal(t, "degraded", status.Status)
		assert.Equal(t, "unhealthy", status.Subsystems["git"].Status)
		assert.Equal(t, "no repository URL configured", status.Subsystems["git"].Message)
	})

	t.Run("circuit breaker open after max failures", func(t *testing.T) {
		tmpDir := t.TempDir()
		tmpDir = evalSymlinks(t, tmpDir)

		stateFile := filepath.Join(tmpDir, "state.json")
		state := &reconcile.DeployState{
			AttemptCount:        reconcile.MaxAttempts,
			LastAttemptedCommit: "abc123",
		}
		require.NoError(t, reconcile.SaveState(stateFile, state))

		mockAPI := &dockertest.MockDockerAPI{}
		mockClient := docker.NewClientWithAPI(mockAPI)

		d := &Daemon{
			config: &Config{
				ReconcileConfig: &reconcile.Config{
					RepoURL:   "https://github.com/test/repo",
					StateFile: stateFile,
				},
			},
			dockerClientOverride: mockClient,
			stopLoops:            make(chan struct{}),
		}

		status := d.HealthStatus()
		assert.Equal(t, "degraded", status.Status)
		assert.Equal(t, "open", status.Subsystems["circuit_breaker"].Status)
		assert.Equal(t, reconcile.MaxAttempts, status.Subsystems["circuit_breaker"].Failures)
	})

	t.Run("reconciler reports last_run when set", func(t *testing.T) {
		tmpDir := t.TempDir()
		tmpDir = evalSymlinks(t, tmpDir)

		mockAPI := &dockertest.MockDockerAPI{}
		mockClient := docker.NewClientWithAPI(mockAPI)

		d := &Daemon{
			config: &Config{
				ReconcileConfig: &reconcile.Config{
					RepoURL:   "https://github.com/test/repo",
					StateFile: filepath.Join(tmpDir, "state.json"),
				},
			},
			dockerClientOverride: mockClient,
			lastReconcile:        time.Date(2025, 3, 10, 12, 0, 0, 0, time.UTC),
			stopLoops:            make(chan struct{}),
		}

		status := d.HealthStatus()
		assert.Equal(t, "2025-03-10T12:00:00Z", status.Subsystems["reconciler"].LastRun)
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
	// err, when set, is returned by Send instead of recording the alert —
	// simulates a delivery failure (network error, provider outage, etc).
	err error
	// attempts counts every Send call, success or failure, so tests can
	// confirm a delivery was retried without depending on it succeeding.
	attempts int
}

func (p *testAlertProvider) Name() string       { return "test" }
func (p *testAlertProvider) IsConfigured() bool { return true }
func (p *testAlertProvider) Send(_ context.Context, a *alert.Alert) error {
	p.attempts++
	if p.err != nil {
		return p.err
	}
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

		err := d.sendDriftAlert(context.Background(), report)

		require.NoError(t, err)
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

		err := d.sendDriftAlert(context.Background(), report)

		require.NoError(t, err)
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
			_ = d.sendDriftAlert(context.Background(), &reconcile.DriftReport{
				Items: []reconcile.DriftItem{{Service: "x", Type: reconcile.DriftMissing}},
			})
		}, "sendDriftAlert with nil alerter panics because the guard is in the caller")
	})
}

func TestSendDriftResolvedAlert(t *testing.T) {
	t.Run("resolved keys sends alert with target", func(t *testing.T) {
		provider := &testAlertProvider{}
		d := newAlertDaemon(t, provider)

		err := d.sendDriftResolvedAlert(context.Background(), []string{"api:missing", "web:unhealthy"})

		require.NoError(t, err)
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

		err := d.sendDriftResolvedAlert(context.Background(), []string{"api:missing"})

		require.NoError(t, err)
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
			_ = d.sendDriftResolvedAlert(context.Background(), []string{"api:missing"})
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
	d.config.DriftAlertDebounce = reconcile.NewConfigField(time.Duration(0))
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

func TestRunDriftCheck_AlertDeliveryFailure_StateNotAdvanced(t *testing.T) {
	// A declared service with no matching running container produces a
	// "missing" drift item. If alert delivery fails, DriftAlertedItems must
	// NOT be updated -- otherwise the next check would treat the item as
	// already-alerted and never retry delivery.
	provider := &testAlertProvider{err: errors.New("provider unreachable")}
	d := newAlertDaemon(t, provider)
	d.dockerClientOverride = docker.NewClientWithAPI(&dockertest.MockDockerAPI{})
	// Restart breaker is unrelated to this test; disable it explicitly rather
	// than relying on the empty container list to skip its inspect calls.
	d.config.ReconcileConfig.RestartBreakerEnabled = false

	stateFile := d.config.ReconcileConfig.StateFile
	state := &reconcile.DeployState{
		LastDeployedCommit: "abc123",
		DeclaredServices: []reconcile.DeclaredService{
			{Name: "api", Image: "api:latest"},
		},
	}
	require.NoError(t, reconcile.SaveState(stateFile, state))

	d.runDriftCheck(context.Background())

	assert.Equal(t, 1, provider.attempts, "delivery should have been attempted")
	assert.Empty(t, provider.alerts, "delivery failed, no alert should have been recorded as sent")

	loaded := reconcile.LoadState(stateFile)
	assert.Empty(t, loaded.DriftAlertedItems, "DriftAlertedItems must not advance when delivery fails")

	// Retry: once the provider recovers, the next check should alert successfully
	// and only then mark the item as alerted.
	provider.err = nil
	d.runDriftCheck(context.Background())

	assert.Equal(t, 2, provider.attempts, "delivery should be retried on the next check")
	require.Len(t, provider.alerts, 1, "delivery succeeded, one alert should have been sent")

	loaded = reconcile.LoadState(stateFile)
	assert.Contains(t, loaded.DriftAlertedItems, "api:missing", "DriftAlertedItems should advance once delivery succeeds")
}

func TestRunDriftCheck_ResolvedAlertDeliveryFailure_StateNotCleared(t *testing.T) {
	// Simulates drift that was previously alerted and has now resolved (the
	// declared service is running again). If the resolution-alert delivery
	// fails, DriftAlertedItems must remain intact so the resolution alert is
	// retried on the next check instead of being silently dropped.
	provider := &testAlertProvider{err: errors.New("provider unreachable")}
	d := newAlertDaemon(t, provider)

	mockAPI := &dockertest.MockDockerAPI{
		ContainerListFunc: func(_ context.Context, _ client.ContainerListOptions) (client.ContainerListResult, error) {
			return client.ContainerListResult{Items: []container.Summary{
				{
					ID:    "abc123456789",
					Names: []string{"/api"},
					State: "running",
					Image: "api:latest",
				},
			}}, nil
		},
	}
	d.dockerClientOverride = docker.NewClientWithAPI(mockAPI)
	// Restart breaker is unrelated to this test and its inspect path isn't
	// backed by the mock; disable it so it doesn't interfere.
	d.config.ReconcileConfig.RestartBreakerEnabled = false

	stateFile := d.config.ReconcileConfig.StateFile
	state := &reconcile.DeployState{
		LastDeployedCommit: "abc123",
		DeclaredServices: []reconcile.DeclaredService{
			{Name: "api", Image: "api:latest"},
		},
		DriftItems: []reconcile.DriftItem{
			{Service: "api", Type: reconcile.DriftMissing},
		},
		DriftAlertedItems: map[string]time.Time{
			"api:missing": time.Now().Add(-2 * time.Hour),
		},
	}
	require.NoError(t, reconcile.SaveState(stateFile, state))

	d.runDriftCheck(context.Background())

	assert.Equal(t, 1, provider.attempts, "resolution delivery should have been attempted")
	assert.Empty(t, provider.alerts, "delivery failed, no alert should have been recorded as sent")

	loaded := reconcile.LoadState(stateFile)
	assert.Contains(t, loaded.DriftAlertedItems, "api:missing", "DriftAlertedItems must not clear when resolved-alert delivery fails")

	// Retry: once the provider recovers, the resolution alert should succeed
	// and only then clear the alerted state.
	provider.err = nil
	d.runDriftCheck(context.Background())

	assert.Equal(t, 2, provider.attempts, "resolution delivery should be retried on the next check")
	require.Len(t, provider.alerts, 1, "delivery succeeded, one resolution alert should have been sent")

	loaded = reconcile.LoadState(stateFile)
	assert.Empty(t, loaded.DriftAlertedItems, "DriftAlertedItems should clear once resolved-alert delivery succeeds")
}

func TestRunDriftCheck_InDriftResolvedAlertDeliveryFailure_StateNotCleared(t *testing.T) {
	// Two declared services: "api" stays drifting (missing) while "web"
	// resolves (running with the declared image). report.HasDrift() is still
	// true overall (because of "api"), so this exercises the in-drift
	// resolution branch -- a different call site than the no-drift resolution
	// branch covered by TestRunDriftCheck_ResolvedAlertDeliveryFailure_StateNotCleared.
	// If the resolution-alert delivery for "web" fails, its DriftAlertedItems
	// entry must remain intact so the resolution alert is retried.
	provider := &testAlertProvider{err: errors.New("provider unreachable")}
	d := newAlertDaemon(t, provider)

	mockAPI := &dockertest.MockDockerAPI{
		ContainerListFunc: func(_ context.Context, _ client.ContainerListOptions) (client.ContainerListResult, error) {
			return client.ContainerListResult{Items: []container.Summary{
				{
					ID:    "web123456789",
					Names: []string{"/web"},
					State: "running",
					Image: "web:latest",
				},
			}}, nil
		},
	}
	d.dockerClientOverride = docker.NewClientWithAPI(mockAPI)
	d.config.ReconcileConfig.RestartBreakerEnabled = false

	stateFile := d.config.ReconcileConfig.StateFile
	state := &reconcile.DeployState{
		LastDeployedCommit: "abc123",
		DeclaredServices: []reconcile.DeclaredService{
			{Name: "api", Image: "api:latest"},
			{Name: "web", Image: "web:latest"},
		},
		DriftAlertedItems: map[string]time.Time{
			"api:missing": time.Now().Add(-2 * time.Hour),
			"web:missing": time.Now().Add(-2 * time.Hour),
		},
	}
	require.NoError(t, reconcile.SaveState(stateFile, state))

	d.runDriftCheck(context.Background())

	// Both the still-drifting "api" alert and the "web" resolution alert are
	// attempted and both fail (single provider, same error).
	assert.Equal(t, 2, provider.attempts, "both the drift alert and the resolution alert should have been attempted")
	assert.Empty(t, provider.alerts, "delivery failed, no alert should have been recorded as sent")

	loaded := reconcile.LoadState(stateFile)
	assert.Contains(t, loaded.DriftAlertedItems, "web:missing", "DriftAlertedItems must not clear web's entry when its resolved-alert delivery fails, even though api is still drifting")
	assert.Contains(t, loaded.DriftAlertedItems, "api:missing", "api's alerted entry must survive its own failed delivery too")

	// Retry: once the provider recovers, both deliveries succeed -- web's
	// resolution clears its entry, api's (still drifting) alert refreshes its timestamp.
	provider.err = nil
	d.runDriftCheck(context.Background())

	assert.Equal(t, 4, provider.attempts, "both deliveries should be retried on the next check")

	loaded = reconcile.LoadState(stateFile)
	assert.NotContains(t, loaded.DriftAlertedItems, "web:missing", "web's entry should clear once its resolved-alert delivery succeeds")
	assert.Contains(t, loaded.DriftAlertedItems, "api:missing", "api is still drifting, so its entry should remain (refreshed)")
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

func TestConfigFromEnv_RestartBreaker(t *testing.T) {
	t.Run("default enabled with default threshold and window", func(t *testing.T) {
		cfg := ConfigFromEnv()
		require.NotNil(t, cfg.ReconcileConfig)
		assert.True(t, cfg.ReconcileConfig.RestartBreakerEnabled)
		assert.Equal(t, 5, cfg.ReconcileConfig.RestartThreshold)
		assert.Equal(t, 10*time.Minute, cfg.ReconcileConfig.RestartWindow)
	})

	t.Run("BOSUN_RESTART_BREAKER false disables", func(t *testing.T) {
		t.Setenv("BOSUN_RESTART_BREAKER", "false")
		cfg := ConfigFromEnv()
		require.NotNil(t, cfg.ReconcileConfig)
		assert.False(t, cfg.ReconcileConfig.RestartBreakerEnabled)
	})

	t.Run("BOSUN_RESTART_BREAKER 0 disables", func(t *testing.T) {
		t.Setenv("BOSUN_RESTART_BREAKER", "0")
		cfg := ConfigFromEnv()
		require.NotNil(t, cfg.ReconcileConfig)
		assert.False(t, cfg.ReconcileConfig.RestartBreakerEnabled)
	})

	t.Run("BOSUN_RESTART_BREAKER 1 enables", func(t *testing.T) {
		t.Setenv("BOSUN_RESTART_BREAKER", "1")
		cfg := ConfigFromEnv()
		require.NotNil(t, cfg.ReconcileConfig)
		assert.True(t, cfg.ReconcileConfig.RestartBreakerEnabled)
	})

	t.Run("BOSUN_RESTART_BREAKER non-canonical truthy keeps enabled", func(t *testing.T) {
		t.Setenv("BOSUN_RESTART_BREAKER", "yes")
		cfg := ConfigFromEnv()
		require.NotNil(t, cfg.ReconcileConfig)
		assert.True(t, cfg.ReconcileConfig.RestartBreakerEnabled)
	})

	t.Run("BOSUN_RESTART_THRESHOLD valid integer", func(t *testing.T) {
		t.Setenv("BOSUN_RESTART_THRESHOLD", "10")
		cfg := ConfigFromEnv()
		require.NotNil(t, cfg.ReconcileConfig)
		assert.Equal(t, 10, cfg.ReconcileConfig.RestartThreshold)
	})

	t.Run("BOSUN_RESTART_THRESHOLD zero retains default", func(t *testing.T) {
		t.Setenv("BOSUN_RESTART_THRESHOLD", "0")
		cfg := ConfigFromEnv()
		require.NotNil(t, cfg.ReconcileConfig)
		assert.Equal(t, 5, cfg.ReconcileConfig.RestartThreshold)
	})

	t.Run("BOSUN_RESTART_THRESHOLD negative retains default", func(t *testing.T) {
		t.Setenv("BOSUN_RESTART_THRESHOLD", "-3")
		cfg := ConfigFromEnv()
		require.NotNil(t, cfg.ReconcileConfig)
		assert.Equal(t, 5, cfg.ReconcileConfig.RestartThreshold)
	})

	t.Run("BOSUN_RESTART_THRESHOLD invalid retains default", func(t *testing.T) {
		t.Setenv("BOSUN_RESTART_THRESHOLD", "not-a-number")
		cfg := ConfigFromEnv()
		require.NotNil(t, cfg.ReconcileConfig)
		assert.Equal(t, 5, cfg.ReconcileConfig.RestartThreshold)
	})

	t.Run("BOSUN_RESTART_WINDOW parses duration", func(t *testing.T) {
		t.Setenv("BOSUN_RESTART_WINDOW", "30m")
		cfg := ConfigFromEnv()
		require.NotNil(t, cfg.ReconcileConfig)
		assert.Equal(t, 30*time.Minute, cfg.ReconcileConfig.RestartWindow)
	})

	t.Run("BOSUN_RESTART_WINDOW parses plain seconds", func(t *testing.T) {
		t.Setenv("BOSUN_RESTART_WINDOW", "600")
		cfg := ConfigFromEnv()
		require.NotNil(t, cfg.ReconcileConfig)
		assert.Equal(t, 10*time.Minute, cfg.ReconcileConfig.RestartWindow)
	})

	t.Run("BOSUN_RESTART_WINDOW zero retains default", func(t *testing.T) {
		t.Setenv("BOSUN_RESTART_WINDOW", "0")
		cfg := ConfigFromEnv()
		require.NotNil(t, cfg.ReconcileConfig)
		assert.Equal(t, 10*time.Minute, cfg.ReconcileConfig.RestartWindow)
	})

	t.Run("BOSUN_RESTART_WINDOW negative retains default", func(t *testing.T) {
		t.Setenv("BOSUN_RESTART_WINDOW", "-5m")
		cfg := ConfigFromEnv()
		require.NotNil(t, cfg.ReconcileConfig)
		assert.Equal(t, 10*time.Minute, cfg.ReconcileConfig.RestartWindow)
	})

	t.Run("BOSUN_RESTART_WINDOW invalid retains default", func(t *testing.T) {
		t.Setenv("BOSUN_RESTART_WINDOW", "not-a-duration")
		cfg := ConfigFromEnv()
		require.NotNil(t, cfg.ReconcileConfig)
		assert.Equal(t, 10*time.Minute, cfg.ReconcileConfig.RestartWindow)
	})
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

// ---------------------------------------------------------------------------
// Drift Self-Heal
// ---------------------------------------------------------------------------

func TestDefaultConfig_DriftSelfHeal(t *testing.T) {
	cfg := DefaultConfig()

	assert.False(t, cfg.DriftSelfHeal.Value, "DriftSelfHeal should be false by default")
	assert.Equal(t, 15*time.Minute, cfg.DriftSelfHealCooldown.Value, "DriftSelfHealCooldown should default to 15m")
}

func TestConfigFromEnv_DriftSelfHeal(t *testing.T) {
	t.Run("true enables self-heal", func(t *testing.T) {
		t.Setenv("BOSUN_DRIFT_SELF_HEAL", "true")

		cfg := ConfigFromEnv()

		assert.True(t, cfg.DriftSelfHeal.Value)
	})

	t.Run("1 enables self-heal", func(t *testing.T) {
		t.Setenv("BOSUN_DRIFT_SELF_HEAL", "1")

		cfg := ConfigFromEnv()

		assert.True(t, cfg.DriftSelfHeal.Value)
	})

	t.Run("false disables self-heal", func(t *testing.T) {
		t.Setenv("BOSUN_DRIFT_SELF_HEAL", "false")

		cfg := ConfigFromEnv()

		assert.False(t, cfg.DriftSelfHeal.Value)
	})

	t.Run("defaults to false when not set", func(t *testing.T) {
		cfg := ConfigFromEnv()

		assert.False(t, cfg.DriftSelfHeal.Value)
	})
}

func TestConfigFromEnv_DriftSelfHealCooldown(t *testing.T) {
	t.Run("parses Go duration string", func(t *testing.T) {
		t.Setenv("BOSUN_DRIFT_SELF_HEAL_COOLDOWN", "10m")

		cfg := ConfigFromEnv()

		assert.Equal(t, 10*time.Minute, cfg.DriftSelfHealCooldown.Value)
	})

	t.Run("parses bare seconds", func(t *testing.T) {
		t.Setenv("BOSUN_DRIFT_SELF_HEAL_COOLDOWN", "600")

		cfg := ConfigFromEnv()

		assert.Equal(t, 600*time.Second, cfg.DriftSelfHealCooldown.Value)
	})

	t.Run("defaults to 15m when not set", func(t *testing.T) {
		cfg := ConfigFromEnv()

		assert.Equal(t, 15*time.Minute, cfg.DriftSelfHealCooldown.Value)
	})

	t.Run("invalid value keeps default", func(t *testing.T) {
		t.Setenv("BOSUN_DRIFT_SELF_HEAL_COOLDOWN", "not-a-duration")

		cfg := ConfigFromEnv()

		assert.Equal(t, 15*time.Minute, cfg.DriftSelfHealCooldown.Value)
	})

	t.Run("zero or negative keeps default", func(t *testing.T) {
		t.Setenv("BOSUN_DRIFT_SELF_HEAL_COOLDOWN", "0")

		cfg := ConfigFromEnv()

		assert.Equal(t, 15*time.Minute, cfg.DriftSelfHealCooldown.Value)
	})
}

func TestMaybeSelfHeal_TriggersWhenEnabled(t *testing.T) {
	provider := &testAlertProvider{}
	d := newAlertDaemon(t, provider)
	d.config.DriftSelfHeal = reconcile.NewConfigField(true)
	d.config.DriftSelfHealCooldown = reconcile.NewConfigField(15 * time.Minute)

	report := &reconcile.DriftReport{
		CheckedAt: time.Now(),
		Items: []reconcile.DriftItem{
			{Service: "traefik", Type: reconcile.DriftMissing},
		},
	}

	ctx := context.Background()
	d.maybeSelfHeal(ctx, report)

	// Give the goroutine a moment to start.
	time.Sleep(50 * time.Millisecond)

	// Verify lastSelfHeal was updated.
	assert.False(t, d.lastSelfHeal.IsZero(), "lastSelfHeal should be set after triggering")
}

func TestMaybeSelfHeal_SkipsWhenReconciling(t *testing.T) {
	provider := &testAlertProvider{}
	d := newAlertDaemon(t, provider)
	d.config.DriftSelfHeal = reconcile.NewConfigField(true)
	d.config.DriftSelfHealCooldown = reconcile.NewConfigField(15 * time.Minute)

	// Simulate reconciliation in progress.
	d.reconcileMu.Lock()
	d.reconciling = true
	d.reconcileMu.Unlock()

	report := &reconcile.DriftReport{
		CheckedAt: time.Now(),
		Items: []reconcile.DriftItem{
			{Service: "traefik", Type: reconcile.DriftMissing},
		},
	}

	ctx := context.Background()
	d.maybeSelfHeal(ctx, report)

	// lastSelfHeal should NOT be updated when skipped.
	assert.True(t, d.lastSelfHeal.IsZero(), "lastSelfHeal should remain zero when reconciling")

	// Clean up.
	d.reconcileMu.Lock()
	d.reconciling = false
	d.reconcileMu.Unlock()
}

func TestMaybeSelfHeal_RespectsCooldown(t *testing.T) {
	provider := &testAlertProvider{}
	d := newAlertDaemon(t, provider)
	d.config.DriftSelfHeal = reconcile.NewConfigField(true)
	d.config.DriftSelfHealCooldown = reconcile.NewConfigField(15 * time.Minute)

	// Simulate a recent self-heal.
	d.lastSelfHeal = time.Now().Add(-5 * time.Minute) // 5 minutes ago, within 15m cooldown

	report := &reconcile.DriftReport{
		CheckedAt: time.Now(),
		Items: []reconcile.DriftItem{
			{Service: "traefik", Type: reconcile.DriftMissing},
		},
	}

	originalSelfHeal := d.lastSelfHeal
	ctx := context.Background()
	d.maybeSelfHeal(ctx, report)

	// lastSelfHeal should NOT be updated during cooldown.
	assert.Equal(t, originalSelfHeal, d.lastSelfHeal, "lastSelfHeal should not change during cooldown")
}

func TestMaybeSelfHeal_TriggersAfterCooldownExpires(t *testing.T) {
	provider := &testAlertProvider{}
	d := newAlertDaemon(t, provider)
	d.config.DriftSelfHeal = reconcile.NewConfigField(true)
	d.config.DriftSelfHealCooldown = reconcile.NewConfigField(15 * time.Minute)

	// Simulate a self-heal that happened 20 minutes ago (past the 15m cooldown).
	d.lastSelfHeal = time.Now().Add(-20 * time.Minute)

	report := &reconcile.DriftReport{
		CheckedAt: time.Now(),
		Items: []reconcile.DriftItem{
			{Service: "traefik", Type: reconcile.DriftMissing},
		},
	}

	ctx := context.Background()
	d.maybeSelfHeal(ctx, report)

	// Give the goroutine a moment to start.
	time.Sleep(50 * time.Millisecond)

	// lastSelfHeal should be updated since cooldown expired.
	assert.True(t, d.lastSelfHeal.After(time.Now().Add(-1*time.Second)),
		"lastSelfHeal should be updated to roughly now")
}

// ---------------------------------------------------------------------------
// Multi-target: ConfigFromEnv BOSUN_TARGETS parsing
// ---------------------------------------------------------------------------

func TestConfigFromEnv_BOSUN_TARGETS(t *testing.T) {
	t.Run("parses_valid_JSON_array", func(t *testing.T) {
		t.Setenv("BOSUN_TARGETS", `[{"name":"unraid","target_host":"root@192.168.1.8"}]`)

		cfg := ConfigFromEnv()

		targets := cfg.ReconcileConfig.Targets
		require.Len(t, targets, 1)
		assert.Equal(t, "unraid", targets[0].Name)
		assert.Equal(t, "root@192.168.1.8", targets[0].TargetHost)
	})

	t.Run("invalid_JSON_ignored", func(t *testing.T) {
		t.Setenv("BOSUN_TARGETS", "not-json")

		cfg := ConfigFromEnv()

		assert.Empty(t, cfg.ReconcileConfig.Targets)
	})

	t.Run("empty_array_is_explicit_override", func(t *testing.T) {
		tmpDir := t.TempDir()
		tmpDir = evalSymlinks(t, tmpDir)

		yamlContent := `manifest_dir: manifest
targets:
  - name: from-config
    target_host: user@host
`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "bosun.yaml"), []byte(yamlContent), 0o644))
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "manifest"), 0o755))

		origDir, err := os.Getwd()
		require.NoError(t, err)
		defer func() { _ = os.Chdir(origDir) }()
		require.NoError(t, os.Chdir(tmpDir))

		t.Setenv("BOSUN_TARGETS", "[]")

		cfg := ConfigFromEnv()

		// Empty array from env should NOT be repopulated from config file
		assert.Empty(t, cfg.ReconcileConfig.Targets,
			"BOSUN_TARGETS=[] should override config file targets")
	})

	t.Run("env_overrides_config_file", func(t *testing.T) {
		tmpDir := t.TempDir()
		tmpDir = evalSymlinks(t, tmpDir)

		yamlContent := `manifest_dir: manifest
targets:
  - name: from-config
    target_host: user@config-host
`
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "bosun.yaml"), []byte(yamlContent), 0o644))
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "manifest"), 0o755))

		origDir, err := os.Getwd()
		require.NoError(t, err)
		defer func() { _ = os.Chdir(origDir) }()
		require.NoError(t, os.Chdir(tmpDir))

		t.Setenv("BOSUN_TARGETS", `[{"name":"from-env","target_host":"user@env-host"}]`)

		cfg := ConfigFromEnv()

		targets := cfg.ReconcileConfig.Targets
		require.Len(t, targets, 1)
		assert.Equal(t, "from-env", targets[0].Name,
			"env should override config file")
		assert.Equal(t, "user@env-host", targets[0].TargetHost)
	})
}

// ---------------------------------------------------------------------------
// Multi-target: executeReconcile loop behavior
// ---------------------------------------------------------------------------

func TestExecuteReconcile_MultipleTargets(t *testing.T) {
	t.Run("two_targets_both_attempted", func(t *testing.T) {
		d := newConcurrencyDaemon(t)
		d.config.ReconcileConfig.Targets = []reconcile.Target{
			{Name: "alpha", TargetHost: "user@alpha"},
			{Name: "beta", TargetHost: "user@beta"},
		}

		ctx := context.Background()
		err := d.executeReconcile(ctx, "test", false)

		// Both fail (no git repo), but both should be attempted
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "alpha", "error should reference first failing target")
	})

	t.Run("lastReconcile_updated", func(t *testing.T) {
		d := newConcurrencyDaemon(t)
		d.stateMu.Lock()
		before := d.lastReconcile
		d.stateMu.Unlock()

		ctx := context.Background()
		_ = d.executeReconcile(ctx, "test", false)

		d.stateMu.Lock()
		after := d.lastReconcile
		d.stateMu.Unlock()
		assert.True(t, after.After(before), "lastReconcile should be updated")
	})

	t.Run("lastError_set_on_failure", func(t *testing.T) {
		d := newConcurrencyDaemon(t)

		ctx := context.Background()
		_ = d.executeReconcile(ctx, "test", false)

		d.stateMu.Lock()
		lastErr := d.lastError
		d.stateMu.Unlock()
		assert.Error(t, lastErr, "lastError should be set after failed reconcile")
	})

	t.Run("default_single_target", func(t *testing.T) {
		d := newConcurrencyDaemon(t)
		// No explicit targets — implicit default

		ctx := context.Background()
		err := d.executeReconcile(ctx, "test", false)

		// Should still run (and fail at git), not panic or skip
		assert.Error(t, err, "should attempt reconcile even with implicit default target")
	})
}

// --- reloadDaemonConfig tests ---

func TestReloadDaemonConfig_UpdatesFieldsFromBosunYAML(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir = evalSymlinks(t, tmpDir)

	yamlContent := `manifest_dir: manifest
drift_alert_debounce: "5m"
drift_self_heal: true
drift_self_heal_cooldown: "20m"
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "bosun.yaml"), []byte(yamlContent), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "manifest"), 0o755))

	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(origDir) }()
	require.NoError(t, os.Chdir(tmpDir))

	d := &Daemon{
		config: &Config{
			DriftAlertDebounce:    reconcile.NewConfigField(time.Duration(0)),
			DriftSelfHeal:         reconcile.NewConfigField(false),
			DriftSelfHealCooldown: reconcile.NewConfigField(15 * time.Minute),
		},
	}

	d.reloadDaemonConfig()

	d.configMu.RLock()
	defer d.configMu.RUnlock()
	assert.Equal(t, 5*time.Minute, d.config.DriftAlertDebounce.Value)
	assert.True(t, d.config.DriftSelfHeal.Value)
	assert.Equal(t, 20*time.Minute, d.config.DriftSelfHealCooldown.Value)
}

func TestReloadDaemonConfig_EnvVarsPreventsOverride(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir = evalSymlinks(t, tmpDir)

	yamlContent := `manifest_dir: manifest
drift_alert_debounce: "5m"
drift_self_heal: true
drift_self_heal_cooldown: "20m"
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "bosun.yaml"), []byte(yamlContent), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "manifest"), 0o755))

	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(origDir) }()
	require.NoError(t, os.Chdir(tmpDir))

	// All three fields are locked by env vars.
	d := &Daemon{
		config: &Config{
			DriftAlertDebounce:    reconcile.EnvConfigField(2 * time.Minute),
			DriftSelfHeal:         reconcile.EnvConfigField(false),
			DriftSelfHealCooldown: reconcile.EnvConfigField(10 * time.Minute),
		},
	}

	d.reloadDaemonConfig()

	d.configMu.RLock()
	defer d.configMu.RUnlock()
	// Values must stay as set by "env vars" — bosun.yaml must not overwrite them.
	assert.Equal(t, 2*time.Minute, d.config.DriftAlertDebounce.Value, "DriftAlertDebounce must not change when locked by env")
	assert.False(t, d.config.DriftSelfHeal.Value, "DriftSelfHeal must not change when locked by env")
	assert.Equal(t, 10*time.Minute, d.config.DriftSelfHealCooldown.Value, "DriftSelfHealCooldown must not change when locked by env")
}

func TestReloadDaemonConfig_NoConfigFile_IsNoop(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir = evalSymlinks(t, tmpDir)

	origDir, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(origDir) }()
	require.NoError(t, os.Chdir(tmpDir))

	d := &Daemon{
		config: &Config{
			DriftAlertDebounce:    reconcile.NewConfigField(3 * time.Minute),
			DriftSelfHeal:         reconcile.NewConfigField(true),
			DriftSelfHealCooldown: reconcile.NewConfigField(12 * time.Minute),
		},
	}

	// Should not panic or change any values when no bosun.yaml is present.
	d.reloadDaemonConfig()

	d.configMu.RLock()
	defer d.configMu.RUnlock()
	assert.Equal(t, 3*time.Minute, d.config.DriftAlertDebounce.Value)
	assert.True(t, d.config.DriftSelfHeal.Value)
	assert.Equal(t, 12*time.Minute, d.config.DriftSelfHealCooldown.Value)
}

func TestDriftConfig_ReturnsConsistentSnapshot(t *testing.T) {
	d := &Daemon{
		config: &Config{
			DriftAlertDebounce:    reconcile.NewConfigField(7 * time.Minute),
			DriftSelfHeal:         reconcile.NewConfigField(true),
			DriftSelfHealCooldown: reconcile.NewConfigField(25 * time.Minute),
		},
	}

	snap := d.driftConfig()

	assert.Equal(t, 7*time.Minute, snap.driftAlertDebounce)
	assert.True(t, snap.driftSelfHeal)
	assert.Equal(t, 25*time.Minute, snap.driftSelfHealCooldown)
}

func TestConfigFromEnv_DriftFromEnvFlags(t *testing.T) {
	t.Run("DriftAlertDebounceFromEnv set when env var present", func(t *testing.T) {
		t.Setenv("BOSUN_DRIFT_ALERT_DEBOUNCE", "5m")

		cfg := ConfigFromEnv()

		assert.True(t, cfg.DriftAlertDebounce.FromEnv())
	})

	t.Run("DriftAlertDebounceFromEnv false when env var absent", func(t *testing.T) {
		cfg := ConfigFromEnv()

		assert.False(t, cfg.DriftAlertDebounce.FromEnv())
	})

	t.Run("DriftSelfHealFromEnv set when env var present", func(t *testing.T) {
		t.Setenv("BOSUN_DRIFT_SELF_HEAL", "true")

		cfg := ConfigFromEnv()

		assert.True(t, cfg.DriftSelfHeal.FromEnv())
	})

	t.Run("DriftSelfHealFromEnv false when env var absent", func(t *testing.T) {
		cfg := ConfigFromEnv()

		assert.False(t, cfg.DriftSelfHeal.FromEnv())
	})

	t.Run("DriftSelfHealCooldownFromEnv set when env var present", func(t *testing.T) {
		t.Setenv("BOSUN_DRIFT_SELF_HEAL_COOLDOWN", "30m")

		cfg := ConfigFromEnv()

		assert.True(t, cfg.DriftSelfHealCooldown.FromEnv())
	})

	t.Run("DriftSelfHealCooldownFromEnv false when env var absent", func(t *testing.T) {
		cfg := ConfigFromEnv()

		assert.False(t, cfg.DriftSelfHealCooldown.FromEnv())
	})
}
