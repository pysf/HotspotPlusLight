package domain

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Errors shared by the services and the web layer.
var (
	ErrNotFound  = errors.New("not found")
	ErrNameTaken = errors.New("name already taken")
	ErrDefault   = errors.New("plan is the default plan")
)

// InUseError is returned when a plan cannot be deleted because members use it (O12).
// A batch row also holds a plan (even after all its members were deleted one by one).
type InUseError struct{ Members, Batches int }

func (e *InUseError) Error() string {
	return fmt.Sprintf("plan is used by %d members and %d batches", e.Members, e.Batches)
}

// FieldErrors maps a form field name to its message. Empty = valid.
type FieldErrors map[string]string

func (f FieldErrors) add(field, msg string) {
	if _, dup := f[field]; !dup {
		f[field] = msg
	}
}

// parser collects typed values and field errors for a form of raw strings.
type parser struct {
	errs FieldErrors
}

func (p *parser) integer(field, raw string, min, max int) int {
	raw = strings.TrimSpace(raw)
	n, err := strconv.Atoi(raw)
	switch {
	case raw == "":
		p.errs.add(field, "Required.")
	case err != nil:
		p.errs.add(field, "Enter a whole number.")
	case n < min || n > max:
		p.errs.add(field, fmt.Sprintf("Enter a whole number from %d to %d.", min, max))
	default:
		return n
	}
	return 0
}

// decimal parses a non-negative decimal up to max.
func (p *parser) decimal(field, raw string, max float64) float64 {
	raw = strings.TrimSpace(raw)
	v, err := strconv.ParseFloat(raw, 64)
	switch {
	case raw == "":
		p.errs.add(field, "Required (0 = no limit).")
	case err != nil || math.IsNaN(v) || math.IsInf(v, 0):
		p.errs.add(field, "Enter a number.")
	case v < 0 || v > max:
		p.errs.add(field, fmt.Sprintf("Enter a number from 0 to %g.", max))
	default:
		return v
	}
	return 0
}

// MaxBulkBytes caps a data amount entered in any unit (1 PiB), so the byte
// conversion and base+extra sums stay far from int64 overflow.
const MaxBulkBytes = int64(1) << 50

// bulkCap adds a field error when value in unit exceeds MaxBulkBytes.
func (p *parser) bulkCap(field string, value float64, unit string) {
	if b, err := BulkBytes(value, unit); err == nil && b > MaxBulkBytes {
		p.errs.add(field, fmt.Sprintf("Too large: the limit is %d GB.", MaxBulkBytes>>30))
	}
}

func (p *parser) oneOf(field, raw string, allowed []string) string {
	for _, a := range allowed {
		if raw == a {
			return raw
		}
	}
	p.errs.add(field, "Choose one of: "+strings.Join(allowed, ", ")+".")
	return ""
}

func (p *parser) text(field, raw string, required bool, maxLen int) string {
	raw = strings.TrimSpace(raw)
	switch {
	case required && raw == "":
		p.errs.add(field, "Required.")
	case len([]rune(raw)) > maxLen:
		p.errs.add(field, fmt.Sprintf("At most %d characters.", maxLen))
	}
	return raw
}

// line is like text, but it also rejects control characters (a newline too).
func (p *parser) line(field, raw string, required bool, maxLen int) string {
	raw = p.text(field, raw, required, maxLen)
	if strings.ContainsFunc(raw, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		p.errs.add(field, "Must not contain control characters or line breaks.")
	}
	return raw
}

// SecretMaxLen is the longest RADIUS shared secret.
const SecretMaxLen = 128

// SecretProblem returns why raw is not a valid RADIUS shared secret, or "" when it is.
// A secret is 1 to SecretMaxLen ASCII letters and digits; symbols and spaces break
// the pasted router script and some routers, so they are not allowed.
func SecretProblem(raw string) string {
	switch {
	case raw == "":
		return "Required."
	case len(raw) > SecretMaxLen:
		return fmt.Sprintf("At most %d characters.", SecretMaxLen)
	case strings.ContainsFunc(raw, func(r rune) bool { return !isASCIIAlnum(r) }):
		return "Use English letters and digits only. No spaces or symbols."
	}
	return ""
}

func isASCIIAlnum(r rune) bool {
	return r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
}

// secret checks a RADIUS shared secret and returns it unchanged. A secret equal to
// keep (the stored one) skips the letters-and-digits rule, so an older database
// with a symbol in its secret can still save its other settings.
func (p *parser) secret(field, raw, keep string) string {
	if keep != "" && raw == keep {
		switch {
		case strings.ContainsFunc(raw, func(r rune) bool { return r < 0x20 || r == 0x7f }):
			p.errs.add(field, "Must not contain control characters.")
		case len(raw) > SecretMaxLen:
			p.errs.add(field, fmt.Sprintf("At most %d characters.", SecretMaxLen))
		}
		return raw
	}
	if msg := SecretProblem(raw); msg != "" {
		p.errs.add(field, msg)
	}
	return raw
}

// Checked reports whether a checkbox value means "on".
func Checked(v string) bool {
	switch strings.ToLower(v) {
	case "on", "1", "true", "yes":
		return true
	}
	return false
}

func (p *parser) alnum(field, raw string, maxLen int) string {
	raw = strings.TrimSpace(raw)
	if !usernameRE.MatchString(raw) || len(raw) > maxLen {
		p.errs.add(field, fmt.Sprintf("1-%d English letters or digits.", maxLen))
	}
	return raw
}
