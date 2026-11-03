# FRONTEND.md — HTML, CSS and UI-code standards

Companion to `DESIGN.md` (what it looks like). This file covers **how the front-end code is written**: HTML, CSS, templates, small JS, and the HTMX contract. Goal: clean, consistent, accessible markup and styles that stay easy to maintain for years with **no build step**.

## 0. Scope and boundary with the backend
- **The backend is out of scope.** Do not refactor, restructure or rename existing Go packages, store code, SQL, auth or config.
- The UI layer is: templates, static assets (CSS/JS), and **thin render handlers** that call existing backend functions, build a view-model, and render a template. If a needed backend function does not exist, add the smallest possible one, keep the existing style, and flag it in your final notes.
- Follow the conventions already in the repo (naming, error handling, routing). Read the repo before writing anything and post a short plan first.

### This is a redesign of an existing UI
The app already has working templates, routes and HTMX behavior (styled in an older, different way). The new design **replaces the look**, not the behavior.
1. **Inventory first** (before changing anything): list the existing templates, CSS, JS, HTMX endpoints and form field names. Post a short plan mapping each existing page/fragment to the new components, what will be replaced, and the build order.
2. **Existing wins over this document** for routes, endpoint paths, form field names, request/response shapes and template names. The endpoints and folder layout below are suggestions; adapt them to what exists and mention any deviation.
3. **Do not mix old and new styling.** Migrate one page at a time; a migrated page uses only the new tokens/components and none of the old CSS. Do not delete old CSS/templates until the page that used them is migrated and verified; then remove what became unused (tell me what you removed).
4. **Behavior must not regress:** every action that works today (create, edit, reset usage, enable/disable, delete, filters, paging, login/logout) must still work after its page is migrated.
5. If something in the existing code conflicts with these docs, or the right choice is unclear, **ask** instead of guessing.

## 1. Stack
Go `html/template` (server-rendered) + HTMX + plain CSS + a few tiny vanilla JS modules. No Node, no bundler, no CSS framework, no UI kit, no CDN. HTMX is vendored (one pinned file) and all assets are embedded with `//go:embed`.
Browser target: current evergreen Chrome, Edge, Safari, Firefox (last ~2 years). Native `<dialog>` and `popover` are required. Newer CSS (`@starting-style`, `:has()`, nesting) is allowed only as progressive enhancement: the UI must stay fully usable without it.

## 2. Front-end layout (target; adapt names to the repo's existing conventions)
```
web/
  templates/
    layouts/      base.html (html/head/body), app.html (shell: sidebar, main), auth.html
    pages/        login.html, users.html, plans.html
    fragments/    HTMX responses: users_rows.html, user_form.html, user_created.html, confirm_delete.html ...
    components/   button, icon_btn, field, input, select, search, chip, card, table, pill, meter,
                  pager, menu, sheet, modal, toast, empty, credentials, icon_*
  static/
    css/    tokens.css  base.css  layout.css  components/<name>.css  app.css
    js/     theme.js  dialog.js  clipboard.js  toast.js  shortcuts.js
    vendor/ htmx.min.js
internal/web/   (only the thin handlers + view-models + template loader, following the repo's existing style)
```
One component = one template partial + one CSS file + one entry on the dev gallery. File names say exactly what is inside.

