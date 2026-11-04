package domain

import (
	"errors"
	"net/mail"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// ErrUsernameTaken is returned by the repository on a case-insensitive duplicate (BR-1).
var ErrUsernameTaken = errors.New("username already taken")

// Member mirrors the member table without the encrypted password.
type Member struct {
	ID               int64
	Username         string
	Active           bool
	FirstName        string
	LastName         string
	Mobile           string
	Email            string
	NationalCode     string
	MAC              string
	PlanID           int64
	SubscriptionDate time.Time
	ExtraBulkBytes   int64
	ExpiresAt        *time.Time
	BatchID          *int64
	UsedDownload     int64
	UsedUpload       int64
	UsedSeconds      int64
	LastRenewedAt    *time.Time
	RenewalCount     int
	LastRenewalKind  string // "", "auto", "manual"
	CreatedAt        time.Time
}

// State returns the part of the member that the login rules look at.
func (m Member) State() MemberState {
	return MemberState{
		Active: m.Active, SubscriptionDate: m.SubscriptionDate, ExtraBulkBytes: m.ExtraBulkBytes, ExpiresAt: m.ExpiresAt,
		UsedDownload: m.UsedDownload, UsedUpload: m.UsedUpload, UsedSeconds: m.UsedSeconds,
	}
}

// MemberInput is the editable part of a member after validation (the password is handled separately).
type MemberInput struct {
	Username     string
	Active       bool
	FirstName    string
	LastName     string
	Mobile       string
	Email        string
	NationalCode string
	MAC          string
	PlanID       int64
	ExpiresAt    *time.Time
}

// MemberForm is the raw text of the member form.
type MemberForm struct {
	Username, Password, FirstName, LastName, Mobile, Email, NationalCode string
	PlanID, MAC, ExpiresAt                                               string
	Active                                                               bool
}

// ExpiresLayout is the format of the expires_at form field (<input type=datetime-local>).
const ExpiresLayout = "2006-01-02T15:04"

// NewMemberForm is the form shown for a new member; defaultPlan preselects the plan (O10).
func NewMemberForm(defaultPlan *int64) MemberForm {
	f := MemberForm{Active: true}
	if defaultPlan != nil {
		f.PlanID = strconv.FormatInt(*defaultPlan, 10)
	}
	return f
}

// FormFromMember renders a stored member back into form text (dates in loc).
func FormFromMember(m Member, loc *time.Location) MemberForm {
	f := MemberForm{
		Username: m.Username, FirstName: m.FirstName, LastName: m.LastName, Mobile: m.Mobile, Email: m.Email,
		NationalCode: m.NationalCode, PlanID: strconv.FormatInt(m.PlanID, 10), MAC: m.MAC, Active: m.Active,
	}
	if m.ExpiresAt != nil {
		f.ExpiresAt = m.ExpiresAt.In(loc).Format(ExpiresLayout)
	}
	return f
}

func onlyASCIIDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// NormalizeMobile applies BR-2: keep digits, then the last 10; fewer than 10 is invalid.
// An empty input is valid and stays empty.
func NormalizeMobile(raw string) (string, bool) {
	if strings.TrimSpace(raw) == "" {
		return "", true
	}
	d := onlyASCIIDigits(raw)
	if len(d) < 10 {
		return "", false
	}
	return d[len(d)-10:], true
}

// NormalizeMAC applies BR-3: the RouterOS spelling, upper-case hex pairs joined by ':'
// ("AA:BB:CC:DD:EE:FF"). Separators ':', '-', '.' and spaces are stripped first; what remains
// must be exactly 12 hex digits. Empty is valid. This is the only MAC normalizer.
func NormalizeMAC(raw string) (string, bool) {
	s := strings.ToUpper(strings.NewReplacer("-", "", ":", "", ".", "", " ", "", "\t", "", "\r", "", "\n", "").Replace(raw))
	if s == "" {
		return "", true
	}
	if len(s) != 12 {
		return "", false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'A' && r <= 'F') {
			return "", false
		}
	}
	return s[0:2] + ":" + s[2:4] + ":" + s[4:6] + ":" + s[6:8] + ":" + s[8:10] + ":" + s[10:12], true
}

// MaxMemberPasswordLen bounds member passwords (RADIUS User-Password carries at most 128 bytes).
const MaxMemberPasswordLen = 64

// ValidateMemberPassword returns an error message, or "" when the password is acceptable.
// Member passwords are not subject to the admin password rule (BR-20).
func ValidateMemberPassword(pw string) string {
	switch {
	case strings.TrimSpace(pw) == "":
		return "Required."
	case len([]byte(pw)) > MaxMemberPasswordLen:
		return "At most 64 bytes."
	case !utf8.ValidString(pw) || strings.ContainsFunc(pw, func(r rune) bool { return r < 0x20 || r == 0x7f }):
		// The RADIUS edge drops such passwords as "wrong shared secret" (PLAN 7.1).
		return "No control characters."
	}
	return ""
}

