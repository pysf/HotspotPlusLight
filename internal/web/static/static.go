// Package static embeds the admin panel's assets: htmx and Tabler CSS (vendored, pinned) and app.css.
package static

import (
	"embed"
	"io/fs"
)

//go:embed htmx.min.js tabler.min.css app.css voucher.css
var files embed.FS

// Files returns the asset tree served under /admin/static/.
func Files() fs.FS { return files }
