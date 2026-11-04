package e2e

import (
	"database/sql"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"hotspotpluslight/e2e/harness"
)

// E10.20 plans: list region, row menu, delete from the list, a blocked delete shows a clear toast.
func TestE10_20_PlansListAndMenu(t *testing.T) {
	f := newP2(t)
	used := f.plan("Used")
	free := f.plan("Free")
	f.member(used, "alice")
	d := f.c.Get("/admin/plans").Doc(t)
	if list := d.Find("#plan-list"); list.Length() != 1 || list.AttrOr("aria-live", "") != "polite" || list.Find("tbody tr").Length() != 2 {
		t.Fatal("plans page lacks the list region")
	}
	menu := d.Find("tbody tr").Last().Find(".c-menu")
	var items []string
	menu.Find(".c-menu__item").Each(func(_ int, s *goquery.Selection) { items = append(items, text(s)) })
	if strings.Join(items, "|") != "Edit|Delete" {
		t.Errorf("plan menu %q", items)
	}
	if menu.Find(".c-menu__item").Last().AttrOr("data-danger", "x") == "x" || menu.Find("hr.c-menu__sep").Length() != 1 {
		t.Error("Delete must be danger after a divider")
	}
	del := d.Find(`.c-menu__item[hx-delete="/admin/plans/` + strconv.FormatInt(free, 10) + `"]`)
	if del.AttrOr("hx-confirm", "") == "" || del.AttrOr("hx-target", "") != "#plan-list" {
		t.Errorf("plan delete item: %v", del.Nodes)
	}
	edit := d.Find(`.c-menu__item[hx-get="/admin/plans/` + strconv.FormatInt(free, 10) + `"]`)
	if edit.AttrOr("hx-target", "") != "#sheet-body" {
		t.Errorf("plan edit item: %v", edit.Nodes)
	}
	// The fragment for the list region.
	frag := f.c.GetHX("/admin/plans", planTarget)
	if strings.Contains(frag.Body, "<html") || fragmentDoc(t, frag).Find("tbody tr").Length() != 2 {
		t.Error("plan list fragment")
	}
	if rf := fragmentDoc(t, frag).Find(`[hx-trigger*="plans-changed"]`); rf.Length() != 1 || rf.AttrOr("hx-get", "") != "/admin/plans" {
		t.Error("plan list does not refresh itself")
	}
	// Delete blocked: 409 with the clear message in a toast, nothing swapped.
	r := f.c.SendHX("DELETE", "/admin/plans/"+strconv.FormatInt(used, 10), nil, planTarget)
	ev := hxTrigger(t, r)
	if r.StatusCode != 409 || r.Header.Get("HX-Reswap") != "none" || ev["toast"] == nil || !strings.Contains(ev["toast"].(string), "used by 1 members and cannot be deleted") {
		t.Errorf("blocked delete: %d %v", r.StatusCode, r.Header)
	}
	// Delete works.
	r = f.c.SendHX("DELETE", "/admin/plans/"+strconv.FormatInt(free, 10), nil, planTarget)
	if ev := hxTrigger(t, r); r.StatusCode != 200 || ev["plans-changed"] == nil || ev["toast"] == nil || r.Header.Get("HX-Redirect") != "" {
		t.Errorf("delete: %d %v", r.StatusCode, r.Header)
	}
	if r := f.c.Send("DELETE", "/admin/plans/"+strconv.FormatInt(used, 10), nil); r.StatusCode != 409 || !strings.Contains(r.Body, "used by 1 members") {
		t.Errorf("blocked delete without target must keep its body: %d %s", r.StatusCode, r.Body)
	}
}

func insertMembers(t *testing.T, db *sql.DB, planID int64, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		_, err := db.Exec(`INSERT INTO member(username, password_enc, plan_id, subscription_date, created_at, updated_at)
			VALUES (?, x'00', ?, 1, 1, 1)`, fmt.Sprintf("user%d_%d", planID, i), planID)
		if err != nil {
			t.Fatal(err)
		}
	}
}