// ParseMember validates the member form (BR-1, BR-2, BR-3, BR-14). create enables
// the "username defaults to the mobile number" rule. Plan existence is the
// service's concern. The password is not part of the input.
func ParseMember(f MemberForm, loc *time.Location, create bool) (MemberInput, FieldErrors) {
	p := &parser{errs: FieldErrors{}}
	in := MemberInput{
		Active:       f.Active,
		FirstName:    p.text("first_name", f.FirstName, false, 100),
		LastName:     p.text("last_name", f.LastName, false, 100),
		NationalCode: p.text("national_code", f.NationalCode, false, 50),
	}
	if mobile, ok := NormalizeMobile(f.Mobile); ok {
		in.Mobile = mobile
	} else {
		p.errs.add("mobile", "Enter at least 10 digits, or leave empty.")
	}
	username := strings.TrimSpace(f.Username)
	if username == "" && create {
		username = in.Mobile // BR-14: username defaults to the mobile number
	}
	if username == "" {
		p.errs.add("username", "Required (or enter a mobile number to use it as the username).")
	} else {
		in.Username = p.alnum("username", username, MaxUsernameLen)
	}
	if email := strings.TrimSpace(f.Email); email != "" {
		if a, err := mail.ParseAddress(email); err != nil || a.Address != email || len(email) > 254 {
			p.errs.add("email", "Enter a valid email address, or leave empty.")
		} else {
			in.Email = email
		}
	}
	if mac, ok := NormalizeMAC(f.MAC); ok {
		in.MAC = mac
	} else {
		p.errs.add("mac", "Enter a MAC address of 12 hex digits (separators allowed), or leave empty.")
	}
	if id, err := strconv.ParseInt(strings.TrimSpace(f.PlanID), 10, 64); err != nil || id < 1 {
		p.errs.add("plan_id", "Choose a plan.")
	} else {
		in.PlanID = id
	}
	if raw := strings.TrimSpace(f.ExpiresAt); raw != "" {
		if t, ok := parseLocal(raw, loc); ok {
			in.ExpiresAt = &t
		} else {
			p.errs.add("expires_at", "Enter a date and time like 2026-12-31T23:59, or leave empty.")
		}
	}
	return in, p.errs
}

func parseLocal(raw string, loc *time.Location) (time.Time, bool) {
	for _, layout := range []string{ExpiresLayout, "2006-01-02T15:04:05", "2006-01-02 15:04", "2006-01-02 15:04:05"} {
		if t, err := time.ParseInLocation(layout, raw, loc); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// Member list statuses and filters (E2.09, E2.14, BR-24).
const (
	StatusActive   = "active"
	StatusExpiring = "expiring"
	StatusInactive = "inactive" // shown as "Disabled"
	StatusExpired  = "expired"
)

// ExpiringSoon is how long before expires_at a member counts as "expiring soon" (BR-24).
const ExpiringSoon = 3 * 24 * time.Hour

// MemberStatus returns the status shown in the list (BR-24), with precedence
// expired, inactive, expiring, active. Expired means now is strictly after
// expires_at (O7); exactly ExpiringSoon left still counts as expiring. Times
// are compared in whole seconds, because store.statusClause must agree.
func MemberStatus(active bool, expiresAt *time.Time, now time.Time) string {
	n := now.Unix()
	switch {
	case expiresAt != nil && n > expiresAt.Unix():
		return StatusExpired
	case !active:
		return StatusInactive
	case expiresAt != nil && expiresAt.Unix() <= n+int64(ExpiringSoon/time.Second):
		return StatusExpiring
	}
	return StatusActive
}

// StatusCounts is how many members have each status (the chips of the member list).
type StatusCounts struct{ All, Active, Expiring, Expired, Inactive int }

// MemberFilter narrows the member list (and bulk selections). Zero value = everything.
type MemberFilter struct {
	Query  string
	PlanID int64  // 0 = any plan
	Status string // "" = any, else StatusActive | StatusExpiring | StatusInactive | StatusExpired
}

// MemberSelection is the set a bulk action applies to: explicit ids, or every
// member matching a filter (re-evaluated when the action runs, Q29).
type MemberSelection struct {
	IDs    []int64
	Filter *MemberFilter
	// BatchID selects every member of one batch; deleting such a selection also
	// removes the batch row in the same transaction (BR-23).
	BatchID *int64
}

// MemberRow is one list entry: the member, its plan and whether it is online.
type MemberRow struct {
	Member Member
	Plan   Plan
	Online bool
}

var PageSizes = []int{10, 20, 40, 60, 80, 100}

// DefaultPageSize is used for a missing or invalid per-page value.
const DefaultPageSize = 20

// NormalizePaging returns a valid page size and a page number clamped to [1, pages].
func NormalizePaging(page, per, total int) (int, int, int) {
	ok := false
	for _, s := range PageSizes {
		ok = ok || s == per
	}
	if !ok {
		per = DefaultPageSize
	}
	pages := (total + per - 1) / per
	if pages < 1 {
		pages = 1
	}
	if page < 1 {
		page = 1
	}
	if page > pages {
		page = pages
	}
	return page, per, pages
}

// ErrUnlimitedData is returned when extra bulk is set on a plan with unlimited data (Q17).
var ErrUnlimitedData = errors.New("this plan has unlimited data")

// ParseExtraBulk validates the extra-bulk form (decimal amount >= 0 in KB/MB/GB) and returns bytes (BR-4).
func ParseExtraBulk(value, unit string) (int64, FieldErrors) {
	p := &parser{errs: FieldErrors{}}
	v := p.decimal("value", value, 1e12)
	u := p.oneOf("unit", unit, BulkUnits)
	p.bulkCap("value", v, u)
	if len(p.errs) > 0 {
		return 0, p.errs
	}
	b, _ := BulkBytes(v, u)
	return b, nil
}
