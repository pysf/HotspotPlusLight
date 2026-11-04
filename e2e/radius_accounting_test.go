package e2e

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"layeh.com/radius"
	"layeh.com/radius/rfc2866"

	"hotspotpluslight/e2e/fakenas"
	"hotspotpluslight/e2e/harness"
	"hotspotpluslight/internal/clock"
)

// E4.01 Start creates a session row; the MAC is captured once (BR-12).
func TestE4_01_StartCreatesSession(t *testing.T) {
	f := newP4(t)
	id := f.member(f.plan("Basic"), "alice")
	r := f.acct(fakenas.Acct{Type: rfc2866.AcctStatusType_Value_Start, User: "alice", SessionID: "s1", FramedIP: "192.168.88.10", MAC: "AA:BB:CC:DD:EE:01"})
	if len(r.Attributes) != 0 {
		t.Errorf("Accounting-Response carries attributes: %v", r.Attributes)
	}
	s := f.session("s1")
	if s.status != 1 || s.startedAt != t0.Unix() || s.lastUpdate != t0.Unix() || s.ended.Valid || s.nasIP != "127.0.0.1" ||
		s.framedIP != "192.168.88.10" || s.mac != "AA:BB:CC:DD:EE:01" || s.nasIdent != "fakenas" {
		t.Fatalf("session row %+v", s)
	}
	if got := scalar[string](t, f.db, `SELECT mac FROM member WHERE id=?`, id); got != "AA:BB:CC:DD:EE:01" {
		t.Fatalf("member mac %q", got)
	}
	// A later session from another device never overwrites it (BR-12).
	f.acct(fakenas.Acct{Type: rfc2866.AcctStatusType_Value_Start, User: "alice", SessionID: "s2", MAC: "11-22-33-44-55-66"})
	if got := scalar[string](t, f.db, `SELECT mac FROM member WHERE id=?`, id); got != "AA:BB:CC:DD:EE:01" {
		t.Fatalf("member mac overwritten: %q", got)
	}
	if f.session("s2").mac != "11:22:33:44:55:66" {
		t.Fatal("the session keeps its own device MAC")
	}
	// The username is case-insensitive and stored canonically.
	f.acct(fakenas.Acct{Type: rfc2866.AcctStatusType_Value_Start, User: "ALICE", SessionID: "s3"})
	if u := scalar[string](t, f.db, `SELECT username FROM session WHERE acct_session_id='s3'`); u != "alice" {
		t.Fatalf("session username %q", u)
	}
	f.wantUsage(id, usage{})
}

// E4.02 Interim adds deltas to the member counters (BR-11); server receive time stamps the row (O19).
func TestE4_02_InterimAddsDeltas(t *testing.T) {
	f := newP4(t)
	id := f.member(f.plan("Basic"), "alice")
	f.start("alice", "s1")
	f.fc.Advance(60 * time.Second)
	f.interim("alice", "s1", 1000, 200, 60)
	f.wantUsage(id, usage{1000, 200, 60})
	s := f.session("s1")
	if s.status != 3 || s.dl != 1000 || s.ul != 200 || s.secs != 60 || s.lastUpdate != t0.Unix()+60 || s.startedAt != t0.Unix() {
		t.Fatalf("row %+v", s)
	}
	f.fc.Advance(60 * time.Second)
	f.interim("alice", "s1", 2500, 300, 120)
	f.wantUsage(id, usage{2500, 300, 120}) // cumulative values, not summed twice
	if s := f.session("s1"); s.dl != 2500 || s.lastUpdate != t0.Unix()+120 {
		t.Fatalf("row %+v", s)
	}
}

