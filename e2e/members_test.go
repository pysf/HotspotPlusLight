package e2e

import (
	"bytes"
	"database/sql"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"layeh.com/radius"
	"layeh.com/radius/rfc2865"

	"hotspotpluslight/e2e/fakenas"
	"hotspotpluslight/e2e/harness"
)

// E2.01 create member defaults (BR-14, Q19); no plan => the form says to create one.
func TestE2_01_CreateDefaults(t *testing.T) {
	f := newP2(t)
	d := f.c.Get("/admin/members/new").Doc(t)
	if d.Find("form#member-form").Length() != 0 || !strings.Contains(strings.ToLower(d.Text()), "create a plan first") {
		t.Fatal("with no plan the page must say to create a plan first and show no form")
	}
	if r := f.c.Send("POST", "/admin/members", harness.MemberForm(1, "ghost")); r.StatusCode != 422 || fieldError(r, t, "plan_id") == "" {
		t.Fatalf("create with a plan that does not exist: %d", r.StatusCode)
	}
	p := f.plan("Basic")
	// No plan chosen is rejected (Q19).
	r := f.c.Send("POST", "/admin/members", harness.MemberForm(p, "alice1", "plan_id", ""))
	if r.StatusCode != 422 || fieldError(r, t, "plan_id") == "" || memberCount(t, f.db) != 0 {
		t.Fatalf("create without plan: %d", r.StatusCode)
	}
	// Defaults: random 6-digit password, active, window starts now, counters 0.
	id := f.member(p, "alice1")
	if pw := revealed(f, id); !regexp.MustCompile(`^\d{6}$`).MatchString(pw) {
		t.Fatalf("default password %q", pw)
	}
	row := f.db.QueryRow(`SELECT active, subscription_date, used_download_bytes, used_upload_bytes, used_seconds, extra_bulk_bytes, renewal_count, plan_id FROM member WHERE id=?`, id)
	var active, sub, dl, ul, secs, extra, renewals, plan int64
	if err := row.Scan(&active, &sub, &dl, &ul, &secs, &extra, &renewals, &plan); err != nil {
		t.Fatal(err)
	}
	if active != 1 || sub != t0.Unix() || dl+ul+secs+extra+renewals != 0 || plan != p {
		t.Fatalf("defaults: active=%d sub=%d counters=%d plan=%d", active, sub, dl+ul+secs+extra+renewals, plan)
	}
	if scalar[sql.NullInt64](t, f.db, `SELECT expires_at FROM member WHERE id=?`, id).Valid {
		t.Fatal("expires_at must default to NULL")
	}
	// Username defaults to the mobile number.
	id2 := f.member(p, "", "mobile", "0912 345 6789")
	if u := scalar[string](t, f.db, `SELECT username FROM member WHERE id=?`, id2); u != "9123456789" {
		t.Fatalf("username from mobile %q", u)
	}
	// An explicit password is kept.
	id3 := f.member(p, "bob", "password", "s3cret!")
	if revealed(f, id3) != "s3cret!" {
		t.Fatal("explicit password not kept")
	}
	// Neither username nor mobile => error.
	if r := f.c.Send("POST", "/admin/members", harness.MemberForm(p, "")); r.StatusCode != 422 || fieldError(r, t, "username") == "" {
		t.Fatalf("no username and no mobile: %d", r.StatusCode)
	}
}

// E2.11 the default plan is preselected in the form, never silently assigned (O10, Q19).
func TestE2_11_DefaultPlanPreselected(t *testing.T) {
	f := newP2(t)
	a, b := f.plan("Alpha"), f.plan("Beta")
	selected := func() string {
		return strings.TrimSpace(f.c.Get("/admin/members/new").Doc(t).Find(`select[name="plan_id"] option[selected]`).AttrOr("value", ""))
	}
	if sel := selected(); sel != "" {
		t.Fatalf("no default plan set, but %q is preselected", sel)
	}
	if r := setDefaultPlan(t, f.c, strconv.FormatInt(b, 10)); r.StatusCode != 200 {
		t.Fatalf("set default: %d", r.StatusCode)
	}
	if sel := selected(); sel != strconv.FormatInt(b, 10) {
		t.Fatalf("preselected %q, want %d", sel, b)
	}
	opts := f.c.Get("/admin/members/new").Doc(t).Find(`select[name="plan_id"] option`).Length()
	if opts < 3 { // "choose" + two plans
		t.Fatalf("plan options: %d", opts)
	}
	// A request that omits the plan is rejected even though a default exists.
	if r := f.c.Send("POST", "/admin/members", harness.MemberForm(a, "carol", "plan_id", "-")); r.StatusCode != 422 {
		t.Fatalf("silent default assignment: %d", r.StatusCode)
	}
	if memberCount(t, f.db) != 0 {
		t.Fatal("member created without plan")
	}
}

