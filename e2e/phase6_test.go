package e2e

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"hotspotpluslight/e2e/harness"
)

// p6 is the Phase 6 fixture: the Phase 3 fixture (fake clock, admin client, fake router, log capture)
// (the page API tests were removed in Phase 12a).
type p6 struct{ *p3 }

func newP6(t *testing.T) *p6 { return &p6{newP3(t)} }

func (f *p6) setSettings(kv ...string) {
	f.t.Helper()
	form := harness.FormValues(f.t, f.c.Get("/admin/settings").Doc(f.t), "form#settings-form")
	for i := 0; i+1 < len(kv); i += 2 {
		if kv[i+1] == "-" {
			form.Del(kv[i])
		} else {
			form.Set(kv[i], kv[i+1])
		}
	}
	if r := f.c.Send("PUT", "/admin/settings", form); r.StatusCode != 200 {
		f.t.Fatalf("settings: %d %s", r.StatusCode, r.Body)
	}
}

// ---- admin side: starter pack and themes ----

// pack downloads files.zip (optionally for a theme) and returns its entries by name.
func (f *p6) pack(theme string) (resp harness.Resp, files map[string][]byte) {
	f.t.Helper()
	path := "/admin/router/files.zip"
	if theme != "" {
		path += "?theme=" + url.QueryEscape(theme)
	}
	resp = f.c.Get(path)
	if resp.StatusCode != 200 {
		return resp, nil
	}
	zr, err := zip.NewReader(bytes.NewReader([]byte(resp.Body)), int64(len(resp.Body)))
	if err != nil {
		f.t.Fatalf("%s: not a zip: %v", path, err)
	}
	files = map[string][]byte{}
	for _, zf := range zr.File {
		rc, err := zf.Open()
		if err != nil {
			f.t.Fatal(err)
		}
		b, _ := io.ReadAll(rc)
		rc.Close()
		files[zf.Name] = b
	}
	return resp, files
}

var packFiles = []string{"login.html", "alogin.html", "status.html", "logout.html", "error.html", "redirect.html", "rlogin.html", "background.jpg", "logo.png"}

// reasonCodes are all message codes the page must be able to translate (PLAN 7.6 plus the API's own codes).
var reasonCodes = []string{"INVALID_CREDENTIALS", "INACTIVE_USER", "NO_PLAN", "PLAN_REMOVED", "EXPIRED", "NO_ACTIVE_SUBSCRIPTION",
	"OUT_OF_BULK", "OUT_OF_TIME", "NO_MORE_SESSION", "UNSUPPORTED_AUTH_METHOD", "GENERAL_ERROR", "TOO_MANY_ATTEMPTS"}

// E6.14 theme registry: exactly 38 themes in 6 categories, manifests, backgrounds and colors as in PLAN section 16.
func TestE6_14_ThemeRegistry(t *testing.T) {
	f := newP6(t)
	if len(expectedThemes) != 38 {
		t.Fatalf("test table has %d themes", len(expectedThemes))
	}
	d := f.c.Get("/admin/router/themes").Doc(t)
	if n := d.Find("[data-category]").Length(); n != 6 {
		t.Fatalf("%d categories, want 6", n)
	}
	if n := d.Find("body [data-theme]").Length(); n != 38 {
		t.Fatalf("%d themes listed, want 38", n)
	}
	for _, e := range expectedThemes {
		card := d.Find(fmt.Sprintf(`[data-category="%s"] [data-theme="%s"]`, e.Category, e.ID))
		if card.Length() != 1 {
			t.Errorf("%s: %d cards under category %s", e.ID, card.Length(), e.Category)
			continue
		}
		if got := text(card.Find("[data-title]")); got != e.Title {
			t.Errorf("%s: title %q, want %q", e.ID, got, e.Title)
		}
		if got := card.AttrOr("data-primary", ""); got != e.Primary {
			t.Errorf("%s: primary %q, want %q", e.ID, got, e.Primary)
		}
		if got := card.AttrOr("data-overlay", ""); got != e.Overlay {
			t.Errorf("%s: overlay %q, want %q", e.ID, got, e.Overlay)
		}
		bg := f.c.Get(card.Find("img").AttrOr("src", "/missing"))
		if bg.StatusCode != 200 || len(bg.Body) != e.BackgroundBytes || !strings.HasPrefix(bg.Body, "\xff\xd8\xff") || bg.Header.Get("Content-Type") != "image/jpeg" {
			t.Errorf("%s: background %d, %d bytes (want %d), type %q", e.ID, bg.StatusCode, len(bg.Body), e.BackgroundBytes, bg.Header.Get("Content-Type"))
		}
	}
}