## 3. HTML best practices
- **Semantics first.** Landmarks (`header`, `nav`, `main`, `aside`), one `h1` per page, logical heading order. `lang="en"`, `dir="ltr"`, viewport meta, unique `<title>` per page.
- **Button vs link:** `<a>` navigates (changes URL), `<button>` performs an action. Never `div`/`span` with click handlers. Buttons have explicit `type`.
- **Forms:** every control has a real `<label for>`; correct `type`/`inputmode`/`autocomplete` (`username`, `current-password`, `new-password`, `tel`, `numeric`); use native `required`/`min`/`max`/`pattern` for instant feedback, but **validate on the server** too. Field errors are linked with `aria-describedby` and marked `aria-invalid="true"`; an error summary region uses `role="alert"`.
- **Tables:** real `<table>` with `<thead>`, `<th scope="col">`, `<caption class="sr-only">`. When CSS turns rows into cards on small screens, keep semantics (add `role="table|row|cell"` where `display` changes would drop them) and use `data-label` + `::before` for labels. Do not render a second copy of the markup for mobile.
- **Dialogs and menus:** native `<dialog>` (sheet and modal) with `aria-labelledby`; `popover` for row menus. Let the platform handle focus trap, Esc and top layer. Return focus to the trigger on close.
- **Live updates:** toast container `role="status"` (`aria-live="polite"`); list region that HTMX updates gets `aria-live="polite"` plus `aria-busy` while loading.
- **Navigation:** `aria-current="page"` on the active nav link. Icon-only buttons need `aria-label`; decorative SVGs get `aria-hidden="true"`.
- No inline `style`, no inline `<script>`, no `onclick=` attributes. Use `hidden`/`inert`/`data-*`/classes instead.
- Keep the DOM shallow: no wrapper `div` without a reason.

## 4. Templates and components
- Pages compose layout + components; components never know which page uses them.
- Each component is `{{define "components/<name>"}}` with a short comment header (inputs, variants, example). Pass **typed props structs** (`ButtonProps`, `FieldProps`, `PillProps`...) or one tiny `dict` helper used only for components. No loose maps of unknown keys.
- **Duplication rule:** the second time markup appears, extract a component. Pages contain layout and component calls, not repeated raw HTML.
- Fragments reuse the same components as full pages, so a row looks identical whether it came from a full load or an HTMX swap.
- Templates contain **no logic beyond** `if/range/with`, component calls and small formatting helpers. All display formatting ("3.2 / 20 GB", "in 4 days", status → `data-status`, meter value/max) is done in view-models in Go.
- Never use `template.HTML`/`template.JS` on user-controlled data. Rely on auto-escaping.
- **Dev component gallery** at `/dev/components` (only when `DEV=1`): every component in every variant and state, in all three themes, at desktop and phone width. It is the main review surface and part of the definition of done.

## 5. CSS best practices
**Architecture**
- Files: `tokens.css` (all variables, 3 themes, see DESIGN.md §3), `base.css` (minimal reset, typography, focus), `layout.css` (shell, page grid, responsive rules), `components/<name>.css`, and `app.css` which declares `@layer tokens, base, components, utilities;` and imports the others. No CSS in templates.
- **Cascade layers** control order; do not fight specificity with `!important` (never allowed) or long selectors.

**Naming and selectors**
- `c-<component>` for components, `c-<component>__<part>` for inner parts, `data-*` or `aria-*` attributes for variants and states (`[data-variant="primary"]`, `[aria-pressed="true"]`, `[aria-current="page"]`). No modifier-class soup.
- Style with classes only (no ID selectors, no styling via tag selectors inside components). Max one class deep plus a state. Use `:where()` to keep base rules at zero specificity.

**Tokens and values**
- Components read **only** tokens: no hex colors, no magic numbers, no px values outside the scale. Themes change tokens, never component CSS (no `[data-theme]` selectors in components).
- Sizes for type and spacing use `rem`/`em` so user font-size settings work (the px values in DESIGN.md assume a 16px root; 14px = `.875rem`). 1px hairlines stay `1px`.
- Use **logical properties** (`margin-inline`, `padding-block`, `inset-inline-start`, `border-start-start-radius`, `text-align: start`) instead of left/right, so RTL support later costs nothing even though the UI is LTR today.

**Layout and responsiveness**
- Mobile-friendly but desktop-first as specified. Use CSS Grid/Flex, `gap` for spacing (not margin hacks), `min()`/`clamp()` for fluid sizing, `dvh` for full-height sheets.
- Breakpoints live in one place (`layout.css`): 980px and 820px, plus 560px for the sheet. Prefer container queries for components that must adapt to their container.
- No horizontal scrolling at any width; test at 360px, 768px, 1280px, 1920px.

