// Package admin is the admin listener's handler. Phase 0 placeholder: 404 for everything.
package admin

import "net/http"

func Handler() http.Handler { return http.NotFoundHandler() }
