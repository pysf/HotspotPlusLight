package domain

import (
	"math"
	"time"
)

// maxWindowDays keeps DurationDays*24h inside time.Duration (about 292 years).
const maxWindowDays = 100_000

// Reason is the stable code of a login rejection (PLAN 7.6). The empty Reason means "allowed".
type Reason string

const (
	ReasonInactive       Reason = "INACTIVE_USER"
	ReasonExpired        Reason = "EXPIRED"
	ReasonNoSubscription Reason = "NO_ACTIVE_SUBSCRIPTION"
	ReasonOutOfBulk      Reason = "OUT_OF_BULK"
	ReasonOutOfTime      Reason = "OUT_OF_TIME"
)

// MemberState is what the login rules need to know about a member.
type MemberState struct {
	Active           bool
	SubscriptionDate time.Time
	ExtraBulkBytes   int64
	ExpiresAt        *time.Time
	UsedDownload     int64
	UsedUpload       int64
	UsedSeconds      int64
}

// Quota is a member's allowance in the current validity window (BR-5, BR-7, BR-8).
type Quota struct {
	WindowStart, WindowEnd time.Time

	DataUnlimited                              bool
	AllowedBytes, UsedBytes, RemainingBytes    int64
	TimeUnlimited                              bool
	AllowedSeconds, UsedSeconds, RemainingSecs int64
}

// QuotaOf computes the quota of member state m on plan p. It is the single
// implementation behind the login check and the admin summary (BR-22).
func QuotaOf(m MemberState, p Plan) Quota {
	q := Quota{
		WindowStart: m.SubscriptionDate,
		WindowEnd:   m.SubscriptionDate.Add(time.Duration(min(p.DurationDays, maxWindowDays)) * 24 * time.Hour), // BR-5
		UsedBytes:   m.UsedDownload + m.UsedUpload,
		UsedSeconds: m.UsedSeconds,
	}
	if p.BulkValue == 0 { // BR-7: 0 = unlimited
		q.DataUnlimited = true
	} else {
		// An unknown unit cannot happen (DB CHECK); allow nothing rather than everything if it does.
		base, _ := BulkBytes(p.BulkValue, p.BulkUnit)
		q.AllowedBytes = base
		if m.ExtraBulkBytes > math.MaxInt64-base { // saturate
			q.AllowedBytes = math.MaxInt64
		} else {
			q.AllowedBytes += m.ExtraBulkBytes
		}
		q.RemainingBytes = max(q.AllowedBytes-q.UsedBytes, 0)
	}
	if p.TimeLimitMinutes == 0 { // BR-8: 0 = unlimited
		q.TimeUnlimited = true
	} else {
		q.AllowedSeconds = int64(p.TimeLimitMinutes) * 60
		q.RemainingSecs = max(q.AllowedSeconds-q.UsedSeconds, 0)
	}
	return q
}

// WindowLapsed reports whether now is outside [start, end] (BR-5/6; legacy "between", inclusive).
func (q Quota) WindowLapsed(now time.Time) bool {
	return now.Before(q.WindowStart) || now.After(q.WindowEnd)
}

// Decision is the outcome of the login rules 2 and 5-8 of PLAN 7.2.
type Decision struct {
	Reason Reason // "" = allowed
	// Renew is true when the validity window has lapsed and the plan renews
	// automatically (BR-6): the caller renews the member (BR-13b) before accepting.
	Renew bool
}

// Allowed reports whether the member may log in (ignoring the concurrent-session check).
func (d Decision) Allowed() bool { return d.Reason == "" }

// Evaluate applies, in order: active (2), expires_at not passed (5, O7), validity
// window with auto-resubscribe (6), remaining data (7), remaining time (8).
// Checks 1 (credentials), 3-4 (plan present) and 9 (sessions) are the caller's.
// It is pure: the caller supplies now and performs any renewal.
func Evaluate(m MemberState, p Plan, now time.Time) Decision {
	if !m.Active {
		return Decision{Reason: ReasonInactive}
	}
	if m.ExpiresAt != nil && now.After(*m.ExpiresAt) {
		return Decision{Reason: ReasonExpired}
	}
	q := QuotaOf(m, p)
	var d Decision
	if q.WindowLapsed(now) {
		if !p.AutoResubscribe {
			return Decision{Reason: ReasonNoSubscription}
		}
		// BR-13b: a renewal restarts the window and clears extra bulk and counters.
		m = RenewedState(m, now)
		q = QuotaOf(m, p)
		d.Renew = true
	}
	if !q.DataUnlimited && q.RemainingBytes <= 0 {
		d.Reason = ReasonOutOfBulk
	} else if !q.TimeUnlimited && q.RemainingSecs <= 0 {
		d.Reason = ReasonOutOfTime
	}
	return d
}

// RenewedState is the member state after a renewal at now (BR-13b).
func RenewedState(m MemberState, now time.Time) MemberState {
	m.SubscriptionDate = now
	m.ExtraBulkBytes = 0
	m.UsedDownload, m.UsedUpload, m.UsedSeconds = 0, 0, 0
	return m
}

// Summary is what the admin pages show for a member (BR-22).
type Summary struct {
	Quota    Quota
	Decision Decision
	Status   string // active | inactive | expired
}

// Summarize builds the summary from the same functions the login uses.
func Summarize(m Member, p Plan, now time.Time) Summary {
	return Summary{Quota: QuotaOf(m.State(), p), Decision: Evaluate(m.State(), p, now), Status: MemberStatus(m.Active, m.ExpiresAt, now)}
}

// ReasonLabel is the short admin wording of a rejection (BR-22).
func ReasonLabel(r Reason) string {
	switch r {
	case ReasonInactive:
		return "inactive"
	case ReasonExpired:
		return "expired"
	case ReasonNoSubscription:
		return "window ended"
	case ReasonOutOfBulk:
		return "out of data"
	case ReasonOutOfTime:
		return "out of time"
	}
	return ""
}
