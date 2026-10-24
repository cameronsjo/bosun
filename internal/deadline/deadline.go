// Package deadline provides a context timeout whose clock can be paused.
//
// The daemon bounds each reconcile cycle with one timeout. Some phases inside
// the cycle carry their own, tighter per-step bounds (isolated compose up gives
// every compose file its own deadline). Those phases pause the cycle clock so
// their time does not come out of the budget the rest of the cycle needs, and
// so the cycle deadline cannot cut a later step short (#727). Caller
// cancellation still propagates while the clock is paused, and a separate hard
// cap (WithCycleBudget) keeps running so a long run of slow steps still ends.
package deadline

import (
	"context"
	"sync"
	"time"
)

// Clock is the time source for these timeouts. Tests inject a fake one
// (package deadlinetest).
type Clock interface {
	Now() time.Time
	// AfterFunc calls f once d has elapsed. stop prevents the call if it has
	// not happened yet and reports whether it did so.
	AfterFunc(d time.Duration, f func()) (stop func() bool)
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (realClock) AfterFunc(d time.Duration, f func()) func() bool {
	return time.AfterFunc(d, f).Stop
}

// RealClock is the wall clock.
var RealClock Clock = realClock{}

type pausableKey struct{}

type cycleCapKey struct{}

// timeoutCtx expires after a budget of running time. Cancellation rides on an
// inner context.WithCancelCause, so it reaches this context synchronously from
// a stdlib parent (or from another timeoutCtx, whose inner context it derives
// from). Err reports the reason itself: context.DeadlineExceeded when this
// context's own budget ran out, otherwise the parent's error. A plain
// WithCancelCause would report context.Canceled for both.
type timeoutCtx struct {
	parent      context.Context
	inner       context.Context
	cancelInner context.CancelCauseFunc
	clock       Clock
	// pausable is false for a hard cap: Pause skips it and its clock always runs.
	pausable bool
	// grace is set on a cycle cap (WithCycleBudget): cleanup may run until the
	// cap's deadline plus grace (CleanupContext).
	grace    time.Duration
	cycleCap bool
	// capDeadline is a cycle cap's fixed deadline; a cap never pauses, so it
	// is known when the cycle starts.
	capDeadline time.Time

	mu        sync.Mutex
	expiredAt time.Time     // the deadline this timeout expired at (zero if it did not)
	err       error         // set when this context ended itself (expiry or cancel)
	remaining time.Duration // budget left when the clock last stopped
	runningAt time.Time     // when the clock last started (valid while running)
	pauses    int
	stopTimer func() bool
}

// WithPausableTimeout returns a context that expires once d of unpaused time
// has passed, and its cancel function. It is canceled early when parent is.
func WithPausableTimeout(parent context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	return WithPausableTimeoutClock(parent, d, RealClock)
}

// WithPausableTimeoutClock is WithPausableTimeout with an explicit clock.
func WithPausableTimeoutClock(parent context.Context, d time.Duration, clock Clock) (context.Context, context.CancelFunc) {
	return newTimeout(parent, d, clock, true)
}

// WithTimeoutClock is context.WithTimeout on an explicit clock. Pause never
// stops it: it is a hard cap.
func WithTimeoutClock(parent context.Context, d time.Duration, clock Clock) (context.Context, context.CancelFunc) {
	return newTimeout(parent, d, clock, false)
}

// WithCycleBudget returns the context for one reconcile cycle. timeout is the
// pausable cycle budget: phases with their own per-step bounds pause it.
// maxTimeout is a hard cap on the whole cycle that keeps running while the
// budget is paused, so a long run of slow steps still ends. A maxTimeout below
// timeout is raised to timeout. grace extends the cap for cleanup only
// (CleanupContext): backup extraction and rollbacks end by the cap's deadline
// plus grace, so the deploy work of a cycle ends within maxTimeout + grace.
func WithCycleBudget(parent context.Context, timeout, maxTimeout, grace time.Duration, clock Clock) (context.Context, context.CancelFunc) {
	if maxTimeout < timeout {
		maxTimeout = timeout
	}
	capCtx, capCancel := newTimeout(parent, maxTimeout, clock, false)
	capT := capCtx.(*timeoutCtx)
	capT.mu.Lock()
	capT.cycleCap = true
	capT.grace = grace
	capT.capDeadline = capT.runningAt.Add(maxTimeout)
	capT.mu.Unlock()
	ctx, cancel := WithPausableTimeoutClock(capCtx, timeout, clock)
	return ctx, func() {
		cancel()
		capCancel()
	}
}

func newTimeout(parent context.Context, d time.Duration, clock Clock, pausable bool) (context.Context, context.CancelFunc) {
	innerParent := parent
	if p, ok := parent.(*timeoutCtx); ok {
		innerParent = p.inner // synchronous propagation between our own contexts
	}
	inner, cancelInner := context.WithCancelCause(innerParent)
	c := &timeoutCtx{
		parent:      parent,
		inner:       inner,
		cancelInner: cancelInner,
		clock:       clock,
		pausable:    pausable,
		remaining:   d,
	}
	c.mu.Lock()
	c.startLocked()
	c.mu.Unlock()
	// Release the timer when the parent ends first.
	stop := context.AfterFunc(inner, func() { c.end(nil) })
	return c, func() {
		stop()
		c.end(context.Canceled)
	}
}

// startLocked runs the clock for the remaining budget. Caller holds c.mu.
func (c *timeoutCtx) startLocked() {
	if c.inner.Err() != nil {
		return
	}
	c.runningAt = c.clock.Now()
	if c.remaining <= 0 {
		c.endLocked(context.DeadlineExceeded)
		return
	}
	c.stopTimer = c.clock.AfterFunc(c.remaining, func() { c.end(context.DeadlineExceeded) })
}

// end stops the clock and, when err is non-nil and the context is still live,
// ends it with err.
func (c *timeoutCtx) end(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.endLocked(err)
}

func (c *timeoutCtx) endLocked(err error) {
	if c.stopTimer != nil {
		c.stopTimer()
		c.stopTimer = nil
	}
	if err == nil || c.err != nil || c.inner.Err() != nil {
		return
	}
	// Record the reason before closing Done, so a reader woken by Done sees it.
	c.err = err
	if err == context.DeadlineExceeded {
		c.expiredAt = c.runningAt.Add(c.remaining)
	}
	c.cancelInner(err)
}

// Deadline reports the timeout's deadline while its clock runs. While paused
// it has none, so it reports the parent's, if any.
func (c *timeoutCtx) Deadline() (time.Time, bool) {
	c.mu.Lock()
	paused := c.pauses > 0
	own := c.runningAt.Add(c.remaining)
	c.mu.Unlock()
	parentDeadline, parentOK := c.parent.Deadline()
	if paused {
		return parentDeadline, parentOK
	}
	if parentOK && parentDeadline.Before(own) {
		return parentDeadline, true
	}
	return own, true
}

func (c *timeoutCtx) Done() <-chan struct{} { return c.inner.Done() }

func (c *timeoutCtx) Err() error {
	if c.inner.Err() == nil {
		return nil
	}
	c.mu.Lock()
	err := c.err
	c.mu.Unlock()
	if err != nil {
		return err
	}
	return c.parent.Err()
}

func (c *timeoutCtx) Value(key any) any {
	if key == (pausableKey{}) && c.pausable {
		return c
	}
	if key == (cycleCapKey{}) && c.cycleCap {
		return c
	}
	return c.parent.Value(key)
}

// pause stops the clock. Pauses nest; the clock runs again after the last
// matching resume.
func (c *timeoutCtx) pause() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pauses++
	if c.pauses > 1 || c.inner.Err() != nil {
		return
	}
	if c.stopTimer != nil && !c.stopTimer() {
		// The timer already fired; its end call is waiting on c.mu and will
		// expire the context. Leave the budget spent.
		c.remaining = 0
		c.stopTimer = nil
		return
	}
	c.stopTimer = nil
	c.remaining -= c.clock.Now().Sub(c.runningAt)
}

