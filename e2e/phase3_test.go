package e2e

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"testing"
	"time"

	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/rfc2869"
	"layeh.com/radius/vendors/microsoft"
	"layeh.com/radius/vendors/mikrotik"

	"hotspotpluslight/e2e/fakenas"
	"hotspotpluslight/e2e/harness"
	"hotspotpluslight/internal/clock"
)

// p3 is the Phase 3 fixture: the Phase 2 fixture plus a fake router that talks to the RADIUS port.
type p3 struct {
	*p2
	nas *fakenas.NAS
	log *harness.SafeBuffer
}

func newP3(t *testing.T) *p3 {
	t.Helper()
	fc := clock.NewFake(t0)
	disc := &harness.FakeDisconnector{}
	logs := &harness.SafeBuffer{}
	s := harness.Start(t, harness.Options{Clock: fc, Disconnector: disc, Logger: slog.New(slog.NewTextHandler(logs, nil))})
	f := &p2{t: t, s: s, fc: fc, c: harness.LoginAsAdmin(t, s), db: harness.DB(t, s.DataDir), disc: disc}
	secret := scalar[string](t, f.db, `SELECT radius_shared_secret FROM settings`)
	return &p3{p2: f, nas: fakenas.New(s.Addrs.RadiusAuth, secret), log: logs}
}

// login sends a PAP Access-Request and fails the test when no verifiable reply arrives.
func (f *p3) login(user, pw string) *radius.Packet {
	f.t.Helper()
	r, err := f.nas.Authenticate(user, pw)
	if err != nil {
		f.t.Fatalf("Access-Request %q: %v", user, err)
	}
	return r
}

// accepted asserts an Access-Accept.
func (f *p3) accepted(user, pw string) *radius.Packet {
	f.t.Helper()
	r := f.login(user, pw)
	if r.Code != radius.CodeAccessAccept {
		f.t.Fatalf("%q: want Accept, got %v %q", user, r.Code, rfc2865.ReplyMessage_GetString(r))
	}
	return r
}

// rejected asserts an Access-Reject whose Reply-Message is exactly "<code>" (O20, Phase 12a).
func (f *p3) rejected(user, pw, code string) *radius.Packet {
	f.t.Helper()
	r := f.login(user, pw)
	msg := rfc2865.ReplyMessage_GetString(r)
	if r.Code != radius.CodeAccessReject || msg != code {
		f.t.Fatalf("%q: want Reject %s, got %v %q", user, code, r.Code, msg)
	}
	return r
}

// vsa returns the value of the MikroTik (vendor 14988) attribute of the given
// type, parsed from the raw packet bytes without the radius library's
// dictionary, so the attribute ids of PLAN 7.5 are checked independently.
func vsa(t *testing.T, p *radius.Packet, typ byte) []byte {
	t.Helper()
	for _, a := range p.Attributes {
		if a.Type != rfc2865.VendorSpecific_Type || len(a.Attribute) < 6 || binary.BigEndian.Uint32(a.Attribute[:4]) != 14988 {
			continue
		}
		sub := []byte(a.Attribute[4:])
		if sub[0] == typ && int(sub[1]) == len(sub) {
			return sub[2:]
		}
	}
	return nil
}

func xmitLimit(t *testing.T, p *radius.Packet) uint32 {
	t.Helper()
	v := vsa(t, p, 2)
	if len(v) != 4 {
		t.Fatalf("Mikrotik-Xmit-Limit missing or malformed: %v", v)
	}
	if lib := mikrotik.MikrotikXmitLimit_Get(p); uint32(lib) != binary.BigEndian.Uint32(v) {
		t.Fatalf("xmit limit mismatch %d vs %d", lib, binary.BigEndian.Uint32(v))
	}
	return binary.BigEndian.Uint32(v)
}

