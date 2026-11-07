// Package e2e holds the black-box tests. They drive the real app only through
// its interfaces: UDP RADIUS, the admin HTTP listener and the CLI. Test names
// are E<phase>.<nn>, and each comment lists the BR and O ids it covers.
package e2e

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/rfc2866"
	"layeh.com/radius/vendors/mikrotik"

	"hotspotpluslight/e2e/fakenas"
	"hotspotpluslight/e2e/harness"
	"hotspotpluslight/internal/auth"
	"hotspotpluslight/internal/clock"
	"hotspotpluslight/internal/secret"
	"hotspotpluslight/internal/store"
)

func scalar[T any](t *testing.T, db *sql.DB, q string, args ...any) T {
	t.Helper()
	var v T
	if err := db.QueryRow(q, args...).Scan(&v); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return v
}

// wantMigrations is the number of shipped migrations (0001_init, 0002_usage_daily, 0003_admin_must_change, 0004_drop_page_api_settings, 0005_business_name, 0006_server_settings).
const wantMigrations = 6

// pageIsClean reports inline style, inline handlers, hx-on and inline scripts/styles in a page.
func pageIsClean(d *goquery.Document) []string {
	var bad []string
	d.Find("*").Each(func(_ int, s *goquery.Selection) {
		for _, a := range s.Get(0).Attr {
			k := strings.ToLower(a.Key)
			if k == "style" || strings.HasPrefix(k, "on") || strings.HasPrefix(k, "hx-on") {
				bad = append(bad, goquery.NodeName(s)+"["+k+"]")
			}
		}
	})
	d.Find("script").Each(func(_ int, s *goquery.Selection) {
		if _, has := s.Attr("src"); !has {
			bad = append(bad, "inline <script>")
		}
	})
	if n := d.Find("style").Length(); n > 0 {
		bad = append(bad, "inline <style>")
	}
	return bad
}

const (
	listTarget  = "member-list"
	sheetTarget = "sheet-body"
	planTarget  = "plan-list"
)

func fragmentDoc(t *testing.T, r harness.Resp) *goquery.Document {
	t.Helper()
	return r.Doc(t)
}

// hxTrigger parses the HX-Trigger header of a response.
func hxTrigger(t *testing.T, r harness.Resp) map[string]any {
	t.Helper()
	raw := r.Header.Get("HX-Trigger")
	if raw == "" {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("HX-Trigger %q: %v", raw, err)
	}
	return m
}

func (f *p2) idOf(username string) int64 {
	return scalar[int64](f.t, f.db, `SELECT id FROM member WHERE username=?`, username)
}

