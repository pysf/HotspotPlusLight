package admin

import (
	"testing"
	"time"

	"hotspotpluslight/internal/linkstatus"
)

// A tiny non-zero average must still draw a bar, or the chart would show its empty text although sessions exist.
func TestBusyBarsNonZeroAlwaysDraws(t *testing.T) {
	var avg [24]float64
	avg[3] = 0.004
	avg[14] = 2.34
	bars := busyBars(avg)
	if bars[3].Value < 1 {
		t.Errorf("a 0.004 average drew a bar of height %d", bars[3].Value)
	}
	if bars[0].Value != 0 {
		t.Errorf("a zero average drew a bar of height %d", bars[0].Value)
	}
	if bars[14].Value != 234 {
		t.Errorf("2.34 is drawn as %d, want 234 (hundredths)", bars[14].Value)
	}
	if got := bars[14].Attrs["data-avg"]; got != "2.34" {
		t.Errorf("data-avg %v, want 2.34", got)
	}
	if got := bars[0].Attrs["data-avg"]; got != "0.00" {
		t.Errorf("data-avg %v, want 0.00", got)
	}
}

// The caption peak comes from the averages, not from the rounded bar heights.
func TestBusyPeakReadsTheAverages(t *testing.T) {
	var avg [24]float64
	if got := busyPeak(avg); got != "0.0" {
		t.Errorf("empty peak %q", got)
	}
	avg[9], avg[10] = 1.26, 2.34
	if got := busyPeak(avg); got != "2.3" {
		t.Errorf("peak %q, want 2.3", got)
	}
	avg[11] = 2.36
	if got := busyPeak(avg); got != "2.4" {
		t.Errorf("peak %q, want 2.4", got)
	}
}

func TestSummaryAndItemsText(t *testing.T) {
	for _, c := range []struct {
		items int
		bad   bool
		want  string
	}{
		{0, false, "Everything is running normally."},
		{0, true, "The router sends packets with a wrong shared secret."},
		{1, false, "1 item needs attention."},
		{4, true, "4 items need attention."},
	} {
		if got := summaryText(c.items, c.bad); got != c.want {
			t.Errorf("summaryText(%d, %v) = %q, want %q", c.items, c.bad, got, c.want)
		}
	}
	for items, want := range map[int]string{0: "All clear", 1: "1 item", 9: "9 items"} {
		if got := itemsText(items); got != want {
			t.Errorf("itemsText(%d) = %q, want %q", items, got, want)
		}
	}
}

func TestDayLabel(t *testing.T) {
	if got := dayLabel("2026-11-02", false); got != "Mon" {
		t.Errorf("weekday label %q, want Mon", got)
	}
	if got := dayLabel("2026-11-02", true); got != "Today" {
		t.Errorf("last label %q, want Today", got)
	}
	if got := dayLabel("not a day", false); got != "not a day" {
		t.Errorf("bad day label %q", got)
	}
}

// The Router link state is red for a wrong secret, green for recent packets and grey otherwise.
func TestLinkRowsState(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	var s linkstatus.Snapshot
	if got := linkRows(s, time.UTC, now); got.Tone != "off" || got.Events[0].At != "never" {
		t.Errorf("empty snapshot: tone %q", got.Tone)
	}
	s.Accounting.At = now.Add(-routerRecent + time.Second)
	if got := linkRows(s, time.UTC, now); got.Tone != "ok" || !got.BadNone {
		t.Errorf("recent packet: tone %q", got.Tone)
	}
	s.Accounting.At = now.Add(-routerRecent)
	if got := linkRows(s, time.UTC, now); got.Tone != "off" {
		t.Errorf("packet exactly %v ago: tone %q, want off", routerRecent, got.Tone)
	}
	s.BadSecretTotal = 2
	if got := linkRows(s, time.UTC, now); got.Tone != "bad" || got.BadNone {
		t.Errorf("wrong secret: tone %q", got.Tone)
	}
}
