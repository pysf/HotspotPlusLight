# Admin UI notes

Decision (owner, 2026-11-01, D24): the admin UI is styled with **Bootstrap 5.3.8, CSS only**.
Pico was rejected (archived), Tailwind rejected (build step). No Bootstrap JavaScript is used:
confirmations use htmx `hx-confirm` (native dialog), there are no dropdowns or collapses, and htmx stays as is.

## How the CSS is vendored
- `internal/web/static/bootstrap.min.css` is the unmodified official file from
  `https://cdn.jsdelivr.net/npm/bootstrap@5.3.8/dist/css/bootstrap.min.css`. Its first lines carry the
  "Bootstrap v5.3.8" banner. It is embedded with `go:embed` and served at `/admin/static/bootstrap.min.css`.
  Nothing is loaded from a CDN at runtime; there is no npm/Node step.
- `internal/web/static/app.css` holds only the few overrides Bootstrap does not cover (login width,
  online dot, filter-bar spacing, htmx request indicator).
- Markup lives only in the shared components in `internal/web/ui/ui.templ` (layout/navbar, flash = `alert`,
  field = `form-label`/`form-control`/`invalid-feedback`, table = `table-responsive`, pagination = `pagination`,
  confirm buttons); pages reuse them.

## Upgrading Bootstrap
1. `curl -L -o internal/web/static/bootstrap.min.css https://cdn.jsdelivr.net/npm/bootstrap@X.Y.Z/dist/css/bootstrap.min.css`
2. Check the file starts with the `Bootstrap vX.Y.Z` banner.
3. Update the version in `internal/web/static/README.md`, this file and `PLAN.md`.
4. Run `make ci` and look at the login, members, plans and settings pages at a narrow width.
