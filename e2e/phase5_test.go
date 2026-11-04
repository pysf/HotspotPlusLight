package e2e

import (
	"net"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/rfc2866"

	"hotspotpluslight/e2e/fakenas"
	"hotspotpluslight/e2e/harness"
	"hotspotpluslight/internal/clock"
)

// p5 is the Phase 5 fixture: the Phase 4 fixture, but with the REAL Disconnect
// client, talking to a fake router listener (coa) whose port is in settings.
type p5 struct {
	*p4
	coa *fakenas.CoA
}

func newP5(t *testing.T) *p5 {
	t.Helper()
	fc := clock.NewFake(t0)
	hook := &beforeCommit{}
	s := harness.Start(t, harness.Options{Clock: fc, AccountingBeforeCommit: hook.run})
	f := &p2{t: t, s: s, fc: fc, c: harness.LoginAsAdmin(t, s), db: harness.DB(t, s.DataDir)}
	secret := scalar[string](t, f.db, `SELECT radius_shared_secret FROM settings`)
	nas := fakenas.New(s.Addrs.RadiusAuth, secret)
	nas.AcctAddr = s.Addrs.RadiusAcct
	coa := fakenas.ListenCoA(t, secret)
	f.exec(`UPDATE settings SET coa_port=?`, coa.Port())
	return &p5{p4: &p4{p3: &p3{p2: f, nas: nas, log: &harness.SafeBuffer{}}, hook: hook}, coa: coa}
}

// online starts a session the way the router does: an Accounting Start with the identifiers a kick needs.
func (f *p5) online(user, sid, ip, mac string) {
	f.t.Helper()
	f.acct(fakenas.Acct{Type: rfc2866.AcctStatusType_Value_Start, User: user, SessionID: sid, FramedIP: ip, MAC: mac})
}

// sessionDBID is the id of the session row of an Acct-Session-Id.
func (f *p5) sessionDBID(sid string) int64 {
	return scalar[int64](f.t, f.db, `SELECT id FROM session WHERE acct_session_id=? ORDER BY id DESC LIMIT 1`, sid)
}

func spath(id int64) string {
	return "/admin/sessions/" + strconv.FormatInt(id, 10) + "/disconnections"
}

// onlineUsers lists the usernames shown on the online page, in order.
func onlineUsers(f *p2) []string {
	f.t.Helper()
	r := f.c.Get("/admin/sessions")
	if r.StatusCode != 200 {
		f.t.Fatalf("online page: %d", r.StatusCode)
	}
	return sessionUsers(r.Doc(f.t))
}

func sessionUsers(d *goquery.Document) []string {
	var out []string
	d.Find("tr[data-session]").Each(func(_ int, s *goquery.Selection) {
		out = append(out, s.AttrOr("data-username", ""))
	})
	return out
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := map[string]int{}
	for _, x := range a {
		m[x]++
	}
	for _, x := range b {
		m[x]--
	}
	for _, n := range m {
		if n != 0 {
			return false
		}
	}
	return true
}

// memberDots returns the usernames whose online dot is lit on the member list.
func memberDots(f *p2) []string {
	f.t.Helper()
	var out []string
	f.c.Get("/admin/members?per=100").Doc(f.t).Find("tr[data-member]").Each(func(_ int, s *goquery.Selection) {
		if s.AttrOr("data-online", "") == "true" {
			out = append(out, s.AttrOr("data-username", ""))
		}
	})
	return out
}

// scenario5 builds three members: alice online, bob stopped, carol silent for too long (stale).
func (f *p5) scenario() {
	p := f.plan("Basic")
	for _, u := range []string{"alice", "bob", "carol", "dave"} {
		f.member(p, u)
	}
	f.online("carol", "sc", "192.168.88.12", "AA:BB:CC:DD:EE:03")
	f.online("bob", "sb", "192.168.88.11", "AA:BB:CC:DD:EE:02")
	f.stop("bob", "sb", 10, 10, 5)
	f.advance(3 * time.Minute) // default interval 60 s + 60 s = 120 s: carol is now stale
	f.online("alice", "sa", "192.168.88.10", "AA:BB:CC:DD:EE:01")
}

