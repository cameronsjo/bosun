package daemon

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/cameronsjo/bosun/internal/log"
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

	// Widget endpoint for Homepage dashboard
	mux.HandleFunc("/api/widget", s.handleWidget)

	// Prometheus metrics endpoint
	mux.Handle("/metrics", s.metricsMiddleware(s.promHandler()))

	s.server = &http.Server{
		Handler:      s.loggingMiddleware(mux),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return s
}

// Start starts the HTTP server on the given port.
func (s *Server) Start(port int) error {
	s.server.Addr = fmt.Sprintf(":%d", port)
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
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status := s.daemon.HealthStatus()

	w.Header().Set("Content-Type", "application/json")
	if status.Status != "healthy" {
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	_ = json.NewEncoder(w).Encode(status)
}

// handleReady handles the readiness check endpoint.
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !s.daemon.IsReady() {
		http.Error(w, "Not ready", http.StatusServiceUnavailable)
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}

// handleWebhook handles generic webhook requests.
func (s *Server) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Validate webhook secret if configured
	if s.daemon.config.WebhookSecret != "" {
		sig := r.Header.Get("X-Signature")
		if sig == "" {
			sig = r.Header.Get("X-Hub-Signature-256")
		}

		// Limit body size to prevent DoS
		body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBodySize))
		if err != nil {
			http.Error(w, "Failed to read body", http.StatusBadRequest)
			return
		}

		if !s.validateSignature(body, sig) {
			// Log security event for failed signature validation
			secLogger := log.ComponentCtx(r.Context(), log.ComponentHTTP)
			secLogger.Warn().
				Str("remote_addr", r.RemoteAddr).
				Str("endpoint", r.URL.Path).
				Msg("Webhook signature validation failed")
			http.Error(w, "Invalid signature", http.StatusUnauthorized)
			return
		}
	}

	// Propagate enriched logger into background context (request ctx is cancelled after response).
	bgCtx := log.WithContext(context.Background(), log.Ctx(r.Context()))

	s.metrics.RecordWebhookTrigger("generic")

	// Trigger reconciliation with goroutine tracking
	webhookLogger := log.Component(log.ComponentWebhook)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ctx, cancel := context.WithTimeout(bgCtx, s.daemon.config.ReconcileTimeout)
		defer cancel()
		if err := s.daemon.TriggerReconcile(ctx, "webhook", false); err != nil {
			webhookLogger.Error().
				Err(err).
				Str(log.FieldSource, log.SourceWebhook).
				Msg("Webhook-triggered reconciliation failed")
		}
	}()

	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "accepted",
		"message": "Reconciliation triggered",
	})
}

// handleGitHubWebhook handles GitHub-specific webhook requests.
func (s *Server) handleGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read body with size limit to prevent DoS
	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBodySize))
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	// Validate GitHub signature
	if s.daemon.config.WebhookSecret != "" {
		sig := r.Header.Get("X-Hub-Signature-256")
		if !s.validateGitHubSignature(body, sig) {
			// Log security event for failed signature validation
			secLogger := log.ComponentCtx(r.Context(), log.ComponentHTTP)
			secLogger.Warn().
				Str("remote_addr", r.RemoteAddr).
				Str("endpoint", r.URL.Path).
				Str("event_type", r.Header.Get("X-GitHub-Event")).
				Msg("GitHub webhook signature validation failed")
			http.Error(w, "Invalid signature", http.StatusUnauthorized)
			return
		}
	}

	// Check event type
	eventType := r.Header.Get("X-GitHub-Event")
	action, reason := shouldProcessGitHubEvent(eventType)
	switch action {
	case "ping":
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("pong"))
		return
	case "ignore":
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
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	// Check if it's the branch we care about
	process, branchReason := shouldProcessGitHubPush(payload.Ref, s.daemon.config.ReconcileConfig.RepoBranch)
	if !process {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":  "ignored",
			"message": branchReason,
		})
		return
	}

	ghLogger := log.Component(log.ComponentWebhook)
	ghLogger.Info().
		Str(log.FieldSource, log.SourceGitHub).
		Str(log.FieldBranch, payload.Ref).
		Str("pusher", payload.Pusher.Name).
		Str(log.FieldCommit, payload.After).
		Msg("GitHub push received")

	s.metrics.RecordWebhookTrigger("github")

	// Propagate enriched logger into background context (request ctx is cancelled after response).
	bgCtx := log.WithContext(context.Background(), log.Ctx(r.Context()))

	// Trigger reconciliation with goroutine tracking
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ctx, cancel := context.WithTimeout(bgCtx, s.daemon.config.ReconcileTimeout)
		defer cancel()
		source := fmt.Sprintf("github:%s", payload.Pusher.Name)
		if err := s.daemon.TriggerReconcile(ctx, source, false); err != nil {
			ghLogger.Error().
				Err(err).
				Str(log.FieldSource, log.SourceGitHub).
				Msg("GitHub webhook reconciliation failed")
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

	// Validate signature if configured
	if s.daemon.config.WebhookSecret != "" {
		// Limit body size to prevent DoS
		body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBodySize))
		if err != nil {
			http.Error(w, "Failed to read body", http.StatusBadRequest)
			return
		}

		sig := r.Header.Get("X-Signature")
		if !s.validateSignature(body, sig) {
			// Log security event for failed signature validation
			secLogger := log.ComponentCtx(r.Context(), log.ComponentHTTP)
			secLogger.Warn().
				Str("remote_addr", r.RemoteAddr).
				Str("endpoint", r.URL.Path).
				Msg("Manual trigger signature validation failed")
			http.Error(w, "Invalid signature", http.StatusUnauthorized)
			return
		}
	}

	// Propagate enriched logger into background context (request ctx is cancelled after response).
	bgCtx := log.WithContext(context.Background(), log.Ctx(r.Context()))

	s.metrics.RecordWebhookTrigger("manual")

	// Trigger reconciliation with goroutine tracking
	manualLogger := log.Component(log.ComponentWebhook)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ctx, cancel := context.WithTimeout(bgCtx, s.daemon.config.ReconcileTimeout)
		defer cancel()
		if err := s.daemon.TriggerReconcile(ctx, "manual", false); err != nil {
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

// validateSignature validates a generic HMAC-SHA256 signature.
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

// validateGitHubSignature validates a GitHub webhook signature.
func (s *Server) validateGitHubSignature(body []byte, signature string) bool {
	if signature == "" {
		return false
	}

	// GitHub uses "sha256=<hex>" format
	signature = strings.TrimPrefix(signature, "sha256=")

	mac := hmac.New(sha256.New, []byte(s.daemon.config.WebhookSecret))
	mac.Write(body)
	expectedMAC := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(signature), []byte(expectedMAC))
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