// E2.01b the member page shows every field the list leaves out (names, mobile, plan, MAC, expiry, status, online).
func TestE2_01b_MemberPageShowsFields(t *testing.T) {
	f := newP2(t)
	p := f.plan("Basic")
	id := f.member(p, "full", "first_name", "Zoe", "last_name", "Quinn", "mobile", "+98 912 345 6789",
		"mac", "aa-bb-cc-dd-ee-ff", "expires_at", "2027-01-15T10:30")
	f.openSession(id, "full", "s1", t0)
	d := f.c.Get(mpath(id)).Doc(t)
	for name, want := range map[string]string{"first_name": "Zoe", "last_name": "Quinn", "mobile": "9123456789",
		"mac": "AA:BB:CC:DD:EE:FF", "expires_at": "2027-01-15T10:30"} {
		if got := d.Find(`#member-form input[name="`+name+`"]`).AttrOr("value", ""); got != want {
			t.Errorf("member page %s = %q, want %q", name, got, want)
		}
	}
	// The stored instant is the settings-timezone wall time.
	loc, err := time.LoadLocation(scalar[string](t, f.db, `SELECT timezone FROM settings`))
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2027, 1, 15, 10, 30, 0, 0, loc).Unix()
	if got := scalar[int64](t, f.db, `SELECT expires_at FROM member WHERE id=?`, id); got != want {
		t.Errorf("expires_at %d, want %d", got, want)
	}
	if got := d.Find(`#member-form select[name="plan_id"] option[selected]`).Text(); !strings.HasPrefix(strings.TrimSpace(got), "Basic") {
		t.Errorf("member page plan %q", got)
	}
	st := text(d.Find("#member-status"))
	if !strings.Contains(st, "status active") || !strings.Contains(st, "online") {
		t.Errorf("member status line %q", st)
	}
	// The online dot is not colour-only.
	dot := f.c.Get("/admin/members").Doc(t).Find(`tr[data-username="full"] .c-dot`)
	if dot.AttrOr("role", "") != "img" || dot.AttrOr("aria-label", "") != "Online" {
		t.Errorf("online dot a11y: role=%q label=%q", dot.AttrOr("role", ""), dot.AttrOr("aria-label", ""))
	}
	f.member(p, "idle")
	dot = f.c.Get("/admin/members").Doc(t).Find(`tr[data-username="idle"] .c-dot`)
	if dot.AttrOr("aria-label", "") != "Offline" {
		t.Errorf("offline dot label %q", dot.AttrOr("aria-label", ""))
	}
}

// E2.02 username rules (BR-1, O13).
func TestE2_02_UsernameRules(t *testing.T) {
	f := newP2(t)
	p := f.plan("Basic")
	long := strings.Repeat("a", 64)
	for _, ok := range []string{"a", "Ab9", "USER42", long, "  padded  "} {
		id := f.member(p, ok)
		want := strings.TrimSpace(ok)
		if got := scalar[string](t, f.db, `SELECT username FROM member WHERE id=?`, id); got != want {
			t.Errorf("stored %q, want %q", got, want)
		}
	}
	before := memberCount(t, f.db)
	for _, bad := range []string{strings.Repeat("a", 65), "é", "علی", "۱۲۳", "a b", "a-b", "a_b", "x.y", "a@b", "ünï", "a​"} {
		r := f.c.Send("POST", "/admin/members", harness.MemberForm(p, bad))
		if r.StatusCode != 422 || fieldError(r, t, "username") == "" {
			t.Errorf("username %q: status %d", bad, r.StatusCode)
		}
	}
	// Case-insensitive uniqueness.
	if r := f.c.Send("POST", "/admin/members", harness.MemberForm(p, "ab9")); r.StatusCode != 422 || fieldError(r, t, "username") == "" {
		t.Errorf("duplicate differing in case: %d", r.StatusCode)
	}
	if memberCount(t, f.db) != before {
		t.Fatal("a rejected username was stored")
	}
	// Editing: taking someone else's name (any case) is refused; changing the case of one's own is fine.
	id := f.member(p, "carol")
	form := harness.FormValues(t, f.c.Get(mpath(id)).Doc(t), "form#member-form")
	form.Set("username", "AB9")
	if r := f.c.Send("PUT", mpath(id), form); r.StatusCode != 422 || fieldError(r, t, "username") == "" {
		t.Errorf("rename to taken name: %d", r.StatusCode)
	}
	form.Set("username", "CAROL")
	if r := f.c.Send("PUT", mpath(id), form); r.StatusCode != 200 {
		t.Errorf("recase own name: %d %s", r.StatusCode, r.Body)
	}
	if got := scalar[string](t, f.db, `SELECT username FROM member WHERE id=?`, id); got != "CAROL" {
		t.Errorf("username after edit %q", got)
	}
}

// E2.03 mobile rule (BR-2).
func TestE2_03_Mobile(t *testing.T) {
	f := newP2(t)
	p := f.plan("Basic")
	for i, tc := range [][2]string{{"0912 345 6789", "9123456789"}, {"+98 912 345 6789", "9123456789"}, {"9123456789", "9123456789"}, {"", ""}} {
		id := f.member(p, fmt.Sprintf("m%d", i), "mobile", tc[0])
		if got := scalar[string](t, f.db, `SELECT mobile FROM member WHERE id=?`, id); got != tc[1] {
			t.Errorf("mobile %q stored as %q, want %q", tc[0], got, tc[1])
		}
	}
	for _, bad := range []string{"12345", "0912", "abc", "123456789"} {
		if r := f.c.Send("POST", "/admin/members", harness.MemberForm(p, "x", "mobile", bad)); r.StatusCode != 422 || fieldError(r, t, "mobile") == "" {
			t.Errorf("mobile %q: %d", bad, r.StatusCode)
		}
	}
}

