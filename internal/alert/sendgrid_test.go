package alert

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSendGrid_Name(t *testing.T) {
	sg := NewSendGrid(SendGridConfig{})
	if got := sg.Name(); got != "sendgrid" {
		t.Errorf("Name() = %q, want %q", got, "sendgrid")
	}
}

func TestSendGrid_IsConfigured(t *testing.T) {
	tests := []struct {
		name   string
		config SendGridConfig
		want   bool
	}{
		{
			name:   "empty config",
			config: SendGridConfig{},
			want:   false,
		},
		{
			name: "missing API key",
			config: SendGridConfig{
				FromEmail: "sender@example.com",
				ToEmails:  []string{"recipient@example.com"},
			},
			want: false,
		},
		{
			name: "missing from email",
			config: SendGridConfig{
				APIKey:   "SG.test-key",
				ToEmails: []string{"recipient@example.com"},
			},
			want: false,
		},
		{
			name: "missing to emails",
			config: SendGridConfig{
				APIKey:    "SG.test-key",
				FromEmail: "sender@example.com",
				ToEmails:  []string{},
			},
			want: false,
		},
		{
			name: "nil to emails",
			config: SendGridConfig{
				APIKey:    "SG.test-key",
				FromEmail: "sender@example.com",
				ToEmails:  nil,
			},
			want: false,
		},
		{
			name: "fully configured",
			config: SendGridConfig{
				APIKey:    "SG.test-key",
				FromEmail: "sender@example.com",
				FromName:  "Bosun Alerts",
				ToEmails:  []string{"recipient@example.com"},
			},
			want: true,
		},
		{
			name: "multiple recipients",
			config: SendGridConfig{
				APIKey:    "SG.test-key",
				FromEmail: "sender@example.com",
				ToEmails:  []string{"one@example.com", "two@example.com"},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sg := NewSendGrid(tt.config)
			if got := sg.IsConfigured(); got != tt.want {
				t.Errorf("IsConfigured() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSendGrid_Send_Success(t *testing.T) {
	var receivedReq sendGridRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer SG.test-api-key" {
			t.Errorf("Authorization = %q, want %q", r.Header.Get("Authorization"), "Bearer SG.test-api-key")
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q, want %q", r.Header.Get("Content-Type"), "application/json")
		}
		if err := json.NewDecoder(r.Body).Decode(&receivedReq); err != nil {
			t.Fatalf("Failed to decode request body: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	sg := &SendGrid{
		config: SendGridConfig{
			APIKey:    "SG.test-api-key",
			FromEmail: "alerts@example.com",
			FromName:  "Bosun Alerts",
			ToEmails:  []string{"ops@example.com", "backup@example.com"},
		},
		client:   server.Client(),
		endpoint: server.URL,
	}

	alert := &Alert{
		Title:    "Test Alert",
		Message:  "This is a test message",
		Severity: SeverityError,
		Source:   "test",
		Metadata: map[string]string{"key1": "value1", "key2": "value2"},
	}

	err := sg.Send(context.Background(), alert)
	if err != nil {
		t.Fatalf("Send() returned unexpected error: %v", err)
	}

	// Verify the payload that was received by the server.
	if len(receivedReq.Personalizations) != 1 {
		t.Errorf("Personalizations count = %d, want 1", len(receivedReq.Personalizations))
	}
	if len(receivedReq.Personalizations[0].To) != 2 {
		t.Errorf("To recipients = %d, want 2", len(receivedReq.Personalizations[0].To))
	}
	if receivedReq.From.Email != "alerts@example.com" {
		t.Errorf("From.Email = %q, want %q", receivedReq.From.Email, "alerts@example.com")
	}
	if receivedReq.Subject != "[ERROR] Test Alert" {
		t.Errorf("Subject = %q, want %q", receivedReq.Subject, "[ERROR] Test Alert")
	}
	if len(receivedReq.Content) != 2 {
		t.Errorf("Content count = %d, want 2", len(receivedReq.Content))
	}
}

func TestSendGrid_Send_NotConfigured(t *testing.T) {
	sg := NewSendGrid(SendGridConfig{})
	err := sg.Send(context.Background(), &Alert{
		Title:    "Test",
		Message:  "Test",
		Severity: SeverityInfo,
	})

	if err == nil {
		t.Error("Expected error for unconfigured SendGrid")
	}
}

func TestSendGrid_Send_APIErrorWithJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"errors": []map[string]string{
				{"message": "Invalid email address", "field": "to"},
			},
		})
	}))
	defer server.Close()

	sg := &SendGrid{
		config: SendGridConfig{
			APIKey:    "SG.test-key",
			FromEmail: "sender@example.com",
			ToEmails:  []string{"invalid"},
		},
		client:   server.Client(),
		endpoint: server.URL,
	}

	err := sg.Send(context.Background(), &Alert{
		Title:    "Test",
		Message:  "Test",
		Severity: SeverityCritical,
	})
	require.Error(t, err, "Send() should return error for API error response")
	assert.Contains(t, err.Error(), "Invalid email address")
}

func TestSendGrid_Send_APIErrorNonJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal server error"))
	}))
	defer server.Close()

	sg := &SendGrid{
		config: SendGridConfig{
			APIKey:    "SG.test-key",
			FromEmail: "sender@example.com",
			ToEmails:  []string{"recipient@example.com"},
		},
		client:   server.Client(),
		endpoint: server.URL,
	}

	err := sg.Send(context.Background(), &Alert{
		Title:    "Test",
		Message:  "Test",
		Severity: SeverityInfo,
	})
	require.Error(t, err, "Send() should return error for non-2xx status")
	assert.Contains(t, err.Error(), "status 500")
}

