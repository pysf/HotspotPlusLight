package e2e

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hotspotpluslight/e2e/harness"
)

// fakeGitHub answers the "latest release" call. Tests change the tag or the status while it runs.
type fakeGitHub struct {
	*httptest.Server
	hits   atomic.Int32
	mu     sync.Mutex
	tag    string
	status int
	agent  string
	path   string
	files  map[string][]byte // release files by name, served at /dl/<name>
	gate   chan struct{}     // when set, the download of a release file other than SHA256SUMS waits until it is closed
	dlHits atomic.Int32      // requests for release files
}

func newFakeGitHub(t *testing.T, tag string) *fakeGitHub {
	t.Helper()
	f := &fakeGitHub{tag: tag, status: 200, files: map[string][]byte{"SHA256SUMS": nil}}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if name, ok := strings.CutPrefix(r.URL.Path, "/dl/"); ok {
			f.serveFile(w, r, name)
			return
		}
		f.hits.Add(1)
		f.mu.Lock()
		tag, status := f.tag, f.status
		f.agent, f.path = r.UserAgent(), r.URL.Path
		f.mu.Unlock()
		if status != 200 {
			w.WriteHeader(status)
			return
		}
		fmt.Fprintf(w, `{"tag_name":%q,"html_url":"https://example.test/releases/%s","body":"notes","assets":[%s]}`, tag, tag, f.assetJSON())
	}))
	t.Cleanup(f.Close)
	return f
}

// assetJSON lists the release files with their download links.
func (f *fakeGitHub) assetJSON() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var items []string
	for name := range f.files {
		items = append(items, fmt.Sprintf(`{"name":%q,"browser_download_url":%q}`, name, f.URL+"/dl/"+name))
	}
	return strings.Join(items, ",")
}

func (f *fakeGitHub) serveFile(w http.ResponseWriter, r *http.Request, name string) {
	f.dlHits.Add(1)
	f.mu.Lock()
	data, ok := f.files[name]
	gate := f.gate
	f.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	if gate != nil && name != "SHA256SUMS" {
		select {
		case <-gate:
		case <-r.Context().Done():
			return
		}
	}
	w.Write(data)
}

func (f *fakeGitHub) set(tag string, status int) {
	f.mu.Lock()
	f.tag, f.status = tag, status
	f.mu.Unlock()
}

// updateServer starts the app with a version and the fake GitHub. delay 0 means no background check within the test.
func updateServer(t *testing.T, f *fakeGitHub, version string, delay time.Duration) (*harness.Server, *harness.AdminClient) {
	t.Helper()
	s := harness.Start(t, harness.Options{Version: version, UpdateFirstDelay: delay, Env: map[string]string{"UPDATE_API_URL": f.URL}})
	return s, harness.LoginAsAdmin(t, s)
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatalf("timeout: %s", what)
}

func hasNotice(c *harness.AdminClient, t *testing.T, path string) bool {
	t.Helper()
	d := c.GetPage(path).Doc(t)
	return d.Find(`[data-update-notice] a[href="/admin/update"]`).Length() == 1
}

func setCheckUpdates(t *testing.T, c *harness.AdminClient, v string) {
	t.Helper()
	form := harness.FormValues(t, c.Get("/admin/settings").Doc(t), "form#settings-form")
	form.Set("check_updates", v)
	if r := c.Send("PUT", "/admin/settings", form); r.StatusCode != 200 {
		t.Fatalf("save settings: %d", r.StatusCode)
	}
}