// E5.01 the online list shows only active sessions: not stopped, not stale (BR-17, BR-9).
func TestE5_01_OnlineListOnlyActive(t *testing.T) {
	f := newP5(t)
	f.scenario()
	if got := onlineUsers(f.p2); !sameSet(got, []string{"alice"}) {
		t.Fatalf("online page lists %v, want only alice", got)
	}
	// Fresh Interim keeps it listed; it goes stale when the router stays silent.
	f.advance(100 * time.Second)
	if got := onlineUsers(f.p2); !sameSet(got, []string{"alice"}) {
		t.Fatalf("after 100 s: %v", got)
	}
	f.advance(30 * time.Second)
	if got := onlineUsers(f.p2); len(got) != 0 {
		t.Fatalf("stale session still listed: %v", got)
	}
	// Newest first.
	f.online("bob", "sb2", "192.168.88.11", "AA:BB:CC:DD:EE:02")
	f.advance(time.Second)
	f.online("dave", "sd", "192.168.88.13", "AA:BB:CC:DD:EE:04")
	if got := onlineUsers(f.p2); len(got) != 2 || got[0] != "dave" || got[1] != "bob" {
		t.Fatalf("order %v, want dave then bob", got)
	}
}

// E5.02 the member list's online dot equals the online page, member for member.
func TestE5_02_OnlineDotMatchesOnlinePage(t *testing.T) {
	f := newP5(t)
	f.scenario()
	check := func(when string) {
		t.Helper()
		if page, dots := onlineUsers(f.p2), memberDots(f.p2); !sameSet(page, dots) {
			t.Fatalf("%s: online page %v, member list dots %v", when, page, dots)
		}
	}
	check("start")
	f.advance(119 * time.Second) // one second before alice goes stale
	check("edge")
	f.advance(2 * time.Second)
	check("stale")
	if len(memberDots(f.p2)) != 0 {
		t.Fatal("nobody should be online")
	}
}

// E5.12 columns, the "updated every N s" note, polling and live updates.
func TestE5_12_OnlinePageColumnsAndPolling(t *testing.T) {
	f := newP5(t)
	p := f.plan("Basic")
	f.member(p, "alice")
	f.online("alice", "sa", "192.168.88.10", "AA:BB:CC:DD:EE:01")
	f.advance(30 * time.Second)
	f.interim("alice", "sa", 3*1024*1024, 1024*1024, 30)

	r := f.c.Get("/admin/sessions")
	d := r.Doc(t)
	var heads []string
	d.Find("table thead th").Each(func(_ int, s *goquery.Selection) { heads = append(heads, text(s)) })
	want := []string{"Username", "IP", "MAC", "Started", "Duration", "Download", "Upload", ""}
	if strings.Join(heads, "|") != strings.Join(want, "|") {
		t.Fatalf("headers %q, want %q", heads, want)
	}
	row := d.Find("tr[data-session]")
	if row.Length() != 1 {
		t.Fatalf("%d rows", row.Length())
	}
	cell := func(c string) string { return text(row.Find("td[data-col=" + c + "]")) }
	if cell("username") != "alice" || cell("ip") != "192.168.88.10" || cell("mac") != "AA:BB:CC:DD:EE:01" {
		t.Fatalf("cells: user %q ip %q mac %q", cell("username"), cell("ip"), cell("mac"))
	}
	if cell("started") != "2026-11-01 12:00" {
		t.Errorf("started %q", cell("started"))
	}
	if cell("duration") == "" || !strings.Contains(cell("duration"), "30") {
		t.Errorf("duration %q should show 30 s", cell("duration"))
	}
	if cell("download") != "3 MB" || cell("upload") != "1 MB" {
		t.Errorf("session data %q / %q", cell("download"), cell("upload"))
	}
	if row.Find("button[hx-post*=\"/disconnections\"]").Length() != 1 {
		t.Error("no kick button on the row")
	}

	// The note follows the setting.
	note := text(d.Find("[data-updated-note]"))
	if note != "Updated by the router every 60 s" {
		t.Errorf("note %q", note)
	}
	f.exec(`UPDATE settings SET accounting_interval_seconds=120`)
	if note := text(f.c.Get("/admin/sessions").Doc(t).Find("[data-updated-note]")); note != "Updated by the router every 120 s" {
		t.Errorf("note after setting change %q", note)
	}

	// Polling: the table container re-fetches the fragment endpoint every 10 s.
	box := d.Find("#online-table")
	if box.AttrOr("hx-trigger", "") != "every 10s" {
		t.Fatalf("hx-trigger %q", box.AttrOr("hx-trigger", ""))
	}
	src := box.AttrOr("hx-get", "")
	if !strings.HasPrefix(src, "/admin/sessions/table") {
		t.Fatalf("hx-get %q", src)
	}
	frag := f.c.Get(src)
	if frag.StatusCode != 200 || strings.Contains(frag.Body, "<html") || frag.Doc(t).Find("tr[data-session]").Length() != 1 {
		t.Fatalf("fragment: %d %s", frag.StatusCode, frag.Body)
	}
	// A new session shows up through the fragment alone.
	f.member(f.plan("Other"), "bob")
	f.online("bob", "sb", "192.168.88.11", "AA:BB:CC:DD:EE:02")
	if got := sessionUsers(f.c.Get(src).Doc(t)); !sameSet(got, []string{"alice", "bob"}) {
		t.Fatalf("fragment after new session: %v", got)
	}
	if r := f.c.Do("GET", "/admin/sessions/table", nil, ""); r.StatusCode != 200 {
		t.Fatalf("fragment without params: %d", r.StatusCode)
	}
}

