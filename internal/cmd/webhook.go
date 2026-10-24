package cmd

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
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

	"github.com/cameronsjo/bosun/internal/config"
	"github.com/cameronsjo/bosun/internal/daemon"
	"github.com/cameronsjo/bosun/internal/log"
	"github.com/cameronsjo/bosun/internal/ui"
)

var (
	webhookPort        int
	webhookSocket      string
	webhookSecret      string
	webhookFetchSecret bool
	webhookBranch      string
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

WEBHOOK AUTH FAILS CLOSED:
  With no secret resolved (--secret, WEBHOOK_SECRET, GITHUB_WEBHOOK_SECRET,
  or --fetch-secret), the trigger endpoints reject every request with 403.
  Set BOSUN_ALLOW_UNAUTHENTICATED_WEBHOOK=true to accept unauthenticated
  triggers instead; each accepted request is logged as a security warning.
  This mirrors the daemon's own webhook posture.

DAEMON-INJECTED SECRETS:
  Use --fetch-secret to have the webhook server fetch the webhook secret
  from the daemon at startup. This way the secret is never stored on disk
  in the webhook container - it only exists in the daemon's memory. The
  daemon serves that secret only to an authorized socket peer, so the
  receiver must run as the daemon's UID or a BOSUN_SOCKET_ALLOWED_UIDS
  member.

Configuration:
  --port          HTTP port to listen on (default: 8080)
  --socket        Path to daemon socket (default: $BOSUN_SOCKET_PATH or /var/run/bosun.sock)
  --secret        Webhook secret for signature validation
  --branch        Tracked branch for the webhook ref filter (generic and
                  provider endpoints; default: $BOSUN_REPO_BRANCH, then the
                  daemon's branch)
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
	webhookCmd.Flags().StringVar(&webhookSocket, "socket", resolveSocketPath(), "Path to daemon socket")
	webhookCmd.Flags().StringVar(&webhookSecret, "secret", "", "Webhook secret for signature validation")
	webhookCmd.Flags().BoolVar(&webhookFetchSecret, "fetch-secret", false, "Fetch webhook secret from daemon (daemon-injected secrets)")
	webhookCmd.Flags().StringVar(&webhookBranch, "branch", "", "Tracked branch for the webhook ref filter on every trigger endpoint (default: $BOSUN_REPO_BRANCH, then the daemon's configured branch)")

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

	// Tracked branch for the generic /webhook ref filter (#728) - priority:
	// flag > env > daemon config.
	branch := webhookBranch
	if branch == "" {
		branch = config.BosunEnv("REPO_BRANCH")
	}

	// Fetch secret (daemon-injected secrets pattern) and, when still unknown,
	// the tracked branch from the daemon.
	if webhookFetchSecret || secret == "" || branch == "" {
		ui.Info("Fetching configuration from daemon...")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		cfg, err := client.Config(ctx)
		cancel()

		if err != nil {
			if webhookFetchSecret {
				// Explicit request to fetch - fail if we can't
				ui.Fatal("Failed to fetch config from daemon: %v", err)
			}
			// Implicit fallback - the receiver still serves, but with no
			// secret resolved the trigger endpoints fail closed below rather
			// than silently accepting unsigned requests.
			ui.Warning("Could not fetch config from daemon: %v", err)
		} else {
			// Same rule as before the branch lookup: the daemon secret applies on
			// --fetch-secret or when no secret resolved.
			if (webhookFetchSecret || secret == "") && cfg.WebhookSecret != "" {
				secret = cfg.WebhookSecret
				ui.Success("Webhook secret fetched from daemon (never stored on disk)")
			}
			if branch == "" {
				branch = cfg.RepoBranch
			}
		}
	}

	// Create webhook handler
	handler := &webhookHandler{
		client:               client,
		secret:               secret,
		allowUnauthenticated: os.Getenv("BOSUN_ALLOW_UNAUTHENTICATED_WEBHOOK") == "true",
		branch:               branch,
	}

	server := &http.Server{
		Addr:           fmt.Sprintf(":%d", webhookPort),
		Handler:        newWebhookMux(handler),
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   30 * time.Second,
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: maxWebhookHeaderBytes,
	}

	// Start server in goroutine
	go func() {
		ui.Info("Webhook server listening on :%d", webhookPort)
		warnWebhookReceiverAuthPosture(handler)
		announceWebhookReceiverBranchFilter(handler)
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

// maxWebhookBodySize caps request bodies on the standalone webhook receiver to
// prevent unbounded reads from a tunnel-exposed endpoint (mirrors the daemon's
// own 1MB limit in internal/daemon/server.go).
const maxWebhookBodySize = 1 << 20 // 1MB

// maxWebhookHeaderBytes caps request header size (request line + header fields),
// which is distinct from the body limit above. 64KB is generous for webhook
// headers while bounding header-based memory abuse.
const maxWebhookHeaderBytes = 64 * 1024 // 64KB

type webhookHandler struct {
	client webhookTriggerClient
	secret string

	// allowUnauthenticated opts out of the fail-closed posture below
	// (BOSUN_ALLOW_UNAUTHENTICATED_WEBHOOK=true, strict match), mirroring the
	// daemon's own opt-out.
	allowUnauthenticated bool

	// branch is the tracked branch every trigger endpoint compares a
	// payload's ref against (#728 generic, #740 providers). Empty means
	// unknown: the ref check is off, only event filters apply, and startup
	// says so.
	branch string

	// deliveries remembers recent delivery IDs so a repeated delivery is not
	// forwarded twice (#747). The zero value is ready.
	deliveries daemon.DeliveryCache
}

// claimDelivery records the request's replay keys (delivery ID and body
// digest); call it only after the signature or token check passes (#747). A
// repeat of either key gets 200 ignored, written here, and false; the caller
// MUST return. The caller releases the returned keys when the forward fails,
// so the provider's retry is processed.
func (h *webhookHandler) claimDelivery(w http.ResponseWriter, r *http.Request, body []byte) ([]string, bool) {
	keys := daemon.WebhookReplayKeys(r.Header, body)
	matched, ok := h.deliveries.ClaimMatch(keys...)
	if ok {
		return keys, true
	}
	daemon.WriteDuplicateDeliveryIgnored(w, r, matched)
	return keys, false
}

// authorizeTrigger enforces the receiver's webhook authentication posture
// before a trigger endpoint does any work. On failure it writes the 403 itself
// and returns false — the caller MUST NOT write a second response
// (`if !h.authorizeTrigger(w, r) { return }`).
//
// It mirrors the daemon's gate (#345, internal/daemon/server.go
// authorizeTrigger): with no secret resolved, every trigger request is
// rejected with 403 unless the operator explicitly opted out via
// BOSUN_ALLOW_UNAUTHENTICATED_WEBHOOK=true, in which case each accepted
// unauthenticated request logs a security warning. The receiver needs its own
// copy because it forwards over the Unix socket, which authorizes by peer
// credential and never applies the daemon's HTTP webhook gate.
//
// A configured secret passes this gate; the caller's provider-specific
// signature check is the second one.
func (h *webhookHandler) authorizeTrigger(w http.ResponseWriter, r *http.Request) bool {
	if h.secret != "" {
		return true
	}

	logger := log.ComponentCtx(r.Context(), log.ComponentWebhook)
	if h.allowUnauthenticated {
		logger.Warn().
			Str("remote_addr", r.RemoteAddr).
			Str("endpoint", r.URL.Path).
			Msg("SECURITY: accepting unauthenticated trigger request. Reason: no webhook secret configured and BOSUN_ALLOW_UNAUTHENTICATED_WEBHOOK=true")
		return true
	}

	logger.Warn().
		Str("remote_addr", r.RemoteAddr).
		Str("endpoint", r.URL.Path).
		Msg("Rejecting trigger request, expected a configured webhook secret but none is set. Set WEBHOOK_SECRET, pass --secret or --fetch-secret, or set BOSUN_ALLOW_UNAUTHENTICATED_WEBHOOK=true to accept unauthenticated triggers")
	http.Error(w, "Webhook authentication not configured", http.StatusForbidden)
	return false
}

type webhookTriggerClient interface {
	Trigger(context.Context, string, bool) (*daemon.TriggerResponse, error)
	Health(context.Context) (*daemon.HealthResponse, error)
}

// newWebhookMux builds the receiver's routing table. Extracted from runWebhook
// so tests exercise the same routes the server serves.
func newWebhookMux(handler *webhookHandler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", handler.handleWebhook)
	mux.HandleFunc("/webhook/github", handler.handleGitHubWebhook)
	mux.HandleFunc("/webhook/gitlab", handler.handleGitLabWebhook)
	mux.HandleFunc("/webhook/gitea", handler.handleGiteaWebhook)
	mux.HandleFunc("/webhook/bitbucket", handler.handleBitbucketWebhook)
	mux.HandleFunc("/health", handler.handleHealth)
	mux.HandleFunc("/ready", handler.handleReady)
	return mux
}

// warnWebhookReceiverAuthPosture announces the receiver's webhook auth posture
// at startup, mirroring the daemon's warnWebhookAuthPosture: fail-closed is the
// default, so a secret-less receiver must never be a silent 403, and an
// opted-out receiver must never hide its exposure.
func warnWebhookReceiverAuthPosture(handler *webhookHandler) {
	logger := log.Component(log.ComponentWebhook)
	if handler.secret != "" {
		ui.Info("Signature validation: enabled")
		return
	}
	if handler.allowUnauthenticated {
		logger.Warn().
			Msg("SECURITY: webhook receiver forwards UNAUTHENTICATED trigger requests to the daemon. Reason: no webhook secret configured and BOSUN_ALLOW_UNAUTHENTICATED_WEBHOOK=true. Anyone who can reach the HTTP port can trigger a deploy")
		ui.Warning("SECURITY: unauthenticated webhook triggers enabled (no secret, opt-out set)")
		return
	}
	logger.Warn().
		Msg("Webhook receiver will REJECT all trigger requests, expected a webhook secret but none is configured — webhook auth fails closed. Set WEBHOOK_SECRET, pass --secret or --fetch-secret, or set BOSUN_ALLOW_UNAUTHENTICATED_WEBHOOK=true to accept unauthenticated triggers")
	ui.Warning("Webhooks fail closed: no WEBHOOK_SECRET set, trigger requests will be rejected (403)")
}

// announceWebhookReceiverBranchFilter states at startup which branch the
// trigger endpoints filter on, or that the ref filter is off.
func announceWebhookReceiverBranchFilter(handler *webhookHandler) {
	if handler.branch != "" {
		ui.Info("Webhook branch filter: refs/heads/%s", daemon.SanitizeWebhookPusherName(handler.branch))
		return
	}
	logger := log.Component(log.ComponentWebhook)
	logger.Warn().
		Msg("Webhook ref filter is off on every trigger endpoint, expected a tracked branch but none is known. Pass --branch, set BOSUN_REPO_BRANCH, or let the receiver read the daemon config (--fetch-secret)")
	ui.Warning("Webhook ref filter off: tracked branch unknown, pushes to any branch are forwarded")
}

func (h *webhookHandler) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.authorizeTrigger(w, r) {
		return
	}

	// Read body for signature validation
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBodySize))
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
	replayKeys, fresh := h.claimDelivery(w, r, body)
	if !fresh {
		return
	}

	// Decide on the payload only after authorization, with the daemon's
	// shared rule (#728).
	githubEvent := r.Header.Get("X-GitHub-Event")
	process, ref, reason := daemon.ShouldProcessGenericWebhook(githubEvent, body, h.branch)
	if !process {
		reason = daemon.SanitizeWebhookPusherName(reason)
		logger := log.ComponentCtx(r.Context(), log.ComponentWebhook)
		logger.Info().
			Str(log.FieldBranch, daemon.SanitizeWebhookPusherName(ref)).
			Str("event_type", daemon.SanitizeWebhookPusherName(githubEvent)).
			Str("reason", reason).
			Msg("Generic webhook ignored")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":  "ignored",
			"message": reason,
		})
		return
	}
	if daemon.PushDeletesBranch(body) {
		daemon.WriteBranchDeleteIgnored(w, r, "generic", ref)
		return
	}

	// Forward to daemon
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	resp, err := h.client.Trigger(ctx, "webhook", false)
	if err != nil {
		h.deliveries.Release(replayKeys...)
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
	if !h.authorizeTrigger(w, r) {
		return
	}

	// Read body
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBodySize))
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
	replayKeys, fresh := h.claimDelivery(w, r, body)
	if !fresh {
		return
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
	pusherName := ""
	if err := json.Unmarshal(body, &payload); err == nil {
		pusherName = daemon.SanitizeWebhookPusherName(payload.Pusher.Name)
		ui.Info("GitHub push from %s on %s", pusherName, daemon.SanitizeWebhookPusherName(payload.Ref))
	}
	if process, reason := daemon.ShouldProcessPushRef(payload.Ref, h.branch); !process {
		writeProviderPushIgnored(w, r, "github", payload.Ref, reason)
		return
	}
	if daemon.PushDeletesBranch(body) {
		daemon.WriteBranchDeleteIgnored(w, r, "github", payload.Ref)
		return
	}

	// Forward to daemon
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	source := "github"
	if pusherName != "" {
		source = fmt.Sprintf("github:%s", pusherName)
	}

	resp, err := h.client.Trigger(ctx, source, false)
	if err != nil {
		h.deliveries.Release(replayKeys...)
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
	if !h.authorizeTrigger(w, r) {
		return
	}

	// Read body
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBodySize))
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	// Validate GitLab token (X-Gitlab-Token header)
	if h.secret != "" {
		token := r.Header.Get("X-Gitlab-Token")
		if subtle.ConstantTimeCompare([]byte(token), []byte(h.secret)) != 1 {
			ui.Warning("Invalid GitLab token from %s", r.RemoteAddr)
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}
	}
	replayKeys, fresh := h.claimDelivery(w, r, body)
	if !fresh {
		return
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
	userName := ""
	if err := json.Unmarshal(body, &payload); err == nil {
		userName = daemon.SanitizeWebhookPusherName(payload.UserName)
		ui.Info("GitLab push from %s on %s", userName, daemon.SanitizeWebhookPusherName(payload.Ref))
	}
	if process, reason := daemon.ShouldProcessPushRef(payload.Ref, h.branch); !process {
		writeProviderPushIgnored(w, r, "gitlab", payload.Ref, reason)
		return
	}
	if daemon.PushDeletesBranch(body) {
		daemon.WriteBranchDeleteIgnored(w, r, "gitlab", payload.Ref)
		return
	}

	// Forward to daemon
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	source := "gitlab"
	if userName != "" {
		source = fmt.Sprintf("gitlab:%s", userName)
	}

	resp, err := h.client.Trigger(ctx, source, false)
	if err != nil {
		h.deliveries.Release(replayKeys...)
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
	if !h.authorizeTrigger(w, r) {
		return
	}

	// Read body
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBodySize))
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
	replayKeys, fresh := h.claimDelivery(w, r, body)
	if !fresh {
		return
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
	pusherLogin := ""
	if err := json.Unmarshal(body, &payload); err == nil {
		pusherLogin = daemon.SanitizeWebhookPusherName(payload.Pusher.Login)
		ui.Info("Gitea push from %s on %s", pusherLogin, daemon.SanitizeWebhookPusherName(payload.Ref))
	}
	if process, reason := daemon.ShouldProcessPushRef(payload.Ref, h.branch); !process {
		writeProviderPushIgnored(w, r, "gitea", payload.Ref, reason)
		return
	}
	if daemon.PushDeletesBranch(body) {
		daemon.WriteBranchDeleteIgnored(w, r, "gitea", payload.Ref)
		return
	}

	// Forward to daemon
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	source := "gitea"
	if pusherLogin != "" {
		source = fmt.Sprintf("gitea:%s", pusherLogin)
	}

	resp, err := h.client.Trigger(ctx, source, false)
	if err != nil {
		h.deliveries.Release(replayKeys...)
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
	if !h.authorizeTrigger(w, r) {
		return
	}

	// Read body
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBodySize))
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
	replayKeys, fresh := h.claimDelivery(w, r, body)
	if !fresh {
		return
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
			Changes []bitbucketPushChange `json:"changes"`
		} `json:"push"`
	}
	actorName := ""
	if err := json.Unmarshal(body, &payload); err == nil {
		actorName = daemon.SanitizeWebhookPusherName(payload.Actor.DisplayName)
		branch := ""
		if len(payload.Push.Changes) > 0 && payload.Push.Changes[0].New != nil {
			branch = daemon.SanitizeWebhookPusherName(payload.Push.Changes[0].New.Name)
		}
		ui.Info("Bitbucket push from %s on %s", actorName, branch)
	}
	if process, name, reason := shouldProcessBitbucketPush(payload.Push.Changes, h.branch); !process {
		writeProviderPushIgnored(w, r, "bitbucket", name, reason)
		return
	}
	if bitbucketPushDeletesOnly(payload.Push.Changes) {
		daemon.WriteBranchDeleteIgnored(w, r, "bitbucket", "")
		return
	}

	// Forward to daemon
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	source := "bitbucket"
	if actorName != "" {
		source = fmt.Sprintf("bitbucket:%s", actorName)
	}

	resp, err := h.client.Trigger(ctx, source, false)
	if err != nil {
		h.deliveries.Release(replayKeys...)
		ui.Error("Failed to trigger daemon: %v", err)
		http.Error(w, "Failed to trigger reconciliation", http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(resp)
}

// bitbucketPushChange is one entry of a Bitbucket push payload's
// push.changes. New is nil when the change deletes a branch or tag.
type bitbucketPushChange struct {
	New *struct {
		Type string `json:"type"`
		Name string `json:"name"`
	} `json:"new"`
}

// shouldProcessBitbucketPush decides whether an authorized Bitbucket push
// updates the tracked branch (#740). Bitbucket names a branch bare (no
// refs/heads/ prefix) and lists one entry per updated ref, so the push passes
// when any change's new target is a branch with exactly the tracked name. Tags,
// deletions (no new target), other branches, and a push with no changes are
// ignored. An empty trackingBranch means the branch is unknown: every push
// passes. The returned name is the first change's raw target name for logging;
// callers must sanitize it, and reason may embed it unsanitized.
func shouldProcessBitbucketPush(changes []bitbucketPushChange, trackingBranch string) (process bool, name string, reason string) {
	if trackingBranch == "" {
		return true, "", "no tracked branch configured; branch not checked"
	}
	for _, change := range changes {
		if change.New != nil && change.New.Type == "branch" && change.New.Name == trackingBranch {
			return true, change.New.Name, "push to tracked branch " + trackingBranch
		}
	}
	if len(changes) > 0 && changes[0].New != nil {
		name = changes[0].New.Name
		return false, name, "Push to " + changes[0].New.Type + " " + name + " ignored (tracking branch " + trackingBranch + ")"
	}
	return false, "", "Push with no new branch target ignored (tracking branch " + trackingBranch + ")"
}

// bitbucketPushDeletesOnly reports whether every change in a Bitbucket push
// deletes its ref (no new target). The branch filter already ignores such a
// push when a branch is tracked; this covers the receiver with no known branch
// (#747). An empty change list is not a deletion.
func bitbucketPushDeletesOnly(changes []bitbucketPushChange) bool {
	if len(changes) == 0 {
		return false
	}
	for _, change := range changes {
		if change.New != nil {
			return false
		}
	}
	return true
}

// writeProviderPushIgnored answers an authorized provider push that does not
// target the tracked branch (#740): 200 so the provider does not retry, a
// sanitized log line, and nothing forwarded to the daemon. Mirrors the generic
// /webhook's ignored response.
func writeProviderPushIgnored(w http.ResponseWriter, r *http.Request, provider, ref, reason string) {
	reason = daemon.SanitizeWebhookPusherName(reason)
	logger := log.ComponentCtx(r.Context(), log.ComponentWebhook)
	logger.Info().
		Str("provider", provider).
		Str(log.FieldBranch, daemon.SanitizeWebhookPusherName(ref)).
		Str("reason", reason).
		Msg("Provider webhook push ignored, expected a push to the tracked branch")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "ignored",
		"message": reason,
	})
}

func (h *webhookHandler) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// Check daemon connectivity
	health, err := h.client.Health(ctx)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(daemon.HealthResponse{Status: "unhealthy"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if health.Status != "healthy" {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	_ = json.NewEncoder(w).Encode(health)
}

func (h *webhookHandler) handleReady(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

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
