// Package daemon provides a long-running daemon for GitOps operations.
// It handles webhook reception, polling-based reconciliation, and health checks.
package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/trace"

	"github.com/cameronsjo/bosun/internal/alert"
	"github.com/cameronsjo/bosun/internal/config"
	"github.com/cameronsjo/bosun/internal/docker"
	"github.com/cameronsjo/bosun/internal/log"
	"github.com/cameronsjo/bosun/internal/reconcile"
	sentrypkg "github.com/cameronsjo/bosun/internal/sentry"
	"github.com/cameronsjo/bosun/internal/telemetry"
	"github.com/cameronsjo/bosun/internal/ui"
)

// Config holds daemon configuration.
type Config struct {
	// Socket API settings (primary)
	SocketPath        string      // Path to Unix socket (default: /var/run/bosun.sock)
	SocketMode        os.FileMode // Socket file permissions (default: 0660)
	SocketAllowedUIDs []uint32    // Additional UIDs allowed to mutate through the socket

	// AllowUnauthenticatedSocket opts out of fail-closed peer-credential auth.
	// This is intentionally separate from SocketMode: the mode grants access to
	// connect, while peer credentials authorize mutating requests.
	AllowUnauthenticatedSocket bool
	socketAllowedUIDsError     error

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

	// ListenAddr is the host/IP the HTTP server binds to (BOSUN_LISTEN_ADDR).
	// Empty binds all interfaces — the default MUST stay all-interfaces
	// because container-side callers (Traefik, Prometheus, Homepage) reach
	// bosun over the docker bridge, not loopback.
	ListenAddr string

	// AllowUnauthenticatedWebhook opts out of fail-closed webhook auth (#345).
	// When WebhookSecret is empty, trigger endpoints reject every request
	// unless this is true (BOSUN_ALLOW_UNAUTHENTICATED_WEBHOOK=true, strict match).
	AllowUnauthenticatedWebhook bool

	// MetricsToken is a read-scope credential for /metrics and /api/widget
	// (BOSUN_METRICS_TOKEN). It is SEPARATE from BearerToken: the TCP bearer
	// also authorizes /trigger and /api/restart (control operations), so a
	// metrics scraper must never need it. The control bearer, being strictly
	// more privileged, is also accepted on these read endpoints.
	MetricsToken string

	// AllowUnauthenticatedMetrics opts out of fail-closed metrics auth (#296).
	// When both MetricsToken and BearerToken are empty, /metrics and /api/widget
	// reject every request unless this is true (BOSUN_ALLOW_UNAUTHENTICATED_METRICS=true,
	// strict match). A loud opt-out: logged at startup and per accepted request.
	AllowUnauthenticatedMetrics bool

	// Polling settings
	PollInterval time.Duration // Interval between polls (0 disables polling)
	InitialDelay time.Duration // Delay before first poll (default: 10s)

	// Reconcile settings
	ReconcileConfig *reconcile.Config
	// projectConfigError retains the narrow class of project configuration
	// errors that must fail startup instead of using graceful degradation.
	projectConfigError error

	// Timeout settings
	ReconcileTimeout time.Duration // Max time for a reconcile operation (default: 10m)
	ShutdownTimeout  time.Duration // Max time for graceful shutdown (default: 30s)
	APITimeout       time.Duration // Timeout for API handler requests (default: 30s)

	// Drift check settings
	DriftInterval         time.Duration                        // Interval for periodic drift checks (0 disables, default: 5m)
	DriftAlertCooldown    time.Duration                        // Cooldown between repeated drift alerts per item (default: 1h)
	DriftAlertDebounce    reconcile.ConfigField[time.Duration] // Debounce window before first drift alert fires (0 = disabled, default: 0)
	DriftResolveAlerts    bool                                 // Send "drift resolved" alerts (default: true)
	DriftSelfHeal         reconcile.ConfigField[bool]          // Trigger reconciliation when drift detected (default: false)
	DriftSelfHealCooldown reconcile.ConfigField[time.Duration] // Minimum interval between self-heal reconciliations (default: 15m)
	MaxSelfHealAttempts   int                                  // Maximum attempts for one stable drift signature (default: 3)

	// Content-hash sync settings
	ContentHashSync bool // Compare file hashes before writing (default: true)

	// Orphan container cleanup settings
	RemoveOrphans bool // Pass --remove-orphans to docker compose up (default: true)

	// Alerting
	AlertManager *alert.Manager

	// Version is the bosun version string (set by caller from build-time ldflags).
	Version string
}

// DefaultDriftInterval is the daemon's restart-count sampling cadence.
const DefaultDriftInterval = 5 * time.Minute

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		SocketPath:            DefaultSocketPath,
		SocketMode:            0o660,
		EnableTCP:             false,            // Disabled by default for security
		TCPAddr:               "127.0.0.1:9090", // Localhost only by default
		Port:                  8080,
		EnableHTTP:            true, // Backwards compat: enable HTTP by default for now
		WebhookPath:           "/webhook",
		HealthPath:            "/health",
		ReadyPath:             "/ready",
		PollInterval:          time.Hour,
		InitialDelay:          10 * time.Second,
		ReconcileTimeout:      10 * time.Minute,
		ShutdownTimeout:       30 * time.Second,
		APITimeout:            30 * time.Second,
		DriftInterval:         DefaultDriftInterval,
		DriftAlertCooldown:    time.Hour,
		DriftResolveAlerts:    true,
		DriftSelfHealCooldown: reconcile.NewConfigField(15 * time.Minute),
		MaxSelfHealAttempts:   DefaultMaxSelfHealAttempts,
		ContentHashSync:       true,
		RemoveOrphans:         true,
	}
}

// DefaultSocketPath is the default path for the bosun daemon Unix socket.
const DefaultSocketPath = "/var/run/bosun.sock"

// DefaultMaxSelfHealAttempts bounds unattended remediation of one stable drift signature.
const DefaultMaxSelfHealAttempts = 3