// dataLimits returns the data-limit attributes of an Accept as full byte counts:
// Recv-Limit (id 1) and Xmit-Limit (id 2), each joined with its Gigawords
// attribute (ids 14 and 15). present is false when neither limit is there.
func dataLimits(t *testing.T, p *radius.Packet) (recv, xmit uint64, present bool) {
	t.Helper()
	get := func(typ byte) (uint64, bool) {
		v := vsa(t, p, typ)
		if v == nil {
			return 0, false
		}
		if len(v) != 4 {
			t.Fatalf("attribute %d malformed: %v", typ, v)
		}
		return uint64(binary.BigEndian.Uint32(v)), true
	}
	r, rok := get(1)
	x, xok := get(2)
	rg, rgok := get(14)
	xg, xgok := get(15)
	if rok != xok || rgok != xgok || (rgok && !rok) {
		t.Fatalf("limit attributes do not match: recv=%v xmit=%v recv-gw=%v xmit-gw=%v", rok, xok, rgok, xgok)
	}
	return r | rg<<32, x | xg<<32, rok
}

func rateLimit(t *testing.T, p *radius.Packet) (string, bool) {
	t.Helper()
	v := vsa(t, p, 8)
	if v == nil {
		return "", false
	}
	return string(v), true
}

func sessionTimeout(p *radius.Packet) uint32 { return uint32(rfc2865.SessionTimeout_Get(p)) }

const (
	mb = int64(1 << 20)
	gb = int64(1 << 30)
)

// E3.01 valid member -> Accept with Acct-Interim-Interval, Session-Timeout, Mikrotik-Rate-Limit (BR-10).
func TestE3_01_AcceptWithAttributes(t *testing.T) {
	f := newP3(t)
	plan := f.plan("Basic", "speed_value", "2", "speed_unit", "Mbps", "bulk_value", "1", "bulk_unit", "GB", "time_limit_minutes", "60")
	f.member(plan, "alice", "password", "secret1")
	r := f.accepted("alice", "secret1")
	if v, _ := rfc2869.AcctInterimInterval_Lookup(r); v != 60 {
		t.Errorf("Acct-Interim-Interval %d, want 60 (settings default)", v)
	}
	if st := sessionTimeout(r); st != 3600 {
		t.Errorf("Session-Timeout %d, want 3600", st)
	}
	if rl, ok := rateLimit(t, r); !ok || rl != "2000k/2000k 8000k/8000k 4000k/4000k 8/8" {
		t.Errorf("Mikrotik-Rate-Limit %q", rl)
	}
	if x := xmitLimit(t, r); x != uint32(gb) {
		t.Errorf("Mikrotik-Xmit-Limit %d, want %d", x, gb)
	}
	// The interval comes from settings and applies without restart.
	form := harness.FormValues(t, f.c.Get("/admin/settings").Doc(t), "form#settings-form")
	form.Set("accounting_interval_seconds", "120")
	if resp := f.c.Send("PUT", "/admin/settings", form); resp.StatusCode != 200 {
		t.Fatalf("settings: %d", resp.StatusCode)
	}
	if v, _ := rfc2869.AcctInterimInterval_Lookup(f.accepted("alice", "secret1")); v != 120 {
		t.Errorf("Acct-Interim-Interval %d after settings change, want 120", v)
	}
}

// E3.02 wrong password / unknown user -> the same Reject text (no user enumeration).
func TestE3_02_InvalidCredentialsIndistinguishable(t *testing.T) {
	f := newP3(t)
	f.member(f.plan("Basic"), "alice", "password", "secret1")
	const want = "INVALID_CREDENTIALS"
	for _, c := range []struct{ user, pw string }{{"alice", "wrong"}, {"alice", ""}, {"nobody", "secret1"}} {
		r := f.login(c.user, c.pw)
		if msg := rfc2865.ReplyMessage_GetString(r); r.Code != radius.CodeAccessReject || msg != want {
			t.Errorf("%q/%q: %v %q", c.user, c.pw, r.Code, msg)
		}
	}
	// Password is case-sensitive; the check precedes every other rule (an inactive member with a wrong password is not told so).
	f.rejected("alice", "SECRET1", "INVALID_CREDENTIALS")
	f.exec(`UPDATE member SET active=0`)
	f.rejected("alice", "wrong", "INVALID_CREDENTIALS")
}

// E3.03 inactive member (check 2).
func TestE3_03_Inactive(t *testing.T) {
	f := newP3(t)
	id := f.member(f.plan("Basic"), "alice", "password", "secret1")
	f.accepted("alice", "secret1")
	f.exec(`UPDATE member SET active=0 WHERE id=?`, id)
	r := f.rejected("alice", "secret1", "INACTIVE_USER")
	if msg := rfc2865.ReplyMessage_GetString(r); msg != "INACTIVE_USER" {
		t.Errorf("message %q", msg)
	}
}

