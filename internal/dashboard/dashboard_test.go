package dashboard

import (
	"context"
	"testing"
	"time"

	"hotspotpluslight/internal/clock"
	"hotspotpluslight/internal/domain"
)

var t0 = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)

// fakeRepo is a hand-written Repo.
type fakeRepo struct {
	tz        string
	online    int
	spans     []domain.SessionSpan
	totals    []domain.UsageDay
	active    []domain.DayCount
	failures  []domain.ReasonCount
	expiring  []domain.AttentionRow
	expCount  int
	limited   []domain.MemberRow
	added     map[string]int // "day/reason" -> count
	oldest    time.Time
	hasOldest bool
}

func (f *fakeRepo) GetSettings(context.Context) (domain.Settings, error) {
	tz := f.tz
	if tz == "" {
		tz = "UTC"
	}
	return domain.Settings{Timezone: tz, AccountingIntervalSeconds: 60}, nil
}
func (f *fakeRepo) CountOnlineSessions(context.Context, time.Time) (int, error) { return f.online, nil }
func (f *fakeRepo) SessionSpansSince(context.Context, time.Time) ([]domain.SessionSpan, error) {
	return f.spans, nil
}
func (f *fakeRepo) OldestSessionStart(context.Context) (time.Time, bool, error) {
	return f.oldest, f.hasOldest, nil
}
func (f *fakeRepo) DailyTotals(context.Context, string, string) ([]domain.UsageDay, error) {
	return f.totals, nil
}
func (f *fakeRepo) DailyActive(context.Context, string, string) ([]domain.DayCount, error) {
	return f.active, nil
}
func (f *fakeRepo) LoginFailures(context.Context, string) ([]domain.ReasonCount, error) {
	return f.failures, nil
}
func (f *fakeRepo) AddLoginFailure(_ context.Context, day string, r domain.Reason) error {
	if f.added == nil {
		f.added = map[string]int{}
	}
	f.added[day+"/"+string(r)]++
	return nil
}
func (f *fakeRepo) ExpiringMembers(context.Context, time.Time, int, int) ([]domain.AttentionRow, int, error) {
	return f.expiring, f.expCount, nil
}
func (f *fakeRepo) DataLimitedMembers(context.Context, time.Time) ([]domain.MemberRow, error) {
	return f.limited, nil
}

func limited(id int64, name string, usedMB int64) domain.MemberRow {
	return domain.MemberRow{
		Member: domain.Member{ID: id, Username: name, Active: true, SubscriptionDate: t0, UsedDownload: usedMB << 20},
		Plan:   domain.Plan{DurationDays: 30, BulkValue: 100, BulkUnit: "MB"},
	}
}

func TestOverviewTrafficCompare(t *testing.T) {
	days := domain.DayWindow(t0, time.UTC, ChartDays)
	f := &fakeRepo{totals: []domain.UsageDay{
		{Day: days[13], Down: 150}, // today
		{Day: days[12], Down: 100}, // yesterday
		{Day: days[6], Up: 300},    // the same weekday last week
	}}
	o, err := New(f, clock.NewFake(t0)).Overview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	tr := o.Traffic
	if tr.Today != 150 || tr.Yesterday != 100 || tr.LastWeek != 300 {
		t.Fatalf("%+v", tr)
	}
	if tr.VsYesterday != (Change{50, true}) || tr.VsLastWeek != (Change{-50, true}) {
		t.Errorf("changes %+v %+v", tr.VsYesterday, tr.VsLastWeek)
	}
	if len(o.TrafficDays) != ChartDays || len(o.ActiveDays) != ChartDays {
		t.Errorf("chart lengths %d %d", len(o.TrafficDays), len(o.ActiveDays))
	}
	// An empty base is "no data", not a percent.
	o, _ = New(&fakeRepo{}, clock.NewFake(t0)).Overview(context.Background())
	if o.Traffic.VsYesterday.Known || o.Traffic.VsLastWeek.Known {
		t.Errorf("zero base reported as known: %+v", o.Traffic)
	}
}

