package daemon

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/cameronsjo/bosun/internal/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func selfHealReport(items ...reconcile.DriftItem) *reconcile.DriftReport {
	return &reconcile.DriftReport{Items: items}
}

func enabledSelfHealConfig(max int, cooldown time.Duration) driftRuntimeConfig {
	return driftRuntimeConfig{
		driftSelfHeal:         true,
		driftSelfHealCooldown: cooldown,
		maxSelfHealAttempts:   max,
	}
}

func TestPlanSelfHealExhaustsExactlyOnceAtConfiguredBound(t *testing.T) {
	state := &reconcile.DeployState{}
	report := selfHealReport(reconcile.DriftItem{Service: "secret-service-name", Type: reconcile.DriftMissing})
	cfg := enabledSelfHealConfig(3, time.Minute)
	now := time.Date(2026, time.August, 23, 20, 0, 0, 0, time.UTC)

	for attempt := 1; attempt <= 3; attempt++ {
		decision := planSelfHeal(state, report, cfg, false, now)
		assert.True(t, decision.trigger)
		assert.Equal(t, attempt, decision.attempts)
		assert.Len(t, decision.signatureID, selfHealSignatureIDLength)
		assert.NotContains(t, decision.signatureID, "secret-service-name")
		assert.False(t, decision.alertExhausted, "exhaustion is confirmed only if drift persists after the final attempt")
		now = now.Add(time.Minute)
	}

	require.NotNil(t, state.DriftSelfHeal)
	assert.Equal(t, 3, state.DriftSelfHeal.Attempts)
	assert.False(t, state.DriftSelfHeal.Exhausted)
	assert.False(t, state.DriftSelfHeal.ExhaustedAlerted)

	firstExhausted := planSelfHeal(state, report, cfg, false, now)
	assert.False(t, firstExhausted.trigger)
	assert.True(t, firstExhausted.alertExhausted)
	assert.Equal(t, "attempt_budget_exhausted", firstExhausted.reason)
	assert.True(t, state.DriftSelfHeal.Exhausted)
	assert.True(t, state.DriftSelfHeal.ExhaustedAlerted)

	for range 3 {
		decision := planSelfHeal(state, report, cfg, false, now)
		assert.False(t, decision.trigger)
		assert.False(t, decision.alertExhausted, "an exhausted signature alerts at most once")
		assert.Equal(t, "attempt_budget_exhausted", decision.reason)
		now = now.Add(time.Minute)
	}
}

func TestPlanSelfHealChangedSignatureResetsBudgetButNotCooldown(t *testing.T) {
	now := time.Date(2026, time.August, 23, 20, 0, 0, 0, time.UTC)
	state := &reconcile.DeployState{}
	cfg := enabledSelfHealConfig(1, 10*time.Minute)
	first := selfHealReport(reconcile.DriftItem{Service: "api", Type: reconcile.DriftMissing})
	changed := selfHealReport(reconcile.DriftItem{Service: "api", Type: reconcile.DriftUnhealthy})

	firstDecision := planSelfHeal(state, first, cfg, false, now)
	require.True(t, firstDecision.trigger)
	require.False(t, firstDecision.alertExhausted)
	require.True(t, planSelfHeal(state, first, cfg, false, now.Add(time.Minute)).alertExhausted)
	firstSignature := state.DriftSelfHeal.Signature

	cooldownDecision := planSelfHeal(state, changed, cfg, false, now.Add(time.Minute))
	assert.False(t, cooldownDecision.trigger)
	assert.False(t, cooldownDecision.alertExhausted)
	assert.Equal(t, "cooldown_active", cooldownDecision.reason)
	require.NotNil(t, state.DriftSelfHeal)
	assert.NotEqual(t, firstSignature, state.DriftSelfHeal.Signature)
	assert.Zero(t, state.DriftSelfHeal.Attempts)
	assert.False(t, state.DriftSelfHeal.Exhausted)
	assert.Equal(t, now, state.DriftSelfHeal.LastAttemptAt, "signature churn must not bypass the global cooldown")

	afterCooldown := planSelfHeal(state, changed, cfg, false, now.Add(10*time.Minute))
	assert.True(t, afterCooldown.trigger)
	assert.False(t, afterCooldown.alertExhausted)
	assert.Equal(t, 1, state.DriftSelfHeal.Attempts)
}

func TestPlanSelfHealResolutionRearmsSameSignature(t *testing.T) {
	now := time.Date(2026, time.August, 23, 20, 0, 0, 0, time.UTC)
	state := &reconcile.DeployState{}
	cfg := enabledSelfHealConfig(1, 0)
	report := selfHealReport(reconcile.DriftItem{Service: "api", Type: reconcile.DriftMissing})

	require.True(t, planSelfHeal(state, report, cfg, false, now).trigger)
	require.True(t, planSelfHeal(state, report, cfg, false, now.Add(time.Second)).alertExhausted)
	resolved := planSelfHeal(state, selfHealReport(), cfg, false, now.Add(time.Second))
	assert.Equal(t, "drift_resolved", resolved.reason)
	require.NotNil(t, state.DriftSelfHeal)
	assert.Empty(t, state.DriftSelfHeal.Signature)
	assert.Zero(t, state.DriftSelfHeal.Attempts)
	assert.False(t, state.DriftSelfHeal.Exhausted)
	assert.False(t, state.DriftSelfHeal.ExhaustedAlerted)
	assert.Equal(t, now, state.DriftSelfHeal.LastAttemptAt)

	recurrence := planSelfHeal(state, report, cfg, false, now.Add(2*time.Second))
	assert.True(t, recurrence.trigger)
	assert.False(t, recurrence.alertExhausted)
	assert.Equal(t, 1, state.DriftSelfHeal.Attempts)
}

