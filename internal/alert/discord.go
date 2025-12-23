package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

// Discord embed colors (decimal format).
const (
	ColorInfo     = 0x3498db // Blue
	ColorWarning  = 0xf39c12 // Orange
	ColorError    = 0xe74c3c // Red
	ColorSuccess  = 0x2ecc71 // Green
	ColorCritical = 0x9b59b6 // Purple (for critical alerts)
)

// discordEmbed represents a Discord embed object.
type discordEmbed struct {
	Title       string              `json:"title"`
	Description string              `json:"description"`
	Color       int                 `json:"color"`
	Footer      *discordFooter      `json:"footer,omitempty"`
	Timestamp   string              `json:"timestamp,omitempty"`
	Fields      []discordEmbedField `json:"fields,omitempty"`
}

// discordFooter represents a Discord embed footer.
type discordFooter struct {
	Text string `json:"text"`
}

// discordEmbedField represents a Discord embed field.
type discordEmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

// discordPayload represents the Discord webhook payload.
type discordPayload struct {
	Embeds []discordEmbed `json:"embeds"`
}

// DiscordProvider sends alerts via Discord webhooks.
type DiscordProvider struct {
	webhookURL string
	client     *http.Client
}

// NewDiscordProvider creates a new Discord provider.
// If webhookURL is empty, it reads from DISCORD_WEBHOOK_URL environment variable.
func NewDiscordProvider(webhookURL string) *DiscordProvider {
	if webhookURL == "" {
		webhookURL = os.Getenv("DISCORD_WEBHOOK_URL")
	}

	return &DiscordProvider{
		webhookURL: webhookURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Name returns the provider name.
func (d *DiscordProvider) Name() string {
	return "discord"
}

// IsConfigured returns true if the webhook URL is set.
func (d *DiscordProvider) IsConfigured() bool {
	return d.webhookURL != ""
}

// Send sends an alert to Discord.
func (d *DiscordProvider) Send(ctx context.Context, alert *Alert) error {
	if !d.IsConfigured() {
		return nil
	}

	embed := discordEmbed{
		Title:       alert.Title,
		Description: alert.Message,
		Color:       severityToColor(alert.Severity),
		Footer:      &discordFooter{Text: fmt.Sprintf("bosun/%s", alert.Source)},
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}

	// Add metadata as fields.
	if len(alert.Metadata) > 0 {
		embed.Fields = make([]discordEmbedField, 0, len(alert.Metadata))
		for key, value := range alert.Metadata {
			// Skip empty values.
			if value == "" {
				continue
			}
			embed.Fields = append(embed.Fields, discordEmbedField{
				Name:   key,
				Value:  truncateString(value, 1024), // Discord field limit.
				Inline: true,
			})
		}
	}

	payload := discordPayload{
		Embeds: []discordEmbed{embed},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	// Discord returns 204 No Content on success.
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	return nil
}

// severityToColor maps alert severity to Discord embed color.
func severityToColor(severity Severity) int {
	switch severity {
	case SeverityInfo:
		return ColorSuccess // Use green for info (usually success messages).
	case SeverityWarning:
		return ColorWarning
	case SeverityError:
		return ColorError
	case SeverityCritical:
		return ColorCritical
	default:
		return ColorInfo
	}
}

// truncateString truncates a string to the specified length.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
