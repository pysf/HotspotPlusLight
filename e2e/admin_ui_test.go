package e2e

import (
	"encoding/json"
	"io"
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
	m := "/admin/members/" + strconv.FormatInt(member, 10)
	return []string{
		"/admin", "/admin/members", "/admin/members/new", m, m + "/voucher",
		"/admin/plans", "/admin/plans/new", "/admin/plans/" + strconv.FormatInt(plan, 10),
		"/admin/batches", "/admin/batches/new", r.Header.Get("Location"),
		"/admin/sessions", "/admin/usage", "/admin/router", "/admin/settings",
	}
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

// E10.02 the theme cookie sets data-theme on <html> (light or dark); no cookie or a bad value gives "auto" (the OS decides).
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
		{"", "auto"}, {"light", "light"}, {"dark", "dark"}, {"air", "auto"}, {"air-dark", "auto"}, {`"><script>`, "auto"},
	} {
		if got := get("/admin/members", tc.cookie); got != tc.want {
			t.Errorf("cookie %q: data-theme %q, want %q", tc.cookie, got, tc.want)
		}
	}
	// The login page honours the cookie too.
	req, _ := http.NewRequest("GET", anon.Base+"/admin/login", nil)
	req.AddCookie(&http.Cookie{Name: "theme", Value: "dark"})
	resp, err := anon.HTTP.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	d, _ := goquery.NewDocumentFromReader(resp.Body)
	if d.Find("html").AttrOr("data-theme", "") != "dark" {
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
	// The stylesheet is one bundle: the @imports are inlined; there is no font file (system fonts).
	css := c.Get(urls[0])
	if strings.Contains(css.Body, "@import") || !strings.Contains(css.Body, ".c-btn") || !strings.Contains(css.Body, "@layer tokens, base, components, utilities") {
		t.Error("app.css is not the inlined bundle")
	}
	if strings.Contains(css.Body, "@font-face") || strings.Contains(css.Body, "woff2") {
		t.Error("the bundle must use system fonts only")
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
	for _, want := range []string{".c-btn", ".c-icon-btn", ".c-field", ".c-input", ".c-select", ".c-search", ".c-chip", ".c-card", ".c-table", ".c-meter", ".c-pill", ".c-pager", ".c-empty", ".c-menu", ".c-sheet", ".c-modal", ".c-credentials", ".c-code", ".c-chart__plot"} {
		if d.Find(want).Length() == 0 {
			t.Errorf("gallery lacks %s", want)
		}
	}
	// Still behind the login.
	if r := harness.NewAdminClient(t, on).GetPage("/admin/dev/components"); r.StatusCode != 303 {
		t.Errorf("anonymous gallery: %d, want 303", r.StatusCode)
	}
}

// E10.05 the shell keeps the menu: 8 links, the theme button and Log out in the top bar, and the phone tab bar with More.
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
	if d.Find(`nav.l-tabbar > a.l-tabbar__tab`).Length() != 4 || d.Find(`nav.l-tabbar button[popovertarget="more-menu"]`).Length() != 1 || d.Find(`#more-menu a`).Length() != 4 {
		t.Error("the phone tab bar needs 4 tabs and a More menu with 4 links")
	}
	if d.Find(`#more-menu a[href="/admin/plans"][aria-current="page"]`).Length() != 1 || d.Find(`nav.l-tabbar button[popovertarget="more-menu"][aria-current="page"]`).Length() != 1 {
		t.Error("More is not marked current on a page that lives in it")
	}
	if d.Find("dialog#confirm-modal").Length() != 1 || d.Find("#toast[role=status]").Length() != 1 {
		t.Error("shared confirm modal or toast area missing")
	}
}

