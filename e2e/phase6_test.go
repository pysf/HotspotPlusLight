package e2e

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"hotspotpluslight/e2e/harness"
)

// p6 is the Phase 6 fixture: the Phase 3 fixture (fake clock, admin client, fake router, log capture)
// plus HTTP access to the PUBLIC listener, where the page API lives.
type p6 struct{ *p3 }

func newP6(t *testing.T) *p6 { return &p6{newP3(t)} }

// apiResp is a fully read public-listener response.
type apiResp struct {
	*http.Response
	Body []byte
}

// JSON decodes the body into a generic object.
func (r apiResp) JSON(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(r.Body, &m); err != nil {
		t.Fatalf("body is not a JSON object: %v\n%s", err, r.Body)
	}
	return m
}

func basic(user, pw string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pw))
}

// call sends a request to the public listener. headers are name, value pairs.
func (f *p6) call(method, path string, headers ...string) apiResp {
	f.t.Helper()
	req, err := http.NewRequest(method, "http://"+f.s.Addrs.HTTP+path, nil)
	if err != nil {
		f.t.Fatal(err)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return apiResp{resp, b}
}

// account is GET /api/v1/account with Basic credentials.
func (f *p6) account(user, pw string) apiResp {
	f.t.Helper()
	return f.call("GET", "/api/v1/account", "Authorization", basic(user, pw))
}

func obj(t *testing.T, m map[string]any, key string) map[string]any {
	t.Helper()
	o, ok := m[key].(map[string]any)
	if !ok {
		t.Fatalf("%q is not an object in %v", key, m)
	}
	return o
}

func num(t *testing.T, m map[string]any, key string) int64 {
	t.Helper()
	v, ok := m[key].(float64)
	if !ok {
		t.Fatalf("%q is not a number in %v", key, m)
	}
	return int64(v)
}

// wantProblem asserts an RFC 9457 problem+json response (E6.19) with the given status and code.
func wantProblem(t *testing.T, r apiResp, status int, code string) map[string]any {
	t.Helper()
	if r.StatusCode != status {
		t.Fatalf("status %d, want %d: %s", r.StatusCode, status, r.Body)
	}
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Fatalf("Content-Type %q, want application/problem+json", ct)
	}
	m := r.JSON(t)
	for _, k := range []string{"type", "title", "status", "detail", "code"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("problem lacks %q: %s", k, r.Body)
		}
	}
	if _, has := m["ok"]; has {
		t.Fatalf("problem has an ok field: %s", r.Body)
	}
	if m["code"] != code || int(m["status"].(float64)) != status || m["title"] != http.StatusText(status) || m["type"] != "about:blank" {
		t.Fatalf("problem %s, want status %d code %s", r.Body, status, code)
	}
	return m
}

func (f *p6) setSettings(kv ...string) {
	f.t.Helper()
	form := harness.FormValues(f.t, f.c.Get("/admin/settings").Doc(f.t), "form#settings-form")
	for i := 0; i+1 < len(kv); i += 2 {
		if kv[i+1] == "-" {
			form.Del(kv[i])
		} else {
			form.Set(kv[i], kv[i+1])
		}
	}
	if r := f.c.Send("PUT", "/admin/settings", form); r.StatusCode != 200 {
		f.t.Fatalf("settings: %d %s", r.StatusCode, r.Body)
	}
}

// E6.01 valid Basic credentials => 200, loginAllowed true, reason null (and the full body shape, PLAN 8.1).
func TestE6_01_AccountOK(t *testing.T) {
	f := newP6(t)
	p := f.plan("Basic", "speed_value", "2", "speed_unit", "Mbps", "bulk_value", "10", "bulk_unit", "GB", "duration_days", "30", "max_sessions", "2")
	f.member(p, "alice", "password", "secret1")
	r := f.account("alice", "secret1")
	if r.StatusCode != 200 || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("status %d, content type %q: %s", r.StatusCode, r.Header.Get("Content-Type"), r.Body)
	}
	m := r.JSON(t)
	if m["loginAllowed"] != true || m["reason"] != nil || m["message"] != nil {
		t.Fatalf("verdict: %s", r.Body)
	}
	if _, has := m["ok"]; has {
		t.Fatal("no ok envelope allowed")
	}
	pl := obj(t, m, "plan")
	if pl["name"] != "Basic" || num(t, pl, "speedKbps") != 2000 || num(t, pl, "durationDays") != 30 || num(t, pl, "maxSessions") != 2 {
		t.Errorf("plan: %v", pl)
	}
	d := obj(t, m, "data")
	if d["unlimited"] != false || num(t, d, "allowedBytes") != 10*gb || num(t, d, "usedDownloadBytes") != 0 ||
		num(t, d, "usedUploadBytes") != 0 || num(t, d, "remainingBytes") != 10*gb {
		t.Errorf("data: %v", d)
	}
	tm := obj(t, m, "time")
	if tm["unlimited"] != true || num(t, tm, "usedSeconds") != 0 {
		t.Errorf("time: %v", tm)
	}
	if m["validUntil"] != "2026-12-01T12:00:00Z" {
		t.Errorf("validUntil %v", m["validUntil"])
	}
	if v, present := m["expiresAt"]; !present || v != nil {
		t.Errorf("expiresAt must be present and null, got %v (present=%v)", v, present)
	}
	// Username is case-insensitive and trimmed like at the RADIUS edge (BR-1).
	if r := f.account(" ALICE ", "secret1"); r.StatusCode != 200 {
		t.Errorf("case-insensitive username: %d", r.StatusCode)
	}
	// An expiry set on the member is reported.
	f.exec(`UPDATE member SET expires_at=? WHERE username='alice'`, t0.Unix()+3600)
	if m := f.account("alice", "secret1").JSON(t); m["expiresAt"] != "2026-11-01T13:00:00Z" {
		t.Errorf("expiresAt %v", m["expiresAt"])
	}
}

