// Package deadline provides a context timeout whose clock can be paused.
//
// The daemon bounds each reconcile cycle with one timeout. Some phases inside
// the cycle carry their own, tighter per-step bounds (isolated compose up gives
// every compose file its own deadline). Those phases pause the cycle clock so
// their time does not come out of the budget the rest of the cycle needs, and
// so the cycle deadline cannot cut a later step short (#727). Caller
// cancellation still propagates while the clock is paused.
package deadline

import (
	"context"
	"sync"
	"time"
)

// Clock is the time source for a pausable timeout. Tests inject a fake one.
type Clock interface {
	Now() time.Time
	// AfterFunc calls f in its own goroutine once d has elapsed. stop prevents
	// the call if it has not happened yet and reports whether it did so.
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

// pausableCtx expires after budget of unpaused time. It keeps its own done
// channel (rather than wrapping a context.WithCancel) so contexts derived from
// it observe its Err — context.DeadlineExceeded on expiry — instead of the
// inner cancel context's context.Canceled.
type pausableCtx struct {
	parent context.Context
	clock  Clock
	done   chan struct{}

	mu         sync.Mutex
	err        error
	remaining  time.Duration // budget left when the clock last stopped
	runningAt  time.Time     // when the clock last started (valid while running)
	pauses     int
	stopTimer  func() bool
	stopParent func() bool
}

// WithPausableTimeout returns a context that expires once d of unpaused time
// has passed, and its cancel function. It is canceled early when parent is.
func WithPausableTimeout(parent context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	return WithPausableTimeoutClock(parent, d, RealClock)
}

// WithPausableTimeoutClock is WithPausableTimeout with an explicit clock.
func WithPausableTimeoutClock(parent context.Context, d time.Duration, clock Clock) (context.Context, context.CancelFunc) {
	c := &pausableCtx{
		parent:    parent,
		clock:     clock,
		done:      make(chan struct{}),
		remaining: d,
	}
	if err := parent.Err(); err != nil {
		c.finishLocked(err)
		return c, func() {}
	}
	stopParent := context.AfterFunc(parent, func() { c.finish(parent.Err()) })
	c.mu.Lock()
	if c.err != nil {
		stopParent()
	} else {
		c.stopParent = stopParent
		c.startLocked()
	}
	c.mu.Unlock()
	return c, func() { c.finish(context.Canceled) }
}

// startLocked runs the clock for the remaining budget. Caller holds c.mu.
func (c *pausableCtx) startLocked() {
	if c.remaining <= 0 {
		c.finishLocked(context.DeadlineExceeded)
		return
	}
	c.runningAt = c.clock.Now()
	c.stopTimer = c.clock.AfterFunc(c.remaining, func() { c.finish(context.DeadlineExceeded) })
}

func (c *pausableCtx) finish(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.finishLocked(err)
}

// finishLocked records err and closes done once. Caller holds c.mu (or is the
// constructor, before c is shared).
func (c *pausableCtx) finishLocked(err error) {
	if c.err != nil {
		return
	}
	c.err = err
	if c.stopTimer != nil {
		c.stopTimer()
		c.stopTimer = nil
	}
	if c.stopParent != nil {
		c.stopParent()
	}
	close(c.done)
}

// Deadline reports the cycle deadline while the clock runs. While paused there
// is no cycle deadline, so it reports the parent's, if any.
func (c *pausableCtx) Deadline() (time.Time, bool) {
	c.mu.Lock()
	paused := c.err == nil && c.pauses > 0
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

func (c *pausableCtx) Done() <-chan struct{} { return c.done }

func (c *pausableCtx) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *pausableCtx) Value(key any) any {
	if key == (pausableKey{}) {
		return c
	}
	return c.parent.Value(key)
}

// pause stops the clock. Pauses nest; the clock runs again after the last
// matching resume.
func (c *pausableCtx) pause() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return
	}
	c.pauses++
	if c.pauses > 1 {
		return
	}
	if c.stopTimer != nil && !c.stopTimer() {
		// The timer already fired; its finish call is waiting on c.mu and
		// will expire the context. Leave the budget spent.
		c.remaining = 0
		c.stopTimer = nil
		return
	}
	c.stopTimer = nil
	c.remaining -= c.clock.Now().Sub(c.runningAt)
}

func (c *pausableCtx) resume() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil || c.pauses == 0 {
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
	c, ok := ctx.Value(pausableKey{}).(*pausableCtx)
	if !ok {
		return func() {}
	}
	c.pause()
	var once sync.Once
	return func() { once.Do(c.resume) }
}
