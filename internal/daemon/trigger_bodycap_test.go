package daemon

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cameronsjo/bosun/internal/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// oversizeBody returns a syntactically-plausible JSON body larger than the
// 64 KiB trigger cap, so the decoder hits http.MaxBytesReader's limit.
func oversizeBody() string {
	return `{"source":"` + strings.Repeat("a", maxTriggerBodyBytes+1024) + `"}`
}

func TestDecodeTriggerRequest(t *testing.T) {
	t.Run("oversized body maps to 413, not 400 or silent truncation", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/trigger", strings.NewReader(oversizeBody()))
		w := httptest.NewRecorder()

		_, ok := decodeTriggerRequest(w, req)

		assert.False(t, ok)
		assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
	})

	t.Run("malformed JSON maps to 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/trigger", strings.NewReader(`{not json`))
		w := httptest.NewRecorder()

		_, ok := decodeTriggerRequest(w, req)

		assert.False(t, ok)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("small valid body decodes", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/trigger", strings.NewReader(`{"source":"ci","force":true}`))
		w := httptest.NewRecorder()

		got, ok := decodeTriggerRequest(w, req)

		require.True(t, ok)
		assert.Equal(t, "ci", got.Source)
		assert.True(t, got.Force)
	})

	t.Run("empty body is valid", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/trigger", nil)
		w := httptest.NewRecorder()

		_, ok := decodeTriggerRequest(w, req)

		assert.True(t, ok)
	})
}

// TestTriggerBodyCapPerSite proves each of the three trigger decode sites
// (api/socket/tcp) returns 413 for an oversized body. A 413 returns before any
// reconcile goroutine spawns, so no daemon plumbing or cleanup is needed for tcp.
func TestTriggerBodyCapPerSite(t *testing.T) {
	t.Run("api /api/trigger", func(t *testing.T) {
		cfg := &Config{
			SocketPath:       "/tmp/test-bodycap-api.sock",
			ReconcileConfig:  reconcile.DefaultConfig(),
			ReconcileTimeout: 50 * time.Millisecond,
		}
		cfg.ReconcileConfig.RepoURL = "https://github.com/test/repo"
		cfg.ReconcileConfig.DryRun = true
		d, err := New(cfg)
		require.NoError(t, err)

		mux := http.NewServeMux()
		d.RegisterAPIRoutes(mux)

		req := httptest.NewRequest(http.MethodPost, "/api/trigger", strings.NewReader(oversizeBody()))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
	})

	t.Run("socket /trigger", func(t *testing.T) {
		ss, _ := newTestSocketServer(t)
		req := httptest.NewRequest(http.MethodPost, "/trigger", strings.NewReader(oversizeBody()))
		w := httptest.NewRecorder()
		ss.handleTrigger(w, withTestSocketPeer(req))

		assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
	})

	t.Run("tcp /trigger", func(t *testing.T) {
		server := &TCPServer{}
		req := httptest.NewRequest(http.MethodPost, "/trigger", strings.NewReader(oversizeBody()))
		w := httptest.NewRecorder()
		server.handleTrigger(w, req)

		assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
	})
}