// E6.02 every non-credential failure => 200, loginAllowed false, reason code + English message (checks 2-8 of PLAN 7.2).
func TestE6_02_ReasonsAre200(t *testing.T) {
	f := newP6(t)
	capped := f.plan("Capped", "bulk_value", "10", "bulk_unit", "GB", "time_limit_minutes", "60", "duration_days", "30")
	auto := f.plan("Auto", "bulk_value", "10", "bulk_unit", "GB", "duration_days", "30", "auto_resubscribe", "on")
	dayS := int64(24 * 3600)
	type tc struct {
		name, set string
		args      []any
		plan      int64
		reason    string
		message   string
	}
	cases := []tc{
		{"inactive", `active=0`, nil, capped, "INACTIVE_USER", "This account is disabled."},
		{"expired", `expires_at=?`, []any{t0.Unix() - 1}, capped, "EXPIRED", "This account has expired."},
		{"lapsed", `subscription_date=?`, []any{t0.Unix() - 31*dayS}, capped, "NO_ACTIVE_SUBSCRIPTION", "Your plan period has ended."},
		{"nodata", `used_download_bytes=?`, []any{10 * gb}, capped, "OUT_OF_BULK", "Your data allowance is used up."},
		{"notime", `used_seconds=3600`, nil, capped, "OUT_OF_TIME", "Your time allowance is used up."},
		// Auto-resubscribe: the next login renews, so the page must not say "ended" (read-only: nothing is renewed here).
		{"renewing", `subscription_date=?, used_download_bytes=?`, []any{t0.Unix() - 31*dayS, 10 * gb}, auto, "", ""},
	}
	for _, c := range cases {
		id := f.member(c.plan, c.name, "password", "pw-"+c.name)
		f.exec(`UPDATE member SET `+c.set+` WHERE id=?`, append(append([]any{}, c.args...), id)...)
		r := f.account(c.name, "pw-"+c.name)
		if r.StatusCode != 200 {
			t.Errorf("%s: status %d %s", c.name, r.StatusCode, r.Body)
			continue
		}
		m := r.JSON(t)
		if c.reason == "" {
			if m["loginAllowed"] != true || m["reason"] != nil {
				t.Errorf("%s: %s", c.name, r.Body)
			}
			continue
		}
		if m["loginAllowed"] != false || m["reason"] != c.reason || m["message"] != c.message {
			t.Errorf("%s: want %s %q, got %s", c.name, c.reason, c.message, r.Body)
		}
	}
	if n := scalar[int](t, f.db, `SELECT renewal_count FROM member WHERE username='renewing'`); n != 0 {
		t.Errorf("GET account must not renew (renewal_count=%d)", n)
	}
	// The same verdict as the router gives (one implementation).
	f.rejected("nodata", "pw-nodata", "OUT_OF_BULK")

	// No plan / plan removed (defensive checks; foreign keys off to create the state).
	id := f.member(capped, "orphan", "password", "pw-orphan")
	conn, err := f.db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(context.Background(), `PRAGMA foreign_keys=OFF`); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		plan            int64
		reason, message string
	}{{0, "NO_PLAN", "No internet plan is assigned to this account."}, {9999, "PLAN_REMOVED", "This plan has been removed."}} {
		if _, err := conn.ExecContext(context.Background(), `UPDATE member SET plan_id=? WHERE id=?`, c.plan, id); err != nil {
			t.Fatal(err)
		}
		r := f.account("orphan", "pw-orphan")
		m := r.JSON(t)
		if r.StatusCode != 200 || m["loginAllowed"] != false || m["reason"] != c.reason || m["message"] != c.message {
			t.Errorf("%s: %d %s", c.reason, r.StatusCode, r.Body)
		}
		if m["plan"] != nil {
			t.Errorf("%s: plan must be null: %v", c.reason, m["plan"])
		}
	}

	// The concurrent-session check is not part of the page verdict (PLAN 8.1): a member who is already online still gets true.
	idOnline := f.member(capped, "online", "password", "pw-online")
	f.openSession(idOnline, "online", "sid-1", t0)
	if m := f.account("online", "pw-online").JSON(t); m["loginAllowed"] != true {
		t.Errorf("online member: %v", m)
	}
}