**Quality**
- Interactive states are complete: default, hover, `:focus-visible`, active, disabled, loading, error. Focus ring defined once in `base.css`.
- Honor `prefers-reduced-motion` and `color-scheme`. Keep transitions on cheap properties (color, background, opacity, transform).
- Remove unused CSS; keep each component file focused and short (a component file over ~120 lines is a smell).
- Fonts: system stack by default. If Inter is used, self-host a subset `woff2` with `font-display: swap` (no Google Fonts).

## 6. JavaScript policy
Progressive enhancement: pages stay navigable and forms submittable with JS disabled where reasonable. Vanilla ES modules, no globals, no dependencies, each file under ~80 lines, listening to HTMX/DOM events:
- `theme.js` toggles `data-theme` and writes the `theme` cookie (server renders the right theme on first paint).
- `dialog.js` opens/closes sheet and modal on swap or server event; restores focus.
- `clipboard.js` copies credentials ("Copy details").
- `toast.js` shows/hides toasts.
- `shortcuts.js` focuses search on `/`.

## 7. HTMX UI contract
- One URL serves both full page and fragment; the handler decides by `HX-Request`. Fragments reuse components.
- Search/filter: `hx-get` to a rows endpoint, `hx-trigger="input changed delay:300ms, search"`, `hx-sync="this:replace"`, `hx-push-url="true"` so filters live in the URL (reload, Back and sharing work). Counts on chips update via `hx-swap-oob`.
- Mutations return the updated fragment and an `HX-Trigger` event (e.g. `users-changed`, `toast`, `close-sheet`); the list refreshes itself from `users-changed from:body` using the current query string.
- Validation: respond `422` with the form fragment re-rendered (input preserved, errors inline). Configure HTMX `responseHandling` so `422` swaps; other 4xx/5xx show a generic error toast via `htmx:responseError`.
- Submit buttons use `hx-disabled-elt="this"`; loading uses `hx-indicator` (also sets `aria-busy`).
- Destructive actions open a server-rendered confirm modal first (no `window.confirm`).
- Suggested endpoints (adapt to the existing router): `GET /users`, `GET /users/rows`, `GET /users/new`, `GET /users/{id}/edit`, `POST /users`, `PUT /users/{id}`, `POST /users/{id}/reset-usage`, `POST /users/{id}/toggle`, `GET /users/{id}/confirm-delete`, `DELETE /users/{id}`, and the same shape under `/plans`.

## 8. Performance and delivery
- Serve CSS/JS with content-hash or version in the URL and long cache headers (hash computed at startup from embedded files; no build tool). Gzip/brotli if the server already does it.
- Avoid layout shift: reserve space for the sidebar, table and sheet; set `width`/`height` on the logo image.
- Load the HTMX script with `defer`. Keep the first paint free of blocking JS.
- Inline SVG icons as partials (no icon font, no sprite download).

## 9. Checks before calling a step done
- `/dev/components` reviewed in all three themes and at 360px and 1280px widths.
- Keyboard only: tab order sensible, focus visible, Esc closes sheet/modal/menu, focus returns to the trigger.
- Contrast ≥ 4.5:1 for text in every theme; status never conveyed by color alone.
- Works with JS disabled for navigation and login; no console errors; no inline style/script (so a strict CSP would pass).
- HTML validates (no duplicate ids, labels connected, headings ordered). Optionally run Lighthouse/axe and fix accessibility findings.
- Template smoke test renders every component and fragment with sample data.
- `gofmt`, `go vet`, `go test ./...` still pass.

## 10. Build order
1. `tokens.css`, `base.css`, shell layout: verify all three themes.
2. Components + `/dev/components` gallery.
3. Layouts, Login page.
4. Users: list + filters → new/edit sheet → created view → row actions + confirm modal.
5. Plans: list + sheet + delete rule message.
6. Polish: empty/loading/error states, responsive pass (≤820px), accessibility pass.
Small commits; after each step run the checks above.

## 11. Definition of done
Matches `index.html` in all three themes at desktop and phone width; no inline JS/CSS; no hard-coded colors or sizes in components; no duplicated markup; every component on the gallery; checks in §9 pass; README section on adding a component and adding a page; a short note listing deviations from these docs and any backend functions you had to add.
