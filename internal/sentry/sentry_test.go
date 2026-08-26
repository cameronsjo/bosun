package sentry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetSentryState(t *testing.T) {
	t.Helper()
	_ = Close(time.Second)
	state.mu.Lock()
	state.enabled = false
	state.writer = nil
	state.flusher = nil
	state.closer = nil
	state.mu.Unlock()
	t.Cleanup(func() {
		_ = Close(time.Second)
		state.mu.Lock()
		state.enabled = false
		state.writer = nil
		state.flusher = nil
		state.closer = nil
		state.mu.Unlock()
	})
}

func installSentryState(writer io.Writer, flusher func(time.Duration) bool, closer func() error) {
	state.mu.Lock()
	state.enabled = true
	state.writer = writer
	state.flusher = flusher
	state.closer = closer
	state.mu.Unlock()
}

func TestConfigFromEnv_Defaults(t *testing.T) {
	// Clear any existing env vars via t.Setenv (auto-restores after test).
	t.Setenv("BOSUN_SENTRY_DSN", "")
	t.Setenv("BOSUN_SENTRY_ENVIRONMENT", "")
	t.Setenv("BOSUN_SENTRY_TRACES_SAMPLE_RATE", "")
	t.Setenv("BOSUN_DAEMON_MODE", "")

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
	t.Setenv("BOSUN_SENTRY_ENVIRONMENT", "")

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
	resetSentryState(t)
	err := Init(Options{DSN: ""})

	assert.NoError(t, err)
	assert.False(t, Enabled())
	assert.Nil(t, Writer())
}

func TestInit_InvalidDSN(t *testing.T) {
	resetSentryState(t)

	err := Init(Options{DSN: "not-a-valid-dsn"})

	// sentry.Init returns error for invalid DSNs.
	assert.Error(t, err)
	assert.False(t, Enabled())
}

func TestInit_ValidDSN(t *testing.T) {
	resetSentryState(t)

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
	require.NoError(t, Close(time.Second))
	assert.False(t, Enabled())
	assert.Nil(t, Writer())
}

func TestClose_WhenDisabled(t *testing.T) {
	resetSentryState(t)

	require.NoError(t, Close(0))
}

func TestClose_ClearsStateAndIsIdempotent(t *testing.T) {
	resetSentryState(t)
	var flushCalls atomic.Int32
	var closeCalls atomic.Int32
	installSentryState(
		io.Discard,
		func(time.Duration) bool {
			assert.False(t, Enabled(), "flush must run after active state is cleared")
			assert.Nil(t, Writer(), "flush must run without holding the state lock")
			flushCalls.Add(1)
			return true
		},
		func() error {
			assert.False(t, Enabled(), "writer close must run after active state is cleared")
			assert.Nil(t, Writer(), "writer close must run without holding the state lock")
			closeCalls.Add(1)
			return nil
		},
	)

	require.NoError(t, Close(time.Second))
	assert.False(t, Enabled())
	assert.Nil(t, Writer())
	require.NoError(t, Close(time.Second))
	assert.Equal(t, int32(1), flushCalls.Load())
	assert.Equal(t, int32(1), closeCalls.Load())
}

func TestClose_JoinsFlushAndWriterErrorsAfterClearingState(t *testing.T) {
	resetSentryState(t)
	closeErr := errors.New("injected writer close failure")
	installSentryState(
		io.Discard,
		func(time.Duration) bool { return false },
		func() error { return closeErr },
	)

	err := Close(time.Second)

	require.Error(t, err)
	assert.ErrorIs(t, err, errFlushTimeout)
	assert.ErrorIs(t, err, closeErr)
	assert.Contains(t, err.Error(), "flush Sentry events: timeout")
	assert.Contains(t, err.Error(), "close Sentry writer")
	assert.False(t, Enabled())
	assert.Nil(t, Writer())
}

func TestState_ConcurrentReadersAndInit(t *testing.T) {
	resetSentryState(t)
	start := make(chan struct{})
	stop := make(chan struct{})
	var ready sync.WaitGroup
	var wg sync.WaitGroup
	for range 16 {
		ready.Add(1)
		wg.Add(1)
		go func() {
			defer wg.Done()
			ready.Done()
			<-start
			for {
				select {
				case <-stop:
					return
				default:
					_ = Enabled()
					_ = Writer()
				}
			}
		}()
	}
	ready.Wait()
	close(start)

	require.NoError(t, Init(Options{
		DSN:              "https://key@sentry.io/123",
		Environment:      "test",
		Release:          "bosun@test",
		TracesSampleRate: 0,
	}))
	close(stop)
	wg.Wait()
	assert.True(t, Enabled())
	assert.NotNil(t, Writer())
}

