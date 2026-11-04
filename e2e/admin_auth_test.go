package e2e

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"hotspotpluslight/e2e/harness"
)

// rawGet requests with an explicit cookie and no jar (replay of a stolen/old id).
func rawGet(t *testing.T, s *harness.Server, cookie, path string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("GET", "http://"+s.Addrs.Admin+path, nil)
	req.AddCookie(&http.Cookie{Name: "hsp_admin", Value: cookie})
	c := http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp
}

// E1.01 admin login ok/fail (O15). A fresh install logs in with admin/admin and lands on the
// password page; after the change the landing page is the members page (Phase 11b).
func TestE1_01_AdminLogin(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	const pw = "admin"

	c := harness.NewAdminClient(t, s)
	page := c.Get("/admin/login")
	if page.StatusCode != 200 || page.Doc(t).Find(`input[name="username"]`).Length() != 1 || page.Doc(t).Find(`input[name="password"][type="password"]`).Length() != 1 {
		t.Fatalf("login page: %d", page.StatusCode)
	}
	bad1 := c.PostLogin("admin", "wrong-password-123!A")
	bad2 := c.PostLogin("nobody", pw)
	if bad1.StatusCode != 401 || bad2.StatusCode != 401 {
		t.Fatalf("failures: %d %d", bad1.StatusCode, bad2.StatusCode)
	}
	msg := func(r harness.Resp) string { return strings.TrimSpace(r.Doc(t).Find(`[role="alert"]`).Text()) }
	if msg(bad1) == "" || msg(bad1) != msg(bad2) {
		t.Fatalf("error must be present and generic: %q vs %q", msg(bad1), msg(bad2))
	}
	if c.SessionCookie() != "" {
		t.Fatal("cookie set after failed login")
	}
	ok := c.PostLogin("admin", pw)
	if ok.StatusCode != http.StatusSeeOther || ok.Header.Get("Location") != "/admin/password" {
		t.Fatalf("ok login: %d %q", ok.StatusCode, ok.Header.Get("Location"))
	}
	var ck *http.Cookie
	for _, x := range ok.Cookies() {
		if x.Name == "hsp_admin" {
			ck = x
		}
	}
	if ck == nil || !ck.HttpOnly || ck.SameSite != http.SameSiteStrictMode || ck.Value == "" {
		t.Fatalf("cookie attributes: %+v", ck)
	}
	if r := c.Get("/admin/password"); r.StatusCode != 200 {
		t.Fatalf("password page after login: %d", r.StatusCode)
	}
	// After the forced change, a login lands on the members page.
	c.RefreshCSRFFrom("/admin/password")
	if r := c.SetPassword(pw, harness.AdminPassword, harness.AdminPassword); r.StatusCode != 200 {
		t.Fatalf("change: %d", r.StatusCode)
	}
	again := harness.NewAdminClient(t, s)
	if r := again.PostLogin("admin", harness.AdminPassword); r.StatusCode != http.StatusSeeOther || r.Header.Get("Location") != "/admin/members" {
		t.Fatalf("login after change: %d %q", r.StatusCode, r.Header.Get("Location"))
	}
}

// E1.02 throttle: 5 failures lock login for 1 minute (injected clock).
func TestE1_02_LoginThrottle(t *testing.T) {
	s, fc := startWithClock(t)
	const pw = "admin"
	c := harness.NewAdminClient(t, s)
	for i := 0; i < 5; i++ {
		if r := c.PostLogin("admin", "bad-password-123!Ab"); r.StatusCode != 401 {
			t.Fatalf("failure %d: %d", i+1, r.StatusCode)
		}
	}
	r := c.PostLogin("admin", "bad-password-123!Ab")
	if r.StatusCode != 429 || r.Header.Get("Retry-After") == "" {
		t.Fatalf("6th attempt: %d retry-after %q", r.StatusCode, r.Header.Get("Retry-After"))
	}
	if r := c.PostLogin("admin", pw); r.StatusCode != 429 {
		t.Fatalf("correct password while locked must also be refused: %d", r.StatusCode)
	}
	fc.Advance(59 * time.Second)
	if r := c.PostLogin("admin", pw); r.StatusCode != 429 {
		t.Fatalf("still locked at 59 s: %d", r.StatusCode)
	}
	fc.Advance(2 * time.Second)
	if r := c.PostLogin("admin", pw); r.StatusCode != 303 {
		t.Fatalf("after 1 minute: %d", r.StatusCode)
	}
}

// E1.03 protected pages redirect to login; login lands on /admin/members; GET /admin redirects there.
func TestE1_03_ProtectedPagesAndLanding(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	anon := harness.NewAdminClient(t, s)
	for _, p := range []string{"/admin", "/admin/members", "/admin/plans", "/admin/plans/new", "/admin/plans/1", "/admin/settings"} {
		r := anon.Get(p)
		if r.StatusCode != http.StatusSeeOther || r.Header.Get("Location") != "/admin/login" {
			t.Errorf("%s anonymous: %d %q", p, r.StatusCode, r.Header.Get("Location"))
		}
	}
	// Anonymous non-GET (htmx) requests are refused and told to go to login.
	r := anon.Do("POST", "/admin/plans", harness.PlanForm("X"), "")
	if r.StatusCode != 401 || r.Header.Get("HX-Redirect") != "/admin/login" {
		t.Errorf("anonymous POST: %d HX-Redirect %q", r.StatusCode, r.Header.Get("HX-Redirect"))
	}
	c := harness.LoginAsAdmin(t, s)
	r = c.Get("/admin")
	if r.StatusCode != http.StatusSeeOther || r.Header.Get("Location") != "/admin/members" {
		t.Fatalf("GET /admin: %d %q", r.StatusCode, r.Header.Get("Location"))
	}
	if r := c.Get("/admin/members"); r.StatusCode != 200 {
		t.Fatalf("landing: %d", r.StatusCode)
	}
}

