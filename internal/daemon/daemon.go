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
	// Socket API settings (primary)
	SocketPath string // Path to Unix socket (default: /var/run/bosun.sock)

	// TCP API settings (optional, for remote access)
	EnableTCP   bool   // Enable TCP listener (default: false)
	TCPAddr     string // TCP address to listen on (default: 127.0.0.1:9090)
	BearerToken string // Bearer token for TCP authentication (required if EnableTCP)

	// HTTP server settings (for webhooks, kept for backwards compatibility)
	Port          int    // HTTP port for webhooks and health (default: 8080)
	EnableHTTP    bool   // Enable HTTP server (default: true for backwards compat)
	WebhookPath   string // Path for webhook endpoint (default: /webhook)
	HealthPath    string // Path for health endpoint (default: /health)
	ReadyPath     string // Path for readiness endpoint (default: /ready)
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
		SocketPath:   "/var/run/bosun.sock",
		EnableTCP:    false,                  // Disabled by default for security
		TCPAddr:      "127.0.0.1:9090",       // Localhost only by default
		Port:         8080,
		EnableHTTP:   true, // Backwards compat: enable HTTP by default for now
		WebhookPath:  "/webhook",
		HealthPath:   "/health",
		ReadyPath:    "/ready",
		PollInterval: time.Hour,
		InitialDelay: 10 * time.Second,
	}
}

// Daemon is the main GitOps daemon that handles webhooks and polling.
type Daemon struct {
	config        *Config
	socketServer  *SocketServer // Unix socket API (primary)
	tcpServer     *TCPServer    // TCP API with bearer auth (optional)
	httpServer    *Server       // HTTP server for webhooks (optional)
	reconciler    *reconcile.Reconciler
	alerter       *alert.Manager
	ready         bool
	readyMu       sync.RWMutex
	stopPoll      chan struct{}

	// Reconcile state (read frequently for health checks)
	stateMu       sync.RWMutex
	lastReconcile time.Time
	lastError     error

	// Concurrency control: single-flight reconcile with coalescing
	reconcileMu    sync.Mutex // Guards reconcile execution
	reconciling    bool       // True while reconcile is in progress
	pendingTrigger bool       // Dirty flag: another trigger arrived during reconcile
	triggerSource  string     // Source of pending trigger (for logging)
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

	// Create Unix socket server (primary API)
	socketCfg := &SocketConfig{
		SocketPath: cfg.SocketPath,
		SocketMode: 0660,
	}
	socketServer, err := NewSocketServer(d, socketCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create socket server: %w", err)
	}
	d.socketServer = socketServer

	// Create HTTP server for webhooks (optional, for backwards compat)
	if cfg.EnableHTTP {
		d.httpServer = NewServer(d)
	}

	// Create TCP server for remote access (optional)
	if cfg.EnableTCP {
		if cfg.BearerToken == "" {
			return nil, fmt.Errorf("bearer token required when TCP is enabled (set BOSUN_BEARER_TOKEN)")
		}
		tcpServer, err := NewTCPServer(d, cfg.TCPAddr, cfg.BearerToken)
		if err != nil {
			return nil, fmt.Errorf("failed to create TCP server: %w", err)
		}
		d.tcpServer = tcpServer
	}

	return d, nil
}

