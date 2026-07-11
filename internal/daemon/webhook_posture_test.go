package daemon

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWarnWebhookAuthPosture(t *testing.T) {
	posture := func(t *testing.T, mutate func(*Config)) string {
		t.Helper()
		d, _ := newTestDaemon(t)
		d.config.EnableHTTP = true
		mutate(d.config)
		var buf bytes.Buffer
		d.warnWebhookAuthPosture(zerolog.New(&buf))
		return buf.String()
	}

	t.Run("fail-closed posture warns loudly", func(t *testing.T) {
		out := posture(t, func(*Config) {})
		assert.Contains(t, out, "REJECT all trigger requests")
		assert.Contains(t, out, "BOSUN_ALLOW_UNAUTHENTICATED_WEBHOOK")
	})

	t.Run("opt-out posture names the exposure", func(t *testing.T) {
		out := posture(t, func(c *Config) { c.AllowUnauthenticatedWebhook = true })
		assert.Contains(t, out, "UNAUTHENTICATED trigger requests")
	})

	t.Run("configured secret is silent", func(t *testing.T) {
		out := posture(t, func(c *Config) { c.WebhookSecret = "s3cret" })
		assert.Empty(t, out)
	})

	t.Run("disabled HTTP is silent", func(t *testing.T) {
		out := posture(t, func(c *Config) { c.EnableHTTP = false })
		assert.Empty(t, out)
	})
}

// errReader forces the handlers' body-read error branches.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, fmt.Errorf("simulated body read failure") }

func TestHandlers_BodyReadFailureReturns400(t *testing.T) {
	cases := []struct {
		name    string
		path    string
		handler func(*Server) http.HandlerFunc
	}{
		{"generic webhook", "/webhook", func(s *Server) http.HandlerFunc { return s.handleWebhook }},
		{"github webhook", "/webhook/github", func(s *Server) http.HandlerFunc { return s.handleGitHubWebhook }},
		{"manual trigger", "/webhook/manual", func(s *Server) http.HandlerFunc { return s.handleManualTrigger }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, s := newUnauthenticatedTestDaemon(t)

			req := httptest.NewRequest(http.MethodPost, tc.path, errReader{})
			w := httptest.NewRecorder()
			tc.handler(s)(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code, "an unreadable body must fail with 400, not trigger a reconcile")
		})
	}
}

func TestServerStartBindsAndShutsDown(t *testing.T) {
	_, s := newTestDaemon(t)

	errCh := make(chan error, 1)
	go func() { errCh <- s.Start(0) }() // port 0: kernel-assigned, no collisions

	// Give ListenAndServe a moment to bind; Shutdown is safe in either order —
	// a pre-bind Shutdown still makes ListenAndServe return ErrServerClosed.
	time.Sleep(100 * time.Millisecond)
	require.NoError(t, s.Shutdown(context.Background()))

	select {
	case err := <-errCh:
		assert.ErrorIs(t, err, http.ErrServerClosed)
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after Shutdown")
	}
	assert.Equal(t, ":0", s.server.Addr, "default bind must stay all-interfaces")
}
