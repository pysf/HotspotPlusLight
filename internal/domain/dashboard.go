package domain

import (
	"cmp"
	"math"
	"math/bits"
	"slices"
	"time"
)

// NearDataLimitPercent is the share of the data allowance at which a member shows in the "near data limit" list (BR-30).
const NearDataLimitPercent = 80

// Presence is the time span in which a session counts as present (BR-25).
type Presence struct{ Start, End time.Time }

// SessionSpan is the three session times that decide its presence.
type SessionSpan struct {
	StartedAt  time.Time
	EndedAt    *time.Time // nil while the session is open
	LastUpdate time.Time
}

// PresenceOf returns the presence of a session (BR-25). A finished session ends at ended_at. An open one
// that is still online (BR-17: updated since activeSince) ends now. A stale open one ends at its last update.
func PresenceOf(s SessionSpan, now, activeSince time.Time) Presence {
	switch {
	case s.EndedAt != nil:
		return Presence{s.StartedAt, *s.EndedAt}
	case !s.LastUpdate.Before(activeSince):
		return Presence{s.StartedAt, now}
	}
	return Presence{s.StartedAt, s.LastUpdate}
}

// clip returns p cut to [from, to]. ok is false when nothing is left.
func (p Presence) clip(from, to time.Time) (Presence, bool) {
	if p.Start.Before(from) {
		p.Start = from
	}
	if p.End.After(to) {
		p.End = to
	}
	return p, p.End.After(p.Start)
}

// PeakConcurrent returns the largest number of spans that overlap at one moment inside [from, to].
// Spans that only touch (one ends when the next starts) do not overlap, and empty spans are ignored.
func PeakConcurrent(spans []Presence, from, to time.Time) int {
	type event struct {
		at    time.Time
		delta int
	}
	var events []event
	for _, s := range spans {
		if c, ok := s.clip(from, to); ok {
			events = append(events, event{c.Start, 1}, event{c.End, -1})
		}
	}
	// At the same moment the ends come first, so touching spans do not add up.
	slices.SortFunc(events, func(a, b event) int {
		return cmp.Or(a.at.Compare(b.at), cmp.Compare(a.delta, b.delta))
	})
	peak, now := 0, 0
	for _, e := range events {
		now += e.delta
		peak = max(peak, now)
	}
	return peak
}

// HourlyAverage returns, for each local clock hour, the average number of sessions present during that hour
// over the last days days, today included (BR-29). Days before the day of oldest (the oldest kept session) do not
// count, and an hour that has not started yet today is left out. The hour that is running now counts only for the
// part that has passed, so it is not pulled down by time that has not happened. A zero oldest means no data: all
// zeros. Values are rounded to one decimal.
func HourlyAverage(spans []Presence, oldest, now time.Time, loc *time.Location, days int) [24]float64 {
	var out [24]float64
	if oldest.IsZero() {
		return out
	}
	o, n := oldest.In(loc), now.In(loc)
	firstDay := time.Date(o.Year(), o.Month(), o.Day(), 0, 0, 0, 0, loc)
	var sum, weight [24]float64 // weight is the number of hours observed: 1 for a full hour, less for the running one
	for i := range days {
		y, m, d := n.Year(), n.Month(), n.Day()-i
		if time.Date(y, m, d, 0, 0, 0, 0, loc).Before(firstDay) {
			continue
		}
		for h := range 24 {
			start, end := time.Date(y, m, d, h, 0, 0, 0, loc), time.Date(y, m, d, h+1, 0, 0, 0, loc)
			if !start.Before(now) {
				continue
			}
			if end.After(now) {
				end = now
			}
			weight[h] += end.Sub(start).Seconds() / 3600
			for _, s := range spans {
				if c, ok := s.clip(start, end); ok {
					sum[h] += c.End.Sub(c.Start).Seconds() / 3600
				}
			}
		}
	}
	for h := range 24 {
		if weight[h] > 0 {
			out[h] = math.Round(sum[h]/weight[h]*10) / 10
		}
	}
	return out
}

