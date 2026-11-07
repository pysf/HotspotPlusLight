package e2e

import (
	"database/sql"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/PuerkitoBio/goquery"
	"layeh.com/radius"

	"hotspotpluslight/e2e/fakenas"
	"hotspotpluslight/e2e/harness"
)

// setBusinessName saves the settings form with a new business name.
func setBusinessName(t *testing.T, c *harness.AdminClient, name string) harness.Resp {
	t.Helper()
	d := c.Get("/admin/settings").Doc(t)
	f := harness.FormValues(t, d, "form#settings-form")
	f.Set("business_name", name)
	return c.Send("PUT", "/admin/settings", f)
}

// printedVoucher returns the single-voucher print page of a new member.
func printedVoucher(t *testing.T, c *harness.AdminClient) string {
	t.Helper()
	plan := c.CreatePlan(harness.PlanForm("Basic"))
	id := c.CreateMember(harness.MemberForm(plan, "alice"))
	r := c.Get(fmt.Sprintf("/admin/members/%d/voucher", id))
	if r.StatusCode != 200 {
		t.Fatalf("voucher page: %d", r.StatusCode)
	}
	return r.Body
}

// E14.01 (Phase 14) a new install has an empty business name, and nothing shows it: not the voucher, the header or the tab title.
func TestE14_01_EmptyShowsNowhere(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	db := harness.DB(t, s.DataDir)
	if v := scalar[string](t, db, `SELECT business_name FROM settings`); v != "" {
		t.Fatalf("default business name %q, want empty", v)
	}
	c := harness.LoginAsAdmin(t, s)
	d := c.Get("/admin/members").Doc(t)
	if title := d.Find("title").Text(); title != "Members - HotspotPlus Lite" {
		t.Errorf("tab title %q", title)
	}
	if d.Find(".l-brand__name").Length() != 0 {
		t.Error("brand name element is present")
	}
	set := c.Get("/admin/settings").Doc(t)
	if lbl := strings.TrimSpace(set.Find(`label[for="business_name"]`).Text()); lbl != "Business name" {
		t.Errorf("settings label %q", lbl)
	}
	if !strings.Contains(set.Text(), "Shown on printed vouchers and in the admin header. Leave empty to hide.") {
		t.Error("settings hint is missing")
	}
	v := harness.Resp{Body: printedVoucher(t, c)}.Doc(t)
	if v.Find("[data-voucher]").Length() != 1 || v.Find("[data-business]").Length() != 0 || strings.Contains(v.Text(), "Wi-Fi ·") {
		t.Error("voucher shows a business line")
	}
}

