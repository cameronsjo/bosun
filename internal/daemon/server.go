package daemon

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/cameronsjo/bosun/internal/log"
	sentrypkg "github.com/cameronsjo/bosun/internal/sentry"
	"github.com/cameronsjo/bosun/internal/telemetry"
	"github.com/cameronsjo/bosun/internal/ui"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	// maxWebhookBodySize is the maximum allowed size for webhook request bodies (1MB).
	// This prevents denial-of-service attacks via oversized payloads.
	maxWebhookBodySize = 1 * 1024 * 1024
)

// Server handles HTTP requests for webhooks and health checks.
type Server struct {
	daemon   *Daemon
	server   *http.Server
	registry *prometheus.Registry
	metrics  *Metrics

	// Track in-flight reconciliation goroutines for graceful shutdown
	wg sync.WaitGroup
}

// NewServer creates a new HTTP server for the daemon.
func NewServer(d *Daemon) *Server {
	reg := prometheus.NewRegistry()
	metrics := newMetrics(reg)

	s := &Server{
		daemon:   d,
		registry: reg,
		metrics:  metrics,
	}

	mux := http.NewServeMux()

	// Health endpoints
	mux.HandleFunc(d.config.HealthPath, s.handleHealth)
	mux.HandleFunc(d.config.ReadyPath, s.handleReady)

	// Webhook endpoints
	mux.HandleFunc(d.config.WebhookPath, s.handleWebhook)
	mux.HandleFunc(d.config.WebhookPath+"/github", s.handleGitHubWebhook)
	mux.HandleFunc(d.config.WebhookPath+"/manual", s.handleManualTrigger)

	// Widget endpoint for Homepage dashboard (read-scope auth, fail-closed #296).
	mux.Handle("/api/widget", s.requireMetricsAuth(http.HandlerFunc(s.handleWidget)))

	// Prometheus metrics endpoint (read-scope auth, fail-closed #296).
	mux.Handle("/metrics", s.requireMetricsAuth(s.metricsMiddleware(s.promHandler())))

	s.server = &http.Server{
		Handler:      s.loggingMiddleware(mux),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return s
}

// listenAddr composes the HTTP server bind address. An empty host binds all
// interfaces — the default MUST stay all-interfaces because container-side
// callers (Traefik, Prometheus, Homepage) reach bosun over the docker bridge,
// not loopback. Operators narrow the bind with BOSUN_LISTEN_ADDR.
func listenAddr(host string, port int) string {
	return net.JoinHostPort(host, strconv.Itoa(port))
}

// Start starts the HTTP server on the given port, bound to the configured
// listen address (all interfaces when unset).
func (s *Server) Start(port int) error {
	s.server.Addr = listenAddr(s.daemon.config.ListenAddr, port)
	ui.Info("HTTP server listening on %s", s.server.Addr)
	return s.server.ListenAndServe()
}

// Shutdown gracefully shuts down the HTTP server.
// Waits for in-flight reconciliation goroutines to complete.
func (s *Server) Shutdown(ctx context.Context) error {
	// Stop accepting new connections
	err := s.server.Shutdown(ctx)

	// Wait for in-flight reconciliation goroutines
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All goroutines completed
	case <-ctx.Done():
		log.Warn().Msg("Shutdown timeout waiting for in-flight reconciliations")
	}

	return err
}

// loggingMiddleware logs HTTP requests with request ID tracking.
func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Generate or extract request ID.
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			var id string
			r = r.WithContext(log.WithRequestID(r.Context(), ""))
			id = log.RequestIDFromContext(r.Context())
			requestID = id
		} else {
			r = r.WithContext(log.WithRequestID(r.Context(), requestID))
		}

		// Stash enriched logger on context for downstream handlers.
		enriched := log.FromContext(r.Context())
		r = r.WithContext(log.WithContext(r.Context(), &enriched))

		// Add request ID to response headers.
		w.Header().Set("X-Request-ID", requestID)

		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(wrapped, r)

		// Log with enriched logger that already carries request_id.
		enriched.Info().
			Str(log.FieldComponent, log.ComponentHTTP).
			Str(log.FieldMethod, r.Method).
			Str(log.FieldURL, r.URL.Path).
			Int(log.FieldStatus, wrapped.statusCode).
			Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
			Msg("HTTP request completed")
	})
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// handleHealth handles the health check endpoint.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	logger := log.ComponentCtx(r.Context(), log.ComponentHTTP)
	if r.Method != http.MethodGet {
		logger.Warn().
			Str(log.FieldMethod, r.Method).
			Msg("Health check with invalid method rejected")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	logger.Debug().Msg("Processing health check request")
	status := s.daemon.HealthStatus()

	w.Header().Set("Content-Type", "application/json")
	if status.Status != "healthy" {
		w.WriteHeader(http.StatusServiceUnavailable)
		logger.Info().
			Str("daemon_status", status.Status).
			Int(log.FieldStatus, http.StatusServiceUnavailable).
			Msg("Health check returned degraded status")
	} else {
		logger.Debug().
			Str("daemon_status", status.Status).
			Msg("Health check passed")
	}

	_ = json.NewEncoder(w).Encode(status)
}

