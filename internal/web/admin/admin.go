// Package admin is the admin listener's handler: login/logout, session and CSRF
// middleware, and the resource-style pages (PLAN 8.2). Handlers stay thin:
// parse, call a service, render.
package admin

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net"
	"net/http"
	"strconv"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"hotspotpluslight/internal/auth"
	"hotspotpluslight/internal/domain"
	"hotspotpluslight/internal/linkstatus"
	"hotspotpluslight/internal/member"
	"hotspotpluslight/internal/plan"
	"hotspotpluslight/internal/web/static"
	"hotspotpluslight/internal/web/ui"
)

const cookieName = "hsp_admin"

// Consumer-side interfaces: implemented by internal/auth, plan and settings.
type AuthService interface {
	Login(ctx context.Context, username, password, clientKey string) (auth.AdminSession, error)
	Authenticate(ctx context.Context, token string) (csrf string, found bool, err error)
	Logout(ctx context.Context, token string) error
	MustChangePassword(ctx context.Context) (bool, error)
	ChangePassword(ctx context.Context, token, clientKey, current, next, confirm string) (domain.FieldErrors, error)
}

type PlanService interface {
	List(ctx context.Context) ([]domain.Plan, error)
	Get(ctx context.Context, id int64) (plan.Detail, error)
	Create(ctx context.Context, f domain.PlanForm) (int64, domain.FieldErrors, error)
	Update(ctx context.Context, id int64, f domain.PlanForm) (domain.FieldErrors, error)
	Delete(ctx context.Context, id int64) error
}

type MemberService interface {
	NewForm(ctx context.Context) (domain.MemberForm, error)
	Create(ctx context.Context, f domain.MemberForm) (int64, domain.FieldErrors, error)
	Get(ctx context.Context, id int64) (member.View, error)
	Update(ctx context.Context, id int64, f domain.MemberForm) (domain.FieldErrors, member.KickResult, error)
	SetActive(ctx context.Context, id int64, active bool) (member.KickResult, error)
	Renew(ctx context.Context, id int64) error
	SetExtraBulk(ctx context.Context, id int64, value, unit string) (domain.FieldErrors, error)
	RevealPassword(ctx context.Context, id int64) (string, error)
	SetPassword(ctx context.Context, id int64, password string) (string, error)
	List(ctx context.Context, q member.ListQuery) (member.ListResult, error)
	Stats(ctx context.Context, sel domain.MemberSelection) (member.BulkStats, error)
	Delete(ctx context.Context, sel domain.MemberSelection) (member.DeleteResult, error)
	DeleteOne(ctx context.Context, id int64) (member.DeleteResult, error)
	DisconnectMember(ctx context.Context, id int64) (member.KickResult, error)
}

type SettingsService interface {
	Get(ctx context.Context) (domain.Settings, error)
	Update(ctx context.Context, f domain.SettingsForm) (domain.FieldErrors, error)
}

// LinkStatus is the in-memory router link status (implemented by linkstatus.Recorder).
type LinkStatus interface {
	Snapshot() linkstatus.Snapshot
}

type Deps struct {
	Auth     AuthService
	Plans    PlanService
	Members  MemberService
	Batches  BatchService
	Sessions SessionService
	Usage    UsageService
	Settings SettingsService
	Router   RouterPack
	Link     LinkStatus
	// RadiusPorts gives the RADIUS UDP ports for the router setup script, read when the page is rendered.
	RadiusPorts func() (authPort, acctPort int)
	// Assets serves the CSS, JS, font and htmx files under /admin/static/ (built once at startup).
	Assets *static.Assets
	// Dev turns on /admin/dev/components (HSP_DEV=1). Off = 404.
	Dev bool
	Log *slog.Logger
}

type handlers struct{ Deps }

type ctxKey struct{}