// E6.03 wrong password and unknown user => identical 401 problem+json, no WWW-Authenticate.
func TestE6_03_UniformUnauthorized(t *testing.T) {
	f := newP6(t)
	f.member(f.plan("Basic"), "alice", "password", "secret1")
	wrong := f.account("alice", "nope")
	unknown := f.account("nobody", "secret1")
	wantProblem(t, wrong, 401, "INVALID_CREDENTIALS")
	wantProblem(t, unknown, 401, "INVALID_CREDENTIALS")
	if !bytes.Equal(wrong.Body, unknown.Body) {
		t.Errorf("bodies differ:\n%s\n%s", wrong.Body, unknown.Body)
	}
	want := `{"type":"about:blank","title":"Unauthorized","status":401,"detail":"Wrong username or password.","code":"INVALID_CREDENTIALS"}`
	var a, b any
	json.Unmarshal(wrong.Body, &a)
	json.Unmarshal([]byte(want), &b)
	if fmt.Sprint(a) != fmt.Sprint(b) {
		t.Errorf("body %s, want %s", wrong.Body, want)
	}
	missing := f.call("GET", "/api/v1/account")
	malformed := f.call("GET", "/api/v1/account", "Authorization", "Basic !!!not-base64")
	bearer := f.call("GET", "/api/v1/account", "Authorization", "Bearer abc")
	nocolon := f.call("GET", "/api/v1/account", "Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("alice")))
	for name, r := range map[string]apiResp{"wrong": wrong, "unknown": unknown, "missing": missing, "malformed": malformed, "bearer": bearer, "nocolon": nocolon} {
		wantProblem(t, r, 401, "INVALID_CREDENTIALS")
		if got := r.Header.Get("WWW-Authenticate"); got != "" {
			t.Errorf("%s: WWW-Authenticate %q must not be sent (no browser login prompt)", name, got)
		}
		if !bytes.Equal(r.Body, wrong.Body) {
			t.Errorf("%s: body differs: %s", name, r.Body)
		}
	}
}

// E6.04 invalid username characters => 401 INVALID_CREDENTIALS (BR-1).
func TestE6_04_InvalidUsername(t *testing.T) {
	f := newP6(t)
	f.member(f.plan("Basic"), "alice", "password", "secret1")
	for _, u := range []string{"alice@x", "al ice", "ali-ce", "alicé", "علی", "alice.", strings.Repeat("a", 65), ""} {
		r := f.account(u, "secret1")
		wantProblem(t, r, 401, "INVALID_CREDENTIALS")
	}
}

// E6.05 CORS on every route, OPTIONS preflight => 204.
func TestE6_05_CORS(t *testing.T) {
	f := newP6(t)
	f.member(f.plan("Basic"), "alice", "password", "secret1")
	check := func(what string, r apiResp) {
		t.Helper()
		if r.Header.Get("Access-Control-Allow-Origin") != "*" {
			t.Errorf("%s: Allow-Origin %q", what, r.Header.Get("Access-Control-Allow-Origin"))
		}
		if r.Header.Get("Access-Control-Allow-Headers") != "Authorization" {
			t.Errorf("%s: Allow-Headers %q", what, r.Header.Get("Access-Control-Allow-Headers"))
		}
		if r.Header.Get("Access-Control-Allow-Methods") != "GET, OPTIONS" {
			t.Errorf("%s: Allow-Methods %q", what, r.Header.Get("Access-Control-Allow-Methods"))
		}
	}
	paths := []string{"/api/v1/account", "/api/v1/config", "/api/v1/devices/aabbccddeeff/credentials", "/healthz", "/api/v1/nope"}
	for _, p := range paths {
		check("GET "+p, f.call("GET", p, "Origin", "http://192.168.88.1"))
		pre := f.call("OPTIONS", p, "Origin", "http://192.168.88.1", "Access-Control-Request-Method", "GET", "Access-Control-Request-Headers", "authorization")
		if pre.StatusCode != 204 || len(pre.Body) != 0 {
			t.Errorf("OPTIONS %s: %d %q", p, pre.StatusCode, pre.Body)
		}
		check("OPTIONS "+p, pre)
		if pre.Header.Get("Access-Control-Max-Age") != "600" {
			t.Errorf("OPTIONS %s: Max-Age %q", p, pre.Header.Get("Access-Control-Max-Age"))
		}
	}
	// Errors carry the headers too, or the page could not read them.
	check("401", f.account("alice", "bad"))
	check("200", f.account("alice", "secret1"))
	if f.account("alice", "secret1").Header.Get("Access-Control-Allow-Credentials") != "" {
		t.Error("no credentials mode: Allow-Credentials must not be set")
	}
}

// E6.06 account figures equal the admin list; unlimited flags; exhausted members still get 200 (BR-7, BR-8, BR-22).
func TestE6_06_FiguresMatchAdmin(t *testing.T) {
	f := newP6(t)
	capped := f.plan("Capped", "bulk_value", "10", "bulk_unit", "GB", "time_limit_minutes", "60")
	unl := f.plan("Free", "bulk_value", "0", "time_limit_minutes", "0")
	id1 := f.member(capped, "extra", "password", "pw")
	f.exec(`UPDATE member SET extra_bulk_bytes=?, used_download_bytes=?, used_upload_bytes=?, used_seconds=1800 WHERE id=?`, gb, 2*gb, gb/2, id1)
	f.member(unl, "free", "password", "pw")
	f.exec(`UPDATE member SET used_download_bytes=?, used_seconds=500 WHERE username='free'`, gb)
	id3 := f.member(capped, "empty", "password", "pw")
	f.exec(`UPDATE member SET used_download_bytes=? WHERE id=?`, 10*gb, id3)

	m := f.account("extra", "pw").JSON(t)
	d, tm := obj(t, m, "data"), obj(t, m, "time")
	if num(t, d, "allowedBytes") != 11*gb || num(t, d, "usedDownloadBytes") != 2*gb || num(t, d, "usedUploadBytes") != gb/2 ||
		num(t, d, "remainingBytes") != 17*gb/2 || d["unlimited"] != false {
		t.Errorf("data: %v", d)
	}
	if num(t, tm, "allowedSeconds") != 3600 || num(t, tm, "usedSeconds") != 1800 || num(t, tm, "remainingSeconds") != 1800 || tm["unlimited"] != false {
		t.Errorf("time: %v", tm)
	}
	list := f.c.Get("/admin/members?per=100").Doc(t)
	row := list.Find(fmt.Sprintf(`tr[data-member][data-id="%d"]`, id1))
	// The list shows "used / allowed" with a muted "N left" line; time lives on the member page only.
	if got, left := text(row.Find(`[data-col="data"]`)), text(row.Find(`[data-col="data-left"]`)); got != "2.5 GB / 11 GB 8.5 GB left" || left != "8.5 GB left" {
		t.Errorf("admin list data %q, left %q", got, left)
	}
	if got := text(f.c.Get(fmt.Sprintf("/admin/members/%d", id1)).Doc(t).Find(`[data-summary] [data-col="time"]`)); got != "30 min / 60 min (30 min left)" {
		t.Errorf("member page time %q", got)
	}

	m = f.account("free", "pw").JSON(t)
	d, tm = obj(t, m, "data"), obj(t, m, "time")
	if d["unlimited"] != true || tm["unlimited"] != true || num(t, d, "usedDownloadBytes") != gb || num(t, tm, "usedSeconds") != 500 {
		t.Errorf("unlimited plan: %v / %v", d, tm)
	}
	if got := text(list.Find(`tr[data-username="free"] [data-col="data"]`)); got != "1 GB / unlimited" {
		t.Errorf("admin list unlimited data %q", got)
	}

	r := f.account("empty", "pw")
	m = r.JSON(t)
	if r.StatusCode != 200 || m["loginAllowed"] != false || m["reason"] != "OUT_OF_BULK" || num(t, obj(t, m, "data"), "remainingBytes") != 0 {
		t.Errorf("exhausted member: %d %s", r.StatusCode, r.Body)
	}
}

// E6.07 MAC lookup: 403 DISABLED when off; on => credentials only for an active member (BR-3 formats, O8).
func TestE6_07_DeviceCredentials(t *testing.T) {
	f := newP6(t)
	p := f.plan("Basic")
	f.member(p, "alice", "password", "secret1", "mac", "AA:BB:CC:DD:EE:FF")
	f.member(p, "bob", "password", "pw-bob", "mac", "112233445566", "active", "-")
	path := "/api/v1/devices/%s/credentials"

	r := f.call("GET", fmt.Sprintf(path, "aabbccddeeff"))
	wantProblem(t, r, 403, "DISABLED")
	if r.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control %q", r.Header.Get("Cache-Control"))
	}
	f.setSettings("mac_auto_login", "on")

	for _, mac := range []string{"aabbccddeeff", "AA:BB:CC:DD:EE:FF", "aa-bb-cc-dd-ee-ff", "aabb.ccdd.eeff"} {
		r := f.call("GET", fmt.Sprintf(path, url.PathEscape(mac)))
		m := r.JSON(t)
		if r.StatusCode != 200 || m["username"] != "alice" || m["password"] != "secret1" || len(m) != 2 {
			t.Errorf("%s: %d %s", mac, r.StatusCode, r.Body)
		}
		if r.Header.Get("Cache-Control") != "no-store" {
			t.Errorf("%s: Cache-Control %q", mac, r.Header.Get("Cache-Control"))
		}
	}
	for _, mac := range []string{"112233445566" /* inactive */, "ffffffffffff" /* unknown */, "zz", "aabbcc" /* not 12 hex */} {
		wantProblem(t, f.call("GET", fmt.Sprintf(path, mac)), 404, "DEVICE_NOT_FOUND")
	}
	// Several members share a MAC: a newer member that can log in beats an older expired one.
	f.exec(`UPDATE member SET expires_at=1 WHERE username='alice'`)
	f.member(p, "carol", "password", "pw-carol", "mac", "aabbccddeeff")
	if m := f.call("GET", fmt.Sprintf(path, "aabbccddeeff")).JSON(t); m["username"] != "carol" || m["password"] != "pw-carol" {
		t.Errorf("shared MAC, newer valid member: %v", m)
	}
	f.exec(`UPDATE member SET expires_at=1 WHERE username='carol'`)
	if m := f.call("GET", fmt.Sprintf(path, "aabbccddeeff")).JSON(t); m["username"] != "carol" {
		t.Errorf("shared MAC, none allowed: newest expected: %v", m)
	}
	// Turning the setting off again takes effect at once.
	f.setSettings("mac_auto_login", "-")
	wantProblem(t, f.call("GET", fmt.Sprintf(path, "aabbccddeeff")), 403, "DISABLED")
}

