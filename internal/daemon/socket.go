// Package daemon provides a long-running daemon for GitOps operations.
package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/cameronsjo/bosun/internal/ui"
)

// SocketServer handles Unix socket connections for the trigger API.
type SocketServer struct {
	daemon     *Daemon
	socketPath string
	listener   net.Listener
	httpServer *http.Server
}

// SocketConfig holds socket server configuration.
type SocketConfig struct {
	SocketPath string // Path to Unix socket (e.g., /var/run/bosun.sock)
	SocketMode os.FileMode // Socket file permissions (default: 0660)
}

// DefaultSocketConfig returns default socket configuration.
func DefaultSocketConfig() *SocketConfig {
	return &SocketConfig{
		SocketPath: "/var/run/bosun.sock",
		SocketMode: 0660,
	}
}

// NewSocketServer creates a new Unix socket server.
func NewSocketServer(d *Daemon, cfg *SocketConfig) (*SocketServer, error) {
	if cfg == nil {
		cfg = DefaultSocketConfig()
	}

	s := &SocketServer{
		daemon:     d,
		socketPath: cfg.SocketPath,
	}

	// Create HTTP handler for socket
	mux := http.NewServeMux()
	mux.HandleFunc("/trigger", s.handleTrigger)
	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/config", s.handleConfig)

	s.httpServer = &http.Server{
		Handler:      s.auditMiddleware(mux),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	return s, nil
}

// Start starts the Unix socket server.
func (s *SocketServer) Start() error {
	// Ensure socket directory exists
	socketDir := filepath.Dir(s.socketPath)
	if err := os.MkdirAll(socketDir, 0755); err != nil {
		return fmt.Errorf("failed to create socket directory: %w", err)
	}

	// Remove stale socket if it exists
	if err := os.Remove(s.socketPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove stale socket: %w", err)
	}

	// Create Unix socket listener
	listener, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("failed to create socket: %w", err)
	}
	s.listener = listener

	// Set socket permissions
	if err := os.Chmod(s.socketPath, 0660); err != nil {
		listener.Close()
		return fmt.Errorf("failed to set socket permissions: %w", err)
	}

	ui.Info("Socket server listening on %s", s.socketPath)

	// Wrap listener for peer credentials (Linux only, no-op elsewhere)
	wrappedListener := WrapServerForPeerCred(s.httpServer, listener)

	// Serve HTTP over Unix socket
	return s.httpServer.Serve(wrappedListener)
}

// Shutdown gracefully shuts down the socket server.
func (s *SocketServer) Shutdown(ctx context.Context) error {
	if s.httpServer != nil {
		if err := s.httpServer.Shutdown(ctx); err != nil {
			return err
		}
	}
	// Clean up socket file
	os.Remove(s.socketPath)
	return nil
}

// TriggerRequest is the request body for /trigger.
type TriggerRequest struct {
	Source string `json:"source,omitempty"` // Source of trigger (e.g., "github", "manual")
}

// TriggerResponse is the response body for /trigger.
type TriggerResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

// ConfigResponse is the response body for /config.
// This allows the webhook container to fetch secrets from the daemon
// without storing them on disk.
type ConfigResponse struct {
	WebhookSecret string `json:"webhook_secret,omitempty"`
	PollInterval  int    `json:"poll_interval,omitempty"`
	RepoURL       string `json:"repo_url,omitempty"`
	RepoBranch    string `json:"repo_branch,omitempty"`
}

// StatusResponse is the response body for /status.
type StatusResponse struct {
	State         string     `json:"state"`          // idle, reconciling
	LastReconcile *time.Time `json:"last_reconcile,omitempty"`
	LastCommit    string     `json:"last_commit,omitempty"`
	LastError     string     `json:"last_error,omitempty"`
	Uptime        string     `json:"uptime"`
}

// handleTrigger handles POST /trigger requests.
func (s *SocketServer) handleTrigger(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse request (optional body)
	var req TriggerRequest
	if r.Body != nil && r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}
	}

	// Default source
	source := req.Source
	if source == "" {
		source = "socket"
	}

	// Add peer info if available
	if peerInfo := getPeerInfo(r); peerInfo != "" {
		source = fmt.Sprintf("%s (pid:%s)", source, peerInfo)
	}

	// Trigger reconcile
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if err := s.daemon.TriggerReconcile(ctx, source); err != nil {
			ui.Error("Socket-triggered reconciliation failed: %v", err)
		}
	}()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(TriggerResponse{
		Status:  "accepted",
		Message: "Reconciliation triggered",
	})
}

// handleStatus handles GET /status requests.
func (s *SocketServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	lastReconcile, lastErr := s.daemon.LastReconcile()

	// Determine state
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
	_ = json.NewEncoder(w).Encode(resp)
}

// handleHealth handles GET /health requests.
func (s *SocketServer) handleHealth(w http.ResponseWriter, r *http.Request) {
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

// handleConfig handles GET /config requests.
// This endpoint allows the webhook container to fetch secrets from the daemon
// without storing them on disk (daemon-injected secrets pattern).
func (s *SocketServer) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Build config response from daemon config
	cfg := s.daemon.config
	resp := ConfigResponse{
		WebhookSecret: cfg.WebhookSecret,
	}

	// Include poll interval in seconds
	if cfg.PollInterval > 0 {
		resp.PollInterval = int(cfg.PollInterval.Seconds())
	}

	// Include repo info if available
	if cfg.ReconcileConfig != nil {
		resp.RepoURL = cfg.ReconcileConfig.RepoURL
		resp.RepoBranch = cfg.ReconcileConfig.RepoBranch
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// auditMiddleware logs all requests with peer credentials.
func (s *SocketServer) auditMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Get peer credentials for audit
		peerInfo := getPeerInfo(r)

		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(wrapped, r)

		// Audit log
		if peerInfo != "" {
			ui.Info("AUDIT: %s %s from %s -> %d (%s)",
				r.Method, r.URL.Path, peerInfo, wrapped.statusCode, time.Since(start))
		} else {
			ui.Info("AUDIT: %s %s -> %d (%s)",
				r.Method, r.URL.Path, wrapped.statusCode, time.Since(start))
		}
	})
}

// getPeerInfo extracts peer information from the request context.
// This is set by platform-specific code using SO_PEERCRED.
func getPeerInfo(r *http.Request) string {
	if info := r.Context().Value(peerCredKey); info != nil {
		return info.(string)
	}
	return ""
}

// contextKey is a custom type for context keys.
type contextKey string

const peerCredKey contextKey = "peercred"
