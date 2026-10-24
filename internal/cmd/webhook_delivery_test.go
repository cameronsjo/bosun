package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cameronsjo/bosun/internal/daemon"
)

const zeroCommit = "0000000000000000000000000000000000000000"

// receiverPushBody returns a push of commit to the tracked branch main in the
// endpoint's payload shape. Different commits give different bodies.
func receiverPushBody(endpoint, commit string) string {
	switch endpoint {
	case "/webhook/bitbucket":
		return `{"push":{"changes":[{"new":{"type":"branch","name":"main","target":{"hash":"` + commit + `"}}}]}}`
	default:
		return `{"ref":"refs/heads/main","after":"` + commit + `"}`
	}
}

// receiverDeliveryHeader names the delivery header each provider sends.
var receiverDeliveryHeader = map[string]string{
	"/webhook":           "X-GitHub-Delivery",
	"/webhook/github":    "X-GitHub-Delivery",
	"/webhook/gitlab":    "X-Gitlab-Event-UUID",
	"/webhook/gitea":     "X-Gitea-Delivery",
	"/webhook/bitbucket": "X-Request-UUID",
}

// receiverRequest builds a validly signed push for one receiver endpoint, with
// the provider's delivery header when deliveryID is set.
func receiverRequest(t *testing.T, endpoint, secret, body, deliveryID string) *http.Request {
	t.Helper()
	var req *http.Request
	if endpoint == "/webhook" {
		req = httptest.NewRequest(http.MethodPost, endpoint, bytes.NewReader([]byte(body)))
		req.Header.Set("X-Hub-Signature-256", "sha256="+computeHMAC([]byte(body), secret))
	} else {
		req = providerPushRequest(t, endpoint, secret, []byte(body))
	}
	if deliveryID != "" {
		req.Header.Set(receiverDeliveryHeader[endpoint], deliveryID)
	}
	return req
}

func decodeIgnored(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "ignored", resp["status"])
	return resp["message"]
}

// TestStandaloneWebhookIgnoresBranchDelete pins #747: a push that deletes the
// tracked branch passes the ref filter on every endpoint that reads a ref, and
// must not be forwarded.
func TestStandaloneWebhookIgnoresBranchDelete(t *testing.T) {
	const secret = "webhook-secret"
	cases := []struct {
		name string
		body string
	}{
		{name: "deleted flag", body: `{"ref":"refs/heads/main","deleted":true,"after":"` + zeroCommit + `"}`},
		{name: "zero after", body: `{"ref":"refs/heads/main","after":"` + zeroCommit + `"}`},
	}
	for _, endpoint := range []string{"/webhook", "/webhook/github", "/webhook/gitlab", "/webhook/gitea"} {
		for _, tc := range cases {
			t.Run(endpoint+" "+tc.name, func(t *testing.T) {
				client := &recordingWebhookClient{}
				mux := newWebhookMux(&webhookHandler{client: client, secret: secret, branch: "main"})

				w := httptest.NewRecorder()
				mux.ServeHTTP(w, receiverRequest(t, endpoint, secret, tc.body, ""))

				assert.Contains(t, decodeIgnored(t, w), "deletes refs/heads/main")
				assert.Equal(t, 0, client.triggerCalls)
			})
		}
		t.Run(endpoint+" delete with no known branch is still ignored", func(t *testing.T) {
			client := &recordingWebhookClient{}
			mux := newWebhookMux(&webhookHandler{client: client, secret: secret})

			w := httptest.NewRecorder()
			mux.ServeHTTP(w, receiverRequest(t, endpoint, secret, cases[0].body, ""))

			assert.Contains(t, decodeIgnored(t, w), "deletes")
			assert.Equal(t, 0, client.triggerCalls)
		})
	}

	t.Run("/webhook/bitbucket deletion stays ignored", func(t *testing.T) {
		client := &recordingWebhookClient{}
		mux := newWebhookMux(&webhookHandler{client: client, secret: secret, branch: "main"})

		body := `{"push":{"changes":[{"new":null,"old":{"type":"branch","name":"main"}}]}}`
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, receiverRequest(t, "/webhook/bitbucket", secret, body, ""))

		decodeIgnored(t, w)
		assert.Equal(t, 0, client.triggerCalls)
	})
}

