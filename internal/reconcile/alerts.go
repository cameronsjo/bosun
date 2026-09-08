package reconcile

import (
	"context"
	"time"

	"github.com/cameronsjo/bosun/internal/log"
)

const failureAlertDeliveryTimeout = 30 * time.Second

// failureAlertDeliveryContext preserves live context behavior. If the caller
// is already canceled, it retains context values while giving alert providers
// a fresh, bounded delivery window.
func failureAlertDeliveryContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx.Err() == nil {
		return ctx, nil
	}
	return context.WithTimeout(context.WithoutCancel(ctx), failureAlertDeliveryTimeout)
}

// alertTarget returns the target identifier for alert messages.
// DeployMode takes precedence when explicitly set; then TargetName (multi-target mode);
// falls back to TargetHost or "local".
func (r *Reconciler) alertTarget() string {
	if r.config.DeployMode == "local" {
		return "local"
	}
	if r.config.TargetName != "" && r.config.TargetName != DefaultTargetName {
		return r.config.TargetName
	}
	if r.config.TargetHost != "" {
		return r.config.TargetHost
	}
	return "local"
}

// sendSuccessAlert sends a deployment success notification.
// Gated on config.OnSuccess: when false, no success alerts are sent.
func (r *Reconciler) sendSuccessAlert(ctx context.Context) {
	if r.alerter == nil {
		return
	}

	if !r.config.OnSuccess {
		return
	}

	target := r.alertTarget()

	services := r.serviceNames()
	duration := time.Since(r.runStartTime)

	if err := r.alerter.SendDeploySuccess(ctx, r.lastCommit, target, services, duration); err != nil {
		logger := log.ComponentCtx(ctx, log.ComponentReconcile)
		logger.Warn().
			Err(err).
			Str(log.FieldOperation, "alert_success").
			Str(log.FieldTarget, target).
			Msg("Failed to send success alert")
	}
}

// serviceNames extracts service names from declared services.
func (r *Reconciler) serviceNames() []string {
	if len(r.declaredServices) == 0 {
		return nil
	}
	names := make([]string, len(r.declaredServices))
	for i, s := range r.declaredServices {
		names[i] = s.Name
	}
	return names
}

// sendThrottledFailureAlert sends a failure alert if the throttle schedule allows it.
// Updates LastAlertedAttempt in the state and persists it.
// Gated on config.OnFailure: when false, no failure alerts are sent.
func (r *Reconciler) sendThrottledFailureAlert(ctx context.Context, state *DeployState, reason string, causes ...error) {
	if len(causes) > 0 && isPropagatedCallerCancellation(ctx, causes[0]) {
		return
	}
	if r.alerter == nil {
		return
	}

	if !r.config.OnFailure {
		return
	}

	if !ShouldAlert(state.AttemptCount, state.LastAlertedAttempt) {
		return
	}

	alertCtx, cancel := failureAlertDeliveryContext(ctx)
	if cancel != nil {
		defer cancel()
	}

	target := r.alertTarget()
	logger := log.ComponentCtx(alertCtx, log.ComponentReconcile)

	services := r.serviceNames()
	duration := time.Since(r.runStartTime)

	if err := r.alerter.SendDeployFailure(alertCtx, r.lastCommit, target, reason, services, duration); err != nil {
		logger.Warn().
			Err(err).
			Str(log.FieldOperation, "alert_failure").
			Str(log.FieldTarget, target).
			Msg("Failed to send failure alert")
		return
	}

	state.LastAlertedAttempt = state.AttemptCount
	if err := SaveState(r.config.StateFile, state); err != nil {
		logger.Warn().Err(err).Msg("Failed to persist alert throttle state")
	}
}

// sendInterruptionAlert is owned exclusively by the run-boundary finalizer. It
// bypasses attempt throttling and never mutates LastAlertedAttempt because an
// operator interruption consumes no deploy-failure budget.
func (r *Reconciler) sendInterruptionAlert(ctx context.Context) {
	if r.alerter == nil || !r.config.OnFailure {
		return
	}

	alertCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), failureAlertDeliveryTimeout)
	defer cancel()
	target := r.alertTarget()
	logger := log.ComponentCtx(alertCtx, log.ComponentReconcile)
	if err := r.alerter.SendDeployFailure(
		alertCtx,
		r.lastCommit,
		target,
		interruptedReconcileReason,
		r.serviceNames(),
		time.Since(r.runStartTime),
	); err != nil {
		logger.Warn().Err(err).
			Str(log.FieldOperation, "alert_failure").
			Str(log.FieldTarget, target).
			Msg("Failed to send interrupted reconcile alert")
	}
}

