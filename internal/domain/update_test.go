package domain

import "testing"

func TestVersionCompare(t *testing.T) {
	cases := []struct {
		latest, cur string
		want        bool
	}{
		{"v0.9.1", "v0.9.0", true},
		{"v0.10.0", "v0.9.9", true}, // numbers, not text
		{"v1.0.0", "0.9.9", true},   // "v" is optional
		{"v0.9.0", "v0.9.0", false},
		{"v0.8.0", "v0.9.0", false},
		{"v0.9.0", "dev", false},
		{"v0.9.0", "ci", false},
		{"nonsense", "v0.9.0", false},
		{"v1.2", "v0.9.0", false},
		{"v01.2.3", "v0.9.0", false},
		{"v1.2.3-rc1", "v0.9.0", false},
	}
	for _, c := range cases {
		if got := IsNewerVersion(c.latest, c.cur); got != c.want {
			t.Errorf("IsNewerVersion(%q, %q) = %v, want %v", c.latest, c.cur, got, c.want)
		}
	}
	if v, ok := ParseVersion(" v1.2.3 "); !ok || v.String() != "v1.2.3" {
		t.Errorf("ParseVersion trim: %v %v", v, ok)
	}
}

func TestParseLatestRelease(t *testing.T) {
	r, err := ParseLatestRelease([]byte(`{"tag_name":"v1.2.3","html_url":"https://x/r","body":"notes","assets":[{"name":"a.zip","size":1},{"name":"SHA256SUMS"}]}`))
	if err != nil || r.Tag != "v1.2.3" || r.URL != "https://x/r" || len(r.Assets) != 2 || r.Assets[1] != "SHA256SUMS" {
		t.Fatalf("got %+v, %v", r, err)
	}
	for _, bad := range []string{``, `{`, `{}`, `{"tag_name":"latest"}`, `[]`} {
		if _, err := ParseLatestRelease([]byte(bad)); err == nil {
			t.Errorf("%q: want an error", bad)
		}
	}
}

func TestAssetName(t *testing.T) {
	for _, c := range []struct{ os, arch, want string }{
		{"windows", "amd64", "hotspotpluslight-windows-amd64.zip"},
		{"linux", "arm64", "hotspotpluslight-linux-arm64.tar.gz"},
		{"darwin", "arm64", "hotspotpluslight-darwin-arm64.tar.gz"},
	} {
		if got := AssetName(c.os, c.arch); got != c.want {
			t.Errorf("AssetName(%s,%s) = %s", c.os, c.arch, got)
		}
	}
}

func TestParseSHA256Sums(t *testing.T) {
	h1 := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789ABCDEF"
	h2 := "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
	got := ParseSHA256Sums(h1 + "  a.zip\r\n" + h2 + " *b.tar.gz\n\nshort  c\n" + h1 + "  has space.zip\n")
	if len(got) != 2 || got["a.zip"] != "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" || got["b.tar.gz"] != h2 {
		t.Errorf("got %v", got)
	}
}

func TestParseLatestReleaseLinkMustBeHTTPS(t *testing.T) {
	for _, link := range []string{"http://x/r", "javascript:alert(1)", "mailto:a@b", ""} {
		r, err := ParseLatestRelease([]byte(`{"tag_name":"v1.0.0","html_url":"` + link + `"}`))
		if err != nil || r.URL != "" {
			t.Errorf("link %q: got %q, err %v", link, r.URL, err)
		}
	}
}

func TestParseLatestReleaseKeepsLinks(t *testing.T) {
	r, err := ParseLatestRelease([]byte(`{"tag_name":"v1.2.3","assets":[{"name":"a.zip","browser_download_url":"https://x/a.zip"},{"name":"SHA256SUMS","browser_download_url":"https://x/s"}]}`))
	if err != nil || r.AssetLinks["a.zip"] != "https://x/a.zip" || r.AssetLinks["SHA256SUMS"] != "https://x/s" {
		t.Fatalf("got %+v, %v", r, err)
	}
}

func TestDownloadLinkOK(t *testing.T) {
	cases := []struct {
		link      string
		allowHTTP bool
		want      bool
	}{
		{"https://github.com/x", false, true},
		{"http://github.com/x", false, false},
		{"http://127.0.0.1:1/x", true, true},
		{"ftp://x/y", true, false},
		{"", true, false},
	}
	for _, c := range cases {
		if got := DownloadLinkOK(c.link, c.allowHTTP); got != c.want {
			t.Errorf("DownloadLinkOK(%q, %v) = %v", c.link, c.allowHTTP, got)
		}
	}
}

func TestInstallBlocker(t *testing.T) {
	ok := InstallFacts{AssetsKnown: true, Assets: []string{"SHA256SUMS", "hotspotpluslight-linux-amd64.tar.gz"}, GOOS: "linux", GOARCH: "amd64", Writable: true}
	if r := InstallBlocker(ok); r != "" {
		t.Errorf("good facts: %q", r)
	}
	unknown := ok
	unknown.AssetsKnown, unknown.Assets = false, nil
	if r := InstallBlocker(unknown); r != "" {
		t.Errorf("unknown asset list must not block: %q", r)
	}
	dev, test, noAsset, ro := ok, ok, ok, ok
	dev.Dev = true
	test.TestBinary = true
	noAsset.GOARCH = "riscv64"
	ro.Writable = false
	for name, f := range map[string]InstallFacts{"dev": dev, "test": test, "asset": noAsset, "writable": ro} {
		if InstallBlocker(f) == "" {
			t.Errorf("%s: want a reason", name)
		}
	}
	if r := InstallBlocker(ro); r != "cannot update: the program file is not writable. Update by hand." {
		t.Errorf("writable text %q", r)
	}
}
