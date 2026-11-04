package e2e

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"html"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"layeh.com/radius"
	"layeh.com/radius/rfc2866"

	"hotspotpluslight/e2e/fakenas"
	"hotspotpluslight/e2e/harness"
	"hotspotpluslight/internal/clock"
)

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

func portOf(t *testing.T, addr string) string {
	t.Helper()
	_, p, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	return p
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

// E8.03 the setup script carries the secret, ports, interval and CoA port, and follows settings.
func TestE8_03_SetupScript(t *testing.T) {
	f := newP8(t)
	f.exec(`UPDATE settings SET accounting_interval_seconds=120, coa_port=3799, radius_shared_secret='Sup3r-Secret-Value'`)
	script, _ := f.script()
	for _, want := range []string{
		"/radius add",
		"service=hotspot",
		"address=<SERVER-IP>", // no public base URL any more: the admin fills in the server address (Phase 12a)
		`secret="Sup3r-Secret-Value"`,
		"authentication-port=" + portOf(t, f.s.Addrs.RadiusAuth),
		"accounting-port=" + portOf(t, f.s.Addrs.RadiusAcct),
		"timeout=5s",
		"/radius incoming set accept=yes port=3799",
		"use-radius=yes",
		"login-by=http-pap",
		"radius-accounting=yes",
		"radius-interim-update=120s",
		"VERIFY-ON-HARDWARE",
		"extracted hotspot folder",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("script lacks %q\n%s", want, script)
		}
	}
	// match the port as a whole number, so a random port that is a substring of other digits does not trip the check
	adminPort := regexp.MustCompile(`(^|[^0-9])` + portOf(t, f.s.Addrs.Admin) + `([^0-9]|$)`)
	if adminPort.MatchString(script) {
		t.Error("the admin port appears in the script")
	}
	// every RouterOS command line sits under a comment, and every unverified command says so
	lines := strings.Split(script, "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, "/") && (i == 0 || !strings.HasPrefix(lines[i-1], "#")) {
			t.Errorf("command without a comment line above it: %q", l)
		}
	}
	// settings changes show immediately
	f.exec(`UPDATE settings SET accounting_interval_seconds=300, coa_port=3800, radius_shared_secret='another-one'`)
	script, _ = f.script()
	if !strings.Contains(script, "radius-interim-update=300s") || !strings.Contains(script, "port=3800") || !strings.Contains(script, `secret="another-one"`) || strings.Contains(script, "Sup3r-Secret-Value") {
		t.Errorf("script did not follow the settings:\n%s", script)
	}
}

// E8.03b characters RouterOS treats specially in the secret are escaped, and the server address stays a placeholder.
func TestE8_03b_ScriptVariants(t *testing.T) {
	f := newP8(t)
	f.exec(`UPDATE settings SET radius_shared_secret=?`, `a"b$c\d?e`)
	script, _ := f.script()
	if !strings.Contains(script, `secret="a\"b\$c\\d\?e"`) {
		t.Errorf("secret not escaped for RouterOS:\n%s", script)
	}
	// /radius needs an address, not a name: say so instead of guessing.
	if !strings.Contains(script, "address=<SERVER-IP>") {
		t.Errorf("the RADIUS address must be a placeholder:\n%s", script)
	}
	// a blind paste must not half-apply: no executable line may contain a placeholder
	for _, l := range strings.Split(script, "\n") {
		if strings.Contains(l, "<") && !strings.HasPrefix(l, "#") {
			t.Errorf("placeholder in an executable line: %q", l)
		}
	}
	if !strings.Contains(script, "# EDIT THEN UNCOMMENT") {
		t.Error("commented-out commands lack the edit marker")
	}
}

// E8.03c the page needs a login.
func TestE8_03c_RouterPageNeedsLogin(t *testing.T) {
	f := newP8(t)
	anon := harness.NewAdminClient(t, f.s)
	if r := anon.Get("/admin/router"); r.StatusCode == 200 {
		t.Errorf("anonymous GET /admin/router: %d", r.StatusCode)
	}
}