// E1.07 plan create/edit validation (BR-4, BR-5, DDL).
func TestE1_07_PlanValidation(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	c := harness.LoginAsAdmin(t, s)
	db := harness.DB(t, s.DataDir)

	cases := []struct {
		desc, field string
		form        url.Values
	}{
		{"empty name", "name", harness.PlanForm("", "x", "")},
		{"blank name", "name", harness.PlanForm("   ")},
		{"duration 0", "duration_days", harness.PlanForm("P", "duration_days", "0")},
		{"duration negative", "duration_days", harness.PlanForm("P", "duration_days", "-1")},
		{"duration empty", "duration_days", harness.PlanForm("P", "duration_days", "")},
		{"duration decimal", "duration_days", harness.PlanForm("P", "duration_days", "1.5")},
		{"duration text", "duration_days", harness.PlanForm("P", "duration_days", "abc")},
		{"max_sessions 0", "max_sessions", harness.PlanForm("P", "max_sessions", "0")},
		{"max_sessions decimal", "max_sessions", harness.PlanForm("P", "max_sessions", "1.5")},
		{"max_sessions negative", "max_sessions", harness.PlanForm("P", "max_sessions", "-2")},
		{"max_sessions empty", "max_sessions", harness.PlanForm("P", "max_sessions", "")},
		{"speed negative", "speed_value", harness.PlanForm("P", "speed_value", "-1")},
		{"speed text", "speed_value", harness.PlanForm("P", "speed_value", "fast")},
		{"speed unit", "speed_unit", harness.PlanForm("P", "speed_unit", "Tbps")},
		{"bulk negative", "bulk_value", harness.PlanForm("P", "bulk_value", "-0.5")},
		{"bulk unit", "bulk_unit", harness.PlanForm("P", "bulk_unit", "TB")},
		{"time negative", "time_limit_minutes", harness.PlanForm("P", "time_limit_minutes", "-5")},
		{"time decimal", "time_limit_minutes", harness.PlanForm("P", "time_limit_minutes", "2.5")},
	}
	for _, tc := range cases {
		r := c.Send("POST", "/admin/plans", tc.form)
		if r.StatusCode != 422 {
			t.Errorf("%s: status %d", tc.desc, r.StatusCode)
			continue
		}
		if fieldError(r, t, tc.field) == "" {
			t.Errorf("%s: no message for %s", tc.desc, tc.field)
		}
		if r.Doc(t).Find(`form input[name="name"]`).Length() != 1 {
			t.Errorf("%s: form not re-rendered", tc.desc)
		}
	}
	if n := scalar[int](t, db, `SELECT count(*) FROM plan`); n != 0 {
		t.Fatalf("invalid plans stored: %d", n)
	}
	// Re-rendered form keeps what was typed.
	r := c.Send("POST", "/admin/plans", harness.PlanForm("Keep", "duration_days", "0"))
	if v, _ := r.Doc(t).Find(`input[name="name"]`).Attr("value"); v != "Keep" {
		t.Errorf("name not kept: %q", v)
	}

	// All unit spellings are accepted; 0 = unlimited / no limit.
	for i, u := range [][2]string{{"Kbps", "KB"}, {"Mbps", "MB"}, {"Gbps", "GB"}} {
		c.CreatePlan(harness.PlanForm(fmt.Sprintf("U%d", i), "speed_unit", u[0], "bulk_unit", u[1]))
	}
	c.CreatePlan(harness.PlanForm("Unlimited", "speed_value", "0", "bulk_value", "0", "time_limit_minutes", "0"))

	// Name is unique, case-insensitive; on create and on edit.
	gold := c.CreatePlan(harness.PlanForm("Gold"))
	if r := c.Send("POST", "/admin/plans", harness.PlanForm("gOLD")); r.StatusCode != 422 || fieldError(r, t, "name") == "" {
		t.Errorf("duplicate create: %d", r.StatusCode)
	}
	silver := c.CreatePlan(harness.PlanForm("Silver"))
	if r := c.Send("PUT", fmt.Sprintf("/admin/plans/%d", silver), harness.PlanForm("GOLD")); r.StatusCode != 422 || fieldError(r, t, "name") == "" {
		t.Errorf("duplicate edit: %d", r.StatusCode)
	}
	// Saving a plan under its own name (even different case) is fine.
	if r := c.Send("PUT", fmt.Sprintf("/admin/plans/%d", gold), harness.PlanForm("gold", "duration_days", "60")); r.StatusCode != 200 {
		t.Errorf("edit own name: %d %s", r.StatusCode, r.Body)
	}
	if d := scalar[int](t, db, `SELECT duration_days FROM plan WHERE id=?`, gold); d != 60 {
		t.Errorf("duration after edit %d", d)
	}
	// Edit validation mirrors create.
	if r := c.Send("PUT", fmt.Sprintf("/admin/plans/%d", gold), harness.PlanForm("Gold", "max_sessions", "0")); r.StatusCode != 422 || fieldError(r, t, "max_sessions") == "" {
		t.Errorf("edit invalid: %d", r.StatusCode)
	}
}

