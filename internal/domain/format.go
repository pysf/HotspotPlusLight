package domain

import (
	"math"
	"strconv"
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
	if q.DataUnlimited {
		return FormatBytes(q.UsedBytes, unit) + " / unlimited"
	}
	return FormatBytes(q.UsedBytes, unit) + " / " + FormatBytes(q.AllowedBytes, unit) + " (" + FormatBytes(q.RemainingBytes, unit) + " left)"
}

// TimeText is "used / limit (left left)" in minutes, or "used / unlimited" (BR-22).
func (q Quota) TimeText() string {
	if q.TimeUnlimited {
		return FormatMinutes(q.UsedSeconds) + " / unlimited"
	}
	return FormatMinutes(q.UsedSeconds) + " / " + FormatMinutes(q.AllowedSeconds) + " (" + FormatMinutes(q.RemainingSecs) + " left)"
}
