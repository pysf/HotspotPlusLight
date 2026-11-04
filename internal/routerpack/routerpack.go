// Package routerpack builds the starter hotspot folder that the admin uploads to
// the MikroTik router (PLAN 9.4): one shared login page layout, 38 themes (a
// background photo and two colors each), the small router pages and a logo.
// Packs are generated on request from the embedded sources; nothing is stored.
package routerpack

import (
	"archive/zip"
	"bytes"
	"cmp"
	"embed"
	"encoding/json"
	"fmt"
	"html"
	"io/fs"
	"path"
	"slices"
	"strings"
	"text/template"

	"hotspotpluslight/internal/domain"
)

//go:embed layout pages themes
var assets embed.FS

// DefaultTheme is the legacy default style, used when the admin picks none.
const DefaultTheme = "alpha-fall"

// folder is the directory name inside the zip: the admin uploads its content to the router.
const folder = "hotspot"

// Theme is one downloadable login page style (PLAN 16).
type Theme struct {
	ID       string `json:"id"`
	Category string `json:"category"`
	Style    string `json:"style"`
	Title    string `json:"title"`
	Primary  string `json:"primary"` // CSS color of buttons
	Overlay  string `json:"overlay"` // CSS color laid over the background photo
}

// Config is what the pack is generated from (taken from the settings).
type Config struct {
	SiteTitle string
}

// Pack holds the parsed themes and the login page template.
type Pack struct {
	themes []Theme
	login  *template.Template
}

// New parses the embedded sources. It fails only if the binary was built with a broken asset tree.
func New() (*Pack, error) {
	p := &Pack{}
	err := fs.WalkDir(assets, "themes", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path.Base(name) != "theme.json" {
			return err
		}
		raw, err := assets.ReadFile(name)
		if err != nil {
			return err
		}
		var t Theme
		if err := json.Unmarshal(raw, &t); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if t.ID == "" || t.Category == "" || t.Primary == "" || t.Overlay == "" || path.Dir(name) != path.Join("themes", strings.ToLower(t.Category), t.Style) {
			return fmt.Errorf("%s: incomplete or misplaced manifest", name)
		}
		if b, err := assets.ReadFile(path.Join(path.Dir(name), "background.jpg")); err != nil || len(b) == 0 {
			return fmt.Errorf("%s: no background image", name)
		}
		p.themes = append(p.themes, t)
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.SortFunc(p.themes, func(a, b Theme) int {
		return cmp.Or(cmp.Compare(a.Category, b.Category), cmp.Compare(a.Title, b.Title))
	})
	p.login, err = template.New("login.html.tmpl").Funcs(template.FuncMap{"js": jsLiteral, "html": html.EscapeString}).
		ParseFS(assets, "layout/login.html.tmpl")
	if err != nil {
		return nil, err
	}
	return p, nil
}

// jsLiteral renders v as a JavaScript string literal that is also safe inside <script>.
func jsLiteral(v string) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}

// Themes lists all themes, grouped by category (sorted by category, then title).
func (p *Pack) Themes() []Theme { return slices.Clone(p.themes) }

// Theme finds a theme by id.
func (p *Pack) Theme(id string) (Theme, bool) {
	for _, t := range p.themes {
		if t.ID == id {
			return t, true
		}
	}
	return Theme{}, false
}

func (p *Pack) themeDir(t Theme) string {
	return path.Join("themes", strings.ToLower(t.Category), t.Style)
}

// Background is the theme's photo (JPEG).
func (p *Pack) Background(t Theme) ([]byte, error) {
	return assets.ReadFile(path.Join(p.themeDir(t), "background.jpg"))
}

// Logo is the placeholder logo shared by all themes.
func (p *Pack) Logo() ([]byte, error) { return assets.ReadFile("layout/logo.png") }

// Message is one entry of the page's English dictionary.
type Message struct{ Code, Text string }

type loginData struct {
	Theme
	SiteTitle string
	Messages  []Message
}

// LoginHTML renders the customizable member page of a theme. Router variables
// such as $(link-login-only) are left for the router to substitute.
func (p *Pack) LoginHTML(t Theme, c Config) ([]byte, error) {
	d := loginData{Theme: t, SiteTitle: c.SiteTitle}
	for _, r := range domain.AllReasons() {
		d.Messages = append(d.Messages, Message{string(r), r.Message()})
	}
	var b bytes.Buffer
	if err := p.login.Execute(&b, d); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// Zip is the full hotspot folder of a theme: the login page, its background and
// logo, and the small router pages, all under "hotspot/".
func (p *Pack) Zip(t Theme, c Config) ([]byte, error) {
	files := map[string][]byte{}
	var err error
	if files["login.html"], err = p.LoginHTML(t, c); err != nil {
		return nil, err
	}
	if files["background.jpg"], err = p.Background(t); err != nil {
		return nil, err
	}
	if files["logo.png"], err = p.Logo(); err != nil {
		return nil, err
	}
	pages, err := fs.ReadDir(assets, "pages")
	if err != nil {
		return nil, err
	}
	for _, e := range pages {
		if files[e.Name()], err = assets.ReadFile(path.Join("pages", e.Name())); err != nil {
			return nil, err
		}
	}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	slices.Sort(names)
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, n := range names {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: folder + "/" + n, Method: zip.Deflate})
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(files[n]); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
