package e2e

import (
	"net/url"
	"strings"
	"testing"
)

// E25.01 the edit sheet holds the extra data form and has no link to the member page.
func TestE25_01_SheetHasExtraData(t *testing.T) {
	f := newP2(t)
	p := f.plan("Capped", "bulk_value", "10", "bulk_unit", "GB")
	unl := f.plan("Unlimited", "bulk_value", "0")
	id, uid := f.member(p, "alice"), f.member(unl, "bob")
	d := fragmentDoc(t, f.c.GetHX(mpath(id), sheetTarget))
	form := d.Find("[data-more=edit] form#extra-bulk-form")
	if form.AttrOr("hx-put", "") != mpath(id)+"/extra-bulk" || form.AttrOr("hx-target", "") != "#extra-bulk-block" {
		t.Errorf("extra data form: %v", form.Nodes)
	}
	if d.Find("[data-extra-bulk]").Length() != 1 || d.Find(`a[href="`+mpath(id)+`"]`).Length() != 0 {
		t.Error("the sheet needs the current extra data and no link to the member page")
	}
	u := fragmentDoc(t, f.c.GetHX(mpath(uid), sheetTarget))
	if u.Find("form#extra-bulk-form").Length() != 0 || !strings.Contains(u.Text(), "This plan has unlimited data.") {
		t.Error("an unlimited plan shows the note and no form")
	}
}

// E25.02 saving extra data inside the sheet keeps the sheet open and shows fresh values.
func TestE25_02_SheetExtraBulkSave(t *testing.T) {
	f := newP2(t)
	p := f.plan("Capped", "bulk_value", "10", "bulk_unit", "GB")
	id := f.member(p, "alice")
	extra := func() int64 { return scalar[int64](t, f.db, `SELECT extra_bulk_bytes FROM member WHERE id=?`, id) }
	r := f.c.SendHX("PUT", mpath(id)+"/extra-bulk", url.Values{"value": {"2"}, "unit": {"GB"}}, "extra-bulk-block")
	trig := r.Header.Get("HX-Trigger")
	if r.StatusCode != 200 || !strings.Contains(trig, "members-changed") || !strings.Contains(trig, "Extra data saved.") || strings.Contains(trig, "close-sheet") {
		t.Fatalf("save: %d trigger %q", r.StatusCode, trig)
	}
	if r.Header.Get("HX-Redirect") != "" || r.Header.Get("HX-Reswap") == "none" {
		t.Error("the answer must be swapped and must not redirect")
	}
	if extra() != 2<<30 {
		t.Fatalf("stored %d", extra())
	}
	d := fragmentDoc(t, r)
	if text(d.Find("[data-extra-bulk]")) != "2 GB" || d.Find(`#sheet-usage[hx-swap-oob]`).Length() != 1 || !strings.Contains(text(d.Find("#sheet-usage")), "12") {
		t.Errorf("fresh values missing: %s", r.Body)
	}
	bad := f.c.SendHX("PUT", mpath(id)+"/extra-bulk", url.Values{"value": {"abc"}, "unit": {"GB"}}, "extra-bulk-block")
	if bad.StatusCode != 422 || bad.Header.Get("HX-Trigger") != "" || fragmentDoc(t, bad).Find(`p[data-error-for=value]`).Length() == 0 {
		t.Errorf("bad value: %d %s", bad.StatusCode, bad.Body)
	}
	if extra() != 2<<30 {
		t.Error("a bad value changed the stored extra data")
	}
}
