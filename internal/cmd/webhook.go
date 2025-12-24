package cmd

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/cameronsjo/bosun/internal/daemon"
	"github.com/cameronsjo/bosun/internal/ui"
)

var (
	webhookPort       int
	webhookSocket     string
	webhookSecret     string
	webhookFetchSecret bool
)

// webhookCmd represents the webhook command.
var webhookCmd = &cobra.Command{
	Use:   "webhook",
	Short: "Run standalone webhook receiver",
	Long: `Run a lightweight HTTP server that receives webhooks and forwards
them to the bosun daemon via Unix socket.

This is useful for environments where you want to run the webhook
receiver in a container while the daemon runs on the host.

The webhook server validates signatures and forwards valid requests
to the daemon's trigger endpoint.

DAEMON-INJECTED SECRETS:
  Use --fetch-secret to have the webhook server fetch the webhook secret
  from the daemon at startup. This way the secret is never stored on disk
  in the webhook container - it only exists in the daemon's memory.

Configuration:
  --port          HTTP port to listen on (default: 8080)
  --socket        Path to daemon socket (default: /var/run/bosun.sock)
  --secret        Webhook secret for signature validation
  --fetch-secret  Fetch secret from daemon (never stored on disk)

Examples:
  bosun webhook                           # Listen on :8080
  bosun webhook --port 9000               # Listen on :9000
  bosun webhook --secret mywebhooksecret  # With signature validation
  bosun webhook --fetch-secret            # Fetch secret from daemon`,
	Run: runWebhook,
}

func init() {
	webhookCmd.Flags().IntVarP(&webhookPort, "port", "p", 8080, "HTTP port to listen on")
	webhookCmd.Flags().StringVar(&webhookSocket, "socket", "/var/run/bosun.sock", "Path to daemon socket")
	webhookCmd.Flags().StringVar(&webhookSecret, "secret", "", "Webhook secret for signature validation")
	webhookCmd.Flags().BoolVar(&webhookFetchSecret, "fetch-secret", false, "Fetch webhook secret from daemon (daemon-injected secrets)")

	rootCmd.AddCommand(webhookCmd)
}

func runWebhook(cmd *cobra.Command, args []string) {
	// Create daemon client
	client := daemon.NewClient(webhookSocket)

	// Get secret - priority: flag > env > daemon-injected
	secret := webhookSecret
	if secret == "" {
		secret = os.Getenv("WEBHOOK_SECRET")
	}
	if secret == "" {
		secret = os.Getenv("GITHUB_WEBHOOK_SECRET")
	}

	// Fetch secret from daemon if requested (daemon-injected secrets pattern)
	if webhookFetchSecret || secret == "" {
		ui.Info("Fetching configuration from daemon...")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		cfg, err := client.Config(ctx)
		cancel()

		if err != nil {
			if webhookFetchSecret {
				// Explicit request to fetch - fail if we can't
				ui.Fatal("Failed to fetch config from daemon: %v", err)
			}
			// Implicit fallback - just warn
			ui.Warning("Could not fetch config from daemon: %v", err)
		} else if cfg.WebhookSecret != "" {
			secret = cfg.WebhookSecret
			ui.Success("Webhook secret fetched from daemon (never stored on disk)")
		}
	}

	// Create webhook handler
	handler := &webhookHandler{
		client: client,
		secret: secret,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", handler.handleWebhook)
	mux.HandleFunc("/webhook/github", handler.handleGitHubWebhook)
	mux.HandleFunc("/webhook/gitlab", handler.handleGitLabWebhook)
	mux.HandleFunc("/webhook/gitea", handler.handleGiteaWebhook)
	mux.HandleFunc("/webhook/bitbucket", handler.handleBitbucketWebhook)
	mux.HandleFunc("/health", handler.handleHealth)
	mux.HandleFunc("/ready", handler.handleReady)

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", webhookPort),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in goroutine
	go func() {
		ui.Info("Webhook server listening on :%d", webhookPort)
		if secret != "" {
			ui.Info("Signature validation: enabled")
		} else {
			ui.Warning("Signature validation: disabled (set WEBHOOK_SECRET)")
		}
		ui.Info("Forwarding to daemon at %s", webhookSocket)

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			ui.Fatal("Server error: %v", err)
		}
	}()

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	<-sigCh

	ui.Info("Shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		ui.Error("Shutdown error: %v", err)
	}
	ui.Success("Webhook server stopped")
}

type webhookHandler struct {
	client *daemon.Client
	secret string
}

