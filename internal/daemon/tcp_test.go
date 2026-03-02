package daemon

import (
	"encoding/json"
	"io"
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

func TestTCPServer_authMiddleware(t *testing.T) {
	// Create a minimal TCP server for testing middleware
	server := &TCPServer{
		bearerToken: "correct-token",
	}

	// Create a test handler
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	})

	handler := server.authMiddleware(testHandler)

	tests := []struct {
		name       string
		path       string
		authHeader string
		wantStatus int
	}{
		{
			name:       "health endpoint is public",
			path:       "/health",
			authHeader: "",
			wantStatus: http.StatusOK,
		},
		{
			name:       "valid bearer token",
			path:       "/trigger",
			authHeader: "Bearer correct-token",
			wantStatus: http.StatusOK,
		},
		{
			name:       "missing auth header",
			path:       "/trigger",
			authHeader: "",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "wrong token",
			path:       "/trigger",
			authHeader: "Bearer wrong-token",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "invalid auth format - no Bearer prefix",
			path:       "/trigger",
			authHeader: "correct-token",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "invalid auth format - Basic instead of Bearer",
			path:       "/status",
			authHeader: "Basic dXNlcjpwYXNz",
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rr.Code, tt.wantStatus)
			}
		})
	}
}

func TestTCPServer_auditMiddleware(t *testing.T) {
	server := &TCPServer{}

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("ok"))
	})

	handler := server.auditMiddleware(testHandler)

	req := httptest.NewRequest("POST", "/trigger", nil)
	req.RemoteAddr = "192.168.1.100:12345"

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// Verify the handler was called
	if rr.Code != http.StatusAccepted {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusAccepted)
	}

	body, _ := io.ReadAll(rr.Body)
	if string(body) != "ok" {
		t.Errorf("body = %q, want 'ok'", string(body))
	}
}

// newTestTCPServer creates a TCPServer+Daemon pair for handler testing.
// Registers a cleanup that waits for any background reconcile goroutines
// spawned by trigger handlers to complete before temp dir removal.
func newTestTCPServer(t *testing.T) (*TCPServer, *Daemon) {
	t.Helper()
	tmpDir := t.TempDir()
	tmpDir = evalSymlinks(t, tmpDir)

	cfg := DefaultConfig()
	cfg.EnableHTTP = false
	cfg.EnableTCP = true
	cfg.BearerToken = "test-token"
	cfg.TCPAddr = ":0"
	cfg.ReconcileConfig = reconcile.DefaultConfig()
	cfg.ReconcileConfig.RepoURL = "https://github.com/test/repo"
	cfg.ReconcileConfig.DryRun = true
	cfg.ReconcileConfig.RepoDir = filepath.Join(tmpDir, "repo")
	cfg.ReconcileConfig.LockFile = filepath.Join(tmpDir, "test.lock")
	cfg.ReconcileConfig.StateFile = filepath.Join(tmpDir, "state.json")
	cfg.SocketPath = filepath.Join(tmpDir, "test.sock")

	d, err := New(cfg)
	require.NoError(t, err)

	// Wait for background trigger goroutines to finish before temp dir cleanup.
	t.Cleanup(func() { waitForReconcileIdle(t, d) })

	return d.tcpServer, d
}