// E14.02 (Phase 14) a set business name shows on every voucher, in the admin header and in the tab title, HTML-escaped.
func TestE14_02_SetShowsEverywhere(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	c := harness.LoginAsAdmin(t, s)
	const name = `Hotel <Aria> & Spa`
	if r := setBusinessName(t, c, name); r.StatusCode != 200 || !strings.Contains(r.Body, "Settings saved") {
		t.Fatalf("save: %d %s", r.StatusCode, r.Body)
	}
	r := c.Get("/admin/members")
	d := r.Doc(t)
	if title := d.Find("title").Text(); title != "Members - "+name+" · HotspotPlus Lite" {
		t.Errorf("tab title %q", title)
	}
	if got := d.Find(".l-brand__name").Text(); got != name {
		t.Errorf("header name %q", got)
	}
	if strings.Contains(r.Body, "<Aria>") || !strings.Contains(r.Body, "Hotel &lt;Aria&gt; &amp; Spa") {
		t.Error("business name is not HTML-escaped in the page")
	}

	plan := c.CreatePlan(harness.PlanForm("Basic"))
	m1 := c.CreateMember(harness.MemberForm(plan, "alice"))
	m2 := c.CreateMember(harness.MemberForm(plan, "bob"))
	for _, id := range []int64{m1, m2} {
		pr := c.Get(fmt.Sprintf("/admin/members/%d/voucher", id))
		pd := pr.Doc(t)
		if got := strings.TrimSpace(pd.Find("[data-voucher] [data-business]").Text()); got != name+" · Wi-Fi" {
			t.Errorf("voucher line %q", got)
		}
		if strings.Contains(pr.Body, "<Aria>") {
			t.Error("business name is not HTML-escaped on the voucher")
		}
	}

	// Every voucher of a batch gets the line, and 14 still fit on a sheet.
	form := batchForm(plan, 15)
	if br := c.Send("POST", "/admin/batches", form); br.StatusCode != 201 {
		t.Fatalf("batch: %d %s", br.StatusCode, br.Body)
	} else {
		loc := br.Header.Get("Location")
		pd := c.Get(loc).Doc(t)
		if n, b := pd.Find("[data-voucher]").Length(), pd.Find("[data-voucher] [data-business]").Length(); n != 15 || b != 15 {
			t.Errorf("%d vouchers, %d with the line", n, b)
		}
		if pd.Find("[data-page]").Length() != 2 || pd.Find("[data-page]").First().Find("[data-voucher]").Length() != 14 {
			t.Error("a sheet no longer holds 14 vouchers")
		}
	}

	// Clearing it hides it again.
	if r := setBusinessName(t, c, ""); r.StatusCode != 200 {
		t.Fatalf("clear: %d", r.StatusCode)
	}
	if c.Get("/admin/members").Doc(t).Find(".l-brand__name").Length() != 0 {
		t.Error("header still shows the name")
	}
	if strings.Contains(c.Get(fmt.Sprintf("/admin/members/%d/voucher", m1)).Body, "data-business") {
		t.Error("voucher still shows the name")
	}

	// A name over 100 characters is refused.
	if r := setBusinessName(t, c, strings.Repeat("x", 101)); r.StatusCode != 422 {
		t.Errorf("long name: %d", r.StatusCode)
	}
}