// E4.03 retransmitted packets never count twice.
func TestE4_03_RetransmissionIdempotent(t *testing.T) {
	f := newP4(t)
	id := f.member(f.plan("Basic"), "alice")
	start := f.nas.AcctPacket(acct(rfc2866.AcctStatusType_Value_Start, "alice", "s1", 0, 0, 0))
	for range 2 {
		if _, err := f.nas.AcctExchange(start); err != nil {
			t.Fatal(err)
		}
	}
	if n := f.sessions(`acct_session_id='s1'`); n != 1 {
		t.Fatalf("Start retransmission created %d rows", n)
	}
	in := f.nas.AcctPacket(acct(rfc2866.AcctStatusType_Value_InterimUpdate, "alice", "s1", 1000, 200, 60))
	for range 3 {
		if _, err := f.nas.AcctExchange(in); err != nil {
			t.Fatal(err)
		}
	}
	f.wantUsage(id, usage{1000, 200, 60})
	st := f.nas.AcctPacket(acct(rfc2866.AcctStatusType_Value_Stop, "alice", "s1", 1500, 250, 90))
	for range 2 { // the router retransmits a Stop whose answer it did not get
		if _, err := f.nas.AcctExchange(st); err != nil {
			t.Fatal(err)
		}
	}
	f.wantUsage(id, usage{1500, 250, 90})
	if n := f.sessions(`member_id=?`, id); n != 1 {
		t.Fatalf("%d session rows", n)
	}
	// A delayed copy of an older Interim after the Stop changes nothing either.
	f.acct(acct(rfc2866.AcctStatusType_Value_InterimUpdate, "alice", "s1", 1000, 200, 60))
	f.wantUsage(id, usage{1500, 250, 90})
	if n := f.sessions(`member_id=?`, id); n != 1 {
		t.Fatalf("late Interim created a row: %d", n)
	}
}

// E4.04 counter restart (new < old) is handled (BR-11).
func TestE4_04_CounterRestart(t *testing.T) {
	f := newP4(t)
	id := f.member(f.plan("Basic"), "alice")
	f.start("alice", "s1")
	f.interim("alice", "s1", 5000, 400, 300)
	f.interim("alice", "s1", 100, 50, 10) // the router's counters restarted
	f.wantUsage(id, usage{5100, 450, 310})
	f.interim("alice", "s1", 600, 60, 70)
	f.wantUsage(id, usage{5600, 460, 370})
}

// E4.05 Gigawords are part of the totals (BR-11).
func TestE4_05_Gigawords(t *testing.T) {
	f := newP4(t)
	id := f.member(f.plan("Basic", "bulk_value", "0"), "alice")
	f.start("alice", "s1")
	const g = uint64(1) << 32
	f.interim("alice", "s1", 5*g+7, 2*g+9, 3600)
	f.wantUsage(id, usage{int64(5*g + 7), int64(2*g + 9), 3600})
	f.interim("alice", "s1", 5*g+107, 2*g+19, 3660) // crossing within the same gigaword
	f.wantUsage(id, usage{int64(5*g + 107), int64(2*g + 19), 3660})
	f.interim("alice", "s1", 6*g+1, 2*g+19, 3720) // wrap of the 32-bit octets into the next gigaword
	f.wantUsage(id, usage{int64(6*g + 1), int64(2*g + 19), 3720})
}

// E4.06 Stop closes the session and applies the final deltas.
func TestE4_06_StopClosesSession(t *testing.T) {
	f := newP4(t)
	id := f.member(f.plan("Basic"), "alice")
	f.start("alice", "s1")
	f.fc.Advance(100 * time.Second)
	f.interim("alice", "s1", 1000, 100, 60)
	f.fc.Advance(40 * time.Second)
	a := acct(rfc2866.AcctStatusType_Value_Stop, "alice", "s1", 1800, 150, 140)
	a.Cause = rfc2866.AcctTerminateCause_Value_UserRequest
	f.acct(a)
	s := f.session("s1")
	if s.status != 2 || !s.ended.Valid || s.ended.Int64 != t0.Unix()+140 || s.cause != "User-Request" || s.dl != 1800 || s.secs != 140 {
		t.Fatalf("row %+v", s)
	}
	f.wantUsage(id, usage{1800, 150, 140})
	if n := f.sessions(`member_id=? AND ended_at IS NULL`, id); n != 0 {
		t.Fatalf("%d open sessions", n)
	}
}

// E4.07 usage accumulates over several sessions in one window.
func TestE4_07_UsageAccumulatesOverSessions(t *testing.T) {
	f := newP4(t)
	id := f.member(f.plan("Basic"), "alice")
	f.start("alice", "a")
	f.stop("alice", "a", 1000, 100, 60)
	f.start("alice", "b")
	f.interim("alice", "b", 500, 50, 30)
	f.stop("alice", "b", 700, 70, 45)
	f.start("alice", "c") // still open, nothing counted yet
	f.wantUsage(id, usage{1700, 170, 105})
	if n := f.sessions(`member_id=?`, id); n != 3 {
		t.Fatalf("%d rows", n)
	}
}

