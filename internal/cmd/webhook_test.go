package cmd

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cameronsjo/bosun/internal/daemon"
	"github.com/cameronsjo/bosun/internal/log"
)

func TestComputeHMAC(t *testing.T) {
	tests := []struct {
		name   string
		data   []byte
		secret string
		want   string
	}{
		{
			name:   "simple message",
			data:   []byte("hello world"),
			secret: "secret",
			want:   computeExpectedHMAC([]byte("hello world"), "secret"),
		},
		{
			name:   "empty data",
			data:   []byte{},
			secret: "secret",
			want:   computeExpectedHMAC([]byte{}, "secret"),
		},
		{
			name:   "json payload",
			data:   []byte(`{"action":"push","ref":"refs/heads/main"}`),
			secret: "webhook-secret-123",
			want:   computeExpectedHMAC([]byte(`{"action":"push","ref":"refs/heads/main"}`), "webhook-secret-123"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeHMAC(tt.data, tt.secret)
			if got != tt.want {
				t.Errorf("computeHMAC() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestComputeHMACSHA1(t *testing.T) {
	tests := []struct {
		name   string
		data   []byte
		secret string
	}{
		{
			name:   "simple message",
			data:   []byte("hello world"),
			secret: "secret",
		},
		{
			name:   "json payload",
			data:   []byte(`{"push":{"changes":[]}}`),
			secret: "bitbucket-secret",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeHMACSHA1(tt.data, tt.secret)

			// Verify it's valid hex
			_, err := hex.DecodeString(got)
			if err != nil {
				t.Errorf("computeHMACSHA1() returned invalid hex: %v", err)
			}

			// Verify length (SHA1 = 20 bytes = 40 hex chars)
			if len(got) != 40 {
				t.Errorf("computeHMACSHA1() length = %d, want 40", len(got))
			}

			// Verify it matches manual computation
			mac := hmac.New(sha1.New, []byte(tt.secret))
			mac.Write(tt.data)
			expected := hex.EncodeToString(mac.Sum(nil))
			if got != expected {
				t.Errorf("computeHMACSHA1() = %v, want %v", got, expected)
			}
		})
	}
}

func TestValidateSignature(t *testing.T) {
	secret := "test-secret"
	payload := []byte(`{"action":"push"}`)
	validSig := "sha256=" + computeHMAC(payload, secret)

	tests := []struct {
		name      string
		body      []byte
		signature string
		secret    string
		want      bool
	}{
		{
			name:      "valid signature with prefix",
			body:      payload,
			signature: validSig,
			secret:    secret,
			want:      true,
		},
		{
			name:      "valid signature without prefix",
			body:      payload,
			signature: computeHMAC(payload, secret),
			secret:    secret,
			want:      true,
		},
		{
			name:      "invalid signature",
			body:      payload,
			signature: "sha256=invalid",
			secret:    secret,
			want:      false,
		},
		{
			name:      "empty signature",
			body:      payload,
			signature: "",
			secret:    secret,
			want:      false,
		},
		{
			name:      "empty secret",
			body:      payload,
			signature: validSig,
			secret:    "",
			want:      false,
		},
		{
			name:      "wrong secret",
			body:      payload,
			signature: validSig,
			secret:    "wrong-secret",
			want:      false,
		},
		{
			name:      "modified payload",
			body:      []byte(`{"action":"pull"}`),
			signature: validSig,
			secret:    secret,
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validateSignature(tt.body, tt.signature, tt.secret)
			if got != tt.want {
				t.Errorf("validateSignature() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateGitHubSignature(t *testing.T) {
	secret := "github-webhook-secret"
	payload := []byte(`{"ref":"refs/heads/main","pusher":{"name":"user"}}`)
	validSig := "sha256=" + computeHMAC(payload, secret)

	tests := []struct {
		name      string
		body      []byte
		signature string
		secret    string
		want      bool
	}{
		{
			name:      "valid GitHub signature",
			body:      payload,
			signature: validSig,
			secret:    secret,
			want:      true,
		},
		{
			name:      "missing sha256 prefix",
			body:      payload,
			signature: computeHMAC(payload, secret),
			secret:    secret,
			want:      false, // GitHub requires prefix
		},
		{
			name:      "invalid signature",
			body:      payload,
			signature: "sha256=0000000000000000000000000000000000000000000000000000000000000000",
			secret:    secret,
			want:      false,
		},
		{
			name:      "empty signature",
			body:      payload,
			signature: "",
			secret:    secret,
			want:      false,
		},
		{
			name:      "short signature",
			body:      payload,
			signature: "sha256",
			secret:    secret,
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validateGitHubSignature(tt.body, tt.signature, tt.secret)
			if got != tt.want {
				t.Errorf("validateGitHubSignature() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateGiteaSignature(t *testing.T) {
	secret := "gitea-secret"
	payload := []byte(`{"ref":"refs/heads/main","pusher":{"login":"user"}}`)
	validSig := computeHMAC(payload, secret)

	tests := []struct {
		name      string
		body      []byte
		signature string
		secret    string
		want      bool
	}{
		{
			name:      "valid Gitea signature",
			body:      payload,
			signature: validSig,
			secret:    secret,
			want:      true,
		},
		{
			name:      "invalid signature",
			body:      payload,
			signature: "invalid",
			secret:    secret,
			want:      false,
		},
		{
			name:      "empty signature",
			body:      payload,
			signature: "",
			secret:    secret,
			want:      false,
		},
		{
			name:      "empty secret",
			body:      payload,
			signature: validSig,
			secret:    "",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validateGiteaSignature(tt.body, tt.signature, tt.secret)
			if got != tt.want {
				t.Errorf("validateGiteaSignature() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateBitbucketSignature(t *testing.T) {
	secret := "bitbucket-secret"
	payload := []byte(`{"push":{"changes":[{"new":{"name":"main"}}]}}`)
	validSHA256 := "sha256=" + computeHMAC(payload, secret)
	validSHA1 := "sha1=" + computeHMACSHA1(payload, secret)
	validPlain := computeHMAC(payload, secret)

	tests := []struct {
		name      string
		body      []byte
		signature string
		secret    string
		want      bool
	}{
		{
			name:      "valid sha256 signature (Bitbucket Cloud)",
			body:      payload,
			signature: validSHA256,
			secret:    secret,
			want:      true,
		},
		{
			name:      "valid sha1 signature (Bitbucket Server)",
			body:      payload,
			signature: validSHA1,
			secret:    secret,
			want:      true,
		},
		{
			name:      "valid plain signature (fallback)",
			body:      payload,
			signature: validPlain,
			secret:    secret,
			want:      true,
		},
		{
			name:      "invalid sha256 signature",
			body:      payload,
			signature: "sha256=invalid",
			secret:    secret,
			want:      false,
		},
		{
			name:      "invalid sha1 signature",
			body:      payload,
			signature: "sha1=invalid",
			secret:    secret,
			want:      false,
		},
		{
			name:      "empty signature",
			body:      payload,
			signature: "",
			secret:    secret,
			want:      false,
		},
		{
			name:      "empty secret",
			body:      payload,
			signature: validSHA256,
			secret:    "",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validateBitbucketSignature(tt.body, tt.signature, tt.secret)
			if got != tt.want {
				t.Errorf("validateBitbucketSignature() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStandaloneGitHubWebhookSanitizesPusherAttribution(t *testing.T) {
	const malicious = "trusted\nFORGED\r\t\x1b[31m\u0085\u2028\u2029\u202e"
	const sanitized = "trustedFORGED[31m"

	client := &recordingWebhookClient{}
	handler := &webhookHandler{
		client: client,
		secret: "webhook-secret",
	}
	payload, err := json.Marshal(map[string]any{
		"ref":    "refs/heads/main",
		"pusher": map[string]string{"name": malicious},
	})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/webhook/github", bytes.NewReader(payload))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-Hub-Signature-256", "sha256="+computeHMAC(payload, handler.secret))
	w := httptest.NewRecorder()

	output := captureWebhookColorOutput(t, func() {
		handler.handleGitHubWebhook(w, req)
	})

	assert.Equal(t, http.StatusAccepted, w.Code)
	assert.Equal(t, "github:"+sanitized, client.source)
	assert.Equal(t, "GitHub push from "+sanitized+" on refs/heads/main\n", output)
	assert.Equal(t, 1, strings.Count(output, "\n"), "attacker input must not create extra log lines")
}

func TestStandaloneWebhookLivenessHandlers(t *testing.T) {
	t.Run("health proxies bounded healthy response", func(t *testing.T) {
		client := &recordingWebhookClient{health: &daemon.HealthResponse{
			Status: "healthy",
			Ready:  true,
			Uptime: 42,
		}}
		handler := &webhookHandler{client: client}
		w := httptest.NewRecorder()
		handler.handleHealth(w, httptest.NewRequest(http.MethodGet, "/health", nil))

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, 1, client.healthCalls)
		assert.ElementsMatch(t, []string{"status", "ready", "uptime"}, jsonObjectKeys(t, w.Body.Bytes()))
	})

	t.Run("health preserves degraded status", func(t *testing.T) {
		client := &recordingWebhookClient{health: &daemon.HealthResponse{
			Status: "degraded",
			Ready:  true,
		}}
		handler := &webhookHandler{client: client}
		w := httptest.NewRecorder()
		handler.handleHealth(w, httptest.NewRequest(http.MethodGet, "/health", nil))

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
		assert.ElementsMatch(t, []string{"status", "ready", "uptime"}, jsonObjectKeys(t, w.Body.Bytes()))
	})

	t.Run("health bounds daemon failure response", func(t *testing.T) {
		const sensitive = "daemon unreachable at /srv/private/repo"
		client := &recordingWebhookClient{healthError: errors.New(sensitive)}
		handler := &webhookHandler{client: client}
		w := httptest.NewRecorder()
		handler.handleHealth(w, httptest.NewRequest(http.MethodGet, "/health", nil))

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
		assert.Equal(t, 1, client.healthCalls)
		assert.ElementsMatch(t, []string{"status", "ready", "uptime"}, jsonObjectKeys(t, w.Body.Bytes()))
		assert.NotContains(t, w.Body.String(), sensitive)
		assert.NotContains(t, w.Body.String(), `"error"`)
	})

	t.Run("ready keeps plain response", func(t *testing.T) {
		client := &recordingWebhookClient{health: &daemon.HealthResponse{Status: "healthy", Ready: true}}
		handler := &webhookHandler{client: client}
		w := httptest.NewRecorder()
		handler.handleReady(w, httptest.NewRequest(http.MethodGet, "/ready", nil))

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "ready", w.Body.String())
	})

	for _, path := range []string{"/health", "/ready"} {
		t.Run("non-GET "+path+" does not call daemon", func(t *testing.T) {
			client := &recordingWebhookClient{}
			handler := &webhookHandler{client: client}
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, path, nil)
			if path == "/health" {
				handler.handleHealth(w, req)
			} else {
				handler.handleReady(w, req)
			}

			assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
			assert.Equal(t, 0, client.healthCalls)
		})
	}
}

func jsonObjectKeys(t *testing.T, body []byte) []string {
	t.Helper()
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &fields))
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	return keys
}

type recordingWebhookClient struct {
	source      string
	health      *daemon.HealthResponse
	healthError error
	healthCalls int
}

func (c *recordingWebhookClient) Trigger(_ context.Context, source string, _ bool) (*daemon.TriggerResponse, error) {
	c.source = source
	return &daemon.TriggerResponse{Status: "accepted"}, nil
}

func (c *recordingWebhookClient) Health(context.Context) (*daemon.HealthResponse, error) {
	c.healthCalls++
	if c.healthError != nil {
		return nil, c.healthError
	}
	if c.health != nil {
		return c.health, nil
	}
	return &daemon.HealthResponse{Status: "healthy"}, nil
}

func captureWebhookColorOutput(t *testing.T, fn func()) string {
	t.Helper()

	oldFormat := log.GetFormat()
	oldOutput := color.Output
	oldNoColor := color.NoColor
	var output bytes.Buffer

	log.Init(&log.Options{Format: log.FormatConsole})
	color.Output = &output
	color.NoColor = true
	defer func() {
		color.Output = oldOutput
		color.NoColor = oldNoColor
		log.Init(&log.Options{Format: oldFormat})
	}()

	fn()
	return output.String()
}

// computeExpectedHMAC is a helper for test verification.
func computeExpectedHMAC(data []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil))
}