// PercentChange returns (today - base) / base in whole percent, rounded half away from zero (BR-26).
// ok is false when base is 0, because a percent of nothing has no meaning.
func PercentChange(today, base int64) (percent int, ok bool) {
	if base == 0 {
		return 0, false
	}
	return int(math.Round(float64(today-base) / float64(base) * 100)), true
}

// atLeastPercent reports whether used * 100 >= allowed * pct. It multiplies in 128 bits, because
// the allowance can be saturated at the largest int64 (QuotaOf).
func atLeastPercent(used, allowed int64, pct uint64) bool {
	uh, ul := bits.Mul64(uint64(max(used, 0)), 100)
	ah, al := bits.Mul64(uint64(max(allowed, 0)), pct)
	return uh > ah || (uh == ah && ul >= al)
}

// NearDataLimit reports whether the quota shows 80% or more of a limited data allowance used (BR-30).
func NearDataLimit(q Quota) bool {
	return !q.DataUnlimited && q.AllowedBytes > 0 && atLeastPercent(q.UsedBytes, q.AllowedBytes, NearDataLimitPercent)
}

// OutOfData reports whether a limited data allowance is used up (BR-30 labels it "out of data").
func OutOfData(q Quota) bool {
	return !q.DataUnlimited && q.AllowedBytes > 0 && q.UsedBytes >= q.AllowedBytes
}

// DataPercent returns the used share of the data allowance in whole percent, rounded down and capped at 1000.
func DataPercent(q Quota) int {
	if q.DataUnlimited || q.AllowedBytes <= 0 || q.UsedBytes <= 0 {
		return 0
	}
	if q.UsedBytes/q.AllowedBytes >= 10 {
		return 1000
	}
	hi, lo := bits.Mul64(uint64(q.UsedBytes), 100)
	// hi < AllowedBytes because UsedBytes < 10 * AllowedBytes, so Div64 cannot overflow.
	p, _ := bits.Div64(hi, lo, uint64(q.AllowedBytes))
	return int(p)
}

// ExpiresWithin reports whether expiresAt is set, not yet past, and at most days days after now (BR-24, BR-30).
// Exactly days days away still counts. Times are compared in whole seconds, as in MemberStatus.
func ExpiresWithin(now time.Time, expiresAt *time.Time, days int) bool {
	if expiresAt == nil {
		return false
	}
	e := expiresAt.Unix()
	return e >= now.Unix() && e <= ExpiryLimit(now, days)
}

// ExpiryLimit returns the last Unix second at which an expiry still counts as "within days days of now".
// The SQL in the store uses it too, so the Go rule and the queries cannot drift.
func ExpiryLimit(now time.Time, days int) int64 {
	return now.Unix() + int64(days)*24*3600
}

// ReasonCount is how many logins failed for one reason.
type ReasonCount struct {
	Reason Reason
	Count  int64
}

// TopReason returns the reason with the highest count (BR-27). A tie goes to the code that sorts first.
// ok is false when nothing was counted.
func TopReason(counts []ReasonCount) (top ReasonCount, ok bool) {
	for _, c := range counts {
		if c.Count <= 0 {
			continue
		}
		if !ok || c.Count > top.Count || (c.Count == top.Count && c.Reason < top.Reason) {
			top, ok = c, true
		}
	}
	return top, ok
}

// DayCount is a count for one day key.
type DayCount struct {
	Day   string
	Count int
}

// FillDayCounts returns one DayCount per key in days (zero where rows has none), in that order.
func FillDayCounts(days []string, rows []DayCount) []DayCount {
	by := make(map[string]int, len(rows))
	for _, r := range rows {
		by[r.Day] = r.Count
	}
	out := make([]DayCount, len(days))
	for i, d := range days {
		out[i] = DayCount{Day: d, Count: by[d]}
	}
	return out
}

// AttentionRow is one line of a "needs attention" list (BR-30).
type AttentionRow struct {
	MemberID  int64
	Username  string
	ExpiresAt time.Time // set for the expiring list
	Percent   int       // used data share, set for the near-limit list
	OutOfData bool      // 100% or more used
}
