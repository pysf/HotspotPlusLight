package e2e

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"

	"hotspotpluslight/e2e/fakenas"
	"hotspotpluslight/e2e/harness"
)

// setExpiry puts an exact expires_at on a member (unix seconds).
func (f *p2) setExpiry(id int64, at time.Time) {
	f.t.Helper()
	f.exec(`UPDATE member SET expires_at=? WHERE id=?`, at.Unix(), id)
}

// rowStatuses maps username to data-status for the rows of a page or fragment.
func rowStatuses(d *goquery.Document) map[string]string {
	out := map[string]string{}
	d.Find("tr[data-member]").Each(func(_ int, s *goquery.Selection) {
		out[s.AttrOr("data-username", "")] = s.AttrOr("data-status", "")
	})
	return out
}

func chipCount(d *goquery.Document, chip string) string {
	return text(d.Find(`#member-chips [data-chip="` + chip + `"] .c-chip__count`))
}

// expiryFixture: one member per boundary of BR-24, all at t0.
func expiryFixture(t *testing.T) (*p2, map[string]int64) {
	t.Helper()
	f := newP2(t)
	p := f.plan("Basic")
	ids := map[string]int64{}
	add := func(name string, at *time.Time, disabled bool) {
		kv := []string{}
		if disabled {
			kv = append(kv, "active", "-")
		}
		id := f.member(p, name, kv...)
		ids[name] = id
		if at != nil {
			f.setExpiry(id, *at)
		}
	}
	d := func(x time.Duration) *time.Time { v := t0.Add(x); return &v }
	soon := 72 * time.Hour
	add("noexpiry", nil, false)
	add("past", d(-time.Second), false)
	add("now", d(0), false)
	add("in3d", d(soon), false)
	add("in3dplus", d(soon+time.Second), false)
	add("far", d(30*24*time.Hour), false)
	add("offsoon", d(time.Hour), true)
	add("offpast", d(-time.Hour), true)
	add("off", nil, true)
	return f, ids
}

var wantExpiry = map[string]string{
	"noexpiry": "active", "past": "expired", "now": "expiring", "in3d": "expiring", "in3dplus": "active",
	"far": "active", "offsoon": "inactive", "offpast": "expired", "off": "inactive",
}

// E10.10 BR-24: the expiring-soon rule at every boundary, in the rows and in the status filter.
func TestE10_10_ExpiringSoonBoundaries(t *testing.T) {
	f, _ := expiryFixture(t)
	got := rowStatuses(f.c.Get("/admin/members?per=100").Doc(t))
	for name, want := range wantExpiry {
		if got[name] != want {
			t.Errorf("%s: status %q, want %q", name, got[name], want)
		}
	}
	for _, st := range []string{"active", "expiring", "expired", "inactive"} {
		list := rowStatuses(f.c.Get("/admin/members?per=100&status=" + st).Doc(t))
		for name, want := range wantExpiry {
			_, listed := list[name]
			if listed != (want == st) {
				t.Errorf("status=%s: %s listed=%v, status is %s", st, name, listed, want)
			}
		}
	}
	// The pill text: Disabled for inactive, Expiring for expiring.
	d := f.c.Get("/admin/members?per=100").Doc(t)
	for name, label := range map[string]string{"off": "Disabled", "now": "Expiring", "past": "Expired", "noexpiry": "Active"} {
		if got := text(d.Find(`tr[data-username="` + name + `"] [data-col="status"] .c-pill`)); got != label {
			t.Errorf("%s: pill %q, want %q", name, got, label)
		}
	}
	// One second later "now" has expired and in3d is 3 days minus 1 s away.
	f.fc.Advance(time.Second)
	f.c.Get("/admin/plans")
	got = rowStatuses(f.c.Get("/admin/members?per=100").Doc(t))
	if got["now"] != "expired" || got["in3d"] != "expiring" || got["in3dplus"] != "expiring" {
		t.Errorf("after 1 s: %v", got)
	}
	// The member page and the login rule are not touched: an expiring member still logs in.
	if !strings.Contains(text(f.c.Get(mpath(f.idOf("in3d"))).Doc(t).Find("#member-status")), "status expiring") {
		t.Error("member page does not show the new status")
	}
}

// E10.11 the chips show counts under the current search and plan filter; the status is not applied to them.
func TestE10_11_ChipCounts(t *testing.T) {
	f, _ := expiryFixture(t)
	other := f.plan("Other")
	f.member(other, "zeta")
	d := f.c.GetHX("/admin/members?per=100", listTarget).Doc(t)
	for chip, want := range map[string]string{"all": "10", "active": "4", "expiring": "2", "expired": "2", "inactive": "2"} {
		if got := chipCount(d, chip); got != want {
			t.Errorf("chip %s: %q, want %q", chip, got, want)
		}
	}
	// The counts ignore the status filter but obey q.
	d = f.c.GetHX("/admin/members?per=100&status=expired&q=in3d", listTarget).Doc(t)
	for chip, want := range map[string]string{"all": "2", "active": "1", "expiring": "1", "expired": "0", "inactive": "0"} {
		if got := chipCount(d, chip); got != want {
			t.Errorf("q=in3d chip %s: %q, want %q", chip, got, want)
		}
	}
	// ... and the plan.
	d = f.c.GetHX("/admin/members?per=100&plan="+strconv.FormatInt(other, 10), listTarget).Doc(t)
	if chipCount(d, "all") != "1" || chipCount(d, "active") != "1" {
		t.Errorf("plan filter counts: all %q active %q", chipCount(d, "all"), chipCount(d, "active"))
	}
	// aria-current marks the chip of the current status; chips keep the filter in their link.
	d = f.c.GetHX("/admin/members?per=40&status=expiring&q=in", listTarget).Doc(t)
	if d.Find(`#member-chips [aria-current="true"]`).Length() != 1 || d.Find(`#member-chips [data-chip="expiring"][aria-current="true"]`).Length() != 1 {
		t.Error("exactly the expiring chip must be pressed")
	}
	href := d.Find(`#member-chips [data-chip="expired"]`).AttrOr("href", "")
	if !strings.Contains(href, "status=expired") || !strings.Contains(href, "q=in") || !strings.Contains(href, "per=40") {
		t.Errorf("chip link loses the filter: %q", href)
	}
	if d.Find(`#member-chips [data-chip="expired"]`).AttrOr("hx-get", "") != href || d.Find(`#member-chips [data-chip="all"]`).AttrOr("hx-push-url", "") != "true" {
		t.Error("chips must update the list with hx-get and hx-push-url")
	}
	var labels []string
	d.Find("#member-chips .c-chip").Each(func(_ int, s *goquery.Selection) { labels = append(labels, text(s.Clone().Children().Remove().End())) })
	if strings.Join(labels, "|") != "All|Active|Expiring soon|Expired|Disabled" {
		t.Errorf("chip labels %q", labels)
	}
}