// E6.08 config reflects settings changes immediately.
func TestE6_08_Config(t *testing.T) {
	f := newP6(t)
	r := f.call("GET", "/api/v1/config")
	m := r.JSON(t)
	if r.StatusCode != 200 || m["siteTitle"] != "HotspotPlus" || m["afterLoginUrl"] != "" || len(m) != 2 {
		t.Fatalf("defaults: %d %s", r.StatusCode, r.Body)
	}
	f.setSettings("site_title", "Cafe Wi-Fi", "after_login_url", "https://example.org/welcome")
	m = f.call("GET", "/api/v1/config").JSON(t)
	if m["siteTitle"] != "Cafe Wi-Fi" || m["afterLoginUrl"] != "https://example.org/welcome" {
		t.Errorf("after change: %v", m)
	}
}

// E6.09 throttle: repeated 401s from one source => 429 + Retry-After + TOO_MANY_ATTEMPTS; resets with the clock.
func TestE6_09_Throttle(t *testing.T) {
	f := newP6(t)
	f.member(f.plan("Basic"), "alice", "password", "secret1")
	for i := range 10 {
		wantProblem(t, f.account("alice", "bad"+strconv.Itoa(i)), 401, "INVALID_CREDENTIALS")
	}
	r := f.account("alice", "bad")
	wantProblem(t, r, 429, "TOO_MANY_ATTEMPTS")
	ra, err := strconv.Atoi(r.Header.Get("Retry-After"))
	if err != nil || ra < 1 || ra > 60 {
		t.Errorf("Retry-After %q", r.Header.Get("Retry-After"))
	}
	if r.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Error("429 needs CORS headers")
	}
	// Even the right password is refused while locked, and the MAC lookup is locked too.
	wantProblem(t, f.account("alice", "secret1"), 429, "TOO_MANY_ATTEMPTS")
	f.setSettings("mac_auto_login", "on")
	wantProblem(t, f.call("GET", "/api/v1/devices/aabbccddeeff/credentials"), 429, "TOO_MANY_ATTEMPTS")
	// config is not credentials-related and stays open.
	if f.call("GET", "/api/v1/config").StatusCode != 200 {
		t.Error("config must not be throttled")
	}
	f.fc.Advance(61 * time.Second)
	if r := f.account("alice", "secret1"); r.StatusCode != 200 {
		t.Fatalf("after the lock: %d %s", r.StatusCode, r.Body)
	}

	t.Run("MAC lookups that find nothing do not count as failures", func(t *testing.T) {
		for range 15 {
			wantProblem(t, f.call("GET", "/api/v1/devices/aabbccddeeff/credentials"), 404, "DEVICE_NOT_FOUND")
		}
		if r := f.account("alice", "secret1"); r.StatusCode != 200 {
			t.Errorf("account after unknown lookups: %d", r.StatusCode)
		}
	})

	t.Run("per source IP", func(t *testing.T) {
		other := &net.Dialer{LocalAddr: &net.TCPAddr{IP: net.ParseIP("127.0.0.2")}, Timeout: 2 * time.Second}
		conn, err := other.Dial("tcp", f.s.Addrs.HTTP)
		if err != nil {
			t.Skipf("cannot bind 127.0.0.2 on this OS: %v", err)
		}
		conn.Close()
		for range 10 {
			wantProblem(t, f.account("alice", "bad"), 401, "INVALID_CREDENTIALS")
		}
		wantProblem(t, f.account("alice", "bad"), 429, "TOO_MANY_ATTEMPTS")
		c := &http.Client{Transport: &http.Transport{DialContext: other.DialContext}, Timeout: 5 * time.Second}
		req, _ := http.NewRequest("GET", "http://"+f.s.Addrs.HTTP+"/api/v1/account", nil)
		req.Header.Set("Authorization", basic("alice", "secret1"))
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Errorf("another source IP must not be locked: %d", resp.StatusCode)
		}
	})
}

