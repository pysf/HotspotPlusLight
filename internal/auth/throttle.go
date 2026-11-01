package auth

import (
	"sync"
	"time"
)

const (
	loginMaxFailures = 5
	loginLockout     = time.Minute
)

// throttle locks a client out for `lockout` after `max` failures within one
// lockout period. In memory only: a restart forgives (single admin, LAN).
type throttle struct {
	mu      sync.Mutex
	max     int
	lockout time.Duration
	byKey   map[string]*entry
	locks   map[string]*keyLock
}

// keyLock serializes the logins of one client key; refs lets it be dropped.
type keyLock struct {
	mu   sync.Mutex
	refs int
}

type entry struct {
	fails       int
	first       time.Time
	lockedUntil time.Time
}

func newThrottle(max int, lockout time.Duration) *throttle {
	return &throttle{max: max, lockout: lockout, byKey: map[string]*entry{}, locks: map[string]*keyLock{}}
}

// retryAfter returns how long key must wait, or 0 if it may try.
func (t *throttle) retryAfter(key string, now time.Time) time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	if e := t.byKey[key]; e != nil && now.Before(e.lockedUntil) {
		return e.lockedUntil.Sub(now)
	}
	return 0
}

func (t *throttle) failure(key string, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for k, e := range t.byKey { // opportunistic cleanup keeps the map small
		if now.After(e.lockedUntil) && now.Sub(e.first) > t.lockout {
			delete(t.byKey, k)
		}
	}
	e := t.byKey[key]
	if e == nil || now.Sub(e.first) > t.lockout {
		e = &entry{first: now}
		t.byKey[key] = e
	}
	e.fails++
	if e.fails >= t.max {
		e.lockedUntil = now.Add(t.lockout)
		e.fails = 0
		e.first = now
	}
}

func (t *throttle) success(key string) {
	t.mu.Lock()
	delete(t.byKey, key)
	t.mu.Unlock()
}

// lock serializes check, verify and record for one key (other keys are not
// blocked) and returns the release function.
func (t *throttle) lock(key string) (unlock func()) {
	t.mu.Lock()
	l := t.locks[key]
	if l == nil {
		l = &keyLock{}
		t.locks[key] = l
	}
	l.refs++
	t.mu.Unlock()
	l.mu.Lock()
	return func() {
		l.mu.Unlock()
		t.mu.Lock()
		if l.refs--; l.refs == 0 {
			delete(t.locks, key)
		}
		t.mu.Unlock()
	}
}
