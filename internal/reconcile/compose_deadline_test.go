package reconcile

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/cameronsjo/bosun/internal/deadline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRun_ComposeFilesDoNotShareTheCycleDeadline drives a full run under a
// daemon-style cycle context (10m pausable budget on a fake clock) in which
// each compose file's up takes 6 fake minutes: 12 minutes together, more than
// the cycle budget, though each file is well inside its own ComposeUpTimeout.
// Before #727 the second file ran on the expired cycle context and failed;
// now the cycle clock pauses for the compose phase, every file succeeds, and
// the steps after compose up keep the budget that was left.
func TestRun_ComposeFilesDoNotShareTheCycleDeadline(t *testing.T) {
	prior := steadyPrior()
	prior.NeedsRedeploy = true
	r, calls, _, _ := changedOnlyHarness(t, "services:\n  svc-b:\n    image: alpine:OLD\n", prior, []string{"svc-a", "svc-b"})
	r.deploy.ComposeUpTimeout = 10 * time.Minute

	clock := deadline.NewFakeClock(time.Now())
	cycleCtx, cancel := deadline.WithPausableTimeoutClock(context.Background(), 10*time.Minute, clock)
	defer cancel()

	r.deploy.composeUpFn = func(ctx context.Context, files []string) error {
		*calls = append(*calls, files)
		dl, ok := ctx.Deadline()
		require.True(t, ok, "each file's up must run under its own deadline")
		assert.WithinDuration(t, time.Now().Add(10*time.Minute), dl, time.Minute,
			"the file's deadline is its own ComposeUpTimeout, not what is left of the cycle")
		clock.Advance(6 * time.Minute) // a slow but healthy compose up
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("docker compose up timed out: %w", err)
		}
		return nil
	}

	err := r.Run(cycleCtx)

	require.NoError(t, err, "files that together exceed the cycle budget must not fail")
	assert.Equal(t, []string{"a.yml", "b.yml"}, upFileBases(*calls), "precondition: a full run")
	require.NoError(t, cycleCtx.Err(), "compose time must not come out of the cycle budget")
	dl, ok := cycleCtx.Deadline()
	require.True(t, ok)
	assert.Equal(t, 10*time.Minute, dl.Sub(clock.Now()),
		"the cycle resumes with the budget it had when compose up started")
}

// TestRun_HungComposeFileFailsAtItsOwnDeadline: a.yml's up never returns. It
// must fail when its own ComposeUpTimeout elapses, b.yml must still run with a
// full budget of its own, and — a.yml's rollback failing — the orphan pass is
// skipped (#737 semantics: a timed-out file is a failed file).
func TestRun_HungComposeFileFailsAtItsOwnDeadline(t *testing.T) {
	const perFile = 100 * time.Millisecond
	prior := steadyPrior()
	prior.NeedsRedeploy = true
	r, calls, dockerLog, _ := changedOnlyHarness(t, "services:\n  svc-b:\n    image: alpine:OLD\n", prior, []string{"svc-a", "svc-b"})
	installRunDockerShim(t, dockerLog, 1)
	r.deploy.ComposeUpTimeout = perFile

	clock := deadline.NewFakeClock(time.Now())
	cycleCtx, cancel := deadline.WithPausableTimeoutClock(context.Background(), 10*time.Minute, clock)
	defer cancel()

	var bBudget time.Duration
	r.deploy.composeUpFn = func(ctx context.Context, files []string) error {
		*calls = append(*calls, files)
		if filepath.Base(files[0]) == "a.yml" {
			select { // hung compose up: only its deadline ends it
			case <-ctx.Done():
				return fmt.Errorf("docker compose up timed out: %w", ctx.Err())
			case <-time.After(3 * time.Second):
				t.Error("the hung file's up was not bounded by its own deadline")
				return fmt.Errorf("hung compose up was never canceled")
			}
		}
		dl, ok := ctx.Deadline()
		require.True(t, ok)
		bBudget = time.Until(dl)
		return ctx.Err()
	}

	start := time.Now()
	err := r.Run(cycleCtx)
	elapsed := time.Since(start)

	require.Error(t, err, "a partial deploy is a failed deploy")
	assert.Less(t, elapsed, 10*time.Second, "the hung file must end at its own deadline")
	assert.Equal(t, []string{"a.yml", "b.yml"}, upFileBases(*calls), "the file after the hung one still runs")
	assert.Greater(t, bBudget, perFile/2, "the next file gets its own full budget, not the hung file's leftovers")
	assert.Contains(t, err.Error(), "1 of 2 files failed")
	assert.Contains(t, err.Error(), "orphan pass skipped")
	assert.Contains(t, err.Error(), "a.yml")
	assert.NotContains(t, readDockerLog(t, dockerLog), "--remove-orphans",
		"a timed-out file that was not rolled back blocks the orphan pass")
	assert.NoError(t, cycleCtx.Err(), "the cycle deadline did not run during compose up")
}

// TestComposeUpIsolated_ShutdownCancelsDuringPausedCycle: the cycle clock is
// paused for compose up, but caller cancellation (daemon shutdown) must still
// reach the in-flight compose call immediately.
func TestComposeUpIsolated_ShutdownCancelsDuringPausedCycle(t *testing.T) {
	shutdownCtx, shutdown := context.WithCancel(context.Background())
	cycleCtx, cancel := deadline.WithPausableTimeoutClock(shutdownCtx, time.Minute, deadline.NewFakeClock(time.Now()))
	defer cancel()

	var gotErr error
	d := &DeployOps{
		ComposeUpTimeout: time.Hour,
		composeUpFn: func(ctx context.Context, _ []string) error {
			shutdown()
			select {
			case <-ctx.Done():
				gotErr = ctx.Err()
			case <-time.After(5 * time.Second):
				gotErr = fmt.Errorf("compose call was not canceled")
			}
			return gotErr
		},
	}

	summary, err := d.composeUpIsolated(cycleCtx, []string{"/compose/a.yml"}, "", false)

	require.Error(t, err)
	require.NotNil(t, summary)
	assert.ErrorIs(t, gotErr, context.Canceled, "shutdown, not a deadline, ended the call")
}
