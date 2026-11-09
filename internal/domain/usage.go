package domain

import (
	"math"
	"time"
)

// DayFormat is the layout of a usage day key; keys sort chronologically as text.
const DayFormat = "2006-01-02"

// DayKey returns the calendar day of t in loc, e.g. "2026-11-01". Usage is
// stored under the day (in settings.timezone) on which the packet arrived.
func DayKey(t time.Time, loc *time.Location) string { return t.In(loc).Format(DayFormat) }

// UsageDay is the traffic of all members on one day.
type UsageDay struct {
	Day               string
	Down, Up, Seconds int64
}

// Total is download plus upload.
func (u UsageDay) Total() int64 { return u.Down + u.Up }

// MemberUsage is one member's traffic over a range (top-N list).
type MemberUsage struct {
	MemberID          int64
	Username          string
	Down, Up, Seconds int64
}

// Total is download plus upload.
func (u MemberUsage) Total() int64 { return u.Down + u.Up }

// UsageRanges are the selectable top-N ranges in days.
var UsageRanges = []int{7, 30, 90}

// DefaultUsageDays is the range used when none (or an unknown one) is asked for.
const DefaultUsageDays = 30

// NormalizeUsageDays returns days if it is one of UsageRanges, else the default.
func NormalizeUsageDays(days int) int {
	for _, r := range UsageRanges {
		if r == days {
			return days
		}
	}
	return DefaultUsageDays
}

// DayWindow returns the n day keys ending with the day of now in loc, oldest first.
func DayWindow(now time.Time, loc *time.Location, n int) []string {
	t := now.In(loc)
	out := make([]string, n)
	for i := range n {
		// Noon, so a DST shift at midnight cannot change the day.
		d := time.Date(t.Year(), t.Month(), t.Day()-(n-1-i), 12, 0, 0, 0, loc)
		out[i] = d.Format(DayFormat)
	}
	return out
}

// FillDays returns one UsageDay per key in days (zeros where rows has none), in that order.
func FillDays(days []string, rows []UsageDay) []UsageDay {
	by := make(map[string]UsageDay, len(rows))
	for _, r := range rows {
		by[r.Day] = r
	}
	out := make([]UsageDay, len(days))
	for i, d := range days {
		u := by[d]
		u.Day = d
		out[i] = u
	}
	return out
}

// UsageSummary is the three figures above the usage chart.
type UsageSummary struct {
	Total, Average, PeakBytes int64
	PeakDay                   string // empty when there is no traffic
}

// SummarizeUsage adds up days. The average counts every day of the range, quiet ones too. On a tie the earlier day is the peak.
func SummarizeUsage(days []UsageDay) UsageSummary {
	var s UsageSummary
	for _, d := range days {
		s.Total += d.Total()
		if d.Total() > s.PeakBytes {
			s.PeakBytes, s.PeakDay = d.Total(), d.Day
		}
	}
	if len(days) > 0 {
		s.Average = s.Total / int64(len(days))
	}
	return s
}

// ChartAxis is the vertical axis of the usage chart: three equal steps up to a round Top, in Unit.
type ChartAxis struct {
	Top    int64    // bytes at the top grid line; 0 when there is no traffic
	Unit   string   // B, KB, MB, GB or TB
	Labels []string // the four grid lines, bottom (0) to top, as numbers in Unit
}

// NewChartAxis returns a round axis for a chart whose tallest value is max (the top is never below it).
func NewChartAxis(max int64) ChartAxis {
	if max <= 0 {
		return ChartAxis{Unit: "B"}
	}
	unit, div := "B", int64(1)
	for _, u := range []string{"KB", "MB", "GB", "TB"} {
		if max < div*1024 {
			break
		}
		unit, div = u, div*1024
	}
	step := niceStep(float64(max) / float64(div) / 3)
	a := ChartAxis{Top: int64(math.Round(step * 3 * float64(div))), Unit: unit}
	for i := range 4 {
		a.Labels = append(a.Labels, FormatNumber(step*float64(i)))
	}
	return a
}

// niceStep returns the smallest of 1, 2, 2.5 and 5 times a power of ten that is at least want.
func niceStep(want float64) float64 {
	for p := 0.001; ; p *= 10 {
		for _, m := range []float64{1, 2, 2.5, 5} {
			if m*p >= want {
				return m * p
			}
		}
	}
}
