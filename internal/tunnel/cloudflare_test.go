package tunnel

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCloudflare(t *testing.T) {
	cf, err := NewCloudflare()
	if err != nil {
		// Expected if cloudflared is not installed
		notInstalled, ok := err.(ErrNotInstalled)
		assert.True(t, ok, "expected ErrNotInstalled error")
		assert.Contains(t, notInstalled.Provider, "Cloudflare")
		return
	}

	assert.NotNil(t, cf)
	assert.NotEmpty(t, cf.binaryPath)
}

func TestNewCloudflareWithConfig(t *testing.T) {
	config := CloudflareConfig{
		TunnelName:     "my-tunnel",
		Hostname:       "myhost.example.com",
		HealthEndpoint: "https://health.example.com",
		HealthTimeout:  10 * time.Second,
	}

	cf, err := NewCloudflareWithConfig(config)
	if err != nil {
		// Expected if cloudflared is not installed
		_, ok := err.(ErrNotInstalled)
		assert.True(t, ok, "expected ErrNotInstalled error")
		return
	}

	assert.NotNil(t, cf)
	assert.Equal(t, "my-tunnel", cf.config.TunnelName)
	assert.Equal(t, "myhost.example.com", cf.config.Hostname)
	assert.Equal(t, "https://health.example.com", cf.config.HealthEndpoint)
	assert.Equal(t, 10*time.Second, cf.config.HealthTimeout)
}

func TestNewCloudflareWithPath(t *testing.T) {
	config := CloudflareConfig{
		Hostname: "test.example.com",
	}
	cf := NewCloudflareWithPath("/custom/path/cloudflared", config)
	assert.NotNil(t, cf)
	assert.Equal(t, "/custom/path/cloudflared", cf.binaryPath)
	assert.Equal(t, DefaultHealthTimeout, cf.config.HealthTimeout)
}

func TestCloudflare_Name(t *testing.T) {
	cf := NewCloudflareWithPath("/bin/cloudflared", CloudflareConfig{})
	assert.Equal(t, "cloudflare", cf.Name())
}

func TestCloudflare_GetHostname(t *testing.T) {
	config := CloudflareConfig{
		Hostname: "test.example.com",
	}
	cf := NewCloudflareWithPath("/bin/cloudflared", config)
	assert.Equal(t, "test.example.com", cf.GetHostname())
}

func TestCloudflare_Status_InvalidBinary(t *testing.T) {
	cf := NewCloudflareWithPath("/nonexistent/cloudflared", CloudflareConfig{})
	ctx := context.Background()

	status, err := cf.Status(ctx)
	// Should not error, just return disconnected status
	assert.NoError(t, err)
	assert.NotNil(t, status)
	assert.False(t, status.Connected)
}

func TestCloudflare_IsConnected_InvalidBinary(t *testing.T) {
	cf := NewCloudflareWithPath("/nonexistent/cloudflared", CloudflareConfig{})
	ctx := context.Background()

	connected := cf.IsConnected(ctx)
	assert.False(t, connected)
}

func TestCloudflare_CheckHealthEndpoint(t *testing.T) {
	t.Run("healthy endpoint", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		config := CloudflareConfig{
			HealthEndpoint: server.URL,
			HealthTimeout:  5 * time.Second,
		}
		cf := NewCloudflareWithPath("/nonexistent/cloudflared", config)

		ctx := context.Background()
		connected := cf.checkHealthEndpoint(ctx)
		assert.True(t, connected)
	})

	t.Run("unhealthy endpoint", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer server.Close()

		config := CloudflareConfig{
			HealthEndpoint: server.URL,
			HealthTimeout:  5 * time.Second,
		}
		cf := NewCloudflareWithPath("/nonexistent/cloudflared", config)

		ctx := context.Background()
		connected := cf.checkHealthEndpoint(ctx)
		assert.False(t, connected)
	})

	t.Run("unreachable endpoint", func(t *testing.T) {
		config := CloudflareConfig{
			HealthEndpoint: "http://127.0.0.1:59999/health",
			HealthTimeout:  1 * time.Second,
		}
		cf := NewCloudflareWithPath("/nonexistent/cloudflared", config)

		ctx := context.Background()
		connected := cf.checkHealthEndpoint(ctx)
		assert.False(t, connected)
	})
}

