package session

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"hotspotpluslight/internal/clock"
	"hotspotpluslight/internal/domain"
)

// Tuning values of the Enforcer: workers, queue size and retry limits.
const (
	enforcerWorkers = 8
	enforcerQueue   = 1024
	maxTries        = 3 // failed Disconnects per session, then give up
	retryFactor     = 2 // wait 2 x the accounting interval before trying a session again
	forgetAfter     = 1 // hours until a session's record is dropped
	sweepAbove      = 1000
)

// EnforcerRepo is what the Enforcer reads. It reads fresh member and plan, so
// admin changes count.
type EnforcerRepo interface {
	GetMember(ctx context.Context, id int64) (domain.Member, error)
	GetPlan(ctx context.Context, id int64) (domain.Plan, error)
	GetSettings(ctx context.Context) (domain.Settings, error)
	// OpenSessionTarget finds the open session of an accounting key; false when it is not open.
	OpenSessionTarget(ctx context.Context, username, acctSessionID, nasIP string) (domain.SessionTarget, bool, error)
}

// Ender disconnects a session and ends its row with the given cause (*Service).
type Ender interface {
	DisconnectWithCause(ctx context.Context, t Target, cause string) error
}

type sessionKey struct{ username, acctSessionID, nasIP string }

type job struct {
	key      sessionKey
	memberID int64
}

// track is what the Enforcer remembers about one session.
type track struct {
	busy    bool
	tries   int
	retryAt time.Time // not checked again before this; set after a failure and after a success
	last    time.Time
}

// Enforcer is layer 2 of the enforcement (O3/O4): on every Start and Interim of an
// open session it checks the member again (domain.ShouldEnd) and sends a Disconnect
// if the member must not be online. Check never blocks.
type Enforcer struct {
	repo  EnforcerRepo
	clock clock.Clock
	log   *slog.Logger

	jobs    chan job
	mu      sync.Mutex
	tracks  map[sessionKey]*track
	queued  int  // jobs in the channel or being worked on
	stopped bool // set by Stop, before the channel is closed
	wg      sync.WaitGroup
	cancel  context.CancelFunc
}

// NewEnforcer returns an Enforcer. Call Start before Check.
func NewEnforcer(repo EnforcerRepo, clk clock.Clock, log *slog.Logger) *Enforcer {
	return &Enforcer{repo: repo, clock: clk, log: log, jobs: make(chan job, enforcerQueue), tracks: map[sessionKey]*track{}}
}

// Start launches the workers. They disconnect through end.
func (e *Enforcer) Start(end Ender) {
	ctx, cancel := context.WithCancel(context.Background())
	e.cancel = cancel
	for range enforcerWorkers {
		e.wg.Add(1)
		go func() {
			defer e.wg.Done()
			for j := range e.jobs {
				e.work(ctx, end, j)
			}
		}()
	}
}

// Stop ends the workers and waits for them. A second call does nothing.
func (e *Enforcer) Stop() {
	if e.cancel == nil {
		return
	}
	e.mu.Lock()
	if e.stopped {
		e.mu.Unlock()
		return
	}
	e.cancel()
	e.stopped = true // a late Check must not send on the closed channel
	close(e.jobs)
	e.mu.Unlock()
	e.wg.Wait()
}

// Idle reports whether no check is queued or running. Tests use it to wait.
func (e *Enforcer) Idle() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.queued == 0
}

// Check asks for a check of one open session. It never blocks: when the queue
// is full the job is dropped with a warning. A session is checked one at a
// time, and one whose Disconnect failed waits 2 x the accounting interval and
// is given up after 3 failures.
func (e *Enforcer) Check(memberID int64, username, acctSessionID, nasIP string) {
	k := sessionKey{username, acctSessionID, nasIP}
	now := e.clock.Now()
	e.mu.Lock()
	if e.stopped {
		e.mu.Unlock()
		return
	}
	t := e.tracks[k]
	if t != nil && !t.busy && now.Sub(t.last) > forgetAfter*time.Hour {
		delete(e.tracks, k) // a stale record
		t = nil
	}
	if t != nil && (t.busy || t.tries >= maxTries || now.Before(t.retryAt)) {
		e.mu.Unlock()
		return
	}
	if t == nil {
		t = &track{}
		e.tracks[k] = t
	}
	t.busy = true
	e.queued++
	// The send never blocks, so it can be under the lock; then Stop cannot close the channel in between.
	select {
	case e.jobs <- job{key: k, memberID: memberID}:
		e.mu.Unlock()
	default:
		e.mu.Unlock()
		e.log.Warn("enforcement queue is full; check dropped (the next Interim will try again)", "username", username)
		e.finish(k, nil)
	}
}

