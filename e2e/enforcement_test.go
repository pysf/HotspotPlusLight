package e2e

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"layeh.com/radius/rfc2865"
	"layeh.com/radius/rfc2866"

	"hotspotpluslight/e2e/fakenas"
	"hotspotpluslight/e2e/harness"
	"hotspotpluslight/internal/clock"
)

// newP11 is the Phase 5 fixture (real Disconnect client, fake router) with a server log we can read.
func newP11(t *testing.T) *p5 {
	t.Helper()
	fc := clock.NewFake(t0)
	hook := &beforeCommit{}
	logs := &harness.SafeBuffer{}
	s := harness.Start(t, harness.Options{Clock: fc, AccountingBeforeCommit: hook.run, Logger: slog.New(slog.NewTextHandler(logs, nil))})
	f := &p2{t: t, s: s, fc: fc, c: harness.LoginAsAdmin(t, s), db: harness.DB(t, s.DataDir)}
	secret := scalar[string](t, f.db, `SELECT radius_shared_secret FROM settings`)
	nas := fakenas.New(s.Addrs.RadiusAuth, secret)
	nas.AcctAddr = s.Addrs.RadiusAcct
	coa := fakenas.ListenCoA(t, secret)
	f.exec(`UPDATE settings SET coa_port=?`, coa.Port())
	return &p5{p4: &p4{p3: &p3{p2: f, nas: nas, log: logs}, hook: hook}, coa: coa}
}

// settle waits until the server has finished every automatic-disconnect check.
func (f *p5) settle() {
	f.t.Helper()
	waitUntil(f.t, "enforcement idle", f.s.App.EnforcementIdle)
}

// serverKicks waits for n Disconnect-Requests at the fake router and then for the server to be idle.
func (f *p5) serverKicks(n int) {
	f.t.Helper()
	waitUntil(f.t, "Disconnect-Requests at the router", func() bool { return len(f.coa.Requests()) >= n })
	f.settle()
	if got := len(f.coa.Requests()); got != n {
		f.t.Fatalf("%d Disconnect-Requests, want %d", got, n)
	}
}

func (f *p5) noKicks() {
	f.t.Helper()
	f.settle()
	time.Sleep(100 * time.Millisecond)
	if n := len(f.coa.Requests()); n != 0 {
		f.t.Fatalf("%d Disconnect-Requests, want none", n)
	}
}

// E11.01 data left: Xmit-Limit and Recv-Limit carry the bytes left; from 4 GiB the high 32 bits go to the
// -Gigawords attributes (O3).
func TestE11_01_DataLimitsAndGigawords(t *testing.T) {
	f := newP3(t)
	id := f.member(f.plan("Five", "bulk_value", "5", "bulk_unit", "GB"), "alice", "password", "pw")
	for _, c := range []struct {
		used   int64
		want   uint64
		giga   bool
		reason string
	}{
		{gb, uint64(4 * gb), true, "exactly 4 GiB left"},
		{gb + 1, uint64(4*gb - 1), false, "4 GiB - 1 left"},
		{0, uint64(5 * gb), true, "5 GiB left"},
		{4*gb + 100*mb, uint64(gb - 100*mb), false, "under 1 GiB left"},
	} {
		f.exec(`UPDATE member SET used_download_bytes=?, used_upload_bytes=0 WHERE id=?`, c.used, id)
		r := f.accepted("alice", "pw")
		recv, xmit, ok := dataLimits(t, r)
		if !ok || recv != c.want || xmit != c.want {
			t.Errorf("%s: recv %d xmit %d, want %d", c.reason, recv, xmit, c.want)
		}
		if has := vsa(t, r, 14) != nil && vsa(t, r, 15) != nil; has != c.giga {
			t.Errorf("%s: Gigawords attributes present=%v, want %v", c.reason, has, c.giga)
		}
	}
	// Upload and download both use up the allowance.
	f.exec(`UPDATE member SET used_download_bytes=?, used_upload_bytes=? WHERE id=?`, gb, gb, id)
	if recv, xmit, _ := dataLimits(t, f.accepted("alice", "pw")); recv != uint64(3*gb) || xmit != uint64(3*gb) {
		t.Errorf("recv %d xmit %d, want %d", recv, xmit, 3*gb)
	}
}