func TestCloudflare_Status_WithHealthEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := CloudflareConfig{
		HealthEndpoint: server.URL,
		Hostname:       "test.example.com",
		HealthTimeout:  5 * time.Second,
	}
	cf := NewCloudflareWithPath("/nonexistent/cloudflared", config)

	ctx := context.Background()
	status, err := cf.Status(ctx)
	assert.NoError(t, err)
	assert.NotNil(t, status)
	assert.True(t, status.Connected)
	assert.Equal(t, "Running", status.BackendState)
	assert.Equal(t, "cloudflare", status.Provider)
	assert.Equal(t, "test.example.com", status.Hostname)
}

func TestCloudflare_GetVersion_InvalidBinary(t *testing.T) {
	cf := NewCloudflareWithPath("/nonexistent/cloudflared", CloudflareConfig{})
	ctx := context.Background()

	version, err := cf.GetVersion(ctx)
	assert.Error(t, err)
	assert.Empty(t, version)
}

func TestCloudflare_GetTunnelList_InvalidBinary(t *testing.T) {
	cf := NewCloudflareWithPath("/nonexistent/cloudflared", CloudflareConfig{})
	ctx := context.Background()

	tunnels, err := cf.GetTunnelList(ctx)
	assert.Error(t, err)
	assert.Nil(t, tunnels)
}

func TestCloudflaredTunnelInfo_JSON(t *testing.T) {
	t.Run("parse tunnel with active connections", func(t *testing.T) {
		jsonData := `{
			"id": "abc123",
			"name": "my-tunnel",
			"createdAt": "2024-01-01T00:00:00Z",
			"connections": [
				{
					"colo_name": "DFW",
					"id": "conn1",
					"is_pending_reconnect": false,
					"clientId": "client1",
					"client_version": "2024.1.1"
				},
				{
					"colo_name": "ORD",
					"id": "conn2",
					"is_pending_reconnect": false,
					"clientId": "client1",
					"client_version": "2024.1.1"
				}
			]
		}`

		var info cloudflaredTunnelInfo
		err := json.Unmarshal([]byte(jsonData), &info)
		assert.NoError(t, err)
		assert.Equal(t, "abc123", info.ID)
		assert.Equal(t, "my-tunnel", info.Name)
		assert.Len(t, info.Connections, 2)
		assert.Equal(t, "DFW", info.Connections[0].ColoName)
		assert.True(t, len(info.Connections) > 0, "should detect as connected")
	})

	t.Run("parse tunnel with no connections", func(t *testing.T) {
		jsonData := `{
			"id": "abc123",
			"name": "my-tunnel",
			"createdAt": "2024-01-01T00:00:00Z",
			"connections": []
		}`

		var info cloudflaredTunnelInfo
		err := json.Unmarshal([]byte(jsonData), &info)
		assert.NoError(t, err)
		assert.Equal(t, "my-tunnel", info.Name)
		assert.Len(t, info.Connections, 0)
		assert.False(t, len(info.Connections) > 0, "should detect as disconnected")
	})

	t.Run("parse tunnel with null connections", func(t *testing.T) {
		jsonData := `{
			"id": "abc123",
			"name": "my-tunnel",
			"createdAt": "2024-01-01T00:00:00Z",
			"connections": null
		}`

		var info cloudflaredTunnelInfo
		err := json.Unmarshal([]byte(jsonData), &info)
		assert.NoError(t, err)
		assert.Nil(t, info.Connections)
		assert.False(t, len(info.Connections) > 0, "should detect as disconnected")
	})

	t.Run("invalid JSON returns error", func(t *testing.T) {
		jsonData := `{invalid json}`

		var info cloudflaredTunnelInfo
		err := json.Unmarshal([]byte(jsonData), &info)
		assert.Error(t, err)
	})

	t.Run("parse connection with pending reconnect", func(t *testing.T) {
		jsonData := `{
			"id": "abc123",
			"name": "my-tunnel",
			"createdAt": "2024-01-01T00:00:00Z",
			"connections": [
				{
					"colo_name": "DFW",
					"id": "conn1",
					"is_pending_reconnect": true,
					"clientId": "client1",
					"client_version": "2024.1.1"
				}
			]
		}`

		var info cloudflaredTunnelInfo
		err := json.Unmarshal([]byte(jsonData), &info)
		assert.NoError(t, err)
		assert.True(t, info.Connections[0].IsPendingReconnect)
		// Even with pending reconnect, connection exists
		assert.True(t, len(info.Connections) > 0)
	})
}

