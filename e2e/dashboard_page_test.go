package e2e

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"

	"hotspotpluslight/e2e/harness"
	"hotspotpluslight/internal/domain"
)

// dashDoc returns the dashboard page of a logged-in client.
func dashDoc(t *testing.T, c *harness.AdminClient) *goquery.Document {
	t.Helper()
	r := c.Get("/admin")
	if r.StatusCode != 200 {
		t.Fatalf("GET /admin: %d", r.StatusCode)
	}
	return r.Doc(t)
}

// dashInt reads a whole-number attribute of the first element that matches sel.
func dashInt(t *testing.T, d *goquery.Document, sel, name string) int64 {
	t.Helper()
	v, err := strconv.ParseInt(attr(d, sel, name), 10, 64)
	if err != nil {
		t.Fatalf("%s [%s] = %q: %v", sel, name, attr(d, sel, name), err)
	}
	return v
}

// chartBars returns the bars of one dashboard chart.
func chartBars(d *goquery.Document, chart string) *goquery.Selection {
	return d.Find(`[data-chart="` + chart + `"] [data-bar]`)
}

// dayKeyAgo is the UTC day key n days before the fixture clock.
func dayKeyAgo(f *p2, n int) string { return domain.DayKey(f.fc.Now().AddDate(0, 0, -n), time.UTC) }

// addUsage inserts one usage_daily row.
func addUsage(f *p2, memberID int64, day string, down, up int64) {
	f.t.Helper()
	f.exec(`INSERT INTO usage_daily(member_id, day, down, up, seconds) VALUES (?,?,?,?,0)`, memberID, day, down, up)
}

// addClosedSession inserts a session that has ended.
func addClosedSession(f *p2, memberID int64, username, sid string, start, end time.Time) {
	f.t.Helper()
	f.exec(`INSERT INTO session(member_id, username, acct_session_id, nas_ip, framed_ip, mac, status, started_at, last_update_at, ended_at)
		VALUES (?,?,?,?,?,?,2,?,?,?)`, memberID, username, sid, "10.0.0.1", "192.168.88.10", "AA:BB:CC:DD:EE:FF", start.Unix(), end.Unix(), end.Unix())
}

// attentionNames lists the usernames of one Needs attention list, in order.
func attentionNames(d *goquery.Document, key string) []string {
	var out []string
	d.Find(`[data-attention="` + key + `"] tr[data-attention-row]`).Each(func(_ int, s *goquery.Selection) {
		out = append(out, s.AttrOr("data-username", ""))
	})
	return out
}

// E18.01 GET /admin is the dashboard: 200 for the admin, a redirect to login for anyone else, and the landing page after login.
func TestE18_01_DashboardIsTheHomePage(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	anon := harness.NewAdminClient(t, s)
	if r := anon.Get("/admin"); r.StatusCode != http.StatusSeeOther || r.Header.Get("Location") != "/admin/login" {
		t.Errorf("anonymous: %d %q", r.StatusCode, r.Header.Get("Location"))
	}
	c := harness.LoginAsAdmin(t, s)
	r := c.Get("/admin")
	if r.StatusCode != 200 || r.Header.Get("Location") != "" {
		t.Fatalf("logged in: %d %q (never a redirect to Members)", r.StatusCode, r.Header.Get("Location"))
	}
	if h := text(r.Doc(t).Find("h1")); h != "Dashboard" {
		t.Errorf("heading %q", h)
	}
	again := harness.NewAdminClient(t, s)
	if r := again.PostLogin("admin", harness.AdminPassword); r.StatusCode != http.StatusSeeOther || r.Header.Get("Location") != "/admin" {
		t.Errorf("login lands on %d %q, want /admin", r.StatusCode, r.Header.Get("Location"))
	}
}

// E18.02 the sidebar lists Dashboard first and marks it as the current page; the other items are as before.
func TestE18_02_SidebarHasDashboardFirst(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	c := harness.LoginAsAdmin(t, s)
	want := []string{"Dashboard", "Members", "Batches", "Online", "Usage", "Plans", "Router", "Settings"}
	links := func(d *goquery.Document) (names []string, current []string) {
		d.Find("#sidebar a.l-nav__link").Each(func(_ int, a *goquery.Selection) {
			names = append(names, text(a))
			if a.AttrOr("aria-current", "") == "page" {
				current = append(current, text(a))
			}
		})
		return names, current
	}
	names, current := links(c.Get("/admin").Doc(t))
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("sidebar %v, want %v", names, want)
	}
	if len(current) != 1 || current[0] != "Dashboard" {
		t.Errorf("current item on /admin: %v", current)
	}
	if href := c.Get("/admin").Doc(t).Find("#sidebar a.l-nav__link").First().AttrOr("href", ""); href != "/admin" {
		t.Errorf("Dashboard links to %q", href)
	}
	_, current = links(c.Get("/admin/members").Doc(t))
	if len(current) != 1 || current[0] != "Members" {
		t.Errorf("current item on /admin/members: %v", current)
	}
}