// handleReady handles the readiness check endpoint.
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	logger := log.ComponentCtx(r.Context(), log.ComponentHTTP)
	if r.Method != http.MethodGet {
		logger.Warn().
			Str(log.FieldMethod, r.Method).
			Msg("Readiness check with invalid method rejected")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !s.daemon.IsReady() {
		logger.Debug().Msg("Readiness check: daemon not yet ready")
		http.Error(w, "Not ready", http.StatusServiceUnavailable)
		return
	}

	logger.Debug().Msg("Readiness check passed")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}

// handleWebhook handles generic webhook requests.
func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	logger := log.ComponentCtx(r.Context(), log.ComponentWebhook)
	if r.Method != http.MethodPost {
		logger.Warn().
			Str(log.FieldMethod, r.Method).
			Msg("Webhook request with invalid method rejected")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	logger.Debug().Msg("Preparing to process generic webhook request")

	// Limit body size to prevent DoS
	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBodySize))
	if err != nil {
		logger.Warn().
			Err(err).
			Msg("Failed to read webhook body")
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	if !s.authorizeTrigger(w, r, body, schemeGeneric) {
		return
	}

	logger.Info().
		Str(log.FieldSource, log.SourceWebhook).
		Msg("Generic webhook received")

	// Propagate enriched logger into background context (request ctx is cancelled after response).
	bgCtx := log.WithContext(context.Background(), log.Ctx(r.Context()))

	s.metrics.RecordWebhookTrigger("generic")

	// Trigger reconciliation with goroutine tracking.
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer sentrypkg.Recover()
		ctx, cancel := context.WithTimeout(bgCtx, s.daemon.config.ReconcileTimeout)
		defer cancel()
		ctx, webhookSpan := telemetry.Tracer("daemon").Start(ctx, "daemon.webhook",
			trace.WithAttributes(telemetry.StringAttr("webhook_type", "generic")),
		)
		bgLogger := log.ComponentCtx(ctx, log.ComponentWebhook)
		err := s.daemon.TriggerReconcile(ctx, log.SourceWebhook, false)
		if err != nil {
			telemetry.SpanError(webhookSpan, err)
			bgLogger.Error().
				Err(err).
				Str(log.FieldSource, log.SourceWebhook).
				Msg("Failed to trigger reconciliation from webhook")
		} else {
			telemetry.SpanOK(webhookSpan)
			bgLogger.Debug().
				Str(log.FieldSource, log.SourceWebhook).
				Msg("Webhook reconciliation triggered successfully")
		}
		webhookSpan.End()
	}()

	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "accepted",
		"message": "Reconciliation triggered",
	})
}

