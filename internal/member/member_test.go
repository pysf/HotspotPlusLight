package member

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"hotspotpluslight/internal/clock"
	"hotspotpluslight/internal/domain"
)

var t0 = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

// fakeRepo is a hand-written in-memory Repo; it records what the service asks for.
type fakeRepo struct {
	members  map[int64]domain.Member
	enc      map[int64][]byte
	plans    map[int64]domain.Plan
	settings domain.Settings
	sessions []domain.SessionTarget
	next     int64
	planGone bool // the write fails as if the plan was deleted after the check
	deleted  []domain.MemberSelection
	late     []domain.SessionTarget // sessions that appear after the kick step, seen inside the delete transaction
}

func newRepo() *fakeRepo {
	return &fakeRepo{members: map[int64]domain.Member{}, enc: map[int64][]byte{},
		plans:    map[int64]domain.Plan{1: {ID: 1, Name: "P", DurationDays: 30, BulkValue: 1, BulkUnit: "GB", MaxSessions: 1}, 2: {ID: 2, Name: "Free", DurationDays: 30, BulkUnit: "GB", MaxSessions: 1}},
		settings: domain.Settings{Timezone: "UTC", AccountingIntervalSeconds: 60}}
}

func (f *fakeRepo) CreateMember(_ context.Context, in domain.MemberInput, enc []byte, now time.Time) (int64, error) {
	if f.planGone {
		return 0, domain.ErrPlanGone
	}
	for _, m := range f.members {
		if equalFold(m.Username, in.Username) {
			return 0, domain.ErrUsernameTaken
		}
	}
	f.next++
	f.members[f.next] = domain.Member{ID: f.next, Username: in.Username, Active: in.Active, PlanID: in.PlanID, SubscriptionDate: now}
	f.enc[f.next] = enc
	return f.next, nil
}

func equalFold(a, b string) bool {
	return len(a) == len(b) && (a == b || lower(a) == lower(b))
}

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

func (f *fakeRepo) GetMember(_ context.Context, id int64) (domain.Member, error) {
	m, ok := f.members[id]
	if !ok {
		return m, domain.ErrNotFound
	}
	return m, nil
}

func (f *fakeRepo) GetMemberRow(ctx context.Context, id int64, _ time.Time) (domain.MemberRow, error) {
	m, err := f.GetMember(ctx, id)
	return domain.MemberRow{Member: m, Plan: f.plans[m.PlanID]}, err
}

func (f *fakeRepo) UpdateMember(_ context.Context, id int64, in domain.MemberInput, _ time.Time) error {
	if f.planGone {
		return domain.ErrPlanGone
	}
	m := f.members[id]
	m.Username, m.Active, m.PlanID = in.Username, in.Active, in.PlanID
	f.members[id] = m
	return nil
}

func (f *fakeRepo) SetMemberActive(_ context.Context, id int64, active bool, _ time.Time) error {
	m, ok := f.members[id]
	if !ok {
		return domain.ErrNotFound
	}
	m.Active = active
	f.members[id] = m
	return nil
}

func (f *fakeRepo) GetMemberPassword(_ context.Context, id int64) ([]byte, error) {
	if e, ok := f.enc[id]; ok {
		return e, nil
	}
	return nil, domain.ErrNotFound
}

func (f *fakeRepo) SetMemberPassword(_ context.Context, id int64, enc []byte, _ time.Time) error {
	f.enc[id] = enc
	return nil
}

func (f *fakeRepo) RenewMember(context.Context, int64, string, time.Time) error { return nil }

func (f *fakeRepo) SetExtraBulk(_ context.Context, id int64, b int64, _ time.Time) error {
	m := f.members[id]
	m.ExtraBulkBytes = b
	f.members[id] = m
	return nil
}

func (f *fakeRepo) CountFiltered(context.Context, domain.MemberFilter, time.Time) (int, error) {
	return len(f.members), nil
}

func (f *fakeRepo) CountStatuses(context.Context, domain.MemberFilter, time.Time) (domain.StatusCounts, error) {
	return domain.StatusCounts{All: len(f.members)}, nil
}

func (f *fakeRepo) ListMembers(context.Context, domain.MemberFilter, int, int, time.Time, time.Time) ([]domain.MemberRow, error) {
	return nil, nil
}

func (f *fakeRepo) SelectionStats(context.Context, domain.MemberSelection, time.Time, time.Time) (int, int, error) {
	return len(f.members), len(f.sessions), nil
}

