package auth

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hotspotpluslight/internal/clock"
	"hotspotpluslight/internal/domain"
)

type sess struct {
	csrf    string
	expires int64
}

type fakeRepo struct {
	a        domain.Admin
	sessions map[string]*sess
}

func newRepo(a domain.Admin) *fakeRepo { return &fakeRepo{a: a, sessions: map[string]*sess{}} }

func (f *fakeRepo) GetAdmin(context.Context) (domain.Admin, error) { return f.a, nil }
func (f *fakeRepo) InsertAdminSession(_ context.Context, h, csrf string, _, exp int64) error {
	f.sessions[h] = &sess{csrf, exp}
	return nil
}
func (f *fakeRepo) TouchAdminSession(_ context.Context, h string, now, newExp int64) (string, bool, error) {
	s, ok := f.sessions[h]
	if !ok || s.expires <= now {
		return "", false, nil
	}
	s.expires = newExp
	return s.csrf, true, nil
}
func (f *fakeRepo) DeleteAdminSession(_ context.Context, h string) error {
	delete(f.sessions, h)
	return nil
}
func (f *fakeRepo) PurgeExpiredAdminSessions(_ context.Context, now int64) error {
	for h, s := range f.sessions {
		if s.expires <= now {
			delete(f.sessions, h)
		}
	}
	return nil
}
func (f *fakeRepo) SetAdminPassword(_ context.Context, hash string, _ int64) error {
	f.a.PasswordHash = hash
	f.sessions = map[string]*sess{}
	return nil
}

func (f *fakeRepo) ChangeAdminPassword(_ context.Context, hash string, _ int64, keep string) error {
	f.a.PasswordHash, f.a.MustChangePassword = hash, false
	for h := range f.sessions {
		if h != keep {
			delete(f.sessions, h)
		}
	}
	return nil
}

const goodPW = "Correct-Horse-9-Battery"

func newSvc(t *testing.T) (*Service, *fakeRepo, *clock.Fake) {
	h, err := HashPassword(goodPW)
	if err != nil {
		t.Fatal(err)
	}
	r := newRepo(domain.Admin{Username: "admin", PasswordHash: h})
	fc := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	return New(r, fc), r, fc
}

func TestGeneratedPasswordAlwaysSatisfiesBR20(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 300; i++ {
		p, err := GeneratePassword(20)
		if err != nil || len(p) != 20 {
			t.Fatalf("%q %v", p, err)
		}
		if err := domain.ValidateAdminPassword(p, "admin"); err != nil {
			t.Fatalf("%q: %v", p, err)
		}
		seen[p] = true
	}
	if len(seen) < 300 {
		t.Fatal("passwords repeat")
	}
}

func TestVerifyAdmin(t *testing.T) {
	s, _, _ := newSvc(t)
	ctx := context.Background()
	for _, c := range []struct {
		u, p string
		ok   bool
	}{{"admin", goodPW, true}, {"admin", "nope", false}, {"root", goodPW, false}} {
		if ok, _ := s.VerifyAdmin(ctx, c.u, c.p); ok != c.ok {
			t.Errorf("%v: got %v", c, ok)
		}
	}
}

func TestSessionSlidingAndLogout(t *testing.T) {
	s, r, fc := newSvc(t)
	ctx := context.Background()
	sn, err := s.Login(ctx, "admin", goodPW, "ip")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := r.sessions[sn.Token]; ok {
		t.Fatal("token stored in clear")
	}
	if _, ok := r.sessions[hashToken(sn.Token)]; !ok {
		t.Fatal("hash not stored")
	}
	fc.Advance(59 * time.Minute)
	if csrf, ok, _ := s.Authenticate(ctx, sn.Token); !ok || csrf != sn.CSRF {
		t.Fatal("live session rejected")
	}
	fc.Advance(59 * time.Minute)
	if _, ok, _ := s.Authenticate(ctx, sn.Token); !ok {
		t.Fatal("sliding window did not extend")
	}
	fc.Advance(61 * time.Minute)
	if _, ok, _ := s.Authenticate(ctx, sn.Token); ok {
		t.Fatal("expired session accepted")
	}
	sn, _ = s.Login(ctx, "admin", goodPW, "ip")
	s.Logout(ctx, sn.Token)
	if _, ok, _ := s.Authenticate(ctx, sn.Token); ok {
		t.Fatal("logged-out session accepted")
	}
	if _, ok, _ := s.Authenticate(ctx, ""); ok {
		t.Fatal("empty token accepted")
	}
}

func TestThrottle(t *testing.T) {
	s, _, fc := newSvc(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if _, err := s.Login(ctx, "admin", "bad", "a"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("%d: %v", i, err)
		}
	}
	var th *ThrottledError
	if _, err := s.Login(ctx, "admin", goodPW, "a"); !errors.As(err, &th) || th.RetryAfter <= 0 {
		t.Fatalf("not throttled: %v", err)
	}
	if _, err := s.Login(ctx, "admin", goodPW, "b"); err != nil {
		t.Fatalf("other client throttled: %v", err)
	}
	fc.Advance(61 * time.Second)
	if _, err := s.Login(ctx, "admin", goodPW, "a"); err != nil {
		t.Fatalf("after lockout: %v", err)
	}
	// Old failures expire: 4 failures, a minute, 4 more must not lock.
	for i := 0; i < 4; i++ {
		s.Login(ctx, "admin", "bad", "c")
	}
	fc.Advance(2 * time.Minute)
	for i := 0; i < 4; i++ {
		s.Login(ctx, "admin", "bad", "c")
	}
	if _, err := s.Login(ctx, "admin", goodPW, "c"); err != nil {
		t.Fatalf("stale failures counted: %v", err)
	}
}

