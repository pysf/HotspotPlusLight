package e2e

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/crypto/bcrypt"

	"hotspotpluslight/e2e/harness"
)

const goodNew = "correct horse battery"

func hasError(r harness.Resp, t *testing.T, field string) bool {
	t.Helper()
	return r.Doc(t).Find(`[data-error-for="`+field+`"]`).Length() == 1
}

// E11.20 first boot: admin/admin works and every page is forced to the password page (page and htmx requests).
func TestE11_20_ForcedToPasswordPage(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	db := harness.DB(t, s.DataDir)
	c := harness.NewAdminClient(t, s)
	c.Login("admin", "admin") // lands on the password page

	for _, p := range []string{"/admin", "/admin/members", "/admin/members/new", "/admin/plans", "/admin/settings", "/admin/router", "/admin/usage", "/admin/sessions", "/admin/batches"} {
		r := c.GetPage(p)
		if r.StatusCode != http.StatusSeeOther || r.Header.Get("Location") != "/admin/password" {
			t.Errorf("GET %s: %d %q (want 303 /admin/password)", p, r.StatusCode, r.Header.Get("Location"))
		}
	}
	// htmx change requests: HX-Redirect, and nothing is changed.
	r := c.Send("POST", "/admin/plans", harness.PlanForm("Sneaky"))
	if r.StatusCode != http.StatusForbidden || r.Header.Get("HX-Redirect") != "/admin/password" {
		t.Errorf("POST /admin/plans: %d HX-Redirect %q", r.StatusCode, r.Header.Get("HX-Redirect"))
	}
	if n := scalar[int](t, db, `SELECT count(*) FROM plan`); n != 0 {
		t.Errorf("a plan was created while the password change was pending")
	}
	// Open routes: the password page, logout, static files (and the login page).
	if r := c.Get("/admin/password"); r.StatusCode != 200 {
		t.Errorf("password page: %d", r.StatusCode)
	}
	if r := c.Get("/admin/static/htmx.min.js"); r.StatusCode != 200 {
		t.Errorf("static: %d", r.StatusCode)
	}
	if r := c.Send("POST", "/admin/logout", nil); r.StatusCode != 200 {
		t.Errorf("logout: %d", r.StatusCode)
	}
	if r := c.GetPage("/admin/password"); r.StatusCode != 303 || r.Header.Get("Location") != "/admin/login" {
		t.Errorf("after logout: %d %q", r.StatusCode, r.Header.Get("Location"))
	}
}

// E11.21 BR-20: too short, the username, a common password, too long and a wrong confirmation all give 422.
func TestE11_21_PasswordRule(t *testing.T) {
	s := harness.Start(t, harness.Options{Env: map[string]string{"ADMIN_USERNAME": "operator-user"}})
	db := harness.DB(t, s.DataDir)
	hash0 := scalar[string](t, db, `SELECT password_hash FROM admin`)
	c := harness.NewAdminClient(t, s)
	c.Login("operator-user", "admin")
	for _, bad := range []string{"", "short9abc", "OPERATOR-USER", "Password123", "qwertyuiop", strings.Repeat("a", 73)} {
		r := c.SetPassword("admin", bad, bad)
		if r.StatusCode != 422 || !hasError(r, t, "new_password") {
			t.Errorf("%q: %d (want 422 with a new_password error)", bad, r.StatusCode)
		}
	}
	if r := c.SetPassword("admin", goodNew, goodNew+"x"); r.StatusCode != 422 || !hasError(r, t, "confirm_password") {
		t.Errorf("mismatch: %d", r.StatusCode)
	}
	if scalar[string](t, db, `SELECT password_hash FROM admin`) != hash0 || scalar[int](t, db, `SELECT must_change_password FROM admin`) != 1 {
		t.Fatal("a rejected password changed the account")
	}
	// No character-class rules: lower-case words pass.
	if r := c.SetPassword("admin", goodNew, goodNew); r.StatusCode != 200 {
		t.Fatalf("good password: %d %s", r.StatusCode, r.Body)
	}
}

