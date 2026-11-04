package e2e

import (
	"context"
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

	"hotspotpluslight/e2e/harness"
	"hotspotpluslight/internal/domain"
)

// E12.07 (O20, changed 2026-11-04) the Reply-Message of every Access-Reject is exactly the code, and
// the sweep covers every reason the server can send over RADIUS.
func TestE12_07_ReplyMessageIsOnlyTheCode(t *testing.T) {
	f := newP3(t)
	day := int64(24 * 3600)
	seen := map[domain.Reason]bool{}
	check := func(r *radius.Packet, want domain.Reason) {
		t.Helper()
		if r.Code != radius.CodeAccessReject {
			t.Fatalf("%s: got %v, want Access-Reject", want, r.Code)
		}
		if got := rfc2865.ReplyMessage_GetString(r); got != string(want) {
			t.Errorf("%s: Reply-Message %q, want exactly the code", want, got)
		}
		seen[want] = true
	}
	basic := f.plan("Basic")
	alice := f.member(basic, "alice", "password", "pw")
	check(f.login("alice", "wrong"), domain.ReasonInvalidCredentials)
	f.exec(`UPDATE member SET active=0 WHERE id=?`, alice)
	check(f.login("alice", "pw"), domain.ReasonInactive)
	f.exec(`UPDATE member SET active=1, expires_at=? WHERE id=?`, t0.Unix()-1, alice)
	check(f.login("alice", "pw"), domain.ReasonExpired)
	f.exec(`UPDATE member SET expires_at=NULL, subscription_date=? WHERE id=?`, t0.Unix()-400*day, alice)
	check(f.login("alice", "pw"), domain.ReasonNoSubscription)

	small := f.member(f.plan("Small", "bulk_value", "100", "bulk_unit", "MB"), "small", "password", "pw")
	f.exec(`UPDATE member SET used_download_bytes=?, used_upload_bytes=0 WHERE id=?`, 200*mb, small)
	check(f.login("small", "pw"), domain.ReasonOutOfBulk)

	hour := f.member(f.plan("Hour", "time_limit_minutes", "60"), "hour", "password", "pw")
	f.exec(`UPDATE member SET used_seconds=3600 WHERE id=?`, hour)
	check(f.login("hour", "pw"), domain.ReasonOutOfTime)

	one := f.member(f.plan("One", "max_sessions", "1"), "one", "password", "pw")
	f.openSession(one, "one", "s1", f.fc.Now())
	check(f.login("one", "pw"), domain.ReasonNoMoreSession)

	// NO_PLAN and PLAN_REMOVED need rows the UI never creates (see E3.04): foreign keys off.
	np := f.member(basic, "noplan", "password", "pw")
	conn, err := f.db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(context.Background(), `PRAGMA foreign_keys=OFF`); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		plan int64
		want domain.Reason
	}{{0, domain.ReasonNoPlan}, {9999, domain.ReasonPlanRemoved}} {
		if _, err := conn.ExecContext(context.Background(), `UPDATE member SET plan_id=? WHERE id=?`, c.plan, np); err != nil {
			t.Fatal(err)
		}
		check(f.login("noplan", "pw"), c.want)
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
		r, err := f.nas.Exchange(p)
		if err != nil {
			t.Fatal(err)
		}
		check(r, domain.ReasonUnsupportedAuth)
	}

	// Every reason is covered except the two that are never sent over RADIUS.
	for _, r := range domain.AllReasons() {
		if !seen[r] && r != domain.ReasonGeneralError && r != domain.ReasonTooManyAttempts {
			t.Errorf("reason %s was not exercised", r)
		}
	}
}

