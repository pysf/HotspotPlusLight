package admin

import (
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"hotspotpluslight/internal/linkstatus"
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

// routerPage shows the page designs, the setup script and the link status.
func (h handlers) routerPage(w http.ResponseWriter, r *http.Request) {
	st, err := h.Settings.Get(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	loc, err := st.Location()
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
	h.render(w, r, http.StatusOK, RouterPage(layout(r, "Router", routerHome), RouterProps{
		Designs: h.Router.Designs(), Script: script, Link: linkRows(h.Link.Snapshot(), loc),
	}))
}

// linkStatus shows the link status panel alone. The router page polls it every 5 seconds.
func (h handlers) linkStatus(w http.ResponseWriter, r *http.Request) {
	st, err := h.Settings.Get(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	loc, err := st.Location()
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, LinkStatusPanel(linkRows(h.Link.Snapshot(), loc)))
}

// RouterProps holds what the router page shows.
type RouterProps struct {
	Designs []routerpack.Design
	Script  string
	Link    linkView
}

// linkEvent is one row of the link status table.
type linkEvent struct{ Key, Label, At, From, Detail string }

// badSource is one row of the bad-secret table.
type badSource struct{ From, Count, At string }

// linkView is the link status prepared for display.
type linkView struct {
	Events     []linkEvent
	BadTotal   string
	BadSources []badSource
}

// linkTimeFormat is the time format of the link status.
const linkTimeFormat = "2006-01-02 15:04:05"

// linkRows returns the link status in snapshot s with times in loc.
func linkRows(s linkstatus.Snapshot, loc *time.Location) linkView {
	ev := func(key, label string, e linkstatus.Event) linkEvent {
		if e.At.IsZero() {
			return linkEvent{Key: key, Label: label, At: "never"}
		}
		return linkEvent{key, label, e.At.In(loc).Format(linkTimeFormat), e.From, e.Detail}
	}
	v := linkView{
		Events: []linkEvent{
			ev("access-request", "Last Access-Request", s.AccessRequest),
			ev("accounting", "Last Accounting packet", s.Accounting),
			ev("disconnect", "Last Disconnect", s.Disconnect),
		},
		BadTotal: strconv.Itoa(s.BadSecretTotal),
	}
	for _, b := range s.BadSecretSources {
		v.BadSources = append(v.BadSources, badSource{b.From, strconv.Itoa(b.Count), b.Last.In(loc).Format(linkTimeFormat)})
	}
	return v
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
