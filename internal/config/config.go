// Package config handles project discovery and configuration.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// defaultInfraContainers is the fallback list of infrastructure containers.
var defaultInfraContainers = []string{"traefik", "authelia", "gatus"}

// defaultTunnelProvider is the default tunnel provider.
const defaultTunnelProvider = "tailscale"

// Config holds the bosun project configuration.
type Config struct {
	// Root is the project root directory (contains bosun/ or manifest/).
	Root string

	// ManifestDir is the path to the manifest directory.
	ManifestDir string

	// ComposeFile is the path to the main docker-compose.yml.
	ComposeFile string

	// SnapshotsDir is the path to the snapshots directory.
	SnapshotsDir string

	// infraContainers holds the configured infrastructure container names.
	infraContainers []string

	// tunnelProvider holds the configured tunnel provider name.
	tunnelProvider string

	// tunnelConfig holds provider-specific tunnel configuration.
	tunnelConfig TunnelConfig
}

// TunnelConfig holds tunnel provider-specific configuration.
type TunnelConfig struct {
	// Hostname is the tunnel hostname (for Cloudflare).
	Hostname string

	// TunnelName is the tunnel name (for Cloudflare).
	TunnelName string

	// HealthEndpoint is the health check URL (for Cloudflare).
	HealthEndpoint string
}

// configFile represents the structure of .bosun/config.yml or bosun.yml.
type configFile struct {
	Infrastructure struct {
		Containers []string `yaml:"containers"`
	} `yaml:"infrastructure"`

	// Tunnel configuration
	Tunnel struct {
		Provider       string `yaml:"provider"`
		Hostname       string `yaml:"hostname"`
		TunnelName     string `yaml:"tunnel_name"`
		HealthEndpoint string `yaml:"health_endpoint"`
	} `yaml:"tunnel"`
}

// FindRoot searches upward from the current directory to find the project root.
// The project root is identified by the presence of a bosun/ or manifest/ directory.
func FindRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}

	for dir != "/" {
		// Check for bosun directory with docker-compose.yml
		bosunDir := filepath.Join(dir, "bosun")
		if info, err := os.Stat(bosunDir); err == nil && info.IsDir() {
			composeFile := filepath.Join(bosunDir, "docker-compose.yml")
			if _, err := os.Stat(composeFile); err == nil {
				return dir, nil
			}
		}

		// Check for manifest directory
		manifestDir := filepath.Join(dir, "manifest")
		if info, err := os.Stat(manifestDir); err == nil && info.IsDir() {
			return dir, nil
		}

		// Move up one directory
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return "", fmt.Errorf("project root not found (no bosun/ or manifest/ directory)")
}

// Load finds the project root and returns a Config.
func Load() (*Config, error) {
	root, err := FindRoot()
	if err != nil {
		return nil, err
	}

	tunnelProvider, tunnelConfig := loadTunnelConfig(root)

	cfg := &Config{
		Root:            root,
		ManifestDir:    filepath.Join(root, "manifest"),
		ComposeFile:    filepath.Join(root, "bosun", "docker-compose.yml"),
		SnapshotsDir:   filepath.Join(root, "manifest", ".bosun", "snapshots"),
		infraContainers: loadInfraContainers(root),
		tunnelProvider:  tunnelProvider,
		tunnelConfig:    tunnelConfig,
	}

	return cfg, nil
}

// loadInfraContainers loads infrastructure container names from config files.
// Checks for .bosun/config.yml or bosun.yml in the project root.
// Falls back to default list if no config is found.
func loadInfraContainers(root string) []string {
	// Check for .bosun/config.yml first
	configPaths := []string{
		filepath.Join(root, ".bosun", "config.yml"),
		filepath.Join(root, "bosun.yml"),
	}

	for _, path := range configPaths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var cfg configFile
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			continue
		}

		if len(cfg.Infrastructure.Containers) > 0 {
			return cfg.Infrastructure.Containers
		}
	}

	return defaultInfraContainers
}

// ProvisionsDir returns the path to the provisions directory.
func (c *Config) ProvisionsDir() string {
	return filepath.Join(c.ManifestDir, "provisions")
}

// ServicesDir returns the path to the services directory.
func (c *Config) ServicesDir() string {
	return filepath.Join(c.ManifestDir, "services")
}

// StacksDir returns the path to the stacks directory.
func (c *Config) StacksDir() string {
	return filepath.Join(c.ManifestDir, "stacks")
}

// OutputDir returns the path to the output directory.
func (c *Config) OutputDir() string {
	return filepath.Join(c.ManifestDir, "output")
}

// InfraContainers returns the list of infrastructure container names.
// These containers are shown separately in status displays and excluded from orphan detection.
func (c *Config) InfraContainers() []string {
	return c.infraContainers
}

// TunnelProvider returns the configured tunnel provider name.
// Defaults to "tailscale" if not configured.
func (c *Config) TunnelProvider() string {
	return c.tunnelProvider
}

// TunnelHostname returns the configured tunnel hostname.
func (c *Config) TunnelHostname() string {
	return c.tunnelConfig.Hostname
}

// TunnelName returns the configured tunnel name (for Cloudflare).
func (c *Config) TunnelName() string {
	return c.tunnelConfig.TunnelName
}

// TunnelHealthEndpoint returns the configured health endpoint (for Cloudflare).
func (c *Config) TunnelHealthEndpoint() string {
	return c.tunnelConfig.HealthEndpoint
}

// GetTunnelConfig returns the full tunnel configuration.
func (c *Config) GetTunnelConfig() TunnelConfig {
	return c.tunnelConfig
}

// loadTunnelConfig loads tunnel configuration from config files.
// Returns the provider name and tunnel-specific configuration.
func loadTunnelConfig(root string) (string, TunnelConfig) {
	configPaths := []string{
		filepath.Join(root, ".bosun", "config.yml"),
		filepath.Join(root, "bosun.yml"),
	}

	for _, path := range configPaths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var cfg configFile
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			continue
		}

		provider := cfg.Tunnel.Provider
		if provider == "" {
			provider = defaultTunnelProvider
		}

		tunnelCfg := TunnelConfig{
			Hostname:       cfg.Tunnel.Hostname,
			TunnelName:     cfg.Tunnel.TunnelName,
			HealthEndpoint: cfg.Tunnel.HealthEndpoint,
		}

		return provider, tunnelCfg
	}

	return defaultTunnelProvider, TunnelConfig{}
}
