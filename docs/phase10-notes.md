# Phase 10, step A: own CSS, new look

Done (2026-11-03). Tailwind removed. New hand-written CSS and JS, served with content hashes. All admin pages restyled.

## What changed
- Removed: `internal/web/ui/input.css`, generated `app.css`, the Tailwind block and `css`, `css-check`, `tailwind-verify` targets
  in the Makefile (also `css-check` from `ci`), `/tools/` in `.gitignore`, every utility class in the templ files, old `app.js`.
- Added: `static/css/*`, `static/js/*`, `static.go` (bundle, hash, cache), `ui/env.go`, `admin/dev.templ`, `HSP_DEV` in config,
  `e2e/phase10_test.go`.
- Components: `c-btn`, `c-icon-btn`, `c-field`, `c-input`, `c-select`, `c-search`, `c-chip`, `c-card`, `c-table`, `c-meter`, `c-pill`
  (and `c-dot`), `c-pager`, `c-empty`, `c-notice`, `c-menu`, `c-sheet`, `c-modal`, `c-toast`, `c-credentials`, `c-code`, `c-chart`.
  Menu, sheet, credentials, meter, chip and search are only used in the gallery so far (step B uses them).
- JS: removed every inline `onclick`, `hx-on` and `style=`. Settings Reveal, select-all, bulk Cancel and the empty-selection guard
  are in `forms.js` (data attributes). Voucher Print is `print.js`. Theme swatches use an SVG `fill`.
- htmx config has `includeIndicatorStyles:false`. htmx is still the first `script[src]`.

## Tests
New: E10.01 to E10.05. Adapted selectors (same checks):
- E1.20: `.nav-heading` -> `.l-nav__heading`, `a.nav-link` -> `a.l-nav__link`.
- E2.01: `.dot` -> `.c-dot` (role and aria-label checks unchanged).
- E6.14: `[data-theme]` -> `body [data-theme]` (the `<html>` tag now has `data-theme` too).
- E7.06: `button[onclick*=print]` -> `button[data-print]`, no `[onclick]`, and the `print.js?v=` script loads.
- E8.12: `app.js` -> `js/clipboard.js?v=<hash>` (there is no `app.js` any more; the copy code moved to `clipboard.js`).

## Deviations
See `docs/ui-notes.md`. Extra: the Theme button is in the top bar (not the sidebar footer); `menu.js` and `forms.js`, `nav.js`
exist beside the five JS modules named in FRONTEND.md; the shared confirm dialog has no `<form>`.

## Not verified
No real browser session. Headless Chrome screenshots only.