// Daemon is the main GitOps daemon that handles webhooks and polling.
type Daemon struct {
	config               *Config
	socketServer         *SocketServer                // Unix socket API (primary)
	tcpServer            *TCPServer                   // TCP API with bearer auth (optional)
	httpServer           *Server                      // HTTP server for webhooks (optional)
	reconciler           *reconcile.Reconciler        // Single-target fallback (used when only one target)
	reconcileOpts        []reconcile.ReconcilerOption // Options applied to each per-target reconciler
	alerter              *alert.Manager
	metrics              *Metrics       // Prometheus metrics (nil when HTTP is disabled)
	dockerOnce           sync.Once      // Lazily initialize Docker client
	dockerClient         *docker.Client // Shared Docker client for API handlers
	dockerErr            error          // Error from Docker client initialization
	dockerClientOverride *docker.Client // Test injection point: bypasses sync.Once
	ready                bool
	readyMu              sync.RWMutex
	stopLoops            chan struct{}
	stopLoopsOnce        sync.Once

	// lifecycleCtx is the daemon-wide cancellation root for every asynchronous
	// reconcile. lifecycleMu serializes shutdown with WaitGroup registration so
	// no handler can call Add after shutdown has begun waiting (#256/#356).
	lifecycleMu     sync.Mutex
	lifecycleCtx    context.Context
	lifecycleCancel context.CancelFunc
	shuttingDown    bool
	wg              sync.WaitGroup

	// triggerReconcileFn is the asynchronous trigger entry point. Production
	// uses TriggerReconcile; tests replace it to exercise cancellation and panic
	// behavior without running the full deploy pipeline.
	triggerReconcileFn func(context.Context, string, bool) error

	// Reconcile state (read frequently for health checks)
	stateMu       sync.RWMutex
	lastReconcile time.Time
	lastError     error

	// Concurrency control: single-flight reconcile with lossless coalescing.
	reconcileMu                   sync.Mutex          // Guards reconcile execution and pending trigger state
	reconciling                   bool                // True while reconcile is in progress
	pendingTriggerCount           uint64              // Number of triggers queued for the next coalesced run
	pendingTriggerSources         map[string]struct{} // Distinct sources contributing to the next run
	pendingTriggerForce           bool                // Sticky force flag for the next run
	pendingForceRedeployUnchanged bool                // Preserve drift-self-heal semantics through source aggregation

	// driftCheckMu serializes the drift state file's load-mutate-save cycle.
	// The periodic loop is single-threaded, but tests and future callers may invoke
	// runDriftCheck concurrently.
	driftCheckMu sync.Mutex

	// configMu guards daemon-level config fields that are hot-reloaded from bosun.yaml.
	// Only covers DriftAlertDebounce, DriftSelfHeal, DriftSelfHealCooldown.
	configMu sync.RWMutex
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

	lifecycleCtx, lifecycleCancel := context.WithCancel(context.Background())
	d := &Daemon{
		config:          cfg,
		alerter:         cfg.AlertManager,
		stopLoops:       make(chan struct{}),
		lifecycleCtx:    lifecycleCtx,
		lifecycleCancel: lifecycleCancel,
	}
	d.triggerReconcileFn = d.TriggerReconcile

	// Lazily inject Docker client into reconciler for post-deploy verification.
	// The client is initialized on first use via DockerClient().
	opts = append(opts, reconcile.WithDockerClientFunc(func() *docker.Client {
		client, err := d.DockerClient()
		if err != nil {
			return nil
		}
		return client
	}))

	d.reconcileOpts = opts
	d.reconciler = reconcile.NewReconciler(cfg.ReconcileConfig, opts...)

	// Create Unix socket server (primary API)
	socketCfg := &SocketConfig{
		SocketPath:                   cfg.SocketPath,
		SocketMode:                   cfg.SocketMode,
		AllowedUIDs:                  cfg.SocketAllowedUIDs,
		AllowUnauthenticatedMutation: cfg.AllowUnauthenticatedSocket,
	}
	socketServer, err := NewSocketServer(d, socketCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create socket server: %w", err)
	}
	d.socketServer = socketServer

	// Create HTTP server for webhooks (optional, for backwards compat)
	if cfg.EnableHTTP {
		d.httpServer = NewServer(d)
		d.metrics = d.httpServer.metrics
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

// warnWebhookAuthPosture announces the webhook auth posture at startup (#345):
// fail-closed is the default, so a secret-less upgrade must never be a silent
// 403, and an opted-out daemon must never hide its exposure. No-op when HTTP
// is disabled or a secret is configured.
func (d *Daemon) warnWebhookAuthPosture(logger zerolog.Logger) {
	if !d.config.EnableHTTP || d.config.WebhookSecret != "" {
		return
	}
	if d.config.AllowUnauthenticatedWebhook {
		logger.Warn().
			Msg("SECURITY: webhook endpoints accept UNAUTHENTICATED trigger requests. Reason: no webhook secret configured and BOSUN_ALLOW_UNAUTHENTICATED_WEBHOOK=true. Anyone who can reach the HTTP port can trigger a deploy")
		ui.Warning("SECURITY: unauthenticated webhook triggers enabled (no secret, opt-out set)")
		return
	}
	logger.Warn().
		Msg("Webhook endpoints will REJECT all trigger requests, expected a webhook secret but none is configured — webhook auth fails closed. Set WEBHOOK_SECRET, or set BOSUN_ALLOW_UNAUTHENTICATED_WEBHOOK=true to accept unauthenticated triggers")
	ui.Warning("Webhooks fail closed: no WEBHOOK_SECRET set, trigger requests will be rejected (403)")
}

// metricsAuthConfigured reports whether a credential is configured that admits a
// caller to /metrics and /api/widget. Either the read-scope MetricsToken or the
// strictly-more-privileged control BearerToken qualifies.
func (d *Daemon) metricsAuthConfigured() bool {
	return d.config.MetricsToken != "" || d.config.BearerToken != ""
}

// warnMetricsAuthPosture announces the read-endpoint auth posture at startup
// (#296): /metrics and /api/widget fail closed by default, so a token-less
// upgrade must never silently keep serving the deployed commit, and an opted-out
// daemon must never hide its exposure. No-op when HTTP is disabled or a
// credential is configured.
func (d *Daemon) warnMetricsAuthPosture(logger zerolog.Logger) {
	if !d.config.EnableHTTP || d.metricsAuthConfigured() {
		return
	}
	if d.config.AllowUnauthenticatedMetrics {
		logger.Warn().
			Msg("SECURITY: /metrics and /api/widget accept UNAUTHENTICATED requests. Reason: no BOSUN_METRICS_TOKEN configured and BOSUN_ALLOW_UNAUTHENTICATED_METRICS=true. Anyone who can reach the HTTP port can read the deployed commit and daemon stats")
		ui.Warning("SECURITY: unauthenticated /metrics and /api/widget enabled (no token, opt-out set)")
		return
	}
	logger.Warn().
		Msg("/metrics and /api/widget will REJECT all requests, expected a read token but none is configured — metrics auth fails closed. Set BOSUN_METRICS_TOKEN, or set BOSUN_ALLOW_UNAUTHENTICATED_METRICS=true to serve them unauthenticated")
	ui.Warning("Metrics fail closed: no BOSUN_METRICS_TOKEN set, /metrics and /api/widget will be rejected (403)")
}

// warnSocketAuthPosture makes the explicit socket-auth opt-out loud. On
// platforms without peer credential support, it also explains why mutating
// requests fail closed instead of leaving operators with unexplained 403s.
func (d *Daemon) warnSocketAuthPosture(logger zerolog.Logger) {
	if d.config.AllowUnauthenticatedSocket {
		logger.Warn().
			Msg("SECURITY: Unix socket accepts UNAUTHENTICATED mutating requests because BOSUN_ALLOW_UNAUTHENTICATED_SOCKET=true. Any process that can connect to the socket can trigger a deploy")
		ui.Warning("SECURITY: unauthenticated Unix socket mutations enabled (opt-out set)")
		return
	}
	if !peerCredentialSupportAvailable() {
		logger.Warn().
			Msg("Unix socket mutating requests will be REJECTED because peer credentials are unavailable on this platform; socket auth fails closed. Set BOSUN_ALLOW_UNAUTHENTICATED_SOCKET=true only when socket file permissions are a sufficient trust boundary")
		ui.Warning("Unix socket mutations fail closed: peer credentials unavailable on this platform")
	}
}

// prepareStateDir creates and write-probes the deploy-state directory before
// any listener starts. MkdirAll alone is insufficient for bind mounts: an
// existing root-owned directory passes it even though the non-root daemon
// cannot create the state file.
func prepareStateDir(stateFile string) error {
	if stateFile == "" {
		return nil
	}

	stateDir := filepath.Dir(stateFile)
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return fmt.Errorf("create state directory %q: %w", stateDir, err)
	}

	probe, err := os.CreateTemp(stateDir, ".bosun-state-probe-*")
	if err != nil {
		return fmt.Errorf("state directory %q is not writable: %w", stateDir, err)
	}
	probePath := probe.Name()
	closeErr := probe.Close()
	removeErr := os.Remove(probePath)
	if cleanupErr := errors.Join(closeErr, removeErr); cleanupErr != nil {
		return fmt.Errorf("clean up state directory probe %q: %w", probePath, cleanupErr)
	}
	return nil
}

// Run starts the daemon and blocks until shutdown.
// It handles SIGTERM and SIGINT for graceful shutdown.
func (d *Daemon) Run(ctx context.Context) (err error) {
	// A bare "defer sentrypkg.Recover()" here would swallow a panic in Run's
	// own synchronous body (before any goroutine is even spawned) and return
	// nil -- the caller's ui.Fatal never fires, the process exits 0, and a
	// supervisor keyed on a nonzero exit code never restarts a daemon that
	// never actually came up. Run needs its own recover that sets the named
	// return so a startup panic still surfaces as a real error (#364 review
	// follow-up). The six goroutines spawned below keep their own
	// "defer sentrypkg.Recover()" -- a panic there must NOT propagate to
	// this frame; recovering it locally in each goroutine is correct.
	defer func() {
		if r := recover(); r != nil {
			sentrypkg.Report(r)
			err = fmt.Errorf("daemon run panicked: %v", r)
		}
	}()
	if err := ValidateConfig(d.config); err != nil {
		return fmt.Errorf("invalid daemon configuration: %w", err)
	}

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

	d.warnWebhookAuthPosture(logger)
	d.warnMetricsAuthPosture(logger)
	d.warnSocketAuthPosture(logger)

	// Ensure state storage is usable before any API listener or reconcile loop
	// starts. A persistent mount that exists but is not writable is fatal: if we
	// continued, drift, skip, and circuit-breaker state would silently disappear.
	if d.config.ReconcileConfig != nil && d.config.ReconcileConfig.StateFile != "" {
		stateDir := filepath.Dir(d.config.ReconcileConfig.StateFile)
		logger.Debug().Str(log.FieldPath, stateDir).Msg("Preparing deploy state directory")
		if err := prepareStateDir(d.config.ReconcileConfig.StateFile); err != nil {
			logger.Error().
				Err(err).
				Str(log.FieldPath, stateDir).
				Msg("Deploy state directory is not writable")
			return fmt.Errorf("prepare deploy state: %w", err)
		}
	}

	// Bind every asynchronous reconcile to this Run invocation. New() installs
	// a background lifecycle for direct handler tests and embedded use; Run
	// replaces it before any server can accept requests.
	ctx = d.resetLifecycleContext(ctx)
	defer d.cancelLifecycle()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	// Error channel for goroutines
	errCh := make(chan error, 3)

	// Start Unix socket server (primary API)
	logger.Info().Str("socket", d.config.SocketPath).Msg("Preparing to start Unix socket server")
	go func() {
		defer sentrypkg.Recover()
		if err := d.socketServer.Start(); err != nil {
			logger.Error().
				Err(err).
				Str("socket", d.config.SocketPath).
				Msg("Failed to start Unix socket server")
			errCh <- fmt.Errorf("socket server: %w", err)
		}
	}()

	// Start TCP server for remote access (optional)
	if d.config.EnableTCP && d.tcpServer != nil {
		logger.Info().Str("addr", d.config.TCPAddr).Msg("Preparing to start TCP server")
		go func() {
			defer sentrypkg.Recover()
			if err := d.tcpServer.Start(); err != nil {
				logger.Error().
					Err(err).
					Str("addr", d.config.TCPAddr).
					Msg("Failed to start TCP server")
				errCh <- fmt.Errorf("TCP server: %w", err)
			}
		}()
	}

	// Start HTTP server for webhooks (optional)
	if d.config.EnableHTTP && d.httpServer != nil {
		logger.Info().Int("port", d.config.Port).Msg("Preparing to start HTTP server")
		go func() {
			defer sentrypkg.Recover()
			if err := d.httpServer.Start(d.config.Port); err != nil {
				logger.Error().
					Err(err).
					Int("port", d.config.Port).
					Msg("Failed to start HTTP server")
				errCh <- fmt.Errorf("HTTP server: %w", err)
			}
		}()
	}

	// Run initial reconciliation after delay
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		defer sentrypkg.Recover()
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
			return
		}
		logger.Info().Msg("Daemon ready to serve requests")
	}()

	// Start polling loop if enabled
	if d.config.PollInterval > 0 {
		d.wg.Add(1)
		go func() {
			defer d.wg.Done()
			defer sentrypkg.Recover()
			d.pollLoop(ctx)
		}()
	}

	// Start periodic drift check loop if enabled
	if d.config.DriftInterval > 0 {
		d.wg.Add(1)
		go func() {
			defer d.wg.Done()
			defer sentrypkg.Recover()
			d.driftCheckLoop(ctx)
		}()
	}

	ui.Success("Daemon ready")

	// Wait for shutdown signal or error
	var runErr error
	select {
	case sig := <-sigCh:
		logger.Info().Str("signal", sig.String()).Msg("Received shutdown signal")
		ui.Warning("Received signal %v, shutting down...", sig)
	case err := <-errCh:
		logger.Error().Err(err).Msg("Fatal error, shutting down")
		ui.Error("Fatal error: %v", err)
		runErr = err
	case <-ctx.Done():
		logger.Info().Msg("Context cancelled, shutting down")
		ui.Warning("Context cancelled, shutting down...")
	}

	// Graceful shutdown
	shutdownErr := d.shutdown()
	if shutdownErr != nil {
		logger.Error().Err(shutdownErr).Msg("Shutdown encountered error")
	}
	return errors.Join(runErr, shutdownErr)
}

// resetLifecycleContext installs the cancellation root used by asynchronous
// reconcile handlers for this daemon run.
func (d *Daemon) resetLifecycleContext(parent context.Context) context.Context {
	d.lifecycleMu.Lock()
	defer d.lifecycleMu.Unlock()

	if d.lifecycleCancel != nil {
		d.lifecycleCancel()
	}
	d.lifecycleCtx, d.lifecycleCancel = context.WithCancel(parent)
	d.shuttingDown = false
	return d.lifecycleCtx
}

func (d *Daemon) cancelLifecycle() {
	d.lifecycleMu.Lock()
	d.shuttingDown = true
	if d.lifecycleCancel != nil {
		d.lifecycleCancel()
	}
	d.lifecycleMu.Unlock()
}

// reconcileGoroutineReservation registers asynchronous reconcile work with the
// daemon lifecycle before the caller commits any durable side effects. The
// caller must release every accepted reservation; false cancels it without
// running the task.
type reconcileGoroutineReservation struct {
	once     sync.Once
	decision chan bool
}

func (r *reconcileGoroutineReservation) release(run bool) {
	if r == nil {
		return
	}
	r.once.Do(func() { r.decision <- run })
}

func (d *Daemon) reserveReconcileGoroutine(requestCtx context.Context, task func(context.Context)) (*reconcileGoroutineReservation, bool) {
	d.lifecycleMu.Lock()
	if d.shuttingDown {
		d.lifecycleMu.Unlock()
		return nil, false
	}
	ctx := d.lifecycleCtx
	if ctx == nil {
		ctx = context.Background()
	}
	d.wg.Add(1)
	d.lifecycleMu.Unlock()

	if requestCtx != nil {
		// TriggerReconcile deliberately rebuilds its logger from raw correlation
		// values before adding a reconcile ID. Preserve the request ID on the
		// detached lifecycle context as well as the already-enriched logger, or
		// the rebuild silently drops request correlation.
		if requestID := log.RequestIDFromContext(requestCtx); requestID != "" {
			ctx = log.WithRequestID(ctx, requestID)
		}
		ctx = log.WithContext(ctx, log.Ctx(requestCtx))
	}

	reservation := &reconcileGoroutineReservation{decision: make(chan bool, 1)}
	go func() {
		defer d.wg.Done()
		defer sentrypkg.Recover()
		if <-reservation.decision {
			task(ctx)
		}
	}()
	return reservation, true
}

// startReconcileGoroutine registers a reconcile before launching it and refuses
// new work once shutdown begins. The task inherits request logging values but
// cancellation comes from the daemon lifecycle rather than the short-lived HTTP
// request context.
func (d *Daemon) startReconcileGoroutine(requestCtx context.Context, task func(context.Context)) bool {
	reservation, accepted := d.reserveReconcileGoroutine(requestCtx, task)
	if !accepted {
		return false
	}
	reservation.release(true)
	return true
}

func (d *Daemon) runTriggerReconcile(ctx context.Context, source string, force bool) error {
	if d.triggerReconcileFn != nil {
		return d.triggerReconcileFn(ctx, source, force)
	}
	return d.TriggerReconcile(ctx, source, force)
}

