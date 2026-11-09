package voucher

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"hotspotpluslight/internal/clock"
	"hotspotpluslight/internal/domain"
	"hotspotpluslight/internal/member"
)

// fakeRepo is a hand-written in-memory Repo.
type fakeRepo struct {
	settings domain.Settings
	plans    map[int64]domain.Plan
	batches  map[int64]domain.Batch
	creds    map[int64][]domain.BatchCredential
	members  map[int64]domain.Member
	pw       map[int64][]byte
	created  []domain.NewBatch
	conflict string
	planGone bool
}

func (f *fakeRepo) GetSettings(context.Context) (domain.Settings, error) { return f.settings, nil }
func (f *fakeRepo) GetPlan(_ context.Context, id int64) (domain.Plan, error) {
	if p, ok := f.plans[id]; ok {
		return p, nil
	}
	return domain.Plan{}, domain.ErrNotFound
}
func (f *fakeRepo) CreateBatch(_ context.Context, nb domain.NewBatch, now time.Time) (domain.Batch, []string, error) {
	if f.planGone {
		return domain.Batch{}, nil, domain.ErrPlanGone
	}
	if f.conflict != "" {
		return domain.Batch{}, nil, &domain.UsernameConflictError{Username: f.conflict}
	}
	f.created = append(f.created, nb)
	return domain.Batch{ID: 1, Count: nb.Input.Count, ExpiresAt: nb.ExpiresAt}, nil, nil
}
func (f *fakeRepo) GetBatch(_ context.Context, id int64) (domain.Batch, error) {
	if b, ok := f.batches[id]; ok {
		return b, nil
	}
	return domain.Batch{}, domain.ErrNotFound
}
func (f *fakeRepo) CountBatches(context.Context) (int, error) { return len(f.batches), nil }
func (f *fakeRepo) ListBatches(context.Context, int, int, time.Time) ([]domain.BatchRow, error) {
	return nil, nil
}
func (f *fakeRepo) RenameBatch(_ context.Context, id int64, label string) error {
	b, ok := f.batches[id]
	if !ok {
		return domain.ErrNotFound
	}
	b.Label = label
	f.batches[id] = b
	return nil
}
func (f *fakeRepo) ListBatchCredentials(_ context.Context, id int64) ([]domain.BatchCredential, error) {
	return f.creds[id], nil
}
func (f *fakeRepo) GetMember(_ context.Context, id int64) (domain.Member, error) {
	if m, ok := f.members[id]; ok {
		return m, nil
	}
	return domain.Member{}, domain.ErrNotFound
}
func (f *fakeRepo) GetMemberPassword(_ context.Context, id int64) ([]byte, error) {
	if p, ok := f.pw[id]; ok {
		return p, nil
	}
	return nil, domain.ErrNotFound
}

// rot is a reversible fake cipher.
type rot struct{}

func (rot) Encrypt(p []byte) ([]byte, error) { return append([]byte("enc:"), p...), nil }
func (rot) Decrypt(d []byte) ([]byte, error) {
	return []byte(strings.TrimPrefix(string(d), "enc:")), nil
}

type fakeMembers struct{ sels []domain.MemberSelection }

func (f *fakeMembers) Delete(_ context.Context, sel domain.MemberSelection) (member.DeleteResult, error) {
	f.sels = append(f.sels, sel)
	return member.DeleteResult{Deleted: 3}, nil
}

func newSvc() (*Service, *fakeRepo, *fakeMembers) {
	repo := &fakeRepo{
		settings: domain.Settings{Timezone: "Asia/Tehran", BatchMaxCount: 30, VoucherPasswordLength: 6, AccountingIntervalSeconds: 60},
		plans:    map[int64]domain.Plan{1: {ID: 1, Name: "Basic", DurationDays: 30}},
		batches:  map[int64]domain.Batch{7: {ID: 7, Label: "x"}},
		members:  map[int64]domain.Member{4: {ID: 4, Username: "carol"}},
		pw:       map[int64][]byte{4: []byte("enc:Secret42")},
	}
	fm := &fakeMembers{}
	return New(repo, rot{}, fm, clock.NewFake(time.Date(2026, 11, 1, 21, 0, 0, 0, time.UTC))), repo, fm
}

