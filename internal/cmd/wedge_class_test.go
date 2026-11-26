package cmd

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunComposeUp_HonorsCancelledContext proves the disaster-recovery compose-up
// honors context cancellation rather than running unbounded (sibling of #319).
func TestRunComposeUp_HonorsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled before the call

	err := runComposeUp(ctx, filepath.Join(t.TempDir(), "core.yml"))

	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

// TestRunComposeUp_ReturnsErrorOnMissingComposeFile exercises the exec path
// (timeout wrap + CommandContext + Run) with a live context: a nonexistent
// compose file makes `docker compose up` fail fast, so the call returns an error
// promptly rather than hanging — and does so regardless of Docker availability.
func TestRunComposeUp_ReturnsErrorOnMissingComposeFile(t *testing.T) {
	done := make(chan error, 1)
	go func() {
		done <- runComposeUp(context.Background(), filepath.Join(t.TempDir(), "does-not-exist.yml"))
	}()

	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(30 * time.Second):
		t.Fatal("runComposeUp did not return promptly for a missing compose file")
	}
}

// TestWebhookHandlers_RejectOversizedBody proves the standalone (tunnel-exposed)
// webhook receiver refuses a body past the daemon's limit with 413 and
// forwards nothing (#794). Each mode must get past the receiver's fail-closed
// gate, which runs before the body read; the fail-closed row pins that order.
func TestWebhookHandlers_RejectOversizedBody(t *testing.T) {
	oversized := bytes.Repeat([]byte("a"), maxWebhookBodySize+1)

	endpoints := []struct {
		name string
		path string
		fn   func(*webhookHandler) http.HandlerFunc
	}{
		{"generic", "/webhook", func(h *webhookHandler) http.HandlerFunc { return h.handleWebhook }},
		{"github", "/webhook/github", func(h *webhookHandler) http.HandlerFunc { return h.handleGitHubWebhook }},
		{"gitlab", "/webhook/gitlab", func(h *webhookHandler) http.HandlerFunc { return h.handleGitLabWebhook }},
		{"gitea", "/webhook/gitea", func(h *webhookHandler) http.HandlerFunc { return h.handleGiteaWebhook }},
		{"bitbucket", "/webhook/bitbucket", func(h *webhookHandler) http.HandlerFunc { return h.handleBitbucketWebhook }},
	}
	modes := []struct {
		name     string
		handler  func(*recordingWebhookClient) *webhookHandler
		wantCode int
	}{
		{"unauthenticated opt-out", func(c *recordingWebhookClient) *webhookHandler {
			return &webhookHandler{client: c, allowUnauthenticated: true}
		}, http.StatusRequestEntityTooLarge},
		{"secret configured", func(c *recordingWebhookClient) *webhookHandler {
			return &webhookHandler{client: c, secret: "oversize-secret"}
		}, http.StatusRequestEntityTooLarge},
		{"fail-closed no secret", func(c *recordingWebhookClient) *webhookHandler {
			return &webhookHandler{client: c}
		}, http.StatusForbidden},
	}

	for _, ep := range endpoints {
		for _, mode := range modes {
			t.Run(ep.name+"/"+mode.name, func(t *testing.T) {
				client := &recordingWebhookClient{}
				h := mode.handler(client)
				req := httptest.NewRequest(http.MethodPost, ep.path, bytes.NewReader(oversized))
				rec := httptest.NewRecorder()

				ep.fn(h)(rec, req)

				assert.Equal(t, mode.wantCode, rec.Code)
				assert.Zero(t, client.triggerCalls, "an oversized body must not be forwarded to the daemon")
			})
		}
	}
}
