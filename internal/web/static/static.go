// Package static embeds the admin panel's assets: htmx (vendored, pinned) and CSS.
package static

import (
	"embed"
	"io/fs"
)

//go:embed htmx.min.js app.css
var files embed.FS

// Files returns the asset tree served under /admin/static/.
func Files() fs.FS { return files }
