// Package plan is the plan service: create, edit, list and delete plans, with
// validation and the in-use checks.
package plan

import (
	"context"
	"errors"
	"time"

	"hotspotpluslight/internal/clock"
	"hotspotpluslight/internal/domain"
)

// Repo is what the service needs from storage (store.Store). Create and Update return
// domain.ErrNameTaken on a case-insensitive duplicate. Get, Update and Delete return
// domain.ErrNotFound for an unknown id.
type Repo interface {
	ListPlans(ctx context.Context) ([]domain.Plan, error)
	GetPlan(ctx context.Context, id int64) (domain.Plan, error)
	CreatePlan(ctx context.Context, p domain.Plan, now time.Time) (int64, error)
	UpdatePlan(ctx context.Context, p domain.Plan, now time.Time) error
	DeletePlan(ctx context.Context, id int64) error
	CountMembersOnPlan(ctx context.Context, id int64) (int, error)
	PlanUsage(ctx context.Context) (map[int64]domain.PlanUsage, error)
	IsDefaultPlan(ctx context.Context, id int64) (bool, error)
}

// Service implements the plan use cases.
type Service struct {
	repo  Repo
	clock clock.Clock
}

// New returns a Service.
func New(repo Repo, c clock.Clock) *Service { return &Service{repo: repo, clock: c} }

// Detail is a plan with the number of members using it. Details also fills Batches and Default.
type Detail struct {
	Plan    domain.Plan
	Members int
	Batches int  // batches that hold the plan
	Default bool // the plan is the default plan
}

// Blocked reports whether the plan cannot be deleted.
func (d Detail) Blocked() bool { return d.Members > 0 || d.Batches > 0 || d.Default }

// List returns all plans.
func (s *Service) List(ctx context.Context) ([]domain.Plan, error) { return s.repo.ListPlans(ctx) }

// Details returns all plans with their usage, for the plan cards.
func (s *Service) Details(ctx context.Context) ([]Detail, error) {
	plans, err := s.repo.ListPlans(ctx)
	if err != nil {
		return nil, err
	}
	use, err := s.repo.PlanUsage(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Detail, len(plans))
	for i, p := range plans {
		def, err := s.repo.IsDefaultPlan(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		out[i] = Detail{Plan: p, Members: use[p.ID].Members, Batches: use[p.ID].Batches, Default: def}
	}
	return out, nil
}

// Get returns the plan with its usage, or domain.ErrNotFound.
func (s *Service) Get(ctx context.Context, id int64) (Detail, error) {
	p, err := s.repo.GetPlan(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	n, err := s.repo.CountMembersOnPlan(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	use, err := s.repo.PlanUsage(ctx)
	if err != nil {
		return Detail{}, err
	}
	def, err := s.repo.IsDefaultPlan(ctx, id)
	return Detail{Plan: p, Members: n, Batches: use[id].Batches, Default: def}, err
}

// Create validates and stores a plan and returns its id. A non-empty FieldErrors means nothing was stored.
func (s *Service) Create(ctx context.Context, f domain.PlanForm) (int64, domain.FieldErrors, error) {
	p, errs := domain.ParsePlan(f)
	if len(errs) > 0 {
		return 0, errs, nil
	}
	id, err := s.repo.CreatePlan(ctx, p, s.clock.Now())
	if errors.Is(err, domain.ErrNameTaken) {
		return 0, domain.FieldErrors{"name": "A plan with this name already exists."}, nil
	}
	return id, nil, err
}

// Update validates and stores changes to plan id. They apply to all its members at once (BR-5b).
// A non-empty FieldErrors means nothing was stored.
func (s *Service) Update(ctx context.Context, id int64, f domain.PlanForm) (domain.FieldErrors, error) {
	p, errs := domain.ParsePlan(f)
	if len(errs) > 0 {
		// An unknown id is not found, whatever was typed.
		if _, err := s.repo.GetPlan(ctx, id); err != nil {
			return nil, err
		}
		return errs, nil
	}
	p.ID = id
	err := s.repo.UpdatePlan(ctx, p, s.clock.Now())
	if errors.Is(err, domain.ErrNameTaken) {
		return domain.FieldErrors{"name": "A plan with this name already exists."}, nil
	}
	return nil, err
}

// Delete removes a plan that no member or batch uses and that is not the default (O12).
// It returns domain.ErrNotFound, *domain.InUseError or domain.ErrDefault.
func (s *Service) Delete(ctx context.Context, id int64) error {
	if _, err := s.repo.GetPlan(ctx, id); err != nil {
		return err
	}
	if def, err := s.repo.IsDefaultPlan(ctx, id); err != nil {
		return err
	} else if def {
		return domain.ErrDefault
	}
	return s.repo.DeletePlan(ctx, id) // the store counts the members and batches that block it
}
