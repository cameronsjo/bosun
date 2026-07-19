// Package config handles project discovery and configuration.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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

	// deploySyncPaths is an allowlist of glob patterns for deploy sync targets.
	// When non-empty, only staging directory entries matching these patterns are deployed.
	deploySyncPaths []string

	// deploySyncExclude is a blocklist of glob patterns for deploy sync targets.
	// Matching entries are excluded from deployment. Exclude wins over include.
	deploySyncExclude []string

	// criticalContainers is a list of container names that must be healthy after compose up.
	criticalContainers []string

	// healthGateScope selects the post-compose-up health gate target set:
	// "critical" (default), "declared", or "off". Empty means "critical".
	healthGateScope string

	// templateIncludeDir overrides the subtree that template include/fromJsonFile
	// reads are confined to. Empty means <infraDir>/templates.
	templateIncludeDir string

	// driftIgnore is a list of rules for suppressing known drift noise.
	driftIgnore []reconcile.DriftIgnoreRule

	// driftAlertDebounce is the debounce window before first drift alert fires.
	driftAlertDebounce time.Duration

	// driftSelfHeal enables automatic reconciliation when drift is detected.
	driftSelfHeal bool

	// driftSelfHealCooldown is the minimum interval between self-heal reconciliations.
	driftSelfHealCooldown time.Duration

	// domain is the project-level domain for Traefik defaultRule.
	domain string

	// removeOrphans controls whether --remove-orphans is passed to docker compose up.
	removeOrphans bool

	// shutdownTimeout is the grace period for container stop operations (SIGTERM → SIGKILL).
	shutdownTimeout time.Duration

	// targets holds the parsed deployment target descriptors.
	targets []reconcile.Target
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

	// Slack
	SlackWebhookURL string `yaml:"slack_webhook_url"`

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

	// Webhook
	WebhookURL     string            `yaml:"webhook_url"`
	WebhookHeaders map[string]string `yaml:"webhook_headers"`
	WebhookMethod  string            `yaml:"webhook_method"`

	// Settings
	OnSuccess bool `yaml:"on_success"` // Alert on successful deploys
	OnFailure bool `yaml:"on_failure"` // Alert on failed deploys (default: true)
}

// alertConfigRaw is the YAML DTO for alert settings.
// Pointer booleans distinguish "unset" (nil → apply default) from explicit false.
type alertConfigRaw struct {
	DiscordWebhookURL string            `yaml:"discord_webhook_url"`
	SlackWebhookURL   string            `yaml:"slack_webhook_url"`
	SendGridAPIKey    string            `yaml:"sendgrid_api_key"`
	SendGridFromEmail string            `yaml:"sendgrid_from_email"`
	SendGridFromName  string            `yaml:"sendgrid_from_name"`
	SendGridToEmails  []string          `yaml:"sendgrid_to_emails"`
	TwilioAccountSID  string            `yaml:"twilio_account_sid"`
	TwilioAuthToken   string            `yaml:"twilio_auth_token"`
	TwilioFromNumber  string            `yaml:"twilio_from_number"`
	TwilioToNumbers   []string          `yaml:"twilio_to_numbers"`
	WebhookURL        string            `yaml:"webhook_url"`
	WebhookHeaders    map[string]string `yaml:"webhook_headers"`
	WebhookMethod     string            `yaml:"webhook_method"`
	OnSuccess         *bool             `yaml:"on_success"`
	OnFailure         *bool             `yaml:"on_failure"`
}

