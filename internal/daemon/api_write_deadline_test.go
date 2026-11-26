package daemon

import (
	"context"
	"encoding/json"
	"math"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cameronsjo/bosun/internal/docker/dockertest"
)

func TestAPIWriteDeadlineFor(t *testing.T) {
	tests := []struct {
		name       string
		apiTimeout time.Duration
		factor     int64
		want       time.Duration
	}{
		{name: "zero timeout keeps the server floor", apiTimeout: 0, factor: 1, want: 30 * time.Second},
		{name: "negative timeout keeps the server floor", apiTimeout: -time.Second, factor: 2, want: 30 * time.Second},
		{name: "small budget never shortens the server floor", apiTimeout: time.Second, factor: 2, want: 30 * time.Second},
		{name: "five seconds at factor 1 stays at the floor", apiTimeout: 5 * time.Second, factor: 1, want: 30 * time.Second},
		{name: "default timeout at factor 1 adds the margin", apiTimeout: 30 * time.Second, factor: 1, want: 35 * time.Second},
		{name: "default timeout at factor 2 covers restart", apiTimeout: 30 * time.Second, factor: 2, want: 65 * time.Second},
		{name: "huge timeout is capped at the ceiling", apiTimeout: 24 * time.Hour, factor: 2, want: 10 * time.Minute},
		{name: "overflowing product is capped, not wrapped", apiTimeout: time.Duration(math.MaxInt64/2 + 1), factor: 2, want: 10 * time.Minute},
		{name: "maximum duration is capped", apiTimeout: time.Duration(math.MaxInt64), factor: 1, want: 10 * time.Minute},
		{name: "ceiling itself is capped", apiTimeout: 10 * time.Minute, factor: 1, want: 10 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, apiWriteDeadlineFor(tt.apiTimeout, tt.factor))
		})
	}
}

// serveTCPAPI serves the TCP server's real http.Server (auth middleware, audit
// wrapper and all) on a loopback port and returns its base URL.
func serveTCPAPI(t *testing.T, d *Daemon, writeTimeout time.Duration) string {
	t.Helper()

	tcp, err := NewTCPServer(d, "127.0.0.1:0", "tok")
	require.NoError(t, err)
	tcp.httpServer.WriteTimeout = writeTimeout

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = tcp.httpServer.Serve(ln) }()
	t.Cleanup(func() { _ = tcp.httpServer.Close() })

	return "http://" + ln.Addr().String()
}

func postRestart(t *testing.T, baseURL string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, baseURL+"/api/containers/web/restart", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer tok")
	httpClient := &http.Client{Timeout: 10 * time.Second}
	return httpClient.Do(req)
}

// TestTCPAPIRestart_SlowHandlerStillDeliversResponse is the handler-level pin
// for the TCP write deadline. The server's own write deadline is shortened to
// 300ms and the restart takes 600ms, which is the shape of a restart that runs
// longer than the server's 30s: the handler stays inside its own context budget
// but outlives the server deadline. The response must still arrive.
func TestTCPAPIRestart_SlowHandlerStillDeliversResponse(t *testing.T) {
	tests := []struct {
		name         string
		restartDelay time.Duration
	}{
		{name: "handler faster than the server deadline", restartDelay: 0},
		{name: "handler slower than the server deadline", restartDelay: 600 * time.Millisecond},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := dockertest.NewMockDockerAPI()
			mock.ContainerRestartFunc = func(_ context.Context, _ string, _ client.ContainerRestartOptions) (client.ContainerRestartResult, error) {
				time.Sleep(tt.restartDelay)
				return client.ContainerRestartResult{}, nil
			}
			d := newDockerDaemon(t, mock)
			baseURL := serveTCPAPI(t, d, 300*time.Millisecond)

			resp, err := postRestart(t, baseURL)

			require.NoError(t, err, "the response must be written before the connection is cut")
			defer func() { _ = resp.Body.Close() }()
			assert.Equal(t, http.StatusOK, resp.StatusCode)
			var body APIRestartResponse
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
			assert.Equal(t, "success", body.Status)
		})
	}
}

// TestTCPServer_NonAPIRoutesKeepServerWriteDeadline pins the other half of the
// per-handler design: only the API routes extend their deadline, so the server
// default is still 30s for every other route.
func TestTCPServer_NonAPIRoutesKeepServerWriteDeadline(t *testing.T) {
	d, _ := newTestDaemon(t)
	tcp, err := NewTCPServer(d, "127.0.0.1:0", "tok")
	require.NoError(t, err)

	assert.Equal(t, 30*time.Second, tcp.httpServer.WriteTimeout)
}
