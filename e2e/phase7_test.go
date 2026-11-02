package e2e

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"layeh.com/radius/rfc2865"

	"hotspotpluslight/e2e/fakenas"
	"hotspotpluslight/e2e/harness"
)

// p7 is the Phase 7 fixture: the Phase 5 fixture (real Disconnect client + fake
// router listener) plus batch helpers.
type p7 struct{ *p5 }

func newP7(t *testing.T) *p7 { return &p7{newP5(t)} }

func bpath(id int64) string { return "/admin/batches/" + strconv.FormatInt(id, 10) }

func batchForm(planID int64, count int, kv ...string) url.Values {
	v := url.Values{"plan_id": {strconv.FormatInt(planID, 10)}, "count": {strconv.Itoa(count)}}
	for i := 0; i+1 < len(kv); i += 2 {
		if kv[i+1] == "-" {
			v.Del(kv[i])
		} else {
			v.Set(kv[i], kv[i+1])
		}
	}
	return v
}

// batch creates a batch through the admin UI, expects 201 and returns its id.
func (f *p7) batch(planID int64, count int, kv ...string) int64 {
	f.t.Helper()
	r := f.c.Send("POST", "/admin/batches", batchForm(planID, count, kv...))
	if r.StatusCode != http.StatusCreated {
		f.t.Fatalf("create batch: %d %s", r.StatusCode, r.Body)
	}
	loc := r.Header.Get("Location")
	rest, ok := strings.CutSuffix(strings.TrimPrefix(loc, "/admin/batches/"), "/vouchers")
	if !ok {
		f.t.Fatalf("Location %q, want /admin/batches/<id>/vouchers", loc)
	}
	id, err := strconv.ParseInt(rest, 10, 64)
	if err != nil {
		f.t.Fatalf("Location %q", loc)
	}
	return id
}

