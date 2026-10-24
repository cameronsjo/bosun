package daemon

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const zeroSHA = "0000000000000000000000000000000000000000"

func TestPushDeletesBranch(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{name: "github delete", body: `{"ref":"refs/heads/main","deleted":true,"after":"` + zeroSHA + `"}`, want: true},
		{name: "deleted flag alone", body: `{"deleted":true}`, want: true},
		{name: "gitlab or gitea zero after", body: `{"ref":"refs/heads/main","after":"` + zeroSHA + `"}`, want: true},
		{name: "sha256 zero after", body: `{"after":"` + strings.Repeat("0", 64) + `"}`, want: true},
		{name: "ordinary push", body: `{"ref":"refs/heads/main","deleted":false,"after":"abc123"}`, want: false},
		{name: "after with leading zeros", body: `{"after":"000abc"}`, want: false},
		{name: "empty after", body: `{"after":""}`, want: false},
		{name: "deleted not a bool", body: `{"deleted":"true"}`, want: false},
		{name: "after not a string", body: `{"after":0}`, want: false},
		{name: "no fields", body: `{}`, want: false},
		{name: "empty body", body: ``, want: false},
		{name: "not json", body: `ref=refs/heads/main`, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, PushDeletesBranch([]byte(tt.body)))
		})
	}
}

func TestWebhookDeliveryID(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string]string
		want    string
	}{
		{name: "github", headers: map[string]string{"X-GitHub-Delivery": "g-1"}, want: "X-GitHub-Delivery:g-1"},
		{name: "gitea sends both", headers: map[string]string{"X-GitHub-Delivery": "t-1", "X-Gitea-Delivery": "t-1"}, want: "X-GitHub-Delivery:t-1"},
		{name: "gitea only", headers: map[string]string{"X-Gitea-Delivery": "t-2"}, want: "X-Gitea-Delivery:t-2"},
		{name: "gitlab webhook-id first", headers: map[string]string{"webhook-id": "l-1", "Idempotency-Key": "l-1", "X-Gitlab-Event-UUID": "e-1"}, want: "Webhook-Id:l-1"},
		{name: "gitlab event uuid", headers: map[string]string{"X-Gitlab-Event-UUID": "e-2"}, want: "X-Gitlab-Event-UUID:e-2"},
		{name: "bitbucket", headers: map[string]string{"X-Request-UUID": "b-1"}, want: "X-Request-UUID:b-1"},
		{name: "per-hook ids are not delivery ids", headers: map[string]string{"X-Gitlab-Webhook-UUID": "hook", "X-Hook-UUID": "hook"}, want: ""},
		{name: "blank value skipped", headers: map[string]string{"X-GitHub-Delivery": "  ", "X-Request-UUID": "b-2"}, want: "X-Request-UUID:b-2"},
		{name: "none", headers: map[string]string{}, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := http.Header{}
			for k, v := range tt.headers {
				h.Set(k, v)
			}
			assert.Equal(t, tt.want, WebhookDeliveryID(h))
		})
	}
}

func TestWebhookBodyKey(t *testing.T) {
	assert.Equal(t, "", WebhookBodyKey(nil))
	assert.Equal(t, "", WebhookBodyKey([]byte{}))
	a := WebhookBodyKey([]byte(`{"after":"abc"}`))
	assert.Regexp(t, `^body-sha256:[0-9a-f]{64}$`, a)
	assert.Equal(t, a, WebhookBodyKey([]byte(`{"after":"abc"}`)))
	assert.NotEqual(t, a, WebhookBodyKey([]byte(`{"after":"abd"}`)))

	h := http.Header{}
	h.Set("X-GitHub-Delivery", "g-1")
	assert.Equal(t, []string{"X-GitHub-Delivery:g-1", a}, WebhookReplayKeys(h, []byte(`{"after":"abc"}`)))
}