// TestStandaloneWebhookDropsReplayedDelivery pins #747 on every receiver
// endpoint: a repeated delivery ID or a repeated authorized body is answered
// 200 ignored and forwarded once.
func TestStandaloneWebhookDropsReplayedDelivery(t *testing.T) {
	const secret = "webhook-secret"
	for endpoint := range receiverDeliveryHeader {
		body := receiverPushBody(endpoint, "abc123")
		otherBody := receiverPushBody(endpoint, "def456")

		t.Run(endpoint+" repeat ignored", func(t *testing.T) {
			client := &recordingWebhookClient{}
			mux := newWebhookMux(&webhookHandler{client: client, secret: secret, branch: "main"})

			first := httptest.NewRecorder()
			mux.ServeHTTP(first, receiverRequest(t, endpoint, secret, body, "d-1"))
			require.Equal(t, http.StatusAccepted, first.Code)

			replay := httptest.NewRecorder()
			mux.ServeHTTP(replay, receiverRequest(t, endpoint, secret, body, "d-1"))
			assert.Contains(t, decodeIgnored(t, replay), "Duplicate delivery")
			assert.Equal(t, 1, client.triggerCalls)

			other := httptest.NewRecorder()
			mux.ServeHTTP(other, receiverRequest(t, endpoint, secret, otherBody, "d-2"))
			assert.Equal(t, http.StatusAccepted, other.Code)
			assert.Equal(t, 2, client.triggerCalls)
		})

		t.Run(endpoint+" same body with a changed delivery header is ignored", func(t *testing.T) {
			client := &recordingWebhookClient{}
			mux := newWebhookMux(&webhookHandler{client: client, secret: secret, branch: "main"})

			first := httptest.NewRecorder()
			mux.ServeHTTP(first, receiverRequest(t, endpoint, secret, body, "d-1"))
			require.Equal(t, http.StatusAccepted, first.Code)

			for _, id := range []string{"d-forged", ""} {
				replay := httptest.NewRecorder()
				mux.ServeHTTP(replay, receiverRequest(t, endpoint, secret, body, id))
				assert.Contains(t, decodeIgnored(t, replay), "Duplicate delivery")
			}
			assert.Equal(t, 1, client.triggerCalls)
		})

		t.Run(endpoint+" different bodies to the same branch are accepted", func(t *testing.T) {
			client := &recordingWebhookClient{}
			mux := newWebhookMux(&webhookHandler{client: client, secret: secret, branch: "main"})

			for _, b := range []string{body, otherBody} {
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, receiverRequest(t, endpoint, secret, b, ""))
				require.Equal(t, http.StatusAccepted, w.Code)
			}
			assert.Equal(t, 2, client.triggerCalls)
		})

		t.Run(endpoint+" bad signature on a recorded id is rejected", func(t *testing.T) {
			client := &recordingWebhookClient{}
			mux := newWebhookMux(&webhookHandler{client: client, secret: secret, branch: "main"})

			first := httptest.NewRecorder()
			mux.ServeHTTP(first, receiverRequest(t, endpoint, secret, body, "d-1"))
			require.Equal(t, http.StatusAccepted, first.Code)

			forged := httptest.NewRecorder()
			mux.ServeHTTP(forged, receiverRequest(t, endpoint, "wrong-secret", body, "d-1"))
			assert.Equal(t, http.StatusUnauthorized, forged.Code)
		})

		for _, retryID := range []string{"d-1", ""} {
			t.Run(endpoint+" failed forward is retried (retry delivery id "+retryID+")", func(t *testing.T) {
				client := &failingOnceWebhookClient{}
				mux := newWebhookMux(&webhookHandler{client: client, secret: secret, branch: "main"})

				failed := httptest.NewRecorder()
				mux.ServeHTTP(failed, receiverRequest(t, endpoint, secret, body, "d-1"))
				require.Equal(t, http.StatusBadGateway, failed.Code)

				retry := httptest.NewRecorder()
				mux.ServeHTTP(retry, receiverRequest(t, endpoint, secret, body, retryID))
				assert.Equal(t, http.StatusAccepted, retry.Code)
				assert.Equal(t, 2, client.triggerCalls)
			})
		}
	}
}

