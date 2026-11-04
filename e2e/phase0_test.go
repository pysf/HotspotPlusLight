package e2e

import (
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"golang.org/x/crypto/bcrypt"
	"layeh.com/radius"
	"layeh.com/radius/rfc2865"

	"hotspotpluslight/e2e/fakenas"
	"hotspotpluslight/e2e/harness"
)

func scalar[T any](t *testing.T, db *sql.DB, q string, args ...any) T {
	t.Helper()
	var v T
	if err := db.QueryRow(q, args...).Scan(&v); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return v
}

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

// E0.05 missing HSP_SECRET_KEY refuses to start (real binary and app).
func TestE0_05_MissingSecretKeyRefusesToStart(t *testing.T) {
	dir := t.TempDir()
	_, stderr, code := runBinary(t, []string{"HSP_DATA_DIR=" + dir}, "serve")
	if code == 0 || !strings.Contains(stderr, "HSP_SECRET_KEY") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "hsp.db")); err == nil {
		t.Fatal("db created despite missing key")
	}
	if _, err := harness.TryStart(harness.Options{DataDir: t.TempDir(), NoKey: true}); err == nil {
		t.Fatal("app started without key")
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

// E0.07 (changed 2026-11-04, Phase 13a) the backup text and the systemd example moved from the README to the
// user docs site; their check moves to the site test in 13b. Here: no Dockerfile, and the README points to the site.
func TestE0_07_ReadmeLinksToSiteNoDockerfile(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(harness.ModuleRoot(), "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "https://pysf.github.io/HotspotPlusLight/") {
		t.Error("README does not link to the user docs site")
	}
	if _, err := os.Stat(filepath.Join(harness.ModuleRoot(), "Dockerfile")); err == nil {
		t.Error("Dockerfile must not exist")
	}
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

// wantMigrations is the number of shipped migrations (0001_init, 0002_usage_daily, 0003_admin_must_change, 0004_drop_page_api_settings).
const wantMigrations = 4

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
		"0005_bad.sql":                    {Data: []byte("CREATE TABLE half (id INTEGER); INSERT INTO nope VALUES (1);")},
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

// E0.10 layering (PLAN 9.1).
func TestE0_10_Layering(t *testing.T) {
	const mod = "hotspotpluslight/"
	deps := func(pkg string) (all, std map[string]bool) {
		cmd := exec.Command("go", "list", "-deps", "-f", "{{.ImportPath}} {{.Standard}}", pkg)
		cmd.Dir = harness.ModuleRoot()
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("go list %s: %v", pkg, err)
		}
		all, std = map[string]bool{}, map[string]bool{}
		for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			f := strings.Fields(l)
			all[f[0]] = true
			std[f[0]] = f[1] == "true"
		}
		return
	}
	all, std := deps(mod + "internal/domain")
	for p := range all {
		if !std[p] && p != mod+"internal/domain" {
			t.Errorf("internal/domain imports non-stdlib %s", p)
		}
	}
	forbid := func(pkg string, prefixes ...string) {
		all, _ := deps(mod + pkg)
		for p := range all {
			for _, pre := range prefixes {
				if strings.HasPrefix(p, mod+pre) {
					t.Errorf("%s must not (transitively) import %s", pkg, p)
				}
			}
		}
	}
	for _, svc := range []string{"auth", "member", "plan", "session", "settings", "voucher"} {
		forbid("internal/"+svc, "internal/web", "internal/radius", "internal/store")
	}
	forbid("internal/radius", "internal/store")
	forbid("internal/web/admin", "internal/store")
}
