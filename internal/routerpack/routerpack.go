// Package routerpack builds the hotspot folders that the admin uploads to the MikroTik
// router (PLAN 9.4, D22), and the RouterOS setup script. There are 8 downloads: the
// MikroTik default pages as RouterOS ships them, and 7 themes of one layout. Zips are
// built on request from embedded sources; nothing is stored.
package routerpack

import (
	"archive/zip"
	"bytes"
	"embed"
	"fmt"
	"html"
	"io/fs"
	"path"
	"slices"
	"strings"
	"time"

	"hotspotpluslight/internal/domain"
)

// assets holds the page sources. The mikrotik folder is a byte-for-byte copy of
// reference/hotspot (E12.00); go:embed cannot reach ../../reference.
//
//go:embed mikrotik src
var assets embed.FS

// folder is the directory name inside the zip. The admin uploads its content to the router.
const folder = "hotspot"

// KeyMikrotik is the key of the unchanged MikroTik default pages.
const KeyMikrotik = "mikrotik"

// DefaultKey is the theme shown first among the themes.
const DefaultKey = "air"

// Design is one downloadable hotspot folder: the MikroTik default or a theme.
type Design struct {
	Key   string
	Title string
	About string
}

// Themes lists the 7 themes in display order. A new theme needs a file
// src/css/themes/<key>.css and a line here; TestThemeSource checks that they agree.
var Themes = []Design{
	{"air", "Air", "Light grey with Apple blue."},
	{"midnight", "Midnight", "Graphite with a blue glow and a faint grid."},
	{"aurora", "Aurora", "Pastel mesh with a frosted glass card."},
	{"dusk", "Dusk", "Violet-black with a glow from below."},
	{"ripple", "Ripple", "Sage paper with signal rings."},
	{"sunrise", "Sunrise", "Warm cream with a low sun."},
	{"ocean", "Ocean", "Deep teal with a white card."},
}

// Pack builds the folders. It has no state.
type Pack struct{}

// zipTime is the time of every zip entry, so the same design always gives the same zip.
var zipTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// New returns a Pack. It fails only if the binary was built with a broken asset tree.
func New() (*Pack, error) {
	p := &Pack{}
	for _, d := range p.Designs() {
		if _, err := p.Files(d.Key); err != nil {
			return nil, fmt.Errorf("%s: %w", d.Key, err)
		}
	}
	return p, nil
}

// Designs lists the 8 downloads: MikroTik default first, then the themes.
func (*Pack) Designs() []Design {
	return append([]Design{{KeyMikrotik, "MikroTik default", "The pages exactly as RouterOS ships them. Failed logins show the raw code."}}, Themes...)
}

// Design returns the download with the given key, and false if there is none.
func (p *Pack) Design(key string) (Design, bool) {
	for _, d := range p.Designs() {
		if d.Key == key {
			return d, true
		}
	}
	return Design{}, false
}

// Files returns the content of the folder of design key, by path inside the folder.
// It fails for an unknown key.
func (p *Pack) Files(key string) (map[string][]byte, error) {
	if key == KeyMikrotik {
		return readTree("mikrotik", nil)
	}
	d, ok := p.Design(key)
	if !ok {
		return nil, fmt.Errorf("unknown design %q", key)
	}
	css, err := p.css(d.Key)
	if err != nil {
		return nil, err
	}
	replace := func(name string, b []byte) []byte {
		if !strings.HasSuffix(name, ".html") {
			return b
		}
		s := strings.ReplaceAll(string(b), "@THEME@", d.Key)
		s = strings.ReplaceAll(s, "@MESSAGES@", MessagesBlock())
		return []byte(s)
	}
	files, err := readTree("src/hotspot", replace)
	if err != nil {
		return nil, err
	}
	files["css/hotspot.css"] = css
	for n, b := range files {
		if bytes.Contains(b, []byte("@THEME@")) || bytes.Contains(b, []byte("@MESSAGES@")) {
			return nil, fmt.Errorf("%s: placeholder left in the page", n)
		}
	}
	return files, nil
}

// css returns the shared layout plus the token block of theme key.
func (*Pack) css(key string) ([]byte, error) {
	layout, err := assets.ReadFile("src/css/layout.css")
	if err != nil {
		return nil, err
	}
	block, err := assets.ReadFile("src/css/themes/" + key + ".css")
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	b.Write(layout)
	b.WriteString("\n/* ===== THEME ===== */\n")
	b.Write(block)
	return b.Bytes(), nil
}

// MessagesBlock returns the code-to-text block of login.html (the "MESSAGES" block).
// It has one branch per reject code, then a fallback that shows any other text as it is.
func MessagesBlock() string {
	var b strings.Builder
	for i, r := range domain.AllReasons() {
		word := "elif"
		if i == 0 {
			word = "if"
		}
		fmt.Fprintf(&b, "$(%s error == \"%s\")%s\n", word, r, html.EscapeString(r.Message()))
	}
	b.WriteString("$(elif error)$(error)$(endif)")
	return b.String()
}

// readTree returns the files under root, by path relative to root. If edit is not nil,
// it changes each file's content.
func readTree(root string, edit func(name string, b []byte) []byte) (map[string][]byte, error) {
	files := map[string][]byte{}
	err := fs.WalkDir(assets, root, func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := assets.ReadFile(name)
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(name, root+"/")
		if edit != nil {
			b = edit(rel, b)
		}
		files[rel] = b
		return nil
	})
	return files, err
}

// Zip returns the zip of the hotspot folder of design key, with every file under "hotspot/".
// It fails for an unknown key.
func (p *Pack) Zip(key string) ([]byte, error) {
	files, err := p.Files(key)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	slices.Sort(names)
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, n := range names {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: path.Join(folder, n), Method: zip.Deflate, Modified: zipTime})
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