func (c *timeoutCtx) resume() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pauses == 0 {
		return
	}
	c.pauses--
	if c.pauses == 0 {
		c.startLocked()
	}
}

// Pause stops the clock of the nearest pausable timeout in ctx's ancestry and
// returns the function that restarts it with the budget that was left. With no
// pausable timeout in ctx, Pause does nothing. resume is safe to call more than
// once; only the first call counts.
func Pause(ctx context.Context) (resume func()) {
	c, ok := ctx.Value(pausableKey{}).(*timeoutCtx)
	if !ok {
		return func() {}
	}
	c.pause()
	var once sync.Once
	return func() { once.Do(c.resume) }
}

// CapExpired reports whether the hard cap of the cycle ctx belongs to
// (WithCycleBudget) has fired. It reads the cap directly, so it is accurate
// even through stdlib contexts derived from the cycle, which learn of the cap
// asynchronously.
func CapExpired(ctx context.Context) bool {
	c, ok := ctx.Value(cycleCapKey{}).(*timeoutCtx)
	if !ok {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.expiredAt.IsZero()
}

// CleanupDeadline returns the latest time cleanup in the cycle ctx belongs to
// may run: the cycle cap's deadline plus its grace (WithCycleBudget). With no
// cycle cap in ctx it returns false.
func CleanupDeadline(ctx context.Context) (time.Time, bool) {
	c, ok := ctx.Value(cycleCapKey{}).(*timeoutCtx)
	if !ok {
		return time.Time{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.capDeadline.Add(c.grace), true
}

// CleanupContext returns a context for cleanup work (backup extraction,
// rollbacks): it keeps ctx's values but not its cancellation, so a canceled
// deploy is still rolled back, and it ends after timeout or at
// CleanupDeadline, whichever comes first, whether or not the cap has fired
// yet. With no cycle cap in ctx it ends after timeout.
func CleanupContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	detached := context.WithoutCancel(ctx)
	c, ok := ctx.Value(cycleCapKey{}).(*timeoutCtx)
	if !ok {
		return context.WithTimeout(detached, timeout)
	}
	c.mu.Lock()
	limit := c.capDeadline.Add(c.grace).Sub(c.clock.Now())
	clock := c.clock
	c.mu.Unlock()
	return newTimeout(detached, min(timeout, limit), clock, false)
}