// Handler builds the admin listener's router. Everything outside /admin is 404.
func Handler(d Deps) http.Handler {
	if d.Assets == nil {
		panic("admin.Handler: Deps.Assets is required")
	}
	h := handlers{d}
	r := chi.NewRouter()
	r.Route("/admin", func(r chi.Router) {
		// Assets cache by their hash; pages are never cached.
		r.Handle("/static/*", http.StripPrefix(static.Prefix, h.Assets.Handler()))
		r.Group(func(r chi.Router) {
			r.Use(noStore, h.pageEnv)
			r.Get("/login", h.loginPage)
			r.Post("/login", h.loginSubmit)
			r.Group(func(r chi.Router) {
				// MONITOR: sessions are opaque server-side rows (sliding 1 h); a stolen
				// cookie is valid until it idles out or the admin logs out / changes password.
				r.Use(h.requireSession, h.requireCSRF, h.requirePasswordChanged)
				r.Get("/", redirect("/admin/members"))
				r.Post("/logout", h.logout)
				r.Get("/password", h.passwordPage)
				r.Put("/password", h.passwordChange)
				r.Route("/members", func(r chi.Router) {
					r.Get("/", h.members)
					r.Post("/", h.memberCreate)
					r.Get("/new", h.memberNew)
					r.Post("/bulk-delete", h.memberBulkDelete)
					// Digits only: "/members/delete" and friends are plain 404s, not 405s.
					r.Route("/{id:[0-9]+}", func(r chi.Router) {
						r.Get("/", h.memberPage)
						r.Put("/", h.memberUpdate)
						r.Delete("/", h.memberDelete)
						r.Post("/renewals", h.memberRenew)
						r.Post("/toggle", h.memberToggle)
						r.Put("/extra-bulk", h.memberExtraBulk)
						r.Get("/password", h.passwordReveal)
						r.Put("/password", h.passwordSet)
						r.Post("/disconnections", h.memberKick)
						r.Get("/voucher", h.memberPrint)
					})
				})
				r.Route("/batches", func(r chi.Router) {
					r.Get("/", h.batches)
					r.Post("/", h.batchCreate)
					r.Get("/new", h.batchNew)
					r.Route("/{id:[0-9]+}", func(r chi.Router) {
						r.Put("/", h.batchRename)
						r.Delete("/", h.batchDelete)
						r.Get("/vouchers", h.batchPrint)
					})
				})
				r.Route("/sessions", func(r chi.Router) {
					r.Get("/", h.sessionsPage)
					r.Get("/table", h.sessionsTable)
					r.Post("/{id}/disconnections", h.sessionKick)
				})
				r.Get("/usage", h.usagePage)
				r.Route("/plans", func(r chi.Router) {
					r.Get("/", h.plansList)
					r.Post("/", h.planCreate)
					r.Get("/new", h.planNew)
					r.Get("/{id}", h.planEdit)
					r.Put("/{id}", h.planUpdate)
					r.Delete("/{id}", h.planDelete)
				})
				r.Route("/router", func(r chi.Router) {
					r.Get("/", h.routerPage)
					r.Get("/files.zip", h.packDownload)
					r.Get("/themes", h.themeGallery)
					r.Route("/themes/{id}", func(r chi.Router) {
						r.Get("/", h.themePage)
						r.Get("/login.html", h.themePreview)
						r.Get("/background.jpg", h.themeBackground)
						r.Get("/logo.png", h.themeLogo)
					})
				})
				r.Get("/settings", h.settingsPage)
				r.Put("/settings", h.settingsUpdate)
				r.Get("/dev/components", h.devComponents)
			})
		})
	})
	return r
}

// pageEnv puts the theme (from the cookie) and the asset URLs into the request context for ui.Layout.
func (h handlers) pageEnv(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		theme := ""
		if c, err := r.Cookie(ui.ThemeCookie); err == nil {
			theme = c.Value
		}
		env := ui.Env{Theme: ui.ParseTheme(theme), Asset: h.Assets.URL}
		next.ServeHTTP(w, r.WithContext(ui.WithEnv(r.Context(), env)))
	})
}

// devComponents is the component gallery. It exists only with HSP_DEV=1.
func (h handlers) devComponents(w http.ResponseWriter, r *http.Request) {
	if !h.Dev {
		http.NotFound(w, r)
		return
	}
	h.render(w, r, http.StatusOK, DevComponentsPage(layout(r, "Components", "")))
}

func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func redirect(to string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, to, http.StatusSeeOther) }
}

var nav = []ui.NavItem{
	{Label: "Members", Href: "/admin/members", Group: "Manage", Icon: "users"},
	{Label: "Batches", Href: batchesHome, Group: "Manage", Icon: "ticket"},
	{Label: "Online", Href: sessionsHome, Group: "Manage", Icon: "wifi"},
	{Label: "Usage", Href: usageHome, Group: "Manage", Icon: "chart"},
	{Label: "Plans", Href: "/admin/plans", Group: "Setup", Icon: "layers"},
	{Label: "Router", Href: "/admin/router", Group: "Setup", Icon: "router"},
	{Label: "Settings", Href: "/admin/settings", Group: "Setup", Icon: "settings"},
}

func layout(r *http.Request, title, active string) ui.LayoutData {
	csrf, _ := r.Context().Value(ctxKey{}).(string)
	return ui.LayoutData{Title: title, CSRF: csrf, Nav: nav, Active: active, LogoutURL: "/admin/logout", PasswordURL: passwordHome}
}

func (h handlers) render(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := c.Render(r.Context(), w); err != nil {
		h.Log.Error("render", "path", r.URL.Path, "err", err)
	}
}

func (h handlers) serverError(w http.ResponseWriter, r *http.Request, err error) {
	h.Log.Error("admin request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	if fromApp(r) {
		h.appFail(w, http.StatusInternalServerError, "", "Something went wrong. Try again.")
		return
	}
	h.render(w, r, http.StatusInternalServerError, ui.ErrorFragment("Something went wrong. Check the server log."))
}

// Element ids that htmx targets. A request whose HX-Target is one of them wants a fragment
// (or, for a change, events and a toast instead of a redirect). The e2e harness sends
// HX-Request on every request, so HX-Request alone never decides this.
const (
	listID      = "member-list" // the members list region
	planListID  = "plan-list"   // the plans list region
	sheetBodyID = "sheet-body"  // the body of the one sheet (new/edit forms)

	membersChanged = "members-changed" // events sent in HX-Trigger; the list regions refresh on them
	plansChanged   = "plans-changed"
)

