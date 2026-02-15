// Package daemon provides a long-running daemon for GitOps operations.
// It handles webhook reception, polling-based reconciliation, and health checks.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/cameronsjo/bosun/internal/alert"
	"github.com/cameronsjo/bosun/internal/docker"
	"github.com/cameronsjo/bosun/internal/log"
	"github.com/cameronsjo/bosun/internal/reconcile"
	sentrypkg "github.com/cameronsjo/bosun/internal/sentry"
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

	// Timeout settings
	ReconcileTimeout time.Duration // Max time for a reconcile operation (default: 10m)
	ShutdownTimeout  time.Duration // Max time for graceful shutdown (default: 30s)
	APITimeout       time.Duration // Timeout for API handler requests (default: 30s)

	// Drift check settings
	DriftInterval time.Duration // Interval for periodic drift checks (0 disables, default: 5m)

	// Alerting
	AlertManager *alert.Manager

	// Version is the bosun version string (set by caller from build-time ldflags).
	Version string
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		SocketPath:       DefaultSocketPath,
		EnableTCP:        false,                  // Disabled by default for security
		TCPAddr:          "127.0.0.1:9090",       // Localhost only by default
		Port:             8080,
		EnableHTTP:       true, // Backwards compat: enable HTTP by default for now
		WebhookPath:      "/webhook",
		HealthPath:       "/health",
		ReadyPath:        "/ready",
		PollInterval:     time.Hour,
		InitialDelay:     10 * time.Second,
		ReconcileTimeout: 10 * time.Minute,
		ShutdownTimeout:  30 * time.Second,
		APITimeout:       30 * time.Second,
		DriftInterval:    5 * time.Minute,
	}
}

// DefaultSocketPath is the default path for the bosun daemon Unix socket.
const DefaultSocketPath = "/var/run/bosun.sock"

