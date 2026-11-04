// Package domain holds the pure business rules (BR-*). It imports only the
// standard library and performs no I/O and no clock reads.
package domain

import (
	"errors"
	"strings"
	"unicode/utf8"
)

// Plan mirrors the plan table (PLAN.md section 6).
type Plan struct {
	ID               int64
	Name             string
	DurationDays     int
	SpeedValue       float64
	SpeedUnit        string // Kbps | Mbps | Gbps
	BulkValue        float64
	BulkUnit         string // KB | MB | GB
	TimeLimitMinutes int
	MaxSessions      int
	AutoResubscribe  bool
}

// Admin is the single admin account.
type Admin struct {
	Username     string
	PasswordHash string // bcrypt
	// MustChangePassword is true after the first start (account admin/admin) until the admin sets a new password.
	MustChangePassword bool
}

// Admin password limits (BR-20).
const (
	MinAdminPasswordLen   = 10 // characters
	MaxAdminPasswordBytes = 72 // the bcrypt limit
)

// ValidateAdminPassword enforces BR-20: at least 10 characters, at most 72 bytes,
// not the username, not "admin", not a common password. No character-class rules.
func ValidateAdminPassword(password, username string) error {
	if utf8.RuneCountInString(password) < MinAdminPasswordLen {
		return errors.New("password must be at least 10 characters")
	}
	if len(password) > MaxAdminPasswordBytes {
		return errors.New("password must be at most 72 bytes")
	}
	if strings.EqualFold(password, username) {
		return errors.New("password must not equal the username")
	}
	if strings.EqualFold(password, "admin") {
		return errors.New(`password must not be "admin"`)
	}
	if _, bad := commonPasswords[strings.ToLower(password)]; bad {
		return errors.New("password is too common")
	}
	return nil
}
