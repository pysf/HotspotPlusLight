package plan

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"hotspotpluslight/internal/clock"
	"hotspotpluslight/internal/domain"
)

type fakeRepo struct {
	plans   map[int64]domain.Plan
	members map[int64]int
	batches map[int64]int
	def     int64
	next    int64
}

func newFake() *fakeRepo {
	return &fakeRepo{plans: map[int64]domain.Plan{}, members: map[int64]int{}, batches: map[int64]int{}}
}

func (f *fakeRepo) ListPlans(context.Context) ([]domain.Plan, error) {
	var out []domain.Plan
	for _, p := range f.plans {
		out = append(out, p)
	}
	return out, nil
}
func (f *fakeRepo) GetPlan(_ context.Context, id int64) (domain.Plan, error) {
	p, ok := f.plans[id]
	if !ok {
		return p, domain.ErrNotFound
	}
	return p, nil
}
func (f *fakeRepo) taken(name string, except int64) bool {
	for id, p := range f.plans {
		if id != except && strings.EqualFold(p.Name, name) {
			return true
		}
	}
	return false
}
func (f *fakeRepo) CreatePlan(_ context.Context, p domain.Plan, _ time.Time) (int64, error) {
	if f.taken(p.Name, 0) {
		return 0, domain.ErrNameTaken
	}
	f.next++
	p.ID = f.next
	f.plans[p.ID] = p
	return p.ID, nil
}
func (f *fakeRepo) UpdatePlan(_ context.Context, p domain.Plan, _ time.Time) error {
	if _, ok := f.plans[p.ID]; !ok {
		return domain.ErrNotFound
	}
	if f.taken(p.Name, p.ID) {
		return domain.ErrNameTaken
	}
	f.plans[p.ID] = p
	return nil
}
func (f *fakeRepo) DeletePlan(_ context.Context, id int64) error {
	if n := f.members[id]; n > 0 { // like the store: the foreign key refuses
		return &domain.InUseError{Members: n}
	}
	delete(f.plans, id)
	return nil
}
func (f *fakeRepo) CountMembersOnPlan(_ context.Context, id int64) (int, error) {
	return f.members[id], nil
}
func (f *fakeRepo) PlanUsage(context.Context) (map[int64]domain.PlanUsage, error) {
	out := map[int64]domain.PlanUsage{}
	for id, n := range f.members {
		out[id] = domain.PlanUsage{Members: n, Batches: f.batches[id]}
	}
	for id, n := range f.batches {
		out[id] = domain.PlanUsage{Members: f.members[id], Batches: n}
	}
	return out, nil
}
func (f *fakeRepo) IsDefaultPlan(_ context.Context, id int64) (bool, error) { return f.def == id, nil }

func svc() (*Service, *fakeRepo) {
	r := newFake()
	return New(r, clock.NewFake(time.Unix(0, 0))), r
}

func form(name string) domain.PlanForm { f := domain.NewPlanForm(); f.Name = name; return f }

func TestCreateValidatesAndDetectsDuplicates(t *testing.T) {
	s, _ := svc()
	ctx := context.Background()
	if _, errs, err := s.Create(ctx, form("")); err != nil || errs["name"] == "" {
		t.Fatalf("empty name: %v %v", errs, err)
	}
	id, errs, err := s.Create(ctx, form("Gold"))
	if err != nil || len(errs) != 0 || id == 0 {
		t.Fatalf("%v %v %v", id, errs, err)
	}
	if _, errs, _ := s.Create(ctx, form("gold")); errs["name"] == "" {
		t.Fatal("duplicate accepted")
	}
}

func TestUpdateAndNotFound(t *testing.T) {
	s, _ := svc()
	ctx := context.Background()
	id, _, _ := s.Create(ctx, form("Gold"))
	if errs, err := s.Update(ctx, id, form("GOLD")); err != nil || len(errs) != 0 {
		t.Fatalf("own name: %v %v", errs, err)
	}
	if _, err := s.Update(ctx, 99, form("X")); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown: %v", err)
	}
	bad := form("X")
	bad.DurationDays = "0"
	if _, err := s.Update(ctx, 99, bad); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown with invalid form: %v", err)
	}
}

func TestDeleteRules(t *testing.T) {
	s, r := svc()
	ctx := context.Background()
	id, _, _ := s.Create(ctx, form("Gold"))
	r.members[id] = 2
	var inUse *domain.InUseError
	if err := s.Delete(ctx, id); !errors.As(err, &inUse) || inUse.Members != 2 {
		t.Fatalf("in use: %v", err)
	}
	r.members[id] = 0
	r.def = id
	if err := s.Delete(ctx, id); !errors.Is(err, domain.ErrDefault) {
		t.Fatalf("default: %v", err)
	}
	r.def = 0
	if err := s.Delete(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, id); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("again: %v", err)
	}
}

func TestGetCountsMembers(t *testing.T) {
	s, r := svc()
	id, _, _ := s.Create(context.Background(), form("Gold"))
	r.members[id] = 3
	d, err := s.Get(context.Background(), id)
	if err != nil || d.Members != 3 || d.Plan.Name != "Gold" {
		t.Fatalf("%+v %v", d, err)
	}
}

func TestDetailsCountsUsageAndBlocks(t *testing.T) {
	s, r := svc()
	ctx := context.Background()
	free, _, _ := s.Create(ctx, form("Free"))
	used, _, _ := s.Create(ctx, form("Used"))
	batch, _, _ := s.Create(ctx, form("Batch"))
	def, _, _ := s.Create(ctx, form("Def"))
	r.members[used] = 3
	r.batches[batch] = 2
	r.def = def
	list, err := s.Details(ctx)
	if err != nil || len(list) != 4 {
		t.Fatalf("%v %v", list, err)
	}
	got := map[int64]Detail{}
	for _, d := range list {
		got[d.Plan.ID] = d
	}
	if got[free].Blocked() || got[free].Members != 0 {
		t.Errorf("free plan %+v", got[free])
	}
	if !got[used].Blocked() || got[used].Members != 3 {
		t.Errorf("used plan %+v", got[used])
	}
	if !got[batch].Blocked() || got[batch].Batches != 2 {
		t.Errorf("batch plan %+v", got[batch])
	}
	if !got[def].Blocked() || !got[def].Default {
		t.Errorf("default plan %+v", got[def])
	}
}
