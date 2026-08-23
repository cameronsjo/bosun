// Package daemon provides a long-running daemon for GitOps operations.
package daemon

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/cameronsjo/bosun/internal/log"
	"github.com/cameronsjo/bosun/internal/reconcile"
	"github.com/cameronsjo/bosun/internal/ui"
)

// TCPServer handles TCP connections with bearer token authentication.
// This allows remote access to the daemon API for trusted clients.
type TCPServer struct {
	daemon      *Daemon
	addr        string
	bearerToken string
	listener    net.Listener
	httpServer  *http.Server
}

// NewTCPServer creates a new TCP server with bearer token auth.
func NewTCPServer(d *Daemon, addr, bearerToken string) (*TCPServer, error) {
	s := &TCPServer{
		daemon:      d,
		addr:        addr,
		bearerToken: bearerToken,
	}

	// Create HTTP handler with auth middleware
	mux := http.NewServeMux()
	mux.HandleFunc("/trigger", s.handleTrigger)
	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/health", s.handleHealth)
	// Note: /config endpoint is NOT exposed over TCP for security

	// Register WebUI API routes
	d.RegisterAPIRoutes(mux)

	s.httpServer = &http.Server{
		Handler:           s.authMiddleware(s.auditMiddleware(mux)),
		ReadHeaderTimeout: daemonReadHeaderTimeout,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		MaxHeaderBytes:    daemonMaxHeaderBytes,
	}

	return s, nil
}

// Start starts the TCP server.
func (s *TCPServer) Start() error {
	logger := log.Component(log.ComponentDaemon)
	logger.Debug().Str("addr", s.addr).Msg("Preparing to bind TCP socket")

	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		logger.Error().
			Err(err).
			Str("addr", s.addr).
			Msg("Failed to bind TCP socket")
		return err
	}
	s.listener = listener

	logger.Info().Str("addr", s.addr).Msg("Successfully bound TCP socket, accepting connections")
	ui.Info("TCP server listening on %s (bearer auth required)", s.addr)

	return s.httpServer.Serve(listener)
}

// Shutdown gracefully shuts down the TCP server.
func (s *TCPServer) Shutdown(ctx context.Context) error {
	if s.httpServer != nil {
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}

// authMiddleware validates bearer token authentication.
func (s *TCPServer) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Enrich request context with a request ID so downstream logs carry it.
		ctx := r.Context()
		if log.RequestIDFromContext(ctx) == "" {
			ctx = log.WithRequestID(ctx, "")
		}
		enriched := log.FromContext(ctx)
		ctx = log.WithContext(ctx, &enriched)
		r = r.WithContext(ctx)

		logger := log.ComponentCtx(r.Context(), log.ComponentDaemon)

		// Health endpoint is public for load balancer checks
		if r.URL.Path == "/health" {
			logger.Debug().
				Str(log.FieldMethod, r.Method).
				Str(log.FieldURL, r.URL.Path).
				Msg("Public health check, skipping authentication")
			next.ServeHTTP(w, r)
			return
		}

		logger.Debug().
			Str(log.FieldURL, r.URL.Path).
			Msg("Preparing to validate TCP bearer token")

		// Validate Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			logger.Warn().
				Str("remote_addr", r.RemoteAddr).
				Str(log.FieldURL, r.URL.Path).
				Msg("TCP request missing authorization header")
			w.Header().Set("WWW-Authenticate", `Bearer realm="bosun"`)
			http.Error(w, "Authorization required", http.StatusUnauthorized)
			return
		}

		// Parse bearer token
		const bearerPrefix = "Bearer "
		if !strings.HasPrefix(authHeader, bearerPrefix) {
			logger.Warn().
				Str("remote_addr", r.RemoteAddr).
				Str(log.FieldURL, r.URL.Path).
				Msg("TCP request with invalid authorization format")
			http.Error(w, "Invalid authorization format", http.StatusUnauthorized)
			return
		}
		token := authHeader[len(bearerPrefix):]

		// Constant-time comparison to prevent timing attacks
		if subtle.ConstantTimeCompare([]byte(token), []byte(s.bearerToken)) != 1 {
			logger.Warn().
				Str("remote_addr", r.RemoteAddr).
				Str(log.FieldMethod, r.Method).
				Str(log.FieldURL, r.URL.Path).
				Msg("TCP authentication failed, invalid token")
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}

		logger.Debug().
			Str(log.FieldURL, r.URL.Path).
			Msg("TCP bearer token authentication passed")

		next.ServeHTTP(w, r)
	})
}

// auditMiddleware logs all TCP requests with structured fields.
func (s *TCPServer) auditMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(wrapped, r)

		logger := log.ComponentCtx(r.Context(), log.ComponentDaemon)
		logger.Info().
			Str(log.FieldOperation, "audit").
			Str(log.FieldMethod, r.Method).
			Str(log.FieldURL, r.URL.Path).
			Str("remote_addr", r.RemoteAddr).
			Int(log.FieldStatus, wrapped.statusCode).
			Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
			Msg("TCP request handled")
	})
}

// handleTrigger handles POST /trigger requests.
func (s *TCPServer) handleTrigger(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse request under a size cap.
	req, ok := decodeTriggerRequest(w, r)
	if !ok {
		return
	}

	// Default source with TCP identifier
	source := req.Source
	if source == "" {
		source = "tcp"
	}
	source = source + " (tcp:" + r.RemoteAddr + ")"

	// Trigger reconcile under the daemon lifecycle rather than the request
	// context, which ends as soon as this 202 response is written.
	if !s.daemon.startReconcileGoroutine(r.Context(), func(ctx context.Context) {
		if err := s.daemon.runTriggerReconcile(ctx, source, req.Force); err != nil {
			ui.Error("TCP-triggered reconciliation failed: %v", err)
		}
	}) {
		http.Error(w, "Daemon is shutting down", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(TriggerResponse{
		Status:  "accepted",
		Message: "Reconciliation triggered",
	})
}

// handleStatus handles GET /status requests.
func (s *TCPServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	lastReconcile, lastErr := s.daemon.LastReconcile()

	s.daemon.reconcileMu.Lock()
	reconciling := s.daemon.reconciling
	s.daemon.reconcileMu.Unlock()

	resp := StatusResponse{
		State:  reconcileStateString(reconciling),
		Uptime: time.Since(startTime).Round(time.Second).String(),
	}

	if !lastReconcile.IsZero() {
		resp.LastReconcile = &lastReconcile
	}

	if lastErr != nil {
		resp.LastError = reconcile.SanitizeGitText(lastErr.Error())
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// handleHealth handles GET /health requests.
func (s *TCPServer) handleHealth(w http.ResponseWriter, r *http.Request) {
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
