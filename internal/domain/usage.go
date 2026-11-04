package domain

import "time"

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
