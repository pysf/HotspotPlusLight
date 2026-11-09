package e2e

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"hotspotpluslight/e2e/harness"
)

// setUsername sends the username card form of the Password and username page.
func setUsername(c *harness.AdminClient, name, current string) harness.Resp {
	return c.Send("PUT", "/admin/password/username", url.Values{"username": {name}, "username_current_password": {current}})
}

// E20.01 renaming works: the new name logs in, the old one does not, other sessions end, this one stays, a toast shows.
func TestE20_01_RenameAdmin(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	db := harness.DB(t, s.DataDir)
	c := harness.LoginAsAdmin(t, s)
	other := harness.NewAdminClient(t, s)
	other.Login("admin", harness.AdminPassword)
	hash0 := scalar[string](t, db, `SELECT password_hash FROM admin`)

	r := setUsername(c, "Owner.One-2", harness.AdminPassword)
	if r.StatusCode != 200 || r.Header.Get("HX-Redirect") != "/admin/password" {
		t.Fatalf("rename: %d HX-Redirect %q", r.StatusCode, r.Header.Get("HX-Redirect"))
	}
	toast := false
	for _, ck := range r.Cookies() {
		toast = toast || (ck.Name == "hsp_toast" && strings.Contains(ck.Value, "Username"))
	}
	if !toast {
		t.Error("no toast cookie")
	}
	if scalar[string](t, db, `SELECT username FROM admin`) != "Owner.One-2" || scalar[string](t, db, `SELECT password_hash FROM admin`) != hash0 {
		t.Fatal("name not stored, or the password changed")
	}
	if n := scalar[int](t, db, `SELECT count(*) FROM admin_session`); n != 1 {
		t.Fatalf("sessions left: %d (want only the current one)", n)
	}
	if r := c.Get("/admin/members"); r.StatusCode != 200 {
		t.Errorf("current session: %d", r.StatusCode)
	}
	if r := other.GetPage("/admin/members"); r.StatusCode != 303 || r.Header.Get("Location") != "/admin/login" {
		t.Errorf("other session: %d %q", r.StatusCode, r.Header.Get("Location"))
	}
	fresh := harness.NewAdminClient(t, s)
	if r := fresh.PostLogin("admin", harness.AdminPassword); r.StatusCode != 401 {
		t.Errorf("old name: %d", r.StatusCode)
	}
	if r := fresh.PostLogin("Owner.One-2", harness.AdminPassword); r.StatusCode != 303 || r.Header.Get("Location") != "/admin" {
		t.Errorf("new name: %d %q", r.StatusCode, r.Header.Get("Location"))
	}
	// The page shows the new name in the field.
	c.RefreshCSRFFrom("/admin/password")
	if v := c.Get("/admin/password").Doc(t).Find(`input[name=username]`).AttrOr("value", ""); v != "Owner.One-2" {
		t.Errorf("field value %q", v)
	}
}

// E20.02 a wrong current password is a field error, changes nothing and counts in the login throttle.
func TestE20_02_RenameWrongPassword(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	db := harness.DB(t, s.DataDir)
	c := harness.LoginAsAdmin(t, s)
	for i := 0; i < 5; i++ {
		r := setUsername(c, "newname", "not-the-password")
		if r.StatusCode != 422 || !hasError(r, t, "username_current_password") {
			t.Fatalf("wrong password %d: %d", i+1, r.StatusCode)
		}
	}
	if r := setUsername(c, "newname", harness.AdminPassword); r.StatusCode != 429 || r.Header.Get("Retry-After") == "" {
		t.Fatalf("locked: %d", r.StatusCode)
	}
	if scalar[string](t, db, `SELECT username FROM admin`) != "admin" {
		t.Fatal("name changed")
	}
}

// E20.03 bad names (too short, too long, bad characters, equal to the password) give 422 on the username field.
func TestE20_03_RenameInvalid(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	db := harness.DB(t, s.DataDir)
	c := harness.LoginAsAdmin(t, s)
	for _, bad := range []string{"", "abc", strings.Repeat("a", 33), "has space", "bad/name", "name_1", harness.AdminPassword} {
		r := setUsername(c, bad, harness.AdminPassword)
		if r.StatusCode != 422 || !hasError(r, t, "username") {
			t.Errorf("%q: %d (want 422 with a username error)", bad, r.StatusCode)
		}
	}
	if scalar[string](t, db, `SELECT username FROM admin`) != "admin" || scalar[int](t, db, `SELECT count(*) FROM admin_session`) != 1 {
		t.Fatal("a rejected rename changed the account")
	}
	// The edges of the range are fine.
	if r := setUsername(c, "abcd", harness.AdminPassword); r.StatusCode != 200 {
		t.Errorf("4 characters: %d", r.StatusCode)
	}
	if r := setUsername(c, strings.Repeat("b", 32), harness.AdminPassword); r.StatusCode != 200 {
		t.Errorf("32 characters: %d", r.StatusCode)
	}
}

// E20.04 saving the same name is a success and changes nothing.
func TestE20_04_RenameSameName(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	db := harness.DB(t, s.DataDir)
	c := harness.LoginAsAdmin(t, s)
	if r := setUsername(c, "admin", harness.AdminPassword); r.StatusCode != 200 {
		t.Fatalf("same name: %d", r.StatusCode)
	}
	if scalar[string](t, db, `SELECT username FROM admin`) != "admin" {
		t.Fatal("name changed")
	}
}