func (f *p7) strs(q string, args ...any) []string {
	f.t.Helper()
	rows, err := f.db.Query(q, args...)
	if err != nil {
		f.t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			f.t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

func (f *p7) users(batchID int64) []string {
	return f.strs(`SELECT username FROM member WHERE batch_id=? ORDER BY id`, batchID)
}

func (f *p7) memberID(username string) int64 {
	return scalar[int64](f.t, f.db, `SELECT id FROM member WHERE username=?`, username)
}

func (f *p7) counter() int { return scalar[int](f.t, f.db, `SELECT batch_counter FROM settings`) }

func (f *p7) rowCount(table string) int {
	return scalar[int](f.t, f.db, `SELECT count(*) FROM `+table)
}

// vouchers parses a print page into (page, username, password) triples.
type voucher struct {
	page             int
	user, pass, help string
}

func vouchersOf(d *goquery.Document) (out []voucher) {
	d.Find("[data-page]").Each(func(pi int, pg *goquery.Selection) {
		pg.Find("[data-voucher]").Each(func(_ int, v *goquery.Selection) {
			out = append(out, voucher{pi + 1, v.AttrOr("data-username", ""), v.AttrOr("data-password", ""), text(v.Find("[data-help]"))})
		})
	})
	return
}

func (f *p7) printPage(path string) (Doc *goquery.Document, body string) {
	f.t.Helper()
	r := f.c.Get(path)
	if r.StatusCode != 200 {
		f.t.Fatalf("GET %s: %d %s", path, r.StatusCode, r.Body)
	}
	return r.Doc(f.t), r.Body
}

// E7.01 create batch => N members with prefix+counter, counter persisted (BR-15, O14).
func TestE7_01_CreateBatch(t *testing.T) {
	f := newP7(t)
	p := f.plan("Basic")
	id := f.batch(p, 5, "label", "Hotel A")

	if got, want := f.users(id), []string{"wifi1360", "wifi1361", "wifi1362", "wifi1363", "wifi1364"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("usernames %v, want %v", got, want)
	}
	if f.counter() != 1365 {
		t.Fatalf("counter %d, want 1365", f.counter())
	}
	var label string
	var planID, count, days int64
	if err := f.db.QueryRow(`SELECT label, plan_id, count, duration_days FROM batch WHERE id=?`, id).Scan(&label, &planID, &count, &days); err != nil {
		t.Fatal(err)
	}
	if label != "Hotel A" || planID != p || count != 5 || days != 30 {
		t.Fatalf("batch row: %q plan %d count %d days %d", label, planID, count, days)
	}
	if n := scalar[int](t, f.db, `SELECT count(*) FROM member WHERE batch_id=? AND active=1 AND plan_id=? AND subscription_date=?`, id, p, t0.Unix()); n != 5 {
		t.Fatalf("%d members active, on the plan, subscribed now; want 5", n)
	}
	// A second batch continues the numbering.
	id2 := f.batch(p, 3)
	if got := f.users(id2); strings.Join(got, ",") != "wifi1365,wifi1366,wifi1367" {
		t.Fatalf("second batch %v", got)
	}
	if f.counter() != 1368 {
		t.Fatalf("counter %d, want 1368", f.counter())
	}
	// Prefix and counter come from the settings.
	f.exec(`UPDATE settings SET batch_username_prefix='card', batch_counter=7`)
	id3 := f.batch(p, 2)
	if got := f.users(id3); strings.Join(got, ",") != "card7,card8" || f.counter() != 9 {
		t.Fatalf("custom prefix: %v counter %d", got, f.counter())
	}
	// The batch members show up in the member list like any other.
	if !strings.Contains(f.c.Get("/admin/members?q=card7").Body, "card7") {
		t.Error("batch member missing from the member list")
	}
	// CSRF, unknown plan.
	if r := f.c.Do("POST", "/admin/batches", batchForm(p, 1), ""); r.StatusCode != 403 {
		t.Errorf("no CSRF: %d", r.StatusCode)
	}
	before := f.rowCount("member")
	r := f.c.Send("POST", "/admin/batches", batchForm(9999, 1))
	if r.StatusCode != 422 || r.Doc(t).Find("[data-error-for=plan_id]").Length() != 1 {
		t.Errorf("unknown plan: %d", r.StatusCode)
	}
	if r := f.c.Send("POST", "/admin/batches", batchForm(p, 1, "plan_id", "-")); r.StatusCode != 422 {
		t.Errorf("no plan: %d", r.StatusCode)
	}
	if f.rowCount("member") != before || f.counter() != 9 || f.rowCount("batch") != 3 {
		t.Error("a refused batch changed something")
	}
	// A taken username refuses the whole batch and changes nothing (409).
	f.member(p, "card9")
	before = f.rowCount("member")
	r = f.c.Send("POST", "/admin/batches", batchForm(p, 3))
	if r.StatusCode != 409 || !strings.Contains(r.Body, "card9") {
		t.Fatalf("taken username: %d %s", r.StatusCode, r.Body)
	}
	if f.rowCount("member") != before || f.counter() != 9 || f.rowCount("batch") != 3 {
		t.Error("a conflicting batch left rows behind or moved the counter")
	}
	// Form page.
	d := f.c.Get("/admin/batches/new").Doc(t)
	for _, name := range []string{"label", "plan_id", "count", "duration_days"} {
		if d.Find("#batch-form [name="+name+"]").Length() != 1 {
			t.Errorf("new-batch form has no %s field", name)
		}
	}
}

// E7.01b with no plan the form says to create one; the default plan is preselected (O10).
func TestE7_01b_NewFormPlans(t *testing.T) {
	f := newP7(t)
	if b := f.c.Get("/admin/batches/new").Body; !strings.Contains(b, "/admin/plans/new") || strings.Contains(b, `id="batch-form"`) {
		t.Fatalf("no plans: %s", b)
	}
	f.plan("A")
	pb := f.plan("B")
	f.exec(`UPDATE settings SET default_plan_id=?`, pb)
	d := f.c.Get("/admin/batches/new").Doc(t)
	if v := harness.FormValues(t, d, "#batch-form").Get("plan_id"); v != strconv.FormatInt(pb, 10) {
		t.Fatalf("preselected plan %q, want %d", v, pb)
	}
}

// E7.02 concurrent batch creations never reuse a number (BR-15).
func TestE7_02_ConcurrentBatches(t *testing.T) {
	f := newP7(t)
	p := f.plan("Basic")
	const workers, each = 8, 4
	var wg sync.WaitGroup
	codes := make([]int, workers)
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest("POST", f.c.Base+"/admin/batches", strings.NewReader(batchForm(p, each).Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("X-CSRF-Token", f.c.CSRF)
			req.Header.Set("HX-Request", "true")
			resp, err := f.c.HTTP.Do(req)
			if err != nil {
				codes[i] = -1
				return
			}
			resp.Body.Close()
			codes[i] = resp.StatusCode
		}()
	}
	wg.Wait()
	for i, c := range codes {
		if c != http.StatusCreated {
			t.Fatalf("worker %d: status %d", i, c)
		}
	}
	names := f.strs(`SELECT username FROM member ORDER BY username`)
	if len(names) != workers*each {
		t.Fatalf("%d members, want %d", len(names), workers*each)
	}
	want := map[string]bool{}
	for n := 1360; n < 1360+workers*each; n++ {
		want["wifi"+strconv.Itoa(n)] = true
	}
	for _, n := range names {
		if !want[n] {
			t.Fatalf("unexpected or duplicate username %s", n)
		}
		delete(want, n)
	}
	if len(want) != 0 || f.counter() != 1360+workers*each {
		t.Fatalf("missing %v, counter %d", want, f.counter())
	}
}

// E7.03 count above the maximum is rejected (BR-15).
func TestE7_03_CountAboveMax(t *testing.T) {
	f := newP7(t)
	p := f.plan("Basic")
	for _, bad := range []string{"31", "0", "-1", "", "abc", "2.5", "1e1"} {
		r := f.c.Send("POST", "/admin/batches", batchForm(p, 1, "count", bad))
		if r.StatusCode != 422 || r.Doc(t).Find("[data-error-for=count]").Length() != 1 {
			t.Errorf("count %q: %d", bad, r.StatusCode)
		}
	}
	if f.rowCount("member") != 0 || f.rowCount("batch") != 0 || f.counter() != 1360 {
		t.Fatal("a rejected batch changed something")
	}
	if msg := text(f.c.Send("POST", "/admin/batches", batchForm(p, 31)).Doc(t).Find("[data-error-for=count]")); !strings.Contains(msg, "30") {
		t.Errorf("message %q should name the maximum", msg)
	}
	f.batch(p, 30) // exactly the maximum
	f.exec(`UPDATE settings SET batch_max_count=5`)
	if r := f.c.Send("POST", "/admin/batches", batchForm(p, 6)); r.StatusCode != 422 {
		t.Errorf("6 with max 5: %d", r.StatusCode)
	}
	f.batch(p, 5)
	if f.rowCount("member") != 35 {
		t.Errorf("%d members", f.rowCount("member"))
	}
	// Duration, when given, must be a positive whole number of days.
	for _, bad := range []string{"0", "-3", "x", "1.5"} {
		if r := f.c.Send("POST", "/admin/batches", batchForm(p, 1, "duration_days", bad)); r.StatusCode != 422 {
			t.Errorf("duration %q: %d", bad, r.StatusCode)
		}
	}
	if r := f.c.Send("POST", "/admin/batches", batchForm(p, 1, "label", strings.Repeat("x", 101))); r.StatusCode != 422 {
		t.Errorf("long label: %d", r.StatusCode)
	}
}

// E7.04 expires_at = local midnight of today + duration + 1 days in the settings timezone (BR-15, O14).
func TestE7_04_ExpiresAtLocalMidnight(t *testing.T) {
	f := newP7(t)
	p := f.plan("Basic") // 30 days
	check := func(zone string, id int64, want time.Time) {
		t.Helper()
		got := f.strs(`SELECT DISTINCT expires_at FROM member WHERE batch_id=?`, id)
		if len(got) != 1 || got[0] != strconv.FormatInt(want.Unix(), 10) {
			t.Fatalf("%s: member expires_at %v, want %d (%s)", zone, got, want.Unix(), want)
		}
		if b := scalar[int64](t, f.db, `SELECT expires_at FROM batch WHERE id=?`, id); b != want.Unix() {
			t.Fatalf("%s: batch expires_at %d, want %d", zone, b, want.Unix())
		}
	}
	tehran, _ := time.LoadLocation("Asia/Tehran")
	newYork, _ := time.LoadLocation("America/New_York")

	// UTC (default zone): 2026-11-01 12:00 UTC + 30 + 1 days => 2026-12-02 00:00 UTC.
	check("UTC", f.batch(p, 2), time.Date(2026, 12, 2, 0, 0, 0, 0, time.UTC))

	f.exec(`UPDATE settings SET timezone='Asia/Tehran'`)
	check("Asia/Tehran", f.batch(p, 2), time.Date(2026, 12, 2, 0, 0, 0, 0, tehran))

	// Across a DST change: New York leaves DST on 2026-11-01, the date is Nov 1 local.
	f.exec(`UPDATE settings SET timezone='America/New_York'`)
	check("America/New_York", f.batch(p, 2, "duration_days", "7"), time.Date(2026, 11, 9, 0, 0, 0, 0, newYork))

	// The local date, not the UTC date: 21:00 UTC is already the next day in Tehran.
	f.exec(`UPDATE settings SET timezone='Asia/Tehran'`)
	f.advance(9 * time.Hour) // 2026-11-01 21:00 UTC = 2026-11-02 00:30 Tehran
	check("Tehran after local midnight", f.batch(p, 1, "duration_days", "1"), time.Date(2026, 11, 4, 0, 0, 0, 0, tehran))

	// An explicit duration is stored on the batch row; empty = the plan's validity.
	id := f.batch(p, 1, "duration_days", "3")
	if d := scalar[int](t, f.db, `SELECT duration_days FROM batch WHERE id=?`, id); d != 3 {
		t.Errorf("duration_days %d, want 3", d)
	}
	// The window length (plan) is not touched by the batch duration: subscription_date = now.
	if s := scalar[int64](t, f.db, `SELECT subscription_date FROM member WHERE batch_id=?`, id); s != f.fc.Now().Unix() {
		t.Errorf("subscription_date %d, want now", s)
	}
}

// E7.05 password length setting; digits only (O14, BR-14a).
func TestE7_05_PasswordLength(t *testing.T) {
	f := newP7(t)
	p := f.plan("Basic")
	digits := regexp.MustCompile(`^[0-9]+$`)
	for _, n := range []int{6, 8, 4, 20} {
		f.exec(`UPDATE settings SET voucher_password_length=?`, n)
		id := f.batch(p, 12)
		seen := map[string]bool{}
		for _, u := range f.users(id) {
			pw := revealed(f.p2, f.memberID(u))
			if len(pw) != n || !digits.MatchString(pw) {
				t.Fatalf("length %d: %s has password %q", n, u, pw)
			}
			seen[pw] = true
		}
		if n >= 6 && len(seen) < 2 {
			t.Fatalf("length %d: all 12 passwords identical (%v)", n, seen)
		}
		// Raw database bytes are the encrypted form, not the digits (D8).
		raw := scalar[[]byte](t, f.db, `SELECT password_enc FROM member WHERE batch_id=? LIMIT 1`, id)
		if digits.Match(raw) {
			t.Errorf("password stored in the clear")
		}
	}
}

// E7.06 print page: 14 vouchers per A4 page, 2 columns, correct page count (BR-16).
func TestE7_06_PrintLayout(t *testing.T) {
	f := newP7(t)
	p := f.plan("Basic")
	f.exec(`UPDATE settings SET batch_max_count=100`)
	for _, c := range []struct{ n, pages int }{{1, 1}, {14, 1}, {15, 2}, {28, 2}, {29, 3}, {30, 3}} {
		id := f.batch(p, c.n)
		d, _ := f.printPage(bpath(id) + "/vouchers")
		pages := d.Find("[data-page]")
		if pages.Length() != c.pages {
			t.Fatalf("%d vouchers: %d pages, want %d", c.n, pages.Length(), c.pages)
		}
		vs := vouchersOf(d)
		if len(vs) != c.n {
			t.Fatalf("%d vouchers listed, want %d", len(vs), c.n)
		}
		pages.Each(func(i int, pg *goquery.Selection) {
			want := min(14, c.n-14*i)
			if got := pg.Find("[data-voucher]").Length(); got != want {
				t.Errorf("%d vouchers: page %d holds %d, want %d", c.n, i+1, got, want)
			}
		})
		if got := d.Find("[data-page-count]").AttrOr("data-page-count", ""); got != strconv.Itoa(c.pages) {
			t.Errorf("data-page-count %q, want %d", got, c.pages)
		}
	}

	// Chrome: a standalone page with its own print stylesheet, no admin shell.
	id := f.batch(p, 14)
	d, body := f.printPage(bpath(id) + "/vouchers")
	var sheets []string
	d.Find(`link[rel=stylesheet]`).Each(func(_ int, s *goquery.Selection) { sheets = append(sheets, s.AttrOr("href", "")) })
	if len(sheets) != 1 || strings.Contains(sheets[0], "tabler") {
		t.Fatalf("stylesheets %v: want only the print stylesheet", sheets)
	}
	if d.Find("header.navbar, nav, [hx-headers]").Length() != 0 || strings.Contains(body, "tabler") {
		t.Error("the print page carries admin chrome")
	}
	css := f.c.Get(sheets[0])
	if css.StatusCode != 200 || !strings.Contains(css.Header.Get("Content-Type"), "css") {
		t.Fatalf("print stylesheet: %d %q", css.StatusCode, css.Header.Get("Content-Type"))
	}
	for _, want := range []string{"@media print", "@page", "A4", "repeat(2", "repeat(7", "page-break-after", "d-print-none"} {
		if want == "d-print-none" {
			continue // Tabler's class is not used here
		}
		if !strings.Contains(css.Body, want) {
			t.Errorf("print stylesheet lacks %q", want)
		}
	}
	// Every voucher shows its username and password; nothing is shared between pages.
	for _, v := range vouchersOf(d) {
		if v.user == "" || v.pass == "" {
			t.Errorf("voucher without credentials: %+v", v)
		}
	}
	// A print button for the screen view that print media hides.
	if d.Find("button[onclick*=print]").Length() != 1 {
		t.Error("no print button")
	}
}

// E7.07 help text from settings.voucher_help (English default); left-to-right (BR-16, O21).
func TestE7_07_HelpTextAndDirection(t *testing.T) {
	f := newP7(t)
	p := f.plan("Basic")
	id := f.batch(p, 3)
	d, body := f.printPage(bpath(id) + "/vouchers")
	if d.Find("html").AttrOr("dir", "") != "ltr" || strings.Contains(strings.ToLower(body), `dir="rtl"`) {
		t.Error("page is not left-to-right")
	}
	def := ""
	for i, v := range vouchersOf(d) {
		if v.help == "" {
			t.Fatalf("voucher %d has no help text with the setting empty", i)
		}
		if i == 0 {
			def = v.help
		} else if v.help != def {
			t.Fatalf("help differs between vouchers: %q / %q", v.help, def)
		}
		for _, r := range v.help {
			if r > 127 {
				t.Fatalf("default help is not plain English: %q", v.help)
			}
		}
	}
	d.Find("[data-voucher]").Each(func(_ int, v *goquery.Selection) {
		if v.AttrOr("dir", "") != "ltr" {
			t.Error("voucher without dir=ltr")
		}
	})

	// The admin edits the text on the settings page.
	sp := f.c.Get("/admin/settings").Doc(t)
	form := harness.FormValues(t, sp, "form")
	form.Set("voucher_help", "Join the Cafe network <b>now</b> & enjoy.")
	if r := f.c.Send("PUT", "/admin/settings", form); r.StatusCode != 200 {
		t.Fatalf("save settings: %d %s", r.StatusCode, r.Body)
	}
	d, body = f.printPage(bpath(id) + "/vouchers")
	for _, v := range vouchersOf(d) {
		if v.help != "Join the Cafe network <b>now</b> & enjoy." {
			t.Fatalf("help %q", v.help)
		}
	}
	if strings.Contains(body, "<b>now</b>") {
		t.Error("help text is not escaped")
	}
}

// E7.08 every generated member authenticates over RADIUS with its plan applied (BR-15).
func TestE7_08_MembersAuthenticate(t *testing.T) {
	f := newP7(t)
	p := f.plan("Fast", "speed_value", "2", "speed_unit", "Mbps", "bulk_value", "1", "bulk_unit", "GB", "time_limit_minutes", "60")
	id := f.batch(p, 6)
	users := f.users(id)
	if len(users) != 6 {
		t.Fatalf("%d members", len(users))
	}
	for _, u := range users {
		pw := revealed(f.p2, f.memberID(u))
		r := f.accepted(u, pw)
		if rl, ok := rateLimit(t, r); !ok || rl != "2000k/2000k 8000k/8000k 4000k/4000k 8/8" {
			t.Errorf("%s: Mikrotik-Rate-Limit %q", u, rl)
		}
		if x := xmitLimit(t, r); x != uint32(gb) {
			t.Errorf("%s: Xmit-Limit %d", u, x)
		}
		if st := sessionTimeout(r); st != 3600 {
			t.Errorf("%s: Session-Timeout %d", u, st)
		}
		f.rejected(u, pw+"0", "INVALID_CREDENTIALS")
	}
}

// E7.09 after expires_at the login rejects (O7).
func TestE7_09_ExpiredBatchRejects(t *testing.T) {
	f := newP7(t)
	p := f.plan("Basic")
	id := f.batch(p, 2, "duration_days", "1") // expires 2026-11-03 00:00 UTC
	u := f.users(id)[0]
	pw := revealed(f.p2, f.memberID(u))
	f.accepted(u, pw)
	f.advance(time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC).Sub(t0)) // exactly at expires_at: not yet passed
	f.accepted(u, pw)
	f.advance(time.Second)
	f.rejected(u, pw, "EXPIRED")
	// The member list agrees.
	if st := f.c.Get("/admin/members?status=expired&per=100").Doc(t).Find("tr[data-member][data-username=" + u + "]").Length(); st != 1 {
		t.Error("expired batch member not listed as expired")
	}
}

func batchRows(d *goquery.Document) map[string]*goquery.Selection {
	out := map[string]*goquery.Selection{}
	d.Find("tr[data-batch]").Each(func(_ int, s *goquery.Selection) { out[s.AttrOr("data-id", "")] = s })
	return out
}

func cell(row *goquery.Selection, col string) string {
	return text(row.Find("td[data-col=" + col + "]"))
}

// E7.10 batch list and labels (BR-15).
func TestE7_10_BatchList(t *testing.T) {
	f := newP7(t)
	if !strings.Contains(f.c.Get("/admin/batches").Body, "No batches") {
		t.Error("empty state missing")
	}
	f.exec(`UPDATE settings SET timezone='Asia/Tehran'`)
	pa := f.plan("Basic")
	pb := f.plan("Premium")
	a := f.batch(pa, 4, "label", "Room 12")
	f.advance(time.Hour)
	b := f.batch(pb, 3)

	d := f.c.Get("/admin/batches").Doc(t)
	rows := batchRows(d)
	ka, kb := strconv.FormatInt(a, 10), strconv.FormatInt(b, 10)
	if len(rows) != 2 {
		t.Fatalf("%d rows", len(rows))
	}
	ra, rb := rows[ka], rows[kb]
	if cell(ra, "label") != "Room 12" || cell(ra, "plan") != "Basic" || cell(ra, "members") != "4" || cell(ra, "unused") != "4" {
		t.Errorf("row a: label %q plan %q members %q unused %q", cell(ra, "label"), cell(ra, "plan"), cell(ra, "members"), cell(ra, "unused"))
	}
	if cell(ra, "created") != "2026-11-01 15:30" { // t0 12:00 UTC in Asia/Tehran
		t.Errorf("created %q", cell(ra, "created"))
	}
	if cell(rb, "label") != "(no label)" || cell(rb, "plan") != "Premium" || cell(rb, "members") != "3" {
		t.Errorf("row b: label %q plan %q members %q", cell(rb, "label"), cell(rb, "plan"), cell(rb, "members"))
	}
	// Newest first.
	var order []string
	d.Find("tr[data-batch]").Each(func(_ int, s *goquery.Selection) { order = append(order, s.AttrOr("data-id", "")) })
	if strings.Join(order, ",") != kb+","+ka {
		t.Errorf("order %v, want newest first", order)
	}
	// Reprint link per batch.
	if href := ra.Find("a[data-reprint]").AttrOr("href", ""); href != bpath(a)+"/vouchers" {
		t.Errorf("reprint href %q", href)
	}
	// Navigation has a Batches item.
	if d.Find(`header a.nav-link[href="/admin/batches"]`).Length() != 1 {
		t.Error("no Batches nav item")
	}
	if d.Find(`header a.nav-link.active[href="/admin/batches"]`).Length() != 1 {
		t.Error("Batches is not the active item on its own page")
	}

	// Usage: a member who used data (or merely has a session row) is no longer unused.
	us := f.users(a)
	f.start(us[0], "s1")
	f.interim(us[0], "s1", 100, 50, 10)
	f.start(us[1], "s2") // a session row with no traffic still counts as used
	rows = batchRows(f.c.Get("/admin/batches").Doc(t))
	if got := cell(rows[ka], "unused"); got != "2" {
		t.Errorf("unused after usage: %q, want 2", got)
	}
	f.exec(`UPDATE member SET used_seconds=5 WHERE username=?`, us[2])
	if got := cell(batchRows(f.c.Get("/admin/batches").Doc(t))[ka], "unused"); got != "1" {
		t.Errorf("unused after counters: %q, want 1", got)
	}
	// An individual delete lowers the member count; the stored size stays.
	if r := f.c.Send("DELETE", mpath(f.memberID(us[3])), nil); r.StatusCode != 200 {
		t.Fatalf("delete member: %d", r.StatusCode)
	}
	rows = batchRows(f.c.Get("/admin/batches").Doc(t))
	if cell(rows[ka], "members") != "3" || cell(rows[ka], "unused") != "0" {
		t.Errorf("after individual delete: members %q unused %q", cell(rows[ka], "members"), cell(rows[ka], "unused"))
	}
	if n := scalar[int](t, f.db, `SELECT count FROM batch WHERE id=?`, a); n != 4 {
		t.Errorf("stored count %d, want the original 4", n)
	}
	// Pagination keeps working on the list (shared component).
	if r := f.c.Get("/admin/batches?page=99"); r.StatusCode != 200 {
		t.Errorf("page out of range: %d", r.StatusCode)
	}
}

// E7.11 Reprint prints the same members again with their current passwords (BR-16, Q24).
func TestE7_11_Reprint(t *testing.T) {
	f := newP7(t)
	p := f.plan("Basic")
	id := f.batch(p, 5)
	d1, _ := f.printPage(bpath(id) + "/vouchers")
	first := vouchersOf(d1)
	if len(first) != 5 {
		t.Fatalf("%d vouchers", len(first))
	}
	for _, v := range first {
		if v.pass != revealed(f.p2, f.memberID(v.user)) {
			t.Fatalf("%s: printed %q, member password differs", v.user, v.pass)
		}
	}
	d2, _ := f.printPage(bpath(id) + "/vouchers")
	if fmt.Sprint(vouchersOf(d2)) != fmt.Sprint(first) {
		t.Fatal("a plain reprint changed")
	}
	// Change one password, delete one member: the reprint follows.
	target := first[1].user
	if r := f.c.Send("PUT", mpath(f.memberID(target))+"/password", url.Values{"password": {"NewPass77"}}); r.StatusCode != 200 {
		t.Fatalf("set password: %d %s", r.StatusCode, r.Body)
	}
	f.c.Send("DELETE", mpath(f.memberID(first[3].user)), nil)
	d3, body := f.printPage(bpath(id) + "/vouchers")
	got := vouchersOf(d3)
	if len(got) != 4 {
		t.Fatalf("%d vouchers after a delete, want 4", len(got))
	}
	for _, v := range got {
		if v.user == first[3].user {
			t.Error("deleted member still printed")
		}
		if v.user == target && v.pass != "NewPass77" {
			t.Errorf("reprint shows %q, want the new password", v.pass)
		}
		if v.user == first[2].user && v.pass != first[2].pass {
			t.Error("untouched member changed")
		}
	}
	if strings.Contains(body, first[1].pass) && first[1].pass != "NewPass77" {
		// the old password must be gone (a 6-digit collision with other text is not possible in this page)
		t.Error("old password still printed")
	}
	// Other batches are not mixed in.
	f.batch(p, 2)
	if n := len(vouchersOf(f.c.Get(bpath(id) + "/vouchers").Doc(t))); n != 4 {
		t.Errorf("%d vouchers after another batch", n)
	}
	// Guards.
	if r := f.c.Get(bpath(99999) + "/vouchers"); r.StatusCode != 404 {
		t.Errorf("unknown batch: %d", r.StatusCode)
	}
	if r := f.c.Get("/admin/batches/abc/vouchers"); r.StatusCode != 404 {
		t.Errorf("non-numeric id: %d", r.StatusCode)
	}
	if r := f.c.Send("POST", bpath(id)+"/vouchers", nil); r.StatusCode != 405 {
		t.Errorf("POST vouchers: %d", r.StatusCode)
	}
	anon := harness.NewAdminClient(t, f.s)
	if r := anon.Get(bpath(id) + "/vouchers"); r.StatusCode != 303 || r.Header.Get("Location") != "/admin/login" {
		t.Errorf("anonymous: %d %q", r.StatusCode, r.Header.Get("Location"))
	}
	if r := anon.Get("/admin/static/voucher.css"); r.StatusCode != 200 {
		t.Errorf("static stylesheet: %d", r.StatusCode)
	}
}

// E7.12 single-member Print voucher (BR-16, Q24).
func TestE7_12_SingleVoucher(t *testing.T) {
	f := newP7(t)
	p := f.plan("Basic")
	bid := f.batch(p, 2)
	inBatch := f.users(bid)[0]
	solo := f.member(p, "carol", "password", "Secret42")

	for _, c := range []struct {
		id         int64
		user, pass string
	}{
		{f.memberID(inBatch), inBatch, revealed(f.p2, f.memberID(inBatch))},
		{solo, "carol", "Secret42"},
	} {
		d, body := f.printPage(mpath(c.id) + "/voucher")
		vs := vouchersOf(d)
		if len(vs) != 1 || vs[0].user != c.user || vs[0].pass != c.pass || vs[0].help == "" {
			t.Fatalf("%s: %+v", c.user, vs)
		}
		if d.Find("[data-page]").Length() != 1 || d.Find("html").AttrOr("dir", "") != "ltr" || d.Find("[data-voucher]").AttrOr("dir", "") != "ltr" {
			t.Error("not one left-to-right page")
		}
		if !strings.Contains(body, "voucher.css") || d.Find("header.navbar, [hx-headers]").Length() != 0 {
			t.Error("not the shared print page")
		}
	}
	// The help text is the same setting.
	f.exec(`UPDATE settings SET voucher_help='Hello there'`)
	if h := vouchersOf(f.c.Get(mpath(solo) + "/voucher").Doc(t))[0].help; h != "Hello there" {
		t.Errorf("help %q", h)
	}
	// The member page offers it.
	pg := f.c.Get(mpath(solo)).Doc(t)
	if pg.Find(`a[href="`+mpath(solo)+`/voucher"]`).Length() != 1 {
		t.Error("member page has no Print voucher link")
	}
	if !strings.Contains(text(pg.Find(`a[href="`+mpath(solo)+`/voucher"]`)), "Print voucher") {
		t.Error("link label is not 'Print voucher'")
	}
	// Guards.
	if r := f.c.Get(mpath(99999) + "/voucher"); r.StatusCode != 404 {
		t.Errorf("unknown member: %d", r.StatusCode)
	}
	if r := f.c.Send("PUT", mpath(solo)+"/voucher", nil); r.StatusCode != 405 {
		t.Errorf("PUT: %d", r.StatusCode)
	}
	if r := harness.NewAdminClient(t, f.s).Get(mpath(solo) + "/voucher"); r.StatusCode != 303 {
		t.Errorf("anonymous: %d", r.StatusCode)
	}
}

// snapshot reads every column of every member of a batch plus the batch row.
func (f *p7) snapshot(id int64) string {
	f.t.Helper()
	var sb strings.Builder
	for _, q := range []string{
		`SELECT id||'|'||username||'|'||hex(password_enc)||'|'||active||'|'||plan_id||'|'||subscription_date||'|'||ifnull(expires_at,'')||'|'||used_download_bytes||'|'||updated_at FROM member WHERE batch_id=` + strconv.FormatInt(id, 10) + ` ORDER BY id`,
		`SELECT plan_id||'|'||count||'|'||duration_days||'|'||expires_at||'|'||created_at FROM batch WHERE id=` + strconv.FormatInt(id, 10),
	} {
		sb.WriteString(strings.Join(f.strs(q), "\n"))
		sb.WriteString("\n--\n")
	}
	return sb.String()
}

// E7.13 rename changes only the label (BR-23a).
func TestE7_13_Rename(t *testing.T) {
	f := newP7(t)
	p := f.plan("Basic")
	id := f.batch(p, 3, "label", "Old")
	before := f.snapshot(id)
	f.advance(time.Hour)

	r := f.c.Send("PUT", bpath(id), url.Values{"label": {"  New name  "}})
	if r.StatusCode != 200 {
		t.Fatalf("rename: %d %s", r.StatusCode, r.Body)
	}
	if got := scalar[string](t, f.db, `SELECT label FROM batch WHERE id=?`, id); got != "New name" {
		t.Fatalf("label %q (trimmed expected)", got)
	}
	if f.snapshot(id) != before {
		t.Fatal("rename changed more than the label")
	}
	if !strings.Contains(f.c.Get("/admin/batches").Body, "New name") {
		t.Error("list does not show the new label")
	}
	// The row carries a rename form.
	row := batchRows(f.c.Get("/admin/batches").Doc(t))[strconv.FormatInt(id, 10)]
	if row.Find(`form[hx-put="`+bpath(id)+`"] input[name=label]`).Length() != 1 {
		t.Error("no rename form on the row")
	}
	// Blank clears the label.
	if r := f.c.Send("PUT", bpath(id), url.Values{"label": {""}}); r.StatusCode != 200 {
		t.Errorf("clear: %d", r.StatusCode)
	}
	if got := scalar[string](t, f.db, `SELECT label FROM batch WHERE id=?`, id); got != "" {
		t.Errorf("label %q", got)
	}
	// Guards.
	if r := f.c.Send("PUT", bpath(id), url.Values{"label": {strings.Repeat("y", 101)}}); r.StatusCode != 422 || r.Doc(t).Find("[data-error-for=label]").Length() != 1 {
		t.Errorf("long label: %d (want 422 with the shared field error)", r.StatusCode)
	}
	if r := f.c.Do("PUT", bpath(id), url.Values{"label": {"x"}}, ""); r.StatusCode != 403 {
		t.Errorf("no CSRF: %d", r.StatusCode)
	}
	if r := f.c.Send("PUT", bpath(99999), url.Values{"label": {"x"}}); r.StatusCode != 404 {
		t.Errorf("unknown: %d", r.StatusCode)
	}
	if r := f.c.Send("POST", bpath(id), url.Values{"label": {"x"}}); r.StatusCode != 405 {
		t.Errorf("POST: %d", r.StatusCode)
	}
	if f.snapshot(id) != before {
		t.Fatal("a refused rename changed something")
	}
}

// E7.14 delete batch: kicks open sessions, then removes members, sessions and the batch in one transaction (BR-23b, Q25).
func TestE7_14_DeleteBatch(t *testing.T) {
	f := newP7(t)
	p := f.plan("Basic")
	id := f.batch(p, 4, "label", "Doomed")
	other := f.batch(p, 2, "label", "Keep")
	solo := f.member(p, "carol")
	us := f.users(id)
	f.online(us[0], "d1", "192.168.88.10", "AA:BB:CC:DD:EE:01")
	f.online(us[1], "d2", "192.168.88.11", "AA:BB:CC:DD:EE:02")
	f.online(f.users(other)[0], "k1", "192.168.88.12", "AA:BB:CC:DD:EE:03")
	f.start(us[2], "d3")
	f.stop(us[2], "d3", 10, 10, 5) // finished session row: removed with the member

	// The confirmation names the counts.
	row := batchRows(f.c.Get("/admin/batches").Doc(t))[strconv.FormatInt(id, 10)]
	btn := row.Find(`button[hx-delete="` + bpath(id) + `"]`)
	conf := btn.AttrOr("hx-confirm", "")
	if btn.Length() != 1 || !strings.Contains(conf, "4 members") || !strings.Contains(conf, "2 online") {
		t.Fatalf("confirmation %q (button found: %d)", conf, btn.Length())
	}

	// Guards first.
	if r := f.c.Do("DELETE", bpath(id), nil, ""); r.StatusCode != 403 {
		t.Errorf("no CSRF: %d", r.StatusCode)
	}
	if r := f.c.Send("DELETE", bpath(99999), nil); r.StatusCode != 404 {
		t.Errorf("unknown: %d", r.StatusCode)
	}
	if f.rowCount("member") != 7 {
		t.Fatal("a refused delete removed members")
	}

	// Atomicity: if the batch row cannot be removed, nothing is removed (the kicks before it were ACKed, so the members stay but their sessions end).
	f.exec(`CREATE TRIGGER fail_batch_delete BEFORE DELETE ON batch BEGIN SELECT RAISE(ABORT, 'no'); END`)
	r := f.c.Send("DELETE", bpath(id), nil)
	if r.StatusCode != 500 {
		t.Fatalf("forced failure: %d %s", r.StatusCode, r.Body)
	}
	if f.rowCount("member") != 7 || f.rowCount("batch") != 2 || len(f.users(id)) != 4 {
		t.Fatalf("partial delete: %d members, %d batches", f.rowCount("member"), f.rowCount("batch"))
	}
	f.exec(`DROP TRIGGER fail_batch_delete`)
	f.online(us[0], "d1", "192.168.88.10", "AA:BB:CC:DD:EE:01") // the failed attempt's kick may have ended them
	f.online(us[1], "d2", "192.168.88.11", "AA:BB:CC:DD:EE:02")
	base := len(f.coa.Requests())

	r = f.c.Send("DELETE", bpath(id), nil)
	if r.StatusCode != 200 || r.Header.Get("HX-Redirect") != "/admin/batches" {
		t.Fatalf("delete: %d %q %s", r.StatusCode, r.Header.Get("HX-Redirect"), r.Body)
	}
	got := map[string]bool{}
	for _, d := range f.coa.Requests()[base:] {
		got[rfc2865.UserName_GetString(d.Packet)] = true
	}
	if len(got) != 2 || !got[us[0]] || !got[us[1]] {
		t.Fatalf("Disconnects sent to %v, want exactly the two online members", got)
	}
	if n := scalar[int](t, f.db, `SELECT count(*) FROM batch WHERE id=?`, id); n != 0 {
		t.Error("batch row still there")
	}
	if n := scalar[int](t, f.db, `SELECT count(*) FROM member WHERE username IN (?,?,?,?)`, us[0], us[1], us[2], us[3]); n != 0 {
		t.Errorf("%d batch members left", n)
	}
	if n := scalar[int](t, f.db, `SELECT count(*) FROM session WHERE username IN (?,?,?,?)`, us[0], us[1], us[2], us[3]); n != 0 {
		t.Errorf("%d session rows left", n)
	}
	// Everything else is untouched.
	if f.rowCount("member") != 3 || len(f.users(other)) != 2 || scalar[int](t, f.db, `SELECT count(*) FROM member WHERE id=?`, solo) != 1 {
		t.Error("other members were touched")
	}
	if scalar[int](t, f.db, `SELECT count(*) FROM session WHERE username=?`, f.users(other)[0]) != 1 {
		t.Error("another batch's session was removed")
	}
	if r := f.c.Get(bpath(id) + "/vouchers"); r.StatusCode != 404 {
		t.Errorf("print of a deleted batch: %d", r.StatusCode)
	}
	if r := f.c.Send("DELETE", bpath(id), nil); r.StatusCode != 404 {
		t.Errorf("second delete: %d", r.StatusCode)
	}
	// The deleted vouchers no longer log in.
	f.rejected(us[0], "x", "INVALID_CREDENTIALS")

	// A kick failure is shown but does not stop the delete.
	id2 := f.batch(p, 3, "label", "Stubborn")
	us2 := f.users(id2)
	f.online(us2[0], "z1", "192.168.88.20", "AA:BB:CC:DD:EE:20")
	f.coa.Always(fakenas.NAK)
	r = f.c.Send("DELETE", bpath(id2), nil)
	if r.StatusCode != 200 || r.Header.Get("HX-Redirect") != "" || !strings.Contains(r.Body, "could not be disconnected") {
		t.Fatalf("delete with failing kick: %d redirect %q %s", r.StatusCode, r.Header.Get("HX-Redirect"), r.Body)
	}
	if !strings.Contains(r.Body, "3") {
		t.Error("the notice does not say how many members were deleted")
	}
	if n := scalar[int](t, f.db, `SELECT count(*) FROM batch WHERE id=?`, id2); n != 0 || len(f.users(id2)) != 0 {
		t.Error("batch survived a failed kick")
	}
	if scalar[int](t, f.db, `SELECT count(*) FROM session WHERE username=?`, us2[0]) != 0 {
		t.Error("session row survived the delete")
	}

	// An empty batch (all members deleted one by one) can still be deleted.
	id3 := f.batch(p, 1)
	f.c.Send("DELETE", mpath(f.memberID(f.users(id3)[0])), nil)
	if r := f.c.Send("DELETE", bpath(id3), nil); r.StatusCode != 200 || scalar[int](t, f.db, `SELECT count(*) FROM batch WHERE id=?`, id3) != 0 {
		t.Errorf("empty batch delete: %d", r.StatusCode)
	}
}

// E7.15 a batch whose members were all deleted one by one still holds its plan: the plan cannot be deleted until the batch is (O12).
func TestE7_15_PlanHeldByBatch(t *testing.T) {
	f := newP7(t)
	p := f.plan("Basic")
	id := f.batch(p, 1)
	f.c.Send("DELETE", mpath(f.memberID(f.users(id)[0])), nil)
	r := f.c.Send("DELETE", "/admin/plans/"+strconv.FormatInt(p, 10), nil)
	if r.StatusCode != 409 || !strings.Contains(r.Body, "1 batch") {
		t.Fatalf("plan with an empty batch: %d %s", r.StatusCode, r.Body)
	}
	f.c.Send("DELETE", bpath(id), nil)
	if r := f.c.Send("DELETE", "/admin/plans/"+strconv.FormatInt(p, 10), nil); r.StatusCode != 200 {
		t.Fatalf("plan after the batch is gone: %d %s", r.StatusCode, r.Body)
	}
}