// E11.22 a wrong current password is rejected and counts in the login throttle.
func TestE11_22_WrongCurrentIsThrottled(t *testing.T) {
	s, fc := startWithClock(t)
	db := harness.DB(t, s.DataDir)
	hash0 := scalar[string](t, db, `SELECT password_hash FROM admin`)
	c := harness.NewAdminClient(t, s)
	c.Login("admin", "admin")
	for i := 0; i < 5; i++ {
		r := c.SetPassword("not-the-password", goodNew, goodNew)
		if r.StatusCode != 422 || !hasError(r, t, "current_password") {
			t.Fatalf("wrong current %d: %d", i+1, r.StatusCode)
		}
	}
	r := c.SetPassword("admin", goodNew, goodNew) // the right one, but locked
	if r.StatusCode != 429 || r.Header.Get("Retry-After") == "" {
		t.Fatalf("locked: %d retry-after %q", r.StatusCode, r.Header.Get("Retry-After"))
	}
	// The login form is locked as well: one throttle for both.
	if r := harness.NewAdminClient(t, s).PostLogin("admin", "admin"); r.StatusCode != 429 {
		t.Fatalf("login while locked: %d", r.StatusCode)
	}
	if scalar[string](t, db, `SELECT password_hash FROM admin`) != hash0 {
		t.Fatal("hash changed while locked")
	}
	fc.Advance(61 * time.Second)
	if r := c.SetPassword("admin", goodNew, goodNew); r.StatusCode != 200 {
		t.Fatalf("after the lock: %d", r.StatusCode)
	}
}

// E11.23 success: new bcrypt hash, flag cleared, other sessions ended, this one kept, free navigation, a toast.
func TestE11_23_ChangeSucceeds(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	db := harness.DB(t, s.DataDir)
	other := harness.NewAdminClient(t, s)
	other.Login("admin", "admin")
	c := harness.NewAdminClient(t, s)
	c.Login("admin", "admin")
	if n := scalar[int](t, db, `SELECT count(*) FROM admin_session`); n != 2 {
		t.Fatalf("sessions %d", n)
	}

	r := c.SetPassword("admin", goodNew, goodNew)
	if r.StatusCode != 200 || r.Header.Get("HX-Redirect") != "/admin" {
		t.Fatalf("change: %d HX-Redirect %q", r.StatusCode, r.Header.Get("HX-Redirect"))
	}
	toast := false
	for _, ck := range r.Cookies() {
		toast = toast || (ck.Name == "hsp_toast" && strings.Contains(ck.Value, "Password"))
	}
	if !toast {
		t.Error("no toast cookie")
	}
	hash := scalar[string](t, db, `SELECT password_hash FROM admin`)
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(goodNew)) != nil || strings.Contains(hash, goodNew) {
		t.Fatal("hash is not bcrypt(new)")
	}
	if scalar[int](t, db, `SELECT must_change_password FROM admin`) != 0 {
		t.Fatal("flag not cleared")
	}
	if n := scalar[int](t, db, `SELECT count(*) FROM admin_session`); n != 1 {
		t.Fatalf("sessions left: %d (want only the current one)", n)
	}
	for _, p := range []string{"/admin/members", "/admin/plans", "/admin/settings", "/admin/password"} {
		if r := c.Get(p); r.StatusCode != 200 {
			t.Errorf("current session %s: %d", p, r.StatusCode)
		}
	}
	if r := other.GetPage("/admin/members"); r.StatusCode != 303 || r.Header.Get("Location") != "/admin/login" {
		t.Errorf("other session: %d %q", r.StatusCode, r.Header.Get("Location"))
	}
	fresh := harness.NewAdminClient(t, s)
	if r := fresh.PostLogin("admin", "admin"); r.StatusCode != 401 {
		t.Errorf("old password: %d", r.StatusCode)
	}
	if r := fresh.PostLogin("admin", goodNew); r.StatusCode != 303 || r.Header.Get("Location") != "/admin" {
		t.Errorf("new password: %d %q", r.StatusCode, r.Header.Get("Location"))
	}
	// A later change from the same page works with the new current password.
	c.RefreshCSRFFrom("/admin/password")
	if r := c.SetPassword(goodNew, "another long phrase 7", "another long phrase 7"); r.StatusCode != 200 {
		t.Errorf("second change: %d", r.StatusCode)
	}
}