// finish clears the busy flag and applies update. The record is dropped unless
// it holds failed tries or a "recently ended" mark.
func (e *Enforcer) finish(k sessionKey, update func(*track)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.queued--
	t := e.tracks[k]
	if t == nil {
		return
	}
	t.busy = false
	if update != nil {
		update(t)
	}
	if t.tries == 0 && t.retryAt.IsZero() {
		delete(e.tracks, k)
	}
	if len(e.tracks) > sweepAbove {
		now := e.clock.Now()
		for key, v := range e.tracks {
			if !v.busy && now.Sub(v.last) > forgetAfter*time.Hour {
				delete(e.tracks, key)
			}
		}
	}
}

func (e *Enforcer) work(ctx context.Context, end Ender, j job) {
	reason, target, ok := e.decide(ctx, j)
	if !ok {
		e.finish(j.key, nil)
		return
	}
	cause := "Server: " + string(reason)
	err := end.DisconnectWithCause(ctx, target, cause)
	if err == nil {
		e.log.Info("member disconnected by the server", "username", j.key.username, "reason", string(reason))
		// Keep a "recently ended" mark: a late Interim before the router's Stop must not cause a second Disconnect.
		e.finish(j.key, func(t *track) {
			now := e.clock.Now()
			t.tries, t.last = 0, now
			t.retryAt = now.Add(retryFactor * e.interval(ctx))
		})
		return
	}
	if ctx.Err() != nil { // Stop cancelled us: not a failed try, no log
		e.finish(j.key, nil)
		return
	}
	wait := retryFactor * e.interval(ctx)
	e.finish(j.key, func(t *track) {
		now := e.clock.Now()
		t.tries++
		t.last = now
		t.retryAt = now.Add(wait)
		if t.tries >= maxTries {
			e.log.Warn("giving up: the router did not disconnect this session", "username", j.key.username, "reason", string(reason), "tries", t.tries, "err", err)
		} else {
			e.log.Warn("automatic disconnect failed; will try again later", "username", j.key.username, "reason", string(reason), "tries", t.tries, "err", err)
		}
	})
}

// interval returns the accounting interval, or 60 s when settings cannot be read.
func (e *Enforcer) interval(ctx context.Context) time.Duration {
	st, err := e.repo.GetSettings(ctx)
	if err != nil || st.AccountingIntervalSeconds <= 0 {
		return 60 * time.Second
	}
	return time.Duration(st.AccountingIntervalSeconds) * time.Second
}

// decide loads fresh state and applies domain.ShouldEnd. ok is true when the
// session must be disconnected.
func (e *Enforcer) decide(ctx context.Context, j job) (domain.Reason, Target, bool) {
	fail := func(err error) (domain.Reason, Target, bool) {
		if ctx.Err() == nil {
			e.log.Error("enforcement check failed", "username", j.key.username, "err", err)
		}
		return "", Target{}, false
	}
	m, err := e.repo.GetMember(ctx, j.memberID)
	if err != nil {
		return fail(err)
	}
	plan, err := e.repo.GetPlan(ctx, m.PlanID)
	if err != nil {
		return fail(err)
	}
	reason, end := domain.ShouldEnd(m.State(), plan, e.clock.Now())
	if !end {
		return "", Target{}, false
	}
	t, open, err := e.repo.OpenSessionTarget(ctx, j.key.username, j.key.acctSessionID, j.key.nasIP)
	if err != nil {
		return fail(err)
	}
	return reason, t, open
}