// E18.03 Online now equals the Online page (a stale session is not counted).
func TestE18_03_OnlineNowTile(t *testing.T) {
	f := newP4(t)
	plan := f.plan("Basic")
	f.member(plan, "alice")
	bob := f.member(plan, "bob")
	f.start("alice", "s1")
	f.openSession(bob, "bob", "stale", f.fc.Now().Add(-time.Hour)) // BR-17: not online any more

	d := dashDoc(t, f.c)
	now := dashInt(t, d, "[data-online-now]", "data-online-now")
	if want := int64(len(onlineUsers(f.p3.p2))); now != 1 || now != want {
		t.Errorf("Online now %d, Online page %d, want 1", now, want)
	}
	if peak := dashInt(t, d, "[data-peak-today]", "data-peak-today"); peak < now {
		t.Errorf("peak %d is below online now %d", peak, now)
	}
}

// E18.04 peak today is the largest overlap since local midnight, a stopped session still counts, and the next day starts again.
func TestE18_04_PeakToday(t *testing.T) {
	f := newP4(t)
	plan := f.plan("Basic")
	f.member(plan, "alice")
	f.member(plan, "bob")
	f.start("alice", "s1")
	f.advance(time.Minute)
	f.start("bob", "s2")
	f.advance(time.Minute)
	f.stop("alice", "s1", 0, 0, 120)

	d := dashDoc(t, f.c)
	if now, peak := dashInt(t, d, "[data-online-now]", "data-online-now"), dashInt(t, d, "[data-peak-today]", "data-peak-today"); now != 1 || peak != 2 {
		t.Errorf("online now %d, peak today %d, want 1 and 2", now, peak)
	}

	f.advance(24 * time.Hour) // bob's session is stale, and it all happened yesterday
	d = dashDoc(t, f.c)
	if now, peak := dashInt(t, d, "[data-online-now]", "data-online-now"), dashInt(t, d, "[data-peak-today]", "data-peak-today"); now != 0 || peak != 0 {
		t.Errorf("next day: online now %d, peak today %d, want 0 and 0", now, peak)
	}
}

// E18.05 the Traffic today tile and its two comparisons match usage_daily and the usage page; a base of 0 shows "no data".
func TestE18_05_TrafficTile(t *testing.T) {
	f := newP3(t)
	plan := f.plan("Basic")
	a, b := f.member(plan, "alice"), f.member(plan, "bob")
	today, yesterday, lastWeek := dayKeyAgo(f.p2, 0), dayKeyAgo(f.p2, 1), dayKeyAgo(f.p2, 7)
	addUsage(f.p2, a, today, 1000, 500)
	addUsage(f.p2, b, today, 300, 200) // today: 2000 bytes
	addUsage(f.p2, a, yesterday, 800, 200)

	d := dashDoc(t, f.c)
	if got := dashInt(t, d, "[data-traffic-today]", "data-traffic-today"); got != 2000 {
		t.Errorf("traffic today %d, want 2000", got)
	}
	if got := attr(d, "[data-vs-yesterday]", "data-vs-yesterday"); got != "+100%" {
		t.Errorf("vs yesterday %q, want +100%%", got)
	}
	if got := attr(d, "[data-vs-lastweek]", "data-vs-lastweek"); got != "no data" {
		t.Errorf("vs last week %q, want no data (the base is 0)", got)
	}

	// The usage page shows the same total for the same day.
	bar := f.c.Get("/admin/usage?days=7").Doc(t).Find(`[data-bar][data-day="` + today + `"]`)
	down, _ := strconv.ParseInt(bar.AttrOr("data-down", ""), 10, 64)
	up, _ := strconv.ParseInt(bar.AttrOr("data-up", ""), 10, 64)
	if down+up != 2000 {
		t.Errorf("usage page shows %d for today, want 2000", down+up)
	}

	addUsage(f.p2, a, lastWeek, 4000, 0)
	d = dashDoc(t, f.c)
	if got := attr(d, "[data-vs-lastweek]", "data-vs-lastweek"); got != "-50%" {
		t.Errorf("vs last week %q, want -50%%", got)
	}
}

