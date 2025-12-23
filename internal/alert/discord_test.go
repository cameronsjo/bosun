package alert

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscordProvider_Name(t *testing.T) {
	p := NewDiscordProvider("https://example.com/webhook")
	assert.Equal(t, "discord", p.Name())
}

func TestDiscordProvider_IsConfigured(t *testing.T) {
	t.Run("configured with URL", func(t *testing.T) {
		p := NewDiscordProvider("https://example.com/webhook")
		assert.True(t, p.IsConfigured())
	})

	t.Run("not configured with empty URL", func(t *testing.T) {
		p := NewDiscordProvider("")
		// Only configured if env var is set.
		// For testing, we assume env var is not set.
		assert.False(t, p.IsConfigured())
	})
}

func TestDiscordProvider_Send(t *testing.T) {
	t.Run("sends alert successfully", func(t *testing.T) {
		var receivedPayload discordPayload

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "POST", r.Method)
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

			err := json.NewDecoder(r.Body).Decode(&receivedPayload)
			require.NoError(t, err)

			w.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()

		p := NewDiscordProvider(server.URL)
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

		require.Len(t, receivedPayload.Embeds, 1)
		embed := receivedPayload.Embeds[0]

		assert.Equal(t, "Test Alert", embed.Title)
		assert.Equal(t, "This is a test message", embed.Description)
		assert.Equal(t, ColorWarning, embed.Color)
		assert.Equal(t, "bosun/test", embed.Footer.Text)
		assert.NotEmpty(t, embed.Timestamp)
		assert.Len(t, embed.Fields, 2)
	})

	t.Run("handles 200 OK response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		p := NewDiscordProvider(server.URL)
		err := p.Send(context.Background(), &Alert{Title: "Test", Source: "test"})
		assert.NoError(t, err)
	})

	t.Run("returns error on non-2xx status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
		}))
		defer server.Close()

		p := NewDiscordProvider(server.URL)
		err := p.Send(context.Background(), &Alert{Title: "Test", Source: "test"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unexpected status: 400")
	})

	t.Run("returns nil when not configured", func(t *testing.T) {
		p := &DiscordProvider{} // No webhook URL.
		err := p.Send(context.Background(), &Alert{Title: "Test"})
		assert.NoError(t, err)
	})

	t.Run("skips empty metadata values", func(t *testing.T) {
		var receivedPayload discordPayload

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewDecoder(r.Body).Decode(&receivedPayload)
			w.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()

		p := NewDiscordProvider(server.URL)
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
		require.Len(t, receivedPayload.Embeds, 1)
		assert.Len(t, receivedPayload.Embeds[0].Fields, 1)
		assert.Equal(t, "filled", receivedPayload.Embeds[0].Fields[0].Name)
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			// Server never responds.
			select {}
		}))
		defer server.Close()

		p := NewDiscordProvider(server.URL)

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately.

		err := p.Send(ctx, &Alert{Title: "Test", Source: "test"})
		require.Error(t, err)
	})
}

func TestSeverityToColor(t *testing.T) {
	tests := []struct {
		severity Severity
		expected int
	}{
		{SeverityInfo, ColorSuccess},
		{SeverityWarning, ColorWarning},
		{SeverityError, ColorError},
		{SeverityCritical, ColorCritical},
		{Severity("unknown"), ColorInfo},
	}

	for _, tc := range tests {
		t.Run(string(tc.severity), func(t *testing.T) {
			assert.Equal(t, tc.expected, severityToColor(tc.severity))
		})
	}
}

func TestTruncateString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{"short string", "hello", 10, "hello"},
		{"exact length", "hello", 5, "hello"},
		{"needs truncation", "hello world", 8, "hello..."},
		{"very long", "this is a very long string that needs truncation", 20, "this is a very lo..."},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := truncateString(tc.input, tc.maxLen)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestDiscordColorConstants(t *testing.T) {
	// Verify colors are in expected decimal format.
	assert.Equal(t, 0x3498db, ColorInfo)    // Blue.
	assert.Equal(t, 0xf39c12, ColorWarning) // Orange.
	assert.Equal(t, 0xe74c3c, ColorError)   // Red.
	assert.Equal(t, 0x2ecc71, ColorSuccess) // Green.
	assert.Equal(t, 0x9b59b6, ColorCritical)
}
