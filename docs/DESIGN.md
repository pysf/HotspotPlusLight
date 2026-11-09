# DESIGN.md — UI spec

Visual and interaction spec for the web UI of an internet-accounting service (hotels, cafes, small ISPs). Companion to `FRONTEND.md` (how the HTML, CSS and UI code is written). `index.html` is the **visual reference only**: a throwaway single-file mock. Match how it looks and behaves; do **not** copy its code or structure.

Stack: Go `html/template` (server-rendered) + HTMX + plain CSS + a few tiny vanilla JS modules. No Node, no bundler, no CSS framework, no UI kit.

## 1. Product scope
English, LTR. Pages: **Login**, **Users**, **Internet plans**. Desktop-first, fully responsive down to phones.
Main jobs: create a user, reset usage, disable/enable, delete. The Users list can hold thousands of rows: search by **username or mobile**, filter by plan and status, paginate.

## 2. Look and feel
Apple-like: calm, airy, minimal. Default theme is **Air**. One accent color; green/amber/red only for status. No decorative icons, no illustrations, no emoji, no gradients (except the faint login glow), no heavy shadows. Generous whitespace.

## 3. Themes (token-driven)
Three themes, selected by `data-theme` on `<html>`: `air` (default), `light`, `dark`. The choice is stored in a cookie (`theme`) so the **server renders the right `data-theme` on first paint** (no flash). A tiny JS module toggles it and writes the cookie.

Components never contain theme-specific rules and never hard-code colors, radii or sizes. Everything comes from tokens, including **shape** tokens, so a theme can change both colors and proportions with zero component overrides.

```css
@layer tokens, base, components, utilities;

@layer tokens {
  :root {
    --font-sans: Inter, -apple-system, BlinkMacSystemFont, "SF Pro Text", "Segoe UI", Roboto, sans-serif;
    --font-mono: ui-monospace, SFMono-Regular, Menlo, monospace;
    /* spacing scale (4px base): use only these */
    --s-1: 4px; --s-2: 8px; --s-3: 12px; --s-4: 16px; --s-6: 24px; --s-8: 32px; --s-11: 44px;
    --ok: #3ecf8e; --warn: #f5a524; --bad: #ff5d5d;
    --ease: 150ms ease;
  }

  /* Air (default) */
  :root, [data-theme="air"] {
    color-scheme: light;
    --bg: #f5f5f7; --panel: #fff; --elev: #f0f0f3;
    --line: rgb(0 0 0 / .06); --line-strong: rgb(0 0 0 / .12);
    --text: #1d1d1f; --muted: #6e6e73; --faint: #a1a1a6;
    --accent: #0071e3; --on-accent: #fff;
    --shadow-card: 0 1px 2px rgb(0 0 0 / .04), 0 10px 30px rgb(0 0 0 / .05);
    --shadow-pop: 0 12px 40px rgb(0 0 0 / .10);
    --r-ctl: 12px; --r-card: 18px; --r-btn: 99px; --r-sheet: 20px;
    --h-btn: 36px; --h-input: 42px; --cell-y: 18px; --cell-x: 22px; --fs-title: 32px;
    --sidebar-bg: rgb(245 245 247 / .8);
  }
  /* Light */
  [data-theme="light"] {
    color-scheme: light;
    --bg: #fbfbfc; --panel: #fff; --elev: #f4f4f6;
    --line: rgb(0 0 0 / .07); --line-strong: rgb(0 0 0 / .14);
    --text: #111113; --muted: #6e6e76; --faint: #a3a3ab;
    --accent: #4c8dff; --on-accent: #fff;
    --shadow-card: none; --shadow-pop: 0 10px 40px rgb(0 0 0 / .12);
    --r-ctl: 8px; --r-card: 12px; --r-btn: 8px; --r-sheet: 0;
    --h-btn: 34px; --h-input: 38px; --cell-y: 13px; --cell-x: 16px; --fs-title: 26px;
    --sidebar-bg: var(--bg);
  }
  /* Dark */
  [data-theme="dark"] {
    color-scheme: dark;
    --bg: #0b0b0c; --panel: #111113; --elev: #17171a;
    --line: rgb(255 255 255 / .07); --line-strong: rgb(255 255 255 / .13);
    --text: #f2f2f3; --muted: #8b8b93; --faint: #5d5d65;
    --accent: #4c8dff; --on-accent: #fff;
    --shadow-card: none; --shadow-pop: 0 10px 40px rgb(0 0 0 / .45);
    --r-ctl: 8px; --r-card: 12px; --r-btn: 8px; --r-sheet: 0;
    --h-btn: 34px; --h-input: 38px; --cell-y: 13px; --cell-x: 16px; --fs-title: 26px;
    --sidebar-bg: var(--bg);
  }
}
```

