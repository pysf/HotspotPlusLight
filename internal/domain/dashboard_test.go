package domain

import (
	"math"
	"testing"
	"time"
)

var dt0 = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)

func span(startMin, endMin int) Presence {
	return Presence{dt0.Add(time.Duration(startMin) * time.Minute), dt0.Add(time.Duration(endMin) * time.Minute)}
}

func TestPeakConcurrent(t *testing.T) {
	from, to := dt0.Add(-time.Hour), dt0.Add(time.Hour)
	for name, c := range map[string]struct {
		spans []Presence
		want  int
	}{
		"none":                {nil, 0},
		"one":                 {[]Presence{span(0, 10)}, 1},
		"overlap":             {[]Presence{span(0, 10), span(5, 15), span(20, 30)}, 2},
		"touching":            {[]Presence{span(0, 10), span(10, 20)}, 1},
		"three at once":       {[]Presence{span(0, 30), span(5, 25), span(10, 20)}, 3},
		"empty span ignored":  {[]Presence{span(5, 5), span(0, 10)}, 1},
		"before the window":   {[]Presence{span(-300, -200)}, 0},
		"clipped at midnight": {[]Presence{span(-300, 5), span(-100, 5)}, 2},
	} {
		if got := PeakConcurrent(c.spans, from, to); got != c.want {
			t.Errorf("%s: got %d, want %d", name, got, c.want)
		}
	}
}

func TestPresenceOf(t *testing.T) {
	now := dt0
	activeSince := now.Add(-2 * time.Minute)
	ended := now.Add(-time.Hour)
	for name, c := range map[string]struct {
		s    SessionSpan
		want time.Time
	}{
		"finished":         {SessionSpan{StartedAt: now.Add(-2 * time.Hour), EndedAt: &ended, LastUpdate: ended}, ended},
		"open online":      {SessionSpan{StartedAt: now.Add(-2 * time.Hour), LastUpdate: now.Add(-time.Minute)}, now},
		"open at the edge": {SessionSpan{StartedAt: now.Add(-2 * time.Hour), LastUpdate: activeSince}, now},
		"stale":            {SessionSpan{StartedAt: now.Add(-2 * time.Hour), LastUpdate: now.Add(-time.Hour)}, now.Add(-time.Hour)},
	} {
		if got := PresenceOf(c.s, now, activeSince); !got.End.Equal(c.want) {
			t.Errorf("%s: end %v, want %v", name, got.End, c.want)
		}
	}
}

func TestHourlyAverage(t *testing.T) {
	for _, zone := range []string{"UTC", "Asia/Tehran"} {
		loc, err := time.LoadLocation(zone)
		if err != nil {
			t.Fatal(err)
		}
		now := time.Date(2026, 11, 7, 10, 30, 0, 0, loc)
		at := func(day, hour, min int) time.Time { return time.Date(2026, 11, day, hour, min, 0, 0, loc) }
		// One session on the 5th from 09:00 to 10:30, one on the 6th from 09:30 to 10:00, one today from 09:00 until now.
		spans := []Presence{{at(5, 9, 0), at(5, 10, 30)}, {at(6, 9, 30), at(6, 10, 0)}, {at(7, 9, 0), now}}
		oldest := at(5, 9, 0)
		got := HourlyAverage(spans, oldest, now, loc, 14)
		// Hour 9: days 5, 6, 7 count. Presence: 1 h, 0.5 h, 1 h -> 2.5 / 3 = 0.83 -> 0.8.
		if got[9] != 0.8 {
			t.Errorf("%s: hour 9 = %v, want 0.8", zone, got[9])
		}
		// Hour 10: today it has run for 30 minutes so far, so today counts half an hour of observation.
		// Presence 0.5 h (day 5) + 0 (day 6) + 0.5 h (day 7) = 1.0, over 1 + 1 + 0.5 = 2.5 hours -> 0.4.
		if got[10] != 0.4 {
			t.Errorf("%s: hour 10 = %v, want 0.4", zone, got[10])
		}
		// Hour 11 has not started today: only the days 5 and 6 count, both 0.
		if got[11] != 0 {
			t.Errorf("%s: hour 11 = %v, want 0", zone, got[11])
		}
		// Days before the oldest session do not lower the average (a new install).
		if got[3] != 0 {
			t.Errorf("%s: hour 3 = %v", zone, got[3])
		}
	}
	// No sessions at all: all zeros.
	if got := HourlyAverage(nil, time.Time{}, dt0, time.UTC, 14); got != ([24]float64{}) {
		t.Errorf("no data: %v", got)
	}
}

func TestHourlyAverageLeavesOutHoursNotStarted(t *testing.T) {
	// Today is 10:30, so hour 20 has not started today. A full hour 20:00-21:00 on the 6th gives 1.0,
	// not 0.5 as it would if today counted as an empty day.
	loc := time.UTC
	now := time.Date(2026, 11, 7, 10, 30, 0, 0, loc)
	spans := []Presence{{time.Date(2026, 11, 6, 20, 0, 0, 0, loc), time.Date(2026, 11, 6, 21, 0, 0, 0, loc)}}
	got := HourlyAverage(spans, spans[0].Start, now, loc, 14)
	if got[20] != 1.0 {
		t.Errorf("hour 20 = %v, want 1.0", got[20])
	}
}

func TestHourlyAverageRunningHourIsNotPulledDown(t *testing.T) {
	// At 14:05 one session has been present for all 5 minutes of the running hour, the only day with data.
	// The bar is 1.0, not 0.1 as it would be if the whole hour counted as observed.
	loc := time.UTC
	now := time.Date(2026, 11, 7, 14, 5, 0, 0, loc)
	spans := []Presence{{time.Date(2026, 11, 7, 14, 0, 0, 0, loc), now}}
	got := HourlyAverage(spans, spans[0].Start, now, loc, 14)
	if got[14] != 1.0 {
		t.Errorf("hour 14 = %v, want 1.0", got[14])
	}
}