// waitForReconcileIdle spins until the daemon's reconciling flag clears.
// Trigger handlers spawn background goroutines that use temp dir paths;
// we must wait for them to finish before the test's TempDir cleanup runs.
func waitForReconcileIdle(t *testing.T, d *Daemon) {
	t.Helper()
	for i := 0; i < 200; i++ {
		d.reconcileMu.Lock()
		busy := d.reconciling
		d.reconcileMu.Unlock()
		if !busy {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("daemon stayed reconciling during cleanup")
}

func TestTCPHandleTrigger(t *testing.T) {
	// Share one daemon and pre-set reconciling=true so the background goroutines
	// spawned by handleTrigger coalesce (set pendingTrigger) instead of running
	// a full reconcile that touches temp dirs. We're testing the HTTP envelope.
	ts, d := newTestTCPServer(t)

	d.reconcileMu.Lock()
	d.reconciling = true
	d.reconcileMu.Unlock()
	t.Cleanup(func() {
		d.reconcileMu.Lock()
		d.reconciling = false
		d.pendingTrigger = false
		d.reconcileMu.Unlock()
	})

	t.Run("POST empty body returns 202 with tcp source", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/trigger", nil)
		req.ContentLength = 0
		req.RemoteAddr = "192.168.1.10:54321"
		w := httptest.NewRecorder()
		ts.handleTrigger(w, req)

		assert.Equal(t, http.StatusAccepted, w.Code)
		assert.Contains(t, w.Header().Get("Content-Type"), "application/json")

		var resp TriggerResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Equal(t, "accepted", resp.Status)
	})

	t.Run("POST with JSON body returns 202", func(t *testing.T) {
		body := `{"source":"remote-ci","force":true}`
		req := httptest.NewRequest(http.MethodPost, "/trigger", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "10.0.0.1:12345"
		w := httptest.NewRecorder()
		ts.handleTrigger(w, req)

		assert.Equal(t, http.StatusAccepted, w.Code)

		var resp TriggerResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Equal(t, "accepted", resp.Status)
	})

	t.Run("GET returns 405", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/trigger", nil)
		w := httptest.NewRecorder()
		ts.handleTrigger(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})
}

func TestTCPHandleStatus(t *testing.T) {
	t.Run("GET idle returns 200", func(t *testing.T) {
		ts, _ := newTestTCPServer(t)

		req := httptest.NewRequest(http.MethodGet, "/status", nil)
		w := httptest.NewRecorder()
		ts.handleStatus(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Header().Get("Content-Type"), "application/json")

		var resp StatusResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Equal(t, "idle", resp.State)
		assert.NotEmpty(t, resp.Uptime)
	})

	t.Run("GET with error includes error", func(t *testing.T) {
		ts, d := newTestTCPServer(t)

		d.stateMu.Lock()
		d.lastError = assert.AnError
		d.stateMu.Unlock()

		req := httptest.NewRequest(http.MethodGet, "/status", nil)
		w := httptest.NewRecorder()
		ts.handleStatus(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp StatusResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.NotEmpty(t, resp.LastError)
	})

	t.Run("POST returns 405", func(t *testing.T) {
		ts, _ := newTestTCPServer(t)

		req := httptest.NewRequest(http.MethodPost, "/status", nil)
		w := httptest.NewRecorder()
		ts.handleStatus(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})
}

func TestTCPHandleHealth(t *testing.T) {
	t.Run("GET healthy returns 200", func(t *testing.T) {
		ts, _ := newTestTCPServer(t)

		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		w := httptest.NewRecorder()
		ts.handleHealth(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Header().Get("Content-Type"), "application/json")

		var resp HealthStatus
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Equal(t, "healthy", resp.Status)
	})

	t.Run("GET degraded returns 503", func(t *testing.T) {
		ts, d := newTestTCPServer(t)

		d.stateMu.Lock()
		d.lastError = assert.AnError
		d.stateMu.Unlock()

		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		w := httptest.NewRecorder()
		ts.handleHealth(w, req)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)

		var resp HealthStatus
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Equal(t, "degraded", resp.Status)
	})

	t.Run("POST returns 405", func(t *testing.T) {
		ts, _ := newTestTCPServer(t)

		req := httptest.NewRequest(http.MethodPost, "/health", nil)
		w := httptest.NewRecorder()
		ts.handleHealth(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})
}

func TestResponseWriter(t *testing.T) {
	t.Run("captures status code", func(t *testing.T) {
		rr := httptest.NewRecorder()
		w := &responseWriter{ResponseWriter: rr, statusCode: http.StatusOK}

		w.WriteHeader(http.StatusCreated)

		if w.statusCode != http.StatusCreated {
			t.Errorf("statusCode = %d, want %d", w.statusCode, http.StatusCreated)
		}
	})

	t.Run("default status code is 200", func(t *testing.T) {
		rr := httptest.NewRecorder()
		w := &responseWriter{ResponseWriter: rr, statusCode: http.StatusOK}

		// Write body without calling WriteHeader
		_, _ = w.Write([]byte("test"))

		if w.statusCode != http.StatusOK {
			t.Errorf("statusCode = %d, want %d", w.statusCode, http.StatusOK)
		}
	})
}
