package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebhookProvider_Name(t *testing.T) {
	p := NewWebhookProvider(WebhookConfig{URL: "https://example.com/hook"})
	assert.Equal(t, "webhook", p.Name())
}

func TestWebhookProvider_IsConfigured(t *testing.T) {
	t.Run("configured with URL", func(t *testing.T) {
		p := NewWebhookProvider(WebhookConfig{URL: "https://example.com/hook"})
		assert.True(t, p.IsConfigured())
	})

	t.Run("not configured with empty URL", func(t *testing.T) {
		p := NewWebhookProvider(WebhookConfig{})
		assert.False(t, p.IsConfigured())
	})
}

func TestWebhookProvider_DefaultMethod(t *testing.T) {
	p := NewWebhookProvider(WebhookConfig{URL: "https://example.com/hook"})
	assert.Equal(t, http.MethodPost, p.config.Method)
}

func TestWebhookProvider_CustomMethod(t *testing.T) {
	p := NewWebhookProvider(WebhookConfig{
		URL:    "https://example.com/hook",
		Method: http.MethodPut,
	})
	assert.Equal(t, http.MethodPut, p.config.Method)
}

func TestWebhookProvider_Send(t *testing.T) {
	t.Run("sends alert successfully", func(t *testing.T) {
		var receivedPayload webhookPayload
		var receivedMethod string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedMethod = r.Method
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

			err := json.NewDecoder(r.Body).Decode(&receivedPayload)
			require.NoError(t, err)

			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		p := NewWebhookProvider(WebhookConfig{URL: server.URL})
		alert := &Alert{
			Title:    "Test Alert",
			Message:  "This is a test message",
			Severity: SeverityWarning,
			Source:   "reconcile",
			Metadata: map[string]string{
				"commit": "abc123",
				"target": "prod",
			},
		}

		err := p.Send(context.Background(), alert)
		require.NoError(t, err)

		assert.Equal(t, "POST", receivedMethod)
		assert.Equal(t, "reconcile", receivedPayload.Event)
		assert.Equal(t, "Test Alert", receivedPayload.Title)
		assert.Equal(t, "This is a test message", receivedPayload.Message)
		assert.Equal(t, "warning", receivedPayload.Severity)
		assert.NotEmpty(t, receivedPayload.Timestamp)
		assert.Equal(t, "abc123", receivedPayload.Metadata["commit"])
		assert.Equal(t, "prod", receivedPayload.Metadata["target"])
	})

	t.Run("sends with custom headers", func(t *testing.T) {
		var receivedAuth string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedAuth = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		p := NewWebhookProvider(WebhookConfig{
			URL: server.URL,
			Headers: map[string]string{
				"Authorization": "Bearer my-token",
			},
		})

		err := p.Send(context.Background(), &Alert{Title: "Test", Source: "test"})
		require.NoError(t, err)
		assert.Equal(t, "Bearer my-token", receivedAuth)
	})

	t.Run("sends with custom method", func(t *testing.T) {
		var receivedMethod string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedMethod = r.Method
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		p := NewWebhookProvider(WebhookConfig{
			URL:    server.URL,
			Method: http.MethodPut,
		})

		err := p.Send(context.Background(), &Alert{Title: "Test", Source: "test"})
		require.NoError(t, err)
		assert.Equal(t, "PUT", receivedMethod)
	})

	t.Run("allows content-type override via headers", func(t *testing.T) {
		var receivedContentType string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedContentType = r.Header.Get("Content-Type")
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		p := NewWebhookProvider(WebhookConfig{
			URL: server.URL,
			Headers: map[string]string{
				"Content-Type": "application/json; charset=utf-8",
			},
		})

		err := p.Send(context.Background(), &Alert{Title: "Test", Source: "test"})
		require.NoError(t, err)
		assert.Equal(t, "application/json; charset=utf-8", receivedContentType)
	})

	t.Run("returns error on non-2xx status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
		}))
		defer server.Close()

		p := NewWebhookProvider(WebhookConfig{URL: server.URL})
		err := p.Send(context.Background(), &Alert{Title: "Test", Source: "test"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unexpected status: 400")
	})

	t.Run("accepts any 2xx status", func(t *testing.T) {
		for _, code := range []int{200, 201, 202, 204} {
			t.Run(http.StatusText(code), func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(code)
				}))
				defer server.Close()

				p := NewWebhookProvider(WebhookConfig{URL: server.URL})
				err := p.Send(context.Background(), &Alert{Title: "Test", Source: "test"})
				assert.NoError(t, err)
			})
		}
	})

	t.Run("returns nil when not configured", func(t *testing.T) {
		p := &WebhookProvider{}
		err := p.Send(context.Background(), &Alert{Title: "Test"})
		assert.NoError(t, err)
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			select {}
		}))
		defer server.Close()

		p := NewWebhookProvider(WebhookConfig{URL: server.URL})

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := p.Send(ctx, &Alert{Title: "Test", Source: "test"})
		require.Error(t, err)
	})
}

func TestWebhookProvider_Send_NetworkError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	closedURL := server.URL
	server.Close()

	p := &WebhookProvider{
		config: WebhookConfig{URL: closedURL, Method: http.MethodPost},
		client: &http.Client{Timeout: 50 * time.Millisecond},
	}

	err := p.Send(context.Background(), &Alert{Title: "Test", Source: "test"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "send request")
}

func TestWebhookProvider_PayloadStructure(t *testing.T) {
	var receivedBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(r.Body)
		receivedBody = buf.Bytes()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	p := NewWebhookProvider(WebhookConfig{URL: server.URL})
	testAlert := &Alert{
		Title:    "Deploy Success",
		Message:  "Deployed commit abc123",
		Severity: SeverityInfo,
		Source:   "reconcile",
	}

	err := p.Send(context.Background(), testAlert)
	require.NoError(t, err)

	var payload map[string]interface{}
	err = json.Unmarshal(receivedBody, &payload)
	require.NoError(t, err)

	assert.Contains(t, payload, "event")
	assert.Contains(t, payload, "title")
	assert.Contains(t, payload, "message")
	assert.Contains(t, payload, "severity")
	assert.Contains(t, payload, "timestamp")

	assert.Equal(t, "reconcile", payload["event"])
	assert.Equal(t, "Deploy Success", payload["title"])
	assert.Equal(t, "Deployed commit abc123", payload["message"])
	assert.Equal(t, "info", payload["severity"])
}
