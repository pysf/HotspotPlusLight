// Package auth holds the login checks: admin login, server-side admin sessions
// and password helpers (Phase 1); the member Evaluator (PLAN 7.2) arrives in Phase 3.
package auth

import (
	"context"
	"crypto/rand"
	"math/big"

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
}

type Service struct {
	repo     Repo
	clock    clock.Clock
	throttle *throttle
}

func New(repo Repo, c clock.Clock) *Service {
	return &Service{repo: repo, clock: c, throttle: newThrottle(loginMaxFailures, loginLockout)}
}

// VerifyAdmin reports whether username/password match the admin account.
func (s *Service) VerifyAdmin(ctx context.Context, username, password string) (bool, error) {
	a, err := s.repo.GetAdmin(ctx)
	if err != nil {
		return false, err
	}
	// Always run bcrypt so a wrong username costs the same as a wrong password.
	okPW := bcrypt.CompareHashAndPassword([]byte(a.PasswordHash), []byte(password)) == nil
	return okPW && a.Username == username, nil
}

// SetAdminPassword enforces BR-20, stores the bcrypt hash and ends every admin
// session (PLAN 9.5). It is the only way the password changes (CLI only).
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
