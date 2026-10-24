package reconcile

import (
	"context"
	"errors"
	"time"

	"github.com/cameronsjo/bosun/internal/log"
)

const interruptedReconcileReason = "reconciliation interrupted by caller cancellation"

type attemptBudgetSnapshot struct {
	state               *DeployState
	commit              string
	lastAttemptedCommit string
	attemptCount        int
	lastAlertedAttempt  int
}

func snapshotAttemptBudget(state *DeployState, commit string) *attemptBudgetSnapshot {
	return &attemptBudgetSnapshot{
		state:               state,
		commit:              commit,
		lastAttemptedCommit: state.LastAttemptedCommit,
		attemptCount:        state.AttemptCount,
		lastAlertedAttempt:  state.LastAlertedAttempt,
	}
}

// isPropagatedCallerCancellation deliberately requires both signals. A child
// operation returning context.Canceled while its caller remains live is a real
// failure, as is a non-cancellation error that races with caller shutdown.
func isPropagatedCallerCancellation(ctx context.Context, err error) bool {
	return errors.Is(ctx.Err(), context.Canceled) && errors.Is(err, context.Canceled)
}

func (r *Reconciler) finalizeAttempt(ctx context.Context, runErr error, snapshot *attemptBudgetSnapshot) {
	recoverySent := r.recoverySentThisRun
	r.recoverySentThisRun = false
	if snapshot == nil {
		return
	}

	state := snapshot.state
	logger := log.ComponentCtx(ctx, log.ComponentReconcile)
	if isPropagatedCallerCancellation(ctx, runErr) {
		state.LastAttemptedCommit = snapshot.lastAttemptedCommit
		state.AttemptCount = snapshot.attemptCount
		state.LastAlertedAttempt = snapshot.lastAlertedAttempt
		state.LastAttemptOutcome = &LastAttemptOutcome{
			Outcome:   attemptOutcomeInterrupted,
			Commit:    snapshot.commit,
			Timestamp: time.Now().UTC(),
		}
		if err := SaveState(r.config.StateFile, state); err != nil {
			logger.Error().Err(err).Str(log.FieldPath, r.config.StateFile).Msg("Failed to persist interrupted reconcile outcome")
		}
		r.sendInterruptionAlert(ctx)
		return
	}

	// Only a clean run settles an interruption. A failed run keeps the marker
	// so the recovery is still owed when a later run succeeds.
	if state.LastAttemptOutcome == nil || runErr != nil {
		return
	}

	// An interruption alert told the operator the deploy is broken, so a later
	// clean run owes a retraction whatever LastAlertedAttempt says. Zero prior failures selects the interruption wording. The budget
	// field is untouched by interruptions, so retractFailureAlert cannot see
	// this debt. Skip it when that path already retracted during this run.
	if !recoverySent && state.LastAttemptOutcome.Outcome == attemptOutcomeInterrupted {
		if r.sendRecoveryAlert(ctx, 0) == recoveryDeliveryFailed {
			// Keep the marker so the next clean run retries.
			return
		}
	}

	state.LastAttemptOutcome = nil
	if err := SaveState(r.config.StateFile, state); err != nil {
		logger.Error().Err(err).Str(log.FieldPath, r.config.StateFile).Msg("Failed to clear stale interrupted reconcile outcome")
	}
}
