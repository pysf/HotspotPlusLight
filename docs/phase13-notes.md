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
