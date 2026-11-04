# SITE.md — public landing page + documentation (GitHub Pages)

Brief for building the project's public website: a landing page and a documentation section, hosted on GitHub Pages.

**Visual source of truth: `site-reference.html`** (a single-file draft, "Terminal" design). Match its look, spacing, colors and behavior. Copy its CSS **values** (tokens, sizes, radii, gradients). Do **not** copy its structure: it is a throwaway draft that switches between "landing" and "docs" views with URL hashes and contains **placeholder content**. The real site is multi-page (see §3) and every fact comes from the repository (see §2).

Follow the same engineering taste as the rest of this project: plain HTML + CSS, tiny vanilla JS, **no Node, no bundler, no framework, no CDN, no build step required to publish**.

## 1. Goal and audience
Visitors are hotel/cafe owners and small-ISP admins who self-host. In 10 seconds they must understand what it is, see that it is simple to run (one Go binary, SQLite inside), and reach the install guide. Tone: calm, precise, no hype, no emoji, no stock imagery.

## 2. Content accuracy (most important rule)
The reference contains **invented placeholders**. Replace every one with facts read from this repo (README, `go.mod`, release workflow, `cmd/`, config code, migrations, the admin UI):
- project name, one-line description, repository URL, license;
- release asset names and the real download URL pattern; supported OS/arch;
- real commands and flags (`serve`, `version`, etc. only if they exist), default port, data/DB path, config/env variable names and defaults, first-run/admin account creation flow;
- real systemd unit, reverse-proxy and backup instructions that you have **verified against the code**, not guessed.
Rules: never state a feature, number or command you did not verify. The "1 binary / 0 runtime dependencies / 1 file" strip must be checked against `go.mod` and the code (or reworded/removed). No version badge unless it comes from the real latest release. If something is unknown, **ask** instead of guessing, and list open questions in your final note.

## 3. Site structure (multi-page, real URLs)
```
/index.html                      landing
/docs/index.html                 docs overview + links to all pages
/docs/installation/index.html
/docs/running-as-a-service/index.html
/docs/configuration/index.html
/docs/first-login/index.html
/docs/hotspot-login-page/index.html
/docs/backup-and-upgrade/index.html
/404.html  /robots.txt  /sitemap.xml  /.nojekyll
/assets/css/  site.css  (tokens + base) , docs.css , components.css (or one file if small)
/assets/js/   copy.js  docs-search.js  docs-nav.js
/assets/img/  favicon.svg, og-image.png, optional real screenshots (webp/png)
```
Adapt page list to what the project really needs; keep slugs short, lowercase, hyphenated. One folder per page with `index.html` so URLs have no extension.

## 4. GitHub Pages specifics
- Publish from the `docs/` folder or a `gh-pages` branch (pick what fits the repo; if `docs/` already holds something else, ask). Add `.nojekyll` so files are served as-is.
- The site usually lives at `https://OWNER.github.io/REPO/` (a **sub-path**). Therefore use **relative URLs everywhere** (`../assets/css/site.css`, `../installation/`), never root-absolute ones like `/assets/...`. It must also work unchanged on a custom domain (support an optional `CNAME` file).
- Canonical URL, sitemap and Open Graph URLs need the final base URL: put it in one obvious place (a short README note listing where to change it).
- Custom `404.html` in the same style.
- Do not add analytics, trackers, cookie banners, or external fonts/scripts. Zero third-party requests.

## 5. Design tokens (from the reference)
Dark only (`color-scheme: dark`).
```css
:root{
  --bg:#09090b; --panel:#101013; --elev:#17171b;
  --line:rgb(255 255 255/.08); --line2:rgb(255 255 255/.14);
  --text:#ededf0; --muted:#9a9aa5; --faint:#62626c;
  --accent:#2dd4ee; --accent2:#7c5cff;
  --ok:#3ecf8e; --warn:#f5a524; --bad:#ff6b6b;
  --font:Inter,-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,Helvetica,Arial,sans-serif;
  --mono:ui-monospace,SFMono-Regular,"SF Mono",Menlo,Consolas,monospace;
}
```
Put tokens in one file; components read tokens only (no stray hex values). Type: base 15.5px/1.65; H1 `clamp(38px,5.6vw,64px)` weight 650, letter-spacing -0.04em, gradient on the emphasized words (accent → accent2); section H2 `clamp(28px,4vw,40px)`; mono 12.5px uppercase eyebrows. Use `rem` for sizes where practical. Content width `min(1120px, 100% - 40px)`. Radii: 10px controls, 12px code blocks, 16–22px cards. Page background: two soft radial glows plus a faint 56px grid fading out with a mask (as in the reference).