// E6.15 each of the 38 downloads is a valid zip with that theme's colors, background and the shared layout.
// Also E6.11 (lint) and E6.18 (disconnected hint) for every theme.
func TestE6_15_ThemeDownloads(t *testing.T) {
	f := newP6(t)
	var shared string
	for _, e := range expectedThemes {
		resp, files := f.pack(e.ID)
		if resp.StatusCode != 200 {
			t.Errorf("%s: %d", e.ID, resp.StatusCode)
			continue
		}
		if ct := resp.Header.Get("Content-Type"); ct != "application/zip" {
			t.Errorf("%s: Content-Type %q", e.ID, ct)
		}
		if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "attachment") || !strings.Contains(cd, e.ID+".zip") {
			t.Errorf("%s: Content-Disposition %q", e.ID, cd)
		}
		for _, name := range packFiles {
			if len(files["hotspot/"+name]) == 0 {
				t.Errorf("%s: hotspot/%s missing or empty", e.ID, name)
			}
		}
		if len(files["hotspot/background.jpg"]) != e.BackgroundBytes {
			t.Errorf("%s: background.jpg %d bytes, want %d", e.ID, len(files["hotspot/background.jpg"]), e.BackgroundBytes)
		}
		login := string(files["hotspot/login.html"])
		// colors as CSS custom properties at the top (PLAN 9.4)
		if !strings.Contains(login, "--primary-color: "+e.Primary+";") || !strings.Contains(login, "--overlay-color: "+e.Overlay+";") {
			t.Errorf("%s: colors not in login.html", e.ID)
		}
		if !strings.Contains(login, `url("background.jpg")`) && !strings.Contains(login, `url(background.jpg)`) {
			t.Errorf("%s: login.html does not use background.jpg", e.ID)
		}
		// shared layout elements (T2)
		for _, id := range []string{"logo", "site-title", "login-form", "footer", "error-message"} {
			if !strings.Contains(login, `id="`+id+`"`) {
				t.Errorf("%s: layout element #%s missing", e.ID, id)
			}
		}
		for _, field := range []string{`name="username"`, `name="password"`, `type="submit"`} {
			if !strings.Contains(login, field) {
				t.Errorf("%s: form lacks %s", e.ID, field)
			}
		}
		// E6.11 lint
		for _, tok := range []string{"{{", "}}", "{%", "%}", "<no value>", "%!"} {
			if strings.Contains(login, tok) {
				t.Errorf("%s: unresolved template token %q", e.ID, tok)
			}
		}
		if strings.Contains(login, "HSP_API_BASE") || strings.Contains(login, "fetch(") {
			t.Errorf("%s: the page must not call the server (Phase 12a)", e.ID)
		}
		for _, code := range reasonCodes {
			if n := len(regexp.MustCompile(`(?m)^\s*`+code+`:\s*"`).FindAllString(login, -1)); n != 1 {
				t.Errorf("%s: %s has %d dictionary entries, want 1 (English only)", e.ID, code, n)
			}
		}
		if regexp.MustCompile(`[\x{0600}-\x{06FF}]`).MatchString(login) {
			t.Errorf("%s: the page contains Arabic-script text; it ships English only", e.ID)
		}
		// E6.18 the disconnected hint and its dictionary key
		if !strings.Contains(login, "If you were disconnected, your plan's data or time may be used up. Try logging in again to see why.") {
			t.Errorf("%s: hint line missing", e.ID)
		}
		if n := len(regexp.MustCompile(`(?m)^\s*DISCONNECTED_HINT:\s*"`).FindAllString(login, -1)); n != 1 {
			t.Errorf("%s: DISCONNECTED_HINT has %d dictionary entries, want 1", e.ID, n)
		}
		if !strings.Contains(login, `data-i18n="DISCONNECTED_HINT"`) {
			t.Errorf("%s: hint not bound to its key", e.ID)
		}
		// The themes share ONE layout: apart from the two colors the markup is identical.
		neutral := strings.NewReplacer(e.Primary, "P", e.Overlay, "O").Replace(login)
		if shared == "" {
			shared = neutral
		} else if neutral != shared {
			t.Errorf("%s: login.html differs from the shared layout beyond the colors", e.ID)
		}
		// router pages: no server tokens, and nothing that leaks a password into a URL (PLAN 9.4).
		for _, name := range packFiles {
			if strings.HasSuffix(name, ".html") {
				if body := string(files["hotspot/"+name]); strings.Contains(body, "plain-passwd") || strings.Contains(body, "{{") {
					t.Errorf("%s: %s contains plain-passwd or a template token", e.ID, name)
				}
			}
		}
	}
}