// E4.08 renew mid-session: only later deltas count (BR-13b); a plan change leaves the counters alone (BR-13a).
func TestE4_08_RenewAndPlanChangeMidSession(t *testing.T) {
	f := newP4(t)
	p1, p2id := f.plan("One"), f.plan("Two", "bulk_value", "20")
	id := f.member(p1, "alice")
	f.start("alice", "s1")
	f.interim("alice", "s1", 5000, 500, 100)
	f.wantUsage(id, usage{5000, 500, 100})

	form := harness.FormValues(t, f.c.Get(mpath(id)).Doc(t), "form#member-form")
	form.Set("plan_id", strconv.FormatInt(p2id, 10))
	if r := f.c.Send("PUT", mpath(id), form); r.StatusCode != 200 {
		t.Fatalf("change plan: %d", r.StatusCode)
	}
	f.wantUsage(id, usage{5000, 500, 100})
	f.interim("alice", "s1", 6000, 600, 160)
	f.wantUsage(id, usage{6000, 600, 160})

	f.fc.Advance(time.Minute)
	if r := f.c.Send("POST", mpath(id)+"/renewals", nil); r.StatusCode != 200 && r.StatusCode != 201 {
		t.Fatalf("renew: %d", r.StatusCode)
	}
	f.wantUsage(id, usage{})
	f.interim("alice", "s1", 6900, 650, 220) // 900 / 50 / 60 after the renewal
	f.wantUsage(id, usage{900, 50, 60})
	f.stop("alice", "s1", 7000, 700, 250)
	f.wantUsage(id, usage{1000, 100, 90})
}

// E4.09 a Start that reuses an Acct-Session-Id after a Stop creates a new row.
func TestE4_09_ReusedSessionIDAfterStop(t *testing.T) {
	f := newP4(t)
	id := f.member(f.plan("Basic"), "alice")
	f.start("alice", "s1")
	f.stop("alice", "s1", 1000, 100, 60)
	f.fc.Advance(time.Hour)
	f.start("alice", "s1")
	if n := f.sessions(`acct_session_id='s1'`); n != 2 {
		t.Fatalf("%d rows, want 2", n)
	}
	if n := f.sessions(`acct_session_id='s1' AND ended_at IS NULL`); n != 1 {
		t.Fatalf("%d open rows, want 1", n)
	}
	f.interim("alice", "s1", 300, 30, 20) // belongs to the new session: counted from zero
	f.wantUsage(id, usage{1300, 130, 80})
	if s := f.session("s1"); s.startedAt != t0.Unix()+3600 || s.dl != 300 {
		t.Fatalf("new row %+v", s)
	}
}

// E4.09b a Start that reuses the id of a still-open row that already counted time (router reboot) closes the
// old row (it must not stay online or count toward max_sessions, BR-9); usage is unchanged by the close.
func TestE4_09b_ReusedSessionIDWhileOpen(t *testing.T) {
	f := newP4(t)
	id := f.member(f.plan("Basic"), "alice")
	f.start("alice", "s1")
	f.interim("alice", "s1", 1000, 100, 60)
	f.fc.Advance(time.Hour)
	f.start("alice", "s1")
	if n := f.sessions(`acct_session_id='s1'`); n != 2 {
		t.Fatalf("%d rows, want 2", n)
	}
	if n := f.sessions(`acct_session_id='s1' AND ended_at IS NULL`); n != 1 {
		t.Fatalf("%d open rows, want 1", n)
	}
	if n := f.sessions(`ended_at=? AND terminate_cause='Lost-Service' AND status=2 AND download_bytes=1000`, t0.Unix()+3600); n != 1 {
		t.Fatalf("old row not closed as expected")
	}
	f.wantUsage(id, usage{1000, 100, 60})
}