// Run starts the daemon and blocks until shutdown.
// It handles SIGTERM and SIGINT for graceful shutdown.
func (d *Daemon) Run(ctx context.Context) error {
	ui.Header("=== Bosun Daemon Starting ===")
	ui.Info("Version: %s", getVersion())
	ui.Info("Socket: %s", d.config.SocketPath)
	if d.config.EnableTCP {
		ui.Info("TCP: %s (bearer auth)", d.config.TCPAddr)
	}
	if d.config.EnableHTTP {
		ui.Info("HTTP Port: %d", d.config.Port)
	}
	ui.Info("Poll interval: %s", d.config.PollInterval)

	// Setup signal handling
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	// Error channel for goroutines
	errCh := make(chan error, 3)

	// Start Unix socket server (primary API)
	go func() {
		if err := d.socketServer.Start(); err != nil {
			errCh <- fmt.Errorf("socket server: %w", err)
		}
	}()

	// Start TCP server for remote access (optional)
	if d.config.EnableTCP && d.tcpServer != nil {
		go func() {
			if err := d.tcpServer.Start(); err != nil {
				errCh <- fmt.Errorf("TCP server: %w", err)
			}
		}()
	}

	// Start HTTP server for webhooks (optional)
	if d.config.EnableHTTP && d.httpServer != nil {
		go func() {
			if err := d.httpServer.Start(d.config.Port); err != nil {
				errCh <- fmt.Errorf("HTTP server: %w", err)
			}
		}()
	}

	// Run initial reconciliation after delay
	go func() {
		time.Sleep(d.config.InitialDelay)
		ui.Info("Running initial reconciliation...")
		if err := d.TriggerReconcile(ctx, "startup"); err != nil {
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

	// Shutdown timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Stop socket server
	if d.socketServer != nil {
		if err := d.socketServer.Shutdown(ctx); err != nil {
			ui.Warning("Socket server shutdown: %v", err)
		}
	}

	// Stop TCP server
	if d.tcpServer != nil {
		if err := d.tcpServer.Shutdown(ctx); err != nil {
			ui.Warning("TCP server shutdown: %v", err)
		}
	}

	// Stop HTTP server
	if d.httpServer != nil {
		if err := d.httpServer.Shutdown(ctx); err != nil {
			ui.Warning("HTTP server shutdown: %v", err)
		}
	}

	ui.Success("Shutdown complete")
	return nil
}

// TriggerReconcile triggers a reconciliation run.
// If a reconcile is already in progress, it sets the pending flag and returns immediately.
// The running reconcile will check the pending flag and re-run if set.
func (d *Daemon) TriggerReconcile(ctx context.Context, source string) error {
	d.reconcileMu.Lock()

	if d.reconciling {
		// Another reconcile is in progress - set dirty flag and return
		d.pendingTrigger = true
		d.triggerSource = source
		d.reconcileMu.Unlock()
		ui.Info("Reconcile already in progress, queued trigger from %s", source)
		return nil
	}

	// Mark as reconciling
	d.reconciling = true
	d.reconcileMu.Unlock()

	// Run the reconcile loop (may run multiple times if pending triggers arrive)
	return d.reconcileLoop(ctx, source)
}

// reconcileLoop runs reconciliation, checking for pending triggers after each run.
func (d *Daemon) reconcileLoop(ctx context.Context, source string) error {
	var lastErr error

	for {
		// Execute reconcile
		err := d.executeReconcile(ctx, source)
		if err != nil {
			lastErr = err
		}

		// Check for pending trigger
		d.reconcileMu.Lock()
		if d.pendingTrigger {
			// Another trigger arrived - reset flag and run again
			source = d.triggerSource
			d.pendingTrigger = false
			d.triggerSource = ""
			d.reconcileMu.Unlock()
			ui.Info("Processing queued trigger from %s", source)
			continue
		}

		// No pending trigger - we're done
		d.reconciling = false
		d.reconcileMu.Unlock()
		return lastErr
	}
}

// executeReconcile runs a single reconciliation and updates state.
func (d *Daemon) executeReconcile(ctx context.Context, source string) error {
	start := time.Now()
	ui.Info("Starting reconciliation (source: %s)", source)

	err := d.reconciler.Run(ctx)

	// Update state (use stateMu for thread-safe reads from health checks)
	d.stateMu.Lock()
	d.lastReconcile = time.Now()
	d.lastError = err
	d.stateMu.Unlock()

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
			ui.Info("Poll triggered")
			if err := d.TriggerReconcile(ctx, "poll"); err != nil {
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
	d.stateMu.RLock()
	defer d.stateMu.RUnlock()
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

	// Socket configuration
	if socketPath := os.Getenv("BOSUN_SOCKET_PATH"); socketPath != "" {
		cfg.SocketPath = socketPath
	}

	// HTTP configuration
	if port := os.Getenv("PORT"); port != "" {
		fmt.Sscanf(port, "%d", &cfg.Port)
	}
	if port := os.Getenv("WEBHOOK_PORT"); port != "" {
		fmt.Sscanf(port, "%d", &cfg.Port)
	}

	// Disable HTTP server if explicitly set
	if os.Getenv("BOSUN_DISABLE_HTTP") == "true" {
		cfg.EnableHTTP = false
	}

	// TCP server configuration (opt-in)
	if os.Getenv("BOSUN_ENABLE_TCP") == "true" {
		cfg.EnableTCP = true
	}
	if addr := os.Getenv("BOSUN_TCP_ADDR"); addr != "" {
		cfg.TCPAddr = addr
	}
	cfg.BearerToken = os.Getenv("BOSUN_BEARER_TOKEN")

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