## 4. Foundations
- **Font:** `--font-sans`, base 14px/1.45. Page title `--fs-title`/600, letter-spacing -0.025em. Sheet/modal title 16px/600. Table headers and field labels 12px muted. Numbers use `font-variant-numeric: tabular-nums`.
- **Spacing:** only the `--s-*` scale. Page padding 36px 44px (≤980px: 28px 24px; ≤820px: 22px 16px). Content max-width 1180px, centered.
- **Motion:** `--ease` on color/border/background; sheet slide 250ms. Honor `prefers-reduced-motion` (no transitions).
- **Icons:** inline SVG, 16px, stroke 1.7, `currentColor`, line style. Each icon is one template partial, not copy-pasted markup.
- **Focus:** `:focus-visible` ring on every interactive element: 2px `--accent`, 2px offset. Never remove outlines without replacement.
- **Contrast:** text ≥ 4.5:1 in all themes. Status is never color-only (always a text label).

## 5. Component catalog
Each component is one Go template partial (`components/<name>`) plus one CSS file (`components/<name>.css`), styled only through tokens. Class prefix `c-`; variants via `data-*` attributes (not modifier class soup). Every component must appear on the dev gallery page (see FRONTEND.md).

| Component | Spec |
|---|---|
| `c-btn` | height `--h-btn`, padding 0 `--s-4`+2px (Air) / `--s-3`+2px, radius `--r-btn`, weight 500. `data-variant`: `default` (elev fill, strong border), `primary` (accent fill), `danger` (default with `--bad` text), `ghost`. `data-size="block"`: full width, height +6px. Disabled: 40% opacity, no pointer events. |
| `c-icon-btn` | 32×32, radius `--r-ctl`, muted; hover elev. Requires `aria-label`. |
| `c-field` | label (12px muted) above control, gap 6px; fields separated by 16px; two-column rows gap 12px; error text 12px `--bad` below. |
| `c-input` | height `--h-input`, padding 0 12px, radius `--r-ctl`, 1px strong border (Air: shadow-ring instead), panel bg. Focus: accent border + 3px accent ring at 22% (Air: 2px accent ring). |
| `c-select` | native `<select>`, same box as input, custom chevron, 32px right padding. Use `<datalist>`/small module only if search-in-select is truly needed. |
| `c-search` | input with leading search icon (36px left padding) and a `/` keyboard hint; max-width 420px. `/` focuses it. |
| `c-chip` | filter pill, height 28–30px, 13px, muted; count at 60% opacity. `aria-current="true"` for selected: inverted (Light/Dark) or accent fill (Air). Chips are links/buttons that trigger the list request. |
| `c-card` | panel bg, `--r-card`, 1px line border (Air: none + `--shadow-card`), overflow hidden. |
| `c-table` | header 12px muted; cell padding `--cell-y` `--cell-x`; row hairline dividers; row hover elev; whole row opens edit. Columns (Users): User (name + mobile below), Plan (+ speed), Usage, Expires (+ relative), Devices, Status, actions. |
| `c-meter` | usage bar: **native `<progress>`/`<meter>`** styled with CSS (no inline `style` attributes). 4–5px high, rounded; accent, warn ≥80%, bad at 100%. Text above: `used / total GB`. |
| `c-pill` | status: 12px/500 coloured label, no dot. `data-status`: `active` (ok), `expiring` (warn, ≤3 days), `expired` (bad), `disabled` (faint). |
| `c-menu` | row actions via native `popover`. Min-width 176px, 5px padding, items 8px 10px, destructive item in `--bad` after a divider. Esc/outside-click close for free. |
| `c-sheet` | right-side drawer built on native `<dialog>`: 430px (full width ≤560px), header, scrolling body, sticky footer (Cancel + primary). `::backdrop` 50% black + 2px blur. Radius `--r-sheet` on the left corners (0 on mobile). Slide-in with `@starting-style`/`transition-behavior: allow-discrete` as progressive enhancement. |
| `c-modal` | confirm dialog, native `<dialog>`, 380px, centered, 22px padding. Required for every reset/delete; destructive confirm uses `danger`. Content is server-rendered. |
| `c-toast` | bottom-center pill, 2.2s, inverted colors; `role="status"`. Triggered by `HX-Trigger` events. |
| `c-pager` | "1–15 of 143" + Previous/Next buttons (disabled at ends). |
| `c-empty` | centered muted text, 56px vertical padding. Also loading skeleton rows and error state variants. |
| `c-credentials` | success view after creating a user: username, password (monospace), plan, expiry, and a **Copy details** button. This is what staff hands to a guest. |

