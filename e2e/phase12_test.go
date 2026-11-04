package e2e

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"html"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/PuerkitoBio/goquery"
	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/vendors/microsoft"

	"hotspotpluslight/e2e/harness"
	"hotspotpluslight/internal/domain"
)

// E12.07 (O20, changed 2026-11-04) the Reply-Message of every Access-Reject is exactly the code, and
// the sweep covers every reason the server can send over RADIUS.
func TestE12_07_ReplyMessageIsOnlyTheCode(t *testing.T) {
	f := newP3(t)
	day := int64(24 * 3600)
	seen := map[domain.Reason]bool{}
	check := func(r *radius.Packet, want domain.Reason) {
		t.Helper()
		if r.Code != radius.CodeAccessReject {
			t.Fatalf("%s: got %v, want Access-Reject", want, r.Code)
		}
		if got := rfc2865.ReplyMessage_GetString(r); got != string(want) {
			t.Errorf("%s: Reply-Message %q, want exactly the code", want, got)
		}
		seen[want] = true
	}
	basic := f.plan("Basic")
	alice := f.member(basic, "alice", "password", "pw")
	check(f.login("alice", "wrong"), domain.ReasonInvalidCredentials)
	f.exec(`UPDATE member SET active=0 WHERE id=?`, alice)
	check(f.login("alice", "pw"), domain.ReasonInactive)
	f.exec(`UPDATE member SET active=1, expires_at=? WHERE id=?`, t0.Unix()-1, alice)
	check(f.login("alice", "pw"), domain.ReasonExpired)
	f.exec(`UPDATE member SET expires_at=NULL, subscription_date=? WHERE id=?`, t0.Unix()-400*day, alice)
	check(f.login("alice", "pw"), domain.ReasonNoSubscription)

	small := f.member(f.plan("Small", "bulk_value", "100", "bulk_unit", "MB"), "small", "password", "pw")
	f.exec(`UPDATE member SET used_download_bytes=?, used_upload_bytes=0 WHERE id=?`, 200*mb, small)
	check(f.login("small", "pw"), domain.ReasonOutOfBulk)

	hour := f.member(f.plan("Hour", "time_limit_minutes", "60"), "hour", "password", "pw")
	f.exec(`UPDATE member SET used_seconds=3600 WHERE id=?`, hour)
	check(f.login("hour", "pw"), domain.ReasonOutOfTime)

	one := f.member(f.plan("One", "max_sessions", "1"), "one", "password", "pw")
	f.openSession(one, "one", "s1", f.fc.Now())
	check(f.login("one", "pw"), domain.ReasonNoMoreSession)

	// NO_PLAN and PLAN_REMOVED need rows the UI never creates (see E3.04): foreign keys off.
	np := f.member(basic, "noplan", "password", "pw")
	conn, err := f.db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(context.Background(), `PRAGMA foreign_keys=OFF`); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		plan int64
		want domain.Reason
	}{{0, domain.ReasonNoPlan}, {9999, domain.ReasonPlanRemoved}} {
		if _, err := conn.ExecContext(context.Background(), `UPDATE member SET plan_id=? WHERE id=?`, c.plan, np); err != nil {
			t.Fatal(err)
		}
		check(f.login("noplan", "pw"), c.want)
	}

	chap := radius.New(radius.CodeAccessRequest, []byte(f.nas.Secret))
	rfc2865.UserName_SetString(chap, "alice")
	rfc2865.CHAPChallenge_Set(chap, []byte("0123456789abcdef"))
	rfc2865.CHAPPassword_Set(chap, append([]byte{1}, make([]byte, 16)...))
	ms := radius.New(radius.CodeAccessRequest, []byte(f.nas.Secret))
	rfc2865.UserName_SetString(ms, "alice")
	microsoft.MSCHAPChallenge_Add(ms, []byte("0123456789abcdef"))
	microsoft.MSCHAPResponse_Add(ms, make([]byte, 50))
	for _, p := range []*radius.Packet{chap, ms} {
		r, err := f.nas.Exchange(p)
		if err != nil {
			t.Fatal(err)
		}
		check(r, domain.ReasonUnsupportedAuth)
	}

	// Every reason is covered.
	for _, r := range domain.AllReasons() {
		if !seen[r] {
			t.Errorf("reason %s was not exercised", r)
		}
	}
}