func (f *fakeRepo) OpenSessions(context.Context, domain.MemberSelection, time.Time) ([]domain.SessionTarget, error) {
	return f.sessions, nil
}

func (f *fakeRepo) DeleteMembers(_ context.Context, sel domain.MemberSelection, _ time.Time) (int, []domain.SessionTarget, error) {
	f.deleted = append(f.deleted, sel)
	return len(sel.IDs), append(append([]domain.SessionTarget(nil), f.sessions...), f.late...), nil
}

func (f *fakeRepo) GetPlan(_ context.Context, id int64) (domain.Plan, error) {
	p, ok := f.plans[id]
	if !ok {
		return p, domain.ErrNotFound
	}
	return p, nil
}

func (f *fakeRepo) GetSettings(context.Context) (domain.Settings, error) { return f.settings, nil }

// xorCipher is a reversible stand-in for secret.Box.
type xorCipher struct{}

func (xorCipher) Encrypt(p []byte) ([]byte, error) { return append([]byte("enc:"), p...), nil }
func (xorCipher) Decrypt(d []byte) ([]byte, error) { return d[4:], nil }

type fakeDisc struct {
	mu    sync.Mutex
	calls []domain.SessionTarget
	fail  bool
	delay time.Duration                   // simulates a router that is slow to answer
	failF func(domain.SessionTarget) bool // fail only these targets
}

func (d *fakeDisc) Disconnect(_ context.Context, t domain.SessionTarget) error {
	time.Sleep(d.delay)
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, t)
	if d.fail || (d.failF != nil && d.failF(t)) {
		return errors.New("nak")
	}
	return nil
}

func setup() (*Service, *fakeRepo, *fakeDisc) {
	r, d := newRepo(), &fakeDisc{}
	return New(r, xorCipher{}, d, clock.NewFake(t0)), r, d
}

func TestCreateDefaultsAndValidation(t *testing.T) {
	s, r, _ := setup()
	ctx := context.Background()
	id, errs, err := s.Create(ctx, domain.MemberForm{Username: "Alice", PlanID: "1", Active: true})
	if err != nil || len(errs) != 0 {
		t.Fatalf("%v %v", errs, err)
	}
	pw, _ := s.RevealPassword(ctx, id)
	if len(pw) != 6 {
		t.Fatalf("default password %q", pw)
	}
	if !r.members[id].SubscriptionDate.Equal(t0) {
		t.Fatal("window must start now")
	}
	if _, errs, _ = s.Create(ctx, domain.MemberForm{Username: "ALICE", PlanID: "1"}); errs["username"] == "" {
		t.Fatal("duplicate username accepted")
	}
	if _, errs, _ = s.Create(ctx, domain.MemberForm{Username: "bob", PlanID: "99"}); errs["plan_id"] == "" {
		t.Fatal("unknown plan accepted")
	}
	if _, errs, _ = s.Create(ctx, domain.MemberForm{Username: "bob", PlanID: ""}); errs["plan_id"] == "" {
		t.Fatal("missing plan accepted")
	}
	if len(r.members) != 1 {
		t.Fatalf("%d members stored", len(r.members))
	}
}

func TestNewFormPreselectsDefaultPlan(t *testing.T) {
	s, r, _ := setup()
	if f, _ := s.NewForm(context.Background()); f.PlanID != "" {
		t.Fatalf("preselected %q without a default", f.PlanID)
	}
	two := int64(2)
	r.settings.DefaultPlanID = &two
	if f, _ := s.NewForm(context.Background()); f.PlanID != "2" || !f.Active {
		t.Fatalf("%+v", f)
	}
}

func TestUpdateKicksOnlyOnDeactivation(t *testing.T) {
	s, r, d := setup()
	ctx := context.Background()
	id, _, _ := s.Create(ctx, domain.MemberForm{Username: "a", PlanID: "1", Active: true})
	r.sessions = []domain.SessionTarget{{NASIP: "10.0.0.1", Username: "a", AcctSessionID: "s1"}}
	d.fail = true
	_, kick, err := s.Update(ctx, id, domain.MemberForm{Username: "a", PlanID: "2", Active: true})
	if err != nil || kick.Attempted != 0 || len(d.calls) != 0 {
		t.Fatalf("plan change kicked: %+v %v", kick, err)
	}
	errs, kick, err := s.Update(ctx, id, domain.MemberForm{Username: "a", PlanID: "2"})
	if err != nil || len(errs) != 0 || kick.Attempted != 1 || kick.Failed != 1 {
		t.Fatalf("deactivation: %+v %v %v", kick, errs, err)
	}
	if r.members[id].Active {
		t.Fatal("member must be inactive even though the kick failed")
	}
	if _, _, err := s.Update(ctx, 99, domain.MemberForm{Username: "a", PlanID: "1"}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown member: %v", err)
	}
}