// E18.06 the Failed logins tile shows today's total and the top reason; a tie goes to the code that sorts first.
func TestE18_06_FailedLoginsTile(t *testing.T) {
	f := newP3(t)
	id := f.member(f.plan("Basic"), "alice", "password", "pw")

	d := dashDoc(t, f.c)
	if got := attr(d, "[data-failed-total]", "data-failed-total"); got != "0" || d.Find("[data-failed-empty]").Length() != 1 {
		t.Errorf("empty tile: total %q, empty text %d", got, d.Find("[data-failed-empty]").Length())
	}

	f.login("nobody", "x")
	f.login("nobody", "y") // two INVALID_CREDENTIALS
	f.exec(`UPDATE member SET active=0 WHERE id=?`, id)
	f.login("alice", "pw")
	f.login("alice", "pw") // two INACTIVE_USER
	d = dashDoc(t, f.c)
	if got := attr(d, "[data-failed-total]", "data-failed-total"); got != "4" {
		t.Errorf("total %q, want 4", got)
	}
	if got := attr(d, "[data-failed-top]", "data-failed-top"); got != "INACTIVE_USER" {
		t.Errorf("top reason on a tie %q, want INACTIVE_USER", got)
	}

	f.login("nobody", "z")
	d = dashDoc(t, f.c)
	if got := attr(d, "[data-failed-top]", "data-failed-top"); got != "INVALID_CREDENTIALS" {
		t.Errorf("top reason %q, want INVALID_CREDENTIALS", got)
	}
	if note := text(d.Find("[data-failed-top]")); !strings.Contains(note, domain.ReasonInvalidCredentials.Message()) {
		t.Errorf("the tile does not show the English text: %q", note)
	}
}

// E18.07 the router status panel is on the dashboard (it refreshes with the whole dashboard, E18.15); the Router page no longer has it.
func TestE18_07_RouterStatusMovedToDashboard(t *testing.T) {
	f := newP3(t)

	d := dashDoc(t, f.c)
	if n := d.Find("[data-link-status]").Length(); n != 1 {
		t.Errorf("the dashboard shows %d router status panels, want 1", n)
	}
	for _, k := range []string{"access-request", "accounting", "disconnect"} {
		if got := link(t, d, k); got.at != "never" {
			t.Errorf("%s on a new start: %+v", k, got)
		}
	}

	router := f.c.Get("/admin/router").Doc(t)
	if router.Find("[data-link-status]").Length() != 0 || router.Find("[data-link]").Length() != 0 {
		t.Error("the Router page still shows the link status panel")
	}
	if router.Find("[data-script]").Length() != 1 || router.Find("[data-design]").Length() == 0 {
		t.Error("the Router page lost the setup script or the page designs")
	}

	// The panel is empty again after a restart: E8.07b.
}

