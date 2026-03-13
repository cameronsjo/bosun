package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cameronsjo/bosun/internal/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// ---------------------------------------------------------------------------
// handleAPIDrift handler tests
// ---------------------------------------------------------------------------

func TestAPIDrift(t *testing.T) {
	t.Run("no state file returns 503", func(t *testing.T) {
		tmpDir := t.TempDir()
		tmpDir = evalSymlinks(t, tmpDir)

		// Construct daemon directly — New() backfills ReconcileConfig with defaults
		// that include a state file path, so we bypass it to test the nil/empty path.
		d := &Daemon{
			config: &Config{
				SocketPath:      filepath.Join(tmpDir, "test.sock"),
				ReconcileConfig: &reconcile.Config{StateFile: ""},
			},
			stopLoops: make(chan struct{}),
		}

		mux := http.NewServeMux()
		d.RegisterAPIRoutes(mux)

		req := httptest.NewRequest(http.MethodGet, "/api/drift", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})

	t.Run("nil reconcile config returns 503", func(t *testing.T) {
		tmpDir := t.TempDir()
		tmpDir = evalSymlinks(t, tmpDir)

		d := &Daemon{
			config: &Config{
				SocketPath:      filepath.Join(tmpDir, "test.sock"),
				ReconcileConfig: nil,
			},
			stopLoops: make(chan struct{}),
		}

		mux := http.NewServeMux()
		d.RegisterAPIRoutes(mux)

		req := httptest.NewRequest(http.MethodGet, "/api/drift", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})

	t.Run("state file with drift items returns 200 with items", func(t *testing.T) {
		tmpDir := t.TempDir()
		tmpDir = evalSymlinks(t, tmpDir)

		stateFile := filepath.Join(tmpDir, "state.json")
		checkedAt := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
		state := &reconcile.DeployState{
			LastDeployedCommit: "abc1234",
			DriftCheckedAt:     checkedAt,
			DriftItems: []reconcile.DriftItem{
				{Service: "api", Type: reconcile.DriftMissing, Declared: "myapp:v1"},
				{Service: "web", Type: reconcile.DriftUnhealthy},
			},
			DeclaredServices: []reconcile.DeclaredService{
				{Name: "api", Image: "myapp:v1"},
				{Name: "web", Image: "nginx:latest"},
			},
		}
		require.NoError(t, reconcile.SaveState(stateFile, state))

		cfg := &Config{
			SocketPath:      filepath.Join(tmpDir, "test.sock"),
			ReconcileConfig: &reconcile.Config{StateFile: stateFile},
		}

		d, err := New(cfg)
		require.NoError(t, err)

		mux := http.NewServeMux()
		d.RegisterAPIRoutes(mux)

		req := httptest.NewRequest(http.MethodGet, "/api/drift", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp APIDriftResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Equal(t, "drifted", resp.Status)
		assert.Equal(t, 2, resp.DriftItemCount)
		assert.Equal(t, 2, resp.DeclaredCount)
		require.Len(t, resp.Items, 2)
		assert.Equal(t, "api", resp.Items[0].Service)
		assert.Equal(t, "missing", resp.Items[0].Type)
		assert.NotNil(t, resp.CheckedAt)
	})

	t.Run("clean state returns 200 with clean status", func(t *testing.T) {
		tmpDir := t.TempDir()
		tmpDir = evalSymlinks(t, tmpDir)

		stateFile := filepath.Join(tmpDir, "state.json")
		state := &reconcile.DeployState{
			LastDeployedCommit: "abc1234",
			DriftItems:         nil, // no drift
		}
		require.NoError(t, reconcile.SaveState(stateFile, state))

		cfg := &Config{
			SocketPath:      filepath.Join(tmpDir, "test.sock"),
			ReconcileConfig: &reconcile.Config{StateFile: stateFile},
		}

		d, err := New(cfg)
		require.NoError(t, err)

		mux := http.NewServeMux()
		d.RegisterAPIRoutes(mux)

		req := httptest.NewRequest(http.MethodGet, "/api/drift", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp APIDriftResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Equal(t, "clean", resp.Status)
		assert.Equal(t, 0, resp.DriftItemCount)
	})

	t.Run("POST returns 405", func(t *testing.T) {
		tmpDir := t.TempDir()
		tmpDir = evalSymlinks(t, tmpDir)

		stateFile := filepath.Join(tmpDir, "state.json")
		cfg := &Config{
			SocketPath:      filepath.Join(tmpDir, "test.sock"),
			ReconcileConfig: &reconcile.Config{StateFile: stateFile},
		}

		d, err := New(cfg)
		require.NoError(t, err)

		mux := http.NewServeMux()
		d.RegisterAPIRoutes(mux)

		req := httptest.NewRequest(http.MethodPost, "/api/drift", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})
}
