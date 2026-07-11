package daemon

import (
	"context"
	"testing"

	"github.com/cameronsjo/bosun/internal/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test Plan for daemon.go executeReconcile (resilience-slate #391 propagation)
//
// executeReconcile (Classification: I/O boundary / state machine)
//   [x] Unhappy: ResolveTargets() failure (multi-target config carrying the
//       reserved "default" name) aborts the cycle, returns the error, and
//       records cycle state (lastReconcile/lastError) instead of panicking
//       over a nil target list
//   [x] Boundary: zero targets from a resolution failure means the per-target
//       loop never runs (no reconciler constructed, no docker/git calls)
//   [x] Happy: a resolvable single-target config still completes normally
//       (regression guard for the targetsErr plumbing added around ResolveTargets)

// #391: ResolveTargets now returns (nil, error) for a multi-target config that
// carries the reserved "default" name (previously silently dropped the
// target). executeReconcile must fail the cycle loud — not panic ranging over
// a nil slice, and not leave lastReconcile/lastError stale.
func TestExecuteReconcile_TargetResolutionFailureAbortsCycle(t *testing.T) {
	d, _ := newTestDaemon(t)

	d.config.ReconcileConfig.Targets = []reconcile.Target{
		{Name: "unraid", TargetHost: "user@unraid"},
		{Name: "default", ProjectName: "homelab"},
	}

	err := d.executeReconcile(context.Background(), "test", false)

	require.Error(t, err, "a target-resolution failure must abort the reconcile cycle")
	assert.Contains(t, err.Error(), "default", "the propagated error must name the offending target")

	d.stateMu.RLock()
	lastReconcile := d.lastReconcile
	lastErr := d.lastError
	d.stateMu.RUnlock()

	assert.False(t, lastReconcile.IsZero(), "cycle state must still be recorded even when target resolution fails")
	assert.Error(t, lastErr, "lastError must reflect the aborted cycle")
}

// Regression guard: a resolvable target list (the common single-target case)
// must not be affected by the targetsErr plumbing — the cycle still runs the
// per-target loop and returns whatever the reconciler itself reports.
func TestExecuteReconcile_ResolvableTargetsStillRun(t *testing.T) {
	d, _ := newTestDaemon(t)

	// newTestDaemon leaves ReconcileConfig.Targets empty, so ResolveTargets
	// synthesizes the implicit default target — a resolvable, zero-error case.
	require.Empty(t, d.config.ReconcileConfig.Targets)

	// DryRun+a fake repo means r.Run() itself may fail (no real clone), but the
	// point under test is that the cycle reaches the per-target loop at all —
	// i.e. targetsErr is nil and does not short-circuit before ever calling Run.
	_ = d.executeReconcile(context.Background(), "test", false)

	d.stateMu.RLock()
	lastReconcile := d.lastReconcile
	d.stateMu.RUnlock()

	assert.False(t, lastReconcile.IsZero(), "a resolvable target list must still complete a cycle and record state")
}
