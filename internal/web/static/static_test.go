package static

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func fakeFS(root string, extra map[string]string) fstest.MapFS {
	m := fstest.MapFS{"css/app.css": {Data: []byte(root)}}
	for k, v := range extra {
		m[k] = &fstest.MapFile{Data: []byte(v)}
	}
	return m
}

func TestBundleOrderAndURLHash(t *testing.T) {
	a := &Assets{items: map[string]asset{}}
	a.add("font.woff2", []byte("fontdata"), "font/woff2")
	src := fakeFS("@import \"b.css\";\n@import \"a.css\";\n.x{src:url(\"/admin/static/font.woff2\")}\n", map[string]string{
		"css/a.css": ".a{}\n", "css/b.css": ".b{}\n",
	})
	out, err := a.bundleCSS(src)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, "@import") {
		t.Fatalf("import left in bundle: %s", s)
	}
	if strings.Index(s, ".b{}") > strings.Index(s, ".a{}") || !strings.Contains(s, ".a{}") {
		t.Fatalf("wrong order: %s", s)
	}
	if !strings.Contains(s, `url("/admin/static/font.woff2?v=`+a.items["font.woff2"].hash+`")`) {
		t.Fatalf("url not hashed: %s", s)
	}
}

func TestBundleUnknownImportFails(t *testing.T) {
	a := &Assets{items: map[string]asset{}}
	if _, err := a.bundleCSS(fakeFS("@import \"missing.css\";\n", nil)); err == nil {
		t.Fatal("want error for unknown import")
	}
}

func TestBundleUnknownURLFails(t *testing.T) {
	a := &Assets{items: map[string]asset{}}
	if _, err := a.bundleCSS(fakeFS(".x{src:url(\"/admin/static/nope.woff2\")}\n", nil)); err == nil {
		t.Fatal("want error for unknown url")
	}
}

func TestNewBuildsRealBundle(t *testing.T) {
	if _, err := New(); err != nil {
		t.Fatal(err)
	}
}

func get(t *testing.T, h http.Handler, url string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.URL.Path = strings.TrimPrefix(req.URL.Path, "/admin/static")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHandlerCaching(t *testing.T) {
	a, err := New()
	if err != nil {
		t.Fatal(err)
	}
	h := a.Handler()
	good := get(t, h, a.URL("app.css"))
	if good.Code != 200 || !strings.Contains(good.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("matching hash: %d %q", good.Code, good.Header().Get("Cache-Control"))
	}
	for _, u := range []string{"/admin/static/app.css", "/admin/static/app.css?v=wrong"} {
		r := get(t, h, u)
		if r.Code != 200 || r.Header().Get("Cache-Control") != "no-cache" {
			t.Fatalf("%s: %d %q", u, r.Code, r.Header().Get("Cache-Control"))
		}
	}
}

func TestHandler404(t *testing.T) {
	a, err := New()
	if err != nil {
		t.Fatal(err)
	}
	h := a.Handler()
	for _, u := range []string{"/admin/static/nope.js", "/admin/static/base.css", "/admin/static/components/button.css", "/admin/static/tokens.css"} {
		r := get(t, h, u)
		if r.Code != 404 || r.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: %d %q", u, r.Code, r.Header().Get("Cache-Control"))
		}
	}
}