// E6.10 credentials never appear in the server log.
func TestE6_10_NoCredentialsInLogs(t *testing.T) {
	f := newP6(t)
	f.member(f.plan("Basic"), "carol", "password", "Zx9-very-secret", "mac", "aabbccddeeff")
	f.setSettings("mac_auto_login", "on")
	f.account("carol", "Zx9-very-secret")
	f.account("carol", "Wrong-Pass-123")
	f.call("GET", "/api/v1/devices/aabbccddeeff/credentials")
	f.call("GET", "/api/v1/account", "Authorization", basic("carol", "Zx9-very-secret")+"x")
	for range 12 { // reach the 429 path too
		f.account("carol", "Wrong-Pass-123")
	}
	logs := f.log.String()
	for _, secret := range []string{"Zx9-very-secret", "Wrong-Pass-123", base64.StdEncoding.EncodeToString([]byte("carol:Zx9-very-secret")),
		base64.StdEncoding.EncodeToString([]byte("carol:Wrong-Pass-123"))} {
		if strings.Contains(logs, secret) {
			t.Errorf("the log contains %q", secret)
		}
	}
}

// E6.19 problem+json everywhere, no ok field, no-store, 404 and 405 are problems too.
func TestE6_19_ProblemJSONAndHeaders(t *testing.T) {
	f := newP6(t)
	f.member(f.plan("Basic"), "alice", "password", "secret1")
	wantProblem(t, f.call("GET", "/api/v1/nope"), 404, "NOT_FOUND")
	wantProblem(t, f.call("GET", "/nothing-here"), 404, "NOT_FOUND")
	wantProblem(t, f.call("GET", "/admin/members"), 404, "NOT_FOUND") // admin routes do not exist here (D23)
	r := f.call("POST", "/api/v1/account", "Authorization", basic("alice", "secret1"))
	wantProblem(t, r, 405, "METHOD_NOT_ALLOWED")
	if !strings.Contains(r.Header.Get("Allow"), "GET") {
		t.Errorf("Allow %q", r.Header.Get("Allow"))
	}
	for _, c := range []struct {
		name string
		r    apiResp
	}{
		{"account 200", f.account("alice", "secret1")},
		{"account 401", f.account("alice", "x")},
		{"credentials 403", f.call("GET", "/api/v1/devices/aabbccddeeff/credentials")},
	} {
		if cc := c.r.Header.Get("Cache-Control"); cc != "no-store" {
			t.Errorf("%s: Cache-Control %q", c.name, cc)
		}
	}
	// healthz is plain text, not JSON.
	h := f.call("GET", "/healthz")
	if h.StatusCode != 200 || strings.TrimSpace(string(h.Body)) != "ok" {
		t.Errorf("healthz %d %q", h.StatusCode, h.Body)
	}
	// And the other way round: page-API routes do not exist on the admin listener.
	resp, body := harness.Get(t, "http://"+f.s.Addrs.Admin+"/api/v1/config")
	if resp.StatusCode != 404 {
		t.Errorf("admin listener served /api/v1/config: %d %s", resp.StatusCode, body)
	}
	resp, _ = harness.Get(t, "http://"+f.s.Addrs.HTTP+"/admin/router/files.zip")
	if resp.StatusCode != 404 {
		t.Errorf("public listener served the pack download: %d", resp.StatusCode)
	}
}

