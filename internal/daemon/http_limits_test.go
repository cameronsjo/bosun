package daemon

import (
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDaemonHTTPServersBoundRequestHeaders(t *testing.T) {
	d, webhook := newTestDaemon(t)

	socket, err := NewSocketServer(d, &SocketConfig{
		SocketPath: filepath.Join(t.TempDir(), "bosun.sock"),
		SocketMode: 0660,
	})
	require.NoError(t, err)

	tcp, err := NewTCPServer(d, "127.0.0.1:0", "test-token")
	require.NoError(t, err)

	tests := []struct {
		name        string
		server      *http.Server
		idleTimeout time.Duration
	}{
		{name: "webhook", server: webhook.server, idleTimeout: 60 * time.Second},
		{name: "Unix socket", server: socket.httpServer},
		{name: "TCP", server: tcp.httpServer},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, 5*time.Second, tt.server.ReadHeaderTimeout)
			assert.Equal(t, 32<<10, tt.server.MaxHeaderBytes)
			assert.Equal(t, 10*time.Second, tt.server.ReadTimeout)
			assert.Equal(t, 30*time.Second, tt.server.WriteTimeout)
			assert.Equal(t, tt.idleTimeout, tt.server.IdleTimeout)
		})
	}

	// Handler execution remains independently bounded by BOSUN_API_TIMEOUT.
	assert.Equal(t, 30*time.Second, d.config.APITimeout)
}
