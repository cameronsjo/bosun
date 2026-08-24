package daemon

import (
	"time"

	"github.com/cameronsjo/bosun/internal/reconcile"
)

const selfHealSignatureIDLength = 12

type selfHealDecision struct {
	trigger           bool
	alertExhausted    bool
	reason            string
	signatureID       string
	itemCount         int
	attempts          int
	maxAttempts       int
	remainingCooldown time.Duration
}

// planSelfHeal advances the persisted self-heal state machine without causing
// external side effects. The caller must atomically save state before acting on
// the returned decision.
func planSelfHeal(
	state *reconcile.DeployState,
	report *reconcile.DriftReport,
	cfg driftRuntimeConfig,
	busy bool,
	now time.Time,
) selfHealDecision {
	maxAttempts := cfg.maxSelfHealAttempts
	if maxAttempts <= 0 {
		maxAttempts = DefaultMaxSelfHealAttempts
	}

	decision := selfHealDecision{
		reason:      "disabled",
		itemCount:   len(report.Items),
		maxAttempts: maxAttempts,
	}

	if !report.HasDrift() {
		if state.DriftSelfHeal != nil && !state.DriftSelfHeal.LastAttemptAt.IsZero() {
			state.DriftSelfHeal = &reconcile.DriftSelfHealTracking{
				LastAttemptAt: state.DriftSelfHeal.LastAttemptAt,
			}
		} else {
			state.DriftSelfHeal = nil
		}
		decision.reason = "drift_resolved"
		return decision
	}

	if !cfg.driftSelfHeal {
		return decision
	}

	signature := reconcile.DriftSignature(report.Items)
	decision.signatureID = boundedSignatureID(signature)

	if busy {
		decision.reason = "reconciliation_in_progress"
		return decision
	}

	tracking := state.DriftSelfHeal
	if tracking == nil || tracking.Signature != signature {
		lastAttemptAt := time.Time{}
		if tracking != nil {
			// Cooldown is global, not per signature. A changing drift set cannot
			// create an unbounded rapid-fire retry loop.
			lastAttemptAt = tracking.LastAttemptAt
		}
		tracking = &reconcile.DriftSelfHealTracking{
			Signature:     signature,
			LastAttemptAt: lastAttemptAt,
		}
		state.DriftSelfHeal = tracking
	}

	decision.attempts = tracking.Attempts
	if tracking.Exhausted || tracking.Attempts >= maxAttempts {
		tracking.Exhausted = true
		decision.reason = "attempt_budget_exhausted"
		if !tracking.ExhaustedAlerted {
			tracking.ExhaustedAlerted = true
			decision.alertExhausted = true
		}
		return decision
	}

	if !tracking.LastAttemptAt.IsZero() && now.Before(tracking.LastAttemptAt.Add(cfg.driftSelfHealCooldown)) {
		decision.reason = "cooldown_active"
		decision.remainingCooldown = tracking.LastAttemptAt.Add(cfg.driftSelfHealCooldown).Sub(now)
		return decision
	}

	tracking.Attempts++
	tracking.LastAttemptAt = now
	decision.trigger = true
	decision.reason = "trigger"
	decision.attempts = tracking.Attempts

	return decision
}

func boundedSignatureID(signature string) string {
	if len(signature) <= selfHealSignatureIDLength {
		return signature
	}
	return signature[:selfHealSignatureIDLength]
}

func cloneDriftSelfHealTracking(tracking *reconcile.DriftSelfHealTracking) *reconcile.DriftSelfHealTracking {
	if tracking == nil {
		return nil
	}
	clone := *tracking
	return &clone
}