// ---- admin side: starter pack and themes ----

// pack downloads files.zip (optionally for a theme) and returns its entries by name.
func (f *p6) pack(theme string) (resp harness.Resp, files map[string][]byte) {
	f.t.Helper()
	path := "/admin/router/files.zip"
	if theme != "" {
		path += "?theme=" + url.QueryEscape(theme)
	}
	resp = f.c.Get(path)
	if resp.StatusCode != 200 {
		return resp, nil
	}
	zr, err := zip.NewReader(bytes.NewReader([]byte(resp.Body)), int64(len(resp.Body)))
	if err != nil {
		f.t.Fatalf("%s: not a zip: %v", path, err)
	}
	files = map[string][]byte{}
	for _, zf := range zr.File {
		rc, err := zf.Open()
		if err != nil {
			f.t.Fatal(err)
		}
		b, _ := io.ReadAll(rc)
		rc.Close()
		files[zf.Name] = b
	}
	return resp, files
}

var packFiles = []string{"login.html", "alogin.html", "status.html", "logout.html", "error.html", "redirect.html", "rlogin.html", "background.jpg", "logo.png"}

// reasonCodes are all message codes the page must be able to translate (PLAN 7.6 plus the API's own codes).
var reasonCodes = []string{"INVALID_CREDENTIALS", "INACTIVE_USER", "NO_PLAN", "PLAN_REMOVED", "EXPIRED", "NO_ACTIVE_SUBSCRIPTION",
	"OUT_OF_BULK", "OUT_OF_TIME", "NO_MORE_SESSION", "UNSUPPORTED_AUTH_METHOD", "GENERAL_ERROR", "TOO_MANY_ATTEMPTS"}

// E6.14 theme registry: exactly 38 themes in 6 categories, manifests, backgrounds and colors as in PLAN section 16.
func TestE6_14_ThemeRegistry(t *testing.T) {
	f := newP6(t)
	if len(expectedThemes) != 38 {
		t.Fatalf("test table has %d themes", len(expectedThemes))
	}
	d := f.c.Get("/admin/router/themes").Doc(t)
	if n := d.Find("[data-category]").Length(); n != 6 {
		t.Fatalf("%d categories, want 6", n)
	}
	if n := d.Find("[data-theme]").Length(); n != 38 {
		t.Fatalf("%d themes listed, want 38", n)
	}
	for _, e := range expectedThemes {
		card := d.Find(fmt.Sprintf(`[data-category="%s"] [data-theme="%s"]`, e.Category, e.ID))
		if card.Length() != 1 {
			t.Errorf("%s: %d cards under category %s", e.ID, card.Length(), e.Category)
			continue
		}
		if got := text(card.Find("[data-title]")); got != e.Title {
			t.Errorf("%s: title %q, want %q", e.ID, got, e.Title)
		}
		if got := card.AttrOr("data-primary", ""); got != e.Primary {
			t.Errorf("%s: primary %q, want %q", e.ID, got, e.Primary)
		}
		if got := card.AttrOr("data-overlay", ""); got != e.Overlay {
			t.Errorf("%s: overlay %q, want %q", e.ID, got, e.Overlay)
		}
		bg := f.c.Get(card.Find("img").AttrOr("src", "/missing"))
		if bg.StatusCode != 200 || len(bg.Body) != e.BackgroundBytes || !strings.HasPrefix(bg.Body, "\xff\xd8\xff") || bg.Header.Get("Content-Type") != "image/jpeg" {
			t.Errorf("%s: background %d, %d bytes (want %d), type %q", e.ID, bg.StatusCode, len(bg.Body), e.BackgroundBytes, bg.Header.Get("Content-Type"))
		}
	}
}

