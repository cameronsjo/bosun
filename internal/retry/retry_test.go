package retry

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDo_SucceedsOnFirstAttempt(t *testing.T) {
	calls := 0
	err := Do(context.Background(), Config{MaxAttempts: 3}, "test-op", func(ctx context.Context) error {
		calls++
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, 1, calls)
}

func TestDo_SucceedsAfterTransientFailure(t *testing.T) {
	calls := 0
	err := Do(context.Background(), Config{
		MaxAttempts:  3,
		InitialDelay: 1 * time.Millisecond,
	}, "test-op", func(ctx context.Context) error {
		calls++
		if calls < 3 {
			return &net.OpError{Op: "dial", Err: errors.New("connection refused")}
		}
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, 3, calls)
}

func TestDo_ExhaustsAllAttempts(t *testing.T) {
	calls := 0
	transientErr := &net.OpError{Op: "dial", Err: errors.New("connection refused")}

	err := Do(context.Background(), Config{
		MaxAttempts:  3,
		InitialDelay: 1 * time.Millisecond,
	}, "test-op", func(ctx context.Context) error {
		calls++
		return transientErr
	})

	require.Error(t, err)
	assert.Equal(t, 3, calls)
	assert.ErrorIs(t, err, transientErr)
}

func TestDo_StopsOnNonRetryableError(t *testing.T) {
	calls := 0
	permanentErr := errors.New("permission denied")

	err := Do(context.Background(), Config{
		MaxAttempts:  5,
		InitialDelay: 1 * time.Millisecond,
	}, "test-op", func(ctx context.Context) error {
		calls++
		return permanentErr
	})

	require.Error(t, err)
	assert.Equal(t, 1, calls, "should stop after first non-retryable error")
	assert.ErrorIs(t, err, permanentErr)
}

func TestDo_RespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0

	err := Do(ctx, Config{
		MaxAttempts:  10,
		InitialDelay: 100 * time.Millisecond,
	}, "test-op", func(ctx context.Context) error {
		calls++
		if calls == 1 {
			cancel()
		}
		return &net.OpError{Op: "dial", Err: errors.New("connection refused")}
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled, "should return context error, not operation error")
	// Should stop after 1 call because context was cancelled during backoff wait.
	assert.LessOrEqual(t, calls, 2)
}

func TestDo_RespectsContextDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	calls := 0
	err := Do(ctx, Config{
		MaxAttempts:  100,
		InitialDelay: 200 * time.Millisecond,
	}, "test-op", func(ctx context.Context) error {
		calls++
		return &net.OpError{Op: "dial", Err: errors.New("connection refused")}
	})

	require.Error(t, err)
	// With 200ms backoff and 50ms deadline, we expect 1-2 calls max.
	assert.LessOrEqual(t, calls, 2)
}

func TestDo_DefaultConfig(t *testing.T) {
	calls := 0
	err := Do(context.Background(), Config{
		InitialDelay: 1 * time.Millisecond,
	}, "test-op", func(ctx context.Context) error {
		calls++
		if calls < 2 {
			return &net.OpError{Op: "dial", Err: errors.New("connection refused")}
		}
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, 2, calls, "default MaxAttempts is 3, should succeed on attempt 2")
}

func TestDo_SingleAttemptNoRetry(t *testing.T) {
	calls := 0
	err := Do(context.Background(), Config{
		MaxAttempts: 1,
	}, "test-op", func(ctx context.Context) error {
		calls++
		return &net.OpError{Op: "dial", Err: errors.New("connection refused")}
	})

	require.Error(t, err)
	assert.Equal(t, 1, calls)
}

func TestDo_CustomIsRetryable(t *testing.T) {
	calls := 0
	customErr := errors.New("custom-retryable")

	err := Do(context.Background(), Config{
		MaxAttempts:  3,
		InitialDelay: 1 * time.Millisecond,
		IsRetryable: func(err error) bool {
			return errors.Is(err, customErr)
		},
	}, "test-op", func(ctx context.Context) error {
		calls++
		if calls < 3 {
			return customErr
		}
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, 3, calls)
}

func TestDo_BackoffIncreases(t *testing.T) {
	var timestamps []time.Time
	cfg := Config{
		MaxAttempts:  4,
		InitialDelay: 10 * time.Millisecond,
		Multiplier:   2.0,
		MaxDelay:     1 * time.Second,
	}

	_ = Do(context.Background(), cfg, "test-op", func(ctx context.Context) error {
		timestamps = append(timestamps, time.Now())
		return &net.OpError{Op: "dial", Err: errors.New("connection refused")}
	})

	require.Len(t, timestamps, 4)

	// Verify delays are increasing (with jitter tolerance).
	// Delay 1: ~5-10ms, Delay 2: ~10-20ms, Delay 3: ~20-40ms
	for i := 2; i < len(timestamps); i++ {
		prevGap := timestamps[i-1].Sub(timestamps[i-2])
		currGap := timestamps[i].Sub(timestamps[i-1])
		// Current gap should be at least 75% of previous (accounting for jitter).
		// With 2x multiplier and [d/2, d] jitter, minimum growth ratio is 0.5.
		assert.Greater(t, currGap.Nanoseconds(), prevGap.Nanoseconds()*3/4,
			"backoff delay should grow between attempt %d and %d (prev=%v, curr=%v)", i-1, i, prevGap, currGap)
	}
}

func TestIsTransient(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "context cancelled",
			err:      context.Canceled,
			expected: false,
		},
		{
			name:     "context deadline exceeded",
			err:      context.DeadlineExceeded,
			expected: true,
		},
		{
			name:     "net OpError connection refused",
			err:      &net.OpError{Op: "dial", Err: errors.New("connection refused")},
			expected: true,
		},
		{
			name:     "DNS error",
			err:      &net.DNSError{Err: "no such host", Name: "example.com"},
			expected: true,
		},
		{
			name:     "wrapped timeout",
			err:      fmt.Errorf("git clone failed: %w", &net.OpError{Op: "dial", Err: &timeoutError{}}),
			expected: true,
		},
		{
			name:     "connection reset string",
			err:      errors.New("read tcp: connection reset by peer"),
			expected: true,
		},
		{
			name:     "broken pipe string",
			err:      errors.New("write: broken pipe"),
			expected: true,
		},
		{
			name:     "i/o timeout string",
			err:      errors.New("dial tcp: i/o timeout"),
			expected: true,
		},
		{
			name:     "no such host string",
			err:      errors.New("dial tcp: lookup example.com: no such host"),
			expected: true,
		},
		{
			name:     "TLS handshake timeout string",
			err:      errors.New("net/http: TLS handshake timeout"),
			expected: true,
		},
		{
			name:     "EOF string",
			err:      errors.New("unexpected EOF"),
			expected: true,
		},
		{
			name:     "permanent error",
			err:      errors.New("permission denied"),
			expected: false,
		},
		{
			name:     "file not found",
			err:      errors.New("file not found"),
			expected: false,
		},
		{
			name:     "wrapped permanent error",
			err:      fmt.Errorf("operation failed: %w", errors.New("invalid argument")),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, IsTransient(tt.err))
		})
	}
}