// E18.08 Needs attention: expiring within 3 days and near the data limit, with the boundaries, the 5-row cap and the member links.
func TestE18_08_NeedsAttention(t *testing.T) {
	f := newP3(t)
	now := f.fc.Now()
	basic := f.plan("Basic")
	expiring := func(name string, in time.Duration) int64 {
		id := f.member(basic, name)
		f.exec(`UPDATE member SET expires_at=? WHERE id=?`, now.Add(in).Unix(), id)
		return id
	}
	day := 24 * time.Hour
	expiring("e1", day)
	expiring("e3", 3*day)                 // exactly 3 days counts (the Members page uses the same rule)
	expiring("e3plus", 3*day+time.Second) // one second more does not
	f.exec(`UPDATE member SET active=0 WHERE id=?`, expiring("edisabled", 2*day))
	expiring("eexpired", -time.Hour)

	d := dashDoc(t, f.c)
	if got, want := strings.Join(attentionNames(d, "expiring"), ","), "e1,e3"; got != want {
		t.Errorf("expiring rows %q, want %q", got, want)
	}
	if got := d.Find(`[data-attention="expiring"]`).AttrOr("data-count", ""); got != "2" {
		t.Errorf("expiring count %q, want 2", got)
	}

	// At most 5 rows, with the full count, soonest first.
	for i := 2; i <= 6; i++ {
		expiring("x"+strconv.Itoa(i), time.Duration(24+6*(i-1))*time.Hour) // 30 h to 54 h, between e1 and e3
	}
	d = dashDoc(t, f.c)
	if got, want := strings.Join(attentionNames(d, "expiring"), ","), "e1,x2,x3,x4,x5"; got != want {
		t.Errorf("capped rows %q, want %q", got, want)
	}
	exp := d.Find(`[data-attention="expiring"]`)
	if exp.AttrOr("data-count", "") != "7" || text(exp.Find("[data-more]")) != "and 2 more" {
		t.Errorf("count %q, more %q", exp.AttrOr("data-count", ""), text(exp.Find("[data-more]")))
	}

	// Near the data limit: 79% is out, 80% is in, 100% is "out of data", unlimited plans and extra bulk change the picture.
	small := f.plan("Small", "bulk_value", "100", "bulk_unit", "MB")
	used := func(name string, bytes int64) int64 {
		id := f.member(small, name)
		f.exec(`UPDATE member SET used_download_bytes=?, used_upload_bytes=0 WHERE id=?`, bytes, id)
		return id
	}
	used("p79", 79*mb)
	p80 := used("p80", 80*mb)
	full := used("full", 100*mb)
	// The default test plan has a 10 GB limit, so the unlimited member needs a plan with bulk_value 0.
	unlimited := f.member(f.plan("Unlimited", "bulk_value", "0"), "unlimited")
	f.exec(`UPDATE member SET used_download_bytes=? WHERE id=?`, 10*gb, unlimited)
	f.exec(`UPDATE member SET extra_bulk_bytes=? WHERE id=?`, 100*mb, used("extra", 90*mb)) // 90 of 200 MB
	f.exec(`UPDATE member SET active=0 WHERE id=?`, used("pdisabled", 95*mb))

	d = dashDoc(t, f.c)
	if got, want := strings.Join(attentionNames(d, "near-limit"), ","), "full,p80"; got != want {
		t.Errorf("near-limit rows %q, want %q", got, want)
	}
	near := d.Find(`[data-attention="near-limit"]`)
	if near.AttrOr("data-count", "") != "2" {
		t.Errorf("near-limit count %q", near.AttrOr("data-count", ""))
	}
	rows := near.Find("tr[data-attention-row]")
	if got := text(rows.Eq(0).Find("[data-detail]")); got != "Out of data" {
		t.Errorf("full: %q", got)
	}
	if got := text(rows.Eq(1).Find("[data-detail]")); got != "80% used" {
		t.Errorf("p80: %q", got)
	}
	if near.Find(`a[href="`+mpath(full)+`"]`).Length() != 1 || near.Find(`a[href="`+mpath(p80)+`"]`).Length() != 1 {
		t.Error("rows do not link to the member pages")
	}
}

// E18.09 the traffic chart has 14 bars that equal the usage_daily sums, with zero-fill, and links to the usage page.
func TestE18_09_TrafficChart(t *testing.T) {
	f := newP3(t)
	plan := f.plan("Basic")
	a, b := f.member(plan, "alice"), f.member(plan, "bob")
	addUsage(f.p2, a, dayKeyAgo(f.p2, 0), 100, 50)
	addUsage(f.p2, a, dayKeyAgo(f.p2, 3), 10, 5)
	addUsage(f.p2, b, dayKeyAgo(f.p2, 3), 20, 10)
	addUsage(f.p2, a, dayKeyAgo(f.p2, 13), 7, 3)
	addUsage(f.p2, a, dayKeyAgo(f.p2, 14), 999, 999) // outside the chart

	d := dashDoc(t, f.c)
	bars := chartBars(d, "traffic")
	if bars.Length() != 14 {
		t.Fatalf("%d bars, want 14", bars.Length())
	}
	want := map[int][2]int64{0: {100, 50}, 3: {30, 15}, 13: {7, 3}}
	bars.Each(func(i int, s *goquery.Selection) {
		ago := 13 - i
		if day := s.AttrOr("data-day", ""); day != dayKeyAgo(f.p2, ago) {
			t.Errorf("bar %d is day %q, want %q", i, day, dayKeyAgo(f.p2, ago))
		}
		down, _ := strconv.ParseInt(s.AttrOr("data-down", ""), 10, 64)
		up, _ := strconv.ParseInt(s.AttrOr("data-up", ""), 10, 64)
		if w := want[ago]; down != w[0] || up != w[1] {
			t.Errorf("day %d ago: %d down %d up, want %v", ago, down, up, w)
		}
	})
	if d.Find(`[data-chart="traffic"] a[href="/admin/usage"]`).Length() != 1 {
		t.Error("the traffic chart does not link to /admin/usage")
	}
}

