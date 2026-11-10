package e2e

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"hotspotpluslight/e2e/harness"
	"hotspotpluslight/internal/domain"
)

const oldBinary = "OLD-BINARY"

// platformArchive builds the release archive for this OS and CPU holding a fake program file.
func platformArchive(t *testing.T, content string) (name string, data []byte) {
	t.Helper()
	name = domain.AssetName(runtime.GOOS, runtime.GOARCH)
	var buf bytes.Buffer
	if runtime.GOOS == "windows" {
		zw := zip.NewWriter(&buf)
		for _, e := range [][2]string{{"README.md", "readme"}, {"hotspotpluslight.exe", content}} {
			w, err := zw.Create(e[0])
			if err != nil {
				t.Fatal(err)
			}
			w.Write([]byte(e[1]))
		}
		zw.Close()
		return name, buf.Bytes()
	}
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range [][2]string{{"README.md", "readme"}, {"hotspotpluslight", content}} {
		tw.WriteHeader(&tar.Header{Name: e[0], Mode: 0o755, Size: int64(len(e[1])), Typeflag: tar.TypeReg})
		tw.Write([]byte(e[1]))
	}
	tw.Close()
	gz.Close()
	return name, buf.Bytes()
}

// publish puts a release with a fake new program file and a correct SHA256SUMS on the fake GitHub.
func (f *fakeGitHub) publish(t *testing.T, content string) {
	t.Helper()
	name, data := platformArchive(t, content)
	sum := sha256.Sum256(data)
	f.mu.Lock()
	f.files[name] = data
	f.files["SHA256SUMS"] = []byte(hex.EncodeToString(sum[:]) + "  " + name + "\n")
	f.mu.Unlock()
}

func (f *fakeGitHub) setFile(name, text string) {
	f.mu.Lock()
	f.files[name] = []byte(text)
	f.mu.Unlock()
}

func (f *fakeGitHub) setGate(g chan struct{}) {
	f.mu.Lock()
	f.gate = g
	f.mu.Unlock()
}

// installEnv is a running app with a fake old program file, a fake GitHub release and a fake restarter.
type installEnv struct {
	s    *harness.Server
	c    *harness.AdminClient
	f    *fakeGitHub
	rest *harness.FakeRestarter
	exe  string
}

func newInstallEnv(t *testing.T, version string) *installEnv {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "hsp-prog")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	if err := os.WriteFile(exe, []byte(oldBinary), 0o755); err != nil {
		t.Fatal(err)
	}
	f := newFakeGitHub(t, "v1.2.0")
	f.publish(t, "NEW-BINARY")
	rest := &harness.FakeRestarter{}
	s := harness.Start(t, harness.Options{Version: version, ExePath: exe, Restarter: rest, Env: map[string]string{"UPDATE_API_URL": f.URL}})
	c := harness.LoginAsAdmin(t, s)
	return &installEnv{s: s, c: c, f: f, rest: rest, exe: exe}
}

// check runs Check now so the page knows the release files.
func (e *installEnv) check(t *testing.T) {
	t.Helper()
	if r := e.c.SendHX("POST", "/admin/update/check", nil, "update-status"); r.StatusCode != 200 {
		t.Fatalf("check: %d", r.StatusCode)
	}
}

func (e *installEnv) install() harness.Resp {
	return e.c.SendHX("POST", "/admin/update/install", nil, "update-progress")
}

func (e *installEnv) progress(t *testing.T) harness.Resp {
	t.Helper()
	return e.c.GetHX("/admin/update/progress", "update-progress")
}

func (e *installEnv) waitState(t *testing.T, want string) {
	t.Helper()
	waitFor(t, "state "+want, func() bool {
		return e.progress(t).Doc(t).Find("#update-progress").AttrOr("data-state", "") == want
	})
}

