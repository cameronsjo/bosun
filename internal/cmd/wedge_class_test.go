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
// webhook receiver caps request-body size like the daemon's own handlers do,
// instead of reading an unbounded body into memory.
func TestWebhookHandlers_RejectOversizedBody(t *testing.T) {
	h := &webhookHandler{secret: ""} // no secret: would otherwise forward to a nil client

	oversized := bytes.Repeat([]byte("a"), maxWebhookBodySize+1)

	cases := []struct {
		name string
		path string
		fn   func(http.ResponseWriter, *http.Request)
	}{
		{"generic", "/webhook", h.handleWebhook},
		{"github", "/webhook/github", h.handleGitHubWebhook},
		{"gitlab", "/webhook/gitlab", h.handleGitLabWebhook},
		{"gitea", "/webhook/gitea", h.handleGiteaWebhook},
		{"bitbucket", "/webhook/bitbucket", h.handleBitbucketWebhook},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, c.path, bytes.NewReader(oversized))
			rec := httptest.NewRecorder()

			c.fn(rec, req)

			assert.GreaterOrEqualf(t, rec.Code, 400, "oversized body must be rejected, got %d", rec.Code)
			assert.Lessf(t, rec.Code, 500, "rejection should be a client error, got %d", rec.Code)
		})
	}
}