func (h *webhookHandler) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read body for signature validation
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	// Validate signature if secret is configured
	if h.secret != "" {
		sig := r.Header.Get("X-Hub-Signature-256")
		if sig == "" {
			sig = r.Header.Get("X-Signature-256")
		}
		if !validateSignature(body, sig, h.secret) {
			ui.Warning("Invalid webhook signature from %s", r.RemoteAddr)
			http.Error(w, "Invalid signature", http.StatusUnauthorized)
			return
		}
	}

	// Forward to daemon
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	resp, err := h.client.Trigger(ctx, "webhook")
	if err != nil {
		ui.Error("Failed to trigger daemon: %v", err)
		http.Error(w, "Failed to trigger reconciliation", http.StatusBadGateway)
		return
	}

	ui.Info("Webhook received, triggered reconciliation")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *webhookHandler) handleGitHubWebhook(w http.ResponseWriter, r *http.Request) {
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
	if h.secret != "" {
		sig := r.Header.Get("X-Hub-Signature-256")
		if !validateGitHubSignature(body, sig, h.secret) {
			ui.Warning("Invalid GitHub signature from %s", r.RemoteAddr)
			http.Error(w, "Invalid signature", http.StatusUnauthorized)
			return
		}
	}

	// Parse event type
	eventType := r.Header.Get("X-GitHub-Event")
	if eventType == "ping" {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"pong"}`))
		return
	}

	// Only process push events
	if eventType != "push" {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ignored","reason":"not a push event"}`))
		return
	}

	// Extract pusher info for logging
	var payload struct {
		Pusher struct {
			Name string `json:"name"`
		} `json:"pusher"`
		Ref string `json:"ref"`
	}
	if err := json.Unmarshal(body, &payload); err == nil {
		ui.Info("GitHub push from %s on %s", payload.Pusher.Name, payload.Ref)
	}

	// Forward to daemon
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	source := "github"
	if payload.Pusher.Name != "" {
		source = fmt.Sprintf("github:%s", payload.Pusher.Name)
	}

	resp, err := h.client.Trigger(ctx, source)
	if err != nil {
		ui.Error("Failed to trigger daemon: %v", err)
		http.Error(w, "Failed to trigger reconciliation", http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *webhookHandler) handleGitLabWebhook(w http.ResponseWriter, r *http.Request) {
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

	// Validate GitLab token (X-Gitlab-Token header)
	if h.secret != "" {
		token := r.Header.Get("X-Gitlab-Token")
		if token != h.secret {
			ui.Warning("Invalid GitLab token from %s", r.RemoteAddr)
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}
	}

	// Parse event type
	eventType := r.Header.Get("X-Gitlab-Event")

	// Only process push events
	if eventType != "Push Hook" {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ignored","reason":"not a push event"}`))
		return
	}

	// Extract user info for logging
	var payload struct {
		UserName string `json:"user_name"`
		Ref      string `json:"ref"`
	}
	if err := json.Unmarshal(body, &payload); err == nil {
		ui.Info("GitLab push from %s on %s", payload.UserName, payload.Ref)
	}

	// Forward to daemon
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	source := "gitlab"
	if payload.UserName != "" {
		source = fmt.Sprintf("gitlab:%s", payload.UserName)
	}

	resp, err := h.client.Trigger(ctx, source)
	if err != nil {
		ui.Error("Failed to trigger daemon: %v", err)
		http.Error(w, "Failed to trigger reconciliation", http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *webhookHandler) handleGiteaWebhook(w http.ResponseWriter, r *http.Request) {
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

	// Validate Gitea signature (X-Gitea-Signature header, HMAC-SHA256)
	if h.secret != "" {
		sig := r.Header.Get("X-Gitea-Signature")
		if !validateGiteaSignature(body, sig, h.secret) {
			ui.Warning("Invalid Gitea signature from %s", r.RemoteAddr)
			http.Error(w, "Invalid signature", http.StatusUnauthorized)
			return
		}
	}

	// Parse event type
	eventType := r.Header.Get("X-Gitea-Event")

	// Only process push events
	if eventType != "push" {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ignored","reason":"not a push event"}`))
		return
	}

	// Extract pusher info for logging
	var payload struct {
		Pusher struct {
			Login string `json:"login"`
		} `json:"pusher"`
		Ref string `json:"ref"`
	}
	if err := json.Unmarshal(body, &payload); err == nil {
		ui.Info("Gitea push from %s on %s", payload.Pusher.Login, payload.Ref)
	}

	// Forward to daemon
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	source := "gitea"
	if payload.Pusher.Login != "" {
		source = fmt.Sprintf("gitea:%s", payload.Pusher.Login)
	}

	resp, err := h.client.Trigger(ctx, source)
	if err != nil {
		ui.Error("Failed to trigger daemon: %v", err)
		http.Error(w, "Failed to trigger reconciliation", http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *webhookHandler) handleBitbucketWebhook(w http.ResponseWriter, r *http.Request) {
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

	// Validate Bitbucket signature (X-Hub-Signature header for Bitbucket Cloud)
	if h.secret != "" {
		sig := r.Header.Get("X-Hub-Signature")
		if sig == "" {
			// Try legacy header
			sig = r.Header.Get("X-Hook-UUID")
		}
		if !validateBitbucketSignature(body, sig, h.secret) {
			ui.Warning("Invalid Bitbucket signature from %s", r.RemoteAddr)
			http.Error(w, "Invalid signature", http.StatusUnauthorized)
			return
		}
	}

	// Parse event type
	eventType := r.Header.Get("X-Event-Key")

	// Only process push events
	if eventType != "repo:push" {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ignored","reason":"not a push event"}`))
		return
	}

	// Extract actor info for logging
	var payload struct {
		Actor struct {
			DisplayName string `json:"display_name"`
		} `json:"actor"`
		Push struct {
			Changes []struct {
				New struct {
					Name string `json:"name"`
				} `json:"new"`
			} `json:"changes"`
		} `json:"push"`
	}
	if err := json.Unmarshal(body, &payload); err == nil {
		branch := ""
		if len(payload.Push.Changes) > 0 {
			branch = payload.Push.Changes[0].New.Name
		}
		ui.Info("Bitbucket push from %s on %s", payload.Actor.DisplayName, branch)
	}

	// Forward to daemon
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	source := "bitbucket"
	if payload.Actor.DisplayName != "" {
		source = fmt.Sprintf("bitbucket:%s", payload.Actor.DisplayName)
	}

	resp, err := h.client.Trigger(ctx, source)
	if err != nil {
		ui.Error("Failed to trigger daemon: %v", err)
		http.Error(w, "Failed to trigger reconciliation", http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *webhookHandler) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// Check daemon connectivity
	health, err := h.client.Health(ctx)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "unhealthy",
			"error":  "daemon unreachable",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if health.Status != "healthy" {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	_ = json.NewEncoder(w).Encode(health)
}

func (h *webhookHandler) handleReady(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	health, err := h.client.Health(ctx)
	if err != nil || !health.Ready {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("not ready"))
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ready"))
}

// validateSignature validates a generic HMAC-SHA256 signature.
func validateSignature(body []byte, signature, secret string) bool {
	if signature == "" || secret == "" {
		return false
	}

	// Remove "sha256=" prefix if present
	if len(signature) > 7 && signature[:7] == "sha256=" {
		signature = signature[7:]
	}

	expected := computeHMAC(body, secret)
	return hmac.Equal([]byte(signature), []byte(expected))
}

// validateGitHubSignature validates a GitHub webhook signature.
func validateGitHubSignature(body []byte, signature, secret string) bool {
	if signature == "" || secret == "" {
		return false
	}

	// GitHub format: sha256=<hex>
	if len(signature) < 8 || signature[:7] != "sha256=" {
		return false
	}

	expected := "sha256=" + computeHMAC(body, secret)
	return hmac.Equal([]byte(signature), []byte(expected))
}

// computeHMAC computes HMAC-SHA256 and returns hex string.
func computeHMAC(data []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil))
}

// validateGiteaSignature validates a Gitea webhook signature.
// Gitea uses HMAC-SHA256 in the X-Gitea-Signature header (hex encoded).
func validateGiteaSignature(body []byte, signature, secret string) bool {
	if signature == "" || secret == "" {
		return false
	}

	expected := computeHMAC(body, secret)
	return hmac.Equal([]byte(signature), []byte(expected))
}

// validateBitbucketSignature validates a Bitbucket webhook signature.
// Bitbucket Cloud uses HMAC-SHA256 in the X-Hub-Signature header (sha256=<hex>).
// Bitbucket Server uses a different mechanism with X-Hub-Signature (sha1=<hex>).
func validateBitbucketSignature(body []byte, signature, secret string) bool {
	if signature == "" || secret == "" {
		return false
	}

	// Handle sha256= prefix (Bitbucket Cloud)
	if len(signature) > 7 && signature[:7] == "sha256=" {
		expected := "sha256=" + computeHMAC(body, secret)
		return hmac.Equal([]byte(signature), []byte(expected))
	}

	// Handle sha1= prefix (Bitbucket Server) - compute SHA1 HMAC
	if len(signature) > 5 && signature[:5] == "sha1=" {
		expected := "sha1=" + computeHMACSHA1(body, secret)
		return hmac.Equal([]byte(signature), []byte(expected))
	}

	// Plain hex (fallback)
	expected := computeHMAC(body, secret)
	return hmac.Equal([]byte(signature), []byte(expected))
}

// computeHMACSHA1 computes HMAC-SHA1 and returns hex string.
// Used for Bitbucket Server webhook validation.
func computeHMACSHA1(data []byte, secret string) string {
	mac := hmac.New(sha1.New, []byte(secret))
	mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil))
}
