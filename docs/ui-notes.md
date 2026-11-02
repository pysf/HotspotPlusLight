# Admin UI: styling concept

Decision (owner, 2026-11-02, D24): plain server-rendered HTML plus **our own small CSS**, built with
**Tailwind CSS v4** (standalone CLI, no Node). No UI kit. Earlier steps (Bootstrap, then Tabler 1.6.1) are history only;
nothing of them is left in the code.

## Look
Linear-like, Vercel-clean: minimal, compact, calm. Light mode only. English only.
Flat: 1px borders, no shadows, no gradients. Radius 6px (controls) and 8px (cards). 8px spacing rhythm.
The accent color is used sparingly: links, focus ring, the chart bars, checkboxes.

## Tokens
All design values are in one `@theme` block in `internal/web/ui/input.css`. Tailwind's own palette, radii,
fonts and text sizes are switched off there (`--color-*: initial` and so on), so only our tokens exist.
Components (`@layer components` in the same file) and templates use tokens only.

| Token | Value | Use |
|---|---|---|
| `bg` | #FAFAFA | page background, row hover |
| `surface` | #FFFFFF | cards, sidebar, top bar, inputs |
| `fg` | #18181B | text, primary button |
| `muted` | #71717A | secondary text (4.8:1 on white) |
| `line` | #E4E4E7 | hairlines, card borders |
| `control` | #8E8E98 | input borders (3:1 on white, WCAG non-text contrast) |
| `hover` | #F4F4F5 | light hover and active nav background |
| `accent` | #4F46E5 | links, focus ring |
| `success` / `warning` / `error` | #16A34A / #D97706 / #DC2626 | dots, borders, error text |
| `*-soft`, `*-ink` | tints and dark text for them | badges and notices (the ink colors pass AA on the tint) |

Type: Inter variable (one file, Latin subset), page title 24px semibold, section 16px semibold, body 14px,
labels 12px. Bytes, durations, timestamps and counts use `tabular-nums`.

Text and spacing use logical utilities (`ps-`, `pe-`, `start-`, `end-`, `border-e`, `text-start`) so RTL stays possible.

## Build
- Source: `internal/web/ui/input.css`. Output (committed, minified): `internal/web/static/app.css`.
  It is embedded in the binary with `go:embed`; nothing is loaded from a CDN.
- `make css` writes the output. It downloads the pinned Tailwind standalone CLI (v4.3.3) to `tools/`
  (gitignored) if missing and checks its sha256 (the table is in the Makefile).
  Tailwind scans `internal/web/ui/*.templ|go` and `internal/web/admin/*.templ|go` (not the generated `*_templ.go`).
- `make css-check` (part of `make ci`) rebuilds into a temp file and fails if the committed CSS differs.
- Rule: write each class name whole in the source (never build `"btn-" + kind`), or Tailwind will not see it.
  After changing a class or `input.css`, run `make css` and commit the result.

## Font
`internal/web/static/inter-latin-wght-normal.woff2` is the Latin subset, variable weight axis, of Inter 4
(package `@fontsource-variable/inter` 5.2.5, from `cdn.jsdelivr.net`). Licence: SIL Open Font License 1.1, in
`internal/web/static/OFL-Inter.txt`. It is served from `/admin/static/` with `font-display: swap`; system fonts are the fallback.

## Layout and components
- Shell (`ui.Layout`): fixed 220px sidebar (brand, grouped nav, icons), compact top bar (breadcrumb and Log out), then
  `main` (max 72rem). The sidebar item of the current page has `aria-current="page"`.
  Below 1024px the sidebar is hidden; the menu button in the top bar (`data-nav-toggle`, tiny code in `static/app.js`) shows it.
- Login page (`Bare` layout): one centered card, no shell.
- Form pages are at most `max-w-xl`; tables use the full width. Tables scroll sideways on narrow screens.
- Reusable components, all in `internal/web/ui/ui.templ`: `Layout`, `Icon`, `PageHeader`, `Card`, `EmptyState`, `Actions`,
  `FilterBar`, `StatusBadge`, `Button`, `LinkButton`, `Flash`, `ErrorFragment`, `Field`/`Select`/`Textarea`/`Checkbox`/`FieldPair`,
  `Table`, `Pagination`, `ConfirmDelete`/`ConfirmAction`, `CodeBlock`, `BarChart`. Pages build only from these.
- Icons: one `Icon(name)` component with a handful of inline 24px SVGs (stroke, `currentColor`). No icon library.
- Confirmation uses htmx `hx-confirm` (the browser's own dialog, keyboard accessible). No other JavaScript besides htmx and `app.js`.
- Voucher print page and the router page pack are separate and have their own CSS (`voucher.css`, `internal/routerpack`).

## Member list
Shows only: checkbox, username (link), online dot, status badge, data ("used / allowed", with a muted "N left" line).
Everything else is on the member page.

## Not verified
Nobody has looked at this UI in a browser yet. Check the member list, member form, login, plans, batches,
online, usage, settings and router pages at 1280px and 390px wide, and print one voucher sheet.