func TestPercentChange(t *testing.T) {
	for _, c := range []struct {
		today, base int64
		want        int
		ok          bool
	}{
		{100, 0, 0, false}, {0, 0, 0, false}, {150, 100, 50, true}, {50, 100, -50, true}, {100, 100, 0, true},
		{1, 3, -67, true}, {2, 3, -33, true}, {4, 3, 33, true}, {5, 3, 67, true}, {0, 100, -100, true},
		{1005, 1000, 1, true}, {1004, 1000, 0, true}, {995, 1000, -1, true},
	} {
		got, ok := PercentChange(c.today, c.base)
		if got != c.want || ok != c.ok {
			t.Errorf("PercentChange(%d, %d) = %d, %v; want %d, %v", c.today, c.base, got, ok, c.want, c.ok)
		}
	}
}

func TestExpiresWithinAndMemberStatusBoundaries(t *testing.T) {
	now := dt0
	at := func(d time.Duration) *time.Time { v := now.Add(d); return &v }
	day := 24 * time.Hour
	if ExpiresWithin(now, nil, 7) {
		t.Error("nil expiry")
	}
	for name, c := range map[string]struct {
		exp  *time.Time
		days int
		want bool
	}{
		"exactly now":       {at(0), 7, true},
		"one second past":   {at(-time.Second), 7, false},
		"exactly 7 days":    {at(7 * day), 7, true},
		"7 days + 1 s":      {at(7*day + time.Second), 7, false},
		"exactly 3 days":    {at(3 * day), 3, true},
		"3 days + 1 s":      {at(3*day + time.Second), 3, false},
		"far in the future": {at(400 * day), 7, false},
	} {
		if got := ExpiresWithin(now, c.exp, c.days); got != c.want {
			t.Errorf("%s: %v, want %v", name, got, c.want)
		}
	}
	// MemberStatus keeps its BR-24 behaviour at every boundary, with the shared function.
	for name, c := range map[string]struct {
		active bool
		exp    *time.Time
		want   string
	}{
		"no expiry":             {true, nil, StatusActive},
		"expires in 3 days":     {true, at(3 * day), StatusExpiring},
		"3 days and a second":   {true, at(3*day + time.Second), StatusActive},
		"expires now":           {true, at(0), StatusExpiring},
		"expired a second ago":  {true, at(-time.Second), StatusExpired},
		"inactive and expiring": {false, at(day), StatusInactive},
		"inactive and expired":  {false, at(-day), StatusExpired},
	} {
		if got := MemberStatus(c.active, c.exp, now); got != c.want {
			t.Errorf("%s: %s, want %s", name, got, c.want)
		}
	}
}

func TestDataLimitRule(t *testing.T) {
	q := func(used, allowed int64) Quota { return Quota{UsedBytes: used, AllowedBytes: allowed} }
	for name, c := range map[string]struct {
		q         Quota
		near, out bool
		percent   int
	}{
		"79%":                {q(79, 100), false, false, 79},
		"80%":                {q(80, 100), true, false, 80},
		"99%":                {q(99, 100), true, false, 99},
		"100%":               {q(100, 100), true, true, 100},
		"over":               {q(250, 100), true, true, 250},
		"far over is capped": {q(5000, 100), true, true, 1000},
		"unlimited":          {Quota{DataUnlimited: true, UsedBytes: 999}, false, false, 0},
		"nothing used":       {q(0, 100), false, false, 0},
		"huge, 79%":          {q(79e16, 1e18), false, false, 79}, // used * 100 does not fit in int64
		"huge, 80%":          {q(80e16, 1e18), true, false, 80},
		"saturated, half":    {q(math.MaxInt64/2, math.MaxInt64), false, false, 49},
	} {
		if got := NearDataLimit(c.q); got != c.near {
			t.Errorf("%s: near %v, want %v", name, got, c.near)
		}
		if got := OutOfData(c.q); got != c.out {
			t.Errorf("%s: out %v, want %v", name, got, c.out)
		}
		if got := DataPercent(c.q); got != c.percent {
			t.Errorf("%s: percent %d, want %d", name, got, c.percent)
		}
	}
}

func TestTopReason(t *testing.T) {
	if _, ok := TopReason(nil); ok {
		t.Error("empty list has no top reason")
	}
	if _, ok := TopReason([]ReasonCount{{ReasonExpired, 0}}); ok {
		t.Error("a zero count is not a top reason")
	}
	got, ok := TopReason([]ReasonCount{{ReasonOutOfBulk, 3}, {ReasonExpired, 5}, {ReasonInactive, 2}})
	if !ok || got.Reason != ReasonExpired || got.Count != 5 {
		t.Errorf("top %+v", got)
	}
	// A tie goes to the code that sorts first.
	got, _ = TopReason([]ReasonCount{{ReasonOutOfTime, 4}, {ReasonExpired, 4}, {ReasonOutOfBulk, 4}})
	if got.Reason != ReasonExpired {
		t.Errorf("tie: %s, want EXPIRED", got.Reason)
	}
}

func TestFillDayCounts(t *testing.T) {
	got := FillDayCounts([]string{"2026-11-05", "2026-11-06", "2026-11-07"}, []DayCount{{"2026-11-07", 4}, {"2026-11-05", 1}})
	want := []DayCount{{"2026-11-05", 1}, {"2026-11-06", 0}, {"2026-11-07", 4}}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%d: %+v, want %+v", i, got[i], want[i])
		}
	}
}
