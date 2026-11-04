# HOTSPOT-LOGIN.md — guest Wi-Fi login page

Brief for implementing the **hotspot login page** (what guests see when they connect to a hotel/cafe Wi-Fi) in the existing Go + HTMX project.

**Visual source of truth: `hotspot-login.html`** (a throwaway preview file). Match how it looks and behaves in all 7 themes. Copy its CSS **values exactly** (colors, sizes, radii, gradients). Do **not** copy its preview bar (`.proto`), the theme-switcher script, or the demo submit script.

Also follow `FRONTEND.md` (HTML/CSS/template standards) and the visual language of `DESIGN.md`, with the exceptions in §2.

## 1. What to build
A single public page with: optional logo, welcome heading, one-line subtitle, **Username**, **Password**, **Log in** button, and an inline error message. One fixed layout, **seven selectable color themes**.

Customizable per installation (stored with the app's existing settings mechanism; add the smallest thing if none exists):
| Setting | Notes |
|---|---|
| Theme | one of: `air` (default), `midnight`, `aurora`, `dusk`, `ripple`, `sunrise`, `ocean` |
| Heading | default "Welcome to {business name}" (the sample says "Welcome to Hotel Aria") |
| Subtitle | default "Sign in with the details from reception to get online." |
| Logo | optional uploaded image. If none, show the default Wi-Fi mark exactly as in the reference. The logo box is always 56×56, radius 16; an uploaded image is shown inside it with `object-fit: contain`. |

Theme is rendered server-side as `<body data-theme="air">` (use these readable keys, not the a–g letters of the preview).

## 2. Differences from the admin dashboard (important)
- This page is **standalone and tiny**. It does **not** use HTMX, the dashboard CSS bundle, or the admin layout. One small CSS file (all 7 themes, a few KB) plus an optional logo image.
- It must work with **JavaScript disabled**: a plain `POST` form. An optional small module may add a loading spinner (`aria-busy`) and double-submit protection, nothing else.
- It runs inside the phone's **captive-portal mini browser**, which is restricted: no external requests (no CDN, no web fonts, no analytics), no reliance on cookies/localStorage/service workers, no `window.open`/popups, no clipboard, no downloads. Keep total weight small (target < 30 KB without the logo).
- Still follows FRONTEND.md: semantic HTML, token-driven CSS, no inline `style`/`<script>`/`onclick`, no hard-coded colors outside the theme blocks.

## 3. Existing behavior wins
Before changing anything, **inspect the current hotspot login**: where it is served, the form `action`/`method`, field names, hidden fields the hotspot engine requires (for example redirect or session parameters), how errors are returned, and where users land after success. **Preserve all of that exactly**; this task only changes presentation and adds the settings above. Post a short plan first; if the way the page is served or how login is performed is unclear, ask instead of guessing.

## 4. Layout (identical in every theme)
- Page: centered, `min-height: 100dvh`, padding `max(24px, env(safe-area-inset-top)) 16px`.
- Card: max-width 400px, padding 40px 32px 32px, radius 24px (≤480px: padding 32px 22px 24px, radius 22px). Colors come from tokens.
- Logo 56×56 radius 16, centered, 20px below to heading. Heading 28px/600, letter-spacing -0.03em, `text-wrap: balance`. Subtitle muted, 8px below heading, 28px above the form.
- Fields: label 13px/500 muted above the input (6px gap), 14px between fields. Input height 48px, radius 14px, **font-size 16px** (prevents iOS zoom), focus = accent border + 4px ring.
- Button: full width, height 50px, **pill** (radius 99px), 600 weight; hover brightens; visible `:focus-visible` outline; busy state shows a spinner.
- Error: 13px, `--bad` color, `role="alert"`, shown between the fields and the button.
- Phone-first behavior, fine on desktop: same card centered.

## 5. Themes (tokens only)
A theme changes **only** the CSS variables and an optional background decoration; never layout. Token names (as in the reference): `--bg`, `--bg-img`, `--blur`, `--card`, `--card-bd`, `--card-shadow`, `--text`, `--muted`, `--accent`, `--bad`, `--ring`, `--logo-bg`, `--logo-fg`, `--logo-bd`, `--in-bg`, `--in-bd`, `--in-bg-f`, `--btn-bg`, `--btn-fg`, `--btn-shadow`. Dark themes also set `color-scheme: dark`.

| Key | Character | Notes |
|---|---|---|
| `air` | light grey, Apple blue `#0071e3` | default |
| `midnight` | graphite, blue glow + faint fading grid | grid is a `::before` with a mask |
| `aurora` | pastel mesh gradient, frosted-glass card, indigo | uses `backdrop-filter` (with `-webkit-` prefix) |
| `dusk` | violet-black, glow from below, glass card, violet button | dark |
| `ripple` | sage paper, concentric signal rings behind the card, deep green | rings are a `::before` with a mask |
| `sunrise` | warm cream, low sun glow, coral accent, espresso button | |
| `ocean` | deep teal color-block, white card | |

Exact values are in `hotspot-login.html`. Keep one theme block per theme, and adding a new theme must require **only** a new block of tokens (no other CSS or template change). Backgrounds are CSS only; no photos or image files.

## 6. Accessibility and forms
- Real `<label for>` per input; `name="username"` / `name="password"` unless the existing hotspot contract requires other names (keep those).
- `autocomplete="username"` / `current-password`, `autocapitalize="none"`, `autocorrect="off"`, `spellcheck="false"`.
- One `h1`; `lang="en"`, correct viewport meta with `viewport-fit=cover`.
- Contrast ≥ 4.5:1 for text and for the button label in every theme. Check each theme; if one fails, adjust that theme's tokens slightly and tell me what changed.
- Visible focus on every control; error announced via `role="alert"`; respects `prefers-reduced-motion`.
- Uploaded logo: accept PNG/JPG/WebP only (or sanitize SVG strictly, rejecting scripts and external references), limit size (e.g. 200 KB) and dimensions, store it safely, serve it with a correct content type and `alt=""` (decorative).

## 7. Admin side (second step, after the page works)
In the admin dashboard, add a small **Login page** settings section (built with the admin design system from DESIGN.md/FRONTEND.md): theme picker (swatches of the 7 themes), heading, subtitle, logo upload/remove, and a live or refreshable preview of the real page. Saving applies immediately. Keep it as a separate step so the guest page can ship first.

## 8. Build order
1. Inventory the existing hotspot login (§3) and post the plan.
2. Template + `hotspot-login.css` with the Air theme; verify the form still logs a user in.
3. Add the other six themes; add a dev-only way to preview all themes (for example `?theme=ocean` when `DEV=1`).
4. Settings storage + rendering of heading/subtitle/logo/theme.
5. Optional loading-state module; error and busy states.
6. Admin settings section (§7).

## 9. Acceptance checklist
- Looks like the reference in all 7 themes at 360, 390 and 1280 px widths; no horizontal scroll.
- Logging in works exactly as before (including any hidden fields and the success redirect); wrong credentials show the error.
- Works with JS disabled; makes no external requests; page weight within budget.
- No inline style/script; no hard-coded colors outside theme blocks; adding a theme = adding one token block.
- Contrast and keyboard checks pass; tested in a real captive-portal mini browser (iOS and Android) or the closest available equivalent, and noted if not possible.
- A short note listing deviations from this document and anything you added to the backend.