// E4.10 unknown user: a response, nothing changes.
func TestE4_10_UnknownUser(t *testing.T) {
	f := newP4(t)
	id := f.member(f.plan("Basic"), "alice")
	f.start("nobody", "s1")
	f.interim("nobody", "s1", 1000, 100, 60)
	f.stop("nobody", "s1", 1000, 100, 60)
	f.acct(acct(rfc2866.AcctStatusType_Value_Start, "", "s2", 0, 0, 0)) // no User-Name at all
	if n := f.sessions(`1=1`); n != 0 {
		t.Fatalf("%d session rows", n)
	}
	f.wantUsage(id, usage{})
}

// E4.11 Accounting-On/Off are acknowledged and change nothing (PLAN 7.3: reboot handling declined by the owner;
// PLAN section 11 still says "ends open sessions" - see docs/open-questions.md).
func TestE4_11_AccountingOnOff(t *testing.T) {
	f := newP4(t)
	id := f.member(f.plan("Basic"), "alice")
	f.start("alice", "s1")
	f.interim("alice", "s1", 1000, 100, 60)
	for _, typ := range []rfc2866.AcctStatusType{rfc2866.AcctStatusType_Value_AccountingOn, rfc2866.AcctStatusType_Value_AccountingOff} {
		f.acct(fakenas.Acct{Type: typ, SessionID: "x"})
	}
	if n := f.sessions(`member_id=? AND ended_at IS NULL`, id); n != 1 {
		t.Fatalf("open sessions %d, want 1 (untouched)", n)
	}
	f.wantUsage(id, usage{1000, 100, 60})
}

// E4.12 a bad authenticator (or a wrong secret) is dropped silently and changes nothing.
func TestE4_12_BadAuthenticatorDropped(t *testing.T) {
	f := newP4(t)
	id := f.member(f.plan("Basic"), "alice")
	p := f.nas.AcctPacket(acct(rfc2866.AcctStatusType_Value_InterimUpdate, "alice", "s1", 1000, 100, 60))
	silent, err := f.nas.AcctSilent(p, func(wire []byte) { wire[4] ^= 0xFF }) // the Request Authenticator
	if err != nil || !silent {
		t.Fatalf("bad authenticator: silent=%v err=%v", silent, err)
	}
	bad := fakenas.New(f.nas.AuthAddr, "not-the-secret")
	bad.AcctAddr, bad.Timeout = f.nas.AcctAddr, 500*time.Millisecond
	if _, err := bad.Account(acct(rfc2866.AcctStatusType_Value_InterimUpdate, "alice", "s1", 1000, 100, 60)); err == nil {
		t.Fatal("a packet signed with the wrong secret got a reply")
	}
	if n := f.sessions(`1=1`); n != 0 {
		t.Fatalf("%d session rows", n)
	}
	f.wantUsage(id, usage{})
	f.acct(acct(rfc2866.AcctStatusType_Value_InterimUpdate, "alice", "s1", 1000, 100, 60)) // the genuine one works
	f.wantUsage(id, usage{1000, 100, 60})
}

// E4.13 quota exhaustion through Interims: the next Access-Request is rejected OUT_OF_BULK, and the server
// sends one Disconnect itself (O3, changed 2026-11-04; was "no Disconnect"). The details are E11.04.
func TestE4_13_DataExhaustedViaInterims(t *testing.T) {
	f := newP4(t)
	f.member(f.plan("Tiny", "bulk_value", "1", "bulk_unit", "MB", "max_sessions", "5"), "alice", "password", "pw")
	f.accepted("alice", "pw")
	f.start("alice", "s1")
	f.interim("alice", "s1", uint64(mb/2), uint64(mb/4), 60)
	if x := xmitLimit(t, f.accepted("alice", "pw")); x != uint32(mb/4) {
		t.Fatalf("remaining %d, want %d", x, mb/4)
	}
	if len(f.disc.Calls()) != 0 {
		t.Fatalf("Disconnect sent although the member is under quota: %v", f.disc.Calls())
	}
	f.interim("alice", "s1", uint64(mb/2), uint64(mb/2), 120) // upload counts too
	waitUntil(t, "the server's Disconnect", func() bool { return len(f.disc.Calls()) == 1 })
	f.rejected("alice", "pw", "OUT_OF_BULK")
	if c := f.disc.Calls(); len(c) != 1 || c[0].Username != "alice" || c[0].AcctSessionID != "s1" {
		t.Fatalf("server-side Disconnects: %v", c)
	}
}

