// Package session is the accounting / online list / kick service. Phase 0 skeleton.
package session

import (
	"context"

	"hotspotpluslight/internal/clock"
	"hotspotpluslight/internal/domain"
)

type Repo interface {
	CountOpenSessions(ctx context.Context) (int, error)
}

// Target identifies a session to disconnect (BR-18). The type lives in domain
// so that the member service can use it without importing this package.
type Target = domain.SessionTarget

// Disconnector sends a RADIUS Disconnect-Request (implemented in Phase 5 by internal/radius).
type Disconnector interface {
	Disconnect(ctx context.Context, t Target) error
}

type Service struct {
	repo  Repo
	disc  Disconnector
	clock clock.Clock
}

func New(repo Repo, disc Disconnector, clk clock.Clock) *Service {
	return &Service{repo: repo, disc: disc, clock: clk}
}

func (s *Service) OpenCount(ctx context.Context) (int, error) { return s.repo.CountOpenSessions(ctx) }
