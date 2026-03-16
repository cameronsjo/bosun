package tunnel

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/cameronsjo/bosun/internal/log"
)

var (
	bearerTokenPattern = regexp.MustCompile(`Bearer [A-Za-z0-9._-]+`)
	authHeaderPattern  = regexp.MustCompile(`(?i)Authorization:.*`)
	base64BlobPattern  = regexp.MustCompile(`[A-Za-z0-9+/=]{40,}`)
)

// redactSensitiveOutput replaces common sensitive patterns in cloudflared stderr output
// before logging. Targets bearer tokens, authorization headers, and long base64 blobs
// that may contain credentials.
func redactSensitiveOutput(output string) string {
	result := bearerTokenPattern.ReplaceAllString(output, "Bearer [REDACTED]")
	result = authHeaderPattern.ReplaceAllString(result, "Authorization: [REDACTED]")
	result = base64BlobPattern.ReplaceAllString(result, "[REDACTED-CREDENTIAL]")
	return result
}

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
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	CreatedAt   string                 `json:"createdAt"`
	Connections []cloudflaredConnection `json:"connections"`
}

// cloudflaredConnection represents a tunnel connection.
type cloudflaredConnection struct {
	ColoName           string `json:"colo_name"`
	ID                 string `json:"id"`
	IsPendingReconnect bool   `json:"is_pending_reconnect"`
	ClientID           string `json:"clientId"`
	ClientVersion      string `json:"client_version"`
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
	logger := log.ComponentCtx(ctx, log.ComponentTunnel)

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
			logger.Debug().Bool("connected", connected).Str("tunnel", c.config.TunnelName).Msg("Cloudflare tunnel info check completed")
			return status, nil
		}
		logger.Debug().Err(err).Str("tunnel", c.config.TunnelName).Msg("Cloudflare tunnel info check failed, trying fallback")
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
		logger.Debug().Bool("connected", connected).Str("endpoint", c.config.HealthEndpoint).Msg("Cloudflare health endpoint check completed")
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
	start := time.Now()
	logger := log.ComponentCtx(ctx, log.ComponentTunnel)

	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, c.binaryPath, "tunnel", "info", "--output", "json", c.config.TunnelName)
	cmd.Stderr = &stderr

	logger.Debug().Str(log.FieldOperation, "tunnel_info").Str("tunnel", c.config.TunnelName).Msg("Executing cloudflared tunnel info")

	output, err := cmd.Output()
	if err != nil {
		logger.Error().
			Err(err).
			Str(log.FieldOperation, "tunnel_info").
			Str("stderr", redactSensitiveOutput(stderr.String())).
			Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
			Msg("cloudflared tunnel info failed")
		return false, err
	}

	if stderrStr := stderr.String(); stderrStr != "" {
		logger.Debug().Str("stderr", redactSensitiveOutput(stderrStr)).Msg("cloudflared tunnel info stderr")
	}

	var info cloudflaredTunnelInfo
	if err := json.Unmarshal(output, &info); err != nil {
		return false, err
	}

	logger.Info().
		Str(log.FieldOperation, "tunnel_info").
		Int("connections", len(info.Connections)).
		Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
		Msg("cloudflared tunnel info completed")

	// Tunnel is connected if it has active connections
	return len(info.Connections) > 0, nil
}

// checkHealthEndpoint checks the configured health endpoint.
func (c *Cloudflare) checkHealthEndpoint(ctx context.Context) bool {
	start := time.Now()
	logger := log.ComponentCtx(ctx, log.ComponentTunnel)

	client := &http.Client{
		Timeout: c.config.HealthTimeout,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.config.HealthEndpoint, nil)
	if err != nil {
		logger.Error().Err(err).Str("endpoint", c.config.HealthEndpoint).Msg("Failed to create health check request")
		return false
	}

	resp, err := client.Do(req)
	if err != nil {
		logger.Debug().
			Err(err).
			Str("endpoint", c.config.HealthEndpoint).
			Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
			Msg("Cloudflare health endpoint unreachable")
		return false
	}
	defer func() { _ = resp.Body.Close() }()

	logger.Debug().
		Int(log.FieldStatus, resp.StatusCode).
		Str("endpoint", c.config.HealthEndpoint).
		Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
		Msg("Cloudflare health endpoint check completed")

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
	start := time.Now()
	logger := log.ComponentCtx(ctx, log.ComponentTunnel)

	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, c.binaryPath, "tunnel", "list", "--output", "json")
	cmd.Stderr = &stderr

	logger.Debug().Str(log.FieldOperation, "tunnel_list").Msg("Executing cloudflared tunnel list")

	output, err := cmd.Output()
	if err != nil {
		logger.Error().
			Err(err).
			Str(log.FieldOperation, "tunnel_list").
			Str("stderr", redactSensitiveOutput(stderr.String())).
			Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
			Msg("cloudflared tunnel list failed")
		return nil, err
	}

	if stderrStr := stderr.String(); stderrStr != "" {
		logger.Debug().Str("stderr", redactSensitiveOutput(stderrStr)).Msg("cloudflared tunnel list stderr")
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

	logger.Info().
		Str(log.FieldOperation, "tunnel_list").
		Int("count", len(names)).
		Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
		Msg("cloudflared tunnel list completed")

	return names, nil
}

// GetVersion returns the cloudflared version.
func (c *Cloudflare) GetVersion(ctx context.Context) (string, error) {
	start := time.Now()
	logger := log.ComponentCtx(ctx, log.ComponentTunnel)

	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, c.binaryPath, "version")
	cmd.Stderr = &stderr

	logger.Debug().Str(log.FieldOperation, "version").Msg("Executing cloudflared version")

	output, err := cmd.Output()
	if err != nil {
		logger.Error().
			Err(err).
			Str(log.FieldOperation, "version").
			Str("stderr", redactSensitiveOutput(stderr.String())).
			Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
			Msg("cloudflared version check failed")
		return "", err
	}

	if stderrStr := stderr.String(); stderrStr != "" {
		logger.Debug().Str("stderr", redactSensitiveOutput(stderrStr)).Msg("cloudflared version stderr")
	}

	version := strings.TrimSpace(string(output))

	logger.Debug().
		Str(log.FieldOperation, "version").
		Str("version", version).
		Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
		Msg("cloudflared version check completed")

	return version, nil
}
