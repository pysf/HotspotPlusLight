// Package domain holds the pure business rules (BR-*). It imports only the
// standard library and performs no I/O and no clock reads.
package domain

import (
	"errors"
	"strings"
	"unicode"
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
}

// MinAdminPasswordLen is the minimum admin password length (BR-20).
const MinAdminPasswordLen = 16

// ValidateAdminPassword enforces BR-20: at least 16 characters, with an upper
// case letter, a lower case letter, a digit and a symbol, and not equal to the
// username.
func ValidateAdminPassword(password, username string) error {
	if len([]rune(password)) < MinAdminPasswordLen {
		return errors.New("password must be at least 16 characters")
	}
	var upper, lower, digit, symbol bool
	for _, r := range password {
		switch {
		case unicode.IsUpper(r):
			upper = true
		case unicode.IsLower(r):
			lower = true
		case unicode.IsDigit(r):
			digit = true
		default:
			symbol = true
		}
	}
	if !upper || !lower || !digit || !symbol {
		return errors.New("password must contain an upper case letter, a lower case letter, a digit and a symbol")
	}
	if strings.EqualFold(password, username) {
		return errors.New("password must not equal the username")
	}
	return nil
}