// Daemon is the main GitOps daemon that handles webhooks and polling.
type Daemon struct {
	config        *Config
	socketServer  *SocketServer // Unix socket API (primary)
	tcpServer     *TCPServer    // TCP API with bearer auth (optional)
	httpServer    *Server       // HTTP server for webhooks (optional)
	reconciler    *reconcile.Reconciler
	alerter       *alert.Manager
	dockerOnce    sync.Once      // Lazily initialize Docker client
	dockerClient  *docker.Client // Shared Docker client for API handlers
	dockerErr     error          // Error from Docker client initialization
	ready         bool
	readyMu       sync.RWMutex
	stopLoops      chan struct{}

	// Track background goroutines for graceful shutdown
	wg sync.WaitGroup

	// Reconcile state (read frequently for health checks)
	stateMu       sync.RWMutex
	lastReconcile time.Time
	lastError     error

	// Concurrency control: single-flight reconcile with coalescing
	reconcileMu    sync.Mutex // Guards reconcile execution
	reconciling    bool       // True while reconcile is in progress
	pendingTrigger bool       // Dirty flag: another trigger arrived during reconcile
	triggerSource  string     // Source of pending trigger (for logging)
	triggerForce   bool       // Force flag for pending trigger (sticky: once set, stays set)
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
		config:   cfg,
		alerter:  cfg.AlertManager,
		stopLoops: make(chan struct{}),
	}

	// Lazily inject Docker client into reconciler for post-deploy verification.
	// The client is initialized on first use via DockerClient().
	opts = append(opts, reconcile.WithDockerClientFunc(func() *docker.Client {
		client, err := d.DockerClient()
		if err != nil {
			return nil
		}
		return client
	}))

	d.reconciler = reconcile.NewReconciler(cfg.ReconcileConfig, opts...)

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
	defer sentrypkg.Recover()

	// Initialize structured logging for daemon mode (JSON output).
	_ = os.Setenv("BOSUN_DAEMON_MODE", "true")
	log.Init(nil)

	logger := log.Component(log.ComponentDaemon)

	logger.Info().
		Str("version", versionOrDev(d.config.Version)).
		Str("socket", d.config.SocketPath).
		Bool("tcp_enabled", d.config.EnableTCP).
		Str("tcp_addr", d.config.TCPAddr).
		Bool("http_enabled", d.config.EnableHTTP).
		Int("http_port", d.config.Port).
		Dur("poll_interval", d.config.PollInterval).
		Msg("Daemon starting")

	ui.Header("=== Bosun Daemon Starting ===")
	ui.Info("Version: %s", versionOrDev(d.config.Version))
	ui.Info("Socket: %s", d.config.SocketPath)
	if d.config.EnableTCP {
		ui.Info("TCP: %s (bearer auth)", d.config.TCPAddr)
	}
	if d.config.EnableHTTP {
		ui.Info("HTTP Port: %d", d.config.Port)
	}
	ui.Info("Poll interval: %s", d.config.PollInterval)

	// Ensure state directory exists for deploy state tracking.
	if d.config.ReconcileConfig != nil && d.config.ReconcileConfig.StateFile != "" {
		stateDir := filepath.Dir(d.config.ReconcileConfig.StateFile)
		if err := os.MkdirAll(stateDir, 0755); err != nil {
			logger.Warn().Err(err).Str("dir", stateDir).Msg("Failed to create state directory")
		}
	}

	// Setup signal handling
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	// Error channel for goroutines
	errCh := make(chan error, 3)

	// Start Unix socket server (primary API)
	logger.Debug().Str("socket", d.config.SocketPath).Msg("Starting Unix socket server")
	go func() {
		if err := d.socketServer.Start(); err != nil {
			logger.Error().Err(err).Str("socket", d.config.SocketPath).Msg("Unix socket server failed")
			errCh <- fmt.Errorf("socket server: %w", err)
		}
	}()

	// Start TCP server for remote access (optional)
	if d.config.EnableTCP && d.tcpServer != nil {
		logger.Debug().Str("addr", d.config.TCPAddr).Msg("Starting TCP server")
		go func() {
			if err := d.tcpServer.Start(); err != nil {
				logger.Error().Err(err).Str("addr", d.config.TCPAddr).Msg("TCP server failed")
				errCh <- fmt.Errorf("TCP server: %w", err)
			}
		}()
	}

	// Start HTTP server for webhooks (optional)
	if d.config.EnableHTTP && d.httpServer != nil {
		logger.Debug().Int("port", d.config.Port).Msg("Starting HTTP server")
		go func() {
			if err := d.httpServer.Start(d.config.Port); err != nil {
				logger.Error().Err(err).Int("port", d.config.Port).Msg("HTTP server failed")
				errCh <- fmt.Errorf("HTTP server: %w", err)
			}
		}()
	}

	// Run initial reconciliation after delay
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		logger.Info().Dur("delay", d.config.InitialDelay).Msg("Waiting before initial reconciliation")

		// Use a select to allow cancellation during delay
		select {
		case <-time.After(d.config.InitialDelay):
		case <-ctx.Done():
			return
		}

		logger.Info().Msg("Running initial reconciliation")
		ui.Info("Running initial reconciliation...")
		if err := d.TriggerReconcile(ctx, "startup", false); err != nil {
			logger.Error().Err(err).Msg("Initial reconciliation failed")
			ui.Error("Initial reconciliation failed: %v", err)
		}
		logger.Info().Msg("Daemon ready to serve requests")
		d.setReady(true)
	}()

	// Start polling loop if enabled
	if d.config.PollInterval > 0 {
		d.wg.Add(1)
		go func() {
			defer d.wg.Done()
			d.pollLoop(ctx)
		}()
	}

	// Start periodic drift check loop if enabled
	if d.config.DriftInterval > 0 {
		d.wg.Add(1)
		go func() {
			defer d.wg.Done()
			d.driftCheckLoop(ctx)
		}()
	}

	ui.Success("Daemon ready")

	// Wait for shutdown signal or error
	select {
	case sig := <-sigCh:
		logger.Info().Str("signal", sig.String()).Msg("Received shutdown signal")
		ui.Warning("Received signal %v, shutting down...", sig)
	case err := <-errCh:
		logger.Error().Err(err).Msg("Fatal error, shutting down")
		ui.Error("Fatal error: %v", err)
		return err
	case <-ctx.Done():
		logger.Info().Msg("Context cancelled, shutting down")
		ui.Warning("Context cancelled, shutting down...")
	}

	// Graceful shutdown
	return d.shutdown()
}