// E5.03 a kick sends a Disconnect-Request with the right attributes to nas_ip:coa_port (BR-18).
func TestE5_03_KickSendsDisconnect(t *testing.T) {
	f := newP5(t)
	f.member(f.plan("Basic"), "alice")
	f.online("alice", "sess-1", "192.168.88.10", "AA:BB:CC:DD:EE:01")
	id := f.sessionDBID("sess-1")
	if r := f.c.Send("POST", spath(id), nil); r.StatusCode != 200 {
		t.Fatalf("kick: %d %s", r.StatusCode, r.Body)
	}
	reqs := f.coa.Requests()
	if len(reqs) != 1 {
		t.Fatalf("%d Disconnect-Requests, want 1", len(reqs))
	}
	d := reqs[0]
	if !d.Authentic {
		t.Error("request authenticator does not verify with the shared secret")
	}
	p := d.Packet
	if rfc2865.UserName_GetString(p) != "alice" {
		t.Errorf("User-Name %q", rfc2865.UserName_GetString(p))
	}
	if rfc2866.AcctSessionID_GetString(p) != "sess-1" {
		t.Errorf("Acct-Session-Id %q", rfc2866.AcctSessionID_GetString(p))
	}
	if ip, err := rfc2865.FramedIPAddress_Lookup(p); err != nil || !ip.Equal(net.ParseIP("192.168.88.10")) {
		t.Errorf("Framed-IP-Address %v %v", ip, err)
	}
	// The stored MAC is already in the RouterOS spelling and is sent as is. VERIFY-ON-HARDWARE: that the router matches on it.
	if got := rfc2865.CallingStationID_GetString(p); got != "AA:BB:CC:DD:EE:01" {
		t.Errorf("Calling-Station-Id %q", got)
	}
}

// E5.03b CSRF, unknown and finished sessions.
func TestE5_03b_KickRouteGuards(t *testing.T) {
	f := newP5(t)
	f.member(f.plan("Basic"), "alice")
	f.online("alice", "s1", "192.168.88.10", "AA:BB:CC:DD:EE:01")
	id := f.sessionDBID("s1")
	if r := f.c.Do("POST", spath(id), nil, ""); r.StatusCode != 403 {
		t.Fatalf("no CSRF: %d", r.StatusCode)
	}
	if r := f.c.Send("POST", spath(9999), nil); r.StatusCode != 404 {
		t.Fatalf("unknown session: %d", r.StatusCode)
	}
	if r := f.c.Send("GET", spath(id), nil); r.StatusCode != 405 {
		t.Fatalf("GET: %d", r.StatusCode)
	}
	f.stop("alice", "s1", 1, 1, 1)
	if r := f.c.Send("POST", spath(id), nil); r.StatusCode != 409 || !strings.Contains(r.Body, "already ended") {
		t.Fatalf("finished session: %d %s", r.StatusCode, r.Body)
	}
	if n := len(f.coa.Requests()); n != 0 {
		t.Fatalf("%d Disconnects sent for refused kicks", n)
	}
}