// E11.02 unlimited data: no data-limit attribute, but Session-Timeout is still sent.
func TestE11_02_UnlimitedDataNoLimit(t *testing.T) {
	f := newP3(t)
	id := f.member(f.plan("Free", "bulk_value", "0"), "alice", "password", "pw")
	f.exec(`UPDATE member SET used_download_bytes=?, used_upload_bytes=? WHERE id=?`, 80*gb, 20*gb, id)
	r := f.accepted("alice", "pw")
	if _, _, ok := dataLimits(t, r); ok {
		t.Error("data-limit attributes sent for unlimited data")
	}
	if st := sessionTimeout(r); st != 30*24*3600 {
		t.Errorf("Session-Timeout %d, want the 30-day window", st)
	}
}

// E11.03 Session-Timeout is the smallest of time left, expires_at and window end; auto-renew plans are cut at
// the window end too. Never 172800 (O4).
func TestE11_03_SessionTimeoutSmallest(t *testing.T) {
	f := newP3(t)
	hour := f.plan("Hour", "time_limit_minutes", "60")
	oneDay := f.plan("OneDay", "time_limit_minutes", "60", "duration_days", "1")
	auto := f.plan("Auto", "time_limit_minutes", "0", "duration_days", "1", "auto_resubscribe", "on")
	free := f.plan("Free", "time_limit_minutes", "0")
	for _, c := range []struct {
		name string
		plan int64
		sql  string // sets the state; ? is the member id
		args []any
		want uint32
	}{
		{"time left wins", hour, `UPDATE member SET used_seconds=3000 WHERE id=?`, nil, 600},
		{"expires_at wins", hour, `UPDATE member SET expires_at=? WHERE id=?`, []any{t0.Unix() + 300}, 300},
		{"window end wins", oneDay, `UPDATE member SET subscription_date=? WHERE id=?`, []any{t0.Unix() - 86400 + 200}, 200},
		{"window end, auto-renew", auto, `UPDATE member SET subscription_date=? WHERE id=?`, []any{t0.Unix() - 86400 + 200}, 200},
		{"no limits: the window, not 48 h", free, `UPDATE member SET used_seconds=0 WHERE id=?`, nil, 30 * 86400},
		{"expires_at after the window end loses", oneDay, `UPDATE member SET expires_at=? WHERE id=?`, []any{t0.Unix() + 10*86400}, 3600},
		{"expires_at tied with time left", hour, `UPDATE member SET expires_at=?, used_seconds=3000 WHERE id=?`, []any{t0.Unix() + 600}, 600},
	} {
		user := strings.ReplaceAll(strings.ToLower(c.name), " ", "")
		user = strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' {
				return r
			}
			return -1
		}, user)
		id := f.member(c.plan, user, "password", "pw")
		f.exec(c.sql, append(c.args, id)...)
		if st := sessionTimeout(f.accepted(user, "pw")); st != c.want {
			t.Errorf("%s: Session-Timeout %d, want %d", c.name, st, c.want)
		}
	}
}

