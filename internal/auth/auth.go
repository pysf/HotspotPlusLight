// Package auth holds the login checks. Phase 0: admin credential helpers only;
// the member Evaluator (PLAN 7.2) arrives in Phase 3.
package auth

import (
	"context"
	"crypto/rand"
	"math/big"

	"golang.org/x/crypto/bcrypt"

	"hotspotpluslight/internal/domain"
)

// Repo is what auth needs from storage (declared here, implemented by store).
type Repo interface {
	GetAdmin(ctx context.Context) (domain.Admin, error)
}

type Service struct{ repo Repo }

func New(repo Repo) *Service { return &Service{repo: repo} }

// VerifyAdmin reports whether username/password match the admin account.
func (s *Service) VerifyAdmin(ctx context.Context, username, password string) (bool, error) {
	a, err := s.repo.GetAdmin(ctx)
	if err != nil {
		return false, err
	}
	if a.Username != username {
		return false, nil
	}
	return bcrypt.CompareHashAndPassword([]byte(a.PasswordHash), []byte(password)) == nil, nil
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