// E1.08 decimal speed/bulk accepted and stored exactly (BR-4).
func TestE1_08_DecimalValues(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	c := harness.LoginAsAdmin(t, s)
	db := harness.DB(t, s.DataDir)
	id := c.CreatePlan(harness.PlanForm("Dec", "speed_value", "1.5", "speed_unit", "Mbps", "bulk_value", "0.5", "bulk_unit", "GB", "time_limit_minutes", "90", "max_sessions", "3"))
	q := func(col string) float64 { return scalar[float64](t, db, `SELECT `+col+` FROM plan WHERE id=?`, id) }
	if q("speed_value") != 1.5 || q("bulk_value") != 0.5 || q("time_limit_minutes") != 90 || q("max_sessions") != 3 {
		t.Fatalf("stored %v %v", q("speed_value"), q("bulk_value"))
	}
	if r := c.Send("PUT", fmt.Sprintf("/admin/plans/%d", id), harness.PlanForm("Dec", "speed_value", "0.25", "speed_unit", "Gbps", "bulk_value", "100.75", "bulk_unit", "KB")); r.StatusCode != 200 {
		t.Fatalf("edit: %d %s", r.StatusCode, r.Body)
	}
	if q("speed_value") != 0.25 || q("bulk_value") != 100.75 || scalar[string](t, db, `SELECT speed_unit FROM plan WHERE id=?`, id) != "Gbps" {
		t.Fatal("decimal edit not stored")
	}
	// The edit page shows the stored values back.
	d := c.Get(fmt.Sprintf("/admin/plans/%d", id)).Doc(t)
	if v, _ := d.Find(`input[name="speed_value"]`).Attr("value"); v != "0.25" {
		t.Errorf("speed shown as %q", v)
	}
	if v, _ := d.Find(`input[name="bulk_value"]`).Attr("value"); v != "100.75" {
		t.Errorf("bulk shown as %q", v)
	}
}

// E1.09 deleting a plan that members use is blocked with the count; the default plan too (O12).
func TestE1_09_DeletePlanInUse(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	c := harness.LoginAsAdmin(t, s)
	db := harness.DB(t, s.DataDir)
	id := c.CreatePlan(harness.PlanForm("Used"))
	insertMembers(t, db, id, 3)
	path := fmt.Sprintf("/admin/plans/%d", id)
	r := c.Send("DELETE", path, nil)
	if r.StatusCode != 409 || !strings.Contains(r.Body, "3 members") {
		t.Fatalf("delete in use: %d %q", r.StatusCode, r.Body)
	}
	if n := scalar[int](t, db, `SELECT count(*) FROM plan WHERE id=?`, id); n != 1 {
		t.Fatal("plan deleted despite members")
	}
	if _, err := db.Exec(`DELETE FROM member`); err != nil {
		t.Fatal(err)
	}
	// Default plan cannot be deleted until another default (or none) is chosen.
	if r := setDefaultPlan(t, c, fmt.Sprint(id)); r.StatusCode != 200 {
		t.Fatalf("set default: %d %s", r.StatusCode, r.Body)
	}
	r = c.Send("DELETE", path, nil)
	if r.StatusCode != 409 || !strings.Contains(strings.ToLower(r.Body), "default") {
		t.Fatalf("delete default: %d %q", r.StatusCode, r.Body)
	}
	if r := setDefaultPlan(t, c, ""); r.StatusCode != 200 {
		t.Fatalf("clear default: %d", r.StatusCode)
	}
	if r := c.Send("DELETE", path, nil); r.StatusCode != 200 || r.Header.Get("HX-Redirect") != "/admin/plans" {
		t.Fatalf("delete unused: %d %q", r.StatusCode, r.Header.Get("HX-Redirect"))
	}
	if r := c.Get(path); r.StatusCode != 404 {
		t.Fatalf("deleted plan page: %d", r.StatusCode)
	}
}

