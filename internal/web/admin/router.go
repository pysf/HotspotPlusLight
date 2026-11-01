package admin

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"hotspotpluslight/internal/domain"
	"hotspotpluslight/internal/routerpack"
)

// RouterPack is the starter hotspot folder generator (implemented by internal/routerpack).
type RouterPack interface {
	Themes() []routerpack.Theme
	Theme(id string) (routerpack.Theme, bool)
	Background(t routerpack.Theme) ([]byte, error)
	Logo() ([]byte, error)
	LoginHTML(t routerpack.Theme, c routerpack.Config) ([]byte, error)
	Zip(t routerpack.Theme, c routerpack.Config) ([]byte, error)
}

const routerHome = "/admin/router"

func packConfig(st domain.Settings) routerpack.Config {
	return routerpack.Config{SiteTitle: st.SiteTitle, PublicBaseURL: st.PublicBaseURL}
}

func themeURL(id string) string { return routerHome + "/themes/" + id }

func downloadURL(id string) string { return routerHome + "/files.zip?theme=" + id }

type themeGroup struct {
	Category string
	Themes   []routerpack.Theme
}

// groups splits the (category-sorted) theme list into one group per category.
func groups(themes []routerpack.Theme) []themeGroup {
	var out []themeGroup
	for _, t := range themes {
		if n := len(out); n == 0 || out[n-1].Category != t.Category {
			out = append(out, themeGroup{Category: t.Category})
		}
		out[len(out)-1].Themes = append(out[len(out)-1].Themes, t)
	}
	return out
}

func (h handlers) routerPage(w http.ResponseWriter, r *http.Request) {
	st, err := h.Settings.Get(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, RouterPage(layout(r, "Router", routerHome), st.PublicBaseURL == "", routerpack.DefaultTheme))
}

func (h handlers) themeGallery(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, http.StatusOK, ThemeGalleryPage(layout(r, "Themes", routerHome), groups(h.Router.Themes())))
}

// theme loads {id}; an unknown theme is a 404.
func (h handlers) theme(w http.ResponseWriter, r *http.Request) (routerpack.Theme, bool) {
	t, ok := h.Router.Theme(chi.URLParam(r, "id"))
	if !ok {
		h.notFound(w, r, "Theme")
	}
	return t, ok
}

func (h handlers) themePage(w http.ResponseWriter, r *http.Request) {
	if t, ok := h.theme(w, r); ok {
		h.render(w, r, http.StatusOK, ThemePage(layout(r, t.Category+" "+t.Title, routerHome), t))
	}
}

// themePreview serves the theme's login page as it would be in the downloaded folder; its
// relative references (background.jpg, logo.png) resolve to the sibling routes below.
// The page and its script come from the generator, not from the admin, but they run on the admin
// origin, so the response sandboxes them (opaque origin, no network, no forms) and the page is built
// without the API base (no API calls in a preview).
func (h handlers) themePreview(w http.ResponseWriter, r *http.Request) {
	t, ok := h.theme(w, r)
	if !ok {
		return
	}
	st, err := h.Settings.Get(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	cfg := packConfig(st)
	cfg.PublicBaseURL = ""
	page, err := h.Router.LoginHTML(t, cfg)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	w.Header().Set("Content-Security-Policy", previewCSP)
	h.blob(w, "text/html; charset=utf-8", page)
}

// previewCSP: scripts may run (the page's own), nothing may be fetched except the theme's images.
const previewCSP = "sandbox allow-scripts; default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; img-src 'self' data:; connect-src 'none'"

func (h handlers) themeBackground(w http.ResponseWriter, r *http.Request) {
	t, ok := h.theme(w, r)
	if !ok {
		return
	}
	b, err := h.Router.Background(t)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.blob(w, "image/jpeg", b)
}

func (h handlers) themeLogo(w http.ResponseWriter, r *http.Request) {
	b, err := h.Router.Logo()
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.blob(w, "image/png", b)
}

// packDownload is GET /admin/router/files.zip?theme=<id>: the full hotspot folder (default alpha-fall).
func (h handlers) packDownload(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("theme")
	if id == "" {
		id = routerpack.DefaultTheme
	}
	t, ok := h.Router.Theme(id)
	if !ok {
		h.notFound(w, r, "Theme")
		return
	}
	st, err := h.Settings.Get(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	zipped, err := h.Router.Zip(t, packConfig(st))
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="hotspot-`+t.ID+`.zip"`)
	h.blob(w, "application/zip", zipped)
}

func (h handlers) blob(w http.ResponseWriter, contentType string, b []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(b)))
	w.Write(b)
}
