// Package static embeds the admin panel's assets: htmx, the Inter font (vendored, pinned) and the built app.css.
package static

import (
	"embed"
	"io/fs"
)

//go:embed htmx.min.js app.css app.js voucher.css inter-latin-wght-normal.woff2
var files embed.FS

// Files returns the asset tree served under /admin/static/.
func Files() fs.FS { return files }
