// Package settings is the service behind the single-row settings table.
package settings

import (
	"context"
	"errors"
	"time"

	_ "time/tzdata" // IANA names must validate on machines without a zoneinfo database

	"hotspotpluslight/internal/clock"
	"hotspotpluslight/internal/domain"
)

// Repo is what the service needs from storage (store.Store).
type Repo interface {
	GetSettings(ctx context.Context) (domain.Settings, error)
	UpdateSettings(ctx context.Context, s domain.Settings, now time.Time) error
	PlanExists(ctx context.Context, id int64) (bool, error)
}

// Service implements the settings use cases.
type Service struct {
	repo  Repo
	clock clock.Clock
}

// New returns a Service.
func New(repo Repo, c clock.Clock) *Service { return &Service{repo: repo, clock: c} }

// Get returns the settings row.
func (s *Service) Get(ctx context.Context) (domain.Settings, error) { return s.repo.GetSettings(ctx) }

// Update validates and stores the form. A non-empty FieldErrors means nothing was stored.
// The RADIUS shared secret is read per packet, so it applies without a restart (PLAN 7.1).
func (s *Service) Update(ctx context.Context, f domain.SettingsForm) (domain.FieldErrors, error) {
	cur, err := s.repo.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	st, errs := domain.ParseSettingsKeeping(f, cur.RadiusSharedSecret)
	if _, bad := errs["timezone"]; !bad {
		// LoadLocation accepts "" and "Local" (the host zone), which are not IANA names.
		if _, err := time.LoadLocation(st.Timezone); err != nil || st.Timezone == "Local" {
			errs["timezone"] = "Unknown time zone (use an IANA name such as Europe/London or UTC)."
		}
	}
	if st.DefaultPlanID != nil && errs["default_plan_id"] == "" {
		ok, err := s.repo.PlanExists(ctx, *st.DefaultPlanID)
		if err != nil {
			return nil, err
		}
		if !ok {
			errs["default_plan_id"] = "Choose a plan from the list."
		}
	}
	if len(errs) > 0 {
		return errs, nil
	}
	err = s.repo.UpdateSettings(ctx, st, s.clock.Now())
	if errors.Is(err, domain.ErrPlanGone) { // deleted between the check and the write
		return domain.FieldErrors{"default_plan_id": "Choose a plan from the list."}, nil
	}
	return nil, err
}
