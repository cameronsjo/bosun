package daemon

import (
	"encoding/json"
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
