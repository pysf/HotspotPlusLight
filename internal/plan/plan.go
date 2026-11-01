// Package plan is the plan service (CRUD, in-use checks). Phase 0 skeleton.
package plan

import (
	"context"

	"hotspotpluslight/internal/domain"
)

type Repo interface {
	ListPlans(ctx context.Context) ([]domain.Plan, error)
}

type Service struct{ repo Repo }

func New(repo Repo) *Service { return &Service{repo: repo} }

func (s *Service) List(ctx context.Context) ([]domain.Plan, error) { return s.repo.ListPlans(ctx) }