// E10.12 the list is a fragment only when HX-Target names the list region.
func TestE10_12_ListFragmentOrFullPage(t *testing.T) {
	f, _ := expiryFixture(t)
	frag := f.c.GetHX("/admin/members?per=100", listTarget)
	if frag.StatusCode != 200 || strings.Contains(frag.Body, "<html") || strings.Contains(frag.Body, "<body") {
		t.Fatalf("fragment: %d, has page shell: %v", frag.StatusCode, strings.Contains(frag.Body, "<html"))
	}
	d := fragmentDoc(t, frag)
	if d.Find("tr[data-member]").Length() != 9 || d.Find("form#bulk-form").Length() != 1 {
		t.Errorf("fragment lacks rows or the bulk form")
	}
	if d.Find(`#member-chips[hx-swap-oob]`).Length() != 1 {
		t.Error("fragment lacks the out-of-band chips")
	}
	if d.Find("form#member-filter").Length() != 0 {
		t.Error("the toolbar must not be in the fragment (it would lose focus while typing)")
	}
	// The same URL, HX-Request on every request (the harness) but no list target: the full page.
	for _, target := range []string{"", "flash", "body", sheetTarget} {
		full := f.c.GetHX("/admin/members?per=100", target)
		fd := fragmentDoc(t, full)
		if fd.Find("html").Length() != 1 || fd.Find("#sidebar").Length() != 1 || fd.Find("form#member-filter").Length() != 1 {
			t.Errorf("target %q: not the full page", target)
		}
		list := fd.Find("#member-list")
		if list.Length() != 1 || list.AttrOr("aria-live", "") != "polite" || list.AttrOr("aria-busy", "") != "false" {
			t.Errorf("target %q: list region needs id, aria-live=polite, aria-busy=false", target)
		}
		if list.Find("tr[data-member]").Length() != 9 || fd.Find(`#member-chips`).Length() != 1 || fd.Find(`#member-chips[hx-swap-oob]`).Length() != 0 {
			t.Errorf("target %q: rows or in-page chips wrong", target)
		}
	}
	// The toolbar: debounced search with replace-sync, the plan select, per page, push-url.
	fd := f.c.Get("/admin/members").Doc(t)
	form := fd.Find("form#member-filter")
	if form.AttrOr("hx-get", "") != "/admin/members" || form.AttrOr("hx-target", "") != "#member-list" || form.AttrOr("hx-push-url", "") != "true" ||
		!strings.Contains(form.AttrOr("hx-sync", ""), "replace") || !strings.Contains(form.AttrOr("hx-trigger", ""), "delay:300ms") {
		t.Errorf("filter form attributes: %v", form.Get(0).Attr)
	}
	if form.Find(`input[type=search][name=q][data-search]`).Length() != 1 || form.Find(`select[name=plan]`).Length() != 1 || form.Find(`select[name=per]`).Length() != 1 {
		t.Error("toolbar lacks search, plan or per-page controls")
	}
	if form.Find(`input[type=hidden][name=status]`).Length() != 1 {
		t.Error("the filter form must carry the status so search keeps it")
	}
	// The list refreshes itself after members-changed, using the current query string.
	r := f.c.GetHX("/admin/members?per=10&status=expired&page=1", listTarget).Doc(t).Find(`[hx-trigger*="members-changed"]`)
	if r.Length() != 1 || !strings.Contains(r.AttrOr("hx-trigger", ""), "from:body") || r.AttrOr("hx-get", "") != "/admin/members?per=10&status=expired&page=1" || r.AttrOr("hx-target", "") != "#member-list" {
		t.Errorf("refresher: %v", r.Nodes)
	}
}

