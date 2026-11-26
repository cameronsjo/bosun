// Package tunnel provides an abstraction layer for tunnel providers
// such as Tailscale, Cloudflare Tunnel, and others.
package tunnel

import (
	"context"
	"fmt"
)

// Provider defines the interface for tunnel providers.
// Implementations include Tailscale, Cloudflare Tunnel, and others.
type Provider interface {
	// Name returns the provider name (e.g., "tailscale", "cloudflare").
	Name() string

	// Status returns the current tunnel status.
	Status(ctx context.Context) (*Status, error)

	// IsConnected returns true if the tunnel is currently connected.
	IsConnected(ctx context.Context) bool

	// GetHostname returns the tunnel hostname.
	// For Tailscale: "myhost.tail1234.ts.net"
	// For Cloudflare: "myhost.example.com"
	GetHostname() string
}

// Status represents the current state of a tunnel connection.
type Status struct {
	// Connected indicates whether the tunnel is currently connected.
	Connected bool

	// Hostname is the tunnel hostname (e.g., DNS name).
	Hostname string

	// IP is the tunnel IP address.
	IP string

	// Peers is the list of peers (for mesh networks like Tailscale).
	Peers []Peer

	// Provider is the name of the tunnel provider.
	Provider string

	// BackendState is the provider-specific state (e.g., "Running", "Stopped").
	BackendState string

	// TailnetName is the name of the tailnet (Tailscale-specific).
	TailnetName string
}

// Peer represents a peer in a mesh network.
type Peer struct {
	// Name is the peer's hostname or display name.
	Name string

	// DNSName is the peer's fully qualified DNS name.
	DNSName string

	// IP is the peer's tunnel IP address.
	IP string

	// Online indicates whether the peer is currently online.
	Online bool

	// ExitNode indicates whether this peer is an exit node.
	ExitNode bool

	// Active indicates whether this peer is actively connected.
	Active bool
}

// ProviderType represents the supported tunnel provider types.
type ProviderType string

const (
	// ProviderTailscale represents the Tailscale tunnel provider.
	ProviderTailscale ProviderType = "tailscale"

	// ProviderCloudflare represents the Cloudflare Tunnel provider.
	ProviderCloudflare ProviderType = "cloudflare"
)

// ErrNotInstalled indicates the tunnel provider binary is not installed.
type ErrNotInstalled struct {
	Provider string
}

func (e ErrNotInstalled) Error() string {
	return fmt.Sprintf("%s is not installed", e.Provider)
}

// ErrNotConnected indicates the tunnel is not connected.
type ErrNotConnected struct {
	Provider string
}

func (e ErrNotConnected) Error() string {
	return fmt.Sprintf("%s tunnel is not connected", e.Provider)
}

// ErrNotConfigured indicates the tunnel provider is not configured.
type ErrNotConfigured struct {
	Provider string
	Message  string
}

func (e ErrNotConfigured) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("%s is not configured: %s", e.Provider, e.Message)
	}
	return fmt.Sprintf("%s is not configured", e.Provider)
}

// ProviderConfig carries the provider-specific settings from the project
// config (the tunnel section of bosun.yaml). Only Cloudflare reads them today;
// Tailscale discovers its state from the local daemon.
type ProviderConfig struct {
	// TunnelName is the name of the Cloudflare tunnel.
	TunnelName string

	// Hostname is the tunnel hostname (e.g., "myhost.example.com").
	Hostname string

	// HealthEndpoint is the URL checked for tunnel health.
	HealthEndpoint string
}

// NewProvider creates a new tunnel provider based on the provider type, with
// no provider-specific settings.
func NewProvider(providerType string) (Provider, error) {
	return NewProviderWithConfig(providerType, ProviderConfig{})
}

// NewProviderWithConfig creates a new tunnel provider and applies the
// provider-specific settings from the project config.
func NewProviderWithConfig(providerType string, cfg ProviderConfig) (Provider, error) {
	switch ProviderType(providerType) {
	case ProviderTailscale:
		return NewTailscale()
	case ProviderCloudflare:
		return NewCloudflareWithConfig(CloudflareConfig{
			TunnelName:     cfg.TunnelName,
			Hostname:       cfg.Hostname,
			HealthEndpoint: cfg.HealthEndpoint,
		})
	default:
		return nil, fmt.Errorf("unknown tunnel provider: %s", providerType)
	}
}

// SupportedProviders returns a list of supported provider types.
func SupportedProviders() []ProviderType {
	return []ProviderType{
		ProviderTailscale,
		ProviderCloudflare,
	}
}
