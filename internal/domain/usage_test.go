package domain

import (
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
