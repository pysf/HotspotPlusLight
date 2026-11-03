package domain

import (
	"fmt"
	"math"
	"strconv"
	"time"
)

// FormatNumber prints v with at most 2 decimals and no trailing zeros.
func FormatNumber(v float64) string {
	return strconv.FormatFloat(math.Round(v*100)/100, 'f', -1, 64)
}

// bytesIn converts bytes to the given bulk unit (KB, MB, GB).
func bytesIn(b int64, unit string) float64 {
	one, err := BulkBytes(1, unit)
	if err != nil || one == 0 {
		return float64(b)
	}
	return float64(b) / float64(one)
}

// FormatBytes prints a byte count in the given bulk unit, e.g. "2.5 GB".
func FormatBytes(b int64, unit string) string { return FormatNumber(bytesIn(b, unit)) + " " + unit }

// BytesIn is the numeric value of b in unit, rounded for editing in a form field.
func BytesIn(b int64, unit string) string { return FormatNumber(bytesIn(b, unit)) }

// FormatMinutes prints seconds as minutes with at most 1 decimal, e.g. "30 min".
func FormatMinutes(seconds int64) string {
	return strconv.FormatFloat(math.Round(float64(seconds)/60*10)/10, 'f', -1, 64) + " min"
}

// DataText is "used / allowed (left left)" in the plan's unit, or "used / unlimited" (BR-22).
func (q Quota) DataText(unit string) string {
	main, left := q.DataParts(unit)
	if left == "" {
		return main
	}
	return main + " (" + left + ")"
}

// DataParts splits DataText: "used / allowed" and "N left" (empty when unlimited), for the member list cell.
func (q Quota) DataParts(unit string) (main, left string) {
	if q.DataUnlimited {
		return FormatBytes(q.UsedBytes, unit) + " / unlimited", ""
	}
	return FormatBytes(q.UsedBytes, unit) + " / " + FormatBytes(q.AllowedBytes, unit), FormatBytes(q.RemainingBytes, unit) + " left"
}

// TimeText is "used / limit (left left)" in minutes, or "used / unlimited" (BR-22).
func (q Quota) TimeText() string {
	if q.TimeUnlimited {
		return FormatMinutes(q.UsedSeconds) + " / unlimited"
	}
	return FormatMinutes(q.UsedSeconds) + " / " + FormatMinutes(q.AllowedSeconds) + " (" + FormatMinutes(q.RemainingSecs) + " left)"
}

// FormatSize prints a byte count in the unit that fits it: B, KB, MB or GB (online list).
func FormatSize(b int64) string {
	switch {
	case b < 1024:
		return strconv.FormatInt(b, 10) + " B"
	case b < 1024*1024:
		return FormatBytes(b, "KB")
	case b < 1024*1024*1024:
		return FormatBytes(b, "MB")
	}
	return FormatBytes(b, "GB")
}

// FormatDuration prints seconds as "30 s", "5 min 03 s" or "2 h 05 min".
func FormatDuration(seconds int64) string {
	seconds = max(seconds, 0)
	h, m, s := seconds/3600, seconds/60%60, seconds%60
	switch {
	case h > 0:
		return fmt.Sprintf("%d h %02d min", h, m)
	case m > 0:
		return fmt.Sprintf("%d min %02d s", m, s)
	}
	return fmt.Sprintf("%d s", s)
}

// FormatRelative says how far t is from now: "in 5 min", "47 h ago", "in 3 days" (member list).
func FormatRelative(now, t time.Time) string {
	d := t.Sub(now)
	past := d < 0
	if past {
		d = -d
	}
	var s string
	switch {
	case d < time.Minute:
		s = "under 1 min"
	case d < time.Hour:
		s = strconv.Itoa(int(d/time.Minute)) + " min"
	case d < 48*time.Hour:
		s = strconv.Itoa(int(d/time.Hour)) + " h"
	default:
		s = strconv.Itoa(int(d/(24*time.Hour))) + " days"
	}
	if past {
		return s + " ago"
	}
	return "in " + s
}