// E11.24 an existing install (flag 0, the migration default) is not forced.
func TestE11_24_ExistingInstallNotForced(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	db := harness.DB(t, s.DataDir)
	if d := scalar[string](t, db, `SELECT dflt_value FROM pragma_table_info('admin') WHERE name='must_change_password'`); d != "0" {
		t.Fatalf("column default %q, want 0", d)
	}
	if _, err := db.Exec(`UPDATE admin SET must_change_password=0`); err != nil {
		t.Fatal(err)
	}
	c := harness.NewAdminClient(t, s)
	r := c.PostLogin("admin", "admin")
	if r.StatusCode != 303 || r.Header.Get("Location") != "/admin" {
		t.Fatalf("login: %d %q", r.StatusCode, r.Header.Get("Location"))
	}
	if r := c.Get("/admin/members"); r.StatusCode != 200 {
		t.Fatalf("members: %d", r.StatusCode)
	}
	if r := c.Get("/admin/password"); r.StatusCode != 200 {
		t.Fatalf("password page: %d", r.StatusCode)
	}
}

// E11.26 the password page is reachable any time from the top bar; Generate and password.js are present, no inline code.
func TestE11_26_PageLinkAndGenerate(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	c := harness.LoginAsAdmin(t, s)
	if c.Get("/admin/members").Doc(t).Find(`header a[href="/admin/password"]`).Length() != 1 {
		t.Error("members page has no top bar link to /admin/password")
	}
	r := c.Get("/admin/password")
	if r.StatusCode != 200 {
		t.Fatalf("page: %d", r.StatusCode)
	}
	d := r.Doc(t)
	if d.Find(`form#password-form[hx-put="/admin/password"]`).Length() != 1 {
		t.Error("no htmx form")
	}
	if d.Find(`button[data-generate="new_password"]`).Length() != 1 || d.Find(`button[data-reveal="new_password"]`).Length() != 1 {
		t.Error("Generate or Reveal button missing")
	}
	if d.Find(`.c-notice[data-kind="warn"]`).Length() != 0 {
		t.Error("the default-password warning shows after the change")
	}
	if bad := pageIsClean(d); len(bad) > 0 {
		t.Errorf("inline code: %v", bad)
	}
	var js string
	d.Find(`script[src]`).Each(func(_ int, sc *goquery.Selection) {
		if src := sc.AttrOr("src", ""); strings.Contains(src, "js/password.js") {
			js = src
		}
	})
	if js == "" {
		t.Fatal("password.js is not loaded by the page")
	}
	jr := c.Get(js)
	if jr.StatusCode != 200 || !strings.Contains(jr.Body, "crypto.getRandomValues") {
		t.Fatalf("password.js: %d", jr.StatusCode)
	}
	if n := strings.Count(jr.Body, "\n"); n > 80 {
		t.Errorf("password.js has %d lines (limit 80)", n)
	}
}

// E11.25 `admin set-password` uses the new rule and clears the forced-change flag.
func TestE11_25_CLIClearsFlag(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	db := harness.DB(t, s.DataDir)
	if scalar[int](t, db, `SELECT must_change_password FROM admin`) != 1 {
		t.Fatal("flag not set at first start")
	}
	if _, _, code := runCLI(t, s, "short\n", "admin", "set-password", "--stdin"); code == 0 {
		t.Fatal("short password accepted")
	}
	if scalar[int](t, db, `SELECT must_change_password FROM admin`) != 1 {
		t.Fatal("a rejected password cleared the flag")
	}
	if _, se, code := runCLI(t, s, goodNew+"\n", "admin", "set-password", "--stdin"); code != 0 {
		t.Fatalf("set-password: %s", se)
	}
	if scalar[int](t, db, `SELECT must_change_password FROM admin`) != 0 {
		t.Fatal("flag not cleared by the CLI")
	}
	c := harness.NewAdminClient(t, s)
	if r := c.PostLogin("admin", goodNew); r.StatusCode != 303 || r.Header.Get("Location") != "/admin" {
		t.Fatalf("login after the CLI change: %d %q", r.StatusCode, r.Header.Get("Location"))
	}
}

