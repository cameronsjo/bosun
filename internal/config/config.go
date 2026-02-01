// Package config handles project discovery and configuration.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/cameronsjo/bosun/internal/log"
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

	// provisionsDir is the path to the provisions directory.
	provisionsDir string

	// ComposeFile is the path to the main docker-compose.yml.
	ComposeFile string

	// SnapshotsDir is the path to the snapshots directory.
	SnapshotsDir string

	// projectName is the docker compose project name.
	projectName string

	// infraContainers holds the configured infrastructure container names.
	infraContainers []string

	// tunnelProvider holds the configured tunnel provider name.
	tunnelProvider string

	// tunnelConfig holds provider-specific tunnel configuration.
	tunnelConfig TunnelConfig

	// alertConfig holds alert provider configuration.
	alertConfig AlertConfig
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

// AlertConfig holds alert provider configuration.
type AlertConfig struct {
	// Discord
	DiscordWebhookURL string `yaml:"discord_webhook_url"`

	// SendGrid
	SendGridAPIKey    string   `yaml:"sendgrid_api_key"`
	SendGridFromEmail string   `yaml:"sendgrid_from_email"`
	SendGridFromName  string   `yaml:"sendgrid_from_name"`
	SendGridToEmails  []string `yaml:"sendgrid_to_emails"`

	// Twilio
	TwilioAccountSID string   `yaml:"twilio_account_sid"`
	TwilioAuthToken  string   `yaml:"twilio_auth_token"`
	TwilioFromNumber string   `yaml:"twilio_from_number"`
	TwilioToNumbers  []string `yaml:"twilio_to_numbers"`

	// Settings
	OnSuccess bool `yaml:"on_success"` // Alert on successful deploys
	OnFailure bool `yaml:"on_failure"` // Alert on failed deploys (default: true)
}

// configFile represents the structure of .bosun/config.yml or bosun.yml.
type configFile struct {
	// Root is the project root (relative paths are resolved from here).
	Root string `yaml:"root"`

	// ManifestDir overrides the default manifest directory.
	ManifestDir string `yaml:"manifest_dir"`

	// ProvisionsDir overrides the default provisions directory.
	ProvisionsDir string `yaml:"provisions_dir"`

	// ProjectName sets the docker compose project name for all stacks.
	// This ensures all containers share a namespace and --remove-orphans works correctly.
	// Defaults to the project root directory name.
	ProjectName string `yaml:"project_name"`

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

	// Alerts configuration
	Alerts AlertConfig `yaml:"alerts"`
}

// FindRoot searches upward from the current directory to find the project root.
// The project root is identified by:
// - bosun.yaml or bosun.yml config file
// - bosun/ directory with docker-compose.yml
// - manifest/ or manifests/ directory
func FindRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}

	for dir != "/" {
		// Check for bosun.yaml or bosun.yml config file
		for _, name := range []string{"bosun.yaml", "bosun.yml"} {
			configPath := filepath.Join(dir, name)
			if _, err := os.Stat(configPath); err == nil {
				return dir, nil
			}
		}

		// Check for bosun directory with docker-compose.yml
		bosunDir := filepath.Join(dir, "bosun")
		if info, err := os.Stat(bosunDir); err == nil && info.IsDir() {
			composeFile := filepath.Join(bosunDir, "docker-compose.yml")
			if _, err := os.Stat(composeFile); err == nil {
				return dir, nil
			}
		}

		// Check for manifest or manifests directory
		for _, name := range []string{"manifest", "manifests"} {
			manifestDir := filepath.Join(dir, name)
			if info, err := os.Stat(manifestDir); err == nil && info.IsDir() {
				return dir, nil
			}
		}

		// Move up one directory
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return "", fmt.Errorf("project root not found (no bosun.yaml, bosun/, manifest/, or manifests/ directory)")
}

// Load finds the project root and returns a Config.
func Load() (*Config, error) {
	root, err := FindRoot()
	if err != nil {
		return nil, err
	}

	// Load config file if present
	fileCfg := loadConfigFile(root)

	// Determine manifest directory
	manifestDir := filepath.Join(root, "manifest")
	if fileCfg.ManifestDir != "" {
		manifestDir = filepath.Join(root, fileCfg.ManifestDir)
	} else {
		// Check for manifests/ (plural) if manifest/ doesn't exist
		if _, err := os.Stat(manifestDir); os.IsNotExist(err) {
			manifestsDir := filepath.Join(root, "manifests")
			if _, err := os.Stat(manifestsDir); err == nil {
				manifestDir = manifestsDir
			}
		}
	}

	// Determine provisions directory
	provisionsDir := filepath.Join(manifestDir, "provisions")
	if fileCfg.ProvisionsDir != "" {
		provisionsDir = filepath.Join(root, fileCfg.ProvisionsDir)
	}

	tunnelProvider, tunnelConfig := loadTunnelConfig(root)
	alertConfig := loadAlertConfig(root)

	// Determine project name (defaults to directory name)
	projectName := fileCfg.ProjectName
	if projectName == "" {
		projectName = filepath.Base(root)
	}

	cfg := &Config{
		Root:            root,
		ManifestDir:     manifestDir,
		provisionsDir:   provisionsDir,
		ComposeFile:     filepath.Join(root, "bosun", "docker-compose.yml"),
		SnapshotsDir:    filepath.Join(manifestDir, ".bosun", "snapshots"),
		projectName:     projectName,
		infraContainers: loadInfraContainers(root),
		tunnelProvider:  tunnelProvider,
		tunnelConfig:    tunnelConfig,
		alertConfig:     alertConfig,
	}

	return cfg, nil
}

