package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cameronsjo/bosun/internal/daemon"
)

func TestResolveReceiverSecret(t *testing.T) {
	tests := []struct {
		name         string
		flag         string
		primary      string
		github       string
		wantSecret   string
		wantConflict bool
	}{
		{name: "flag wins over the environment", flag: "f", primary: "p", github: "g", wantSecret: "f"},
		{name: "WEBHOOK_SECRET alone", primary: "p", wantSecret: "p"},
		{name: "GITHUB_WEBHOOK_SECRET alone", github: "g", wantSecret: "g"},
		{name: "differing environment secrets fail closed", primary: "p", github: "g", wantConflict: true},
		{name: "placeholder flag counts as unset", flag: daemon.PlaceholderWebhookSecret, primary: "p", wantSecret: "p"},
		{name: "placeholder environment secret counts as unset", primary: daemon.PlaceholderWebhookSecret},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := map[string]string{"WEBHOOK_SECRET": tt.primary, "GITHUB_WEBHOOK_SECRET": tt.github}

			secret, conflict := resolveReceiverSecret(tt.flag, func(k string) string { return env[k] })

			assert.Equal(t, tt.wantSecret, secret)
			assert.Equal(t, tt.wantConflict, conflict)
		})
	}
}

var receiverTriggerPaths = []string{"/webhook", "/webhook/github", "/webhook/gitlab", "/webhook/gitea", "/webhook/bitbucket"}

func TestMergeFetchedSecret(t *testing.T) {
	tests := []struct {
		name         string
		secret       string
		conflict     bool
		fetch        bool
		cfg          *daemon.ConfigResponse
		wantSecret   string
		wantConflict bool
		wantFetched  bool
	}{
		{name: "fetch adopts the daemon secret", fetch: true, cfg: &daemon.ConfigResponse{WebhookSecret: "d"}, wantSecret: "d", wantFetched: true},
		{name: "no local secret adopts the daemon secret", cfg: &daemon.ConfigResponse{WebhookSecret: "d"}, wantSecret: "d", wantFetched: true},
		{name: "local secret without fetch is kept", secret: "l", cfg: &daemon.ConfigResponse{WebhookSecret: "d"}, wantSecret: "l"},
		{name: "daemon conflict fails closed on fetch", fetch: true, cfg: &daemon.ConfigResponse{WebhookSecretConflict: true}, wantConflict: true},
		{name: "daemon conflict fails closed with no local secret", cfg: &daemon.ConfigResponse{WebhookSecretConflict: true}, wantConflict: true},
		{name: "daemon conflict does not touch a local secret without fetch", secret: "l", cfg: &daemon.ConfigResponse{WebhookSecretConflict: true}, wantSecret: "l"},
		{name: "local conflict is not settled by a fetched secret", conflict: true, fetch: true, cfg: &daemon.ConfigResponse{WebhookSecret: "d"}, wantConflict: true},
		{name: "daemon placeholder is not adopted", fetch: true, cfg: &daemon.ConfigResponse{WebhookSecret: "Change-Me"}},
		{name: "no daemon config keeps the local state", secret: "l", fetch: true, wantSecret: "l"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			secret, conflict, fetched := mergeFetchedSecret(tt.secret, tt.conflict, tt.fetch, tt.cfg)

			assert.Equal(t, tt.wantSecret, secret)
			assert.Equal(t, tt.wantConflict, conflict)
			assert.Equal(t, tt.wantFetched, fetched)
		})
	}
}

// TestWebhookReceiver_DaemonConflictFailsClosedDespiteOptOut is #837: a
// receiver with no secret of its own and its own opt-out set, fetching from a
// daemon whose secrets conflict, must refuse every trigger with 403. Before
// the fix the daemon sent an empty secret and no conflict, so the receiver's
// opt-out forwarded unsigned requests.
func TestWebhookReceiver_DaemonConflictFailsClosedDespiteOptOut(t *testing.T) {
	daemonCfg := &daemon.ConfigResponse{WebhookSecretConflict: true}
	secret, conflict, _ := mergeFetchedSecret("", false, true, daemonCfg)

	for _, path := range receiverTriggerPaths {
		t.Run(path, func(t *testing.T) {
			client := &recordingWebhookClient{}
			mux := newWebhookMux(&webhookHandler{client: client, secret: secret, secretConflict: conflict, allowUnauthenticated: true})

			req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader([]byte(`{"event":"push"}`)))
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			assert.Equal(t, http.StatusForbidden, w.Code)
			assert.Zero(t, client.triggerCalls)
		})
	}
}

