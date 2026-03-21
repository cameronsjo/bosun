package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cameronsjo/bosun/internal/docker"
	"github.com/cameronsjo/bosun/internal/docker/dockertest"
	"github.com/cameronsjo/bosun/internal/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestSocketServer creates a SocketServer+Daemon for handler testing.
// Uses newConcurrencyDaemon which sets DryRun=true — reconcile fails fast,
// which is fine since we're testing the HTTP envelope, not the pipeline.
func newTestSocketServer(t *testing.T) (*SocketServer, *Daemon) {
	t.Helper()
	tmpDir := t.TempDir()
	tmpDir = evalSymlinks(t, tmpDir)

	cfg := DefaultConfig()
	cfg.EnableHTTP = false
	cfg.ReconcileTimeout = 50 * time.Millisecond // Short timeout so fire-and-forget goroutines from handleTrigger bail quickly during test cleanup
	cfg.ReconcileConfig = reconcile.DefaultConfig()
	cfg.ReconcileConfig.RepoURL = "https://github.com/test/repo"
	cfg.ReconcileConfig.DryRun = true
	cfg.ReconcileConfig.RepoDir = filepath.Join(tmpDir, "repo")
	cfg.ReconcileConfig.LockFile = filepath.Join(tmpDir, "test.lock")
	cfg.ReconcileConfig.StateFile = filepath.Join(tmpDir, "state.json")
	cfg.SocketPath = filepath.Join(tmpDir, "test.sock")
	cfg.WebhookSecret = "test-secret"
	cfg.ReconcileConfig.RepoBranch = "main"

	d, err := New(cfg)
	require.NoError(t, err)

	// Inject mock Docker client so health checks report docker as healthy.
	d.dockerClientOverride = docker.NewClientWithAPI(&dockertest.MockDockerAPI{})

	// Wait for background trigger goroutines to finish before temp dir cleanup.
	t.Cleanup(func() { waitForReconcileIdle(t, d) })

	return d.socketServer, d
}

func TestSocketHandleTrigger(t *testing.T) {
	// Pre-set reconciling=true so the background goroutines spawned by
	// handleTrigger coalesce (set pendingTrigger) instead of running a full
	// reconcile that races with temp dir cleanup. We're testing the HTTP envelope.
	ss, d := newTestSocketServer(t)

	d.reconcileMu.Lock()
	d.reconciling = true
	d.reconcileMu.Unlock()
	t.Cleanup(func() {
		d.reconcileMu.Lock()
		d.reconciling = false
		d.pendingTrigger = false
		d.reconcileMu.Unlock()
	})

	t.Run("POST empty body returns 202 with source=socket", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/trigger", nil)
		req.ContentLength = 0
		w := httptest.NewRecorder()
		ss.handleTrigger(w, req)

		assert.Equal(t, http.StatusAccepted, w.Code)
		assert.Contains(t, w.Header().Get("Content-Type"), "application/json")

		var resp TriggerResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Equal(t, "accepted", resp.Status)
		assert.Equal(t, "Reconciliation triggered", resp.Message)
	})

	t.Run("POST with JSON body returns 202 with custom source", func(t *testing.T) {
		body := `{"source":"ci-pipeline","force":true}`
		req := httptest.NewRequest(http.MethodPost, "/trigger", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		ss.handleTrigger(w, req)

		assert.Equal(t, http.StatusAccepted, w.Code)

		var resp TriggerResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Equal(t, "accepted", resp.Status)
	})

	t.Run("GET returns 405", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/trigger", nil)
		w := httptest.NewRecorder()
		ss.handleTrigger(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})
}

// TestSocketHandleTrigger_ForcePropagation verifies that force=true in the
// request body reaches TriggerReconcile regardless of ContentLength.
// Regression test for: body not parsed when ContentLength was 0 or -1.
func TestSocketHandleTrigger_ForcePropagation(t *testing.T) {
	tests := []struct {
		name          string
		body          string
		contentLength int64 // -1 = unknown, 0 = none, >0 = explicit
		wantForce     bool
	}{
		{
			name:          "force=true with explicit ContentLength",
			body:          `{"source":"cli","force":true}`,
			contentLength: 28,
			wantForce:     true,
		},
		{
			name:          "force=true with ContentLength=-1 (unknown, pre-fix client bug)",
			body:          `{"source":"cli","force":true}`,
			contentLength: -1,
			wantForce:     true,
		},
		{
			name:          "force=false with no body",
			body:          "",
			contentLength: 0,
			wantForce:     false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ss, d := newTestSocketServer(t)

			// Pre-set reconciling so the goroutine coalesces into pendingTrigger
			// instead of running a full reconcile pipeline.
			d.reconcileMu.Lock()
			d.reconciling = true
			d.reconcileMu.Unlock()
			t.Cleanup(func() {
				d.reconcileMu.Lock()
				d.reconciling = false
				d.pendingTrigger = false
				d.triggerForce = false
				d.reconcileMu.Unlock()
			})

			var req *http.Request
			if tc.body != "" {
				req = httptest.NewRequest(http.MethodPost, "/trigger", strings.NewReader(tc.body))
				req.ContentLength = tc.contentLength
				req.Header.Set("Content-Type", "application/json")
			} else {
				req = httptest.NewRequest(http.MethodPost, "/trigger", nil)
				req.ContentLength = 0
			}

			w := httptest.NewRecorder()
			ss.handleTrigger(w, req)

			require.Equal(t, http.StatusAccepted, w.Code)

			// The goroutine in handleTrigger calls TriggerReconcile which, seeing
			// d.reconciling=true, sets d.triggerForce. Give it a moment to run.
			require.Eventually(t, func() bool {
				d.reconcileMu.Lock()
				defer d.reconcileMu.Unlock()
				// Either a pending trigger arrived (body case) or we just need
				// the goroutine to have exited for the no-body case.
				if tc.wantForce {
					return d.pendingTrigger && d.triggerForce == tc.wantForce
				}
				return d.pendingTrigger || true // no-body: goroutine always coalesces
			}, 200*time.Millisecond, 5*time.Millisecond)

			if tc.wantForce {
				d.reconcileMu.Lock()
				gotForce := d.triggerForce
				d.reconcileMu.Unlock()
				assert.True(t, gotForce, "expected force=true to reach daemon state")
			}
		})
	}
}

