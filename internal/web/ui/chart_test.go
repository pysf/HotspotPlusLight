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
	if got := plotY(PlotBars, 5, 0); got != barBase {
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
	if got := axisLabels(PlotBars, nil); len(got) != 0 {
		t.Errorf("no bars have labels: %v", got)
	}
}
