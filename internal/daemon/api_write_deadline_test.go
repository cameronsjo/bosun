package daemon

import (
	"bytes"
	"context"
	"io"
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

func doAPIRequest(t *testing.T, baseURL, method, path string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, baseURL+path, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer tok")
	httpClient := &http.Client{Timeout: 10 * time.Second}
	return httpClient.Do(req)
}

// TestTCPAPI_RoutesExtendWriteDeadline pins, per route, that the handler
// extends its own response deadline and by which factor. The numbers are
// shrunk through Daemon.apiWriteDeadlines so the test runs in about two
// seconds:
//
//	server write deadline  300ms
//	BOSUN_API_TIMEOUT      1s
//	floor 50ms, margin 50ms  =>  factor 1 extends to 1.05s, factor 2 to 2.05s
//
// A handler slower than the server deadline delivers its response only if it
// called the helper. Restart's handler runs 1.5s, between the factor-1 and the
// factor-2 deadline, so it also fails if its factor is wrong. Each route is
// exercised alone, so deleting one route's call fails only that route's row.
func TestTCPAPI_RoutesExtendWriteDeadline(t *testing.T) {
	tests := []struct {
		name      string
		method    string
		path      string
		delay     time.Duration
		configure func(m *dockertest.MockDockerAPI, delay time.Duration)
	}{
		{
			name:   "container list, slower than the server deadline",
			method: http.MethodGet,
			path:   "/api/containers",
			delay:  600 * time.Millisecond,
			configure: func(m *dockertest.MockDockerAPI, delay time.Duration) {
				m.ContainerListFunc = func(_ context.Context, _ client.ContainerListOptions) (client.ContainerListResult, error) {
					time.Sleep(delay)
					return client.ContainerListResult{}, nil
				}
			},
		},
		{
			name:   "container logs, slower than the server deadline",
			method: http.MethodGet,
			path:   "/api/containers/web/logs",
			delay:  600 * time.Millisecond,
			configure: func(m *dockertest.MockDockerAPI, delay time.Duration) {
				m.ContainerLogsFunc = func(_ context.Context, _ string, _ client.ContainerLogsOptions) (client.ContainerLogsResult, error) {
					time.Sleep(delay)
					return io.NopCloser(bytes.NewBufferString("line1\n")), nil
				}
			},
		},
		{
			name:   "container restart, between the factor 1 and factor 2 deadlines",
			method: http.MethodPost,
			path:   "/api/containers/web/restart",
			delay:  1500 * time.Millisecond,
			configure: func(m *dockertest.MockDockerAPI, delay time.Duration) {
				m.ContainerRestartFunc = func(_ context.Context, _ string, _ client.ContainerRestartOptions) (client.ContainerRestartResult, error) {
					time.Sleep(delay)
					return client.ContainerRestartResult{}, nil
				}
			},
		},
		{
			name:   "container restart, faster than the server deadline (control)",
			method: http.MethodPost,
			path:   "/api/containers/web/restart",
			delay:  0,
			configure: func(m *dockertest.MockDockerAPI, delay time.Duration) {
				m.ContainerRestartFunc = func(_ context.Context, _ string, _ client.ContainerRestartOptions) (client.ContainerRestartResult, error) {
					time.Sleep(delay)
					return client.ContainerRestartResult{}, nil
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := dockertest.NewMockDockerAPI()
			tt.configure(mock, tt.delay)
			d := newDockerDaemon(t, mock)
			d.config.APITimeout = time.Second
			d.apiWriteDeadlines = &apiWriteDeadlinePolicy{
				floor:   50 * time.Millisecond,
				margin:  50 * time.Millisecond,
				ceiling: 10 * time.Second,
			}
			baseURL := serveTCPAPI(t, d, 300*time.Millisecond)

			resp, err := doAPIRequest(t, baseURL, tt.method, tt.path)

			require.NoError(t, err, "the response must be written before the connection is cut")
			defer func() { _ = resp.Body.Close() }()
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err, "the whole response body must arrive")
			assert.Equal(t, http.StatusOK, resp.StatusCode, string(body))
		})
	}
}

// TestNewTCPServer_ServerLimits pins the server-level limits the docs state:
// the write deadline stays 30s (only the three API routes extend their own), the
// read deadline is 10s, and IdleTimeout is unset, so net/http uses ReadTimeout
// as the idle bound. A change to any of them has to change docs/security.md too.
// Whether a given route extends its deadline is pinned by
// TestTCPAPI_RoutesExtendWriteDeadline, not here.
func TestNewTCPServer_ServerLimits(t *testing.T) {
	d, _ := newTestDaemon(t)
	tcp, err := NewTCPServer(d, "127.0.0.1:0", "tok")
	require.NoError(t, err)

	assert.Equal(t, 30*time.Second, tcp.httpServer.WriteTimeout)
	assert.Equal(t, 10*time.Second, tcp.httpServer.ReadTimeout)
	assert.Zero(t, tcp.httpServer.IdleTimeout)
}