// E5.03c a row kick returns to the page and size the admin was on.
func TestE5_03c_KickKeepsPageAndSize(t *testing.T) {
	f := newP5(t)
	f.member(f.plan("Basic"), "alice")
	f.online("alice", "s1", "192.168.88.10", "AA:BB:CC:DD:EE:01")
	id := f.sessionDBID("s1")
	if !strings.Contains(f.c.Get("/admin/sessions?page=1&per=10").Body, "/disconnections?page=1&amp;per=10") {
		t.Fatal("row button does not carry page and per")
	}
	r := f.c.Send("POST", spath(id)+"?page=2&per=10", nil)
	if r.StatusCode != 200 || r.Header.Get("HX-Redirect") != "/admin/sessions?page=2&per=10" {
		t.Fatalf("kick: %d redirect %q", r.StatusCode, r.Header.Get("HX-Redirect"))
	}
}

// E5.04 an ACK ends the session in the UI and the database (terminate cause Admin-Reset).
func TestE5_04_AckEndsSession(t *testing.T) {
	f := newP5(t)
	mid := f.member(f.plan("Basic"), "alice")
	f.online("alice", "s1", "192.168.88.10", "AA:BB:CC:DD:EE:01")
	f.interim("alice", "s1", 1000, 500, 20)
	r := f.c.Send("POST", spath(f.sessionDBID("s1")), nil)
	if r.StatusCode != 200 || r.Header.Get("HX-Redirect") != "/admin/sessions" {
		t.Fatalf("kick: %d redirect %q", r.StatusCode, r.Header.Get("HX-Redirect"))
	}
	s := f.session("s1")
	if !s.ended.Valid || s.ended.Int64 != t0.Unix() || s.cause != "Admin-Reset" {
		t.Fatalf("session after ACK: ended %v cause %q", s.ended, s.cause)
	}
	if got := onlineUsers(f.p2); len(got) != 0 {
		t.Fatalf("still listed: %v", got)
	}
	if got := memberDots(f.p2); len(got) != 0 {
		t.Fatalf("still online in the member list: %v", got)
	}
	// The router's own Stop arrives afterwards with the final totals: it completes
	// the same session; the usage is counted once (no double billing).
	f.stop("alice", "s1", 1500, 700, 30)
	f.wantUsage(mid, usage{1500, 700, 30})
	if n := f.sessions("acct_session_id='s1'"); n != 1 {
		t.Fatalf("%d rows for the session, want 1", n)
	}
	if s := f.session("s1"); s.cause != "Admin-Reset" || s.status != 2 {
		t.Fatalf("after the router's Stop: status %d cause %q", s.status, s.cause)
	}
}

// E5.05 a NAK shows an error and leaves the session as it was.
func TestE5_05_NakKeepsSession(t *testing.T) {
	f := newP5(t)
	f.member(f.plan("Basic"), "alice")
	f.online("alice", "s1", "192.168.88.10", "AA:BB:CC:DD:EE:01")
	f.coa.Always(fakenas.NAK)
	r := f.c.Send("POST", spath(f.sessionDBID("s1")), nil)
	if r.StatusCode != 502 || !strings.Contains(r.Body, "refused") {
		t.Fatalf("NAK: %d %s", r.StatusCode, r.Body)
	}
	if f.session("s1").ended.Valid {
		t.Fatal("session ended despite NAK")
	}
	if got := onlineUsers(f.p2); len(got) != 1 {
		t.Fatalf("session must stay listed: %v", got)
	}
	if n := len(f.coa.Requests()); n != 1 {
		t.Fatalf("NAK is final, no retry: %d requests", n)
	}
}

// E5.05b a NAK with Error-Cause 503 (Session-Context-Not-Found) means the router no
// longer has the session: it counts as disconnected and the ghost row is closed (BR-18).
func TestE5_05b_NakSessionNotFoundClosesGhost(t *testing.T) {
	f := newP5(t)
	f.member(f.plan("Basic"), "alice")
	f.online("alice", "s1", "192.168.88.10", "AA:BB:CC:DD:EE:01")
	f.coa.Always(fakenas.NAKNoSession)
	if r := f.c.Send("POST", spath(f.sessionDBID("s1")), nil); r.StatusCode != 200 {
		t.Fatalf("503 NAK: %d %s", r.StatusCode, r.Body)
	}
	if s := f.session("s1"); !s.ended.Valid || s.cause != "Admin-Reset" {
		t.Fatalf("ghost not closed: ended %v cause %q", s.ended, s.cause)
	}
	if n := len(f.coa.Requests()); n != 1 {
		t.Fatalf("%d requests, want 1", n)
	}
}

