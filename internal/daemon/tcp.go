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

	s.httpServer = &http.Server{
		Handler:      s.authMiddleware(s.auditMiddleware(mux)),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	return s, nil
}

// Start starts the TCP server.
func (s *TCPServer) Start() error {
	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	s.listener = listener

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
		// Health endpoint is public for load balancer checks
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}

		// Validate Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			w.Header().Set("WWW-Authenticate", `Bearer realm="bosun"`)
			http.Error(w, "Authorization required", http.StatusUnauthorized)
			return
		}

		// Parse bearer token
		const bearerPrefix = "Bearer "
		if !strings.HasPrefix(authHeader, bearerPrefix) {
			http.Error(w, "Invalid authorization format", http.StatusUnauthorized)
			return
		}
		token := authHeader[len(bearerPrefix):]

		// Constant-time comparison to prevent timing attacks
		if subtle.ConstantTimeCompare([]byte(token), []byte(s.bearerToken)) != 1 {
			ui.Warning("TCP auth failed from %s", r.RemoteAddr)
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// auditMiddleware logs all requests.
func (s *TCPServer) auditMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(wrapped, r)

		ui.Info("TCP AUDIT: %s %s from %s -> %d (%s)",
			r.Method, r.URL.Path, r.RemoteAddr, wrapped.statusCode, time.Since(start))
	})
}

// handleTrigger handles POST /trigger requests.
func (s *TCPServer) handleTrigger(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse request
	var req TriggerRequest
	if r.Body != nil && r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}
	}

	// Default source with TCP identifier
	source := req.Source
	if source == "" {
		source = "tcp"
	}
	source = source + " (tcp:" + r.RemoteAddr + ")"

	// Trigger reconcile
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if err := s.daemon.TriggerReconcile(ctx, source); err != nil {
			ui.Error("TCP-triggered reconciliation failed: %v", err)
		}
	}()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(TriggerResponse{
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

	state := "idle"
	if reconciling {
		state = "reconciling"
	}

	resp := StatusResponse{
		State:  state,
		Uptime: time.Since(startTime).Round(time.Second).String(),
	}

	if !lastReconcile.IsZero() {
		resp.LastReconcile = &lastReconcile
	}

	if lastErr != nil {
		resp.LastError = lastErr.Error()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
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

	json.NewEncoder(w).Encode(status)
}