// E14.03 (Phase 14) migration 0005 renames site_title to business_name, clears the old default, and keeps a real name and the other settings.
func TestE14_03_Migration0005(t *testing.T) {
	root := harness.ModuleRoot()
	read := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join(root, "internal/store/migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	old := fstest.MapFS{}
	for _, n := range []string{"0001_init.sql", "0002_usage_daily.sql", "0003_admin_must_change.sql", "0004_drop_page_api_settings.sql"} {
		old[n] = &fstest.MapFile{Data: read(n)}
	}
	for _, c := range []struct{ before, after string }{{"HotspotPlus", ""}, {"Hotel Aria", "Hotel Aria"}} {
		in := seedOldDB(t, old)
		db := harness.DB(t, in.DataDir)
		if scalar[int](t, db, `SELECT count(*) FROM pragma_table_info('settings') WHERE name='site_title'`) != 1 {
			t.Fatal("before 0005 the column site_title should exist")
		}
		if _, err := db.Exec(`UPDATE settings SET site_title=?, timezone='Asia/Tehran', coa_port=3800, voucher_help='help text'`, c.before); err != nil {
			t.Fatal(err)
		}
		secret := scalar[string](t, db, `SELECT radius_shared_secret FROM settings`)

		s2 := startUpgraded(t, in)
		if n := scalar[int](t, db, `SELECT count(*) FROM schema_migrations`); n != wantMigrations {
			t.Errorf("%d migrations, want %d", n, wantMigrations)
		}
		if scalar[int](t, db, `SELECT count(*) FROM pragma_table_info('settings') WHERE name='site_title'`) != 0 {
			t.Error("site_title still exists")
		}
		if got := scalar[string](t, db, `SELECT business_name FROM settings`); got != c.after {
			t.Errorf("from %q: business_name %q, want %q", c.before, got, c.after)
		}
		if scalar[string](t, db, `SELECT timezone FROM settings`) != "Asia/Tehran" || scalar[int](t, db, `SELECT coa_port FROM settings`) != 3800 ||
			scalar[string](t, db, `SELECT voucher_help FROM settings`) != "help text" || scalar[string](t, db, `SELECT radius_shared_secret FROM settings`) != secret ||
			scalar[int](t, db, `SELECT count(*) FROM admin`) != 1 {
			t.Error("other settings did not survive the migration")
		}
		// The rebuilt table keeps its constraints (foreign keys are on, as in the app).
		if _, err := db.Exec(`INSERT INTO settings(id, radius_shared_secret, updated_at) VALUES (2, 'x', 0)`); err == nil {
			t.Error("a second settings row was accepted")
		}
		if _, err := db.Exec(`UPDATE settings SET accounting_interval_seconds=5`); err == nil {
			t.Error("accounting_interval_seconds=5 was accepted")
		}
		res, err := db.Exec(`INSERT INTO plan(name, duration_days, speed_value, speed_unit, created_at, updated_at) VALUES ('P', 30, 0, 'Mbps', 0, 0)`)
		if err != nil {
			t.Fatal(err)
		}
		pid, _ := res.LastInsertId()
		if _, err := db.Exec(`UPDATE settings SET default_plan_id=?`, pid); err != nil {
			t.Fatalf("default_plan_id rejects a real plan: %v", err)
		}
		if _, err := db.Exec(`DELETE FROM plan WHERE id=?`, pid); err != nil {
			t.Fatal(err)
		}
		if scalar[int](t, db, `SELECT count(*) FROM settings WHERE default_plan_id IS NULL`) != 1 {
			t.Error("deleting the default plan did not set default_plan_id to NULL")
		}
		s2.Stop()
	}
}

// E1.10 settings: all columns, validation, persistence, masked secret (PLAN 6, 8.2, O8).
func TestE1_10_Settings(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	c := harness.LoginAsAdmin(t, s)
	db := harness.DB(t, s.DataDir)
	plan := c.CreatePlan(harness.PlanForm("Default one"))

	page := c.Get("/admin/settings")
	if page.StatusCode != 200 {
		t.Fatalf("page: %d", page.StatusCode)
	}
	d := page.Doc(t)
	form := harness.FormValues(t, d, "form#settings-form")
	for _, f := range []string{"business_name", "timezone", "radius_shared_secret", "coa_port", "accounting_interval_seconds",
		"default_plan_id", "batch_username_prefix", "batch_counter", "batch_max_count",
		"voucher_password_length", "voucher_help", "session_retention_days"} {
		if d.Find(`form#settings-form [name="`+f+`"]`).Length() != 1 {
			t.Errorf("settings form lacks %s", f)
		}
	}
	if d.Find(`input[name="radius_shared_secret"][type="password"]`).Length() != 1 || d.Find(`#reveal-secret`).Length() != 1 {
		t.Error("secret must be masked with a reveal button")
	}
	if v := form.Get("coa_port"); v != "3799" {
		t.Errorf("default coa_port shown %q", v)
	}
	if d.Find(`select[name="default_plan_id"] option`).Length() != 2 { // none + the one plan
		t.Errorf("default plan select options: %d", d.Find(`select[name="default_plan_id"] option`).Length())
	}
	for _, gone := range []string{"mac_auto_login", "after_login_url", "public_base_url"} {
		if d.Find(`form#settings-form [name="`+gone+`"]`).Length() != 0 {
			t.Errorf("settings form still has %s (removed in Phase 12a)", gone)
		}
	}

	// Invalid values: 422, message next to the field, nothing stored.
	invalid := []struct{ field, val string }{
		{"business_name", strings.Repeat("x", 101)}, {"timezone", "Mars/Base"}, {"radius_shared_secret", ""},
		{"coa_port", "0"}, {"coa_port", "65536"}, {"coa_port", "abc"},
		{"accounting_interval_seconds", "9"}, {"accounting_interval_seconds", "x"},
		{"default_plan_id", "9999"}, {"batch_username_prefix", "wi-fi"}, {"batch_counter", "-1"}, {"batch_max_count", "0"},
		{"voucher_password_length", "0"}, {"voucher_password_length", "x"}, {"session_retention_days", "-1"},
	}
	before := scalar[string](t, db, `SELECT business_name||coa_port||accounting_interval_seconds||radius_shared_secret FROM settings`)
	for _, iv := range invalid {
		f := harness.FormValues(t, d, "form#settings-form")
		f.Set(iv.field, iv.val)
		r := c.Send("PUT", "/admin/settings", f)
		if r.StatusCode != 422 || fieldError(r, t, iv.field) == "" {
			t.Errorf("%s=%q: status %d err %q", iv.field, iv.val, r.StatusCode, fieldError(r, t, iv.field))
		}
	}
	if after := scalar[string](t, db, `SELECT business_name||coa_port||accounting_interval_seconds||radius_shared_secret FROM settings`); after != before {
		t.Fatal("invalid settings were stored")
	}

	// Valid update persists every column.
	f := harness.FormValues(t, d, "form#settings-form")
	for k, v := range map[string]string{
		"business_name": "My Cafe", "timezone": "Asia/Tehran", "radius_shared_secret": "s3cretvalue", "coa_port": "3800",
		"accounting_interval_seconds": "120", "default_plan_id": fmt.Sprint(plan),
		"batch_username_prefix": "cafe", "batch_counter": "2000",
		"batch_max_count": "50", "voucher_password_length": "8", "voucher_help": "Connect to the WiFi and sign in.",
		"session_retention_days": "0",
	} {
		f.Set(k, v)
	}
	r := c.Send("PUT", "/admin/settings", f)
	if r.StatusCode != 200 || !strings.Contains(r.Body, "Settings saved") {
		t.Fatalf("save: %d %s", r.StatusCode, r.Body)
	}
	row := db.QueryRow(`SELECT business_name,timezone,radius_shared_secret,coa_port,accounting_interval_seconds,default_plan_id,batch_username_prefix,batch_counter,batch_max_count,voucher_password_length,voucher_help,session_retention_days FROM settings`)
	var (
		title, tz, sec, prefix, help               string
		coa, interval, counter, maxc, vlen, retain int
		def                                        sql.NullInt64
	)
	if err := row.Scan(&title, &tz, &sec, &coa, &interval, &def, &prefix, &counter, &maxc, &vlen, &help, &retain); err != nil {
		t.Fatal(err)
	}
	if title != "My Cafe" || tz != "Asia/Tehran" || sec != "s3cretvalue" || coa != 3800 || interval != 120 || !def.Valid || def.Int64 != plan ||
		prefix != "cafe" || counter != 2000 || maxc != 50 || vlen != 8 ||
		help != "Connect to the WiFi and sign in." || retain != 0 {
		t.Fatalf("stored: %v", []any{title, tz, sec, coa, interval, def, prefix, counter, maxc, vlen, help, retain})
	}
	// Reload shows what was saved; "" clears the default plan.
	d2 := c.Get("/admin/settings").Doc(t)
	f2 := harness.FormValues(t, d2, "form#settings-form")
	if f2.Get("business_name") != "My Cafe" || f2.Get("default_plan_id") != fmt.Sprint(plan) {
		t.Fatalf("reloaded form: %v", f2)
	}
	f2.Set("default_plan_id", "")
	if r := c.Send("PUT", "/admin/settings", f2); r.StatusCode != 200 {
		t.Fatalf("second save: %d", r.StatusCode)
	}
	if scalar[sql.NullInt64](t, db, `SELECT default_plan_id FROM settings`).Valid {
		t.Fatal("default plan not cleared")
	}
}

// E1.11 a changed shared secret is used by the RADIUS listener without restart (PLAN 7.1).
func TestE1_11_SharedSecretAppliesWithoutRestart(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	c := harness.LoginAsAdmin(t, s)
	db := harness.DB(t, s.DataDir)
	oldSecret := scalar[string](t, db, `SELECT radius_shared_secret FROM settings`)
	if r, err := fakenas.New(s.Addrs.RadiusAuth, oldSecret).Authenticate("u", "p"); err != nil || r.Code != radius.CodeAccessReject {
		t.Fatalf("before change: %v %v", r, err)
	}
	const newSecret = "ACompletelyNewSharedSecret42"
	f := harness.FormValues(t, c.Get("/admin/settings").Doc(t), "form#settings-form")
	f.Set("radius_shared_secret", newSecret)
	if r := c.Send("PUT", "/admin/settings", f); r.StatusCode != 200 {
		t.Fatalf("save: %d", r.StatusCode)
	}
	nas := fakenas.New(s.Addrs.RadiusAuth, newSecret)
	if r, err := nas.Authenticate("u", "p"); err != nil || r.Code != radius.CodeAccessReject {
		t.Fatalf("new secret: %v %v", r, err)
	}
	stale := fakenas.New(s.Addrs.RadiusAuth, oldSecret)
	stale.Timeout = 1500 * time.Millisecond
	if r, err := stale.Authenticate("u", "p"); err == nil {
		t.Fatalf("old secret still verifies replies: %v", r.Code)
	}
}

// E16.01 the settings form shows server config fields; invalid addresses/log values are rejected; valid values persist.
func TestE16_01_ServerSettingsForm(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	c := harness.LoginAsAdmin(t, s)
	db := harness.DB(t, s.DataDir)

	d := c.Get("/admin/settings").Doc(t)
	for _, f := range []string{"admin_addr", "radius_auth_addr", "radius_acct_addr", "log_level", "log_format"} {
		if d.Find(`form#settings-form [name="`+f+`"]`).Length() != 1 {
			t.Errorf("settings form lacks %s", f)
		}
	}
	// The harness seeds the addresses from env on first boot, and the form shows what is stored.
	form := harness.FormValues(t, d, "form#settings-form")
	if v := form.Get("admin_addr"); v != "127.0.0.1:0" {
		t.Errorf("seeded admin_addr %q", v)
	}
	if v := form.Get("log_level"); v != "info" {
		t.Errorf("default log_level %q", v)
	}

	// Invalid values: bad address format, bad log level.
	for _, iv := range []struct{ field, val string }{
		{"admin_addr", "8081"},          // missing colon
		{"radius_auth_addr", ""},        // empty
		{"radius_acct_addr", ":999999"}, // port out of range
		{"log_level", "verbose"},        // unknown level
		{"log_format", "yaml"},          // unknown format
	} {
		f := harness.FormValues(t, d, "form#settings-form")
		f.Set(iv.field, iv.val)
		r := c.Send("PUT", "/admin/settings", f)
		if r.StatusCode != 422 || fieldError(r, t, iv.field) == "" {
			t.Errorf("%s=%q: status %d err %q", iv.field, iv.val, r.StatusCode, fieldError(r, t, iv.field))
		}
	}

	// Two listeners on the same port are rejected.
	dup := harness.FormValues(t, d, "form#settings-form")
	dup.Set("admin_addr", ":9090")
	dup.Set("radius_auth_addr", ":9090")
	if r := c.Send("PUT", "/admin/settings", dup); r.StatusCode != 422 || (fieldError(r, t, "admin_addr") == "" && fieldError(r, t, "radius_auth_addr") == "") {
		t.Errorf("duplicate ports: status %d", r.StatusCode)
	}

	// A PUT without the server fields keeps the stored values.
	before := scalar[string](t, db, `SELECT admin_addr||'|'||radius_auth_addr||'|'||radius_acct_addr||'|'||log_level||'|'||log_format FROM settings`)
	partial := harness.FormValues(t, d, "form#settings-form")
	for _, k := range []string{"admin_addr", "radius_auth_addr", "radius_acct_addr", "log_level", "log_format"} {
		partial.Del(k)
	}
	if r := c.Send("PUT", "/admin/settings", partial); r.StatusCode != 200 {
		t.Fatalf("partial save: %d", r.StatusCode)
	}
	if after := scalar[string](t, db, `SELECT admin_addr||'|'||radius_auth_addr||'|'||radius_acct_addr||'|'||log_level||'|'||log_format FROM settings`); after != before {
		t.Errorf("server settings changed: %q -> %q", before, after)
	}

	// Valid update: save new values and verify they persist.
	f := harness.FormValues(t, d, "form#settings-form")
	f.Set("admin_addr", ":9090")
	f.Set("radius_auth_addr", ":1812")
	f.Set("radius_acct_addr", ":1813")
	f.Set("log_level", "debug")
	f.Set("log_format", "json")
	if r := c.Send("PUT", "/admin/settings", f); r.StatusCode != 200 || !strings.Contains(r.Body, "Settings saved") {
		t.Fatalf("save: %d %s", r.StatusCode, r.Body)
	}
	if v := scalar[string](t, db, `SELECT admin_addr FROM settings`); v != ":9090" {
		t.Errorf("admin_addr %q", v)
	}
	if v := scalar[string](t, db, `SELECT log_level FROM settings`); v != "debug" {
		t.Errorf("log_level %q", v)
	}
}

// E16.02 after the first boot the settings row wins over HSP_* env vars for the server settings.
func TestE16_02_DatabaseWinsOverEnvAfterFirstBoot(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	s.Stop()
	s2 := harness.Start(t, harness.Options{DataDir: s.DataDir, SecretKey: s.SecretKey,
		Env: map[string]string{"ADMIN_ADDR": "127.0.0.1:1", "LOG_LEVEL": "debug", "LOG_FORMAT": "json"}})
	db := harness.DB(t, s.DataDir)
	if v := scalar[string](t, db, `SELECT admin_addr FROM settings`); v != "127.0.0.1:0" {
		t.Errorf("admin_addr %q changed by env", v)
	}
	if v := scalar[string](t, db, `SELECT log_level FROM settings`); v != "info" {
		t.Errorf("log_level %q changed by env", v)
	}
	// Port 1 would fail to bind or be fixed; the DB address picks a free port instead.
	if strings.HasSuffix(s2.Addrs.Admin, ":1") {
		t.Errorf("admin bound to the env address %s", s2.Addrs.Admin)
	}
}

// E16.03 the first boot stores the HSP_* server settings from env in the settings row.
func TestE16_03_FirstBootSeedsServerSettingsFromEnv(t *testing.T) {
	s := harness.Start(t, harness.Options{Env: map[string]string{
		"ADMIN_ADDR": "127.0.0.1:0", "LOG_LEVEL": "debug", "LOG_FORMAT": "json"}})
	db := harness.DB(t, s.DataDir)
	if v := scalar[string](t, db, `SELECT admin_addr FROM settings`); v != "127.0.0.1:0" {
		t.Errorf("admin_addr %q", v)
	}
	if v := scalar[string](t, db, `SELECT log_level FROM settings`); v != "debug" {
		t.Errorf("log_level %q", v)
	}
	if v := scalar[string](t, db, `SELECT log_format FROM settings`); v != "json" {
		t.Errorf("log_format %q", v)
	}
}

// E16.04 the settings page shows a restart notice only when the saved server settings differ from the running ones.
func TestE16_04_RestartNotice(t *testing.T) {
	const notice = "Restart the server to apply"
	s := harness.Start(t, harness.Options{})
	c := harness.LoginAsAdmin(t, s)

	d := c.Get("/admin/settings")
	if strings.Contains(d.Body, notice) {
		t.Fatal("restart notice shown before any change")
	}
	f := harness.FormValues(t, d.Doc(t), "form#settings-form")
	f.Set("log_level", "debug")
	r := c.Send("PUT", "/admin/settings", f)
	if r.StatusCode != 200 || !strings.Contains(r.Body, "Settings saved") || !strings.Contains(r.Body, notice) {
		t.Fatalf("save: %d %s", r.StatusCode, r.Body)
	}
	if !strings.Contains(c.Get("/admin/settings").Body, notice) {
		t.Error("restart notice missing on the page after a saved change")
	}
	// Saving the running value again clears the notice.
	f.Set("log_level", "info")
	if r := c.Send("PUT", "/admin/settings", f); r.StatusCode != 200 || strings.Contains(r.Body, notice) {
		t.Errorf("notice still shown after restoring the running value: %d", r.StatusCode)
	}
}

// E16.05 the time zone is a dropdown with the current value selected; a stored zone that is not in the list still appears.
func TestE16_05_TimezoneIsADropdown(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	c := harness.LoginAsAdmin(t, s)
	db := harness.DB(t, s.DataDir)

	d := c.Get("/admin/settings").Doc(t)
	if d.Find(`form#settings-form input[name="timezone"]`).Length() != 0 || d.Find(`form#settings-form select[name="timezone"]`).Length() != 1 {
		t.Fatal("time zone must be a select, not a text input")
	}
	sel := d.Find(`select[name="timezone"]`)
	for _, want := range []string{"UTC", "Asia/Tehran", "Europe/London"} {
		if sel.Find(`option[value="`+want+`"]`).Length() != 1 {
			t.Errorf("option %s missing", want)
		}
	}
	if v := sel.Find("option[selected]").AttrOr("value", ""); v != "UTC" {
		t.Errorf("selected %q, want the stored UTC", v)
	}

	// Choosing another listed zone saves it, and the page selects it.
	f := harness.FormValues(t, d, "form#settings-form")
	f.Set("timezone", "Asia/Tehran")
	if r := c.Send("PUT", "/admin/settings", f); r.StatusCode != 200 {
		t.Fatalf("save: %d", r.StatusCode)
	}
	if v := c.Get("/admin/settings").Doc(t).Find(`select[name="timezone"] option[selected]`).AttrOr("value", ""); v != "Asia/Tehran" {
		t.Errorf("selected %q after save", v)
	}

	// A valid IANA zone outside the list (set directly in the database) stays visible and selected, and can be saved again.
	const odd = "Pacific/Chatham"
	if _, err := db.Exec(`UPDATE settings SET timezone=?`, odd); err != nil {
		t.Fatal(err)
	}
	d = c.Get("/admin/settings").Doc(t)
	if v := d.Find(`select[name="timezone"] option[selected]`).AttrOr("value", ""); v != odd {
		t.Fatalf("selected %q, want the stored %s", v, odd)
	}
	f = harness.FormValues(t, d, "form#settings-form")
	if f.Get("timezone") != odd {
		t.Fatalf("form posts %q", f.Get("timezone"))
	}
	if r := c.Send("PUT", "/admin/settings", f); r.StatusCode != 200 {
		t.Fatalf("saving the odd zone again: %d", r.StatusCode)
	}
	if scalar[string](t, db, `SELECT timezone FROM settings`) != odd {
		t.Error("the stored zone changed")
	}

	// A name that is not an IANA zone is still refused.
	f.Set("timezone", "Mars/Base")
	if r := c.Send("PUT", "/admin/settings", f); r.StatusCode != 422 || fieldError(r, t, "timezone") == "" {
		t.Errorf("Mars/Base: %d", r.StatusCode)
	}
}

// E16.06 the RADIUS secret takes letters and digits only. A space or symbol gets a 422 on the field and nothing is
// stored; letters and digits save. A stored older secret with symbols can be kept while other settings change.
func TestE16_06_SecretLettersAndDigitsOnly(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	c := harness.LoginAsAdmin(t, s)
	db := harness.DB(t, s.DataDir)
	d := c.Get("/admin/settings").Doc(t)

	generated := scalar[string](t, db, `SELECT radius_shared_secret FROM settings`)
	if len(generated) != 24 || strings.Trim(generated, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789") != "" {
		t.Errorf("the first-start secret %q must be 24 letters and digits", generated)
	}
	for _, bad := range []string{"with space", "with-dash", "p@ssw0rd!", "caf\u00e9", ""} {
		f := harness.FormValues(t, d, "form#settings-form")
		f.Set("radius_shared_secret", bad)
		r := c.Send("PUT", "/admin/settings", f)
		if r.StatusCode != 422 || fieldError(r, t, "radius_shared_secret") == "" {
			t.Errorf("%q: status %d err %q", bad, r.StatusCode, fieldError(r, t, "radius_shared_secret"))
		}
	}
	if scalar[string](t, db, `SELECT radius_shared_secret FROM settings`) != generated {
		t.Fatal("a refused secret was stored")
	}
	f := harness.FormValues(t, d, "form#settings-form")
	f.Set("radius_shared_secret", "LongRandomSecret0123456789")
	if r := c.Send("PUT", "/admin/settings", f); r.StatusCode != 200 {
		t.Fatalf("letters and digits: %d", r.StatusCode)
	}
	if scalar[string](t, db, `SELECT radius_shared_secret FROM settings`) != "LongRandomSecret0123456789" {
		t.Error("the secret was not saved")
	}

	// An older database may hold a secret with symbols. Other settings can still be saved while the form posts it unchanged.
	const old = `old s3cret-"$`
	if _, err := db.Exec(`UPDATE settings SET radius_shared_secret=?`, old); err != nil {
		t.Fatal(err)
	}
	d = c.Get("/admin/settings").Doc(t)
	f = harness.FormValues(t, d, "form#settings-form")
	f.Set("coa_port", "3801")
	if r := c.Send("PUT", "/admin/settings", f); r.StatusCode != 200 {
		t.Fatalf("saving with the stored old secret: %d %s", r.StatusCode, r.Body)
	}
	if scalar[string](t, db, `SELECT radius_shared_secret FROM settings`) != old || scalar[int](t, db, `SELECT coa_port FROM settings`) != 3801 {
		t.Error("the old secret or the other setting is wrong after the save")
	}
	f.Set("radius_shared_secret", old+"x")
	if r := c.Send("PUT", "/admin/settings", f); r.StatusCode != 422 {
		t.Errorf("a changed secret with symbols: %d", r.StatusCode)
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

// E8.10 the shared secret is letters and digits only (owner, 2026-11-07, replacing the older "kept as typed" rule).
// Control characters stay refused: a newline would end the quoted string in the pasted RouterOS script and turn the
// rest into another command. Spaces and symbols are refused too. Nothing is stored for a refused value.
func TestE8_10_SecretLettersAndDigitsOnly(t *testing.T) {
	f := newP8(t)
	d := f.c.Get("/admin/settings").Doc(t)
	before := scalar[string](t, f.db, `SELECT radius_shared_secret FROM settings`)
	for _, bad := range []string{"abc\ndef", "abc\r\n/system reset-configuration", "ab\tc", "ab\x7fc", ` sp ace"$\? `, "ab-cd", "ab cd"} {
		form := harness.FormValues(t, d, "form#settings-form")
		form.Set("radius_shared_secret", bad)
		r := f.c.Send("PUT", "/admin/settings", form)
		if r.StatusCode != 422 || fieldError(r, t, "radius_shared_secret") == "" {
			t.Errorf("secret %q: %d", bad, r.StatusCode)
		}
	}
	if got := scalar[string](t, f.db, `SELECT radius_shared_secret FROM settings`); got != before {
		t.Errorf("a refused secret was stored: %q", got)
	}
	form := harness.FormValues(t, d, "form#settings-form")
	form.Set("radius_shared_secret", "Abc123xyz")
	if r := f.c.Send("PUT", "/admin/settings", form); r.StatusCode != 200 {
		t.Fatalf("allowed secret: %d %s", r.StatusCode, r.Body)
	}
	if got := scalar[string](t, f.db, `SELECT radius_shared_secret FROM settings`); got != "Abc123xyz" {
		t.Errorf("stored %q, want exactly as typed", got)
	}
}
