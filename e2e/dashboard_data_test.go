package e2e

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/vendors/microsoft"

	"hotspotpluslight/e2e/fakenas"
	"hotspotpluslight/e2e/harness"
	"hotspotpluslight/internal/domain"
)

// failedTotal is the sum of every failed-login count in the table.
func failedTotal(f *p3) int64 {
	f.t.Helper()
	return scalar[int64](f.t, f.db, `SELECT COALESCE(SUM(count), 0) FROM login_failure_daily`)
}

// failedOn is the count of one reason on one day key.
func failedOn(f *p3, day string, reason domain.Reason) int64 {
	f.t.Helper()
	return scalar[int64](f.t, f.db, `SELECT COALESCE(SUM(count), 0) FROM login_failure_daily WHERE day=? AND reason=?`, day, string(reason))
}

// sendRaw writes wire on conn and reads one reply datagram. It is how a router repeats a packet from one source port.
func sendRaw(t *testing.T, conn net.Conn, wire []byte) {
	t.Helper()
	if _, err := conn.Write(wire); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Read(make([]byte, 4096)); err != nil {
		t.Fatalf("no reply: %v", err)
	}
}

// E17.01 (Phase 17) migration 0007 applies on an empty database and on a 0006 database, and keeps the data.
func TestE17_01_Migration0007(t *testing.T) {
	// An empty database gets the table.
	s := harness.Start(t, harness.Options{})
	db := harness.DB(t, s.DataDir)
	if n := scalar[int](t, db, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name='login_failure_daily'`); n != 1 {
		t.Fatal("login_failure_daily is missing on a new database")
	}
	if n := scalar[int](t, db, `SELECT count(*) FROM schema_migrations`); n != wantMigrations {
		t.Errorf("%d migrations, want %d", n, wantMigrations)
	}

	// A 0006 database keeps its data.
	read := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join(harness.ModuleRoot(), "internal/store/migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	old := fstest.MapFS{}
	for _, n := range []string{"0001_init.sql", "0002_usage_daily.sql", "0003_admin_must_change.sql", "0004_drop_page_api_settings.sql", "0005_business_name.sql", "0006_server_settings.sql"} {
		old[n] = &fstest.MapFile{Data: read(n)}
	}
	in := seedOldDB(t, old)
	odb := harness.DB(t, in.DataDir)
	if scalar[int](t, odb, `SELECT count(*) FROM sqlite_master WHERE name='login_failure_daily'`) != 0 {
		t.Fatal("before 0007 the table must not exist")
	}
	if _, err := odb.Exec(`UPDATE settings SET timezone='Asia/Tehran', coa_port=3800, business_name='Hotel Aria'`); err != nil {
		t.Fatal(err)
	}
	startUpgraded(t, in)
	if n := scalar[int](t, odb, `SELECT count(*) FROM schema_migrations`); n != wantMigrations {
		t.Errorf("%d migrations after the upgrade, want %d", n, wantMigrations)
	}
	if scalar[int](t, odb, `SELECT count(*) FROM sqlite_master WHERE name='login_failure_daily'`) != 1 {
		t.Error("the table was not created by the upgrade")
	}
	if scalar[string](t, odb, `SELECT timezone FROM settings`) != "Asia/Tehran" || scalar[int](t, odb, `SELECT coa_port FROM settings`) != 3800 ||
		scalar[string](t, odb, `SELECT business_name FROM settings`) != "Hotel Aria" || scalar[int](t, odb, `SELECT count(*) FROM admin`) != 1 {
		t.Error("data did not survive migration 0007")
	}
}

// E17.02 every reject reason that the server sends adds 1 to its (day, reason) count.
func TestE17_02_EveryRejectReasonIsCounted(t *testing.T) {
	f := newP3(t)
	today := domain.DayKey(f.fc.Now(), time.UTC)
	seen := map[domain.Reason]bool{}
	reject := func(want domain.Reason, send func() *radius.Packet) {
		t.Helper()
		before, beforeTotal := failedOn(f, today, want), failedTotal(f)
		r := send()
		if r.Code != radius.CodeAccessReject || rfc2865.ReplyMessage_GetString(r) != string(want) {
			t.Fatalf("%s: got %v %q", want, r.Code, rfc2865.ReplyMessage_GetString(r))
		}
		if got := failedOn(f, today, want); got != before+1 {
			t.Errorf("%s: count %d, want %d", want, got, before+1)
		}
		if got := failedTotal(f); got != beforeTotal+1 {
			t.Errorf("%s: total %d, want %d", want, got, beforeTotal+1)
		}
		seen[want] = true
	}
	login := func(user, pw string) func() *radius.Packet {
		return func() *radius.Packet { return f.login(user, pw) }
	}
	day := int64(24 * 3600)
	basic := f.plan("Basic")
	alice := f.member(basic, "alice", "password", "pw")
	reject(domain.ReasonInvalidCredentials, login("alice", "wrong"))
	reject(domain.ReasonInvalidCredentials, login("nobody", "pw")) // an unknown user is the same reason
	f.exec(`UPDATE member SET active=0 WHERE id=?`, alice)
	reject(domain.ReasonInactive, login("alice", "pw"))
	f.exec(`UPDATE member SET active=1, expires_at=? WHERE id=?`, t0.Unix()-1, alice)
	reject(domain.ReasonExpired, login("alice", "pw"))
	f.exec(`UPDATE member SET expires_at=NULL, subscription_date=? WHERE id=?`, t0.Unix()-400*day, alice)
	reject(domain.ReasonNoSubscription, login("alice", "pw"))

	small := f.member(f.plan("Small", "bulk_value", "100", "bulk_unit", "MB"), "small", "password", "pw")
	f.exec(`UPDATE member SET used_download_bytes=?, used_upload_bytes=0 WHERE id=?`, 200*mb, small)
	reject(domain.ReasonOutOfBulk, login("small", "pw"))

	hour := f.member(f.plan("Hour", "time_limit_minutes", "60"), "hour", "password", "pw")
	f.exec(`UPDATE member SET used_seconds=3600 WHERE id=?`, hour)
	reject(domain.ReasonOutOfTime, login("hour", "pw"))

	one := f.member(f.plan("One", "max_sessions", "1"), "one", "password", "pw")
	f.openSession(one, "one", "s1", f.fc.Now())
	reject(domain.ReasonNoMoreSession, login("one", "pw"))

	// NO_PLAN and PLAN_REMOVED need rows the UI never creates (see E3.04): foreign keys off.
	np := f.member(basic, "noplan", "password", "pw")
	conn, err := f.db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(t.Context(), `PRAGMA foreign_keys=OFF`); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		plan int64
		want domain.Reason
	}{{0, domain.ReasonNoPlan}, {9999, domain.ReasonPlanRemoved}} {
		if _, err := conn.ExecContext(t.Context(), `UPDATE member SET plan_id=? WHERE id=?`, c.plan, np); err != nil {
			t.Fatal(err)
		}
		reject(c.want, login("noplan", "pw"))
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
		reject(domain.ReasonUnsupportedAuth, func() *radius.Packet {
			r, err := f.nas.Exchange(p)
			if err != nil {
				t.Fatal(err)
			}
			return r
		})
	}

	for _, r := range domain.AllReasons() {
		if !seen[r] {
			t.Errorf("reason %s was not exercised", r)
		}
	}
}

// E17.03 an Access-Accept adds nothing, and neither does a packet that gets no reply. Bad-secret packets still show in the link status.
func TestE17_03_OnlyRejectsAreCounted(t *testing.T) {
	f := newP3(t)
	f.member(f.plan("Basic"), "alice", "password", "pw")
	f.accepted("alice", "pw")
	if n := failedTotal(f); n != 0 {
		t.Fatalf("an Accept was counted: %d", n)
	}

	bad := fakenas.New(f.s.Addrs.RadiusAuth, "not-the-shared-secret")
	bad.Timeout = 500 * time.Millisecond
	for _, ma := range []bool{true, false} { // a bad Message-Authenticator, then the heuristic path
		bad.MessageAuth = ma
		if silent, err := bad.Silent(bad.PAPPacket("alice", "pw")); err != nil || !silent {
			t.Fatalf("wrong secret (MA %v) got a reply: silent=%v err=%v", ma, silent, err)
		}
	}
	f.exec(`ALTER TABLE member RENAME TO member_broken`) // a database failure sends no reply (E3.18)
	quick := fakenas.New(f.s.Addrs.RadiusAuth, f.nas.Secret)
	quick.Timeout = 500 * time.Millisecond
	if silent, err := quick.Silent(quick.PAPPacket("alice", "pw")); err != nil || !silent {
		t.Fatalf("database failure produced a reply: silent=%v err=%v", silent, err)
	}
	f.exec(`ALTER TABLE member_broken RENAME TO member`)

	if n := failedTotal(f); n != 0 {
		t.Errorf("packets without a reply were counted: %d", n)
	}
	d := f.c.Get("/admin/dashboard/link-status").Doc(t)
	if got := text(d.Find("[data-bad-secret-count]")); got == "0" || got == "" {
		t.Errorf("bad-secret packets are missing from the link status: %q", got)
	}
}

// E17.04 a retransmission (same source port, Identifier and Request Authenticator) is counted once;
// the same packet after 60 seconds counts again; a new request counts.
func TestE17_04_RetransmissionCountsOnce(t *testing.T) {
	f := newP3(t)
	today := domain.DayKey(f.fc.Now(), time.UTC)
	conn, err := net.Dial("udp", f.s.Addrs.RadiusAuth)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	p := f.nas.PAPPacket("nobody", "x")
	wire, err := p.Encode()
	if err != nil {
		t.Fatal(err)
	}
	for range 3 { // a router repeats the packet every few seconds when it gets no reply
		sendRaw(t, conn, wire)
	}
	if n := failedOn(f, today, domain.ReasonInvalidCredentials); n != 1 {
		t.Fatalf("a retransmission was counted %d times, want 1", n)
	}
	f.fc.Advance(61 * time.Second)
	sendRaw(t, conn, wire)
	if n := failedOn(f, today, domain.ReasonInvalidCredentials); n != 2 {
		t.Fatalf("the same packet after 60 s: count %d, want 2", n)
	}
	f.login("nobody", "x") // a new request (new Identifier and authenticator)
	if n := failedOn(f, today, domain.ReasonInvalidCredentials); n != 3 {
		t.Fatalf("a new request: count %d, want 3", n)
	}
}

// E17.05 the day key follows settings.timezone across midnight; a later timezone change leaves old rows alone.
func TestE17_05_DayKeyFollowsTimezone(t *testing.T) {
	f := newP3(t)                                             // the clock starts at 2026-11-01 12:00 UTC
	f.advance(11*time.Hour + 59*time.Minute + 30*time.Second) // keeps the admin session alive
	f.login("nobody", "x")                                    // 23:59:30 on the 1st
	f.advance(time.Minute)
	f.login("nobody", "x") // 00:00:30 on the 2nd
	r := domain.ReasonInvalidCredentials
	if failedOn(f, "2026-11-01", r) != 1 || failedOn(f, "2026-11-02", r) != 1 {
		t.Fatalf("rows across midnight: %v", failedTotal(f))
	}

	// Asia/Tehran is UTC+3:30: 21:00 UTC on the 2nd is already the 3rd there.
	form := harness.FormValues(t, f.c.Get("/admin/settings").Doc(t), "form#settings-form")
	form.Set("timezone", "Asia/Tehran")
	if resp := f.c.Send("PUT", "/admin/settings", form); resp.StatusCode != 200 {
		t.Fatalf("timezone: %d %s", resp.StatusCode, resp.Body)
	}
	f.advance(20*time.Hour + 30*time.Minute)
	f.login("nobody", "x")
	if failedOn(f, "2026-11-03", r) != 1 || failedOn(f, "2026-11-02", r) != 1 || failedOn(f, "2026-11-01", r) != 1 {
		t.Errorf("after the timezone change the rows are wrong, total %d", failedTotal(f))
	}
	if n := scalar[int](t, f.db, `SELECT count(*) FROM login_failure_daily`); n != 3 {
		t.Errorf("%d rows, want 3 (old rows must stay as they were)", n)
	}
}

// E17.06 pruning removes counts of days before the cutoff day, keeps newer ones, and retention 0 keeps everything.
func TestE17_06_Pruning(t *testing.T) {
	f := newP3(t) // now is 2026-11-01 12:00 UTC, so a 90-day cutoff falls on 2026-08-03
	for _, d := range []string{"2026-08-02", "2026-08-03", "2026-10-30"} {
		f.exec(`INSERT INTO login_failure_daily(day, reason, count) VALUES (?, 'EXPIRED', 4)`, d)
	}
	cur := f.s
	restart := func() {
		cur.Stop()
		cur = harness.Start(t, harness.Options{Clock: f.fc, DataDir: f.s.DataDir, SecretKey: f.s.SecretKey})
	}
	days := func() []string {
		rows, err := f.db.Query(`SELECT day FROM login_failure_daily ORDER BY day`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var d string
			rows.Scan(&d)
			out = append(out, d)
		}
		return out
	}
	// The prune runs in the background after start: poll until the table has n rows.
	waitDays := func(n int) []string {
		deadline := time.Now().Add(5 * time.Second)
		for {
			got := days()
			if len(got) == n || time.Now().After(deadline) {
				return got
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	f.exec(`UPDATE settings SET session_retention_days=0`)
	restart()
	if got := days(); len(got) != 3 {
		t.Fatalf("retention 0 must keep everything, left %v", got)
	}
	f.exec(`UPDATE settings SET session_retention_days=90`)
	restart()
	if got := waitDays(2); strings.Join(got, ",") != "2026-08-03,2026-10-30" {
		t.Fatalf("after 90-day pruning left %v", got)
	}
}

// E17.07 the table holds only day, reason and count, and a reject writes no session or member row.
func TestE17_07_OnlyCountsAreStored(t *testing.T) {
	f := newP3(t)
	f.member(f.plan("Basic"), "alice", "password", "pw")
	cols := scalar[string](t, f.db, `SELECT group_concat(name) FROM (SELECT name FROM pragma_table_info('login_failure_daily') ORDER BY cid)`)
	if cols != "day,reason,count" {
		t.Errorf("columns %q, want day,reason,count", cols)
	}
	members, sessions := memberCount(t, f.db), scalar[int](t, f.db, `SELECT count(*) FROM session`)
	f.rejected("alice", "wrong", "INVALID_CREDENTIALS")
	f.rejected("nobody", "wrong", "INVALID_CREDENTIALS")
	if memberCount(t, f.db) != members || scalar[int](t, f.db, `SELECT count(*) FROM session`) != sessions {
		t.Error("a reject wrote a member or session row")
	}
	if failedTotal(f) != 2 {
		t.Errorf("total %d, want 2", failedTotal(f))
	}
	// No username or address anywhere in the stored rows.
	if n := scalar[int](t, f.db, `SELECT count(*) FROM login_failure_daily WHERE day LIKE '%alice%' OR reason LIKE '%alice%' OR day LIKE '%127.0.0.1%' OR reason LIKE '%127.0.0.1%'`); n != 0 {
		t.Error("a username or address was stored")
	}
}

// E17.08 a counter that cannot be written never stops the Access-Reject; the error is logged.
func TestE17_08_CounterFailureStillRejects(t *testing.T) {
	f := newP3(t)
	f.exec(`ALTER TABLE login_failure_daily RENAME TO login_failure_broken`)
	f.rejected("nobody", "x", "INVALID_CREDENTIALS")
	if !strings.Contains(f.log.String(), "failed-login counter not written") {
		t.Errorf("counter failure not logged:\n%s", f.log.String())
	}
	f.exec(`ALTER TABLE login_failure_broken RENAME TO login_failure_daily`)
	f.rejected("nobody", "x", "INVALID_CREDENTIALS")
	if n := failedTotal(f); n != 1 {
		t.Errorf("after recovery total %d, want 1", n)
	}
}