// TestInitEnvTemplate_WebhookSecretPlaceholderFailsClosed: the example value
// `bosun init` writes is a placeholder the servers refuse, so uncommenting the
// line as written cannot configure a public secret.
func TestInitEnvTemplate_WebhookSecretPlaceholderFailsClosed(t *testing.T) {
	require.Contains(t, systemdEnvFile, "# WEBHOOK_SECRET="+daemon.InitTemplateWebhookSecretPlaceholder+"\n")
	assert.False(t, daemon.WebhookSecretConfigured(daemon.InitTemplateWebhookSecretPlaceholder))
}

// TestWebhookReceiver_SecretConflictFailsClosed: two differing environment
// secrets with no --secret leave the receiver refusing every trigger with
// 403, and the unauthenticated opt-out does not reopen it (#794).
func TestWebhookReceiver_SecretConflictFailsClosed(t *testing.T) {
	env := map[string]string{"WEBHOOK_SECRET": "generic-secret", "GITHUB_WEBHOOK_SECRET": "github-secret"}
	secret, conflict := resolveReceiverSecret("", func(k string) string { return env[k] })
	require.True(t, conflict)

	for _, path := range receiverTriggerPaths {
		t.Run(path, func(t *testing.T) {
			client := &recordingWebhookClient{}
			mux := newWebhookMux(&webhookHandler{client: client, secret: secret, secretConflict: conflict, allowUnauthenticated: true})

			payload := []byte(`{"event":"push"}`)
			req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(payload))
			req.Header.Set("X-Hub-Signature-256", "sha256="+computeHMAC(payload, "github-secret"))
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			assert.Equal(t, http.StatusForbidden, w.Code)
			assert.Zero(t, client.triggerCalls)
		})
	}
}

// TestWebhookReceiver_PlaceholderSecretFailsClosed: the public "change-me"
// default authenticates nothing on the receiver either (#794).
func TestWebhookReceiver_PlaceholderSecretFailsClosed(t *testing.T) {
	for _, path := range receiverTriggerPaths {
		t.Run(path, func(t *testing.T) {
			client := &recordingWebhookClient{}
			mux := newWebhookMux(&webhookHandler{client: client, secret: daemon.PlaceholderWebhookSecret})

			req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader([]byte(`{}`)))
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			assert.Equal(t, http.StatusForbidden, w.Code)
			assert.Zero(t, client.triggerCalls)
		})
	}

	t.Run("a request signed with the placeholder is refused", func(t *testing.T) {
		client := &recordingWebhookClient{}
		mux := newWebhookMux(&webhookHandler{client: client, secret: daemon.PlaceholderWebhookSecret})

		payload := []byte(`{"event":"push"}`)
		req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(payload))
		req.Header.Set("X-Hub-Signature-256", "sha256="+computeHMAC(payload, daemon.PlaceholderWebhookSecret))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Zero(t, client.triggerCalls)
	})
}

// TestWebhookReceiver_GenericHeaderParity is the receiver half of the generic
// header table in internal/daemon's TestServerWebhook_GenericHeaderParity;
// keep the two tables identical (#794).
func TestWebhookReceiver_GenericHeaderParity(t *testing.T) {
	const secret = "parity-secret"
	payload := []byte(`{"event":"push"}`)
	valid := "sha256=" + computeHMAC(payload, secret)

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
			client := &recordingWebhookClient{}
			mux := newWebhookMux(&webhookHandler{client: client, secret: secret})

			req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(payload))
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if tt.accept {
				assert.Equal(t, http.StatusAccepted, w.Code)
				assert.Equal(t, 1, client.triggerCalls)
			} else {
				assert.Equal(t, http.StatusUnauthorized, w.Code)
				assert.Zero(t, client.triggerCalls)
			}
		})
	}
}
