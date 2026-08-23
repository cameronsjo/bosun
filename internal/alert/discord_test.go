package alert

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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
			_ = json.NewDecoder(r.Body).Decode(&receivedPayload)
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

func TestDiscordProvider_Send_NetworkError(t *testing.T) {
	// Start and immediately close a server to get a guaranteed-refused local port.
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	closedURL := server.URL
	server.Close()

	p := &DiscordProvider{
		webhookURL: closedURL,
		client:     &http.Client{Timeout: 50 * time.Millisecond},
	}

	err := p.Send(context.Background(), &Alert{Title: "Test", Source: "test"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "send request")
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

func TestDiscordProvider_BoundsIndividualComponents(t *testing.T) {
	tests := []struct {
		name       string
		alert      *Alert
		value      func(discordEmbed) string
		limit      int
		wantPrefix string
	}{
		{
			name:  "title",
			alert: &Alert{Title: strings.Repeat("t", discordTitleLimit+1), Source: "test"},
			value: func(embed discordEmbed) string { return embed.Title }, limit: discordTitleLimit,
			wantPrefix: "t",
		},
		{
			name:  "description",
			alert: &Alert{Title: "test", Message: strings.Repeat("d", discordDescriptionLimit+1), Source: "test"},
			value: func(embed discordEmbed) string { return embed.Description }, limit: discordDescriptionLimit,
			wantPrefix: "d",
		},
		{
			name:  "footer",
			alert: &Alert{Title: "test", Source: strings.Repeat("s", discordFooterLimit)},
			value: func(embed discordEmbed) string { return embed.Footer.Text }, limit: discordFooterLimit,
			wantPrefix: "bosun/",
		},
		{
			name: "field name",
			alert: &Alert{Title: "test", Source: "test", Metadata: map[string]string{
				strings.Repeat("k", discordFieldNameLimit+1): "value",
			}},
			value: func(embed discordEmbed) string { return embed.Fields[0].Name }, limit: discordFieldNameLimit,
			wantPrefix: "k",
		},
		{
			name: "field value",
			alert: &Alert{Title: "test", Source: "test", Metadata: map[string]string{
				"key": strings.Repeat("v", discordFieldValueLimit+1),
			}},
			value: func(embed discordEmbed) string { return embed.Fields[0].Value }, limit: discordFieldValueLimit,
			wantPrefix: "v",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			embed := captureDiscordEmbed(t, tt.alert)
			got := tt.value(embed)

			assert.Equal(t, tt.limit, utf16Units(got))
			assert.True(t, strings.HasPrefix(got, tt.wantPrefix))
			assert.True(t, strings.HasSuffix(got, truncationSuffix))
			assert.True(t, utf8.ValidString(got))
		})
	}
}

func TestDiscordProvider_OversizedAlertRemainsDeliverable(t *testing.T) {
	var validationErr error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload discordPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			validationErr = err
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if len(payload.Embeds) != 1 {
			validationErr = fmt.Errorf("got %d embeds, want 1", len(payload.Embeds))
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		validationErr = validateDiscordEmbedBounds(payload.Embeds[0])
		if validationErr != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	metadata := make(map[string]string, 30)
	for i := 0; i < 30; i++ {
		metadata[fmt.Sprintf("field-%02d-%s", i, strings.Repeat("k", 300))] = strings.Repeat("v", 1200)
	}
	provider := NewDiscordProvider(server.URL)
	err := provider.Send(context.Background(), &Alert{
		Title:    strings.Repeat("title", 100),
		Message:  strings.Repeat("verbose compose failure\n", 400),
		Source:   strings.Repeat("source", 400),
		Severity: SeverityError,
		Metadata: metadata,
	})

	require.NoError(t, err)
	require.NoError(t, validationErr)
}

func TestDiscordProvider_PreservesIndividualBoundaries(t *testing.T) {
	tests := []struct {
		name  string
		alert *Alert
		value func(discordEmbed) string
		want  string
	}{
		{
			name: "title", alert: &Alert{Title: strings.Repeat("t", discordTitleLimit), Source: "test"},
			value: func(embed discordEmbed) string { return embed.Title }, want: strings.Repeat("t", discordTitleLimit),
		},
		{
			name: "description", alert: &Alert{Title: "test", Message: strings.Repeat("d", discordDescriptionLimit), Source: "test"},
			value: func(embed discordEmbed) string { return embed.Description }, want: strings.Repeat("d", discordDescriptionLimit),
		},
		{
			name: "footer", alert: &Alert{Title: "test", Source: strings.Repeat("s", discordFooterLimit-len("bosun/"))},
			value: func(embed discordEmbed) string { return embed.Footer.Text }, want: "bosun/" + strings.Repeat("s", discordFooterLimit-len("bosun/")),
		},
		{
			name: "field name", alert: &Alert{Title: "test", Source: "test", Metadata: map[string]string{
				strings.Repeat("k", discordFieldNameLimit): "value",
			}},
			value: func(embed discordEmbed) string { return embed.Fields[0].Name }, want: strings.Repeat("k", discordFieldNameLimit),
		},
		{
			name: "field value", alert: &Alert{Title: "test", Source: "test", Metadata: map[string]string{
				"key": strings.Repeat("v", discordFieldValueLimit),
			}},
			value: func(embed discordEmbed) string { return embed.Fields[0].Value }, want: strings.Repeat("v", discordFieldValueLimit),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			embed := captureDiscordEmbed(t, tt.alert)
			assert.Equal(t, tt.want, tt.value(embed))
		})
	}
}

func TestDiscordProvider_UsesDeterministicAggregateBudget(t *testing.T) {
	alert := &Alert{
		Title:   strings.Repeat("t", discordTitleLimit),
		Message: strings.Repeat("d", discordDescriptionLimit),
		Source:  strings.Repeat("s", 1640),
		Metadata: map[string]string{
			"a-too-large": "value",
			"z":           "v",
		},
	}

	embed := captureDiscordEmbed(t, alert)

	require.Len(t, embed.Fields, 1)
	assert.Equal(t, "z", embed.Fields[0].Name)
	assert.Equal(t, "v", embed.Fields[0].Value)
	assert.Equal(t, discordEmbedTotalLimit, discordEmbedUnits(embed))
}

func TestDiscordProvider_SortsFiltersAndLimitsMetadata(t *testing.T) {
	metadata := map[string]string{
		"":      "empty key",
		"empty": "",
	}
	for i := 29; i >= 0; i-- {
		metadata[fmt.Sprintf("key-%02d", i)] = "value"
	}

	embed := captureDiscordEmbed(t, &Alert{Title: "test", Source: "test", Metadata: metadata})

	require.Len(t, embed.Fields, discordFieldCountLimit)
	for i, field := range embed.Fields {
		assert.Equal(t, fmt.Sprintf("key-%02d", i), field.Name)
		assert.Equal(t, "value", field.Value)
	}
}

func TestDiscordProvider_UsesUTF16UnitsWithoutSplittingUnicode(t *testing.T) {
	t.Run("exact supplementary boundary remains unchanged", func(t *testing.T) {
		title := strings.Repeat("🚀", discordTitleLimit/2)
		embed := captureDiscordEmbed(t, &Alert{Title: title, Source: "test"})
		assert.Equal(t, title, embed.Title)
		assert.Equal(t, discordTitleLimit, utf16Units(embed.Title))
	})

	t.Run("supplementary overflow preserves complete code points", func(t *testing.T) {
		embed := captureDiscordEmbed(t, &Alert{
			Title:  strings.Repeat("🚀", discordTitleLimit/2+1),
			Source: "test",
		})

		assert.Equal(t, strings.Repeat("🚀", 126)+truncationSuffix, embed.Title)
		assert.LessOrEqual(t, utf16Units(embed.Title), discordTitleLimit)
		assert.True(t, utf8.ValidString(embed.Title))
	})

	t.Run("BMP Unicode uses one unit", func(t *testing.T) {
		title := strings.Repeat("é", discordTitleLimit)
		embed := captureDiscordEmbed(t, &Alert{Title: title, Source: "test"})
		assert.Equal(t, title, embed.Title)
	})
}

func captureDiscordEmbed(t *testing.T, alert *Alert) discordEmbed {
	t.Helper()

	var (
		received   discordPayload
		handlerErr error
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerErr = json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	provider := NewDiscordProvider(server.URL)
	require.NoError(t, provider.Send(context.Background(), alert))
	require.NoError(t, handlerErr)
	require.Len(t, received.Embeds, 1)
	return received.Embeds[0]
}

func discordEmbedUnits(embed discordEmbed) int {
	total := utf16Units(embed.Title) + utf16Units(embed.Description)
	if embed.Footer != nil {
		total += utf16Units(embed.Footer.Text)
	}
	for _, field := range embed.Fields {
		total += utf16Units(field.Name) + utf16Units(field.Value)
	}
	return total
}

func validateDiscordEmbedBounds(embed discordEmbed) error {
	if utf16Units(embed.Title) > discordTitleLimit {
		return fmt.Errorf("title exceeds %d units", discordTitleLimit)
	}
	if utf16Units(embed.Description) > discordDescriptionLimit {
		return fmt.Errorf("description exceeds %d units", discordDescriptionLimit)
	}
	if embed.Footer != nil && utf16Units(embed.Footer.Text) > discordFooterLimit {
		return fmt.Errorf("footer exceeds %d units", discordFooterLimit)
	}
	if len(embed.Fields) > discordFieldCountLimit {
		return fmt.Errorf("field count exceeds %d", discordFieldCountLimit)
	}
	for _, field := range embed.Fields {
		if utf16Units(field.Name) > discordFieldNameLimit {
			return fmt.Errorf("field name exceeds %d units", discordFieldNameLimit)
		}
		if utf16Units(field.Value) > discordFieldValueLimit {
			return fmt.Errorf("field value exceeds %d units", discordFieldValueLimit)
		}
	}
	if discordEmbedUnits(embed) > discordEmbedTotalLimit {
		return fmt.Errorf("embed exceeds %d aggregate units", discordEmbedTotalLimit)
	}
	return nil
}

func TestDiscordColorConstants(t *testing.T) {
	// Verify colors are in expected decimal format.
	assert.Equal(t, 0x3498db, ColorInfo)    // Blue.
	assert.Equal(t, 0xf39c12, ColorWarning) // Orange.
	assert.Equal(t, 0xe74c3c, ColorError)   // Red.
	assert.Equal(t, 0x2ecc71, ColorSuccess) // Green.
	assert.Equal(t, 0x9b59b6, ColorCritical)
}
