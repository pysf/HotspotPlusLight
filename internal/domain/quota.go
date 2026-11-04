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
	ReasonInvalidCredentials Reason = "INVALID_CREDENTIALS"
	ReasonInactive           Reason = "INACTIVE_USER"
	ReasonNoPlan             Reason = "NO_PLAN"
	ReasonPlanRemoved        Reason = "PLAN_REMOVED"
	ReasonExpired            Reason = "EXPIRED"
	ReasonNoSubscription     Reason = "NO_ACTIVE_SUBSCRIPTION"
	ReasonOutOfBulk          Reason = "OUT_OF_BULK"
	ReasonOutOfTime          Reason = "OUT_OF_TIME"
	ReasonNoMoreSession      Reason = "NO_MORE_SESSION"
	ReasonUnsupportedAuth    Reason = "UNSUPPORTED_AUTH_METHOD"
)

// AllReasons lists every code of the message catalog, in catalog order. The
// starter login page's translation dictionary is generated from it (PLAN 9.4).
func AllReasons() []Reason {
	return []Reason{ReasonInvalidCredentials, ReasonInactive, ReasonNoPlan, ReasonPlanRemoved, ReasonExpired, ReasonNoSubscription,
		ReasonOutOfBulk, ReasonOutOfTime, ReasonNoMoreSession, ReasonUnsupportedAuth}
}

// reasonMessages is the English message catalog (PLAN 7.6, O11, O20).
var reasonMessages = map[Reason]string{
	ReasonInvalidCredentials: "Wrong username or password.",
	ReasonInactive:           "This account is disabled.",
	ReasonNoPlan:             "No internet plan is assigned to this account.",
	ReasonPlanRemoved:        "This plan has been removed.",
	ReasonExpired:            "This account has expired.",
	ReasonNoSubscription:     "Your plan period has ended.",
	ReasonOutOfBulk:          "Your data allowance is used up.",
	ReasonOutOfTime:          "Your time allowance is used up.",
	ReasonNoMoreSession:      "Someone else is using this account. Try again in a few minutes.",
	ReasonUnsupportedAuth:    "This network only supports PAP login.",
}

// Message returns the English text of a rejection code, or "" for the empty Reason.
func (r Reason) Message() string { return reasonMessages[r] }

// ReplyMessage returns the Reply-Message of a rejection: the bare code (O20).
// The router page maps the code to text.
func (r Reason) ReplyMessage() string { return string(r) }

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

// QuotaOf returns the quota of member state m on plan p. The login check and
// the admin summary both use it (BR-22).
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

// Evaluate applies checks 2 and 5-8 of PLAN 7.2 in order: active, expires_at
// (O7), validity window with auto-resubscribe, data, time. The caller does
// checks 1, 3-4 and 9, and performs any renewal that Decision.Renew asks for.
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

// ShouldEnd is layer 2 of the enforcement (O3/O4). It reports whether an online
// member must be disconnected now, and why: Evaluate rejects, or the window of
// an auto-renew plan has ended (the next login renews, BR-6).
func ShouldEnd(m MemberState, p Plan, now time.Time) (Reason, bool) {
	d := Evaluate(m, p, now)
	if d.Renew {
		return ReasonNoSubscription, true
	}
	return d.Reason, d.Reason != ""
}

// RenewedState returns the member state after a renewal at now (BR-13b).
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
	Status   string // active | expiring | inactive | expired (MemberStatus)
}

// Summarize returns the admin summary, using the same functions as the login.
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
