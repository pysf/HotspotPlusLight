// Package admin is the admin listener's handler: login/logout, session and CSRF
// middleware, and the resource-style pages (PLAN 8.2). Handlers stay thin:
// parse, call a service, render.
package admin

import (
	"context"
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

type Deps struct {
	Auth     AuthService
	Plans    PlanService
	Members  MemberService
	Batches  BatchService
	Sessions SessionService
	Settings SettingsService
	Router   RouterPack
	Log      *slog.Logger
}

type handlers struct{ Deps }

type ctxKey struct{}

// Handler builds the admin listener's router. Everything outside /admin is 404.
func Handler(d Deps) http.Handler {
	h := handlers{d}
	r := chi.NewRouter()
	r.Route("/admin", func(r chi.Router) {
		r.Use(noStore)
		r.Handle("/static/*", http.StripPrefix("/admin/static/", http.FileServerFS(static.Files())))
		r.Get("/login", h.loginPage)
		r.Post("/login", h.loginSubmit)
		r.Group(func(r chi.Router) {
			// MONITOR: sessions are opaque server-side rows (sliding 1 h); a stolen
			// cookie is valid until it idles out or the admin logs out / changes password.
			r.Use(h.requireSession, h.requireCSRF)
			r.Get("/", redirect("/admin/members"))
			r.Post("/logout", h.logout)
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
		})
	})
	return r
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

var nav = []ui.NavItem{{Label: "Members", Href: "/admin/members"}, {Label: "Batches", Href: batchesHome}, {Label: "Online", Href: sessionsHome}, {Label: "Plans", Href: "/admin/plans"}, {Label: "Router", Href: "/admin/router"}, {Label: "Settings", Href: "/admin/settings"}}

func layout(r *http.Request, title, active string) ui.LayoutData {
	csrf, _ := r.Context().Value(ctxKey{}).(string)
	return ui.LayoutData{Title: title, CSRF: csrf, Nav: nav, Active: active, LogoutURL: "/admin/logout"}
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
	h.render(w, r, http.StatusInternalServerError, ui.ErrorFragment("Something went wrong. Check the server log."))
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
		http.Redirect(w, r, "/admin/members", http.StatusSeeOther)
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
