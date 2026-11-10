package e2e

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"hotspotpluslight/e2e/harness"
)

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
	for _, svc := range []string{"auth", "dashboard", "member", "plan", "session", "settings", "voucher"} {
		forbid("internal/"+svc, "internal/web", "internal/radius", "internal/store")
	}
	forbid("internal/radius", "internal/store")
	forbid("internal/web/admin", "internal/store")
}

// E13.01 (Phase 13a) the LICENSE is the full AGPL-3.0 text, the README says so, and the public name is
// "HotspotPlus Lite" in the README and the admin pages.
func TestE13_01_LicenseAndName(t *testing.T) {
	l := readRoot(t, "LICENSE")
	for _, want := range []string{"GNU AFFERO GENERAL PUBLIC LICENSE", "Version 3, 19 November 2007", "END OF TERMS AND CONDITIONS", "13. Remote Network Interaction"} {
		if !strings.Contains(l, want) {
			t.Errorf("LICENSE lacks %q", want)
		}
	}
	if n := strings.Count(l, "\n"); n != 661 {
		t.Errorf("LICENSE has %d lines, want 661", n)
	}
	// sha256 of the official https://www.gnu.org/licenses/agpl-3.0.txt
	if sum := fmt.Sprintf("%x", sha256.Sum256([]byte(l))); sum != "0d96a4ff68ad6d4b6f1f30f713b18d5184912ba8dd389f86aa7710db079abcb0" {
		t.Errorf("LICENSE sha256 %s differs from the official AGPL-3.0 text", sum)
	}
	r := readRoot(t, "README.md")
	if !strings.HasPrefix(r, "# HotspotPlus Lite\n") || !strings.Contains(r, "AGPL-3.0") {
		t.Error("README title or license line is wrong")
	}
	s := harness.Start(t, harness.Options{})
	c := harness.LoginAsAdmin(t, s)
	d := c.Get("/admin/members").Doc(t)
	if title := d.Find("title").Text(); !strings.HasSuffix(title, " - HotspotPlus Lite") {
		t.Errorf("admin page title %q", title)
	}
	if brand := strings.TrimSpace(d.Find(".l-brand").Text()); brand != "HotspotPlus Lite" {
		t.Errorf("brand text %q", brand)
	}
	login := harness.NewAdminClient(t, s).Get("/admin/login").Doc(t)
	if title := login.Find("title").Text(); !strings.Contains(title, "HotspotPlus Lite") {
		t.Errorf("login page title %q", title)
	}
}

// E13.02 (Phase 13a) release assets have stable names without the version: the Makefile keeps the platform
// list in one place (PLATFORMS) and names the files hotspotpluslight-<os>-<arch>; the release workflow
// runs on v* tags and publishes dist/*.
func TestE13_02_ReleaseAssetNames(t *testing.T) {
	mk := readRoot(t, "Makefile")
	m := regexp.MustCompile(`(?m)^PLATFORMS := (.+)$`).FindStringSubmatch(mk)
	if m == nil {
		t.Fatal("Makefile has no PLATFORMS line")
	}
	var got []string
	for _, p := range strings.Fields(m[1]) {
		osName, arch, _ := strings.Cut(p, "/")
		ext := ".tar.gz"
		if osName == "windows" {
			ext = ".zip"
		}
		got = append(got, "hotspotpluslight-"+osName+"-"+arch+ext)
	}
	want := []string{
		"hotspotpluslight-linux-amd64.tar.gz", "hotspotpluslight-linux-386.tar.gz", "hotspotpluslight-linux-arm64.tar.gz", "hotspotpluslight-linux-arm.tar.gz",
		"hotspotpluslight-windows-amd64.zip", "hotspotpluslight-windows-386.zip", "hotspotpluslight-windows-arm64.zip",
		"hotspotpluslight-darwin-amd64.tar.gz", "hotspotpluslight-darwin-arm64.tar.gz",
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("asset names\n got %v\nwant %v", got, want)
	}
	// the build and pack steps must not put the version into a file name
	if strings.Contains(mk, "hotspotpluslight-$(VERSION)") {
		t.Error("Makefile still puts $(VERSION) into the asset names")
	}
	if !strings.Contains(mk, "-o dist/hotspotpluslight-$$os-$$arch$$ext") || !strings.Contains(mk, "SHA256SUMS") {
		t.Error("Makefile does not build dist/hotspotpluslight-<os>-<arch> or write SHA256SUMS")
	}
	wf := readRoot(t, ".github/workflows/release.yml")
	for _, w := range []string{`tags: ["v*"]`, "contents: write", "ubuntu-latest", "actions/checkout@v5", "actions/setup-go@v6", "go-version-file: go.mod",
		"make release VERSION=${TAG}", `gh release create "$TAG" dist/* --generate-notes`, "workflow_call:", "workflow_dispatch:", "GH_TOKEN: ${{ github.token }}"} {
		if !strings.Contains(wf, w) {
			t.Errorf("release.yml lacks %q", w)
		}
	}
	// PLAN and README describe the same names
	if r := readRoot(t, "README.md"); !strings.Contains(r, "releases/latest/download/") || !strings.Contains(r, "hotspotpluslight-<os>-<arch>") {
		t.Error("README does not describe the stable release names")
	}
}