// E12.08 one listener: HSP_HTTP_ADDR is ignored (nothing listens there), /healthz is on the admin listener
// without a login, and the old page API routes are 404 even for a logged-in admin.
func TestE12_08_OnlyTheAdminListener(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	old := l.Addr().String()
	l.Close() // a free port that the old public listener would have used
	s := harness.Start(t, harness.Options{Env: map[string]string{"HTTP_ADDR": old}})
	if c, err := net.DialTimeout("tcp", old, 300*time.Millisecond); err == nil {
		c.Close()
		t.Fatalf("something listens on the old public address %s", old)
	}
	resp, body := harness.Get(t, "http://"+s.Addrs.Admin+"/healthz")
	if resp.StatusCode != 200 || strings.TrimSpace(body) != "ok" || resp.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("/healthz: %d %q cache=%q", resp.StatusCode, body, resp.Header.Get("Cache-Control"))
	}
	c := harness.LoginAsAdmin(t, s)
	for _, p := range []string{"/api/v1/account", "/api/v1/config", "/api/v1/devices/aabbccddeeff/credentials", "/api/v1/nope"} {
		if r, _ := harness.Get(t, "http://"+s.Addrs.Admin+p); r.StatusCode != 404 {
			t.Errorf("anonymous %s: %d, want 404", p, r.StatusCode)
		}
		if r := c.Get(p); r.StatusCode != 404 {
			t.Errorf("admin %s: %d, want 404", p, r.StatusCode)
		}
	}
}