// E2.04 MAC normalization (BR-3).
func TestE2_04_MAC(t *testing.T) {
	f := newP2(t)
	p := f.plan("Basic")
	for i, in := range []string{"AA-BB-CC-DD-EE-FF", "aa:bb:cc:dd:ee:ff", "AABB.CCDD.EEFF", " aabbccddeeff ", "aa bb cc dd ee ff"} {
		id := f.member(p, fmt.Sprintf("m%d", i), "mac", in)
		if got := scalar[string](t, f.db, `SELECT mac FROM member WHERE id=?`, id); got != "AA:BB:CC:DD:EE:FF" {
			t.Errorf("mac %q stored as %q", in, got)
		}
	}
	if id := f.member(p, "nomac"); scalar[string](t, f.db, `SELECT mac FROM member WHERE id=?`, id) != "" {
		t.Error("empty mac must stay empty")
	}
	for _, bad := range []string{"zz:zz:zz:zz:zz:zz", "aa:bb:cc", "aabbccddeeff00", "AA:BB:CC:DD:EE:GG"} {
		if r := f.c.Send("POST", "/admin/members", harness.MemberForm(p, "x", "mac", bad)); r.StatusCode != 422 || fieldError(r, t, "mac") == "" {
			t.Errorf("mac %q: %d", bad, r.StatusCode)
		}
	}
	// The member page shows the canonical form.
	if got := f.c.Get(mpath(scalar[int64](t, f.db, `SELECT id FROM member WHERE username='m0'`))).Doc(t).Find(`input[name="mac"]`).AttrOr("value", ""); got != "AA:BB:CC:DD:EE:FF" {
		t.Errorf("member page mac %q", got)
	}
	// Editing normalizes too.
	id := f.member(p, "edit")
	form := harness.FormValues(t, f.c.Get(mpath(id)).Doc(t), "form#member-form")
	form.Set("mac", "11-22-33-44-55-66")
	if r := f.c.Send("PUT", mpath(id), form); r.StatusCode != 200 {
		t.Fatalf("edit: %d", r.StatusCode)
	}
	if got := scalar[string](t, f.db, `SELECT mac FROM member WHERE id=?`, id); got != "11:22:33:44:55:66" {
		t.Errorf("edited mac %q", got)
	}
}

// E2.05 password encrypted at rest, revealable and changeable in the UI (D8).
func TestE2_05_PasswordEncrypted(t *testing.T) {
	f := newP2(t)
	p := f.plan("Basic")
	id := f.member(p, "alice", "password", "Plain-Text-1")
	var raw []byte
	if err := f.db.QueryRow(`SELECT password_enc FROM member WHERE id=?`, id).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("Plain-Text-1")) || len(raw) <= len("Plain-Text-1") {
		t.Fatalf("password stored in the clear or not as nonce||ciphertext: %x", raw)
	}
	if revealed(f, id) != "Plain-Text-1" {
		t.Fatal("reveal does not return the password")
	}
	// The detail page itself never contains the password.
	if strings.Contains(f.c.Get(mpath(id)).Body, "Plain-Text-1") {
		t.Fatal("password is in the detail page source")
	}
	if r := f.c.Send("PUT", mpath(id)+"/password", url.Values{"password": {"New-Pass-2"}}); r.StatusCode != 200 {
		t.Fatalf("set password: %d %s", r.StatusCode, r.Body)
	}
	if revealed(f, id) != "New-Pass-2" {
		t.Fatal("changed password not revealed")
	}
	if r := f.c.Send("PUT", mpath(id)+"/password", url.Values{"password": {"  "}}); r.StatusCode != 422 {
		t.Fatalf("blank password: %d", r.StatusCode)
	}
	if r := f.c.Send("PUT", mpath(9999)+"/password", url.Values{"password": {"x"}}); r.StatusCode != 404 {
		t.Fatalf("unknown member: %d", r.StatusCode)
	}
	if r := f.c.Get(mpath(9999) + "/password"); r.StatusCode != 404 {
		t.Fatalf("reveal unknown member: %d", r.StatusCode)
	}
}

// E2.06 changing plan keeps window, extra bulk, counters and sessions; renew resets them (BR-13).
func TestE2_06_PlanChangeVsRenew(t *testing.T) {
	f := newP2(t)
	p1, p2id := f.plan("One"), f.plan("Two", "bulk_value", "20")
	id := f.member(p1, "alice")
	oldSub := t0.Add(-5 * 24 * time.Hour).Unix()
	f.exec(`UPDATE member SET subscription_date=?, extra_bulk_bytes=1073741824, used_download_bytes=123, used_upload_bytes=45, used_seconds=60 WHERE id=?`, oldSub, id)
	f.openSession(id, "alice", "s-1", t0)

	form := harness.FormValues(t, f.c.Get(mpath(id)).Doc(t), "form#member-form")
	form.Set("plan_id", strconv.FormatInt(p2id, 10))
	if r := f.c.Send("PUT", mpath(id), form); r.StatusCode != 200 {
		t.Fatalf("change plan: %d %s", r.StatusCode, r.Body)
	}
	state := func() string {
		return scalar[string](t, f.db, `SELECT plan_id||'/'||subscription_date||'/'||extra_bulk_bytes||'/'||used_download_bytes||'/'||used_upload_bytes||'/'||used_seconds||'/'||renewal_count FROM member WHERE id=?`, id)
	}
	if got, want := state(), fmt.Sprintf("%d/%d/1073741824/123/45/60/0", p2id, oldSub); got != want {
		t.Fatalf("after plan change %s, want %s", got, want)
	}
	if scalar[int](t, f.db, `SELECT count(*) FROM session WHERE member_id=? AND ended_at IS NULL`, id) != 1 || len(f.disc.Calls()) != 0 {
		t.Fatal("running session touched by a plan change")
	}

	f.advance(time.Hour)
	if r := f.c.Send("POST", mpath(id)+"/renewals", url.Values{}); r.StatusCode != 200 && r.StatusCode != 201 {
		t.Fatalf("renew: %d %s", r.StatusCode, r.Body)
	}
	if got, want := state(), fmt.Sprintf("%d/%d/0/0/0/0/1", p2id, t0.Add(time.Hour).Unix()); got != want {
		t.Fatalf("after renewal %s, want %s", got, want)
	}
	if scalar[int](t, f.db, `SELECT count(*) FROM session WHERE member_id=? AND ended_at IS NULL`, id) != 1 {
		t.Fatal("renewal touched the session")
	}
	if r := f.c.Send("POST", mpath(9999)+"/renewals", url.Values{}); r.StatusCode != 404 {
		t.Fatalf("renew unknown: %d", r.StatusCode)
	}
}

