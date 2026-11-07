package admin

import (
	"net/http"
	"slices"
	"strconv"

	"github.com/go-chi/chi/v5"

	"hotspotpluslight/internal/routerpack"
)

// RouterPack is the page design part of internal/routerpack.
type RouterPack interface {
	Designs() []routerpack.Design
	Design(key string) (routerpack.Design, bool)
	Zip(key string) ([]byte, error)
	Preview(key, page, errCode string) ([]byte, error)
	PreviewAsset(key, name string) ([]byte, string, bool)
}

// routerHome is the path of the router page.
const routerHome = "/admin/router"

// zipURL returns the download path of the zip of design key.
func zipURL(key string) string { return routerHome + "/pages/" + key + ".zip" }

// previewURL returns the preview path of design key.
func previewURL(key string) string { return routerHome + "/pages/" + key + "/preview" }

// routerPage shows the page designs and the setup script.
func (h handlers) routerPage(w http.ResponseWriter, r *http.Request) {
	st, err := h.Settings.Get(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	// MONITOR: the script shows the RADIUS shared secret in plain text (read-only textarea) because
	// the pasted script needs it. Admin listener only, behind login, Cache-Control: no-store (admin group).
	authPort, acctPort := h.RadiusPorts()
	script := routerpack.SetupScript(routerpack.ScriptConfig{
		Secret: st.RadiusSharedSecret, AuthPort: authPort, AcctPort: acctPort,
		CoaPort: st.CoaPort, InterimSeconds: st.AccountingIntervalSeconds,
	})
	h.render(w, r, http.StatusOK, RouterPage(layout(r, "Router", routerHome), RouterProps{Designs: h.Router.Designs(), Script: script}))
}

// RouterProps holds what the router page shows.
type RouterProps struct {
	Designs []routerpack.Design
	Script  string
}

// pack returns the design of {key}. For an unknown design it answers 404 and returns false.
func (h handlers) pack(w http.ResponseWriter, r *http.Request) (routerpack.Design, bool) {
	d, ok := h.Router.Design(chi.URLParam(r, "key"))
	if !ok {
		h.notFound(w, r, "Page design")
	}
	return d, ok
}

// packDownload sends the zip with the full hotspot folder of one design.
func (h handlers) packDownload(w http.ResponseWriter, r *http.Request) {
	d, ok := h.pack(w, r)
	if !ok {
		return
	}
	zipped, err := h.Router.Zip(d.Key)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="hotspot-`+d.Key+`.zip"`)
	h.blob(w, "application/zip", zipped)
}

// previewCSP limits the preview, which is shown on the admin origin, to its own stylesheet and images.
const previewCSP = "default-src 'none'; style-src 'self'; img-src 'self' data:; form-action 'none'; base-uri 'none'"

// packPreview shows one page of a design with sample values in place of the router variables.
// The query has page (login, alogin, status, logout or error) and error (a reject code).
func (h handlers) packPreview(w http.ResponseWriter, r *http.Request) {
	d, ok := h.pack(w, r)
	if !ok {
		return
	}
	page := r.URL.Query().Get("page")
	if page == "" {
		page = "login"
	}
	if !slices.Contains(routerpack.PreviewPages, page) {
		h.notFound(w, r, "Page")
		return
	}
	b, err := h.Router.Preview(d.Key, page, r.URL.Query().Get("error"))
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	w.Header().Set("Content-Security-Policy", previewCSP)
	h.blob(w, "text/html; charset=utf-8", b)
}

// packAsset sends a stylesheet or image that the preview page links to.
func (h handlers) packAsset(w http.ResponseWriter, r *http.Request) {
	d, ok := h.pack(w, r)
	if !ok {
		return
	}
	b, ct, ok := h.Router.PreviewAsset(d.Key, chi.URLParam(r, "*"))
	if !ok {
		h.notFound(w, r, "File")
		return
	}
	w.Header().Set("Content-Security-Policy", "default-src 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	h.blob(w, ct, b)
}

// blob writes b as the response body with the given content type.
func (h handlers) blob(w http.ResponseWriter, contentType string, b []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(b)))
	w.Write(b)
}