// E5.04b after a kick, a later Interim or a counter-reset Stop that reuses the same
// Acct-Session-Id must not overwrite the kicked row (BR-18).
func TestE5_04b_KickedRowNotOverwrittenByReusedId(t *testing.T) {
	f := newP5(t)
	mid := f.member(f.plan("Basic"), "alice")
	f.online("alice", "s1", "192.168.88.10", "AA:BB:CC:DD:EE:01")
	f.interim("alice", "s1", 1000, 500, 20)
	if r := f.c.Send("POST", spath(f.sessionDBID("s1")), nil); r.StatusCode != 200 {
		t.Fatalf("kick: %d", r.StatusCode)
	}
	// The router reuses the id without a Stop for the kicked session. A Stop with
	// smaller figures is a duplicate of what the kicked row already covers: ignored.
	f.stop("alice", "s1", 100, 50, 5)
	f.wantUsage(mid, usage{1000, 500, 20})
	// An Interim above the stored figures is not merged into the kicked row: new row.
	f.interim("alice", "s1", 1500, 700, 30)
	if n := f.sessions("acct_session_id='s1'"); n != 2 {
		t.Fatalf("%d rows, want the kicked row plus a new one", n)
	}
	if k := scalar[int64](t, f.db, `SELECT download_bytes FROM session WHERE acct_session_id='s1' ORDER BY id LIMIT 1`); k != 1000 {
		t.Fatalf("kicked row overwritten: download %d", k)
	}
}

// E5.06 no answer: the request is repeated once (same packet), then an error is shown within about 3 s.
func TestE5_06_TimeoutRetriesOnce(t *testing.T) {
	f := newP5(t)
	f.member(f.plan("Basic"), "alice")
	f.online("alice", "s1", "192.168.88.10", "AA:BB:CC:DD:EE:01")
	f.coa.Always(fakenas.Silent)
	start := time.Now()
	r := f.c.Send("POST", spath(f.sessionDBID("s1")), nil)
	took := time.Since(start)
	if r.StatusCode != 502 || !strings.Contains(r.Body, "did not answer") {
		t.Fatalf("timeout: %d %s", r.StatusCode, r.Body)
	}
	if took < 2*time.Second || took > 4*time.Second {
		t.Fatalf("took %v, want about 3 s", took)
	}
	reqs := f.coa.Requests()
	if len(reqs) != 2 {
		t.Fatalf("%d requests, want the first plus one retry", len(reqs))
	}
	if reqs[0].Packet.Identifier != reqs[1].Packet.Identifier || reqs[0].Packet.Authenticator != reqs[1].Packet.Authenticator {
		t.Error("the retry must be the same packet")
	}
	if f.session("s1").ended.Valid {
		t.Fatal("session ended without an ACK")
	}
	// A late success after one lost packet still works: first request silent, retry answered.
	f.coa.SetBehavior(func(n int, _ fakenas.Disconnect) fakenas.Reply {
		if n%2 == 1 {
			return fakenas.Silent
		}
		return fakenas.ACK
	})
	// n continues from 2: request 3 is silent, request 4 (retry) is answered.
	if r := f.c.Send("POST", spath(f.sessionDBID("s1")), nil); r.StatusCode != 200 {
		t.Fatalf("retry success: %d %s", r.StatusCode, r.Body)
	}
	if !f.session("s1").ended.Valid {
		t.Fatal("session must end when the retry is ACKed")
	}
}

