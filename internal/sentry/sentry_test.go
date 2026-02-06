package sentry

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/getsentry/sentry-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigFromEnv_Defaults(t *testing.T) {
	// Clear any existing env vars.
	os.Unsetenv("BOSUN_SENTRY_DSN")
	os.Unsetenv("BOSUN_SENTRY_ENVIRONMENT")
	os.Unsetenv("BOSUN_SENTRY_TRACES_SAMPLE_RATE")
	os.Unsetenv("BOSUN_DAEMON_MODE")

	opts := ConfigFromEnv()

	assert.Empty(t, opts.DSN)
	assert.Equal(t, "development", opts.Environment)
	assert.Equal(t, 0.1, opts.TracesSampleRate)
}

func TestConfigFromEnv_WithValues(t *testing.T) {
	t.Setenv("BOSUN_SENTRY_DSN", "https://key@sentry.io/123")
	t.Setenv("BOSUN_SENTRY_ENVIRONMENT", "staging")
	t.Setenv("BOSUN_SENTRY_TRACES_SAMPLE_RATE", "0.5")

	opts := ConfigFromEnv()

	assert.Equal(t, "https://key@sentry.io/123", opts.DSN)
	assert.Equal(t, "staging", opts.Environment)
	assert.Equal(t, 0.5, opts.TracesSampleRate)
}

func TestConfigFromEnv_DaemonModeAutoDetect(t *testing.T) {
	t.Setenv("BOSUN_DAEMON_MODE", "true")
	os.Unsetenv("BOSUN_SENTRY_ENVIRONMENT")

	opts := ConfigFromEnv()

	assert.Equal(t, "production", opts.Environment)
}

func TestConfigFromEnv_InvalidTraceRate(t *testing.T) {
	t.Setenv("BOSUN_SENTRY_TRACES_SAMPLE_RATE", "notanumber")

	opts := ConfigFromEnv()

	// Should fall back to default.
	assert.Equal(t, 0.1, opts.TracesSampleRate)
}

func TestConfigFromEnv_OutOfRangeTraceRate(t *testing.T) {
	t.Setenv("BOSUN_SENTRY_TRACES_SAMPLE_RATE", "2.0")

	opts := ConfigFromEnv()

	// Out of range, should keep default.
	assert.Equal(t, 0.1, opts.TracesSampleRate)
}

func TestInit_EmptyDSN(t *testing.T) {
	err := Init(Options{DSN: ""})

	assert.NoError(t, err)
	assert.False(t, Enabled())
	assert.Nil(t, Writer())
}

func TestInit_InvalidDSN(t *testing.T) {
	// Reset state from any previous test.
	state.enabled = false
	state.writer = nil
	state.closer = nil

	err := Init(Options{DSN: "not-a-valid-dsn"})

	// sentry.Init returns error for invalid DSNs.
	assert.Error(t, err)
	assert.False(t, Enabled())
}

func TestInit_ValidDSN(t *testing.T) {
	// Reset state.
	state.enabled = false
	state.writer = nil
	state.closer = nil

	// Use the Sentry test DSN format.
	err := Init(Options{
		DSN:              "https://key@sentry.io/123",
		Environment:      "test",
		Release:          "bosun@test",
		TracesSampleRate: 0.0,
	})

	require.NoError(t, err)
	assert.True(t, Enabled())
	assert.NotNil(t, Writer())

	// Clean up.
	Close(0)
	assert.False(t, Enabled())
}

func TestClose_WhenDisabled(t *testing.T) {
	state.enabled = false
	state.writer = nil
	state.closer = nil

	// Should not panic.
	Close(0)
}

func TestRecover_WhenDisabled(t *testing.T) {
	state.enabled = false

	// Should not panic.
	Recover()
}

func TestBeforeSend_PassesNormalEvents(t *testing.T) {
	event := &sentry.Event{
		Message: "something broke",
	}

	result := beforeSend(event, &sentry.EventHint{})

	assert.NotNil(t, result, "normal events should pass through")
}

func TestBeforeSend_DropsAlreadyUpToDate(t *testing.T) {
	event := &sentry.Event{
		Exception: []sentry.Exception{
			{Value: "already up to date"},
		},
	}

	result := beforeSend(event, &sentry.EventHint{})

	assert.Nil(t, result, "already up to date events should be dropped")
}

func TestBeforeSend_DropsAlreadyUpToDateFromHint(t *testing.T) {
	event := &sentry.Event{Message: "git error"}
	hint := &sentry.EventHint{
		OriginalException: errors.New("Already up to date"),
	}

	result := beforeSend(event, hint)

	assert.Nil(t, result, "already up to date from hint should be dropped")
}

func TestReconcileTransaction_WhenDisabled(t *testing.T) {
	state.enabled = false

	ctx := context.Background()
	newCtx, finish := ReconcileTransaction(ctx, "test")

	// Should return same context and no-op finish.
	assert.Equal(t, ctx, newCtx)
	finish(nil)          // Should not panic.
	finish(errors.New("err")) // Should not panic.
}

func TestStartSpan_WhenDisabled(t *testing.T) {
	state.enabled = false

	ctx := context.Background()
	newCtx, finish := StartSpan(ctx, "op", "desc")

	assert.Equal(t, ctx, newCtx)
	finish(nil)
	finish(errors.New("err"))
}

func TestStartSpan_NoParentTransaction(t *testing.T) {
	// Temporarily enable to test the "no parent" path.
	state.enabled = true
	defer func() { state.enabled = false }()

	ctx := context.Background() // No transaction in context.
	newCtx, finish := StartSpan(ctx, "op", "desc")

	// Should return same context (no parent to attach to).
	assert.Equal(t, ctx, newCtx)
	finish(nil)
}
