package daemon

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// oversizedWebhookBody returns a well-formed JSON push for a branch the daemon
// does not track, one byte past MaxWebhookBodySize once the padding is added.
// Truncated at the limit it is invalid JSON, which the generic filter used to
// read as "no ref" and trigger on (#794).
func oversizedWebhookBody() []byte {
	const prefix = `{"ref":"refs/heads/feature","pad":"`
	const suffix = `"}`
	pad := strings.Repeat("a", MaxWebhookBodySize-len(prefix)-len(suffix)+1)
	return []byte(prefix + pad + suffix)
}

func TestReadWebhookBody(t *testing.T) {
	exact := bytes.Repeat([]byte("a"), MaxWebhookBodySize)

	tests := []struct {
		name     string
		body     io.Reader
		nilBody  bool
		wantOK   bool
		wantCode int
		wantLen  int
	}{
		{name: "body at the limit is read in full", body: bytes.NewReader(exact), wantOK: true, wantCode: http.StatusOK, wantLen: MaxWebhookBodySize},
		{name: "body one byte past the limit is refused with 413", body: bytes.NewReader(oversizedWebhookBody()), wantOK: false, wantCode: http.StatusRequestEntityTooLarge},
		{name: "nil body reads as empty", nilBody: true, wantOK: true, wantCode: http.StatusOK, wantLen: 0},
		{name: "unreadable body is refused with 400", body: errReader{}, wantOK: false, wantCode: http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/webhook", tt.body)
			if tt.nilBody {
				req.Body = nil
			}
			w := httptest.NewRecorder()

			body, ok := ReadWebhookBody(w, req)

			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.wantCode, w.Code)
			if tt.wantOK {
				assert.Len(t, body, tt.wantLen)
			} else {
				assert.Nil(t, body, "a refused body must not be returned for a decision")
			}
		})
	}
}

// TestServerWebhooks_OversizedBodyReturns413 pins #794 on every daemon webhook
// endpoint: a body past the limit is refused with 413 and starts no reconcile,
// whatever the auth mode. The old io.LimitReader truncated it silently, so the
// request went on to auth and payload decisions with a partial body.
func TestServerWebhooks_OversizedBodyReturns413(t *testing.T) {
	const secret = "oversize-secret"

	endpoints := []struct {
		name      string
		path      string
		sigHeader string
		label     string
		handler   func(*Server) http.HandlerFunc
	}{
		{name: "generic", path: "/webhook", sigHeader: "X-Signature", label: "generic", handler: func(s *Server) http.HandlerFunc { return s.handleWebhook }},
		{name: "github", path: "/webhook/github", sigHeader: "X-Hub-Signature-256", label: "github", handler: func(s *Server) http.HandlerFunc { return s.handleGitHubWebhook }},
		{name: "manual", path: "/webhook/manual", sigHeader: "X-Signature", label: "manual", handler: func(s *Server) http.HandlerFunc { return s.handleManualTrigger }},
	}
	modes := []struct {
		name  string
		setup func(t *testing.T) (*Daemon, *Server)
		sign  bool
	}{
		{name: "signed", sign: true, setup: func(t *testing.T) (*Daemon, *Server) {
			d, s := newTestDaemon(t)
			d.config.WebhookSecret = secret
			return d, s
		}},
		{name: "unauthenticated opt-out", setup: newUnauthenticatedTestDaemon},
		{name: "fail-closed no secret", setup: newTestDaemon},
	}

	body := oversizedWebhookBody()
	for _, ep := range endpoints {
		for _, mode := range modes {
			t.Run(ep.name+"/"+mode.name, func(t *testing.T) {
				d, s := mode.setup(t)
				d.config.ReconcileConfig.RepoBranch = "main"

				req := httptest.NewRequest(http.MethodPost, ep.path, bytes.NewReader(body))
				if mode.sign {
					req.Header.Set(ep.sigHeader, computeHMACSHA256(body, secret))
				}
				w := httptest.NewRecorder()
				ep.handler(s)(w, req)
				waitForServerTasks(t, s, 5*time.Second)

				require.Equal(t, http.StatusRequestEntityTooLarge, w.Code, "an oversized body must be refused, not truncated")
				assert.Equal(t, 0.0, testutil.ToFloat64(s.metrics.WebhookTriggersTotal.WithLabelValues(ep.label)),
					"an oversized body must not trigger a reconcile")
			})
		}
	}
}