// E6.15 each of the 38 downloads is a valid zip with that theme's colors, background and the shared layout.
// Also E6.11 (lint) and E6.18 (disconnected hint) for every theme.
func TestE6_15_ThemeDownloads(t *testing.T) {
	f := newP6(t)
	f.setSettings("public_base_url", "http://192.168.88.2:8080")
	var shared string
	for _, e := range expectedThemes {
		resp, files := f.pack(e.ID)
		if resp.StatusCode != 200 {
			t.Errorf("%s: %d", e.ID, resp.StatusCode)
			continue
		}
		if ct := resp.Header.Get("Content-Type"); ct != "application/zip" {
			t.Errorf("%s: Content-Type %q", e.ID, ct)
		}
		if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "attachment") || !strings.Contains(cd, e.ID+".zip") {
			t.Errorf("%s: Content-Disposition %q", e.ID, cd)
		}
		for _, name := range packFiles {
			if len(files["hotspot/"+name]) == 0 {
				t.Errorf("%s: hotspot/%s missing or empty", e.ID, name)
			}
		}
		if len(files["hotspot/background.jpg"]) != e.BackgroundBytes {
			t.Errorf("%s: background.jpg %d bytes, want %d", e.ID, len(files["hotspot/background.jpg"]), e.BackgroundBytes)
		}
		login := string(files["hotspot/login.html"])
		// colors as CSS custom properties at the top (PLAN 9.4)
		if !strings.Contains(login, "--primary-color: "+e.Primary+";") || !strings.Contains(login, "--overlay-color: "+e.Overlay+";") {
			t.Errorf("%s: colors not in login.html", e.ID)
		}
		if !strings.Contains(login, `url("background.jpg")`) && !strings.Contains(login, `url(background.jpg)`) {
			t.Errorf("%s: login.html does not use background.jpg", e.ID)
		}
		// shared layout elements (T2)
		for _, id := range []string{"logo", "site-title", "login-form", "footer", "error-message"} {
			if !strings.Contains(login, `id="`+id+`"`) {
				t.Errorf("%s: layout element #%s missing", e.ID, id)
			}
		}
		for _, field := range []string{`name="username"`, `name="password"`, `type="submit"`} {
			if !strings.Contains(login, field) {
				t.Errorf("%s: form lacks %s", e.ID, field)
			}
		}
		// E6.11 lint
		for _, tok := range []string{"{{", "}}", "{%", "%}", "<no value>", "%!"} {
			if strings.Contains(login, tok) {
				t.Errorf("%s: unresolved template token %q", e.ID, tok)
			}
		}
		if !strings.Contains(login, `HSP_API_BASE = "http://192.168.88.2:8080"`) {
			t.Errorf("%s: API base not baked from public_base_url", e.ID)
		}
		for _, code := range reasonCodes {
			if n := len(regexp.MustCompile(`(?m)^\s*`+code+`:\s*"`).FindAllString(login, -1)); n != 1 {
				t.Errorf("%s: %s has %d dictionary entries, want 1 (English only)", e.ID, code, n)
			}
		}
		if regexp.MustCompile(`[\x{0600}-\x{06FF}]`).MatchString(login) {
			t.Errorf("%s: the page contains Arabic-script text; it ships English only", e.ID)
		}
		// E6.18 the disconnected hint and its dictionary key
		if !strings.Contains(login, "If you were disconnected, your plan's data or time may be used up. Try logging in again to see why.") {
			t.Errorf("%s: hint line missing", e.ID)
		}
		if n := len(regexp.MustCompile(`(?m)^\s*DISCONNECTED_HINT:\s*"`).FindAllString(login, -1)); n != 1 {
			t.Errorf("%s: DISCONNECTED_HINT has %d dictionary entries, want 1", e.ID, n)
		}
		if !strings.Contains(login, `data-i18n="DISCONNECTED_HINT"`) {
			t.Errorf("%s: hint not bound to its key", e.ID)
		}
		// The themes share ONE layout: apart from the two colors the markup is identical.
		neutral := strings.NewReplacer(e.Primary, "P", e.Overlay, "O").Replace(login)
		if shared == "" {
			shared = neutral
		} else if neutral != shared {
			t.Errorf("%s: login.html differs from the shared layout beyond the colors", e.ID)
		}
		// router pages: no server tokens, and nothing that leaks a password into a URL (PLAN 9.4).
		for _, name := range packFiles {
			if strings.HasSuffix(name, ".html") {
				if body := string(files["hotspot/"+name]); strings.Contains(body, "plain-passwd") || strings.Contains(body, "{{") {
					t.Errorf("%s: %s contains plain-passwd or a template token", e.ID, name)
				}
			}
		}
	}
}