// shutdown performs graceful shutdown of all components.
func (d *Daemon) shutdown() error {
	logger := log.Component(log.ComponentDaemon)
	logger.Info().Msg("Initiating graceful shutdown")
	ui.Info("Shutting down...")
	d.cancelLifecycle()

	// Flush Sentry events before shutting down network servers.
	logger.Debug().Dur("timeout", 5*time.Second).Msg("Preparing to flush Sentry events")
	if err := sentrypkg.Close(5 * time.Second); err != nil {
		logger.Warn().Err(err).Msg("Sentry shutdown error")
	}

	// Stop polling
	logger.Debug().Msg("Stopping background loops")
	d.stopLoopsOnce.Do(func() { close(d.stopLoops) })

	// Shutdown timeout
	ctx, cancel := context.WithTimeout(context.Background(), d.config.ShutdownTimeout)
	defer cancel()

	// Stop socket server
	if d.socketServer != nil {
		logger.Info().Msg("Shutting down socket server")
		if err := d.socketServer.Shutdown(ctx); err != nil {
			logger.Warn().
				Err(err).
				Msg("Socket server shutdown error")
			ui.Warning("Socket server shutdown: %v", err)
		} else {
			logger.Debug().Msg("Socket server shut down successfully")
		}
	}

	// Stop TCP server
	if d.tcpServer != nil {
		logger.Info().Msg("Shutting down TCP server")
		if err := d.tcpServer.Shutdown(ctx); err != nil {
			logger.Warn().
				Err(err).
				Msg("TCP server shutdown error")
			ui.Warning("TCP server shutdown: %v", err)
		} else {
			logger.Debug().Msg("TCP server shut down successfully")
		}
	}

	// Stop HTTP server
	if d.httpServer != nil {
		logger.Info().Msg("Shutting down HTTP server")
		if err := d.httpServer.Shutdown(ctx); err != nil {
			logger.Warn().
				Err(err).
				Msg("HTTP server shutdown error")
			ui.Warning("HTTP server shutdown: %v", err)
		} else {
			logger.Debug().Msg("HTTP server shut down successfully")
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
		logger.Debug().Msg("Closing Docker client")
		if err := d.dockerClient.Close(); err != nil {
			logger.Warn().
				Err(err).
				Msg("Docker client close error")
		} else {
			logger.Debug().Msg("Docker client closed successfully")
		}
	}

	logger.Info().Msg("Successfully shut down daemon")
	ui.Success("Shutdown complete")
	return nil
}

// DockerClient returns the shared Docker client, creating it on first use.
func (d *Daemon) DockerClient() (*docker.Client, error) {
	if d.dockerClientOverride != nil {
		return d.dockerClientOverride, nil
	}
	d.dockerOnce.Do(func() {
		d.dockerClient, d.dockerErr = docker.NewClient()
	})
	return d.dockerClient, d.dockerErr
}

type reconcileTrigger struct {
	source                 string
	force                  bool
	forceRedeployUnchanged bool
}

func newReconcileTrigger(source string, force bool) reconcileTrigger {
	return reconcileTrigger{
		source:                 source,
		force:                  force,
		forceRedeployUnchanged: source == log.SourceDriftSelfHeal,
	}
}

// queuePendingTrigger records a trigger for the next coalesced run.
// d.reconcileMu must be held by the caller.
func (d *Daemon) queuePendingTrigger(trigger reconcileTrigger) {
	d.pendingTriggerCount++
	if d.pendingTriggerSources == nil {
		d.pendingTriggerSources = make(map[string]struct{})
	}
	d.pendingTriggerSources[trigger.source] = struct{}{}
	d.pendingTriggerForce = d.pendingTriggerForce || trigger.force
	d.pendingForceRedeployUnchanged = d.pendingForceRedeployUnchanged || trigger.forceRedeployUnchanged
}

// takePendingTriggers atomically drains the next coalesced trigger batch.
// d.reconcileMu must be held by the caller.
func (d *Daemon) takePendingTriggers() (reconcileTrigger, uint64, bool) {
	if d.pendingTriggerCount == 0 {
		return reconcileTrigger{}, 0, false
	}

	sources := make([]string, 0, len(d.pendingTriggerSources))
	for source := range d.pendingTriggerSources {
		sources = append(sources, source)
	}
	sort.Strings(sources)

	trigger := reconcileTrigger{
		source:                 strings.Join(sources, ","),
		force:                  d.pendingTriggerForce,
		forceRedeployUnchanged: d.pendingForceRedeployUnchanged,
	}
	count := d.pendingTriggerCount

	d.pendingTriggerCount = 0
	d.pendingTriggerSources = nil
	d.pendingTriggerForce = false
	d.pendingForceRedeployUnchanged = false

	return trigger, count, true
}

// clearPendingTriggers resets all queued trigger metadata.
// d.reconcileMu must be held by the caller.
func (d *Daemon) clearPendingTriggers() {
	d.pendingTriggerCount = 0
	d.pendingTriggerSources = nil
	d.pendingTriggerForce = false
	d.pendingForceRedeployUnchanged = false
}

// TriggerReconcile triggers a reconciliation run.
// If a reconcile is already in progress, it queues source/force metadata and
// returns immediately. All triggers in the batch contribute to the next run;
// force remains sticky if any queued trigger requests it.
func (d *Daemon) TriggerReconcile(ctx context.Context, source string, force bool) error {
	trigger := newReconcileTrigger(source, force)

	// Add reconcile ID to context and stash enriched logger for downstream propagation.
	// FromContext builds from the global logger + raw context values (request_id, reconcile_id),
	// avoiding zerolog's append-only field duplication if called repeatedly.
	ctx, reconcileID := log.NewReconcileContext(ctx)
	enriched := log.FromContext(ctx)
	ctx = log.WithContext(ctx, &enriched)

	logger := log.ComponentCtx(ctx, log.ComponentDaemon)

	logger.Debug().
		Str(log.FieldSource, source).
		Bool("force", force).
		Str(log.FieldReconcileID, reconcileID).
		Msg("Preparing to trigger reconciliation")

	d.reconcileMu.Lock()

	if d.reconciling {
		// Another reconcile is in progress. Count every arrival and retain all
		// distinct source metadata for the next coalesced run.
		d.queuePendingTrigger(trigger)
		d.reconcileMu.Unlock()

		logger.Info().
			Str(log.FieldSource, source).
			Bool("force", force).
			Msg("Reconcile already in progress, queued trigger")

		ui.Info("Reconcile already in progress, queued trigger from %s", source)
		return nil
	}

	// Mark as reconciling.
	d.reconciling = true
	d.reconcileMu.Unlock()

	logger.Info().
		Str(log.FieldSource, source).
		Bool("force", force).
		Msg("Dispatching reconciliation")

	// Run the reconcile loop (may run multiple times if pending triggers arrive).
	err := d.reconcileLoop(ctx, trigger, d.executeReconcileTrigger)

	// Readiness reflects "has ever successfully reconciled" (#346), not just
	// "the initial boot reconcile succeeded" -- flip it here, the single
	// choke point every trigger path (startup, poll, webhook, socket, tcp,
	// api, drift-self-heal) runs through, so a later successful reconcile
	// can recover the daemon from a failed initial boot reconcile too.
	if err == nil {
		d.setReady(true)
	}

	return err
}

// reconcileCycle executes one reconcile using the trigger metadata selected for
// that cycle. The function type keeps the coalescing loop independently testable.
type reconcileCycle func(context.Context, reconcileTrigger) error

// reconcileLoop runs reconciliation, atomically draining triggers that arrived
// during each cycle into the following coalesced cycle.
func (d *Daemon) reconcileLoop(ctx context.Context, trigger reconcileTrigger, execute reconcileCycle) error {
	logger := log.ComponentCtx(ctx, log.ComponentDaemon)

	var lastErr error

	for {
		// Recover per cycle so a pending batch survives a panic and is still
		// drained by the same loop instead of being silently discarded.
		err := d.runReconcileCycleSafely(ctx, trigger, execute)
		if err != nil {
			lastErr = err
		}

		// Check and drain pending triggers while holding the same mutex that
		// producers use. Arrivals after this drain form the following batch,
		// so a trigger cannot be erased by a cycle-boundary reset.
		d.reconcileMu.Lock()
		if nextTrigger, triggerCount, ok := d.takePendingTriggers(); ok {
			trigger = nextTrigger
			d.reconcileMu.Unlock()
			logger.Info().
				Str(log.FieldSource, trigger.source).
				Bool("force", trigger.force).
				Uint64("trigger_count", triggerCount).
				Msg("Processing queued trigger from coalescing")
			ui.Info("Processing %d queued trigger(s) from %s (force=%t)", triggerCount, trigger.source, trigger.force)
			// Generate a fresh reconcile_id for the coalesced run so logs are distinct.
			// Rebuild from global logger + raw context values to avoid zerolog key duplication.
			ctx, newID := log.NewReconcileContext(ctx)
			refreshed := log.FromContext(ctx)
			ctx = log.WithContext(ctx, &refreshed)
			logger = log.ComponentCtx(ctx, log.ComponentDaemon)
			logger.Debug().
				Str(log.FieldReconcileID, newID).
				Msg("Coalesced reconciliation assigned new reconcile ID")
			continue
		}

		// No pending trigger - we're done
		d.reconciling = false
		d.reconcileMu.Unlock()
		if lastErr != nil {
			logger.Error().
				Err(lastErr).
				Msg("Reconciliation loop completed with error")
		} else {
			logger.Info().
				Msg("Reconciliation loop completed successfully")
		}
		return lastErr
	}
}

// runReconcileCycleSafely converts a panic from the reconcile pipeline into an
// ordinary error without unwinding the coalescing loop (#364). Keeping recovery
// inside the cycle preserves triggers that arrived before the panic.
func (d *Daemon) runReconcileCycleSafely(ctx context.Context, trigger reconcileTrigger, execute reconcileCycle) (result error) {
	defer func() {
		r := recover()
		if r == nil {
			return
		}

		logger := log.ComponentCtx(ctx, log.ComponentDaemon)
		logger.Error().
			Interface("panic", r).
			Str("stack", string(debug.Stack())).
			Str(log.FieldSource, trigger.source).
			Msg("Recovered from panic during reconciliation; daemon remains up")
		ui.Error("Recovered from panic during reconciliation (source: %s): %v", trigger.source, r)

		panicErr := fmt.Errorf("recovered from panic during reconciliation: %v", r)

		d.stateMu.Lock()
		d.lastReconcile = time.Now()
		d.lastError = panicErr
		d.stateMu.Unlock()

		if d.alerter != nil {
			alertCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			if alertErr := d.alerter.SendDeployFailure(alertCtx, "", trigger.source, panicErr.Error(), nil, 0); alertErr != nil {
				logger.Warn().Err(alertErr).Msg("Failed to send panic-recovery alert")
			}
			cancel()
		}

		result = panicErr
	}()

	return execute(ctx, trigger)
}

func (d *Daemon) executeReconcileTrigger(ctx context.Context, trigger reconcileTrigger) error {
	return d.executeReconcileWithMode(ctx, trigger.source, trigger.force, trigger.forceRedeployUnchanged)
}

// executeReconcile runs a reconciliation cycle across all configured targets.
// Each target is reconciled sequentially; a failure on one target does not
// prevent the next target from running.
func (d *Daemon) executeReconcile(ctx context.Context, source string, force bool) error {
	return d.executeReconcileWithMode(ctx, source, force, source == log.SourceDriftSelfHeal)
}

func (d *Daemon) executeReconcileWithMode(ctx context.Context, source string, force, forceRedeployUnchanged bool) error {
	start := time.Now()
	logger := log.ComponentCtx(ctx, log.ComponentReconcile)

	// Bound this reconcile cycle by ReconcileTimeout regardless of trigger source.
	// Entry points deliberately pass an unbounded parent context: wrapping them
	// outside the coalescing loop would make every queued cycle inherit the first
	// trigger's deadline instead of receiving a fresh per-cycle timeout budget.
	ctx, cancel := context.WithTimeout(ctx, d.config.ReconcileTimeout)
	defer cancel()

	// Start a Sentry transaction for performance monitoring.
	ctx, finishTx := sentrypkg.ReconcileTransaction(ctx, source)

	// Start OTel span for daemon-level reconciliation orchestration.
	ctx, otelSpan := telemetry.Tracer("daemon").Start(ctx, "daemon.reconcile",
		trace.WithAttributes(
			telemetry.StringAttr("source", source),
			telemetry.BoolAttr("force", force),
		),
	)

	// Reload daemon-level config from the operator's local bosun.yaml.
	//
	// Drift settings (debounce, self-heal, cooldown) are daemon-level concerns
	// controlled by the operator on the host, so they read from config.Load()
	// (project root), not from the git-cloned repo. Changes take effect on
	// the next cycle after the file is saved.
	//
	// Per-target settings (hooks, deploy_paths, critical_containers) are
	// deployment concerns pushed via git, so they reload from the repo clone
	// inside each target's reconciler.Run() via ConfigReloader.
	d.reloadDaemonConfig()

	// NOTE: Target list is resolved from the startup config snapshot. Per-target
	// operational overrides (hooks, critical containers, deploy sync paths) hot-reload
	// via ConfigReloader inside each reconciler.Run(). Structural target changes
	// (adding/removing targets, changing host/paths) require a daemon restart.
	targets, targetsErr := d.config.ReconcileConfig.ResolveTargets()
	if targetsErr == nil {
		targetsErr = reconcile.PreflightStagingEvidence(ctx, d.config.ReconcileConfig, targets)
	}
	if targetsErr != nil {
		// Fail loud (#391): a multi-target config carrying a reserved `default`
		// name fails the cycle instead of silently dropping the target.
		logger.Error().Err(targetsErr).Msg("Target or staging preflight failed, aborting reconciliation cycle")
		ui.Error("Target or staging preflight failed: %v", targetsErr)
	} else {
		logger.Info().
			Str(log.FieldSource, source).
			Bool("force", force).
			Int("target_count", len(targets)).
			Msg("Starting reconciliation cycle")

		ui.Info("Starting reconciliation (source: %s, force: %t, targets: %d)", source, force, len(targets))
	}

	firstErr := targetsErr
	successCount := 0

	// NOTE: Daemon-level drift checks (runDriftCheck), health status (HealthStatus),
	// and circuit-breaker state still read from the base config's single state file.
	// In multi-target mode, each target writes its own state file, but these daemon
	// subsystems are not yet fan-out aware. This means periodic drift alerts and
	// health reporting only reflect the default/base target until these are refactored.
	for _, target := range targets {
		targetLogger := logger.With().Str("target", target.Name).Logger()

		// Create a per-target config and reconciler.
		targetCfg := d.config.ReconcileConfig.ConfigForTarget(target)
		targetCfg.Source = source
		targetCfg.Force = force

		// Drift self-heal must bypass the commit-unchanged skip (#350) --
		// image_mismatch/unhealthy drift doesn't move the declared commit --
		// but must NOT silently override the circuit breaker or the
		// deploy_paths allowlist gate the way operator Force does. Route it
		// through ForceRedeployUnchanged instead of Force so those stay keyed
		// on a real human decision.
		if forceRedeployUnchanged {
			targetCfg.ForceRedeployUnchanged = true
		}

		r := reconcile.NewReconciler(targetCfg, d.reconcileOpts...)

		targetLogger.Info().Msg("Reconciling target")
		if !target.IsDefault() {
			ui.Info("Reconciling target: %s", target.Name)
		}

		err := r.Run(ctx)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("target %s: %w", target.Name, err)
			}
			targetLogger.Error().Err(err).Msg("Target reconciliation failed")
			ui.Error("Target %s failed: %v", target.Name, err)
			continue
		}

		successCount++
		targetLogger.Info().Msg("Target reconciliation completed")
	}

	// Determine overall cycle result.
	cycleErr := firstErr
	if cycleErr != nil {
		telemetry.SpanError(otelSpan, cycleErr)
	} else {
		telemetry.SpanOK(otelSpan)
	}
	otelSpan.End()

	// Update state (use stateMu for thread-safe reads from health checks).
	d.stateMu.Lock()
	d.lastReconcile = time.Now()
	d.lastError = cycleErr
	d.stateMu.Unlock()

	durationMS := time.Since(start).Milliseconds()

	// Finish the Sentry transaction with the result status.
	finishTx(cycleErr)

	durationSec := time.Since(start).Seconds()

	if cycleErr != nil {
		if d.metrics != nil {
			d.metrics.RecordReconcileFailure(durationSec)
		}

		logger.Error().
			Str(log.FieldSource, source).
			Int64(log.FieldDurationMS, durationMS).
			Int("success_count", successCount).
			Int("target_count", len(targets)).
			Err(cycleErr).
			Msg("Reconciliation cycle completed with errors")

		ui.Error("Reconciliation completed with errors after %s (%d/%d targets succeeded)",
			time.Since(start), successCount, len(targets))
		return cycleErr
	}

	if d.metrics != nil {
		d.metrics.RecordReconcileSuccess(durationSec)
		d.metrics.SetLastReconcileTime(float64(d.lastReconcile.Unix()))
	}

	logger.Info().
		Str(log.FieldSource, source).
		Int64(log.FieldDurationMS, durationMS).
		Int("target_count", len(targets)).
		Bool("success", true).
		Msg("Reconciliation cycle completed")

	ui.Success("Reconciliation completed in %s (%d targets)", time.Since(start), len(targets))
	return nil
}

