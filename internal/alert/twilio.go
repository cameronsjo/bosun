package alert

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// TwilioConfig holds configuration for the Twilio SMS alerter.
type TwilioConfig struct {
	// AccountSID is the Twilio account SID (TWILIO_ACCOUNT_SID).
	AccountSID string

	// AuthToken is the Twilio auth token (TWILIO_AUTH_TOKEN).
	AuthToken string

	// FromNumber is the Twilio phone number to send from (e.g., "+15551234567").
	FromNumber string

	// ToNumbers is the list of recipient phone numbers.
	ToNumbers []string
}

// Twilio implements the Provider interface for Twilio SMS notifications.
type Twilio struct {
	config TwilioConfig
	client *http.Client
}

// twilioAPIURL is the base URL for the Twilio REST API.
const twilioAPIURL = "https://api.twilio.com/2010-04-01"

// maxSMSLength is the maximum length for SMS messages.
// Twilio supports up to 1600 characters for concatenated SMS.
const maxSMSLength = 1600

// NewTwilio creates a new Twilio alerter with the given configuration.
func NewTwilio(config TwilioConfig) *Twilio {
	return &Twilio{
		config: config,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// Name returns the provider name.
func (t *Twilio) Name() string {
	return "twilio"
}

// IsConfigured returns true if all required configuration is present.
func (t *Twilio) IsConfigured() bool {
	return t.config.AccountSID != "" &&
		t.config.AuthToken != "" &&
		t.config.FromNumber != "" &&
		len(t.config.ToNumbers) > 0
}

// Send sends an SMS alert to all configured recipients.
// Only sends for error or critical severity to minimize SMS costs.
func (t *Twilio) Send(ctx context.Context, alert *Alert) error {
	if !t.IsConfigured() {
		return fmt.Errorf("twilio is not configured")
	}

	// Only send SMS for error or critical severity (SMS is expensive)
	if alert.Severity != SeverityError && alert.Severity != SeverityCritical {
		return nil
	}

	message := t.formatMessage(alert)
	var lastErr error

	for _, toNumber := range t.config.ToNumbers {
		if err := t.sendSMS(ctx, toNumber, message); err != nil {
			lastErr = fmt.Errorf("send to %s: %w", maskPhoneNumber(toNumber), err)
		}
	}

	return lastErr
}

// sendSMS sends a single SMS message to one recipient.
func (t *Twilio) sendSMS(ctx context.Context, toNumber, message string) error {
	endpoint := fmt.Sprintf("%s/Accounts/%s/Messages.json", twilioAPIURL, t.config.AccountSID)

	formData := url.Values{}
	formData.Set("To", formatPhoneNumber(toNumber))
	formData.Set("From", formatPhoneNumber(t.config.FromNumber))
	formData.Set("Body", message)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(formData.Encode()))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", t.buildAuthHeader())

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("twilio API error (status %d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// formatMessage creates an SMS-friendly message from an alert.
// Format: [SEVERITY] Title: Message (truncated to maxSMSLength)
func (t *Twilio) formatMessage(alert *Alert) string {
	severityPrefix := strings.ToUpper(string(alert.Severity))
	msg := fmt.Sprintf("[%s] %s: %s", severityPrefix, alert.Title, alert.Message)

	return truncateMessage(msg, maxSMSLength)
}

// buildAuthHeader creates the Basic auth header for Twilio API.
func (t *Twilio) buildAuthHeader() string {
	credentials := fmt.Sprintf("%s:%s", t.config.AccountSID, t.config.AuthToken)
	encoded := base64.StdEncoding.EncodeToString([]byte(credentials))
	return fmt.Sprintf("Basic %s", encoded)
}

// truncateMessage truncates a message to the specified length.
// If truncated, adds "..." to indicate continuation.
func truncateMessage(msg string, maxLen int) string {
	if len(msg) <= maxLen {
		return msg
	}

	// Leave room for "..."
	return msg[:maxLen-3] + "..."
}

// formatPhoneNumber ensures a phone number is in E.164 format.
// If the number starts with a digit, prepends "+".
func formatPhoneNumber(number string) string {
	number = strings.TrimSpace(number)
	if number == "" {
		return number
	}

	// Already has + prefix
	if strings.HasPrefix(number, "+") {
		return number
	}

	// Add + prefix for E.164 format
	return "+" + number
}

// maskPhoneNumber masks a phone number for logging (shows last 4 digits).
func maskPhoneNumber(number string) string {
	if len(number) <= 4 {
		return "****"
	}
	return "****" + number[len(number)-4:]
}
