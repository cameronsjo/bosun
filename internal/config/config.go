// Package config handles project discovery and configuration.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cameronsjo/bosun/internal/log"
	"github.com/cameronsjo/bosun/internal/reconcile"
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

	// postSyncHooks holds post-sync container restart hooks.
	postSyncHooks []reconcile.PostSyncHook

	// hookSettleDelay is a global pause after deploy before any post-sync hooks run.
	hookSettleDelay time.Duration

	// deployPaths is an allowlist of glob patterns for deploy-relevant paths.
	deployPaths []string

	// domain is the project-level domain for Traefik defaultRule.
	domain string
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

	// Domain is the project-level domain used for Traefik defaultRule.
	Domain string `yaml:"domain"`

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

	// PostSyncHooks defines container restart actions triggered by file changes.
	PostSyncHooks []reconcile.PostSyncHook `yaml:"post_sync_hooks"`

	// HookSettleDelay is a global pause after deploy before post-sync hooks run.
	HookSettleDelay reconcile.Duration `yaml:"hook_settle_delay"`

	// DeployPaths is an allowlist of glob patterns for deploy-relevant paths.
	// When configured, commits that only touch files outside these patterns skip the pipeline.
	DeployPaths []string `yaml:"deploy_paths"`
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

// LoadFrom loads project config from a specific directory path (skips FindRoot).
// Returns nil with no error if no config file is found in the directory.
// Returns nil with error only if a config file exists but fails to parse.
func LoadFrom(dir string) (*Config, error) {
	fileCfg := loadConfigFile(dir)

	// Check if any config was actually loaded by testing whether
	// loadConfigFile found and parsed a file. Since configFile is a value
	// type, we check for non-zero state in any field.
	postSyncHooks := extractPostSyncHooks(fileCfg)
	hookSettleDelay := extractHookSettleDelay(fileCfg)
	deployPaths := extractDeployPaths(fileCfg)
	domain := extractDomain(fileCfg)

	return &Config{
		Root:            dir,
		postSyncHooks:   postSyncHooks,
		hookSettleDelay: hookSettleDelay,
		deployPaths:     deployPaths,
		domain:          domain,
	}, nil
}

// Load finds the project root and returns a Config.
func Load() (*Config, error) {
	start := time.Now()
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

	// Extract all config sections from the already-parsed config file.
	// This avoids re-reading and re-parsing the YAML file for each section.
	infraContainers := extractInfraContainers(fileCfg)
	tunnelProvider, tunnelConfig := extractTunnelConfig(fileCfg)
	alertConfig := extractAlertConfig(fileCfg)
	postSyncHooks := extractPostSyncHooks(fileCfg)
	hookSettleDelay := extractHookSettleDelay(fileCfg)
	deployPaths := extractDeployPaths(fileCfg)
	domain := extractDomain(fileCfg)

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
		infraContainers: infraContainers,
		tunnelProvider:  tunnelProvider,
		tunnelConfig:    tunnelConfig,
		alertConfig:     alertConfig,
		postSyncHooks:   postSyncHooks,
		hookSettleDelay: hookSettleDelay,
		deployPaths:     deployPaths,
		domain:          domain,
	}

	logger := log.Component("config")
	logger.Debug().
		Str(log.FieldPath, root).
		Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
		Msg("Successfully loaded project configuration")

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

// extractInfraContainers extracts infrastructure container names from a parsed config.
// Falls back to default list if not configured.
func extractInfraContainers(cfg configFile) []string {
	if len(cfg.Infrastructure.Containers) > 0 {
		return cfg.Infrastructure.Containers
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

// Domain returns the project-level domain for Traefik defaultRule.
// Returns an empty string if not configured.
func (c *Config) Domain() string {
	return c.domain
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

// extractTunnelConfig extracts tunnel configuration from a parsed config.
// Returns the provider name and tunnel-specific configuration.
func extractTunnelConfig(cfg configFile) (string, TunnelConfig) {
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

// GetAlertConfig returns the alert configuration.
func (c *Config) GetAlertConfig() AlertConfig {
	return c.alertConfig
}

// PostSyncHooks returns the configured post-sync container restart hooks.
func (c *Config) PostSyncHooks() []reconcile.PostSyncHook {
	return c.postSyncHooks
}

// extractPostSyncHooks extracts post-sync hooks from a parsed config.
func extractPostSyncHooks(cfg configFile) []reconcile.PostSyncHook {
	return cfg.PostSyncHooks
}

// DeployPaths returns the configured deploy-relevant path patterns.
func (c *Config) DeployPaths() []string {
	return c.deployPaths
}

// extractDeployPaths extracts deploy path patterns from a parsed config.
func extractDeployPaths(cfg configFile) []string {
	return cfg.DeployPaths
}

// HookSettleDelay returns the configured global settle delay for post-sync hooks.
func (c *Config) HookSettleDelay() time.Duration {
	return c.hookSettleDelay
}

// extractHookSettleDelay extracts the hook settle delay from a parsed config.
func extractHookSettleDelay(cfg configFile) time.Duration {
	return cfg.HookSettleDelay.Duration
}

// extractDomain extracts the domain from a parsed config.
func extractDomain(cfg configFile) string {
	return cfg.Domain
}

// extractAlertConfig extracts alert configuration from a parsed config.
// Supports environment variable overrides for sensitive values.
func extractAlertConfig(cfg configFile) AlertConfig {
	alertCfg := cfg.Alerts

	// Ensure default for OnFailure if not explicitly set.
	if !cfg.Alerts.OnSuccess && !cfg.Alerts.OnFailure {
		alertCfg.OnFailure = true
	}

	// Environment variable overrides for sensitive values.
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
