package daemon

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebhookSecretConfigured(t *testing.T) {
	tests := []struct {
		name   string
		secret string
		want   bool
	}{
		{name: "real secret", secret: "3f9a1c0e", want: true},
		{name: "empty", secret: ""},
		{name: "whitespace only", secret: " \t\n"},
		{name: "placeholder", secret: "change-me"},
		{name: "placeholder in another case", secret: "Change-Me"},
		{name: "placeholder with surrounding whitespace", secret: "  CHANGE-ME\n"},
		{name: "init template example uncommented as written", secret: InitTemplateWebhookSecretPlaceholder},
		{name: "init template example in another case and padded", secret: " <OUTPUT OF: OPENSSL RAND -HEX 32> "},
		{name: "secret that merely contains the placeholder", secret: "change-me-please-2a7f", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, WebhookSecretConfigured(tt.secret))
		})
	}
}

func TestResolveWebhookSecret(t *testing.T) {
	tests := []struct {
		name         string
		primary      string
		github       string
		wantSecret   string
		wantConflict bool
	}{
		{name: "none set"},
		{name: "only WEBHOOK_SECRET", primary: "p", wantSecret: "p"},
		{name: "only GITHUB_WEBHOOK_SECRET", github: "g", wantSecret: "g"},
		{name: "both set to the same value", primary: "same", github: "same", wantSecret: "same"},
		{name: "both set to different values fail closed", primary: "p", github: "g", wantConflict: true},
		{name: "placeholder WEBHOOK_SECRET defers to GITHUB_WEBHOOK_SECRET", primary: PlaceholderWebhookSecret, github: "g", wantSecret: "g"},
		{name: "placeholder GITHUB_WEBHOOK_SECRET is ignored", primary: "p", github: PlaceholderWebhookSecret, wantSecret: "p"},
		{name: "placeholder alone is no secret", primary: PlaceholderWebhookSecret},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := map[string]string{"WEBHOOK_SECRET": tt.primary, "GITHUB_WEBHOOK_SECRET": tt.github}

			secret, conflict := ResolveWebhookSecret(func(k string) string { return env[k] })

			assert.Equal(t, tt.wantSecret, secret)
			assert.Equal(t, tt.wantConflict, conflict)
		})
	}
}

func TestGenericWebhookSignature(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string]string
		want    string
	}{
		{name: "X-Signature", headers: map[string]string{"X-Signature": "a"}, want: "a"},
		{name: "X-Hub-Signature-256", headers: map[string]string{"X-Hub-Signature-256": "b"}, want: "b"},
		{name: "X-Signature-256", headers: map[string]string{"X-Signature-256": "c"}, want: "c"},
		{name: "first present header wins", headers: map[string]string{"X-Signature": "a", "X-Hub-Signature-256": "b", "X-Signature-256": "c"}, want: "a"},
		{name: "none set", headers: map[string]string{"X-Gitea-Signature": "d"}, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := http.Header{}
			for k, v := range tt.headers {
				h.Set(k, v)
			}
			assert.Equal(t, tt.want, GenericWebhookSignature(h))
		})
	}
}

// authParityEndpoints are the daemon's three trigger endpoints, each with its
// own signature header and a request that, once authorized, gets a 2xx
// without starting work that outlives the test: an empty generic and manual
// body, and a GitHub ping.
var authParityEndpoints = []struct {
	name      string
	path      string
	sigHeader string
	event     string
	label     string
	okCode    int
	handler   func(*Server) http.HandlerFunc
}{
	{name: "generic", path: "/webhook", sigHeader: "X-Signature", label: "generic", okCode: http.StatusAccepted, handler: func(s *Server) http.HandlerFunc { return s.handleWebhook }},
	{name: "github", path: "/webhook/github", sigHeader: "X-Hub-Signature-256", event: "ping", label: "github", okCode: http.StatusOK, handler: func(s *Server) http.HandlerFunc { return s.handleGitHubWebhook }},
	{name: "manual", path: "/webhook/manual", sigHeader: "X-Signature", label: "manual", okCode: http.StatusAccepted, handler: func(s *Server) http.HandlerFunc { return s.handleManualTrigger }},
}

func signedEmptyTrigger(path, header, event, secret string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, path, nil)
	req.Header.Set(header, computeHMACSHA256(nil, secret))
	if event != "" {
		req.Header.Set("X-GitHub-Event", event)
	}
	return req
}

// TestServerWebhooks_SecretConflictFailsClosed: with WEBHOOK_SECRET and
// GITHUB_WEBHOOK_SECRET set to different values, a request signed with
// either one is refused with 403 on every endpoint, and the unauthenticated
// opt-out does not reopen it (#794). The config comes from ConfigFromEnv so
// the env resolution is exercised, not hand-built.
func TestServerWebhooks_SecretConflictFailsClosed(t *testing.T) {
	const primary, github = "generic-secret", "github-secret"

	for _, ep := range authParityEndpoints {
		for _, signWith := range []string{primary, github} {
			for _, optOut := range []bool{false, true} {
				name := ep.name + "/signed with " + signWith
				if optOut {
					name += "/opt-out set"
				}
				t.Run(name, func(t *testing.T) {
					t.Setenv("WEBHOOK_SECRET", primary)
					t.Setenv("GITHUB_WEBHOOK_SECRET", github)
					envCfg := ConfigFromEnv()

					d, s := newTestDaemon(t)
					d.config.WebhookSecret = envCfg.WebhookSecret
					d.config.WebhookSecretConflict = envCfg.WebhookSecretConflict
					d.config.AllowUnauthenticatedWebhook = optOut

					w := httptest.NewRecorder()
					ep.handler(s)(w, signedEmptyTrigger(ep.path, ep.sigHeader, ep.event, signWith))
					waitForServerTasks(t, s, 5*time.Second)

					require.Equal(t, http.StatusForbidden, w.Code, "conflicting secrets must authenticate nothing")
					assert.Equal(t, 0.0, testutil.ToFloat64(s.metrics.WebhookTriggersTotal.WithLabelValues(ep.label)))
				})
			}
		}
	}
}

