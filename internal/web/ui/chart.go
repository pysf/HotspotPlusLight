package ui

import (
	"strconv"
	"strings"

	"github.com/a-h/templ"
)

// The kinds of Plot. The first three match the dashboard charts of the design, PlotWide is the usage chart.
const (
	PlotArea = "area" // a line with a soft area under it
	PlotBars = "bars" // round bars; all but the last are dimmed
	PlotDots = "dots" // a line with a dot on every value
	PlotWide = "wide" // a large bar plot with a value axis; all bars but the highest are dimmed
)

// geom is the geometry of one kind of plot, in view box units.
type geom struct {
	w, h        float64 // view box
	x0, x1      float64 // first and last x of a value
	base, rise  float64 // y of the zero value, and the height of the tallest value
	gridY, step float64 // y of the lowest grid line, and the distance between lines
	grids       int     // number of grid lines
	gridX0, gX1 float64 // ends of the grid lines
	axisY       float64 // y of the labels under the plot
	valueAxisX  float64 // x of the value labels (wide plot only)
}

// The plot is drawn in a 320 x 114 view box, as in the design. Area plots use the first geometry,
// bar and dot plots the second. The wide plot is the 920 x 230 chart of the usage board.
var (
	geomArea = geom{w: 320, h: 114, x0: 8, x1: 312, base: 92, rise: 72, gridY: 92, step: 36, grids: 3, gridX0: 8, gX1: 312, axisY: 110}
	geomBars = geom{w: 320, h: 114, x0: 30, x1: 300, base: 88, rise: 72, gridY: 88, step: 36, grids: 3, gridX0: 8, gX1: 312, axisY: 108}
	geomWide = geom{w: 920, h: 230, x0: 20, x1: 861, base: 186, rise: 168, gridY: 190, step: 57.5, grids: 4, gridX0: 10, gX1: 905, axisY: 212, valueAxisX: 915}
)

// geomOf returns the geometry of kind.
func geomOf(kind string) geom {
	switch kind {
	case PlotArea:
		return geomArea
	case PlotWide:
		return geomWide
	}
	return geomBars
}

// plotBase returns the y of the zero line.
func plotBase(kind string) float64 { return geomOf(kind).base }

// plotViewBox returns the view box of a plot.
func plotViewBox(kind string) string {
	g := geomOf(kind)
	return "0 0 " + strconv.Itoa(int(g.w)) + " " + strconv.Itoa(int(g.h))
}

// num formats a coordinate with at most one decimal.
func num(f float64) string { return strconv.FormatFloat(f, 'f', 1, 64) }

// plotX returns the x of value i out of n.
func plotX(kind string, i, n int) float64 {
	g := geomOf(kind)
	if n < 2 {
		return (g.x0 + g.x1) / 2
	}
	return g.x0 + (g.x1-g.x0)*float64(i)/float64(n-1)
}

// plotY returns the y of value v when the tallest value is top. A value above top is clipped to it.
func plotY(kind string, v, top int64) float64 {
	if top <= 0 || v <= 0 {
		return plotBase(kind)
	}
	return plotBase(kind) - geomOf(kind).rise*float64(min(v, top))/float64(top)
}

// gridLineY returns the y of grid line i, counted from the bottom.
func gridLineY(kind string, i int) float64 {
	g := geomOf(kind)
	return g.gridY - g.step*float64(i)
}

// plotGrid returns the path of the horizontal grid lines. They span the whole width in every kind, as in the design.
func plotGrid(kind string) string {
	g := geomOf(kind)
	var b strings.Builder
	for i := range g.grids {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString("M" + num(g.gridX0) + " " + num(gridLineY(kind, i)) + "H" + num(g.gX1))
	}
	return b.String()
}

// valueLabel is one text of the value axis.
type valueLabel struct{ Y, Text string }