// E11.04 an Interim that goes over the data quota: the Accounting-Response first, then the server sends a
// Disconnect with the right identifiers, and the row ends with "Server: OUT_OF_BULK".
func TestE11_04_DataOverQuotaKicks(t *testing.T) {
	f := newP11(t)
	f.member(f.plan("Tiny", "bulk_value", "1", "bulk_unit", "MB"), "alice", "password", "pw")
	f.accepted("alice", "pw")
	f.online("alice", "s1", "192.168.88.10", "AA:BB:CC:DD:EE:01")
	f.interim("alice", "s1", uint64(mb/2), 0, 30) // the Accounting-Response comes back (f.acct requires it)
	f.noKicks()
	f.interim("alice", "s1", uint64(mb), uint64(mb/4), 60)
	f.serverKicks(1)
	d := f.coa.Requests()[0]
	if !d.Authentic {
		t.Error("Disconnect not signed with the shared secret")
	}
	if u := rfc2865.UserName_GetString(d.Packet); u != "alice" {
		t.Errorf("User-Name %q", u)
	}
	if sid := rfc2866.AcctSessionID_GetString(d.Packet); sid != "s1" {
		t.Errorf("Acct-Session-Id %q", sid)
	}
	if ip, _ := rfc2865.FramedIPAddress_Lookup(d.Packet); ip.String() != "192.168.88.10" {
		t.Errorf("Framed-IP-Address %v", ip)
	}
	if mac := rfc2865.CallingStationID_GetString(d.Packet); mac != "AA:BB:CC:DD:EE:01" {
		t.Errorf("Calling-Station-Id %q", mac)
	}
	s := f.session("s1")
	if !s.ended.Valid || s.cause != "Server: OUT_OF_BULK" {
		t.Fatalf("row ended=%v cause=%q", s.ended, s.cause)
	}
	if got := onlineUsers(f.p2); len(got) != 0 {
		t.Fatalf("still online: %v", got)
	}
	// The router's Stop completes the row; the cause stays, the usage is counted once.
	f.stop("alice", "s1", uint64(mb), uint64(mb/4), 60)
	if c := f.cause("s1"); c != "Server: OUT_OF_BULK" {
		t.Errorf("cause after Stop: %q", c)
	}
	f.rejected("alice", "pw", "OUT_OF_BULK")
}

// E11.05 the same over the time quota.
func TestE11_05_TimeOverQuotaKicks(t *testing.T) {
	f := newP11(t)
	f.member(f.plan("Short", "time_limit_minutes", "2"), "alice", "password", "pw")
	f.online("alice", "s1", "192.168.88.10", "AA:BB:CC:DD:EE:01")
	f.interim("alice", "s1", 10, 10, 90)
	f.noKicks()
	f.interim("alice", "s1", 20, 20, 120)
	f.serverKicks(1)
	if c := f.cause("s1"); c != "Server: OUT_OF_TIME" {
		t.Errorf("cause %q", c)
	}
}

// E11.06 the admin sets expires_at in the past while the member is online: kick at the next Interim, not before.
func TestE11_06_ExpiryKicks(t *testing.T) {
	f := newP11(t)
	p := f.plan("Basic")
	id := f.member(p, "alice", "password", "pw")
	f.online("alice", "s1", "192.168.88.10", "AA:BB:CC:DD:EE:01")
	f.settle() // the Start's own check is done
	form := harness.MemberForm(p, "alice", "expires_at", "2026-10-31T10:00")
	if r := f.c.Send("PUT", mpath(id), form); r.StatusCode != 200 {
		t.Fatalf("edit: %d %s", r.StatusCode, r.Body)
	}
	f.noKicks() // layer 2 runs on the Interim
	f.advance(60 * time.Second)
	f.interim("alice", "s1", 100, 100, 60)
	f.serverKicks(1)
	if c := f.cause("s1"); c != "Server: EXPIRED" {
		t.Errorf("cause %q", c)
	}
}

// E11.07 the window of an auto-renew plan ends while the member is online: kick at the next Interim (cut at the
// window end). The next login renews: counters are reset.
func TestE11_07_AutoRenewCutAtWindowEnd(t *testing.T) {
	f := newP11(t)
	id := f.member(f.plan("Daily", "duration_days", "1", "auto_resubscribe", "on"), "alice", "password", "pw")
	f.accepted("alice", "pw")
	f.online("alice", "s1", "192.168.88.10", "AA:BB:CC:DD:EE:01")
	f.settle() // the Start's own check is done
	f.interim("alice", "s1", 1000, 100, 60)
	f.noKicks()
	f.advance(25 * time.Hour)
	f.interim("alice", "s1", 2000, 200, 120)
	f.serverKicks(1)
	if c := f.cause("s1"); c != "Server: NO_ACTIVE_SUBSCRIPTION" {
		t.Errorf("cause %q", c)
	}
	f.accepted("alice", "pw") // login renews
	if u := f.usage(id); u != (usage{}) {
		t.Errorf("counters after the renewal: %+v", u)
	}
	if sub := scalar[int64](t, f.db, `SELECT subscription_date FROM member WHERE id=?`, id); sub != f.fc.Now().Unix() {
		t.Errorf("window restarted at %d, now %d", sub, f.fc.Now().Unix())
	}
}