// handleGitHubWebhook handles GitHub-specific webhook requests.
func (s *Server) handleGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	logger := log.ComponentCtx(r.Context(), log.ComponentWebhook)
	if r.Method != http.MethodPost {
		logger.Warn().
			Str(log.FieldMethod, r.Method).
			Msg("GitHub webhook request with invalid method rejected")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	logger.Debug().Msg("Preparing to process GitHub webhook")

	// Read body with size limit to prevent DoS
	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBodySize))
	if err != nil {
		logger.Warn().
			Err(err).
			Msg("Failed to read GitHub webhook body")
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	// Authorize before any event dispatch — even ping/ignored events reveal
	// endpoint liveness, so fail-closed covers them too.
	eventType := r.Header.Get("X-GitHub-Event")
	if !s.authorizeTrigger(w, r, body, schemeGitHub) {
		return
	}

	// Check event type
	logger.Debug().
		Str("event_type", eventType).
		Msg("Processing GitHub event type")
	action, reason := shouldProcessGitHubEvent(eventType)
	switch action {
	case "ping":
		logger.Info().
			Str(log.FieldSource, log.SourceGitHub).
			Str("event_type", eventType).
			Msg("GitHub ping received")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("pong"))
		return
	case "ignore":
		logger.Debug().
			Str("event_type", eventType).
			Str("reason", reason).
			Msg("GitHub event ignored")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":  "ignored",
			"message": reason,
		})
		return
	}

	// Parse push event
	var payload GitHubPushPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		logger.Warn().
			Err(err).
			Msg("Failed to parse GitHub webhook JSON payload")
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	// Check if it's the branch we care about
	process, branchReason := shouldProcessGitHubPush(payload.Ref, s.daemon.config.ReconcileConfig.RepoBranch)
	if !process {
		logger.Debug().
			Str(log.FieldBranch, payload.Ref).
			Str("reason", branchReason).
			Msg("GitHub push on non-tracked branch ignored")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":  "ignored",
			"message": branchReason,
		})
		return
	}

	logger.Info().
		Str(log.FieldSource, log.SourceGitHub).
		Str(log.FieldBranch, payload.Ref).
		Str("pusher", payload.Pusher.Name).
		Str(log.FieldCommit, payload.After).
		Msg("GitHub push received on tracked branch")

	s.metrics.RecordWebhookTrigger("github")

	// Propagate enriched logger into background context (request ctx is cancelled after response).
	bgCtx := log.WithContext(context.Background(), log.Ctx(r.Context()))

	// Trigger reconciliation with goroutine tracking
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer sentrypkg.Recover()
		ctx, cancel := context.WithTimeout(bgCtx, s.daemon.config.ReconcileTimeout)
		defer cancel()
		source := fmt.Sprintf("github:%s", payload.Pusher.Name)
		bgLogger := log.ComponentCtx(ctx, log.ComponentWebhook)
		if err := s.daemon.TriggerReconcile(ctx, source, false); err != nil {
			bgLogger.Error().
				Err(err).
				Str(log.FieldSource, log.SourceGitHub).
				Msg("Failed to trigger reconciliation from GitHub webhook")
		} else {
			bgLogger.Debug().
				Str(log.FieldSource, log.SourceGitHub).
				Msg("GitHub webhook reconciliation triggered successfully")
		}
	}()

	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "accepted",
		"message": "Reconciliation triggered",
		"commit":  payload.After,
	})
}

// handleManualTrigger handles manual reconciliation triggers.
func (s *Server) handleManualTrigger(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req TriggerRequest

	// Limit body size to prevent DoS; bytes needed for both HMAC and JSON decode.
	var body []byte
	if r.Body != nil {
		var err error
		body, err = io.ReadAll(io.LimitReader(r.Body, maxWebhookBodySize))
		if err != nil {
			http.Error(w, "Failed to read body", http.StatusBadRequest)
			return
		}
	}

	if !s.authorizeTrigger(w, r, body, schemeManual) {
		return
	}

	// Decode optional JSON body from the already-read bytes.
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			parseLogger := log.ComponentCtx(r.Context(), log.ComponentWebhook)
			parseLogger.Debug().
				Err(err).
				Str(log.FieldSource, log.SourceManual).
				Msg("Failed to parse trigger request body, proceeding without force flag")
		}
	}

	// Log the incoming trigger, distinguishing force from normal.
	triggerLogger := log.ComponentCtx(r.Context(), log.ComponentWebhook)
	if req.Force {
		triggerLogger.Info().
			Str(log.FieldSource, log.SourceManual).
			Bool("force", true).
			Msg("Force trigger request received via HTTP")
	} else {
		triggerLogger.Info().
			Str(log.FieldSource, log.SourceManual).
			Msg("Manual trigger request received via HTTP")
	}

	// Propagate enriched logger into background context (request ctx is cancelled after response).
	bgCtx := log.WithContext(context.Background(), log.Ctx(r.Context()))

	s.metrics.RecordWebhookTrigger("manual")

	// Trigger reconciliation with goroutine tracking
	manualLogger := log.Component(log.ComponentWebhook)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer sentrypkg.Recover()
		ctx, cancel := context.WithTimeout(bgCtx, s.daemon.config.ReconcileTimeout)
		defer cancel()
		if err := s.daemon.TriggerReconcile(ctx, "manual", req.Force); err != nil {
			manualLogger.Error().
				Err(err).
				Str(log.FieldSource, log.SourceManual).
				Msg("Manual trigger reconciliation failed")
		}
	}()

	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "accepted",
		"message": "Manual reconciliation triggered",
	})
}

