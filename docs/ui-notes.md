# Admin UI: concept and how to extend it

Decision (owner, 2026-11-03, D24): plain server-rendered HTML (templ) + htmx + **our own hand-written CSS and small JS**.
No Tailwind, no Node, no build step, no UI kit. The look is in `docs/DESIGN.md`. The code rules are in `docs/FRONTEND.md`.
`reference/index.html` is a picture of the look only; none of its code is used.

## Where things are
- `internal/web/static/css/`: `app.css` (layer order and the `@import` list), `tokens.css`, `base.css`, `layout.css`,
  `components/<name>.css`, `utilities.css`.
- `internal/web/static/js/`: one small ES module per job (`theme`, `dialog`, `clipboard`, `toast`, `shortcuts`, `forms`, `menu`, `password`, `print`).
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
`data-theme` on `<html>` is `light`, `dark` or `auto`. The server reads the `theme` cookie (`light` or `dark`; anything else means `auto`) and renders the attribute,
so there is no flash. With `auto` the OS decides. `theme.js` switches to the other mode and writes the cookie. Components never name a theme; only `tokens.css` does.

## Shell
Side bar with 8 links (Dashboard, Members, Online now, Plans, Vouchers, Usage, Router, Settings), no groups. Top bar: date, Password, theme button, Log out.
Under 1000 px the side bar is an icon rail; under 700 px a bottom tab bar (4 tabs + More menu) replaces it. See `DESIGN.md` section 5.
Layout rule: the children of `.l-page` are spaced by its grid gap. A block that holds several children and needs the same spacing uses `.l-flow` (24 px) or `.l-stack` (16 px); do not use margins.

## Deviations from DESIGN.md and FRONTEND.md
- **templ, not `html/template`** (O1). One templ func per component in `internal/web/ui`.
- **Existing routes, field names and "Members" naming stay.**
- **Fragment or full page** is decided by the `HX-Target` header (step B), not `HX-Request`, because the e2e harness sends
  `HX-Request` on every request.
- **Confirm modal:** the server still renders `hx-confirm` text. `dialog.js` catches `htmx:confirm` and shows one shared `<dialog>`.
  No new routes, no `window.confirm`.
- **Two modes:** light and dark (plus auto), see Themes.
- **The menu is the design's list of 8 places**, mapped to our routes (Vouchers = `/admin/batches`).
- **Layout classes `l-`** (shell, flow, stack, cluster) next to the `c-` components. Breakpoints are 999 and 699 px, in the files of the components they change.
- **Row menu** needs `menu.js` to place the popover next to its button (CSS anchor positioning is not wide enough yet).
- **Pager** keeps numbered links (tests and filters use them) and adds Previous and Next.
- Tokens are in px, as in the design.

## How to add a component
1. Add a templ func in `internal/web/ui/ui.templ` (props struct if it has more than two inputs). Variants are `data-*` attributes.
2. Add `static/css/components/<name>.css` with a `@layer components { .c-<name> ... }` block. Tokens only: no hex, no magic
   numbers, logical properties, no `!important`, no `[data-theme]`; sizes and colors from `tokens.css`. Add its `@import` line to `css/app.css`.
3. Add it, in every variant and state, to `internal/web/admin/dev.templ`.
4. Run `make generate`, then look at it with `HSP_DEV=1 make run` in both themes.

## How to add a page
Write a templ page in `internal/web/admin/<name>.templ` that wraps `@ui.Layout(l)` and uses only `ui` components. Add the handler and
route in `admin.go`. Give every `<td>` a `data-label` so rows become cards on phones. No inline `style`, `onclick`, `hx-on` or `<script>`
(E10.01 checks this).

## Font
System stack only (`--font` in `tokens.css`). No font file is shipped (the Inter file was removed in design v2).

## Design v2 (2026-11-09)
The source is a design canvas, copied to `docs/design/` (`hp.css`, boards, `README.md`). Step 1 ported the foundations, the shell and every shared component.
- **Kept from our code:** the `c-` / `l-` class names with `data-*` variants, the native `<meter>` for usage bars (no `pct-N` classes), `@media` queries instead of container queries,
  our markup and test hooks (ids, `data-*`, aria), the `theme` cookie (values are now `light` and `dark`; none means `auto`).
