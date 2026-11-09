# DESIGN.md: UI spec (design v2, 2026-11-09)

Visual and interaction spec of the admin UI. The source is the design canvas copied to `docs/design/` (`hp.css` plus the boards).
`internal/web/static/css/` is the production port of `hp.css`. Code rules: `FRONTEND.md`. What we changed on the way: `ui-notes.md`, "Design v2".
When the look changes, change the board in `docs/design/` first, then the CSS.

## 1. Idea
A quiet tool for one person at a front desk. Calm: light grey page, white rounded cards, thin lines, soft shadows, one blue accent.
Colour is for meaning only. Status is never colour alone: icon + word + colour. Online is a filled dot, offline a hollow ring.
No emoji, no gradients, no decorative illustrations. English, LTR.

## 2. Themes
`<html data-theme>` is `light`, `dark` or `auto`. The `theme` cookie holds `light` or `dark`; without it (or with any other value) the server renders `auto`
and the OS decides (`prefers-color-scheme`). The server renders the attribute, so there is no flash. `theme.js` switches to the other mode and writes the cookie.
Components never name a theme. Only `tokens.css` does (and the theme button swaps its sun and moon icons in `icon.css`).

## 3. Tokens (`tokens.css`)
Colour (light / dark): `--c-bg`, `--c-surface`, `--c-surface-2`, `--c-fill`, `--c-fill-2`, `--c-line`, `--c-line-strong`, `--c-input-line`, `--c-text`, `--c-text-2`,
`--c-text-3`, `--c-accent`, `--c-accent-hover`, `--c-accent-fg`, `--c-accent-soft`, `--c-focus`, `--c-danger`, `--c-danger-hover`, `--c-danger-fg`, `--c-glass`, `--c-scrim`.
Status (four families, each `-fg`, `-bg`, `-dot`): `--st-ok-*`, `--st-warn-*`, `--st-bad-*`, `--st-off-*`. Shadows `--sh-1` card, `--sh-2` raised, `--sh-3` sheet and menu.
Values are exactly those of the design; do not change them (contrast was checked: text 4.5:1, status text on its fill 5.3:1, input border 3:1).

| Group | Tokens |
|---|---|
| Font | `--font` system stack (`-apple-system, BlinkMacSystemFont, "SF Pro Text", ... sans-serif`), `--font-mono`. No font file is shipped. |
| Type | `--fs-xs` 12, `--fs-sm` 13, `--fs-md` 15 (body), `--fs-lg` 17, `--fs-xl` 20, `--fs-2xl` 28 (24 on phone), `--fs-3xl` 34 (30 on phone). Numbers use tabular figures. |
| Space | `--s-1..8` = 4, 8, 12, 16, 24, 32, 48, 64 px. |
| Radius | `--r-sm` 8, `--r-md` 12, `--r-lg` 20, `--r-xl` 28, `--r-pill` 999. Buttons, chips, pills: pill. Inputs, menus, alerts: 12. Cards, tiles: 20. Sheets, dialogs: 28. |
| Control | `--ctl` 40 px (44 px on phone), `--pad` page padding 32 px (16 on phone), `--nav-w` 240 px (76 as icon rail). |
| Motion | `--ease` cubic-bezier(.2,.8,.2,1), `--dur` 160 ms, sheet 200 ms, refresh fade 400 ms. Reduced motion turns it all off. |

Other component sizes (brand mark, pill height, meter, switch...) are tokens too. Never write a one-off value in a component; add a token.

## 4. Breakpoints
Media queries, same numbers as the design: **under 1000 px** the side bar is an icon rail (76 px, labels clipped for screen readers, no badge);
**under 700 px** the side bar is gone, a bottom tab bar appears, controls are 44 px, padding 16 px, tables become cards, sheets are bottom sheets.
Both numbers live in the component files (`layout.css`, `table.css`, `sheet.css`, ...). No sideways scroll from 360 px: grids use `minmax(0, 1fr)`, long values `overflow-wrap: anywhere`.

## 5. App shell (`ui.Layout`, `layout.css`)
Written once in the base layout:
- **Side bar** (glass): brand mark + "HotspotPlus Lite" + business name; nav: Dashboard, Members, Online now, Plans, Vouchers (the batches page), Usage, Router, Settings. The page title and h1 equal the nav label ("Vouchers", "Online now"). The Online now badge shows the online count and is hidden at 0; the Online page and the dashboard fragment refresh it out of band every 10 s (`ui.OnlineBadge`, so it also appears when the count goes from 0 to more);
  footer "Router connected" (dot filled) or "No recent router packets" (hollow; a packet in the last 5 minutes counts).
