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

func TestSlackProvider_Name(t *testing.T) {
	p := NewSlackProvider("https://hooks.slack.com/services/test")
	assert.Equal(t, "slack", p.Name())
}

func TestSlackProvider_IsConfigured(t *testing.T) {
	t.Run("configured with URL", func(t *testing.T) {
		p := NewSlackProvider("https://hooks.slack.com/services/test")
		assert.True(t, p.IsConfigured())
	})

	t.Run("not configured with empty URL", func(t *testing.T) {
		p := NewSlackProvider("")
		// Only configured if env var is set.
		// For testing, we assume env var is not set.
		assert.False(t, p.IsConfigured())
	})
}

func TestSlackProvider_Send(t *testing.T) {
	t.Run("sends alert successfully", func(t *testing.T) {
		var receivedPayload slackPayload

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "POST", r.Method)
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

			err := json.NewDecoder(r.Body).Decode(&receivedPayload)
			require.NoError(t, err)

			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		p := NewSlackProvider(server.URL)
		alert := &Alert{
			Title:    "Test Alert",
			Message:  "This is a test message",
			Severity: SeverityWarning,
			Source:   "test",
			Metadata: map[string]string{
				"key1": "value1",
				"key2": "value2",
			},
		}

		err := p.Send(context.Background(), alert)
		require.NoError(t, err)

		require.Len(t, receivedPayload.Attachments, 1)
		attachment := receivedPayload.Attachments[0]

		assert.Equal(t, "Test Alert", attachment.Title)
		assert.Equal(t, "This is a test message", attachment.Text)
		assert.Equal(t, "#f39c12", attachment.Color) // Warning = orange.
		assert.Equal(t, "bosun/test", attachment.Footer)
		assert.NotZero(t, attachment.Ts)
		assert.Len(t, attachment.Fields, 2)
	})

	t.Run("handles 200 OK response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		p := NewSlackProvider(server.URL)
		err := p.Send(context.Background(), &Alert{Title: "Test", Source: "test"})
		assert.NoError(t, err)
	})

	t.Run("returns error on non-200 status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
		}))
		defer server.Close()

		p := NewSlackProvider(server.URL)
		err := p.Send(context.Background(), &Alert{Title: "Test", Source: "test"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unexpected status: 400")
	})

	t.Run("returns nil when not configured", func(t *testing.T) {
		p := &SlackProvider{} // No webhook URL.
		err := p.Send(context.Background(), &Alert{Title: "Test"})
		assert.NoError(t, err)
	})

	t.Run("skips empty metadata values", func(t *testing.T) {
		var receivedPayload slackPayload

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&receivedPayload)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		p := NewSlackProvider(server.URL)
		alert := &Alert{
			Title:   "Test",
			Message: "Test",
			Source:  "test",
			Metadata: map[string]string{
				"filled": "value",
				"empty":  "",
			},
		}

		err := p.Send(context.Background(), alert)
		require.NoError(t, err)

		// Should only have one field (the non-empty one).
		require.Len(t, receivedPayload.Attachments, 1)
		assert.Len(t, receivedPayload.Attachments[0].Fields, 1)
		assert.Equal(t, "filled", receivedPayload.Attachments[0].Fields[0].Title)
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			// Server never responds.
			select {}
		}))
		defer server.Close()

		p := NewSlackProvider(server.URL)

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately.

		err := p.Send(ctx, &Alert{Title: "Test", Source: "test"})
		require.Error(t, err)
	})
}

func TestSlackProvider_Send_NetworkError(t *testing.T) {
	// Start and immediately close a server to get a guaranteed-refused local port.
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	closedURL := server.URL
	server.Close()

	p := &SlackProvider{
		webhookURL: closedURL,
		client:     &http.Client{Timeout: 50 * time.Millisecond},
	}

	err := p.Send(context.Background(), &Alert{Title: "Test", Source: "test"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "send request")
}

func TestSeverityToSlackColor(t *testing.T) {
	tests := []struct {
		severity Severity
		expected string
	}{
		{SeverityInfo, "#2ecc71"},     // Green
		{SeverityWarning, "#f39c12"}, // Orange
		{SeverityError, "#e74c3c"},   // Red
		{SeverityCritical, "#9b59b6"}, // Purple
		{Severity("unknown"), "#3498db"}, // Blue (default)
	}

	for _, tc := range tests {
		t.Run(string(tc.severity), func(t *testing.T) {
			assert.Equal(t, tc.expected, severityToSlackColor(tc.severity))
		})
	}
}
