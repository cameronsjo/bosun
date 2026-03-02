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
	assert.Equal(t, "sendgrid", sg.Name())
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
			assert.Equal(t, tt.want, sg.IsConfigured())
		})
	}
}

func TestSendGrid_Send_Success(t *testing.T) {
	var receivedReq sendGridRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer SG.test-api-key", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&receivedReq))
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
	require.NoError(t, err)

	// Verify the payload that was received by the server.
	require.Len(t, receivedReq.Personalizations, 1)
	assert.Len(t, receivedReq.Personalizations[0].To, 2)
	assert.Equal(t, "alerts@example.com", receivedReq.From.Email)
	assert.Equal(t, "[ERROR] Test Alert", receivedReq.Subject)
	assert.Len(t, receivedReq.Content, 2)
}

func TestSendGrid_Send_NotConfigured(t *testing.T) {
	sg := NewSendGrid(SendGridConfig{})
	err := sg.Send(context.Background(), &Alert{
		Title:    "Test",
		Message:  "Test",
		Severity: SeverityInfo,
	})

	require.Error(t, err, "Expected error for unconfigured SendGrid")
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
			assert.Equal(t, tt.want, sg.formatSubject(alert))
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
			assert.Equal(t, tt.wantHex, sg.getSeverityColor(tt.severity))
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
			assert.Equal(t, tt.wantHex, sg.getSeverityBgColor(tt.severity))
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

	require.Len(t, payload.Personalizations, 1)
	require.Len(t, payload.Personalizations[0].To, 3)

	expectedEmails := []string{"one@example.com", "two@example.com", "three@example.com"}
	for i, email := range expectedEmails {
		assert.Equal(t, email, payload.Personalizations[0].To[i].Email)
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

	require.Error(t, err, "Expected error for cancelled context")
}
