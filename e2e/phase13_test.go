package e2e

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"hotspotpluslight/e2e/harness"
)

func readRoot(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(harness.ModuleRoot(), name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
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
		"make release VERSION=${GITHUB_REF_NAME}", `gh release create "$GITHUB_REF_NAME" dist/* --generate-notes`, "GH_TOKEN: ${{ github.token }}"} {
		if !strings.Contains(wf, w) {
			t.Errorf("release.yml lacks %q", w)
		}
	}
	// PLAN and README describe the same names
	if r := readRoot(t, "README.md"); !strings.Contains(r, "releases/latest/download/") || !strings.Contains(r, "hotspotpluslight-<os>-<arch>") {
		t.Error("README does not describe the stable release names")
	}
}