- **Top bar** (glass, 64 px / 60 on phone): business name on phone, today's date, Password link, theme icon button, Log out. On phone the three are 44 px icon buttons.
- **Page**: `.l-page` is a grid with 24 px gap. Page header `.l-page-head`: h1, optional line under it, actions on the right (full width on phone).
- **Tab bar** (phone): Home, Members, Online, Vouchers, More. More opens a menu with Plans, Usage, Router, Settings and is current when one of them is open.
- `aria-current="page"` is set in one function (`navAttrs`). The icon sprite is written once (`ui.Sprite`); icons are `<svg class="c-icon"><use href="#i-name"/></svg>`.

## 6. Components (`c-` classes, variants by `data-*`)
| Component | Notes |
|---|---|
| `c-btn` | `data-variant`: default, `primary`, `ghost`, `danger` (soft red), `danger-solid`. `data-size`: `sm`, `block`. Loading: `.htmx-request` or `data-loading` shows a spinner. Disabled: 42 % opacity. |
| `c-icon-btn` | Round, `--ctl` square, `data-fill` for a quiet background. Needs `aria-label`. |
| `c-field` | Label (13/600) 6 px above the control; hint or error (with icon) right under it. |
| `c-input`, `c-select` (+ `c-select-wrap`), `c-search` | 1 px `--c-input-line` border, radius 12, focus ring 3 px. Search is a pill with an icon and a `/` hint. |
| `c-check`, `c-switch`, `c-seg`, `c-hit`, `c-choice` | Custom checkbox and switch; `c-hit` makes a 44 px tap area. |
| `c-chip` | Pill with count; selected = inverted (`aria-current`, or `aria-pressed`). |
| `c-pill` | Icon + word, `data-tone` ok / warn / bad / off / info (`ui.Pill(status, text)` picks tone and icon). No dot. `c-dot` (`data-on`) follows it with 12 px space. `c-live`, `c-badge`. |
| `c-meter` | Native `<meter>`: accent below 80 %, warning from 80 %, red when full. `c-usage` groups it with its figures. |
| `c-card`, `c-tile`, `c-kv`, `c-li`, `c-group-title` | Card 20 px radius; `data-pad` adds padding. Tiles 4 across (2 under 1000 px). |
| `c-table`, `c-pager`, `c-bulk` | Cell 12 x 16. The pager sits inside the table card (`ui.TablePaged`) under the rows, with a 1 px top line. A card that holds a table is `data-flush` (the only card that clips). Under 700 px rows are cards from `data-label`; `thead` is hidden by clipping, not `display: none`. |
| `c-menu`, `c-menu__item` | Popover, glass, `--sh-3`; items 40 px, optional icon, danger in red after a divider. |
| `c-sheet`, `c-modal`, `c-toast`, `c-notice`, `c-empty`, `c-skel` | Native `<dialog>` sheet (right drawer, bottom sheet on phone) and modal (icon, title, text, buttons); toast pill (errors stay until closed); notice with icon (ok, warn, error, info); empty state with icon circle; skeleton lines. |
| `c-credentials`, `c-code`, `c-chart` | Credential blocks in big monospace; code box; charts are inline SVG with classes (`c-chart__line`, `__bar`, ...). |

## 7. Interaction rules (htmx)
- Filtering, searching and paging update only the list region and push the URL. Forms submit with htmx; errors (422) re-render the form fragment in place.
- Polled regions (`data-poll`) skip a poll while the tab is hidden or a dialog or menu is open (`swap.js`). After a swap only the regions (`data-region`) whose content changed get `u-swap-in` (55 % to 100 % opacity in 0.4 s); the first render does not fade. Refreshed regions have no `aria-live`. Tiles keep their size, charts keep their view box, so nothing jumps.
- Row menu, sheet and modal are native `popover` and `<dialog>`. Destructive actions ask in the shared confirm dialog first.
- Focus: `:focus-visible` 3 px outline in `--c-focus`, offset 2 px. Never removed. Touch targets are 44 px under 700 px.
