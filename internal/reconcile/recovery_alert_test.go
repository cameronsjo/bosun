package reconcile

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingAlerter counts recovery dispatches and can fail them on demand.
type recordingAlerter struct {
	recoveries    int
	lastPrior     int
	recoveryError error
}

func (a *recordingAlerter) SendDeploySuccess(context.Context, string, string, []string, time.Duration) error {
	return nil
}

func (a *recordingAlerter) SendDeployFailure(context.Context, string, string, string, []string, time.Duration) error {
	return nil
}

func (a *recordingAlerter) SendDeployRecovery(_ context.Context, _, _ string, priorFailures int) error {
	a.recoveries++
	a.lastPrior = priorFailures
	return a.recoveryError
}

func (a *recordingAlerter) SendUnhealthyContainers(context.Context, string, []string) error {
	return nil
}
func (a *recordingAlerter) SendRollbackSuccess(context.Context, string, string) error { return nil }
func (a *recordingAlerter) SendRollbackFailure(context.Context, string, string) error { return nil }

func newRecoveryReconciler(t *testing.T, alerter *recordingAlerter, cfg *Config) *Reconciler {
	t.Helper()
	if cfg == nil {
		cfg = &Config{}
	}
	return &Reconciler{config: cfg, alerter: alerter}
}

func TestRetractionOwedUsesLastAlertedAttempt(t *testing.T) {
	t.Run("no state owes nothing", func(t *testing.T) {
		assert.False(t, retractionOwed(nil))
	})

	t.Run("a failure that alerted owes a retraction", func(t *testing.T) {
		assert.True(t, retractionOwed(&DeployState{AttemptCount: 1, LastAlertedAttempt: 1}))
	})

	t.Run("a failure below the alert threshold owes nothing", func(t *testing.T) {
		// AttemptCount > 0 would be the wrong predicate here: this failure was
		// recorded but never produced an alert, so there is nothing to retract.
		assert.False(t, retractionOwed(&DeployState{AttemptCount: 2, LastAlertedAttempt: 0}))
	})
}

func TestRetractFailureAlert(t *testing.T) {
	t.Run("dispatches once and clears", func(t *testing.T) {
		alerter := &recordingAlerter{}
		r := newRecoveryReconciler(t, alerter, &Config{OnRecovery: true})
		state := &DeployState{AttemptCount: 1, LastAlertedAttempt: 1}

		assert.True(t, r.retractFailureAlert(context.Background(), state))
		assert.Equal(t, 1, alerter.recoveries)
	})

	t.Run("reports the real prior-failure count, not zero", func(t *testing.T) {
		// The old call site passed AttemptCount-1, which was only correct while
		// the dispatch was gated on AttemptCount > 1. With the gate gone it
		// would report 0 in exactly the single-failure case.
		alerter := &recordingAlerter{}
		r := newRecoveryReconciler(t, alerter, &Config{OnRecovery: true})
		state := &DeployState{AttemptCount: 1, LastAlertedAttempt: 1}

		require.True(t, r.retractFailureAlert(context.Background(), state))
		assert.Equal(t, 1, alerter.lastPrior, "a single failure must be reported as 1 prior failure")
	})

	t.Run("not gated on OnSuccess", func(t *testing.T) {
		alerter := &recordingAlerter{}
		r := newRecoveryReconciler(t, alerter, &Config{OnRecovery: true, OnSuccess: false})
		state := &DeployState{AttemptCount: 1, LastAlertedAttempt: 1}

		require.True(t, r.retractFailureAlert(context.Background(), state))
		assert.Equal(t, 1, alerter.recoveries, "recovery must fire with on_success false")
	})

	t.Run("disabled gate clears state anyway", func(t *testing.T) {
		// Retaining state here would bank a stale retraction that fires
		// whenever an operator later enables on_recovery.
		alerter := &recordingAlerter{}
		r := newRecoveryReconciler(t, alerter, &Config{OnRecovery: false})
		state := &DeployState{AttemptCount: 1, LastAlertedAttempt: 1}

		assert.True(t, r.retractFailureAlert(context.Background(), state))
		assert.Equal(t, 0, alerter.recoveries)
	})

	t.Run("delivery failure retains state for retry", func(t *testing.T) {
		alerter := &recordingAlerter{recoveryError: errors.New("all providers failed")}
		r := newRecoveryReconciler(t, alerter, &Config{OnRecovery: true})
		state := &DeployState{AttemptCount: 1, LastAlertedAttempt: 1}

		assert.False(t, r.retractFailureAlert(context.Background(), state),
			"a provider outage must not consume the retraction")
		assert.Equal(t, 1, alerter.recoveries)

		// Next clean run re-attempts, and succeeds.
		alerter.recoveryError = nil
		assert.True(t, r.retractFailureAlert(context.Background(), state))
		assert.Equal(t, 2, alerter.recoveries)
	})

	t.Run("nothing owed dispatches nothing", func(t *testing.T) {
		alerter := &recordingAlerter{}
		r := newRecoveryReconciler(t, alerter, &Config{OnRecovery: true})

		assert.True(t, r.retractFailureAlert(context.Background(), &DeployState{}))
		assert.Equal(t, 0, alerter.recoveries)
	})

	t.Run("no alerter is not a delivery failure", func(t *testing.T) {
		r := &Reconciler{config: &Config{OnRecovery: true}}
		state := &DeployState{AttemptCount: 1, LastAlertedAttempt: 1}
		assert.True(t, r.retractFailureAlert(context.Background(), state),
			"retrying forever against a nil alerter would never succeed")
	})
}

