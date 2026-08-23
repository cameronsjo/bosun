// Package daemon provides a long-running daemon for GitOps operations.
package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/cameronsjo/bosun/internal/log"
	"github.com/cameronsjo/bosun/internal/reconcile"
	sentrypkg "github.com/cameronsjo/bosun/internal/sentry"
	"github.com/cameronsjo/bosun/internal/ui"
)

var errSocketPathReplaced = errors.New("socket path was replaced; refusing to remove or trust replacement entry")

// socketOwnership records the published entry and, on Unix, a private hard
// link that pins its inode until ownership-checked cleanup completes.
type socketOwnership struct {
	file       os.FileInfo
	anchorPath string
}

// SocketServer handles Unix socket connections for the trigger API.
type SocketServer struct {
	daemon     *Daemon
	socketPath string
	socketMode os.FileMode
	mu         sync.Mutex
	starting   bool
	listener   net.Listener
	socketFile *socketOwnership
	httpServer *http.Server
}

// SocketConfig holds socket server configuration.
type SocketConfig struct {
	SocketPath string      // Path to Unix socket (e.g., /var/run/bosun.sock)
	SocketMode os.FileMode // Socket file permissions (default: 0660)
}

// DefaultSocketConfig returns default socket configuration.
func DefaultSocketConfig() *SocketConfig {
	return &SocketConfig{
		SocketPath: DefaultSocketPath,
		SocketMode: 0o660,
	}
}

// NewSocketServer creates a new Unix socket server.
func NewSocketServer(d *Daemon, cfg *SocketConfig) (*SocketServer, error) {
	if cfg == nil {
		cfg = DefaultSocketConfig()
	}
	if cfg.SocketPath == "" {
		return nil, errors.New("socket path is required")
	}
	if cfg.SocketMode&^os.ModePerm != 0 {
		return nil, fmt.Errorf("invalid socket mode %s: only permission bits are allowed", cfg.SocketMode)
	}

	s := &SocketServer{
		daemon:     d,
		socketPath: cfg.SocketPath,
		socketMode: cfg.SocketMode,
	}

	// Create HTTP handler for socket
	mux := http.NewServeMux()
	mux.HandleFunc("/trigger", s.handleTrigger)
	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/config", s.handleConfig)

	s.httpServer = &http.Server{
		Handler:           s.auditMiddleware(mux),
		ReadHeaderTimeout: daemonReadHeaderTimeout,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		MaxHeaderBytes:    daemonMaxHeaderBytes,
	}

	return s, nil
}

// Start starts the Unix socket server.
func (s *SocketServer) Start() (err error) {
	s.mu.Lock()
	if s.starting {
		s.mu.Unlock()
		return errors.New("socket server is already starting or running")
	}
	s.starting = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.starting = false
		s.mu.Unlock()
	}()

	// Ensure socket directory exists
	socketDir := filepath.Dir(s.socketPath)
	if err := os.MkdirAll(socketDir, 0o755); err != nil {
		return fmt.Errorf("failed to create socket directory: %w", err)
	}

	// Remove only a stale socket. Refuse symlinks, regular files, directories,
	// and other entries so a misconfigured path cannot delete unrelated data.
	if err := removeStaleSocket(s.socketPath); err != nil {
		return fmt.Errorf("failed to remove stale socket: %w", err)
	}

	// Publish the socket only after its configured mode is applied. Unix uses a
	// private staging directory plus atomic rename, avoiding process-global
	// umask changes and the net.Listen-then-Chmod permissive window.
	listener, socketFile, err := listenUnixSocket(s.socketPath, s.socketMode)
	if err != nil {
		return fmt.Errorf("failed to create socket: %w", err)
	}
	s.mu.Lock()
	s.listener = listener
	s.socketFile = socketFile
	s.mu.Unlock()
	defer func() {
		_ = listener.Close()
		if cleanupErr := s.releaseSocket(listener, socketFile); cleanupErr != nil {
			err = errors.Join(err, cleanupErr)
		}
	}()

	ui.Info("Socket server listening on %s", s.socketPath)

	// Wrap listener for peer credentials (Linux only, no-op elsewhere)
	wrappedListener := WrapServerForPeerCred(s.httpServer, listener)

	// Serve HTTP over Unix socket
	return s.httpServer.Serve(wrappedListener)
}

