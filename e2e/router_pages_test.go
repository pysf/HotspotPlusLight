package e2e

import (
	"archive/zip"
	"bytes"
	"fmt"
	"html"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"layeh.com/radius"
	"layeh.com/radius/rfc2866"

	"hotspotpluslight/e2e/fakenas"
	"hotspotpluslight/e2e/harness"
	"hotspotpluslight/internal/clock"
	"hotspotpluslight/internal/domain"
)

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

func portOf(t *testing.T, addr string) string {
	t.Helper()
	_, p, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// E8.03 the setup script carries the secret, ports, interval and CoA port, and follows settings.
func TestE8_03_SetupScript(t *testing.T) {
	f := newP8(t)
	f.exec(`UPDATE settings SET accounting_interval_seconds=120, coa_port=3799, radius_shared_secret='Sup3r-Secret-Value'`)
	script, _ := f.script()
	for _, want := range []string{
		"/radius add",
		"service=hotspot",
		"address=<SERVER-IP>", // no public base URL any more: the admin fills in the server address (Phase 12a)
		`secret="Sup3r-Secret-Value"`,
		"authentication-port=" + portOf(t, f.s.Addrs.RadiusAuth),
		"accounting-port=" + portOf(t, f.s.Addrs.RadiusAcct),
		"timeout=5s",
		"/radius incoming set accept=yes port=3799",
		"use-radius=yes",
		"login-by=http-pap",
		"radius-accounting=yes",
		"radius-interim-update=120s",
		"VERIFY-ON-HARDWARE",
		"extracted hotspot folder",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("script lacks %q\n%s", want, script)
		}
	}
	// match the port as a whole number, so a random port that is a substring of other digits does not trip the check
	adminPort := regexp.MustCompile(`(^|[^0-9])` + portOf(t, f.s.Addrs.Admin) + `([^0-9]|$)`)
	if adminPort.MatchString(script) {
		t.Error("the admin port appears in the script")
	}
	// every RouterOS command line sits under a comment, and every unverified command says so
	lines := strings.Split(script, "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, "/") && (i == 0 || !strings.HasPrefix(lines[i-1], "#")) {
			t.Errorf("command without a comment line above it: %q", l)
		}
	}
	// settings changes show immediately
	f.exec(`UPDATE settings SET accounting_interval_seconds=300, coa_port=3800, radius_shared_secret='another-one'`)
	script, _ = f.script()
	if !strings.Contains(script, "radius-interim-update=300s") || !strings.Contains(script, "port=3800") || !strings.Contains(script, `secret="another-one"`) || strings.Contains(script, "Sup3r-Secret-Value") {
		t.Errorf("script did not follow the settings:\n%s", script)
	}
}

// E8.03b characters RouterOS treats specially in the secret are escaped, and the server address stays a placeholder.
func TestE8_03b_ScriptVariants(t *testing.T) {
	f := newP8(t)
	f.exec(`UPDATE settings SET radius_shared_secret=?`, `a"b$c\d?e`)
	script, _ := f.script()
	if !strings.Contains(script, `secret="a\"b\$c\\d\?e"`) {
		t.Errorf("secret not escaped for RouterOS:\n%s", script)
	}
	// /radius needs an address, not a name: say so instead of guessing.
	if !strings.Contains(script, "address=<SERVER-IP>") {
		t.Errorf("the RADIUS address must be a placeholder:\n%s", script)
	}
	// a blind paste must not half-apply: no executable line may contain a placeholder
	for _, l := range strings.Split(script, "\n") {
		if strings.Contains(l, "<") && !strings.HasPrefix(l, "#") {
			t.Errorf("placeholder in an executable line: %q", l)
		}
	}
	if !strings.Contains(script, "# EDIT THEN UNCOMMENT") {
		t.Error("commented-out commands lack the edit marker")
	}
}

// E8.03c the page needs a login.
func TestE8_03c_RouterPageNeedsLogin(t *testing.T) {
	f := newP8(t)
	anon := harness.NewAdminClient(t, f.s)
	if r := anon.Get("/admin/router"); r.StatusCode == 200 {
		t.Errorf("anonymous GET /admin/router: %d", r.StatusCode)
	}
}

