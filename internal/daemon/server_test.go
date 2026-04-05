package daemon

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cameronsjo/bosun/internal/docker"
	"github.com/cameronsjo/bosun/internal/docker/dockertest"
	"github.com/cameronsjo/bosun/internal/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestDaemon creates a Daemon+Server pair for handler testing.
// HTTP is disabled so New() skips creating its own http.Server;
// we test handler methods directly via httptest.
//
// A t.Cleanup is registered that waits for background goroutines
// spawned by webhook handlers (via Server.wg) so that the temp dir
// is not removed while reconciliation is still in-flight.
func newTestDaemon(t *testing.T) (*Daemon, *Server) {
	t.Helper()
	tmpDir := t.TempDir()
	// macOS: /var -> /private/var symlink resolution.
	tmpDir = evalSymlinks(t, tmpDir)

	cfg := DefaultConfig()
	cfg.EnableHTTP = false
	cfg.ReconcileConfig = reconcile.DefaultConfig()
	cfg.ReconcileConfig.RepoURL = "https://github.com/test/repo"
	cfg.ReconcileConfig.DryRun = true
	cfg.ReconcileConfig.RepoDir = tmpDir
	cfg.ReconcileConfig.LockFile = filepath.Join(tmpDir, "test.lock")
	cfg.ReconcileConfig.StateFile = filepath.Join(tmpDir, "state.json")
	cfg.SocketPath = filepath.Join(tmpDir, "test.sock")

	d, err := New(cfg)
	require.NoError(t, err)

	// Inject mock Docker client so health checks report docker as healthy.
	d.dockerClientOverride = docker.NewClientWithAPI(&dockertest.MockDockerAPI{})

	s := NewServer(d)

	// Wait for background goroutines to finish before temp dir cleanup.
	t.Cleanup(func() { s.wg.Wait() })

	return d, s
}

