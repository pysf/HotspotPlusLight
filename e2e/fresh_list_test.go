package e2e

import (
	"net/url"
	"strconv"
	"strings"
	"testing"

	"hotspotpluslight/e2e/harness"
)

// closeLast fails unless "close-sheet" is the last event in the HX-Trigger header. close-sheet
// removes the form from the page, and events fired after that never reach the body, so the list
// would not refresh. (Bug: lists showed old data after a save from the sheet.)
func closeLast(t *testing.T, what string, raw string) {
	t.Helper()
	i := strings.Index(raw, `"close-sheet"`)
	if i < 0 {
		t.Fatalf("%s: no close-sheet in %q", what, raw)
	}
	if strings.Contains(raw[i+len(`"close-sheet"`):], `":`) {
		t.Errorf("%s: close-sheet is not the last event: %q", what, raw)
	}
}

// E21.01 after a save from the sheet the lists show fresh data: the refresh events come before
// close-sheet, and the list the browser then fetches already has the change.
func TestE21_01_ListsAreFreshAfterChange(t *testing.T) {
	f := newP5(t)
	p := f.plan("Basic")
	r := f.c.SendHX("POST", "/admin/members", harness.MemberForm(p, "anna"), sheetTarget)
	if r.StatusCode != 201 {
		t.Fatalf("create: %d", r.StatusCode)
	}
	id := f.idOf("anna")

	// Member edit.
	r = f.c.SendHX("PUT", mpath(id), harness.MemberForm(p, "anna2", "first_name", "Annabel"), sheetTarget)
	closeLast(t, "member edit", r.Header.Get("HX-Trigger"))
	if list := f.c.GetHX("/admin/members", listTarget).Body; !strings.Contains(list, "anna2") || strings.Contains(list, ">anna<") {
		t.Errorf("member list is stale after edit: %s", list)
	}

	// Plan create, edit, delete.
	r = f.c.SendHX("POST", "/admin/plans", harness.PlanForm("Gold"), sheetTarget)
	closeLast(t, "plan create", r.Header.Get("HX-Trigger"))
	if !strings.Contains(f.c.GetHX("/admin/plans", "plan-list").Body, "Gold") {
		t.Error("plan list lacks the new plan")
	}
	gold := scalar[int64](t, f.db, `SELECT id FROM plan WHERE name='Gold'`)
	gpath := "/admin/plans/" + strconv.FormatInt(gold, 10)
	r = f.c.SendHX("PUT", gpath, harness.PlanForm("Silver"), sheetTarget)
	closeLast(t, "plan edit", r.Header.Get("HX-Trigger"))
	if list := f.c.GetHX("/admin/plans", "plan-list").Body; !strings.Contains(list, "Silver") || strings.Contains(list, "Gold") {
		t.Errorf("plan list is stale after edit: %s", list)
	}
	r = f.c.SendHX("DELETE", gpath, nil, "plan-list")
	if list := f.c.GetHX("/admin/plans", "plan-list").Body; strings.Contains(list, "Silver") {
		t.Errorf("plan list still shows a deleted plan (status %d)", r.StatusCode)
	}
}

// E21.02 a batch rename reloads the page, so the list shows the new label.
func TestE21_02_BatchRenameReloads(t *testing.T) {
	f := newP7(t)
	id := f.batch(f.plan("Basic"), 2, "label", "Old")
	r := f.c.Send("PUT", bpath(id), url.Values{"label": {"Fresh"}})
	if r.StatusCode != 200 || r.Header.Get("HX-Refresh") != "true" {
		t.Fatalf("rename: %d HX-Refresh %q", r.StatusCode, r.Header.Get("HX-Refresh"))
	}
	if body := f.c.Get("/admin/batches").Body; !strings.Contains(body, "Fresh") || strings.Contains(body, "Old") {
		t.Error("batch list is stale after rename")
	}
}
