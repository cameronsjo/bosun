package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"
)

const sendGridAPIEndpoint = "https://api.sendgrid.com/v3/mail/send"

// SendGridConfig holds configuration for the SendGrid email provider.
type SendGridConfig struct {
	APIKey    string   // SENDGRID_API_KEY
	FromEmail string   // Sender email address
	FromName  string   // Sender name (e.g., "Bosun Alerts")
	ToEmails  []string // Recipient email addresses
}

// SendGrid implements the Provider interface for email alerts via SendGrid.
type SendGrid struct {
	config SendGridConfig
	client *http.Client
}

// NewSendGrid creates a new SendGrid alert provider.
func NewSendGrid(config SendGridConfig) *SendGrid {
	return &SendGrid{
		config: config,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// Name returns the provider name.
func (s *SendGrid) Name() string {
	return "sendgrid"
}

// IsConfigured returns true if SendGrid is properly configured.
func (s *SendGrid) IsConfigured() bool {
	return s.config.APIKey != "" &&
		s.config.FromEmail != "" &&
		len(s.config.ToEmails) > 0
}

// Send sends an alert via SendGrid email.
func (s *SendGrid) Send(ctx context.Context, alert *Alert) error {
	if !s.IsConfigured() {
		return fmt.Errorf("sendgrid not configured")
	}

	payload := s.buildPayload(alert)
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sendGridAPIEndpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+s.config.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	// SendGrid returns 202 Accepted on success
	if resp.StatusCode != http.StatusAccepted {
		var errResp sendGridErrorResponse
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err == nil && len(errResp.Errors) > 0 {
			return fmt.Errorf("sendgrid api error (status %d): %s", resp.StatusCode, errResp.Errors[0].Message)
		}
		return fmt.Errorf("sendgrid api error: status %d", resp.StatusCode)
	}

	return nil
}

// sendGridRequest represents the SendGrid v3 API request structure.
type sendGridRequest struct {
	Personalizations []sendGridPersonalization `json:"personalizations"`
	From             sendGridEmail             `json:"from"`
	Subject          string                    `json:"subject"`
	Content          []sendGridContent         `json:"content"`
}

type sendGridPersonalization struct {
	To []sendGridEmail `json:"to"`
}

type sendGridEmail struct {
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
}

type sendGridContent struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type sendGridErrorResponse struct {
	Errors []struct {
		Message string `json:"message"`
		Field   string `json:"field,omitempty"`
	} `json:"errors"`
}

// buildPayload constructs the SendGrid API request payload.
func (s *SendGrid) buildPayload(alert *Alert) sendGridRequest {
	toEmails := make([]sendGridEmail, len(s.config.ToEmails))
	for i, email := range s.config.ToEmails {
		toEmails[i] = sendGridEmail{Email: email}
	}

	subject := s.formatSubject(alert)
	htmlBody := s.formatHTMLBody(alert)
	plainBody := s.formatPlainBody(alert)

	return sendGridRequest{
		Personalizations: []sendGridPersonalization{
			{To: toEmails},
		},
		From: sendGridEmail{
			Email: s.config.FromEmail,
			Name:  s.config.FromName,
		},
		Subject: subject,
		Content: []sendGridContent{
			{Type: "text/plain", Value: plainBody},
			{Type: "text/html", Value: htmlBody},
		},
	}
}

// formatSubject creates the email subject line with severity prefix.
func (s *SendGrid) formatSubject(alert *Alert) string {
	prefix := ""
	switch alert.Severity {
	case SeverityCritical:
		prefix = "[CRITICAL] "
	case SeverityError:
		prefix = "[ERROR] "
	case SeverityWarning:
		prefix = "[WARNING] "
	case SeverityInfo:
		prefix = "[INFO] "
	}
	return prefix + alert.Title
}

// formatHTMLBody creates an HTML email body with styling.
func (s *SendGrid) formatHTMLBody(alert *Alert) string {
	var sb strings.Builder

	severityColor := s.getSeverityColor(alert.Severity)
	severityBg := s.getSeverityBgColor(alert.Severity)

	sb.WriteString(`<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
</head>
<body style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif; margin: 0; padding: 20px; background-color: #f5f5f5;">
<div style="max-width: 600px; margin: 0 auto; background-color: #ffffff; border-radius: 8px; overflow: hidden; box-shadow: 0 2px 4px rgba(0,0,0,0.1);">
`)

	// Header with severity color
	sb.WriteString(fmt.Sprintf(`<div style="background-color: %s; color: #ffffff; padding: 20px;">
<h1 style="margin: 0; font-size: 20px;">%s</h1>
<span style="display: inline-block; margin-top: 8px; padding: 4px 12px; background-color: rgba(255,255,255,0.2); border-radius: 4px; font-size: 12px; text-transform: uppercase;">%s</span>
</div>
`, severityColor, html.EscapeString(alert.Title), html.EscapeString(string(alert.Severity))))

	// Body
	sb.WriteString(`<div style="padding: 20px;">`)

	// Message
	sb.WriteString(fmt.Sprintf(`<div style="background-color: %s; border-left: 4px solid %s; padding: 15px; margin-bottom: 20px; border-radius: 0 4px 4px 0;">
<p style="margin: 0; color: #333333; line-height: 1.5;">%s</p>
</div>
`, severityBg, severityColor, html.EscapeString(alert.Message)))

	// Metadata
	if len(alert.Metadata) > 0 {
		sb.WriteString(`<h2 style="font-size: 14px; color: #666666; margin: 0 0 10px 0; text-transform: uppercase; letter-spacing: 0.5px;">Details</h2>
<table style="width: 100%; border-collapse: collapse;">
`)
		for key, value := range alert.Metadata {
			sb.WriteString(fmt.Sprintf(`<tr>
<td style="padding: 8px 12px; border-bottom: 1px solid #eeeeee; color: #666666; font-weight: 600; width: 120px;">%s</td>
<td style="padding: 8px 12px; border-bottom: 1px solid #eeeeee; color: #333333; font-family: 'Menlo', 'Monaco', 'Courier New', monospace; font-size: 13px;">%s</td>
</tr>
`, html.EscapeString(key), html.EscapeString(value)))
		}
		sb.WriteString(`</table>`)
	}

	sb.WriteString(`</div>`)

	// Footer
	sb.WriteString(fmt.Sprintf(`<div style="background-color: #f8f8f8; padding: 15px 20px; border-top: 1px solid #eeeeee; font-size: 12px; color: #999999;">
Source: %s | Sent by Bosun
</div>
`, html.EscapeString(alert.Source)))

	sb.WriteString(`</div>
</body>
</html>`)

	return sb.String()
}

// formatPlainBody creates a plain text email body.
func (s *SendGrid) formatPlainBody(alert *Alert) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("%s\n", alert.Title))
	sb.WriteString(fmt.Sprintf("Severity: %s\n", alert.Severity))
	sb.WriteString(strings.Repeat("=", 50) + "\n\n")
	sb.WriteString(alert.Message + "\n")

	if len(alert.Metadata) > 0 {
		sb.WriteString("\nDetails:\n")
		sb.WriteString(strings.Repeat("-", 30) + "\n")
		for key, value := range alert.Metadata {
			sb.WriteString(fmt.Sprintf("  %s: %s\n", key, value))
		}
	}

	sb.WriteString(fmt.Sprintf("\n---\nSource: %s | Sent by Bosun\n", alert.Source))

	return sb.String()
}

// getSeverityColor returns the primary color for the given severity.
func (s *SendGrid) getSeverityColor(severity Severity) string {
	switch severity {
	case SeverityCritical:
		return "#dc2626" // red-600
	case SeverityError:
		return "#ea580c" // orange-600
	case SeverityWarning:
		return "#ca8a04" // yellow-600
	case SeverityInfo:
		return "#2563eb" // blue-600
	default:
		return "#6b7280" // gray-500
	}
}

// getSeverityBgColor returns the background color for the given severity.
func (s *SendGrid) getSeverityBgColor(severity Severity) string {
	switch severity {
	case SeverityCritical:
		return "#fef2f2" // red-50
	case SeverityError:
		return "#fff7ed" // orange-50
	case SeverityWarning:
		return "#fefce8" // yellow-50
	case SeverityInfo:
		return "#eff6ff" // blue-50
	default:
		return "#f9fafb" // gray-50
	}
}
