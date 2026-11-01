// Package plan is the plan service: CRUD with validation and in-use checks.
package plan

import (
	"context"
	"errors"
	"time"

	"hotspotpluslight/internal/clock"
	"hotspotpluslight/internal/domain"
)

// Repo is what the service needs from storage (implemented by store).
// Create/Update return domain.ErrNameTaken on a case-insensitive duplicate and
// Get/Update/Delete return domain.ErrNotFound for an unknown id.
type Repo interface {
	ListPlans(ctx context.Context) ([]domain.Plan, error)
	GetPlan(ctx context.Context, id int64) (domain.Plan, error)
	CreatePlan(ctx context.Context, p domain.Plan, now time.Time) (int64, error)
	UpdatePlan(ctx context.Context, p domain.Plan, now time.Time) error
	DeletePlan(ctx context.Context, id int64) error
	CountMembersOnPlan(ctx context.Context, id int64) (int, error)
	IsDefaultPlan(ctx context.Context, id int64) (bool, error)
}

type Service struct {
	repo  Repo
	clock clock.Clock
}

func New(repo Repo, c clock.Clock) *Service { return &Service{repo: repo, clock: c} }

// Detail is a plan with the number of members using it.
type Detail struct {
	Plan    domain.Plan
	Members int
}

func (s *Service) List(ctx context.Context) ([]domain.Plan, error) { return s.repo.ListPlans(ctx) }

// Get returns the plan and its member count (domain.ErrNotFound if unknown).
func (s *Service) Get(ctx context.Context, id int64) (Detail, error) {
	p, err := s.repo.GetPlan(ctx, id)
	if err != nil {
		return Detail{}, err
	}
	n, err := s.repo.CountMembersOnPlan(ctx, id)
	return Detail{Plan: p, Members: n}, err
}

// Create validates and stores a plan. A non-empty FieldErrors means nothing was stored.
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

// Update validates and stores changes to plan id (BR-5b: applies to all its members live).
func (s *Service) Update(ctx context.Context, id int64, f domain.PlanForm) (domain.FieldErrors, error) {
	p, errs := domain.ParsePlan(f)
	if len(errs) > 0 {
		// Unknown ids are 404 regardless of what was typed.
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

// Delete removes a plan that no member uses and that is not the default (O12).
// Errors: domain.ErrNotFound, *domain.InUseError, domain.ErrDefault.
func (s *Service) Delete(ctx context.Context, id int64) error {
	if _, err := s.repo.GetPlan(ctx, id); err != nil {
		return err
	}
	if def, err := s.repo.IsDefaultPlan(ctx, id); err != nil {
		return err
	} else if def {
		return domain.ErrDefault
	}
	if n, err := s.repo.CountMembersOnPlan(ctx, id); err != nil {
		return err
	} else if n > 0 {
		return &domain.InUseError{Members: n}
	}
	return s.repo.DeletePlan(ctx, id) // the FK (RESTRICT) still guards a concurrent member insert
}