func TestDeliveryCache(t *testing.T) {
	newClocked := func(capacity int, ttl time.Duration) (*DeliveryCache, *time.Time) {
		now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
		c := NewDeliveryCache(capacity, ttl)
		c.now = func() time.Time { return now }
		return c, &now
	}

	t.Run("repeat within ttl is refused", func(t *testing.T) {
		c, now := newClocked(8, time.Hour)
		assert.True(t, c.Claim("a"))
		*now = now.Add(59 * time.Minute)
		assert.False(t, c.Claim("a"))
		assert.True(t, c.Claim("b"))
	})

	t.Run("entry expires after ttl", func(t *testing.T) {
		c, now := newClocked(8, time.Hour)
		assert.True(t, c.Claim("a"))
		*now = now.Add(time.Hour)
		assert.True(t, c.Claim("a"))
		assert.False(t, c.Claim("a"))
	})

	t.Run("oldest entry is dropped at capacity", func(t *testing.T) {
		c, _ := newClocked(3, time.Hour)
		for _, id := range []string{"a", "b", "c", "d"} {
			require.True(t, c.Claim(id))
		}
		assert.Equal(t, 3, c.order.Len())
		assert.Len(t, c.entries, 3)
		assert.True(t, c.Claim("a"), "a was evicted")
		assert.False(t, c.Claim("d"))
	})

	t.Run("memory stays bounded", func(t *testing.T) {
		c, _ := newClocked(DefaultDeliveryCacheSize, time.Hour)
		for i := 0; i < 5*DefaultDeliveryCacheSize; i++ {
			c.Claim(fmt.Sprintf("id-%d-%s", i, strings.Repeat("x", 1000)))
		}
		assert.Equal(t, DefaultDeliveryCacheSize, c.order.Len())
		assert.Len(t, c.entries, DefaultDeliveryCacheSize)
	})

	t.Run("release allows a retry", func(t *testing.T) {
		c, _ := newClocked(8, time.Hour)
		assert.True(t, c.Claim("a"))
		c.Release("a")
		assert.True(t, c.Claim("a"))
		c.Release("never-claimed")
	})

	t.Run("empty id is never recorded", func(t *testing.T) {
		var c DeliveryCache
		assert.True(t, c.Claim(""))
		assert.True(t, c.Claim(""))
		c.Release("")
	})

	t.Run("any repeated key refuses and records nothing", func(t *testing.T) {
		c, _ := newClocked(8, time.Hour)
		assert.True(t, c.Claim("id-1", "body-1"))
		assert.False(t, c.Claim("id-2", "body-1"), "body repeated under a new id")
		assert.True(t, c.Claim("id-2", "body-2"), "id-2 was not recorded by the refused claim")
		assert.False(t, c.Claim("id-1", "body-3"), "id repeated with a new body")
	})

	t.Run("release forgets every key", func(t *testing.T) {
		c, _ := newClocked(8, time.Hour)
		assert.True(t, c.Claim("id-1", "body-1"))
		c.Release("id-1", "body-1")
		assert.True(t, c.Claim("id-9", "body-1"))
		assert.True(t, c.Claim("id-1"))
	})

	t.Run("zero value uses defaults", func(t *testing.T) {
		var c DeliveryCache
		assert.True(t, c.Claim("a"))
		assert.False(t, c.Claim("a"))
		assert.Equal(t, DefaultDeliveryCacheSize, c.capacity)
		assert.Equal(t, DefaultDeliveryCacheTTL, c.ttl)
	})
}

// TestDaemonWebhookIgnoresBranchDelete pins #747 on the daemon's endpoints: a
// push that deletes the tracked branch passes the ref filter but must not
// start a reconcile.
func TestDaemonWebhookIgnoresBranchDelete(t *testing.T) {
	const secret = "webhook-secret"
	deleteBody := `{"ref":"refs/heads/main","deleted":true,"after":"` + zeroSHA + `","before":"abc123"}`
	zeroAfterBody := `{"ref":"refs/heads/main","after":"` + zeroSHA + `"}`

	for _, tc := range []struct {
		name     string
		endpoint string
		body     string
		label    string
	}{
		{name: "generic github delete", endpoint: "/webhook", body: deleteBody, label: "generic"},
		{name: "generic zero after", endpoint: "/webhook", body: zeroAfterBody, label: "generic"},
		{name: "github delete", endpoint: "/webhook/github", body: deleteBody, label: "github"},
		{name: "github zero after", endpoint: "/webhook/github", body: zeroAfterBody, label: "github"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, s := newTestDaemon(t)
			d.config.WebhookSecret = secret
			d.config.ReconcileConfig.RepoBranch = "main"

			w := httptest.NewRecorder()
			serveDaemonWebhook(s, w, signedDaemonRequest(tc.endpoint, tc.body, secret, ""))

			require.Equal(t, http.StatusOK, w.Code)
			assert.Contains(t, w.Body.String(), `"status":"ignored"`)
			assert.Contains(t, w.Body.String(), "deletes refs/heads/main")
			assert.Equal(t, 0.0, testutil.ToFloat64(s.metrics.WebhookTriggersTotal.WithLabelValues(tc.label)))
		})
	}
}

