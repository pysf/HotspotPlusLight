package e2e

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"testing/fstest"
	"time"

	"golang.org/x/crypto/bcrypt"
	"layeh.com/radius"
	"layeh.com/radius/rfc2865"
	"layeh.com/radius/rfc2866"

	"hotspotpluslight/e2e/fakenas"
	"hotspotpluslight/e2e/harness"
)

// E0.01 boot with an empty dir creates DB, admin, settings (O15, BR-20).
func TestE0_01_FirstBootCreatesDBAdminSettings(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	if _, err := os.Stat(s.DBPath()); err != nil {
		t.Fatalf("db file missing: %v", err)
	}
	db := harness.DB(t, s.DataDir)
	if u := scalar[string](t, db, `SELECT username FROM admin WHERE id=1`); u != "admin" {
		t.Fatalf("admin username %q", u)
	}
	out := s.Stdout.String()
	for _, want := range []string{"=== HotspotPlus Lite first start ===", "username: admin", "password: admin", "set a new password", "hotspotpluslight admin set-password"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout lacks %q:\n%s", want, out)
		}
	}
	// O15/BR-20 (Phase 11b): the account starts as admin/admin and must change it at first login.
	hash := scalar[string](t, db, `SELECT password_hash FROM admin WHERE id=1`)
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte("admin")) != nil {
		t.Fatal("stored hash is not bcrypt(admin)")
	}
	if scalar[int](t, db, `SELECT must_change_password FROM admin WHERE id=1`) != 1 {
		t.Fatal("must_change_password not set at first start")
	}
	if scalar[string](t, db, `SELECT key_check FROM settings WHERE id=1`) == "" {
		t.Fatal("key_check empty")
	}
	if scalar[string](t, db, `SELECT radius_shared_secret FROM settings WHERE id=1`) == "" {
		t.Fatal("radius secret not generated")
	}
}

// E0.02 second boot keeps data, prints no new password.
func TestE0_02_SecondBootKeepsData(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	db := harness.DB(t, s.DataDir)
	hash1 := scalar[string](t, db, `SELECT password_hash FROM admin`)
	sec1 := scalar[string](t, db, `SELECT radius_shared_secret FROM settings`)
	s.Stop()

	s2 := harness.Start(t, harness.Options{DataDir: s.DataDir, SecretKey: s.SecretKey})
	if strings.Contains(s2.Stdout.String(), "first start") {
		t.Fatalf("first-start block printed again:\n%s", s2.Stdout.String())
	}
	if hash1 != scalar[string](t, db, `SELECT password_hash FROM admin`) ||
		sec1 != scalar[string](t, db, `SELECT radius_shared_secret FROM settings`) {
		t.Fatal("data changed on second boot")
	}
	if n := scalar[int](t, db, `SELECT count(*) FROM admin`); n != 1 {
		t.Fatalf("admin rows %d", n)
	}
}

// E0.03 /healthz on the admin listener (the only listener since Phase 12a); "/" is a 404.
func TestE0_03_Healthz(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	resp, body := harness.Get(t, "http://"+s.Addrs.Admin+"/healthz")
	if resp.StatusCode != 200 || strings.TrimSpace(body) != "ok" {
		t.Fatalf("healthz: %d %q", resp.StatusCode, body)
	}
	if r, _ := harness.Get(t, "http://"+s.Addrs.Admin+"/"); r.StatusCode != 404 {
		t.Errorf("admin /: %d", r.StatusCode)
	}
}

// E0.04 fakenas gets a well-formed Reject from the RADIUS listener.
func TestE0_04_FakeNASSmoke(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	secret := scalar[string](t, harness.DB(t, s.DataDir), `SELECT radius_shared_secret FROM settings`)
	reply, err := fakenas.New(s.Addrs.RadiusAuth, secret).Authenticate("nobody", "nothing")
	if err != nil {
		t.Fatal(err)
	}
	if reply.Code != radius.CodeAccessReject {
		t.Fatalf("code %v", reply.Code)
	}
	if rfc2865.ReplyMessage_GetString(reply) != "INVALID_CREDENTIALS" {
		t.Fatalf("reply message %q", rfc2865.ReplyMessage_GetString(reply))
	}
}