// E1.18 editing a plan used by N members shows the N-member warning (BR-5b, Q16).
func TestE1_18_PlanEditWarning(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	c := harness.LoginAsAdmin(t, s)
	db := harness.DB(t, s.DataDir)
	id := c.CreatePlan(harness.PlanForm("Shared"))
	path := fmt.Sprintf("/admin/plans/%d", id)
	if n := c.Get(path).Doc(t).Find(`[data-plan-warning]`).Length(); n != 0 {
		t.Fatalf("warning shown for 0 members")
	}
	warning := func() string {
		return strings.Join(strings.Fields(c.Get(path).Doc(t).Find(`[data-plan-warning]`).Text()), " ")
	}
	insertMembers(t, db, id, 1)
	if w := warning(); w != "1 member uses this plan; changes apply to it at their next login" {
		t.Fatalf("singular warning %q", w)
	}
	if _, err := db.Exec(`DELETE FROM member`); err != nil {
		t.Fatal(err)
	}
	insertMembers(t, db, id, 3)
	w := strings.Join(strings.Fields(c.Get(path).Doc(t).Find(`[data-plan-warning]`).Text()), " ")
	if w != "3 members use this plan; changes apply to all of them at their next login" {
		t.Fatalf("warning %q", w)
	}
	// A failed edit re-renders the form with the warning still present.
	r := c.Send("PUT", path, harness.PlanForm("Shared", "duration_days", "0"))
	if r.StatusCode != 422 || r.Doc(t).Find(`[data-plan-warning]`).Length() != 1 {
		t.Fatalf("422 form lost the warning: %d", r.StatusCode)
	}
}

// E1.18 second half: plan limits are read live at each login (BR-5b, Q16).
func TestE1_18_LimitsChangeAffectsNextAccessRequest(t *testing.T) {
	f := newP3(t)
	form := func(kv ...string) url.Values { return harness.PlanForm("Shared", kv...) }
	planID := f.plan("Shared", "bulk_value", "1", "bulk_unit", "GB", "duration_days", "30")
	path := fmt.Sprintf("/admin/plans/%d", planID)
	edit := func(kv ...string) {
		t.Helper()
		if r := f.c.Send("PUT", path, form(kv...)); r.StatusCode != 200 {
			t.Fatalf("edit plan: %d %s", r.StatusCode, r.Body)
		}
	}
	id := f.member(planID, "alice", "password", "pw")
	f.exec(`UPDATE member SET used_download_bytes=?, subscription_date=? WHERE id=?`, 500*mb, t0.Unix()-10*24*3600, id)
	f.accepted("alice", "pw")

	// Lowering the data limit below the usage rejects at the next login; raising it accepts again.
	edit("bulk_value", "100", "bulk_unit", "MB", "duration_days", "30")
	f.rejected("alice", "pw", "OUT_OF_BULK")
	edit("bulk_value", "2", "bulk_unit", "GB", "duration_days", "30")
	f.accepted("alice", "pw")

	// Shortening the duration below the elapsed time lapses the window; auto-resubscribe renews instead.
	edit("bulk_value", "2", "bulk_unit", "GB", "duration_days", "5")
	f.rejected("alice", "pw", "NO_ACTIVE_SUBSCRIPTION")
	edit("bulk_value", "2", "bulk_unit", "GB", "duration_days", "5", "auto_resubscribe", "on")
	f.accepted("alice", "pw")
	if n := scalar[int](t, f.db, `SELECT renewal_count FROM member WHERE id=?`, id); n != 1 {
		t.Errorf("renewal_count %d, want 1", n)
	}

	// The time limit and the session limit are live too.
	edit("bulk_value", "2", "bulk_unit", "GB", "duration_days", "5", "time_limit_minutes", "10")
	f.exec(`UPDATE member SET used_seconds=600 WHERE id=?`, id)
	f.rejected("alice", "pw", "OUT_OF_TIME")
	edit("bulk_value", "2", "bulk_unit", "GB", "duration_days", "5", "time_limit_minutes", "20")
	f.accepted("alice", "pw")
	f.openSession(id, "alice", "s1", f.fc.Now())
	f.rejected("alice", "pw", "NO_MORE_SESSION")
	edit("bulk_value", "2", "bulk_unit", "GB", "duration_days", "5", "time_limit_minutes", "20", "max_sessions", "2")
	f.accepted("alice", "pw")
}

// E16.02 (review A6) a positive data value that rounds to 0 bytes is a field error (422), not an "unlimited"
// plan that rejects every login with 0 bytes allowed.
func TestE16_02_TinyBulkRejected(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	c := harness.LoginAsAdmin(t, s)
	db := harness.DB(t, s.DataDir)
	r := c.Send("POST", "/admin/plans", harness.PlanForm("P", "bulk_value", "0.0000000001", "bulk_unit", "KB"))
	if r.StatusCode != 422 || fieldError(r, t, "bulk_value") == "" {
		t.Fatalf("status %d, want 422 with a bulk_value message", r.StatusCode)
	}
	if n := scalar[int](t, db, `SELECT count(*) FROM plan`); n != 0 {
		t.Fatalf("tiny plan stored: %d", n)
	}
}
