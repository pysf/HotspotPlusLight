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
	// macMembers are further members (newer than member) for the MAC lookup.
	macMembers []domain.Member
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
func (f *fakeLogin) ListActiveMembersByMAC(_ context.Context, mac string) ([]domain.Member, [][]byte, error) {
	var ms []domain.Member
	var encs [][]byte
	for _, m := range append(append([]domain.Member{}, f.macMembers...), f.member) { // newest first
		if m.Active && m.MAC != "" && m.MAC == mac {
			ms, encs = append(ms, m), append(encs, []byte(m.Username+"-pw"))
			if m.Username == f.member.Username {
				encs[len(encs)-1] = []byte(f.pw)
			}
		}
	}
	return ms, encs, nil
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
	if v, _ := login(f, "alice", "pw"); !v.Accepted() || v.Grant.DataLow != 1<<30 {
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
	if err != nil || !v.Accepted() || f.renewals != 1 || v.Grant.DataLow != 1<<30 {
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

func account(f *fakeLogin, user, pw string) (Account, error) {
	return NewEvaluator(f, plainCipher{}, clock.NewFake(now0), slog.New(slog.NewTextHandler(io.Discard, nil))).Account(context.Background(), user, pw)
}

// Account has no side effects and checks the password (legacy bug 9 fixed).
func TestAccount(t *testing.T) {
	f := newFake()
	if _, err := account(f, "alice", "wrong"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong password: %v", err)
	}
	if _, err := account(f, "nobody", "pw"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("unknown user: %v", err)
	}
	a, err := account(f, "alice", "pw")
	if err != nil || a.Reason != "" || a.Plan == nil || a.Quota.RemainingBytes != 1<<30 {
		t.Fatalf("%+v %v", a, err)
	}
	f.active = 5 // the session check is not part of the page verdict
	if a, _ := account(f, "alice", "pw"); a.Reason != "" {
		t.Fatalf("sessions must not count: %+v", a)
	}
	f.member.UsedDownload = 1 << 30
	if a, _ := account(f, "alice", "pw"); a.Reason != domain.ReasonOutOfBulk || a.Quota.RemainingBytes != 0 {
		t.Fatalf("%+v", a)
	}
	// Lapsed window + auto-resubscribe: reported as the renewed state, nothing renewed.
	f.member.SubscriptionDate = now0.AddDate(0, 0, -40)
	f.plan.AutoResubscribe = true
	a, _ = account(f, "alice", "pw")
	if a.Reason != "" || a.State.UsedDownload != 0 || !a.State.SubscriptionDate.Equal(now0) || f.renewals != 0 {
		t.Fatalf("%+v renewals=%d", a, f.renewals)
	}
	f.member.PlanID = 0
	if a, _ := account(f, "alice", "pw"); a.Reason != domain.ReasonNoPlan || a.Plan != nil {
		t.Fatalf("%+v", a)
	}
}

func TestCredentialsByMAC(t *testing.T) {
	f := newFake()
	f.member.MAC = "AA:BB:CC:DD:EE:FF"
	ev := NewEvaluator(f, plainCipher{}, clock.NewFake(now0), slog.New(slog.NewTextHandler(io.Discard, nil)))
	u, pw, ok, err := ev.CredentialsByMAC(context.Background(), "AA:BB:CC:DD:EE:FF")
	if err != nil || !ok || u != "alice" || pw != "pw" {
		t.Fatalf("%q %q %v %v", u, pw, ok, err)
	}
	for _, mac := range []string{"", "zz", "112233445566"} {
		if _, _, ok, err := ev.CredentialsByMAC(context.Background(), mac); ok || err != nil {
			t.Errorf("%q: %v %v", mac, ok, err)
		}
	}
	f.member.Active = false
	if _, _, ok, _ := ev.CredentialsByMAC(context.Background(), "aabbccddeeff"); ok {
		t.Error("inactive member must not be returned")
	}
}

// Several members may share a MAC: the newest one that could log in wins; if none can, the newest.
func TestCredentialsByMACSeveralMembers(t *testing.T) {
	f := newFake() // alice, id 1: expired
	f.member.MAC = "AA:BB:CC:DD:EE:FF"
	exp := now0.Add(-time.Hour)
	f.member.ExpiresAt = &exp
	f.pw = "old"
	ev := NewEvaluator(f, plainCipher{}, clock.NewFake(now0), slog.New(slog.NewTextHandler(io.Discard, nil)))
	get := func() (string, string) {
		u, pw, ok, err := ev.CredentialsByMAC(context.Background(), "aabbccddeeff")
		if err != nil || !ok {
			t.Fatalf("%v %v", ok, err)
		}
		return u, pw
	}
	// Only the expired one: it is returned so the page can show EXPIRED.
	if u, _ := get(); u != "alice" {
		t.Fatalf("only member: %q", u)
	}
	// A valid newer member wins over the expired old one.
	f.macMembers = []domain.Member{{ID: 2, Username: "bob", Active: true, PlanID: 1, SubscriptionDate: now0, MAC: "AA:BB:CC:DD:EE:FF"}}
	if u, pw := get(); u != "bob" || pw != "bob-pw" {
		t.Fatalf("valid newer: %q %q", u, pw)
	}
	// Newest is expired, an older one is valid: the valid one wins.
	f.member.ExpiresAt = nil
	f.macMembers[0].ExpiresAt = &exp
	if u, _ := get(); u != "alice" {
		t.Fatalf("valid older: %q", u)
	}
	// None allowed: the newest.
	f.member.ExpiresAt = &exp
	if u, _ := get(); u != "bob" {
		t.Fatalf("none allowed: %q", u)
	}
}