// E6.16 no theme => alpha-fall; unknown id => 404.
func TestE6_16_DefaultAndUnknownTheme(t *testing.T) {
	f := newP6(t)
	_, def := f.pack("")
	_, fall := f.pack("alpha-fall")
	if len(def) == 0 || !bytes.Equal(def["hotspot/login.html"], fall["hotspot/login.html"]) || !bytes.Equal(def["hotspot/background.jpg"], fall["hotspot/background.jpg"]) {
		t.Error("default pack is not alpha-fall")
	}
	if _, other := f.pack("alpha-space"); bytes.Equal(other["hotspot/background.jpg"], fall["hotspot/background.jpg"]) {
		t.Error("different themes must differ")
	}
	for _, id := range []string{"nope", "alpha-nope", "../alpha-fall", "Alpha-Fall"} {
		if r, _ := f.pack(id); r.StatusCode != 404 {
			t.Errorf("theme %q: %d, want 404", id, r.StatusCode)
		}
	}
	if r := f.c.Get("/admin/router/themes/nope"); r.StatusCode != 404 {
		t.Errorf("detail of an unknown theme: %d", r.StatusCode)
	}
	// Admin only.
	anon := harness.NewAdminClient(t, f.s)
	for _, p := range []string{"/admin/router", "/admin/router/themes", "/admin/router/files.zip"} {
		if r := anon.Get(p); r.StatusCode != 303 || r.Header.Get("Location") != "/admin/login" {
			t.Errorf("anonymous %s: %d %s", p, r.StatusCode, r.Header.Get("Location"))
		}
	}
}

// E6.17 the gallery lists all 38 grouped by category, and every preview renders.
func TestE6_17_Gallery(t *testing.T) {
	f := newP6(t)
	d := f.c.Get("/admin/router/themes").Doc(t)
	counts := map[string]int{"Alpha": 3, "Restaurant": 5, "Cafe": 7, "Hotel": 9, "University": 7, "Market": 7}
	for cat, n := range counts {
		if got := d.Find(fmt.Sprintf(`[data-category="%s"] [data-theme]`, cat)).Length(); got != n {
			t.Errorf("category %s lists %d themes, want %d", cat, got, n)
		}
	}
	for _, e := range expectedThemes {
		card := d.Find(fmt.Sprintf(`[data-theme="%s"]`, e.ID))
		href := card.Find("a[data-detail]").AttrOr("href", "")
		if href != "/admin/router/themes/"+e.ID {
			t.Errorf("%s: detail link %q", e.ID, href)
			continue
		}
		page := f.c.Get(href)
		if page.StatusCode != 200 {
			t.Errorf("%s: detail page %d", e.ID, page.StatusCode)
			continue
		}
		pd := page.Doc(t)
		if dl := pd.Find("a[data-download]").AttrOr("href", ""); dl != "/admin/router/files.zip?theme="+e.ID {
			t.Errorf("%s: download link %q", e.ID, dl)
		}
		prev := f.c.Get(pd.Find("iframe[data-preview]").AttrOr("src", "/missing"))
		if prev.StatusCode != 200 || !strings.HasPrefix(prev.Header.Get("Content-Type"), "text/html") || !strings.Contains(prev.Body, e.Primary) {
			t.Errorf("%s: preview %d %q", e.ID, prev.StatusCode, prev.Header.Get("Content-Type"))
		}
		// The preview runs on the admin origin: sandboxed (iframe attribute and CSP), no API base, no network.
		if got := pd.Find("iframe[data-preview]").AttrOr("sandbox", ""); got != "allow-scripts" {
			t.Errorf("%s: iframe sandbox %q", e.ID, got)
		}
		csp := prev.Header.Get("Content-Security-Policy")
		for _, want := range []string{"sandbox allow-scripts", "default-src 'none'", "connect-src 'none'", "img-src 'self'"} {
			if !strings.Contains(csp, want) {
				t.Errorf("%s: preview CSP %q lacks %q", e.ID, csp, want)
			}
		}
		if strings.Contains(csp, "allow-same-origin") || !strings.Contains(prev.Body, `HSP_API_BASE = ""`) {
			t.Errorf("%s: preview must be opaque-origin and without API base", e.ID)
		}
		// The preview's relative references resolve next to it.
		src := pd.Find("iframe[data-preview]").AttrOr("src", "")
		base := src[:strings.LastIndex(src, "/")+1]
		for _, asset := range []string{"background.jpg", "logo.png"} {
			if r := f.c.Get(base + asset); r.StatusCode != 200 || len(r.Body) == 0 {
				t.Errorf("%s: preview asset %s: %d", e.ID, asset, r.StatusCode)
			}
		}
	}
}

// E6.20 /admin/router: entry page with the download and gallery links, and a hint when public_base_url is unset.
func TestE6_20_RouterPage(t *testing.T) {
	f := newP6(t)
	r := f.c.Get("/admin/router")
	if r.StatusCode != 200 {
		t.Fatalf("status %d", r.StatusCode)
	}
	d := r.Doc(t)
	if d.Find(`a[href="/admin/router/files.zip"]`).Length() != 1 || d.Find(`a[href="/admin/router/themes"]`).Length() != 1 {
		t.Error("download and gallery links missing")
	}
	if d.Find("[data-warning=public-base-url]").Length() != 1 {
		t.Error("public_base_url unset: a warning is expected")
	}
	f.setSettings("public_base_url", "http://192.168.88.2:8080")
	if f.c.Get("/admin/router").Doc(t).Find("[data-warning=public-base-url]").Length() != 0 {
		t.Error("warning should disappear once public_base_url is set")
	}
}

// E6.12 starter page script against a router stub in a headless browser (optional per PLAN 10).
func TestE6_12_StarterPageScript(t *testing.T) {
	t.Skip("optional (PLAN 10, 11): needs a headless browser (Playwright), which this repository does not vendor; the script is covered by lint tests E6.11/E6.15/E6.18 and by the manual hardware checklist (PLAN 13.9)")
}
