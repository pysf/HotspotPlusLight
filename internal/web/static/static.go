// Package static embeds the admin panel's assets: htmx and Bootstrap CSS (vendored, pinned) and app.css.
package static

import (
	"embed"
	"io/fs"
)

//go:embed htmx.min.js bootstrap.min.css app.css
var files embed.FS

// Files returns the asset tree served under /admin/static/.
func Files() fs.FS { return files }