// E8.04 every download (all 8): a zip under hotspot/, no server template tokens, no plain-passwd anywhere,
// no password in a URL. (Rewritten in Phase 12b for the 8 designs; the file lists are E12.01.)
func TestE8_04_PackContents(t *testing.T) {
	f := newP8(t)
	for _, key := range []string{"mikrotik", "air", "midnight", "aurora", "dusk", "ripple", "sunrise", "ocean"} {
		path := "/admin/router/pages/" + key + ".zip"
		r := f.c.Get(path)
		if r.StatusCode != 200 || r.Header.Get("Content-Type") != "application/zip" {
			t.Fatalf("%s: %d %s", path, r.StatusCode, r.Header.Get("Content-Type"))
		}
		zr, err := zip.NewReader(bytes.NewReader([]byte(r.Body)), int64(len(r.Body)))
		if err != nil {
			t.Fatal(err)
		}
		for _, zf := range zr.File {
			if !strings.HasPrefix(zf.Name, "hotspot/") {
				t.Errorf("%s: %s is outside hotspot/", path, zf.Name)
			}
			rc, err := zf.Open()
			if err != nil {
				t.Fatal(err)
			}
			b, _ := io.ReadAll(rc)
			rc.Close()
			if bytes.Contains(b, []byte("plain-passwd")) {
				t.Errorf("%s: %s contains plain-passwd", path, zf.Name)
			}
			if strings.HasSuffix(zf.Name, ".html") {
				body := string(b)
				for _, tok := range []string{"{{", "}}", "{%", "%}", "<no value>", "%!", "@THEME@", "@MESSAGES@"} {
					if strings.Contains(body, tok) {
						t.Errorf("%s: %s contains the template token %q", path, zf.Name, tok)
					}
				}
				if regexp.MustCompile(`(?i)[?&;](pap|password|passwd)=`).MatchString(body) {
					t.Errorf("%s: %s puts a password into a URL", path, zf.Name)
				}
			}
		}
	}
}