// Shutdown gracefully shuts down the socket server.
func (s *SocketServer) Shutdown(ctx context.Context) error {
	var shutdownErr error
	if s.httpServer != nil {
		if err := s.httpServer.Shutdown(ctx); err != nil {
			shutdownErr = err
		}
	}

	s.mu.Lock()
	listener := s.listener
	socketFile := s.socketFile
	s.mu.Unlock()
	cleanupErr := s.releaseSocket(listener, socketFile)
	return errors.Join(shutdownErr, cleanupErr)
}

// releaseSocket removes only the filesystem entry published by this server.
// If the path has been replaced, it is left untouched and the caller receives
// an error instead of deleting an attacker-controlled or unrelated entry.
func (s *SocketServer) releaseSocket(listener net.Listener, socketFile *socketOwnership) error {
	if socketFile == nil {
		return nil
	}
	cleanupErr := removeSocketIfSame(s.socketPath, socketFile)

	s.mu.Lock()
	if s.listener == listener && s.socketFile == socketFile &&
		(cleanupErr == nil || errors.Is(cleanupErr, errSocketPathReplaced)) {
		s.listener = nil
		s.socketFile = nil
	}
	s.mu.Unlock()

	return cleanupErr
}

// TriggerRequest is the request body for /trigger.
type TriggerRequest struct {
	Source string `json:"source,omitempty"` // Source of trigger (e.g., "github", "manual")
	Force  bool   `json:"force,omitempty"`  // Force full pipeline execution regardless of state
}

// maxTriggerBodyBytes caps trigger request bodies. The payload ({source, force,
// target}) is well under 1 KB; 64 KB is generous headroom that still defends
// against a JSON-bomb DoS via an unbounded decoder.
const maxTriggerBodyBytes = 64 << 10 // 64 KiB

// decodeTriggerRequest decodes a /trigger body under a hard size cap. It wraps
// the body in http.MaxBytesReader (not io.LimitReader, which would silently
// truncate) so an oversized body surfaces as *http.MaxBytesError, mapped to
// 413. Malformed JSON maps to 400. An absent or empty body is valid (all
// fields optional). On any error the response is written here and ok is false;
// callers proceed only when ok is true.
func decodeTriggerRequest(w http.ResponseWriter, r *http.Request) (TriggerRequest, bool) {
	var req TriggerRequest
	if r.Body == nil {
		return req, true
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxTriggerBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
			return req, false
		}
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return req, false
	}
	return req, true
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
	State         string     `json:"state"` // idle, reconciling
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

	// Parse request (optional body) under a size cap. Do not gate on
	// ContentLength — it may be -1 (unknown) when the client omits it.
	req, ok := decodeTriggerRequest(w, r)
	if !ok {
		return
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
		defer sentrypkg.Recover()
		if err := s.daemon.TriggerReconcile(context.Background(), source, req.Force); err != nil {
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

	resp := buildConfigResponse(s.daemon.config)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// auditMiddleware logs all requests with peer credentials.
func (s *SocketServer) auditMiddleware(next http.Handler) http.Handler {
	logger := log.Component(log.ComponentDaemon)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Get peer credentials for audit
		peerInfo := getPeerInfo(r)

		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(wrapped, r)

		// Structured audit log with queryable fields
		event := logger.Info().
			Str(log.FieldOperation, "audit").
			Str(log.FieldMethod, r.Method).
			Str(log.FieldURL, r.URL.Path).
			Int(log.FieldStatus, wrapped.statusCode).
			Int64(log.FieldDurationMS, time.Since(start).Milliseconds())

		if peerInfo != "" {
			event = event.Str("peer", peerInfo)
		}

		event.Msg("Handled socket request")
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
