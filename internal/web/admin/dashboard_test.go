package admin

import "testing"

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
