// Package auth holds the login checks: the admin login with server-side
// sessions and password helpers, and the member login Evaluator (PLAN 7.2).
package auth

import (
	"context"
	"crypto/rand"
	"math/big"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"hotspotpluslight/internal/clock"
	"hotspotpluslight/internal/domain"
)

// Repo is what auth needs from storage (declared here, implemented by store).
type Repo interface {
	// GetAdmin returns domain.ErrNotFound when the admin has not been seeded.
	GetAdmin(ctx context.Context) (domain.Admin, error)
	InsertAdminSession(ctx context.Context, tokenHash, csrf string, createdAt, expiresAt int64) error
	// TouchAdminSession extends a live (unexpired) session and returns its CSRF token.
	TouchAdminSession(ctx context.Context, tokenHash string, now, newExpiresAt int64) (csrf string, found bool, err error)
	DeleteAdminSession(ctx context.Context, tokenHash string) error
	PurgeExpiredAdminSessions(ctx context.Context, now int64) error
	// SetAdminPassword stores the hash and deletes every admin session, atomically.
	SetAdminPassword(ctx context.Context, hash string, now int64) error
	// ChangeAdminPassword stores the hash, clears the must-change flag and deletes every
	// admin session except keepTokenHash, atomically.
	ChangeAdminPassword(ctx context.Context, hash string, now int64, keepTokenHash string) error
}

// Service is the admin login, session and password service.
type Service struct {
	repo     Repo
	clock    clock.Clock
	throttle *Throttle
}

// New returns a Service that stores its data in repo.
func New(repo Repo, c clock.Clock) *Service {
	return &Service{repo: repo, clock: c, throttle: NewThrottle(loginMaxFailures, loginLockout)}
}

// VerifyAdmin reports whether username and password match the admin account.
func (s *Service) VerifyAdmin(ctx context.Context, username, password string) (bool, error) {
	a, err := s.repo.GetAdmin(ctx)
	if err != nil {
		return false, err
	}
	// Always run bcrypt so a wrong username costs the same as a wrong password.
	okPW := bcrypt.CompareHashAndPassword([]byte(a.PasswordHash), []byte(password)) == nil
	return okPW && a.Username == username, nil
}

// SetAdminPassword checks the password against BR-20, stores its bcrypt hash and
// ends every admin session (PLAN 9.5). It is the CLI path; the web page uses
// ChangePassword.
func (s *Service) SetAdminPassword(ctx context.Context, password string) error {
	a, err := s.repo.GetAdmin(ctx)
	if err != nil {
		return err
	}
	if err := domain.ValidateAdminPassword(password, a.Username); err != nil {
		return err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	return s.repo.SetAdminPassword(ctx, hash, s.clock.Now().Unix())
}

// MustChangePassword reports whether the admin still has to change the first-start password (admin/admin).
func (s *Service) MustChangePassword(ctx context.Context) (bool, error) {
	a, err := s.repo.GetAdmin(ctx)
	return a.MustChangePassword, err
}

// ChangePassword changes the admin password from the web page (BR-20) and
// ends every admin session except the one for token. A wrong current password
// counts in the login throttle of clientKey. It also clears the must-change
// flag. Wrong input is returned in FieldErrors; a non-empty FieldErrors means
// nothing was changed.
func (s *Service) ChangePassword(ctx context.Context, token, clientKey, current, next, confirm string) (domain.FieldErrors, error) {
	defer s.throttle.Lock(clientKey)()
	now := s.clock.Now()
	if wait := s.throttle.RetryAfter(clientKey, now); wait > 0 {
		return nil, &ThrottledError{RetryAfter: wait}
	}
	a, err := s.repo.GetAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if bcrypt.CompareHashAndPassword([]byte(a.PasswordHash), []byte(current)) != nil {
		s.throttle.Failure(clientKey, now)
		return domain.FieldErrors{"current_password": "Current password is wrong."}, nil
	}
	errs := domain.FieldErrors{}
	if err := domain.ValidateAdminPassword(next, a.Username); err != nil {
		errs["new_password"] = capitalize(err.Error()) + "."
	}
	if next != confirm {
		errs["confirm_password"] = "The two passwords do not match."
	}
	if len(errs) > 0 {
		return errs, nil
	}
	hash, err := HashPassword(next)
	if err != nil {
		return nil, err
	}
	if err := s.repo.ChangeAdminPassword(ctx, hash, now.Unix(), hashToken(token)); err != nil {
		return nil, err
	}
	// Only a finished change clears the counter, or guessing the new password would reset it.
	s.throttle.Success(clientKey)
	return nil, nil
}

func capitalize(s string) string { return strings.ToUpper(s[:1]) + s[1:] }

// HashPassword returns the bcrypt hash stored in admin.password_hash.
func HashPassword(password string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(h), err
}

const (
	upper   = "ABCDEFGHJKLMNPQRSTUVWXYZ"
	lower   = "abcdefghijkmnopqrstuvwxyz"
	digits  = "23456789"
	symbols = "!@#$%^&*-_=+?"
)

// GeneratePassword returns n random characters (crypto/rand) that always
// satisfy BR-20: at least one upper, lower, digit and symbol.
func GeneratePassword(n int) (string, error) {
	groups := []string{upper, lower, digits, symbols}
	all := upper + lower + digits + symbols
	out := make([]byte, 0, n)
	for _, g := range groups {
		c, err := pick(g)
		if err != nil {
			return "", err
		}
		out = append(out, c)
	}
	for len(out) < n {
		c, err := pick(all)
		if err != nil {
			return "", err
		}
		out = append(out, c)
	}
	for i := len(out) - 1; i > 0; i-- { // Fisher-Yates
		j, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return "", err
		}
		out[i], out[j.Int64()] = out[j.Int64()], out[i]
	}
	return string(out), nil
}

func pick(set string) (byte, error) {
	i, err := rand.Int(rand.Reader, big.NewInt(int64(len(set))))
	if err != nil {
		return 0, err
	}
	return set[i.Int64()], nil
}
