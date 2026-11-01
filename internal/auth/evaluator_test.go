package auth

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"hotspotpluslight/internal/clock"
	"hotspotpluslight/internal/domain"
)

type plainCipher struct{}

func (plainCipher) Decrypt(b []byte) ([]byte, error) { return b, nil }

type fakeLogin struct {
	member   domain.Member
	pw       string
	plan     domain.Plan
	planErr  error
	active   int
	renewals int
	err      error
	sub      time.Time // when renewed, the member's new window start
	// raceRenew simulates a concurrent admin edit: the guarded renewal does
	// nothing and leaves the member in this (still lapsed) state.
	raceRenew *domain.Member
}

func (f *fakeLogin) GetMemberForLogin(_ context.Context, u string) (domain.Member, []byte, error) {
	if f.err != nil {
		return domain.Member{}, nil, f.err
	}
	if u != f.member.Username {
		return domain.Member{}, nil, domain.ErrNotFound
	}
	return f.member, []byte(f.pw), nil
}
func (f *fakeLogin) GetPlan(context.Context, int64) (domain.Plan, error) { return f.plan, f.planErr }
func (f *fakeLogin) GetSettings(context.Context) (domain.Settings, error) {
	return domain.Settings{AccountingIntervalSeconds: 60}, nil
}
func (f *fakeLogin) CountActiveSessions(context.Context, int64, time.Time) (int, error) {
	return f.active, nil
}
func (f *fakeLogin) RenewMemberIfUnchanged(_ context.Context, _ int64, _ time.Time, _ string, now time.Time) (bool, error) {
	f.renewals++
	if f.raceRenew != nil {
		f.member = *f.raceRenew
		return false, nil
	}
	f.member.SubscriptionDate = now
	f.member.UsedDownload = 0
	return true, nil
}

var now0 = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

func newFake() *fakeLogin {
	return &fakeLogin{
		member: domain.Member{ID: 1, Username: "alice", Active: true, PlanID: 1, SubscriptionDate: now0},
		pw:     "pw",
		plan:   domain.Plan{ID: 1, DurationDays: 30, MaxSessions: 1, BulkValue: 1, BulkUnit: "GB"},
	}
}

func login(f *fakeLogin, user, pw string) (Verdict, error) {
	return NewEvaluator(f, plainCipher{}, clock.NewFake(now0), slog.New(slog.NewTextHandler(io.Discard, nil))).Login(context.Background(), user, pw)
}

func TestEvaluatorOrder(t *testing.T) {
	f := newFake()
	if v, _ := login(f, "alice", "pw"); !v.Accepted() || v.Grant.XmitLimit != 1<<30 {
		t.Fatalf("%+v", v)
	}
	f.active = 1
	if v, _ := login(f, "alice", "pw"); v.Reason != domain.ReasonNoMoreSession {
		t.Fatalf("%+v", v)
	}
	f.member.Active = false
	if v, _ := login(f, "alice", "pw"); v.Reason != domain.ReasonInactive { // before the session check
		t.Fatalf("%+v", v)
	}
	if v, _ := login(f, "alice", "wrong"); v.Reason != domain.ReasonInvalidCredentials { // before everything
		t.Fatalf("%+v", v)
	}
	if v, _ := login(f, "a b", "pw"); v.Reason != domain.ReasonInvalidCredentials {
		t.Fatalf("%+v", v)
	}
}

func TestEvaluatorPlanChecks(t *testing.T) {
	f := newFake()
	f.member.PlanID = 0
	if v, _ := login(f, "alice", "pw"); v.Reason != domain.ReasonNoPlan {
		t.Fatalf("%+v", v)
	}
	f.member.PlanID = 5
	f.planErr = domain.ErrNotFound
	if v, _ := login(f, "alice", "pw"); v.Reason != domain.ReasonPlanRemoved {
		t.Fatalf("%+v", v)
	}
}

func TestEvaluatorAutoRenew(t *testing.T) {
	f := newFake()
	f.member.SubscriptionDate = now0.AddDate(0, 0, -40)
	f.member.UsedDownload = 1 << 30
	if v, _ := login(f, "alice", "pw"); v.Reason != domain.ReasonNoSubscription || f.renewals != 0 {
		t.Fatalf("%+v renewals=%d", v, f.renewals)
	}
	f.plan.AutoResubscribe = true
	v, err := login(f, "alice", "pw")
	if err != nil || !v.Accepted() || f.renewals != 1 || v.Grant.XmitLimit != 1<<30 {
		t.Fatalf("%+v %v renewals=%d", v, err, f.renewals)
	}
}

func TestEvaluatorInfrastructureError(t *testing.T) {
	f := newFake()
	f.err = errors.New("db down")
	if v, err := login(f, "alice", "pw"); err == nil {
		t.Fatalf("error swallowed: %+v", v)
	}
}

// A concurrent edit can leave the member outside its window after the guarded
// renewal: that is a normal Reject, never "no reply" (reserved for DB failures).
func TestEvaluatorStillLapsedAfterRenewalRejects(t *testing.T) {
	f := newFake()
	f.plan.AutoResubscribe = true
	f.member.SubscriptionDate = now0.AddDate(0, 0, -40)
	other := f.member
	other.SubscriptionDate = now0.AddDate(0, 0, -50) // edited meanwhile, still lapsed
	f.raceRenew = &other
	v, err := login(f, "alice", "pw")
	if err != nil || v.Reason != domain.ReasonNoSubscription {
		t.Fatalf("%+v %v", v, err)
	}
}

func TestEvaluatorUnknownUserRejectsLikeWrongPassword(t *testing.T) {
	f := newFake()
	a, errA := login(f, "nobody", "pw")
	b, errB := login(f, "alice", "bad")
	if errA != nil || errB != nil || a.Reason != domain.ReasonInvalidCredentials || b != a {
		t.Fatalf("%+v %+v %v %v", a, b, errA, errB)
	}
}