func TestCreateResolvesDurationExpiryAndPasswords(t *testing.T) {
	s, repo, _ := newSvc()
	if _, errs, err := s.Create(context.Background(), domain.BatchForm{PlanID: "1", Count: "4"}); err != nil || len(errs) > 0 {
		t.Fatal(errs, err)
	}
	nb := repo.created[0]
	if nb.DurationDays != 30 || nb.Input.Count != 4 || len(nb.PasswordsEnc) != 4 {
		t.Fatalf("%+v", nb)
	}
	// 21:00 UTC is already 2026-11-02 in Tehran: 2 Nov + 30 + 1 days = 3 Dec 00:00 +0330.
	if want := time.Date(2026, 12, 3, 0, 0, 0, 0, time.FixedZone("", 3*3600+1800)); !nb.ExpiresAt.Equal(want) {
		t.Fatalf("expires %s, want %s", nb.ExpiresAt, want)
	}
	for _, e := range nb.PasswordsEnc {
		pw := strings.TrimPrefix(string(e), "enc:")
		if len(pw) != 6 || strings.Trim(pw, "0123456789") != "" {
			t.Fatalf("password %q", pw)
		}
	}
	if _, _, err := s.Create(context.Background(), domain.BatchForm{PlanID: "1", Count: "1", DurationDays: "5"}); err != nil || repo.created[1].DurationDays != 5 {
		t.Fatalf("explicit duration: %v %+v", err, repo.created[1])
	}
}

func TestCreateValidation(t *testing.T) {
	s, repo, _ := newSvc()
	for _, f := range []domain.BatchForm{{PlanID: "1", Count: "31"}, {PlanID: "9", Count: "1"}, {PlanID: "", Count: "1"}, {PlanID: "1", Count: "0"}} {
		if _, errs, err := s.Create(context.Background(), f); err != nil || len(errs) == 0 {
			t.Errorf("%+v accepted (%v, %v)", f, errs, err)
		}
	}
	if len(repo.created) != 0 {
		t.Fatal("a rejected form stored a batch")
	}
	repo.conflict = "wifi1"
	var c *domain.UsernameConflictError
	if _, _, err := s.Create(context.Background(), domain.BatchForm{PlanID: "1", Count: "1"}); !errors.As(err, &c) || !errors.Is(err, domain.ErrUsernameTaken) {
		t.Fatalf("conflict error %v", err)
	}
}

func TestDeleteUsesMemberDeletePath(t *testing.T) {
	s, _, fm := newSvc()
	res, err := s.Delete(context.Background(), 7)
	if err != nil || res.Deleted != 3 || len(fm.sels) != 1 || fm.sels[0].BatchID == nil || *fm.sels[0].BatchID != 7 {
		t.Fatalf("%+v %v %+v", res, err, fm.sels)
	}
	if _, err := s.Delete(context.Background(), 99); !errors.Is(err, domain.ErrNotFound) || len(fm.sels) != 1 {
		t.Fatalf("unknown batch: %v", err)
	}
}

func TestRename(t *testing.T) {
	s, repo, _ := newSvc()
	if errs, err := s.Rename(context.Background(), 7, "  New "); err != nil || len(errs) > 0 || repo.batches[7].Label != "New" {
		t.Fatalf("%v %v %q", errs, err, repo.batches[7].Label)
	}
	if errs, _ := s.Rename(context.Background(), 7, strings.Repeat("x", 101)); len(errs) == 0 {
		t.Fatal("long label accepted")
	}
	if _, err := s.Rename(context.Background(), 99, strings.Repeat("x", 101)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown batch with a bad label: %v", err)
	}
}

func TestPrintBatchAndMember(t *testing.T) {
	s, repo, _ := newSvc()
	for i := range 19 {
		repo.creds = map[int64][]domain.BatchCredential{7: append(repo.creds[7], domain.BatchCredential{Username: "u" + string(rune('a'+i)), PasswordEnc: []byte("enc:p")})}
	}
	p, err := s.PrintBatch(context.Background(), 7)
	if err != nil || p.Total != 19 || len(p.Pages) != 2 || len(p.Pages[0]) != 18 || p.Pages[1][0].Password != "p" || p.Help != domain.DefaultVoucherHelp {
		t.Fatalf("%+v %v", p, err)
	}
	p, err = s.PrintMember(context.Background(), 4)
	if err != nil || p.Total != 1 || p.Pages[0][0] != (domain.Voucher{Username: "carol", Password: "Secret42"}) {
		t.Fatalf("%+v %v", p, err)
	}
	if _, err := s.PrintMember(context.Background(), 5); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
	repo.settings.VoucherHelp = " Hi "
	if p, _ := s.PrintMember(context.Background(), 4); p.Help != "Hi" {
		t.Fatalf("help %q", p.Help)
	}
}

// The plan deleted between the check and the insert is a field error, not a 500.
func TestCreatePlanDeletedMeanwhile(t *testing.T) {
	s, repo, _ := newSvc()
	repo.planGone = true
	_, errs, err := s.Create(context.Background(), domain.BatchForm{PlanID: "1", Count: "1"})
	if err != nil || errs["plan_id"] == "" {
		t.Fatalf("%v %v", errs, err)
	}
}