// pollLoop runs periodic reconciliation.
func (d *Daemon) pollLoop(ctx context.Context) {
	logger := log.Component(log.ComponentDaemon)
	logger.Info().
		Dur("interval", d.config.PollInterval).
		Msg("Polling loop started")
	ticker := time.NewTicker(d.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			logger.Debug().
				Str(log.FieldSource, log.SourcePoll).
				Msg("Poll interval triggered, attempting reconciliation")
			if err := d.TriggerReconcile(ctx, log.SourcePoll, false); err != nil {
				logger.Warn().
					Err(err).
					Str(log.FieldSource, log.SourcePoll).
					Msg("Poll reconciliation failed")
			}
		case <-d.stopLoops:
			logger.Debug().Msg("Polling loop stopped")
			return
		case <-ctx.Done():
			logger.Debug().Msg("Polling loop cancelled")
			return
		}
	}
}

// driftCheckLoop runs periodic drift checks independent of reconciliation.
func (d *Daemon) driftCheckLoop(ctx context.Context) {
	logger := log.Component(log.ComponentDaemon)
	logger.Info().
		Dur("interval", d.config.DriftInterval).
		Msg("Drift check loop started")
	ticker := time.NewTicker(d.config.DriftInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			logger.Debug().
				Dur("interval", d.config.DriftInterval).
				Msg("Drift check interval triggered")
			d.runDriftCheck(ctx)
		case <-d.stopLoops:
			logger.Debug().Msg("Drift check loop stopped")
			return
		case <-ctx.Done():
			logger.Debug().Msg("Drift check loop cancelled")
			return
		}
	}
}

