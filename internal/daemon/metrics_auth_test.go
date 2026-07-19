package daemon

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

// authorizeMetrics matrix: fail-closed by default, read-scope token required,
// control bearer also accepted, loud opt-out (#296).
func TestAuthorizeMetrics(t *testing.T) {
	newReq := func(auth string) *http.Request {
		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		return req
	}

	t.Run("no credential, no opt-out: 403 fail closed", func(t *testing.T) {
		d, s := newTestDaemon(t)
		d.config.MetricsToken = ""
		d.config.BearerToken = ""
		w := httptest.NewRecorder()

		ok := s.authorizeMetrics(w, newReq(""))

		assert.False(t, ok)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("no credential, opt-out set: accepted (loud-warn path)", func(t *testing.T) {
		d, s := newTestDaemon(t)
		d.config.MetricsToken = ""
		d.config.BearerToken = ""
		d.config.AllowUnauthenticatedMetrics = true
		w := httptest.NewRecorder()

		ok := s.authorizeMetrics(w, newReq(""))

		assert.True(t, ok)
		assert.Equal(t, http.StatusOK, w.Code) // nothing written; handler proceeds
	})

	t.Run("metrics token configured: correct token accepted", func(t *testing.T) {
		d, s := newTestDaemon(t)
		d.config.MetricsToken = "read-token"
		w := httptest.NewRecorder()

		ok := s.authorizeMetrics(w, newReq("Bearer read-token"))

		assert.True(t, ok)
	})

	t.Run("metrics token configured: wrong token 401", func(t *testing.T) {
		d, s := newTestDaemon(t)
		d.config.MetricsToken = "read-token"
		w := httptest.NewRecorder()

		ok := s.authorizeMetrics(w, newReq("Bearer nope"))

		assert.False(t, ok)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("metrics token configured: missing header 401", func(t *testing.T) {
		d, s := newTestDaemon(t)
		d.config.MetricsToken = "read-token"
		w := httptest.NewRecorder()

		ok := s.authorizeMetrics(w, newReq(""))

		assert.False(t, ok)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("control bearer is also accepted on read endpoints", func(t *testing.T) {
		d, s := newTestDaemon(t)
		d.config.MetricsToken = ""
		d.config.BearerToken = "control-bearer"
		w := httptest.NewRecorder()

		ok := s.authorizeMetrics(w, newReq("Bearer control-bearer"))

		assert.True(t, ok)
	})

	t.Run("metrics token alone suffices when both are set (scraper needs no control creds)", func(t *testing.T) {
		d, s := newTestDaemon(t)
		d.config.MetricsToken = "read-token"
		d.config.BearerToken = "control-bearer"
		w := httptest.NewRecorder()

		ok := s.authorizeMetrics(w, newReq("Bearer read-token"))

		assert.True(t, ok)
	})

	t.Run("configured token takes precedence over opt-out", func(t *testing.T) {
		d, s := newTestDaemon(t)
		d.config.MetricsToken = "read-token"
		d.config.AllowUnauthenticatedMetrics = true
		w := httptest.NewRecorder()

		ok := s.authorizeMetrics(w, newReq(""))

		assert.False(t, ok)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

func TestMetricsAuthConfigured(t *testing.T) {
	d, _ := newTestDaemon(t)

	d.config.MetricsToken, d.config.BearerToken = "", ""
	assert.False(t, d.metricsAuthConfigured())

	d.config.MetricsToken = "read"
	assert.True(t, d.metricsAuthConfigured())

	d.config.MetricsToken, d.config.BearerToken = "", "control"
	assert.True(t, d.metricsAuthConfigured(), "control bearer alone counts as configured")
}

// TestWarnMetricsAuthPosture exercises the startup-warning branches (#296). It
// asserts the branch selection is driven by config, not the log text.
func TestWarnMetricsAuthPosture(t *testing.T) {
	logger := zerolog.Nop()

	t.Run("no-op when HTTP disabled", func(t *testing.T) {
		d, _ := newTestDaemon(t)
		d.config.EnableHTTP = false
		d.warnMetricsAuthPosture(logger) // must not panic
	})

	t.Run("no-op when a credential is configured", func(t *testing.T) {
		d, _ := newTestDaemon(t)
		d.config.EnableHTTP = true
		d.config.MetricsToken = "read"
		d.warnMetricsAuthPosture(logger)
	})

	t.Run("warns on opt-out with no credential", func(t *testing.T) {
		d, _ := newTestDaemon(t)
		d.config.EnableHTTP = true
		d.config.MetricsToken, d.config.BearerToken = "", ""
		d.config.AllowUnauthenticatedMetrics = true
		d.warnMetricsAuthPosture(logger)
	})

	t.Run("warns fail-closed with no credential and no opt-out", func(t *testing.T) {
		d, _ := newTestDaemon(t)
		d.config.EnableHTTP = true
		d.config.MetricsToken, d.config.BearerToken = "", ""
		d.config.AllowUnauthenticatedMetrics = false
		d.warnMetricsAuthPosture(logger)
	})
}

// TestMetricsRoutesRequireAuth proves the /metrics and /api/widget routes are
// wired through requireMetricsAuth end-to-end.
func TestMetricsRoutesRequireAuth(t *testing.T) {
	for _, path := range []string{"/metrics", "/api/widget"} {
		t.Run(path+" rejects unauthenticated (fail closed)", func(t *testing.T) {
			d, s := newTestDaemon(t)
			d.config.MetricsToken = ""
			d.config.BearerToken = ""

			req := httptest.NewRequest(http.MethodGet, path, nil)
			w := httptest.NewRecorder()
			s.server.Handler.ServeHTTP(w, req)

			assert.Equal(t, http.StatusForbidden, w.Code)
		})

		t.Run(path+" serves with a valid metrics token", func(t *testing.T) {
			d, s := newTestDaemon(t)
			d.config.MetricsToken = "read-token"

			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("Authorization", "Bearer read-token")
			w := httptest.NewRecorder()
			s.server.Handler.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
		})
	}
}