func TestSetActiveKicksOnceAndIsIdempotent(t *testing.T) {
	s, r, d := setup()
	ctx := context.Background()
	id, _, _ := s.Create(ctx, domain.MemberForm{Username: "a", PlanID: "1", Active: true})
	r.sessions = []domain.SessionTarget{{NASIP: "10.0.0.1", Username: "a", AcctSessionID: "s1"}}
	d.fail = true
	kick, err := s.SetActive(ctx, id, false)
	if err != nil || kick.Attempted != 1 || kick.Failed != 1 || r.members[id].Active {
		t.Fatalf("disable: %+v %v active=%v", kick, err, r.members[id].Active)
	}
	kick, err = s.SetActive(ctx, id, false)
	if err != nil || kick.Attempted != 0 || len(d.calls) != 1 {
		t.Fatalf("second disable must not kick: %+v %v calls=%d", kick, err, len(d.calls))
	}
	if kick, err = s.SetActive(ctx, id, true); err != nil || kick.Attempted != 0 || !r.members[id].Active {
		t.Fatalf("enable: %+v %v", kick, err)
	}
	if _, err := s.SetActive(ctx, 99, true); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown member: %v", err)
	}
}

func TestExtraBulkRules(t *testing.T) {
	s, _, _ := setup()
	ctx := context.Background()
	capped, _, _ := s.Create(ctx, domain.MemberForm{Username: "a", PlanID: "1", Active: true})
	free, _, _ := s.Create(ctx, domain.MemberForm{Username: "b", PlanID: "2", Active: true})
	if errs, err := s.SetExtraBulk(ctx, capped, "1.5", "GB"); err != nil || errs != nil {
		t.Fatalf("%v %v", errs, err)
	}
	if _, err := s.SetExtraBulk(ctx, free, "1", "GB"); !errors.Is(err, domain.ErrUnlimitedData) {
		t.Fatalf("unlimited plan: %v", err)
	}
	if errs, _ := s.SetExtraBulk(ctx, capped, "-1", "GB"); len(errs) == 0 {
		t.Fatal("negative accepted")
	}
}

func TestDeleteKicksFirstAndSurvivesKickFailure(t *testing.T) {
	s, r, d := setup()
	r.sessions = []domain.SessionTarget{{Username: "a", AcctSessionID: "1"}, {Username: "a", AcctSessionID: "2"}}
	d.fail = true
	res, err := s.Delete(context.Background(), domain.MemberSelection{IDs: []int64{1, 2}})
	if err != nil || res.Deleted != 2 || res.Kick.Attempted != 2 || res.Kick.Failed != 2 || len(r.deleted) != 1 {
		t.Fatalf("%+v %v", res, err)
	}
	if _, err := s.DeleteOne(context.Background(), 77); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown member: %v", err)
	}
}

func TestRenameRefusedWhileOnline(t *testing.T) {
	s, r, _ := setup()
	ctx := context.Background()
	id, _, _ := s.Create(ctx, domain.MemberForm{Username: "alice", PlanID: "1", Active: true})
	r.sessions = []domain.SessionTarget{{Username: "alice", AcctSessionID: "1"}}
	errs, _, err := s.Update(ctx, id, domain.MemberForm{Username: "bob", PlanID: "1", Active: true})
	if err != nil || errs["username"] == "" || r.members[id].Username != "alice" {
		t.Fatalf("%v %v %+v", errs, err, r.members[id])
	}
	if errs, _, err = s.Update(ctx, id, domain.MemberForm{Username: "alice", PlanID: "1", Active: true}); err != nil || len(errs) != 0 {
		t.Fatalf("same name while online must work: %v %v", errs, err)
	}
}

func TestDeleteKicksSessionsThatAppearBeforeTheTransaction(t *testing.T) {
	s, r, d := setup()
	r.late = []domain.SessionTarget{{Username: "a", AcctSessionID: "late"}}
	res, err := s.Delete(context.Background(), domain.MemberSelection{IDs: []int64{1}})
	if err != nil || len(d.calls) != 1 || d.calls[0].AcctSessionID != "late" || res.Kick.Attempted != 1 {
		t.Fatalf("%+v %v %+v", res, err, d.calls)
	}
}

