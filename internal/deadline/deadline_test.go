package deadline

import (
	"context"
	"testing"
	"time"

	"github.com/cameronsjo/bosun/internal/deadline/deadlinetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var epoch = time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)

func TestPausableTimeout_ExpiresAfterBudget(t *testing.T) {
	clock := deadlinetest.NewFakeClock(epoch)
	ctx, cancel := WithPausableTimeoutClock(context.Background(), 10*time.Minute, clock)
	defer cancel()

	dl, ok := ctx.Deadline()
	require.True(t, ok)
	assert.Equal(t, epoch.Add(10*time.Minute), dl)

	clock.Advance(9 * time.Minute)
	require.NoError(t, ctx.Err())
	clock.Advance(time.Minute)
	assert.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
	<-ctx.Done()
}

func TestPausableTimeout_PauseKeepsRemainingBudget(t *testing.T) {
	clock := deadlinetest.NewFakeClock(epoch)
	ctx, cancel := WithPausableTimeoutClock(context.Background(), 10*time.Minute, clock)
	defer cancel()

	clock.Advance(4 * time.Minute)
	resume := Pause(ctx)
	_, ok := ctx.Deadline()
	assert.False(t, ok, "a paused cycle has no deadline of its own")

	clock.Advance(3 * time.Hour)
	require.NoError(t, ctx.Err(), "paused time must not count against the budget")

	resume()
	resume() // idempotent
	dl, ok := ctx.Deadline()
	require.True(t, ok)
	assert.Equal(t, clock.Now().Add(6*time.Minute), dl, "the 6 minutes left at pause remain")

	clock.Advance(6*time.Minute - time.Second)
	require.NoError(t, ctx.Err())
	clock.Advance(time.Second)
	assert.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
}

func TestPausableTimeout_NestedPauses(t *testing.T) {
	clock := deadlinetest.NewFakeClock(epoch)
	ctx, cancel := WithPausableTimeoutClock(context.Background(), time.Minute, clock)
	defer cancel()

	outer := Pause(ctx)
	inner := Pause(ctx)
	inner()
	clock.Advance(time.Hour)
	require.NoError(t, ctx.Err(), "the clock stays paused until the outer pause resumes")
	outer()
	clock.Advance(time.Minute)
	assert.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
}

func TestPausableTimeout_ParentCancelWhilePaused(t *testing.T) {
	parent, parentCancel := context.WithCancel(context.Background())
	ctx, cancel := WithPausableTimeoutClock(parent, time.Minute, deadlinetest.NewFakeClock(epoch))
	defer cancel()

	resume := Pause(ctx)
	defer resume()
	parentCancel()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("parent cancellation must propagate while paused")
	}
	assert.ErrorIs(t, ctx.Err(), context.Canceled)
}

func TestPausableTimeout_DerivedContextSeesDeadlineExceeded(t *testing.T) {
	clock := deadlinetest.NewFakeClock(epoch)
	ctx, cancel := WithPausableTimeoutClock(context.Background(), time.Minute, clock)
	defer cancel()

	// A real-clock child with a later deadline follows the parent's expiry and
	// must report DeadlineExceeded (callers tell a timeout from a shutdown by it).
	child, childCancel := context.WithTimeout(ctx, time.Hour)
	defer childCancel()
	clock.Advance(time.Minute)
	select {
	case <-child.Done():
	case <-time.After(time.Second):
		t.Fatal("child must follow the pausable parent's expiry")
	}
	assert.ErrorIs(t, child.Err(), context.DeadlineExceeded)
}

func TestPausableTimeout_ChildCreatedWhilePausedGetsOwnDeadline(t *testing.T) {
	ctx, cancel := WithPausableTimeoutClock(context.Background(), time.Nanosecond, deadlinetest.NewFakeClock(epoch))
	defer cancel()
	resume := Pause(ctx)
	defer resume()

	child, childCancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer childCancel()
	<-child.Done()
	assert.ErrorIs(t, child.Err(), context.DeadlineExceeded)
	assert.NoError(t, ctx.Err(), "the child's own deadline does not expire the paused parent")
}

func TestPausableTimeout_CancelAndAlreadyCanceledParent(t *testing.T) {
	ctx, cancel := WithPausableTimeoutClock(context.Background(), time.Minute, deadlinetest.NewFakeClock(epoch))
	cancel()
	assert.ErrorIs(t, ctx.Err(), context.Canceled)
	Pause(ctx)() // no-op on a finished context

	parent, parentCancel := context.WithCancel(context.Background())
	parentCancel()
	done, cancel2 := WithPausableTimeout(parent, time.Minute)
	defer cancel2()
	assert.ErrorIs(t, done.Err(), context.Canceled)
}

