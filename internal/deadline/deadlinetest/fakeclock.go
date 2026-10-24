// Package deadlinetest holds test helpers for package deadline. Production
// code must not import it.
package deadlinetest

import (
	"sort"
	"sync"
	"time"
)

// FakeClock is a manually advanced deadline.Clock. Timers fire synchronously
// inside Advance, in deadline order.
type FakeClock struct {
	mu     sync.Mutex
	now    time.Time
	nextID int
	timers map[int]fakeTimer
}

type fakeTimer struct {
	at time.Time
	f  func()
}

// NewFakeClock returns a FakeClock starting at start.
func NewFakeClock(start time.Time) *FakeClock {
	return &FakeClock{now: start, timers: make(map[int]fakeTimer)}
}

// Now returns the fake time.
func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// AfterFunc schedules f for when the fake time reaches Now()+d.
func (c *FakeClock) AfterFunc(d time.Duration, f func()) func() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.nextID
	c.nextID++
	c.timers[id] = fakeTimer{at: c.now.Add(d), f: f}
	return func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		if _, ok := c.timers[id]; !ok {
			return false
		}
		delete(c.timers, id)
		return true
	}
}

// Advance moves the fake time forward by d and runs every timer now due.
func (c *FakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	var due []fakeTimer
	for id, t := range c.timers {
		if !t.at.After(c.now) {
			due = append(due, t)
			delete(c.timers, id)
		}
	}
	c.mu.Unlock()
	sort.Slice(due, func(i, j int) bool { return due[i].at.Before(due[j].at) })
	for _, t := range due {
		t.f()
	}
}
