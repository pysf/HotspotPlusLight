package routerpack

import (
	"archive/zip"
	"bytes"
	"regexp"
	"strings"
	"testing"
)

func TestRegistry(t *testing.T) {
	p, err := New()
	if err != nil {
		t.Fatal(err)
	}
	cats := map[string]int{}
	colorRE := regexp.MustCompile(`^rgba?\(\d{1,3}, \d{1,3}, \d{1,3}(, [01](\.\d+)?)?\)$`)
	for _, th := range p.Themes() {
		cats[th.Category]++
		if !colorRE.MatchString(th.Primary) || !colorRE.MatchString(th.Overlay) {
			t.Errorf("%s: colors %q %q", th.ID, th.Primary, th.Overlay)
		}
		if bg, err := p.Background(th); err != nil || len(bg) < 10_000 {
			t.Errorf("%s: background %d bytes, %v", th.ID, len(bg), err)
		}
	}
	if len(p.Themes()) != 38 || len(cats) != 6 {
		t.Fatalf("%d themes in %d categories: %v", len(p.Themes()), len(cats), cats)
	}
	if _, ok := p.Theme(DefaultTheme); !ok {
		t.Error("default theme missing")
	}
	if _, ok := p.Theme("nope"); ok {
		t.Error("unknown theme found")
	}
}

func TestLoginHTMLEscapesAndBakes(t *testing.T) {
	p, _ := New()
	th, _ := p.Theme(DefaultTheme)
	out, err := p.LoginHTML(th, Config{SiteTitle: `Caf<e> "Wi-Fi"`, PublicBaseURL: "http://10.0.0.2:8080/"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, "Caf<e>") || !strings.Contains(s, "Caf&lt;e&gt;") {
		t.Error("site title is not HTML-escaped")
	}
	if !strings.Contains(s, `HSP_API_BASE = "http://10.0.0.2:8080"`) {
		t.Error("API base not baked (trailing slash must be trimmed)")
	}
	if !strings.Contains(s, "$(link-login-only)") {
		t.Error("router variables must be left for the router")
	}
}

// The username preset from the router goes through routerValue like every other router variable, so an
// unsubstituted "$(username)" (admin preview) is not shown literally.
func TestUsernamePresetUsesRouterValue(t *testing.T) {
	p, _ := New()
	th, _ := p.Theme(DefaultTheme)
	out, err := p.LoginHTML(th, Config{SiteTitle: "x"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, ` value="$(username)"`) {
		t.Error("username input must not carry the raw router variable as its value")
	}
	if !strings.Contains(s, `data-router-value="$(username)"`) ||
		!strings.Contains(s, `routerValue(form.elements.username.getAttribute("data-router-value"))`) {
		t.Error("username preset must be read through routerValue")
	}
}

func TestZipLayout(t *testing.T) {
	p, _ := New()
	th, _ := p.Theme(DefaultTheme)
	raw, err := p.Zip(th, Config{SiteTitle: "x"})
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, f := range zr.File {
		got[f.Name] = true
	}
	for _, n := range []string{"login.html", "alogin.html", "status.html", "logout.html", "error.html", "redirect.html", "rlogin.html", "background.jpg", "logo.png"} {
		if !got["hotspot/"+n] {
			t.Errorf("missing hotspot/%s", n)
		}
	}
}