func TestSetAdminPassword(t *testing.T) {
	s, r, _ := newSvc(t)
	ctx := context.Background()
	s.Login(ctx, "admin", goodPW, "ip")
	if err := s.SetAdminPassword(ctx, "short"); err == nil {
		t.Fatal("weak password accepted")
	}
	if len(r.sessions) != 1 {
		t.Fatal("rejected password touched sessions")
	}
	if err := s.SetAdminPassword(ctx, "Another-Passw0rd-OK!"); err != nil {
		t.Fatal(err)
	}
	if len(r.sessions) != 0 {
		t.Fatal("sessions survive")
	}
	if ok, _ := s.VerifyAdmin(ctx, "admin", "Another-Passw0rd-OK!"); !ok {
		t.Fatal("new password does not verify")
	}
}

func TestChangePassword(t *testing.T) {
	s, r, _ := newSvc(t)
	ctx := context.Background()
	r.a.MustChangePassword = true
	keep, _ := s.Login(ctx, "admin", goodPW, "ip")
	s.Login(ctx, "admin", goodPW, "ip2") // a second session
	if len(r.sessions) != 2 {
		t.Fatalf("sessions %d", len(r.sessions))
	}
	if errs, _ := s.ChangePassword(ctx, keep.Token, "ip", "wrong", "Fresh-Pass-12345", "Fresh-Pass-12345"); errs["current_password"] == "" {
		t.Fatal("wrong current accepted")
	}
	if errs, _ := s.ChangePassword(ctx, keep.Token, "ip", goodPW, "short", "short"); errs["new_password"] == "" {
		t.Fatal("short password accepted")
	}
	if errs, _ := s.ChangePassword(ctx, keep.Token, "ip", goodPW, "Fresh-Pass-12345", "different-one-1"); errs["confirm_password"] == "" {
		t.Fatal("mismatch accepted")
	}
	if !r.a.MustChangePassword || len(r.sessions) != 2 {
		t.Fatal("a rejected change touched the state")
	}
	if errs, err := s.ChangePassword(ctx, keep.Token, "ip", goodPW, "Fresh-Pass-12345", "Fresh-Pass-12345"); err != nil || len(errs) > 0 {
		t.Fatalf("%v %v", errs, err)
	}
	if r.a.MustChangePassword || len(r.sessions) != 1 {
		t.Fatalf("flag %v sessions %d", r.a.MustChangePassword, len(r.sessions))
	}
	if _, ok, _ := s.Authenticate(ctx, keep.Token); !ok {
		t.Fatal("current session was ended")
	}
}

// A right current password with an invalid new one must not reset the failure counter.
func TestChangePasswordInvalidNewKeepsCounter(t *testing.T) {
	s, _, _ := newSvc(t)
	ctx := context.Background()
	for i := 0; i < loginMaxFailures-1; i++ {
		s.ChangePassword(ctx, "t", "ip", "wrong", "Fresh-Pass-12345", "Fresh-Pass-12345")
	}
	if errs, _ := s.ChangePassword(ctx, "t", "ip", goodPW, "short", "short"); errs["new_password"] == "" {
		t.Fatal("short password accepted")
	}
	s.ChangePassword(ctx, "t", "ip", "wrong", "Fresh-Pass-12345", "Fresh-Pass-12345") // the failure that locks
	_, err := s.ChangePassword(ctx, "t", "ip", goodPW, "Fresh-Pass-12345", "Fresh-Pass-12345")
	var th *ThrottledError
	if !errors.As(err, &th) {
		t.Fatalf("counter was reset, want throttled, got %v", err)
	}
}

func TestChangePasswordWrongCurrentIsThrottled(t *testing.T) {
	s, _, _ := newSvc(t)
	ctx := context.Background()
	for i := 0; i < loginMaxFailures; i++ {
		s.ChangePassword(ctx, "t", "ip", "wrong", "Fresh-Pass-12345", "Fresh-Pass-12345")
	}
	_, err := s.ChangePassword(ctx, "t", "ip", goodPW, "Fresh-Pass-12345", "Fresh-Pass-12345")
	var th *ThrottledError
	if !errors.As(err, &th) {
		t.Fatalf("want throttled, got %v", err)
	}
}

type countingRepo struct {
	*fakeRepo
	gets atomic.Int32
}

func (c *countingRepo) GetAdmin(ctx context.Context) (domain.Admin, error) {
	c.gets.Add(1)
	return c.fakeRepo.GetAdmin(ctx)
}

// Parallel wrong guesses from one IP must not exceed the failure limit.
func TestThrottleConcurrentAttempts(t *testing.T) {
	h, err := HashPassword(goodPW)
	if err != nil {
		t.Fatal(err)
	}
	repo := &countingRepo{fakeRepo: newRepo(domain.Admin{Username: "admin", PasswordHash: h})}
	s := New(repo, clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)))
	const n = 30
	var wg sync.WaitGroup
	var throttled atomic.Int32
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Login(context.Background(), "admin", "bad", "1.2.3.4")
			var th *ThrottledError
			if errors.As(err, &th) {
				throttled.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := repo.gets.Load(); got != loginMaxFailures {
		t.Fatalf("%d password verifications, want %d", got, loginMaxFailures)
	}
	if got := throttled.Load(); got != n-loginMaxFailures {
		t.Fatalf("%d throttled, want %d", got, n-loginMaxFailures)
	}
}

func TestGeneratedSecretIsLettersAndDigits(t *testing.T) {
	for i := 0; i < 100; i++ {
		v, err := GenerateSecret(24)
		if err != nil || len(v) != 24 || domain.SecretProblem(v) != "" {
			t.Fatalf("%q %v", v, err)
		}
	}
}
