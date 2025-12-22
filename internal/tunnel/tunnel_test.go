package tunnel

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderType_Constants(t *testing.T) {
	assert.Equal(t, ProviderType("tailscale"), ProviderTailscale)
	assert.Equal(t, ProviderType("cloudflare"), ProviderCloudflare)
}

func TestSupportedProviders(t *testing.T) {
	providers := SupportedProviders()
	assert.Len(t, providers, 2)
	assert.Contains(t, providers, ProviderTailscale)
	assert.Contains(t, providers, ProviderCloudflare)
}

func TestNewProvider_UnknownProvider(t *testing.T) {
	provider, err := NewProvider("unknown")
	assert.Nil(t, provider)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown tunnel provider")
}

func TestNewProvider_Tailscale(t *testing.T) {
	// This test will fail if tailscale is not installed, which is expected
	// We're testing the factory function, not Tailscale itself
	provider, err := NewProvider("tailscale")
	if err != nil {
		// If Tailscale is not installed, verify we get the right error
		notInstalled, ok := err.(ErrNotInstalled)
		require.True(t, ok, "expected ErrNotInstalled, got: %v", err)
		assert.Equal(t, "Tailscale", notInstalled.Provider)
	} else {
		assert.NotNil(t, provider)
		assert.Equal(t, "tailscale", provider.Name())
	}
}

func TestNewProvider_Cloudflare(t *testing.T) {
	// This test will fail if cloudflared is not installed, which is expected
	provider, err := NewProvider("cloudflare")
	if err != nil {
		notInstalled, ok := err.(ErrNotInstalled)
		require.True(t, ok, "expected ErrNotInstalled, got: %v", err)
		assert.Contains(t, notInstalled.Provider, "Cloudflare")
	} else {
		assert.NotNil(t, provider)
		assert.Equal(t, "cloudflare", provider.Name())
	}
}

func TestErrNotInstalled(t *testing.T) {
	err := ErrNotInstalled{Provider: "TestProvider"}
	assert.Equal(t, "TestProvider is not installed", err.Error())
}

func TestErrNotConnected(t *testing.T) {
	err := ErrNotConnected{Provider: "TestProvider"}
	assert.Equal(t, "TestProvider tunnel is not connected", err.Error())
}

func TestErrNotConfigured(t *testing.T) {
	t.Run("without message", func(t *testing.T) {
		err := ErrNotConfigured{Provider: "TestProvider"}
		assert.Equal(t, "TestProvider is not configured", err.Error())
	})

	t.Run("with message", func(t *testing.T) {
		err := ErrNotConfigured{Provider: "TestProvider", Message: "missing API key"}
		assert.Equal(t, "TestProvider is not configured: missing API key", err.Error())
	})
}

func TestStatus_Fields(t *testing.T) {
	status := Status{
		Connected:    true,
		Hostname:     "myhost.example.com",
		IP:           "100.100.100.1",
		Provider:     "tailscale",
		BackendState: "Running",
		TailnetName:  "my-tailnet.ts.net",
		Peers: []Peer{
			{
				Name:     "peer1",
				DNSName:  "peer1.my-tailnet.ts.net",
				IP:       "100.100.100.2",
				Online:   true,
				ExitNode: false,
				Active:   true,
			},
		},
	}

	assert.True(t, status.Connected)
	assert.Equal(t, "myhost.example.com", status.Hostname)
	assert.Equal(t, "100.100.100.1", status.IP)
	assert.Equal(t, "tailscale", status.Provider)
	assert.Equal(t, "Running", status.BackendState)
	assert.Equal(t, "my-tailnet.ts.net", status.TailnetName)
	assert.Len(t, status.Peers, 1)
	assert.Equal(t, "peer1", status.Peers[0].Name)
}

func TestPeer_Fields(t *testing.T) {
	peer := Peer{
		Name:     "test-peer",
		DNSName:  "test-peer.tailnet.ts.net",
		IP:       "100.100.100.5",
		Online:   true,
		ExitNode: true,
		Active:   true,
	}

	assert.Equal(t, "test-peer", peer.Name)
	assert.Equal(t, "test-peer.tailnet.ts.net", peer.DNSName)
	assert.Equal(t, "100.100.100.5", peer.IP)
	assert.True(t, peer.Online)
	assert.True(t, peer.ExitNode)
	assert.True(t, peer.Active)
}