func (e *installEnv) buttons(t *testing.T) int {
	t.Helper()
	return e.c.GetPage("/admin/update").Doc(t).Find(`button[hx-post="/admin/update/install"]`).Length()
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// E22.09 a good update: the button asks first, the files are swapped, the database is backed up, the program restarts once.
func TestE22_09_GoodUpdate(t *testing.T) {
	e := newInstallEnv(t, "v1.0.0")
	e.check(t)
	d := e.c.GetPage("/admin/update").Doc(t)
	btn := d.Find(`button[hx-post="/admin/update/install"]`)
	if btn.Length() != 1 || btn.AttrOr("hx-target", "") != "#update-progress" || d.Find("#update-progress").Length() != 1 {
		t.Fatal("Update now button or its target is missing")
	}
	confirm := btn.AttrOr("hx-confirm", "")
	for _, want := range []string{"v1.2.0", "https://example.test/releases/v1.2.0", "The admin panel is down for a moment. Online members stay connected. If the new version does not start, the old version and your data are restored."} {
		if !strings.Contains(confirm, want) {
			t.Errorf("confirm text lacks %q: %q", want, confirm)
		}
	}
	r := e.install()
	if r.StatusCode != 200 || !strings.Contains(r.Header.Get("HX-Trigger"), "toast") || !strings.Contains(r.Body, `id="update-progress"`) {
		t.Fatalf("install: %d %q %q", r.StatusCode, r.Header.Get("HX-Trigger"), r.Body)
	}
	e.waitState(t, "done")

	if got := readFile(t, e.exe); got != "NEW-BINARY" {
		t.Errorf("program file = %q", got)
	}
	if got := readFile(t, e.exe+".old"); got != oldBinary {
		t.Errorf(".old = %q", got)
	}
	if exists(e.exe + ".new") {
		t.Error(".new must be gone")
	}
	if tmp, _ := filepath.Glob(filepath.Join(e.s.DataDir, "update-*.tmp")); len(tmp) != 0 {
		t.Errorf("temp download left: %v", tmp)
	}
	if e.rest.Calls() != 1 {
		t.Errorf("restarts = %d", e.rest.Calls())
	}
	db, err := sql.Open("sqlite", "file:"+filepath.Join(e.s.DataDir, "hsp.db.bak-v1.0.0"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if n := scalar[int](t, db, `SELECT COUNT(*) FROM settings`); n != 1 {
		t.Errorf("backup settings rows = %d", n)
	}
	// A new install after a finished one is refused until the program has restarted.
	if r := e.install(); r.StatusCode != 409 {
		t.Errorf("install after done: %d", r.StatusCode)
	}
	if e.buttons(t) != 0 || !strings.Contains(e.c.GetPage("/admin/update").Body, "is installed") {
		t.Error("done page must show the result and no button")
	}
}

// E22.10 a wrong hash or a missing hash line aborts: nothing is changed and the reason is friendly.
func TestE22_10_BadHashAborts(t *testing.T) {
	name := domain.AssetName(runtime.GOOS, runtime.GOARCH)
	cases := map[string]string{
		"wrong hash":   strings.Repeat("0", 64) + "  " + name + "\n",
		"missing line": strings.Repeat("0", 64) + "  other-file.zip\n",
		"empty":        "",
	}
	for label, sums := range cases {
		t.Run(label, func(t *testing.T) {
			e := newInstallEnv(t, "v1.0.0")
			e.f.setFile("SHA256SUMS", sums)
			e.check(t)
			if r := e.install(); r.StatusCode != 200 {
				t.Fatalf("install: %d", r.StatusCode)
			}
			e.waitState(t, "failed")
			body := e.progress(t).Body
			if !strings.Contains(body, "The update failed.") || strings.Contains(body, "hx-trigger") {
				t.Errorf("failed block: %q", body)
			}
			if got := readFile(t, e.exe); got != oldBinary {
				t.Errorf("program file changed: %q", got)
			}
			for _, p := range []string{e.exe + ".new", e.exe + ".old", filepath.Join(e.s.DataDir, "hsp.db.bak-v1.0.0")} {
				if exists(p) {
					t.Errorf("%s must not exist", p)
				}
			}
			if tmp, _ := filepath.Glob(filepath.Join(e.s.DataDir, "update-*.tmp")); len(tmp) != 0 {
				t.Errorf("temp download left: %v", tmp)
			}
			if e.rest.Calls() != 0 {
				t.Error("restart must not happen")
			}
			if e.buttons(t) != 1 { // the admin can try again
				t.Error("button must return after a failure")
			}
		})
	}
}

// E22.11 a release without a file for this system: the page gives the reason, shows no button, and a POST downloads nothing.
func TestE22_11_NoAssetForPlatform(t *testing.T) {
	e := newInstallEnv(t, "v1.0.0")
	e.f.mu.Lock()
	delete(e.f.files, domain.AssetName(runtime.GOOS, runtime.GOARCH))
	e.f.mu.Unlock()
	e.check(t)
	d := e.c.GetPage("/admin/update").Doc(t)
	if d.Find(`button[hx-post="/admin/update/install"]`).Length() != 0 || !strings.Contains(d.Find("[data-update-status]").Text(), "this release has no file for "+runtime.GOOS+"/"+runtime.GOARCH) {
		t.Errorf("page: %q", d.Find("[data-update-status]").Text())
	}
	if r := e.install(); r.StatusCode != 422 || !strings.Contains(r.Body, "no file for") {
		t.Errorf("install: %d %q", r.StatusCode, r.Body)
	}
	if e.f.dlHits.Load() != 0 || readFile(t, e.exe) != oldBinary {
		t.Error("nothing may be downloaded or changed")
	}
}

// E22.12 a program folder that is not writable: reason shown, no button, POST refused.
func TestE22_12_NotWritable(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("folder permissions do not stop this user")
	}
	e := newInstallEnv(t, "v1.0.0")
	dir := filepath.Dir(e.exe)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	e.check(t)
	d := e.c.GetPage("/admin/update").Doc(t)
	if d.Find(`button[hx-post="/admin/update/install"]`).Length() != 0 || !strings.Contains(d.Find("[data-update-status]").Text(), "cannot update: the program file is not writable. Update by hand.") {
		t.Errorf("page: %q", d.Find("[data-update-status]").Text())
	}
	if r := e.install(); r.StatusCode != 422 || e.f.dlHits.Load() != 0 {
		t.Errorf("install: %d, downloads %d", r.StatusCode, e.f.dlHits.Load())
	}
}

// E22.13 a second install while one runs answers 409; the first one finishes.
func TestE22_13_OneAtATime(t *testing.T) {
	e := newInstallEnv(t, "v1.0.0")
	gate := make(chan struct{})
	e.f.setGate(gate)
	e.check(t)
	if r := e.install(); r.StatusCode != 200 {
		t.Fatalf("first install: %d", r.StatusCode)
	}
	r := e.install()
	if r.StatusCode != 409 || !strings.Contains(r.Header.Get("HX-Trigger"), "toast") {
		t.Errorf("second install: %d %q", r.StatusCode, r.Header.Get("HX-Trigger"))
	}
	if e.buttons(t) != 0 {
		t.Error("no button while an install runs")
	}
	close(gate)
	e.waitState(t, "done")
	if e.rest.Calls() != 1 || readFile(t, e.exe) != "NEW-BINARY" {
		t.Errorf("restarts %d, exe %q", e.rest.Calls(), readFile(t, e.exe))
	}
}

// E22.14 an install needs the CSRF token and the login.
func TestE22_14_InstallNeedsCSRFAndLogin(t *testing.T) {
	e := newInstallEnv(t, "v1.0.0")
	e.check(t)
	if r := e.c.Do("POST", "/admin/update/install", nil, ""); r.StatusCode != 403 {
		t.Errorf("no CSRF: %d", r.StatusCode)
	}
	anon := harness.NewAdminClient(t, e.s)
	if r := anon.Do("POST", "/admin/update/install", nil, ""); r.StatusCode == 200 {
		t.Errorf("anonymous: %d", r.StatusCode)
	}
	if r := anon.Get("/admin/update/progress"); r.StatusCode == 200 {
		t.Errorf("anonymous progress: %d", r.StatusCode)
	}
	if e.f.dlHits.Load() != 0 || readFile(t, e.exe) != oldBinary {
		t.Error("nothing may happen")
	}
}

// E22.15 with checks off, or in a dev build, no install is possible and nothing is downloaded.
func TestE22_15_OffOrDevCannotInstall(t *testing.T) {
	e := newInstallEnv(t, "v1.0.0")
	e.check(t)
	setCheckUpdates(t, e.c, "off")
	hits := e.f.hits.Load()
	if r := e.install(); r.StatusCode != 422 || !strings.Contains(r.Body, "Update checks are off") {
		t.Errorf("off: %d %q", r.StatusCode, r.Body)
	}
	if e.buttons(t) != 0 {
		t.Error("no button while off")
	}
	if e.f.hits.Load() != hits || e.f.dlHits.Load() != 0 || readFile(t, e.exe) != oldBinary {
		t.Error("off: no call, no download, no change")
	}

	dev := newInstallEnv(t, "dev")
	if r := dev.install(); r.StatusCode != 422 || !strings.Contains(r.Body, "development build") {
		t.Errorf("dev: %d %q", r.StatusCode, r.Body)
	}
	if dev.f.hits.Load() != 0 || dev.f.dlHits.Load() != 0 || dev.buttons(t) != 0 {
		t.Error("dev: no call and no button")
	}
}

// E22.16 the progress block polls every 2 s while running and stops when it is done or failed.
func TestE22_16_ProgressFragment(t *testing.T) {
	e := newInstallEnv(t, "v1.0.0")
	idle := e.progress(t).Body
	if !strings.Contains(idle, `id="update-progress"`) || strings.Contains(idle, "hx-trigger") {
		t.Errorf("idle: %q", idle)
	}
	gate := make(chan struct{})
	e.f.setGate(gate)
	e.check(t)
	r := e.install()
	for _, body := range []string{r.Body, e.progress(t).Body} {
		d, err := goquery.NewDocumentFromReader(strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		el := d.Find("#update-progress")
		if el.AttrOr("data-state", "") != "downloading" || el.AttrOr("hx-trigger", "") != "every 2s" || el.AttrOr("hx-get", "") != "/admin/update/progress" || !strings.Contains(el.Text(), "Downloading v1.2.0") {
			t.Errorf("running block: %q", body)
		}
	}
	close(gate)
	e.waitState(t, "done")
	done := e.progress(t).Body
	if strings.Contains(done, "hx-trigger") || !strings.Contains(done, "v1.2.0 is installed") {
		t.Errorf("done block: %q", done)
	}
}

// E22.17 the install checks the release again: a release that is no longer newer is not installed.
func TestE22_17_RechecksRelease(t *testing.T) {
	e := newInstallEnv(t, "v1.0.0")
	e.check(t)
	e.f.set("v1.0.0", 200) // GitHub now says the latest is what we run
	if r := e.install(); r.StatusCode != 200 {
		t.Fatalf("install: %d", r.StatusCode)
	}
	e.waitState(t, "failed")
	if !strings.Contains(e.progress(t).Body, "already have the latest") || readFile(t, e.exe) != oldBinary || e.f.dlHits.Load() != 0 {
		t.Error("must stop without a download")
	}
	// A GitHub error at install time is also a friendly failure.
	e.f.set("", 500)
	e.install()
	waitFor(t, "second failure", func() bool { return strings.Contains(e.progress(t).Body, "GitHub answered with an error") })
	if strings.Contains(e.progress(t).Body, "500 Internal") {
		t.Error("raw error shown")
	}
}
