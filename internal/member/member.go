// Package member is the member service (create, edit, renew, delete...). Phase 0 skeleton.
package member

import "context"

type Repo interface {
	CountMembers(ctx context.Context) (int, error)
}

type Service struct{ repo Repo }

func New(repo Repo) *Service { return &Service{repo: repo} }

func (s *Service) Count(ctx context.Context) (int, error) { return s.repo.CountMembers(ctx) }