// handleWidget returns lightweight stats for Homepage's customapi widget.
func (s *Server) handleWidget(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.daemon.WidgetData())
}

// promHandler returns the Prometheus HTTP handler for the server's registry.
func (s *Server) promHandler() http.Handler {
	return promhttp.HandlerFor(s.registry, promhttp.HandlerOpts{})
}

// metricsMiddleware refreshes point-in-time gauges (uptime, ready, last reconcile)
// before delegating to the Prometheus HTTP handler.
func (s *Server) metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		status := s.daemon.HealthStatus()
		s.metrics.SetReady(status.Ready)
		s.metrics.SetUptime(status.Uptime.Seconds())

		if !status.LastReconcile.IsZero() {
			s.metrics.SetLastReconcileTime(float64(status.LastReconcile.Unix()))
		}

		next.ServeHTTP(w, r)
	})
}

// requireMetricsAuth wraps a handler so /metrics and /api/widget enforce
// read-scope authentication before serving. On rejection authorizeMetrics has
// already written the response.
func (s *Server) requireMetricsAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.authorizeMetrics(w, r) {
			return
		}
		next.ServeHTTP(w, r)
	})
}

// authorizeMetrics enforces read-scope auth for /metrics and /api/widget (#296).
// On failure it writes the 401/403 response itself and returns false — the
// caller MUST NOT write a second response. On success it returns true.
//
// Fail-closed: with no credential configured every request is rejected 403
// unless the operator explicitly opted out via
// BOSUN_ALLOW_UNAUTHENTICATED_METRICS=true, in which case each accepted request
// logs a security warning. When a credential IS configured, a Bearer token
// matching the read-scope MetricsToken or the strictly-more-privileged control
// BearerToken is required; anything else is 401.
func (s *Server) authorizeMetrics(w http.ResponseWriter, r *http.Request) bool {
	logger := log.ComponentCtx(r.Context(), log.ComponentHTTP)

	if !s.daemon.metricsAuthConfigured() {
		if s.daemon.config.AllowUnauthenticatedMetrics {
			logger.Warn().
				Str("remote_addr", r.RemoteAddr).
				Str("endpoint", r.URL.Path).
				Msg("SECURITY: accepting unauthenticated metrics request. Reason: no BOSUN_METRICS_TOKEN configured and BOSUN_ALLOW_UNAUTHENTICATED_METRICS=true")
			return true
		}
		logger.Warn().
			Str("remote_addr", r.RemoteAddr).
			Str("endpoint", r.URL.Path).
			Msg("Rejecting metrics request, expected a configured read token but none is set. Set BOSUN_METRICS_TOKEN, or set BOSUN_ALLOW_UNAUTHENTICATED_METRICS=true to serve unauthenticated")
		http.Error(w, "Metrics authentication not configured", http.StatusForbidden)
		return false
	}

	const bearerPrefix = "Bearer "
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, bearerPrefix) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return false
	}
	if !s.metricsTokenValid(authHeader[len(bearerPrefix):]) {
		logger.Warn().
			Str("remote_addr", r.RemoteAddr).
			Str("endpoint", r.URL.Path).
			Msg("Rejecting metrics request. Reason: invalid read token")
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}

// metricsTokenValid reports whether token matches the read-scope MetricsToken or
// the control BearerToken, using constant-time comparison. An empty configured
// value never matches, so a blank token cannot admit.
func (s *Server) metricsTokenValid(token string) bool {
	if t := s.daemon.config.MetricsToken; t != "" && subtle.ConstantTimeCompare([]byte(token), []byte(t)) == 1 {
		return true
	}
	if t := s.daemon.config.BearerToken; t != "" && subtle.ConstantTimeCompare([]byte(token), []byte(t)) == 1 {
		return true
	}
	return false
}

// triggerScheme identifies which signature header a trigger endpoint expects.
type triggerScheme int

const (
	// schemeGeneric accepts X-Signature or X-Hub-Signature-256 (the generic
	// /webhook endpoint serves forwarders that use either convention).
	schemeGeneric triggerScheme = iota
	// schemeGitHub accepts only X-Hub-Signature-256 (GitHub's convention).
	schemeGitHub
	// schemeManual accepts only X-Signature (bosun's own trigger convention).
	schemeManual
)

