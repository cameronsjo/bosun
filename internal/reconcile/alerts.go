package reconcile

import (
	"context"
	"time"

	"github.com/cameronsjo/bosun/internal/log"
)

// alertTarget returns the target identifier for alert messages.
// Uses TargetName when set (multi-target mode); falls back to TargetHost or "local".
func (r *Reconciler) alertTarget() string {
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
func (r *Reconciler) sendThrottledFailureAlert(ctx context.Context, state *DeployState, reason string) {
	if r.alerter == nil {
		return
	}

	if !r.config.OnFailure {
		return
	}

	if !ShouldAlert(state.AttemptCount, state.LastAlertedAttempt) {
		return
	}

	target := r.alertTarget()

	services := r.serviceNames()
	duration := time.Since(r.runStartTime)

	if err := r.alerter.SendDeployFailure(ctx, r.lastCommit, target, reason, services, duration); err != nil {
		logger := log.ComponentCtx(ctx, log.ComponentReconcile)
		logger.Warn().
			Err(err).
			Str(log.FieldOperation, "alert_failure").
			Str(log.FieldTarget, target).
			Msg("Failed to send failure alert")
		return
	}

	state.LastAlertedAttempt = state.AttemptCount
	if err := SaveState(r.config.StateFile, state); err != nil {
		log.Warn().Err(err).Msg("Failed to persist alert throttle state")
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

// sendRecoveryAlert sends a notification when deployment succeeds after failures.
// Gated on config.OnSuccess: recovery is a success-side alert.
func (r *Reconciler) sendRecoveryAlert(ctx context.Context, priorFailures int) {
	if r.alerter == nil {
		return
	}

	if !r.config.OnSuccess {
		return
	}

	target := r.alertTarget()

	if err := r.alerter.SendDeployRecovery(ctx, r.lastCommit, target, priorFailures); err != nil {
		logger := log.ComponentCtx(ctx, log.ComponentReconcile)
		logger.Warn().
			Err(err).
			Str(log.FieldOperation, "alert_recovery").
			Str(log.FieldTarget, target).
			Int("prior_failures", priorFailures).
			Msg("Failed to send recovery alert")
	}
}