// E10.13 columns, test hooks and the row menu.
func TestE10_13_RowsAndMenu(t *testing.T) {
	f := newP2(t)
	p := f.plan("Fast", "speed_value", "5", "speed_unit", "Mbps", "bulk_value", "10", "bulk_unit", "GB")
	unl := f.plan("Free", "bulk_value", "0")
	f.member(p, "alice", "mobile", "09123456789")
	off := f.member(unl, "bob", "active", "-")
	f.exec(`UPDATE member SET used_download_bytes=?, expires_at=? WHERE username='alice'`, int64(1<<30), t0.Add(48*time.Hour).Unix())
	d := f.c.Get("/admin/members").Doc(t)
	var heads []string
	d.Find("table thead th").Each(func(_ int, s *goquery.Selection) { heads = append(heads, text(s)) })
	if strings.Join(heads, "|") != "|Username|Plan|Data used|Expires|Status|Online|Actions" {
		t.Errorf("headers %q", heads)
	}
	row := d.Find(`tr[data-member][data-username="alice"]`)
	cell := func(label string) *goquery.Selection { return row.Find(`td[data-label="` + label + `"]`) }
	if !strings.Contains(text(cell("Username")), "alice") || !strings.Contains(text(cell("Username")), "9123456789") || cell("Username").Find("a[href]").Length() != 1 {
		t.Errorf("user cell %q", text(cell("Username")))
	}
	if !strings.HasPrefix(text(cell("Plan")), "Fast") || text(cell("Plan").Find(".c-table__sub")) != "5 Mbps" {
		t.Errorf("plan cell %q", text(cell("Plan")))
	}
	m := cell("Data used").Find("meter.c-meter")
	if m.Length() != 1 || m.AttrOr("value", "") == "" || m.AttrOr("max", "") == "" || m.AttrOr("aria-label", "") == "" || m.Text() != "" {
		t.Errorf("usage meter: %v", m.Nodes)
	}
	if text(row.Find(`[data-col="data"]`)) != "1 GB / 10 GB 9 GB left" {
		t.Errorf("usage text %q", text(row.Find(`[data-col="data"]`)))
	}
	if got := text(cell("Expires")); !strings.Contains(got, "2026-11-03") || !strings.Contains(got, "in 2 days") {
		t.Errorf("expires cell %q", got)
	}
	if row.Find(`[data-col="status"] .c-pill[data-status="expiring"]`).Length() != 1 || row.Find(`[data-col="online"] .c-dot[role=img]`).AttrOr("aria-label", "") != "Offline" || text(row.Find(`[data-col="online"]`)) != "Offline" {
		t.Error("status cell lacks the pill, or the online cell lacks the dot and the word")
	}
	// Unlimited plan: no meter, "unlimited" text; no expiry: a dash.
	brow := d.Find(`tr[data-username="bob"]`)
	if brow.Find("meter").Length() != 0 || text(brow.Find(`[data-col="data"]`)) != "0 GB / unlimited" {
		t.Errorf("unlimited usage cell %q", text(brow.Find(`[data-col="data"]`)))
	}
	if text(brow.Find(`td[data-label="Expires"]`)) != "—" {
		t.Errorf("no-expiry cell %q", text(brow.Find(`td[data-label="Expires"]`)))
	}
	// The menu: items in order, destructive last after a divider, with server-rendered confirm text.
	items := func(r *goquery.Selection) []string {
		var out []string
		r.Find(".c-menu .c-menu__item").Each(func(_ int, s *goquery.Selection) { out = append(out, text(s)) })
		return out
	}
	if got := strings.Join(items(row), "|"); got != "Edit|Renew|Disable|Disconnect|Delete" {
		t.Errorf("alice menu %q", got)
	}
	if got := strings.Join(items(brow), "|"); got != "Edit|Renew|Enable|Disconnect|Delete" {
		t.Errorf("bob menu %q", got)
	}
	menu := row.Find(".c-menu")
	if menu.Find("hr.c-menu__sep").Length() != 1 || menu.Find(".c-menu__item").Last().AttrOr("data-danger", "x") == "x" || text(menu.Find(".c-menu__item").Last()) != "Delete" {
		t.Error("Delete must come last, after a divider, marked danger")
	}
	id := strconv.FormatInt(f.idOf("alice"), 10)
	chk := func(name, attr, want string) {
		it := menu.Find(`.c-menu__item:contains("` + name + `")`).First()
		if got := it.AttrOr(attr, ""); got != want {
			t.Errorf("%s %s = %q, want %q", name, attr, got, want)
		}
	}
	chk("Edit", "hx-get", "/admin/members/"+id)
	chk("Edit", "hx-target", "#sheet-body")
	// The username opens the same edit sheet. The link still goes to the detail page without JavaScript.
	if name := row.Find("a.c-table__main"); name.AttrOr("hx-get", "") != "/admin/members/"+id || name.AttrOr("hx-target", "") != "#sheet-body" || name.AttrOr("href", "") != "/admin/members/"+id {
		t.Errorf("the username link must open the edit sheet: hx-get %q, hx-target %q", name.AttrOr("hx-get", ""), name.AttrOr("hx-target", ""))
	}
	chk("Renew", "hx-post", "/admin/members/"+id+"/renewals")
	chk("Disable", "hx-post", "/admin/members/"+id+"/toggle")
	chk("Disconnect", "hx-post", "/admin/members/"+id+"/disconnections")
	chk("Delete", "hx-delete", "/admin/members/"+id)
	for _, n := range []string{"Renew", "Disconnect", "Delete", "Disable"} {
		if menu.Find(`.c-menu__item:contains("`+n+`")`).First().AttrOr("hx-confirm", "") == "" {
			t.Errorf("%s has no confirm text", n)
		}
	}
	if brow.Find(`.c-menu__item:contains("Enable")`).AttrOr("hx-post", "") != "/admin/members/"+strconv.FormatInt(off, 10)+"/toggle" {
		t.Error("Enable route")
	}
	if menu.Find("[popover]").Length() != 0 || row.Find("button[popovertarget]").Length() != 1 || row.Find("[popover].c-menu").Length() != 1 {
		t.Error("the menu must be a native popover opened by one button")
	}
	// The pager updates the list in place.
	// The new-member button and the sheet host.
	nb := d.Find(`a[href="/admin/members/new"]`)
	if nb.AttrOr("hx-get", "") != "/admin/members/new" || nb.AttrOr("hx-target", "") != "#sheet-body" {
		t.Errorf("New member button: %v", nb.Nodes)
	}
	if d.Find("dialog.c-sheet#sheet #sheet-body").Length() != 1 {
		t.Error("members page lacks the sheet host")
	}
}

// E10.14 pager links update the list region.
func TestE10_14_PagerInPlace(t *testing.T) {
	f := newP2(t)
	p := f.plan("Basic")
	for i := 0; i < 12; i++ {
		f.member(p, "user"+strconv.Itoa(100+i))
	}
	d := f.c.Get("/admin/members?per=10&status=active").Doc(t)
	a := d.Find(`nav[aria-label="Pages"] a[data-page="2"]`)
	if a.AttrOr("hx-get", "") != a.AttrOr("href", "x") || a.AttrOr("hx-target", "") != "#member-list" || a.AttrOr("hx-push-url", "") != "true" || !strings.Contains(a.AttrOr("href", ""), "status=active") {
		t.Errorf("pager link: %v", a.Nodes)
	}
	// Page 2 as a fragment carries the next rows.
	d2 := f.c.GetHX("/admin/members?per=10&status=active&page=2", listTarget).Doc(t)
	if d2.Find("tr[data-member]").Length() != 2 {
		t.Errorf("page 2 rows: %d", d2.Find("tr[data-member]").Length())
	}
}