func TestSendGrid_formatSubject(t *testing.T) {
	sg := NewSendGrid(SendGridConfig{})

	tests := []struct {
		severity Severity
		title    string
		want     string
	}{
		{SeverityCritical, "Alert", "[CRITICAL] Alert"},
		{SeverityError, "Alert", "[ERROR] Alert"},
		{SeverityWarning, "Alert", "[WARNING] Alert"},
		{SeverityInfo, "Alert", "[INFO] Alert"},
		{Severity("unknown"), "Alert", "Alert"},
	}

	for _, tt := range tests {
		t.Run(string(tt.severity), func(t *testing.T) {
			alert := &Alert{Title: tt.title, Severity: tt.severity}
			if got := sg.formatSubject(alert); got != tt.want {
				t.Errorf("formatSubject() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSendGrid_formatPlainBody(t *testing.T) {
	sg := NewSendGrid(SendGridConfig{})

	alert := &Alert{
		Title:    "Test Alert",
		Message:  "Something happened",
		Severity: SeverityError,
		Source:   "test-source",
		Metadata: map[string]string{"commit": "abc123"},
	}

	body := sg.formatPlainBody(alert)

	assert.Contains(t, body, "Test Alert")
	assert.Contains(t, body, "error")
	assert.Contains(t, body, "Something happened")
	assert.Contains(t, body, "commit: abc123")
	assert.Contains(t, body, "test-source")
}

func TestSendGrid_formatHTMLBody(t *testing.T) {
	sg := NewSendGrid(SendGridConfig{})

	alert := &Alert{
		Title:    "Test Alert",
		Message:  "Something <script>bad</script> happened",
		Severity: SeverityError,
		Source:   "test-source",
		Metadata: map[string]string{"commit": "abc123"},
	}

	body := sg.formatHTMLBody(alert)

	assert.Contains(t, body, "Test Alert")
	assert.Contains(t, body, "#ea580c") // orange-600 for error
	assert.Contains(t, body, "&lt;script&gt;")
	assert.Contains(t, body, "commit")
	assert.Contains(t, body, "abc123")
}

func TestSendGrid_getSeverityColor(t *testing.T) {
	sg := NewSendGrid(SendGridConfig{})

	tests := []struct {
		severity Severity
		wantHex  string
	}{
		{SeverityCritical, "#dc2626"},
		{SeverityError, "#ea580c"},
		{SeverityWarning, "#ca8a04"},
		{SeverityInfo, "#2563eb"},
		{Severity("unknown"), "#6b7280"},
	}

	for _, tt := range tests {
		t.Run(string(tt.severity), func(t *testing.T) {
			if got := sg.getSeverityColor(tt.severity); got != tt.wantHex {
				t.Errorf("getSeverityColor(%s) = %q, want %q", tt.severity, got, tt.wantHex)
			}
		})
	}
}

func TestSendGrid_getSeverityBgColor(t *testing.T) {
	sg := NewSendGrid(SendGridConfig{})

	tests := []struct {
		severity Severity
		wantHex  string
	}{
		{SeverityCritical, "#fef2f2"},
		{SeverityError, "#fff7ed"},
		{SeverityWarning, "#fefce8"},
		{SeverityInfo, "#eff6ff"},
		{Severity("unknown"), "#f9fafb"},
	}

	for _, tt := range tests {
		t.Run(string(tt.severity), func(t *testing.T) {
			if got := sg.getSeverityBgColor(tt.severity); got != tt.wantHex {
				t.Errorf("getSeverityBgColor(%s) = %q, want %q", tt.severity, got, tt.wantHex)
			}
		})
	}
}

func TestSendGrid_buildPayload_MultipleRecipients(t *testing.T) {
	sg := NewSendGrid(SendGridConfig{
		APIKey:    "SG.test-key",
		FromEmail: "sender@example.com",
		FromName:  "Test Sender",
		ToEmails:  []string{"one@example.com", "two@example.com", "three@example.com"},
	})

	alert := &Alert{
		Title:    "Multi-recipient test",
		Message:  "Test message",
		Severity: SeverityInfo,
		Source:   "test",
	}

	payload := sg.buildPayload(alert)

	if len(payload.Personalizations) != 1 {
		t.Fatalf("Expected 1 personalization, got %d", len(payload.Personalizations))
	}

	recipients := payload.Personalizations[0].To
	if len(recipients) != 3 {
		t.Errorf("Expected 3 recipients, got %d", len(recipients))
	}

	expectedEmails := []string{"one@example.com", "two@example.com", "three@example.com"}
	for i, email := range expectedEmails {
		if recipients[i].Email != email {
			t.Errorf("Recipient[%d] = %q, want %q", i, recipients[i].Email, email)
		}
	}
}

func TestSendGrid_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Simulate slow response.
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	sg := &SendGrid{
		config: SendGridConfig{
			APIKey:    "SG.test-key",
			FromEmail: "sender@example.com",
			ToEmails:  []string{"recipient@example.com"},
		},
		client:   server.Client(),
		endpoint: server.URL,
	}

	// Context already cancelled.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := sg.Send(ctx, &Alert{
		Title:    "Test",
		Message:  "Test",
		Severity: SeverityInfo,
	})

	if err == nil {
		t.Error("Expected error for cancelled context")
	}
}

