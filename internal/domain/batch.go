package domain

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Batch is a group of vouchers created together (BR-15). Count is the original
// size; members edited or removed individually do not change it.
type Batch struct {
	ID           int64
	Label        string
	PlanID       int64
	Count        int
	DurationDays int
	ExpiresAt    time.Time
	CreatedAt    time.Time
}

// BatchRow is one entry of the batch list: the batch, its plan and live counts.
type BatchRow struct {
	Batch    Batch
	PlanName string
	Members  int // members still in the batch
	Unused   int // members with all usage counters 0 and no session row
	Online   int // members online now (BR-17)
}

// BatchForm is the raw text of the new-batch form.
type BatchForm struct{ Label, PlanID, Count, DurationDays string }

// BatchInput is a validated new batch. DurationDays 0 means "the plan's validity".
type BatchInput struct {
	Label        string
	PlanID       int64
	Count        int
	DurationDays int
}

// MaxLabelLen is the longest batch label.
const MaxLabelLen = 100

// NewBatchForm preselects the default plan like the new-member form does (O10).
func NewBatchForm(defaultPlan *int64) BatchForm {
	f := BatchForm{}
	if defaultPlan != nil {
		f.PlanID = strconv.FormatInt(*defaultPlan, 10)
	}
	return f
}

// ParseBatchLabel trims and validates a batch label (create and rename).
func ParseBatchLabel(raw string) (string, FieldErrors) {
	p := &parser{errs: FieldErrors{}}
	label := p.text("label", raw, false, MaxLabelLen)
	return label, p.errs
}

// ParseBatchForm validates the form; maxCount is settings.batch_max_count.
// Whether the plan exists is checked by the service.
func ParseBatchForm(f BatchForm, maxCount int) (BatchInput, FieldErrors) {
	label, errs := ParseBatchLabel(f.Label)
	p := &parser{errs: errs}
	in := BatchInput{Label: label}
	if id, err := strconv.ParseInt(strings.TrimSpace(f.PlanID), 10, 64); err != nil || id < 1 {
		p.errs.add("plan_id", "Choose a plan from the list.")
	} else {
		in.PlanID = id
	}
	in.Count = p.integer("count", f.Count, 1, maxCount)
	if strings.TrimSpace(f.DurationDays) != "" {
		in.DurationDays = p.integer("duration_days", f.DurationDays, 1, 3650)
	}
	return in, p.errs
}

// BatchExpiry is the batch expiry: local midnight of (today + days + 1), in loc
// (BR-15, O14). The date is the local calendar date of now, so 21:00 UTC is
// already tomorrow in Tehran.
func BatchExpiry(now time.Time, loc *time.Location, days int) time.Time {
	y, m, d := now.In(loc).Date()
	t := time.Date(y, m, d+days+1, 0, 0, 0, 0, loc)
	// When a DST jump skips local midnight, time.Date may answer with an instant on
	// the previous day; step forward to the first valid instant of the target day.
	target := time.Date(y, m, d+days+1, 12, 0, 0, 0, loc) // noon always exists
	for t.Before(target) && t.In(loc).Day() != target.Day() {
		t = t.Add(15 * time.Minute)
	}
	return t
}

// ErrPlanGone: the batch's plan no longer exists (deleted while the form was submitted).
var ErrPlanGone = errors.New("plan no longer exists")

// BatchUsername is the username of the n-th voucher.
func BatchUsername(prefix string, n int) string { return prefix + strconv.Itoa(n) }

// UsernameConflictError says which generated username already exists; it
// matches ErrUsernameTaken with errors.Is.
type UsernameConflictError struct{ Username string }

func (e *UsernameConflictError) Error() string {
	return fmt.Sprintf("username %s is already taken", e.Username)
}

func (e *UsernameConflictError) Is(target error) bool { return target == ErrUsernameTaken }

// Voucher is what one printed voucher shows (BR-16).
type Voucher struct{ Username, Password string }

// VouchersPerPage is 14 on an A4 sheet: 2 columns of 7 (BR-16).
const VouchersPerPage = 14

// DefaultVoucherHelp is printed when settings.voucher_help is empty (O21).
const DefaultVoucherHelp = "To use the internet, connect to the Wi-Fi network, open any web page and sign in with the username and password below."

// VoucherHelp is the help text to print for the setting value.
func VoucherHelp(setting string) string {
	if strings.TrimSpace(setting) == "" {
		return DefaultVoucherHelp
	}
	return strings.TrimSpace(setting)
}

// VoucherPages splits vouchers into A4 pages of VouchersPerPage.
func VoucherPages(vs []Voucher) [][]Voucher {
	var pages [][]Voucher
	for len(vs) > 0 {
		n := min(VouchersPerPage, len(vs))
		pages = append(pages, vs[:n])
		vs = vs[n:]
	}
	return pages
}

// NewBatch is everything the repository stores for a new batch; the usernames
// are not part of it because they come from the persisted counter inside the
// transaction.
type NewBatch struct {
	Input        BatchInput
	DurationDays int // resolved: the plan's validity when the form left it empty
	ExpiresAt    time.Time
	PasswordsEnc [][]byte // one per member, len = Input.Count
}

// BatchCredential is a batch member's username and encrypted password.
type BatchCredential struct {
	Username    string
	PasswordEnc []byte
}
