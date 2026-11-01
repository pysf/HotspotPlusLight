package plan

import (
	"context"
	"testing"

	"hotspotpluslight/internal/domain"
)

type fakeRepo struct{ plans []domain.Plan }

func (f fakeRepo) ListPlans(context.Context) ([]domain.Plan, error) { return f.plans, nil }

func TestList(t *testing.T) {
	s := New(fakeRepo{[]domain.Plan{{Name: "a"}}})
	got, err := s.List(context.Background())
	if err != nil || len(got) != 1 || got[0].Name != "a" {
		t.Fatalf("%v %v", got, err)
	}
}