func TestCloudflare_CheckProcess_InvalidBinary(t *testing.T) {
	cf := NewCloudflareWithPath("/nonexistent/cloudflared", CloudflareConfig{})
	ctx := context.Background()

	// Should return false when binary doesn't exist
	connected := cf.checkProcess(ctx)
	assert.False(t, connected)
}

func TestCloudflare_CheckTunnelInfo_DeterministicResponses(t *testing.T) {
	tests := []struct {
		name      string
		output    string
		stderr    string
		err       error
		connected bool
		wantErr   bool
	}{
		{name: "active connection", output: `{"connections":[{"id":"connection-1"}]}`, stderr: "diagnostic\n", connected: true},
		{name: "no connections", output: `{"connections":[]}`},
		{name: "invalid JSON", output: `{not-json}`, wantErr: true},
		{name: "command failure", stderr: "Authorization: secret", err: errors.New("command failed"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &stubCommandRunner{outputFn: func(context.Context, string, ...string) ([]byte, string, error) {
				return []byte(tt.output), tt.stderr, tt.err
			}}
			cf := NewCloudflareWithPath("cloudflared-test", CloudflareConfig{TunnelName: "home"})
			cf.runner = runner

			connected, err := cf.checkTunnelInfo(context.Background())
			if tt.wantErr {
				require.Error(t, err)
				assert.False(t, connected)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.connected, connected)
			}
			assert.Equal(t, []commandCall{{
				name: "cloudflared-test",
				args: []string{"tunnel", "info", "--output", "json", "home"},
			}}, runner.recordedCalls())
		})
	}
}

func TestCloudflare_Status_TunnelInfoAndFallbacks(t *testing.T) {
	tests := []struct {
		name         string
		tunnelOutput string
		tunnelErr    error
		healthStatus int
		wantState    string
		wantOnline   bool
	}{
		{name: "connected tunnel info", tunnelOutput: `{"connections":[{}]}`, wantState: "Running", wantOnline: true},
		{name: "disconnected tunnel info", tunnelOutput: `{"connections":[]}`, wantState: "Disconnected"},
		{name: "failed info uses healthy endpoint", tunnelErr: errors.New("info failed"), healthStatus: http.StatusOK, wantState: "Running", wantOnline: true},
		{name: "failed info uses unhealthy endpoint", tunnelErr: errors.New("info failed"), healthStatus: http.StatusBadGateway, wantState: "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.healthStatus)
			}))
			defer server.Close()

			cf := NewCloudflareWithPath("cloudflared-test", CloudflareConfig{
				TunnelName:     "home",
				Hostname:       "home.example.com",
				HealthEndpoint: server.URL,
			})
			cf.runner = &stubCommandRunner{outputFn: func(context.Context, string, ...string) ([]byte, string, error) {
				return []byte(tt.tunnelOutput), "", tt.tunnelErr
			}}

			status, err := cf.Status(context.Background())
			require.NoError(t, err)
			assert.Equal(t, tt.wantOnline, status.Connected)
			assert.Equal(t, tt.wantState, status.BackendState)
			assert.Equal(t, "cloudflare", status.Provider)
			assert.Equal(t, "home.example.com", status.Hostname)
		})
	}
}

func TestCloudflare_CheckHealthEndpoint_InvalidURL(t *testing.T) {
	cf := NewCloudflareWithPath("cloudflared-test", CloudflareConfig{HealthEndpoint: "://bad-url"})
	assert.False(t, cf.checkHealthEndpoint(context.Background()))
}