// E11.08 under quota nothing is sent, however many Interims arrive (keeps the intent of E5.09).
func TestE11_08_UnderQuotaNoKick(t *testing.T) {
	f := newP11(t)
	f.member(f.plan("Big", "time_limit_minutes", "600"), "alice", "password", "pw")
	f.member(f.plan("Free", "bulk_value", "0", "time_limit_minutes", "0"), "bob", "password", "pw")
	f.online("alice", "a1", "192.168.88.10", "AA:BB:CC:DD:EE:01")
	f.online("bob", "b1", "192.168.88.11", "AA:BB:CC:DD:EE:02")
	for i := uint64(1); i <= 5; i++ {
		f.advance(60 * time.Second)
		f.interim("alice", "a1", i*mbU, i*mbU, uint32(60*i))
		f.interim("bob", "b1", 50*i*mbU, i*mbU, uint32(60*i))
	}
	f.noKicks()
	if got := onlineUsers(f.p2); len(got) != 2 {
		t.Fatalf("online: %v", got)
	}
}

const mbU = uint64(1 << 20)

// E11.09 the router says NAK: at most 3 tries in total across many Interims, spaced by 2 x the accounting
// interval; then a warning in the log and no more tries.
func TestE11_09_RetryLimit(t *testing.T) {
	f := newP11(t)
	f.member(f.plan("Tiny", "bulk_value", "1", "bulk_unit", "MB"), "alice", "password", "pw")
	f.online("alice", "s1", "192.168.88.10", "AA:BB:CC:DD:EE:01")
	f.coa.Always(fakenas.NAK)
	over := func() { f.interim("alice", "s1", uint64(2*mb), 0, 60) }
	over()
	f.serverKicks(1)
	over() // same moment: inside the window
	f.noKicksMore(1)
	f.fc.Advance(90 * time.Second) // still inside 2 x 60 s
	over()
	f.noKicksMore(1)
	f.fc.Advance(40 * time.Second) // 130 s after the first try
	over()
	f.serverKicks(2)
	over()
	f.noKicksMore(2)
	f.fc.Advance(121 * time.Second)
	over()
	f.serverKicks(3)
	if !strings.Contains(f.log.String(), "giving up") {
		t.Errorf("no warning after the third failure:\n%s", f.log.String())
	}
	for range 3 {
		f.fc.Advance(5 * time.Minute)
		over()
		f.noKicksMore(3)
	}
	if f.session("s1").ended.Valid {
		t.Error("row ended although the router never ACKed")
	}
}

// noKicksMore asserts the router has received exactly n Disconnect-Requests, after the server is idle.
func (f *p5) noKicksMore(n int) {
	f.t.Helper()
	f.settle()
	time.Sleep(50 * time.Millisecond)
	if got := len(f.coa.Requests()); got != n {
		f.t.Fatalf("%d Disconnect-Requests, want %d", got, n)
	}
}

// E11.09b a silent router never delays the Accounting-Response (the Disconnect takes seconds to give up).
func TestE11_09b_SilentRouterDoesNotDelayAccounting(t *testing.T) {
	f := newP11(t)
	f.member(f.plan("Tiny", "bulk_value", "1", "bulk_unit", "MB"), "alice", "password", "pw")
	f.online("alice", "s1", "192.168.88.10", "AA:BB:CC:DD:EE:01")
	f.coa.Always(fakenas.Silent)
	start := time.Now()
	f.interim("alice", "s1", uint64(2*mb), 0, 60)
	if d := time.Since(start); d > time.Second {
		t.Errorf("Accounting-Response took %v", d)
	}
	waitUntil(t, "the Disconnect-Request", func() bool { return len(f.coa.Requests()) >= 1 })
	f.settle() // the router never answered
	if f.session("s1").ended.Valid {
		t.Error("row ended without an ACK")
	}
}