var pwName = regexp.MustCompile(`^(current_|new_|old_|confirm_)?password(_confirm)?$`)

// E1.05 (inverted in Phase 11b) the password page exists, is the only password UI, and stays out of the other pages.
func TestE1_05_PasswordPageOnly(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	c := harness.LoginAsAdmin(t, s)
	for _, p := range []string{"/admin/account/password", "/admin/account"} {
		for _, m := range []string{"GET", "POST", "PUT"} {
			if r := c.Do(m, p, url.Values{"x": {"y"}}, c.CSRF); r.StatusCode != 404 {
				t.Errorf("%s %s: %d (want 404)", m, p, r.StatusCode)
			}
		}
	}
	d := c.Get("/admin/password").Doc(t)
	for _, n := range []string{"current_password", "new_password", "confirm_password"} {
		if d.Find(`input[type=password][name=`+n+`]`).Length() != 1 {
			t.Errorf("password page lacks the %s input", n)
		}
	}
	for _, p := range []string{"/admin/members", "/admin/plans", "/admin/plans/new", "/admin/settings"} {
		d := c.Get(p).Doc(t)
		d.Find("input").Each(func(_ int, in *goquery.Selection) {
			n, _ := in.Attr("name")
			typ, _ := in.Attr("type")
			if pwName.MatchString(n) || (typ == "password" && n != "radius_shared_secret") {
				t.Errorf("%s has a password input %q", p, n)
			}
		})
	}
}

// E1.06 `admin set-password` (stdin): new password works, old does not, all sessions end, secrets not echoed.
func TestE1_06_SetPasswordCLI(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	old := adminPassword(t, s)
	c := harness.LoginAsAdmin(t, s)
	db := harness.DB(t, s.DataDir)
	hashBefore := scalar[string](t, db, `SELECT password_hash FROM admin`)

	const np = "Brand-New-Passw0rd!x"
	if scalar[int](t, db, `SELECT count(*) FROM admin_session`) == 0 {
		t.Fatal("no session to end")
	}
	so, se, code := runCLI(t, s, np+"\n", "admin", "set-password", "--stdin")
	if code != 0 {
		t.Fatalf("exit %d: %s %s", code, so, se)
	}
	if strings.Contains(so+se, np) || strings.Contains(so+se, old) {
		t.Fatal("password printed by the CLI")
	}
	hash := scalar[string](t, db, `SELECT password_hash FROM admin`)
	if hash == hashBefore || bcrypt.CompareHashAndPassword([]byte(hash), []byte(np)) != nil || strings.Contains(hash, np) {
		t.Fatal("hash not updated to bcrypt(new)")
	}
	if n := scalar[int](t, db, `SELECT count(*) FROM admin_session`); n != 0 {
		t.Fatalf("sessions left: %d", n)
	}
	if r := c.GetPage("/admin/members"); r.StatusCode != 303 {
		t.Fatalf("old session still valid: %d", r.StatusCode)
	}
	fresh := harness.NewAdminClient(t, s)
	if r := fresh.PostLogin("admin", old); r.StatusCode != 401 {
		t.Fatalf("old password: %d", r.StatusCode)
	}
	if r := fresh.PostLogin("admin", np); r.StatusCode != 303 {
		t.Fatalf("new password: %d", r.StatusCode)
	}
}