// E10.15 sheets: GET fragments by HX-Target, full pages otherwise.
func TestE10_15_SheetFragments(t *testing.T) {
	f := newP2(t)
	p := f.plan("Basic")
	id := f.member(p, "alice")
	// New member.
	frag := f.c.GetHX("/admin/members/new", sheetTarget)
	d := fragmentDoc(t, frag)
	if frag.StatusCode != 200 || strings.Contains(frag.Body, "<html") {
		t.Fatalf("new member fragment: %d", frag.StatusCode)
	}
	form := d.Find("form#member-form")
	if form.AttrOr("hx-post", "") != "/admin/members" || form.AttrOr("hx-target", "") != "#sheet-body" || form.Find("input[name=username]").Length() != 1 {
		t.Errorf("new member form: %v", form.Nodes)
	}
	if d.Find("[data-sheet-title]").AttrOr("data-sheet-title", "") != "Add member" && form.AttrOr("data-sheet-title", "") != "Add member" {
		t.Error("the sheet title is missing")
	}
	if form.Find(`button[type=submit][hx-disabled-elt="this"]`).Length() != 1 {
		t.Error("submit button must disable itself while sending")
	}
	if form.Find("[data-close]").Length() == 0 {
		t.Error("no Cancel button that closes the sheet")
	}
	if full := fragmentDoc(t, f.c.GetHX("/admin/members/new", "")); full.Find("html").Length() != 1 || full.Find("form#member-form").AttrOr("hx-target", "") != "this" {
		t.Error("without the sheet target the page is unchanged")
	}
	// Edit member: the same route as the member page.
	frag = f.c.GetHX(mpath(id), sheetTarget)
	d = fragmentDoc(t, frag)
	if strings.Contains(frag.Body, "<html") || d.Find("form#member-form").AttrOr("hx-put", "") != mpath(id) || d.Find("form#member-form").AttrOr("hx-target", "") != "#sheet-body" {
		t.Fatalf("edit member fragment wrong: %s", frag.Body)
	}
	if d.Find("input[name=username]").AttrOr("value", "") != "alice" {
		t.Error("edit form lacks the member's data")
	}
	// Design 4b: the actions sit outside the form element, so htmx sends no form fields with them.
	if d.Find("form#member-form button[hx-post], form#member-form button[hx-delete]").Length() != 0 {
		t.Error("sheet actions must not sit inside the form")
	}
	var names []string
	d.Find("[data-state] button, details[data-more] button").Each(func(_ int, s *goquery.Selection) { names = append(names, text(s)) })
	if strings.Join(names, "|") != "Kick now|Renew|Delete" {
		t.Errorf("sheet actions %q", names)
	}
	kick := d.Find(`button:contains("Kick now")`)
	if kick.AttrOr("hx-post", "") != mpath(id)+"/disconnections" || kick.AttrOr("hx-confirm", "") == "" || kick.AttrOr("disabled", "x") == "x" {
		t.Errorf("Kick now: %v (disabled while the member is offline)", kick.Nodes)
	}
	if d.Find(`button:contains("Renew")`).AttrOr("hx-post", "") != mpath(id)+"/renewals" || d.Find(`button:contains("Delete")`).AttrOr("hx-confirm", "") == "" {
		t.Error("sheet actions: renew route or delete confirm missing")
	}
	// Enable and disable is the switch, saved with the form (it keeps the toggle behaviour of Update, BR-18).
	if sw := d.Find(`input[name=active][form="member-form"]`); sw.Length() != 1 || sw.AttrOr("role", "") != "switch" {
		t.Error("the Account enabled switch is missing or not tied to the form")
	}
	if p := d.Find(`a[href="` + mpath(id) + `/voucher"]`); text(p) != "Print login details" || p.AttrOr("target", "") != "_blank" {
		t.Error("the state card needs a Print login details link")
	}
	if d.Find("[data-sheet-sub]").AttrOr("data-sheet-sub", "") == "" || d.Find("[data-sheet-title]").AttrOr("data-sheet-title", "") != "alice" {
		t.Error("the sheet title is the username and the sub line says since when")
	}
	if pw := d.Find("form#member-form input[name=password]"); pw.AttrOr("type", "") != "password" || pw.AttrOr("value", "") == "" {
		t.Error("the edit sheet has the current password in a hidden field")
	}
	if u := d.Find(`form#member-form input[name=username]`); u.Length() != 1 || u.AttrOr("disabled", "x") != "x" || u.AttrOr("readonly", "x") != "x" {
		t.Error("the username stays editable, in the form (not in More options)")
	}
	if a := d.Find(`a[href="` + mpath(id) + `"]`); text(a) != "Open details" {
		t.Errorf("Open details link: %q", text(a))
	}
	if full := f.c.GetHX(mpath(id), ""); !strings.Contains(full.Body, "<html") || !strings.Contains(full.Body, "Extra data") {
		t.Error("the member page is unchanged without the sheet target")
	}
	// Unknown member: 404, not a sheet.
	if r := f.c.GetHX(mpath(999), sheetTarget); r.StatusCode != 404 {
		t.Errorf("unknown member: %d", r.StatusCode)
	}
	// No plans: the new-member sheet says so.
	g := newP2(t)
	if !strings.Contains(g.c.GetHX("/admin/members/new", sheetTarget).Body, "No plans exist yet") {
		t.Error("no-plan hint missing in the sheet")
	}
	// Plans.
	frag = f.c.GetHX("/admin/plans/new", sheetTarget)
	d = fragmentDoc(t, frag)
	if strings.Contains(frag.Body, "<html") || d.Find("form#plan-form").AttrOr("hx-post", "") != "/admin/plans" || d.Find("form#plan-form").AttrOr("hx-target", "") != "#sheet-body" {
		t.Errorf("new plan fragment: %s", frag.Body)
	}
	pid := strconv.FormatInt(p, 10)
	frag = f.c.GetHX("/admin/plans/"+pid, sheetTarget)
	d = fragmentDoc(t, frag)
	if strings.Contains(frag.Body, "<html") || d.Find("form#plan-form").AttrOr("hx-put", "") != "/admin/plans/"+pid || d.Find("[data-plan-warning]").Length() != 1 {
		t.Errorf("edit plan fragment: %s", frag.Body)
	}
	if !strings.Contains(f.c.GetHX("/admin/plans/"+pid, "").Body, "<html") {
		t.Error("plan edit page unchanged without target")
	}
	if pl := f.c.Get("/admin/plans").Doc(t); pl.Find("dialog#sheet #sheet-body").Length() != 1 || pl.Find(`a[href="/admin/plans/new"]`).AttrOr("hx-target", "") != "#sheet-body" {
		t.Error("plans page lacks the sheet host or the New plan button")
	}
}

