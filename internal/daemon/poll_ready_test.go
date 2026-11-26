package daemon

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigFromEnv_PollInterval(t *testing.T) {
	tests := []struct {
		name      string
		bosunEnv  string
		legacyEnv string
		want      time.Duration
	}{
		{name: "unset keeps the default", want: time.Hour},
		{name: "legacy bare zero disables polling", legacyEnv: "0", want: 0},
		{name: "prefixed bare zero disables polling", bosunEnv: "0", want: 0},
		{name: "prefixed zero duration disables polling", bosunEnv: "0s", want: 0},
		{name: "prefixed zero beats a legacy value", bosunEnv: "0", legacyEnv: "300", want: 0},
		{name: "bare seconds", legacyEnv: "300", want: 300 * time.Second},
		{name: "go duration", bosunEnv: "90s", want: 90 * time.Second},
		{name: "negative keeps the default", bosunEnv: "-5", want: time.Hour},
		{name: "invalid keeps the default", bosunEnv: "not-a-number", want: time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("BOSUN_POLL_INTERVAL", tt.bosunEnv)
			t.Setenv("POLL_INTERVAL", tt.legacyEnv)

			cfg := ConfigFromEnv()

			assert.Equal(t, tt.want, cfg.PollInterval)
		})
	}
}

// TestReconcileLoop_CoalescedRecoveryBecomesReady covers /ready after a cycle
// fails and the coalesced follow-up cycle succeeds. The loop still returns the
// first error, but the daemon has now reconciled successfully.
func TestReconcileLoop_CoalescedRecoveryBecomesReady(t *testing.T) {
	failFirst := errors.New("first cycle failed")

	tests := []struct {
		name      string
		results   []error
		wantReady bool
		wantErr   error
	}{
		{name: "fail then coalesced success is ready", results: []error{failFirst, nil}, wantReady: true, wantErr: failFirst},
		{name: "success then coalesced failure stays ready", results: []error{nil, failFirst}, wantReady: true, wantErr: failFirst},
		{name: "every cycle failing is not ready", results: []error{failFirst, failFirst}, wantReady: false, wantErr: failFirst},
		{name: "a single success is ready", results: []error{nil}, wantReady: true, wantErr: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newConcurrencyDaemon(t)
			require.False(t, d.IsReady())

			calls := 0
			execute := func(_ context.Context, _ reconcileTrigger) error {
				result := tt.results[calls]
				calls++
				if calls < len(tt.results) {
					// A trigger arriving mid-cycle forms the coalesced follow-up.
					d.reconcileMu.Lock()
					d.queuePendingTrigger(newReconcileTrigger("webhook", false))
					d.reconcileMu.Unlock()
				}
				return result
			}

			d.reconcileMu.Lock()
			d.reconciling = true
			d.reconcileMu.Unlock()

			err := d.reconcileLoop(context.Background(), newReconcileTrigger("initial", false), execute)

			assert.Equal(t, len(tt.results), calls, "every queued cycle must run")
			assert.Equal(t, tt.wantErr, err, "the loop keeps returning the aggregate error")
			assert.Equal(t, tt.wantReady, d.IsReady())
		})
	}
}