// E1.04 CSRF: every non-GET needs the session's X-CSRF-Token.
func TestE1_04_CSRF(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	c := harness.LoginAsAdmin(t, s)
	db := harness.DB(t, s.DataDir)
	form := harness.PlanForm("Csrf")
	if r := c.Do("POST", "/admin/plans", form, ""); r.StatusCode != 403 {
		t.Errorf("no token: %d", r.StatusCode)
	}
	if r := c.Do("POST", "/admin/plans", form, "not-the-token"); r.StatusCode != 403 {
		t.Errorf("wrong token: %d", r.StatusCode)
	}
	if r := c.Do("POST", "/admin/logout", nil, ""); r.StatusCode != 403 {
		t.Errorf("logout without token: %d", r.StatusCode)
	}
	if n := scalar[int](t, db, `SELECT count(*) FROM plan`); n != 0 {
		t.Fatalf("plan created without CSRF token: %d", n)
	}
	if r := c.Send("POST", "/admin/plans", form); r.StatusCode != 201 {
		t.Errorf("right token: %d %s", r.StatusCode, r.Body)
	}
	// The token is per session: another session's token is refused.
	other := harness.LoginAsAdmin(t, s)
	if other.CSRF == c.CSRF {
		t.Fatal("two sessions share a CSRF token")
	}
	if r := c.Do("POST", "/admin/plans", harness.PlanForm("Csrf2"), other.CSRF); r.StatusCode != 403 {
		t.Errorf("foreign token: %d", r.StatusCode)
	}
	// htmx carries it via hx-headers on the layout.
	if tok := harness.CSRFFromDoc(t, c.Get("/admin/plans").Doc(t)); tok != c.CSRF {
		t.Errorf("hx-headers token differs between pages")
	}
}

// E1.17 admin session: opaque cookie, sliding 1 h, logout and set-password invalidate, hash only.
func TestE1_17_AdminSession(t *testing.T) {
	s, fc := startWithClock(t)
	db := harness.DB(t, s.DataDir)
	c := harness.LoginAsAdmin(t, s)
	cookie := c.SessionCookie()
	if len(cookie) < 40 || !regexp.MustCompile(`^[A-Za-z0-9_-]+$`).MatchString(cookie) {
		t.Fatalf("cookie not an opaque random id: %q", cookie)
	}
	sum := sha256.Sum256([]byte(cookie))
	hexSum := hex.EncodeToString(sum[:])
	var tokenHash string
	var expires int64
	if err := db.QueryRow(`SELECT token_hash, expires_at FROM admin_session`).Scan(&tokenHash, &expires); err != nil {
		t.Fatal(err)
	}
	if tokenHash == cookie || (tokenHash != hexSum) {
		t.Fatalf("token_hash %q is not sha256 hex of the cookie", tokenHash)
	}
	if expires != t0.Unix()+3600 {
		t.Fatalf("expires_at %d", expires)
	}
	if scalar[int](t, db, `SELECT count(*) FROM admin_session WHERE token_hash=? OR csrf_token=?`, cookie, cookie) != 0 {
		t.Fatal("cookie stored in clear")
	}

	// Activity extends the session; idle > 60 minutes ends it.
	fc.Advance(59 * time.Minute)
	if r := c.Get("/admin/members"); r.StatusCode != 200 {
		t.Fatalf("at 59 min: %d", r.StatusCode)
	}
	fc.Advance(59 * time.Minute) // 118 min since login, 59 since activity
	if r := c.Get("/admin/members"); r.StatusCode != 200 {
		t.Fatalf("activity did not extend: %d", r.StatusCode)
	}
	fc.Advance(61 * time.Minute)
	if r := c.Get("/admin/members"); r.StatusCode != 303 {
		t.Fatalf("61 min idle: %d", r.StatusCode)
	}
	if r := rawGet(t, s, cookie, "/admin/plans"); r.StatusCode != 303 {
		t.Fatalf("expired id replay: %d", r.StatusCode)
	}

	// Logout deletes the row; the id is dead server-side.
	c2 := harness.LoginAsAdmin(t, s)
	ck2 := c2.SessionCookie()
	if r := c2.Send("POST", "/admin/logout", nil); r.StatusCode != 200 && r.StatusCode != 303 {
		t.Fatalf("logout: %d", r.StatusCode)
	}
	if rawGet(t, s, ck2, "/admin/plans").StatusCode != 303 {
		t.Fatal("logged-out id still valid")
	}
	if n := scalar[int](t, db, `SELECT count(*) FROM admin_session WHERE token_hash=?`, func() string { h := sha256.Sum256([]byte(ck2)); return hex.EncodeToString(h[:]) }()); n != 0 {
		t.Fatal("session row survives logout")
	}

	// set-password invalidates all sessions.
	a, b := harness.LoginAsAdmin(t, s), harness.LoginAsAdmin(t, s)
	if _, se, code := runCLI(t, s, "Another-Passw0rd-OK!\n", "admin", "set-password", "--stdin"); code != 0 {
		t.Fatalf("set-password: %s", se)
	}
	for _, cl := range []*harness.AdminClient{a, b} {
		if r := cl.Get("/admin/members"); r.StatusCode != 303 {
			t.Fatalf("session survived set-password: %d", r.StatusCode)
		}
	}
	if scalar[int](t, db, `SELECT count(*) FROM admin_session`) != 0 {
		t.Fatal("rows survive set-password")
	}
}