// E3.04 no plan / removed plan (checks 3-4). Plans are mandatory in the UI and
// protected by a foreign key, so the rows are forced with foreign keys off.
func TestE3_04_NoPlanAndPlanRemoved(t *testing.T) {
	f := newP3(t)
	id := f.member(f.plan("Basic"), "alice", "password", "secret1")
	conn, err := f.db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(context.Background(), `PRAGMA foreign_keys=OFF`); err != nil {
		t.Fatal(err)
	}
	set := func(plan int64) {
		if _, err := conn.ExecContext(context.Background(), `UPDATE member SET plan_id=? WHERE id=?`, plan, id); err != nil {
			t.Fatal(err)
		}
	}
	set(0)
	r := f.rejected("alice", "secret1", "NO_PLAN")
	if msg := rfc2865.ReplyMessage_GetString(r); msg != "NO_PLAN" {
		t.Errorf("message %q", msg)
	}
	set(9999)
	r = f.rejected("alice", "secret1", "PLAN_REMOVED")
	if msg := rfc2865.ReplyMessage_GetString(r); msg != "PLAN_REMOVED" {
		t.Errorf("message %q", msg)
	}
}

// E3.05 window lapsed -> reject; with auto_resubscribe -> accept and renew (BR-5, BR-6, BR-13b, Q21).
func TestE3_05_WindowLapsedAndAutoResubscribe(t *testing.T) {
	f := newP3(t)
	const day = int64(24 * 3600)
	plain := f.plan("Plain", "duration_days", "30")
	auto := f.plan("Auto", "duration_days", "30", "auto_resubscribe", "on", "bulk_value", "10", "bulk_unit", "GB", "time_limit_minutes", "60")
	p := f.member(plain, "plain", "password", "pw")
	a := f.member(auto, "auto", "password", "pw")

	// Inside the window (inclusive at the end) both are accepted.
	f.exec(`UPDATE member SET subscription_date=? WHERE id=?`, t0.Unix()-30*day, p)
	f.accepted("plain", "pw")
	f.exec(`UPDATE member SET subscription_date=? WHERE id=?`, t0.Unix()-30*day-1, p)
	r := f.rejected("plain", "pw", "NO_ACTIVE_SUBSCRIPTION")
	if msg := rfc2865.ReplyMessage_GetString(r); msg != "NO_ACTIVE_SUBSCRIPTION" {
		t.Errorf("message %q", msg)
	}
	if n := scalar[int](t, f.db, `SELECT renewal_count FROM member WHERE id=?`, p); n != 0 {
		t.Errorf("a plan without auto-resubscribe was renewed")
	}

	f.exec(`UPDATE member SET subscription_date=?, extra_bulk_bytes=?, used_download_bytes=?, used_upload_bytes=?, used_seconds=?
		WHERE id=?`, t0.Unix()-31*day, gb, 10*gb, gb, 3600, a)
	resp := f.accepted("auto", "pw")
	if st := sessionTimeout(resp); st != 3600 {
		t.Errorf("after renewal Session-Timeout %d, want the full 3600", st)
	}
	q := func(col string) int64 { return scalar[int64](t, f.db, `SELECT `+col+` FROM member WHERE id=?`, a) }
	if q("subscription_date") != t0.Unix() || q("extra_bulk_bytes") != 0 || q("used_download_bytes") != 0 || q("used_upload_bytes") != 0 || q("used_seconds") != 0 {
		t.Errorf("window/extra bulk/counters not reset: sub=%d extra=%d dl=%d ul=%d s=%d", q("subscription_date"), q("extra_bulk_bytes"), q("used_download_bytes"), q("used_upload_bytes"), q("used_seconds"))
	}
	if q("renewal_count") != 1 || q("last_renewed_at") != t0.Unix() || scalar[string](t, f.db, `SELECT last_renewal_kind FROM member WHERE id=?`, a) != "auto" {
		t.Errorf("renewal bookkeeping: count=%d at=%d", q("renewal_count"), q("last_renewed_at"))
	}
	// The next login finds a fresh window: no second renewal.
	f.accepted("auto", "pw")
	if q("renewal_count") != 1 {
		t.Errorf("renewed twice: %d", q("renewal_count"))
	}
	// The member page shows the automatic renewal (Q21).
	if txt := text(f.c.Get(mpath(a)).Doc(t).Find("body")); !strings.Contains(txt, "Renewed 1 time") || !strings.Contains(txt, "automatic") {
		t.Errorf("member page does not show the renewal: %q", txt)
	}
}

