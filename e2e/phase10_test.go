package e2e

import (
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"hotspotpluslight/e2e/harness"
)

// admin pages that need data: a plan, a member and a batch.
func adminPages(t *testing.T, c *harness.AdminClient) []string {
	t.Helper()
	plan := c.CreatePlan(harness.PlanForm("Basic"))
	member := c.CreateMember(harness.MemberForm(plan, "alice"))
	r := c.Send("POST", "/admin/batches", url.Values{"plan_id": {strconv.FormatInt(plan, 10)}, "count": {"2"}})
	if r.StatusCode != http.StatusCreated {
		t.Fatalf("create batch: %d", r.StatusCode)
	}
	themes := c.Get("/admin/router/themes").Doc(t).Find("a[data-detail]").First().AttrOr("href", "")
	if themes == "" {
		t.Fatal("no theme link")
	}
	m := "/admin/members/" + strconv.FormatInt(member, 10)
	return []string{
		"/admin/members", "/admin/members/new", m, m + "/voucher",
		"/admin/plans", "/admin/plans/new", "/admin/plans/" + strconv.FormatInt(plan, 10),
		"/admin/batches", "/admin/batches/new", r.Header.Get("Location"),
		"/admin/sessions", "/admin/usage", "/admin/router", "/admin/router/themes", themes, "/admin/settings",
	}
}

// pageIsClean reports inline style, inline handlers, hx-on and inline scripts/styles in a page.
func pageIsClean(d *goquery.Document) []string {
	var bad []string
	d.Find("*").Each(func(_ int, s *goquery.Selection) {
		for _, a := range s.Get(0).Attr {
			k := strings.ToLower(a.Key)
			if k == "style" || strings.HasPrefix(k, "on") || strings.HasPrefix(k, "hx-on") {
				bad = append(bad, goquery.NodeName(s)+"["+k+"]")
			}
		}
	})
	d.Find("script").Each(func(_ int, s *goquery.Selection) {
		if _, has := s.Attr("src"); !has {
			bad = append(bad, "inline <script>")
		}
	})
	if n := d.Find("style").Length(); n > 0 {
		bad = append(bad, "inline <style>")
	}
	return bad
}

// E10.01 no inline style, onclick, hx-on or inline script on any admin page (a strict CSP would pass).
func TestE10_01_NoInlineCodeOrStyle(t *testing.T) {
	s := harness.Start(t, harness.Options{Env: map[string]string{"DEV": "1"}})
	anon := harness.NewAdminClient(t, s)
	if bad := pageIsClean(anon.Get("/admin/login").Doc(t)); len(bad) > 0 {
		t.Errorf("/admin/login: %v", bad)
	}
	c := harness.LoginAsAdmin(t, s)
	pages := append(adminPages(t, c), "/admin/dev/components")
	for _, p := range pages {
		r := c.Get(p)
		if r.StatusCode != 200 {
			t.Errorf("%s: %d", p, r.StatusCode)
			continue
		}
		if bad := pageIsClean(r.Doc(t)); len(bad) > 0 {
			t.Errorf("%s: %v", p, bad)
		}
	}
}

