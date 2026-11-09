package ui

import (
	"strings"
	"testing"
)

func bars(n int) []Bar {
	out := make([]Bar, n)
	for i := range out {
		out[i] = Bar{Label: strings.Repeat("x", i+1), Value: int64(i)}
	}
	return out
}

func TestPlotCoordinates(t *testing.T) {
	if got := num(plotX(PlotArea, 0, 24)); got != "8.0" {
		t.Errorf("area first x %s, want 8.0", got)
	}
	if got := num(plotX(PlotArea, 23, 24)); got != "312.0" {
		t.Errorf("area last x %s, want 312.0", got)
	}
	// Seven bars sit 45 apart, as in the design.
	if a, b := plotX(PlotBars, 0, 7), plotX(PlotBars, 1, 7); b-a != 45 || a != 30 {
		t.Errorf("bars at %v and %v", a, b)
	}
	if got := num(plotY(PlotBars, 10, 10)); got != "16.0" {
		t.Errorf("tallest bar top %s, want 16.0", got)
	}
	if got := num(plotY(PlotArea, 0, 10)); got != "92.0" {
		t.Errorf("zero value y %s, want 92.0", got)
	}
	if got := plotY(PlotBars, 99, 10); got != plotY(PlotBars, 10, 10) {
		t.Errorf("a value above top is not clipped: %v", got)
	}
	if got := plotY(PlotBars, 5, 0); got != geomBars.base {
		t.Errorf("top 0 gives y %v", got)
	}
}

func TestAxisLabels(t *testing.T) {
	texts := func(l []axisLabel) []string {
		var out []string
		for _, a := range l {
			out = append(out, a.Text)
		}
		return out
	}
	if got := len(axisLabels(PlotBars, bars(7))); got != 7 {
		t.Errorf("7 bars have %d labels, want 7", got)
	}
	// 14 bars: every second one, counted back from the last.
	l := axisLabels(PlotBars, bars(14))
	if len(l) != 7 || l[len(l)-1].Text != bars(14)[13].Label {
		t.Errorf("14 bars: %v", texts(l))
	}
	// 24 hours: 0, 6, 12, 18 and the last one, with the ends anchored inside the view box.
	l = axisLabels(PlotArea, bars(24))
	if len(l) != 5 || l[0].Anchor != "start" || l[4].Anchor != "end" || l[2].Anchor != "middle" {
		t.Errorf("24 hours: %+v", l)
	}
	// Short wide plots must not loop forever.
	for n := 1; n <= 3; n++ {
		if got := axisLabels(PlotWide, bars(n)); len(got) == 0 {
			t.Errorf("wide plot of %d bars has no labels", n)
		}
	}
	if got := axisLabels(PlotBars, nil); len(got) != 0 {
		t.Errorf("no bars have labels: %v", got)
	}
}

func TestWidePlotMatchesTheDesign(t *testing.T) {
	// The usage board: 62 of 75 GB is a bar from 186 up to about 47; the grid lines sit at 190, 132.5, 75 and 17.5.
	if got := num(plotY(PlotWide, 62, 75)); got != "47.1" {
		t.Errorf("62 of 75 gives y %s", got)
	}
	if got := plotGrid(PlotWide); got != "M10.0 190.0H905.0 M10.0 132.5H905.0 M10.0 75.0H905.0 M10.0 17.5H905.0" {
		t.Errorf("grid %q", got)
	}
	if a, b := plotX(PlotWide, 0, 30), plotX(PlotWide, 29, 30); a != 20 || b != 861 {
		t.Errorf("first and last x %v %v", a, b)
	}
	if got := plotViewBox(PlotWide); got != "0 0 920 230" {
		t.Errorf("view box %q", got)
	}
}

func TestWideAxisLabels(t *testing.T) {
	// 30 days: every fifth, plus the last (4 away is far enough).
	l := axisLabels(PlotWide, bars(30))
	if len(l) != 7 || l[0].Text != bars(30)[0].Label || l[6].Text != bars(30)[29].Label {
		t.Errorf("30 days: %d labels", len(l))
	}
	// 90 days: every fifteenth.
	if got := len(axisLabels(PlotWide, bars(90))); got < 6 || got > 8 {
		t.Errorf("90 days: %d labels", got)
	}
	// 7 days: all of them.
	if got := len(axisLabels(PlotWide, bars(7))); got != 7 {
		t.Errorf("7 days: %d labels", got)
	}
}

func TestPeakAndDimming(t *testing.T) {
	b := []Bar{{Value: 3}, {Value: 9}, {Value: 9}, {Value: 1}}
	if got := peakIndex(b); got != 1 {
		t.Errorf("peak %d, want the first highest (1)", got)
	}
	if peakIndex([]Bar{{}, {}}) != -1 {
		t.Error("an empty plot has no peak")
	}
	if dimmed(PlotWide, 1, b) || !dimmed(PlotWide, 0, b) || !dimmed(PlotWide, 3, b) {
		t.Error("wide plot: only the peak is full")
	}
	if dimmed(PlotBars, 3, b) || !dimmed(PlotBars, 2, b) {
		t.Error("bar plot: only the last is full")
	}
	if barSize(PlotWide, 7)["data-size"] != "lg" || barSize(PlotWide, 30) != nil || barSize(PlotWide, 90)["data-size"] != "sm" || barSize(PlotBars, 7) != nil {
		t.Error("bar sizes")
	}
}