// runDriftCheck performs a single drift check and updates the state file.
// Skips if a reconciliation is in progress to avoid state file race conditions.
func (d *Daemon) runDriftCheck(ctx context.Context) {
	logger := log.Component(log.ComponentDaemon)
	d.driftCheckMu.Lock()
	defer d.driftCheckMu.Unlock()

	// Reconciliation and drift checking both load, mutate, and save the same
	// state file. Hold their shared admission mutex for the complete drift cycle
	// so a reconcile cannot begin after this one-time busy check and overwrite
	// drift, breaker, or self-heal state with a stale snapshot.
	d.reconcileMu.Lock()
	if d.reconciling {
		d.reconcileMu.Unlock()
		logger.Debug().
			Str("reason", "reconciliation_in_progress").
			Msg("Skipping drift check")
		return
	}
	defer d.reconcileMu.Unlock()

	// Snapshot reloadable config fields once so we hold the lock only briefly.
	dcfg := d.driftConfig()

	stateFile := ""
	projectName := ""
	if d.config.ReconcileConfig != nil {
		stateFile = d.config.ReconcileConfig.StateFile
		projectName = d.config.ReconcileConfig.ProjectName
	}
	if stateFile == "" {
		logger.Debug().
			Str("reason", "no_state_file_configured").
			Msg("Skipping drift check")
		return
	}

	logger.Debug().
		Str(log.FieldPath, stateFile).
		Str("project", projectName).
		Msg("Preparing to collect Docker state for drift check")

	client, err := d.DockerClient()
	if err != nil {
		logger.Warn().
			Err(err).
			Msg("Failed to get Docker client, skipping drift check")
		return
	}

	checkCtx, cancel := context.WithTimeout(ctx, d.config.APITimeout)
	defer cancel()

	// Collect drift ignore rules from reconcile config.
	var ignoreRules []reconcile.DriftIgnoreRule
	if d.config.ReconcileConfig != nil {
		ignoreRules = d.config.ReconcileConfig.DriftIgnore.Value
	}

	checkCtx, finishSpan := sentrypkg.StartSpan(checkCtx, "drift.periodic_check", "Periodic drift check")
	checkCtx, otelDriftSpan := telemetry.Tracer("daemon").Start(checkCtx, "daemon.drift_check")
	report, err := reconcile.RunDriftCheck(checkCtx, client, stateFile, projectName, ignoreRules)
	finishSpan(err)
	if err != nil {
		telemetry.SpanError(otelDriftSpan, err)
		otelDriftSpan.End()
		logger.Warn().Err(err).Msg("Drift check failed")
		return
	}
	telemetry.SpanOK(otelDriftSpan)
	otelDriftSpan.End()

	// Load previous state to detect drift resolution.
	state := reconcile.LoadState(stateFile)
	previouslyDrifted := len(state.DriftItems) > 0

	// Stage drift results — save happens after alert dedup updates the state.
	state.DriftCheckedAt = report.CheckedAt
	state.DriftItems = report.Items

	// Initialize debounce map for FilterDebounced mutation.
	if state.DriftDebounceItems == nil {
		state.DriftDebounceItems = make(map[string]time.Time)
	}

	// Keep filtered and raw critical drift separate: filtered items drive alerts,
	// while raw items decide whether a service actually resolved. An ignore rule
	// silently retires prior alert state instead of lying that active drift cleared.
	activeCriticalItems := criticalDriftItems(report.Items)
	unfilteredCriticalItems := criticalDriftItems(report.UnfilteredItems)
	suppressedKeys := reconcile.SuppressedDriftAlertKeys(
		unfilteredCriticalItems, activeCriticalItems, state.DriftAlertedItems,
	)
	for _, key := range suppressedKeys {
		delete(state.DriftAlertedItems, key)
		delete(state.DriftDebounceItems, key)
	}
	if len(suppressedKeys) > 0 {
		logger.Debug().
			Strs("items", suppressedKeys).
			Msg("Removed drift alert state for items suppressed by ignore rules")
	}

	if d.metrics != nil {
		d.metrics.RecordDriftCheck(report.HasDrift())
	}

	if report.HasDrift() {
		logger.Warn().
			Int("drift_items", len(report.Items)).
			Strs("drift_containers", report.DriftSummaries()).
			Msg("Drift detected during periodic check")

		// Deduplicated alerting for critical drift (missing/unhealthy).
		// Always run cleanup when alerting is enabled so resolved items are cleared
		// even if only non-critical drift remains.
		if d.alerter != nil {
			// Debounce layer: filter items that haven't persisted past the debounce window.
			// Only debounce items not yet in the dedup/cooldown layer. Once an item
			// has alerted, keep it flowing through ShouldAlertDrift directly.
			var pendingItems []reconcile.DriftItem
			var alreadyAlerted []reconcile.DriftItem
			for _, item := range activeCriticalItems {
				if _, alerted := state.DriftAlertedItems[reconcile.DriftAlertKey(item)]; alerted {
					alreadyAlerted = append(alreadyAlerted, item)
				} else {
					pendingItems = append(pendingItems, item)
				}
			}

			// FilterDebounced mutates state.DriftDebounceItems in place (adds new, removes graduated/resolved).
			preDebounceCount := len(pendingItems)
			pendingItems = reconcile.FilterDebounced(pendingItems, state.DriftDebounceItems, dcfg.driftAlertDebounce)
			alertCandidates := append(alreadyAlerted, pendingItems...)

			filteredCount := preDebounceCount - len(pendingItems)
			if filteredCount > 0 {
				logger.Debug().
					Int("filtered", filteredCount).
					Int("debounce_pending", len(state.DriftDebounceItems)).
					Dur("debounce_window", dcfg.driftAlertDebounce).
					Msg("Drift items filtered by debounce window")
			}
			if len(pendingItems) > 0 && dcfg.driftAlertDebounce > 0 {
				for _, item := range pendingItems {
					logger.Info().
						Str("item", reconcile.DriftAlertKey(item)).
						Dur("debounce_window", dcfg.driftAlertDebounce).
						Msg("Drift item graduated from debounce to dedup layer")
				}
			}

			if state.DriftAlertedItems == nil {
				state.DriftAlertedItems = make(map[string]time.Time)
			}

			alertItems, resolvedKeys := reconcile.ShouldAlertDrift(
				unfilteredCriticalItems, alertCandidates, state.DriftAlertedItems, d.config.DriftAlertCooldown,
			)

			if len(alertItems) > 0 {
				if err := d.sendDriftAlert(ctx, &reconcile.DriftReport{
					CheckedAt: report.CheckedAt,
					Items:     alertItems,
				}); err != nil {
					// Delivery failed — leave dedup/cooldown state untouched so the
					// next drift check retries the alert instead of silently dropping it.
					logger.Warn().
						Err(err).
						Int("items", len(alertItems)).
						Msg("Drift alert delivery failed, will retry on next check")
				} else {
					state.RecordDriftAlerts(alertItems, time.Now())
				}
			}

			// Resolution alerts: only fire for items that were previously alerted.
			// Skip items still in debounce (never alerted).
			if len(resolvedKeys) > 0 && d.config.DriftResolveAlerts {
				var actuallyResolved []string
				for _, key := range resolvedKeys {
					if _, inDebounce := state.DriftDebounceItems[key]; inDebounce {
						// Item resolved while still in debounce — no alert was ever sent.
						logger.Debug().
							Str("item", key).
							Msg("Drift item resolved before debounce window expired, no resolution alert")
						delete(state.DriftDebounceItems, key)
						continue
					}
					actuallyResolved = append(actuallyResolved, key)
				}
				if len(actuallyResolved) > 0 {
					if err := d.sendDriftResolvedAlert(ctx, actuallyResolved); err != nil {
						// Delivery failed — keep the items marked as alerted so the
						// resolution alert is retried on the next drift check.
						logger.Warn().
							Err(err).
							Int("items", len(actuallyResolved)).
							Msg("Drift resolved alert delivery failed, will retry on next check")
					} else {
						for _, key := range actuallyResolved {
							delete(state.DriftAlertedItems, key)
						}
					}
				}
			}
		}
	} else {
		// No drift — clean up debounce items and check if we need to send resolution alerts.
		if len(state.DriftDebounceItems) > 0 {
			logger.Debug().
				Int("cleared", len(state.DriftDebounceItems)).
				Msg("Drift resolved: clearing debounce items")
			state.DriftDebounceItems = nil
		}

		if len(report.UnfilteredItems) > 0 {
			logger.Debug().
				Int("ignored_items", report.IgnoredCount).
				Msg("Drift check: all detected drift suppressed by ignore rules")
		} else if previouslyDrifted {
			logger.Info().Msg("Drift resolved: all declared services now match actual state")
		} else {
			logger.Debug().Msg("Drift check: no drift detected")
		}

		if len(state.DriftAlertedItems) > 0 && d.alerter != nil && d.config.DriftResolveAlerts {
			var resolvedKeys []string
			for key := range state.DriftAlertedItems {
				resolvedKeys = append(resolvedKeys, key)
			}
			if err := d.sendDriftResolvedAlert(ctx, resolvedKeys); err != nil {
				// Delivery failed — leave DriftAlertedItems intact so the resolution
				// alert is retried on the next drift check.
				logger.Warn().
					Err(err).
					Int("items", len(resolvedKeys)).
					Msg("Drift resolved alert delivery failed, will retry on next check")
			} else {
				state.DriftAlertedItems = nil
			}
		}
	}

	// Clean up empty debounce map for omitempty serialization.
	if len(state.DriftDebounceItems) == 0 {
		state.DriftDebounceItems = nil
	}

	// Restart circuit breaker: detect containers in restart loops and stop them.
	if d.config.ReconcileConfig != nil && d.config.ReconcileConfig.RestartBreakerEnabled {
		d.runRestartBreaker(checkCtx, client, state, projectName)
	}

	// Clean up empty restart tracking for omitempty serialization.
	if len(state.RestartTracking) == 0 {
		state.RestartTracking = nil
	}

	// Plan self-heal before the shared atomic state write. Side effects only run
	// after that write succeeds, so a daemon restart cannot forget a consumed
	// attempt or repeat an exhaustion alert.
	previousSelfHeal := cloneDriftSelfHealTracking(state.DriftSelfHeal)
	selfHeal := planSelfHeal(state, report, dcfg, false, time.Now())
	var selfHealReservation *reconcileGoroutineReservation
	if selfHeal.trigger {
		var accepted bool
		selfHealReservation, accepted = d.reserveSelfHealReconcile(ctx)
		if !accepted {
			// Shutdown won lifecycle admission, so no trigger occurred and no
			// attempt may be consumed. Preserve all other drift state updates.
			state.DriftSelfHeal = previousSelfHeal
			selfHeal.trigger = false
			selfHeal.reason = "daemon_shutting_down"
		}
	}
	if selfHealReservation != nil {
		defer selfHealReservation.release(false)
	}

	// Persist state with drift results, alert timestamps, and self-heal budget.
	if err := reconcile.SaveState(stateFile, state); err != nil {
		logger.Warn().Err(err).Msg("Drift check: failed to save state")
		return
	}

	d.maybeSelfHeal(ctx, selfHeal, selfHealReservation)
}

func criticalDriftItems(items []reconcile.DriftItem) []reconcile.DriftItem {
	var criticalItems []reconcile.DriftItem
	for _, item := range items {
		if item.Type == reconcile.DriftMissing || item.Type == reconcile.DriftUnhealthy {
			criticalItems = append(criticalItems, item)
		}
	}
	return criticalItems
}

// maybeSelfHeal executes a durable decision produced by planSelfHeal. The
// attempt and once-per-signature alert marker have already been persisted.
func (d *Daemon) maybeSelfHeal(ctx context.Context, decision selfHealDecision, reservation *reconcileGoroutineReservation) {
	logger := log.Component(log.ComponentDaemon)

	if !decision.trigger && !decision.alertExhausted {
		event := logger.Debug().
			Str("reason", decision.reason).
			Str("drift_signature", decision.signatureID)
		if decision.remainingCooldown > 0 {
			event = event.Dur("remaining_cooldown", decision.remainingCooldown)
		}
		event.Msg("Skipping drift self-heal")
		return
	}

	if decision.alertExhausted {
		logger.Error().
			Str("source", "self-heal-exhausted").
			Str("drift_signature", decision.signatureID).
			Int("drift_items", decision.itemCount).
			Int("attempts", decision.attempts).
			Int("max_attempts", decision.maxAttempts).
			Msg("Drift self-heal attempt budget exhausted")
		if d.alerter != nil {
			target := "local"
			if d.config.ReconcileConfig != nil && d.config.ReconcileConfig.ProjectName != "" {
				target = d.config.ReconcileConfig.ProjectName
			}
			if err := d.alerter.SendDriftSelfHealExhausted(
				ctx, target, decision.signatureID,
				decision.itemCount, decision.attempts,
			); err != nil {
				logger.Warn().Err(err).Msg("Failed to dispatch drift self-heal exhaustion alert")
			}
		}
	}

	if !decision.trigger {
		return
	}

	logger.Info().
		Str("drift_signature", decision.signatureID).
		Int("drift_items", decision.itemCount).
		Int("attempt", decision.attempts).
		Int("max_attempts", decision.maxAttempts).
		Msg("Triggering reconciliation to resolve detected drift via self-heal")

	ui.Info("Drift self-heal: triggering reconciliation (attempt %d/%d, %d drift items)", decision.attempts, decision.maxAttempts, decision.itemCount)

	reservation.release(true)
}

func (d *Daemon) reserveSelfHealReconcile(ctx context.Context) (*reconcileGoroutineReservation, bool) {
	logger := log.Component(log.ComponentDaemon)
	return d.reserveReconcileGoroutine(ctx, func(reconcileCtx context.Context) {
		// force stays false here -- Force is the human-supplied override that
		// also bypasses the circuit breaker and deploy_paths gate, and an
		// unattended self-heal loop must never touch either. executeReconcile
		// grants ForceRedeployUnchanged based on the "drift-self-heal" source
		// instead (#350 / review follow-up), bypassing only the
		// commit-unchanged skip.
		if err := d.runTriggerReconcile(reconcileCtx, log.SourceDriftSelfHeal, false); err != nil {
			logger.Error().
				Err(err).
				Msg("Failed to trigger drift self-heal reconciliation")
		} else {
			logger.Debug().
				Msg("Drift self-heal reconciliation triggered successfully")
		}
	})
}

// runRestartBreaker checks running containers for restart loops and stops offenders.
func (d *Daemon) runRestartBreaker(ctx context.Context, client *docker.Client, state *reconcile.DeployState, projectName string) {
	logger := log.Component(log.ComponentDaemon)
	logger.Debug().
		Str("project", projectName).
		Msg("Preparing to run restart circuit breaker check")
	rcfg := d.config.ReconcileConfig

	actual, err := reconcile.CollectActualState(ctx, client, projectName)
	if err != nil {
		logger.Warn().
			Err(err).
			Str("project", projectName).
			Msg("Failed to collect container state for restart breaker")
		return
	}

	result, err := reconcile.RunRestartBreaker(
		ctx, client, actual, state,
		rcfg.RestartThreshold, rcfg.RestartWindow,
	)
	if err != nil {
		logger.Error().
			Err(err).
			Msg("Restart breaker action failed")
		return
	}

	// Update state with new tracking data.
	state.RestartTracking = result.Updated

	// Alert on tripped containers.
	if len(result.Tripped) > 0 {
		logger.Info().
			Strs("containers", result.Tripped).
			Msg("Restart circuit breaker tripped, stopping containers")
		if d.alerter != nil {
			target := projectName
			if err := d.alerter.SendRestartBreakerTripped(ctx, target, result.Tripped); err != nil {
				logger.Warn().
					Err(err).
					Str("target", target).
					Msg("Failed to send restart breaker alert")
			}
		}
	}

	// Alert on resolved containers.
	if len(result.Resolved) > 0 {
		logger.Info().
			Strs("containers", result.Resolved).
			Msg("Containers resolved from restart circuit breaker")
		if d.alerter != nil {
			target := projectName
			if err := d.alerter.SendRestartBreakerResolved(ctx, target, result.Resolved); err != nil {
				logger.Warn().
					Err(err).
					Str("target", target).
					Msg("Failed to send restart breaker resolved alert")
			}
		}
	} else if len(result.Tripped) == 0 {
		logger.Debug().Msg("Restart breaker check complete: no new trip or resolution transitions")
	}
}