// E5.07 a member's disconnect kicks all of that member's sessions and no one else's.
func TestE5_07_KickAllSessionsOfMember(t *testing.T) {
	f := newP5(t)
	p := f.plan("Two", "max_sessions", "2")
	a := f.member(p, "alice")
	f.member(p, "bob")
	f.online("alice", "a1", "192.168.88.10", "AA:BB:CC:DD:EE:01")
	f.online("alice", "a2", "192.168.88.11", "AA:BB:CC:DD:EE:02")
	f.online("bob", "b1", "192.168.88.12", "AA:BB:CC:DD:EE:03")
	r := f.c.Send("POST", mpath(a)+"/disconnections", nil)
	if r.StatusCode != 200 {
		t.Fatalf("member kick: %d %s", r.StatusCode, r.Body)
	}
	got := map[string]bool{}
	for _, d := range f.coa.Requests() {
		got[rfc2866.AcctSessionID_GetString(d.Packet)] = true
	}
	if len(got) != 2 || !got["a1"] || !got["a2"] {
		t.Fatalf("Disconnects for %v, want a1 and a2", got)
	}
	if !f.session("a1").ended.Valid || !f.session("a2").ended.Valid || f.session("b1").ended.Valid {
		t.Fatal("wrong sessions ended")
	}
	if got := onlineUsers(f.p2); len(got) != 1 || got[0] != "bob" {
		t.Fatalf("online after kick: %v", got)
	}
	// Guards.
	if r := f.c.Do("POST", mpath(a)+"/disconnections", nil, ""); r.StatusCode != 403 {
		t.Fatalf("no CSRF: %d", r.StatusCode)
	}
	if r := f.c.Send("POST", "/admin/members/9999/disconnections", nil); r.StatusCode != 404 {
		t.Fatalf("unknown member: %d", r.StatusCode)
	}
	// A member with nothing open: a friendly answer, nothing sent.
	n := len(f.coa.Requests())
	if r := f.c.Send("POST", mpath(a)+"/disconnections", nil); r.StatusCode != 200 || len(f.coa.Requests()) != n {
		t.Fatalf("nothing open: %d, %d new requests", r.StatusCode, len(f.coa.Requests())-n)
	}
	// One failing session is reported, the other still ends.
	f.online("bob", "b2", "192.168.88.13", "AA:BB:CC:DD:EE:04")
	f.coa.SetBehavior(func(_ int, d fakenas.Disconnect) fakenas.Reply {
		if rfc2866.AcctSessionID_GetString(d.Packet) == "b1" {
			return fakenas.NAK
		}
		return fakenas.ACK
	})
	bob := scalar[int64](t, f.db, `SELECT id FROM member WHERE username='bob'`)
	r = f.c.Send("POST", mpath(bob)+"/disconnections", nil)
	if r.StatusCode != 502 || !strings.Contains(r.Body, "1 of 2") {
		t.Fatalf("partial failure: %d %s", r.StatusCode, r.Body)
	}
	// The page is refreshed too: the member's status line comes back out-of-band (b1 is still online).
	if !strings.Contains(r.Body, `id="member-status"`) || !strings.Contains(r.Body, `hx-swap-oob="true"`) || !strings.Contains(r.Body, "online") {
		t.Fatalf("no refreshed status line after a partial kick: %s", r.Body)
	}
	if f.session("b1").ended.Valid || !f.session("b2").ended.Valid {
		t.Fatal("b1 must stay, b2 must end")
	}
}

