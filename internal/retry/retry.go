// Package retry provides a generic retry utility with exponential backoff
// and jitter for transient network failures.
package retry

import (
	"context"
	"errors"
	"math/rand/v2"
	"net"
	"os"
	"strings"
	"time"

	"github.com/cameronsjo/bosun/internal/log"
)

// ComponentRetry is the log component name for retry operations.
const ComponentRetry = "retry"

// Config controls retry behavior.
type Config struct {
	// MaxAttempts is the total number of attempts (initial + retries).
	// A value of 1 means no retries. Zero defaults to 3.
	MaxAttempts int

	// InitialDelay is the base delay before the first retry.
	// Zero defaults to 1 second.
	InitialDelay time.Duration

	// MaxDelay caps the backoff delay. Zero defaults to 30 seconds.
	MaxDelay time.Duration

	// Multiplier scales the delay between retries. Zero defaults to 2.0.
	Multiplier float64

	// IsRetryable determines whether an error should be retried.
	// If nil, the default transient error classifier is used.
	IsRetryable func(error) bool
}

// defaults fills zero-valued fields with sensible defaults.
func (c *Config) defaults() {
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = 3
	}
	if c.InitialDelay <= 0 {
		c.InitialDelay = 1 * time.Second
	}
	if c.MaxDelay <= 0 {
		c.MaxDelay = 30 * time.Second
	}
	if c.Multiplier <= 0 {
		c.Multiplier = 2.0
	}
	if c.IsRetryable == nil {
		c.IsRetryable = IsTransient
	}
}

// Do executes fn, retrying on transient errors with exponential backoff and
// jitter. It respects context cancellation and logs each retry attempt.
// Returns the last error if all attempts are exhausted.
func Do(ctx context.Context, cfg Config, operation string, fn func(ctx context.Context) error) error {
	cfg.defaults()
	logger := log.ComponentCtx(ctx, ComponentRetry)

	var lastErr error
	delay := cfg.InitialDelay

	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		// Check context before each attempt.
		if err := ctx.Err(); err != nil {
			if lastErr != nil {
				return lastErr
			}
			return err
		}

		lastErr = fn(ctx)
		if lastErr == nil {
			if attempt > 1 {
				logger.Info().
					Str(log.FieldOperation, operation).
					Int("attempt", attempt).
					Msg("Operation succeeded after retry")
			}
			return nil
		}

		// Don't retry if we've used all attempts.
		if attempt >= cfg.MaxAttempts {
			break
		}

		// Don't retry non-transient errors.
		if !cfg.IsRetryable(lastErr) {
			logger.Debug().
				Err(lastErr).
				Str(log.FieldOperation, operation).
				Int("attempt", attempt).
				Msg("Non-retryable error, aborting")
			return lastErr
		}

		// Apply jitter: uniform random in [delay/2, delay].
		jitteredDelay := jitter(delay)

		logger.Warn().
			Err(lastErr).
			Str(log.FieldOperation, operation).
			Int("attempt", attempt).
			Int("max_attempts", cfg.MaxAttempts).
			Int64("backoff_ms", jitteredDelay.Milliseconds()).
			Msg("Transient error, retrying after backoff")

		// Wait for backoff or context cancellation.
		select {
		case <-ctx.Done():
			return lastErr
		case <-time.After(jitteredDelay):
		}

		// Increase delay for next retry, capped at MaxDelay.
		delay = time.Duration(float64(delay) * cfg.Multiplier)
		if delay > cfg.MaxDelay {
			delay = cfg.MaxDelay
		}
	}

	logger.Error().
		Err(lastErr).
		Str(log.FieldOperation, operation).
		Int("attempts", cfg.MaxAttempts).
		Msg("All retry attempts exhausted")

	return lastErr
}

// jitter returns a duration uniformly distributed in [d/2, d].
func jitter(d time.Duration) time.Duration {
	half := d / 2
	return half + time.Duration(rand.Int64N(int64(half)+1))
}

// IsTransient returns true for errors that are typically transient network
// failures worth retrying: timeouts, DNS errors, connection refused, and
// connection reset.
func IsTransient(err error) bool {
	if err == nil {
		return false
	}

	// Context deadline exceeded is retryable (the per-attempt timeout fired,
	// not the parent context).
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	// Context cancelled is NOT retryable — the caller wants to stop.
	if errors.Is(err, context.Canceled) {
		return false
	}

	// Net errors: timeouts and temporary.
	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return true
		}
	}

	// DNS lookup failures.
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}

	// Connection refused / reset via OpError.
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}

	// OS-level errors: connection refused, reset, broken pipe.
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}

	// String-based fallback for wrapped errors that lose type info.
	msg := err.Error()
	transientSubstrings := []string{
		"connection refused",
		"connection reset",
		"broken pipe",
		"i/o timeout",
		"no such host",
		"temporary failure in name resolution",
		"tls handshake timeout",
		"EOF",
	}
	for _, substr := range transientSubstrings {
		if strings.Contains(strings.ToLower(msg), strings.ToLower(substr)) {
			return true
		}
	}

	return false
}