// E10.02 the theme cookie sets data-theme on <html>; a bad value falls back to air.
func TestE10_02_ThemeCookie(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	c := harness.LoginAsAdmin(t, s)
	anon := harness.NewAdminClient(t, s)
	get := func(path, theme string) string {
		req, _ := http.NewRequest("GET", c.Base+path, nil)
		if theme != "" {
			req.AddCookie(&http.Cookie{Name: "theme", Value: theme})
		}
		resp, err := c.HTTP.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		d, err := goquery.NewDocumentFromReader(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return d.Find("html").AttrOr("data-theme", "")
	}
	for _, tc := range []struct{ cookie, want string }{
		{"", "air"}, {"air", "air"}, {"air-dark", "air-dark"}, {"dark", "air"}, {"light", "air"}, {`"><script>`, "air"},
	} {
		if got := get("/admin/members", tc.cookie); got != tc.want {
			t.Errorf("cookie %q: data-theme %q, want %q", tc.cookie, got, tc.want)
		}
	}
	// The login page honours the cookie too.
	req, _ := http.NewRequest("GET", anon.Base+"/admin/login", nil)
	req.AddCookie(&http.Cookie{Name: "theme", Value: "air-dark"})
	resp, err := anon.HTTP.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	d, _ := goquery.NewDocumentFromReader(resp.Body)
	if d.Find("html").AttrOr("data-theme", "") != "air-dark" {
		t.Error("login page ignores the theme cookie")
	}
}

var hashed = regexp.MustCompile(`\?v=[0-9a-f]{12}$`)

// E10.03 assets carry a content hash and are cached for a year; pages are never cached.
func TestE10_03_AssetHashAndCache(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	c := harness.LoginAsAdmin(t, s)
	page := c.Get("/admin/members")
	if page.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("page Cache-Control %q", page.Header.Get("Cache-Control"))
	}
	d := page.Doc(t)
	var urls []string
	d.Find(`link[rel=stylesheet]`).Each(func(_ int, s *goquery.Selection) { urls = append(urls, s.AttrOr("href", "")) })
	d.Find(`script[src]`).Each(func(_ int, s *goquery.Selection) { urls = append(urls, s.AttrOr("src", "")) })
	if len(urls) < 3 {
		t.Fatalf("assets: %v", urls)
	}
	for _, u := range urls {
		if !hashed.MatchString(u) || !strings.HasPrefix(u, "/admin/static/") {
			t.Errorf("asset URL %q has no content hash", u)
			continue
		}
		r := c.Get(u)
		if r.StatusCode != 200 || !strings.Contains(r.Header.Get("Cache-Control"), "max-age=31536000") || !strings.Contains(r.Header.Get("Cache-Control"), "immutable") {
			t.Errorf("%s: %d Cache-Control %q", u, r.StatusCode, r.Header.Get("Cache-Control"))
		}
	}
	// Without the hash, or with a stale one, the browser must revalidate.
	for _, u := range []string{"/admin/static/app.css", "/admin/static/app.css?v=000000000000"} {
		if cc := c.Get(u).Header.Get("Cache-Control"); strings.Contains(cc, "immutable") {
			t.Errorf("%s: Cache-Control %q", u, cc)
		}
	}
	// The stylesheet is one bundle: the @imports are inlined and the font URL is hashed.
	css := c.Get(urls[0])
	if strings.Contains(css.Body, "@import") || !strings.Contains(css.Body, ".c-btn") || !strings.Contains(css.Body, "@layer tokens, base, components, utilities") {
		t.Error("app.css is not the inlined bundle")
	}
	if !regexp.MustCompile(`inter-latin-wght-normal\.woff2\?v=[0-9a-f]{12}`).MatchString(css.Body) {
		t.Error("font URL in the bundle has no hash")
	}
	// Unknown files are 404; the source css files are not served.
	for _, u := range []string{"/admin/static/nothing.js", "/admin/static/css/tokens.css"} {
		if r := c.Get(u); r.StatusCode != 404 {
			t.Errorf("%s: %d, want 404", u, r.StatusCode)
		}
	}
}

// E10.04 the component gallery exists only with HSP_DEV=1.
func TestE10_04_GalleryOnlyInDev(t *testing.T) {
	off := harness.Start(t, harness.Options{})
	if r := harness.LoginAsAdmin(t, off).Get("/admin/dev/components"); r.StatusCode != 404 {
		t.Errorf("without HSP_DEV: %d, want 404", r.StatusCode)
	}
	on := harness.Start(t, harness.Options{Env: map[string]string{"DEV": "1"}})
	oc := harness.LoginAsAdmin(t, on)
	r := oc.Get("/admin/dev/components")
	if r.StatusCode != 200 {
		t.Fatalf("with HSP_DEV=1: %d", r.StatusCode)
	}
	d := r.Doc(t)
	for _, want := range []string{".c-btn", ".c-icon-btn", ".c-field", ".c-input", ".c-select", ".c-search", ".c-chip", ".c-card", ".c-table", ".c-meter", ".c-pill", ".c-pager", ".c-empty", ".c-menu", ".c-sheet", ".c-modal", ".c-credentials", ".c-code", ".c-chart"} {
		if d.Find(want).Length() == 0 {
			t.Errorf("gallery lacks %s", want)
		}
	}
	// Still behind the login.
	if r := harness.NewAdminClient(t, on).Get("/admin/dev/components"); r.StatusCode != 303 {
		t.Errorf("anonymous gallery: %d, want 303", r.StatusCode)
	}
}

// E10.05 the shell keeps the menu: 2 groups, 7 links, the theme button and Log out in the top bar.
func TestE10_05_ShellKeepsMenu(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	c := harness.LoginAsAdmin(t, s)
	d := c.Get("/admin/plans").Doc(t)
	if d.Find(`#sidebar a[href="/admin/plans"][aria-current="page"]`).Length() != 1 {
		t.Error("active item not marked")
	}
	if d.Find("header [data-theme-toggle]").Length() != 1 || d.Find(`header button[hx-post="/admin/logout"]`).Length() != 1 {
		t.Error("theme button and Log out must be in the top bar")
	}
	if d.Find("header [data-nav-toggle]").Length() != 1 {
		t.Error("no menu button for small screens")
	}
	if d.Find("dialog#confirm-modal").Length() != 1 || d.Find("#toast[role=status]").Length() != 1 {
		t.Error("shared confirm modal or toast area missing")
	}
}