// failingOnceWebhookClient fails its first Trigger and accepts the rest.
type failingOnceWebhookClient struct {
	recordingWebhookClient
}

func (c *failingOnceWebhookClient) Trigger(ctx context.Context, source string, force bool) (*daemon.TriggerResponse, error) {
	resp, err := c.recordingWebhookClient.Trigger(ctx, source, force)
	if c.triggerCalls == 1 {
		return nil, errors.New("daemon unreachable")
	}
	return resp, err
}

// TestStandaloneGenericWebhookEmptyBodyIsNotDeduplicated keeps the receiver's
// plain signed trigger working: an empty body has no body key.
func TestStandaloneGenericWebhookEmptyBodyIsNotDeduplicated(t *testing.T) {
	const secret = "webhook-secret"
	client := &recordingWebhookClient{}
	mux := newWebhookMux(&webhookHandler{client: client, secret: secret, branch: "main"})

	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, receiverRequest(t, "/webhook", secret, "", ""))
		require.Equal(t, http.StatusAccepted, w.Code)
	}
	assert.Equal(t, 2, client.triggerCalls)
}

// TestStandaloneBitbucketDeletionIgnoredWithoutBranch covers the receiver with
// no tracked branch: a push whose every change deletes its ref is not
// forwarded (#753 review N1).
func TestStandaloneBitbucketDeletionIgnoredWithoutBranch(t *testing.T) {
	const secret = "webhook-secret"
	cases := []struct {
		name      string
		body      string
		wantCalls int
	}{
		{name: "only deletions ignored", body: `{"push":{"changes":[{"new":null,"old":{"type":"branch","name":"main"}}]}}`},
		{name: "deletion plus update forwards", body: `{"push":{"changes":[{"new":null},{"new":{"type":"branch","name":"develop"}}]}}`, wantCalls: 1},
		{name: "no changes forwards", body: `{"push":{"changes":[]}}`, wantCalls: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := &recordingWebhookClient{}
			mux := newWebhookMux(&webhookHandler{client: client, secret: secret})

			w := httptest.NewRecorder()
			mux.ServeHTTP(w, receiverRequest(t, "/webhook/bitbucket", secret, tc.body, ""))

			assert.Equal(t, tc.wantCalls, client.triggerCalls)
			if tc.wantCalls == 0 {
				assert.Contains(t, decodeIgnored(t, w), "deletes")
			}
		})
	}
}

// TestDuplicateDeliveryNamesTheMatchedKey pins #753 review N2: the response
// says which replay key matched.
func TestDuplicateDeliveryNamesTheMatchedKey(t *testing.T) {
	const secret = "webhook-secret"
	client := &recordingWebhookClient{}
	mux := newWebhookMux(&webhookHandler{client: client, secret: secret, branch: "main"})
	body := receiverPushBody("/webhook/github", "abc123")

	first := httptest.NewRecorder()
	mux.ServeHTTP(first, receiverRequest(t, "/webhook/github", secret, body, "d-1"))
	require.Equal(t, http.StatusAccepted, first.Code)

	byID := httptest.NewRecorder()
	mux.ServeHTTP(byID, receiverRequest(t, "/webhook/github", secret, receiverPushBody("/webhook/github", "def456"), "d-1"))
	assert.Contains(t, decodeIgnored(t, byID), "d-1")

	byBody := httptest.NewRecorder()
	mux.ServeHTTP(byBody, receiverRequest(t, "/webhook/github", secret, body, "d-9"))
	assert.Contains(t, decodeIgnored(t, byBody), "request body already handled")
}