func runBinary(t *testing.T, env []string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(harness.Binary(t), args...)
	cmd.Env = append([]string{"PATH=" + os.Getenv("PATH")}, env...)
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

// E0.05 a missing HSP_SECRET_KEY is generated into hsp.key and reused; a lost or unreadable key file is never replaced.
func TestE0_05_MissingSecretKeyIsGenerated(t *testing.T) {
	s := harness.Start(t, harness.Options{NoKey: true})
	keyFile := filepath.Join(s.DataDir, "hsp.key")
	info, err := os.Stat(keyFile)
	if err != nil {
		t.Fatalf("hsp.key not created: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("hsp.key mode %v, want 0600", info.Mode().Perm())
	}
	raw, _ := os.ReadFile(keyFile)
	if key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw))); err != nil || len(key) != 32 {
		t.Fatalf("hsp.key is not base64 of 32 bytes: %q", raw)
	}
	s.Stop()

	// A restart reuses the same key file, so the key check passes.
	s2, err := harness.TryStart(harness.Options{DataDir: s.DataDir, NoKey: true})
	if err != nil {
		t.Fatalf("restart with the generated key: %v", err)
	}
	s2.Stop()
	if after, _ := os.ReadFile(keyFile); string(after) != string(raw) {
		t.Fatal("hsp.key changed on restart")
	}

	// An unreadable key file is an error, and the file is left alone.
	if runtime.GOOS != "windows" && os.Getuid() != 0 {
		if err := os.Chmod(keyFile, 0); err != nil {
			t.Fatal(err)
		}
		_, err := harness.TryStart(harness.Options{DataDir: s.DataDir, NoKey: true})
		os.Chmod(keyFile, 0o600)
		if err == nil || !strings.Contains(err.Error(), "key file") {
			t.Fatalf("unreadable key: want key file error, got %v", err)
		}
		if after, _ := os.ReadFile(keyFile); string(after) != string(raw) {
			t.Fatal("unreadable hsp.key was overwritten")
		}
	}

	// A lost key file with an existing database must not produce a new key.
	if err := os.Remove(keyFile); err != nil {
		t.Fatal(err)
	}
	if _, err := harness.TryStart(harness.Options{DataDir: s.DataDir, NoKey: true}); err == nil || !strings.Contains(err.Error(), "hsp.key") {
		t.Fatalf("lost key: want error naming hsp.key, got %v", err)
	}
	if _, err := os.Stat(keyFile); err == nil {
		t.Fatal("a new hsp.key was written next to an existing database")
	}
}

// E0.06 a key that cannot decrypt key_check refuses to start; data intact.
func TestE0_06_WrongKeyRefusesToStart(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	db := harness.DB(t, s.DataDir)
	hash := scalar[string](t, db, `SELECT password_hash FROM admin`)
	s.Stop()

	_, err := harness.TryStart(harness.Options{DataDir: s.DataDir, SecretKey: harness.NewKey()})
	if err == nil || !strings.Contains(err.Error(), "HSP_SECRET_KEY") || !strings.Contains(err.Error(), "key_check") {
		t.Fatalf("want clear key_check error, got %v", err)
	}
	if hash != scalar[string](t, db, `SELECT password_hash FROM admin`) {
		t.Fatal("data modified")
	}
	// the right key still works
	harness.Start(t, harness.Options{DataDir: s.DataDir, SecretKey: s.SecretKey})
}

// E0.08 first start creates no plan (the admin creates plans in the web UI) and no default plan.
func TestE0_08_NoPlanAtFirstStart(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	db := harness.DB(t, s.DataDir)
	if n := scalar[int](t, db, `SELECT count(*) FROM plan`); n != 0 {
		t.Fatalf("plans created at first start: %d", n)
	}
	if n := scalar[int](t, db, `SELECT count(*) FROM settings WHERE default_plan_id IS NULL`); n != 1 {
		t.Fatal("default_plan_id must be NULL at first start")
	}
	s.Stop()
	harness.Start(t, harness.Options{DataDir: s.DataDir, SecretKey: s.SecretKey})
	if n := scalar[int](t, db, `SELECT count(*) FROM plan`); n != 0 {
		t.Fatalf("plan created on later start: %d", n)
	}
}

