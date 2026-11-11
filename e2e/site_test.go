package e2e

import (
	"encoding/json"
	"image"
	_ "image/png" // registers the PNG decoder for E13.03
	"math"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"

	"hotspotpluslight/e2e/harness"
	"hotspotpluslight/internal/domain"
	"hotspotpluslight/internal/routerpack"
)

const (
	ghRepo   = "https://github.com/pysf/HotspotPlusLight"
	ghLegacy = "https://github.com/pysf/hotspotplus"
)

func siteRead(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(siteRoot(), filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// siteBase returns the canonical base URL, read from the home page.
func siteBase(t *testing.T) string {
	t.Helper()
	b, ok := siteDoc(t, "index.html").Find(`link[rel=canonical]`).Attr("href")
	if !ok || !strings.HasPrefix(b, "https://") || !strings.HasSuffix(b, "/") {
		t.Fatalf("home canonical %q is not a base url", b)
	}
	return b
}

func docsPages(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, p := range sitePages(t) {
		if strings.HasPrefix(p, "docs/") {
			out = append(out, p)
		}
	}
	return out
}

func siteIDs(t *testing.T, rel string) map[string]bool {
	t.Helper()
	ids := map[string]bool{}
	siteDoc(t, rel).Find("[id]").Each(func(_ int, s *goquery.Selection) {
		id, _ := s.Attr("id")
		ids[id] = true
	})
	return ids
}

// TestE13_03_SiteStructure checks the site files, the per-page metadata, the sitemap and the og image (E13.03).
func TestE13_03_SiteStructure(t *testing.T) {
	for _, f := range []string{".nojekyll", "404.html", "robots.txt", "sitemap.xml", "index.html", "assets/img/favicon.svg", "assets/img/og-image.png",
		"assets/css/tokens.css", "assets/css/site.css", "assets/css/docs.css",
		"assets/js/copy.js", "assets/js/docs-search.js", "assets/js/search-index.js", "assets/js/docs-nav.js", "assets/js/os-tabs.js"} {
		if _, err := os.Stat(filepath.Join(siteRoot(), f)); err != nil {
			t.Errorf("site/%s is missing", f)
		}
	}
	base := siteBase(t)
	sitemap := siteRead(t, "sitemap.xml")
	if robots := siteRead(t, "robots.txt"); !strings.Contains(robots, "Sitemap: "+base+"sitemap.xml") {
		t.Errorf("robots.txt does not point to %ssitemap.xml", base)
	}
	listed := regexp.MustCompile(`<loc>([^<]+)</loc>`).FindAllStringSubmatch(sitemap, -1)
	inMap := map[string]bool{}
	for _, m := range listed {
		inMap[m[1]] = true
	}
	for _, p := range sitePages(t) {
		d := siteDoc(t, p)
		if title := strings.TrimSpace(d.Find("title").Text()); title == "" || !strings.Contains(title, "HotspotPlus Lite") {
			t.Errorf("%s: title %q", p, title)
		}
		if desc, _ := d.Find(`meta[name=description]`).Attr("content"); len(desc) < 40 || len(desc) > 200 {
			t.Errorf("%s: description length %d", p, len(desc))
		}
		if lang, _ := d.Find("html").Attr("lang"); lang != "en" {
			t.Errorf("%s: lang %q", p, lang)
		}
		if n := d.Find("h1").Length(); n != 1 {
			t.Errorf("%s: %d h1 elements", p, n)
		}
		for _, sel := range []string{"header", "main#main", "footer", `a.skip[href="#main"]`, `meta[name=viewport]`, `meta[http-equiv="Content-Security-Policy"]`} {
			if d.Find(sel).Length() != 1 {
				t.Errorf("%s: want exactly one %s", p, sel)
			}
		}
		if p == "404.html" {
			if robots, _ := d.Find(`meta[name=robots]`).Attr("content"); robots != "noindex" {
				t.Errorf("404.html robots %q", robots)
			}
			continue
		}
		canon, _ := d.Find(`link[rel=canonical]`).Attr("href")
		want := base + strings.TrimSuffix(p, "index.html")
		if canon != want {
			t.Errorf("%s: canonical %q, want %q", p, canon, want)
		}
		if !inMap[want] {
			t.Errorf("%s: %s is not in sitemap.xml", p, want)
		}
		if og, _ := d.Find(`meta[property="og:url"]`).Attr("content"); og != want {
			t.Errorf("%s: og:url %q", p, og)
		}
		if img, _ := d.Find(`meta[property="og:image"]`).Attr("content"); img != base+"assets/img/og-image.png" {
			t.Errorf("%s: og:image %q", p, img)
		}
		if d.Find(`meta[name="twitter:card"]`).Length() != 1 {
			t.Errorf("%s: no twitter card", p)
		}
	}
	if len(listed) != len(sitePages(t))-1 {
		t.Errorf("sitemap has %d urls, want %d (every page but 404.html)", len(listed), len(sitePages(t))-1)
	}
	f, err := os.Open(filepath.Join(siteRoot(), "assets/img/og-image.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cfg, format, err := image.DecodeConfig(f)
	if err != nil || format != "png" || cfg.Width != 1200 || cfg.Height != 630 {
		t.Errorf("og-image.png: %v %s %dx%d", err, format, cfg.Width, cfg.Height)
	}
}

// siteResolve turns a link found in page into a path relative to site/ and a fragment.
// It returns ok=false for external links and for links that leave site/.
func siteResolve(base, page, link string) (target, frag string, ok bool) {
	if strings.HasPrefix(link, base) {
		link = strings.TrimPrefix(link, base)
		page = ""
	} else if strings.Contains(link, "://") || strings.HasPrefix(link, "mailto:") {
		return "", "", false
	} else {
		rel := strings.SplitN(link, "#", 2)[0]
		joined := path.Join(path.Dir(page), rel)
		if rel == "" || strings.HasSuffix(rel, "/") {
			joined += "/"
		}
		link = joined + hashOf(link)
	}
	link, frag, _ = strings.Cut(link, "#")
	if link == "" || link == "." || strings.HasSuffix(link, "/") {
		link = path.Join(link, "index.html")
	}
	return link, frag, !strings.HasPrefix(link, "..")
}

func hashOf(link string) string {
	if _, h, ok := strings.Cut(link, "#"); ok {
		return "#" + h
	}
	return ""
}

// TestE13_04_LinksResolve checks that every internal link, asset and fragment resolves (E13.04).
// Links are relative, so the site works under /HotspotPlusLight/ and at a root. Only 404.html uses the base URL, because it is served at any depth.
func TestE13_04_LinksResolve(t *testing.T) {
	base := siteBase(t)
	idCache := map[string]map[string]bool{}
	check := func(page, link string) {
		if strings.HasPrefix(link, "/") {
			t.Errorf("%s: root-absolute link %q", page, link)
		}
		if strings.HasPrefix(link, base) && page != "404.html" {
			t.Errorf("%s: base url link %q (use a relative link)", page, link)
		}
		target, frag, ok := siteResolve(base, page, link)
		if !ok {
			return
		}
		if _, err := os.Stat(filepath.Join(siteRoot(), filepath.FromSlash(target))); err != nil {
			t.Errorf("%s: link %q -> site/%s does not exist", page, link, target)
			return
		}
		if frag == "" || !strings.HasSuffix(target, ".html") {
			return
		}
		if idCache[target] == nil {
			idCache[target] = siteIDs(t, target)
		}
		if !idCache[target][frag] {
			t.Errorf("%s: link %q: no id %q in %s", page, link, frag, target)
		}
	}
	for _, p := range sitePages(t) {
		d := siteDoc(t, p)
		d.Find("[href]").Not("link[rel=canonical]").Each(func(_ int, s *goquery.Selection) {
			h, _ := s.Attr("href")
			if h == "#main" || strings.HasPrefix(h, "#") {
				if !siteIDs(t, p)[strings.TrimPrefix(h, "#")] {
					t.Errorf("%s: no id for %q", p, h)
				}
				return
			}
			check(p, h)
		})
		d.Find("[src]").Each(func(_ int, s *goquery.Selection) {
			src, _ := s.Attr("src")
			check(p, src)
		})
	}
	imp := regexp.MustCompile(`from "(\.[^"]+)"`)
	for _, js := range []string{"assets/js/docs-search.js"} {
		for _, m := range imp.FindAllStringSubmatch(siteRead(t, js), -1) {
			if _, err := os.Stat(filepath.Join(siteRoot(), path.Dir(js), m[1])); err != nil {
				t.Errorf("%s imports %q: %v", js, m[1], err)
			}
		}
	}
}

var (
	allowedURLPrefixes = []string{ghRepo, ghLegacy}
	colorLiteral       = regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b|\brgba?\(|\bhsla?\(`)
)

// TestE13_05_NoInlineNoThirdParty checks that the site has no third-party request, no inline code and no color literal outside tokens.css (E13.05).
func TestE13_05_NoInlineNoThirdParty(t *testing.T) {
	base := siteBase(t)
	allowed := func(u string) bool {
		if strings.HasPrefix(u, base) {
			return true
		}
		for _, p := range allowedURLPrefixes {
			if u == p || strings.HasPrefix(u, p+"/") || strings.HasPrefix(u, p+"#") || strings.HasPrefix(u, p+".git") {
				return true
			}
		}
		return false
	}
	for _, p := range sitePages(t) {
		d := siteDoc(t, p)
		d.Find("*").Each(func(_ int, s *goquery.Selection) {
			for _, a := range s.Nodes[0].Attr {
				name := strings.ToLower(a.Key)
				switch {
				case name == "style" || strings.HasPrefix(name, "on"):
					t.Errorf("%s: inline %s attribute", p, name)
				case slices.Contains([]string{"href", "src", "action", "poster", "srcset", "data", "content"}, name) && strings.Contains(a.Val, "://"):
					if !allowed(a.Val) {
						t.Errorf("%s: external url %q in %s", p, a.Val, name)
					}
				}
			}
		})
		d.Find("script").Each(func(_ int, s *goquery.Selection) {
			if strings.TrimSpace(s.Text()) != "" {
				t.Errorf("%s: inline script body", p)
			}
			if typ, _ := s.Attr("type"); typ != "module" {
				t.Errorf("%s: script type %q, want module", p, typ)
			}
			if _, src := s.Attr("src"); !src {
				t.Errorf("%s: script without src", p)
			}
		})
		if d.Find("style").Length() != 0 {
			t.Errorf("%s: style element", p)
		}
		if d.Find("iframe, object, embed, link[rel=preconnect], link[rel=dns-prefetch]").Length() != 0 {
			t.Errorf("%s: embeds or preconnects", p)
		}
	}
	for _, kind := range []string{"css", "js"} {
		files, _ := filepath.Glob(filepath.Join(siteRoot(), "assets", kind, "*"))
		for _, f := range files {
			b, _ := os.ReadFile(f)
			s := string(b)
			name := filepath.Base(f)
			if strings.Contains(s, "http://") || strings.Contains(s, "https://") {
				t.Errorf("%s contains a url", name)
			}
			if kind != "css" {
				continue
			}
			if strings.Contains(s, "url(") || strings.Contains(s, "@import") {
				t.Errorf("%s uses url() or @import", name)
			}
			if name != "tokens.css" {
				if m := colorLiteral.FindString(s); m != "" {
					t.Errorf("%s has a color literal %q outside tokens.css", name, m)
				}
			}
			if strings.Contains(s, "var(--faint)") {
				for _, line := range strings.Split(s, "\n") {
					if strings.Contains(line, "var(--faint)") && !strings.Contains(line, "content:") && !strings.Contains(line, "--dot") {
						t.Errorf("%s: --faint is for decoration only: %s", name, strings.TrimSpace(line))
					}
				}
			}
		}
	}
	for _, f := range []string{"404.html", "index.html"} {
		if strings.Contains(siteRead(t, f), "mermaid") {
			t.Errorf("%s mentions mermaid: the site has no diagrams", f)
		}
	}
}

// TestE13_06_Weight checks that a page with its css and scripts weighs under 100 KB, and that the css honors reduced motion (E13.06).
func TestE13_06_Weight(t *testing.T) {
	for _, p := range sitePages(t) {
		total := len(siteRead(t, p))
		d := siteDoc(t, p)
		for _, sel := range []string{"link[rel=stylesheet]", "script[src]"} {
			d.Find(sel).Each(func(_ int, s *goquery.Selection) {
				ref, _ := s.Attr("href")
				if ref == "" {
					ref, _ = s.Attr("src")
				}
				target, _, _ := siteResolve(siteBase(t), p, ref)
				total += len(siteRead(t, target))
			})
		}
		if total >= 100*1024 {
			t.Errorf("%s weighs %d bytes with its css and scripts", p, total)
		}
	}
	if !strings.Contains(siteRead(t, "assets/css/site.css"), "prefers-reduced-motion") {
		t.Error("site.css has no prefers-reduced-motion rule")
	}
}

// luminance returns the WCAG relative luminance of a #rrggbb color.
func luminance(hex string) float64 {
	n, _ := strconv.ParseUint(strings.TrimPrefix(hex, "#"), 16, 32)
	ch := func(v uint64) float64 {
		c := float64(v) / 255
		if c <= 0.03928 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	return 0.2126*ch(n>>16&255) + 0.7152*ch(n>>8&255) + 0.0722*ch(n&255)
}

func contrast(a, b string) float64 {
	x, y := luminance(a), luminance(b)
	if x < y {
		x, y = y, x
	}
	return (x + 0.05) / (y + 0.05)
}

// TestE13_07_Contrast checks that the text color tokens have a contrast of at least 4.5:1 on every surface (E13.07).
func TestE13_07_Contrast(t *testing.T) {
	tokens := map[string]string{}
	for _, m := range regexp.MustCompile(`--([a-z0-9-]+):\s*(#[0-9a-fA-F]{6})\b`).FindAllStringSubmatch(siteRead(t, "assets/css/tokens.css"), -1) {
		tokens[m[1]] = m[2]
	}
	surfaces := []string{"bg", "panel", "elev", "code-bg"}
	for _, fg := range []string{"text", "body", "muted", "subtle", "accent", "ok", "note-text"} {
		for _, bg := range surfaces {
			if c := contrast(tokens[fg], tokens[bg]); c < 4.5 {
				t.Errorf("--%s on --%s: contrast %.2f", fg, bg, c)
			}
		}
	}
	for _, fg := range []string{"warn", "bad"} {
		if c := contrast(tokens[fg], tokens["panel"]); c < 4.5 {
			t.Errorf("--%s on --panel: contrast %.2f", fg, c)
		}
	}
	if c := contrast(tokens["bg"], tokens["text"]); c < 4.5 {
		t.Errorf("primary button: contrast %.2f", c)
	}
}

var hspVar = regexp.MustCompile(`\bHSP_[A-Z][A-Z_]*\b`)

// configVars returns the variables that internal/config reads, with the HSP_ prefix.
func configVars(t *testing.T) []string {
	t.Helper()
	src := readRoot(t, "internal/config/config.go")
	set := map[string]bool{}
	for _, m := range regexp.MustCompile(`get\("([A-Z_]+)"`).FindAllStringSubmatch(src, -1) {
		set["HSP_"+m[1]] = true
	}
	for _, m := range hspVar.FindAllString(src, -1) {
		if m != "HSP_" {
			set[m] = true
		}
	}
	out := make([]string, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	slices.Sort(out)
	return out
}

// TestE13_08_ConfigVars checks that the site names only real HSP_ variables, and documents every one with its real default (E13.08).
func TestE13_08_ConfigVars(t *testing.T) {
	vars := configVars(t)
	if len(vars) < 10 {
		t.Fatalf("found only %v in internal/config", vars)
	}
	for _, p := range sitePages(t) {
		for _, v := range hspVar.FindAllString(siteText(t, p), -1) {
			if !slices.Contains(vars, v) {
				t.Errorf("%s names %s, which internal/config does not read", p, v)
			}
		}
	}
	cfg := siteDoc(t, "docs/configuration/index.html")
	rows := map[string]string{}
	cfg.Find("table tr").Each(func(_ int, tr *goquery.Selection) {
		cells := tr.Find("td")
		if cells.Length() == 3 {
			rows[strings.TrimSpace(cells.Eq(0).Text())] = strings.TrimSpace(cells.Eq(1).Text())
		}
	})
	defaults := map[string]string{
		"HSP_DATA_DIR": "data", "HSP_ADMIN_ADDR": ":8080", "HSP_RADIUS_AUTH_ADDR": ":1812", "HSP_RADIUS_ACCT_ADDR": ":1813",
		"HSP_ADMIN_USERNAME": "admin", "HSP_LOG_LEVEL": "info", "HSP_LOG_FORMAT": "text", "HSP_SECRET_KEY": "(auto-generated)",
		"HSP_RADIUS_SHARED_SECRET": "(generated)", "HSP_DEV": "(empty)",
	}
	for _, v := range vars {
		got, ok := rows[v]
		if !ok {
			t.Errorf("the configuration page does not document %s", v)
			continue
		}
		if want, known := defaults[v]; !known {
			t.Errorf("%s has no expected default in this test: add it", v)
		} else if got != want {
			t.Errorf("%s default %q on the site, want %q", v, got, want)
		}
	}
	// The defaults must come from the code, not only from this test.
	src := readRoot(t, "internal/config/config.go")
	for _, d := range []string{`"DATA_DIR", "data"`, `"ADMIN_ADDR", ":8080"`, `"RADIUS_AUTH_ADDR", ":1812"`, `"RADIUS_ACCT_ADDR", ":1813"`,
		`"ADMIN_USERNAME", "admin"`, `"LOG_LEVEL", "info"`, `"LOG_FORMAT", "text"`} {
		if !strings.Contains(src, d) {
			t.Errorf("internal/config no longer has the default %s", d)
		}
	}
}

// TestE13_09_Commands checks that every command, flag and subcommand shown with the program name exists in cmd/ (E13.09).
func TestE13_09_Commands(t *testing.T) {
	var src strings.Builder
	files, _ := filepath.Glob(filepath.Join(harness.ModuleRoot(), "cmd", "hotspotpluslight", "*.go"))
	for _, f := range files {
		if !strings.HasSuffix(f, "_test.go") {
			b, _ := os.ReadFile(f)
			src.Write(b)
		}
	}
	code := src.String()
	for _, w := range []string{`"serve"`, `"gen-key"`, `"version"`, `"set-password"`, `"service"`, `"install"`, `"uninstall"`, `"start"`, `"stop"`, `--env-file`, `--stdin`, `--dir`} {
		if !strings.Contains(code, w) {
			t.Errorf("cmd/ has no %s", w)
		}
	}
	cmd := regexp.MustCompile(`\bhotspotpluslight(?:\.exe)?[ \t]+(?:--env-file[ \t]+\S+[ \t]+)?([a-z][a-z-]*)(?:[ \t]+([a-z][a-z-]*))?`)
	flag := regexp.MustCompile(`--[a-z][a-z-]*`)
	seen := map[string]bool{}
	for _, p := range sitePages(t) {
		d := siteDoc(t, p)
		d.Find("code").Each(func(_ int, s *goquery.Selection) {
			for _, line := range strings.Split(s.Text(), "\n") {
				if !strings.Contains(line, "hotspotpluslight") || strings.HasPrefix(strings.TrimSpace(line), "#") {
					continue
				}
				for _, m := range cmd.FindAllStringSubmatch(line, -1) {
					word, next := m[1], m[2]
					seen[word] = true
					switch word {
					case "serve", "gen-key", "version":
					case "admin":
						if next != "set-password" {
							t.Errorf("%s: unknown admin command %q", p, next)
						}
					case "service":
						if !slices.Contains([]string{"install", "uninstall", "start", "stop"}, next) {
							t.Errorf("%s: unknown service command %q", p, next)
						}
					default:
						t.Errorf("%s: unknown command %q in %q", p, word, m[0])
					}
				}
				if !cmd.MatchString(line) {
					continue
				}
				for _, f := range flag.FindAllString(line, -1) {
					if !slices.Contains([]string{"--env-file", "--stdin", "--dir"}, f) {
						t.Errorf("%s: unknown flag %s in %q", p, f, line)
					}
				}
			}
		})
	}
	for _, w := range []string{"serve", "gen-key", "version", "admin", "service"} {
		if !seen[w] {
			t.Errorf("no page shows the command %q", w)
		}
	}
	text := siteText(t)
	for _, w := range []string{"--env-file", "--stdin", "--dir"} {
		if !strings.Contains(text, w) {
			t.Errorf("no page mentions %s", w)
		}
	}
}

// TestE13_10_DownloadNames checks that the download urls use the stable asset names that make release builds (E13.10).
func TestE13_10_DownloadNames(t *testing.T) {
	mk := readRoot(t, "Makefile")
	m := regexp.MustCompile(`(?m)^PLATFORMS := (.+)$`).FindStringSubmatch(mk)
	if m == nil {
		t.Fatal("no PLATFORMS in the Makefile")
	}
	want := map[string]bool{"SHA256SUMS": true}
	for _, p := range strings.Fields(m[1]) {
		ext := "tar.gz"
		if strings.HasPrefix(p, "windows/") {
			ext = "zip"
		}
		want["hotspotpluslight-"+strings.ReplaceAll(p, "/", "-")+"."+ext] = true
	}
	if len(want) != 10 {
		t.Fatalf("expected 9 platforms and SHA256SUMS, got %d names", len(want))
	}
	re := regexp.MustCompile(regexp.QuoteMeta(ghRepo) + `/releases/latest/download/([^"\s<]+)`)
	for _, p := range sitePages(t) {
		for _, m := range re.FindAllStringSubmatch(siteRead(t, p), -1) {
			if !want[m[1]] {
				t.Errorf("%s: download name %q is not a release asset", p, m[1])
			}
		}
		if regexp.MustCompile(`releases/download/v?[0-9]`).MatchString(siteRead(t, p)) {
			t.Errorf("%s links a versioned release", p)
		}
	}
	inst := siteRead(t, "docs/installation/index.html")
	for n := range want {
		if !strings.Contains(inst, ghRepo+"/releases/latest/download/"+n) {
			t.Errorf("the installation page does not link %s", n)
		}
	}
	home := siteDoc(t, "index.html")
	panels := home.Find("[data-os-tabs] [data-os]")
	if panels.Length() != 3 {
		t.Fatalf("home has %d os panels, want 3", panels.Length())
	}
	panels.Each(func(_ int, s *goquery.Selection) {
		c := strings.TrimSpace(s.Find("code").Text())
		cp, _ := s.Find("button[data-copy]").Attr("data-copy")
		if c != cp {
			t.Errorf("os panel %q: shown command %q differs from the copied one %q", s.AttrOr("data-os", ""), c, cp)
		}
		if _, hidden := s.Find("button[data-copy]").Attr("hidden"); !hidden {
			t.Errorf("os panel %q: the copy button must be hidden until the script runs", s.AttrOr("data-os", ""))
		}
		name := c[strings.LastIndex(c, "/")+1:]
		if !want[name] {
			t.Errorf("os panel command %q does not end in an asset name", c)
		}
	})
}

// TestE13_12_FirstStartBlock checks that the first-start output on the site is what the program prints (E13.12).
func TestE13_12_FirstStartBlock(t *testing.T) {
	src := readRoot(t, "internal/app/app.go")
	lines := []string{
		"=== HotspotPlus Lite first start ===",
		"You will be asked to set a new password right after you log in.",
		"Forgot it later? Run: hotspotpluslight admin set-password",
		"====================================",
		"admin account created with the default password; it must be changed at first login",
		"migrations applied",
	}
	for _, l := range lines {
		if !strings.Contains(src, l) {
			t.Errorf("internal/app no longer prints %q", l)
		}
		for _, p := range []string{"index.html", "docs/installation/index.html"} {
			if !strings.Contains(siteText(t, p), l) {
				t.Errorf("%s lacks the real output line %q", p, l)
			}
		}
	}
	for _, p := range []string{"index.html", "docs/installation/index.html"} {
		text := siteText(t, p)
		if !strings.Contains(text, "Log in with username: admin  password: admin") || !strings.Contains(text, "radius_auth=[::]:1812 radius_acct=[::]:1813") {
			t.Errorf("%s: the real first-start lines are incomplete", p)
		}
	}
}

// TestE13_13_RouterAndPagesFacts checks the router script, the designs, the files, the reason codes and the MESSAGES lines against the program (E13.13).
func TestE13_13_RouterAndPagesFacts(t *testing.T) {
	script := routerpack.SetupScript(routerpack.ScriptConfig{Secret: "YOUR-SHARED-SECRET", AuthPort: 1812, AcctPort: 1813, CoaPort: 3799, InterimSeconds: 60})
	router := siteDoc(t, "docs/router-setup/index.html")
	n := 0
	// The Wi-Fi only section holds extra commands that are not part of the setup script.
	router.Find("section:not(:has(#wifi-only)) pre code").Each(func(_ int, s *goquery.Selection) {
		for _, line := range strings.Split(s.Text(), "\n") {
			if !strings.HasPrefix(line, "/radius") && !strings.HasPrefix(line, "/ip hotspot") {
				continue
			}
			n++
			if !strings.Contains(script, "\n"+line+"\n") && !strings.Contains(script, "\n# "+line+"\n") {
				t.Errorf("the router script of the program has no line %q", line)
			}
		}
	})
	if n != 3 {
		t.Errorf("router page shows %d commands, want 3", n)
	}
	if !strings.Contains(script, "<SERVER-IP>") || !strings.Contains(siteText(t, "docs/router-setup/index.html"), "<SERVER-IP>") {
		t.Error("<SERVER-IP> is missing from the script or the router page")
	}
	if !strings.Contains(script, "login-by=http-pap") || !strings.Contains(script, "flash/hotspot") {
		t.Error("the program's script no longer says login-by=http-pap and flash/hotspot")
	}

	pack, err := routerpack.New()
	if err != nil {
		t.Fatal(err)
	}
	pages := siteDoc(t, "docs/hotspot-login-pages/index.html")
	got := map[string][2]string{}
	pages.Find("section:has(#designs) table tr").Each(func(_ int, tr *goquery.Selection) {
		c := tr.Find("td")
		if c.Length() == 3 {
			got[strings.TrimSpace(c.Eq(0).Text())] = [2]string{strings.TrimSpace(c.Eq(1).Text()), strings.TrimSpace(c.Eq(2).Text())}
		}
	})
	if len(got) != len(pack.Designs()) {
		t.Errorf("the page lists %d designs, the program has %d", len(got), len(pack.Designs()))
	}
	for _, d := range pack.Designs() {
		if g := got[d.Key]; g[0] != d.Title || g[1] != d.About {
			t.Errorf("design %s: page has %q, program has %q / %q", d.Key, g, d.Title, d.About)
		}
	}
	files, err := pack.Files("air")
	if err != nil {
		t.Fatal(err)
	}
	pages.Find("#files").Parent().Find("li code").Each(func(_ int, s *goquery.Selection) {
		name := strings.TrimSpace(s.Text())
		if strings.ContainsAny(name, " <>") {
			return
		}
		if _, ok := files[name]; ok {
			return
		}
		if strings.HasSuffix(name, "/") && slices.ContainsFunc(slices.Collect(mapKeys(files)), func(f string) bool { return strings.HasPrefix(f, name) }) {
			return
		}
		t.Errorf("the page names %q, which is not in the hotspot folder", name)
	})

	rows := map[string]string{}
	pages.Find("#codes").Parent().Find("table tr").Each(func(_ int, tr *goquery.Selection) {
		c := tr.Find("td")
		if c.Length() == 3 {
			rows[strings.TrimSpace(c.Eq(0).Text())] = strings.TrimSpace(c.Eq(1).Text())
		}
	})
	if len(rows) != len(domain.AllReasons()) {
		t.Errorf("the page lists %d codes, the program has %d", len(rows), len(domain.AllReasons()))
	}
	for _, r := range domain.AllReasons() {
		if rows[string(r)] != r.Message() {
			t.Errorf("code %s: page has %q, program has %q", r, rows[string(r)], r.Message())
		}
	}
	block := routerpack.MessagesBlock()
	pages.Find("#translate").Parent().Find("pre code").Each(func(_ int, s *goquery.Selection) {
		for _, line := range strings.Split(s.Text(), "\n") {
			if line != "..." && !strings.Contains(block, line) {
				t.Errorf("the MESSAGES example line %q is not in the real block", line)
			}
		}
	})
}

func mapKeys(m map[string][]byte) func(yield func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

// TestE13_14_DocsLayout checks the shared sidebar, the prev and next links, the heading order and the link text of the docs pages (E13.14).
func TestE13_14_DocsLayout(t *testing.T) {
	var order []string
	var labels []string
	siteDoc(t, "docs/index.html").Find(".side nav > ul > li > a").Each(func(_ int, s *goquery.Selection) {
		h, _ := s.Attr("href")
		order = append(order, path.Join("docs", h, "index.html"))
		labels = append(labels, strings.TrimSpace(s.Text()))
	})
	if len(order) == 0 {
		t.Fatal("the docs sidebar is empty")
	}
	pages := docsPages(t)
	if len(pages) != len(order) {
		t.Errorf("%d docs pages, %d sidebar entries: %v", len(pages), len(order), order)
	}
	for i, p := range order {
		if !slices.Contains(pages, p) {
			t.Errorf("sidebar page %s does not exist", p)
			continue
		}
		d := siteDoc(t, p)
		var got, gotHrefs []string
		d.Find(".side nav > ul > li > a").Each(func(_ int, s *goquery.Selection) {
			got = append(got, strings.TrimSpace(s.Text()))
			gotHrefs = append(gotHrefs, path.Join(path.Dir(p), s.AttrOr("href", ""), "index.html"))
		})
		if !slices.Equal(got, labels) {
			t.Errorf("%s: sidebar %v, want %v", p, got, labels)
		}
		if !slices.Equal(gotHrefs, order) {
			t.Errorf("%s: sidebar links resolve to %v, want %v", p, gotHrefs, order)
		}
		var subs, ids []string
		d.Find(`.side a[aria-current="page"]`).Parent().Find("ul.sub a").Each(func(_ int, s *goquery.Selection) { subs = append(subs, s.AttrOr("href", "")) })
		d.Find(".doc section h2").Each(func(_ int, s *goquery.Selection) { ids = append(ids, "#"+s.AttrOr("id", "")) })
		if !slices.Equal(subs, ids) {
			t.Errorf("%s: sub links %v, h2 ids %v", p, subs, ids)
		}
		if d.Find(`.side a[aria-current="page"]`).Length() != 1 {
			t.Errorf("%s: no single current page in the sidebar", p)
		}
		if sub := d.Find(`.side a[aria-current="page"]`).Parent().Find("ul.sub a").Length(); sub != d.Find(".doc section h2").Length() {
			t.Errorf("%s: %d sub links for %d sections", p, sub, d.Find(".doc section h2").Length())
		}
		check := func(rel, want string, label string) {
			h, ok := d.Find(`.pager a[rel="` + rel + `"]`).Attr("href")
			if want == "" {
				if ok {
					t.Errorf("%s: has a %s link", p, rel)
				}
				return
			}
			if got := path.Join(path.Dir(p), h, "index.html"); got != want {
				t.Errorf("%s: %s link goes to %s, want %s", p, rel, got, want)
			}
			c := d.Find(`.pager a[rel="` + rel + `"]`).Clone()
			c.Find("small").Remove()
			if txt := strings.TrimSpace(c.Text()); txt != label {
				t.Errorf("%s: %s link text %q, want the sidebar label %q", p, rel, txt, label)
			}
		}
		prev, next, prevLabel, nextLabel := "", "", "", ""
		if i > 0 {
			prev, prevLabel = order[i-1], labels[i-1]
		}
		if i < len(order)-1 {
			next, nextLabel = order[i+1], labels[i+1]
		}
		check("prev", prev, prevLabel)
		check("next", next, nextLabel)
		last := 1
		d.Find("h1, h2, h3, h4").Each(func(_ int, s *goquery.Selection) {
			lvl := int(s.Nodes[0].Data[1] - '0')
			if lvl > last+1 {
				t.Errorf("%s: heading level jumps from h%d to h%d", p, last, lvl)
			}
			last = lvl
		})
	}
	for _, p := range sitePages(t) {
		siteDoc(t, p).Find("a").Each(func(_ int, s *goquery.Selection) {
			if strings.TrimSpace(s.Text()) == "" && s.AttrOr("aria-label", "") == "" {
				t.Errorf("%s: a link without text", p)
			}
		})
	}
}

// TestE13_15_SearchIndex checks that the search index matches the docs sections (E13.15).
func TestE13_15_SearchIndex(t *testing.T) {
	src := siteRead(t, "assets/js/search-index.js")
	src = strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(src[strings.Index(src, "export default"):], "export default")), ";")
	var index []struct{ Page, Heading, URL, Keywords string }
	if err := json.Unmarshal([]byte(src), &index); err != nil {
		t.Fatal(err)
	}
	in := map[string]bool{}
	heading := map[string]string{}
	keywords := map[string]string{}
	for _, e := range index {
		heading[e.URL] = e.Heading
		file, frag, _ := strings.Cut(e.URL, "#")
		target := path.Join(file, "index.html")
		if !siteIDs(t, target)[frag] {
			t.Errorf("index entry %q has no section %s", e.URL, frag)
		}
		if e.Page == "" || e.Heading == "" || e.Keywords == "" {
			t.Errorf("index entry %q is incomplete", e.URL)
		}
		in[e.URL] = true
		keywords[e.URL] = e.Keywords
	}
	for _, p := range docsPages(t) {
		dir := path.Dir(p)
		siteDoc(t, p).Find(".doc section h2").Each(func(_ int, s *goquery.Selection) {
			u := dir + "/#" + s.AttrOr("id", "")
			if !in[u] {
				t.Errorf("section %s is not in the search index", u)
				return
			}
			if h := strings.TrimSpace(s.Text()); heading[u] != h {
				t.Errorf("index heading for %s is %q, the h2 says %q", u, heading[u], h)
			}
			// The keywords are the start of the section text, so a changed section makes a stale entry fail here.
			if want := sectionKeywordText(s); !strings.HasPrefix(want, squash(keywords[u])) {
				t.Errorf("the index keywords of %s no longer match the start of its section: rebuild the entry", u)
			}
		})
	}
}

// squash removes all white space, so texts can be compared whatever the spacing between tags.
func squash(s string) string { return strings.Join(strings.Fields(s), "") }

// sectionKeywordText returns the text of the section of the h2 (without the h2), with links of the form scheme://... removed
// like the search index does, and with white space removed.
func sectionKeywordText(h2 *goquery.Selection) string {
	var parts []string
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			switch {
			case c.Type == html.TextNode:
				parts = append(parts, c.Data)
			case c.Type == html.ElementNode && c != h2.Nodes[0] && c.Data != "script" && c.Data != "style":
				walk(c)
			}
		}
	}
	walk(h2.Closest("section").Nodes[0])
	var kept []string
	for _, w := range strings.Fields(strings.Join(parts, " ")) {
		if !strings.Contains(w, "://") {
			kept = append(kept, w)
		}
	}
	return squash(strings.Join(kept, " "))
}

// TestE13_16_HomeFacts checks the home page text and the claims behind its stats against go.mod, the Makefile and docs/perf-results.md (E13.16).
func TestE13_16_HomeFacts(t *testing.T) {
	home := siteDoc(t, "index.html")
	if h1 := strings.Join(strings.Fields(home.Find("h1").Text()), " "); h1 != "Wi-Fi accounts for your MikroTik hotspot, on your own network." {
		t.Errorf("h1 %q", h1)
	}
	if em := strings.TrimSpace(home.Find("h1 em").Text()); em != "on your own network." {
		t.Errorf("h1 emphasis %q", em)
	}
	if lead := strings.TrimSpace(home.Find(".lead").Text()); lead != "Members, vouchers, plans and live sessions. One program and one SQLite file. No cloud, no database server." {
		t.Errorf("lead %q", lead)
	}
	if n := home.Find(".bento .cell").Length(); n != 5 {
		t.Errorf("%d bento cells, want 5", n)
	}
	text := siteText(t, "index.html")
	for _, s := range []string{"Members and vouchers", "Plans", "Limits that hold", "Your login page", "Runs anywhere", "8 designs", "9 builds", "Windows service",
		"1binary to deploy", "1SQLite file", "30,000online users tested", "Open source, AGPL-3.0"} {
		if !strings.Contains(strings.ReplaceAll(text, "\n", ""), s) {
			t.Errorf("home lacks %q", s)
		}
	}
	if href, _ := home.Find(`a[href^="` + ghLegacy + `"]`).Attr("href"); href != ghLegacy {
		t.Error("home does not credit the original HotspotPlus")
	}
	perf := readRoot(t, "docs/perf-results.md")
	m := regexp.MustCompile(`Max online users on this machine: \*\*(\d+)\*\*`).FindStringSubmatch(perf)
	if m == nil || m[1] != "30000" {
		t.Errorf("docs/perf-results.md max online users %v: the home page says 30,000", m)
	}
	gomod := readRoot(t, "go.mod")
	if !strings.Contains(gomod, "modernc.org/sqlite") || strings.Contains(gomod, "mattn/go-sqlite3") {
		t.Error("go.mod: the 1 binary / SQLite claim needs the pure Go driver")
	}
	if !strings.Contains(readRoot(t, "Makefile"), "CGO_ENABLED=0") {
		t.Error("the Makefile no longer builds with CGO_ENABLED=0")
	}
	if foot := strings.Join(strings.Fields(home.Find("footer").Text()), " "); !strings.Contains(foot, "Open source, AGPL-3.0") || !strings.Contains(foot, "Docs") || !strings.Contains(foot, "GitHub") || !strings.Contains(foot, "Issues") {
		t.Errorf("footer %q", foot)
	}
	var issues bool
	home.Find("footer a").Each(func(_ int, s *goquery.Selection) {
		if h, _ := s.Attr("href"); h == ghRepo+"/issues" {
			issues = true
		}
	})
	if !issues {
		t.Error("footer has no issues link")
	}
	all := strings.ToLower(siteText(t))
	for _, s := range []string{"plain http", "local network"} {
		if !strings.Contains(all, s) {
			t.Errorf("the site does not say %q", s)
		}
	}
	for _, s := range []string{"caddy", "nginx", "reverse_proxy", "let's encrypt", "letsencrypt"} {
		if strings.Contains(all, s) {
			t.Errorf("the site has a reverse proxy or HTTPS setup (%q): it is LAN HTTP only", s)
		}
	}
	for _, s := range []string{"README on GitHub", "diagrams"} {
		if !strings.Contains(siteText(t, "docs/how-it-works/index.html", "docs/architecture/index.html"), s) {
			t.Errorf("how-it-works and architecture do not link to the README diagrams (%q)", s)
		}
	}
}

// TestE13_17_PagesWorkflow checks the Pages workflow, the README note on the base URL and that the base URL is the same everywhere (E13.17).
func TestE13_17_PagesWorkflow(t *testing.T) {
	wf := readRoot(t, ".github/workflows/pages.yml")
	for _, want := range []string{
		"push:", "branches: [main]", "- site/**", "workflow_dispatch:", "pages: write", "id-token: write",
		"actions/checkout@v5", "actions/configure-pages@v6", "actions/upload-pages-artifact@v5", "path: site", "actions/deploy-pages@v5",
		"group: pages", "environment:", "name: github-pages",
	} {
		if !strings.Contains(wf, want) {
			t.Errorf("pages.yml lacks %q", want)
		}
	}
	readme := readRoot(t, "README.md")
	base := siteBase(t)
	if !strings.Contains(readme, base) || !strings.Contains(readme, "site/") || !strings.Contains(strings.ToLower(readme), "base url") {
		t.Error("README has no note on where to change the site base url")
	}
	// The base url must be the same everywhere it is used.
	pat := regexp.MustCompile(`https://[a-z0-9.-]+\.github\.io/[A-Za-z0-9_-]+/`)
	for _, f := range append(sitePages(t), "sitemap.xml", "robots.txt") {
		for _, u := range pat.FindAllString(siteRead(t, f), -1) {
			if u != base {
				t.Errorf("%s: base url %q differs from %q", f, u, base)
			}
		}
	}
}
