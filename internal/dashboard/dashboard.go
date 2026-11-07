// Package dashboard builds the read-only numbers of the home page (PLAN BR-25 to BR-30) and
// counts failed logins (BR-27). The pure rules are in internal/domain.
package dashboard

import (
	"context"
	"slices"
	"time"

	"hotspotpluslight/internal/clock"
	"hotspotpluslight/internal/domain"
)

const (
	// ChartDays is the length of the traffic and active-members charts, and of the busy-hours average.
	ChartDays = 14
	// ListMax is the most rows a "needs attention" list shows.
	ListMax = 5
)

// Repo is what the service needs from storage (implemented by store.Store). Days are domain.DayFormat keys, inclusive.
type Repo interface {
	GetSettings(ctx context.Context) (domain.Settings, error)
	CountOnlineSessions(ctx context.Context, since time.Time) (int, error)
	SessionSpansSince(ctx context.Context, from time.Time) ([]domain.SessionSpan, error)
	OldestSessionStart(ctx context.Context) (t time.Time, ok bool, err error)
	DailyTotals(ctx context.Context, fromDay, toDay string) ([]domain.UsageDay, error)
	DailyActive(ctx context.Context, fromDay, toDay string) ([]domain.DayCount, error)
	LoginFailures(ctx context.Context, day string) ([]domain.ReasonCount, error)
	AddLoginFailure(ctx context.Context, day string, reason domain.Reason) error
	ExpiringMembers(ctx context.Context, now time.Time, days, limit int) ([]domain.AttentionRow, int, error)
	DataLimitedMembers(ctx context.Context, now time.Time) ([]domain.MemberRow, error)
}

// Service reads the dashboard numbers.
type Service struct {
	repo  Repo
	clock clock.Clock
}

// New returns a Service.
func New(repo Repo, clk clock.Clock) *Service { return &Service{repo: repo, clock: clk} }

// OnlineNow is the first tile (BR-25).
type OnlineNow struct{ Now, PeakToday int }

// Change is a percent change against a base. Known is false when the base is 0 ("no data").
type Change struct {
	Percent int
	Known   bool
}

// Traffic is the second tile (BR-26): bytes of today so far, of yesterday and of the same weekday last week.
type Traffic struct {
	Today, Yesterday, LastWeek int64
	VsYesterday, VsLastWeek    Change
}

// FailedLogins is the third tile (BR-27).
type FailedLogins struct {
	Total   int64
	Top     domain.ReasonCount // zero when Total is 0
	Reasons []domain.ReasonCount
	HasTop  bool
}

// Attention is one "needs attention" list (BR-30): its full size and the first rows.
type Attention struct {
	Count int
	Rows  []domain.AttentionRow
}

// Overview is everything the dashboard page shows, except the router status.
type Overview struct {
	Now         time.Time
	Loc         *time.Location
	Online      OnlineNow
	Traffic     Traffic
	Failures    FailedLogins
	Expiring    Attention
	NearLimit   Attention
	TrafficDays []domain.UsageDay // ChartDays entries, oldest first, zero-filled
	BusyHours   [24]float64
	ActiveDays  []domain.DayCount // ChartDays entries, oldest first, zero-filled
}

// env is the settings and the clock for one call.
type env struct {
	st  domain.Settings
	loc *time.Location
	now time.Time
}

func (s *Service) env(ctx context.Context) (env, error) {
	st, err := s.repo.GetSettings(ctx)
	if err != nil {
		return env{}, err
	}
	loc, err := st.Location()
	if err != nil {
		return env{}, err
	}
	return env{st, loc, s.clock.Now()}, nil
}