// E4.14 time exhaustion through Interims likewise.
func TestE4_14_TimeExhaustedViaInterims(t *testing.T) {
	f := newP4(t)
	f.member(f.plan("Short", "time_limit_minutes", "2", "max_sessions", "5"), "alice", "password", "pw")
	f.start("alice", "s1")
	f.interim("alice", "s1", 10, 10, 60)
	if sessionTimeout(f.accepted("alice", "pw")) != 60 {
		t.Fatal("Session-Timeout is not the remaining 60 s")
	}
	f.stop("alice", "s1", 20, 20, 120)
	f.rejected("alice", "pw", "OUT_OF_TIME")
}

// E4.15 the response is sent only after the transaction commits (crash injection).
func TestE4_15_ResponseAfterCommit(t *testing.T) {
	f := newP4(t)
	id := f.member(f.plan("Basic"), "alice")
	f.start("alice", "s1")
	f.nas.Timeout = 700 * time.Millisecond
	pkt := f.nas.AcctPacket(acct(rfc2866.AcctStatusType_Value_InterimUpdate, "alice", "s1", 1000, 100, 60))

	// (a) the server "crashes" before the commit: no reply, nothing stored.
	f.hook.set(func() error { return fmt.Errorf("injected crash") })
	if _, err := f.nas.AcctExchange(pkt); err == nil {
		t.Fatal("a reply was sent although the transaction did not commit")
	}
	f.wantUsage(id, usage{})
	if s := f.session("s1"); s.dl != 0 || s.status != 1 {
		t.Fatalf("rolled-back packet left a trace: %+v", s)
	}

	// (b) the router retransmits after the server is back: applied exactly once.
	f.hook.set(nil)
	f.nas.Timeout = 3 * time.Second
	for range 2 {
		if _, err := f.nas.AcctExchange(pkt); err != nil {
			t.Fatal(err)
		}
	}
	f.wantUsage(id, usage{1000, 100, 60})

	// (c) while the transaction is held open before its commit, no reply has been sent.
	entered, release := make(chan struct{}), make(chan struct{})
	f.hook.set(func() error { close(entered); <-release; return nil })
	type result struct {
		r   *radius.Packet
		err error
	}
	done := make(chan result, 1)
	go func() {
		r, err := f.nas.AcctExchange(f.nas.AcctPacket(acct(rfc2866.AcctStatusType_Value_InterimUpdate, "alice", "s1", 3000, 300, 120)))
		done <- result{r, err}
	}()
	<-entered
	select {
	case r := <-done:
		t.Fatalf("reply before commit: %+v", r)
	case <-time.After(300 * time.Millisecond):
	}
	f.wantUsage(id, usage{1000, 100, 60}) // not visible yet
	close(release)
	if r := <-done; r.err != nil || r.r.Code != radius.CodeAccountingResponse {
		t.Fatalf("after commit: %+v", r)
	}
	f.wantUsage(id, usage{3000, 300, 120}) // visible by the time the reply was seen
}

