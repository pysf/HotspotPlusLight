# Admin UI: concept and how to extend it

Decision (owner, 2026-11-03, D24): plain server-rendered HTML (templ) + htmx + **our own hand-written CSS and small JS**.
No Tailwind, no Node, no build step, no UI kit. The look is in `docs/DESIGN.md`. The code rules are in `docs/FRONTEND.md`.
`reference/index.html` is a picture of the look only; none of its code is used.

## Where things are
- `internal/web/static/css/`: `app.css` (layer order and the `@import` list), `tokens.css`, `base.css`, `layout.css`,
  `components/<name>.css`, `utilities.css`.
- `internal/web/static/js/`: one small ES module per job (`theme`, `dialog`, `clipboard`, `toast`, `shortcuts`, `nav`, `forms`, `menu`, `print`).
- `internal/web/static/static.go`: at startup it inlines the `@import`s of `css/app.css` into one stylesheet, hashes every asset,
  and serves them under `/admin/static/` as `name?v=hash` with `Cache-Control: public, max-age=31536000, immutable`.
  A URL without the right hash gets `no-cache`. Pages stay `no-store`.
- `internal/web/ui/ui.templ`: the shared components. `internal/web/ui/env.go`: the theme and the asset URLs, passed through the request context.
- `internal/web/admin/dev.templ`: the component gallery. It is served at `/admin/dev/components` only when `HSP_DEV=1`.
- The voucher print page keeps its own `voucher.css` (no app CSS). The router starter pack (`internal/routerpack`) is not touched.

## Patterns of step B (2026-11-03)
- **Fragments by `HX-Target` only.** The list regions are `#member-list` and `#plan-list`; the one sheet is `<dialog id="sheet">` with the body `#sheet-body`.
  A GET with that `HX-Target` returns only the fragment. Anything else returns the full page. Changes (`POST`/`PUT`/`DELETE`) from the list or sheet
  answer with `HX-Trigger` (`members-changed` / `plans-changed`, `toast`, `close-sheet`) and `HX-Reswap: none`. Without the target nothing changed: redirects as before.
- **The edit sheet is `GET /admin/members/{id}`** with `HX-Target: sheet-body` (no extra route). The member page stays for the usage summary, extra data and password.
- **Lists refresh themselves.** Each list fragment holds `ui.Refresher`: a hidden element with `hx-get` (the list's current address) and
  `hx-trigger="members-changed from:body"`. No JavaScript.
- **Search and chips.** The toolbar form has `hx-get`, `hx-push-url`, `hx-trigger="input delay:300ms, submit"`, `hx-sync="this:replace"`. The status is a hidden
  field inside `#member-chips`, which comes back out of band (`hx-swap-oob`) with the list, so chips and counts stay current.
- **Sheet title and opening** are done by `dialog.js` after the swap (`data-sheet-title` in the fragment). The host body is emptied on close.
- **Creating a member from the sheet** answers the credentials card (`201`, sheet stays open, events `members-changed` + `toast`, no `close-sheet`) until "Done".
- **Errors from lists and the sheet are toasts** (status code kept, `HX-Reswap: none`). `#flash` is still used by the full pages and the other sections.
- `ui.Meter` has no text inside; the figures are printed next to it. `ui.Chip` uses `aria-current="true"` (it is a link). A submit button gets `hx-disabled-elt="this"`.

## Themes
Two themes, chosen by `data-theme` on `<html>`: `air` (default) and `air-dark` (the Dark colors with Air's shape tokens).
The server reads the `theme` cookie (a bad value means `air`) and renders the attribute, so there is no flash.
`theme.js` toggles it and writes the cookie. Components never name a theme; only `tokens.css` does.

## Shell
Same menu as before the redesign: Manage (Members, Batches, Online, Usage) and Setup (Plans, Router, Settings), each with an icon
and an active state (`aria-current="page"`). The top bar has the breadcrumb, the Theme button and Log out.
Up to 820px the sidebar is a drawer behind the menu button (`data-nav-toggle`).

## Deviations from DESIGN.md and FRONTEND.md
- **templ, not `html/template`** (O1). One templ func per component in `internal/web/ui`.
- **Existing routes, field names and "Members" naming stay.**
- **Fragment or full page** is decided by the `HX-Target` header (step B), not `HX-Request`, because the e2e harness sends
  `HX-Request` on every request.
- **Confirm modal:** the server still renders `hx-confirm` text. `dialog.js` catches `htmx:confirm` and shows one shared `<dialog>`.
  No new routes, no `window.confirm`.
- **Two themes only:** Air and Air dark. No Light or plain Dark.
- **The current menu is kept** (the design has two items; this app has seven).
- **Layout classes `l-`** (shell, stack, cluster) next to the `c-` components. Responsive table-to-card rules are in `layout.css`
  with the other breakpoints (980, 820, 560), because breakpoints may live only there.
- **Row menu** needs `menu.js` to place the popover next to its button (CSS anchor positioning is not wide enough yet).
- **Pager** keeps numbered links (tests and filters use them) and adds Previous and Next.
- Component sizes are rem tokens; px only in theme shape tokens (radii, heights) as in DESIGN.md.

## How to add a component
1. Add a templ func in `internal/web/ui/ui.templ` (props struct if it has more than two inputs). Variants are `data-*` attributes.
2. Add `static/css/components/<name>.css` with a `@layer components { .c-<name> ... }` block. Tokens only: no hex, no magic
   numbers, logical properties, no `!important`, no `[data-theme]`. Add its `@import` line to `css/app.css`.
3. Add it, in every variant and state, to `internal/web/admin/dev.templ`.
4. Run `make generate`, then look at it with `HSP_DEV=1 make run` in both themes.

## How to add a page
Write a templ page in `internal/web/admin/<name>.templ` that wraps `@ui.Layout(l)` and uses only `ui` components. Add the handler and
route in `admin.go`. Give every `<td>` a `data-label` so rows become cards on phones. No inline `style`, `onclick`, `hx-on` or `<script>`
(E10.01 checks this).

## Font
`internal/web/static/inter-latin-wght-normal.woff2` is the Latin subset, variable weight, of Inter 4 (SIL OFL 1.1, `OFL-Inter.txt`).
`font-display: swap`; system fonts are the fallback.

## Not verified
Only checked with headless Chrome screenshots (gallery, members, plan form, login) at 1280px and 500px. Nobody has used it in a
normal browser yet. Check: keyboard (Esc closes the modal, focus returns), the nav drawer, Safari and Firefox, and the print page.
