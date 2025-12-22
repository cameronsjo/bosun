package tunnel

import (
	"context"
	"encoding/json"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// CloudflareConfig holds configuration for the Cloudflare provider.
type CloudflareConfig struct {
	// TunnelName is the name of the Cloudflare tunnel.
	TunnelName string

	// Hostname is the tunnel hostname (e.g., "myhost.example.com").
	Hostname string

	// HealthEndpoint is the URL to check for tunnel health.
	// If empty, defaults to checking if cloudflared is running.
	HealthEndpoint string

	// HealthTimeout is the timeout for health check requests.
	HealthTimeout time.Duration
}

// Cloudflare implements the Provider interface for Cloudflare Tunnel.
type Cloudflare struct {
	// binaryPath is the path to the cloudflared binary.
	binaryPath string

	// config holds the Cloudflare-specific configuration.
	config CloudflareConfig
}

// cloudflaredTunnelInfo represents the output of `cloudflared tunnel info`.
type cloudflaredTunnelInfo struct {
	ID          string                `json:"id"`
	Name        string                `json:"name"`
	CreatedAt   string                `json:"createdAt"`
	Connections []cloudflaredConnection `json:"connections"`
}

// cloudflaredConnection represents a tunnel connection.
type cloudflaredConnection struct {
	ColoName       string `json:"colo_name"`
	ID             string `json:"id"`
	IsPendingReconnect bool `json:"is_pending_reconnect"`
	ClientID       string `json:"clientId"`
	ClientVersion  string `json:"client_version"`
}

// DefaultHealthTimeout is the default timeout for health checks.
const DefaultHealthTimeout = 5 * time.Second

// NewCloudflare creates a new Cloudflare provider.
// Returns an error if cloudflared is not installed.
func NewCloudflare() (*Cloudflare, error) {
	path, err := exec.LookPath("cloudflared")
	if err != nil {
		return nil, ErrNotInstalled{Provider: "Cloudflare Tunnel (cloudflared)"}
	}

	return &Cloudflare{
		binaryPath: path,
		config: CloudflareConfig{
			HealthTimeout: DefaultHealthTimeout,
		},
	}, nil
}

// NewCloudflareWithConfig creates a new Cloudflare provider with custom configuration.
func NewCloudflareWithConfig(config CloudflareConfig) (*Cloudflare, error) {
	path, err := exec.LookPath("cloudflared")
	if err != nil {
		return nil, ErrNotInstalled{Provider: "Cloudflare Tunnel (cloudflared)"}
	}

	if config.HealthTimeout == 0 {
		config.HealthTimeout = DefaultHealthTimeout
	}

	return &Cloudflare{
		binaryPath: path,
		config:     config,
	}, nil
}

// NewCloudflareWithPath creates a new Cloudflare provider with a custom binary path.
// This is useful for testing or when the binary is not in PATH.
func NewCloudflareWithPath(binaryPath string, config CloudflareConfig) *Cloudflare {
	if config.HealthTimeout == 0 {
		config.HealthTimeout = DefaultHealthTimeout
	}
	return &Cloudflare{
		binaryPath: binaryPath,
		config:     config,
	}
}

// Name returns the provider name.
func (c *Cloudflare) Name() string {
	return string(ProviderCloudflare)
}

// Status returns the current Cloudflare Tunnel status.
func (c *Cloudflare) Status(ctx context.Context) (*Status, error) {
	status := &Status{
		Provider: string(ProviderCloudflare),
		Hostname: c.config.Hostname,
	}

	// Try to get tunnel info if tunnel name is configured
	if c.config.TunnelName != "" {
		connected, err := c.checkTunnelInfo(ctx)
		if err == nil {
			status.Connected = connected
			if connected {
				status.BackendState = "Running"
			} else {
				status.BackendState = "Disconnected"
			}
			return status, nil
		}
	}

	// Fall back to health endpoint check
	if c.config.HealthEndpoint != "" {
		connected := c.checkHealthEndpoint(ctx)
		status.Connected = connected
		if connected {
			status.BackendState = "Running"
		} else {
			status.BackendState = "Unknown"
		}
		return status, nil
	}

	// Check if cloudflared process is running
	connected := c.checkProcess(ctx)
	status.Connected = connected
	if connected {
		status.BackendState = "Running"
	} else {
		status.BackendState = "Stopped"
	}

	return status, nil
}

// checkTunnelInfo attempts to get tunnel info using `cloudflared tunnel info`.
func (c *Cloudflare) checkTunnelInfo(ctx context.Context) (bool, error) {
	cmd := exec.CommandContext(ctx, c.binaryPath, "tunnel", "info", "--output", "json", c.config.TunnelName)
	output, err := cmd.Output()
	if err != nil {
		return false, err
	}

	var info cloudflaredTunnelInfo
	if err := json.Unmarshal(output, &info); err != nil {
		return false, err
	}

	// Tunnel is connected if it has active connections
	return len(info.Connections) > 0, nil
}

// checkHealthEndpoint checks the configured health endpoint.
func (c *Cloudflare) checkHealthEndpoint(ctx context.Context) bool {
	client := &http.Client{
		Timeout: c.config.HealthTimeout,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.config.HealthEndpoint, nil)
	if err != nil {
		return false
	}

	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

// checkProcess checks if cloudflared is running by looking for the process.
func (c *Cloudflare) checkProcess(ctx context.Context) bool {
	// Try running cloudflared version to verify it's accessible
	cmd := exec.CommandContext(ctx, c.binaryPath, "version")
	if err := cmd.Run(); err != nil {
		return false
	}

	// Check if there's a running tunnel process
	// On Linux/macOS, we can check for running cloudflared processes
	cmd = exec.CommandContext(ctx, "pgrep", "-x", "cloudflared")
	if err := cmd.Run(); err != nil {
		return false
	}

	return true
}

// IsConnected returns true if the Cloudflare tunnel is connected.
func (c *Cloudflare) IsConnected(ctx context.Context) bool {
	status, err := c.Status(ctx)
	if err != nil {
		return false
	}
	return status.Connected
}

// GetHostname returns the configured tunnel hostname.
func (c *Cloudflare) GetHostname() string {
	return c.config.Hostname
}

// GetTunnelList returns a list of configured tunnels.
func (c *Cloudflare) GetTunnelList(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, c.binaryPath, "tunnel", "list", "--output", "json")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	// Parse the JSON output
	var tunnels []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(output, &tunnels); err != nil {
		return nil, err
	}

	names := make([]string, 0, len(tunnels))
	for _, t := range tunnels {
		names = append(names, t.Name)
	}

	return names, nil
}

// GetVersion returns the cloudflared version.
func (c *Cloudflare) GetVersion(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, c.binaryPath, "version")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}