// E8.04 every download (all 8): a zip under hotspot/, no server template tokens, no plain-passwd anywhere,
// no password in a URL. (Rewritten in Phase 12b for the 8 designs; the file lists are E12.01.)
func TestE8_04_PackContents(t *testing.T) {
	f := newP8(t)
	for _, key := range []string{"mikrotik", "air", "midnight", "aurora", "dusk", "ripple", "sunrise", "ocean"} {
		path := "/admin/router/pages/" + key + ".zip"
		r := f.c.Get(path)
		if r.StatusCode != 200 || r.Header.Get("Content-Type") != "application/zip" {
			t.Fatalf("%s: %d %s", path, r.StatusCode, r.Header.Get("Content-Type"))
		}
		zr, err := zip.NewReader(bytes.NewReader([]byte(r.Body)), int64(len(r.Body)))
		if err != nil {
			t.Fatal(err)
		}
		for _, zf := range zr.File {
			if !strings.HasPrefix(zf.Name, "hotspot/") {
				t.Errorf("%s: %s is outside hotspot/", path, zf.Name)
			}
			rc, err := zf.Open()
			if err != nil {
				t.Fatal(err)
			}
			b, _ := io.ReadAll(rc)
			rc.Close()
			if bytes.Contains(b, []byte("plain-passwd")) {
				t.Errorf("%s: %s contains plain-passwd", path, zf.Name)
			}
			if strings.HasSuffix(zf.Name, ".html") {
				body := string(b)
				for _, tok := range []string{"{{", "}}", "{%", "%}", "<no value>", "%!", "@THEME@", "@MESSAGES@"} {
					if strings.Contains(body, tok) {
						t.Errorf("%s: %s contains the template token %q", path, zf.Name, tok)
					}
				}
				if regexp.MustCompile(`(?i)[?&;](pap|password|passwd)=`).MatchString(body) {
					t.Errorf("%s: %s puts a password into a URL", path, zf.Name)
				}
			}
		}
	}
}

