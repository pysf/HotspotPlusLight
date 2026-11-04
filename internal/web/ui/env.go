// Package ui holds the small reusable templ components shared by every admin
// page: layout, icon, buttons, fields, table, pager, pill, meter, chip, menu,
// sheet, modal, toast, credentials, code block and bar chart. Each has a c- CSS
// class in internal/web/static/css/components (see docs/ui-notes.md).
package ui

import "context"

// Themes lists the allowed values of the "theme" cookie and of data-theme on <html>. The first is the default.
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

// Env is what Layout needs from the request: the theme and the URL of each asset.
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

// envKey is the context key of the page environment.
type envKey struct{}

// WithEnv returns a copy of ctx that carries e. The admin middleware calls it for every page request.
func WithEnv(ctx context.Context, e Env) context.Context { return context.WithValue(ctx, envKey{}, e) }

// EnvFrom returns the page environment in ctx. Without one it returns the default theme and plain asset URLs.
func EnvFrom(ctx context.Context) Env {
	e, _ := ctx.Value(envKey{}).(Env)
	e.Theme = ParseTheme(e.Theme)
	if e.Asset == nil {
		e.Asset = func(name string) string { return "/admin/static/" + name }
	}
	return e
}