func TestPlanSelfHealResolutionDoesNotEraseGlobalCooldown(t *testing.T) {
	now := time.Date(2026, time.August, 23, 20, 0, 0, 0, time.UTC)
	state := &reconcile.DeployState{}
	cfg := enabledSelfHealConfig(3, 10*time.Minute)
	report := selfHealReport(reconcile.DriftItem{Service: "api", Type: reconcile.DriftMissing})

	require.True(t, planSelfHeal(state, report, cfg, false, now).trigger)
	planSelfHeal(state, selfHealReport(), cfg, false, now.Add(time.Minute))
	recurrence := planSelfHeal(state, report, cfg, false, now.Add(2*time.Minute))

	assert.False(t, recurrence.trigger)
	assert.Equal(t, "cooldown_active", recurrence.reason)
	require.NotNil(t, state.DriftSelfHeal)
	assert.Zero(t, state.DriftSelfHeal.Attempts, "resolution re-arms the budget even while cooldown remains active")
}

func TestPlanSelfHealDisabledAndBusyDoNotConsumeAttempts(t *testing.T) {
	now := time.Date(2026, time.August, 23, 20, 0, 0, 0, time.UTC)
	report := selfHealReport(reconcile.DriftItem{Service: "api", Type: reconcile.DriftMissing})

	t.Run("disabled", func(t *testing.T) {
		state := &reconcile.DeployState{}
		decision := planSelfHeal(state, report, driftRuntimeConfig{maxSelfHealAttempts: 3}, false, now)
		assert.False(t, decision.trigger)
		assert.Nil(t, state.DriftSelfHeal)
	})

	t.Run("reconciliation in progress", func(t *testing.T) {
		state := &reconcile.DeployState{}
		decision := planSelfHeal(state, report, enabledSelfHealConfig(3, 0), true, now)
		assert.False(t, decision.trigger)
		assert.Nil(t, state.DriftSelfHeal)
	})
}

func TestPlanSelfHealRestartPreservesBudgetAndCooldown(t *testing.T) {
	now := time.Date(2026, time.August, 23, 20, 0, 0, 0, time.UTC)
	stateFile := filepath.Join(t.TempDir(), "state.json")
	report := selfHealReport(reconcile.DriftItem{Service: "api", Type: reconcile.DriftMissing})
	cfg := enabledSelfHealConfig(2, 10*time.Minute)
	state := &reconcile.DeployState{}

	require.True(t, planSelfHeal(state, report, cfg, false, now).trigger)
	require.NoError(t, reconcile.SaveState(stateFile, state))

	restartedState := reconcile.LoadState(stateFile)
	duringCooldown := planSelfHeal(restartedState, report, cfg, false, now.Add(time.Minute))
	assert.False(t, duringCooldown.trigger)
	assert.Equal(t, "cooldown_active", duringCooldown.reason)
	assert.Equal(t, 1, restartedState.DriftSelfHeal.Attempts)

	afterCooldown := planSelfHeal(restartedState, report, cfg, false, now.Add(10*time.Minute))
	assert.True(t, afterCooldown.trigger)
	assert.False(t, afterCooldown.alertExhausted)
	assert.Equal(t, 2, restartedState.DriftSelfHeal.Attempts)

	require.NoError(t, reconcile.SaveState(stateFile, restartedState))
	afterSecondRestart := reconcile.LoadState(stateFile)
	exhausted := planSelfHeal(afterSecondRestart, report, cfg, false, now.Add(20*time.Minute))
	assert.False(t, exhausted.trigger)
	assert.True(t, exhausted.alertExhausted)
	assert.Equal(t, 2, afterSecondRestart.DriftSelfHeal.Attempts)
}

func TestPlanSelfHealClockRollbackCannotBypassCooldown(t *testing.T) {
	now := time.Date(2026, time.August, 23, 20, 0, 0, 0, time.UTC)
	state := &reconcile.DeployState{}
	report := selfHealReport(reconcile.DriftItem{Service: "api", Type: reconcile.DriftMissing})
	cfg := enabledSelfHealConfig(3, time.Minute)
	require.True(t, planSelfHeal(state, report, cfg, false, now).trigger)

	decision := planSelfHeal(state, report, cfg, false, now.Add(-time.Hour))
	assert.False(t, decision.trigger)
	assert.Equal(t, "cooldown_active", decision.reason)
	assert.Greater(t, decision.remainingCooldown, time.Hour)
}

func TestMaybeSelfHealDispatchesOneOpaqueExhaustionAlert(t *testing.T) {
	provider := &testAlertProvider{}
	d := newAlertDaemon(t, provider)
	decision := selfHealDecision{
		alertExhausted: true,
		signatureID:    "0123456789ab",
		itemCount:      2,
		attempts:       3,
		maxAttempts:    3,
	}

	d.maybeSelfHeal(context.Background(), decision, nil)
	require.Len(t, provider.alerts, 1)
	assert.Equal(t, "self-heal-exhausted", provider.alerts[0].Source)
	assert.NotContains(t, provider.alerts[0].Message, "private")

	decision.alertExhausted = false
	decision.reason = "attempt_budget_exhausted"
	d.maybeSelfHeal(context.Background(), decision, nil)
	assert.Len(t, provider.alerts, 1)
}
