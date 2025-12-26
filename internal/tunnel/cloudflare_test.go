package tunnel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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