// E8.07 the router link status panel (on the dashboard since Phase 18, D28): last Access-Request / Accounting /
// Disconnect with time and source, bad shared secrets counted with their source; in memory only (no DB writes, gone after a restart).
func TestE8_07_LinkStatusPanel(t *testing.T) {
	f := newP8(t)
	panel := func() *goquery.Document { return f.c.Get("/admin").Doc(t) }

	d := panel()
	for _, k := range []string{"access-request", "accounting", "disconnect"} {
		if got := link(t, d, k); got.at != "never" || got.from != "" {
			t.Errorf("%s before any traffic: %+v", k, got)
		}
	}
	if n := text(d.Find("[data-bad-secret-count]")); n != "0" {
		t.Errorf("bad secret count %q", n)
	}

	f.member(f.plan("Basic"), "alice")
	f.db.SetMaxOpenConns(1) // data_version is per connection
	version := func() int64 { return scalar[int64](t, f.db, `PRAGMA data_version`) }
	_ = version() // data_version only reports commits of OTHER connections once it has been read once
	// The admin session row moves its sliding expiry on every admin request, so that one table is left out of the comparisons.
	const sliding = "admin_session"

	// Reading the panel and its fragment writes nothing.
	reads := dbSnapshot(t, f.db, sliding)
	panel()
	f.c.Get("/admin/dashboard/link-status")
	if diff := snapshotDiff(reads, dbSnapshot(t, f.db, sliding)); diff != "" {
		t.Errorf("reading the panel changed: %s", diff)
	}

	// A rejected login writes its failed-login count (D27, Phase 17) and nothing else.
	failures := func() int64 { return scalar[int64](t, f.db, `SELECT coalesce(sum(count), 0) FROM login_failure_daily`) }
	others, nFail := dbSnapshot(t, f.db, sliding, "login_failure_daily"), failures()
	f.fc.Advance(time.Minute) // 12:01:00
	if r, err := f.nas.Authenticate("nobody", "x"); err != nil || r.Code != radius.CodeAccessReject {
		t.Fatalf("login: %v %v", r, err)
	}
	if diff := snapshotDiff(others, dbSnapshot(t, f.db, sliding, "login_failure_daily")); diff != "" {
		t.Errorf("a rejected login changed more than login_failure_daily: %s", diff)
	}
	if got := failures(); got != nFail+1 {
		t.Errorf("failed-login count %d, want %d", got, nFail+1)
	}

	// Packets that change nothing in the database: a bad accounting authenticator, a wrong secret, an interim of an unknown session.
	quiet, before := dbSnapshot(t, f.db, sliding), version()
	f.fc.Advance(time.Minute) // 12:02:00
	badAcct := fakenas.New(f.s.Addrs.RadiusAuth, "wrong-secret")
	badAcct.AcctAddr, badAcct.Timeout = f.s.Addrs.RadiusAcct, 300*time.Millisecond
	for range 2 {
		if _, err := badAcct.Account(fakenas.Acct{Type: rfc2866.AcctStatusType_Value_Start, User: "alice", SessionID: "x"}); err == nil {
			t.Fatal("bad accounting authenticator answered")
		}
	}
	f.fc.Advance(time.Minute) // 12:03:00
	badAuth := fakenas.New(f.s.Addrs.RadiusAuth, "wrong-secret")
	badAuth.Timeout = 300 * time.Millisecond
	if _, err := badAuth.Authenticate("alice", "pw123456"); err == nil {
		t.Fatal("a wrong-secret Access-Request was answered")
	}
	badAuth.MessageAuth = false // the heuristic path: no Message-Authenticator to check
	if _, err := badAuth.Authenticate("alice", "pw123456"); err == nil {
		t.Fatal("a wrong-secret Access-Request without Message-Authenticator was answered")
	}
	f.fc.Advance(time.Minute) // 12:04:00
	if r, err := f.nas.Account(fakenas.Acct{Type: rfc2866.AcctStatusType_Value_InterimUpdate, User: "nobody", SessionID: "u1"}); err != nil || r.Code != radius.CodeAccountingResponse {
		t.Fatalf("accounting: %v %v", r, err)
	}
	if after := version(); after != before {
		t.Errorf("the database changed (data_version %d -> %d): link status must be in memory only", before, after)
	}
	if diff := snapshotDiff(quiet, dbSnapshot(t, f.db, sliding)); diff != "" {
		t.Errorf("bad-secret, bad-accounting and unknown-session packets changed: %s", diff)
	}

	d = panel()
	if got := link(t, d, "access-request"); got.at != "2026-11-01 12:01:00" || got.from != "127.0.0.1" {
		t.Errorf("last Access-Request: %+v", got)
	}
	if got := link(t, d, "accounting"); got.at != "2026-11-01 12:04:00" || got.from != "127.0.0.1" {
		t.Errorf("last Accounting: %+v", got)
	}
	if got := link(t, d, "disconnect"); got.at != "never" {
		t.Errorf("last Disconnect: %+v", got)
	}
	// 2 bad accounting authenticators + 2 wrong-secret Access-Requests (with and without Message-Authenticator), all from this machine
	if n := text(d.Find("[data-bad-secret-count]")); n != "4" {
		t.Errorf("bad secret count %q, want 4", n)
	}
	src := d.Find("[data-bad-secret-from]")
	if src.Length() != 1 || src.AttrOr("data-bad-secret-from", "") != "127.0.0.1" || text(src.Find("[data-count]")) != "4" || text(src.Find("[data-at]")) != "2026-11-01 12:03:00" {
		t.Errorf("bad secret sources: %s", text(d.Find("[data-bad-secret-sources]")))
	}

	// a Disconnect: ACK
	f.online("alice", "s1", "192.168.88.10", "AA:BB:CC:DD:EE:01")
	f.fc.Advance(time.Minute) // 12:05:00
	if r := f.c.Send("POST", spath(f.sessionDBID("s1")), nil); r.StatusCode != 200 {
		t.Fatalf("kick: %d", r.StatusCode)
	}
	d = panel()
	got := link(t, d, "disconnect")
	if got.at != "2026-11-01 12:05:00" || got.from != "127.0.0.1" || !strings.Contains(text(d.Find(`[data-link="disconnect"]`)), "ACK") {
		t.Errorf("last Disconnect: %+v / %s", got, text(d.Find(`[data-link="disconnect"]`)))
	}
}

