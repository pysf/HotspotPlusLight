# Phase 13a notes (2026-11-04)

Scope: license, public name, technical README, release names. The site is 13b.

## Done
- `LICENSE`: the full GNU AGPL-3.0 text from gnu.org (661 lines), not changed.
- Public name "HotspotPlus Lite": admin page title and sidebar brand (`internal/web/ui/ui.templ`), voucher print title (`internal/web/admin/vouchers.templ`), README title. The generated `*_templ.go` files were edited by the same text change (the templ tool needs the network; the change is only a string).
- Renamed too (owner 2026-11-04): the first-start banner (`=== HotspotPlus Lite first start ===`), the RouterOS setup script comment, the Windows service display name. Not renamed on purpose: the Windows service name/ID `HotspotPlusLight`, the data folder `C:\ProgramData\HotspotPlusLight`, the binary, module, release file names and repo URL.
- README rewritten as a technical file with mermaid diagrams. ASCII only. Performance section and its markers are kept (`perf/report.go` finds them: exactly one start and one end marker).
- Release: `make release` names files `hotspotpluslight-<os>-<arch>.tar.gz` / `.zip` and `SHA256SUMS`, no version in the name. New `.github/workflows/release.yml`.

## Test changes
- New `e2e/phase13_test.go`: E13.01 (LICENSE text, README title and license line, admin title/brand/login title), E13.02 (the Makefile `PLATFORMS` list gives exactly the 9 expected asset names, no `$(VERSION)` in names, release.yml content, README describes the names).
- E8.09 now checks the technical README: new name, the hotspotplus link, AGPL-3.0, the site link, make targets, perf markers, at least 5 mermaid blocks, ports, ASCII only, no Dockerfile. Kept from the old test: the "removed in Phase 12a" strings must be absent, ASCII only, no Dockerfile, the Makefile `set-password` target.
- Moved out of E8.09 (these are user and install content, now on the site; HARD EXIT ITEM for 13b: the site test must assert every one of them on the site pages): `make build`, `HSP_SECRET_KEY`, `HSP_DATA_DIR`, `HSP_ADMIN_ADDR`, `HSP_RADIUS_AUTH_ADDR`, `HSP_RADIUS_ACCT_ADDR`, `HSP_LOG_LEVEL`, `HSP_LOG_FORMAT`, "first start", `make set-password`, `admin set-password`, `/admin/router`, "hotspot folder", `8081` with "firewall", `sqlite3 hsp.db`, "unrecoverable", `[Unit]`, `ExecStart=`. `PLAN.md` and `VERIFY-ON-HARDWARE` are still checked in the README.
- E0.07 (README backup note) also changed: it asserted `sqlite3 hsp.db ".backup`, `HSP_SECRET_KEY` and "systemd" in the README. Those three move to the site test in 13b (backup page, systemd section). E0.07 now checks that the README links to the site, and keeps the no-Dockerfile check.
- This is an owner-approved content move, not a weakening, but only if 13b adds those checks for the site pages.

## Flags
- The README mentions `BR-1...BR-24`; that matches PLAN.md.
- The Pages and CI action versions are not part of 13a (CI already uses checkout@v5 and setup-go@v6).

# Phase 13b notes (2026-11-04)

## Done
- `site/`: `index.html`, `docs/index.html` (Introduction) and 10 docs pages (installation, running-as-a-service, configuration, first-login, router-setup, hotspot-login-pages, how-it-works, architecture, backup-and-upgrade, troubleshooting), `404.html`, `robots.txt`, `sitemap.xml`, `.nojekyll`, `assets/{css,js,img}`.
- Pages were assembled once from content fragments with a throwaway script that is NOT in the repo. The committed files are plain HTML to edit by hand. The sidebar and pager are repeated in each docs page; E13.14 keeps them equal. After changing a docs section, update `assets/js/search-index.js` (E13.15 fails if it is out of step).
- Real first-start output (home terminal and installation page) was captured by running the built binary in a temp dir. Checked by E13.12 against `internal/app`.
- `og-image.png` was drawn once with headless Chrome from `reference/og-image.html` (no Node, no Go drawing code).
- Tests E13.03-E13.17 in `e2e/phase13b_test.go`. E13.11 asserts every moved README check on the page that owns it (hard exit item closed). E13.13 compares the router commands, designs, hotspot files, reason codes and MESSAGES lines with `internal/routerpack` and `internal/domain`.
- Screenshots (headless Chrome, iframe for 390/360 widths) of home and docs at 390 and 1280 px looked right. Lighthouse was not run.

## Deviations from SITE.md
- `--subtle` (#80808c) added for readable low-emphasis text; `--faint` is decoration only (E13.05, E13.07).
- Docs search is a result list under the search box (from `search-index.js`, a JS module, not JSON), not a filter of the sidebar. The sidebar also lists the sections of the current page (scroll-spy).
- The search box, copy buttons and OS tabs appear only with JS; without it all three downloads are listed.
- `404.html` uses the base URL for assets, because it is served at any depth. So the base URL is not in one single place; E13.17 checks it is identical everywhere and the README gives one command to change it.
- Strict CSP meta tag on every page.

## Open questions
- Pages: the owner must set Pages source to "GitHub Actions" once.
- Router commands and the launchd example are untested on real hardware; the site says so (router page). Windows firewall, ufw and launchd commands are standard OS commands, not checked against the code.
- "Site title" setting is stored but shown nowhere; the docs say so.
- Lighthouse scores not measured.