// E8.05 the RADIUS shared secret is rendered only inside a type=password input on the settings page (owner
// Q44, 2026-11-02: kept there, masked). The one other place is the router setup script, which has to carry it
// (E8.03). It never appears in logs, flash messages or any other page.
func TestE8_05_SecretOnlyMasked(t *testing.T) {
	f := newP8(t)
	secret := f.nas.Secret // the generated 24-character secret; the fake router and the fake CoA listener use it too

	// The page is HTML: a secret containing & < > " ' appears escaped in the markup.
	esc := html.EscapeString(secret)
	has := func(body string) bool { return strings.Contains(body, secret) || strings.Contains(body, esc) }
	// settings page: the secret occurs once, as the value of a password input, and nowhere else in the markup
	r := f.c.Get("/admin/settings")
	d := r.Doc(t)
	in := d.Find(`input[name="radius_shared_secret"]`)
	if in.Length() != 1 || in.AttrOr("type", "") != "password" || in.AttrOr("value", "") != secret {
		t.Fatalf("secret input: n=%d type=%q", in.Length(), in.AttrOr("type", ""))
	}
	if n := strings.Count(r.Body, esc); n != 1 {
		t.Errorf("secret occurs %d times in the settings page, want 1 (the password input)", n)
	}
	if d.Find("textarea, pre, code").FilterFunction(func(_ int, s *goquery.Selection) bool { return strings.Contains(s.Text(), secret) }).Length() != 0 {
		t.Error("secret rendered as text")
	}

	// saving shows no secret outside the password input either
	form := harness.FormValues(t, d, "form#settings-form")
	put := f.c.Send("PUT", "/admin/settings", form)
	if put.StatusCode != 200 || strings.Count(put.Body, esc) != 1 {
		t.Errorf("PUT settings: %d, secret occurs %d times", put.StatusCode, strings.Count(put.Body, esc))
	}
	pd := put.Doc(t)
	if pd.Find(`input[name="radius_shared_secret"]`).AttrOr("type", "") != "password" {
		t.Error("saved form: secret not in a password input")
	}
	// a rejected save (422) keeps the secret masked as well
	bad := harness.FormValues(t, d, "form#settings-form")
	bad.Set("coa_port", "nope")
	if r := f.c.Send("PUT", "/admin/settings", bad); r.StatusCode != 422 || strings.Count(r.Body, esc) != 1 || r.Doc(t).Find(`input[name="radius_shared_secret"]`).AttrOr("type", "") != "password" {
		t.Errorf("422 form: %d, occurrences %d", r.StatusCode, strings.Count(r.Body, esc))
	}

	// activity that touches the secret: logins, accounting, a kick, a bad-secret packet
	f.member(f.plan("Basic"), "alice", "password", "secret1")
	f.accepted("alice", "secret1")
	f.online("alice", "s1", "192.168.88.10", "AA:BB:CC:DD:EE:01")
	f.c.Send("POST", spath(f.sessionDBID("s1")), nil)
	wrong := fakenas.New(f.s.Addrs.RadiusAuth, "not-the-secret")
	wrong.Timeout = 300 * time.Millisecond
	wrong.Authenticate("alice", "secret1")

	// every other admin page is free of it; /admin/router only inside the script
	for _, p := range []string{"/admin/members", "/admin/members/" + strconv.FormatInt(f.memberIDByName("alice"), 10), "/admin/plans", "/admin/sessions", "/admin/batches", "/admin/members/new", "/admin/plans/new"} {
		if body := f.c.Get(p).Body; has(body) {
			t.Errorf("%s contains the secret", p)
		}
	}
	rt := f.c.Get("/admin/router")
	rd := rt.Doc(t)
	rd.Find("[data-script]").Remove()
	if h, _ := rd.Html(); has(h) {
		t.Error("/admin/router shows the secret outside the setup script")
	}
	if _, body := harness.Get(t, "http://"+f.s.Addrs.Admin+"/healthz"); has(body) {
		t.Error("/healthz contains the secret")
	}
	if logs := f.logs.String(); strings.Contains(logs, secret) || strings.Contains(logs, "not-the-secret") {
		t.Errorf("a secret reached the log:\n%s", logs)
	}
}

