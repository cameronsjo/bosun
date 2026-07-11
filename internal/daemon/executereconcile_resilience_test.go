package daemon

import (
	"context"
	"testing"

	"github.com/cameronsjo/bosun/internal/reconcile"
	"github.com/stretchr/testify/assert"
)

// #391: ResolveTargets now returns (nil, error) for a multi-target config that
// carries the reserved "default" name (previously silently dropped the
// target). executeReconcile must fail the cycle loud — not panic ranging over
// a nil slice — while a resolvable list still reaches the per-target loop.
// Both cases must record cycle state (lastReconcile/lastError).
func TestExecuteReconcile_TargetResolution(t *testing.T) {
	tests := []struct {
		name            string
		targets         []reconcile.Target
		wantErr         bool
		wantErrContains string
	}{
		{
			name: "multi-target config with reserved default aborts the cycle loud",
			targets: []reconcile.Target{
				{Name: "unraid", TargetHost: "user@unraid"},
				{Name: "default", ProjectName: "homelab"},
			},
			wantErr:         true,
			wantErrContains: "default",
		},
		{
			// Empty targets synthesize the implicit default — a resolvable,
			// zero-error case. DryRun + a fake repo means r.Run() itself may
			// fail; the point under test is that targetsErr is nil and the
			// cycle reaches the per-target loop at all.
			name:    "resolvable target list still runs the per-target loop",
			targets: nil,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, _ := newTestDaemon(t)
			d.config.ReconcileConfig.Targets = tt.targets

			err := d.executeReconcile(context.Background(), "test", false)

			if tt.wantErr {
				assert.Error(t, err, "a target-resolution failure must abort the reconcile cycle")
				assert.Contains(t, err.Error(), tt.wantErrContains, "the propagated error must name the offending target")
			}

			d.stateMu.RLock()
			lastReconcile := d.lastReconcile
			lastErr := d.lastError
			d.stateMu.RUnlock()

			assert.False(t, lastReconcile.IsZero(), "cycle state must be recorded whether or not resolution fails")
			if tt.wantErr {
				assert.Error(t, lastErr, "lastError must reflect the aborted cycle")
			}
		})
	}
}
