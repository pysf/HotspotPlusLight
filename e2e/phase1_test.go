package e2e

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/crypto/bcrypt"
	"layeh.com/radius"

	"hotspotpluslight/e2e/fakenas"
	"hotspotpluslight/e2e/harness"
	"hotspotpluslight/internal/clock"
)

var t0 = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

func startWithClock(t *testing.T) (*harness.Server, *clock.Fake) {
	fc := clock.NewFake(t0)
	return harness.Start(t, harness.Options{Clock: fc}), fc
}

func adminPassword(t *testing.T, s *harness.Server) string {
	t.Helper()
	pw, ok := harness.AdminPasswordFromStdout(s.Stdout.String())
	if !ok {
		t.Fatal("no first-start password")
	}
	return pw
}

// runCLI runs the real binary with the server's data dir and optional stdin.
func runCLI(t *testing.T, s *harness.Server, stdin string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(harness.Binary(t), args...)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HSP_DATA_DIR=" + s.DataDir, "HSP_SECRET_KEY=" + s.SecretKey}
	cmd.Stdin = strings.NewReader(stdin)
	var so, se strings.Builder
	cmd.Stdout, cmd.Stderr = &so, &se
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return so.String(), se.String(), code
}

func insertMembers(t *testing.T, db *sql.DB, planID int64, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		_, err := db.Exec(`INSERT INTO member(username, password_enc, plan_id, subscription_date, created_at, updated_at)
			VALUES (?, x'00', ?, 1, 1, 1)`, fmt.Sprintf("user%d_%d", planID, i), planID)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func fieldError(r harness.Resp, t *testing.T, field string) string {
	return strings.TrimSpace(r.Doc(t).Find(`[data-error-for="` + field + `"]`).First().Text())
}

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

// E1.01 admin login ok/fail (BR-20 password from first start, O15).
func TestE1_01_AdminLogin(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	pw := adminPassword(t, s)

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
	if ok.StatusCode != http.StatusSeeOther || ok.Header.Get("Location") != "/admin/members" {
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
	if r := c.Get("/admin/members"); r.StatusCode != 200 {
		t.Fatalf("members after login: %d", r.StatusCode)
	}
}

// E1.02 throttle: 5 failures lock login for 1 minute (injected clock).
func TestE1_02_LoginThrottle(t *testing.T) {
	s, fc := startWithClock(t)
	pw := adminPassword(t, s)
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

var pwName = regexp.MustCompile(`^(current_|new_|old_|confirm_)?password(_confirm)?$`)

// E1.05 no password-change route or form exists in the admin UI (Q10).
func TestE1_05_NoPasswordChangeUI(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	c := harness.LoginAsAdmin(t, s)
	for _, m := range []string{"GET", "POST", "PUT"} {
		for _, p := range []string{"/admin/account/password", "/admin/password", "/admin/account"} {
			if r := c.Do(m, p, url.Values{"x": {"y"}}, c.CSRF); r.StatusCode != 404 {
				t.Errorf("%s %s: %d (want 404)", m, p, r.StatusCode)
			}
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
		if strings.Contains(strings.ToLower(d.Text()), "change password") {
			t.Errorf("%s mentions changing the password", p)
		}
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
	if r := c.Get("/admin/members"); r.StatusCode != 303 {
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

// E1.07 plan create/edit validation (BR-4, BR-5, DDL).
func TestE1_07_PlanValidation(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	c := harness.LoginAsAdmin(t, s)
	db := harness.DB(t, s.DataDir)

	cases := []struct {
		desc, field string
		form        url.Values
	}{
		{"empty name", "name", harness.PlanForm("", "x", "")},
		{"blank name", "name", harness.PlanForm("   ")},
		{"duration 0", "duration_days", harness.PlanForm("P", "duration_days", "0")},
		{"duration negative", "duration_days", harness.PlanForm("P", "duration_days", "-1")},
		{"duration empty", "duration_days", harness.PlanForm("P", "duration_days", "")},
		{"duration decimal", "duration_days", harness.PlanForm("P", "duration_days", "1.5")},
		{"duration text", "duration_days", harness.PlanForm("P", "duration_days", "abc")},
		{"max_sessions 0", "max_sessions", harness.PlanForm("P", "max_sessions", "0")},
		{"max_sessions decimal", "max_sessions", harness.PlanForm("P", "max_sessions", "1.5")},
		{"max_sessions negative", "max_sessions", harness.PlanForm("P", "max_sessions", "-2")},
		{"max_sessions empty", "max_sessions", harness.PlanForm("P", "max_sessions", "")},
		{"speed negative", "speed_value", harness.PlanForm("P", "speed_value", "-1")},
		{"speed text", "speed_value", harness.PlanForm("P", "speed_value", "fast")},
		{"speed unit", "speed_unit", harness.PlanForm("P", "speed_unit", "Tbps")},
		{"bulk negative", "bulk_value", harness.PlanForm("P", "bulk_value", "-0.5")},
		{"bulk unit", "bulk_unit", harness.PlanForm("P", "bulk_unit", "TB")},
		{"time negative", "time_limit_minutes", harness.PlanForm("P", "time_limit_minutes", "-5")},
		{"time decimal", "time_limit_minutes", harness.PlanForm("P", "time_limit_minutes", "2.5")},
	}
	for _, tc := range cases {
		r := c.Send("POST", "/admin/plans", tc.form)
		if r.StatusCode != 422 {
			t.Errorf("%s: status %d", tc.desc, r.StatusCode)
			continue
		}
		if fieldError(r, t, tc.field) == "" {
			t.Errorf("%s: no message for %s", tc.desc, tc.field)
		}
		if r.Doc(t).Find(`form input[name="name"]`).Length() != 1 {
			t.Errorf("%s: form not re-rendered", tc.desc)
		}
	}
	if n := scalar[int](t, db, `SELECT count(*) FROM plan`); n != 0 {
		t.Fatalf("invalid plans stored: %d", n)
	}
	// Re-rendered form keeps what was typed.
	r := c.Send("POST", "/admin/plans", harness.PlanForm("Keep", "duration_days", "0"))
	if v, _ := r.Doc(t).Find(`input[name="name"]`).Attr("value"); v != "Keep" {
		t.Errorf("name not kept: %q", v)
	}

	// All unit spellings are accepted; 0 = unlimited / no limit.
	for i, u := range [][2]string{{"Kbps", "KB"}, {"Mbps", "MB"}, {"Gbps", "GB"}} {
		c.CreatePlan(harness.PlanForm(fmt.Sprintf("U%d", i), "speed_unit", u[0], "bulk_unit", u[1]))
	}
	c.CreatePlan(harness.PlanForm("Unlimited", "speed_value", "0", "bulk_value", "0", "time_limit_minutes", "0"))

	// Name is unique, case-insensitive; on create and on edit.
	gold := c.CreatePlan(harness.PlanForm("Gold"))
	if r := c.Send("POST", "/admin/plans", harness.PlanForm("gOLD")); r.StatusCode != 422 || fieldError(r, t, "name") == "" {
		t.Errorf("duplicate create: %d", r.StatusCode)
	}
	silver := c.CreatePlan(harness.PlanForm("Silver"))
	if r := c.Send("PUT", fmt.Sprintf("/admin/plans/%d", silver), harness.PlanForm("GOLD")); r.StatusCode != 422 || fieldError(r, t, "name") == "" {
		t.Errorf("duplicate edit: %d", r.StatusCode)
	}
	// Saving a plan under its own name (even different case) is fine.
	if r := c.Send("PUT", fmt.Sprintf("/admin/plans/%d", gold), harness.PlanForm("gold", "duration_days", "60")); r.StatusCode != 200 {
		t.Errorf("edit own name: %d %s", r.StatusCode, r.Body)
	}
	if d := scalar[int](t, db, `SELECT duration_days FROM plan WHERE id=?`, gold); d != 60 {
		t.Errorf("duration after edit %d", d)
	}
	// Edit validation mirrors create.
	if r := c.Send("PUT", fmt.Sprintf("/admin/plans/%d", gold), harness.PlanForm("Gold", "max_sessions", "0")); r.StatusCode != 422 || fieldError(r, t, "max_sessions") == "" {
		t.Errorf("edit invalid: %d", r.StatusCode)
	}
}

// E1.08 decimal speed/bulk accepted and stored exactly (BR-4).
func TestE1_08_DecimalValues(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	c := harness.LoginAsAdmin(t, s)
	db := harness.DB(t, s.DataDir)
	id := c.CreatePlan(harness.PlanForm("Dec", "speed_value", "1.5", "speed_unit", "Mbps", "bulk_value", "0.5", "bulk_unit", "GB", "time_limit_minutes", "90", "max_sessions", "3"))
	q := func(col string) float64 { return scalar[float64](t, db, `SELECT `+col+` FROM plan WHERE id=?`, id) }
	if q("speed_value") != 1.5 || q("bulk_value") != 0.5 || q("time_limit_minutes") != 90 || q("max_sessions") != 3 {
		t.Fatalf("stored %v %v", q("speed_value"), q("bulk_value"))
	}
	if r := c.Send("PUT", fmt.Sprintf("/admin/plans/%d", id), harness.PlanForm("Dec", "speed_value", "0.25", "speed_unit", "Gbps", "bulk_value", "100.75", "bulk_unit", "KB")); r.StatusCode != 200 {
		t.Fatalf("edit: %d %s", r.StatusCode, r.Body)
	}
	if q("speed_value") != 0.25 || q("bulk_value") != 100.75 || scalar[string](t, db, `SELECT speed_unit FROM plan WHERE id=?`, id) != "Gbps" {
		t.Fatal("decimal edit not stored")
	}
	// The edit page shows the stored values back.
	d := c.Get(fmt.Sprintf("/admin/plans/%d", id)).Doc(t)
	if v, _ := d.Find(`input[name="speed_value"]`).Attr("value"); v != "0.25" {
		t.Errorf("speed shown as %q", v)
	}
	if v, _ := d.Find(`input[name="bulk_value"]`).Attr("value"); v != "100.75" {
		t.Errorf("bulk shown as %q", v)
	}
}

func setDefaultPlan(t *testing.T, c *harness.AdminClient, planID string) harness.Resp {
	t.Helper()
	form := harness.FormValues(t, c.Get("/admin/settings").Doc(t), "form#settings-form")
	form.Set("default_plan_id", planID)
	return c.Send("PUT", "/admin/settings", form)
}

// E1.09 deleting a plan that members use is blocked with the count; the default plan too (O12).
func TestE1_09_DeletePlanInUse(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	c := harness.LoginAsAdmin(t, s)
	db := harness.DB(t, s.DataDir)
	id := c.CreatePlan(harness.PlanForm("Used"))
	insertMembers(t, db, id, 3)
	path := fmt.Sprintf("/admin/plans/%d", id)
	r := c.Send("DELETE", path, nil)
	if r.StatusCode != 409 || !strings.Contains(r.Body, "3 members") {
		t.Fatalf("delete in use: %d %q", r.StatusCode, r.Body)
	}
	if n := scalar[int](t, db, `SELECT count(*) FROM plan WHERE id=?`, id); n != 1 {
		t.Fatal("plan deleted despite members")
	}
	if _, err := db.Exec(`DELETE FROM member`); err != nil {
		t.Fatal(err)
	}
	// Default plan cannot be deleted until another default (or none) is chosen.
	if r := setDefaultPlan(t, c, fmt.Sprint(id)); r.StatusCode != 200 {
		t.Fatalf("set default: %d %s", r.StatusCode, r.Body)
	}
	r = c.Send("DELETE", path, nil)
	if r.StatusCode != 409 || !strings.Contains(strings.ToLower(r.Body), "default") {
		t.Fatalf("delete default: %d %q", r.StatusCode, r.Body)
	}
	if r := setDefaultPlan(t, c, ""); r.StatusCode != 200 {
		t.Fatalf("clear default: %d", r.StatusCode)
	}
	if r := c.Send("DELETE", path, nil); r.StatusCode != 200 || r.Header.Get("HX-Redirect") != "/admin/plans" {
		t.Fatalf("delete unused: %d %q", r.StatusCode, r.Header.Get("HX-Redirect"))
	}
	if r := c.Get(path); r.StatusCode != 404 {
		t.Fatalf("deleted plan page: %d", r.StatusCode)
	}
}

// E1.10 settings: all columns, validation, persistence, masked secret (PLAN 6, 8.2, O8).
func TestE1_10_Settings(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	c := harness.LoginAsAdmin(t, s)
	db := harness.DB(t, s.DataDir)
	plan := c.CreatePlan(harness.PlanForm("Default one"))

	page := c.Get("/admin/settings")
	if page.StatusCode != 200 {
		t.Fatalf("page: %d", page.StatusCode)
	}
	d := page.Doc(t)
	form := harness.FormValues(t, d, "form#settings-form")
	for _, f := range []string{"site_title", "timezone", "radius_shared_secret", "coa_port", "accounting_interval_seconds",
		"default_plan_id", "mac_auto_login", "after_login_url", "batch_username_prefix", "batch_counter", "batch_max_count",
		"voucher_password_length", "voucher_help", "public_base_url", "session_retention_days"} {
		if d.Find(`form#settings-form [name="`+f+`"]`).Length() != 1 {
			t.Errorf("settings form lacks %s", f)
		}
	}
	if d.Find(`input[name="radius_shared_secret"][type="password"]`).Length() != 1 || d.Find(`#reveal-secret`).Length() != 1 {
		t.Error("secret must be masked with a reveal button")
	}
	if v := form.Get("coa_port"); v != "3799" {
		t.Errorf("default coa_port shown %q", v)
	}
	if d.Find(`select[name="default_plan_id"] option`).Length() != 2 { // none + the one plan
		t.Errorf("default plan select options: %d", d.Find(`select[name="default_plan_id"] option`).Length())
	}
	if !strings.Contains(strings.ToLower(d.Text()), "spoof") {
		t.Error("MAC auto-login warning missing (O8)")
	}

	// Invalid values: 422, message next to the field, nothing stored.
	invalid := []struct{ field, val string }{
		{"site_title", ""}, {"timezone", "Mars/Base"}, {"radius_shared_secret", ""},
		{"coa_port", "0"}, {"coa_port", "65536"}, {"coa_port", "abc"},
		{"accounting_interval_seconds", "9"}, {"accounting_interval_seconds", "x"},
		{"default_plan_id", "9999"}, {"after_login_url", "ftp://x"}, {"public_base_url", "not a url"},
		{"batch_username_prefix", "wi-fi"}, {"batch_counter", "-1"}, {"batch_max_count", "0"},
		{"voucher_password_length", "0"}, {"voucher_password_length", "x"}, {"session_retention_days", "-1"},
	}
	before := scalar[string](t, db, `SELECT site_title||coa_port||accounting_interval_seconds||radius_shared_secret FROM settings`)
	for _, iv := range invalid {
		f := harness.FormValues(t, d, "form#settings-form")
		f.Set(iv.field, iv.val)
		r := c.Send("PUT", "/admin/settings", f)
		if r.StatusCode != 422 || fieldError(r, t, iv.field) == "" {
			t.Errorf("%s=%q: status %d err %q", iv.field, iv.val, r.StatusCode, fieldError(r, t, iv.field))
		}
	}
	if after := scalar[string](t, db, `SELECT site_title||coa_port||accounting_interval_seconds||radius_shared_secret FROM settings`); after != before {
		t.Fatal("invalid settings were stored")
	}

	// Valid update persists every column.
	f := harness.FormValues(t, d, "form#settings-form")
	for k, v := range map[string]string{
		"site_title": "My Cafe", "timezone": "Asia/Tehran", "radius_shared_secret": "s3cret-value", "coa_port": "3800",
		"accounting_interval_seconds": "120", "default_plan_id": fmt.Sprint(plan), "mac_auto_login": "on",
		"after_login_url": "https://example.com/welcome", "batch_username_prefix": "cafe", "batch_counter": "2000",
		"batch_max_count": "50", "voucher_password_length": "8", "voucher_help": "Connect to the WiFi and sign in.",
		"public_base_url": "http://192.168.1.10:8080", "session_retention_days": "0",
	} {
		f.Set(k, v)
	}
	r := c.Send("PUT", "/admin/settings", f)
	if r.StatusCode != 200 || !strings.Contains(r.Body, "Settings saved") {
		t.Fatalf("save: %d %s", r.StatusCode, r.Body)
	}
	row := db.QueryRow(`SELECT site_title,timezone,radius_shared_secret,coa_port,accounting_interval_seconds,default_plan_id,mac_auto_login,after_login_url,batch_username_prefix,batch_counter,batch_max_count,voucher_password_length,voucher_help,public_base_url,session_retention_days FROM settings`)
	var (
		title, tz, sec, after, prefix, help, base       string
		coa, interval, mac, counter, maxc, vlen, retain int
		def                                             sql.NullInt64
	)
	if err := row.Scan(&title, &tz, &sec, &coa, &interval, &def, &mac, &after, &prefix, &counter, &maxc, &vlen, &help, &base, &retain); err != nil {
		t.Fatal(err)
	}
	if title != "My Cafe" || tz != "Asia/Tehran" || sec != "s3cret-value" || coa != 3800 || interval != 120 || !def.Valid || def.Int64 != plan ||
		mac != 1 || after != "https://example.com/welcome" || prefix != "cafe" || counter != 2000 || maxc != 50 || vlen != 8 ||
		help != "Connect to the WiFi and sign in." || base != "http://192.168.1.10:8080" || retain != 0 {
		t.Fatalf("stored: %v", []any{title, tz, sec, coa, interval, def, mac, after, prefix, counter, maxc, vlen, help, base, retain})
	}
	// Reload shows what was saved; unchecked box turns the flag off; "" clears the default plan.
	d2 := c.Get("/admin/settings").Doc(t)
	f2 := harness.FormValues(t, d2, "form#settings-form")
	if f2.Get("site_title") != "My Cafe" || f2.Get("mac_auto_login") == "" || f2.Get("default_plan_id") != fmt.Sprint(plan) {
		t.Fatalf("reloaded form: %v", f2)
	}
	f2.Del("mac_auto_login")
	f2.Set("default_plan_id", "")
	if r := c.Send("PUT", "/admin/settings", f2); r.StatusCode != 200 {
		t.Fatalf("second save: %d", r.StatusCode)
	}
	if scalar[int](t, db, `SELECT mac_auto_login FROM settings`) != 0 || scalar[sql.NullInt64](t, db, `SELECT default_plan_id FROM settings`).Valid {
		t.Fatal("flag/default not cleared")
	}
}

// E1.11 a changed shared secret is used by the RADIUS listener without restart (PLAN 7.1).
func TestE1_11_SharedSecretAppliesWithoutRestart(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	c := harness.LoginAsAdmin(t, s)
	db := harness.DB(t, s.DataDir)
	oldSecret := scalar[string](t, db, `SELECT radius_shared_secret FROM settings`)
	if r, err := fakenas.New(s.Addrs.RadiusAuth, oldSecret).Authenticate("u", "p"); err != nil || r.Code != radius.CodeAccessReject {
		t.Fatalf("before change: %v %v", r, err)
	}
	const newSecret = "a-completely-new-shared-secret"
	f := harness.FormValues(t, c.Get("/admin/settings").Doc(t), "form#settings-form")
	f.Set("radius_shared_secret", newSecret)
	if r := c.Send("PUT", "/admin/settings", f); r.StatusCode != 200 {
		t.Fatalf("save: %d", r.StatusCode)
	}
	nas := fakenas.New(s.Addrs.RadiusAuth, newSecret)
	if r, err := nas.Authenticate("u", "p"); err != nil || r.Code != radius.CodeAccessReject {
		t.Fatalf("new secret: %v %v", r, err)
	}
	stale := fakenas.New(s.Addrs.RadiusAuth, oldSecret)
	stale.Timeout = 1500 * time.Millisecond
	if r, err := stale.Authenticate("u", "p"); err == nil {
		t.Fatalf("old secret still verifies replies: %v", r.Code)
	}
}

// E1.14 first start prints a generated admin password once and it logs in; the second start prints none (O15).
func TestE1_14_GeneratedPasswordOnce(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	pw := adminPassword(t, s)
	if strings.Count(s.Stdout.String(), pw) != 1 {
		t.Fatal("password printed other than once")
	}
	harness.NewAdminClient(t, s).Login("admin", pw)
	s.Stop()
	s2 := harness.Start(t, harness.Options{DataDir: s.DataDir, SecretKey: s.SecretKey})
	if strings.Contains(s2.Stdout.String(), "Admin password") || strings.Contains(s2.Stdout.String(), pw) {
		t.Fatalf("second start printed a password:\n%s", s2.Stdout.String())
	}
	harness.NewAdminClient(t, s2).Login("admin", pw)
}

// E1.15 BR-20 via `admin set-password`.
func TestE1_15_PasswordStrength(t *testing.T) {
	const username = "Str0ng-Passw0rd-OK!"
	s := harness.Start(t, harness.Options{Env: map[string]string{"ADMIN_USERNAME": username}})
	db := harness.DB(t, s.DataDir)
	hash0 := scalar[string](t, db, `SELECT password_hash FROM admin`)
	for _, bad := range []string{
		"Sh0rt-Pw!aB",             // < 16
		"alllowercase1234!!!!",    // no upper
		"ALLUPPERCASE1234!!!!",    // no lower
		"NoDigitsHere-----xxXX",   // no digit
		"NoSymbolsHere1234567xX",  // no symbol
		username,                  // equals username
		strings.ToLower(username), // equals, ignoring case
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
	good := "Valid-Passw0rd-16+"
	if _, se, code := runCLI(t, s, good+"\n", "admin", "set-password", "--stdin"); code != 0 {
		t.Fatalf("compliant rejected: %s", se)
	}
	harness.NewAdminClient(t, s).Login(username, good)
}

// E1.16 D23: admin routes 404 on the public listener and the reverse.
func TestE1_16_ListenerSeparation(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	for _, p := range []string{"/admin", "/admin/login", "/admin/plans", "/admin/settings", "/admin/static/htmx.min.js"} {
		if r, _ := harness.Get(t, "http://"+s.Addrs.HTTP+p); r.StatusCode != 404 {
			t.Errorf("public %s: %d", p, r.StatusCode)
		}
	}
	if r, b := harness.Get(t, "http://"+s.Addrs.HTTP+"/healthz"); r.StatusCode != 200 || !strings.HasPrefix(b, "ok") {
		t.Errorf("public /healthz: %d", r.StatusCode)
	}
	c := harness.LoginAsAdmin(t, s)
	for _, p := range []string{"/", "/healthz", "/api/v1/config", "/api/v1/account", "/static/htmx.min.js"} {
		if r := c.Get(p); r.StatusCode != 404 {
			t.Errorf("admin %s: %d", p, r.StatusCode)
		}
	}
	if r := c.Get("/admin/login"); r.StatusCode != 200 {
		t.Errorf("admin login page: %d", r.StatusCode)
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

// E1.18 editing a plan used by N members shows the N-member warning (BR-5b, Q16).
func TestE1_18_PlanEditWarning(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	c := harness.LoginAsAdmin(t, s)
	db := harness.DB(t, s.DataDir)
	id := c.CreatePlan(harness.PlanForm("Shared"))
	path := fmt.Sprintf("/admin/plans/%d", id)
	if n := c.Get(path).Doc(t).Find(`[data-plan-warning]`).Length(); n != 0 {
		t.Fatalf("warning shown for 0 members")
	}
	warning := func() string {
		return strings.Join(strings.Fields(c.Get(path).Doc(t).Find(`[data-plan-warning]`).Text()), " ")
	}
	insertMembers(t, db, id, 1)
	if w := warning(); w != "1 member uses this plan; changes apply to it at their next login" {
		t.Fatalf("singular warning %q", w)
	}
	if _, err := db.Exec(`DELETE FROM member`); err != nil {
		t.Fatal(err)
	}
	insertMembers(t, db, id, 3)
	w := strings.Join(strings.Fields(c.Get(path).Doc(t).Find(`[data-plan-warning]`).Text()), " ")
	if w != "3 members use this plan; changes apply to all of them at their next login" {
		t.Fatalf("warning %q", w)
	}
	// A failed edit re-renders the form with the warning still present.
	r := c.Send("PUT", path, harness.PlanForm("Shared", "duration_days", "0"))
	if r.StatusCode != 422 || r.Doc(t).Find(`[data-plan-warning]`).Length() != 1 {
		t.Fatalf("422 form lost the warning: %d", r.StatusCode)
	}
}

// E1.18 second half: plan limits are read live at each login (BR-5b, Q16).
func TestE1_18_LimitsChangeAffectsNextAccessRequest(t *testing.T) {
	f := newP3(t)
	form := func(kv ...string) url.Values { return harness.PlanForm("Shared", kv...) }
	planID := f.plan("Shared", "bulk_value", "1", "bulk_unit", "GB", "duration_days", "30")
	path := fmt.Sprintf("/admin/plans/%d", planID)
	edit := func(kv ...string) {
		t.Helper()
		if r := f.c.Send("PUT", path, form(kv...)); r.StatusCode != 200 {
			t.Fatalf("edit plan: %d %s", r.StatusCode, r.Body)
		}
	}
	id := f.member(planID, "alice", "password", "pw")
	f.exec(`UPDATE member SET used_download_bytes=?, subscription_date=? WHERE id=?`, 500*mb, t0.Unix()-10*24*3600, id)
	f.accepted("alice", "pw")

	// Lowering the data limit below the usage rejects at the next login; raising it accepts again.
	edit("bulk_value", "100", "bulk_unit", "MB", "duration_days", "30")
	f.rejected("alice", "pw", "OUT_OF_BULK")
	edit("bulk_value", "2", "bulk_unit", "GB", "duration_days", "30")
	f.accepted("alice", "pw")

	// Shortening the duration below the elapsed time lapses the window; auto-resubscribe renews instead.
	edit("bulk_value", "2", "bulk_unit", "GB", "duration_days", "5")
	f.rejected("alice", "pw", "NO_ACTIVE_SUBSCRIPTION")
	edit("bulk_value", "2", "bulk_unit", "GB", "duration_days", "5", "auto_resubscribe", "on")
	f.accepted("alice", "pw")
	if n := scalar[int](t, f.db, `SELECT renewal_count FROM member WHERE id=?`, id); n != 1 {
		t.Errorf("renewal_count %d, want 1", n)
	}

	// The time limit and the session limit are live too.
	edit("bulk_value", "2", "bulk_unit", "GB", "duration_days", "5", "time_limit_minutes", "10")
	f.exec(`UPDATE member SET used_seconds=600 WHERE id=?`, id)
	f.rejected("alice", "pw", "OUT_OF_TIME")
	edit("bulk_value", "2", "bulk_unit", "GB", "duration_days", "5", "time_limit_minutes", "20")
	f.accepted("alice", "pw")
	f.openSession(id, "alice", "s1", f.fc.Now())
	f.rejected("alice", "pw", "NO_MORE_SESSION")
	edit("bulk_value", "2", "bulk_unit", "GB", "duration_days", "5", "time_limit_minutes", "20", "max_sessions", "2")
	f.accepted("alice", "pw")
}

// E1.19 resource-style routes (Q39): verbs, statuses, htmx config, removed verb routes.
func TestE1_19_ResourceStyleRoutes(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	c := harness.LoginAsAdmin(t, s)
	db := harness.DB(t, s.DataDir)

	// POST create: 201 + Location + HX-Redirect.
	r := c.Send("POST", "/admin/plans", harness.PlanForm("Res"))
	if r.StatusCode != 201 || r.Header.Get("HX-Redirect") != "/admin/plans" || !strings.HasPrefix(r.Header.Get("Location"), "/admin/plans/") {
		t.Fatalf("create: %d %v", r.StatusCode, r.Header)
	}
	id := strings.TrimPrefix(r.Header.Get("Location"), "/admin/plans/")
	path := "/admin/plans/" + id

	// PUT / DELETE: CSRF required.
	if r := c.Do("PUT", path, harness.PlanForm("Res", "duration_days", "5"), ""); r.StatusCode != 403 {
		t.Errorf("PUT without token: %d", r.StatusCode)
	}
	if r := c.Do("DELETE", path, nil, ""); r.StatusCode != 403 {
		t.Errorf("DELETE without token: %d", r.StatusCode)
	}
	if r := c.Send("PUT", path, harness.PlanForm("Res", "duration_days", "5")); r.StatusCode != 200 || r.Header.Get("HX-Redirect") != "/admin/plans" {
		t.Errorf("PUT: %d", r.StatusCode)
	}
	if scalar[int](t, db, `SELECT duration_days FROM plan`) != 5 {
		t.Error("PUT did not update")
	}

	// 405 for wrong verbs.
	for _, tc := range [][2]string{{"POST", path}, {"PATCH", path}, {"PUT", "/admin/plans"}, {"DELETE", "/admin/plans"}, {"POST", "/admin/plans/new"}, {"PUT", "/admin/settings/x"}} {
		want := 405
		if tc[1] == "/admin/settings/x" {
			want = 404
		}
		if r := c.Send(tc[0], tc[1], url.Values{}); r.StatusCode != want {
			t.Errorf("%s %s: %d (want %d)", tc[0], tc[1], r.StatusCode, want)
		}
	}
	if r := c.Send("POST", "/admin/settings", url.Values{}); r.StatusCode != 405 {
		t.Errorf("POST /admin/settings: %d", r.StatusCode)
	}
	if r := c.Send("DELETE", "/admin/settings", nil); r.StatusCode != 405 {
		t.Errorf("DELETE /admin/settings: %d", r.StatusCode)
	}

	// 404 for unknown ids (also non-numeric) on every verb.
	for _, id := range []string{"9999", "abc", "0"} {
		p := "/admin/plans/" + id
		if r := c.Get(p); r.StatusCode != 404 {
			t.Errorf("GET %s: %d", p, r.StatusCode)
		}
		if r := c.Send("PUT", p, harness.PlanForm("Z")); r.StatusCode != 404 {
			t.Errorf("PUT %s: %d", p, r.StatusCode)
		}
		if r := c.Send("DELETE", p, nil); r.StatusCode != 404 {
			t.Errorf("DELETE %s: %d", p, r.StatusCode)
		}
	}

	// 422 re-renders the form fragment (no layout) with field messages and an htmx-ready form.
	r = c.Send("PUT", path, harness.PlanForm("Res", "duration_days", "0", "max_sessions", "0"))
	d := r.Doc(t)
	if r.StatusCode != 422 || d.Find("html").Length() > 0 && d.Find("nav").Length() > 0 {
		t.Errorf("422: %d (must be a fragment)", r.StatusCode)
	}
	if fieldError(r, t, "duration_days") == "" || fieldError(r, t, "max_sessions") == "" {
		t.Error("422 lacks field messages")
	}
	if v, _ := d.Find("form").Attr("hx-put"); v != path {
		t.Errorf("re-rendered form hx-put %q", v)
	}

	// Edit page is an htmx form and the layout configures htmx for 422/409.
	page := c.Get(path).Doc(t)
	if v, _ := page.Find("form#plan-form").Attr("hx-put"); v != path {
		t.Errorf("edit form hx-put %q", v)
	}
	cfg, _ := page.Find(`meta[name="htmx-config"]`).Attr("content")
	if !strings.Contains(cfg, "422") || !strings.Contains(cfg, "409") {
		t.Errorf("htmx-config %q", cfg)
	}
	src, _ := page.Find(`script[src]`).First().Attr("src")
	resp, err := http.Get(c.Base + src)
	if err != nil {
		t.Fatal(err)
	}
	js, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(js), "htmx") {
		t.Errorf("htmx asset %s: %d", src, resp.StatusCode)
	}

	// The old verb-style routes do not exist.
	for _, tc := range [][2]string{{"POST", "/admin/members/delete"}, {"POST", "/admin/members/1/kick"}, {"POST", "/admin/members/1/renew"}, {"GET", "/admin/members/print"}, {"GET", "/admin/print"}, {"POST", "/admin/plans/delete"}} {
		if r := c.Do(tc[0], tc[1], url.Values{}, c.CSRF); r.StatusCode != 404 && !(tc[1] == "/admin/plans/delete" && r.StatusCode == 405) {
			t.Errorf("%s %s: %d (want 404)", tc[0], tc[1], r.StatusCode)
		}
	}
}

// Sidebar: every group has a heading and every link an icon.
func TestE1_20_SidebarGroupsAndIcons(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	c := harness.LoginAsAdmin(t, s)
	d := c.Get("/admin/members").Doc(t)
	if d.Find("#sidebar .nav-heading").Length() != 2 {
		t.Errorf("want 2 nav groups")
	}
	d.Find("#sidebar .nav-heading").Each(func(_ int, h *goquery.Selection) {
		if strings.TrimSpace(h.Text()) == "" {
			t.Errorf("empty nav group heading")
		}
	})
	if d.Find("#sidebar a.nav-link").Length() != 7 {
		t.Errorf("want 7 nav links")
	}
	d.Find("#sidebar a.nav-link").Each(func(_ int, a *goquery.Selection) {
		if a.Find("svg > path, svg > rect, svg > circle").Length() == 0 {
			t.Errorf("nav link %q has no icon", strings.TrimSpace(a.Text()))
		}
	})
}