// E5.08 deleting a member kicks first (while the member still exists), single and bulk.
func TestE5_08_DeleteKicksFirst(t *testing.T) {
	f := newP5(t)
	p := f.plan("Basic")
	a := f.member(p, "alice")
	b := f.member(p, "bob")
	c := f.member(p, "carol")
	f.online("alice", "a1", "192.168.88.10", "AA:BB:CC:DD:EE:01")
	f.online("bob", "b1", "192.168.88.11", "AA:BB:CC:DD:EE:02")
	f.online("carol", "c1", "192.168.88.12", "AA:BB:CC:DD:EE:03")
	var mu sync.Mutex
	existed := map[string]bool{}
	f.coa.SetBehavior(func(_ int, d fakenas.Disconnect) fakenas.Reply {
		u := rfc2865.UserName_GetString(d.Packet)
		var n int
		f.db.QueryRow(`SELECT count(*) FROM member WHERE username=?`, u).Scan(&n)
		mu.Lock()
		existed[u] = n == 1
		mu.Unlock()
		return fakenas.ACK
	})
	was := func(u string) bool { mu.Lock(); defer mu.Unlock(); return existed[u] }
	if r := f.c.Send("DELETE", mpath(a), nil); r.StatusCode != 200 {
		t.Fatalf("delete: %d %s", r.StatusCode, r.Body)
	}
	if len(f.coa.Requests()) != 1 || !was("alice") {
		t.Fatalf("single delete: %d requests, member existed at kick time: %v", len(f.coa.Requests()), was("alice"))
	}
	r := f.c.Send("POST", "/admin/members/bulk-delete", url.Values{"id": {strconv.FormatInt(b, 10), strconv.FormatInt(c, 10)}, "confirmed": {"1"}})
	if r.StatusCode != 200 {
		t.Fatalf("bulk delete: %d %s", r.StatusCode, r.Body)
	}
	if len(f.coa.Requests()) != 3 || !was("bob") || !was("carol") {
		t.Fatalf("bulk delete: %d requests, bob %v carol %v", len(f.coa.Requests()), was("bob"), was("carol"))
	}
	if memberCount(t, f.db) != 0 {
		t.Fatal("members remain")
	}
	// A router that refuses does not stop the delete, but the admin is told.
	d := f.member(p, "dave")
	f.online("dave", "d1", "192.168.88.13", "AA:BB:CC:DD:EE:04")
	f.coa.Always(fakenas.NAK)
	r = f.c.Send("DELETE", mpath(d), nil)
	if r.StatusCode != 200 || !strings.Contains(r.Body, "could not be disconnected") {
		t.Fatalf("delete with NAK: %d %s", r.StatusCode, r.Body)
	}
	if memberCount(t, f.db) != 0 {
		t.Fatal("member must be deleted even if the kick failed")
	}
}

// E5.09 no Disconnect while the member is under quota (O3, changed 2026-11-04: over quota the server disconnects, see E11.04);
// a rejected login sends none either.
func TestE5_09_NoDisconnectUnderQuota(t *testing.T) {
	f := newP5(t)
	p := f.plan("Tiny", "bulk_value", "1", "bulk_unit", "MB", "max_sessions", "2")
	f.member(p, "alice", "password", "pw")
	f.accepted("alice", "pw")
	f.online("alice", "s1", "192.168.88.10", "AA:BB:CC:DD:EE:01")
	f.interim("alice", "s1", 512*1024, 0, 60)
	waitUntil(t, "enforcement idle", f.s.App.EnforcementIdle)
	time.Sleep(200 * time.Millisecond)
	if n := len(f.coa.Requests()); n != 0 {
		t.Fatalf("%d Disconnect-Requests sent on an Interim under quota", n)
	}
	if got := onlineUsers(f.p2); len(got) != 1 {
		t.Fatalf("session must stay: %v", got)
	}
	f.interim("alice", "s1", 600*1024, 0, 70) // still under 1 MB
	waitUntil(t, "enforcement idle", f.s.App.EnforcementIdle)
	f.exec(`UPDATE member SET active=0`)
	f.rejected("alice", "pw", "INACTIVE_USER")
	if n := len(f.coa.Requests()); n != 0 {
		t.Fatalf("%d Disconnect-Requests sent on a rejected login", n)
	}
}

// E5.10 a session without the identifiers a Disconnect needs gives a clear error and sends nothing.
func TestE5_10_MissingIdentifiers(t *testing.T) {
	f := newP5(t)
	a := f.member(f.plan("Basic"), "alice")
	f.exec(`INSERT INTO session(member_id, username, acct_session_id, nas_ip, status, started_at, last_update_at)
		VALUES (?,?,?,?,3,?,?)`, a, "alice", "", "127.0.0.1", t0.Unix()-10, t0.Unix())
	id := scalar[int64](t, f.db, `SELECT id FROM session`)
	r := f.c.Send("POST", spath(id), nil)
	if r.StatusCode != 422 || !strings.Contains(r.Body, "Acct-Session-Id") {
		t.Fatalf("missing session id: %d %s", r.StatusCode, r.Body)
	}
	f.exec(`UPDATE session SET acct_session_id='x', nas_ip='' WHERE id=?`, id)
	r = f.c.Send("POST", spath(id), nil)
	if r.StatusCode != 422 || !strings.Contains(r.Body, "router address") {
		t.Fatalf("missing nas ip: %d %s", r.StatusCode, r.Body)
	}
	if n := len(f.coa.Requests()); n != 0 {
		t.Fatalf("%d requests sent", n)
	}
	if scalar[int](t, f.db, `SELECT count(*) FROM session WHERE ended_at IS NOT NULL`) != 0 {
		t.Fatal("session ended")
	}
	// Framed-IP and MAC are optional ("those known"): the Disconnect still goes out.
	f.exec(`UPDATE session SET nas_ip='127.0.0.1', framed_ip='', mac='' WHERE id=?`, id)
	if r := f.c.Send("POST", spath(id), nil); r.StatusCode != 200 {
		t.Fatalf("unknown ip/mac: %d %s", r.StatusCode, r.Body)
	}
	reqs := f.coa.Requests()
	if len(reqs) != 1 {
		t.Fatalf("%d requests", len(reqs))
	}
	if _, err := rfc2865.FramedIPAddress_Lookup(reqs[0].Packet); err == nil {
		t.Error("Framed-IP-Address sent although unknown")
	}
	if _, err := rfc2865.CallingStationID_Lookup(reqs[0].Packet); err == nil {
		t.Error("Calling-Station-Id sent although unknown")
	}
}