// computeHMACSHA256 returns "sha256=<hex>" for the given body and secret.
func computeHMACSHA256(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// ---------------------------------------------------------------------------
// Phase 1A: Webhook Signature Validation
// ---------------------------------------------------------------------------

func TestValidateSignature(t *testing.T) {
	const secret = "test-secret"
	body := []byte(`{"action":"push"}`)

	cfg := DefaultConfig()
	cfg.EnableHTTP = false
	cfg.ReconcileConfig = reconcile.DefaultConfig()
	cfg.ReconcileConfig.RepoURL = "https://github.com/test/repo"
	cfg.WebhookSecret = secret

	tmpDir := t.TempDir()
	tmpDir = evalSymlinks(t, tmpDir)
	cfg.SocketPath = filepath.Join(tmpDir, "test.sock")

	d, err := New(cfg)
	require.NoError(t, err)
	s := NewServer(d)

	validSig := computeHMACSHA256(body, secret)

	tests := []struct {
		name      string
		body      []byte
		signature string
		want      bool
	}{
		{
			name:      "valid HMAC-SHA256 with sha256= prefix",
			body:      body,
			signature: validSig,
			want:      true,
		},
		{
			name:      "valid signature without prefix",
			body:      body,
			signature: strings.TrimPrefix(validSig, "sha256="),
			want:      true,
		},
		{
			name:      "empty signature",
			body:      body,
			signature: "",
			want:      false,
		},
		{
			name:      "wrong secret produces different sig",
			body:      body,
			signature: computeHMACSHA256(body, "wrong-secret"),
			want:      false,
		},
		{
			name:      "tampered body",
			body:      []byte(`{"action":"tampered"}`),
			signature: validSig,
			want:      false,
		},
		{
			name:      "empty body with valid signature for empty body",
			body:      []byte{},
			signature: computeHMACSHA256([]byte{}, secret),
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.validateSignature(tt.body, tt.signature)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestValidateGitHubSignature(t *testing.T) {
	const secret = "gh-secret"
	body := []byte(`{"ref":"refs/heads/main"}`)

	cfg := DefaultConfig()
	cfg.EnableHTTP = false
	cfg.ReconcileConfig = reconcile.DefaultConfig()
	cfg.ReconcileConfig.RepoURL = "https://github.com/test/repo"
	cfg.WebhookSecret = secret

	tmpDir := t.TempDir()
	tmpDir = evalSymlinks(t, tmpDir)
	cfg.SocketPath = filepath.Join(tmpDir, "test.sock")

	d, err := New(cfg)
	require.NoError(t, err)
	s := NewServer(d)

	validSig := computeHMACSHA256(body, secret)

	tests := []struct {
		name      string
		body      []byte
		signature string
		want      bool
	}{
		{
			name:      "valid GitHub signature",
			body:      body,
			signature: validSig,
			want:      true,
		},
		{
			name:      "empty signature",
			body:      body,
			signature: "",
			want:      false,
		},
		{
			name:      "tampered body",
			body:      []byte(`{"ref":"refs/heads/evil"}`),
			signature: validSig,
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.validateGitHubSignature(tt.body, tt.signature)
			assert.Equal(t, tt.want, got)
		})
	}
}

// ---------------------------------------------------------------------------
// Phase 1B: HTTP Endpoint Handlers
// ---------------------------------------------------------------------------

func TestHandleHealth(t *testing.T) {
	t.Run("GET returns 200 with healthy JSON and subsystems", func(t *testing.T) {
		_, s := newTestDaemon(t)

		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		w := httptest.NewRecorder()
		s.handleHealth(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Header().Get("Content-Type"), "application/json")

		var status HealthStatus
		require.NoError(t, json.NewDecoder(w.Body).Decode(&status))
		assert.Equal(t, "healthy", status.Status)
		require.NotNil(t, status.Subsystems)
		assert.Equal(t, "healthy", status.Subsystems["docker"].Status)
		assert.Equal(t, "healthy", status.Subsystems["git"].Status)
		assert.Equal(t, "healthy", status.Subsystems["reconciler"].Status)
		assert.Equal(t, "closed", status.Subsystems["circuit_breaker"].Status)
	})

	t.Run("POST returns 405", func(t *testing.T) {
		_, s := newTestDaemon(t)

		req := httptest.NewRequest(http.MethodPost, "/health", nil)
		w := httptest.NewRecorder()
		s.handleHealth(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})

	t.Run("returns 503 when degraded", func(t *testing.T) {
		d, s := newTestDaemon(t)

		d.stateMu.Lock()
		d.lastError = assert.AnError
		d.stateMu.Unlock()

		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		w := httptest.NewRecorder()
		s.handleHealth(w, req)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)

		var status HealthStatus
		require.NoError(t, json.NewDecoder(w.Body).Decode(&status))
		assert.Equal(t, "degraded", status.Status)
		assert.NotEmpty(t, status.LastError)
		assert.Equal(t, "degraded", status.Subsystems["reconciler"].Status)
	})
}

func TestHandleReady(t *testing.T) {
	t.Run("returns 503 when not ready", func(t *testing.T) {
		_, s := newTestDaemon(t)

		req := httptest.NewRequest(http.MethodGet, "/ready", nil)
		w := httptest.NewRecorder()
		s.handleReady(w, req)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})

	t.Run("returns 200 after setReady", func(t *testing.T) {
		d, s := newTestDaemon(t)
		d.setReady(true)

		req := httptest.NewRequest(http.MethodGet, "/ready", nil)
		w := httptest.NewRecorder()
		s.handleReady(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "OK", w.Body.String())
	})

	t.Run("POST returns 405", func(t *testing.T) {
		_, s := newTestDaemon(t)

		req := httptest.NewRequest(http.MethodPost, "/ready", nil)
		w := httptest.NewRecorder()
		s.handleReady(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})
}

func TestHandleWebhook(t *testing.T) {
	t.Run("valid POST returns 202", func(t *testing.T) {
		_, s := newTestDaemon(t)

		req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(`{}`))
		w := httptest.NewRecorder()
		s.handleWebhook(w, req)

		assert.Equal(t, http.StatusAccepted, w.Code)

		var resp map[string]string
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Equal(t, "accepted", resp["status"])
	})

	t.Run("GET returns 405", func(t *testing.T) {
		_, s := newTestDaemon(t)

		req := httptest.NewRequest(http.MethodGet, "/webhook", nil)
		w := httptest.NewRecorder()
		s.handleWebhook(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})

	t.Run("with secret: valid signature returns 202", func(t *testing.T) {
		d, s := newTestDaemon(t)
		d.config.WebhookSecret = "webhook-secret"

		body := []byte(`{"event":"push"}`)
		sig := computeHMACSHA256(body, "webhook-secret")

		req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
		req.Header.Set("X-Signature", sig)
		w := httptest.NewRecorder()
		s.handleWebhook(w, req)

		assert.Equal(t, http.StatusAccepted, w.Code)
	})

	t.Run("with secret: invalid signature returns 401", func(t *testing.T) {
		d, s := newTestDaemon(t)
		d.config.WebhookSecret = "webhook-secret"

		body := []byte(`{"event":"push"}`)

		req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
		req.Header.Set("X-Signature", "sha256=deadbeef")
		w := httptest.NewRecorder()
		s.handleWebhook(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("with secret: empty signature returns 401", func(t *testing.T) {
		d, s := newTestDaemon(t)
		d.config.WebhookSecret = "webhook-secret"

		req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(`{}`))
		w := httptest.NewRecorder()
		s.handleWebhook(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("with secret: signature via X-Hub-Signature-256 header", func(t *testing.T) {
		d, s := newTestDaemon(t)
		d.config.WebhookSecret = "webhook-secret"

		body := []byte(`{"event":"push"}`)
		sig := computeHMACSHA256(body, "webhook-secret")

		req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(string(body)))
		req.Header.Set("X-Hub-Signature-256", sig)
		w := httptest.NewRecorder()
		s.handleWebhook(w, req)

		assert.Equal(t, http.StatusAccepted, w.Code)
	})
}

func TestHandleGitHubWebhook(t *testing.T) {
	t.Run("ping event returns 200 pong", func(t *testing.T) {
		_, s := newTestDaemon(t)

		req := httptest.NewRequest(http.MethodPost, "/webhook/github", strings.NewReader(`{}`))
		req.Header.Set("X-GitHub-Event", "ping")
		w := httptest.NewRecorder()
		s.handleGitHubWebhook(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "pong", w.Body.String())
	})

	t.Run("push to correct branch returns 202", func(t *testing.T) {
		_, s := newTestDaemon(t)

		payload := GitHubPushPayload{
			Ref:   "refs/heads/main",
			After: "abc123def456",
		}
		payload.Pusher.Name = "tester"
		payload.Pusher.Email = "test@example.com"
		body, err := json.Marshal(payload)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/webhook/github", strings.NewReader(string(body)))
		req.Header.Set("X-GitHub-Event", "push")
		w := httptest.NewRecorder()
		s.handleGitHubWebhook(w, req)

		assert.Equal(t, http.StatusAccepted, w.Code)

		var resp map[string]string
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Equal(t, "accepted", resp["status"])
		assert.Equal(t, "abc123def456", resp["commit"])
	})

	t.Run("push to wrong branch returns 200 ignored", func(t *testing.T) {
		_, s := newTestDaemon(t)

		payload := GitHubPushPayload{
			Ref:   "refs/heads/develop",
			After: "abc123",
		}
		body, err := json.Marshal(payload)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/webhook/github", strings.NewReader(string(body)))
		req.Header.Set("X-GitHub-Event", "push")
		w := httptest.NewRecorder()
		s.handleGitHubWebhook(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]string
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Equal(t, "ignored", resp["status"])
		assert.Contains(t, resp["message"], "develop")
	})

	t.Run("non-push event returns 200 ignored", func(t *testing.T) {
		_, s := newTestDaemon(t)

		req := httptest.NewRequest(http.MethodPost, "/webhook/github", strings.NewReader(`{}`))
		req.Header.Set("X-GitHub-Event", "issues")
		w := httptest.NewRecorder()
		s.handleGitHubWebhook(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]string
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Equal(t, "ignored", resp["status"])
		assert.Contains(t, resp["message"], "issues")
	})

	t.Run("invalid JSON returns 400", func(t *testing.T) {
		_, s := newTestDaemon(t)

		req := httptest.NewRequest(http.MethodPost, "/webhook/github", strings.NewReader(`{not json`))
		req.Header.Set("X-GitHub-Event", "push")
		w := httptest.NewRecorder()
		s.handleGitHubWebhook(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("POST with no event header returns ignored", func(t *testing.T) {
		_, s := newTestDaemon(t)

		// No X-GitHub-Event header means eventType == "", which is not "ping"
		// and not "push", so it should return 200 ignored.
		req := httptest.NewRequest(http.MethodPost, "/webhook/github", strings.NewReader(`{}`))
		w := httptest.NewRecorder()
		s.handleGitHubWebhook(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]string
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Equal(t, "ignored", resp["status"])
	})

	t.Run("GET returns 405", func(t *testing.T) {
		_, s := newTestDaemon(t)

		req := httptest.NewRequest(http.MethodGet, "/webhook/github", nil)
		w := httptest.NewRecorder()
		s.handleGitHubWebhook(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})

	t.Run("with secret: valid signature accepted", func(t *testing.T) {
		d, s := newTestDaemon(t)
		d.config.WebhookSecret = "gh-secret"

		payload := GitHubPushPayload{
			Ref:   "refs/heads/main",
			After: "abc123",
		}
		payload.Pusher.Name = "tester"
		body, err := json.Marshal(payload)
		require.NoError(t, err)

		sig := computeHMACSHA256(body, "gh-secret")

		req := httptest.NewRequest(http.MethodPost, "/webhook/github", strings.NewReader(string(body)))
		req.Header.Set("X-GitHub-Event", "push")
		req.Header.Set("X-Hub-Signature-256", sig)
		w := httptest.NewRecorder()
		s.handleGitHubWebhook(w, req)

		assert.Equal(t, http.StatusAccepted, w.Code)
	})

	t.Run("with secret: invalid signature returns 401", func(t *testing.T) {
		d, s := newTestDaemon(t)
		d.config.WebhookSecret = "gh-secret"

		body := []byte(`{"ref":"refs/heads/main"}`)

		req := httptest.NewRequest(http.MethodPost, "/webhook/github", strings.NewReader(string(body)))
		req.Header.Set("X-GitHub-Event", "push")
		req.Header.Set("X-Hub-Signature-256", "sha256=invalid")
		w := httptest.NewRecorder()
		s.handleGitHubWebhook(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("with secret: empty signature returns 401", func(t *testing.T) {
		d, s := newTestDaemon(t)
		d.config.WebhookSecret = "gh-secret"

		req := httptest.NewRequest(http.MethodPost, "/webhook/github", strings.NewReader(`{}`))
		req.Header.Set("X-GitHub-Event", "push")
		w := httptest.NewRecorder()
		s.handleGitHubWebhook(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

func TestHandleManualTrigger(t *testing.T) {
	t.Run("POST returns 202", func(t *testing.T) {
		_, s := newTestDaemon(t)

		req := httptest.NewRequest(http.MethodPost, "/webhook/manual", strings.NewReader(`{}`))
		w := httptest.NewRecorder()
		s.handleManualTrigger(w, req)

		assert.Equal(t, http.StatusAccepted, w.Code)

		var resp map[string]string
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Equal(t, "accepted", resp["status"])
	})

	t.Run("GET returns 405", func(t *testing.T) {
		_, s := newTestDaemon(t)

		req := httptest.NewRequest(http.MethodGet, "/webhook/manual", nil)
		w := httptest.NewRecorder()
		s.handleManualTrigger(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})

	t.Run("with secret: valid signature returns 202", func(t *testing.T) {
		d, s := newTestDaemon(t)
		d.config.WebhookSecret = "manual-secret"

		body := []byte(`{"source":"ci"}`)
		sig := computeHMACSHA256(body, "manual-secret")

		req := httptest.NewRequest(http.MethodPost, "/webhook/manual", strings.NewReader(string(body)))
		req.Header.Set("X-Signature", sig)
		w := httptest.NewRecorder()
		s.handleManualTrigger(w, req)

		assert.Equal(t, http.StatusAccepted, w.Code)
	})

	t.Run("with secret: invalid signature returns 401", func(t *testing.T) {
		d, s := newTestDaemon(t)
		d.config.WebhookSecret = "manual-secret"

		body := []byte(`{"source":"ci"}`)

		req := httptest.NewRequest(http.MethodPost, "/webhook/manual", strings.NewReader(string(body)))
		req.Header.Set("X-Signature", "sha256=wrong")
		w := httptest.NewRecorder()
		s.handleManualTrigger(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("POST with force=true body returns 202", func(t *testing.T) {
		_, s := newTestDaemon(t)

		req := httptest.NewRequest(http.MethodPost, "/webhook/manual", strings.NewReader(`{"force":true}`))
		w := httptest.NewRecorder()
		s.handleManualTrigger(w, req)

		assert.Equal(t, http.StatusAccepted, w.Code)

		var resp map[string]string
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Equal(t, "accepted", resp["status"])
	})

	t.Run("POST with empty body is backward compatible", func(t *testing.T) {
		_, s := newTestDaemon(t)

		req := httptest.NewRequest(http.MethodPost, "/webhook/manual", strings.NewReader(""))
		w := httptest.NewRecorder()
		s.handleManualTrigger(w, req)

		assert.Equal(t, http.StatusAccepted, w.Code)
	})

	t.Run("POST with malformed JSON body degrades gracefully", func(t *testing.T) {
		_, s := newTestDaemon(t)

		req := httptest.NewRequest(http.MethodPost, "/webhook/manual", strings.NewReader(`{not valid json`))
		w := httptest.NewRecorder()
		s.handleManualTrigger(w, req)

		// Malformed body should not block the trigger — force defaults to false.
		assert.Equal(t, http.StatusAccepted, w.Code)

		var resp map[string]string
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Equal(t, "accepted", resp["status"])
	})

	t.Run("with secret: force=true body accepted after valid signature", func(t *testing.T) {
		d, s := newTestDaemon(t)
		d.config.WebhookSecret = "manual-secret"

		body := []byte(`{"force":true}`)
		sig := computeHMACSHA256(body, "manual-secret")

		req := httptest.NewRequest(http.MethodPost, "/webhook/manual", strings.NewReader(string(body)))
		req.Header.Set("X-Signature", sig)
		w := httptest.NewRecorder()
		s.handleManualTrigger(w, req)

		assert.Equal(t, http.StatusAccepted, w.Code)

		var resp map[string]string
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Equal(t, "accepted", resp["status"])
	})

	t.Run("with secret: malformed JSON after valid signature degrades gracefully", func(t *testing.T) {
		d, s := newTestDaemon(t)
		d.config.WebhookSecret = "manual-secret"

		body := []byte(`{not valid json`)
		sig := computeHMACSHA256(body, "manual-secret")

		req := httptest.NewRequest(http.MethodPost, "/webhook/manual", strings.NewReader(string(body)))
		req.Header.Set("X-Signature", sig)
		w := httptest.NewRecorder()
		s.handleManualTrigger(w, req)

		// Signature is valid, body is unreadable — trigger proceeds without force.
		assert.Equal(t, http.StatusAccepted, w.Code)
	})
}

func TestHandleWidget(t *testing.T) {
	t.Run("GET returns 200 JSON", func(t *testing.T) {
		_, s := newTestDaemon(t)

		req := httptest.NewRequest(http.MethodGet, "/api/widget", nil)
		w := httptest.NewRecorder()
		s.handleWidget(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Header().Get("Content-Type"), "application/json")

		var data map[string]any
		require.NoError(t, json.NewDecoder(w.Body).Decode(&data))
		assert.Contains(t, data, "status")
		assert.Contains(t, data, "deploys_total")
		assert.Contains(t, data, "last_deploy")
		assert.Contains(t, data, "git_sha")
	})

	t.Run("POST returns 405", func(t *testing.T) {
		_, s := newTestDaemon(t)

		req := httptest.NewRequest(http.MethodPost, "/api/widget", nil)
		w := httptest.NewRecorder()
		s.handleWidget(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})
}

func TestHandleMetrics(t *testing.T) {
	t.Run("GET returns 200 with prometheus output", func(t *testing.T) {
		_, s := newTestDaemon(t)

		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		w := httptest.NewRecorder()

		handler := s.metricsMiddleware(s.promHandler())
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Header().Get("Content-Type"), "text/plain")

		body := w.Body.String()
		assert.Contains(t, body, "bosun_ready")
		assert.Contains(t, body, "bosun_uptime_seconds")
		assert.Contains(t, body, "# HELP")
		assert.Contains(t, body, "# TYPE")
	})

	t.Run("includes last_reconcile when set", func(t *testing.T) {
		d, s := newTestDaemon(t)

		d.stateMu.Lock()
		d.lastReconcile = startTime.Add(1)
		d.stateMu.Unlock()

		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		w := httptest.NewRecorder()

		handler := s.metricsMiddleware(s.promHandler())
		handler.ServeHTTP(w, req)

		assert.Contains(t, w.Body.String(), "bosun_last_reconcile_timestamp")
	})

	t.Run("includes error metric", func(t *testing.T) {
		_, s := newTestDaemon(t)

		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		w := httptest.NewRecorder()

		handler := s.metricsMiddleware(s.promHandler())
		handler.ServeHTTP(w, req)

		// bosun_reconcile_errors_total is always present as a gauge (value 0 when healthy).
		assert.Contains(t, w.Body.String(), "bosun_reconcile_errors_total")
	})

	t.Run("POST returns 405", func(t *testing.T) {
		_, s := newTestDaemon(t)

		req := httptest.NewRequest(http.MethodPost, "/metrics", nil)
		w := httptest.NewRecorder()

		handler := s.metricsMiddleware(s.promHandler())
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})
}

func TestLoggingMiddleware(t *testing.T) {
	t.Run("adds X-Request-ID to response", func(t *testing.T) {
		_, s := newTestDaemon(t)

		inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		handler := s.loggingMiddleware(inner)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		requestID := w.Header().Get("X-Request-ID")
		assert.NotEmpty(t, requestID, "response should contain X-Request-ID")
	})

	t.Run("preserves existing X-Request-ID", func(t *testing.T) {
		_, s := newTestDaemon(t)

		inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		handler := s.loggingMiddleware(inner)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("X-Request-ID", "existing-id-12345")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		requestID := w.Header().Get("X-Request-ID")
		assert.Equal(t, "existing-id-12345", requestID)
	})
}