func TestPause_WithoutPausableTimeoutIsNoop(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	Pause(ctx)()
	assert.NoError(t, ctx.Err())
}

func TestPausableTimeout_ValueDelegatesToParent(t *testing.T) {
	type key struct{}
	parent := context.WithValue(context.Background(), key{}, "v")
	ctx, cancel := WithPausableTimeout(parent, time.Minute)
	defer cancel()
	assert.Equal(t, "v", ctx.Value(key{}))
}

func TestCycleBudget_CapRunsWhilePaused(t *testing.T) {
	clock := deadlinetest.NewFakeClock(epoch)
	ctx, cancel := WithCycleBudget(context.Background(), 10*time.Minute, 30*time.Minute, 5*time.Minute, clock)
	defer cancel()

	resume := Pause(ctx)
	defer resume()
	clock.Advance(29 * time.Minute)
	require.NoError(t, ctx.Err(), "paused budget, cap not yet reached")
	clock.Advance(time.Minute)
	assert.ErrorIs(t, ctx.Err(), context.DeadlineExceeded, "the hard cap ends the cycle even while the budget is paused")
}

func TestCycleBudget_BudgetStillExpiresUnpaused(t *testing.T) {
	clock := deadlinetest.NewFakeClock(epoch)
	ctx, cancel := WithCycleBudget(context.Background(), 10*time.Minute, 30*time.Minute, 5*time.Minute, clock)
	defer cancel()
	clock.Advance(10 * time.Minute)
	assert.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
}

func TestCycleBudget_CapBelowTimeoutIsRaised(t *testing.T) {
	clock := deadlinetest.NewFakeClock(epoch)
	ctx, cancel := WithCycleBudget(context.Background(), 10*time.Minute, time.Minute, 5*time.Minute, clock)
	defer cancel()
	clock.Advance(9 * time.Minute)
	assert.NoError(t, ctx.Err(), "a cap below the cycle budget is raised to the budget")
	clock.Advance(time.Minute)
	assert.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
}

func TestTimeoutClock_IsNotPausable(t *testing.T) {
	clock := deadlinetest.NewFakeClock(epoch)
	ctx, cancel := WithTimeoutClock(context.Background(), time.Minute, clock)
	defer cancel()
	Pause(ctx)() // finds no pausable timeout; no-op
	resume := Pause(ctx)
	defer resume()
	clock.Advance(time.Minute)
	assert.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
}

func TestCapGrace_SharedAndMeasuredFromTheCap(t *testing.T) {
	clock := deadlinetest.NewFakeClock(epoch)
	ctx, cancel := WithCycleBudget(context.Background(), time.Minute, 10*time.Minute, 5*time.Minute, clock)
	defer cancel()

	_, ok := CapGrace(ctx)
	assert.False(t, ok, "no grace before the cap fires")
	assert.False(t, CapExpired(ctx))

	resume := Pause(ctx)
	defer resume()
	clock.Advance(12 * time.Minute) // the cap fired at 10m; this step overran it by 2m
	require.True(t, CapExpired(ctx))
	type childKey struct{}
	child := context.WithValue(ctx, childKey{}, 1)
	assert.True(t, CapExpired(child), "readable through derived contexts")

	g1, ok := CapGrace(child)
	require.True(t, ok)
	g2, _ := CapGrace(ctx)
	assert.Same(t, g1, g2, "one grace for the whole cycle, not per caller")
	dl, ok := g1.Deadline()
	require.True(t, ok)
	assert.Equal(t, epoch.Add(15*time.Minute), dl, "grace runs from the cap's deadline, not from when cleanup asks")
	require.NoError(t, g1.Err(), "the grace context ignores the ended cycle")

	clock.Advance(3 * time.Minute)
	assert.ErrorIs(t, g1.Err(), context.DeadlineExceeded)
}

func TestCapGrace_NoCycleCap(t *testing.T) {
	ctx, cancel := WithTimeoutClock(context.Background(), time.Minute, deadlinetest.NewFakeClock(epoch))
	defer cancel()
	_, ok := CapGrace(ctx)
	assert.False(t, ok)
	assert.False(t, CapExpired(context.Background()))
}