func TestJitter(t *testing.T) {
	base := 100 * time.Millisecond

	for range 100 {
		j := jitter(base)
		assert.GreaterOrEqual(t, j, base/2, "jitter should be >= base/2")
		assert.LessOrEqual(t, j, base, "jitter should be <= base")
	}
}

func TestConfigDefaults(t *testing.T) {
	cfg := Config{}
	cfg.defaults()

	assert.Equal(t, 3, cfg.MaxAttempts)
	assert.Equal(t, 1*time.Second, cfg.InitialDelay)
	assert.Equal(t, 30*time.Second, cfg.MaxDelay)
	assert.Equal(t, 2.0, cfg.Multiplier)
	assert.NotNil(t, cfg.IsRetryable)
}

func TestConfigDefaults_PreservesExplicitValues(t *testing.T) {
	cfg := Config{
		MaxAttempts:  5,
		InitialDelay: 500 * time.Millisecond,
		MaxDelay:     10 * time.Second,
		Multiplier:   1.5,
	}
	cfg.defaults()

	assert.Equal(t, 5, cfg.MaxAttempts)
	assert.Equal(t, 500*time.Millisecond, cfg.InitialDelay)
	assert.Equal(t, 10*time.Second, cfg.MaxDelay)
	assert.Equal(t, 1.5, cfg.Multiplier)
}

// timeoutError implements net.Error with Timeout() = true.
type timeoutError struct{}

func (e *timeoutError) Error() string   { return "i/o timeout" }
func (e *timeoutError) Timeout() bool   { return true }
func (e *timeoutError) Temporary() bool { return true }