- **Renamed:** the design's status tokens `--s-ok-fg` and friends are `--st-ok-fg`, `--st-ok-bg`, `--st-ok-dot` (and warn, bad, off), because `--s-N` is our spacing scale
  (re-mapped to the design's 4 to 64 px). `ui.Pill` now draws icon + word; `kind` "danger" of `ui.Button` is the soft red button, "danger-solid" the solid one.
- **Product wins over the design:** brand text is "HotspotPlus Lite" (the public name); "Over quota" is not a member status; the Router page has the MikroTik default plus 7 themes.
- **Live facts in the shell:** the date, the Online badge (open online sessions) and the router footer are filled by `pageEnv` for logged-in full pages only.
- **Not ported:** the voucher sheet rules (`.a4`, `.vch`; the print page keeps `voucher.css` until its own step), the board-only helpers (`.board`, `.u-*` swatches, `.scrim`/`.layer`/`.toast-pos`
  that only place static demos; native `<dialog>` and the fixed toast do that job), and the "Needs attention" / "Router link" screen content (later steps).
- **Defined in CSS but not used yet** (later steps use them): `c-kv`, `c-li`, `c-group-title`, `c-icon-circle`, `c-steps`, `c-rank`, `c-rules` (list.css); the design chart classes `c-chart__grid/line/area/bar/dot/axis` (the usage and dashboard charts);
  `c-plan`, `c-thumb`, `c-setting` (plans, router, settings); `c-bulk` (members bulk bar, step 3); `c-usage` (usage cell); `c-seg`, `c-switch` (usage range, settings); `c-form-grid`;
  the `data-pos="top"` toast. They are shown on the dev gallery where a component exists.
- **Review fixes (2026-11-09):** the toast has one status icon (toast.js sets the alert icon for an error); `ui.EmptyState(title, text)` has a title (h3) and children for a button; `ui.TileIcon` adds an icon to a tile label;
  `ui.TablePaged` puts the pager inside the card; `swap.js` skips polls while hidden or with an open dialog or menu and fades only changed regions; the router footer uses the injected clock (`Deps.Clock`);
  the meter turns warning at exactly 80 %; a submit button shows its spinner while its form request runs; `static.go` no longer rewrites `url()` in CSS (there is no font file).
- **Step 2 (login, password, dashboard):** `c-auth` is the sign-in card (`ui.AuthHead`); the login page and the forced password change use it in the bare layout, which now loads htmx and the scripts too.
  The forced page keeps a "Current password" field and a Log out button (the server needs the first; the shell with its Log out is gone). The rules list (`c-rules`) is `10 or more characters` and `Both passwords match`,
  ticked live by `password.js`; the design's "a letter and a number" is not a product rule (BR-20), so it is not shown. The eye button (`ui.RevealButton`) keeps its icon (`forms.js`).
  The dashboard has four tiles (the design's "Members" tile is not in `dashboard.Overview`; "Peak today" is its own tile), Needs attention as one card with two groups (expiring, near the data limit; no plan name, the overview has none),
  the Router link card (`c-kv`; keeps the time, address and result of each event, the state pill is Healthy / No recent packets / Wrong secret) and three charts (`ui.Plot`: area = busy hours, bars = traffic, dots = active members; the design's
  devices-online-24h and logins-per-day are not in the overview). Every value keeps its tooltip and `data-bar` hooks. The title, the Live line and the summary line are inside the polled fragment (region `head`).
  The fade runs only on htmx swaps and only for regions whose content changed (`swap.js`); the first load has none.
- **Step 3 (members):** the list has a toolbar (search, two unlabeled selects, chips), a bulk bar (`c-bulk`, hidden until a row is ticked; `forms.js` `syncBulk` sets the count and the header checkbox), and the table (`ui.TablePagedHead` takes the header cells so the first can hold the select-all checkbox).
  Cells wrap their value in `.c-table__v`; on a phone that makes a flex row. The usage cell is `c-usage` (figures, bar, "left"). `ui.OnlineState` is the dot with its word. The row menu has icons.
  The sheet host body is `.c-sheet__host`; a fragment that is a `ui.SheetFormPane` (form) or `ui.SheetPane` (div) with a `SheetBody` (scrolls) and a `SheetFoot` (stays at the bottom) fills it. A plain fragment (the plan form, an empty state) still scrolls inside the host with padding.
  The add form has a mono username, a password with the eye (`ui.RevealToggle`) and Generate (`data-generate-digits`, 6 digits), plan options with limit and days, and the "Account enabled" switch row (`ui.SwitchRow`). The credentials card is `ui.NoticeTitled`, `ui.Credentials` (a Copy button per row), `ui.KeyValues` and "Copy all details"; the footer is Add another, Print (the voucher), Done.
- **Next steps:** screens are redesigned one by one (dashboard, members, sessions, plans, vouchers, usage, router, settings, login) on top of these components.

## Not verified
Only checked with headless Chrome screenshots (gallery, members, plans, dashboard, router) at 1280, 900 and 390 px, light and dark. Nobody has used it in a
normal browser yet. Check: keyboard (Esc closes the modal, focus returns), the More menu, Safari and Firefox, and the print page.
