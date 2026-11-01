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

// ActiveSince is the oldest last_update_at at which an unended session still
// counts as active/online: accounting interval + 60 s (BR-9, BR-17).
func ActiveSince(now time.Time, accountingIntervalSeconds int) time.Time {
	return now.Add(-time.Duration(accountingIntervalSeconds+60) * time.Second)
}

// Legacy reply-attribute constants (PLAN BR-10; O2, O3, O4).
const (
	UnlimitedSessionTimeout = 172800        // 2 days, sent when the plan has no time limit (O4)
	UnlimitedXmitLimit      = 3_900_000_000 // sent when the plan has unlimited data (O3)
	MaxAttr32               = math.MaxUint32
	BurstFactor             = 4        // burst rate = 4 x speed (O2)
	BurstSeconds            = 8        // burst time (O2)
	MaxBurstKbps            = 12000000 // above this the rate limit is sent without burst
)

// Grant is what an Access-Accept carries (BR-10).
type Grant struct {
	InterimSeconds uint32
	// SessionTimeout is the remaining time quota in seconds.
	// MONITOR: legacy-faithful (O3/O4): the router ends the session when this
	// elapses, but the server does not enforce it and it is not capped by the
	// validity window or expires_at, so an unlimited-time member can outlive
	// the window by up to 48 h.
	SessionTimeout uint32
	// XmitLimit is the remaining data in bytes (Mikrotik-Xmit-Limit, 32-bit).
	// MONITOR: legacy-faithful (O3): a download-only cap enforced only if the
	// router honours it; there is no server-side enforcement on interim updates.
	XmitLimit uint32
	// XmitClamped reports that the real value exceeded 32 bits and was clamped.
	XmitClamped bool
	// RateLimit is the Mikrotik-Rate-Limit string, empty when the plan has no speed limit.
	RateLimit string
}

// GrantFor builds the Accept attributes from the member's quota and plan.
func GrantFor(q Quota, p Plan, intervalSeconds int) Grant {
	g := Grant{InterimSeconds: uint32(max(min(int64(intervalSeconds), MaxAttr32), 0)), RateLimit: RateLimit(p)}
	timeout := int64(UnlimitedSessionTimeout)
	if !q.TimeUnlimited {
		timeout = q.RemainingSecs
	}
	g.SessionTimeout = uint32(max(min(timeout, MaxAttr32), 0))
	xmit := int64(UnlimitedXmitLimit)
	if !q.DataUnlimited {
		xmit = q.RemainingBytes
	}
	if xmit > MaxAttr32 {
		xmit, g.XmitClamped = MaxAttr32, true
	}
	xmit = max(xmit, 0)
	g.XmitLimit = uint32(xmit)
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
