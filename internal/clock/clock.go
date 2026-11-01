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

func (Real) Now() time.Time { return time.Now() }

// Fake is a manually advanced clock, safe for concurrent use.
type Fake struct {
	mu  sync.Mutex
	now time.Time
}

func NewFake(start time.Time) *Fake { return &Fake{now: start} }

func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	f.now = f.now.Add(d)
	f.mu.Unlock()
}

func (f *Fake) Set(t time.Time) {
	f.mu.Lock()
	f.now = t
	f.mu.Unlock()
}