// E2.07 renewal bookkeeping and its display (BR-13b, Q21).
func TestE2_07_RenewalDisplay(t *testing.T) {
	f := newP2(t)
	p := f.plan("Basic")
	id := f.member(p, "alice")
	if f.c.Get(mpath(id)).Doc(t).Find("[data-renewal]").Length() != 0 {
		t.Fatal("never-renewed member shows renewal info")
	}
	f.advance(2 * time.Hour)
	f.c.Send("POST", mpath(id)+"/renewals", url.Values{})
	var last sql.NullInt64
	var n int
	var kind string
	if err := f.db.QueryRow(`SELECT last_renewed_at, renewal_count, last_renewal_kind FROM member WHERE id=?`, id).Scan(&last, &n, &kind); err != nil {
		t.Fatal(err)
	}
	if !last.Valid || last.Int64 != t0.Add(2*time.Hour).Unix() || n != 1 || kind != "manual" {
		t.Fatalf("after renewal: %v %d %q", last, n, kind)
	}
	got := text(f.c.Get(mpath(id)).Doc(t).Find("[data-renewal]"))
	if !strings.HasPrefix(got, "Renewed 1 time, last on 2026-11-01 14:00") || !strings.Contains(got, "(manual)") {
		t.Fatalf("renewal text %q", got)
	}
	f.c.Send("POST", mpath(id)+"/renewals", url.Values{})
	if scalar[int](t, f.db, `SELECT renewal_count FROM member WHERE id=?`, id) != 2 {
		t.Fatal("renewal_count not incremented")
	}
	f.exec(`UPDATE member SET last_renewal_kind='auto', renewal_count=3 WHERE id=?`, id)
	got = text(f.c.Get(mpath(id)).Doc(t).Find("[data-renewal]"))
	if !strings.HasPrefix(got, "Renewed 3 times") || !strings.Contains(got, "(automatic)") {
		t.Fatalf("auto renewal text %q", got)
	}
}

// E2.08 set extra bulk (Q17).
func TestE2_08_ExtraBulk(t *testing.T) {
	f := newP2(t)
	p := f.plan("Capped", "bulk_value", "10", "bulk_unit", "GB")
	unl := f.plan("Unlimited", "bulk_value", "0")
	id := f.member(p, "alice")
	path := mpath(id) + "/extra-bulk"
	extra := func() int64 { return scalar[int64](t, f.db, `SELECT extra_bulk_bytes FROM member WHERE id=?`, id) }
	set := func(v, unit string) harness.Resp {
		return f.c.Send("PUT", path, url.Values{"value": {v}, "unit": {unit}})
	}
	for _, tc := range []struct {
		v, unit string
		want    int64
	}{{"1.5", "GB", 1610612736}, {"512", "MB", 536870912}, {"100.5", "KB", 102912}, {"2", "GB", 2147483648}, {"1", "MB", 1048576}, {"0", "GB", 0}} {
		if r := set(tc.v, tc.unit); r.StatusCode != 200 {
			t.Fatalf("set %s %s: %d %s", tc.v, tc.unit, r.StatusCode, r.Body)
		}
		if got := extra(); got != tc.want {
			t.Fatalf("set %s %s: stored %d, want %d", tc.v, tc.unit, got, tc.want)
		}
	}
	set("1.5", "GB")
	if got := text(f.c.Get(mpath(id)).Doc(t).Find("[data-extra-bulk]")); got != "1.5 GB" {
		t.Fatalf("extra bulk shown as %q (plan units)", got)
	}
	for _, tc := range [][2]string{{"-1", "GB"}, {"abc", "GB"}, {"", "GB"}, {"1", "TB"}} {
		if r := set(tc[0], tc[1]); r.StatusCode != 422 {
			t.Errorf("set %q %q: %d", tc[0], tc[1], r.StatusCode)
		}
	}
	if extra() != 1610612736 {
		t.Fatal("a rejected value changed the stored extra bulk")
	}
	uid := f.member(unl, "bob")
	if r := f.c.Send("PUT", mpath(uid)+"/extra-bulk", url.Values{"value": {"1"}, "unit": {"GB"}}); r.StatusCode != 409 || !strings.Contains(r.Body, "This plan has unlimited data") {
		t.Fatalf("unlimited plan: %d %s", r.StatusCode, r.Body)
	}
	if r := f.c.Send("PUT", mpath(9999)+"/extra-bulk", url.Values{"value": {"1"}, "unit": {"GB"}}); r.StatusCode != 404 {
		t.Fatalf("unknown member: %d", r.StatusCode)
	}
}