// E10.21 errors keep going to #flash; the rest of the UI hooks: toasts, busy state, no inline code.
func TestE10_21_ToastsAndHooks(t *testing.T) {
	f := newP2(t)
	p := f.plan("Basic")
	f.member(p, "alice")
	for _, path := range []string{"/admin/members", "/admin/plans"} {
		d := f.c.Get(path).Doc(t)
		if d.Find("#toast[role=status]").Length() != 1 || d.Find("#flash").Length() != 1 {
			t.Errorf("%s: toast or flash area missing", path)
		}
		if bad := pageIsClean(d); len(bad) > 0 {
			t.Errorf("%s: %v", path, bad)
		}
	}
	// Fragments are clean too (a strict CSP would pass).
	for _, c := range []struct{ path, target string }{
		{"/admin/members", listTarget}, {"/admin/members/new", sheetTarget}, {mpath(f.idOf("alice")), sheetTarget},
		{"/admin/plans", planTarget}, {"/admin/plans/new", sheetTarget},
	} {
		r := f.c.GetHX(c.path, c.target)
		if bad := pageIsClean(fragmentDoc(t, r)); len(bad) > 0 {
			t.Errorf("fragment %s: %v", c.path, bad)
		}
	}
	// JS: dialog.js opens the sheet; the sheet title is set from the fragment.
	js := f.c.Get(f.c.Get("/admin/members").Doc(t).Find(`script[src*="js/dialog.js"]`).AttrOr("src", "")).Body
	if !strings.Contains(js, "sheet-body") {
		t.Error("dialog.js does not handle the sheet body")
	}
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

// Sidebar: one flat list of 8 links and every link has an icon from the sprite.
func TestE1_20_SidebarGroupsAndIcons(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	c := harness.LoginAsAdmin(t, s)
	d := c.Get("/admin/members").Doc(t)
	if d.Find("#sidebar a.l-nav__link").Length() != 8 {
		t.Errorf("want 8 nav links")
	}
	d.Find("#sidebar a.l-nav__link").Each(func(_ int, a *goquery.Selection) {
		if a.Find(`svg > use[href^="#i-"]`).Length() == 0 {
			t.Errorf("nav link %q has no icon", strings.TrimSpace(a.Text()))
		}
	})
}

// E16.11 (review B10.3) the htmx config swaps every status for which a handler renders an error fragment
// (400, 404, 422, 409, 429, 500, 502). Rules are read the way htmx does: the first rule whose code matches wins.
func TestE16_11_HtmxSwapsErrorFragments(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	c := harness.LoginAsAdmin(t, s)
	raw, ok := c.Get("/admin/plans").Doc(t).Find(`meta[name="htmx-config"]`).Attr("content")
	if !ok {
		t.Fatal("no htmx-config meta tag")
	}
	var cfg struct {
		ResponseHandling []struct {
			Code string `json:"code"`
			Swap bool   `json:"swap"`
		} `json:"responseHandling"`
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatal(err)
	}
	swaps := func(status int) bool {
		for _, rule := range cfg.ResponseHandling {
			if regexp.MustCompile("^(?:" + rule.Code + ")$").MatchString(strconv.Itoa(status)) {
				return rule.Swap
			}
		}
		return false
	}
	for _, st := range []int{400, 404, 409, 422, 429, 500, 502} {
		if !swaps(st) {
			t.Errorf("status %d is not swapped, so its error message would be dropped", st)
		}
	}
	// A redirect answer carries no body to swap.
	if swaps(401) || swaps(403) {
		t.Error("401 and 403 must not be swapped")
	}
}

// E16.15 (review B10.3) a polled fragment (the Online table) that gets a 404 must not have its content
// replaced by an error box: htmx is told not to swap and a toast is sent. A plain page load still gets the box.
func TestE16_15_PolledTargetErrorIsNotSwapped(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	c := harness.NewAdminClient(t, s)
	c.Login("admin", "admin")
	c.SetPassword("admin", "a much longer password", "a much longer password")
	r := c.SendHX("POST", "/admin/sessions/999999/disconnections", nil, "session-list")
	if r.StatusCode != http.StatusNotFound || r.Header.Get("HX-Reswap") != "none" || !strings.Contains(r.Header.Get("HX-Trigger"), `"toast"`) {
		t.Errorf("session-list: %d reswap %q trigger %q", r.StatusCode, r.Header.Get("HX-Reswap"), r.Header.Get("HX-Trigger"))
	}
	r = c.SendHX("POST", "/admin/sessions/999999/disconnections", nil, "flash")
	if r.Header.Get("HX-Reswap") != "none" {
		t.Errorf("flash: reswap %q", r.Header.Get("HX-Reswap"))
	}
	r = c.Send("POST", "/admin/sessions/999999/disconnections", nil)
	if r.StatusCode != http.StatusNotFound || r.Header.Get("HX-Reswap") != "" {
		t.Errorf("no target: %d reswap %q", r.StatusCode, r.Header.Get("HX-Reswap"))
	}
}
