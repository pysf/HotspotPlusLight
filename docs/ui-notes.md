# Admin UI notes

Decision (owner, 2026-11-02, D24): the admin UI is styled with **Tabler 1.6.1, CSS only, boxed layout**.
This supersedes the Bootstrap-only restyle of 2026-11-01 (Pico rejected: archived; Tailwind rejected: build step).
Tabler is built on Bootstrap 5.3 and bundles its CSS, so `tabler.min.css` replaced `bootstrap.min.css` (removed).
No Tabler/Bootstrap JavaScript is used: confirmations use htmx `hx-confirm` (native dialog), there are no
dropdowns or collapses, and htmx stays as is.

## How the CSS is vendored
- `internal/web/static/tabler.min.css` is the unmodified official file from
  `https://cdn.jsdelivr.net/npm/@tabler/core@1.6.1/dist/css/tabler.min.css`. Its first lines carry the
  "Tabler v1.6.1" banner. Version choice: the newest stable release at least two weeks old on 2026-11-02
  (1.7.0 was published 2026-10-20, 13 days before; 1.6.1 was published 2026-09-28). It is embedded with `go:embed` and
  served at `/admin/static/tabler.min.css`. Nothing is loaded from a CDN at runtime; there is no npm/Node step.
- Classes the app uses were verified present in the file: table, btn, form-control, form-select, alert, badge,
  pagination, card, navbar, form-check, input-group, page, page-wrapper, page-body, page-center, container-xl, container-tight.
- `internal/web/static/app.css` holds only the few overrides Tabler does not cover (login width, online dot,
  filter-bar spacing, htmx request indicator, theme swatches). It uses Tabler's `--tblr-*` variables.
- Markup lives only in the shared components in `internal/web/ui/ui.templ` (layout/navbar, flash = `alert`,
  field = `form-label`/`form-control`/`invalid-feedback`, table = `table-responsive`, pagination = `pagination`,
  confirm buttons); pages reuse them.

## Fonts, icons and external URLs
- Tabler 1.6.1 CSS has no `@font-face` and no `@import`. Its font stack is the system stack
  (`system-ui, -apple-system, ... sans-serif`), so no web font (Inter) is downloaded. No icon font is used (no icon CSS vendored).
- The only `url(...)` values in the file are inline `data:image/svg+xml` images (form-check ticks, select arrow, etc.).
  The remaining `http(s)://` strings are the SVG namespace (`www.w3.org/2000/svg`) and the licence banner comment; none is fetched.
- When upgrading, repeat: `grep -o 'url([^)]*)' tabler.min.css | grep -v data:` and `grep -c '@font-face\|@import'`
  must find nothing external; if they do, neutralize it in `app.css` (or open a question) before shipping.

## Boxed layout
Tabler's CSS enables it with the `layout-boxed` class on `<body>` (or `data-bs-layout="boxed"` on `<html>`):
`.layout-boxed .page { max-width: var(--tblr-theme-boxed-width) /* 1320px */; margin: 0 auto }` with padding and a
border from 768px up. `ui.Layout` sets `class="layout-boxed"` on `<body>` for logged-in pages, with the structure
`.page > header.navbar (horizontal top nav) + .page-wrapper > main.page-body > .container-xl`. The login page
(no CSRF = not logged in) uses Tabler's centered card instead: `.page.page-center > .container-tight > .card.card-md`.

## Member list
The list shows only: checkbox, username (link to the member page), online dot, status badge, data
("used / allowed" with a muted "N left" line; "used / unlimited" without it). Everything else (name, mobile, plan, MAC,
expiry, renewal, time, window end, verdict) is on the member page (form + usage summary).

## Upgrading Tabler
1. `curl -L -o internal/web/static/tabler.min.css https://cdn.jsdelivr.net/npm/@tabler/core@X.Y.Z/dist/css/tabler.min.css`
   (pick a release at least two weeks old).
2. Check the file starts with the `Tabler vX.Y.Z` banner and repeat the external-URL check above.
3. Update the version in `internal/web/static/README.md`, this file and `PLAN.md`.
4. Run `make ci` and look at the login, members, plans and settings pages at a narrow width.