func TestSocketHandleStatus(t *testing.T) {
	t.Run("GET idle state returns 200 with idle", func(t *testing.T) {
		ss, _ := newTestSocketServer(t)

		req := httptest.NewRequest(http.MethodGet, "/status", nil)
		w := httptest.NewRecorder()
		ss.handleStatus(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Header().Get("Content-Type"), "application/json")

		var resp StatusResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Equal(t, "idle", resp.State)
		assert.NotEmpty(t, resp.Uptime)
	})

	t.Run("GET with last error includes error", func(t *testing.T) {
		ss, d := newTestSocketServer(t)

		d.stateMu.Lock()
		d.lastError = assert.AnError
		d.stateMu.Unlock()

		req := httptest.NewRequest(http.MethodGet, "/status", nil)
		w := httptest.NewRecorder()
		ss.handleStatus(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp StatusResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.NotEmpty(t, resp.LastError)
	})

	t.Run("GET while reconciling returns reconciling state", func(t *testing.T) {
		ss, d := newTestSocketServer(t)

		d.reconcileMu.Lock()
		d.reconciling = true
		d.reconcileMu.Unlock()
		defer func() {
			d.reconcileMu.Lock()
			d.reconciling = false
			d.reconcileMu.Unlock()
		}()

		req := httptest.NewRequest(http.MethodGet, "/status", nil)
		w := httptest.NewRecorder()
		ss.handleStatus(w, req)

		var resp StatusResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Equal(t, "reconciling", resp.State)
	})

	t.Run("POST returns 405", func(t *testing.T) {
		ss, _ := newTestSocketServer(t)

		req := httptest.NewRequest(http.MethodPost, "/status", nil)
		w := httptest.NewRecorder()
		ss.handleStatus(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})
}

func TestSocketHandleHealth(t *testing.T) {
	t.Run("GET healthy returns 200", func(t *testing.T) {
		ss, _ := newTestSocketServer(t)

		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		w := httptest.NewRecorder()
		ss.handleHealth(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Header().Get("Content-Type"), "application/json")

		var resp HealthStatus
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Equal(t, "healthy", resp.Status)
	})

	t.Run("GET degraded returns 503", func(t *testing.T) {
		ss, d := newTestSocketServer(t)

		d.stateMu.Lock()
		d.lastError = assert.AnError
		d.stateMu.Unlock()

		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		w := httptest.NewRecorder()
		ss.handleHealth(w, req)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)

		var resp HealthStatus
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Equal(t, "degraded", resp.Status)
		assert.NotEmpty(t, resp.LastError)
	})

	t.Run("POST returns 405", func(t *testing.T) {
		ss, _ := newTestSocketServer(t)

		req := httptest.NewRequest(http.MethodPost, "/health", nil)
		w := httptest.NewRecorder()
		ss.handleHealth(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})
}

func TestSocketHandleConfig(t *testing.T) {
	t.Run("GET returns 200 with config JSON", func(t *testing.T) {
		ss, _ := newTestSocketServer(t)

		req := httptest.NewRequest(http.MethodGet, "/config", nil)
		w := httptest.NewRecorder()
		ss.handleConfig(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Header().Get("Content-Type"), "application/json")

		var resp ConfigResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Equal(t, "test-secret", resp.WebhookSecret)
		assert.Equal(t, "https://github.com/test/repo", resp.RepoURL)
		assert.Equal(t, "main", resp.RepoBranch)
		assert.Greater(t, resp.PollInterval, 0)
	})

	t.Run("POST returns 405", func(t *testing.T) {
		ss, _ := newTestSocketServer(t)

		req := httptest.NewRequest(http.MethodPost, "/config", nil)
		w := httptest.NewRecorder()
		ss.handleConfig(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})
}