// E3.06 out of bulk; extra bulk flips it to accept (BR-7).
func TestE3_06_OutOfBulkAndExtraBulk(t *testing.T) {
	f := newP3(t)
	plan := f.plan("Small", "bulk_value", "100", "bulk_unit", "MB")
	id := f.member(plan, "alice", "password", "pw")
	f.exec(`UPDATE member SET used_download_bytes=?, used_upload_bytes=? WHERE id=?`, 70*mb, 30*mb, id)
	r := f.rejected("alice", "pw", "OUT_OF_BULK")
	if msg := rfc2865.ReplyMessage_GetString(r); msg != "OUT_OF_BULK" {
		t.Errorf("message %q", msg)
	}
	resp := f.c.Send("PUT", mpath(id)+"/extra-bulk", url.Values{"value": {"50"}, "unit": {"MB"}})
	if resp.StatusCode != 200 {
		t.Fatalf("extra bulk: %d %s", resp.StatusCode, resp.Body)
	}
	r = f.accepted("alice", "pw")
	if x := xmitLimit(t, r); x != uint32(50*mb) {
		t.Errorf("Xmit-Limit %d, want %d", x, 50*mb)
	}
	// Clearing it rejects again.
	f.c.Send("PUT", mpath(id)+"/extra-bulk", url.Values{"value": {"0"}, "unit": {"MB"}})
	f.rejected("alice", "pw", "OUT_OF_BULK")
}

// E3.07 unlimited bulk -> no data-limit attribute at all (O3, changed 2026-11-04; was the 3,900,000,000 sentinel).
func TestE3_07_UnlimitedBulkNoLimit(t *testing.T) {
	f := newP3(t)
	id := f.member(f.plan("Free", "bulk_value", "0"), "alice", "password", "pw")
	f.exec(`UPDATE member SET used_download_bytes=? WHERE id=?`, 50*gb, id) // usage never limits an unlimited plan
	r := f.accepted("alice", "pw")
	for _, typ := range []byte{1, 2, 14, 15} {
		if vsa(t, r, typ) != nil {
			t.Errorf("MikroTik attribute %d sent for unlimited data", typ)
		}
	}
}

// E3.08 time quota -> Session-Timeout; exhausted -> reject (BR-8).
func TestE3_08_TimeQuota(t *testing.T) {
	f := newP3(t)
	id := f.member(f.plan("Hour", "time_limit_minutes", "60"), "alice", "password", "pw")
	for _, c := range []struct {
		used int64
		want uint32
	}{{0, 3600}, {1800, 1800}, {3599, 1}} {
		f.exec(`UPDATE member SET used_seconds=? WHERE id=?`, c.used, id)
		if st := sessionTimeout(f.accepted("alice", "pw")); st != c.want {
			t.Errorf("used %d: Session-Timeout %d, want %d", c.used, st, c.want)
		}
	}
	f.exec(`UPDATE member SET used_seconds=3600 WHERE id=?`, id)
	r := f.rejected("alice", "pw", "OUT_OF_TIME")
	if msg := rfc2865.ReplyMessage_GetString(r); msg != "OUT_OF_TIME" {
		t.Errorf("message %q", msg)
	}
	f.exec(`UPDATE member SET used_seconds=99999 WHERE id=?`, id)
	f.rejected("alice", "pw", "OUT_OF_TIME")
}