// E22.01 a newer release: the background job finds it, every page shows the notice, the page shows both versions and the notes link.
func TestE22_01_NewerVersionShown(t *testing.T) {
	f := newFakeGitHub(t, "v1.2.0")
	s, c := updateServer(t, f, "v1.0.0", 30*time.Millisecond)
	db := harness.DB(t, s.DataDir)
	waitFor(t, "stored result", func() bool { return scalar[string](t, db, `SELECT update_latest FROM settings`) == "v1.2.0" })

	for _, p := range []string{"/admin", "/admin/members", "/admin/settings", "/admin/update"} {
		if !hasNotice(c, t, p) {
			t.Errorf("%s: no update notice", p)
		}
	}
	if txt := c.GetPage("/admin").Doc(t).Find("[data-update-notice]").Text(); !strings.Contains(txt, "Version v1.2.0 is available") {
		t.Errorf("notice text %q", txt)
	}
	d := c.GetPage("/admin/update").Doc(t)
	for key, want := range map[string]string{"current": "v1.0.0", "latest": "v1.2.0"} {
		if got := strings.TrimSpace(d.Find(`[data-key="` + key + `"]`).Text()); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if href := d.Find("a[data-release-notes]").AttrOr("href", ""); href != "https://example.test/releases/v1.2.0" {
		t.Errorf("release notes link %q", href)
	}
	if got := strings.TrimSpace(d.Find(`[data-key="checked"]`).Text()); got == "" || got == "Not yet" {
		t.Errorf("checked time %q", got)
	}
	f.mu.Lock()
	agent, path := f.agent, f.path
	f.mu.Unlock()
	if agent != "HotspotPlusLight/v1.0.0" || path != "/repos/pysf/HotspotPlusLight/releases/latest" {
		t.Errorf("request: agent %q path %q", agent, path)
	}
}

// E22.02 the same or an older release: no notice.
func TestE22_02_NoNoticeWhenCurrent(t *testing.T) {
	for _, tag := range []string{"v1.0.0", "v0.9.0"} {
		f := newFakeGitHub(t, tag)
		_, c := updateServer(t, f, "v1.0.0", 0)
		r := c.Send("POST", "/admin/update/check", nil)
		if r.StatusCode != 200 || f.hits.Load() != 1 {
			t.Fatalf("%s: check %d hits %d", tag, r.StatusCode, f.hits.Load())
		}
		if hasNotice(c, t, "/admin") || hasNotice(c, t, "/admin/update") {
			t.Errorf("%s: notice shown", tag)
		}
		if txt := c.GetPage("/admin/update").Doc(t).Find("[data-update-status]").Text(); !strings.Contains(txt, "latest version") {
			t.Errorf("%s: page %q", tag, txt)
		}
	}
}

// E22.03 with the setting off, neither the job nor Check now calls GitHub, and the page says checks are off.
func TestE22_03_SettingOffMakesNoCall(t *testing.T) {
	f := newFakeGitHub(t, "v1.2.0")
	s, c := updateServer(t, f, "v1.0.0", 0)
	setCheckUpdates(t, c, "off")
	if v := scalar[int](t, harness.DB(t, s.DataDir), `SELECT check_updates FROM settings`); v != 0 {
		t.Fatalf("check_updates = %d", v)
	}
	r := c.SendHX("POST", "/admin/update/check", nil, "update-status")
	if r.StatusCode != 200 || !strings.Contains(r.Body, "Update checks are off") {
		t.Errorf("check now while off: %d %q", r.StatusCode, r.Body)
	}
	d := c.GetPage("/admin/update").Doc(t)
	if !strings.Contains(d.Find("[data-update-status]").Text(), "Update checks are off") || d.Find(`button[hx-post="/admin/update/check"]`).Length() != 0 {
		t.Error("page must say checks are off and hide Check now")
	}
	if hasNotice(c, t, "/admin") {
		t.Error("notice shown while off")
	}

	// A restart with a short delay: the job runs, reads the setting and stays quiet.
	s.Stop()
	_, c2 := func() (*harness.Server, *harness.AdminClient) {
		s2 := harness.Start(t, harness.Options{DataDir: s.DataDir, SecretKey: s.SecretKey, Version: "v1.0.0", UpdateFirstDelay: 30 * time.Millisecond, Env: map[string]string{"UPDATE_API_URL": f.URL}})
		return s2, harness.LoginAsAdmin(t, s2)
	}()
	time.Sleep(400 * time.Millisecond)
	if n := f.hits.Load(); n != 0 {
		t.Fatalf("fake GitHub got %d requests, want 0", n)
	}

	// Turning it on applies at once, without a restart.
	setCheckUpdates(t, c2, "on")
	if r := c2.SendHX("POST", "/admin/update/check", nil, "update-status"); r.StatusCode != 200 || f.hits.Load() != 1 || !strings.Contains(r.Body, "v1.2.0") {
		t.Errorf("after on: %d hits %d", r.StatusCode, f.hits.Load())
	}
}

// E22.04 Check now shows the fresh answer in the block it replaces.
func TestE22_04_CheckNow(t *testing.T) {
	f := newFakeGitHub(t, "v1.0.0")
	_, c := updateServer(t, f, "v1.0.0", 0)
	if !strings.Contains(c.GetPage("/admin/update").Doc(t).Find("[data-key=checked]").Text(), "Not yet") {
		t.Error("before the first check the page must say Not yet")
	}
	d := c.GetPage("/admin/update").Doc(t)
	btn := d.Find(`button[hx-post="/admin/update/check"]`)
	if btn.Length() != 1 || btn.AttrOr("hx-target", "") != "#update-status" || d.Find("#update-status").Length() != 1 {
		t.Fatal("Check now button or its target is missing")
	}
	f.set("v1.3.0", 200)
	r := c.SendHX("POST", "/admin/update/check", nil, "update-status")
	if r.StatusCode != 200 || strings.Contains(r.Body, "<html") || !strings.Contains(r.Body, `id="update-status"`) || !strings.Contains(r.Body, "v1.3.0") {
		t.Fatalf("check now: %d %q", r.StatusCode, r.Body)
	}
	if !strings.Contains(r.Header.Get("HX-Trigger"), "toast") {
		t.Errorf("HX-Trigger %q", r.Header.Get("HX-Trigger"))
	}
	// The layout notice follows the answer without a reload (it was stale after Check now).
	if !strings.Contains(r.Body, `id="update-notice"`) || !strings.Contains(r.Body, `hx-swap-oob="true"`) || !strings.Contains(r.Body, "data-update-notice") {
		t.Errorf("the answer must swap the layout notice out of band: %q", r.Body)
	}
	if c.GetPage("/admin/update").Doc(t).Find("#update-notice").Length() != 1 {
		t.Error("the page needs a stable #update-notice slot")
	}
	if !hasNotice(c, t, "/admin/members") {
		t.Error("notice missing after the check")
	}
	// A POST without the CSRF token is refused.
	if r := c.Do("POST", "/admin/update/check", nil, ""); r.StatusCode != 403 {
		t.Errorf("no CSRF: %d", r.StatusCode)
	}
}

// E22.05 a GitHub error shows a friendly text, the app keeps working, and the next good answer clears it.
func TestE22_05_GitHubError(t *testing.T) {
	f := newFakeGitHub(t, "v1.0.0")
	f.set("", 500)
	s, c := updateServer(t, f, "v1.0.0", 0)
	r := c.SendHX("POST", "/admin/update/check", nil, "update-status")
	if r.StatusCode != 200 || !strings.Contains(r.Body, "The last check failed") || strings.Contains(r.Body, "500 Internal") {
		t.Fatalf("error check: %d %q", r.StatusCode, r.Body)
	}
	if e := scalar[string](t, harness.DB(t, s.DataDir), `SELECT update_error FROM settings`); e == "" {
		t.Error("error not stored")
	}
	for _, p := range []string{"/admin", "/admin/members", "/admin/update", "/admin/settings"} {
		if got := c.GetPage(p).StatusCode; got != 200 {
			t.Errorf("%s: %d", p, got)
		}
	}
	f.set("v1.1.0", 200)
	if r := c.SendHX("POST", "/admin/update/check", nil, "update-status"); strings.Contains(r.Body, "The last check failed") || !strings.Contains(r.Body, "v1.1.0") {
		t.Errorf("after recovery: %q", r.Body)
	}
	// A broken answer is also a friendly error.
	f.set("not-a-version", 200)
	if r := c.SendHX("POST", "/admin/update/check", nil, "update-status"); r.StatusCode != 200 || !strings.Contains(r.Body, "The last check failed") {
		t.Errorf("bad tag: %d %q", r.StatusCode, r.Body)
	}
}

// E22.06 a dev build never calls GitHub and never offers an update.
func TestE22_06_DevBuildNeverChecks(t *testing.T) {
	for _, version := range []string{"", "dev"} {
		f := newFakeGitHub(t, "v9.0.0")
		_, c := updateServer(t, f, version, 30*time.Millisecond)
		time.Sleep(300 * time.Millisecond)
		r := c.SendHX("POST", "/admin/update/check", nil, "update-status")
		if r.StatusCode != 200 || !strings.Contains(r.Body, "development build") {
			t.Errorf("%q: check now %d %q", version, r.StatusCode, r.Body)
		}
		if f.hits.Load() != 0 || hasNotice(c, t, "/admin") {
			t.Errorf("%q: hits %d", version, f.hits.Load())
		}
	}
}

// E22.07 the setting is a switch on the Settings page, on by default, and a save without the field keeps it.
func TestE22_07_SettingForm(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	c := harness.LoginAsAdmin(t, s)
	db := harness.DB(t, s.DataDir)
	if scalar[int](t, db, `SELECT check_updates FROM settings`) != 1 {
		t.Fatal("default must be on")
	}
	d := c.Get("/admin/settings").Doc(t)
	if d.Find(`form#settings-form input[type="checkbox"][name="check_updates"][checked]`).Length() != 1 || d.Find(`a[href="/admin/update"]`).Length() < 1 {
		t.Error("switch (checked) or link to the Updates page missing")
	}
	setCheckUpdates(t, c, "off")
	d = c.Get("/admin/settings").Doc(t)
	if d.Find(`input[type="checkbox"][name="check_updates"][checked]`).Length() != 0 || scalar[int](t, db, `SELECT check_updates FROM settings`) != 0 {
		t.Error("off not saved")
	}
	// Unchecking in a browser sends only the hidden "off"; checking sends "on" first.
	form := harness.FormValues(t, d, "form#settings-form")
	form.Del("check_updates")
	if r := c.Send("PUT", "/admin/settings", form); r.StatusCode != 200 || scalar[int](t, db, `SELECT check_updates FROM settings`) != 0 {
		t.Errorf("a save without the field must keep the value: %d", r.StatusCode)
	}
	form.Set("check_updates", "on")
	form.Add("check_updates", "off")
	if r := c.Send("PUT", "/admin/settings", form); r.StatusCode != 200 || scalar[int](t, db, `SELECT check_updates FROM settings`) != 1 {
		t.Errorf("on: %d", r.StatusCode)
	}
}

// E22.08 the update page is behind the admin login.
func TestE22_08_NeedsLogin(t *testing.T) {
	s := harness.Start(t, harness.Options{})
	c := harness.NewAdminClient(t, s)
	if r := c.GetPage("/admin/update"); r.StatusCode != 303 || r.Header.Get("Location") != "/admin/login" {
		t.Errorf("anonymous page: %d %q", r.StatusCode, r.Header.Get("Location"))
	}
	if r := c.Do("POST", "/admin/update/check", nil, ""); r.StatusCode == 200 {
		t.Errorf("anonymous check: %d", r.StatusCode)
	}
}
