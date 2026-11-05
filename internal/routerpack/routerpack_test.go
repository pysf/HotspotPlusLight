package routerpack

import (
	"bytes"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The embedded mikrotik folder equals reference/hotspot when that folder exists (E12.00 checks the zip too).
func TestMikrotikCopyEqualsReference(t *testing.T) {
	ref := filepath.Join("..", "..", "reference", "hotspot")
	if _, err := os.Stat(ref); err != nil {
		t.Skip("reference/hotspot is not in this checkout")
	}
	p, _ := New()
	got, err := p.Files(KeyMikrotik)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	err = filepath.WalkDir(ref, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		n++
		rel, _ := filepath.Rel(ref, path)
		want, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !bytes.Equal(got[filepath.ToSlash(rel)], want) {
			t.Errorf("%s differs from reference/hotspot", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != len(got) {
		t.Errorf("embedded copy has %d files, reference has %d", len(got), n)
	}
}

// Adding a theme is one token block file plus one line in Themes: the two lists must agree, and each
// block names only its own theme.
func TestThemeSource(t *testing.T) {
	entries, err := assets.ReadDir("src/css/themes")
	if err != nil {
		t.Fatal(err)
	}
	onDisk := map[string]bool{}
	for _, e := range entries {
		onDisk[strings.TrimSuffix(e.Name(), ".css")] = true
	}
	if len(Themes) != 7 || len(onDisk) != len(Themes) {
		t.Fatalf("%d themes listed, %d block files", len(Themes), len(onDisk))
	}
	seen := map[string]bool{}
	for _, th := range Themes {
		if !onDisk[th.Key] || seen[th.Key] || th.Title == "" || th.About == "" {
			t.Errorf("theme %q: missing block file, duplicate or incomplete", th.Key)
		}
		seen[th.Key] = true
		b, _ := assets.ReadFile("src/css/themes/" + th.Key + ".css")
		keys := map[string]bool{}
		for _, m := range regexp.MustCompile(`data-theme="([a-z]+)"`).FindAllStringSubmatch(string(b), -1) {
			keys[m[1]] = true
		}
		if len(keys) != 1 || !keys[th.Key] {
			t.Errorf("%s.css names themes %v", th.Key, keys)
		}
	}
	p, _ := New()
	if d := p.Designs(); len(d) != 8 || d[0].Key != KeyMikrotik {
		t.Errorf("designs: %v", d)
	}
}

func TestZipIsDeterministic(t *testing.T) {
	p, _ := New()
	a, err := p.Zip("air")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := p.Zip("air")
	if !bytes.Equal(a, b) {
		t.Error("two zips of the same design differ")
	}
	if _, err := p.Zip("nope"); err == nil {
		t.Error("unknown design must fail")
	}
}

// ---- contrast (owner rule: text and button label at least 4.5:1 in every theme) ----

type rgba struct{ r, g, b, a float64 }

var colorRE = regexp.MustCompile(`#[0-9a-fA-F]{3,6}\b|rgba?\([^)]*\)`)

func parseColor(s string) rgba {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "#") {
		h := s[1:]
		if len(h) == 3 {
			h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
		}
		v := func(i int) float64 { n, _ := strconv.ParseUint(h[i:i+2], 16, 8); return float64(n) }
		return rgba{v(0), v(2), v(4), 1}
	}
	inner := s[strings.Index(s, "(")+1 : strings.LastIndex(s, ")")]
	parts := strings.Split(inner, ",")
	f := func(i int) float64 { n, _ := strconv.ParseFloat(strings.TrimSpace(parts[i]), 64); return n }
	c := rgba{f(0), f(1), f(2), 1}
	if len(parts) > 3 {
		c.a = f(3)
	}
	return c
}

func over(fg, bg rgba) rgba {
	return rgba{fg.r*fg.a + bg.r*(1-fg.a), fg.g*fg.a + bg.g*(1-fg.a), fg.b*fg.a + bg.b*(1-fg.a), 1}
}

func lum(c rgba) float64 {
	f := func(v float64) float64 {
		v /= 255
		if v <= 0.03928 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*f(c.r) + 0.7152*f(c.g) + 0.0722*f(c.b)
}

func ratio(a, b rgba) float64 {
	x, y := lum(a), lum(b)
	return (math.Max(x, y) + 0.05) / (math.Min(x, y) + 0.05)
}

// token reads --name:value; from a theme block (up to the next ';' that is outside brackets).
func token(block, name string) string {
	i := strings.Index(block, "--"+name+":")
	if i < 0 {
		return ""
	}
	rest := block[i+len(name)+3:]
	depth := 0
	for j, ch := range rest {
		switch ch {
		case '(':
			depth++
		case ')':
			depth--
		case ';':
			if depth == 0 {
				return strings.TrimSpace(rest[:j])
			}
		}
	}
	return ""
}

// The page background is a gradient; the plain --bg color is used under the translucent cards (an approximation).
// Every gradient stop of the button is checked.
func TestThemeContrast(t *testing.T) {
	for _, th := range Themes {
		b, _ := assets.ReadFile("src/css/themes/" + th.Key + ".css")
		block := string(b)
		bg := parseColor(token(block, "bg"))
		card := over(parseColor(token(block, "card")), bg)
		in := over(parseColor(token(block, "in-bg")), card)
		text, muted, bad := parseColor(token(block, "text")), parseColor(token(block, "muted")), parseColor(token(block, "bad"))
		check := func(what string, fg, back rgba) {
			if r := ratio(fg, back); r < 4.5 {
				t.Errorf("%s: %s contrast %.2f, want 4.5", th.Key, what, r)
			}
		}
		check("text on card", text, card)
		check("text on input", text, in)
		check("muted on card", muted, card)
		check("error on card", bad, card)
		btnFg := parseColor(token(block, "btn-fg"))
		for _, stop := range colorRE.FindAllString(token(block, "btn-bg"), -1) {
			check("button label on "+stop, btnFg, parseColor(stop))
		}
	}
}

// ---- preview engine ----

func TestRenderTemplate(t *testing.T) {
	vars := map[string]string{"error": "OUT_OF_BULK", "username": "a<b", "empty": ""}
	cases := []struct{ in, want string }{
		{`$(username)`, `a&lt;b`},
		{`$(if error)X$(endif)`, `X`},
		{`$(if empty)X$(else)Y$(endif)`, `Y`},
		{`$(if error == "OUT_OF_BULK")A$(elif error == "B")B$(elif error)C$(endif)`, `A`},
		{`$(if error == "NO")A$(elif error == "B")B$(elif error)$(error)$(endif)`, `OUT_OF_BULK`},
		{`$(if empty)$(if error)no$(endif)$(else)out$(if error) in$(endif)$(endif)`, `out in`},
		{`$(if error != 'OUT_OF_BULK')no$(else)yes$(endif)`, `yes`},
		{`$(unknown)x`, `x`},
	}
	for _, c := range cases {
		if got := renderTemplate(c.in, vars); got != c.want {
			t.Errorf("%s -> %q, want %q", c.in, got, c.want)
		}
	}
}

// Preview and PreviewAsset read the copy made in New: same bytes as Files, not built again per call (review B10.5).
func TestPreviewUsesTheBuiltCopy(t *testing.T) {
	p, err := New()
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range p.Designs() {
		want, err := p.Files(d.Key)
		if err != nil {
			t.Fatal(err)
		}
		a, _, ok := p.PreviewAsset(d.Key, "css/"+cssName(want))
		b, _, _ := p.PreviewAsset(d.Key, "css/"+cssName(want))
		if !ok || !bytes.Equal(a, want["css/"+cssName(want)]) {
			t.Fatalf("%s: preview asset differs from Files", d.Key)
		}
		if &a[0] != &b[0] {
			t.Errorf("%s: the asset was built again for the second request", d.Key)
		}
	}
}

// cssName returns the name of one stylesheet in the files.
func cssName(files map[string][]byte) string {
	for n := range files {
		if strings.HasPrefix(n, "css/") {
			return strings.TrimPrefix(n, "css/")
		}
	}
	return ""
}