// sendDriftAlert sends an alert for detected drift. Returns the delivery
// error, if any, so callers can avoid advancing dedup/cooldown state for an
// alert that never reached the provider.
func (d *Daemon) sendDriftAlert(ctx context.Context, report *reconcile.DriftReport) error {
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
		return err
	}
	return nil
}

// sendDriftResolvedAlert sends an alert for drift items that have cleared.
// Returns the delivery error, if any, so callers can avoid clearing alerted
// state for a resolution alert that never reached the provider.
func (d *Daemon) sendDriftResolvedAlert(ctx context.Context, resolvedKeys []string) error {
	logger := log.Component(log.ComponentDaemon)

	target := "local"
	if d.config.ReconcileConfig != nil && d.config.ReconcileConfig.TargetHost != "" {
		target = d.config.ReconcileConfig.TargetHost
	}

	if err := d.alerter.SendDriftResolved(ctx, target, resolvedKeys); err != nil {
		logger.Warn().Err(err).Msg("Failed to send drift resolved alert")
		return err
	}
	return nil
}

// IsReady returns whether the daemon is ready to serve requests.
func (d *Daemon) IsReady() bool {
	d.readyMu.RLock()
	defer d.readyMu.RUnlock()
	return d.ready
}

// setReady sets the readiness state and updates the Prometheus gauge.
func (d *Daemon) setReady(ready bool) {
	d.readyMu.Lock()
	defer d.readyMu.Unlock()
	d.ready = ready
	if d.metrics != nil {
		d.metrics.SetReady(ready)
	}
}

// LastReconcile returns the time of the last reconciliation and any error.
func (d *Daemon) LastReconcile() (time.Time, error) {
	d.stateMu.RLock()
	defer d.stateMu.RUnlock()
	return d.lastReconcile, d.lastError
}

// driftRuntimeConfig is a snapshot of daemon config fields used during drift checks.
type driftRuntimeConfig struct {
	driftAlertDebounce    time.Duration
	driftSelfHeal         bool
	driftSelfHealCooldown time.Duration
	maxSelfHealAttempts   int
}

// driftConfig returns a consistent snapshot of drift-related config fields.
func (d *Daemon) driftConfig() driftRuntimeConfig {
	d.configMu.RLock()
	defer d.configMu.RUnlock()
	return driftRuntimeConfig{
		driftAlertDebounce:    d.config.DriftAlertDebounce.Value,
		driftSelfHeal:         d.config.DriftSelfHeal.Value,
		driftSelfHealCooldown: d.config.DriftSelfHealCooldown.Value,
		maxSelfHealAttempts:   d.config.MaxSelfHealAttempts,
	}
}

// reloadDaemonConfig re-reads bosun.yaml from the local project root and updates
// daemon-level config fields that are safe to change at runtime. Fields locked by
// environment variables are not updated.
func (d *Daemon) reloadDaemonConfig() {
	logger := log.Component(log.ComponentDaemon)

	projectCfg, err := config.Load()
	if err != nil {
		// Missing project config is non-fatal — daemon can run without bosun.yaml.
		logger.Debug().Err(err).Msg("Daemon config reload: no project config found, keeping existing values")
		return
	}

	d.configMu.Lock()
	defer d.configMu.Unlock()

	changed := false

	if !d.config.DriftAlertDebounce.FromEnv() {
		newVal := projectCfg.DriftAlertDebounce()
		if newVal != d.config.DriftAlertDebounce.Value {
			d.config.DriftAlertDebounce.SetFromFile(newVal)
			changed = true
		}
	}

	if !d.config.DriftSelfHeal.FromEnv() {
		newVal := projectCfg.DriftSelfHeal()
		if newVal != d.config.DriftSelfHeal.Value {
			d.config.DriftSelfHeal.SetFromFile(newVal)
			changed = true
		}
	}

	if !d.config.DriftSelfHealCooldown.FromEnv() {
		newVal := projectCfg.DriftSelfHealCooldown()
		if newVal == 0 {
			newVal = DefaultConfig().DriftSelfHealCooldown.Value
		}
		if newVal != d.config.DriftSelfHealCooldown.Value {
			d.config.DriftSelfHealCooldown.SetFromFile(newVal)
			changed = true
		}
	}

	if changed {
		logger.Info().
			Dur("drift_alert_debounce", d.config.DriftAlertDebounce.Value).
			Bool("drift_self_heal", d.config.DriftSelfHeal.Value).
			Dur("drift_self_heal_cooldown", d.config.DriftSelfHealCooldown.Value).
			Msg("Daemon config reloaded from bosun.yaml")
	}
}

// HealthStatus returns the daemon health status with subsystem breakdown.
func (d *Daemon) HealthStatus() HealthStatus {
	lastReconcile, lastError := d.LastReconcile()

	subsystems := make(map[string]SubsystemStatus)

	// Docker subsystem: live ping check (not cached).
	dockerSub := SubsystemStatus{Status: StatusHealthy}
	if client, err := d.DockerClient(); err != nil {
		dockerSub.Status = StatusUnhealthy
		dockerSub.Message = err.Error()
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := client.Ping(ctx); err != nil {
			dockerSub.Status = StatusUnhealthy
			dockerSub.Message = fmt.Sprintf("ping failed: %v", err)
		}
	}
	subsystems["docker"] = dockerSub

	// Git subsystem: report healthy if repo URL is configured.
	gitSub := SubsystemStatus{Status: StatusHealthy}
	if d.config.ReconcileConfig == nil || d.config.ReconcileConfig.RepoURL == "" {
		gitSub.Status = StatusUnhealthy
		gitSub.Message = "no repository URL configured"
	}
	subsystems["git"] = gitSub

	// Reconciler subsystem: report last run time and error.
	reconcilerSub := SubsystemStatus{Status: StatusHealthy}
	if !lastReconcile.IsZero() {
		ts := lastReconcile.UTC().Format(time.RFC3339)
		reconcilerSub.LastRun = ts
	}
	if lastError != nil {
		reconcilerSub.Status = StatusDegraded
		reconcilerSub.Message = reconcile.SanitizeGitText(lastError.Error())
	}
	subsystems["reconciler"] = reconcilerSub

	// Circuit breaker subsystem: read from deploy state file.
	cbSub := SubsystemStatus{Status: StatusClosed}
	if d.config.ReconcileConfig != nil && d.config.ReconcileConfig.StateFile != "" {
		state := reconcile.LoadState(d.config.ReconcileConfig.StateFile)
		cbSub.Failures = state.AttemptCount
		if state.AttemptCount >= reconcile.MaxAttempts && state.LastAttemptedCommit != "" && state.LastAttemptedCommit == state.LastDeployedCommit {
			// Circuit breaker tripped but commit matched deployed — reset scenario.
			// Keep closed.
		} else if state.AttemptCount >= reconcile.MaxAttempts {
			cbSub.Status = StatusOpen
		}
	}
	subsystems["circuit_breaker"] = cbSub

	// Compute top-level status from subsystems.
	topLevel := computeTopLevelStatus(subsystems)

	status := HealthStatus{
		Status:        topLevel,
		Ready:         d.IsReady(),
		LastReconcile: lastReconcile,
		Uptime:        time.Since(startTime),
		Subsystems:    subsystems,
	}

	if lastError != nil {
		status.LastError = reconcile.SanitizeGitText(lastError.Error())
	}

	return status
}

// Status constants for subsystem health.
const (
	StatusHealthy   = "healthy"
	StatusDegraded  = "degraded"
	StatusUnhealthy = "unhealthy"
	StatusClosed    = "closed"
	StatusOpen      = "open"
)

// SubsystemStatus represents the health of an individual subsystem.
type SubsystemStatus struct {
	Status   string `json:"status"`
	Message  string `json:"message,omitempty"`
	LastRun  string `json:"last_run,omitempty"`
	Failures int    `json:"failures,omitempty"`
}

// HealthStatus represents the daemon health with subsystem breakdown.
type HealthStatus struct {
	Status        string                     `json:"status"`
	Ready         bool                       `json:"ready"`
	LastReconcile time.Time                  `json:"last_reconcile,omitempty"`
	LastError     string                     `json:"last_error,omitempty"`
	Uptime        time.Duration              `json:"uptime"`
	Subsystems    map[string]SubsystemStatus `json:"subsystems"`
}

// HealthResponse is the bounded public liveness response returned by /health.
// Detailed reconcile and subsystem diagnostics belong on /status and in logs.
type HealthResponse struct {
	Status string        `json:"status"`
	Ready  bool          `json:"ready"`
	Uptime time.Duration `json:"uptime"`
}

func publicHealthResponse(status HealthStatus) HealthResponse {
	return HealthResponse{
		Status: status.Status,
		Ready:  status.Ready,
		Uptime: status.Uptime,
	}
}

