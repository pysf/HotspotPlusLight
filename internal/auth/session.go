package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// SessionTTL is the sliding idle lifetime of an admin session (PLAN 9.5).
const SessionTTL = time.Hour

var ErrInvalidCredentials = errors.New("invalid credentials")

// ThrottledError means too many failed logins; try again after RetryAfter.
type ThrottledError struct{ RetryAfter time.Duration }

func (e *ThrottledError) Error() string {
	return fmt.Sprintf("too many attempts, retry in %s", e.RetryAfter.Round(time.Second))
}

// AdminSession is what a successful login hands to the web layer.
type AdminSession struct {
	Token string // opaque cookie value; only its SHA-256 is stored
	CSRF  string
}

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Login checks the credentials (throttled per clientKey, e.g. the source IP)
// and opens a server-side session.
func (s *Service) Login(ctx context.Context, username, password, clientKey string) (AdminSession, error) {
	defer s.throttle.Lock(clientKey)() // one attempt at a time per client, or parallel guesses beat the limit
	now := s.clock.Now()
	if wait := s.throttle.RetryAfter(clientKey, now); wait > 0 {
		return AdminSession{}, &ThrottledError{RetryAfter: wait}
	}
	ok, err := s.VerifyAdmin(ctx, username, password)
	if err != nil {
		return AdminSession{}, err
	}
	if !ok {
		s.throttle.Failure(clientKey, now)
		return AdminSession{}, ErrInvalidCredentials
	}
	s.throttle.Success(clientKey)
	token, err := randomToken()
	if err != nil {
		return AdminSession{}, err
	}
	csrf, err := randomToken()
	if err != nil {
		return AdminSession{}, err
	}
	if err := s.repo.PurgeExpiredAdminSessions(ctx, now.Unix()); err != nil {
		return AdminSession{}, err
	}
	if err := s.repo.InsertAdminSession(ctx, hashToken(token), csrf, now.Unix(), now.Add(SessionTTL).Unix()); err != nil {
		return AdminSession{}, err
	}
	return AdminSession{Token: token, CSRF: csrf}, nil
}

// Authenticate resolves a cookie value to a live session and slides its expiry
// to now + 1 h. found is false for unknown, expired or logged-out ids.
// MONITOR: sessions are opaque and server-side; revoking = deleting the row.
func (s *Service) Authenticate(ctx context.Context, token string) (csrf string, found bool, err error) {
	if token == "" {
		return "", false, nil
	}
	now := s.clock.Now()
	return s.repo.TouchAdminSession(ctx, hashToken(token), now.Unix(), now.Add(SessionTTL).Unix())
}

// Logout deletes the session server-side.
func (s *Service) Logout(ctx context.Context, token string) error {
	return s.repo.DeleteAdminSession(ctx, hashToken(token))
}

// CSRFMatches compares tokens in constant time.
func CSRFMatches(want, got string) bool {
	return want != "" && subtle.ConstantTimeCompare([]byte(want), []byte(got)) == 1
}