// E1.14 first start: admin/admin works, no password is printed, the second start prints no first-start block (O15, Phase 11b).
func TestE1_14_DefaultPasswordOnce(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	if !strings.Contains(s.Stdout.String(), "password: admin") {
		t.Fatalf("first-start block lacks the default password:\n%s", s.Stdout.String())
	}
	harness.NewAdminClient(t, s).Login("admin", "admin")
	s.Stop()
	s2 := harness.Start(t, harness.Options{DataDir: s.DataDir, SecretKey: s.SecretKey})
	if strings.Contains(s2.Stdout.String(), "first start") {
		t.Fatalf("second start printed the block again:\n%s", s2.Stdout.String())
	}
	harness.NewAdminClient(t, s2).Login("admin", "admin") // still forced to change it
}

// E1.15 BR-20 via `admin set-password`: 10+ characters, at most 72 bytes, not the username, not "admin", not common.
func TestE1_15_PasswordStrength(t *testing.T) {
	const username = "Str0ng-Passw0rd-OK!"
	s := harness.Start(t, harness.Options{Env: map[string]string{"ADMIN_USERNAME": username}})
	db := harness.DB(t, s.DataDir)
	hash0 := scalar[string](t, db, `SELECT password_hash FROM admin`)
	for _, bad := range []string{
		"short9abc",               // < 10
		strings.Repeat("a", 73),   // > 72 bytes
		username,                  // equals username
		strings.ToUpper(username), // equals, ignoring case
		"Password123",             // common
		"qwertyuiop",              // common
		"",                        // empty
	} {
		_, se, code := runCLI(t, s, bad+"\n", "admin", "set-password", "--stdin")
		if code == 0 {
			t.Errorf("accepted %q", bad)
		}
		if bad != "" && strings.Contains(se, bad) {
			t.Errorf("stderr echoes the rejected password")
		}
	}
	if scalar[string](t, db, `SELECT password_hash FROM admin`) != hash0 {
		t.Fatal("hash changed by a rejected password")
	}
	// No character-class rules: lower-case words are fine.
	good := "correct horse battery"
	if _, se, code := runCLI(t, s, good+"\n", "admin", "set-password", "--stdin"); code != 0 {
		t.Fatalf("compliant rejected: %s", se)
	}
	harness.NewAdminClient(t, s).Login(username, good)
}

// E16.09 (review B10.1) while the password change is pending, an htmx GET (a polling list) gets
// HX-Redirect to the password page, not a 303 whose page would be swapped into the list.
func TestE16_09_ForcedPasswordOnHtmxGet(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	c := harness.NewAdminClient(t, s)
	c.Login("admin", "admin")
	r := c.GetHX("/admin/sessions/table", "session-list")
	if r.StatusCode != http.StatusForbidden || r.Header.Get("HX-Redirect") != "/admin/password" {
		t.Errorf("htmx GET: %d HX-Redirect %q", r.StatusCode, r.Header.Get("HX-Redirect"))
	}
}

// E16.13 (review B11) a password piped in with a trailing space is kept as typed, but the admin is told,
// because "echo pw | ..." in Windows cmd adds that space by mistake.
func TestE16_13_PipedPasswordTrailingSpaceWarns(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	_, se, code := runCLI(t, s, "Another-Passw0rd-OK! \r\n", "admin", "set-password", "--stdin")
	if code != 0 || !strings.Contains(se, "ends with a space") {
		t.Fatalf("exit %d, stderr %q", code, se)
	}
	_, se, code = runCLI(t, s, "Third-Passw0rd-Fine!\r\n", "admin", "set-password", "--stdin")
	if code != 0 || strings.Contains(se, "ends with a space") {
		t.Fatalf("no trailing space: exit %d, stderr %q", code, se)
	}
}

// E16.14 (review B11) set-password on a database that is older than migration 0003 says to run serve first,
// not a raw SQL error.
func TestE16_14_SetPasswordOnOldDatabase(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	db := harness.DB(t, s.DataDir)
	if _, err := db.Exec(`ALTER TABLE admin DROP COLUMN must_change_password`); err != nil {
		t.Fatal(err)
	}
	_, se, code := runCLI(t, s, "Another-Passw0rd-OK!\n", "admin", "set-password", "--stdin")
	if code == 0 || !strings.Contains(se, "serve") || strings.Contains(se, "no such column") {
		t.Fatalf("exit %d, stderr %q", code, se)
	}
}