// E3.09 Session-Timeout = the smallest of time quota left, time until expires_at and time until the window end;
// never the old 172800 (O4, changed 2026-11-04). The full matrix is E11.03.
func TestE3_09_SessionTimeoutSmallest(t *testing.T) {
	f := newP3(t)
	limited := f.member(f.plan("Limited", "time_limit_minutes", "90", "duration_days", "1"), "limited", "password", "pw")
	free := f.member(f.plan("NoLimit", "time_limit_minutes", "0", "duration_days", "1"), "free", "password", "pw")
	// The one-day window ends in 10 minutes: it caps both.
	end := t0.Unix() - 24*3600 + 600
	f.exec(`UPDATE member SET subscription_date=? WHERE id IN (?,?)`, end, limited, free)
	if st := sessionTimeout(f.accepted("limited", "pw")); st != 600 {
		t.Errorf("limited: Session-Timeout %d, want 600 (window end)", st)
	}
	if st := sessionTimeout(f.accepted("free", "pw")); st != 600 {
		t.Errorf("no time limit: Session-Timeout %d, want 600 (window end)", st)
	}
	// A fresh window: the time quota (90 min) is smaller than the day; no time limit gives the whole day.
	f.exec(`UPDATE member SET subscription_date=? WHERE id IN (?,?)`, t0.Unix(), limited, free)
	if st := sessionTimeout(f.accepted("limited", "pw")); st != 5400 {
		t.Errorf("limited: Session-Timeout %d, want 5400", st)
	}
	if st := sessionTimeout(f.accepted("free", "pw")); st != 86400 {
		t.Errorf("no time limit: Session-Timeout %d, want 86400 (window end)", st)
	}
}

// E3.10 rate-limit strings (BR-10, O2): Kbps/Mbps/Gbps, decimals, legacy burst format, plain form above 12,000,000 kbps.
func TestE3_10_RateLimitStrings(t *testing.T) {
	f := newP3(t)
	cases := []struct{ value, unit, want string }{
		{"512", "Kbps", "512k/512k 2048k/2048k 1024k/1024k 8/8"},
		{"2", "Mbps", "2000k/2000k 8000k/8000k 4000k/4000k 8/8"},
		{"1.5", "Mbps", "1500k/1500k 6000k/6000k 3000k/3000k 8/8"},
		{"1.2345", "Kbps", "1k/1k 4k/4k 2k/2k 8/8"}, // rounded to whole kbps
		{"0.5", "Gbps", "500000k/500000k 2000000k/2000000k 1000000k/1000000k 8/8"},
		{"1", "Gbps", "1000000k/1000000k 4000000k/4000000k 2000000k/2000000k 8/8"},
		{"3", "Gbps", "3000000k/3000000k 12000000k/12000000k 6000000k/6000000k 8/8"}, // burst exactly 12,000,000 keeps the full form
		{"3.5", "Gbps", "3500000k"},                                                  // burst 14,000,000 > 12,000,000: plain form
		{"0", "Mbps", ""},                                                            // no speed limit: no attribute
	}
	for i, c := range cases {
		plan := f.plan(fmt.Sprintf("P%d", i), "speed_value", c.value, "speed_unit", c.unit)
		user := fmt.Sprintf("user%d", i)
		f.member(plan, user, "password", "pw")
		got, ok := rateLimit(t, f.accepted(user, "pw"))
		if c.want == "" {
			if ok {
				t.Errorf("%s %s: unexpected Rate-Limit %q", c.value, c.unit, got)
			}
			continue
		}
		if !ok || got != c.want {
			t.Errorf("%s %s: Rate-Limit %q, want %q", c.value, c.unit, got, c.want)
		}
	}
}

// E3.11 data cap = remaining bytes, in Recv-Limit and Xmit-Limit; from 4 GiB up the high 32 bits go into the
// -Gigawords attributes (O3, changed 2026-11-04; was a clamp at 4294967295).
func TestE3_11_DataCapAndGigawords(t *testing.T) {
	f := newP3(t)
	small := f.member(f.plan("Small", "bulk_value", "1", "bulk_unit", "GB"), "small", "password", "pw")
	f.exec(`UPDATE member SET used_download_bytes=?, used_upload_bytes=? WHERE id=?`, 100*mb, 24*mb, small)
	r := f.accepted("small", "pw")
	if x := xmitLimit(t, r); x != uint32(gb-124*mb) {
		t.Errorf("Xmit-Limit %d, want %d", x, gb-124*mb)
	}
	if vsa(t, r, 14) != nil || vsa(t, r, 15) != nil {
		t.Error("Gigawords attribute sent below 4 GiB")
	}
	f.member(f.plan("Big", "bulk_value", "10", "bulk_unit", "GB"), "big", "password", "pw")
	recv, xmit, ok := dataLimits(t, f.accepted("big", "pw"))
	if !ok || recv != uint64(10*gb) || xmit != uint64(10*gb) {
		t.Errorf("recv %d xmit %d, want %d", recv, xmit, 10*gb)
	}
	if strings.Contains(f.log.String(), "clamp") {
		t.Errorf("a clamp is logged:\n%s", f.log.String())
	}
}