// E2.09 search, pagination, per-page.
func TestE2_09_SearchAndPaging(t *testing.T) {
	f := newP2(t)
	p, q := f.plan("Basic"), f.plan("Other")
	for i := 0; i < 25; i++ {
		kv := []string{}
		if i == 3 {
			kv = []string{"first_name", "Zoe", "last_name", "Quinn"}
		}
		if i == 4 {
			kv = []string{"mobile", "09876543210"}
		}
		plan := p
		if i%5 == 0 {
			plan = q
		}
		f.member(plan, fmt.Sprintf("user%02d", i), kv...)
	}
	list := func(query string) *goquery.Document { return f.c.Get("/admin/members?" + query).Doc(t) }
	if n := len(rows(list(""))); n != 20 {
		t.Fatalf("default page size: %d rows", n)
	}
	if n := len(rows(list("per=10"))); n != 10 {
		t.Fatalf("per=10: %d", n)
	}
	if n := len(rows(list("per=10&page=3"))); n != 5 {
		t.Fatalf("page 3: %d", n)
	}
	if n := len(rows(list("per=7"))); n != 20 {
		t.Fatalf("invalid per must fall back to the default: %d", n)
	}
	if n := len(rows(list("per=100"))); n != 25 {
		t.Fatalf("per=100: %d", n)
	}
	if n := len(rows(list("page=999"))); n == 0 {
		t.Fatal("a page past the end must clamp to the last page")
	}
	d := list("per=10")
	if d.Find(`nav[aria-label="Pages"] a[data-page]`).Length() < 2 {
		t.Fatal("no pagination links")
	}
	if href := d.Find(`nav[aria-label="Pages"] a[data-page="2"]`).AttrOr("href", ""); !strings.Contains(href, "per=10") || !strings.Contains(href, "page=2") {
		t.Fatalf("pagination link loses the page size: %q", href)
	}
	for q, want := range map[string]string{"user07": "user07", "Zoe": "user03", "quinn": "user03", "9876543210": "user04", "USER1": "user1"} {
		got := rows(list("q=" + q))
		if len(got) == 0 || !strings.HasPrefix(got[0], want) {
			t.Errorf("search %q: %v", q, got)
		}
	}
	if got := rows(list("q=%25")); len(got) != 0 {
		t.Errorf("a literal %% must not act as a wildcard: %v", got)
	}
	if got := rows(list("q=nosuchperson")); len(got) != 0 {
		t.Errorf("no-match search: %v", got)
	}
	if n := len(rows(list("plan=" + strconv.FormatInt(q, 10) + "&per=100"))); n != 5 {
		t.Fatalf("plan filter: %d", n)
	}
	// Filters survive in the form so paging keeps them.
	if v := list("q=user&plan="+strconv.FormatInt(p, 10)).Find(`form#member-filter input[name="q"]`).AttrOr("value", ""); v != "user" {
		t.Fatalf("filter form lost the query: %q", v)
	}
}

// E2.12 list shows status active / inactive / expired.
func TestE2_12_StatusDisplay(t *testing.T) {
	f := newP2(t)
	p := f.plan("Basic")
	f.member(p, "act")
	f.member(p, "inact", "active", "-")
	e := f.member(p, "exp")
	f.exec(`UPDATE member SET expires_at=? WHERE id=?`, t0.Unix()-1, e)
	d := f.c.Get("/admin/members?per=100").Doc(t)
	got := map[string]string{}
	d.Find("tr[data-member]").Each(func(_ int, s *goquery.Selection) {
		got[s.AttrOr("data-username", "")] = s.AttrOr("data-status", "")
		// The cell shows the status as a word: "inactive" is worded "Disabled" (Phase 10 step B).
		word := map[string]string{"active": "active", "expiring": "expiring", "expired": "expired", "inactive": "disabled"}[s.AttrOr("data-status", "?")]
		if strings.ToLower(text(s.Find(`[data-col="status"]`))) != word {
			t.Errorf("status cell %q != row status %q", text(s.Find(`[data-col="status"]`)), s.AttrOr("data-status", ""))
		}
	})
	if got["act"] != "active" || got["inact"] != "inactive" || got["exp"] != "expired" {
		t.Fatalf("statuses %v", got)
	}
	for _, st := range []string{"active", "inactive", "expired"} {
		r := rows(f.c.Get("/admin/members?status=" + st).Doc(t))
		if len(r) != 1 {
			t.Errorf("status=%s: %v", st, r)
		}
	}
	if n := len(rows(f.c.Get("/admin/members?status=all").Doc(t))); n != 3 {
		t.Errorf("status=all: %d", n)
	}
}