// E20.05 the username route needs a CSRF token and a session.
func TestE20_05_RenameNeedsCSRF(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	db := harness.DB(t, s.DataDir)
	c := harness.LoginAsAdmin(t, s)
	form := url.Values{"username": {"newname"}, "username_current_password": {harness.AdminPassword}}
	if r := c.Do("PUT", "/admin/password/username", form, ""); r.StatusCode != http.StatusForbidden {
		t.Errorf("no token: %d", r.StatusCode)
	}
	if r := c.Do("PUT", "/admin/password/username", form, "wrong"); r.StatusCode != http.StatusForbidden {
		t.Errorf("wrong token: %d", r.StatusCode)
	}
	anon := harness.NewAdminClient(t, s)
	if r := anon.Do("PUT", "/admin/password/username", form, ""); r.StatusCode == 200 {
		t.Errorf("anonymous: %d", r.StatusCode)
	}
	if scalar[string](t, db, `SELECT username FROM admin`) != "admin" {
		t.Fatal("name changed")
	}
}

// E20.06 the page has the Username and Password cards, the top bar link says Password, and the old paths stay 404.
func TestE20_06_PageLayout(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	c := harness.LoginAsAdmin(t, s)
	d := c.Get("/admin/password").Doc(t)
	if h := strings.TrimSpace(d.Find("h1").First().Text()); h != "Password and username" {
		t.Errorf("heading %q", h)
	}
	if d.Find(`form#username-form[hx-put="/admin/password/username"] input[name=username]`).Length() != 1 ||
		d.Find(`form#username-form input[type=password][name=username_current_password]`).Length() != 1 {
		t.Error("username form incomplete")
	}
	if d.Find(`form#password-form input[name=username]`).Length() != 0 {
		t.Error("the password form has a username field outside the forced screen")
	}
	if a := d.Find(`header a[href="/admin/password"]`); a.Length() != 1 || a.AttrOr("aria-label", "") != "Password" {
		t.Error("top bar link")
	}
	if bad := pageIsClean(d); len(bad) > 0 {
		t.Errorf("inline code: %v", bad)
	}
	for _, p := range []string{"/admin/account/password", "/admin/account", "/admin/account/username"} {
		if r := c.Do("GET", p, nil, c.CSRF); r.StatusCode != 404 {
			t.Errorf("%s: %d (want 404)", p, r.StatusCode)
		}
	}
}

// E20.07 the forced screen has a Username field with the current name, and a rename goes in with the password.
func TestE20_07_ForcedScreenRenames(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	db := harness.DB(t, s.DataDir)
	c := harness.NewAdminClient(t, s)
	c.Login("admin", "admin")
	f := c.GetPage("/admin/password").Doc(t)
	if v, ok := f.Find(`form#password-form input[name=username]`).Attr("value"); !ok || v != "admin" {
		t.Fatalf("username field %q %v", v, ok)
	}
	r := c.Send("PUT", "/admin/password", url.Values{"username": {"cafe.boss"}, "current_password": {"admin"}, "new_password": {goodNew}, "confirm_password": {goodNew}})
	if r.StatusCode != 200 || r.Header.Get("HX-Redirect") != "/admin" {
		t.Fatalf("change: %d %q", r.StatusCode, r.Header.Get("HX-Redirect"))
	}
	if scalar[string](t, db, `SELECT username FROM admin`) != "cafe.boss" || scalar[int](t, db, `SELECT must_change_password FROM admin`) != 0 {
		t.Fatal("name or flag not stored")
	}
	fresh := harness.NewAdminClient(t, s)
	if r := fresh.PostLogin("admin", goodNew); r.StatusCode != 401 {
		t.Errorf("old name: %d", r.StatusCode)
	}
	if r := fresh.PostLogin("cafe.boss", goodNew); r.StatusCode != 303 {
		t.Errorf("new name: %d", r.StatusCode)
	}
}

// E20.08 on the forced screen a bad username gives 422 and leaves the password and the flag alone.
func TestE20_08_ForcedBadUsernameKeepsPassword(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	db := harness.DB(t, s.DataDir)
	hash0 := scalar[string](t, db, `SELECT password_hash FROM admin`)
	c := harness.NewAdminClient(t, s)
	c.Login("admin", "admin")
	for _, bad := range []string{"abc", "has space", goodNew} {
		r := c.Send("PUT", "/admin/password", url.Values{"username": {bad}, "current_password": {"admin"}, "new_password": {goodNew}, "confirm_password": {goodNew}})
		if r.StatusCode != 422 || !hasError(r, t, "username") {
			t.Errorf("%q: %d (want 422 with a username error)", bad, r.StatusCode)
		}
		if v := r.Doc(t).Find(`input[name=username]`).AttrOr("value", ""); v != bad {
			t.Errorf("%q: the field shows %q", bad, v)
		}
	}
	// A legal name that equals the new password (in any letter case) is refused too.
	same := "Abcd-efgh-12"
	r := c.Send("PUT", "/admin/password", url.Values{"username": {same}, "current_password": {"admin"}, "new_password": {strings.ToLower(same)}, "confirm_password": {strings.ToLower(same)}})
	if r.StatusCode != 422 {
		t.Errorf("name equal to the password: %d, want 422", r.StatusCode)
	}
	if scalar[string](t, db, `SELECT password_hash FROM admin`) != hash0 || scalar[int](t, db, `SELECT must_change_password FROM admin`) != 1 || scalar[string](t, db, `SELECT username FROM admin`) != "admin" {
		t.Fatal("a rejected change touched the account")
	}
	// The username route stays closed while the password change is pending.
	if r := setUsername(c, "newname", "admin"); r.StatusCode != http.StatusForbidden || r.Header.Get("HX-Redirect") != "/admin/password" {
		t.Errorf("username route while forced: %d %q", r.StatusCode, r.Header.Get("HX-Redirect"))
	}
}