// E3.12 concurrency per plan (BR-9, Q27). Sessions are seeded into the table
// the way Phase 4 accounting creates them (the Phase 2 harness pattern).
func TestE3_12_ConcurrentSessions(t *testing.T) {
	f := newP3(t) // accounting interval 60 s: a session is active for 120 s after its last update
	one := f.member(f.plan("One", "max_sessions", "1"), "one", "password", "pw")
	two := f.member(f.plan("Two", "max_sessions", "2"), "two", "password", "pw")
	now := f.fc.Now()

	f.accepted("one", "pw")
	f.accepted("two", "pw")
	f.openSession(one, "one", "s1", now)
	f.openSession(two, "two", "s1", now)
	r := f.rejected("one", "pw", "NO_MORE_SESSION")
	if msg := rfc2865.ReplyMessage_GetString(r); msg != "NO_MORE_SESSION" {
		t.Errorf("message %q", msg)
	}
	f.accepted("two", "pw") // one of two used
	f.openSession(two, "two", "s2", now)
	f.rejected("two", "pw", "NO_MORE_SESSION")

	// A stopped session does not count.
	f.exec(`UPDATE session SET ended_at=?, status=2 WHERE member_id=?`, now.Unix(), one)
	f.accepted("one", "pw")
	// An open session counts again.
	f.exec(`UPDATE session SET ended_at=NULL, status=3 WHERE member_id=?`, one)
	f.rejected("one", "pw", "NO_MORE_SESSION")

	// Staleness: active while now - last_update <= interval + 60 s.
	f.exec(`UPDATE session SET last_update_at=? WHERE member_id=?`, now.Unix()-120, one)
	f.rejected("one", "pw", "NO_MORE_SESSION")
	f.exec(`UPDATE session SET last_update_at=? WHERE member_id=?`, now.Unix()-121, one)
	f.accepted("one", "pw")
	f.fc.Advance(200 * time.Second) // member two's sessions go stale with the clock
	f.accepted("two", "pw")
}

// E3.13 CHAP / MS-CHAP -> Reject UNSUPPORTED_AUTH_METHOD and a warning (D4).
func TestE3_13_ChapRejected(t *testing.T) {
	f := newP3(t)
	f.member(f.plan("Basic"), "alice", "password", "pw")

	chap := radius.New(radius.CodeAccessRequest, []byte(f.nas.Secret))
	rfc2865.UserName_SetString(chap, "alice")
	rfc2865.CHAPChallenge_Set(chap, []byte("0123456789abcdef"))
	rfc2865.CHAPPassword_Set(chap, append([]byte{1}, make([]byte, 16)...))

	ms := radius.New(radius.CodeAccessRequest, []byte(f.nas.Secret))
	rfc2865.UserName_SetString(ms, "alice")
	microsoft.MSCHAPChallenge_Add(ms, []byte("0123456789abcdef"))
	microsoft.MSCHAPResponse_Add(ms, make([]byte, 50))

	for name, p := range map[string]*radius.Packet{"CHAP": chap, "MS-CHAP": ms} {
		r, err := f.nas.Exchange(p)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		msg := rfc2865.ReplyMessage_GetString(r)
		if r.Code != radius.CodeAccessReject || msg != "UNSUPPORTED_AUTH_METHOD" {
			t.Errorf("%s: %v %q", name, r.Code, msg)
		}
	}
	if logs := f.log.String(); strings.Count(logs, "level=WARN") < 2 || !strings.Contains(logs, "CHAP") {
		t.Errorf("no warning per CHAP request:\n%s", logs)
	}
}