## 6. Components (build once, reuse on every page)
- **Header:** sticky, blurred dark bar. Brand (mark + name in mono), nav (Features, Docs, GitHub), docs search field (shown on docs pages and landing as in the reference), "Get started" button. `aria-current="page"` on the active link.
- **Hero:** left: small tag, H1, lead, **install bar** (command + Copy button), two buttons. Right: terminal window showing the real first-run output (take it from the actual program output, not invented).
- **Terminal window:** title bar with three dots; syntax tones: comments `--faint`, prompt `--accent`, success `--ok`.
- **Install bar / Copy button:** copies the exact command; label changes to "Copied" for ~1.4s; works without breaking if the Clipboard API is unavailable.
- **Bento feature grid:** 5 cells (spans 3-3 then 2-2-2 on desktop, 2 columns tablet, 1 column phone), each with a one-line promise and an optional tiny visual (usage bars, plan chips, theme dots). Feature claims must be real (§2).
- **Stats strip, CTA band, footer** (docs / GitHub / issues links, license).
- **Docs layout:** left sidebar (sticky, thin vertical line, active item accent) + article column max 760px. On ≤900px the sidebar becomes a horizontal pill strip under the header. Prev/Next links at the bottom of each page.
- **Docs content elements:** H2 with `#` prefix and anchor links, inline code, **code blocks with Copy button** (long lines wrap, never hidden under the button), callout "note" (cyan-tinted), tables (config reference), numbered steps (mono counters).
- **Docs search:** `/` or ⌘K focuses it; filters sidebar entries and (preferably) page sections from a small hand-maintained `search-index.json` or an inline list. It must degrade gracefully: without JS, the sidebar and a link to the docs index still work.
- **Hotspot themes strip** (optional on landing): small previews of the 7 login themes, if it can be done with CSS only.

## 7. Behavior and JS
- Every page works with **JavaScript disabled** (navigation, reading, anchors). JS only enhances: copy buttons, search, scroll-spy highlighting in the sidebar.
- Small ES modules, no dependencies, no globals, no inline `<script>`/`onclick` (a strict CSP must be possible). Honor `prefers-reduced-motion`.
- No hash-based routing. Real page navigation; hashes are only section anchors.

## 8. Responsive and accessibility
- Fine at 360, 390, 768, 1280, 1920 px; no horizontal scroll on any page (code blocks wrap; tables scroll inside their own container).
- Semantic landmarks (`header`, `nav`, `main`, `aside`, `footer`), one `h1` per page, ordered headings, skip link, visible `:focus-visible`, labelled controls, descriptive link text, `lang="en"`.
- Text contrast ≥ 4.5:1 (check `--faint` is never used for important text). Don't rely on color alone.
- Images have `alt`, `width`/`height` set (no layout shift); screenshots in `webp` or optimized `png`.

## 9. SEO and sharing
- Unique `<title>` and meta description per page; canonical link; Open Graph and Twitter card tags with `og-image.png` (1200×630, in the site's style); `favicon.svg`; `robots.txt`; `sitemap.xml` listing all pages.
- Landing H1 and first paragraph state clearly what the project is and who it is for.
- Optional `SoftwareApplication` JSON-LD only if all fields are real.

## 10. Performance
Total weight per page target < 100 KB without images. System font stack only (Inter is used if installed). No render-blocking JS (`defer`). Cache-friendly file names. Inline SVG for icons and the logo mark.

## 11. Build order
1. Read the repo and post: the verified facts list (§2), the page list, any open questions.
2. Shared CSS (tokens, base, components) and the page shell; build the landing page.
3. Docs layout + one docs page (Installation) completely, with real commands.
4. Remaining docs pages, prev/next, search, scroll-spy, copy buttons.
5. `404.html`, `sitemap.xml`, `robots.txt`, OG image, favicon, README note on publishing and where to set the base URL.
6. Checks (§12).

## 12. Acceptance checklist
- Looks like the reference at all five widths; no horizontal scroll.
- Every command in the docs was verified against the repo (ideally actually run); no invented flags, variables or paths.
- All links work (internal ones relative; external ones to the real repo/releases); sitemap matches the pages.
- Works with JS disabled; no external requests (check the network panel); no inline script/style; no hard-coded colors outside the token file.
- Keyboard-only pass; contrast check; Lighthouse (or equivalent) accessibility and SEO scores reported.
- Tested under a sub-path (`/REPO/`) and at the domain root.
- Final note: deviations from this document, open questions, and what must be edited when a new release ships (aim: nothing, by linking to `releases/latest`).