func TestSendRecoveryAlertOutcomes(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		err     error
		want    recoveryOutcome
		alerter bool
	}{
		{name: "dispatched", cfg: &Config{OnRecovery: true}, want: recoveryDispatched, alerter: true},
		{name: "disabled", cfg: &Config{OnRecovery: false}, want: recoveryDisabled, alerter: true},
		{name: "delivery failed", cfg: &Config{OnRecovery: true}, err: errors.New("boom"), want: recoveryDeliveryFailed, alerter: true},
		{name: "no alerter counts as dispatched", cfg: &Config{OnRecovery: true}, want: recoveryDispatched, alerter: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := &Reconciler{config: tc.cfg}
			if tc.alerter {
				r.alerter = &recordingAlerter{recoveryError: tc.err}
			}
			assert.Equal(t, tc.want, r.sendRecoveryAlert(context.Background(), 1))
		})
	}
}

// TestDefaultConfigEnablesRecovery pins the default that makes a failure alert
// retractable without any configuration.
func TestDefaultConfigEnablesRecovery(t *testing.T) {
	cfg := DefaultConfig()
	assert.True(t, cfg.OnRecovery, "on_recovery must default to true")
	assert.True(t, cfg.OnFailure, "on_failure default is unchanged")
	assert.False(t, cfg.OnSuccess, "on_success default is unchanged")
}

// TestOnRecoveryHotReloads pins the gate to the config-reload path, not just
// startup. Without this the reload applier could be deleted and every other
// test would still pass, leaving on_recovery the one gate needing a restart.
func TestOnRecoveryHotReloads(t *testing.T) {
	falseVal, trueVal := false, true

	reloadWith := func(t *testing.T, start bool, reloaded *ReloadedConfig) bool {
		t.Helper()
		r := &Reconciler{config: &Config{
			OnRecovery: start,
			ConfigReloader: func(string) (*ReloadedConfig, error) {
				return reloaded, nil
			},
		}}
		require.NoError(t, r.reloadProjectConfig())
		return r.config.OnRecovery
	}

	t.Run("reload can disable it", func(t *testing.T) {
		assert.False(t, reloadWith(t, true, &ReloadedConfig{OnRecovery: &falseVal}))
	})

	t.Run("reload can enable it", func(t *testing.T) {
		assert.True(t, reloadWith(t, false, &ReloadedConfig{OnRecovery: &trueVal}))
	})

	t.Run("absent leaves it alone", func(t *testing.T) {
		assert.True(t, reloadWith(t, true, &ReloadedConfig{}))
		assert.False(t, reloadWith(t, false, &ReloadedConfig{}))
	})
}

// TestRetriedRetractionReportsRealCount is the regression test for a bug this
// change introduced and code review caught. AttemptCount was zeroed
// unconditionally while LastAlertedAttempt was retained on delivery failure, so
// the retry read AttemptCount as 0 and reported "0 prior failures" -- the same
// defect this change exists to remove, reappearing on its own retry path.
func TestRetriedRetractionReportsRealCount(t *testing.T) {
	alerter := &recordingAlerter{recoveryError: errors.New("discord down")}
	r := newRecoveryReconciler(t, alerter, &Config{OnRecovery: true})
	state := &DeployState{AttemptCount: 1, LastAlertedAttempt: 1}

	// First clean run: delivery fails, state is retained.
	require.False(t, r.retractFailureAlert(context.Background(), state))
	assert.Equal(t, 1, alerter.lastPrior)

	// Callers must not zero AttemptCount while the retraction is still owed.
	// Simulating a caller that does is what proves the coupling matters.
	alerter.recoveryError = nil
	require.True(t, r.retractFailureAlert(context.Background(), state))
	assert.Equal(t, 1, alerter.lastPrior,
		"the retried retraction must still report 1 prior failure, not 0")
}