func TestDeleteBatchKicksSessionsThatAppearBeforeTheTransaction(t *testing.T) {
	s, r, d := setup()
	r.late = []domain.SessionTarget{{Username: "v1", AcctSessionID: "late"}}
	id := int64(7)
	res, err := s.Delete(context.Background(), domain.MemberSelection{BatchID: &id})
	if err != nil || len(d.calls) != 1 || d.calls[0].AcctSessionID != "late" || res.Kick.Attempted != 1 {
		t.Fatalf("%+v %v %+v", res, err, d.calls)
	}
}

func TestSelectionTooLarge(t *testing.T) {
	s, _, _ := setup()
	ids := make([]int64, MaxSelectionIDs+1)
	if _, err := s.Delete(context.Background(), domain.MemberSelection{IDs: ids}); !errors.Is(err, ErrSelectionTooLarge) {
		t.Fatalf("%v", err)
	}
	if _, err := s.Stats(context.Background(), domain.MemberSelection{IDs: ids}); !errors.Is(err, ErrSelectionTooLarge) {
		t.Fatalf("%v", err)
	}
}

// Passwords the RADIUS edge would mistake for a wrong shared secret are refused up front.
func TestMemberPasswordRejectsControlCharacters(t *testing.T) {
	s, _, _ := setup()
	ctx := context.Background()
	for _, bad := range []string{"ab\tcd", "ab\ncd", "ab\x00cd", "ab\x7fcd", "ab\xffcd"} {
		if _, errs, _ := s.Create(ctx, domain.MemberForm{Username: "carol", PlanID: "1", Password: bad}); errs["password"] == "" {
			t.Errorf("create accepted %q", bad)
		}
	}
	id, errs, err := s.Create(ctx, domain.MemberForm{Username: "carol", PlanID: "1", Password: "pässwörd 1"})
	if err != nil || len(errs) != 0 {
		t.Fatalf("%v %v", errs, err)
	}
	if msg, _ := s.SetPassword(ctx, id, "x\x1by"); msg == "" {
		t.Error("set-password accepted a control character")
	}
}

// Disconnects of one request run concurrently (bounded), so N silent sessions cost
// about one timeout window per batch of workers, not N windows; the result counts stay exact.
func TestKickSendsDisconnectsConcurrently(t *testing.T) {
	s, r, d := setup()
	id, errs, err := s.Create(context.Background(), domain.MemberForm{Username: "alice", PlanID: "1", Active: true})
	if err != nil || len(errs) > 0 {
		t.Fatal(errs, err)
	}
	for i := range 24 {
		r.sessions = append(r.sessions, domain.SessionTarget{ID: int64(i + 1), Username: "u", AcctSessionID: strconv.Itoa(i)})
	}
	d.delay = 100 * time.Millisecond
	d.failF = func(t domain.SessionTarget) bool { return t.ID%6 == 0 } // 4 failures
	start := time.Now()
	res, err := s.DisconnectMember(context.Background(), id)
	took := time.Since(start)
	if err != nil || res.Attempted != 24 || res.Failed != 4 {
		t.Fatalf("result %+v err %v", res, err)
	}
	if took > time.Second { // sequential would be 2.4 s
		t.Fatalf("took %v: Disconnects are not concurrent", took)
	}
	if len(d.calls) != 24 {
		t.Fatalf("%d calls", len(d.calls))
	}
}

// TestPlanGoneBecomesFormError covers review B4: a plan deleted after the check
// gives a plan_id form error, not an error.
func TestPlanGoneBecomesFormError(t *testing.T) {
	s, r, _ := setup()
	ctx := context.Background()
	id, _, _ := s.Create(ctx, domain.MemberForm{Username: "Alice", PlanID: "1", Active: true})
	r.planGone = true
	if _, errs, err := s.Create(ctx, domain.MemberForm{Username: "Bob", PlanID: "1"}); err != nil || errs["plan_id"] == "" {
		t.Errorf("Create: %v %v", errs, err)
	}
	if errs, _, err := s.Update(ctx, id, domain.MemberForm{Username: "Alice", PlanID: "1", Active: true}); err != nil || errs["plan_id"] == "" {
		t.Errorf("Update: %v %v", errs, err)
	}
}