// E11.10 two sessions share one quota and together go over it: both get kicked (each at its own Interim).
func TestE11_10_SharedQuotaKicksBoth(t *testing.T) {
	f := newP11(t)
	f.member(f.plan("Two", "bulk_value", "1", "bulk_unit", "MB", "max_sessions", "2"), "alice", "password", "pw")
	f.online("alice", "s1", "192.168.88.10", "AA:BB:CC:DD:EE:01")
	f.online("alice", "s2", "192.168.88.11", "AA:BB:CC:DD:EE:02")
	f.interim("alice", "s1", uint64(600*1024), 0, 60)
	f.noKicks()
	f.interim("alice", "s2", uint64(600*1024), 0, 60) // together 1.17 MB
	f.serverKicks(1)
	f.interim("alice", "s1", uint64(600*1024), 0, 120)
	f.serverKicks(2)
	for _, sid := range []string{"s1", "s2"} {
		if s := f.session(sid); !s.ended.Valid || s.cause != "Server: OUT_OF_BULK" {
			t.Errorf("%s: ended=%v cause=%q", sid, s.ended, s.cause)
		}
	}
	got := map[string]bool{}
	for _, d := range f.coa.Requests() {
		got[rfc2866.AcctSessionID_GetString(d.Packet)] = true
	}
	if !got["s1"] || !got["s2"] {
		t.Errorf("Disconnects for %v", got)
	}
}

// E11.11 the admin moves the member to a smaller plan while online: kick at the next Interim. The plan change
// itself is stored as before (E2.06).
func TestE11_11_SmallerPlanKicks(t *testing.T) {
	f := newP11(t)
	big := f.plan("Big", "bulk_value", "10", "bulk_unit", "GB")
	small := f.plan("Small", "bulk_value", "1", "bulk_unit", "MB")
	id := f.member(big, "alice", "password", "pw")
	f.online("alice", "s1", "192.168.88.10", "AA:BB:CC:DD:EE:01")
	f.settle()                                    // the Start's own check is done
	f.interim("alice", "s1", uint64(5*mb), 0, 60) // fine on the big plan
	f.noKicks()
	if r := f.c.Send("PUT", mpath(id), harness.MemberForm(small, "alice")); r.StatusCode != 200 {
		t.Fatalf("change plan: %d %s", r.StatusCode, r.Body)
	}
	if got := scalar[int64](t, f.db, `SELECT plan_id FROM member WHERE id=?`, id); got != small {
		t.Fatalf("plan_id %d, want %d", got, small)
	}
	f.noKicks() // not before the next Interim
	f.interim("alice", "s1", uint64(5*mb), 0, 120)
	f.serverKicks(1)
	if c := f.cause("s1"); c != "Server: OUT_OF_BULK" {
		t.Errorf("cause %q", c)
	}
}

// E11.12 a Stop, an unknown user, or a member under quota never triggers a Disconnect.
func TestE11_12_StopAndUnknownUserNeverKick(t *testing.T) {
	f := newP11(t)
	id := f.member(f.plan("Tiny", "bulk_value", "1", "bulk_unit", "MB"), "alice", "password", "pw")
	f.online("alice", "s1", "192.168.88.10", "AA:BB:CC:DD:EE:01")
	f.noKicks()                                                                      // the Start's own check is done
	f.exec(`UPDATE member SET used_download_bytes=?, active=0 WHERE id=?`, 5*mb, id) // over quota and disabled
	f.stop("alice", "s1", uint64(2*mb), 0, 60)
	f.noKicks()
	f.online("ghost", "g1", "192.168.88.20", "AA:BB:CC:DD:EE:09")
	f.interim("ghost", "g1", uint64(5*mb), 0, 60)
	f.acct(fakenas.Acct{Type: rfc2866.AcctStatusType_Value_Stop, User: "ghost", SessionID: "g1"})
	f.noKicks()
}
