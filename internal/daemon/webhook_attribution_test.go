package daemon

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGitHubWebhookSanitizesUnsignedPusherAttribution(t *testing.T) {
	const malicious = "trusted\nFORGED\r\t\x1b[31m\u0085\u2028\u2029\u202e"
	const sanitized = "trustedFORGED[31m"

	status, loggedPusher, source := exerciseGitHubPusherAttribution(t, malicious, "", true)

	assert.Equal(t, http.StatusAccepted, status)
	assert.Equal(t, sanitized, loggedPusher)
	assert.Equal(t, "github:"+sanitized, source)
}

func TestGitHubWebhookSanitizesSignedPusherAttribution(t *testing.T) {
	const malicious = "trusted\nFORGED\r\t\x1b[31m\u0085\u2028\u2029\u202e"
	const sanitized = "trustedFORGED[31m"

	status, loggedPusher, source := exerciseGitHubPusherAttribution(t, malicious, "webhook-secret", false)

	assert.Equal(t, http.StatusAccepted, status)
	assert.Equal(t, sanitized, loggedPusher)
	assert.Equal(t, "github:"+sanitized, source)
}

func TestGitHubWebhookPreservesLegitimatePusherAttribution(t *testing.T) {
	tests := []struct {
		name                 string
		pusher               string
		secret               string
		allowUnauthenticated bool
	}{
		{
			name:                 "unsigned explicit opt-out",
			pusher:               "dependabot[bot]",
			allowUnauthenticated: true,
		},
		{
			name:   "valid signature",
			pusher: "Renée 🚀 / ops",
			secret: "webhook-secret",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, loggedPusher, source := exerciseGitHubPusherAttribution(t, tt.pusher, tt.secret, tt.allowUnauthenticated)

			assert.Equal(t, http.StatusAccepted, status)
			assert.Equal(t, tt.pusher, loggedPusher)
			assert.Equal(t, "github:"+tt.pusher, source)
		})
	}
}

func TestGitHubWebhookRejectsUnsignedPusherBeforeAttribution(t *testing.T) {
	const malicious = "trusted\nFORGED\x1b[31m"

	d, s := newTestDaemon(t)
	payload := GitHubPushPayload{Ref: "refs/heads/main", After: "abc123"}
	payload.Pusher.Name = malicious
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/webhook/github", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "push")
	var logs bytes.Buffer
	requestLogger := zerolog.New(&logs)
	req = req.WithContext(requestLogger.WithContext(req.Context()))
	w := httptest.NewRecorder()

	s.handleGitHubWebhook(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assertNoReconcileTriggered(t, d, s)
	assert.NotContains(t, logs.String(), "GitHub push received on tracked branch")
	assert.NotContains(t, logs.String(), "FORGED")
}

func TestSanitizeWebhookPusherName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "printable Unicode is unchanged",
			input: "Renée 🚀 / ops",
			want:  "Renée 🚀 / ops",
		},
		{
			name:  "ASCII and Unicode controls are stripped",
			input: "trusted\nFORGED\r\t\x1b[31m\u0085\u2028\u2029\u202e",
			want:  "trustedFORGED[31m",
		},
		{
			name:  "exact boundary is preserved",
			input: strings.Repeat("a", maxWebhookPusherNameLength),
			want:  strings.Repeat("a", maxWebhookPusherNameLength),
		},
		{
			name:  "oversized attribution is capped",
			input: strings.Repeat("🚀", maxWebhookPusherNameLength+1),
			want:  strings.Repeat("🚀", maxWebhookPusherNameLength),
		},
		{
			name:  "stripped prefix does not consume the cap",
			input: strings.Repeat("\n", maxWebhookPusherNameLength) + strings.Repeat("b", maxWebhookPusherNameLength+1),
			want:  strings.Repeat("b", maxWebhookPusherNameLength),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, SanitizeWebhookPusherName(tt.input))
		})
	}
}

func exerciseGitHubPusherAttribution(t *testing.T, pusherName, secret string, allowUnauthenticated bool) (int, string, string) {
	t.Helper()

	d, s := newTestDaemon(t)
	d.config.WebhookSecret = secret
	d.config.AllowUnauthenticatedWebhook = allowUnauthenticated

	// Make TriggerReconcile take its deterministic coalescing path. This records
	// the source without cloning a repository or starting external work.
	d.reconcileMu.Lock()
	d.reconciling = true
	d.reconcileMu.Unlock()

	payload := GitHubPushPayload{Ref: "refs/heads/main", After: "abc123"}
	payload.Pusher.Name = pusherName
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/webhook/github", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "push")
	if secret != "" {
		req.Header.Set("X-Hub-Signature-256", computeHMACSHA256(body, secret))
	}

	var logs bytes.Buffer
	requestLogger := zerolog.New(&logs)
	req = req.WithContext(requestLogger.WithContext(req.Context()))
	w := httptest.NewRecorder()
	s.handleGitHubWebhook(w, req)
	s.wg.Wait()

	d.reconcileMu.Lock()
	source := d.triggerSource
	d.reconcileMu.Unlock()

	return w.Code, findWebhookLogField(t, logs.String(), "GitHub push received on tracked branch", "pusher"), source
}

func findWebhookLogField(t *testing.T, logs, message, field string) string {
	t.Helper()

	scanner := bufio.NewScanner(strings.NewReader(logs))
	for scanner.Scan() {
		var event map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue
		}
		if event["message"] == message {
			value, ok := event[field].(string)
			require.True(t, ok, "log field %q must be a string", field)
			return value
		}
	}
	require.NoError(t, scanner.Err())
	require.FailNow(t, "matching webhook log event not found", "message=%q logs=%s", message, logs)
	return ""
}