// sendUnhealthyAlert sends a warning notification for unhealthy containers found post-deploy.
func (r *Reconciler) sendUnhealthyAlert(ctx context.Context, containers []string) {
	if r.alerter == nil {
		return
	}

	target := r.alertTarget()

	if err := r.alerter.SendUnhealthyContainers(ctx, target, containers); err != nil {
		logger := log.ComponentCtx(ctx, log.ComponentReconcile)
		logger.Warn().
			Err(err).
			Str(log.FieldOperation, "alert_unhealthy").
			Str(log.FieldTarget, target).
			Int("container_count", len(containers)).
			Msg("Failed to send unhealthy containers alert")
	}
}

// recoveryOutcome reports what happened to a recovery dispatch, because the
// caller clears failure-tracking state differently for each. A caller that
// cannot tell "gate disabled" from "every provider failed" either banks a stale
// retraction for a later config flip, or consumes the retraction on an outage.
type recoveryOutcome int

const (
	// recoveryDispatched: delivered, or there was nothing to deliver to.
	recoveryDispatched recoveryOutcome = iota
	// recoveryDisabled: the gate is off, so no retraction is ever owed.
	recoveryDisabled
	// recoveryDeliveryFailed: every provider failed; retry on the next clean run.
	recoveryDeliveryFailed
)

// sendRecoveryAlert retracts a previously-sent failure alert.
//
// Gated on config.OnRecovery, not OnSuccess. Recovery is not a success-side
// alert: the retract gate must never be more restrictive than the alert gate.
func (r *Reconciler) sendRecoveryAlert(ctx context.Context, priorFailures int) recoveryOutcome {
	// No alerter is "nothing to deliver to", not a delivery failure -- retrying
	// forever against a nil alerter would never succeed.
	if r.alerter == nil {
		return recoveryDispatched
	}

	if !r.config.OnRecovery {
		return recoveryDisabled
	}

	target := r.alertTarget()

	if err := r.alerter.SendDeployRecovery(ctx, r.lastCommit, target, priorFailures); err != nil {
		logger := log.ComponentCtx(ctx, log.ComponentReconcile)
		logger.Warn().
			Err(err).
			Str(log.FieldOperation, "alert_recovery").
			Str(log.FieldTarget, target).
			Int("prior_failures", priorFailures).
			Msg("Failed to send recovery alert, retraction still owed")
		return recoveryDeliveryFailed
	}
	return recoveryDispatched
}

// retractionOwed reports whether a failure alert was sent for this target and
// has not yet been retracted.
//
// The predicate is LastAlertedAttempt, not AttemptCount: a failure below the
// alert threshold (state.go's alertThresholds) never produced an alert, so it
// owes no retraction.
func retractionOwed(state *DeployState) bool {
	return state != nil && state.LastAlertedAttempt > 0
}

// retractFailureAlert dispatches the recovery alert a clean run owes, and
// reports whether the caller may clear failure-tracking state.
//
// Called from every path a run can end cleanly on -- including the two skip
// branches that return before the deploy-path dispatch site. Those two branches
// fail differently: the deploy-path skip zeroes LastAlertedAttempt (destroying
// the evidence), while the already-deployed skip leaves it set (so a dispatch
// there that does not clear it re-alerts on every subsequent run).
func (r *Reconciler) retractFailureAlert(ctx context.Context, state *DeployState) (clearState bool) {
	if !retractionOwed(state) {
		return true
	}
	switch r.sendRecoveryAlert(ctx, state.AttemptCount) {
	case recoveryDeliveryFailed:
		// Keep the evidence so the next clean run re-attempts. A provider
		// outage must not consume the retraction.
		return false
	default:
		// Dispatched, or disabled. Disabled clears too: retaining state would
		// bank a stale retraction that fires whenever the gate is turned on.
		return true
	}
}