// E2.14 expired filter, boundary, "select all N results" (O7, Q29).
func TestE2_14_ExpiredFilterAndSelectAll(t *testing.T) {
	f := newP2(t)
	p, q := f.plan("Basic"), f.plan("Other")
	mk := func(name string, plan int64, exp any) int64 {
		id := f.member(plan, name)
		f.exec(`UPDATE member SET expires_at=? WHERE id=?`, exp, id)
		return id
	}
	mk("past1", p, t0.Unix()-1)
	mk("edge", p, t0.Unix())     // now == expires_at: NOT expired (strictly after)
	mk("future", p, t0.Unix()+1) // not expired
	mk("never", p, nil)
	mk("otherplan", q, t0.Unix()-3600)
	for i := 0; i < 14; i++ {
		mk(fmt.Sprintf("old%02d", i), p, t0.Unix()-int64(100+i))
	}
	expired := func(extra string) []string {
		return rows(f.c.Get("/admin/members?status=expired&per=100" + extra).Doc(t))
	}
	if got := expired(""); len(got) != 16 {
		t.Fatalf("expired set: %d %v", len(got), got)
	}
	for _, u := range expired("") {
		if u == "edge" || u == "future" || u == "never" {
			t.Errorf("%s is not expired", u)
		}
	}
	if got := expired("&q=old"); len(got) != 14 {
		t.Errorf("expired + search: %d", len(got))
	}
	if got := expired("&plan=" + strconv.FormatInt(q, 10)); len(got) != 1 || got[0] != "otherplan" {
		t.Errorf("expired + plan: %v", got)
	}
	f.fc.Advance(time.Second) // now is strictly after "edge"'s expires_at
	if got := expired(""); len(got) != 17 {
		t.Errorf("after 1s the boundary member must be expired: %d", len(got))
	}
	f.fc.Set(t0)

	// Select all N results (page size 10 => the set spans two pages).
	d := f.c.Get("/admin/members?status=expired&per=10").Doc(t)
	if !strings.Contains(text(d.Find("[data-select-all-results]")), "16") {
		t.Fatalf("select-all offer does not show the count: %q", text(d.Find("[data-select-all-results]")))
	}
	sel := url.Values{"all_results": {"1"}, "status": {"expired"}, "plan": {strconv.FormatInt(p, 10)}}
	r := f.c.Send("POST", "/admin/members/bulk-delete", sel)
	if r.StatusCode != 200 || text(r.Doc(t).Find("[data-confirm-members]")) != "15" {
		t.Fatalf("confirmation: %d members=%q", r.StatusCode, text(r.Doc(t).Find("[data-confirm-members]")))
	}
	if memberCount(t, f.db) != 19 {
		t.Fatal("the first step deleted something")
	}
	conf := harness.FormValues(t, r.Doc(t), "form#bulk-confirm")
	if r := f.c.Send("POST", "/admin/members/bulk-delete", conf); r.StatusCode != 200 || r.Header.Get("HX-Redirect") != "/admin/members" {
		t.Fatalf("confirmed delete: %d %v", r.StatusCode, r.Header)
	}
	if got := scalar[int](t, f.db, `SELECT count(*) FROM member`); got != 4 {
		t.Fatalf("%d members left, want 4 (edge, future, never, otherplan)", got)
	}
	left := scalar[string](t, f.db, `SELECT group_concat(username) FROM (SELECT username FROM member ORDER BY username)`)
	if left != "edge,future,never,otherplan" {
		t.Fatalf("survivors %s", left)
	}
	// No automatic cleanup: nothing disappears by itself when time passes and pages are viewed.
	f.fc.Advance(400 * 24 * time.Hour)
	f.c.Login("admin", adminPassword(t, f.s)) // the admin session idled out meanwhile
	f.c.Get("/admin/members?status=expired")
	if memberCount(t, f.db) != 4 {
		t.Fatal("a member was removed automatically")
	}
}

