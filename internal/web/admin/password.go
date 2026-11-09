package admin

import (
	"errors"
	"math"
	"net/http"
	"net/url"
	"strconv"

	"github.com/a-h/templ"

	"hotspotpluslight/internal/auth"
	"hotspotpluslight/internal/domain"
)

// passwordHome is the path of the admin password page.
const passwordHome = "/admin/password"

// requirePasswordChanged sends the admin to the password page while the first-start
// password (admin/admin) is still in use. Only the password page and logout stay open.
// A page load is redirected (303); any htmx request gets HX-Redirect and 403.
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
		if isPageRequest(r) {
			http.Redirect(w, r, passwordHome, http.StatusSeeOther)
			return
		}
		w.Header().Set("HX-Redirect", passwordHome)
		http.Error(w, "set a new password first", http.StatusForbidden)
	})
}

// passwordPage shows the password and username forms.
func (h handlers) passwordPage(w http.ResponseWriter, r *http.Request) {
	props, err := h.passwordProps(r)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, PasswordPage(layout(r, "Password and username", passwordHome), props))
}

// passwordProps returns the page state: the forced flag and the current admin name.
func (h handlers) passwordProps(r *http.Request) (PasswordProps, error) {
	must, err := h.Auth.MustChangePassword(r.Context())
	if err != nil {
		return PasswordProps{}, err
	}
	name, err := h.Auth.AdminUsername(r.Context())
	return PasswordProps{Forced: must, Username: name}, err
}

// sessionToken returns the admin session cookie value, or "".
func sessionToken(r *http.Request) string {
	if c, err := r.Cookie(cookieName); err == nil {
		return c.Value
	}
	return ""
}

// passwordDone sends the admin back to dest with a toast. The page changes, so the
// toast text goes in a short cookie that toast.js shows and clears.
func passwordDone(w http.ResponseWriter, r *http.Request, toast, dest string) {
	http.SetCookie(w, &http.Cookie{Name: "hsp_toast", Value: url.PathEscape(toast), Path: "/admin", MaxAge: 30, SameSite: http.SameSiteStrictMode})
	htmxRedirect(w, r, http.StatusOK, dest)
}

// passwordChange changes the admin password (BR-20), or shows the errors. On the
// forced screen a username field renames the admin in the same step.
func (h handlers) passwordChange(w http.ResponseWriter, r *http.Request) {
	props, err := h.passwordProps(r)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	username := ""
	if props.Forced {
		username = r.PostFormValue("username")
		props.Username = username
	}
	errs, err := h.Auth.ChangePassword(r.Context(), sessionToken(r), clientKey(r), username,
		r.PostFormValue("current_password"), r.PostFormValue("new_password"), r.PostFormValue("confirm_password"))
	h.passwordResult(w, r, props, errs, err, "Password changed", dashboardHome, func(p PasswordProps) templ.Component { return PasswordFormView(p) })
}

// usernameChange changes the admin username, or shows the errors.
func (h handlers) usernameChange(w http.ResponseWriter, r *http.Request) {
	props, err := h.passwordProps(r)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	props.Username = r.PostFormValue("username")
	errs, err := h.Auth.ChangeUsername(r.Context(), sessionToken(r), clientKey(r), props.Username, r.PostFormValue("username_current_password"))
	h.passwordResult(w, r, props, errs, err, "Username changed", passwordHome, func(p PasswordProps) templ.Component { return UsernameFormView(p) })
}

// passwordResult answers a password or username change: the throttle, an error page,
// the field errors (422), or a redirect to dest with a toast.
func (h handlers) passwordResult(w http.ResponseWriter, r *http.Request, props PasswordProps, errs domain.FieldErrors, err error, toast, dest string, view func(PasswordProps) templ.Component) {
	var throttled *auth.ThrottledError
	switch {
	case errors.As(err, &throttled):
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(throttled.RetryAfter.Seconds()))))
		props.Throttled = true
		h.render(w, r, http.StatusTooManyRequests, view(props))
	case err != nil:
		h.serverError(w, r, err)
	case len(errs) > 0:
		props.Errors = errs
		h.render(w, r, http.StatusUnprocessableEntity, view(props))
	default:
		passwordDone(w, r, toast, dest)
	}
}