// E6.16 no theme => alpha-fall; unknown id => 404.
func TestE6_16_DefaultAndUnknownTheme(t *testing.T) {
	f := newP6(t)
	_, def := f.pack("")
	_, fall := f.pack("alpha-fall")
	if len(def) == 0 || !bytes.Equal(def["hotspot/login.html"], fall["hotspot/login.html"]) || !bytes.Equal(def["hotspot/background.jpg"], fall["hotspot/background.jpg"]) {
		t.Error("default pack is not alpha-fall")
	}
	if _, other := f.pack("alpha-space"); bytes.Equal(other["hotspot/background.jpg"], fall["hotspot/background.jpg"]) {
		t.Error("different themes must differ")
	}
	for _, id := range []string{"nope", "alpha-nope", "../alpha-fall", "Alpha-Fall"} {
		if r, _ := f.pack(id); r.StatusCode != 404 {
			t.Errorf("theme %q: %d, want 404", id, r.StatusCode)
		}
	}
	if r := f.c.Get("/admin/router/themes/nope"); r.StatusCode != 404 {
		t.Errorf("detail of an unknown theme: %d", r.StatusCode)
	}
	// Admin only.
	anon := harness.NewAdminClient(t, f.s)
	for _, p := range []string{"/admin/router", "/admin/router/themes", "/admin/router/files.zip"} {
		if r := anon.Get(p); r.StatusCode != 303 || r.Header.Get("Location") != "/admin/login" {
			t.Errorf("anonymous %s: %d %s", p, r.StatusCode, r.Header.Get("Location"))
		}
	}
}

// E6.17 the gallery lists all 38 grouped by category, and every preview renders.
func TestE6_17_Gallery(t *testing.T) {
	f := newP6(t)
	d := f.c.Get("/admin/router/themes").Doc(t)
	counts := map[string]int{"Alpha": 3, "Restaurant": 5, "Cafe": 7, "Hotel": 9, "University": 7, "Market": 7}
	for cat, n := range counts {
		if got := d.Find(fmt.Sprintf(`[data-category="%s"] [data-theme]`, cat)).Length(); got != n {
			t.Errorf("category %s lists %d themes, want %d", cat, got, n)
		}
	}
	for _, e := range expectedThemes {
		card := d.Find(fmt.Sprintf(`[data-theme="%s"]`, e.ID))
		href := card.Find("a[data-detail]").AttrOr("href", "")
		if href != "/admin/router/themes/"+e.ID {
			t.Errorf("%s: detail link %q", e.ID, href)
			continue
		}
		page := f.c.Get(href)
		if page.StatusCode != 200 {
			t.Errorf("%s: detail page %d", e.ID, page.StatusCode)
			continue
		}
		pd := page.Doc(t)
		if dl := pd.Find("a[data-download]").AttrOr("href", ""); dl != "/admin/router/files.zip?theme="+e.ID {
			t.Errorf("%s: download link %q", e.ID, dl)
		}
		prev := f.c.Get(pd.Find("iframe[data-preview]").AttrOr("src", "/missing"))
		if prev.StatusCode != 200 || !strings.HasPrefix(prev.Header.Get("Content-Type"), "text/html") || !strings.Contains(prev.Body, e.Primary) {
			t.Errorf("%s: preview %d %q", e.ID, prev.StatusCode, prev.Header.Get("Content-Type"))
		}
		// The preview runs on the admin origin: sandboxed (iframe attribute and CSP), no API base, no network.
		if got := pd.Find("iframe[data-preview]").AttrOr("sandbox", ""); got != "allow-scripts" {
			t.Errorf("%s: iframe sandbox %q", e.ID, got)
		}
		csp := prev.Header.Get("Content-Security-Policy")
		for _, want := range []string{"sandbox allow-scripts", "default-src 'none'", "connect-src 'none'", "img-src 'self'"} {
			if !strings.Contains(csp, want) {
				t.Errorf("%s: preview CSP %q lacks %q", e.ID, csp, want)
			}
		}
		if strings.Contains(csp, "allow-same-origin") {
			t.Errorf("%s: preview must be opaque-origin", e.ID)
		}
		// The preview's relative references resolve next to it.
		src := pd.Find("iframe[data-preview]").AttrOr("src", "")
		base := src[:strings.LastIndex(src, "/")+1]
		for _, asset := range []string{"background.jpg", "logo.png"} {
			if r := f.c.Get(base + asset); r.StatusCode != 200 || len(r.Body) == 0 {
				t.Errorf("%s: preview asset %s: %d", e.ID, asset, r.StatusCode)
			}
		}
	}
}

// E6.20 /admin/router: entry page with the download and gallery links.
func TestE6_20_RouterPage(t *testing.T) {
	f := newP6(t)
	r := f.c.Get("/admin/router")
	if r.StatusCode != 200 {
		t.Fatalf("status %d", r.StatusCode)
	}
	d := r.Doc(t)
	if d.Find(`a[href="/admin/router/files.zip"]`).Length() != 1 || d.Find(`a[href="/admin/router/themes"]`).Length() != 1 {
		t.Error("download and gallery links missing")
	}
	if d.Find("[data-warning=public-base-url]").Length() != 0 {
		t.Error("the public base URL warning is gone")
	}
}

// E6.12 starter page script against a router stub in a headless browser (optional per PLAN 10).
func TestE6_12_StarterPageScript(t *testing.T) {
	t.Skip("optional (PLAN 10, 11): needs a headless browser (Playwright), which this repository does not vendor; the script is covered by lint tests E6.11/E6.15/E6.18 and by the manual hardware checklist (PLAN 13.9)")
}