// shutdown performs graceful shutdown of all components.
func (d *Daemon) shutdown() error {
	logger := log.Component(log.ComponentDaemon)
	logger.Info().Msg("Initiating graceful shutdown")
	ui.Info("Shutting down...")

	// Flush Sentry events before shutting down network servers.
	sentrypkg.Close(5 * time.Second)

	// Stop polling
	close(d.stopLoops)

	// Shutdown timeout
	ctx, cancel := context.WithTimeout(context.Background(), d.config.ShutdownTimeout)
	defer cancel()

	// Stop socket server
	if d.socketServer != nil {
		logger.Debug().Msg("Shutting down socket server")
		if err := d.socketServer.Shutdown(ctx); err != nil {
			logger.Warn().Err(err).Msg("Socket server shutdown error")
			ui.Warning("Socket server shutdown: %v", err)
		}
	}

	// Stop TCP server
	if d.tcpServer != nil {
		logger.Debug().Msg("Shutting down TCP server")
		if err := d.tcpServer.Shutdown(ctx); err != nil {
			logger.Warn().Err(err).Msg("TCP server shutdown error")
			ui.Warning("TCP server shutdown: %v", err)
		}
	}

	// Stop HTTP server
	if d.httpServer != nil {
		logger.Debug().Msg("Shutting down HTTP server")
		if err := d.httpServer.Shutdown(ctx); err != nil {
			logger.Warn().Err(err).Msg("HTTP server shutdown error")
			ui.Warning("HTTP server shutdown: %v", err)
		}
	}

	// Wait for background goroutines to complete
	logger.Debug().Msg("Waiting for background goroutines to complete")
	done := make(chan struct{})
	go func() {
		d.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		logger.Debug().Msg("All background goroutines completed")
	case <-ctx.Done():
		logger.Warn().Msg("Shutdown timeout waiting for background goroutines")
	}

	// Close shared Docker client.
	if d.dockerClient != nil {
		if err := d.dockerClient.Close(); err != nil {
			logger.Warn().Err(err).Msg("Docker client close error")
		}
	}

	logger.Info().Msg("Shutdown complete")
	ui.Success("Shutdown complete")
	return nil
}

// DockerClient returns the shared Docker client, creating it on first use.
func (d *Daemon) DockerClient() (*docker.Client, error) {
	d.dockerOnce.Do(func() {
		d.dockerClient, d.dockerErr = docker.NewClient()
	})
	return d.dockerClient, d.dockerErr
}

// TriggerReconcile triggers a reconciliation run.
// If a reconcile is already in progress, it sets the pending flag and returns immediately.
// The running reconcile will check the pending flag and re-run if set.
// The force flag is sticky: if any trigger requests force, the coalesced run will be forced.
func (d *Daemon) TriggerReconcile(ctx context.Context, source string, force bool) error {
	// Add reconcile ID to context for correlation.
	ctx, reconcileID := log.NewReconcileContext(ctx)

	d.reconcileMu.Lock()

	if d.reconciling {
		// Another reconcile is in progress - set dirty flag and return.
		d.pendingTrigger = true
		d.triggerSource = source
		// Force is sticky: once any trigger requests force, keep it.
		d.triggerForce = d.triggerForce || force
		d.reconcileMu.Unlock()

		log.Info().
			Str(log.FieldComponent, log.ComponentDaemon).
			Str(log.FieldSource, source).
			Str(log.FieldReconcileID, reconcileID).
			Bool("force", force).
			Msg("Reconcile already in progress, queued trigger")

		ui.Info("Reconcile already in progress, queued trigger from %s", source)
		return nil
	}

	// Mark as reconciling.
	d.reconciling = true
	d.reconcileMu.Unlock()

	// Run the reconcile loop (may run multiple times if pending triggers arrive).
	return d.reconcileLoop(ctx, source, force)
}

// reconcileLoop runs reconciliation, checking for pending triggers after each run.
func (d *Daemon) reconcileLoop(ctx context.Context, source string, force bool) error {
	var lastErr error

	for {
		// Execute reconcile
		err := d.executeReconcile(ctx, source, force)
		if err != nil {
			lastErr = err
		}

		// Check for pending trigger
		d.reconcileMu.Lock()
		if d.pendingTrigger {
			// Another trigger arrived - reset flag and run again
			source = d.triggerSource
			force = d.triggerForce
			d.pendingTrigger = false
			d.triggerSource = ""
			d.triggerForce = false
			d.reconcileMu.Unlock()
			ui.Info("Processing queued trigger from %s (force=%t)", source, force)
			continue
		}

		// No pending trigger - we're done
		d.reconciling = false
		d.reconcileMu.Unlock()
		return lastErr
	}
}