// E10.16 a 422 in the sheet re-renders the form in the sheet with the input kept.
func TestE10_16_SheetValidation(t *testing.T) {
	f := newP2(t)
	p := f.plan("Basic")
	form := harness.MemberForm(p, "carol", "mobile", "12", "first_name", "Carol")
	r := f.c.SendHX("POST", "/admin/members", form, sheetTarget)
	d := fragmentDoc(t, r)
	if r.StatusCode != 422 || strings.Contains(r.Body, "<html") {
		t.Fatalf("status %d", r.StatusCode)
	}
	fm := d.Find("form#member-form")
	if fm.AttrOr("hx-target", "") != "#sheet-body" || fm.Find("input[name=username]").AttrOr("value", "") != "carol" ||
		fm.Find("input[name=first_name]").AttrOr("value", "") != "Carol" || fm.Find("input[name=mobile]").AttrOr("value", "") != "12" ||
		fm.Find(`[data-error-for="mobile"]`).Length() != 1 {
		t.Errorf("422 form: %s", r.Body)
	}
	if r.Header.Get("HX-Trigger") != "" || r.Header.Get("HX-Redirect") != "" {
		t.Error("a failed save must not trigger anything")
	}
	if scalar[int](t, f.db, `SELECT count(*) FROM member`) != 0 {
		t.Error("nothing may be stored")
	}
	// Edit: the 422 keeps the actions.
	id := f.member(p, "dave")
	r = f.c.SendHX("PUT", mpath(id), harness.MemberForm(p, "dave", "email", "nope"), sheetTarget)
	d = fragmentDoc(t, r)
	if r.StatusCode != 422 || d.Find(`input[name=email][form="member-form"]`).AttrOr("value", "") != "nope" || d.Find("[data-state] [data-actions] button").Length() != 1 || d.Find(`details[data-more] button:contains("Delete")`).Length() != 1 {
		t.Errorf("edit 422: %d %s", r.StatusCode, r.Body)
	}
	// Plan.
	r = f.c.SendHX("POST", "/admin/plans", harness.PlanForm("Bad", "duration_days", "0"), sheetTarget)
	d = fragmentDoc(t, r)
	if r.StatusCode != 422 || d.Find("form#plan-form").AttrOr("hx-target", "") != "#sheet-body" || d.Find("form#plan-form input[name=name]").AttrOr("value", "") != "Bad" {
		t.Errorf("plan 422: %d %s", r.StatusCode, r.Body)
	}
	pid := f.plan("Edit me")
	r = f.c.SendHX("PUT", "/admin/plans/"+strconv.FormatInt(pid, 10), harness.PlanForm("Edit me", "max_sessions", "0"), sheetTarget)
	if r.StatusCode != 422 || fragmentDoc(t, r).Find("form#plan-form input[name=max_sessions]").AttrOr("value", "") != "0" {
		t.Errorf("plan edit 422: %d", r.StatusCode)
	}
}

// E10.17 success from the sheet: events and toast, no redirect. Without the sheet target nothing changes.
func TestE10_17_SheetSuccess(t *testing.T) {
	f := newP2(t)
	p := f.plan("Basic")
	// Create member from the sheet: credentials card, no close (the card is the result).
	r := f.c.SendHX("POST", "/admin/members", harness.MemberForm(p, "erin", "expires_at", "2026-12-31T23:59"), sheetTarget)
	if r.StatusCode != http.StatusCreated || r.Header.Get("HX-Redirect") != "" {
		t.Fatalf("create: %d redirect %q", r.StatusCode, r.Header.Get("HX-Redirect"))
	}
	ev := hxTrigger(t, r)
	if ev["members-changed"] == nil || ev["toast"] == nil || ev["close-sheet"] != nil {
		t.Errorf("create events %v", ev)
	}
	if !strings.HasPrefix(r.Header.Get("Location"), "/admin/members/") {
		t.Errorf("Location %q", r.Header.Get("Location"))
	}
	d := fragmentDoc(t, r)
	id := f.idOf("erin")
	row := func(k string) string { return text(d.Find(`[data-key="` + k + `"]`)) }
	revealed := text(f.c.Get(mpath(id) + "/password").Doc(t).Find("[data-password]"))
	if row("username") != "erin" || row("plan") != "Basic" || !strings.Contains(row("expires"), "2026-12-31") || len(row("password")) != 6 || row("password") != revealed {
		t.Errorf("credentials: user %q pass %q (member has %q) plan %q expires %q", row("username"), row("password"), revealed, row("plan"), row("expires"))
	}
	if d.Find(".c-credentials [data-key=password]").Length() != 1 {
		t.Error("password row needs its hook")
	}
	// Each credential has its own Copy button; "Copy all details" copies the lot.
	if d.Find(`.c-credentials__row button[data-copy-text]`).Length() != 2 || d.Find(`.c-credentials__row button[data-copy-text="`+revealed+`"]`).Length() != 1 {
		t.Error("username and password each need a Copy button with their value")
	}
	copyText := d.Find(`button[data-copy-all]`).AttrOr("data-copy-text", "")
	for _, want := range []string{"erin", revealed, "Basic"} {
		if !strings.Contains(copyText, want) {
			t.Errorf("copy text %q lacks %q", copyText, want)
		}
	}
	if d.Find(`[hx-get="/admin/members/new"][hx-target="#sheet-body"]`).Length() != 1 || text(d.Find(`[hx-get="/admin/members/new"]`)) != "Add another" {
		t.Error("no Create another button")
	}
	if done := d.Find("button[data-close]"); text(done) != "Done" {
		t.Errorf("Done button %q", text(done))
	}
	if d.Find("[data-sheet-title]").Length() == 0 {
		t.Error("the card needs a sheet title")
	}
	if p := d.Find(`a[href="` + mpath(id) + `/voucher"]`); text(p) != "Print login details" || p.AttrOr("target", "") != "_blank" {
		t.Error("the card needs a Print link to the voucher")
	}
	if got := text(d.Find(`[data-key="devices"]`)); got == "" {
		t.Error("the card shows the device count")
	}
	// A password typed by the admin is shown as typed; no expiry says so.
	r = f.c.SendHX("POST", "/admin/members", harness.MemberForm(p, "frank", "password", "s3cret-pw"), sheetTarget)
	d = fragmentDoc(t, r)
	if text(d.Find(`[data-key="password"]`)) != "s3cret-pw" || text(d.Find(`[data-key="expires"]`)) != "No expiry" {
		t.Errorf("typed password card: %s", r.Body)
	}
	// Edit from the sheet: closes the sheet.
	frankID := f.idOf("frank")
	r = f.c.SendHX("PUT", mpath(frankID), harness.MemberForm(p, "frank", "first_name", "Frank"), sheetTarget)
	ev = hxTrigger(t, r)
	if r.StatusCode != 200 || r.Header.Get("HX-Redirect") != "" || ev["members-changed"] == nil || ev["close-sheet"] == nil || ev["toast"] == nil {
		t.Errorf("edit: %d redirect %q events %v", r.StatusCode, r.Header.Get("HX-Redirect"), ev)
	}
	if scalar[string](t, f.db, `SELECT first_name FROM member WHERE id=?`, frankID) != "Frank" {
		t.Error("edit not stored")
	}
	// Plan create and edit from the sheet.
	r = f.c.SendHX("POST", "/admin/plans", harness.PlanForm("Gold"), sheetTarget)
	ev = hxTrigger(t, r)
	if r.StatusCode != 201 || r.Header.Get("HX-Redirect") != "" || ev["plans-changed"] == nil || ev["close-sheet"] == nil || ev["toast"] == nil || !strings.HasPrefix(r.Header.Get("Location"), "/admin/plans/") {
		t.Errorf("plan create: %d %v %v", r.StatusCode, r.Header, ev)
	}
	gold := scalar[int64](t, f.db, `SELECT id FROM plan WHERE name='Gold'`)
	r = f.c.SendHX("PUT", "/admin/plans/"+strconv.FormatInt(gold, 10), harness.PlanForm("Gold 2"), sheetTarget)
	ev = hxTrigger(t, r)
	if r.StatusCode != 200 || r.Header.Get("HX-Redirect") != "" || ev["plans-changed"] == nil || ev["close-sheet"] == nil {
		t.Errorf("plan edit: %d %v", r.StatusCode, ev)
	}
	// Outside the sheet: the old answers (redirect headers, no events).
	r = f.c.Send("POST", "/admin/members", harness.MemberForm(p, "gina"))
	if r.StatusCode != 201 || r.Header.Get("HX-Redirect") == "" || r.Header.Get("HX-Trigger") != "" || r.Header.Get("Location") != r.Header.Get("HX-Redirect") {
		t.Errorf("member create without sheet: %d %v", r.StatusCode, r.Header)
	}
	r = f.c.Send("PUT", mpath(f.idOf("gina")), harness.MemberForm(p, "gina", "first_name", "G"))
	if r.StatusCode != 200 || r.Header.Get("HX-Redirect") != mpath(f.idOf("gina")) || r.Header.Get("HX-Trigger") != "" {
		t.Errorf("member update without sheet: %d %v", r.StatusCode, r.Header)
	}
	r = f.c.Send("POST", "/admin/plans", harness.PlanForm("Silver"))
	if r.StatusCode != 201 || r.Header.Get("HX-Redirect") != "/admin/plans" || r.Header.Get("HX-Trigger") != "" {
		t.Errorf("plan create without sheet: %d %v", r.StatusCode, r.Header)
	}
}

