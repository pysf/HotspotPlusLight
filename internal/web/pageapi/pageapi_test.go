package pageapi

import (
	"context"
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hotspotpluslight/internal/auth"
	"hotspotpluslight/internal/clock"
	"hotspotpluslight/internal/domain"
)

type fakeAccounts struct{}

func (fakeAccounts) Account(_ context.Context, u, p string) (auth.Account, error) {
	if u == "alice" && p == "pw" {
		return auth.Account{Plan: &domain.Plan{Name: "P", DurationDays: 1}, Quota: domain.Quota{DataUnlimited: true, TimeUnlimited: true}}, nil
	}
	return auth.Account{}, auth.ErrInvalidCredentials
}
func (fakeAccounts) CredentialsByMAC(context.Context, string) (string, string, bool, error) {
	return "", "", false, nil
}

type fakeSettings struct{}

func (fakeSettings) Get(context.Context) (domain.Settings, error) { return domain.Settings{}, nil }

func do(h http.Handler, remote, user, pw string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", "/api/v1/account", nil)
	req.RemoteAddr = remote
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(user+":"+pw)))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// The throttle is per source IP; the port of the connection does not matter.
func TestThrottlePerSourceIP(t *testing.T) {
	fc := clock.NewFake(time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC))
	h := Handler(Deps{Accounts: fakeAccounts{}, Settings: fakeSettings{}, Clock: fc, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	for i := 0; i < maxFailures; i++ {
		if got := do(h, "10.0.0.1:1000"+string(rune('0'+i)), "alice", "bad").Code; got != 401 {
			t.Fatalf("failure %d: %d", i, got)
		}
	}
	if got := do(h, "10.0.0.1:55555", "alice", "pw").Code; got != 429 {
		t.Fatalf("locked source: %d", got)
	}
	if got := do(h, "10.0.0.2:55555", "alice", "pw").Code; got != 200 {
		t.Fatalf("other source: %d", got)
	}
	fc.Advance(failureWindow + time.Second)
	if got := do(h, "10.0.0.1:55555", "alice", "pw").Code; got != 200 {
		t.Fatalf("after the lock: %d", got)
	}
}

func TestBasicCredentials(t *testing.T) {
	cases := []struct {
		header, user, pass string
		ok                 bool
	}{
		{"Basic " + base64.StdEncoding.EncodeToString([]byte("a:b:c")), "a", "b:c", true}, // the password may contain colons
		{"basic " + base64.StdEncoding.EncodeToString([]byte("a:")), "a", "", true},
		{"Basic " + base64.StdEncoding.EncodeToString([]byte("nocolon")), "", "", false},
		{"Basic ###", "", "", false},
		{"Bearer x", "", "", false},
		{"", "", "", false},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		if c.header != "" {
			r.Header.Set("Authorization", c.header)
		}
		u, p, ok := basicCredentials(r)
		if ok != c.ok || (ok && (u != c.user || p != c.pass)) {
			t.Errorf("%q: got %q %q %v", c.header, u, p, ok)
		}
	}
}

// Parallel wrong passwords from one IP: exactly maxFailures get through, the rest are 429.
func TestThrottleIsAtomic(t *testing.T) {
	fc := clock.NewFake(time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC))
	h := Handler(Deps{Accounts: fakeAccounts{}, Settings: fakeSettings{}, Clock: fc, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	var n401, n429 atomic.Int32
	var wg sync.WaitGroup
	for range 30 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			switch do(h, "10.0.0.9:4000", "alice", "bad").Code {
			case 401:
				n401.Add(1)
			case 429:
				n429.Add(1)
			}
		}()
	}
	wg.Wait()
	if n401.Load() != maxFailures || n429.Load() != 30-maxFailures {
		t.Fatalf("401=%d 429=%d", n401.Load(), n429.Load())
	}
}

// Unknown-MAC lookups are normal (new devices) and never lock anybody out.
func TestUnknownMACDoesNotThrottle(t *testing.T) {
	fc := clock.NewFake(time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC))
	h := Handler(Deps{Accounts: fakeAccounts{}, Settings: macOn{}, Clock: fc, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	for i := range 3 * maxFailures {
		req := httptest.NewRequest("GET", "/api/v1/devices/aabbccddeeff/credentials", nil)
		req.RemoteAddr = "10.0.0.1:5000"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 404 {
			t.Fatalf("lookup %d: %d", i, rec.Code)
		}
	}
	if got := do(h, "10.0.0.1:5000", "alice", "pw").Code; got != 200 {
		t.Fatalf("account after unknown lookups: %d", got)
	}
}

type macOn struct{}

func (macOn) Get(context.Context) (domain.Settings, error) {
	return domain.Settings{MacAutoLogin: true}, nil
}