// executeReconcile runs a single reconciliation and updates state.
func (d *Daemon) executeReconcile(ctx context.Context, source string, force bool) error {
	start := time.Now()
	reconcileID := log.ReconcileIDFromContext(ctx)

	// Start a Sentry transaction for performance monitoring.
	ctx, finishTx := sentrypkg.ReconcileTransaction(ctx, source)

	// Set source and force on the reconciler config so the state-based
	// skip logic and attempt tracking have the right context.
	d.reconciler.SetRunOptions(source, force)

	log.Info().
		Str(log.FieldComponent, log.ComponentReconcile).
		Str(log.FieldReconcileID, reconcileID).
		Str(log.FieldSource, source).
		Bool("force", force).
		Msg("Starting reconciliation")

	ui.Info("Starting reconciliation (source: %s, force: %t)", source, force)

	err := d.reconciler.Run(ctx)

	// Update state (use stateMu for thread-safe reads from health checks).
	d.stateMu.Lock()
	d.lastReconcile = time.Now()
	d.lastError = err
	d.stateMu.Unlock()

	durationMS := time.Since(start).Milliseconds()

	// Finish the Sentry transaction with the result status.
	finishTx(err)

	if err != nil {
		log.Error().
			Str(log.FieldComponent, log.ComponentReconcile).
			Str(log.FieldReconcileID, reconcileID).
			Str(log.FieldSource, source).
			Int64(log.FieldDurationMS, durationMS).
			Err(err).
			Msg("Reconciliation failed")

		ui.Error("Reconciliation failed after %s: %v", time.Since(start), err)
		return err
	}

	log.Info().
		Str(log.FieldComponent, log.ComponentReconcile).
		Str(log.FieldReconcileID, reconcileID).
		Str(log.FieldSource, source).
		Int64(log.FieldDurationMS, durationMS).
		Bool("success", true).
		Msg("Reconciliation completed")

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
			if err := d.TriggerReconcile(ctx, "poll", false); err != nil {
				ui.Error("Poll reconciliation failed: %v", err)
			}
		case <-d.stopLoops:
			return
		case <-ctx.Done():
			return
		}
	}
}

// driftCheckLoop runs periodic drift checks independent of reconciliation.
func (d *Daemon) driftCheckLoop(ctx context.Context) {
	logger := log.Component(log.ComponentDaemon)
	ticker := time.NewTicker(d.config.DriftInterval)
	defer ticker.Stop()

	logger.Info().Dur("interval", d.config.DriftInterval).Msg("Drift check loop started")

	for {
		select {
		case <-ticker.C:
			d.runDriftCheck(ctx)
		case <-d.stopLoops:
			return
		case <-ctx.Done():
			return
		}
	}
}

// runDriftCheck performs a single drift check and updates the state file.
// Skips if a reconciliation is in progress to avoid state file race conditions.
func (d *Daemon) runDriftCheck(ctx context.Context) {
	logger := log.Component(log.ComponentDaemon)

	// Skip drift check if reconcile is in progress to avoid lost-update
	// race on the shared state file (both load → modify → save).
	d.reconcileMu.Lock()
	busy := d.reconciling
	d.reconcileMu.Unlock()
	if busy {
		logger.Debug().Msg("Drift check: skipping, reconciliation in progress")
		return
	}

	stateFile := ""
	projectName := ""
	if d.config.ReconcileConfig != nil {
		stateFile = d.config.ReconcileConfig.StateFile
		projectName = d.config.ReconcileConfig.ProjectName
	}
	if stateFile == "" {
		return
	}

	client, err := d.DockerClient()
	if err != nil {
		logger.Warn().Err(err).Msg("Drift check: Docker unavailable")
		return
	}

	checkCtx, cancel := context.WithTimeout(ctx, d.config.APITimeout)
	defer cancel()

	checkCtx, finishSpan := sentrypkg.StartSpan(checkCtx, "drift.periodic_check", "Periodic drift check")
	report, err := reconcile.RunDriftCheck(checkCtx, client, stateFile, projectName)
	finishSpan(err)
	if err != nil {
		logger.Warn().Err(err).Msg("Drift check failed")
		return
	}

	// Load previous state to detect drift resolution.
	state := reconcile.LoadState(stateFile)
	previouslyDrifted := len(state.DriftItems) > 0

	// Update state file with drift results.
	state.DriftCheckedAt = report.CheckedAt
	state.DriftItems = report.Items
	if err := reconcile.SaveState(stateFile, state); err != nil {
		logger.Warn().Err(err).Msg("Drift check: failed to save state")
	}

	if report.HasDrift() {
		logger.Warn().
			Int("drift_items", len(report.Items)).
			Msg("Drift detected during periodic check")

		// Send alert for critical drift (missing/unhealthy).
		if report.HasCriticalDrift() && d.alerter != nil {
			d.sendDriftAlert(ctx, report)
		}
	} else if previouslyDrifted {
		logger.Info().Msg("Drift resolved: all declared services now match actual state")
	} else {
		logger.Debug().Msg("Drift check: no drift detected")
	}
}