// E10.18 POST /admin/members/{id}/toggle: disable kicks, enable does not; the kick failure path matches the form.
func TestE10_18_Toggle(t *testing.T) {
	f := newP5(t)
	p := f.plan("Two", "max_sessions", "2")
	id := f.member(p, "alice")
	path := mpath(id) + "/toggle"
	off, on := url.Values{"active": {"0"}}, url.Values{"active": {"1"}}
	active := func() int { return scalar[int](t, f.db, `SELECT active FROM member WHERE id=?`, id) }
	f.online("alice", "a1", "192.168.88.10", "AA:BB:CC:DD:EE:01")
	f.online("alice", "a2", "192.168.88.11", "AA:BB:CC:DD:EE:02")

	// Disable: member inactive, one Disconnect per open session, sessions end.
	f.coa.Always(fakenas.ACK)
	r := f.c.Send("POST", path, off)
	if r.StatusCode != 200 || r.Header.Get("HX-Redirect") != mpath(id) || active() != 0 {
		t.Fatalf("disable: %d %v active=%d", r.StatusCode, r.Header, active())
	}
	if n := len(f.coa.Requests()); n != 2 || !f.session("a1").ended.Valid || !f.session("a2").ended.Valid {
		t.Fatalf("disable: %d Disconnects, sessions ended %v %v", n, f.session("a1").ended.Valid, f.session("a2").ended.Valid)
	}
	// Enable: no Disconnect.
	r = f.c.Send("POST", path, on)
	if r.StatusCode != 200 || active() != 1 || len(f.coa.Requests()) != 2 {
		t.Fatalf("enable: %d active=%d requests=%d", r.StatusCode, active(), len(f.coa.Requests()))
	}
	// Other fields are untouched by a toggle.
	if scalar[string](t, f.db, `SELECT username FROM member WHERE id=?`, id) != "alice" || scalar[int64](t, f.db, `SELECT plan_id FROM member WHERE id=?`, id) != p {
		t.Error("toggle changed other fields")
	}
	// Kick failure: the member is disabled anyway; the answer says so (form path wording).
	f.online("alice", "a3", "192.168.88.12", "AA:BB:CC:DD:EE:03")
	f.coa.Always(fakenas.NAK)
	r = f.c.Send("POST", path, off)
	if r.StatusCode != 200 || active() != 0 || !strings.Contains(r.Body, "1 of 1 open sessions could not be disconnected") || r.Header.Get("HX-Redirect") != "" {
		t.Fatalf("disable with NAK: %d %q active=%d", r.StatusCode, r.Body, active())
	}
	if f.session("a3").ended.Valid {
		t.Error("a NAKed session must stay")
	}
	// From the list: events and a toast instead of a redirect; the failure goes in the toast.
	f.c.Send("POST", path, on) // enable
	f.online("alice", "a4", "192.168.88.13", "AA:BB:CC:DD:EE:04")
	r = f.c.SendHX("POST", path, off, listTarget)
	ev := hxTrigger(t, r)
	if r.StatusCode != 200 || r.Header.Get("HX-Redirect") != "" || ev["members-changed"] == nil || !strings.Contains(strconv.Quote(ev["toast"].(string)), "could not be disconnected") {
		t.Fatalf("disable from list with NAK: %d %v", r.StatusCode, r.Header)
	}
	if r.Header.Get("HX-Reswap") != "none" && strings.TrimSpace(r.Body) != "" {
		t.Error("a list action must not swap its body into the list")
	}
	// Enable from the sheet closes it.
	r = f.c.SendHX("POST", path, on, sheetTarget)
	ev = hxTrigger(t, r)
	if r.StatusCode != 200 || active() != 1 || ev["members-changed"] == nil || ev["close-sheet"] == nil || ev["toast"] == nil {
		t.Errorf("enable from sheet: %d %v active=%d", r.StatusCode, ev, active())
	}
	// Needs CSRF and a known id.
	if r := f.c.Do("POST", path, off, ""); r.StatusCode != 403 {
		t.Errorf("no CSRF: %d", r.StatusCode)
	}
	if r := f.c.Send("POST", mpath(9999)+"/toggle", off); r.StatusCode != 404 {
		t.Errorf("unknown id: %d", r.StatusCode)
	}
	if r := f.c.Send("GET", path, nil); r.StatusCode != 405 {
		t.Errorf("GET toggle: %d", r.StatusCode)
	}
}