## 6. Pages
- **Login:** centered 360px column on `--bg` with a faint accent glow. Logo slot (52px, from config: business logo or default mark), title, username, password, primary block button. Inline error text. No sign-up, no "forgot".
- **App shell:** sticky 232px sidebar (brand, nav: Users, Internet plans; bottom: Theme, Sign out). ≤820px: becomes a sticky top bar (icons + nav, footer buttons icon-only).
- **Users:** title + count, "New user" primary button; toolbar (search + plan select); status chips with counts (All, Active, Expiring soon, Expired, Disabled); table card; pager. Click a row → edit sheet. Row menu: Edit, Reset usage, Disable/Enable, Delete. Edit sheet also has an "Actions" section with the same.
- **New/edit user sheet fields:** Username, Mobile, Password (+ Generate), Plan, Expires, Device limit, Speed (Mbps). Choosing a plan pre-fills Expires/Devices/Speed (still editable). After create: success view (`c-credentials`) with "Create another" and "Done".
- **Internet plans:** table (Plan, Volume, Duration, Speed, Devices, Users, actions) + New/Edit sheet (name, volume GB, duration days, speed Mbps, device limit). A plan in use cannot be deleted (show a clear message).

## 7. Responsive
- **≥821px:** sidebar + table.
- **≤820px:** top bar; table rows become cards (name/mobile on top, status top-right beside the menu, then labeled lines Plan/Usage/Expires/Devices). Do this with CSS (`display:block` + `data-label` via `::before`), not a second markup. No horizontal scrolling anywhere.

## 8. Interaction rules (HTMX-friendly)
- Filtering/searching/paging updates only the list region and pushes the URL (`hx-push-url`), so filters survive reload, back button and sharing. Search debounced 300ms; cancel in-flight requests.
- Forms submit via HTMX; validation errors re-render the form fragment in place (same sheet), keeping the user's input.
- After any mutation the list refreshes and a toast confirms. Submit buttons are disabled while a request runs.
- Row menu, sheet and modal behavior comes from native `popover`/`<dialog>`. Custom JS only where the platform has no answer (see FRONTEND.md, JavaScript policy).

## 9. Don'ts
No UI kit, no pre-made admin template, no CSS framework. No additional accent colors. No new spacing/radius/size values outside the tokens. No hard-coded colors in components. No inline `style` or `<script>`. No theme-specific selectors inside component CSS.