// E8.07b the panel (on the dashboard) is empty again after a restart.
func TestE8_07b_PanelResetsOnRestart(t *testing.T) {
	fc := clock.NewFake(t0)
	s := harness.Start(t, harness.Options{Clock: fc})
	db := harness.DB(t, s.DataDir)
	secret := scalar[string](t, db, `SELECT radius_shared_secret FROM settings`)
	nas := fakenas.New(s.Addrs.RadiusAuth, secret)
	if _, err := nas.Authenticate("x", "y"); err != nil {
		t.Fatal(err)
	}
	c := harness.LoginAsAdmin(t, s)
	if got := link(t, c.Get("/admin").Doc(t), "access-request"); got.from != "127.0.0.1" {
		t.Fatalf("before restart: %+v", got)
	}
	s.Stop()
	s2 := harness.Start(t, harness.Options{DataDir: s.DataDir, SecretKey: s.SecretKey, Clock: fc})
	c2 := harness.NewAdminClient(t, s2)
	c2.Login("admin", harness.AdminPassword)
	if got := link(t, c2.Get("/admin").Doc(t), "access-request"); got.at != "never" {
		t.Errorf("after restart: %+v", got)
	}
}

// E8.12 the router page: no-store, the copy behaviour comes from the static script (no inline script).
func TestE8_12_RouterPageAssets(t *testing.T) {
	f := newP8(t)
	r := f.c.Get("/admin/router")
	if r.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control %q", r.Header.Get("Cache-Control"))
	}
	d := r.Doc(t)
	clip := ""
	d.Find("script[src]").Each(func(_ int, s *goquery.Selection) {
		if src := s.AttrOr("src", ""); strings.HasPrefix(src, "/admin/static/js/clipboard.js?v=") {
			clip = src
		}
	})
	if clip == "" {
		t.Error("page does not load /admin/static/js/clipboard.js?v=<hash>")
	}
	d.Find("script").Each(func(_ int, s *goquery.Selection) {
		if _, has := s.Attr("src"); !has {
			t.Errorf("inline script: %s", s.Text())
		}
	})
	if d.Find("button[data-copy-target]").Length() != 1 || d.Find("button[onclick]").Length() != 0 {
		t.Error("copy button must be markup-only with data-copy-target")
	}
	js := f.c.Get(clip)
	if js.StatusCode != 200 || !strings.Contains(js.Body, "data-copy-target") {
		t.Errorf("clipboard.js: %d", js.StatusCode)
	}
}

// E8.13 the link status panel refreshes itself: the dashboard polls /admin/dashboard/link-status, a fragment
// with the three rows that needs a login and shows a new packet without a full page load.
func TestE8_13_LinkStatusPolling(t *testing.T) {
	f := newP8(t)
	const path = "/admin/dashboard/link-status"

	anon := harness.NewAdminClient(t, f.s)
	if r := anon.Get(path); r.StatusCode != http.StatusSeeOther || r.Header.Get("Location") != "/admin/login" {
		t.Errorf("anonymous: %d %q", r.StatusCode, r.Header.Get("Location"))
	}

	panel := f.c.Get("/admin").Doc(t).Find("[data-link-status]")
	if panel.AttrOr("hx-get", "") != path || panel.AttrOr("hx-trigger", "") != "every 5s" || panel.AttrOr("hx-swap", "") != "outerHTML" {
		t.Errorf("panel attributes: hx-get=%q hx-trigger=%q hx-swap=%q",
			panel.AttrOr("hx-get", ""), panel.AttrOr("hx-trigger", ""), panel.AttrOr("hx-swap", ""))
	}

	r := f.c.Get(path)
	if r.StatusCode != 200 || r.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("fragment: %d Cache-Control %q", r.StatusCode, r.Header.Get("Cache-Control"))
	}
	d := r.Doc(t)
	if strings.Contains(strings.ToLower(r.Body), "<html") || d.Find("[data-link-status]").Length() != 1 {
		t.Error("the response must be the panel fragment, not a full page")
	}
	for _, k := range []string{"access-request", "accounting", "disconnect"} {
		if got := link(t, d, k); got.at != "never" {
			t.Errorf("%s before any traffic: %+v", k, got)
		}
	}

	// A new Access-Request shows up in the fragment without loading the full page again.
	if _, err := f.nas.Authenticate("nobody", "x"); err != nil {
		t.Fatal(err)
	}
	if got := link(t, f.c.Get(path).Doc(t), "access-request"); got.at == "never" || got.from != "127.0.0.1" {
		t.Errorf("after an Access-Request: %+v", got)
	}
}