func TestState_ConcurrentReadersAndClose(t *testing.T) {
	resetSentryState(t)
	installSentryState(
		io.Discard,
		func(time.Duration) bool { return true },
		func() error { return nil },
	)

	start := make(chan struct{})
	closeErr := make(chan error, 1)
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for range 100 {
				_ = Enabled()
				_ = Writer()
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		closeErr <- Close(time.Second)
	}()

	close(start)
	wg.Wait()
	require.NoError(t, <-closeErr)
	assert.False(t, Enabled())
	assert.Nil(t, Writer())
}

func TestRecover_WhenDisabled(t *testing.T) {
	resetSentryState(t)

	// Should not panic.
	Recover()
}

// TestRecover_StopsPanicWhenDisabled is a #364 regression: a bare
// "defer sentry.Recover()" is bosun's only safety net around every
// reconcile-triggering goroutine in the daemon. That safety net must not
// depend on Sentry being configured — the default, opt-in-only posture with
// no BOSUN_SENTRY_DSN set must still recover, or a panic anywhere in the
// reconcile path takes the whole daemon process down (exit code 2) instead
// of logging an error and staying up.
//
// ranAfterPanic is only reached if the panic below was actually absorbed by
// Go's recover() inside Recover() -- if Recover() returns without calling
// recover() (the pre-fix behavior when Sentry is disabled), the panic keeps
// propagating past the anonymous function call and this test fails loudly
// (a panic, not a quiet assertion failure) rather than silently passing.
func TestRecover_StopsPanicWhenDisabled(t *testing.T) {
	resetSentryState(t)

	ranAfterPanic := false
	func() {
		defer Recover()
		panic("simulated reconcile panic")
	}()
	ranAfterPanic = true

	assert.True(t, ranAfterPanic, "Recover() must stop the panic from propagating even when Sentry is disabled")
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
	resetSentryState(t)

	ctx := context.Background()
	newCtx, finish := ReconcileTransaction(ctx, "test")

	// Should return same context and no-op finish.
	assert.Equal(t, ctx, newCtx)
	finish(nil)               // Should not panic.
	finish(errors.New("err")) // Should not panic.
}

func TestReconcileTransaction_WhenEnabled(t *testing.T) {
	resetSentryState(t)
	installSentryState(io.Discard, func(time.Duration) bool { return true }, func() error { return nil })

	ctx, finish := ReconcileTransaction(context.Background(), "webhook")
	tx := sentry.SpanFromContext(ctx)
	require.NotNil(t, tx)
	assert.Equal(t, "reconcile", tx.Name)
	assert.Equal(t, "GitOps reconciliation", tx.Description)
	assert.Equal(t, "webhook", tx.Tags["source"])

	finish(context.Canceled)
	assert.Equal(t, sentry.SpanStatusCanceled, tx.Status)
}

func TestStartSpan_WhenDisabled(t *testing.T) {
	resetSentryState(t)

	ctx := context.Background()
	newCtx, finish := StartSpan(ctx, "op", "desc")

	assert.Equal(t, ctx, newCtx)
	finish(nil)
	finish(errors.New("err"))
}

func TestStartSpan_WhenEnabled(t *testing.T) {
	resetSentryState(t)
	installSentryState(io.Discard, func(time.Duration) bool { return true }, func() error { return nil })
	parent := sentry.StartTransaction(context.Background(), "parent")
	defer parent.Finish()

	ctx, finish := StartSpan(parent.Context(), "backup", "Create deployment backup")
	span := sentry.SpanFromContext(ctx)
	require.NotNil(t, span)
	assert.NotSame(t, parent, span)
	assert.Equal(t, "backup", span.Op)
	assert.Equal(t, "Create deployment backup", span.Description)

	finish(context.DeadlineExceeded)
	assert.Equal(t, sentry.SpanStatusDeadlineExceeded, span.Status)
}

func TestSpanStatus(t *testing.T) {
	assert.Equal(t, sentry.SpanStatusOK, spanStatus(nil))
	assert.Equal(t, sentry.SpanStatusCanceled, spanStatus(context.Canceled))
	assert.Equal(t, sentry.SpanStatusDeadlineExceeded, spanStatus(context.DeadlineExceeded))
	assert.Equal(t, sentry.SpanStatusInternalError, spanStatus(errors.New("something broke")))

	// Wrapped context errors should also be detected.
	wrapped := fmt.Errorf("operation failed: %w", context.Canceled)
	assert.Equal(t, sentry.SpanStatusCanceled, spanStatus(wrapped))
	wrapped = fmt.Errorf("operation failed: %w", context.DeadlineExceeded)
	assert.Equal(t, sentry.SpanStatusDeadlineExceeded, spanStatus(wrapped))
}

func TestStartSpan_NoParentTransaction(t *testing.T) {
	resetSentryState(t)
	installSentryState(io.Discard, func(time.Duration) bool { return true }, func() error { return nil })

	ctx := context.Background() // No transaction in context.
	newCtx, finish := StartSpan(ctx, "op", "desc")

	// Should return same context (no parent to attach to).
	assert.Equal(t, ctx, newCtx)
	finish(nil)
}
