// Package clock abstracts time so tests can control it.
package clock

import (
	"sync"
	"time"
)

// Clock is injected wherever the current time is needed.
type Clock interface {
	Now() time.Time
}

// Real reads the system clock.
type Real struct{}

// Now returns the current system time.
func (Real) Now() time.Time { return time.Now() }

// Fake is a manually advanced clock, safe for concurrent use.
type Fake struct {
	mu  sync.Mutex
	now time.Time
}

// NewFake returns a Fake clock that starts at start.
func NewFake(start time.Time) *Fake { return &Fake{now: start} }

// Now returns the time of the fake clock.
func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// Advance moves the fake clock forward by d.
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	f.now = f.now.Add(d)
	f.mu.Unlock()
}

// Set sets the fake clock to t.
func (f *Fake) Set(t time.Time) {
	f.mu.Lock()
	f.now = t
	f.mu.Unlock()
}
