package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/cameronsjo/bosun/internal/log"
)

// slackPayload represents the Slack incoming webhook payload.
type slackPayload struct {
	Text        string       `json:"text"`
	Attachments []slackAttachment `json:"attachments,omitempty"`
}

// slackAttachment represents a Slack message attachment for rich formatting.
type slackAttachment struct {
	Color    string       `json:"color"`
	Title    string       `json:"title"`
	Text     string       `json:"text"`
	Footer   string       `json:"footer"`
	Ts       int64        `json:"ts"`
	Fields   []slackField `json:"fields,omitempty"`
}

// slackField represents a field in a Slack attachment.
type slackField struct {
	Title string `json:"title"`
	Value string `json:"value"`
	Short bool   `json:"short"`
}

// SlackProvider sends alerts via Slack incoming webhooks.
type SlackProvider struct {
	webhookURL string
	client     *http.Client
}

// NewSlackProvider creates a new Slack provider.
// If webhookURL is empty, it falls back to BOSUN_SLACK_WEBHOOK_URL then SLACK_WEBHOOK_URL.
func NewSlackProvider(webhookURL string) *SlackProvider {
	if webhookURL == "" {
		webhookURL = os.Getenv("BOSUN_SLACK_WEBHOOK_URL")
	}
	if webhookURL == "" {
		webhookURL = os.Getenv("SLACK_WEBHOOK_URL")
	}

	return &SlackProvider{
		webhookURL: webhookURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Name returns the provider name.
func (s *SlackProvider) Name() string {
	return "slack"
}

// IsConfigured returns true if the webhook URL is set.
func (s *SlackProvider) IsConfigured() bool {
	return s.webhookURL != ""
}

// Send sends an alert to Slack.
func (s *SlackProvider) Send(ctx context.Context, alert *Alert) error {
	if !s.IsConfigured() {
		return nil
	}

	logger := log.Component("slack")
	start := time.Now()
	logger.Debug().
		Str("title", alert.Title).
		Str("severity", string(alert.Severity)).
		Msg("Preparing to send Slack alert")

	attachment := slackAttachment{
		Color:  severityToSlackColor(alert.Severity),
		Title:  alert.Title,
		Text:   alert.Message,
		Footer: fmt.Sprintf("bosun/%s", alert.Source),
		Ts:     time.Now().Unix(),
	}

	// Add metadata as fields.
	if len(alert.Metadata) > 0 {
		attachment.Fields = make([]slackField, 0, len(alert.Metadata))
		for key, value := range alert.Metadata {
			// Skip empty values.
			if value == "" {
				continue
			}
			attachment.Fields = append(attachment.Fields, slackField{
				Title: key,
				Value: truncateString(value, 1024),
				Short: true,
			})
		}
	}

	payload := slackPayload{
		Attachments: []slackAttachment{attachment},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		logger.Error().
			Err(err).
			Msg("Slack webhook request failed")
		return fmt.Errorf("send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Slack returns 200 OK on success.
	if resp.StatusCode != http.StatusOK {
		logger.Error().
			Int("status_code", resp.StatusCode).
			Msg("Slack webhook returned error status")
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	logger.Debug().
		Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
		Msg("Successfully sent Slack alert")

	return nil
}

// severityToSlackColor maps alert severity to Slack attachment color.
// Slack uses hex color strings (e.g., "#2ecc71") or named colors ("good", "warning", "danger").
func severityToSlackColor(severity Severity) string {
	switch severity {
	case SeverityInfo:
		return "#2ecc71" // Green
	case SeverityWarning:
		return "#f39c12" // Orange
	case SeverityError:
		return "#e74c3c" // Red
	case SeverityCritical:
		return "#9b59b6" // Purple
	default:
		return "#3498db" // Blue
	}
}