// E2.10 bulk and single delete: confirmation counts, kick first, rows gone (BR-18, BR-23 style, Q26).
func TestE2_10_BulkDelete(t *testing.T) {
	f := newP2(t)
	p := f.plan("Basic")
	a, b, c := f.member(p, "a"), f.member(p, "b"), f.member(p, "c")
	keep := f.member(p, "keep")
	f.openSession(a, "a", "sa", t0)
	f.openSession(b, "b", "sb", t0)
	f.openSession(b, "b", "sb2", t0)
	f.exec(`UPDATE session SET ended_at=?, status=2 WHERE acct_session_id='sb2'`, t0.Unix())
	f.openSession(c, "c", "sc-stale", t0.Add(-2*time.Hour)) // open but stale: not "online"
	f.openSession(keep, "keep", "sk", t0)

	page := f.c.Get("/admin/members").Doc(t)
	if page.Find("form#bulk-form").Length() != 1 || page.Find(`input[type=checkbox][name="id"]`).Length() != 4 {
		t.Fatal("list lacks the bulk form and one checkbox per row")
	}
	if page.Find("[data-bulk-guard]").Length() == 0 {
		t.Fatal("no client-side guard for an empty selection")
	}
	// Nothing selected => message, nothing happens.
	r := f.c.Send("POST", "/admin/members/bulk-delete", url.Values{})
	if r.StatusCode != 422 || !strings.Contains(r.Body, "No member selected") {
		t.Fatalf("empty selection: %d %s", r.StatusCode, r.Body)
	}
	ids := url.Values{"id": {strconv.FormatInt(a, 10), strconv.FormatInt(b, 10), strconv.FormatInt(c, 10)}}
	r = f.c.Send("POST", "/admin/members/bulk-delete", ids)
	if r.StatusCode != 200 {
		t.Fatalf("confirmation: %d", r.StatusCode)
	}
	if m, o := text(r.Doc(t).Find("[data-confirm-members]")), text(r.Doc(t).Find("[data-confirm-online]")); m != "3" || o != "2" {
		t.Fatalf("confirmation counts members=%q online=%q", m, o)
	}
	if memberCount(t, f.db) != 4 || len(f.disc.Calls()) != 0 {
		t.Fatal("the confirmation step changed something")
	}
	f.disc.Fail = true // a failing kick must not stop the delete
	conf := harness.FormValues(t, r.Doc(t), "form#bulk-confirm")
	r = f.c.Send("POST", "/admin/members/bulk-delete", conf)
	if r.StatusCode != 200 {
		t.Fatalf("delete: %d %s", r.StatusCode, r.Body)
	}
	if got := scalar[string](t, f.db, `SELECT group_concat(username) FROM member`); got != "keep" {
		t.Fatalf("survivors %q", got)
	}
	if scalar[int](t, f.db, `SELECT count(*) FROM session WHERE username <> 'keep'`) != 0 {
		t.Fatal("session rows of deleted members remain")
	}
	if scalar[int](t, f.db, `SELECT count(*) FROM session WHERE username='keep'`) != 1 {
		t.Fatal("another member's session was removed")
	}
	got := map[string]bool{}
	for _, c := range f.disc.Calls() {
		got[c.AcctSessionID] = true
		if c.NASIP != "10.0.0.1" || c.Username == "" {
			t.Errorf("disconnect target incomplete: %+v", c)
		}
	}
	if len(got) != 3 || !got["sa"] || !got["sb"] || !got["sc-stale"] {
		t.Fatalf("disconnected sessions %v (every open session, kicked before deleting)", got)
	}
	// Unknown ids in a selection are ignored; the list of ids is not trusted blindly.
	if r := f.c.Send("POST", "/admin/members/bulk-delete", url.Values{"id": {"99999"}, "confirmed": {"1"}}); r.StatusCode != 200 && r.StatusCode != 422 {
		t.Fatalf("unknown id: %d", r.StatusCode)
	}

	// Single delete kicks first too.
	f.disc.Reset()
	f.disc.Fail = false
	k := keep
	if r := f.c.Send("DELETE", mpath(k), nil); r.StatusCode != 200 || r.Header.Get("HX-Redirect") != "/admin/members" {
		t.Fatalf("single delete: %d", r.StatusCode)
	}
	if calls := f.disc.Calls(); len(calls) != 1 || calls[0].AcctSessionID != "sk" {
		t.Fatalf("single delete kicks: %+v", calls)
	}
	if memberCount(t, f.db) != 0 {
		t.Fatal("member not deleted")
	}
	if r := f.c.Send("DELETE", mpath(k), nil); r.StatusCode != 404 {
		t.Fatalf("delete again: %d", r.StatusCode)
	}
}

// Deactivation kicks the member's open sessions, regardless of the kick result (BR-18, Q14).
func TestE2_10b_DeactivateKicks(t *testing.T) {
	f := newP2(t)
	p := f.plan("Basic")
	id := f.member(p, "alice")
	f.openSession(id, "alice", "s1", t0)
	form := harness.FormValues(t, f.c.Get(mpath(id)).Doc(t), "form#member-form")
	form.Del("active")
	f.disc.Fail = true
	r := f.c.Send("PUT", mpath(id), form)
	if r.StatusCode != 200 || !strings.Contains(r.Body, "could not be disconnected") {
		t.Fatalf("failed kick must be shown: %d %s", r.StatusCode, r.Body)
	}
	if scalar[int](t, f.db, `SELECT active FROM member WHERE id=?`, id) != 0 {
		t.Fatal("member must be inactive even if the kick failed")
	}
	if calls := f.disc.Calls(); len(calls) != 1 || calls[0].Username != "alice" {
		t.Fatalf("kick calls %+v", calls)
	}
	// Reactivating or editing something else does not kick.
	f.disc.Reset()
	f.disc.Fail = false
	form.Set("active", "on")
	f.c.Send("PUT", mpath(id), form)
	form.Set("first_name", "Al")
	f.c.Send("PUT", mpath(id), form)
	if len(f.disc.Calls()) != 0 {
		t.Fatal("kick without deactivation")
	}
}