// targetRaw is the YAML DTO for a deployment target.
type targetRaw struct {
	Name               string                   `yaml:"name"`
	TargetHost         string                   `yaml:"target_host"`
	LocalAppdataPath   string                   `yaml:"local_appdata_path"`
	RemoteAppdataPath  string                   `yaml:"remote_appdata_path"`
	ProjectName        string                   `yaml:"project_name"`
	SecretsScope       string                   `yaml:"secrets_scope"`
	CriticalContainers []string                 `yaml:"critical_containers"`
	PostSyncHooks      []reconcile.PostSyncHook `yaml:"post_sync_hooks"`
	DeploySyncPaths    []string                 `yaml:"deploy_sync_paths"`
	DeploySyncExclude  []string                 `yaml:"deploy_sync_exclude"`
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

	// Alerts configuration (uses pointer booleans for unset detection).
	Alerts alertConfigRaw `yaml:"alerts"`

	// PostSyncHooks defines container restart actions triggered by file changes.
	PostSyncHooks []reconcile.PostSyncHook `yaml:"post_sync_hooks"`

	// HookSettleDelay is a global pause after deploy before post-sync hooks run.
	HookSettleDelay reconcile.Duration `yaml:"hook_settle_delay"`

	// DeployPaths is an allowlist of glob patterns for deploy-relevant paths.
	// When configured, commits that only touch files outside these patterns skip the pipeline.
	DeployPaths []string `yaml:"deploy_paths"`

	// DeploySyncPaths is an allowlist of glob patterns for deploy sync targets.
	// When non-empty, only staging directory entries matching these patterns are deployed.
	DeploySyncPaths []string `yaml:"deploy_sync_paths"`

	// DeploySyncExclude is a blocklist of glob patterns for deploy sync targets.
	// Matching entries are excluded from deployment. Exclude wins over include.
	DeploySyncExclude []string `yaml:"deploy_sync_exclude"`

	// CriticalContainers is a list of container names that must be healthy after compose up.
	// When configured, the health gate runs after startup grace period and before state save.
	CriticalContainers []string `yaml:"critical_containers"`

	// HealthGateScope selects the post-compose-up health gate target set:
	// "critical" (default), "declared", or "off". Empty means "critical".
	HealthGateScope string `yaml:"health_gate_scope"`

	// TemplateIncludeDir overrides the subtree that template include/fromJsonFile
	// reads are confined to. Empty means <infraDir>/templates. Relative values
	// are resolved against the infra directory; absolute values are used as-is.
	TemplateIncludeDir string `yaml:"template_include_dir"`

	// DriftIgnore is a list of rules for suppressing known drift noise.
	// Each rule matches a service name (glob) and drift type.
	DriftIgnore []reconcile.DriftIgnoreRule `yaml:"drift_ignore"`

	// DriftAlertDebounce is the debounce window before first drift alert fires.
	// Items must persist past this duration before alerting. 0 = disabled (default).
	DriftAlertDebounce reconcile.Duration `yaml:"drift_alert_debounce"`

	// DriftSelfHeal enables automatic reconciliation when drift is detected.
	// When true, the daemon triggers a reconcile after detecting drift. Default: false.
	DriftSelfHeal *bool `yaml:"drift_self_heal"`

	// DriftSelfHealCooldown is the minimum interval between self-heal reconciliations.
	// Prevents rapid-fire reconciliations. Default: 15m.
	DriftSelfHealCooldown reconcile.Duration `yaml:"drift_self_heal_cooldown"`

	// RemoveOrphans controls whether --remove-orphans is passed to docker compose up.
	// Defaults to true (preserving existing behavior). Set to false in shared environments
	// where Bosun does not own all containers on the Docker host.
	RemoveOrphans *bool `yaml:"remove_orphans"`

	// ShutdownTimeout is the grace period for container stop operations (SIGTERM → SIGKILL).
	// Defaults to 30s. Docker's default is 10s; increase for containers with long-running requests.
	ShutdownTimeout reconcile.Duration `yaml:"shutdown_timeout"`

	// Targets defines multiple deployment targets. Each target has its own host,
	// paths, and per-target overrides. When absent or empty, the reconciler uses
	// an implicit default target from the flat config fields.
	Targets []targetRaw `yaml:"targets"`

	// TargetHost is the legacy flat field for a single remote target.
	// Deprecated: use targets: section instead.
	TargetHost string `yaml:"target_host"`
}