// E18.10 the busy hours chart has 24 bars with the BR-29 averages; an hour that has not started today does not lower its average.
func TestE18_10_BusyHoursChart(t *testing.T) {
	f := newP3(t) // the clock is 2026-11-01 12:00 UTC
	a := f.member(f.plan("Basic"), "alice")
	yesterday := time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
	addClosedSession(f.p2, a, "alice", "h10", yesterday.Add(10*time.Hour), yesterday.Add(11*time.Hour))
	addClosedSession(f.p2, a, "alice", "h23", yesterday.Add(23*time.Hour), yesterday.Add(24*time.Hour))

	bars := chartBars(dashDoc(t, f.c), "busy-hours")
	if bars.Length() != 24 {
		t.Fatalf("%d bars, want 24", bars.Length())
	}
	avg := func(h int) string { return bars.Eq(h).AttrOr("data-avg", "") }
	// Hour 10: yesterday and today (already over) were observed, one hour of presence in two: 0.5.
	if got := avg(10); got != "0.50" {
		t.Errorf("hour 10: %q, want 0.50", got)
	}
	// Hour 23 has not started today, so only yesterday counts: 1.0, not 0.5.
	if got := avg(23); got != "1.00" {
		t.Errorf("hour 23: %q, want 1.0", got)
	}
	if got := avg(3); got != "0.00" {
		t.Errorf("hour 3: %q, want 0.0", got)
	}
}

// E18.11 the daily active chart has 14 bars; a member with several sessions counts once; a member with no traffic does not count.
func TestE18_11_ActiveMembersChart(t *testing.T) {
	f := newP4(t)
	plan := f.plan("Basic")
	alice, bob := f.member(plan, "alice"), f.member(plan, "bob")
	f.member(plan, "carol")

	f.start("alice", "s1")
	f.interim("alice", "s1", 1000, 500, 30)
	f.stop("alice", "s1", 2000, 900, 60)
	f.start("alice", "s2")
	f.interim("alice", "s2", 500, 100, 20)
	f.stop("alice", "s2", 700, 200, 40)
	f.start("carol", "s3")
	f.stop("carol", "s3", 0, 0, 0) // logged in, no traffic
	addUsage(f.p3.p2, alice, dayKeyAgo(f.p3.p2, 2), 10, 10)
	addUsage(f.p3.p2, bob, dayKeyAgo(f.p3.p2, 2), 5, 5)

	bars := chartBars(dashDoc(t, f.c), "active-members")
	if bars.Length() != 14 {
		t.Fatalf("%d bars, want 14", bars.Length())
	}
	bars.Each(func(i int, s *goquery.Selection) {
		want := map[int]string{0: "1", 2: "2"}[13-i] // today: alice once; two days ago: alice and bob
		if want == "" {
			want = "0"
		}
		if got := s.AttrOr("data-count", ""); got != want {
			t.Errorf("day %d ago: %q, want %q", 13-i, got, want)
		}
	})
}

// E18.12 day boundaries and labels follow settings.timezone.
func TestE18_12_TimezoneDayBoundaries(t *testing.T) {
	f := newP3(t) // 2026-11-01 12:00 UTC
	f.login("nobody", "x")
	form := harness.FormValues(t, f.c.Get("/admin/settings").Doc(t), "form#settings-form")
	form.Set("timezone", "Asia/Tehran")
	if r := f.c.Send("PUT", "/admin/settings", form); r.StatusCode != 200 {
		t.Fatalf("timezone: %d %s", r.StatusCode, r.Body)
	}
	f.advance(9 * time.Hour) // 21:00 UTC is 00:30 on the 2nd in Tehran
	f.login("nobody", "x")

	// A chart with no traffic shows an empty text, not bars (E18.13), so seed one usage row on the Tehran "today".
	alice := f.member(f.plan("Basic"), "alice")
	addUsage(f.p2, alice, "2026-11-02", 7, 3)

	d := dashDoc(t, f.c)
	bars := chartBars(d, "traffic")
	if bars.Length() != 14 {
		t.Fatalf("%d bars", bars.Length())
	}
	if first, last := bars.First().AttrOr("data-day", ""), bars.Last().AttrOr("data-day", ""); first != "2026-10-20" || last != "2026-11-02" {
		t.Errorf("chart runs from %q to %q, want 2026-10-20 to 2026-11-02 (Tehran days)", first, last)
	}
	if down := bars.Last().AttrOr("data-down", ""); down != "7" {
		t.Errorf("the last bar (the 2nd in Tehran) has %q down, want 7", down)
	}
	// Only the second reject belongs to "today" (the 2nd in Tehran); the first was counted on the 1st.
	if got := attr(d, "[data-failed-total]", "data-failed-total"); got != "1" {
		t.Errorf("failed logins today %q, want 1", got)
	}
}