func (f *p8) memberIDByName(name string) int64 {
	return scalar[int64](f.t, f.db, `SELECT id FROM member WHERE username=?`, name)
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

// E8.07 the router link status panel: last Access-Request / Accounting / Disconnect with time and source, bad
// shared secrets counted with their source; in memory only (no DB writes, gone after a restart).
func TestE8_07_LinkStatusPanel(t *testing.T) {
	f := newP8(t)
	panel := func() *goquery.Document { return f.c.Get("/admin/router").Doc(t) }

	d := panel()
	for _, k := range []string{"access-request", "accounting", "disconnect"} {
		if got := link(t, d, k); got.at != "never" || got.from != "" {
			t.Errorf("%s before any traffic: %+v", k, got)
		}
	}
	if n := text(d.Find("[data-bad-secret-count]")); n != "0" {
		t.Errorf("bad secret count %q", n)
	}

	f.member(f.plan("Basic"), "alice")
	f.db.SetMaxOpenConns(1) // data_version is per connection
	version := func() int64 { return scalar[int64](t, f.db, `PRAGMA data_version`) }
	_ = version() // data_version only reports commits of OTHER connections once it has been read once
	before := version()

	// packets that change nothing in the database: an unknown user, a bad accounting authenticator, a wrong secret
	f.fc.Advance(time.Minute) // 12:01:00
	if r, err := f.nas.Authenticate("nobody", "x"); err != nil || r.Code != radius.CodeAccessReject {
		t.Fatalf("login: %v %v", r, err)
	}
	f.fc.Advance(time.Minute) // 12:02:00
	badAcct := fakenas.New(f.s.Addrs.RadiusAuth, "wrong-secret")
	badAcct.AcctAddr, badAcct.Timeout = f.s.Addrs.RadiusAcct, 300*time.Millisecond
	for range 2 {
		if _, err := badAcct.Account(fakenas.Acct{Type: rfc2866.AcctStatusType_Value_Start, User: "alice", SessionID: "x"}); err == nil {
			t.Fatal("bad accounting authenticator answered")
		}
	}
	f.fc.Advance(time.Minute) // 12:03:00
	badAuth := fakenas.New(f.s.Addrs.RadiusAuth, "wrong-secret")
	badAuth.Timeout = 300 * time.Millisecond
	if _, err := badAuth.Authenticate("alice", "pw123456"); err == nil {
		t.Fatal("a wrong-secret Access-Request was answered")
	}
	f.fc.Advance(time.Minute) // 12:04:00
	if r, err := f.nas.Account(fakenas.Acct{Type: rfc2866.AcctStatusType_Value_InterimUpdate, User: "nobody", SessionID: "u1"}); err != nil || r.Code != radius.CodeAccountingResponse {
		t.Fatalf("accounting: %v %v", r, err)
	}
	if after := version(); after != before {
		t.Errorf("the database changed (data_version %d -> %d): link status must be in memory only", before, after)
	}

	d = panel()
	if got := link(t, d, "access-request"); got.at != "2026-11-01 12:01:00" || got.from != "127.0.0.1" {
		t.Errorf("last Access-Request: %+v", got)
	}
	if got := link(t, d, "accounting"); got.at != "2026-11-01 12:04:00" || got.from != "127.0.0.1" {
		t.Errorf("last Accounting: %+v", got)
	}
	if got := link(t, d, "disconnect"); got.at != "never" {
		t.Errorf("last Disconnect: %+v", got)
	}
	// 2 bad accounting authenticators + 1 wrong-secret Access-Request, all from this machine
	if n := text(d.Find("[data-bad-secret-count]")); n != "3" {
		t.Errorf("bad secret count %q, want 3", n)
	}
	src := d.Find("[data-bad-secret-from]")
	if src.Length() != 1 || src.AttrOr("data-bad-secret-from", "") != "127.0.0.1" || text(src.Find("[data-count]")) != "3" || text(src.Find("[data-at]")) != "2026-11-01 12:03:00" {
		t.Errorf("bad secret sources: %s", text(d.Find("[data-bad-secret-sources]")))
	}

	// a Disconnect: ACK
	f.online("alice", "s1", "192.168.88.10", "AA:BB:CC:DD:EE:01")
	f.fc.Advance(time.Minute) // 12:05:00
	if r := f.c.Send("POST", spath(f.sessionDBID("s1")), nil); r.StatusCode != 200 {
		t.Fatalf("kick: %d", r.StatusCode)
	}
	d = panel()
	got := link(t, d, "disconnect")
	if got.at != "2026-11-01 12:05:00" || got.from != "127.0.0.1" || !strings.Contains(text(d.Find(`[data-link="disconnect"]`)), "ACK") {
		t.Errorf("last Disconnect: %+v / %s", got, text(d.Find(`[data-link="disconnect"]`)))
	}
}

// E8.07b the panel is empty again after a restart.
func TestE8_07b_PanelResetsOnRestart(t *testing.T) {
	fc := clock.NewFake(t0)
	s := harness.Start(t, harness.Options{Clock: fc})
	db := harness.DB(t, s.DataDir)
	secret := scalar[string](t, db, `SELECT radius_shared_secret FROM settings`)
	nas := fakenas.New(s.Addrs.RadiusAuth, secret)
	if _, err := nas.Authenticate("x", "y"); err != nil {
		t.Fatal(err)
	}
	c := harness.LoginAsAdmin(t, s)
	if got := link(t, c.Get("/admin/router").Doc(t), "access-request"); got.from != "127.0.0.1" {
		t.Fatalf("before restart: %+v", got)
	}
	s.Stop()
	s2 := harness.Start(t, harness.Options{DataDir: s.DataDir, SecretKey: s.SecretKey, Clock: fc})
	c2 := harness.NewAdminClient(t, s2)
	c2.Login("admin", harness.AdminPassword)
	if got := link(t, c2.Get("/admin/router").Doc(t), "access-request"); got.at != "never" {
		t.Errorf("after restart: %+v", got)
	}
}

// E8.06 graceful shutdown: an accounting transaction in flight completes and is answered, the listeners close,
// the pruning goroutine stops, and nothing is left running.
func TestE8_06_GracefulShutdown(t *testing.T) {
	f := newP8(t)
	id := f.member(f.plan("Basic"), "alice")
	f.start("alice", "s1")

	entered, release := make(chan struct{}), make(chan struct{})
	f.hook.set(func() error { close(entered); <-release; return nil })
	type result struct {
		r   *radius.Packet
		err error
	}
	reply := make(chan result, 1)
	go func() {
		r, err := f.nas.AcctExchange(f.nas.AcctPacket(acct(rfc2866.AcctStatusType_Value_InterimUpdate, "alice", "s1", 5000, 500, 90)))
		reply <- result{r, err}
	}()
	<-entered // the transaction is open, just before its commit

	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		done <- f.s.App.Shutdown(ctx)
	}()
	select {
	case err := <-done:
		t.Fatalf("Shutdown returned (%v) while a transaction was in flight", err)
	case <-time.After(300 * time.Millisecond):
	}
	// new connections are refused as soon as shutdown began
	waitClosed := func(addr string) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for {
			c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
			if err != nil {
				return
			}
			c.Close()
			if time.Now().After(deadline) {
				t.Fatalf("%s still accepts connections during shutdown", addr)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	waitClosed(f.s.Addrs.Admin)

	close(release) // let the commit go
	res := <-reply
	if res.err != nil || res.r.Code != radius.CodeAccountingResponse {
		t.Fatalf("in-flight accounting was not answered: %+v", res)
	}
	if err := <-done; err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	// the committed change is in the file
	db := harness.DB(t, f.s.DataDir)
	if dl := scalar[int64](t, db, `SELECT used_download_bytes FROM member WHERE id=?`, id); dl != 5000 {
		t.Errorf("in-flight accounting lost: download %d, want 5000", dl)
	}
	// the sockets are released: the same addresses can be bound again
	for _, a := range []string{f.s.Addrs.RadiusAuth, f.s.Addrs.RadiusAcct} {
		pc, err := net.ListenPacket("udp", a)
		if err != nil {
			t.Errorf("UDP %s still bound: %v", a, err)
			continue
		}
		pc.Close()
	}
	// the pruning goroutine has stopped
	buf := make([]byte, 1<<20)
	if stacks := string(buf[:runtime.Stack(buf, true)]); strings.Contains(stacks, "startPruning") {
		t.Errorf("pruning goroutine still running after Shutdown:\n%s", stacks)
	}
	// Shutdown is idempotent
	if err := f.s.App.Shutdown(context.Background()); err != nil {
		t.Errorf("second Shutdown: %v", err)
	}
	if !strings.Contains(f.logs.String(), "shutdown complete") {
		t.Error("no 'shutdown complete' log line")
	}
}

// E8.08 the real binary logs structured lines: JSON with HSP_LOG_FORMAT=json (time, level, msg, key=value
// attributes), text by default; it never logs the admin password or the secrets; SIGTERM shuts it down cleanly.
func TestE8_08_LogFormatAndSignal(t *testing.T) {
	for _, format := range []string{"json", "text", ""} {
		dir := t.TempDir()
		cmd := exec.Command(harness.Binary(t), "serve")
		cmd.Env = []string{
			"PATH=" + os.Getenv("PATH"), "HSP_DATA_DIR=" + dir, "HSP_SECRET_KEY=" + harness.NewKey(),
			"HSP_ADMIN_ADDR=127.0.0.1:0", "HSP_RADIUS_AUTH_ADDR=127.0.0.1:0", "HSP_RADIUS_ACCT_ADDR=127.0.0.1:0",
			"HSP_RADIUS_SHARED_SECRET=Env-Seeded-Secret-77",
		}
		if format != "" {
			cmd.Env = append(cmd.Env, "HSP_LOG_FORMAT="+format)
		}
		var so, se harness.SafeBuffer
		cmd.Stdout, cmd.Stderr = &so, &se
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(10 * time.Second)
		for !strings.Contains(se.String(), "listening") && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		if runtime.GOOS == "windows" {
			// Windows cannot send SIGTERM to a child process. Kill it and skip only the
			// signal checks; graceful stop on Windows is covered by E11.33 (it asserts "shutdown complete" in hsp.log).
			cmd.Process.Kill()
			cmd.Wait()
		} else {
			cmd.Process.Signal(syscall.SIGTERM)
			waited := make(chan error, 1)
			go func() { waited <- cmd.Wait() }()
			select {
			case err := <-waited:
				if err != nil {
					t.Errorf("format %q: exit after SIGTERM: %v\n%s", format, err, se.String())
				}
			case <-time.After(10 * time.Second):
				cmd.Process.Kill()
				t.Fatalf("format %q: no exit after SIGTERM", format)
			}
		}
		logs := se.String()
		if runtime.GOOS != "windows" && !strings.Contains(logs, "shutdown complete") {
			t.Errorf("format %q: no 'shutdown complete' in\n%s", format, logs)
		}
		if !strings.Contains(so.String(), "password: admin") || strings.Contains(logs, "password: admin") || strings.Contains(logs, "Env-Seeded-Secret-77") {
			t.Errorf("format %q: first-start block missing from stdout, or a password or secret in the log", format)
		}
		lines := strings.Split(strings.TrimSpace(logs), "\n")
		for _, l := range lines {
			if format == "json" {
				var m map[string]any
				if err := json.Unmarshal([]byte(l), &m); err != nil || m["time"] == nil || m["level"] == nil || m["msg"] == nil {
					t.Errorf("not a JSON log line: %q (%v)", l, err)
				}
			} else if !regexp.MustCompile(`^time=\S+ level=\w+ msg=`).MatchString(l) {
				t.Errorf("format %q: not a key=value log line: %q", format, l)
			}
		}
		if _, err := os.Stat(filepath.Join(dir, "hsp.db")); err != nil {
			t.Error(err)
		}
	}
	// an unknown format is a clear startup error
	_, stderr, code := runBinary(t, []string{"HSP_DATA_DIR=" + t.TempDir(), "HSP_SECRET_KEY=" + harness.NewKey(), "HSP_LOG_FORMAT=xml"}, "serve")
	if code == 0 || !strings.Contains(stderr, "HSP_LOG_FORMAT") {
		t.Errorf("HSP_LOG_FORMAT=xml: code %d, %q", code, stderr)
	}
}

// E8.09 the README covers install, configuration, first start, router setup, listeners, backup and the lab checklist.
func TestE8_09_Readme(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(harness.ModuleRoot(), "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	r := string(b)
	for _, want := range []string{
		"make build", "HSP_SECRET_KEY", "HSP_DATA_DIR", "HSP_ADMIN_ADDR", "HSP_RADIUS_AUTH_ADDR", "HSP_RADIUS_ACCT_ADDR", "HSP_LOG_LEVEL", "HSP_LOG_FORMAT",
		"first start", "make set-password", "admin set-password",
		"/admin/router", "hotspot folder",
		"8081", "firewall",
		"sqlite3 hsp.db", "unrecoverable",
		"[Unit]", "ExecStart=", "PLAN.md", "VERIFY-ON-HARDWARE",
	} {
		if !strings.Contains(r, want) {
			t.Errorf("README lacks %q", want)
		}
	}
	for _, gone := range []string{"public_base_url", "walled garden", "walled-garden", "8080", "HSP_HTTP_ADDR", "Public base URL"} {
		if strings.Contains(r, gone) {
			t.Errorf("README still mentions %q (removed in Phase 12a)", gone)
		}
	}
	if regexp.MustCompile(`[^\x00-\x7F]`).MatchString(strings.ReplaceAll(strings.ReplaceAll(r, "—", "-"), "…", "...")) {
		t.Error("README contains non-ASCII text (English only)")
	}
	for _, name := range []string{"Dockerfile", "docker-compose.yml"} {
		if _, err := os.Stat(filepath.Join(harness.ModuleRoot(), name)); err == nil {
			t.Errorf("%s must not exist", name)
		}
	}
	mk, _ := os.ReadFile(filepath.Join(harness.ModuleRoot(), "Makefile"))
	if !strings.Contains(string(mk), "set-password:") {
		t.Error("Makefile has no set-password target")
	}
}

// E8.10 the shared secret may not contain control characters (a newline would end the quoted string in the
// pasted RouterOS script and turn the rest into another command); other characters are stored as typed.
func TestE8_10_SecretControlCharacters(t *testing.T) {
	f := newP8(t)
	d := f.c.Get("/admin/settings").Doc(t)
	for _, bad := range []string{"abc\ndef", "abc\r\n/system reset-configuration", "ab\tc", "ab\x7fc"} {
		form := harness.FormValues(t, d, "form#settings-form")
		form.Set("radius_shared_secret", bad)
		r := f.c.Send("PUT", "/admin/settings", form)
		if r.StatusCode != 422 || !strings.Contains(r.Body, "control characters") {
			t.Errorf("secret %q: %d", bad, r.StatusCode)
		}
	}
	form := harness.FormValues(t, d, "form#settings-form")
	form.Set("radius_shared_secret", ` sp ace"$\? `)
	if r := f.c.Send("PUT", "/admin/settings", form); r.StatusCode != 200 {
		t.Fatalf("allowed secret: %d %s", r.StatusCode, r.Body)
	}
	if got := scalar[string](t, f.db, `SELECT radius_shared_secret FROM settings`); got != ` sp ace"$\? ` {
		t.Errorf("stored %q, want exactly as typed", got)
	}
}

// E8.12 the router page: no-store, the copy behaviour comes from the static script (no inline script).
func TestE8_12_RouterPageAssets(t *testing.T) {
	f := newP8(t)
	r := f.c.Get("/admin/router")
	if r.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control %q", r.Header.Get("Cache-Control"))
	}
	d := r.Doc(t)
	clip := ""
	d.Find("script[src]").Each(func(_ int, s *goquery.Selection) {
		if src := s.AttrOr("src", ""); strings.HasPrefix(src, "/admin/static/js/clipboard.js?v=") {
			clip = src
		}
	})
	if clip == "" {
		t.Error("page does not load /admin/static/js/clipboard.js?v=<hash>")
	}
	d.Find("script").Each(func(_ int, s *goquery.Selection) {
		if _, has := s.Attr("src"); !has {
			t.Errorf("inline script: %s", s.Text())
		}
	})
	if d.Find("button[data-copy-target]").Length() != 1 || d.Find("button[onclick]").Length() != 0 {
		t.Error("copy button must be markup-only with data-copy-target")
	}
	js := f.c.Get(clip)
	if js.StatusCode != 200 || !strings.Contains(js.Body, "data-copy-target") {
		t.Errorf("clipboard.js: %d", js.StatusCode)
	}
}
