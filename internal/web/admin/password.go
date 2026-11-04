package admin

import (
	"errors"
	"math"
	"net/http"
	"net/url"
	"strconv"

	"hotspotpluslight/internal/auth"
)

const passwordHome = "/admin/password"

// requirePasswordChanged sends the admin to the password page while the first-start
// password (admin/admin) is still in use. Only the password page and logout stay open.
// A page request is redirected (303); a change request (htmx) gets HX-Redirect and 403.
func (h handlers) requirePasswordChanged(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == passwordHome || r.URL.Path == "/admin/logout" {
			next.ServeHTTP(w, r)
			return
		}
		must, err := h.Auth.MustChangePassword(r.Context())
		if err != nil {
			h.serverError(w, r, err)
			return
		}
		if !must {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			http.Redirect(w, r, passwordHome, http.StatusSeeOther)
			return
		}
		w.Header().Set("HX-Redirect", passwordHome)
		http.Error(w, "set a new password first", http.StatusForbidden)
	})
}

func (h handlers) passwordPage(w http.ResponseWriter, r *http.Request) {
	must, err := h.Auth.MustChangePassword(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, PasswordPage(layout(r, "Password", ""), PasswordProps{Forced: must}))
}

func (h handlers) passwordChange(w http.ResponseWriter, r *http.Request) {
	var token string
	if c, err := r.Cookie(cookieName); err == nil {
		token = c.Value
	}
	errs, err := h.Auth.ChangePassword(r.Context(), token, clientKey(r),
		r.PostFormValue("current_password"), r.PostFormValue("new_password"), r.PostFormValue("confirm_password"))
	var throttled *auth.ThrottledError
	switch {
	case errors.As(err, &throttled):
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(throttled.RetryAfter.Seconds()))))
		must, _ := h.Auth.MustChangePassword(r.Context())
		h.render(w, r, http.StatusTooManyRequests, PasswordFormView(PasswordProps{Throttled: true, Forced: must}))
	case err != nil:
		h.serverError(w, r, err)
	case len(errs) > 0:
		must, _ := h.Auth.MustChangePassword(r.Context())
		h.render(w, r, http.StatusUnprocessableEntity, PasswordFormView(PasswordProps{Errors: errs, Forced: must}))
	default:
		// The page changes, so the toast text travels in a short cookie that toast.js shows and clears.
		http.SetCookie(w, &http.Cookie{Name: "hsp_toast", Value: url.PathEscape("Password changed"), Path: "/admin", MaxAge: 30, SameSite: http.SameSiteStrictMode})
		htmxRedirect(w, r, http.StatusOK, "/admin/members")
	}
}
