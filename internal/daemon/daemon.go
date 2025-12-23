// Package daemon provides a long-running daemon for GitOps operations.
// It handles webhook reception, polling-based reconciliation, and health checks.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/cameronsjo/bosun/internal/alert"
	"github.com/cameronsjo/bosun/internal/reconcile"
	"github.com/cameronsjo/bosun/internal/ui"
)

// Config holds daemon configuration.
type Config struct {
	// HTTP server settings
	Port         int    // HTTP port for webhooks and health (default: 8080)
	WebhookPath  string // Path for webhook endpoint (default: /webhook)
	HealthPath   string // Path for health endpoint (default: /health)
	ReadyPath    string // Path for readiness endpoint (default: /ready)
	WebhookSecret string // Secret for validating webhook signatures

	// Polling settings
	PollInterval time.Duration // Interval between polls (0 disables polling)
	InitialDelay time.Duration // Delay before first poll (default: 10s)

	// Reconcile settings
	ReconcileConfig *reconcile.Config

	// Alerting
	AlertManager *alert.Manager
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Port:         8080,
		WebhookPath:  "/webhook",
		HealthPath:   "/health",
		ReadyPath:    "/ready",
		PollInterval: time.Hour,
		InitialDelay: 10 * time.Second,
	}
}

// Daemon is the main GitOps daemon that handles webhooks and polling.
type Daemon struct {
	config       *Config
	server       *Server
	reconciler   *reconcile.Reconciler
	alerter      *alert.Manager
	ready        bool
	readyMu      sync.RWMutex
	lastReconcile time.Time
	lastError     error
	reconcileMu   sync.RWMutex
	stopPoll     chan struct{}
}

// New creates a new Daemon with the given configuration.
func New(cfg *Config) (*Daemon, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	if cfg.ReconcileConfig == nil {
		cfg.ReconcileConfig = reconcile.DefaultConfig()
	}

	// Create reconciler with alerter if available
	var opts []reconcile.ReconcilerOption
	if cfg.AlertManager != nil {
		opts = append(opts, reconcile.WithAlerter(cfg.AlertManager))
	}

	d := &Daemon{
		config:     cfg,
		reconciler: reconcile.NewReconciler(cfg.ReconcileConfig, opts...),
		alerter:    cfg.AlertManager,
		stopPoll:   make(chan struct{}),
	}

	// Create HTTP server
	d.server = NewServer(d)

	return d, nil
}

// Run starts the daemon and blocks until shutdown.
// It handles SIGTERM and SIGINT for graceful shutdown.
func (d *Daemon) Run(ctx context.Context) error {
	ui.Header("=== Bosun Daemon Starting ===")
	ui.Info("Version: %s", getVersion())
	ui.Info("Port: %d", d.config.Port)
	ui.Info("Poll interval: %s", d.config.PollInterval)

	// Setup signal handling
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	// Error channel for goroutines
	errCh := make(chan error, 2)

	// Start HTTP server
	go func() {
		if err := d.server.Start(d.config.Port); err != nil {
			errCh <- fmt.Errorf("HTTP server: %w", err)
		}
	}()

	// Run initial reconciliation after delay
	go func() {
		time.Sleep(d.config.InitialDelay)
		ui.Info("Running initial reconciliation...")
		if err := d.runReconcile(ctx); err != nil {
			ui.Error("Initial reconciliation failed: %v", err)
		}
		d.setReady(true)
	}()

	// Start polling loop if enabled
	if d.config.PollInterval > 0 {
		go d.pollLoop(ctx)
	}

	ui.Success("Daemon ready")

	// Wait for shutdown signal or error
	select {
	case sig := <-sigCh:
		ui.Warning("Received signal %v, shutting down...", sig)
	case err := <-errCh:
		ui.Error("Fatal error: %v", err)
		return err
	case <-ctx.Done():
		ui.Warning("Context cancelled, shutting down...")
	}

	// Graceful shutdown
	return d.shutdown()
}

// shutdown performs graceful shutdown of all components.
func (d *Daemon) shutdown() error {
	ui.Info("Shutting down...")

	// Stop polling
	close(d.stopPoll)

	// Stop HTTP server with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := d.server.Shutdown(ctx); err != nil {
		ui.Warning("HTTP server shutdown: %v", err)
	}

	ui.Success("Shutdown complete")
	return nil
}

// TriggerReconcile triggers a reconciliation run.
// This is called by the webhook handler.
func (d *Daemon) TriggerReconcile(ctx context.Context) error {
	return d.runReconcile(ctx)
}

// runReconcile executes reconciliation with locking and state tracking.
func (d *Daemon) runReconcile(ctx context.Context) error {
	d.reconcileMu.Lock()
	defer d.reconcileMu.Unlock()

	start := time.Now()
	err := d.reconciler.Run(ctx)

	d.lastReconcile = time.Now()
	d.lastError = err

	if err != nil {
		ui.Error("Reconciliation failed after %s: %v", time.Since(start), err)
		return err
	}

	ui.Success("Reconciliation completed in %s", time.Since(start))
	return nil
}

