package domain

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// MaxUsernameLen is the longest member username (BR-1).
const MaxUsernameLen = 64

var usernameRE = regexp.MustCompile(`^[A-Za-z0-9]+$`)

// NormalizeUsername applies BR-1 to a login name: trimmed, only A-Z a-z 0-9,
// 1-64 characters. Uniqueness is case-insensitive in the database, so the case
// is kept. ok is false for anything else (no digit conversion, O13).
func NormalizeUsername(raw string) (string, bool) {
	u := strings.TrimSpace(raw)
	if len(u) > MaxUsernameLen || !usernameRE.MatchString(u) {
		return "", false
	}
	return u, true
}

// ActiveSince returns the oldest last_update_at at which an unended session still
// counts as active/online: accounting interval + 60 s (BR-9, BR-17).
func ActiveSince(now time.Time, accountingIntervalSeconds int) time.Time {
	return now.Add(-time.Duration(accountingIntervalSeconds+60) * time.Second)
}

// Reply-attribute constants (PLAN BR-10; O2).
const (
	MaxAttr32    = math.MaxUint32
	BurstFactor  = 4        // burst rate = 4 x speed (O2)
	BurstSeconds = 8        // burst time (O2)
	MaxBurstKbps = 12000000 // above this the rate limit is sent without burst
)

// Grant is what an Access-Accept carries (BR-10).
type Grant struct {
	InterimSeconds uint32
	// SessionTimeout is the smallest of: time quota left, time until expires_at,
	// time until the window end. At least 1 second (O4, 2026-11-04).
	SessionTimeout uint32
	// DataLimited is false when the plan has unlimited data: then no data-limit
	// attribute is sent.
	DataLimited bool
	// DataLow and DataGiga are the bytes left, split into the low 32 bits and the
	// high 32 bits (the -Gigawords attribute). Sent as Mikrotik-Xmit-Limit and
	// Mikrotik-Recv-Limit, both the same value (O3, 2026-11-04).
	DataLow, DataGiga uint32
	// RateLimit is the Mikrotik-Rate-Limit string, empty when the plan has no speed limit.
	RateLimit string
}

// SplitBytes splits n (negative counts as 0) into the low and the high 32 bits.
func SplitBytes(n int64) (low, giga uint32) {
	u := uint64(max(n, 0))
	return uint32(u), uint32(u >> 32)
}

// GrantFor returns the Accept attributes for the member's state and plan. This
// is layer 1 of the enforcement (O3/O4): exact limits for the router. Layer 2
// is ShouldEnd on every Interim.
func GrantFor(m MemberState, p Plan, intervalSeconds int, now time.Time) Grant {
	q := QuotaOf(m, p)
	g := Grant{InterimSeconds: uint32(max(min(int64(intervalSeconds), MaxAttr32), 0)), RateLimit: RateLimit(p)}
	if !q.DataUnlimited {
		g.DataLimited = true
		g.DataLow, g.DataGiga = SplitBytes(q.RemainingBytes)
	}
	timeout := int64(q.WindowEnd.Sub(now) / time.Second) // the window always has an end
	if !q.TimeUnlimited {
		timeout = min(timeout, q.RemainingSecs)
	}
	if m.ExpiresAt != nil {
		timeout = min(timeout, int64(m.ExpiresAt.Sub(now)/time.Second))
	}
	g.SessionTimeout = uint32(max(min(timeout, MaxAttr32), 1))
	return g
}

// RateLimit is the legacy Mikrotik-Rate-Limit value (avps.js getRateLimit), one
// speed for both directions with a fixed burst (O2); "" when the plan has no speed.
func RateLimit(p Plan) string {
	kbps, err := SpeedKbps(p.SpeedValue, p.SpeedUnit)
	if err != nil || kbps <= 0 {
		return ""
	}
	s := int64(max(math.Round(kbps), 1)) // never send "0k", which RouterOS reads as unlimited
	b := s * BurstFactor
	if b > MaxBurstKbps {
		return strconv.FormatInt(s, 10) + "k"
	}
	half := (b + 1) / 2
	return strconv.FormatInt(s, 10) + "k/" + strconv.FormatInt(s, 10) + "k " + strconv.FormatInt(b, 10) + "k/" + strconv.FormatInt(b, 10) + "k " + strconv.FormatInt(half, 10) + "k/" + strconv.FormatInt(half, 10) + "k " +
		strconv.FormatInt(BurstSeconds, 10) + "/" + strconv.FormatInt(BurstSeconds, 10)
}
