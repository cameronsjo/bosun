package cmd

import (
	"path/filepath"
	"testing"

	"github.com/cameronsjo/bosun/internal/ui"
	"github.com/stretchr/testify/assert"
)

// #391: target-resolution outcomes through the CLI entrypoint. ui.Fatal is
// stubbed, so a "fatal" records an exit call and execution continues — which
// is exactly what lets the first case assert the failure fires exactly once
// (a nil target list must not also trip the end-of-loop "completed with
// errors" fatal). REPO_URL points at a nonexistent LOCAL path so the clone
// fails fast and deterministically — no DNS, no network.
func TestRunReconcile_TargetResolution(t *testing.T) {
	tests := []struct {
		name          string
		targetsJSON   string
		wantExitCalls int
		reason        string
	}{
		{
			name:          "multi-target with reserved default fails loud exactly once",
			targetsJSON:   `[{"name":"unraid","target_host":"user@unraid"},{"name":"default","project_name":"homelab"}]`,
			wantExitCalls: 1,
			reason:        "resolution fatal must fire once; the nil target list must not also trip the end-of-loop fatal",
		},
		{
			// Resolution succeeds (#391 honors the lone default), so the only
			// expected exit call is the reconcile loop failing against the
			// nonexistent local repo path (hadError -> one end-of-loop fatal).
			name:          "lone default target reaches the reconcile loop",
			targetsJSON:   `[{"name":"default","project_name":"homelab"}]`,
			wantExitCalls: 1,
			reason:        "resolution must succeed; the single fatal comes from the loop failing against the missing repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var exitCalls int
			old := ui.SetExitFn(func(int) { exitCalls++ })
			defer ui.SetExitFn(old)

			t.Setenv("REPO_URL", filepath.Join(t.TempDir(), "missing-repo"))
			t.Setenv("BOSUN_TARGETS", tt.targetsJSON)

			runReconcile(nil, nil)

			assert.Equal(t, tt.wantExitCalls, exitCalls, tt.reason)
		})
	}
}