// E18.13 a new install answers 200 with every empty state; the page is clean (E10.01) and no-store.
func TestE18_13_NewInstall(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	c := harness.LoginAsAdmin(t, s)
	r := c.Get("/admin")
	if r.StatusCode != 200 || r.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("%d Cache-Control %q", r.StatusCode, r.Header.Get("Cache-Control"))
	}
	d := r.Doc(t)
	if bad := pageIsClean(d); len(bad) > 0 {
		t.Errorf("not clean: %v", bad)
	}
	if n := d.Find("[data-chart] [data-empty]").Length(); n != 3 {
		t.Errorf("%d empty charts, want 3", n)
	}
	for _, k := range []string{"expiring", "near-limit"} {
		if d.Find(`[data-attention="`+k+`"] [data-empty]`).Length() != 1 {
			t.Errorf("%s has no empty state", k)
		}
	}
	if d.Find("[data-failed-empty]").Length() != 1 || attr(d, "[data-failed-total]", "data-failed-total") != "0" {
		t.Error("the Failed logins tile is not empty")
	}
	if dashInt(t, d, "[data-online-now]", "data-online-now") != 0 || dashInt(t, d, "[data-peak-today]", "data-peak-today") != 0 {
		t.Error("the Online now tile is not 0")
	}
	if attr(d, "[data-traffic-today]", "data-traffic-today") != "0" || attr(d, "[data-vs-yesterday]", "data-vs-yesterday") != "no data" {
		t.Error("the Traffic tile is not empty")
	}
}

// E18.14 the dashboard fragment needs a login.
func TestE18_14_FragmentNeedsLogin(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	anon := harness.NewAdminClient(t, s)
	const p = "/admin/dashboard/content"
	if r := anon.Get(p); r.StatusCode != http.StatusSeeOther || r.Header.Get("Location") != "/admin/login" {
		t.Errorf("%s anonymous: %d %q", p, r.StatusCode, r.Header.Get("Location"))
	}
}

// E18.15 the whole dashboard refreshes by itself: one container polls the content fragment every 10 seconds,
// and the fragment has every part of the page (tiles, router status, Needs attention, three charts) and no page frame.
func TestE18_15_WholeDashboardRefreshes(t *testing.T) {
	f := newP4(t)
	plan := f.plan("Basic")
	f.member(plan, "alice")
	f.start("alice", "s1")
	const path = "/admin/dashboard/content"

	d := dashDoc(t, f.c)
	box := d.Find("[data-dashboard]")
	if box.Length() != 1 || box.AttrOr("hx-get", "") != path || box.AttrOr("hx-trigger", "") != "every 10s" || box.AttrOr("hx-swap", "") != "outerHTML" {
		t.Errorf("dashboard container: %d found, hx-get=%q hx-trigger=%q hx-swap=%q",
			box.Length(), box.AttrOr("hx-get", ""), box.AttrOr("hx-trigger", ""), box.AttrOr("hx-swap", ""))
	}
	for _, sel := range []string{"[data-tile]", "[data-link-status]", "[data-attention]", "[data-chart]"} {
		if d.Find("[data-dashboard] "+sel).Length() == 0 {
			t.Errorf("%s is outside the polled container", sel)
		}
	}

	r := f.c.Get(path)
	if r.StatusCode != 200 || r.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("fragment: %d Cache-Control %q", r.StatusCode, r.Header.Get("Cache-Control"))
	}
	if strings.Contains(strings.ToLower(r.Body), "<html") {
		t.Error("the response must be the fragment, not a full page")
	}
	frag := r.Doc(t)
	if n := frag.Find(`[data-tile]`).Length(); n != 3 {
		t.Errorf("fragment has %d tiles, want 3", n)
	}
	if n := frag.Find(`[data-chart]`).Length(); n != 3 {
		t.Errorf("fragment has %d charts, want 3", n)
	}
	if frag.Find(`[data-link-status]`).Length() != 1 || frag.Find(`[data-attention]`).Length() != 2 {
		t.Error("the fragment lacks the router status or the Needs attention lists")
	}
	if got := dashInt(t, frag, "[data-online-now]", "data-online-now"); got != 1 {
		t.Errorf("fragment Online now %d, want 1", got)
	}
}