func TestOverviewFailuresAndEmptyInstall(t *testing.T) {
	f := &fakeRepo{failures: []domain.ReasonCount{{Reason: domain.ReasonOutOfBulk, Count: 2}, {Reason: domain.ReasonExpired, Count: 2}, {Reason: domain.ReasonInactive, Count: 1}}}
	o, err := New(f, clock.NewFake(t0)).Overview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if o.Failures.Total != 5 || !o.Failures.HasTop || o.Failures.Top.Reason != domain.ReasonExpired {
		t.Errorf("%+v", o.Failures)
	}
	o, err = New(&fakeRepo{}, clock.NewFake(t0)).Overview(context.Background())
	if err != nil || o.Failures.Total != 0 || o.Failures.HasTop || o.Online != (OnlineNow{}) || o.Expiring.Count != 0 || o.NearLimit.Count != 0 {
		t.Errorf("empty install: %+v %v", o, err)
	}
	for _, v := range o.BusyHours {
		if v != 0 {
			t.Errorf("busy hours of an empty install: %v", o.BusyHours)
			break
		}
	}
}

func TestNearLimitList(t *testing.T) {
	f := &fakeRepo{limited: []domain.MemberRow{
		limited(1, "at79", 79), limited(2, "at80", 80), limited(3, "full", 100), limited(4, "over", 150),
		limited(5, "a", 85), limited(6, "b", 90), limited(7, "c", 95),
	}}
	o, err := New(f, clock.NewFake(t0)).Overview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if o.NearLimit.Count != 6 { // 79% is out
		t.Fatalf("count %d", o.NearLimit.Count)
	}
	if len(o.NearLimit.Rows) != ListMax {
		t.Fatalf("rows %d, want %d", len(o.NearLimit.Rows), ListMax)
	}
	first := o.NearLimit.Rows[0]
	if first.Username != "over" || !first.OutOfData || first.Percent != 150 {
		t.Errorf("first row %+v", first)
	}
	for _, r := range o.NearLimit.Rows {
		if r.Username == "at79" {
			t.Error("a member at 79% is listed")
		}
	}
}

func TestNearLimitNeverListsUnlimitedPlans(t *testing.T) {
	unlimited := limited(1, "unlimited", 10<<10) // 10 GB used
	unlimited.Plan.BulkValue = 0
	f := &fakeRepo{limited: []domain.MemberRow{unlimited, limited(2, "at80", 80)}}
	o, err := New(f, clock.NewFake(t0)).Overview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if o.NearLimit.Count != 1 || o.NearLimit.Rows[0].Username != "at80" {
		t.Fatalf("%+v, want only at80", o.NearLimit)
	}
}

func TestNearLimitSkipsLapsedAndFutureWindows(t *testing.T) {
	old, future := limited(1, "lapsed", 90), limited(2, "future", 90)
	old.Member.SubscriptionDate = t0.AddDate(0, 0, -40) // a 30-day window that ended 10 days ago
	future.Member.SubscriptionDate = t0.AddDate(0, 0, 1)
	f := &fakeRepo{limited: []domain.MemberRow{old, future, limited(3, "running", 90)}}
	o, err := New(f, clock.NewFake(t0)).Overview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if o.NearLimit.Count != 1 || o.NearLimit.Rows[0].Username != "running" {
		t.Fatalf("%+v, want only the member whose window is running", o.NearLimit)
	}
}

func TestOnlineAndPeak(t *testing.T) {
	min := func(m int) time.Time { return t0.Add(time.Duration(m) * time.Minute) }
	ended1, ended2 := min(-30), min(-20)
	f := &fakeRepo{online: 1, spans: []domain.SessionSpan{
		{StartedAt: min(-60), EndedAt: &ended1, LastUpdate: min(-30)}, // finished
		{StartedAt: min(-50), EndedAt: &ended2, LastUpdate: min(-20)}, // overlaps the first
		{StartedAt: min(-10), LastUpdate: min(-1)},                    // open and online
		{StartedAt: min(-200), LastUpdate: min(-100)},                 // stale: ended at its last update
	}}
	o, err := New(f, clock.NewFake(t0)).Overview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := o.Online
	if got.Now != 1 || got.PeakToday != 2 {
		t.Errorf("%+v, want now 1 peak 2", got)
	}
}

func TestRecordLoginFailureUsesSettingsTimezone(t *testing.T) {
	// 23:30 UTC on the 7th is already the 8th in Tehran (UTC+3:30).
	now := time.Date(2026, 11, 7, 23, 30, 0, 0, time.UTC)
	for tz, want := range map[string]string{"UTC": "2026-11-07/EXPIRED", "Asia/Tehran": "2026-11-08/EXPIRED"} {
		f := &fakeRepo{tz: tz}
		if err := New(f, clock.NewFake(now)).RecordLoginFailure(context.Background(), domain.ReasonExpired); err != nil {
			t.Fatal(err)
		}
		if f.added[want] != 1 {
			t.Errorf("%s: %v, want key %s", tz, f.added, want)
		}
	}
}