// computeTopLevelStatus derives the top-level health status from subsystems.
// Docker down = "unhealthy" (critical). Any other subsystem degraded/unhealthy/open = "degraded".
func computeTopLevelStatus(subsystems map[string]SubsystemStatus) string {
	if docker, ok := subsystems["docker"]; ok && docker.Status == StatusUnhealthy {
		return StatusUnhealthy
	}

	for _, sub := range subsystems {
		switch sub.Status {
		case StatusDegraded, StatusUnhealthy, StatusOpen:
			return StatusDegraded
		}
	}

	return StatusHealthy
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

func warnRestartBreakerSampling(logger zerolog.Logger, driftInterval, restartWindow time.Duration) {
	if !reconcile.RestartBreakerSamplingMismatch(driftInterval, restartWindow) {
		return
	}
	logger.Warn().
		Dur("drift_interval", driftInterval).
		Dur("restart_window", restartWindow).
		Msg("BOSUN_DRIFT_INTERVAL exceeds BOSUN_RESTART_WINDOW; restart counts are sampled less frequently than the configured window")
}

// ConfigFromEnv loads daemon configuration from environment variables.
func ConfigFromEnv() *Config {
	cfg := DefaultConfig()

	// Socket configuration
	if socketPath := os.Getenv("BOSUN_SOCKET_PATH"); socketPath != "" {
		cfg.SocketPath = socketPath
	}
	if value := os.Getenv("BOSUN_SOCKET_ALLOWED_UIDS"); value != "" {
		allowedUIDs, err := parseSocketAllowedUIDs(value)
		if err != nil {
			cfg.socketAllowedUIDsError = err
		} else {
			cfg.SocketAllowedUIDs = allowedUIDs
		}
	}
	// Security opt-outs use an exact lowercase match so values such as "TRUE"
	// cannot accidentally weaken the default posture.
	cfg.AllowUnauthenticatedSocket = os.Getenv("BOSUN_ALLOW_UNAUTHENTICATED_SOCKET") == "true"

	// HTTP configuration
	if port := os.Getenv("PORT"); port != "" {
		_, _ = fmt.Sscanf(port, "%d", &cfg.Port)
	}
	if port := os.Getenv("WEBHOOK_PORT"); port != "" {
		_, _ = fmt.Sscanf(port, "%d", &cfg.Port)
	}

	// Disable HTTP server if explicitly set
	if v := os.Getenv("BOSUN_DISABLE_HTTP"); v != "" {
		cfg.EnableHTTP = !parseBoolVal(v, false)
	}

	// TCP server configuration (opt-in)
	if v := os.Getenv("BOSUN_ENABLE_TCP"); v != "" {
		cfg.EnableTCP = parseBoolVal(v, false)
	}
	if addr := os.Getenv("BOSUN_TCP_ADDR"); addr != "" {
		cfg.TCPAddr = addr
	}
	cfg.BearerToken = os.Getenv("BOSUN_BEARER_TOKEN")

	cfg.WebhookSecret = os.Getenv("WEBHOOK_SECRET")
	if secret := os.Getenv("GITHUB_WEBHOOK_SECRET"); secret != "" {
		cfg.WebhookSecret = secret
	}

	// Read-scope credential for /metrics and /api/widget (#296), separate from
	// the control bearer. AllowUnauthenticatedMetrics uses the same strict
	// lowercase "true" match as the webhook opt-out.
	cfg.MetricsToken = os.Getenv("BOSUN_METRICS_TOKEN")
	cfg.AllowUnauthenticatedMetrics = os.Getenv("BOSUN_ALLOW_UNAUTHENTICATED_METRICS") == "true"

	// HTTP bind address (empty = all interfaces; see Config.ListenAddr).
	cfg.ListenAddr = os.Getenv("BOSUN_LISTEN_ADDR")

	if d := config.BosunEnvDuration("POLL_INTERVAL", 0); d > 0 {
		cfg.PollInterval = d
	}

	// Reconcile config from environment
	rcfg := reconcile.DefaultConfig()
	rcfg.RepoURL = config.BosunEnv("REPO_URL")
	if branch := config.BosunEnv("REPO_BRANCH"); branch != "" {
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

	rcfg.DryRun = parseBoolVal(os.Getenv("DRY_RUN"), false)

	// Deploy mode override: "local" or "remote" skips auto-detection.
	if mode := os.Getenv("BOSUN_DEPLOY_MODE"); mode != "" {
		rcfg.DeployMode = mode
	}

	if infraDir := os.Getenv("BOSUN_INFRA_DIR"); infraDir != "" {
		rcfg.InfraSubDir = infraDir
	}

	// State directory override
	if stateDir := os.Getenv("BOSUN_STATE_DIR"); stateDir != "" {
		rcfg.StateFile = filepath.Join(stateDir, reconcile.DefaultStateFile)
	}

	// Multi-target configuration: BOSUN_TARGETS (JSON array) overrides bosun.yaml targets.
	// An explicit empty array ("[]") clears repo-defined targets, falling back to the
	// implicit default target.
	targetsFromEnv := false
	if v := os.Getenv("BOSUN_TARGETS"); v != "" {
		targets, apply, err := reconcile.ParseTargetsOverride(v)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to parse BOSUN_TARGETS, ignoring")
		} else if apply {
			// Validate security-sensitive fields on each target parsed from env.
			// Warn and clear (rather than skip) to mirror the YAML-load semantics
			// in extractTargets — a single bad field must not block the whole target.
			targets = reconcile.ValidateAndSanitizeTargets(targets, func(target, field string, err error) {
				log.Warn().Err(err).
					Str("target", target).
					Str(field, "").
					Msgf("BOSUN_TARGETS: invalid %s — ignoring and inheriting global value", field)
			})
			rcfg.Targets = targets
			targetsFromEnv = true
		}
	}
	rcfg.TargetsFromEnv = targetsFromEnv

	cfg.ReconcileConfig = rcfg

	// Compose up timeout override
	if v := os.Getenv("BOSUN_COMPOSE_UP_TIMEOUT"); v != "" {
		if d, ok := parseDurationOrSeconds(v); ok {
			if d <= 0 {
				log.Warn().Str("env", "BOSUN_COMPOSE_UP_TIMEOUT").Str("value", v).Msg("Skipping env var. Reason: duration must be positive")
			} else {
				rcfg.ComposeUpTimeout = d
			}
		} else {
			log.Warn().Str("env", "BOSUN_COMPOSE_UP_TIMEOUT").Str("value", v).Msg("Skipping env var. Reason: invalid duration format")
		}
	}
	// Backup timeout override
	if v := os.Getenv("BOSUN_BACKUP_TIMEOUT"); v != "" {
		if d, ok := parseDurationOrSeconds(v); ok {
			if d <= 0 {
				log.Warn().Str("env", "BOSUN_BACKUP_TIMEOUT").Str("value", v).Msg("Skipping env var. Reason: duration must be positive")
			} else {
				log.Debug().Str("env", "BOSUN_BACKUP_TIMEOUT").Int64("duration_ms", d.Milliseconds()).Msg("Backup timeout configured from environment")
				rcfg.BackupTimeout = d
			}
		} else {
			log.Warn().Str("env", "BOSUN_BACKUP_TIMEOUT").Str("value", v).Msg("Skipping env var. Reason: invalid duration format")
		}
	}
	if v := os.Getenv("BOSUN_HEALTH_CHECK_TIMEOUT"); v != "" {
		if d, ok := parseDurationOrSeconds(v); ok {
			if d < 0 {
				log.Warn().Str("env", "BOSUN_HEALTH_CHECK_TIMEOUT").Str("value", v).Msg("Skipping env var. Reason: duration must not be negative")
			} else {
				rcfg.HealthCheckTimeout = d
			}
		} else {
			log.Warn().Str("env", "BOSUN_HEALTH_CHECK_TIMEOUT").Str("value", v).Msg("Skipping env var. Reason: invalid duration format")
		}
	}
	if v := os.Getenv("BOSUN_HEALTH_CHECK_INTERVAL"); v != "" {
		if d, ok := parseDurationOrSeconds(v); ok {
			if d <= 0 {
				log.Warn().Str("env", "BOSUN_HEALTH_CHECK_INTERVAL").Str("value", v).Msg("Skipping env var. Reason: duration must be positive")
			} else {
				rcfg.HealthCheckInterval = d
			}
		} else {
			log.Warn().Str("env", "BOSUN_HEALTH_CHECK_INTERVAL").Str("value", v).Msg("Skipping env var. Reason: invalid duration format")
		}
	}

	// Restart circuit breaker configuration
	if v := os.Getenv("BOSUN_RESTART_BREAKER"); v != "" {
		rcfg.RestartBreakerEnabled = parseBoolVal(v, true)
	}
	if v := os.Getenv("BOSUN_RESTART_THRESHOLD"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			if n <= 0 {
				log.Warn().Str("env", "BOSUN_RESTART_THRESHOLD").Str("value", v).Msg("Skipping env var. Reason: threshold must be positive")
			} else {
				rcfg.RestartThreshold = n
			}
		} else {
			log.Warn().Str("env", "BOSUN_RESTART_THRESHOLD").Str("value", v).Msg("Skipping env var. Reason: invalid integer")
		}
	}
	if v := os.Getenv("BOSUN_RESTART_WINDOW"); v != "" {
		if d, ok := parseDurationOrSeconds(v); ok {
			if d <= 0 {
				log.Warn().Str("env", "BOSUN_RESTART_WINDOW").Str("value", v).Msg("Skipping env var. Reason: duration must be positive")
			} else {
				rcfg.RestartWindow = d
			}
		} else {
			log.Warn().Str("env", "BOSUN_RESTART_WINDOW").Str("value", v).Msg("Skipping env var. Reason: invalid duration format")
		}
	}

	// Timeout overrides
	if v := os.Getenv("BOSUN_RECONCILE_TIMEOUT"); v != "" {
		if d, ok := parseDurationOrSeconds(v); ok {
			if d <= 0 {
				log.Warn().Str("env", "BOSUN_RECONCILE_TIMEOUT").Str("value", v).Msg("Skipping env var. Reason: duration must be positive, falling back to default")
			} else {
				cfg.ReconcileTimeout = d
			}
		} else {
			log.Warn().Str("env", "BOSUN_RECONCILE_TIMEOUT").Str("value", v).Msg("Skipping env var. Reason: invalid duration format")
		}
	}
	if v := os.Getenv("BOSUN_SHUTDOWN_TIMEOUT"); v != "" {
		if d, ok := parseDurationOrSeconds(v); ok {
			if d <= 0 {
				log.Warn().Str("env", "BOSUN_SHUTDOWN_TIMEOUT").Str("value", v).Msg("Skipping env var. Reason: duration must be positive, falling back to default")
			} else {
				cfg.ShutdownTimeout = d
			}
		} else {
			log.Warn().Str("env", "BOSUN_SHUTDOWN_TIMEOUT").Str("value", v).Msg("Skipping env var. Reason: invalid duration format")
		}
	}
	if v := os.Getenv("BOSUN_API_TIMEOUT"); v != "" {
		if d, ok := parseDurationOrSeconds(v); ok {
			if d <= 0 {
				log.Warn().Str("env", "BOSUN_API_TIMEOUT").Str("value", v).Msg("Skipping env var. Reason: duration must be positive, falling back to default")
			} else {
				cfg.APITimeout = d
			}
		} else {
			log.Warn().Str("env", "BOSUN_API_TIMEOUT").Str("value", v).Msg("Skipping env var. Reason: invalid duration format")
		}
	}

	// Drift check interval (0 disables periodic checks)
	if v := os.Getenv("BOSUN_DRIFT_INTERVAL"); v != "" {
		if d, ok := parseDurationOrSeconds(v); ok {
			cfg.DriftInterval = d
		} else {
			log.Warn().Str("env", "BOSUN_DRIFT_INTERVAL").Str("value", v).Msg("Skipping env var. Reason: invalid duration format")
		}
	}
	warnRestartBreakerSampling(*log.Logger(), cfg.DriftInterval, rcfg.RestartWindow)

	// Drift alert debounce (0 = disabled)
	if v := os.Getenv("BOSUN_DRIFT_ALERT_DEBOUNCE"); v != "" {
		if d, ok := parseDurationOrSeconds(v); ok {
			if d < 0 {
				log.Warn().Str("env", "BOSUN_DRIFT_ALERT_DEBOUNCE").Str("value", v).Msg("Skipping env var. Reason: duration must not be negative")
			} else {
				cfg.DriftAlertDebounce.SetFromEnv(d)
			}
		} else {
			log.Warn().Str("env", "BOSUN_DRIFT_ALERT_DEBOUNCE").Str("value", v).Msg("Skipping env var. Reason: invalid duration format")
		}
	}

	// Drift alert deduplication
	if v := os.Getenv("BOSUN_DRIFT_ALERT_COOLDOWN"); v != "" {
		if d, ok := parseDurationOrSeconds(v); ok {
			cfg.DriftAlertCooldown = d
		} else {
			log.Warn().Str("env", "BOSUN_DRIFT_ALERT_COOLDOWN").Str("value", v).Msg("Skipping env var. Reason: invalid duration format")
		}
	}
	if v := os.Getenv("BOSUN_DRIFT_RESOLVE_ALERTS"); v != "" {
		cfg.DriftResolveAlerts = parseBoolVal(v, true)
	}

	// Drift self-healing (default: false)
	if v := os.Getenv("BOSUN_DRIFT_SELF_HEAL"); v != "" {
		cfg.DriftSelfHeal.SetFromEnv(parseBoolVal(v, false))
	}
	if v := os.Getenv("BOSUN_DRIFT_SELF_HEAL_COOLDOWN"); v != "" {
		if d, ok := parseDurationOrSeconds(v); ok {
			if d <= 0 {
				log.Warn().Str("env", "BOSUN_DRIFT_SELF_HEAL_COOLDOWN").Str("value", v).Msg("Skipping env var. Reason: duration must be positive")
			} else {
				cfg.DriftSelfHealCooldown.SetFromEnv(d)
			}
		} else {
			log.Warn().Str("env", "BOSUN_DRIFT_SELF_HEAL_COOLDOWN").Str("value", v).Msg("Skipping env var. Reason: invalid duration format")
		}
	}
	if v := os.Getenv("BOSUN_DRIFT_SELF_HEAL_MAX_ATTEMPTS"); v != "" {
		attempts, err := strconv.Atoi(v)
		if err != nil || attempts <= 0 {
			log.Warn().Str("env", "BOSUN_DRIFT_SELF_HEAL_MAX_ATTEMPTS").Str("value", v).Msg("Skipping env var. Reason: value must be a positive integer")
		} else {
			cfg.MaxSelfHealAttempts = attempts
		}
	}

	// Content-hash file sync (default: true)
	if v := os.Getenv("BOSUN_CONTENT_HASH_SYNC"); v != "" {
		cfg.ContentHashSync = parseBoolVal(v, true)
	}
	rcfg.ContentHashSync = cfg.ContentHashSync

	// Orphan container cleanup (default: true)
	if v := os.Getenv("BOSUN_REMOVE_ORPHANS"); v != "" {
		cfg.RemoveOrphans = parseBoolVal(v, true)
		rcfg.RemoveOrphans.SetFromEnv(cfg.RemoveOrphans)
	} else {
		rcfg.RemoveOrphans = reconcile.NewConfigField(cfg.RemoveOrphans)
	}

	// Safety overrides use strict lowercase "true" match — intentional.
	// These gates guard against accidental activation of dangerous behavior;
	// "yes", "1", "TRUE" etc. are rejected by design.
	rcfg.AllowEmptyDeclaredState = os.Getenv("BOSUN_ALLOW_EMPTY_DECLARED_STATE") == "true"
	rcfg.SkipDeployInvariant = os.Getenv("BOSUN_SKIP_DEPLOY_INVARIANT") == "true"
	cfg.AllowUnauthenticatedWebhook = os.Getenv("BOSUN_ALLOW_UNAUTHENTICATED_WEBHOOK") == "true"

	// Post-sync hooks, settle delay, deploy paths, alert flags, drift debounce, remove_orphans, and targets: load from project config, env var overrides.
	if projectCfg, err := config.Load(); err == nil {
		config.ApplyInitialHookConfig(projectCfg, rcfg)
		rcfg.DeployPaths.SetFromFile(projectCfg.DeployPaths())
		rcfg.CriticalContainers.SetFromFile(projectCfg.CriticalContainers())
		rcfg.HealthGateScope = projectCfg.HealthGateScope()
		rcfg.TemplateIncludeDir = projectCfg.TemplateIncludeDir()

		// Load targets from project config; BOSUN_TARGETS env var (parsed above) takes precedence.
		// Skip if env explicitly set targets (even to empty — that's an intentional override).
		if !targetsFromEnv && len(rcfg.Targets) == 0 && len(projectCfg.Targets()) > 0 {
			rcfg.Targets = projectCfg.Targets()
		}

		alertCfg := projectCfg.GetAlertConfig()
		rcfg.OnFailure = alertCfg.OnFailure
		rcfg.OnSuccess = alertCfg.OnSuccess

		// Config file debounce value: env var takes precedence (already parsed above).
		if !cfg.DriftAlertDebounce.FromEnv() && projectCfg.DriftAlertDebounce() > 0 {
			cfg.DriftAlertDebounce.SetFromFile(projectCfg.DriftAlertDebounce())
		}

		// Drift self-heal from config file; env var takes precedence.
		if !cfg.DriftSelfHeal.FromEnv() {
			cfg.DriftSelfHeal.SetFromFile(projectCfg.DriftSelfHeal())
		}
		if !cfg.DriftSelfHealCooldown.FromEnv() && projectCfg.DriftSelfHealCooldown() > 0 {
			cfg.DriftSelfHealCooldown.SetFromFile(projectCfg.DriftSelfHealCooldown())
		}

		// Load remove_orphans from project config; env var (parsed above) takes precedence.
		if !rcfg.RemoveOrphans.FromEnv() {
			cfg.RemoveOrphans = projectCfg.RemoveOrphans()
			rcfg.RemoveOrphans.SetFromFile(cfg.RemoveOrphans)
		}
	} else if errors.Is(err, config.ErrInvalidPostSyncHooks) {
		// Preserve graceful degradation for missing or otherwise unreadable
		// config, but an invalid exec hook must fail daemon startup.
		cfg.projectConfigError = err
	}

	// Wire config reloader so the reconciler can re-read bosun.yaml from the repo.
	rcfg.ConfigReloader = config.LoadReloadedConfig
	if v := os.Getenv("BOSUN_POST_SYNC_HOOKS"); v != "" {
		var hooks []reconcile.PostSyncHook
		if err := json.Unmarshal([]byte(v), &hooks); err != nil {
			log.Warn().Err(err).Msg("Failed to parse BOSUN_POST_SYNC_HOOKS, ignoring")
		} else {
			rcfg.PostSyncHooks.SetFromEnv(hooks)
		}
	}
	if v := os.Getenv("BOSUN_HOOK_SETTLE_DELAY"); v != "" {
		if d, ok := parseDurationOrSeconds(v); ok {
			rcfg.HookSettleDelay.SetFromEnv(d)
		} else {
			log.Warn().Str("value", v).Msg("Failed to parse BOSUN_HOOK_SETTLE_DELAY, ignoring")
		}
	}
	if v := os.Getenv("BOSUN_DEPLOY_PATHS"); v != "" {
		var paths []string
		if err := json.Unmarshal([]byte(v), &paths); err != nil {
			log.Warn().Err(err).Msg("Failed to parse BOSUN_DEPLOY_PATHS, ignoring")
		} else {
			rcfg.DeployPaths.SetFromEnv(paths)
		}
	}
	if v := os.Getenv("BOSUN_DEPLOY_SYNC_PATHS"); v != "" {
		var paths []string
		if err := json.Unmarshal([]byte(v), &paths); err != nil {
			log.Warn().Err(err).Msg("Failed to parse BOSUN_DEPLOY_SYNC_PATHS, ignoring")
		} else {
			rcfg.DeploySyncPaths.SetFromEnv(paths)
		}
	}
	if v := os.Getenv("BOSUN_DEPLOY_SYNC_EXCLUDE"); v != "" {
		var paths []string
		if err := json.Unmarshal([]byte(v), &paths); err != nil {
			log.Warn().Err(err).Msg("Failed to parse BOSUN_DEPLOY_SYNC_EXCLUDE, ignoring")
		} else {
			rcfg.DeploySyncExclude.SetFromEnv(paths)
		}
	}
	if v := os.Getenv("BOSUN_CRITICAL_CONTAINERS"); v != "" {
		var containers []string
		if err := json.Unmarshal([]byte(v), &containers); err != nil {
			log.Warn().Err(err).Msg("Failed to parse BOSUN_CRITICAL_CONTAINERS, ignoring")
		} else {
			rcfg.CriticalContainers.SetFromEnv(containers)
		}
	}
	if v := os.Getenv("BOSUN_DRIFT_IGNORE"); v != "" {
		var rules []reconcile.DriftIgnoreRule
		if err := json.Unmarshal([]byte(v), &rules); err != nil {
			log.Warn().Err(err).Msg("Failed to parse BOSUN_DRIFT_IGNORE, ignoring")
		} else {
			rcfg.DriftIgnore.SetFromEnv(rules)
		}
	}
	if v := os.Getenv("BOSUN_HEALTH_GATE_TIMEOUT"); v != "" {
		if d, ok := parseDurationOrSeconds(v); ok {
			if d < 0 {
				log.Warn().Str("env", "BOSUN_HEALTH_GATE_TIMEOUT").Str("value", v).Msg("Skipping env var. Reason: duration must not be negative")
			} else {
				rcfg.HealthGateTimeout = d
			}
		} else {
			log.Warn().Str("value", v).Msg("Failed to parse BOSUN_HEALTH_GATE_TIMEOUT, ignoring")
		}
	}
	if v := os.Getenv("BOSUN_HEALTH_GATE_SCOPE"); v != "" {
		if scope, err := reconcile.ResolveHealthGateScope(v); err == nil {
			rcfg.HealthGateScope = scope
		} else {
			log.Warn().Err(err).Str("value", v).Msg("Invalid BOSUN_HEALTH_GATE_SCOPE, ignoring")
		}
	}
	if v := os.Getenv("BOSUN_TEMPLATE_INCLUDE_DIR"); v != "" {
		rcfg.TemplateIncludeDir = v
	}

	return cfg
}

