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
	"time"

	"github.com/cameronsjo/bosun/internal/ui"
)

// Server handles HTTP requests for webhooks and health checks.
type Server struct {
	daemon *Daemon
	server *http.Server
}

// NewServer creates a new HTTP server for the daemon.
func NewServer(d *Daemon) *Server {
	s := &Server{daemon: d}

	mux := http.NewServeMux()

	// Health endpoints
	mux.HandleFunc(d.config.HealthPath, s.handleHealth)
	mux.HandleFunc(d.config.ReadyPath, s.handleReady)

	// Webhook endpoints
	mux.HandleFunc(d.config.WebhookPath, s.handleWebhook)
	mux.HandleFunc(d.config.WebhookPath+"/github", s.handleGitHubWebhook)
	mux.HandleFunc(d.config.WebhookPath+"/manual", s.handleManualTrigger)

	// Metrics (placeholder for future)
	mux.HandleFunc("/metrics", s.handleMetrics)

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
func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

// loggingMiddleware logs HTTP requests.
func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(wrapped, r)
		ui.Info("HTTP %s %s %d %s", r.Method, r.URL.Path, wrapped.statusCode, time.Since(start))
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

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read body", http.StatusBadRequest)
			return
		}

		if !s.validateSignature(body, sig) {
			http.Error(w, "Invalid signature", http.StatusUnauthorized)
			return
		}
	}

	// Trigger reconciliation
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if err := s.daemon.TriggerReconcile(ctx, "webhook"); err != nil {
			ui.Error("Webhook-triggered reconciliation failed: %v", err)
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

	// Read body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	// Validate GitHub signature
	if s.daemon.config.WebhookSecret != "" {
		sig := r.Header.Get("X-Hub-Signature-256")
		if !s.validateGitHubSignature(body, sig) {
			http.Error(w, "Invalid signature", http.StatusUnauthorized)
			return
		}
	}

	// Check event type
	eventType := r.Header.Get("X-GitHub-Event")
	if eventType == "ping" {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("pong"))
		return
	}

	if eventType != "push" {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":  "ignored",
			"message": fmt.Sprintf("Event type '%s' not handled", eventType),
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
	expectedRef := "refs/heads/" + s.daemon.config.ReconcileConfig.RepoBranch
	if payload.Ref != expectedRef {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":  "ignored",
			"message": fmt.Sprintf("Push to %s ignored (tracking %s)", payload.Ref, expectedRef),
		})
		return
	}

	ui.Info("GitHub push to %s by %s: %s", payload.Ref, payload.Pusher.Name, payload.HeadCommit.Message)

	// Trigger reconciliation
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		source := fmt.Sprintf("github:%s", payload.Pusher.Name)
		if err := s.daemon.TriggerReconcile(ctx, source); err != nil {
			ui.Error("GitHub webhook reconciliation failed: %v", err)
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
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read body", http.StatusBadRequest)
			return
		}

		sig := r.Header.Get("X-Signature")
		if !s.validateSignature(body, sig) {
			http.Error(w, "Invalid signature", http.StatusUnauthorized)
			return
		}
	}

	// Trigger reconciliation
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if err := s.daemon.TriggerReconcile(ctx, "manual"); err != nil {
			ui.Error("Manual trigger reconciliation failed: %v", err)
		}
	}()

	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "accepted",
		"message": "Manual reconciliation triggered",
	})
}

// handleMetrics serves Prometheus metrics (placeholder).
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status := s.daemon.HealthStatus()

	// Simple Prometheus-style metrics
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprintf(w, "# HELP bosun_ready Whether the daemon is ready\n")
	fmt.Fprintf(w, "# TYPE bosun_ready gauge\n")
	if status.Ready {
		fmt.Fprintf(w, "bosun_ready 1\n")
	} else {
		fmt.Fprintf(w, "bosun_ready 0\n")
	}

	fmt.Fprintf(w, "# HELP bosun_uptime_seconds Daemon uptime in seconds\n")
	fmt.Fprintf(w, "# TYPE bosun_uptime_seconds counter\n")
	fmt.Fprintf(w, "bosun_uptime_seconds %f\n", status.Uptime.Seconds())

	if !status.LastReconcile.IsZero() {
		fmt.Fprintf(w, "# HELP bosun_last_reconcile_timestamp Unix timestamp of last reconciliation\n")
		fmt.Fprintf(w, "# TYPE bosun_last_reconcile_timestamp gauge\n")
		fmt.Fprintf(w, "bosun_last_reconcile_timestamp %d\n", status.LastReconcile.Unix())
	}

	if status.LastError != "" {
		fmt.Fprintf(w, "# HELP bosun_reconcile_errors_total Total reconciliation errors\n")
		fmt.Fprintf(w, "# TYPE bosun_reconcile_errors_total counter\n")
		fmt.Fprintf(w, "bosun_reconcile_errors_total 1\n")
	}
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