// TestDaemonWebhookDropsReplayedDelivery pins #747 on the daemon's endpoints:
// a delivery ID already handled gets 200 ignored, after the signature check.
func TestDaemonWebhookDropsReplayedDelivery(t *testing.T) {
	const secret = "webhook-secret"
	body := `{"ref":"refs/heads/main","after":"abc123"}`
	otherBody := `{"ref":"refs/heads/main","after":"def456"}`

	for _, tc := range []struct{ endpoint, label string }{
		{endpoint: "/webhook", label: "generic"},
		{endpoint: "/webhook/github", label: "github"},
	} {
		t.Run(tc.endpoint+" repeat ignored", func(t *testing.T) {
			d, s := newTestDaemon(t)
			d.config.WebhookSecret = secret
			d.config.ReconcileConfig.RepoBranch = "main"
			triggers := func() float64 {
				return testutil.ToFloat64(s.metrics.WebhookTriggersTotal.WithLabelValues(tc.label))
			}

			first := httptest.NewRecorder()
			serveDaemonWebhook(s, first, signedDaemonRequest(tc.endpoint, body, secret, "d-1"))
			require.Equal(t, http.StatusAccepted, first.Code)

			replay := httptest.NewRecorder()
			serveDaemonWebhook(s, replay, signedDaemonRequest(tc.endpoint, body, secret, "d-1"))
			require.Equal(t, http.StatusOK, replay.Code)
			assert.Contains(t, replay.Body.String(), `"status":"ignored"`)
			assert.Contains(t, replay.Body.String(), "Duplicate delivery")
			assert.Equal(t, 1.0, triggers())

			other := httptest.NewRecorder()
			serveDaemonWebhook(s, other, signedDaemonRequest(tc.endpoint, otherBody, secret, "d-2"))
			assert.Equal(t, http.StatusAccepted, other.Code)
			assert.Equal(t, 2.0, triggers())
		})

		t.Run(tc.endpoint+" same body with a changed delivery header is ignored", func(t *testing.T) {
			d, s := newTestDaemon(t)
			d.config.WebhookSecret = secret
			d.config.ReconcileConfig.RepoBranch = "main"

			first := httptest.NewRecorder()
			serveDaemonWebhook(s, first, signedDaemonRequest(tc.endpoint, body, secret, "d-1"))
			require.Equal(t, http.StatusAccepted, first.Code)

			for _, id := range []string{"d-forged", ""} {
				replay := httptest.NewRecorder()
				serveDaemonWebhook(s, replay, signedDaemonRequest(tc.endpoint, body, secret, id))
				require.Equal(t, http.StatusOK, replay.Code)
				assert.Contains(t, replay.Body.String(), "Duplicate delivery")
			}
			assert.Equal(t, 1.0, testutil.ToFloat64(s.metrics.WebhookTriggersTotal.WithLabelValues(tc.label)))
		})

		t.Run(tc.endpoint+" bad signature on a recorded id is rejected", func(t *testing.T) {
			d, s := newTestDaemon(t)
			d.config.WebhookSecret = secret
			d.config.ReconcileConfig.RepoBranch = "main"

			first := httptest.NewRecorder()
			serveDaemonWebhook(s, first, signedDaemonRequest(tc.endpoint, body, secret, "d-1"))
			require.Equal(t, http.StatusAccepted, first.Code)

			forged := httptest.NewRecorder()
			serveDaemonWebhook(s, forged, signedDaemonRequest(tc.endpoint, body, "wrong-secret", "d-1"))
			assert.Equal(t, http.StatusUnauthorized, forged.Code)
		})

		t.Run(tc.endpoint+" bad signature does not record the id", func(t *testing.T) {
			d, s := newTestDaemon(t)
			d.config.WebhookSecret = secret
			d.config.ReconcileConfig.RepoBranch = "main"

			forged := httptest.NewRecorder()
			serveDaemonWebhook(s, forged, signedDaemonRequest(tc.endpoint, body, "wrong-secret", "d-1"))
			require.Equal(t, http.StatusUnauthorized, forged.Code)

			real := httptest.NewRecorder()
			serveDaemonWebhook(s, real, signedDaemonRequest(tc.endpoint, body, secret, "d-1"))
			assert.Equal(t, http.StatusAccepted, real.Code)
		})
	}
}

// signedDaemonRequest builds a push request signed for the daemon. An empty
// deliveryID sends no delivery header.
func signedDaemonRequest(endpoint, body, secret, deliveryID string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, endpoint, strings.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", computeHMACSHA256([]byte(body), secret))
	req.Header.Set("X-GitHub-Event", "push")
	if deliveryID != "" {
		req.Header.Set("X-GitHub-Delivery", deliveryID)
	}
	return req
}

func serveDaemonWebhook(s *Server, w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/webhook/github" {
		s.handleGitHubWebhook(w, r)
		return
	}
	s.handleWebhook(w, r)
}

// TestDaemonGenericWebhookEmptyBodyIsNotDeduplicated keeps the plain signed
// trigger working: an empty body carries no event, so it has no body key, and
// sending it repeatedly still triggers each time.
func TestDaemonGenericWebhookEmptyBodyIsNotDeduplicated(t *testing.T) {
	const secret = "webhook-secret"
	d, s := newTestDaemon(t)
	d.config.WebhookSecret = secret
	d.config.ReconcileConfig.RepoBranch = "main"

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(""))
		req.Header.Set("X-Signature", computeHMACSHA256(nil, secret))
		w := httptest.NewRecorder()
		s.handleWebhook(w, req)
		require.Equal(t, http.StatusAccepted, w.Code)
	}
	assert.Equal(t, 2.0, testutil.ToFloat64(s.metrics.WebhookTriggersTotal.WithLabelValues("generic")))
}
