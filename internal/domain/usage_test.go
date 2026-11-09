package domain

import (
	"strings"
	"testing"
	"time"
)

func TestDayKeyUsesTheZone(t *testing.T) {
	tehran, _ := time.LoadLocation("Asia/Tehran")
	at := time.Date(2026, 11, 1, 21, 0, 0, 0, time.UTC)
	if g := DayKey(at, time.UTC); g != "2026-11-01" {
		t.Fatal(g)
	}
	if g := DayKey(at, tehran); g != "2026-11-02" {
		t.Fatal(g)
	}
}

func TestDayWindowAcrossMonthAndDST(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	// 2026-11-01 is the US fall-back day.
	w := DayWindow(time.Date(2026, 11, 2, 15, 0, 0, 0, time.UTC), ny, 4)
	want := []string{"2026-10-30", "2026-10-31", "2026-11-01", "2026-11-02"}
	for i := range want {
		if w[i] != want[i] {
			t.Fatalf("%v, want %v", w, want)
		}
	}
	if w := DayWindow(time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC), time.UTC, 3); w[0] != "2026-02-28" {
		t.Fatal(w)
	}
}

func TestFillDaysAndRanges(t *testing.T) {
	got := FillDays([]string{"a", "b", "c"}, []UsageDay{{Day: "b", Down: 5, Up: 1}})
	if got[0] != (UsageDay{Day: "a"}) || got[1].Total() != 6 || got[2].Day != "c" {
		t.Fatalf("%+v", got)
	}
	for in, want := range map[int]int{7: 7, 30: 30, 90: 90, 0: 30, 45: 30, -7: 30} {
		if g := NormalizeUsageDays(in); g != want {
			t.Fatalf("%d -> %d", in, g)
		}
	}
}

func TestSummarizeUsage(t *testing.T) {
	days := []UsageDay{{Day: "a", Down: 10, Up: 2}, {Day: "b", Down: 30}, {Day: "c"}, {Day: "d", Down: 30}}
	s := SummarizeUsage(days)
	if s.Total != 72 || s.Average != 18 || s.PeakBytes != 30 || s.PeakDay != "b" {
		t.Fatalf("%+v", s)
	}
	if s := SummarizeUsage(nil); s != (UsageSummary{}) {
		t.Fatalf("no days: %+v", s)
	}
	if s := SummarizeUsage([]UsageDay{{Day: "a"}}); s.PeakDay != "" || s.Total != 0 {
		t.Fatalf("no traffic: %+v", s)
	}
}

func TestNewChartAxis(t *testing.T) {
	gb := int64(1) << 30
	a := NewChartAxis(62 * gb) // the design: 0, 25, 50, 75 GB
	if a.Unit != "GB" || a.Top != 75*gb || strings.Join(a.Labels, ",") != "0,25,50,75" {
		t.Fatalf("%+v", a)
	}
	// The top is never below the peak.
	a = NewChartAxis(76 * gb)
	if a.Top < 76*gb {
		t.Fatalf("%+v", a)
	}
	a = NewChartAxis(1536) // 1.5 KB
	if a.Unit != "KB" || strings.Join(a.Labels, ",") != "0,0.5,1,1.5" || a.Top != 1536 {
		t.Fatalf("%+v", a)
	}
	a = NewChartAxis(900)
	if a.Unit != "B" || a.Top < 900 || len(a.Labels) != 4 {
		t.Fatalf("%+v", a)
	}
	if a := NewChartAxis(0); a.Top != 0 || len(a.Labels) != 0 {
		t.Fatalf("empty: %+v", a)
	}
}