// E3.14 wrong shared secret -> no reply at all (PLAN 7.1).
func TestE3_14_WrongSecretNoReply(t *testing.T) {
	f := newP3(t)
	f.member(f.plan("Basic"), "alice", "password", "pw")
	// Control: the right secret gets a datagram back.
	if silent, err := f.nas.Silent(f.nas.PAPPacket("alice", "pw")); err != nil || silent {
		t.Fatalf("control: silent=%v err=%v", silent, err)
	}
	bad := fakenas.New(f.s.Addrs.RadiusAuth, "not-the-shared-secret")
	bad.Timeout = 700 * time.Millisecond
	for _, c := range []struct{ user, pw string }{{"alice", "pw"}, {"nobody", "pw"}, {"alice", "a-longer-password-than-sixteen-chars"}} {
		if silent, err := bad.Silent(bad.PAPPacket(c.user, c.pw)); err != nil || !silent {
			t.Errorf("%q: wrong secret got a reply (silent=%v err=%v)", c.user, silent, err)
		}
	}
	if !strings.Contains(f.log.String(), "shared secret") {
		t.Errorf("no log line about the probable wrong secret:\n%s", f.log.String())
	}
	f.accepted("alice", "pw") // the server is unharmed
}

// E3.15 usernames are case-insensitive; passwords are not (O13).
func TestE3_15_UsernameCaseInsensitive(t *testing.T) {
	f := newP3(t)
	f.member(f.plan("Basic"), "Alice", "password", "Pw")
	for _, u := range []string{"Alice", "alice", "ALICE", "  alice "} {
		f.accepted(u, "Pw")
	}
	f.rejected("alice", "pw", "INVALID_CREDENTIALS")
}

// E3.16 expires_at passed -> reject EXPIRED (O7).
func TestE3_16_ExpiresAt(t *testing.T) {
	f := newP3(t)
	id := f.member(f.plan("Basic"), "alice", "password", "pw")
	f.exec(`UPDATE member SET expires_at=? WHERE id=?`, t0.Unix()+600, id)
	f.accepted("alice", "pw")
	f.advance(600 * time.Second) // exactly at expires_at: not yet passed
	f.accepted("alice", "pw")
	f.advance(time.Second)
	r := f.rejected("alice", "pw", "EXPIRED")
	if msg := rfc2865.ReplyMessage_GetString(r); msg != "EXPIRED" {
		t.Errorf("message %q", msg)
	}
	f.exec(`UPDATE member SET expires_at=NULL WHERE id=?`, id) // never
	f.accepted("alice", "pw")
}

// E3.17 invalid usernames at the RADIUS edge -> Reject INVALID_CREDENTIALS (BR-1).
func TestE3_17_InvalidUsernames(t *testing.T) {
	f := newP3(t)
	f.member(f.plan("Basic"), "alice", "password", "pw")
	for _, u := range []string{"ali ce", "ali-ce", "ali.ce", "alice@example.com", "ali_ce", "' OR 1=1 --", "alicé", "۱۲۳", "علی", strings.Repeat("a", 65), "%", "ali\tce"} {
		f.rejected(u, "pw", "INVALID_CREDENTIALS")
	}
	f.rejected(strings.Repeat("a", 64), "pw", "INVALID_CREDENTIALS") // valid shape, unknown user: same reject
}

// E3.18 database failure -> no reply, never a false Accept (G-5).
func TestE3_18_DatabaseFailureNoReply(t *testing.T) {
	f := newP3(t)
	f.member(f.plan("Basic"), "alice", "password", "pw")
	f.accepted("alice", "pw")
	f.exec(`ALTER TABLE member RENAME TO member_broken`)
	quick := fakenas.New(f.s.Addrs.RadiusAuth, f.nas.Secret)
	quick.Timeout = 700 * time.Millisecond
	if silent, err := quick.Silent(quick.PAPPacket("alice", "pw")); err != nil || !silent {
		t.Fatalf("database failure produced a reply (silent=%v err=%v)", silent, err)
	}
	if !strings.Contains(f.log.String(), "level=ERROR") {
		t.Errorf("database failure not logged:\n%s", f.log.String())
	}
	f.exec(`ALTER TABLE member_broken RENAME TO member`)
	f.accepted("alice", "pw") // recovers
}