// TestE13_11_MovedReadmeChecks asserts, on the page that owns each, the README checks that moved to the site in Phase 13a (E13.11).
// This is the hard exit item of docs/phase13-notes.md.
func TestE13_11_MovedReadmeChecks(t *testing.T) {
	want := map[string][]string{
		"docs/installation/index.html": {"make build", "first start", "HSP_SECRET_KEY", "HSP_DATA_DIR", "HSP_ADMIN_ADDR", "HSP_RADIUS_AUTH_ADDR", "HSP_RADIUS_ACCT_ADDR", "8081", "firewall"},
		"docs/configuration/index.html": {"HSP_SECRET_KEY", "HSP_DATA_DIR", "HSP_ADMIN_ADDR", "HSP_RADIUS_AUTH_ADDR", "HSP_RADIUS_ACCT_ADDR", "HSP_LOG_LEVEL", "HSP_LOG_FORMAT",
			"make set-password", "admin set-password"},
		"docs/first-login/index.html":          {"admin set-password", "make set-password", "first-start block"},
		"docs/router-setup/index.html":         {"/admin/router", "hotspot folder", "8081", "firewall", "UDP 1812", "UDP 3799"},
		"docs/running-as-a-service/index.html": {"[Unit]", "ExecStart=", "systemd", "HSP_SECRET_KEY", "HSP_DATA_DIR", "8081", "firewall"},
		"docs/backup-and-upgrade/index.html":   {`sqlite3 hsp.db ".backup`, "HSP_SECRET_KEY", "unrecoverable"},
	}
	for page, strs := range want {
		text := siteText(t, page)
		for _, s := range strs {
			if !strings.Contains(text, s) {
				t.Errorf("%s lacks %q", page, s)
			}
		}
	}
	all := siteText(t)
	for _, s := range []string{"make build", "HSP_SECRET_KEY", "HSP_DATA_DIR", "HSP_ADMIN_ADDR", "HSP_RADIUS_AUTH_ADDR", "HSP_RADIUS_ACCT_ADDR", "HSP_LOG_LEVEL", "HSP_LOG_FORMAT",
		"first start", "make set-password", "admin set-password", "/admin/router", "hotspot folder", "8081", "firewall", "sqlite3 hsp.db", ".backup",
		"unrecoverable", "[Unit]", "ExecStart=", "systemd"} {
		if !strings.Contains(all, s) {
			t.Errorf("the site lacks %q", s)
		}
	}
	// The Makefile has the targets that the pages tell people to run.
	mk := readRoot(t, "Makefile")
	for _, target := range []string{"build:", "set-password", "run:"} {
		if !strings.Contains(mk, target) {
			t.Errorf("Makefile has no %s", target)
		}
	}
}

// E8.09 the README is technical (Phase 13a, owner 2026-11-04): ASCII only, diagrams, the legacy link, license,
// make targets, the perf markers and the site link. The user and install content moved to the site (E13.x in 13b).
func TestE8_09_Readme(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(harness.ModuleRoot(), "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	r := string(b)
	for _, want := range []string{
		"HotspotPlus Lite", "https://github.com/pysf/hotspotplus", "AGPL-3.0",
		"https://pysf.github.io/HotspotPlusLight/", "PLAN.md", "CLAUDE.md",
		"make run", "make test", "make ci", "make generate", "make perf", "make perf-quick", "make release", "make build-cross",
		"<!-- perf:start -->", "<!-- perf:end -->",
		"flowchart", "sequenceDiagram", "1812", "1813", "3799", "8081",
	} {
		if !strings.Contains(r, want) {
			t.Errorf("README lacks %q", want)
		}
	}
	if n := strings.Count(r, "```mermaid"); n < 5 {
		t.Errorf("README has %d mermaid blocks, want at least 5 (architecture + 4 sequences)", n)
	}
	for _, gone := range []string{"public_base_url", "walled garden", "walled-garden", "8080", "HSP_HTTP_ADDR", "Public base URL"} {
		if strings.Contains(r, gone) {
			t.Errorf("README still mentions %q (removed in Phase 12a)", gone)
		}
	}
	if regexp.MustCompile(`[^\x00-\x7F]`).MatchString(r) {
		t.Error("README contains non-ASCII text (English only)")
	}
	for _, name := range []string{"Dockerfile", "docker-compose.yml"} {
		if _, err := os.Stat(filepath.Join(harness.ModuleRoot(), name)); err == nil {
			t.Errorf("%s must not exist", name)
		}
	}
	mk, _ := os.ReadFile(filepath.Join(harness.ModuleRoot(), "Makefile"))
	if !strings.Contains(string(mk), "set-password:") {
		t.Error("Makefile has no set-password target")
	}
}
