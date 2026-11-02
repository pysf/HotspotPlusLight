package usage

import (
	"context"
	"testing"
	"time"

	"hotspotpluslight/internal/clock"
	"hotspotpluslight/internal/domain"
)

type fakeRepo struct {
	from, to       string
	topFrom, topTo string
	limit          int
}

func (f *fakeRepo) GetSettings(context.Context) (domain.Settings, error) {
	return domain.Settings{Timezone: "UTC"}, nil
}
func (f *fakeRepo) DailyTotals(_ context.Context, from, to string) ([]domain.UsageDay, error) {
	f.from, f.to = from, to
	return []domain.UsageDay{{Day: "2026-11-01", Down: 10, Up: 5}, {Day: "2026-10-20", Down: 40}}, nil
}
func (f *fakeRepo) TopMembers(_ context.Context, from, to string, limit int) ([]domain.MemberUsage, error) {
	f.topFrom, f.topTo, f.limit = from, to, limit
	return nil, nil
}

func TestReportWindows(t *testing.T) {
	r := &fakeRepo{}
	s := New(r, clock.NewFake(time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)))
	v, err := s.Report(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if r.from != "2026-10-03" || r.to != "2026-11-01" || r.topFrom != "2026-10-26" || r.topTo != "2026-11-01" || r.limit != TopN {
		t.Fatalf("%+v", r)
	}
	if len(v.Days) != ChartDays || v.MaxTotal != 40 || v.Range != 7 || v.Days[29].Total() != 15 {
		t.Fatalf("%+v", v)
	}
	if v, _ := s.Report(context.Background(), 12); v.Range != 30 {
		t.Fatal("bad range not defaulted")
	}
}