// E0.09 (startup side) failing migration aborts startup at the previous version;
// editing an applied migration is refused by checksum.
func TestE0_09_MigrationsAtStartup(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	db := harness.DB(t, s.DataDir)
	if n := scalar[int](t, db, `SELECT count(*) FROM schema_migrations`); n != wantMigrations {
		t.Fatalf("migrations applied: %d, want %d", n, wantMigrations)
	}
	s.Stop()

	// a second start applies nothing
	harness.Start(t, harness.Options{DataDir: s.DataDir, SecretKey: s.SecretKey}).Stop()
	if n := scalar[int](t, db, `SELECT count(*) FROM schema_migrations`); n != wantMigrations {
		t.Fatalf("second start applied migrations: %d", n)
	}

	// deliberately failing migration after the shipped ones
	fsys := fstest.MapFS{
		"0001_init.sql":                   {Data: mustRead(t, "internal/store/migrations/0001_init.sql")},
		"0002_usage_daily.sql":            {Data: mustRead(t, "internal/store/migrations/0002_usage_daily.sql")},
		"0003_admin_must_change.sql":      {Data: mustRead(t, "internal/store/migrations/0003_admin_must_change.sql")},
		"0004_drop_page_api_settings.sql": {Data: mustRead(t, "internal/store/migrations/0004_drop_page_api_settings.sql")},
		"0005_business_name.sql":          {Data: mustRead(t, "internal/store/migrations/0005_business_name.sql")},
		"0006_server_settings.sql":        {Data: mustRead(t, "internal/store/migrations/0006_server_settings.sql")},
		"0007_login_failure_daily.sql":    {Data: mustRead(t, "internal/store/migrations/0007_login_failure_daily.sql")},
		"0008_session_indexes.sql":        {Data: mustRead(t, "internal/store/migrations/0008_session_indexes.sql")},
		"0009_bad.sql":                    {Data: []byte("CREATE TABLE half (id INTEGER); INSERT INTO nope VALUES (1);")},
	}
	_, err := harness.TryStart(harness.Options{DataDir: s.DataDir, SecretKey: s.SecretKey, Migrations: fsys})
	if err == nil {
		t.Fatal("startup must abort on failing migration")
	}
	if n := scalar[int](t, db, `SELECT count(*) FROM schema_migrations`); n != wantMigrations {
		t.Fatalf("version moved: %d", n)
	}
	if n := scalar[int](t, db, `SELECT count(*) FROM sqlite_master WHERE name='half'`); n != 0 {
		t.Fatal("partial migration survived")
	}

	// checksum mismatch: tamper with the stored checksum
	if _, err := db.Exec(`UPDATE schema_migrations SET checksum='deadbeef' WHERE version=1`); err != nil {
		t.Fatal(err)
	}
	_, err = harness.TryStart(harness.Options{DataDir: s.DataDir, SecretKey: s.SecretKey})
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("want checksum refusal, got %v", err)
	}
}

func mustRead(t *testing.T, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(harness.ModuleRoot(), rel))
	if err != nil {
		t.Fatal(err)
	}
	return b
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
	in := seedOldDB(t, old)
	db := harness.DB(t, in.DataDir)
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

	s2 := startUpgraded(t, in)
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
	if scalar[string](t, db, `SELECT business_name FROM settings`) != "Kept Cafe" || scalar[string](t, db, `SELECT timezone FROM settings`) != "Asia/Tehran" ||
		scalar[int](t, db, `SELECT coa_port FROM settings`) != 3800 || scalar[string](t, db, `SELECT voucher_help FROM settings`) != "help text" ||
		scalar[string](t, db, `SELECT radius_shared_secret FROM settings`) != secret || scalar[int](t, db, `SELECT count(*) FROM admin`) != 1 {
		t.Error("existing data did not survive the migration")
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
			"HSP_RADIUS_SHARED_SECRET=EnvSeededSecret77",
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
		if !strings.Contains(so.String(), "password: admin") || strings.Contains(logs, "password: admin") || strings.Contains(logs, "EnvSeededSecret77") {
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