// waitUntil polls cond until it is true; it fails the test after 10 s.
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	end := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(end) {
			t.Fatalf("timed out waiting for: %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (f *p5) cause(sid string) string {
	return f.session(sid).cause
}

func attr(d *goquery.Document, sel, name string) string { return d.Find(sel).First().AttrOr(name, "") }

func readRoot(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(harness.ModuleRoot(), name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func siteRoot() string { return filepath.Join(harness.ModuleRoot(), "site") }

// sitePages returns every html file of the site as a slash path relative to site/.
func sitePages(t *testing.T) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(siteRoot(), func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".html") {
			rel, _ := filepath.Rel(siteRoot(), p)
			out = append(out, filepath.ToSlash(rel))
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(out)
	return out
}

func siteDoc(t *testing.T, rel string) *goquery.Document {
	t.Helper()
	f, err := os.Open(filepath.Join(siteRoot(), filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	d, err := goquery.NewDocumentFromReader(f)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// siteText returns the visible text of the given pages, or of every page when none is given.
func siteText(t *testing.T, pages ...string) string {
	t.Helper()
	if len(pages) == 0 {
		pages = sitePages(t)
	}
	var b strings.Builder
	for _, p := range pages {
		b.WriteString(siteDoc(t, p).Find("body").Text())
		b.WriteString("\n")
	}
	return b.String()
}

var t0 = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

func startWithClock(t *testing.T) (*harness.Server, *clock.Fake) {
	fc := clock.NewFake(t0)
	return harness.Start(t, harness.Options{Clock: fc}), fc
}

// adminPassword is the password LoginAsAdmin set through the password page (Phase 11b).
func adminPassword(t *testing.T, s *harness.Server) string {
	t.Helper()
	return harness.AdminPassword
}

// runCLI runs the real binary with the server's data dir and optional stdin.
func runCLI(t *testing.T, s *harness.Server, stdin string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(harness.Binary(t), args...)
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HSP_DATA_DIR=" + s.DataDir, "HSP_SECRET_KEY=" + s.SecretKey}
	cmd.Stdin = strings.NewReader(stdin)
	var so, se strings.Builder
	cmd.Stdout, cmd.Stderr = &so, &se
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return so.String(), se.String(), code
}

func fieldError(r harness.Resp, t *testing.T, field string) string {
	return strings.TrimSpace(r.Doc(t).Find(`[data-error-for="` + field + `"]`).First().Text())
}

func setDefaultPlan(t *testing.T, c *harness.AdminClient, planID string) harness.Resp {
	t.Helper()
	form := harness.FormValues(t, c.Get("/admin/settings").Doc(t), "form#settings-form")
	form.Set("default_plan_id", planID)
	return c.Send("PUT", "/admin/settings", form)
}

// p2 is the Phase 2 fixture: app with a fake clock and a recording disconnector.
type p2 struct {
	t    *testing.T
	s    *harness.Server
	fc   *clock.Fake
	c    *harness.AdminClient
	db   *sql.DB
	disc *harness.FakeDisconnector
}

func newP2(t *testing.T) *p2 {
	t.Helper()
	fc := clock.NewFake(t0)
	disc := &harness.FakeDisconnector{}
	s := harness.Start(t, harness.Options{Clock: fc, Disconnector: disc})
	return &p2{t: t, s: s, fc: fc, c: harness.LoginAsAdmin(t, s), db: harness.DB(t, s.DataDir), disc: disc}
}

func (f *p2) plan(name string, kv ...string) int64 {
	return f.c.CreatePlan(harness.PlanForm(name, kv...))
}

func (f *p2) member(planID int64, username string, kv ...string) int64 {
	return f.c.CreateMember(harness.MemberForm(planID, username, kv...))
}

func (f *p2) exec(q string, args ...any) {
	f.t.Helper()
	if _, err := f.db.Exec(q, args...); err != nil {
		f.t.Fatalf("%s: %v", q, err)
	}
}

// openSession inserts an open router session row for the member (what Phase 4 accounting will create).
func (f *p2) openSession(memberID int64, username, acctID string, lastUpdate time.Time) {
	f.exec(`INSERT INTO session(member_id, username, acct_session_id, nas_ip, framed_ip, mac, status, started_at, last_update_at)
		VALUES (?,?,?,?,?,?,3,?,?)`, memberID, username, acctID, "10.0.0.1", "192.168.88.10", "AA:BB:CC:DD:EE:FF", lastUpdate.Unix()-100, lastUpdate.Unix())
}

// advance moves the clock in steps shorter than the admin session's idle limit,
// touching a page in between so the session stays alive.
func (f *p2) advance(d time.Duration) {
	for d > 0 {
		step := min(d, 30*time.Minute)
		f.fc.Advance(step)
		d -= step
		f.c.Get("/admin/plans")
	}
}

func mpath(id int64) string { return "/admin/members/" + strconv.FormatInt(id, 10) }

func text(s *goquery.Selection) string { return strings.Join(strings.Fields(s.Text()), " ") }

func memberCount(t *testing.T, db *sql.DB) int {
	return scalar[int](t, db, `SELECT count(*) FROM member`)
}

func revealed(f *p2, id int64) string {
	f.t.Helper()
	r := f.c.Get(mpath(id) + "/password")
	if r.StatusCode != 200 {
		f.t.Fatalf("reveal: %d", r.StatusCode)
	}
	return text(r.Doc(f.t).Find("[data-password]"))
}

func rows(d *goquery.Document) []string {
	var out []string
	d.Find("tr[data-member]").Each(func(_ int, s *goquery.Selection) { out = append(out, s.AttrOr("data-username", "")) })
	return out
}

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

// p4 is the Phase 4 fixture: the Phase 3 fixture plus the router's accounting port
// and a switchable crash-injection hook (E4.15).
type p4 struct {
	*p3
	hook *beforeCommit
}

// beforeCommit is the test side of Options.AccountingBeforeCommit.
type beforeCommit struct {
	mu sync.Mutex
	fn func() error
}

func (b *beforeCommit) run() error {
	b.mu.Lock()
	fn := b.fn
	b.mu.Unlock()
	if fn == nil {
		return nil
	}
	return fn()
}

func (b *beforeCommit) set(fn func() error) {
	b.mu.Lock()
	b.fn = fn
	b.mu.Unlock()
}

func newP4(t *testing.T) *p4 {
	t.Helper()
	fc := clock.NewFake(t0)
	disc := &harness.FakeDisconnector{}
	hook := &beforeCommit{}
	s := harness.Start(t, harness.Options{Clock: fc, Disconnector: disc, AccountingBeforeCommit: hook.run})
	f := &p2{t: t, s: s, fc: fc, c: harness.LoginAsAdmin(t, s), db: harness.DB(t, s.DataDir), disc: disc}
	secret := scalar[string](t, f.db, `SELECT radius_shared_secret FROM settings`)
	nas := fakenas.New(s.Addrs.RadiusAuth, secret)
	nas.AcctAddr = s.Addrs.RadiusAcct
	return &p4{p3: &p3{p2: f, nas: nas, log: &harness.SafeBuffer{}}, hook: hook}
}

// acct sends one accounting packet and requires an Accounting-Response.
func (f *p4) acct(a fakenas.Acct) *radius.Packet {
	f.t.Helper()
	r, err := f.nas.Account(a)
	if err != nil {
		f.t.Fatalf("accounting %v %q: %v", a.Type, a.User, err)
	}
	if r.Code != radius.CodeAccountingResponse {
		f.t.Fatalf("accounting %v %q: reply %v", a.Type, a.User, r.Code)
	}
	return r
}

func acct(typ rfc2866.AcctStatusType, user, sid string, dl, ul uint64, secs uint32) fakenas.Acct {
	return fakenas.Acct{Type: typ, User: user, SessionID: sid, Download: dl, Upload: ul, Seconds: secs}
}

func (f *p4) start(user, sid string) *radius.Packet {
	return f.acct(acct(rfc2866.AcctStatusType_Value_Start, user, sid, 0, 0, 0))
}

func (f *p4) interim(user, sid string, dl, ul uint64, secs uint32) *radius.Packet {
	return f.acct(acct(rfc2866.AcctStatusType_Value_InterimUpdate, user, sid, dl, ul, secs))
}

func (f *p4) stop(user, sid string, dl, ul uint64, secs uint32) *radius.Packet {
	return f.acct(acct(rfc2866.AcctStatusType_Value_Stop, user, sid, dl, ul, secs))
}

type usage struct{ dl, ul, secs int64 }

func (f *p4) usage(id int64) usage {
	f.t.Helper()
	var u usage
	if err := f.db.QueryRow(`SELECT used_download_bytes, used_upload_bytes, used_seconds FROM member WHERE id=?`, id).Scan(&u.dl, &u.ul, &u.secs); err != nil {
		f.t.Fatal(err)
	}
	return u
}

func (f *p4) wantUsage(id int64, want usage) {
	f.t.Helper()
	if got := f.usage(id); got != want {
		f.t.Fatalf("member usage %+v, want %+v", got, want)
	}
}

func (f *p4) sessions(where string, args ...any) int {
	return scalar[int](f.t, f.db, `SELECT count(*) FROM session WHERE `+where, args...)
}

// sessRow is a session row as the tests look at it.
type sessRow struct {
	status                                int
	startedAt, lastUpdate                 int64
	ended                                 sql.NullInt64
	cause, nasIP, framedIP, mac, nasIdent string
	dl, ul, secs                          int64
}

func (f *p4) session(sid string) sessRow {
	f.t.Helper()
	var r sessRow
	err := f.db.QueryRow(`SELECT status, started_at, last_update_at, ended_at, terminate_cause, nas_ip, framed_ip, mac, nas_identifier,
		download_bytes, upload_bytes, session_seconds FROM session WHERE acct_session_id=? ORDER BY id DESC LIMIT 1`, sid).
		Scan(&r.status, &r.startedAt, &r.lastUpdate, &r.ended, &r.cause, &r.nasIP, &r.framedIP, &r.mac, &r.nasIdent, &r.dl, &r.ul, &r.secs)
	if err != nil {
		f.t.Fatalf("session %q: %v", sid, err)
	}
	return r
}

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

// p7 is the Phase 7 fixture: the Phase 5 fixture (real Disconnect client + fake
// router listener) plus batch helpers.
type p7 struct{ *p5 }

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

func cell(row *goquery.Selection, col string) string {
	return text(row.Find("td[data-col=" + col + "]"))
}

// p8 is the Phase 8 fixture: the Phase 5 fixture (real Disconnect client against a fake router) with the
// server log captured, so that tests can look at what was logged.
type p8 struct {
	*p5
	logs *harness.SafeBuffer
}

func newP8(t *testing.T) *p8 {
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
	return &p8{p5: &p5{p4: &p4{p3: &p3{p2: f, nas: nas, log: logs}, hook: hook}, coa: coa}, logs: logs}
}

// script returns the setup script shown on /admin/router.
func (f *p8) script() (string, *goquery.Document) {
	f.t.Helper()
	r := f.c.Get("/admin/router")
	if r.StatusCode != 200 {
		f.t.Fatalf("/admin/router: %d", r.StatusCode)
	}
	d := r.Doc(f.t)
	s := d.Find("[data-script]")
	if s.Length() != 1 {
		f.t.Fatalf("%d [data-script] elements on /admin/router", s.Length())
	}
	return s.Text(), d
}

type linkRow struct{ at, from string }

// link reads one row of the router link status panel.
func link(t *testing.T, d *goquery.Document, kind string) linkRow {
	t.Helper()
	row := d.Find(`[data-link="` + kind + `"]`)
	if row.Length() != 1 {
		t.Fatalf("panel row %q: %d matches", kind, row.Length())
	}
	return linkRow{text(row.Find("[data-at]")), text(row.Find("[data-from]"))}
}

// oldInstall is a database created by an older release, before the current migrations.
type oldInstall struct {
	DataDir, Key string
}

// seedOldDB builds a first-boot database at the schema of the old migrations (0003 or later).
// The app cannot do this itself, because its seeding writes columns the old schema lacks.
func seedOldDB(t *testing.T, old fs.FS) oldInstall {
	t.Helper()
	in := oldInstall{DataDir: t.TempDir(), Key: harness.NewKey()}
	st, err := store.Open(filepath.Join(in.DataDir, "hsp.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Migrate(context.Background(), old, time.Now); err != nil {
		st.Close()
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := secret.ParseKey(in.Key)
	if err != nil {
		t.Fatal(err)
	}
	box, err := secret.NewBox(raw)
	if err != nil {
		t.Fatal(err)
	}
	kc, err := box.KeyCheck()
	if err != nil {
		t.Fatal(err)
	}
	hash, err := auth.HashPassword("admin")
	if err != nil {
		t.Fatal(err)
	}
	db := harness.DB(t, in.DataDir)
	now := time.Now().Unix()
	if _, err := db.Exec(`INSERT INTO admin(id, username, password_hash, must_change_password, updated_at) VALUES (1, 'admin', ?, 1, ?)`, hash, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO settings(id, key_check, radius_shared_secret, updated_at) VALUES (1, ?, 'old-secret', ?)`, kc, now); err != nil {
		t.Fatal(err)
	}
	return in
}

// startUpgraded applies the shipped migrations to an old install and starts the app on it.
// The migrations run before the app starts, because the harness addresses (127.0.0.1:0) only seed a
// first boot. Without the UPDATE, the upgraded row would hold the 0006 defaults and bind :8081, :1812 and :1813.
func startUpgraded(t *testing.T, in oldInstall) *harness.Server {
	t.Helper()
	st, err := store.Open(filepath.Join(in.DataDir, "hsp.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Migrate(context.Background(), store.EmbeddedMigrations(), time.Now); err != nil {
		st.Close()
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := harness.DB(t, in.DataDir).Exec(`UPDATE settings SET admin_addr='127.0.0.1:0', radius_auth_addr='127.0.0.1:0', radius_acct_addr='127.0.0.1:0'`); err != nil {
		t.Fatal(err)
	}
	return harness.Start(t, harness.Options{DataDir: in.DataDir, SecretKey: in.Key})
}
