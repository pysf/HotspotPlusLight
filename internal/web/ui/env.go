package ui

import "context"

// Themes are the allowed values of the "theme" cookie and of data-theme on <html>. The first is the default.
var Themes = []string{"air", "air-dark"}

// ThemeCookie is the cookie name that stores the theme.
const ThemeCookie = "theme"

// ParseTheme returns v when it is a known theme, else the default.
func ParseTheme(v string) string {
	for _, t := range Themes {
		if v == t {
			return t
		}
	}
	return Themes[0]
}

// Env is what Layout needs from the request: the theme to render and the URL of each asset (with its hash).
type Env struct {
	Theme string
	Asset func(name string) string
	// BusinessName is the setting shown in the header and the tab title. Empty means it is hidden.
	BusinessName string
}

// TabTitle returns the browser tab title: the page title, then the business name when set, then the product name.
func (e Env) TabTitle(page string) string {
	product := "HotspotPlus Lite"
	if e.BusinessName != "" {
		product = e.BusinessName + " \u00b7 " + product
	}
	return page + " - " + product
}

type envKey struct{}

// WithEnv stores the page environment in ctx. The admin middleware calls it for every request.
func WithEnv(ctx context.Context, e Env) context.Context { return context.WithValue(ctx, envKey{}, e) }

// EnvFrom reads the page environment. Without one it renders the default theme and plain asset URLs.
func EnvFrom(ctx context.Context) Env {
	e, _ := ctx.Value(envKey{}).(Env)
	e.Theme = ParseTheme(e.Theme)
	if e.Asset == nil {
		e.Asset = func(name string) string { return "/admin/static/" + name }
	}
	return e
}
