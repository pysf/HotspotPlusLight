package session

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"hotspotpluslight/internal/clock"
	"hotspotpluslight/internal/domain"
)

type enfRepo struct {
	member domain.Member
	plan   domain.Plan
}

func (r *enfRepo) GetMember(context.Context, int64) (domain.Member, error) { return r.member, nil }
func (r *enfRepo) GetPlan(context.Context, int64) (domain.Plan, error)     { return r.plan, nil }
func (r *enfRepo) GetSettings(context.Context) (domain.Settings, error) {
	return domain.Settings{AccountingIntervalSeconds: 60}, nil
}
func (r *enfRepo) OpenSessionTarget(_ context.Context, u, sid, ip string) (domain.SessionTarget, bool, error) {
	return domain.SessionTarget{ID: 1, Username: u, AcctSessionID: sid, NASIP: ip}, true, nil
}

type enfEnder struct {
	mu     sync.Mutex
	causes []string
	err    error
}

func (e *enfEnder) DisconnectWithCause(_ context.Context, _ Target, cause string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.causes = append(e.causes, cause)
	return e.err
}
func (e *enfEnder) count() int { e.mu.Lock(); defer e.mu.Unlock(); return len(e.causes) }

func settle(t *testing.T, e *Enforcer) {
	t.Helper()
	for i := 0; i < 2000 && !e.Idle(); i++ {
		time.Sleep(time.Millisecond)
	}
	if !e.Idle() {
		t.Fatal("enforcer not idle")
	}
}

func TestEnforcerKicksAndRetries(t *testing.T) {
	repo := &enfRepo{
		member: domain.Member{ID: 1, Active: true, SubscriptionDate: t0, UsedDownload: 2 << 30},
		plan:   domain.Plan{DurationDays: 30, BulkValue: 1, BulkUnit: "GB", MaxSessions: 1},
	}
	end := &enfEnder{err: errors.New("router said no")}
	clk := clock.NewFake(t0)
	e := NewEnforcer(repo, clk, slog.New(slog.NewTextHandler(io.Discard, nil)))
	e.Start(end)
	defer e.Stop()

	check := func() { e.Check(1, "alice", "s1", "10.0.0.1"); settle(t, e) }
	check()
	check() // inside 2 x 60 s: no new try
	if end.count() != 1 || end.causes[0] != "Server: OUT_OF_BULK" {
		t.Fatalf("tries: %v", end.causes)
	}
	for want := 2; want <= 3; want++ {
		clk.Advance(121 * time.Second)
		check()
		if end.count() != want {
			t.Fatalf("want %d tries, have %d", want, end.count())
		}
	}
	clk.Advance(10 * time.Minute)
	check() // gave up
	if end.count() != 3 {
		t.Fatalf("tries after giving up: %d", end.count())
	}

	// A different session is independent, and a success clears the record.
	end.err = nil
	e.Check(1, "alice", "s2", "10.0.0.1")
	settle(t, e)
	if end.count() != 4 {
		t.Fatalf("tries: %d", end.count())
	}
}

func TestEnforcerUnderQuotaDoesNothing(t *testing.T) {
	repo := &enfRepo{
		member: domain.Member{ID: 1, Active: true, SubscriptionDate: t0},
		plan:   domain.Plan{DurationDays: 30, BulkValue: 1, BulkUnit: "GB", MaxSessions: 1},
	}
	end := &enfEnder{}
	e := NewEnforcer(repo, clock.NewFake(t0), slog.New(slog.NewTextHandler(io.Discard, nil)))
	e.Start(end)
	defer e.Stop()
	e.Check(1, "alice", "s1", "10.0.0.1")
	settle(t, e)
	if end.count() != 0 {
		t.Fatalf("kicked: %v", end.causes)
	}
}

func TestEnforcerFullQueueDropsWithoutBlocking(t *testing.T) {
	e := NewEnforcer(&enfRepo{}, clock.NewFake(t0), slog.New(slog.NewTextHandler(io.Discard, nil)))
	// Not started: nothing reads the queue, so it fills up.
	done := make(chan struct{})
	go func() {
		for i := range enforcerQueue + 50 {
			e.Check(1, "u", string(rune('a'+i%26))+time.Duration(i).String(), "10.0.0.1")
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Check blocked")
	}
}

func TestEnforcerStopThenCheckDoesNotPanic(t *testing.T) {
	e := NewEnforcer(&enfRepo{}, clock.NewFake(t0), slog.New(slog.NewTextHandler(io.Discard, nil)))
	e.Start(&enfEnder{})
	e.Stop()
	e.Check(1, "alice", "s1", "10.0.0.1") // must not panic
}

func TestEnforcerNoSecondDisconnectAfterSuccess(t *testing.T) {
	repo := &enfRepo{
		member: domain.Member{ID: 1, Active: true, SubscriptionDate: t0, UsedDownload: 2 << 30},
		plan:   domain.Plan{DurationDays: 30, BulkValue: 1, BulkUnit: "GB", MaxSessions: 1},
	}
	end := &enfEnder{}
	clk := clock.NewFake(t0)
	e := NewEnforcer(repo, clk, slog.New(slog.NewTextHandler(io.Discard, nil)))
	e.Start(end)
	defer e.Stop()
	for range 2 { // a late Interim right after the kick
		e.Check(1, "alice", "s1", "10.0.0.1")
		settle(t, e)
	}
	if end.count() != 1 {
		t.Fatalf("disconnects: %d", end.count())
	}
	clk.Advance(121 * time.Second) // the mark has run out
	e.Check(1, "alice", "s1", "10.0.0.1")
	settle(t, e)
	if end.count() != 2 {
		t.Fatalf("disconnects after the mark: %d", end.count())
	}
}