// sendDriftAlert sends an alert for detected drift.
func (d *Daemon) sendDriftAlert(ctx context.Context, report *reconcile.DriftReport) {
	logger := log.Component(log.ComponentDaemon)

	target := "local"
	if d.config.ReconcileConfig != nil && d.config.ReconcileConfig.TargetHost != "" {
		target = d.config.ReconcileConfig.TargetHost
	}

	// Build list of critical drift items for the alert.
	var driftItems []string
	for _, item := range report.Items {
		if item.Type == reconcile.DriftMissing || item.Type == reconcile.DriftUnhealthy {
			driftItems = append(driftItems, fmt.Sprintf("%s (%s)", item.Service, item.Type))
		}
	}

	if err := d.alerter.SendDriftDetected(ctx, target, driftItems); err != nil {
		logger.Warn().Err(err).Msg("Failed to send drift alert")
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

// WidgetData returns lightweight stats for Homepage's customapi widget.
// Response is flat JSON with max 4 fields per Homepage block display limits.
func (d *Daemon) WidgetData() map[string]any {
	_, lastError := d.LastReconcile()

	status := "ok"
	if lastError != nil {
		status = "error"
	}

	data := map[string]any{
		"deploys_total": 0,
		"last_deploy":   "",
		"status":        status,
		"git_sha":       "",
	}

	// Load persistent state for deploy history.
	if d.config.ReconcileConfig != nil && d.config.ReconcileConfig.StateFile != "" {
		state := reconcile.LoadState(d.config.ReconcileConfig.StateFile)
		data["deploys_total"] = state.DeployCount
		if !state.DeployedAt.IsZero() {
			data["last_deploy"] = state.DeployedAt.UTC().Format(time.RFC3339)
		}
		if len(state.LastDeployedCommit) >= 7 {
			data["git_sha"] = state.LastDeployedCommit[:7]
		} else if state.LastDeployedCommit != "" {
			data["git_sha"] = state.LastDeployedCommit
		}
	}

	return data
}

var startTime = time.Now()

// versionOrDev returns v if non-empty, otherwise "dev".
func versionOrDev(v string) string {
	if v != "" {
		return v
	}
	return "dev"
}

// parseDurationOrSeconds parses a string as a Go duration (e.g. "30s", "5m"),
// or as bare seconds if no unit suffix is present (e.g. "30" -> 30s).
// Returns the parsed duration and true, or zero and false if parsing fails.
func parseDurationOrSeconds(s string) (time.Duration, bool) {
	if d, err := time.ParseDuration(s); err == nil {
		return d, true
	}
	// Treat as bare number of seconds
	if d, err := time.ParseDuration(s + "s"); err == nil {
		return d, true
	}
	return 0, false
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
		_, _ = fmt.Sscanf(port, "%d", &cfg.Port)
	}
	if port := os.Getenv("WEBHOOK_PORT"); port != "" {
		_, _ = fmt.Sscanf(port, "%d", &cfg.Port)
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
		if d, ok := parseDurationOrSeconds(interval); ok {
			cfg.PollInterval = d
		}
	}
	if interval := os.Getenv("BOSUN_POLL_INTERVAL"); interval != "" {
		if d, ok := parseDurationOrSeconds(interval); ok {
			cfg.PollInterval = d
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

	if infraDir := os.Getenv("BOSUN_INFRA_DIR"); infraDir != "" {
		rcfg.InfraSubDir = infraDir
	}

	// State directory override
	if stateDir := os.Getenv("BOSUN_STATE_DIR"); stateDir != "" {
		rcfg.StateFile = filepath.Join(stateDir, reconcile.DefaultStateFile)
	}

	cfg.ReconcileConfig = rcfg

	// Timeout overrides
	if v := os.Getenv("BOSUN_RECONCILE_TIMEOUT"); v != "" {
		if d, ok := parseDurationOrSeconds(v); ok {
			cfg.ReconcileTimeout = d
		}
	}
	if v := os.Getenv("BOSUN_SHUTDOWN_TIMEOUT"); v != "" {
		if d, ok := parseDurationOrSeconds(v); ok {
			cfg.ShutdownTimeout = d
		}
	}
	if v := os.Getenv("BOSUN_API_TIMEOUT"); v != "" {
		if d, ok := parseDurationOrSeconds(v); ok {
			cfg.APITimeout = d
		}
	}

	// Drift check interval (0 disables periodic checks)
	if v := os.Getenv("BOSUN_DRIFT_INTERVAL"); v != "" {
		if d, ok := parseDurationOrSeconds(v); ok {
			cfg.DriftInterval = d
		}
	}

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
