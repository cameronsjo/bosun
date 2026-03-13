package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/cameronsjo/bosun/internal/log"
)

// WebhookConfig holds configuration for the generic webhook provider.
type WebhookConfig struct {
	URL     string            // Webhook endpoint URL
	Headers map[string]string // Custom HTTP headers (e.g., Authorization)
	Method  string            // HTTP method (default: POST)
}

// webhookPayload is the JSON body sent to the webhook endpoint.
type webhookPayload struct {
	Event     string            `json:"event"`
	Title     string            `json:"title"`
	Message   string            `json:"message"`
	Severity  string            `json:"severity"`
	Timestamp string            `json:"timestamp"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// WebhookProvider sends alerts to a generic HTTP webhook endpoint.
type WebhookProvider struct {
	config WebhookConfig
	client *http.Client
}

// NewWebhookProvider creates a new generic webhook provider.
func NewWebhookProvider(config WebhookConfig) *WebhookProvider {
	if config.Method == "" {
		config.Method = http.MethodPost
	}

	return &WebhookProvider{
		config: config,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// Name returns the provider name.
func (w *WebhookProvider) Name() string {
	return "webhook"
}

// IsConfigured returns true if the webhook URL is set.
func (w *WebhookProvider) IsConfigured() bool {
	return w.config.URL != ""
}

// Send sends an alert to the configured webhook endpoint.
func (w *WebhookProvider) Send(ctx context.Context, alert *Alert) error {
	if !w.IsConfigured() {
		return nil
	}

	logger := log.Component("webhook")
	start := time.Now()
	logger.Debug().
		Str("title", alert.Title).
		Str("severity", string(alert.Severity)).
		Str("url_host", maskURL(w.config.URL)).
		Msg("Preparing to send webhook alert")

	payload := webhookPayload{
		Event:     alert.Source,
		Title:     alert.Title,
		Message:   alert.Message,
		Severity:  string(alert.Severity),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Metadata:  alert.Metadata,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, w.config.Method, w.config.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	// Set default Content-Type, allow override via custom headers.
	req.Header.Set("Content-Type", "application/json")
	for key, value := range w.config.Headers {
		req.Header.Set(key, value)
	}

	resp, err := w.client.Do(req)
	if err != nil {
		logger.Error().
			Err(err).
			Msg("Webhook request failed")
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		logger.Error().
			Int("status_code", resp.StatusCode).
			Msg("Webhook returned error status")
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	logger.Debug().
		Int("status_code", resp.StatusCode).
		Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
		Msg("Successfully sent webhook alert")

	return nil
}

// maskURL returns only the host portion of a URL for safe logging.
func maskURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return "[invalid]"
	}
	return u.Host
}