// parseBoolVal parses a string as a boolean with consistent semantics:
//
//	"1", "true", "yes", "on"      -> true  (case-insensitive)
//	"0", "false", "no", "off"     -> false (case-insensitive)
//	anything else                 -> defaultVal
//
// This is the canonical boolean parser for all BOSUN_ env vars in the daemon.
// Using a single helper prevents the scattered patterns ("== true", "!= false && != 0")
// from diverging and ensures "no"/"yes" work everywhere (GH #263).
func parseBoolVal(v string, defaultVal bool) bool {
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return defaultVal
	}
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

func parseSocketAllowedUIDs(value string) ([]uint32, error) {
	parts := strings.Split(value, ",")
	allowed := make([]uint32, 0, len(parts))
	seen := make(map[uint32]struct{}, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("BOSUN_SOCKET_ALLOWED_UIDS contains an empty UID")
		}
		uid, err := strconv.ParseUint(part, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("BOSUN_SOCKET_ALLOWED_UIDS entry %q must be a numeric UID: %w", part, err)
		}
		numericUID := uint32(uid)
		if _, duplicate := seen[numericUID]; duplicate {
			continue
		}
		seen[numericUID] = struct{}{}
		allowed = append(allowed, numericUID)
	}
	return allowed, nil
}

// ValidateConfig validates the daemon configuration.
func ValidateConfig(cfg *Config) error {
	var errs []string
	if cfg.projectConfigError != nil {
		errs = append(errs, cfg.projectConfigError.Error())
	}

	if cfg.Port < 1 || cfg.Port > 65535 {
		errs = append(errs, fmt.Sprintf("invalid port: %d", cfg.Port))
	}
	if cfg.SocketMode&^os.ModePerm != 0 {
		errs = append(errs, fmt.Sprintf("invalid socket mode: %s (only permission bits are allowed)", cfg.SocketMode))
	}
	if cfg.socketAllowedUIDsError != nil {
		errs = append(errs, cfg.socketAllowedUIDsError.Error())
	}

	if cfg.ReconcileConfig != nil {
		// Secret identity validation runs before Git authentication so a bad Age
		// bind mount fails before any SSH-agent connection or API listener starts.
		if err := reconcile.ValidateAgeIdentityForSecrets(cfg.ReconcileConfig.SecretsFiles); err != nil {
			errs = append(errs, err.Error())
		} else if cfg.ReconcileConfig.RepoURL == "" {
			errs = append(errs, "REPO_URL or BOSUN_REPO_URL is required")
		} else if err := reconcile.ValidateGitAuthentication(cfg.ReconcileConfig.RepoURL); err != nil {
			errs = append(errs, reconcile.SanitizeGitError(err).Error())
		}

		// Unknown drift_ignore types and invalid globs fail startup the same
		// way a bad repo URL does -- such a rule silently never matches and
		// leaves drift unreported. A total-suppression rule ("*"/"*") only
		// warns here (unlike `bosun validate`, which errors on it) so an
		// intentional full mute is possible but never silent.
		if warnings, err := reconcile.ValidateDriftIgnoreRules(cfg.ReconcileConfig.DriftIgnore.Value); err != nil {
			errs = append(errs, err.Error())
		} else {
			for _, w := range warnings {
				log.Warn().Str("component", "drift_ignore").Msg(w)
			}
		}

		if err := reconcile.ValidatePostSyncHooks(cfg.ReconcileConfig.PostSyncHooks.Value); err != nil {
			errs = append(errs, err.Error())
		}
		for _, target := range cfg.ReconcileConfig.Targets {
			if err := reconcile.ValidatePostSyncHooks(target.PostSyncHooks); err != nil {
				errs = append(errs, fmt.Sprintf("target %q: %v", target.Name, err))
			}
		}

		// Structural target errors must fail before Run binds any API listener.
		// Keep this separate from hook validation so ValidateConfig continues to
		// aggregate every invalid hook instead of stopping at the first one.
		if err := cfg.ReconcileConfig.ValidateMultiTargetLayout(); err != nil {
			errs = append(errs, err.Error())
		}
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}

	return nil
}