// E5.11 deactivating sends a Disconnect per open session; the member is inactive whatever the router says.
func TestE5_11_DeactivateKicks(t *testing.T) {
	f := newP5(t)
	p := f.plan("Two", "max_sessions", "2")
	id := f.member(p, "alice")
	f.online("alice", "a1", "192.168.88.10", "AA:BB:CC:DD:EE:01")
	f.online("alice", "a2", "192.168.88.11", "AA:BB:CC:DD:EE:02")
	form := harness.FormValues(t, f.c.Get(mpath(id)).Doc(t), "form#member-form")
	form.Del("active")
	f.coa.Always(fakenas.NAK)
	r := f.c.Send("PUT", mpath(id), form)
	if r.StatusCode != 200 || !strings.Contains(r.Body, "2 of 2 open sessions could not be disconnected") {
		t.Fatalf("deactivate with NAK: %d %s", r.StatusCode, r.Body)
	}
	if scalar[int](t, f.db, `SELECT active FROM member WHERE id=?`, id) != 0 {
		t.Fatal("member must be inactive")
	}
	if n := len(f.coa.Requests()); n != 2 {
		t.Fatalf("%d Disconnects, want one per open session", n)
	}
	if f.session("a1").ended.Valid || f.session("a2").ended.Valid {
		t.Fatal("sessions must stay after a NAK")
	}
	// Re-activate (nothing sent), deactivate again with a router that agrees: sessions end.
	form.Set("active", "on")
	f.c.Send("PUT", mpath(id), form)
	if n := len(f.coa.Requests()); n != 2 {
		t.Fatalf("re-activation sent Disconnects: %d", n)
	}
	f.coa.Always(fakenas.ACK)
	form.Del("active")
	if r := f.c.Send("PUT", mpath(id), form); r.StatusCode != 200 || r.Header.Get("HX-Redirect") == "" {
		t.Fatalf("deactivate with ACK: %d %s", r.StatusCode, r.Body)
	}
	if !f.session("a1").ended.Valid || !f.session("a2").ended.Valid || f.session("a1").cause != "Admin-Reset" {
		t.Fatal("ACKed sessions must end with Admin-Reset")
	}
}

// E5.13 the shared secret in settings is used (a router with another secret never verifies the request).
func TestE5_13_UsesSettingsSecretAndPort(t *testing.T) {
	f := newP5(t)
	f.member(f.plan("Basic"), "alice")
	f.online("alice", "s1", "192.168.88.10", "AA:BB:CC:DD:EE:01")
	other := fakenas.ListenCoA(t, "a-different-secret")
	f.exec(`UPDATE settings SET coa_port=?`, other.Port())
	f.c.Send("POST", spath(f.sessionDBID("s1")), nil) // answers ACK, but with the wrong secret: the reply never verifies
	reqs := other.Requests()
	if len(reqs) == 0 || reqs[0].Authentic {
		t.Fatalf("request must be signed with the settings secret, not the router's: %+v", reqs)
	}
	if f.session("s1").ended.Valid {
		t.Fatal("an unverifiable reply must not end the session")
	}
	if len(f.coa.Requests()) != 0 {
		t.Fatal("the old port was used")
	}
}
