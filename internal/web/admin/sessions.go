package admin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"unicode"
	"unicode/utf8"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"hotspotpluslight/internal/domain"
	"hotspotpluslight/internal/session"
	"hotspotpluslight/internal/web/ui"
)

// SessionService is the online list and kick (implemented by internal/session).
type SessionService interface {
	Online(ctx context.Context, page, per int) (session.OnlineView, error)
	Kick(ctx context.Context, id int64) error
}

const sessionsHome = "/admin/sessions"

// tableURL is the polled fragment URL; it keeps the page and size.
func tableURL(page, per int) string {
	return sessionsHome + "/table?" + url.Values{"page": {strconv.Itoa(page)}, "per": {strconv.Itoa(per)}}.Encode()
}

// kickURL is the row's Disconnect URL; it carries page and size so the redirect returns to the same page.
func kickURL(id int64, v session.OnlineView) string {
	return sessionsHome + "/" + strconv.FormatInt(id, 10) + "/disconnections?" +
		url.Values{"page": {strconv.Itoa(v.Page)}, "per": {strconv.Itoa(v.Per)}}.Encode()
}

func sessionsHref(per int) func(int) string {
	return func(page int) string {
		return sessionsHome + "?" + url.Values{"page": {strconv.Itoa(page)}, "per": {strconv.Itoa(per)}}.Encode()
	}
}

func (h handlers) online(r *http.Request) (session.OnlineView, error) {
	q := r.URL.Query()
	return h.Sessions.Online(r.Context(), atoi(q.Get("page")), atoi(q.Get("per")))
}

func (h handlers) sessionsPage(w http.ResponseWriter, r *http.Request) {
	v, err := h.online(r)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, SessionsPage(layout(r, "Online", sessionsHome), v))
}

// sessionsTable is the fragment the page polls every 10 s.
func (h handlers) sessionsTable(w http.ResponseWriter, r *http.Request) {
	v, err := h.online(r)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, SessionsTable(v))
}

func (h handlers) sessionKick(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		h.notFound(w, r, "Session")
		return
	}
	if err := h.Sessions.Kick(r.Context(), id); err != nil {
		h.kickFailed(w, r, err)
		return
	}
	// Back to the page the admin was on (the row button carries page and per).
	to := sessionsHome
	if q := r.URL.Query(); q.Get("page") != "" || q.Get("per") != "" {
		to = sessionsHref(atoi(q.Get("per")))(atoi(q.Get("page")))
	}
	htmxRedirect(w, r, http.StatusOK, to)
}

// kickFailed turns a Disconnect failure into the response the admin sees (BR-18):
// the session stays as it was.
func (h handlers) kickFailed(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		h.notFound(w, r, "Session")
	case errors.Is(err, domain.ErrSessionEnded):
		h.render(w, r, http.StatusConflict, ui.ErrorFragment("This session has already ended."))
	case errors.Is(err, domain.ErrKickIdentifiers):
		h.render(w, r, http.StatusUnprocessableEntity, ui.ErrorFragment(sentence(err.Error())+"."))
	case errors.Is(err, domain.ErrKickRefused), errors.Is(err, domain.ErrKickTimeout):
		h.render(w, r, http.StatusBadGateway, ui.ErrorFragment(sentence(err.Error())+"; the session was not changed."))
	default:
		h.serverError(w, r, err)
	}
}

// sentence upper-cases the first letter of an error message.
func sentence(s string) string {
	c, n := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(c)) + s[n:]
}

// memberKick disconnects all open sessions of one member (BR-18, E5.07).
func (h handlers) memberKick(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		h.notFound(w, r, "Member")
		return
	}
	res, err := h.Members.DisconnectMember(r.Context(), id)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		h.notFound(w, r, "Member")
	case err != nil:
		h.serverError(w, r, err)
	case res.Failed > 0:
		// Some sessions did end: refresh the member's status line next to the error (out-of-band).
		h.render(w, r, http.StatusBadGateway, h.withStatus(r, id, ui.ErrorFragment(fmt.Sprintf("%d of %d open sessions could not be disconnected.", res.Failed, res.Attempted))))
	case res.Attempted == 0:
		h.render(w, r, http.StatusOK, h.withStatus(r, id, ui.Flash("ok", "This member has no open sessions.")))
	default:
		htmxRedirect(w, r, http.StatusOK, memberURL(id))
	}
}

// withStatus appends the member's refreshed status line (hx-swap-oob) to a flash,
// so the page is not stale after a kick. If the member cannot be loaded the flash goes alone.
func (h handlers) withStatus(r *http.Request, id int64, flash templ.Component) templ.Component {
	v, err := h.Members.Get(r.Context(), id)
	if err != nil {
		return flash
	}
	return templ.Join(flash, MemberStatus(newVM(v), true))
}