// E2.13 usage summary on list and page (BR-22); verdict equals the Access-Request result.
func TestE2_13_UsageSummaryAndVerdict(t *testing.T) {
	f := newP2(t)
	// Time zone Asia/Tehran (UTC+3:30, no DST) so the window end differs from UTC.
	form := harness.FormValues(t, f.c.Get("/admin/settings").Doc(t), "form#settings-form")
	form.Set("timezone", "Asia/Tehran")
	if r := f.c.Send("PUT", "/admin/settings", form); r.StatusCode != 200 {
		t.Fatalf("timezone: %d", r.StatusCode)
	}
	capped := f.plan("Capped", "bulk_value", "10", "bulk_unit", "GB", "time_limit_minutes", "60", "duration_days", "30")
	unl := f.plan("Free", "bulk_value", "0", "time_limit_minutes", "0", "duration_days", "30")
	auto := f.plan("Auto", "bulk_value", "10", "bulk_unit", "GB", "duration_days", "30", "auto_resubscribe", "on")
	const gb = int64(1 << 30)
	const dayS = int64(24 * 3600)
	end := "2026-12-01 15:30" // t0 + 30 days, shown in Asia/Tehran

	type tc struct {
		name                string
		plan                int64
		set                 string // SQL SET clause
		args                []any
		data, tm, windowEnd string
		verdict             string
		label               string
	}
	cases := []tc{
		{"fresh", capped, `used_seconds=0`, nil, "0 GB / 10 GB (10 GB left)", "0 min / 60 min (60 min left)", end, "", ""},
		{"extra", capped, `extra_bulk_bytes=?, used_download_bytes=?, used_upload_bytes=?, used_seconds=1800`, []any{gb, 2 * gb, gb / 2}, "2.5 GB / 11 GB (8.5 GB left)", "30 min / 60 min (30 min left)", end, "", ""},
		{"nodata", capped, `used_download_bytes=?`, []any{10 * gb}, "10 GB / 10 GB (0 GB left)", "0 min / 60 min (60 min left)", end, "OUT_OF_BULK", "out of data"},
		{"notime", capped, `used_seconds=3600`, nil, "0 GB / 10 GB (10 GB left)", "60 min / 60 min (0 min left)", end, "OUT_OF_TIME", "out of time"},
		{"lapsed", capped, `subscription_date=?`, []any{t0.Unix() - 31*dayS}, "0 GB / 10 GB (10 GB left)", "0 min / 60 min (60 min left)", "2026-10-31 15:30", "NO_ACTIVE_SUBSCRIPTION", "window ended"},
		{"autorenew", auto, `subscription_date=?, used_download_bytes=?`, []any{t0.Unix() - 31*dayS, 10 * gb}, "10 GB / 10 GB (0 GB left)", "0 min / unlimited", "2026-10-31 15:30", "", "renews at next login"},
		{"expired", capped, `expires_at=?`, []any{t0.Unix() - 1}, "0 GB / 10 GB (10 GB left)", "0 min / 60 min (60 min left)", end, "EXPIRED", "expired"},
		{"inactive", capped, `active=0`, nil, "0 GB / 10 GB (10 GB left)", "0 min / 60 min (60 min left)", end, "INACTIVE_USER", "inactive"},
		{"unlimited", unl, `used_download_bytes=?, used_seconds=500`, []any{gb}, "1 GB / unlimited", "8.3 min / unlimited", end, "", ""},
	}
	ids := map[string]int64{}
	for _, c := range cases {
		id := f.member(c.plan, c.name, "password", "pw-"+c.name)
		ids[c.name] = id
		f.exec(`UPDATE member SET `+c.set+` WHERE id=?`, append(append([]any{}, c.args...), id)...)
	}
	check := func(where string, sel *goquery.Selection, c tc) {
		t.Helper()
		col := func(n string) string { return text(sel.Find(`[data-col="` + n + `"]`)) }
		if col("data") != c.data || col("time") != c.tm || col("window-end") != c.windowEnd {
			t.Errorf("%s/%s: data=%q time=%q end=%q\n   want %q %q %q", where, c.name, col("data"), col("time"), col("window-end"), c.data, c.tm, c.windowEnd)
		}
		if v := sel.AttrOr("data-verdict", "?"); v != c.verdict {
			t.Errorf("%s/%s: verdict %q, want %q", where, c.name, v, c.verdict)
		}
		if c.label != "" && !strings.Contains(col("verdict"), c.label) {
			t.Errorf("%s/%s: verdict text %q lacks %q", where, c.name, col("verdict"), c.label)
		}
		if c.label == "" && col("verdict") != "" && !strings.Contains(col("verdict"), "renew") {
			t.Errorf("%s/%s: unexpected verdict text %q", where, c.name, col("verdict"))
		}
	}
	list := f.c.Get("/admin/members?per=100").Doc(t)
	for _, c := range cases {
		// The trimmed list keeps the verdict code (row attribute) and the data cell only.
		row := list.Find(fmt.Sprintf(`tr[data-member][data-id="%d"]`, ids[c.name]))
		if v := row.AttrOr("data-verdict", "?"); v != c.verdict {
			t.Errorf("list/%s: verdict %q, want %q", c.name, v, c.verdict)
		}
		wantMain, wantLeft, _ := strings.Cut(c.data, " (")
		wantLeft = strings.TrimSuffix(wantLeft, ")")
		if got := text(row.Find(`[data-col="data"]`)); got != strings.TrimSpace(wantMain+" "+wantLeft) {
			t.Errorf("list/%s: data cell %q, want %q", c.name, got, strings.TrimSpace(wantMain+" "+wantLeft))
		}
		if got := text(row.Find(`[data-col="data-left"]`)); got != wantLeft {
			t.Errorf("list/%s: data-left %q, want %q", c.name, got, wantLeft)
		}
		check("page", f.c.Get(mpath(ids[c.name])).Doc(t).Find("[data-summary]"), c)
	}

	t.Run("matches Access-Request", func(t *testing.T) {
		nas := fakenas.New(f.s.Addrs.RadiusAuth, scalar[string](t, f.db, `SELECT radius_shared_secret FROM settings`))
		for _, c := range cases {
			resp, err := nas.Authenticate(c.name, "pw-"+c.name)
			if err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
			if c.verdict == "" {
				if resp.Code != radius.CodeAccessAccept {
					t.Errorf("%s: page says allowed, RADIUS answered %v", c.name, resp.Code)
				}
				continue
			}
			if msg := rfc2865.ReplyMessage_GetString(resp); resp.Code != radius.CodeAccessReject || msg != c.verdict {
				t.Errorf("%s: page says %s, RADIUS answered %v %q", c.name, c.verdict, resp.Code, msg)
			}
		}
	})
}
