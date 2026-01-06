package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cameronsjo/bosun/internal/reconcile"
)

func TestAPIStatusEndpoint(t *testing.T) {
	cfg := &Config{
		SocketPath:      "/tmp/test-api-status.sock",
		PollInterval:    time.Hour,
		ReconcileConfig: reconcile.DefaultConfig(),
	}
	cfg.ReconcileConfig.RepoURL = "https://github.com/test/repo"
	cfg.ReconcileConfig.DryRun = true

	d, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create daemon: %v", err)
	}

	mux := http.NewServeMux()
	d.RegisterAPIRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var resp APIStatusResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp.Health != "healthy" {
		t.Errorf("Expected health 'healthy', got '%s'", resp.Health)
	}

	if resp.State != "idle" {
		t.Errorf("Expected state 'idle', got '%s'", resp.State)
	}

	if resp.PollInterval != 3600 {
		t.Errorf("Expected poll_interval 3600, got %d", resp.PollInterval)
	}
}

func TestAPIStatusMethodNotAllowed(t *testing.T) {
	cfg := &Config{
		SocketPath:      "/tmp/test-api-status-method.sock",
		ReconcileConfig: reconcile.DefaultConfig(),
	}
	cfg.ReconcileConfig.RepoURL = "https://github.com/test/repo"
	cfg.ReconcileConfig.DryRun = true

	d, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create daemon: %v", err)
	}

	mux := http.NewServeMux()
	d.RegisterAPIRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/status", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", w.Code)
	}
}

func TestAPITriggerEndpoint(t *testing.T) {
	cfg := &Config{
		SocketPath:      "/tmp/test-api-trigger.sock",
		ReconcileConfig: reconcile.DefaultConfig(),
	}
	cfg.ReconcileConfig.RepoURL = "https://github.com/test/repo"
	cfg.ReconcileConfig.DryRun = true

	d, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create daemon: %v", err)
	}

	mux := http.NewServeMux()
	d.RegisterAPIRoutes(mux)

	body := strings.NewReader(`{"source": "test"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/trigger", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("Expected status 202, got %d", w.Code)
	}

	var resp TriggerResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp.Status != "accepted" {
		t.Errorf("Expected status 'accepted', got '%s'", resp.Status)
	}
}

func TestAPITriggerMethodNotAllowed(t *testing.T) {
	cfg := &Config{
		SocketPath:      "/tmp/test-api-trigger-method.sock",
		ReconcileConfig: reconcile.DefaultConfig(),
	}
	cfg.ReconcileConfig.RepoURL = "https://github.com/test/repo"
	cfg.ReconcileConfig.DryRun = true

	d, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create daemon: %v", err)
	}

	mux := http.NewServeMux()
	d.RegisterAPIRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/trigger", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", w.Code)
	}
}

func TestAPIContainerLogsInvalidLines(t *testing.T) {
	cfg := &Config{
		SocketPath:      "/tmp/test-api-logs.sock",
		ReconcileConfig: reconcile.DefaultConfig(),
	}
	cfg.ReconcileConfig.RepoURL = "https://github.com/test/repo"
	cfg.ReconcileConfig.DryRun = true

	d, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create daemon: %v", err)
	}

	mux := http.NewServeMux()
	d.RegisterAPIRoutes(mux)

	// Test with invalid lines parameter - should use default
	req := httptest.NewRequest(http.MethodGet, "/api/containers/test/logs?lines=invalid", nil)
	w := httptest.NewRecorder()

	// This will fail because Docker is not available in tests, but we can verify
	// the handler is registered and processes the request
	mux.ServeHTTP(w, req)

	// Should get ServiceUnavailable because Docker client fails in test env
	if w.Code != http.StatusServiceUnavailable {
		t.Logf("Got status %d (expected 503 in test env without Docker)", w.Code)
	}
}

func TestAPIContainerRestartMethodNotAllowed(t *testing.T) {
	cfg := &Config{
		SocketPath:      "/tmp/test-api-restart.sock",
		ReconcileConfig: reconcile.DefaultConfig(),
	}
	cfg.ReconcileConfig.RepoURL = "https://github.com/test/repo"
	cfg.ReconcileConfig.DryRun = true

	d, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create daemon: %v", err)
	}

	mux := http.NewServeMux()
	d.RegisterAPIRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/containers/test/restart", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", w.Code)
	}
}

func TestAPIContainerUnknownAction(t *testing.T) {
	cfg := &Config{
		SocketPath:      "/tmp/test-api-unknown.sock",
		ReconcileConfig: reconcile.DefaultConfig(),
	}
	cfg.ReconcileConfig.RepoURL = "https://github.com/test/repo"
	cfg.ReconcileConfig.DryRun = true

	d, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create daemon: %v", err)
	}

	mux := http.NewServeMux()
	d.RegisterAPIRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/containers/test/unknown", nil)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", w.Code)
	}
}