// valueLabels places the texts of a wide plot's value axis next to its grid lines, bottom to top.
func valueLabels(kind string, texts []string) []valueLabel {
	if kind != PlotWide {
		return nil
	}
	out := make([]valueLabel, 0, len(texts))
	for i, t := range texts {
		if i >= geomWide.grids {
			break
		}
		out = append(out, valueLabel{Y: num(gridLineY(kind, i) + 4), Text: t})
	}
	return out
}

// barSize returns the data-size attribute of a bar in a wide plot: thicker for few bars, thinner for many, so they never touch.
func barSize(kind string, n int) templ.Attributes {
	if kind != PlotWide {
		return nil
	}
	switch {
	case n <= 14:
		return templ.Attributes{"data-size": "lg"}
	case n > 45:
		return templ.Attributes{"data-size": "sm"}
	}
	return nil
}

// peakIndex returns the index of the first highest bar, or -1 when every bar is 0.
func peakIndex(bars []Bar) int {
	best := -1
	var top int64
	for i, b := range bars {
		if b.Value > top {
			best, top = i, b.Value
		}
	}
	return best
}

// dimmed reports whether bar i is drawn pale: all but the last in a bar plot, all but the peak in a wide plot.
func dimmed(kind string, i int, bars []Bar) bool {
	if kind == PlotWide {
		return i != peakIndex(bars)
	}
	return i < len(bars)-1
}

// peakY returns the y of the peak text above bar i; it never leaves the view box.
func peakY(kind string, v, top int64) string { return num(max(plotY(kind, v, top)-17, 12)) }

// plotLine returns the path of the line through all values.
func plotLine(kind string, bars []Bar, top int64) string {
	var b strings.Builder
	for i, bar := range bars {
		if i == 0 {
			b.WriteString("M")
		} else {
			b.WriteString("L")
		}
		b.WriteString(num(plotX(kind, i, len(bars))) + " " + num(plotY(kind, bar.Value, top)))
	}
	return b.String()
}

// plotArea returns the path of the line closed down to the zero line.
func plotArea(kind string, bars []Bar, top int64) string {
	n := len(bars)
	base := num(plotBase(kind))
	return plotLine(kind, bars, top) + "L" + num(plotX(kind, n-1, n)) + " " + base + "L" + num(plotX(kind, 0, n)) + " " + base + "Z"
}

// barPath returns the path of bar i: a vertical stroke with round ends.
func barPath(kind string, i int, bars []Bar, top int64) string {
	x := num(plotX(kind, i, len(bars)))
	return "M" + x + " " + num(plotBase(kind)) + "V" + num(plotY(kind, bars[i].Value, top))
}

// axisLabel is one text under the plot.
type axisLabel struct {
	X      string
	Text   string
	Anchor string
}

// axisLabels picks the bars that get a label. A plot of up to 7 values labels all of them. A longer plot
// labels every second value counted from the last one; the area plot (24 hours) every sixth plus the last;
// the wide plot about seven values, evenly spread, plus the last when it is not too close to the one before.
func axisLabels(kind string, bars []Bar) []axisLabel {
	n := len(bars)
	var out []axisLabel
	add := func(i int) {
		a := "middle"
		if kind == PlotArea {
			switch i {
			case 0:
				a = "start"
			case n - 1:
				a = "end"
			}
		}
		out = append(out, axisLabel{X: num(plotX(kind, i, n)), Text: bars[i].Label, Anchor: a})
	}
	switch {
	case kind == PlotArea:
		for i := 0; i < n; i += 6 {
			add(i)
		}
		if n > 0 && (n-1)%6 != 0 {
			add(n - 1)
		}
	case n <= 7:
		for i := range bars {
			add(i)
		}
	case kind == PlotWide:
		step := max(1, (n+3)/6) // n / 6, rounded, at least 1
		last := 0
		for i := 0; i < n; i += step {
			add(i)
			last = i
		}
		if n-1-last >= (step+1)/2 {
			add(n - 1)
		}
	default:
		for i := (n - 1) % 2; i < n; i += 2 {
			add(i)
		}
	}
	return out
}

// axisY returns the y of the axis labels.
func axisY(kind string) string { return num(geomOf(kind).axisY) }