// authorizeTrigger enforces webhook authentication for a trigger request.
// On failure it writes the 401/403 response itself and returns false — the
// caller MUST NOT write a second response (`if !s.authorizeTrigger(...) { return }`).
// On success it writes nothing and returns true.
//
// With no webhook secret configured the endpoints fail closed (#345): every
// trigger request is rejected with 403 unless the operator explicitly opted
// out via BOSUN_ALLOW_UNAUTHENTICATED_WEBHOOK=true, in which case each
// accepted unauthenticated request logs a security warning.
func (s *Server) authorizeTrigger(w http.ResponseWriter, r *http.Request, body []byte, scheme triggerScheme) bool {
	logger := log.ComponentCtx(r.Context(), log.ComponentWebhook)

	if s.daemon.config.WebhookSecret == "" {
		if s.daemon.config.AllowUnauthenticatedWebhook {
			logger.Warn().
				Str("remote_addr", r.RemoteAddr).
				Str("endpoint", r.URL.Path).
				Msg("SECURITY: accepting unauthenticated trigger request. Reason: no webhook secret configured and BOSUN_ALLOW_UNAUTHENTICATED_WEBHOOK=true")
			return true
		}
		logger.Warn().
			Str("remote_addr", r.RemoteAddr).
			Str("endpoint", r.URL.Path).
			Msg("Rejecting trigger request, expected a configured webhook secret but none is set. Set WEBHOOK_SECRET, or set BOSUN_ALLOW_UNAUTHENTICATED_WEBHOOK=true to accept unauthenticated triggers")
		http.Error(w, "Webhook authentication not configured", http.StatusForbidden)
		return false
	}

	var sig string
	switch scheme {
	case schemeGitHub:
		sig = r.Header.Get("X-Hub-Signature-256")
	case schemeManual:
		sig = r.Header.Get("X-Signature")
	case schemeGeneric:
		sig = r.Header.Get("X-Signature")
		if sig == "" {
			sig = r.Header.Get("X-Hub-Signature-256")
		}
	default:
		// Fail closed on an unknown scheme: a future variant must pick its
		// header policy explicitly, never inherit generic's broader one.
		logger.Error().
			Int("scheme", int(scheme)).
			Str("endpoint", r.URL.Path).
			Msg("Rejecting trigger request, expected a known trigger scheme but got an unhandled variant")
		http.Error(w, "Unsupported trigger authentication scheme", http.StatusInternalServerError)
		return false
	}

	if !s.validateSignature(body, sig) {
		logger.Warn().
			Str("remote_addr", r.RemoteAddr).
			Str("endpoint", r.URL.Path).
			Msg("Rejecting trigger request. Reason: HMAC signature validation failed")
		http.Error(w, "Invalid signature", http.StatusUnauthorized)
		return false
	}

	logger.Debug().
		Str("endpoint", r.URL.Path).
		Msg("Trigger request signature validated")
	return true
}

// validateSignature validates an HMAC-SHA256 signature (with or without the
// "sha256=" prefix — GitHub and generic forwarders both use this format).
func (s *Server) validateSignature(body []byte, signature string) bool {
	if signature == "" {
		return false
	}

	// Remove "sha256=" prefix if present
	signature = strings.TrimPrefix(signature, "sha256=")

	expected := hmac.New(sha256.New, []byte(s.daemon.config.WebhookSecret))
	expected.Write(body)
	expectedSig := hex.EncodeToString(expected.Sum(nil))

	return hmac.Equal([]byte(signature), []byte(expectedSig))
}

// GitHubPushPayload represents a GitHub push webhook payload.
type GitHubPushPayload struct {
	Ref    string `json:"ref"`
	Before string `json:"before"`
	After  string `json:"after"`
	Pusher struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"pusher"`
	HeadCommit struct {
		ID      string `json:"id"`
		Message string `json:"message"`
		Author  struct {
			Name  string `json:"name"`
			Email string `json:"email"`
		} `json:"author"`
	} `json:"head_commit"`
	Repository struct {
		FullName string `json:"full_name"`
		CloneURL string `json:"clone_url"`
		SSHURL   string `json:"ssh_url"`
	} `json:"repository"`
}
