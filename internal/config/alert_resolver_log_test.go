package config

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/cameronsjo/bosun/internal/log"
)

// TestAlertConfig_DroppedWebhookHeadersAreWarnedAbout pins that an operator who
// keeps the webhook URL in bosun.yaml and the header in the environment is told,
// at the default log level, why no header is sent. BOSUN_WEBHOOK_HEADERS applies
// only together with BOSUN_WEBHOOK_URL. The warning names the variables and
// never their values: the header is usually an Authorization secret.
func TestAlertConfig_DroppedWebhookHeadersAreWarnedAbout(t *testing.T) {
	const secret = "Bearer env-secret-value"
	const fileURL = "alerts:\n  webhook_url: https://file.example.com/hook\n"

	capture := func(t *testing.T) *bytes.Buffer {
		t.Helper()
		var logs bytes.Buffer
		log.Init(&log.Options{Format: log.FormatJSON, Output: &logs})
		t.Cleanup(func() { log.Init(&log.Options{}) })
		return &logs
	}

	t.Run("headers with a URL from the file warn, naming the variables and no value", func(t *testing.T) {
		logs := capture(t)
		clearAlertResolverEnv(t)
		t.Setenv("BOSUN_WEBHOOK_HEADERS", `{"Authorization":"`+secret+`"}`)

		got := alertConfigFrom(t, fileURL)

		assert.Equal(t, "https://file.example.com/hook", got.WebhookURL)
		assert.Nil(t, got.WebhookHeaders)
		out := logs.String()
		assert.Contains(t, out, `"level":"warn"`, "the default log level must show it")
		assert.Contains(t, out, "BOSUN_WEBHOOK_HEADERS")
		assert.Contains(t, out, "BOSUN_WEBHOOK_URL", "the message says what the headers apply with")
		assert.NotContains(t, out, secret, "the header value must not be logged")
		assert.NotContains(t, out, "Authorization", "nor its name")
	})

	t.Run("headers with an environment URL do not warn", func(t *testing.T) {
		logs := capture(t)
		clearAlertResolverEnv(t)
		t.Setenv("BOSUN_WEBHOOK_URL", "https://env.example.com/hook")
		t.Setenv("BOSUN_WEBHOOK_HEADERS", `{"Authorization":"`+secret+`"}`)

		got := alertConfigFrom(t, fileURL)

		assert.Equal(t, map[string]string{"Authorization": secret}, got.WebhookHeaders)
		assert.NotContains(t, logs.String(), `"level":"warn"`)
	})

	t.Run("no headers variable, no warning", func(t *testing.T) {
		logs := capture(t)
		clearAlertResolverEnv(t)

		alertConfigFrom(t, fileURL)

		assert.NotContains(t, logs.String(), "BOSUN_WEBHOOK_HEADERS")
	})
}