// E10.19 actions from the list or the sheet refresh the list with events; without a target they redirect as before.
// E10.22 toggle sets an explicit state: expires_at keeps its seconds, other fields stay, a repeat is a no-op.
func TestE10_22_ToggleExactAndIdempotent(t *testing.T) {
	f := newP5(t)
	p := f.plan("One")
	id := f.member(p, "alice")
	at := time.Date(2030, 6, 15, 10, 20, 37, 0, time.UTC)
	f.setExpiry(id, at)
	f.exec(`UPDATE member SET first_name='Al', email='a@b.co' WHERE id=?`, id)
	path := mpath(id) + "/toggle"
	f.online("alice", "a1", "192.168.88.10", "AA:BB:CC:DD:EE:01")
	f.coa.Always(fakenas.ACK)
	exp := func() int64 { return scalar[int64](t, f.db, `SELECT expires_at FROM member WHERE id=?`, id) }
	if r := f.c.Send("POST", path, url.Values{"active": {"0"}}); r.StatusCode != 200 {
		t.Fatalf("disable: %d", r.StatusCode)
	}
	if r := f.c.Send("POST", path, url.Values{"active": {"0"}}); r.StatusCode != 200 || len(f.coa.Requests()) != 1 {
		t.Fatalf("repeat disable: %d, %d Disconnects", r.StatusCode, len(f.coa.Requests()))
	}
	if r := f.c.Send("POST", path, url.Values{"active": {"1"}}); r.StatusCode != 200 {
		t.Fatalf("enable: %d", r.StatusCode)
	}
	if exp() != at.Unix() || scalar[string](t, f.db, `SELECT first_name||email FROM member WHERE id=?`, id) != "Ala@b.co" {
		t.Errorf("toggle changed other fields: expires_at=%d", exp())
	}
	for _, bad := range []url.Values{nil, {"active": {"x"}}} {
		if r := f.c.Send("POST", path, bad); r.StatusCode != 400 {
			t.Errorf("bad active %v: %d", bad, r.StatusCode)
		}
	}
}

// E10.23 bulk delete from the page refreshes the list with an event and a toast.
func TestE10_23_BulkDeleteRefreshes(t *testing.T) {
	f := newP5(t)
	p := f.plan("One")
	id := f.member(p, "alice")
	r := f.c.SendHX("POST", "/admin/members/bulk-delete", url.Values{"id": {strconv.FormatInt(id, 10)}, "confirmed": {"1"}}, "bulk-result")
	ev := hxTrigger(t, r)
	if r.StatusCode != 200 || ev["members-changed"] == nil || ev["toast"] == nil || r.Header.Get("HX-Redirect") != "" {
		t.Errorf("bulk delete: %d %v", r.StatusCode, r.Header)
	}
}

// E16.10 (review B10.2) a bulk delete from the page whose Disconnect fails still sends the members-changed
// event, so the deleted rows leave the list.
func TestE16_10_BulkDeleteWithFailedKickRefreshes(t *testing.T) {
	f := newP5(t)
	p := f.plan("One")
	id := f.member(p, "alice")
	f.online("alice", "a1", "192.168.88.11", "AA:BB:CC:DD:EE:01")
	f.coa.Always(fakenas.NAK)
	r := f.c.SendHX("POST", "/admin/members/bulk-delete", url.Values{"id": {strconv.FormatInt(id, 10)}, "confirmed": {"1"}}, "bulk-result")
	if r.StatusCode != 200 || !strings.Contains(r.Body, "could not be disconnected") {
		t.Fatalf("bulk delete: %d %s", r.StatusCode, r.Body)
	}
	if ev := hxTrigger(t, r); ev["members-changed"] == nil {
		t.Errorf("no members-changed event: %v", r.Header)
	}
}

// E10.24 a vanished row or a server error from a list or sheet gives a refresh event and a clear toast.
func TestE10_24_VanishedAndFailed(t *testing.T) {
	f := newP5(t)
	p := f.plan("One")
	id := f.member(p, "alice")
	for _, target := range []string{listTarget, sheetTarget} {
		r := f.c.SendHX("POST", mpath(9999)+"/renewals", nil, target)
		ev := hxTrigger(t, r)
		if r.StatusCode != 404 || ev["members-changed"] == nil || ev["toast"] != "This member no longer exists." {
			t.Errorf("member 404 from %s: %d %v", target, r.StatusCode, r.Header)
		}
	}
	r := f.c.SendHX("DELETE", "/admin/plans/9999", nil, planTarget)
	if ev := hxTrigger(t, r); r.StatusCode != 404 || ev["plans-changed"] == nil || ev["toast"] != "This plan no longer exists." {
		t.Errorf("plan 404: %d %v", r.StatusCode, r.Header)
	}
	if r := f.c.Send("POST", mpath(9999)+"/renewals", nil); r.StatusCode != 404 || r.Header.Get("HX-Trigger") != "" {
		t.Errorf("404 without target changed: %v", r.Header)
	}
	f.exec(`ALTER TABLE member RENAME COLUMN renewal_count TO rc`)
	r = f.c.SendHX("POST", mpath(id)+"/renewals", nil, listTarget)
	if ev := hxTrigger(t, r); r.StatusCode != 500 || ev["toast"] != "Something went wrong. Try again." {
		t.Errorf("500 from list: %d %v", r.StatusCode, r.Header)
	}
}

