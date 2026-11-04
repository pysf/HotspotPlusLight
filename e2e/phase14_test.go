package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

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
		s := harness.Start(t, harness.Options{Migrations: old})
		db := harness.DB(t, s.DataDir)
		if scalar[int](t, db, `SELECT count(*) FROM pragma_table_info('settings') WHERE name='site_title'`) != 1 {
			t.Fatal("before 0005 the column site_title should exist")
		}
		if _, err := db.Exec(`UPDATE settings SET site_title=?, timezone='Asia/Tehran', coa_port=3800, voucher_help='help text'`, c.before); err != nil {
			t.Fatal(err)
		}
		secret := scalar[string](t, db, `SELECT radius_shared_secret FROM settings`)
		s.Stop()

		s2 := harness.Start(t, harness.Options{DataDir: s.DataDir, SecretKey: s.SecretKey})
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