// midnight returns the start of the local day of t.
func midnight(t time.Time, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

// RecordLoginFailure adds 1 to today's count of reason (BR-27). Today is the day in settings.timezone.
func (s *Service) RecordLoginFailure(ctx context.Context, reason domain.Reason) error {
	e, err := s.env(ctx)
	if err != nil {
		return err
	}
	return s.repo.AddLoginFailure(ctx, domain.DayKey(e.now, e.loc), reason)
}

// Online returns the online count and today's peak (BR-25). The page polls this one.
func (s *Service) Online(ctx context.Context) (OnlineNow, error) {
	e, err := s.env(ctx)
	if err != nil {
		return OnlineNow{}, err
	}
	n, err := s.repo.CountOnlineSessions(ctx, domain.ActiveSince(e.now, e.st.AccountingIntervalSeconds))
	if err != nil {
		return OnlineNow{}, err
	}
	spans, err := s.repo.SessionSpansSince(ctx, midnight(e.now, e.loc))
	if err != nil {
		return OnlineNow{}, err
	}
	return onlineOf(e, n, spans), nil
}

// onlineOf builds the first tile from the online count and the session spans that reach back to today's midnight or further.
// A session that started this very second has no length yet, so the peak never falls below the count.
func onlineOf(e env, n int, spans []domain.SessionSpan) OnlineNow {
	activeSince := domain.ActiveSince(e.now, e.st.AccountingIntervalSeconds)
	from := midnight(e.now, e.loc)
	return OnlineNow{Now: n, PeakToday: max(domain.PeakConcurrent(presences(spans, e.now, activeSince), from, e.now), n)}
}

func presences(spans []domain.SessionSpan, now, activeSince time.Time) []domain.Presence {
	out := make([]domain.Presence, len(spans))
	for i, sp := range spans {
		out[i] = domain.PresenceOf(sp, now, activeSince)
	}
	return out
}

// Overview returns all dashboard numbers.
func (s *Service) Overview(ctx context.Context) (Overview, error) {
	e, err := s.env(ctx)
	if err != nil {
		return Overview{}, err
	}
	o := Overview{Now: e.now, Loc: e.loc}
	// One read of the sessions and one "now" serve both the online tile and the busy-hours chart.
	// The busy-hours range starts earlier than today's midnight, so it covers the peak too.
	n, err := s.repo.CountOnlineSessions(ctx, domain.ActiveSince(e.now, e.st.AccountingIntervalSeconds))
	if err != nil {
		return o, err
	}
	spans, err := s.repo.SessionSpansSince(ctx, midnight(e.now.AddDate(0, 0, -(ChartDays-1)), e.loc))
	if err != nil {
		return o, err
	}
	o.Online = onlineOf(e, n, spans)
	window := domain.DayWindow(e.now, e.loc, ChartDays)
	first, last := window[0], window[ChartDays-1]
	totals, err := s.repo.DailyTotals(ctx, first, last)
	if err != nil {
		return o, err
	}
	o.TrafficDays = domain.FillDays(window, totals)
	o.Traffic = trafficOf(o.TrafficDays)
	active, err := s.repo.DailyActive(ctx, first, last)
	if err != nil {
		return o, err
	}
	o.ActiveDays = domain.FillDayCounts(window, active)
	if o.BusyHours, err = s.busyHours(ctx, e, spans); err != nil {
		return o, err
	}
	if o.Failures, err = s.failures(ctx, domain.DayKey(e.now, e.loc)); err != nil {
		return o, err
	}
	if o.Expiring.Rows, o.Expiring.Count, err = s.repo.ExpiringMembers(ctx, e.now, domain.DashboardExpiringDays, ListMax); err != nil {
		return o, err
	}
	o.NearLimit, err = s.nearLimit(ctx, e.now)
	return o, err
}

// trafficOf builds the tile from the chart days: the last entry is today, the one before is yesterday,
// and the one 7 places before today is the same weekday last week.
func trafficOf(days []domain.UsageDay) Traffic {
	n := len(days)
	t := Traffic{Today: days[n-1].Total(), Yesterday: days[n-2].Total(), LastWeek: days[n-8].Total()}
	t.VsYesterday.Percent, t.VsYesterday.Known = domain.PercentChange(t.Today, t.Yesterday)
	t.VsLastWeek.Percent, t.VsLastWeek.Known = domain.PercentChange(t.Today, t.LastWeek)
	return t
}

// busyHours is the BR-29 average over the last ChartDays days. spans must reach back to the start of that range.
func (s *Service) busyHours(ctx context.Context, e env, spans []domain.SessionSpan) ([24]float64, error) {
	oldest, ok, err := s.repo.OldestSessionStart(ctx)
	if err != nil || !ok {
		return [24]float64{}, err
	}
	return domain.HourlyAverage(presences(spans, e.now, domain.ActiveSince(e.now, e.st.AccountingIntervalSeconds)), oldest, e.now, e.loc, ChartDays), nil
}

func (s *Service) failures(ctx context.Context, day string) (FailedLogins, error) {
	counts, err := s.repo.LoginFailures(ctx, day)
	if err != nil {
		return FailedLogins{}, err
	}
	f := FailedLogins{Reasons: counts}
	for _, c := range counts {
		f.Total += c.Count
	}
	f.Top, f.HasTop = domain.TopReason(counts)
	return f, nil
}

// nearLimit applies the 80% rule (BR-30b) to members whose validity window is running, highest percent first, then by id.
func (s *Service) nearLimit(ctx context.Context, now time.Time) (Attention, error) {
	members, err := s.repo.DataLimitedMembers(ctx, now)
	if err != nil {
		return Attention{}, err
	}
	var rows []domain.AttentionRow
	for _, m := range members {
		q := domain.QuotaOf(m.Member.State(), m.Plan)
		// A lapsed or not yet started window means the used counters no longer count: the login check
		// renews or rejects the member first (BR-5, BR-6), so such a member is not "near the limit".
		if q.WindowLapsed(now) {
			continue
		}
		if domain.NearDataLimit(q) {
			rows = append(rows, domain.AttentionRow{MemberID: m.Member.ID, Username: m.Member.Username, Percent: domain.DataPercent(q), OutOfData: domain.OutOfData(q)})
		}
	}
	slices.SortStableFunc(rows, func(a, b domain.AttentionRow) int { return b.Percent - a.Percent })
	a := Attention{Count: len(rows), Rows: rows}
	if len(a.Rows) > ListMax {
		a.Rows = a.Rows[:ListMax]
	}
	return a, nil
}