// FindRoot searches upward from the current directory to find the project root.
// The project root is identified by:
// - bosun.yaml or bosun.yml config file
// - bosun/ directory with docker-compose.yml
// - manifest/ or manifests/ directory (not accepted when candidate dir is $HOME)
//
// When the only matching marker in a candidate directory is manifest/ or manifests/
// and that directory equals the user's home directory, FindRoot refuses to anchor
// there. Generic directory names like "manifest/" are common in npm projects, OCI
// tooling, and packaging pipelines; anchoring bosun to $HOME on their presence
// would cause it to operate on the wrong directory silently.
func FindRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}

	homeDir, _ := os.UserHomeDir()
	// Normalize $HOME for comparison: os.Getwd() returns a symlink-resolved
	// path on most platforms (macOS returns /private/var/... not /var/...),
	// but os.UserHomeDir() returns the raw $HOME value. Without normalization
	// the equality check in the weak-marker guard never fires when $HOME
	// contains symlink components. If EvalSymlinks fails (deleted dir,
	// permission issue), skip normalization and proceed — the guard may miss
	// an edge case but the caller is not harmed.
	if homeDir != "" {
		if resolved, err := filepath.EvalSymlinks(homeDir); err == nil {
			homeDir = resolved
		}
	}

	for dir != "/" {
		// Check for bosun.yaml or bosun.yml config file (strong marker — always accepted).
		for _, name := range []string{"bosun.yaml", "bosun.yml"} {
			configPath := filepath.Join(dir, name)
			if _, err := os.Stat(configPath); err == nil {
				return dir, nil
			}
		}

		// Check for bosun/ directory with docker-compose.yml (strong marker — always accepted).
		bosunDir := filepath.Join(dir, "bosun")
		if info, err := os.Stat(bosunDir); err == nil && info.IsDir() {
			composeFile := filepath.Join(bosunDir, "docker-compose.yml")
			if _, err := os.Stat(composeFile); err == nil {
				return dir, nil
			}
		}

		// Check for manifest/ or manifests/ directory (weak marker).
		// Refuse to anchor on these alone when the candidate directory is $HOME —
		// these names are too generic and collide with unrelated tooling.
		if homeDir == "" || dir != homeDir {
			for _, name := range []string{"manifest", "manifests"} {
				manifestDir := filepath.Join(dir, name)
				if info, err := os.Stat(manifestDir); err == nil && info.IsDir() {
					return dir, nil
				}
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
	fileCfg, err := loadConfigFile(dir)
	if err != nil {
		return nil, err
	}

	// Check if any config was actually loaded by testing whether
	// loadConfigFile found and parsed a file. Since configFile is a value
	// type, we check for non-zero state in any field.
	postSyncHooks := extractPostSyncHooks(fileCfg)
	hookSettleDelay := extractHookSettleDelay(fileCfg)
	deployPaths := extractDeployPaths(fileCfg)
	deploySyncPaths := extractDeploySyncPaths(fileCfg)
	deploySyncExclude := extractDeploySyncExclude(fileCfg)
	criticalContainers := extractCriticalContainers(fileCfg)
	healthGateScope := extractHealthGateScope(fileCfg)
	templateIncludeDir := extractTemplateIncludeDir(fileCfg)
	driftIgnore := extractDriftIgnore(fileCfg)
	driftAlertDebounce := extractDriftAlertDebounce(fileCfg)
	driftSelfHeal := extractDriftSelfHeal(fileCfg)
	driftSelfHealCooldown := extractDriftSelfHealCooldown(fileCfg)
	domain := extractDomain(fileCfg)
	removeOrphans := extractRemoveOrphans(fileCfg)
	shutdownTimeout := extractShutdownTimeout(fileCfg)
	targets := extractTargets(fileCfg)

	return &Config{
		Root:                  dir,
		postSyncHooks:         postSyncHooks,
		hookSettleDelay:       hookSettleDelay,
		deployPaths:           deployPaths,
		deploySyncPaths:       deploySyncPaths,
		deploySyncExclude:     deploySyncExclude,
		criticalContainers:    criticalContainers,
		healthGateScope:       healthGateScope,
		templateIncludeDir:    templateIncludeDir,
		driftIgnore:           driftIgnore,
		driftAlertDebounce:    driftAlertDebounce,
		driftSelfHeal:         driftSelfHeal,
		driftSelfHealCooldown: driftSelfHealCooldown,
		domain:                domain,
		removeOrphans:         removeOrphans,
		shutdownTimeout:       shutdownTimeout,
		targets:               targets,
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
	fileCfg, err := loadConfigFile(root)
	if err != nil {
		return nil, err
	}

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
	deploySyncPaths := extractDeploySyncPaths(fileCfg)
	deploySyncExclude := extractDeploySyncExclude(fileCfg)
	criticalContainers := extractCriticalContainers(fileCfg)
	healthGateScope := extractHealthGateScope(fileCfg)
	templateIncludeDir := extractTemplateIncludeDir(fileCfg)
	driftIgnore := extractDriftIgnore(fileCfg)
	driftAlertDebounce := extractDriftAlertDebounce(fileCfg)
	driftSelfHeal := extractDriftSelfHeal(fileCfg)
	driftSelfHealCooldown := extractDriftSelfHealCooldown(fileCfg)
	domain := extractDomain(fileCfg)
	removeOrphans := extractRemoveOrphans(fileCfg)
	shutdownTimeout := extractShutdownTimeout(fileCfg)
	targets := extractTargets(fileCfg)

	// Determine project name (defaults to directory name)
	projectName := fileCfg.ProjectName
	if projectName == "" {
		projectName = filepath.Base(root)
	}
	// Validate project name before it reaches shell commands. Warn and clear
	// to avoid injecting attacker-controlled strings into SSH-built commands.
	if fileCfg.ProjectName != "" {
		if err := reconcile.ValidateProjectName(projectName); err != nil {
			log.Warn().Err(err).Str("project_name", projectName).
				Msg("Config: invalid project_name — ignoring and falling back to directory name")
			projectName = filepath.Base(root)
		}
	}
	// Also validate the fallback value: the project directory name itself may
	// contain characters invalid for docker compose project names (e.g. spaces).
	// Sanitize by replacing spaces with underscores; log a warning so operators
	// know the effective project name differs from their directory name.
	if err := reconcile.ValidateProjectName(projectName); err != nil {
		sanitized := strings.ReplaceAll(projectName, " ", "_")
		log.Warn().Err(err).
			Str("project_name", projectName).
			Str("sanitized", sanitized).
			Msg("Config: project name derived from directory contains invalid characters — using sanitized version")
		projectName = sanitized
	}

	cfg := &Config{
		Root:                  root,
		ManifestDir:           manifestDir,
		provisionsDir:         provisionsDir,
		ComposeFile:           filepath.Join(root, "bosun", "docker-compose.yml"),
		SnapshotsDir:          filepath.Join(manifestDir, ".bosun", "snapshots"),
		projectName:           projectName,
		infraContainers:       infraContainers,
		tunnelProvider:        tunnelProvider,
		tunnelConfig:          tunnelConfig,
		alertConfig:           alertConfig,
		postSyncHooks:         postSyncHooks,
		hookSettleDelay:       hookSettleDelay,
		deployPaths:           deployPaths,
		deploySyncPaths:       deploySyncPaths,
		deploySyncExclude:     deploySyncExclude,
		criticalContainers:    criticalContainers,
		healthGateScope:       healthGateScope,
		templateIncludeDir:    templateIncludeDir,
		driftIgnore:           driftIgnore,
		driftAlertDebounce:    driftAlertDebounce,
		driftSelfHeal:         driftSelfHeal,
		driftSelfHealCooldown: driftSelfHealCooldown,
		domain:                domain,
		removeOrphans:         removeOrphans,
		shutdownTimeout:       shutdownTimeout,
		targets:               targets,
	}

	logger := log.Component("config")
	logger.Debug().
		Str(log.FieldPath, root).
		Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
		Msg("Successfully loaded project configuration")

	return cfg, nil
}

// loadConfigFile loads the bosun config file if present.
// Returns a zero-value configFile with no error if no config file exists.
// Returns an error if a config file exists but contains malformed YAML or unknown fields.
func loadConfigFile(root string) (configFile, error) {
	var cfg configFile

	for _, name := range []string{"bosun.yaml", "bosun.yml", ".bosun/config.yml"} {
		path := filepath.Join(root, name)
		data, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return configFile{}, fmt.Errorf("failed to read config file %s: %w", path, err)
		}

		dec := yaml.NewDecoder(bytes.NewReader(data))
		dec.KnownFields(true)
		if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
			return configFile{}, fmt.Errorf("failed to parse config file %s: %w", path, err)
		}

		log.Debug().
			Str(log.FieldPath, path).
			Msg("Loaded config file")
		return cfg, nil
	}

	return cfg, nil
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

// DeploySyncPaths returns the configured deploy sync path allowlist patterns.
func (c *Config) DeploySyncPaths() []string {
	return c.deploySyncPaths
}

// extractDeploySyncPaths extracts deploy sync path patterns from a parsed config.
func extractDeploySyncPaths(cfg configFile) []string {
	return cfg.DeploySyncPaths
}

// DeploySyncExclude returns the configured deploy sync exclude patterns.
func (c *Config) DeploySyncExclude() []string {
	return c.deploySyncExclude
}

// extractDeploySyncExclude extracts deploy sync exclude patterns from a parsed config.
func extractDeploySyncExclude(cfg configFile) []string {
	return cfg.DeploySyncExclude
}

// CriticalContainers returns the list of container names that must be healthy after deploy.
func (c *Config) CriticalContainers() []string {
	return c.criticalContainers
}

// extractCriticalContainers extracts critical container names from a parsed config.
func extractCriticalContainers(cfg configFile) []string {
	return cfg.CriticalContainers
}

// HealthGateScope returns the configured health gate scope ("critical",
// "declared", "off", or "" for the default). Validation of unknown values
// happens at the reconcile layer (resolveHealthGateScope).
func (c *Config) HealthGateScope() string {
	return c.healthGateScope
}

// extractHealthGateScope extracts the health gate scope from a parsed config.
func extractHealthGateScope(cfg configFile) string {
	return cfg.HealthGateScope
}

// TemplateIncludeDir returns the configured include subtree override for
// template include/fromJsonFile reads. Empty means the default (<infraDir>/templates).
func (c *Config) TemplateIncludeDir() string {
	return c.templateIncludeDir
}

// extractTemplateIncludeDir extracts the template include dir override from a parsed config.
func extractTemplateIncludeDir(cfg configFile) string {
	return cfg.TemplateIncludeDir
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

// DriftIgnore returns the configured drift ignore rules.
func (c *Config) DriftIgnore() []reconcile.DriftIgnoreRule {
	return c.driftIgnore
}

// DriftAlertDebounce returns the configured drift alert debounce duration.
func (c *Config) DriftAlertDebounce() time.Duration {
	return c.driftAlertDebounce
}

// extractDriftIgnore extracts drift ignore rules from a parsed config.
func extractDriftIgnore(cfg configFile) []reconcile.DriftIgnoreRule {
	return cfg.DriftIgnore
}

// extractDriftAlertDebounce extracts the drift alert debounce from a parsed config.
func extractDriftAlertDebounce(cfg configFile) time.Duration {
	return cfg.DriftAlertDebounce.Duration
}

// DriftSelfHeal returns whether drift self-healing is enabled.
func (c *Config) DriftSelfHeal() bool {
	return c.driftSelfHeal
}

// extractDriftSelfHeal extracts the drift self-heal setting from a parsed config.
func extractDriftSelfHeal(cfg configFile) bool {
	if cfg.DriftSelfHeal != nil {
		return *cfg.DriftSelfHeal
	}
	return false
}

// DriftSelfHealCooldown returns the configured drift self-heal cooldown duration.
func (c *Config) DriftSelfHealCooldown() time.Duration {
	return c.driftSelfHealCooldown
}

// defaultDriftSelfHealCooldown is the default cooldown between drift self-heal reconciliations.
const defaultDriftSelfHealCooldown = 15 * time.Minute

// extractDriftSelfHealCooldown extracts the drift self-heal cooldown from a parsed config.
// Returns the documented default (15m) when unset so callers of Config.DriftSelfHealCooldown()
// get the correct value without relying on daemon.DefaultConfig().
func extractDriftSelfHealCooldown(cfg configFile) time.Duration {
	if cfg.DriftSelfHealCooldown.Duration > 0 {
		return cfg.DriftSelfHealCooldown.Duration
	}
	return defaultDriftSelfHealCooldown
}

// extractRemoveOrphans extracts the remove_orphans setting from a parsed config.
// Defaults to true when not explicitly set (preserving existing behavior).
func extractRemoveOrphans(cfg configFile) bool {
	if cfg.RemoveOrphans != nil {
		return *cfg.RemoveOrphans
	}
	return true
}

// RemoveOrphans returns whether --remove-orphans should be passed to docker compose up.
// Defaults to true.
func (c *Config) RemoveOrphans() bool {
	return c.removeOrphans
}

// defaultShutdownTimeout is the default grace period for container stop operations.
const defaultShutdownTimeout = 30 * time.Second

// ShutdownTimeout returns the configured grace period for container stop operations.
// Defaults to 30s. This is the time between SIGTERM and SIGKILL during container stops.
func (c *Config) ShutdownTimeout() time.Duration {
	return c.shutdownTimeout
}

// extractShutdownTimeout extracts the shutdown timeout from a parsed config.
// Defaults to 30s when not configured.
func extractShutdownTimeout(cfg configFile) time.Duration {
	if cfg.ShutdownTimeout.Duration > 0 {
		return cfg.ShutdownTimeout.Duration
	}
	// Check environment variable override
	if v := os.Getenv("BOSUN_STOP_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
		log.Warn().Str("env", "BOSUN_STOP_TIMEOUT").Str("value", v).Msg("Skipping env var. Reason: invalid duration format")
	}
	return defaultShutdownTimeout
}

// Targets returns the configured deployment target descriptors.
// Returns nil when no targets are configured (use reconcile.Config.ResolveTargets()
// to get the effective target list with the implicit default).
func (c *Config) Targets() []reconcile.Target {
	return c.targets
}

// extractTargets converts raw YAML target entries into reconcile.Target descriptors.
// Logs a deprecation warning when both targets: and flat target_host are present.
func extractTargets(cfg configFile) []reconcile.Target {
	if len(cfg.Targets) == 0 {
		return nil
	}

	// Warn when both targets: and flat target_host are set (mixed config).
	if cfg.TargetHost != "" {
		log.Warn().
			Msg("Both 'targets:' section and flat 'target_host' field are present in config. " +
				"The flat field is deprecated and will be ignored. Use 'targets:' exclusively.")
	}

	targets := make([]reconcile.Target, 0, len(cfg.Targets))
	for _, raw := range cfg.Targets {
		if raw.Name == "" {
			log.Warn().Msg("Skipping target with empty name in config")
			continue
		}

		// Validate security-sensitive fields that reach SSH shell commands.
		// Warn and clear (rather than skip the whole target) so a single bad
		// field does not block all deployments to that host.
		projectName := raw.ProjectName
		if projectName != "" {
			if err := reconcile.ValidateProjectName(projectName); err != nil {
				log.Warn().Err(err).Str("target", raw.Name).Str("project_name", projectName).
					Msg("Config: invalid project_name on target — ignoring and inheriting global value")
				projectName = ""
			}
		}
		remoteAppdataPath := raw.RemoteAppdataPath
		if remoteAppdataPath != "" {
			if err := reconcile.ValidateRemotePath(remoteAppdataPath); err != nil {
				log.Warn().Err(err).Str("target", raw.Name).Str("remote_appdata_path", remoteAppdataPath).
					Msg("Config: invalid remote_appdata_path on target — ignoring")
				remoteAppdataPath = ""
			}
		}

		targets = append(targets, reconcile.Target{
			Name:               raw.Name,
			TargetHost:         raw.TargetHost,
			LocalAppdataPath:   raw.LocalAppdataPath,
			RemoteAppdataPath:  remoteAppdataPath,
			ProjectName:        projectName,
			SecretsScope:       raw.SecretsScope,
			CriticalContainers: raw.CriticalContainers,
			PostSyncHooks:      raw.PostSyncHooks,
			DeploySyncPaths:    raw.DeploySyncPaths,
			DeploySyncExclude:  raw.DeploySyncExclude,
		})
	}

	return targets
}

// getEnvOrDefault returns the value of the environment variable if set and non-empty,
// otherwise returns the provided default value.
func getEnvOrDefault(envKey, defaultValue string) string {
	if v := os.Getenv(envKey); v != "" {
		return v
	}
	return defaultValue
}

// splitCommaSeparated splits a comma-separated env-var value into a trimmed,
// non-empty string slice. Returns nil when the input is empty or blank.
func splitCommaSeparated(v string) []string {
	if v == "" {
		return nil
	}
	var result []string
	for _, s := range strings.Split(v, ",") {
		if t := strings.TrimSpace(s); t != "" {
			result = append(result, t)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// AlertConfigFromEnv builds an AlertConfig purely from environment variables,
// applying BOSUN_-first precedence without needing a project config file.
// This is used as a fallback when config.Load() fails (no bosun.yaml present).
func AlertConfigFromEnv() AlertConfig {
	return AlertConfig{
		DiscordWebhookURL: getEnvOrDefault("BOSUN_DISCORD_WEBHOOK_URL", getEnvOrDefault("DISCORD_WEBHOOK_URL", "")),
		SlackWebhookURL:   getEnvOrDefault("BOSUN_SLACK_WEBHOOK_URL", getEnvOrDefault("SLACK_WEBHOOK_URL", "")),
		SendGridAPIKey:    getEnvOrDefault("BOSUN_SENDGRID_API_KEY", getEnvOrDefault("SENDGRID_API_KEY", "")),
		SendGridFromEmail: getEnvOrDefault("BOSUN_SENDGRID_FROM_EMAIL", getEnvOrDefault("SENDGRID_FROM_EMAIL", "")),
		SendGridFromName:  getEnvOrDefault("BOSUN_SENDGRID_FROM_NAME", getEnvOrDefault("SENDGRID_FROM_NAME", "")),
		// BOSUN_SENDGRID_TO_EMAILS is a comma-separated list of recipient addresses.
		// Without this, a provider initialized via env vars would have credentials but no
		// recipients, causing IsConfigured() == false and silent alert drops.
		SendGridToEmails: splitCommaSeparated(os.Getenv("BOSUN_SENDGRID_TO_EMAILS")),
		TwilioAccountSID: getEnvOrDefault("BOSUN_TWILIO_ACCOUNT_SID", getEnvOrDefault("TWILIO_ACCOUNT_SID", "")),
		TwilioAuthToken:  getEnvOrDefault("BOSUN_TWILIO_AUTH_TOKEN", getEnvOrDefault("TWILIO_AUTH_TOKEN", "")),
		TwilioFromNumber: getEnvOrDefault("BOSUN_TWILIO_FROM_NUMBER", getEnvOrDefault("TWILIO_FROM_NUMBER", "")),
		// BOSUN_TWILIO_TO_NUMBERS is a comma-separated list of recipient phone numbers.
		TwilioToNumbers: splitCommaSeparated(os.Getenv("BOSUN_TWILIO_TO_NUMBERS")),
		WebhookURL:      getEnvOrDefault("BOSUN_WEBHOOK_URL", ""),
		WebhookMethod:   getEnvOrDefault("BOSUN_WEBHOOK_METHOD", ""),
		// OnFailure defaults to true when neither flag is set (same as extractAlertConfig).
		OnFailure: true,
	}
}

// extractAlertConfig extracts alert configuration from a parsed config.
// Supports environment variable overrides for sensitive values.
func extractAlertConfig(cfg configFile) AlertConfig {
	raw := cfg.Alerts

	alertCfg := AlertConfig{
		DiscordWebhookURL: raw.DiscordWebhookURL,
		SlackWebhookURL:   raw.SlackWebhookURL,
		SendGridAPIKey:    raw.SendGridAPIKey,
		SendGridFromEmail: raw.SendGridFromEmail,
		SendGridFromName:  raw.SendGridFromName,
		SendGridToEmails:  raw.SendGridToEmails,
		TwilioAccountSID:  raw.TwilioAccountSID,
		TwilioAuthToken:   raw.TwilioAuthToken,
		TwilioFromNumber:  raw.TwilioFromNumber,
		TwilioToNumbers:   raw.TwilioToNumbers,
		WebhookURL:        raw.WebhookURL,
		WebhookHeaders:    raw.WebhookHeaders,
		WebhookMethod:     raw.WebhookMethod,
	}

	// Resolve pointer booleans: nil = unset (apply default), non-nil = explicit.
	// Default: OnFailure=true when neither flag was explicitly set.
	if raw.OnSuccess != nil {
		alertCfg.OnSuccess = *raw.OnSuccess
	}
	if raw.OnFailure != nil {
		alertCfg.OnFailure = *raw.OnFailure
	} else if raw.OnSuccess == nil {
		// Neither flag set → default OnFailure to true.
		alertCfg.OnFailure = true
	}

	// Environment variable overrides for sensitive values.
	// BOSUN_-prefixed vars take precedence; legacy unprefixed vars are fallback.
	alertCfg.DiscordWebhookURL = getEnvOrDefault("BOSUN_DISCORD_WEBHOOK_URL", getEnvOrDefault("DISCORD_WEBHOOK_URL", alertCfg.DiscordWebhookURL))
	alertCfg.SlackWebhookURL = getEnvOrDefault("BOSUN_SLACK_WEBHOOK_URL", getEnvOrDefault("SLACK_WEBHOOK_URL", alertCfg.SlackWebhookURL))
	alertCfg.SendGridAPIKey = getEnvOrDefault("BOSUN_SENDGRID_API_KEY", getEnvOrDefault("SENDGRID_API_KEY", alertCfg.SendGridAPIKey))
	alertCfg.SendGridFromEmail = getEnvOrDefault("BOSUN_SENDGRID_FROM_EMAIL", getEnvOrDefault("SENDGRID_FROM_EMAIL", alertCfg.SendGridFromEmail))
	alertCfg.SendGridFromName = getEnvOrDefault("BOSUN_SENDGRID_FROM_NAME", getEnvOrDefault("SENDGRID_FROM_NAME", alertCfg.SendGridFromName))
	alertCfg.TwilioAccountSID = getEnvOrDefault("BOSUN_TWILIO_ACCOUNT_SID", getEnvOrDefault("TWILIO_ACCOUNT_SID", alertCfg.TwilioAccountSID))
	alertCfg.TwilioAuthToken = getEnvOrDefault("BOSUN_TWILIO_AUTH_TOKEN", getEnvOrDefault("TWILIO_AUTH_TOKEN", alertCfg.TwilioAuthToken))
	alertCfg.TwilioFromNumber = getEnvOrDefault("BOSUN_TWILIO_FROM_NUMBER", getEnvOrDefault("TWILIO_FROM_NUMBER", alertCfg.TwilioFromNumber))

	// Webhook environment variable overrides.
	alertCfg.WebhookURL = getEnvOrDefault("BOSUN_WEBHOOK_URL", alertCfg.WebhookURL)
	if headersJSON := os.Getenv("BOSUN_WEBHOOK_HEADERS"); headersJSON != "" {
		var headers map[string]string
		if err := json.Unmarshal([]byte(headersJSON), &headers); err == nil {
			alertCfg.WebhookHeaders = headers
		} else {
			log.Warn().
				Err(err).
				Msg("Failed to parse BOSUN_WEBHOOK_HEADERS as JSON, ignoring")
		}
	}
	alertCfg.WebhookMethod = getEnvOrDefault("BOSUN_WEBHOOK_METHOD", alertCfg.WebhookMethod)

	return alertCfg
}