func hxTarget(r *http.Request) string { return r.Header.Get("HX-Target") }

// inSheet is true for a request that comes from the sheet.
func inSheet(r *http.Request) bool { return hxTarget(r) == sheetBodyID }

// fromApp is true for a request from a list region or the sheet: it gets events and a toast.
func fromApp(r *http.Request) bool {
	t := hxTarget(r)
	return t == listID || t == planListID || t == sheetBodyID
}

// setTrigger sets the HX-Trigger header: one event per key (a string value is the toast text).
func setTrigger(w http.ResponseWriter, events map[string]any) {
	b, err := json.Marshal(events)
	if err == nil {
		w.Header().Set("HX-Trigger", string(b))
	}
}

// appDone answers a change made from a list or the sheet: the list refreshes (changed), a toast
// shows msg, the sheet closes. The body is empty and never swapped (HX-Reswap).
func (h handlers) appDone(w http.ResponseWriter, r *http.Request, status int, changed, msg string) {
	ev := map[string]any{changed: true, "toast": msg}
	if inSheet(r) {
		ev["close-sheet"] = true
	}
	setTrigger(w, ev)
	w.Header().Set("HX-Reswap", "none")
	w.WriteHeader(status)
}

// appFail answers a failed change made from a list or the sheet: only a toast, nothing swapped.
// changed (may be "") also refreshes the list, when part of the work was done.
func (h handlers) appFail(w http.ResponseWriter, status int, changed, msg string) {
	ev := map[string]any{"toast": msg}
	if changed != "" {
		ev[changed] = true
	}
	setTrigger(w, ev)
	w.Header().Set("HX-Reswap", "none")
	w.WriteHeader(status)
}

// appOrFlash is appFail for list and sheet requests, else the usual error box for #flash.
func (h handlers) appOrFlash(w http.ResponseWriter, r *http.Request, status int, msg string) {
	if fromApp(r) {
		h.appFail(w, status, "", msg)
		return
	}
	h.render(w, r, status, ui.ErrorFragment(msg))
}

func isHTMX(r *http.Request) bool { return r.Header.Get("HX-Request") == "true" }

// htmxRedirect answers an htmx request with HX-Redirect and a plain request with 303.
func htmxRedirect(w http.ResponseWriter, r *http.Request, status int, to string) {
	if isHTMX(r) {
		w.Header().Set("HX-Redirect", to)
		w.WriteHeader(status)
		return
	}
	http.Redirect(w, r, to, http.StatusSeeOther)
}

// requireSession loads the session from the cookie (sliding its expiry) or sends the visitor to login.
func (h handlers) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var token string
		if c, err := r.Cookie(cookieName); err == nil {
			token = c.Value
		}
		csrf, ok, err := h.Auth.Authenticate(r.Context(), token)
		if err != nil {
			h.serverError(w, r, err)
			return
		}
		if !ok {
			if r.Method == http.MethodGet || r.Method == http.MethodHead {
				http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
				return
			}
			w.Header().Set("HX-Redirect", "/admin/login")
			http.Error(w, "login required", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, csrf)))
	})
}

// requireCSRF demands the session's token in X-CSRF-Token on every unsafe method.
func (h handlers) requireCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			want, _ := r.Context().Value(ctxKey{}).(string)
			if !auth.CSRFMatches(want, r.Header.Get("X-CSRF-Token")) {
				http.Error(w, "invalid or missing CSRF token", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (h handlers) loginPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, http.StatusOK, LoginPage("", ""))
}

func clientKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (h handlers) loginSubmit(w http.ResponseWriter, r *http.Request) {
	username, password := r.PostFormValue("username"), r.PostFormValue("password")
	sess, err := h.Auth.Login(r.Context(), username, password, clientKey(r))
	var throttled *auth.ThrottledError
	switch {
	case errors.As(err, &throttled):
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(throttled.RetryAfter.Seconds()))))
		h.render(w, r, http.StatusTooManyRequests, LoginPage(username, "Too many failed attempts. Try again in a minute."))
	case errors.Is(err, auth.ErrInvalidCredentials):
		h.render(w, r, http.StatusUnauthorized, LoginPage(username, "Wrong username or password."))
	case err != nil:
		h.serverError(w, r, err)
	default:
		// Session cookie: no Max-Age, so it lives while the browser does; the
		// server-side row decides validity. No Secure flag: plain HTTP on the LAN (D9).
		http.SetCookie(w, &http.Cookie{Name: cookieName, Value: sess.Token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
		to := "/admin/members"
		if must, err := h.Auth.MustChangePassword(r.Context()); err != nil {
			h.serverError(w, r, err)
			return
		} else if must {
			to = passwordHome
		}
		http.Redirect(w, r, to, http.StatusSeeOther)
	}
}

func (h handlers) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		if err := h.Auth.Logout(r.Context(), c.Value); err != nil {
			h.serverError(w, r, err)
			return
		}
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	htmxRedirect(w, r, http.StatusOK, "/admin/login")
}
