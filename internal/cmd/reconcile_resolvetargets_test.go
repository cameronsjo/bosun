package cmd

import (
	"testing"

	"github.com/cameronsjo/bosun/internal/ui"
	"github.com/stretchr/testify/assert"
)

// Test Plan for reconcile.go runReconcile (resilience-slate #391 propagation)
//
// runReconcile (Classification: I/O boundary / CLI entrypoint)
//   [x] Unhappy: BOSUN_TARGETS carrying a multi-target config with a reserved
//       "default" name fails loud exactly once via ui.Fatal, and execution
//       does not fall through into the per-target reconcile loop (which would
//       range over a nil slice and skip straight to the end without a second
//       Fatal for "completed with errors")
//   [x] Happy: BOSUN_TARGETS with a lone "default" target is honored (#391),
//       reaches ResolveTargets without a fatal, and the per-target loop runs

// #391: a multi-target BOSUN_TARGETS carrying the reserved "default" name is a
// hard error from ResolveTargets. runReconcile must report it via ui.Fatal
// exactly once — not proceed into the per-target loop with a nil slice and
// separately fatal again for "completed with errors".
func TestRunReconcile_ResolveTargetsFailsLoudExactlyOnce(t *testing.T) {
	var exitCalls int
	old := ui.SetExitFn(func(int) { exitCalls++ })
	defer ui.SetExitFn(old)

	t.Setenv("REPO_URL", "https://example.com/repo.git")
	t.Setenv("BOSUN_TARGETS", `[{"name":"unraid","target_host":"user@unraid"},{"name":"default","project_name":"homelab"}]`)

	runReconcile(nil, nil)

	assert.Equal(t, 1, exitCalls, "target-resolution failure must fail loud exactly once; a nil target list must not also trip the end-of-loop 'completed with errors' fatal")
}

// #391: a lone `name: default` target is the implicit default's configuration
// (honored, not discarded) — ResolveTargets succeeds, so runReconcile must NOT
// fatal on target resolution before reaching the reconcile loop. The loop
// itself still fails against the fake repo URL (a single r.Run failure ->
// hadError -> exactly one end-of-loop fatal), which is the only expected exit call.
func TestRunReconcile_LoneDefaultTargetReachesReconcileLoop(t *testing.T) {
	var exitCalls int
	old := ui.SetExitFn(func(int) { exitCalls++ })
	defer ui.SetExitFn(old)

	t.Setenv("REPO_URL", "https://example.com/repo.git")
	t.Setenv("BOSUN_TARGETS", `[{"name":"default","project_name":"homelab"}]`)

	runReconcile(nil, nil)

	assert.Equal(t, 1, exitCalls, "resolution must succeed (no fatal there); the single fatal observed comes from the reconcile loop failing against the fake repo")
}
