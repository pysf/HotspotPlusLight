package e2e

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"layeh.com/radius/rfc2866"

	"hotspotpluslight/e2e/harness"
)

// dayRow is a usage_daily row as the tests look at it.
type dayRow struct {
	day          string
	dl, ul, secs int64
}

func (f *p4) dayRows(memberID int64) []dayRow {
	f.t.Helper()
	rows, err := f.db.Query(`SELECT day, down, up, seconds FROM usage_daily WHERE member_id=? ORDER BY day`, memberID)
	if err != nil {
		f.t.Fatal(err)
	}
	defer rows.Close()
	var out []dayRow
	for rows.Next() {
		var r dayRow
		if err := rows.Scan(&r.day, &r.dl, &r.ul, &r.secs); err != nil {
			f.t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func (f *p4) wantDays(memberID int64, want ...dayRow) {
	f.t.Helper()
	got := f.dayRows(memberID)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		f.t.Fatalf("usage_daily %+v, want %+v", got, want)
	}
}

// seedDay writes a history row directly (what accounting over past days would have produced).
func (f *p4) seedDay(memberID int64, day string, dl, ul, secs int64) {
	f.t.Helper()
	f.exec(`INSERT INTO usage_daily(member_id, day, down, up, seconds) VALUES (?,?,?,?,?)`, memberID, day, dl, ul, secs)
}

// E9.01 accounting deltas (the same ones the member counters get) are added to the day row (D12).
func TestE9_01_AccountingFeedsDayRow(t *testing.T) {
	f := newP4(t)
	id := f.member(f.plan("Basic"), "alice")
	f.start("alice", "s1")
	f.wantDays(id) // a zero delta writes nothing
	f.interim("alice", "s1", 1000, 100, 60)
	f.interim("alice", "s1", 3000, 400, 100)
	f.interim("alice", "s1", 3000, 400, 100) // retransmission: no double count
	f.stop("alice", "s1", 3500, 450, 130)
	f.wantDays(id, dayRow{"2026-11-01", 3500, 450, 130})
	f.wantUsage(id, usage{3500, 450, 130})
	// a second session the same day adds to the same row
	f.start("alice", "s2")
	f.interim("alice", "s2", 10, 20, 30)
	f.wantDays(id, dayRow{"2026-11-01", 3510, 470, 160})
}

// E9.02 the day is the date in settings.timezone (here UTC+03:30, no DST).
func TestE9_02_DayInSettingsTimezone(t *testing.T) {
	f := newP4(t)
	id := f.member(f.plan("Basic"), "alice")
	f.exec(`UPDATE settings SET timezone='Asia/Tehran'`)
	f.start("alice", "s1")
	f.fc.Set(time.Date(2026, 11, 1, 20, 0, 0, 0, time.UTC)) // 23:30 on 1 Nov in Tehran
	f.interim("alice", "s1", 1000, 100, 60)
	f.fc.Set(time.Date(2026, 11, 1, 21, 0, 0, 0, time.UTC)) // 00:30 on 2 Nov in Tehran
	f.interim("alice", "s1", 1500, 150, 90)
	f.wantDays(id, dayRow{"2026-11-01", 1000, 100, 60}, dayRow{"2026-11-02", 500, 50, 30})
}

// E9.03 the day row uses the same Gigawords-aware delta as the member counters (BR-11).
func TestE9_03_GigawordsAndCounterRestart(t *testing.T) {
	f := newP4(t)
	// Unlimited data: the plan must not kick the member in the middle of the test (Phase 11a).
	id := f.member(f.plan("Basic", "bulk_value", "0"), "alice")
	f.start("alice", "s1")
	const gig = uint64(1) << 32
	f.interim("alice", "s1", 3*gig+5, gig+7, 100)
	f.interim("alice", "s1", 10, 20, 5) // counter restart: the new value is the delta
	u := f.usage(id)
	f.wantDays(id, dayRow{"2026-11-01", u.dl, u.ul, u.secs})
	if u.dl != int64(3*gig+5+10) {
		t.Fatalf("member download %d", u.dl)
	}
}

// E9.04 the day row commits with the packet: a rolled-back packet leaves no trace, a retransmit counts once (E4.15).
func TestE9_04_SameTransaction(t *testing.T) {
	f := newP4(t)
	id := f.member(f.plan("Basic"), "alice")
	f.start("alice", "s1")
	f.nas.Timeout = 700 * time.Millisecond
	pkt := f.nas.AcctPacket(acct(rfc2866.AcctStatusType_Value_InterimUpdate, "alice", "s1", 1000, 100, 60))
	f.hook.set(func() error { return fmt.Errorf("injected crash") })
	if _, err := f.nas.AcctExchange(pkt); err == nil {
		t.Fatal("a reply was sent although the transaction did not commit")
	}
	f.wantDays(id)
	f.hook.set(nil)
	f.nas.Timeout = 3 * time.Second
	for range 2 {
		if _, err := f.nas.AcctExchange(pkt); err != nil {
			t.Fatal(err)
		}
	}
	f.wantDays(id, dayRow{"2026-11-01", 1000, 100, 60})
}

// E9.05 renew resets the counters but keeps the history; usage after the renew is added to it (supersedes "no history", BR-13).
func TestE9_05_RenewKeepsHistory(t *testing.T) {
	f := newP4(t)
	id := f.member(f.plan("Basic"), "alice")
	f.start("alice", "s1")
	f.interim("alice", "s1", 1000, 100, 60)
	if r := f.c.Send("POST", mpath(id)+"/renewals", nil); r.StatusCode >= 400 {
		t.Fatalf("renew: %d", r.StatusCode)
	}
	f.wantUsage(id, usage{})
	f.wantDays(id, dayRow{"2026-11-01", 1000, 100, 60})
	f.interim("alice", "s1", 1400, 150, 90)
	f.wantUsage(id, usage{400, 50, 30})
	f.wantDays(id, dayRow{"2026-11-01", 1400, 150, 90})
}

// E9.06 deleting a member removes its history and only its history.
func TestE9_06_DeleteMemberRemovesRows(t *testing.T) {
	f := newP4(t)
	p := f.plan("Basic")
	a, b := f.member(p, "alice"), f.member(p, "bob")
	f.seedDay(a, "2026-11-01", 1, 2, 3)
	f.seedDay(b, "2026-11-01", 4, 5, 6)
	if r := f.c.Send("DELETE", mpath(a), nil); r.StatusCode >= 400 {
		t.Fatalf("delete: %d", r.StatusCode)
	}
	if n := scalar[int](t, f.db, `SELECT count(*) FROM usage_daily WHERE member_id=?`, a); n != 0 {
		t.Fatalf("rows of the deleted member: %d", n)
	}
	f.wantDays(b, dayRow{"2026-11-01", 4, 5, 6})
}

type bar struct {
	day    string
	dl, ul int64
}

func bars(d *goquery.Document) []bar {
	var out []bar
	d.Find("[data-bar]").Each(func(_ int, s *goquery.Selection) {
		dl, _ := strconv.ParseInt(s.AttrOr("data-down", ""), 10, 64)
		ul, _ := strconv.ParseInt(s.AttrOr("data-up", ""), 10, 64)
		out = append(out, bar{s.AttrOr("data-day", ""), dl, ul})
	})
	return out
}

// E9.07 the usage page charts the last 30 days (today included), all members summed, empty days as zero.
func TestE9_07_DailyChart(t *testing.T) {
	f := newP4(t)
	p := f.plan("Basic")
	a, b := f.member(p, "alice"), f.member(p, "bob")
	f.seedDay(a, "2026-11-01", 100, 10, 5)
	f.seedDay(b, "2026-11-01", 200, 20, 5)
	f.seedDay(a, "2026-10-03", 7, 1, 1)   // first day of the window
	f.seedDay(a, "2026-10-02", 999, 9, 9) // outside
	r := f.c.Get("/admin/usage")
	if r.StatusCode != 200 {
		t.Fatalf("usage page: %d", r.StatusCode)
	}
	got := bars(r.Doc(t))
	if len(got) != 30 {
		t.Fatalf("%d bars, want 30", len(got))
	}
	if got[0] != (bar{"2026-10-03", 7, 1}) || got[29] != (bar{"2026-11-01", 300, 30}) || got[10] != (bar{"2026-10-13", 0, 0}) {
		t.Fatalf("bars: first %+v, tenth %+v, last %+v", got[0], got[10], got[29])
	}
}

// E9.08 top 10 members by total bytes for 7, 30 or 90 days; default 30; ties by username; bad range = default.
func TestE9_08_TopMembers(t *testing.T) {
	f := newP4(t)
	p := f.plan("Basic")
	for i := 1; i <= 12; i++ {
		id := f.member(p, fmt.Sprintf("user%02d", i))
		f.seedDay(id, "2026-11-01", int64(i)*1000, 0, 60) // user12 is the biggest
	}
	old := f.member(p, "oldtimer")
	f.seedDay(old, "2026-09-10", 50000, 0, 10) // 52 days ago: only the 90 day range
	recent := f.member(p, "weekold")
	f.seedDay(recent, "2026-10-20", 20000, 5, 10) // 12 days ago: 30 and 90, not 7

	top := func(q string) (users []string, d *goquery.Document) {
		r := f.c.Get("/admin/usage" + q)
		if r.StatusCode != 200 {
			t.Fatalf("usage%s: %d", q, r.StatusCode)
		}
		d = r.Doc(t)
		d.Find("tr[data-top]").Each(func(_ int, s *goquery.Selection) { users = append(users, s.AttrOr("data-username", "")) })
		return
	}
	u, d := top("")
	want := []string{"weekold", "user12", "user11", "user10", "user09", "user08", "user07", "user06", "user05", "user04"}
	if fmt.Sprint(u) != fmt.Sprint(want) {
		t.Fatalf("default range: %v", u)
	}
	if text(d.Find(`tr[data-top][data-username="user12"] td[data-col="total"]`)) == "" {
		t.Fatal("no total cell")
	}
	if text(d.Find(`tr[data-top][data-username="user12"] td[data-col="down"]`)) != "11.72 KB" {
		t.Fatalf("download cell: %q", text(d.Find(`tr[data-top][data-username="user12"] td[data-col="down"]`)))
	}
	u, _ = top("?days=7")
	if len(u) != 10 || u[0] != "user12" || u[9] != "user03" {
		t.Fatalf("7 days: %v", u)
	}
	u, _ = top("?days=90")
	if u[0] != "oldtimer" || u[1] != "weekold" || len(u) != 10 {
		t.Fatalf("90 days: %v", u)
	}
	u, _ = top("?days=45")
	if fmt.Sprint(u) != fmt.Sprint(want) {
		t.Fatalf("bad range must fall back to 30: %v", u)
	}
	// an install without traffic lists nobody
	empty := newP4(t)
	if u, _ := func() (us []string, d *goquery.Document) {
		d = empty.c.Get("/admin/usage").Doc(t)
		d.Find("tr[data-top]").Each(func(_ int, s *goquery.Selection) { us = append(us, s.AttrOr("data-username", "")) })
		return
	}(); len(u) != 0 {
		t.Fatalf("empty install lists %v", u)
	}
}

// E9.09 the page is behind the admin login and linked from the navigation.
func TestE9_09_NavAndAuth(t *testing.T) {
	f := newP4(t)
	if f.c.Get("/admin/usage").Doc(t).Find(`a[href="/admin/usage"]`).Length() == 0 {
		t.Fatal("no nav link to /admin/usage")
	}
	anon := harness.NewAdminClient(t, f.s)
	if r := anon.GetPage("/admin/usage"); r.StatusCode != 302 && r.StatusCode != 303 {
		t.Fatalf("anonymous /admin/usage: %d", r.StatusCode)
	}
}
