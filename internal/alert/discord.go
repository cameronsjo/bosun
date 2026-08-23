package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/cameronsjo/bosun/internal/log"
)

// Discord embed colors (decimal format).
const (
	ColorInfo     = 0x3498db // Blue
	ColorWarning  = 0xf39c12 // Orange
	ColorError    = 0xe74c3c // Red
	ColorSuccess  = 0x2ecc71 // Green
	ColorCritical = 0x9b59b6 // Purple (for critical alerts)
)

const (
	discordTitleLimit       = 256
	discordDescriptionLimit = 4096
	discordFooterLimit      = 2048
	discordFieldNameLimit   = 256
	discordFieldValueLimit  = 1024
	discordFieldCountLimit  = 25
	discordEmbedTotalLimit  = 6000
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

	logger := log.ComponentCtx(ctx, "alert").With().Str("provider", "discord").Logger()
	start := time.Now()
	logger.Debug().
		Str("severity", string(alert.Severity)).
		Str("source", alert.Source).
		Msg("Preparing to send Discord alert")

	embed := buildDiscordEmbed(alert)

	payload := discordPayload{
		Embeds: []discordEmbed{embed},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		logger.Error().
			Err(err).
			Msg("Failed to send Discord alert. Error: failed to marshal payload")
		return fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.webhookURL, bytes.NewReader(body))
	if err != nil {
		logger.Error().
			Err(err).
			Msg("Failed to send Discord alert. Error: failed to create HTTP request")
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		logger.Error().
			Err(err).
			Msg("Failed to send Discord alert. Error: HTTP request failed")
		return fmt.Errorf("send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Discord returns 204 No Content on success.
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		logger.Error().
			Int(log.FieldStatus, resp.StatusCode).
			Msg("Failed to send Discord alert. Error: webhook returned error status")
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	logger.Info().
		Int(log.FieldStatus, resp.StatusCode).
		Int64(log.FieldDurationMS, time.Since(start).Milliseconds()).
		Msg("Successfully sent Discord alert")

	return nil
}

func buildDiscordEmbed(alert *Alert) discordEmbed {
	remaining := discordEmbedTotalLimit
	consume := func(text string, componentLimit int) string {
		limit := min(componentLimit, remaining)
		bounded := truncateUTF16(text, limit)
		remaining -= utf16Units(bounded)
		return bounded
	}

	embed := discordEmbed{
		Title:       consume(alert.Title, discordTitleLimit),
		Description: consume(alert.Message, discordDescriptionLimit),
		Color:       severityToColor(alert.Severity),
		Footer:      &discordFooter{Text: consume(fmt.Sprintf("bosun/%s", alert.Source), discordFooterLimit)},
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}

	keys := make([]string, 0, len(alert.Metadata))
	for key, value := range alert.Metadata {
		if key != "" && value != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)

	for _, key := range keys {
		if len(embed.Fields) == discordFieldCountLimit {
			break
		}

		name := truncateUTF16(key, discordFieldNameLimit)
		value := truncateUTF16(alert.Metadata[key], discordFieldValueLimit)
		name = truncateUTF16(name, remaining)
		value = truncateUTF16(value, remaining-utf16Units(name))
		if name == "" || value == "" {
			continue
		}

		remaining -= utf16Units(name) + utf16Units(value)
		embed.Fields = append(embed.Fields, discordEmbedField{
			Name:   name,
			Value:  value,
			Inline: true,
		})
	}

	return embed
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

// truncateString retains the existing Slack field behavior. Discord uses the
// provider-specific UTF-16 helpers above.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + truncationSuffix
}
