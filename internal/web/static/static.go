// Package static holds the admin panel's assets: our own CSS and JS, htmx and the
// Inter font (vendored, pinned). There is no build tool. At startup New inlines the
// @imports of css/app.css into one stylesheet, hashes every asset, and serves them
// under /admin/static/ with the hash in the URL (?v=hash) and a long cache.
package static

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"
)

//go:embed css js htmx.min.js voucher.css inter-latin-wght-normal.woff2
var files embed.FS

// Prefix is where the assets are mounted.
const Prefix = "/admin/static/"

type asset struct {
	body  []byte
	ctype string
	hash  string
}

// Assets is the set of served files. Build it once with New.
type Assets struct{ items map[string]asset }

var (
	importRe = regexp.MustCompile(`(?m)^@import "([^"]+)";[ \t]*\n?`)
	fontRe   = regexp.MustCompile(`url\("` + regexp.QuoteMeta(Prefix) + `([^"]+)"\)`)
)

// New reads the embedded files and builds the CSS bundle.
func New() (*Assets, error) {
	a := &Assets{items: map[string]asset{}}
	plain := map[string]string{
		"htmx.min.js":                   "text/javascript; charset=utf-8",
		"voucher.css":                   "text/css; charset=utf-8",
		"inter-latin-wght-normal.woff2": "font/woff2",
	}
	for name, ctype := range plain {
		b, err := files.ReadFile(name)
		if err != nil {
			return nil, err
		}
		a.add(name, b, ctype)
	}
	js, err := fs.Glob(files, "js/*.js")
	if err != nil {
		return nil, err
	}
	for _, name := range js {
		b, err := files.ReadFile(name)
		if err != nil {
			return nil, err
		}
		a.add(name, b, "text/javascript; charset=utf-8")
	}
	bundle, err := a.bundleCSS(files)
	if err != nil {
		return nil, err
	}
	a.add("app.css", bundle, "text/css; charset=utf-8")
	return a, nil
}

func (a *Assets) add(name string, body []byte, ctype string) {
	sum := sha256.Sum256(body)
	a.items[name] = asset{body: body, ctype: ctype, hash: hex.EncodeToString(sum[:])[:12]}
}

// bundleCSS inlines the @import lines of css/app.css in order and gives fonts a hashed URL.
func (a *Assets) bundleCSS(src fs.FS) ([]byte, error) {
	root, err := fs.ReadFile(src, "css/app.css")
	if err != nil {
		return nil, err
	}
	var ierr error
	out := importRe.ReplaceAllFunc(root, func(m []byte) []byte {
		name := importRe.FindSubmatch(m)[1]
		b, err := fs.ReadFile(src, path.Join("css", string(name)))
		if err != nil {
			ierr = fmt.Errorf("css import %q: %w", name, err)
			return nil
		}
		return append(bytes.TrimRight(b, "\n"), '\n')
	})
	if ierr != nil {
		return nil, ierr
	}
	out = fontRe.ReplaceAllFunc(out, func(m []byte) []byte {
		name := fontRe.FindSubmatch(m)[1]
		it, ok := a.items[string(name)]
		if !ok {
			ierr = fmt.Errorf("css url %q: no such asset", name)
			return m
		}
		return []byte(`url("` + Prefix + string(name) + `?v=` + it.hash + `")`)
	})
	return out, ierr
}

// URL returns the public URL of an asset, with its hash. An unknown name gets no hash.
func (a *Assets) URL(name string) string {
	if it, ok := a.items[name]; ok {
		return Prefix + name + "?v=" + it.hash
	}
	return Prefix + name
}

// Handler serves the assets. Mount it with http.StripPrefix(Prefix, ...).
// A URL with the current hash is cached for a year; any other URL must revalidate.
func (a *Assets) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		it, ok := a.items[strings.TrimPrefix(r.URL.Path, "/")]
		if !ok || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
			w.Header().Set("Cache-Control", "no-store")
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", it.ctype)
		w.Header().Set("ETag", `"`+it.hash+`"`)
		if r.URL.Query().Get("v") == it.hash {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(it.body))
	})
}