func TestCloudflare_CheckProcess_DeterministicResponses(t *testing.T) {
	tests := []struct {
		name       string
		failAtCall int
		want       bool
		wantCalls  int
	}{
		{name: "version check fails", failAtCall: 1, wantCalls: 1},
		{name: "process lookup fails", failAtCall: 2, wantCalls: 2},
		{name: "process is running", want: true, wantCalls: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			call := 0
			runner := &stubCommandRunner{runFn: func(context.Context, string, ...string) (string, error) {
				call++
				if call == tt.failAtCall {
					return "failure", errors.New("command failed")
				}
				return "diagnostic", nil
			}}
			cf := NewCloudflareWithPath("cloudflared-test", CloudflareConfig{})
			cf.runner = runner

			assert.Equal(t, tt.want, cf.checkProcess(context.Background()))
			assert.Len(t, runner.recordedCalls(), tt.wantCalls)
			if tt.wantCalls == 2 {
				assert.Equal(t, commandCall{name: "pgrep", args: []string{"-x", "cloudflared"}}, runner.recordedCalls()[1])
			}
		})
	}
}

func TestCloudflare_Status_ProcessCheck(t *testing.T) {
	tests := []struct {
		name      string
		processOK bool
		wantState string
	}{
		{name: "running", processOK: true, wantState: "Running"},
		{name: "stopped", wantState: "Stopped"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cf := NewCloudflareWithPath("cloudflared-test", CloudflareConfig{})
			cf.runner = &stubCommandRunner{runFn: func(_ context.Context, name string, _ ...string) (string, error) {
				if !tt.processOK && name == "pgrep" {
					return "", errors.New("not running")
				}
				return "", nil
			}}
			status, err := cf.Status(context.Background())
			require.NoError(t, err)
			assert.Equal(t, tt.processOK, status.Connected)
			assert.Equal(t, tt.wantState, status.BackendState)
		})
	}
}

func TestCloudflare_GetTunnelList_DeterministicResponses(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		stderr  string
		err     error
		want    []string
		wantErr bool
	}{
		{name: "names", output: `[{"name":"alpha"},{"name":"beta"}]`, stderr: "diagnostic", want: []string{"alpha", "beta"}},
		{name: "empty", output: `[]`, want: []string{}},
		{name: "invalid JSON", output: `{not-json}`, wantErr: true},
		{name: "command failure", stderr: "Bearer secret", err: errors.New("command failed"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cf := NewCloudflareWithPath("cloudflared-test", CloudflareConfig{})
			cf.runner = &stubCommandRunner{outputFn: func(context.Context, string, ...string) ([]byte, string, error) {
				return []byte(tt.output), tt.stderr, tt.err
			}}
			names, err := cf.GetTunnelList(context.Background())
			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, names)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, names)
		})
	}
}

func TestCloudflare_GetVersion_DeterministicResponses(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		stderr  string
		err     error
		want    string
		wantErr bool
	}{
		{name: "trimmed version", output: "cloudflared version 2026.8.0\n", stderr: "diagnostic", want: "cloudflared version 2026.8.0"},
		{name: "command failure", stderr: "Authorization: secret", err: errors.New("command failed"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cf := NewCloudflareWithPath("cloudflared-test", CloudflareConfig{})
			cf.runner = &stubCommandRunner{outputFn: func(context.Context, string, ...string) ([]byte, string, error) {
				return []byte(tt.output), tt.stderr, tt.err
			}}
			version, err := cf.GetVersion(context.Background())
			if tt.wantErr {
				require.Error(t, err)
				assert.Empty(t, version)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, version)
		})
	}
}

// Integration tests - only run if cloudflared is installed
func TestCloudflare_Integration(t *testing.T) {
	cf, err := NewCloudflare()
	if err != nil {
		t.Skip("cloudflared not installed, skipping integration tests")
	}

	t.Run("Status", func(t *testing.T) {
		ctx := context.Background()
		status, err := cf.Status(ctx)
		assert.NoError(t, err)
		assert.NotNil(t, status)
		assert.Equal(t, "cloudflare", status.Provider)
	})

	t.Run("GetVersion", func(t *testing.T) {
		ctx := context.Background()
		version, err := cf.GetVersion(ctx)
		assert.NoError(t, err)
		assert.NotEmpty(t, version)
	})
}
