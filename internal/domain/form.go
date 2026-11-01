package domain

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"regexp"
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
type InUseError struct{ Members int }

func (e *InUseError) Error() string { return fmt.Sprintf("plan is used by %d members", e.Members) }

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

// secret keeps the value verbatim (spaces can be part of a shared secret) and
// only rejects empty or whitespace-only input.
func (p *parser) secret(field, raw string, maxLen int) string {
	switch {
	case strings.TrimSpace(raw) == "":
		p.errs.add(field, "Required.")
	case len([]rune(raw)) > maxLen:
		p.errs.add(field, fmt.Sprintf("At most %d characters.", maxLen))
	}
	return raw
}

func (p *parser) httpURL(field, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		p.errs.add(field, "Enter a full http:// or https:// address, or leave empty.")
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

var alnum = regexp.MustCompile(`^[A-Za-z0-9]+$`)

func (p *parser) alnum(field, raw string, maxLen int) string {
	raw = strings.TrimSpace(raw)
	if !alnum.MatchString(raw) || len(raw) > maxLen {
		p.errs.add(field, fmt.Sprintf("1-%d English letters or digits.", maxLen))
	}
	return raw
}