func TestE10_19_ListActions(t *testing.T) {
	f := newP5(t)
	p := f.plan("Basic")
	id := f.member(p, "alice")
	f.coa.Always(fakenas.ACK)
	// Renew.
	for _, c := range []struct{ target, wantClose string }{{listTarget, ""}, {sheetTarget, "close-sheet"}} {
		r := f.c.SendHX("POST", mpath(id)+"/renewals", nil, c.target)
		ev := hxTrigger(t, r)
		if r.StatusCode != 200 || r.Header.Get("HX-Redirect") != "" || ev["members-changed"] == nil || ev["toast"] == nil || (c.wantClose != "") != (ev[c.wantClose] != nil && c.wantClose != "") {
			t.Errorf("renew from %s: %d %v", c.target, r.StatusCode, r.Header)
		}
		if r.Header.Get("HX-Reswap") != "none" {
			t.Errorf("renew from %s: body would be swapped (HX-Reswap %q)", c.target, r.Header.Get("HX-Reswap"))
		}
	}
	if scalar[int](t, f.db, `SELECT renewal_count FROM member WHERE id=?`, id) != 2 {
		t.Error("renewals not applied")
	}
	if r := f.c.Send("POST", mpath(id)+"/renewals", nil); r.Header.Get("HX-Redirect") != mpath(id) || r.Header.Get("HX-Trigger") != "" {
		t.Errorf("renew without target: %v", r.Header)
	}
	// Disconnect: no sessions, then one.
	r := f.c.SendHX("POST", mpath(id)+"/disconnections", nil, listTarget)
	if ev := hxTrigger(t, r); r.StatusCode != 200 || ev["toast"] == nil || r.Header.Get("HX-Redirect") != "" {
		t.Errorf("disconnect none: %d %v", r.StatusCode, r.Header)
	}
	f.online("alice", "k1", "192.168.88.10", "AA:BB:CC:DD:EE:01")
	r = f.c.SendHX("POST", mpath(id)+"/disconnections", nil, listTarget)
	if ev := hxTrigger(t, r); r.StatusCode != 200 || ev["members-changed"] == nil || r.Header.Get("HX-Redirect") != "" || !f.session("k1").ended.Valid {
		t.Errorf("disconnect: %d %v", r.StatusCode, r.Header)
	}
	f.online("alice", "k2", "192.168.88.11", "AA:BB:CC:DD:EE:02")
	f.coa.Always(fakenas.NAK)
	r = f.c.SendHX("POST", mpath(id)+"/disconnections", nil, listTarget)
	ev := hxTrigger(t, r)
	if r.StatusCode != http.StatusBadGateway || r.Header.Get("HX-Reswap") != "none" || !strings.Contains(ev["toast"].(string), "could not be disconnected") {
		t.Errorf("disconnect NAK: %d %v", r.StatusCode, r.Header)
	}
	// Delete.
	r = f.c.SendHX("DELETE", mpath(id), nil, listTarget)
	if ev := hxTrigger(t, r); r.StatusCode != 200 && r.StatusCode != http.StatusOK || ev["members-changed"] == nil || r.Header.Get("HX-Redirect") != "" {
		t.Errorf("delete from list: %d %v", r.StatusCode, r.Header)
	}
	if scalar[int](t, f.db, `SELECT count(*) FROM member`) != 0 {
		t.Error("member not deleted")
	}
	g := f.member(p, "gone")
	if r := f.c.Send("DELETE", mpath(g), nil); r.Header.Get("HX-Redirect") != "/admin/members" {
		t.Errorf("delete without target: %v", r.Header)
	}
}

// E10.25 the add sheet shows the plan summary and keeps the rare fields in More options; the edit sheet changes the password.
func TestE10_25_SheetSummaryAndPassword(t *testing.T) {
	f := newP2(t)
	p := f.plan("Basic")
	d := fragmentDoc(t, f.c.GetHX("/admin/members/new", sheetTarget))
	sel := d.Find("select[name=plan_id]")
	if sel.AttrOr("data-summary-for", "") != "plan-summary" || sel.Find("option[data-summary]").Length() != 1 {
		t.Errorf("plan select: %v", sel.Nodes)
	}
	if got := sel.Find("option[data-summary]").AttrOr("data-summary", ""); !strings.Contains(got, "days") || !strings.Contains(got, "device") {
		t.Errorf("plan summary %q", got)
	}
	if d.Find("p#plan-summary").Length() != 1 {
		t.Error("the summary line is missing")
	}
	more := d.Find("details.c-more")
	if more.Length() != 1 || more.AttrOr("open", "x") != "x" || more.Find("input[name=email], input[name=expires_at], input[name=active]").Length() != 3 {
		t.Error("More options must be closed and hold the rare fields")
	}
	// A field error inside More options opens it.
	r := f.c.SendHX("POST", "/admin/members", harness.MemberForm(p, "carol", "email", "nope"), sheetTarget)
	if fragmentDoc(t, r).Find("details.c-more").AttrOr("open", "x") == "x" {
		t.Error("More options must be open when one of its fields has an error")
	}
	// Edit sheet: a new password is stored; a bad one is refused and nothing else breaks.
	id := f.member(p, "dave")
	r = f.c.SendHX("PUT", mpath(id), harness.MemberForm(p, "dave", "password", "newpass1"), sheetTarget)
	if r.StatusCode != 200 || text(f.c.Get(mpath(id)+"/password").Doc(t).Find("[data-password]")) != "newpass1" {
		t.Errorf("new password: %d", r.StatusCode)
	}
	r = f.c.SendHX("PUT", mpath(id), harness.MemberForm(p, "dave", "password", "bad\x01pw"), sheetTarget)
	if r.StatusCode != 422 || fragmentDoc(t, r).Find(`[data-error-for="password"]`).Length() != 1 {
		t.Errorf("bad password: %d", r.StatusCode)
	}
	// A refused password saves nothing: the rename and the disable in the same form are dropped too.
	r = f.c.SendHX("PUT", mpath(id), harness.MemberForm(p, "dave9", "active", "-", "password", "bad\x01pw"), sheetTarget)
	if r.StatusCode != 422 || scalar[string](t, f.db, `SELECT username FROM member WHERE id=?`, id) != "dave" || scalar[int64](t, f.db, `SELECT active FROM member WHERE id=?`, id) != 1 {
		t.Errorf("a bad password must change nothing else: %d", r.StatusCode)
	}
	if text(f.c.Get(mpath(id)+"/password").Doc(t).Find("[data-password]")) != "newpass1" {
		t.Error("a refused password must not change the stored one")
	}
	// Renaming works from the sheet; a taken name shows its error under the username field.
	f.member(p, "taken1")
	if r = f.c.SendHX("PUT", mpath(id), harness.MemberForm(p, "taken1"), sheetTarget); r.StatusCode != 422 || fragmentDoc(t, r).Find(`form#member-form [data-error-for="username"]`).Length() != 1 {
		t.Errorf("taken username: %d", r.StatusCode)
	}
	if r = f.c.SendHX("PUT", mpath(id), harness.MemberForm(p, "dave2"), sheetTarget); r.StatusCode != 200 || scalar[string](t, f.db, `SELECT username FROM member WHERE id=?`, id) != "dave2" {
		t.Errorf("rename: %d", r.StatusCode)
	}
	// The member page form has no password: a posted one is ignored.
	f.c.Send("PUT", mpath(id), harness.MemberForm(p, "dave2", "password", "ignored1"))
	if text(f.c.Get(mpath(id)+"/password").Doc(t).Find("[data-password]")) != "newpass1" {
		t.Error("the page form must not set the password")
	}
}
