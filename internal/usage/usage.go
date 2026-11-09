// Package usage is the read side of the daily usage history (PLAN D12, Phase 9):
// the daily chart, its summary and the top-N members list. Writing happens in internal/session.
package usage

import (
	"context"

	"hotspotpluslight/internal/clock"
	"hotspotpluslight/internal/domain"
)

// TopN is the length of the top members list.
const TopN = 10

// Repo is what the report needs from the database. Days are domain.DayFormat keys, inclusive.
type Repo interface {
	GetSettings(ctx context.Context) (domain.Settings, error)
	DailyTotals(ctx context.Context, fromDay, toDay string) ([]domain.UsageDay, error)
	TopMembers(ctx context.Context, fromDay, toDay string, limit int) ([]domain.MemberUsage, error)
}

// Service builds the usage report.
type Service struct {
	repo  Repo
	clock clock.Clock
}

// New returns a Service.
func New(repo Repo, clk clock.Clock) *Service { return &Service{repo: repo, clock: clk} }

// View is the data of the usage page.
type View struct {
	Days     []domain.UsageDay // one entry per day of the range, oldest first, empty days as zeros
	MaxTotal int64             // largest daily total, for scaling the bars
	Summary  domain.UsageSummary
	Axis     domain.ChartAxis
	Top      []domain.MemberUsage
	Range    int // days covered by the chart, the summary and Top
}

// Report returns the usage page data for the last rangeDays days; an unknown rangeDays
// falls back to the default. Days are calendar days in settings.timezone, today included.
func (s *Service) Report(ctx context.Context, rangeDays int) (View, error) {
	st, err := s.repo.GetSettings(ctx)
	if err != nil {
		return View{}, err
	}
	loc, err := st.Location()
	if err != nil {
		return View{}, err
	}
	now := s.clock.Now()
	rangeDays = domain.NormalizeUsageDays(rangeDays)
	window := domain.DayWindow(now, loc, rangeDays)
	rows, err := s.repo.DailyTotals(ctx, window[0], window[rangeDays-1])
	if err != nil {
		return View{}, err
	}
	v := View{Days: domain.FillDays(window, rows), Range: rangeDays}
	for _, d := range v.Days {
		v.MaxTotal = max(v.MaxTotal, d.Total())
	}
	v.Summary = domain.SummarizeUsage(v.Days)
	v.Axis = domain.NewChartAxis(v.MaxTotal)
	v.Top, err = s.repo.TopMembers(ctx, window[0], window[rangeDays-1], TopN)
	return v, err
}