// E12.08 one listener: HSP_HTTP_ADDR is ignored (nothing listens there), /healthz is on the admin listener
// without a login, and the old page API routes are 404 even for a logged-in admin.
func TestE12_08_OnlyTheAdminListener(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	old := l.Addr().String()
	l.Close() // a free port that the old public listener would have used
	s := harness.Start(t, harness.Options{Env: map[string]string{"HTTP_ADDR": old}})
	if c, err := net.DialTimeout("tcp", old, 300*time.Millisecond); err == nil {
		c.Close()
		t.Fatalf("something listens on the old public address %s", old)
	}
	resp, body := harness.Get(t, "http://"+s.Addrs.Admin+"/healthz")
	if resp.StatusCode != 200 || strings.TrimSpace(body) != "ok" || resp.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("/healthz: %d %q cache=%q", resp.StatusCode, body, resp.Header.Get("Cache-Control"))
	}
	c := harness.LoginAsAdmin(t, s)
	for _, p := range []string{"/api/v1/account", "/api/v1/config", "/api/v1/devices/aabbccddeeff/credentials", "/api/v1/nope"} {
		if r, _ := harness.Get(t, "http://"+s.Addrs.Admin+p); r.StatusCode != 404 {
			t.Errorf("anonymous %s: %d, want 404", p, r.StatusCode)
		}
		if r := c.Get(p); r.StatusCode != 404 {
			t.Errorf("admin %s: %d, want 404", p, r.StatusCode)
		}
	}
}

// E12.09 migration 0004 drops the three page-API settings columns and keeps every other value.
func TestE12_09_Migration0004(t *testing.T) {
	root := harness.ModuleRoot()
	read := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join(root, "internal/store/migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	old := fstest.MapFS{
		"0001_init.sql":              {Data: read("0001_init.sql")},
		"0002_usage_daily.sql":       {Data: read("0002_usage_daily.sql")},
		"0003_admin_must_change.sql": {Data: read("0003_admin_must_change.sql")},
	}
	s := harness.Start(t, harness.Options{Migrations: old})
	db := harness.DB(t, s.DataDir)
	cols := func() map[string]bool {
		rows, err := db.Query(`SELECT name FROM pragma_table_info('settings')`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		m := map[string]bool{}
		for rows.Next() {
			var n string
			rows.Scan(&n)
			m[n] = true
		}
		return m
	}
	gone := []string{"mac_auto_login", "after_login_url", "public_base_url"}
	for _, g := range gone {
		if !cols()[g] {
			t.Fatalf("before 0004 the column %s should exist", g)
		}
	}
	if _, err := db.Exec(`UPDATE settings SET site_title='Kept Cafe', timezone='Asia/Tehran', coa_port=3800, voucher_help='help text',
		mac_auto_login=1, after_login_url='https://example.com/x', public_base_url='http://10.0.0.2:8080'`); err != nil {
		t.Fatal(err)
	}
	secret := scalar[string](t, db, `SELECT radius_shared_secret FROM settings`)
	s.Stop()

	s2 := harness.Start(t, harness.Options{DataDir: s.DataDir, SecretKey: s.SecretKey})
	defer s2.Stop()
	if n := scalar[int](t, db, `SELECT count(*) FROM schema_migrations`); n != wantMigrations {
		t.Errorf("%d migrations, want %d", n, wantMigrations)
	}
	after := cols()
	for _, g := range gone {
		if after[g] {
			t.Errorf("column %s still exists", g)
		}
	}
	if scalar[string](t, db, `SELECT site_title FROM settings`) != "Kept Cafe" || scalar[string](t, db, `SELECT timezone FROM settings`) != "Asia/Tehran" ||
		scalar[int](t, db, `SELECT coa_port FROM settings`) != 3800 || scalar[string](t, db, `SELECT voucher_help FROM settings`) != "help text" ||
		scalar[string](t, db, `SELECT radius_shared_secret FROM settings`) != secret || scalar[int](t, db, `SELECT count(*) FROM admin`) != 1 {
		t.Error("existing data did not survive the migration")
	}
}

// E12.10 the setup script has no walled-garden and no html-directory lines; it keeps login-by=http-pap and
// tells the admin that the downloaded hotspot folder replaces the default one.
func TestE12_10_SetupScript(t *testing.T) {
	f := newP8(t)
	script, _ := f.script()
	for _, bad := range []string{"walled-garden", "walled garden", "html-directory", "public base URL"} {
		if strings.Contains(strings.ToLower(script), strings.ToLower(bad)) {
			t.Errorf("script still mentions %q:\n%s", bad, script)
		}
	}
	for _, want := range []string{"/radius add", "/radius incoming set accept=yes", "login-by=http-pap", "radius-accounting=yes", "radius-interim-update=",
		"replace the default hotspot folder", "flash/hotspot"} {
		if !strings.Contains(script, want) {
			t.Errorf("script lacks %q:\n%s", want, script)
		}
	}
}