// TestServerWebhooks_PlaceholderSecretFailsClosed: the public "change-me"
// default authenticates nothing. A request signed with it gets the same 403
// as a daemon with no secret (#794).
func TestServerWebhooks_PlaceholderSecretFailsClosed(t *testing.T) {
	for _, ep := range authParityEndpoints {
		t.Run(ep.name, func(t *testing.T) {
			d, s := newTestDaemon(t)
			d.config.WebhookSecret = PlaceholderWebhookSecret

			w := httptest.NewRecorder()
			ep.handler(s)(w, signedEmptyTrigger(ep.path, ep.sigHeader, ep.event, PlaceholderWebhookSecret))
			waitForServerTasks(t, s, 5*time.Second)

			require.Equal(t, http.StatusForbidden, w.Code)
			assert.Equal(t, 0.0, testutil.ToFloat64(s.metrics.WebhookTriggersTotal.WithLabelValues(ep.label)))
		})
	}

	for _, variant := range []string{" Change-Me\n", InitTemplateWebhookSecretPlaceholder} {
		t.Run("variant "+strconv.Quote(variant), func(t *testing.T) {
			d, s := newTestDaemon(t)
			d.config.WebhookSecret = variant

			w := httptest.NewRecorder()
			s.handleWebhook(w, signedEmptyTrigger("/webhook", "X-Signature", "", variant))
			waitForServerTasks(t, s, 5*time.Second)

			assert.Equal(t, http.StatusForbidden, w.Code)
		})
	}

	t.Run("placeholder with the opt-out is an unauthenticated daemon", func(t *testing.T) {
		d, s := newTestDaemon(t)
		d.config.WebhookSecret = PlaceholderWebhookSecret
		d.config.AllowUnauthenticatedWebhook = true

		w := httptest.NewRecorder()
		s.handleWebhook(w, httptest.NewRequest(http.MethodPost, "/webhook", nil))
		waitForServerTasks(t, s, 5*time.Second)

		assert.Equal(t, http.StatusAccepted, w.Code, "an unsigned request is accepted only because the opt-out is set")
	})
}

// TestServerWebhook_GenericHeaderParity pins the generic /webhook header set
// shared with the receiver (#794): one expected table, duplicated in the
// receiver's tests because the two handler sets live in different packages.
func TestServerWebhook_GenericHeaderParity(t *testing.T) {
	const secret = "parity-secret"
	valid := computeHMACSHA256(nil, secret)

	tests := []struct {
		name    string
		headers map[string]string
		accept  bool
	}{
		{name: "X-Signature", headers: map[string]string{"X-Signature": valid}, accept: true},
		{name: "X-Hub-Signature-256", headers: map[string]string{"X-Hub-Signature-256": valid}, accept: true},
		{name: "X-Signature-256", headers: map[string]string{"X-Signature-256": valid}, accept: true},
		{name: "X-Gitea-Signature is not a generic header", headers: map[string]string{"X-Gitea-Signature": valid}},
		{name: "invalid X-Signature is not rescued by a valid X-Hub-Signature-256", headers: map[string]string{"X-Signature": "sha256=deadbeef", "X-Hub-Signature-256": valid}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, s := newTestDaemon(t)
			d.config.WebhookSecret = secret

			req := httptest.NewRequest(http.MethodPost, "/webhook", nil)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			w := httptest.NewRecorder()
			s.handleWebhook(w, req)
			waitForServerTasks(t, s, 5*time.Second)

			if tt.accept {
				assert.Equal(t, http.StatusAccepted, w.Code)
			} else {
				assert.Equal(t, http.StatusUnauthorized, w.Code)
			}
		})
	}
}

// TestServerWebhooks_SchemeSpecificHeadersStayNarrow guards the other two
// schemes against the generic header set leaking into them: /webhook/manual
// reads only X-Signature and /webhook/github only X-Hub-Signature-256.
// These rows pass on the code before #794 too; they fail if a later change
// routes GenericWebhookSignature into either scheme.
func TestServerWebhooks_SchemeSpecificHeadersStayNarrow(t *testing.T) {
	const secret = "narrow-secret"

	tests := []struct {
		name    string
		path    string
		header  string
		event   string
		handler func(*Server) http.HandlerFunc
	}{
		{name: "manual ignores X-Hub-Signature-256", path: "/webhook/manual", header: "X-Hub-Signature-256", handler: func(s *Server) http.HandlerFunc { return s.handleManualTrigger }},
		{name: "manual ignores X-Signature-256", path: "/webhook/manual", header: "X-Signature-256", handler: func(s *Server) http.HandlerFunc { return s.handleManualTrigger }},
		{name: "github ignores X-Signature", path: "/webhook/github", header: "X-Signature", event: "ping", handler: func(s *Server) http.HandlerFunc { return s.handleGitHubWebhook }},
		{name: "github ignores X-Signature-256", path: "/webhook/github", header: "X-Signature-256", event: "ping", handler: func(s *Server) http.HandlerFunc { return s.handleGitHubWebhook }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, s := newTestDaemon(t)
			d.config.WebhookSecret = secret

			w := httptest.NewRecorder()
			tt.handler(s)(w, signedEmptyTrigger(tt.path, tt.header, tt.event, secret))

			assert.Equal(t, http.StatusUnauthorized, w.Code)
		})
	}
}