// E4.16 finished sessions older than the retention are pruned at start (and daily); 0 keeps everything (O18).
func TestE4_16_Pruning(t *testing.T) {
	fc := clock.NewFake(t0)
	s := harness.Start(t, harness.Options{Clock: fc})
	c := harness.LoginAsAdmin(t, s)
	db := harness.DB(t, s.DataDir)
	f := &p2{t: t, s: s, fc: fc, c: c, db: db}
	id := f.member(f.plan("Basic"), "alice")
	day := int64(24 * 3600)
	ins := func(sid string, ended any, last int64) {
		f.exec(`INSERT INTO session(member_id, username, acct_session_id, nas_ip, status, started_at, last_update_at, ended_at, download_bytes)
			VALUES (?,?,?,?,?,?,?,?,?)`, id, "alice", sid, "10.0.0.1", 2, last-100, last, ended, 777)
	}
	now := t0.Unix()
	ins("old-ended", now-100*day, now-100*day)
	ins("edge-ended", now-89*day, now-89*day)
	ins("recent-ended", now-10*day, now-10*day)
	ins("ghost-old", nil, now-100*day) // never got a Stop and was silent for 100 days
	ins("open-recent", nil, now-5*day)
	f.exec(`UPDATE member SET used_download_bytes=999 WHERE id=?`, id)

	cur := s
	restart := func() {
		cur.Stop()
		cur = harness.Start(t, harness.Options{Clock: fc, DataDir: s.DataDir, SecretKey: s.SecretKey})
	}
	left := func() map[string]bool {
		out := map[string]bool{}
		rows, err := db.Query(`SELECT acct_session_id FROM session`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		for rows.Next() {
			var sid string
			rows.Scan(&sid)
			out[sid] = true
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return out
	}

	// The prune runs in the background after start: poll until the row count reaches n.
	waitLeft := func(n int) map[string]bool {
		deadline := time.Now().Add(5 * time.Second)
		for {
			got := left()
			if len(got) == n || time.Now().After(deadline) {
				return got
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	f.exec(`UPDATE settings SET session_retention_days=0`)
	restart()
	if got := left(); len(got) != 5 {
		t.Fatalf("retention 0 must keep everything, left %v", got)
	}
	f.exec(`UPDATE settings SET session_retention_days=90`)
	restart()
	got := waitLeft(3)
	if len(got) != 3 || got["old-ended"] || got["ghost-old"] || !got["edge-ended"] || !got["recent-ended"] || !got["open-recent"] {
		t.Fatalf("after 90-day pruning left %v", got)
	}
	f.exec(`UPDATE settings SET session_retention_days=30`)
	restart()
	if got := waitLeft(2); len(got) != 2 || got["edge-ended"] {
		t.Fatalf("after 30-day pruning left %v", got)
	}
	// Usage counters live on the member and are not touched by pruning.
	if u := scalar[int64](t, db, `SELECT used_download_bytes FROM member WHERE id=?`, id); u != 999 {
		t.Fatalf("member usage changed by pruning: %d", u)
	}
}

// E4.17 server downtime: interims sent while the server is down are lost; the next one
// (cumulative values) catches the counters up without double counting.
func TestE4_17_DowntimeCatchUp(t *testing.T) {
	fc := clock.NewFake(t0)
	s := harness.Start(t, harness.Options{Clock: fc})
	c := harness.LoginAsAdmin(t, s)
	db := harness.DB(t, s.DataDir)
	f := &p2{t: t, s: s, fc: fc, c: c, db: db}
	id := f.member(f.plan("Basic"), "alice")
	secret := scalar[string](t, db, `SELECT radius_shared_secret FROM settings`)
	nas := fakenas.New(s.Addrs.RadiusAuth, secret)
	nas.AcctAddr = s.Addrs.RadiusAcct
	send := func(n *fakenas.NAS, typ rfc2866.AcctStatusType, dl, ul uint64, secs uint32) error {
		r, err := n.Account(acct(typ, "alice", "s1", dl, ul, secs))
		if err == nil && r.Code != radius.CodeAccountingResponse {
			t.Fatalf("reply %v", r.Code)
		}
		return err
	}
	if err := send(nas, rfc2866.AcctStatusType_Value_Start, 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := send(nas, rfc2866.AcctStatusType_Value_InterimUpdate, 1000, 100, 60); err != nil {
		t.Fatal(err)
	}

	s.Stop()
	nas.Timeout = 300 * time.Millisecond
	if err := send(nas, rfc2866.AcctStatusType_Value_InterimUpdate, 2000, 200, 120); err == nil {
		t.Fatal("server answered while down")
	}
	fc.Advance(3 * time.Minute)

	s2 := harness.Start(t, harness.Options{Clock: fc, DataDir: s.DataDir, SecretKey: s.SecretKey})
	nas2 := fakenas.New(s2.Addrs.RadiusAuth, secret)
	nas2.AcctAddr = s2.Addrs.RadiusAcct
	if err := send(nas2, rfc2866.AcctStatusType_Value_InterimUpdate, 4000, 400, 240); err != nil {
		t.Fatal(err)
	}
	var u usage
	db.QueryRow(`SELECT used_download_bytes, used_upload_bytes, used_seconds FROM member WHERE id=?`, id).Scan(&u.dl, &u.ul, &u.secs)
	if u != (usage{4000, 400, 240}) {
		t.Fatalf("usage %+v, want everything the router counted, once", u)
	}
	if n := scalar[int](t, db, `SELECT count(*) FROM session`); n != 1 {
		t.Fatalf("%d session rows", n)
	}
}