// pollLoop runs periodic reconciliation.
func (d *Daemon) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(d.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			ui.Info("Poll triggered, running reconciliation...")
			if err := d.runReconcile(ctx); err != nil {
				ui.Error("Poll reconciliation failed: %v", err)
			}
		case <-d.stopPoll:
			return
		case <-ctx.Done():
			return
		}
	}
}

// IsReady returns whether the daemon is ready to serve requests.
func (d *Daemon) IsReady() bool {
	d.readyMu.RLock()
	defer d.readyMu.RUnlock()
	return d.ready
}

// setReady sets the readiness state.
func (d *Daemon) setReady(ready bool) {
	d.readyMu.Lock()
	defer d.readyMu.Unlock()
	d.ready = ready
}

// LastReconcile returns the time of the last reconciliation and any error.
func (d *Daemon) LastReconcile() (time.Time, error) {
	d.reconcileMu.RLock()
	defer d.reconcileMu.RUnlock()
	return d.lastReconcile, d.lastError
}

// HealthStatus returns the daemon health status.
func (d *Daemon) HealthStatus() HealthStatus {
	lastReconcile, lastError := d.LastReconcile()

	status := HealthStatus{
		Status:        "healthy",
		Ready:         d.IsReady(),
		LastReconcile: lastReconcile,
		Uptime:        time.Since(startTime),
	}

	if lastError != nil {
		status.Status = "degraded"
		status.LastError = lastError.Error()
	}

	return status
}

// HealthStatus represents the daemon health.
type HealthStatus struct {
	Status        string        `json:"status"`
	Ready         bool          `json:"ready"`
	LastReconcile time.Time     `json:"last_reconcile,omitempty"`
	LastError     string        `json:"last_error,omitempty"`
	Uptime        time.Duration `json:"uptime"`
}

var startTime = time.Now()

// getVersion returns the bosun version.
func getVersion() string {
	// This should be injected at build time via ldflags
	return "dev"
}

// ConfigFromEnv loads daemon configuration from environment variables.
func ConfigFromEnv() *Config {
	cfg := DefaultConfig()

	if port := os.Getenv("PORT"); port != "" {
		fmt.Sscanf(port, "%d", &cfg.Port)
	}
	if port := os.Getenv("WEBHOOK_PORT"); port != "" {
		fmt.Sscanf(port, "%d", &cfg.Port)
	}

	cfg.WebhookSecret = os.Getenv("WEBHOOK_SECRET")
	if secret := os.Getenv("GITHUB_WEBHOOK_SECRET"); secret != "" {
		cfg.WebhookSecret = secret
	}

	if interval := os.Getenv("POLL_INTERVAL"); interval != "" {
		if secs, err := time.ParseDuration(interval + "s"); err == nil {
			cfg.PollInterval = secs
		}
	}
	if interval := os.Getenv("BOSUN_POLL_INTERVAL"); interval != "" {
		if secs, err := time.ParseDuration(interval + "s"); err == nil {
			cfg.PollInterval = secs
		}
	}

	// Reconcile config from environment
	rcfg := reconcile.DefaultConfig()
	rcfg.RepoURL = os.Getenv("REPO_URL")
	if url := os.Getenv("BOSUN_REPO_URL"); url != "" {
		rcfg.RepoURL = url
	}

	if branch := os.Getenv("REPO_BRANCH"); branch != "" {
		rcfg.RepoBranch = branch
	}
	if branch := os.Getenv("BOSUN_REPO_BRANCH"); branch != "" {
		rcfg.RepoBranch = branch
	}

	if target := os.Getenv("DEPLOY_TARGET"); target != "" {
		rcfg.TargetHost = target
	}

	if secrets := os.Getenv("SECRETS_FILES"); secrets != "" {
		rcfg.SecretsFiles = splitAndTrim(secrets)
	}
	if secrets := os.Getenv("BOSUN_SECRETS_FILE"); secrets != "" {
		rcfg.SecretsFiles = splitAndTrim(secrets)
	}

	rcfg.DryRun = os.Getenv("DRY_RUN") == "true"

	cfg.ReconcileConfig = rcfg

	return cfg
}

// splitAndTrim splits a comma-separated string and trims whitespace.
func splitAndTrim(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// ValidateConfig validates the daemon configuration.
func ValidateConfig(cfg *Config) error {
	var errs []string

	if cfg.Port < 1 || cfg.Port > 65535 {
		errs = append(errs, fmt.Sprintf("invalid port: %d", cfg.Port))
	}

	if cfg.ReconcileConfig != nil {
		if cfg.ReconcileConfig.RepoURL == "" {
			errs = append(errs, "REPO_URL or BOSUN_REPO_URL is required")
		}
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}

	return nil
}
