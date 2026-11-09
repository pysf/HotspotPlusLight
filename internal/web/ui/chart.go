package ui

import (
	"strconv"
	"strings"
)

// The kinds of Plot. They match the three charts of the design.
const (
	PlotArea = "area" // a line with a soft area under it
	PlotBars = "bars" // round bars; all but the last are dimmed
	PlotDots = "dots" // a line with a dot on every value
)

// The plot is drawn in a 320 x 114 view box, as in the design. Area plots use the left of
// the two geometries below, bar and dot plots the right one.
const (
	plotW, plotH = 320, 114
	areaX0       = 8.0   // first and last x of an area plot
	areaX1       = 312.0 //
	areaBase     = 92.0  // y of the zero line of an area plot
	barX0        = 30.0  // first and last x of a bar or dot plot
	barX1        = 300.0 //
	barBase      = 88.0  // y of the zero line of a bar or dot plot
	plotRise     = 72.0  // height of the tallest value
)

// plotBase returns the y of the zero line.
func plotBase(kind string) float64 {
	if kind == PlotArea {
		return areaBase
	}
	return barBase
}

// plotViewBox returns the view box of every plot.
func plotViewBox() string { return "0 0 " + strconv.Itoa(plotW) + " " + strconv.Itoa(plotH) }

// num formats a coordinate with at most one decimal.
func num(f float64) string { return strconv.FormatFloat(f, 'f', 1, 64) }

// plotX returns the x of value i out of n.
func plotX(kind string, i, n int) float64 {
	x0, x1 := barX0, barX1
	if kind == PlotArea {
		x0, x1 = areaX0, areaX1
	}
	if n < 2 {
		return (x0 + x1) / 2
	}
	return x0 + (x1-x0)*float64(i)/float64(n-1)
}

// plotY returns the y of value v when the tallest value is top. A value above top is clipped to it.
func plotY(kind string, v, top int64) float64 {
	if top <= 0 || v <= 0 {
		return plotBase(kind)
	}
	return plotBase(kind) - plotRise*float64(min(v, top))/float64(top)
}

// plotGrid returns the path of the three horizontal grid lines.
func plotGrid(kind string) string {
	var b strings.Builder
	base := plotBase(kind)
	// The grid spans the whole width in every kind, as in the design.
	x0, x1 := areaX0, areaX1
	for i, y := range []float64{base, base - plotRise/2, base - plotRise} {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString("M" + num(x0) + " " + num(y) + "H" + num(x1))
	}
	return b.String()
}

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
// labels every second value counted from the last one, or, for the area plot (24 hours), every sixth plus the last.
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
	default:
		for i := (n - 1) % 2; i < n; i += 2 {
			add(i)
		}
	}
	return out
}

// axisY returns the y of the axis labels.
func axisY(kind string) string {
	if kind == PlotArea {
		return "110"
	}
	return "108"
}