// loadConfigFile loads the bosun config file if present.
func loadConfigFile(root string) configFile {
	var cfg configFile

	for _, name := range []string{"bosun.yaml", "bosun.yml", ".bosun/config.yml"} {
		path := filepath.Join(root, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		if err := yaml.Unmarshal(data, &cfg); err != nil {
			log.Warn().
				Err(err).
				Str(log.FieldPath, path).
				Msg("Failed to parse config file, skipping")
			continue
		}

		log.Debug().
			Str(log.FieldPath, path).
			Msg("Loaded config file")
		break
	}

	return cfg
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
			log.Warn().
				Err(err).
				Str(log.FieldPath, path).
				Msg("Failed to parse config file for infrastructure containers")
			continue
		}

		if len(cfg.Infrastructure.Containers) > 0 {
			log.Debug().
				Str(log.FieldPath, path).
				Int("count", len(cfg.Infrastructure.Containers)).
				Msg("Loaded infrastructure containers from config")
			return cfg.Infrastructure.Containers
		}
	}

	return defaultInfraContainers
}

// ProvisionsDir returns the path to the provisions directory.
func (c *Config) ProvisionsDir() string {
	if c.provisionsDir != "" {
		return c.provisionsDir
	}
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

// ChartsDir returns the path to the charts directory (Helm-aligned format).
func (c *Config) ChartsDir() string {
	return filepath.Join(c.Root, "charts")
}

// TemplatesDir returns the path to the templates directory (Helm-aligned format).
func (c *Config) TemplatesDir() string {
	return filepath.Join(c.Root, "charts", "templates")
}

// HelmStacksDir returns the path to the stacks directory for Helm-aligned format.
func (c *Config) HelmStacksDir() string {
	return filepath.Join(c.Root, "stacks")
}

// Format returns the manifest format used by this project.
// Returns "helm" for Helm-aligned format, "legacy" for provisions-based format.
func (c *Config) Format() string {
	// Check for Helm-aligned format (charts/ with Chart.yaml files)
	chartsDir := c.ChartsDir()
	if entries, err := os.ReadDir(chartsDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() && entry.Name() != "templates" {
				chartYaml := filepath.Join(chartsDir, entry.Name(), "Chart.yaml")
				if _, err := os.Stat(chartYaml); err == nil {
					return "helm"
				}
			}
		}
	}

	// Check for legacy format (provisions directory)
	if _, err := os.Stat(c.ProvisionsDir()); err == nil {
		return "legacy"
	}

	return "unknown"
}

// ProjectName returns the docker compose project name.
// All stacks share this project name so containers are properly namespaced
// and --remove-orphans works correctly across stack boundaries.
func (c *Config) ProjectName() string {
	return c.projectName
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
			log.Warn().
				Err(err).
				Str(log.FieldPath, path).
				Msg("Failed to parse config file for tunnel configuration")
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

		if cfg.Tunnel.Provider != "" {
			log.Debug().
				Str(log.FieldPath, path).
				Str("provider", provider).
				Msg("Loaded tunnel configuration")
		}

		return provider, tunnelCfg
	}

	return defaultTunnelProvider, TunnelConfig{}
}

// GetAlertConfig returns the alert configuration.
func (c *Config) GetAlertConfig() AlertConfig {
	return c.alertConfig
}

// loadAlertConfig loads alert configuration from config files.
// Supports environment variable overrides for sensitive values.
func loadAlertConfig(root string) AlertConfig {
	configPaths := []string{
		filepath.Join(root, ".bosun", "config.yml"),
		filepath.Join(root, "bosun.yml"),
	}

	var alertCfg AlertConfig
	alertCfg.OnFailure = true // Default to alerting on failures

	for _, path := range configPaths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var cfg configFile
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			log.Warn().
				Err(err).
				Str(log.FieldPath, path).
				Msg("Failed to parse config file for alert configuration")
			continue
		}

		alertCfg = cfg.Alerts
		// Ensure default for OnFailure if not explicitly set
		if !cfg.Alerts.OnSuccess && !cfg.Alerts.OnFailure {
			alertCfg.OnFailure = true
		}

		log.Debug().
			Str(log.FieldPath, path).
			Bool("on_success", alertCfg.OnSuccess).
			Bool("on_failure", alertCfg.OnFailure).
			Msg("Loaded alert configuration")
		break
	}

	// Environment variable overrides
	if v := os.Getenv("DISCORD_WEBHOOK_URL"); v != "" {
		alertCfg.DiscordWebhookURL = v
	}
	if v := os.Getenv("SENDGRID_API_KEY"); v != "" {
		alertCfg.SendGridAPIKey = v
	}
	if v := os.Getenv("SENDGRID_FROM_EMAIL"); v != "" {
		alertCfg.SendGridFromEmail = v
	}
	if v := os.Getenv("SENDGRID_FROM_NAME"); v != "" {
		alertCfg.SendGridFromName = v
	}
	if v := os.Getenv("TWILIO_ACCOUNT_SID"); v != "" {
		alertCfg.TwilioAccountSID = v
	}
	if v := os.Getenv("TWILIO_AUTH_TOKEN"); v != "" {
		alertCfg.TwilioAuthToken = v
	}
	if v := os.Getenv("TWILIO_FROM_NUMBER"); v != "" {
		alertCfg.TwilioFromNumber = v
	}

	return alertCfg
}
