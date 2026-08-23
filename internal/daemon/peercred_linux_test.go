//go:build linux

package daemon

import (
	"context"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This exercises the real listener wrapper and SO_PEERCRED path. Unit tests
// inject structured credentials directly so denial behavior remains testable
// on platforms where the kernel API is unavailable.
func TestLinuxSocketPeerCredentialsAuthorizeDaemonOwner(t *testing.T) {
	server, _ := prepareSocketAuthTest(t)
	startErr := make(chan error, 1)
	go func() { startErr <- server.Start() }()

	require.Eventually(t, func() bool {
		_, err := os.Lstat(server.socketPath)
		return err == nil
	}, 2*time.Second, 10*time.Millisecond)

	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", server.socketPath)
		},
	}
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	resp, err := client.Post("http://unix/trigger", "application/json", nil)
	require.NoError(t, err)
	assert.Equal(t, http.StatusAccepted, resp.StatusCode)
	require.NoError(t, resp.Body.Close())
	transport.CloseIdleConnections()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, server.Shutdown(ctx))
	select {
	case err := <-startErr:
		assert.ErrorIs(t, err, http.ErrServerClosed)
	case <-time.After(2 * time.Second):
		t.Fatal("socket server did not stop after shutdown")
	}
}
