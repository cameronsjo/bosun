package daemon

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func prepareSocketAuthTest(t *testing.T) (*SocketServer, *Daemon) {
	t.Helper()
	server, daemon := newTestSocketServer(t)
	daemon.reconcileMu.Lock()
	daemon.reconciling = true
	daemon.reconcileMu.Unlock()
	t.Cleanup(func() {
		daemon.reconcileMu.Lock()
		daemon.reconciling = false
		daemon.clearPendingTriggers()
		daemon.reconcileMu.Unlock()
	})
	return server, daemon
}

func assertNoPendingSocketTrigger(t *testing.T, daemon *Daemon) {
	t.Helper()
	assert.Never(t, func() bool {
		daemon.reconcileMu.Lock()
		defer daemon.reconcileMu.Unlock()
		return daemon.pendingTriggerCount != 0
	}, 75*time.Millisecond, 5*time.Millisecond)
}

func TestSocketPeerCredentialAuthorization(t *testing.T) {
	t.Run("configured UID is accepted", func(t *testing.T) {
		server, daemon := prepareSocketAuthTest(t)
		req := withTestSocketPeer(httptest.NewRequest(http.MethodPost, "/trigger", nil))
		w := httptest.NewRecorder()

		server.handleTrigger(w, req)

		assert.Equal(t, http.StatusAccepted, w.Code)
		require.Eventually(t, func() bool {
			daemon.reconcileMu.Lock()
			defer daemon.reconcileMu.Unlock()
			return daemon.pendingTriggerCount == 1
		}, 200*time.Millisecond, 5*time.Millisecond)
	})

	t.Run("daemon owner UID is accepted", func(t *testing.T) {
		server, daemon := prepareSocketAuthTest(t)
		if !server.ownerUIDAvailable {
			t.Skip("effective UID unavailable on this platform")
		}
		req := withSocketPeer(
			httptest.NewRequest(http.MethodPost, "/trigger", nil),
			peerCredentials{UID: server.ownerUID, GID: 100, PID: 1234},
		)
		w := httptest.NewRecorder()

		server.handleTrigger(w, req)

		assert.Equal(t, http.StatusAccepted, w.Code)
		require.Eventually(t, func() bool {
			daemon.reconcileMu.Lock()
			defer daemon.reconcileMu.Unlock()
			return daemon.pendingTriggerCount == 1
		}, 200*time.Millisecond, 5*time.Millisecond)
	})

	t.Run("non-allowlisted UID is denied before body parsing", func(t *testing.T) {
		server, daemon := prepareSocketAuthTest(t)
		unauthorizedUID := testSocketUID + 1
		if server.ownerUIDAvailable && unauthorizedUID == server.ownerUID {
			unauthorizedUID++
		}
		req := withSocketPeer(
			httptest.NewRequest(http.MethodPost, "/trigger", strings.NewReader("{not-json")),
			peerCredentials{UID: unauthorizedUID, GID: 100, PID: 1234},
		)
		w := httptest.NewRecorder()

		server.handleTrigger(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assertNoPendingSocketTrigger(t, daemon)
	})

	t.Run("missing credentials fail closed", func(t *testing.T) {
		server, daemon := prepareSocketAuthTest(t)
		req := httptest.NewRequest(http.MethodPost, "/trigger", nil)
		w := httptest.NewRecorder()

		server.handleTrigger(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
		assertNoPendingSocketTrigger(t, daemon)
	})

	t.Run("explicit opt-out permits missing credentials", func(t *testing.T) {
		server, daemon := prepareSocketAuthTest(t)
		server.allowUnauthenticatedMutation = true
		req := httptest.NewRequest(http.MethodPost, "/trigger", nil)
		w := httptest.NewRecorder()

		server.handleTrigger(w, req)

		assert.Equal(t, http.StatusAccepted, w.Code)
		require.Eventually(t, func() bool {
			daemon.reconcileMu.Lock()
			defer daemon.reconcileMu.Unlock()
			return daemon.pendingTriggerCount == 1
		}, 200*time.Millisecond, 5*time.Millisecond)
	})
}

func TestSocketPeerCredentialAuthorizationKeepsReadOnlyEndpointsAvailable(t *testing.T) {
	server, _ := newTestSocketServer(t)

	for _, path := range []string{"/status", "/health"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			w := httptest.NewRecorder()

			server.httpServer.Handler.ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
		})
	}

	t.Run("non-mutating trigger method remains method-not-allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/trigger", nil)
		w := httptest.NewRecorder()

		server.httpServer.Handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})
}

func TestSocketPeerInfoUsesStructuredCredentials(t *testing.T) {
	req := withSocketPeer(
		httptest.NewRequest(http.MethodGet, "/status", nil),
		peerCredentials{UID: 1001, GID: 1002, PID: 1003},
	)

	assert.Equal(t, "uid=1001,gid=1002,pid=1003", getPeerInfo(req))
}

func TestWarnSocketAuthPosture(t *testing.T) {
	t.Run("explicit opt-out names unauthenticated exposure", func(t *testing.T) {
		d := &Daemon{config: DefaultConfig()}
		d.config.AllowUnauthenticatedSocket = true
		var buf bytes.Buffer

		d.warnSocketAuthPosture(zerolog.New(&buf))

		assert.Contains(t, buf.String(), "UNAUTHENTICATED mutating requests")
		assert.Contains(t, buf.String(), "BOSUN_ALLOW_UNAUTHENTICATED_SOCKET=true")
	})

	t.Run("unsupported platform names fail-closed posture", func(t *testing.T) {
		if peerCredentialSupportAvailable() {
			t.Skip("peer credentials are supported on this platform")
		}
		d := &Daemon{config: DefaultConfig()}
		var buf bytes.Buffer

		d.warnSocketAuthPosture(zerolog.New(&buf))

		assert.Contains(t, buf.String(), "REJECTED")
		assert.Contains(t, buf.String(), "peer credentials are unavailable")
	})
}
