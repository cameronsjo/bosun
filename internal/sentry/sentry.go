// Package sentry provides Sentry error tracking and performance monitoring
// for bosun. It is opt-in via the BOSUN_SENTRY_DSN environment variable.
//
// When enabled, Sentry hooks into the logging layer via the zerolog writer
// integration. Error/Fatal logs become Sentry events; Info/Warn logs become
// breadcrumbs. This means every existing log.Error() call in the codebase
// automatically reports to Sentry with zero changes to existing code.
package sentry

import (
	"context"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	sentryzerolog "github.com/getsentry/sentry-go/zerolog"
	"github.com/rs/zerolog"
)

// Options configures Sentry integration.
type Options struct {
	// DSN is the Sentry Data Source Name. Empty disables Sentry.
	DSN string

	// Environment identifies the deployment (e.g. "production", "staging").
	Environment string

	// Release is the application version string.
	Release string

	// TracesSampleRate controls what fraction of transactions are sent (0.0-1.0).
	TracesSampleRate float64
}

// state tracks whether Sentry has been initialized.
var state struct {
	enabled bool
	writer  io.Writer
	closer  func()
}

// Init initializes Sentry if a DSN is configured.
// Returns nil if DSN is empty (Sentry disabled).
func Init(opts Options) error {
	if opts.DSN == "" {
		return nil
	}

	err := sentry.Init(sentry.ClientOptions{
		Dsn:              opts.DSN,
		Environment:      opts.Environment,
		Release:          opts.Release,
		TracesSampleRate: opts.TracesSampleRate,
		AttachStacktrace: true,
		IgnoreErrors: []string{
			"^context canceled$",
			"^context deadline exceeded$",
		},
		IgnoreTransactions: []string{
			"^GET /health",
			"^GET /ready",
		},
		BeforeSend: beforeSend,
	})
	if err != nil {
		return err
	}

	// Create the zerolog writer for log-level integration.
	writer, err := sentryzerolog.New(sentryzerolog.Config{
		ClientOptions: sentry.ClientOptions{
			Dsn:         opts.DSN,
			Environment: opts.Environment,
			Release:     opts.Release,
		},
		Options: sentryzerolog.Options{
			Levels:          []zerolog.Level{zerolog.ErrorLevel, zerolog.FatalLevel},
			FlushTimeout:    3 * time.Second,
			WithBreadcrumbs: true,
		},
	})
	if err != nil {
		return err
	}

	state.enabled = true
	state.writer = writer
	state.closer = func() { _ = writer.Close() }

	return nil
}

// beforeSend filters out noisy events before they reach Sentry.
func beforeSend(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
	if hint != nil && hint.OriginalException != nil {
		msg := hint.OriginalException.Error()
		// Drop "already up to date" from git operations - this is normal.
		if strings.Contains(strings.ToLower(msg), "already up to date") {
			return nil
		}
	}

	// Filter by exception message as fallback.
	for _, exc := range event.Exception {
		lower := strings.ToLower(exc.Value)
		if strings.Contains(lower, "already up to date") {
			return nil
		}
	}

	return event
}

// Close flushes pending Sentry events and shuts down the writer.
// Should be called during graceful shutdown.
func Close(timeout time.Duration) {
	if !state.enabled {
		return
	}
	sentry.Flush(timeout)
	if state.closer != nil {
		state.closer()
	}
	state.enabled = false
}

// Writer returns the Sentry zerolog writer for use with MultiLevelWriter.
// Returns nil if Sentry is not enabled.
func Writer() io.Writer {
	return state.writer
}

// Enabled returns whether Sentry is active.
func Enabled() bool {
	return state.enabled
}

// Recover captures a panic and sends it to Sentry.
// Use as: defer sentry.Recover()
func Recover() {
	if !state.enabled {
		return
	}
	sentry.Recover()
}

// ConfigFromEnv loads Sentry configuration from environment variables.
func ConfigFromEnv() Options {
	opts := Options{
		DSN:              os.Getenv("BOSUN_SENTRY_DSN"),
		Environment:      os.Getenv("BOSUN_SENTRY_ENVIRONMENT"),
		TracesSampleRate: 0.1,
	}

	// Auto-detect environment from daemon mode if not explicitly set.
	if opts.Environment == "" {
		if os.Getenv("BOSUN_DAEMON_MODE") == "true" {
			opts.Environment = "production"
		} else {
			opts.Environment = "development"
		}
	}

	if rateStr := os.Getenv("BOSUN_SENTRY_TRACES_SAMPLE_RATE"); rateStr != "" {
		if rate, err := strconv.ParseFloat(rateStr, 64); err == nil && rate >= 0 && rate <= 1 {
			opts.TracesSampleRate = rate
		}
	}

	return opts
}

// ReconcileTransaction starts a Sentry transaction for a reconcile operation.
// Returns a new context with the transaction and a finish function.
// The finish function should be called with the final error (or nil) when done.
// If Sentry is disabled, returns the original context and a no-op finish function.
func ReconcileTransaction(ctx context.Context, source string) (context.Context, func(error)) {
	if !state.enabled {
		return ctx, func(error) {}
	}

	tx := sentry.StartTransaction(ctx, "reconcile",
		sentry.WithDescription("GitOps reconciliation"),
		sentry.WithTransactionSource(sentry.SourceTask),
	)
	tx.SetTag("source", source)

	finish := func(err error) {
		if err != nil {
			tx.Status = sentry.SpanStatusInternalError
		} else {
			tx.Status = sentry.SpanStatusOK
		}
		tx.Finish()
	}

	return tx.Context(), finish
}

// StartSpan starts a child span on the current transaction in the context.
// Returns the context with the span and a finish function.
// If no transaction exists or Sentry is disabled, returns a no-op.
func StartSpan(ctx context.Context, operation, description string) (context.Context, func(error)) {
	if !state.enabled {
		return ctx, func(error) {}
	}

	parent := sentry.SpanFromContext(ctx)
	if parent == nil {
		return ctx, func(error) {}
	}

	span := parent.StartChild(operation)
	span.Description = description

	finish := func(err error) {
		if err != nil {
			span.Status = sentry.SpanStatusInternalError
		} else {
			span.Status = sentry.SpanStatusOK
		}
		span.Finish()
	}

	return span.Context(), finish
}