// E12.09 migration 0004 drops the three page-API settings columns and keeps every other value.
func TestE12_09_Migration0004(t *testing.T) {
	root := harness.ModuleRoot()
	read := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join(root, "internal/store/migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	old := fstest.MapFS{
		"0001_init.sql":              {Data: read("0001_init.sql")},
		"0002_usage_daily.sql":       {Data: read("0002_usage_daily.sql")},
		"0003_admin_must_change.sql": {Data: read("0003_admin_must_change.sql")},
	}
	s := harness.Start(t, harness.Options{Migrations: old})
	db := harness.DB(t, s.DataDir)
	cols := func() map[string]bool {
		rows, err := db.Query(`SELECT name FROM pragma_table_info('settings')`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		m := map[string]bool{}
		for rows.Next() {
			var n string
			rows.Scan(&n)
			m[n] = true
		}
		return m
	}
	gone := []string{"mac_auto_login", "after_login_url", "public_base_url"}
	for _, g := range gone {
		if !cols()[g] {
			t.Fatalf("before 0004 the column %s should exist", g)
		}
	}
	if _, err := db.Exec(`UPDATE settings SET site_title='Kept Cafe', timezone='Asia/Tehran', coa_port=3800, voucher_help='help text',
		mac_auto_login=1, after_login_url='https://example.com/x', public_base_url='http://10.0.0.2:8080'`); err != nil {
		t.Fatal(err)
	}
	secret := scalar[string](t, db, `SELECT radius_shared_secret FROM settings`)
	s.Stop()

	s2 := harness.Start(t, harness.Options{DataDir: s.DataDir, SecretKey: s.SecretKey})
	defer s2.Stop()
	if n := scalar[int](t, db, `SELECT count(*) FROM schema_migrations`); n != wantMigrations {
		t.Errorf("%d migrations, want %d", n, wantMigrations)
	}
	after := cols()
	for _, g := range gone {
		if after[g] {
			t.Errorf("column %s still exists", g)
		}
	}
	if scalar[string](t, db, `SELECT site_title FROM settings`) != "Kept Cafe" || scalar[string](t, db, `SELECT timezone FROM settings`) != "Asia/Tehran" ||
		scalar[int](t, db, `SELECT coa_port FROM settings`) != 3800 || scalar[string](t, db, `SELECT voucher_help FROM settings`) != "help text" ||
		scalar[string](t, db, `SELECT radius_shared_secret FROM settings`) != secret || scalar[int](t, db, `SELECT count(*) FROM admin`) != 1 {
		t.Error("existing data did not survive the migration")
	}
}

// E12.10 the setup script has no walled-garden and no html-directory lines; it keeps login-by=http-pap and
// tells the admin that the downloaded hotspot folder replaces the default one.
func TestE12_10_SetupScript(t *testing.T) {
	f := newP8(t)
	script, _ := f.script()
	for _, bad := range []string{"walled-garden", "walled garden", "html-directory", "public base URL"} {
		if strings.Contains(strings.ToLower(script), strings.ToLower(bad)) {
			t.Errorf("script still mentions %q:\n%s", bad, script)
		}
	}
	for _, want := range []string{"/radius add", "/radius incoming set accept=yes", "login-by=http-pap", "radius-accounting=yes", "radius-interim-update=",
		"replace the default hotspot folder", "flash/hotspot"} {
		if !strings.Contains(script, want) {
			t.Errorf("script lacks %q:\n%s", want, script)
		}
	}
}

// ---- Phase 12b: router page designs (E12.00 to E12.06) ----

var themeKeys = []string{"air", "midnight", "aurora", "dusk", "ripple", "sunrise", "ocean"}

// packs downloads the 8 zips through the admin panel (key -> path inside hotspot/ -> bytes).
func packs(t *testing.T) (*harness.AdminClient, map[string]map[string][]byte) {
	t.Helper()
	s := harness.Start(t, harness.Options{})
	c := harness.LoginAsAdmin(t, s)
	out := map[string]map[string][]byte{}
	for _, key := range append([]string{"mikrotik"}, themeKeys...) {
		r := c.Get("/admin/router/pages/" + key + ".zip")
		if r.StatusCode != 200 {
			t.Fatalf("%s: download %d", key, r.StatusCode)
		}
		zr, err := zip.NewReader(bytes.NewReader([]byte(r.Body)), int64(len(r.Body)))
		if err != nil {
			t.Fatalf("%s: not a zip: %v", key, err)
		}
		files := map[string][]byte{}
		for _, zf := range zr.File {
			rel, ok := strings.CutPrefix(zf.Name, "hotspot/")
			if !ok || rel == "" {
				t.Fatalf("%s: entry %q is not under hotspot/", key, zf.Name)
			}
			rc, err := zf.Open()
			if err != nil {
				t.Fatal(err)
			}
			b, _ := io.ReadAll(rc)
			rc.Close()
			files[rel] = b
		}
		out[key] = files
	}
	return c, out
}

func sortedNames(m map[string][]byte) []string {
	var n []string
	for k := range m {
		n = append(n, k)
	}
	slices.Sort(n)
	return n
}

var themedFiles = []string{"alogin.html", "api.json", "css/hotspot.css", "error.html", "errors.txt", "favicon.ico", "js/busy.js", "login.html", "logout.html",
	"radvert.html", "redirect.html", "rlogin.html", "status.html", "xml/WISPAccessGatewayParam.xsd", "xml/alogin.html", "xml/error.html", "xml/flogout.html",
	"xml/login.html", "xml/logout.html", "xml/rlogin.html"}

var visiblePages = []string{"login.html", "alogin.html", "status.html", "logout.html", "error.html"}

// E12.00 the mikrotik download is the MikroTik default folder, byte for byte (the embedded copy equals reference/hotspot), under hotspot/.
func TestE12_00_MikrotikZipIsTheReference(t *testing.T) {
	_, all := packs(t)
	ref := filepath.Join(harness.ModuleRoot(), "reference", "hotspot")
	if _, err := os.Stat(ref); err != nil {
		t.Skip("reference/hotspot is not in this checkout")
	}
	want := map[string][]byte{}
	err := filepath.WalkDir(ref, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		rel, _ := filepath.Rel(ref, p)
		want[filepath.ToSlash(rel)] = b
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	got := all["mikrotik"]
	if strings.Join(sortedNames(got), ",") != strings.Join(sortedNames(want), ",") {
		t.Fatalf("files %v, want %v", sortedNames(got), sortedNames(want))
	}
	for n, b := range want {
		if !bytes.Equal(got[n], b) {
			t.Errorf("%s differs from reference/hotspot", n)
		}
	}
}

// E12.01 each themed zip has exactly the expected files under hotspot/, no md5.js, nothing from outside, and is small.
func TestE12_01_FileListsAndNoExternalRequests(t *testing.T) {
	_, all := packs(t)
	urlRE := regexp.MustCompile(`https?://[^\s"'<>]*`)
	for _, key := range themeKeys {
		files := all[key]
		if got := sortedNames(files); strings.Join(got, ",") != strings.Join(themedFiles, ",") {
			t.Errorf("%s: files %v, want %v", key, got, themedFiles)
		}
		if _, has := files["md5.js"]; has {
			t.Errorf("%s: md5.js must not ship", key)
		}
		size := 0
		for n, b := range files {
			if strings.HasPrefix(n, "xml/") {
				continue // WISPr files and their schema carry namespace URLs
			}
			size += len(b)
			for _, u := range urlRE.FindAllString(string(b), -1) {
				// allowed: the router's own name and the XML namespace, both only in the WISPr comment of rlogin.html
				if !strings.HasPrefix(u, "http://$(hostname)/") && u != "http://www.w3.org/2001/XMLSchema-instance" {
					t.Errorf("%s: %s names an outside address %q", key, n, u)
				}
			}
			if strings.Contains(string(b), "@import") || strings.Contains(string(b), "@font-face") {
				t.Errorf("%s: %s loads fonts or other styles", key, n)
			}
		}
		if size > 30*1024 {
			t.Errorf("%s: %d bytes of pages, want under 30 KB (without xml/ and logo)", key, size)
		}
	}
}

func attr(d *goquery.Document, sel, name string) string { return d.Find(sel).First().AttrOr(name, "") }

func doc(t *testing.T, b []byte) *goquery.Document {
	t.Helper()
	d, err := goquery.NewDocumentFromReader(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// E12.02 the RouterOS template syntax each page needs, and every if has its endif (also in the MikroTik default pack).
func TestE12_02_RouterOSSyntax(t *testing.T) {
	_, all := packs(t)
	ifRE, endRE := regexp.MustCompile(`\$\(if[ )]`), regexp.MustCompile(`\$\(endif\)`)
	for key, files := range all {
		for n, b := range files {
			if strings.HasSuffix(n, ".ico") || strings.HasSuffix(n, ".xsd") {
				continue
			}
			if a, e := len(ifRE.FindAll(b, -1)), len(endRE.FindAll(b, -1)); a != e {
				t.Errorf("%s/%s: %d $(if) but %d $(endif)", key, n, a, e)
			}
		}
	}
	for _, key := range themeKeys {
		f := all[key]
		login := doc(t, f["login.html"])
		form := login.Find("form").First()
		if form.AttrOr("action", "") != "$(link-login-only)" || !strings.EqualFold(form.AttrOr("method", ""), "post") {
			t.Errorf("%s: login form action %q method %q", key, form.AttrOr("action", ""), form.AttrOr("method", ""))
		}
		if v := attr(login, `form input[name=dst]`, "value"); v != "$(link-orig)" || attr(login, `form input[name=dst]`, "type") != "hidden" {
			t.Errorf("%s: hidden dst = %q", key, v)
		}
		if v := attr(login, `form input[name=popup]`, "value"); v != "false" || attr(login, `form input[name=popup]`, "type") != "hidden" {
			t.Errorf("%s: hidden popup = %q", key, v)
		}
		if attr(login, `form input[name=username]`, "value") != "$(username)" || attr(login, `form input[name=password]`, "type") != "password" || login.Find("form input[name=username]").Length() != 1 {
			t.Errorf("%s: username/password fields wrong", key)
		}
		if login.Find(`input[name=username]`).First().Parent().Is("label") == false || login.Find("h1").Length() != 1 || attr(login, "html", "lang") != "en" {
			t.Errorf("%s: login needs a label around the username input, one h1 and lang=en", key)
		}
		if strings.Contains(string(f["login.html"]), "chap") || strings.Contains(string(f["login.html"]), "md5") {
			t.Errorf("%s: login.html has CHAP code", key)
		}
		al := string(f["alogin.html"])
		if !strings.Contains(al, `url=$(link-status)"`) || !strings.Contains(al, `href="$(link-status)"`) || strings.Contains(al, "popup") || strings.Contains(al, "link-redirect") {
			t.Errorf("%s: alogin.html must go to $(link-status) with no popup:\n%s", key, al)
		}
		st := string(f["status.html"])
		for _, want := range []string{"$(link-logout)", "$(username)", "$(uptime)", "$(session-time-left)", "$(remain-bytes-total)", "$(if refresh-timeout)", "$(refresh-timeout-secs)"} {
			if !strings.Contains(st, want) {
				t.Errorf("%s: status.html lacks %s", key, want)
			}
		}
		for _, n := range []string{"logout.html", "error.html"} {
			if !strings.Contains(string(f[n]), "$(link-login)") {
				t.Errorf("%s: %s lacks $(link-login)", key, n)
			}
		}
		if !strings.Contains(string(f["error.html"]), "$(error)") {
			t.Errorf("%s: error.html lacks $(error)", key)
		}
		for _, n := range visiblePages {
			if attr(doc(t, f[n]), `link[rel=stylesheet]`, "href") != "css/hotspot.css" {
				t.Errorf("%s: %s does not link css/hotspot.css", key, n)
			}
		}
		if !strings.Contains(string(f["errors.txt"]), "radius-reply = $(error-orig)") {
			t.Errorf("%s: errors.txt lacks the radius-reply line", key)
		}
	}
}

// E12.03 the MESSAGES block of login.html maps every reject code of the domain to text, with the fallback; the preview resolves it.
func TestE12_03_MessagesBlock(t *testing.T) {
	c, all := packs(t)
	for _, key := range themeKeys {
		login := string(all[key]["login.html"])
		i := strings.Index(login, "MESSAGES - translate here")
		if i < 0 {
			t.Fatalf("%s: no MESSAGES block comment", key)
		}
		block := login[i:]
		block = block[:strings.Index(block, "</p>")]
		for _, r := range domain.AllReasons() {
			want := fmt.Sprintf("error == %q)%s", string(r), html.EscapeString(r.Message()))
			if !strings.Contains(block, want) {
				t.Errorf("%s: MESSAGES lacks %q", key, want)
			}
		}
		if !strings.Contains(block, "$(elif error)$(error)$(endif)") {
			t.Errorf("%s: MESSAGES lacks the fallback", key)
		}
		if strings.Count(block, "$(if ") != 1 || strings.Count(block, "$(endif)") != 1 {
			t.Errorf("%s: MESSAGES must be one if/elif chain", key)
		}
	}
	// what a guest sees (preview resolves the tags)
	for _, r := range domain.AllReasons() {
		b := c.Get("/admin/router/pages/air/preview?error=" + string(r)).Body
		if d := doc(t, []byte(b)); !strings.Contains(d.Find(`p[role=alert]`).Text(), r.Message()) {
			t.Errorf("preview with error=%s shows %q, want %q", r, d.Find(`p[role=alert]`).Text(), r.Message())
		}
	}
	if b := c.Get("/admin/router/pages/air/preview?error=radius+is+down").Body; !strings.Contains(doc(t, []byte(b)).Find(`p[role=alert]`).Text(), "radius is down") {
		t.Error("an unknown error is shown as it is")
	}
	if b := c.Get("/admin/router/pages/air/preview").Body; strings.Contains(b, `role="alert"`) {
		t.Error("no error, no alert")
	}
}

// E12.04 one theme block per CSS, the same layout everywhere, no colors outside the theme block, and body data-theme matches.
func TestE12_04_OneThemeBlockEach(t *testing.T) {
	_, all := packs(t)
	themeRE := regexp.MustCompile(`data-theme="([a-z]+)"`)
	colorRE := regexp.MustCompile(`(?i)#[0-9a-f]{3,8}\b|rgba?\(|hsla?\(`)
	var layout string
	for _, key := range themeKeys {
		css := string(all[key]["css/hotspot.css"])
		keys := map[string]bool{}
		for _, m := range themeRE.FindAllStringSubmatch(css, -1) {
			keys[m[1]] = true
		}
		if len(keys) != 1 || !keys[key] {
			t.Errorf("%s: CSS has theme blocks %v, want only %s", key, keys, key)
		}
		before, _, ok := strings.Cut(css, "/* ===== THEME ===== */")
		if !ok {
			t.Fatalf("%s: no theme marker", key)
		}
		if layout == "" {
			layout = before
		} else if before != layout {
			t.Errorf("%s: layout differs from the other themes", key)
		}
		if colorRE.MatchString(before) {
			t.Errorf("%s: hard-coded color in the layout part: %q", key, colorRE.FindString(before))
		}
		for _, n := range visiblePages {
			if got := attr(doc(t, all[key][n]), "body", "data-theme"); got != key {
				t.Errorf("%s: %s has body data-theme %q", key, n, got)
			}
		}
	}
}

// E12.05 no inline style, handler, script body or style tag in the pages, only the optional busy.js, and the pages work without scripts.
func TestE12_05_NoInlineCodeAndWorksWithoutJS(t *testing.T) {
	c, all := packs(t)
	for _, key := range themeKeys {
		for _, n := range visiblePages {
			d := doc(t, all[key][n])
			if bad := pageIsClean(d); len(bad) > 0 {
				t.Errorf("%s/%s: %v", key, n, bad)
			}
			d.Find("script").Each(func(_ int, s *goquery.Selection) {
				if s.AttrOr("src", "") != "js/busy.js" || n != "login.html" {
					t.Errorf("%s/%s: unexpected script %q", key, n, s.AttrOr("src", ""))
				}
			})
		}
		busy := string(all[key]["js/busy.js"])
		for _, bad := range []string{"fetch(", "XMLHttpRequest", "localStorage", "document.cookie", "http"} {
			if strings.Contains(busy, bad) {
				t.Errorf("%s: busy.js uses %q", key, bad)
			}
		}
		// A plain POST form does the login: the form needs no script to submit.
		login := doc(t, all[key]["login.html"])
		if login.Find(`form button[type=submit]`).Length() != 1 {
			t.Errorf("%s: login has no submit button", key)
		}
		for _, p := range []string{"login", "alogin", "status", "logout", "error"} {
			r := c.Get("/admin/router/pages/" + key + "/preview?page=" + p)
			csp := r.Header.Get("Content-Security-Policy")
			if r.StatusCode != 200 || strings.Contains(r.Body, "$(") || !strings.Contains(csp, "default-src 'none'") || strings.Contains(csp, "script-src") {
				t.Errorf("%s/%s preview: %d, unresolved tags or weak CSP %q", key, p, r.StatusCode, csp)
			}
		}
	}
	// the status preview shows the sample values and hides nothing that was set
	st := c.Get("/admin/router/pages/air/preview?page=status").Body
	for _, want := range []string{"Hi, guest42", "12m 30s", "47m 30s", "Log out", "Continue"} {
		if !strings.Contains(st, want) {
			t.Errorf("status preview lacks %q", want)
		}
	}
}

// E12.06 the admin Router page lists 8 cards, downloads need a login and are zips with a good name, the preview works.
func TestE12_06_AdminRouterPage(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	c := harness.LoginAsAdmin(t, s)
	d := c.Get("/admin/router").Doc(t)
	cards := d.Find("[data-design]")
	var got []string
	cards.Each(func(_ int, sel *goquery.Selection) { got = append(got, sel.AttrOr("data-design", "")) })
	want := append([]string{"mikrotik"}, themeKeys...)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("cards %v, want %v", got, want)
	}
	anon := harness.NewAdminClient(t, s)
	for _, key := range want {
		card := d.Find(fmt.Sprintf(`[data-design="%s"]`, key))
		dl := card.Find("a[data-download]").AttrOr("href", "")
		if dl != "/admin/router/pages/"+key+".zip" || card.Find("iframe").AttrOr("src", "") != "/admin/router/pages/"+key+"/preview" {
			t.Errorf("%s: download %q, iframe %q", key, dl, card.Find("iframe").AttrOr("src", ""))
		}
		r := c.Get(dl)
		if r.StatusCode != 200 || r.Header.Get("Content-Type") != "application/zip" || r.Header.Get("Content-Disposition") != `attachment; filename="hotspot-`+key+`.zip"` {
			t.Errorf("%s: %d %q %q", key, r.StatusCode, r.Header.Get("Content-Type"), r.Header.Get("Content-Disposition"))
		}
		if r := anon.Get(dl); r.StatusCode == 200 || strings.HasPrefix(r.Body, "PK") {
			t.Errorf("%s: anonymous download got %d", key, r.StatusCode)
		}
		pv := c.Get(card.Find("iframe").AttrOr("src", ""))
		if pv.StatusCode != 200 || !strings.HasPrefix(pv.Header.Get("Content-Type"), "text/html") || !strings.Contains(pv.Header.Get("Content-Security-Policy"), "form-action 'none'") {
			t.Errorf("%s: preview %d %q", key, pv.StatusCode, pv.Header.Get("Content-Type"))
		}
		if r := anon.Get("/admin/router/pages/" + key + "/preview"); r.StatusCode == 200 {
			t.Errorf("%s: anonymous preview got 200", key)
		}
	}
	// the preview's own stylesheet loads; other files and unknown things do not
	if r := c.Get("/admin/router/pages/air/css/hotspot.css"); r.StatusCode != 200 || !strings.HasPrefix(r.Header.Get("Content-Type"), "text/css") || !strings.Contains(r.Body, `data-theme="air"`) {
		t.Errorf("preview css: %d %q", r.StatusCode, r.Header.Get("Content-Type"))
	}
	if r := c.Get("/admin/router/pages/mikrotik/css/style.css"); r.StatusCode != 200 {
		t.Errorf("mikrotik preview css: %d", r.StatusCode)
	}
	for _, p := range []string{"/admin/router/pages/nope.zip", "/admin/router/pages/nope/preview", "/admin/router/pages/air/preview?page=bogus",
		"/admin/router/pages/air/login.html", "/admin/router/pages/air/js/busy.js", "/admin/router/pages/air/css/missing.css", "/admin/router/pages/air/css/nodot", "/admin/router/pages/air/css/", "/admin/router/pages/air/img/x",
		"/admin/router/files.zip", "/admin/router/themes"} {
		if r := c.Get(p); r.StatusCode != 404 {
			t.Errorf("%s: %d, want 404", p, r.StatusCode)
		}
	}
	for _, text := range []string{"flash/hotspot", "MESSAGES", "errors.txt", "login.html"} {
		if !strings.Contains(c.Get("/admin/router").Body, text) {
			t.Errorf("router page help lacks %q", text)
		}
	}
}
