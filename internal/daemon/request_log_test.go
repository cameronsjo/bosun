package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cameronsjo/bosun/internal/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureRequestLog drives the logging middleware and returns the parsed
// "HTTP request completed" entry.
//
// This exists because the unit tests for forwardedForClient cannot go red on
// the middleware wiring: deleting the remote_addr field, or the
// trustedProxies assignment in NewServer, leaves them all passing.
func captureRequestLog(t *testing.T, trusted *trustedProxies, req *http.Request) map[string]any {
	t.Helper()

	var buf bytes.Buffer
	log.Init(&log.Options{Format: log.FormatJSON, Output: &buf})
	t.Cleanup(func() { log.Init(&log.Options{}) })

	s := &Server{trustedProxies: trusted}
	handler := s.loggingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), req)

	var entry map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
		var candidate map[string]any
		if json.Unmarshal(line, &candidate) != nil {
			continue
		}
		if candidate["message"] == "HTTP request completed" {
			entry = candidate
		}
	}
	require.NotNil(t, entry, "middleware emitted no completion line; log was: %s", buf.String())
	return entry
}

func TestRequestLogAlwaysCarriesRemoteAddr(t *testing.T) {
	req := requestFrom("192.168.1.50:41234", "")
	entry := captureRequestLog(t, mustParseProxies(t), req)

	assert.Equal(t, "192.168.1.50:41234", entry["remote_addr"],
		"remote_addr is unconditional: it is the observed peer, not a claim")
	assert.NotContains(t, entry, "forwarded_for")
}

// TestRequestLogAttributesUnmatchedPath is the case the incident needed: a 404
// on a path the daemon never registered, and no way to say who sent it.
func TestRequestLogAttributesUnmatchedPath(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/webhook/github-push", nil)
	req.RemoteAddr = "172.18.0.9:55001"

	entry := captureRequestLog(t, mustParseProxies(t), req)

	assert.Equal(t, "/webhook/github-push", entry["url"])
	assert.Equal(t, float64(http.StatusNotFound), entry["status"])
	assert.Equal(t, "172.18.0.9:55001", entry["remote_addr"])
}

func TestRequestLogForwardedFor(t *testing.T) {
	trusted := mustParseProxies(t, "172.18.0.5")

	t.Run("trusted proxy contributes it", func(t *testing.T) {
		entry := captureRequestLog(t, trusted, requestFrom("172.18.0.5:41234", "203.0.113.9, 198.51.100.4"))
		assert.Equal(t, "172.18.0.5:41234", entry["remote_addr"], "the observed peer is never replaced")
		assert.Equal(t, "203.0.113.9", entry["forwarded_for"])
	})

	t.Run("untrusted peer's header is not recorded", func(t *testing.T) {
		entry := captureRequestLog(t, trusted, requestFrom("10.99.99.99:41234", "203.0.113.9"))
		assert.Equal(t, "10.99.99.99:41234", entry["remote_addr"])
		assert.NotContains(t, entry, "forwarded_for",
			"any host on the bridge can send this header; believing it would misattribute the sender")
	})

	t.Run("no proxies configured means nobody is believed", func(t *testing.T) {
		entry := captureRequestLog(t, mustParseProxies(t), requestFrom("172.18.0.5:41234", "203.0.113.9"))
		assert.NotContains(t, entry, "forwarded_for")
	})
}

// TestRequestLogSanitizesRequestID pins the caller-supplied header boundary.
// The value flows into every log line for the request.
func TestRequestLogSanitizesRequestID(t *testing.T) {
	req := requestFrom("192.168.1.50:41234", "")
	req.Header.Set("X-Request-ID", "abc\x00\ndef")

	entry := captureRequestLog(t, mustParseProxies(t), req)
	// Assert the exact expected value. A comma-ok that discards the failure
	// yields "" for a missing field, and NotContains passes over "" -- deleting
	// the sanitization call would leave this test green.
	requestID, ok := entry["request_id"].(string)
	require.True(t, ok, "request_id must be present and a string; got %#v", entry["request_id"])
	assert.Equal(t, "abcdef", requestID,
		"control characters are stripped, the rest of the caller's value is preserved")
}
