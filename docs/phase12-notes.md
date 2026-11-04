# Phase 12a notes (2026-11-04)

Step 12a of Phase 12: drop the page API and the public listener. Step 12b (new router pages) is not done here.

## What was removed
- `internal/web/pageapi` (the three `/api/v1` routes, CORS, the throttle) and the public listener on `:8080`.
- `HSP_HTTP_ADDR`. The variable is now ignored. Nothing listens on that address (E12.08).
- MAC auto-login (O8). Also `Evaluator.CredentialsByMAC`, `Evaluator.Account` and `store.ListActiveMembersByMAC`. RADIUS does not use them.
- The settings `public_base_url`, `mac_auto_login`, `after_login_url`: domain, form, store, settings page. Migration `0004_drop_page_api_settings.sql` drops the columns with `ALTER TABLE ... DROP COLUMN` (no index or constraint blocked it).
- Form helpers that only these fields used (`httpURL`, `publicURL`).
- The "public base URL not set" warning on `/admin/router`.

## What changed
- `/healthz` now lives on the admin listener (port 8081). No login. `Cache-Control: no-store`.
- RADIUS Reject: `Reply-Message` is only the code, for example `OUT_OF_BULK`. A debug log line keeps code and English text.
- Setup script: no walled-garden lines, no `html-directory` line, no `public_base_url`. It now ends with a note: the downloaded `hotspot` folder replaces the default one (`flash/hotspot` on flash routers).
- Old router pack (kept until 12b): `login.html` no longer calls the server. It only shows `$(error)` (the code) as translated text. `HSP_API_BASE`, `fetch`, the MAC step and the account check are gone.
- README, `PLAN.md` (D10, D20, D23, O8, O20, P1-P3, section 8.1, 9.2, 9.4, 13), `CLAUDE.md`.
- Windows service test E11.33 and the perf harness now use the admin port.

## Tests removed with the feature (owner decision 2026-11-04)
- E6.01 account ok, E6.02 reasons are 200, E6.03 uniform 401, E6.04 invalid username, E6.05 CORS, E6.06 figures match admin, E6.07 device credentials (MAC auto-login), E6.08 config, E6.09 throttle, E6.10 no credentials in logs (page API side), E6.19 problem+json and headers.
- E6.12 starter page script (it was a skipped stub for the page script).
- E1.16 listener separation. E12.08 replaces it.
- E8.11 `public_base_url` host check.
- Unit tests: all of `internal/web/pageapi/pageapi_test.go`, `TestAccount`, `TestCredentialsByMAC`, `TestCredentialsByMACSeveralMembers` in `internal/auth`.

## Tests changed
- E0.03 (healthz on admin), E0.05 (no pageapi package rule), E0.09 (4 migrations, bad migration is now `0005_bad.sql`).
- E1.10 settings form (no three fields).
- Every reject test (E2.13, E3.02 to E3.17, E4.13, E4.14, E3.12b, E5.09, E7.08, E7.09, E7.14, E0.04, E1.18, E11.x): the Reply-Message is exactly the code.
- E6.15, E6.17, E6.20: no API base, no public base URL warning.
- E8.03, E8.03b (script without walled garden; RADIUS address is always the `<SERVER-IP>` placeholder), E8.04, E8.05, E8.07, E8.08, E8.09 (README strings).
- E11.33 uses `127.0.0.1:8081/healthz`.
- Unit tests: `TestReplyMessage`, config defaults, settings form, store settings, routerpack tests.

## Tests added
- E12.07 Reply-Message is exactly the code for every reason sent over RADIUS.
- E12.08 one listener: `HSP_HTTP_ADDR` ignored, `/healthz` without login, `/api/v1/*` is 404.
- E12.09 migration 0004 drops the columns and keeps the data.
- E12.10 setup script.

## Decisions I made (please check)
- `HSP_HTTP_ADDR` is ignored silently (no startup error), so old env files keep working.
- Without `public_base_url` the setup script cannot know the server IP. The `/radius add` line is always printed commented out with `<SERVER-IP>`, and the README says to replace it. Option for later: take the IP from the address the admin uses in the browser.
- Owner decision 2026-11-04: the server IP stays a placeholder (`<SERVER-IP>`). Revisit later.
- The old theme tests (38 themes, `themes_table_test.go`) stay. Phase 12b removes them with the old pack.
- `Reason.ReplyMessage()` keeps its name but returns only the code.

## Left for 12b
- Replace `internal/routerpack` layout, pages and the 38 themes; new zips; the admin Router page cards and preview routes.
- `login.html` of the old pack still has the JS error mapping. 12b swaps it for the `$(if error == "CODE")` block.
- VERIFY-ON-HARDWARE: `$(error)` carries the bare code.

---

# Phase 12b notes (2026-11-04)

Step 12b of Phase 12: the new router pages, the 8 zips and the admin Router page.

## What was built
- `internal/routerpack` now has `mikrotik/` and `src/`.
  - `mikrotik/`: a byte copy of `reference/hotspot/` (go:embed cannot reach `../../reference`). A unit test and E12.00 keep it equal.
  - `src/hotspot/`: the pages (`login`, `alogin`, `status`, `logout`, `error`), the helpers (`redirect`, `rlogin`, `radvert`, `api.json`, `errors.txt`), `js/busy.js`, `xml/*` and `favicon.ico`.
  - `src/css/layout.css` and `src/css/themes/<key>.css`: one layout, one token block per theme. Adding a theme = a new block file + one line in `routerpack.Themes`.
  - The generator replaces `@THEME@` (body `data-theme`) and `@MESSAGES@` (the code-to-text block, built from `domain.AllReasons()`), and joins layout + one theme block into `css/hotspot.css`.
- `Pack.Zip(key)`, `Pack.Files(key)`, `Pack.Preview(key, page, errCode)`, `Pack.PreviewAsset`. Zips are sorted and have no timestamps.
- Admin Router page: the old theme gallery is gone. The page has the help text, 8 cards (small iframe preview, Download, Open preview), the setup script and the link status panel.
  - `GET /admin/router/pages/{key}.zip` (`application/zip`, `hotspot-{key}.zip`).
  - `GET /admin/router/pages/{key}/preview?page=login|alogin|status|logout|error&error=CODE`.
  - `GET /admin/router/pages/{key}/css|img/*` (the files the preview links to).
- New CSS component `c-pack-frame` (in `gallery.css`); the old `c-swatch`, `c-theme-card`, `c-preview` are removed.

## Preview safety
The preview is a normal admin page with its own header: `Content-Security-Policy: default-src 'none'; style-src 'self'; img-src 'self' data:; form-action 'none'; base-uri 'none'`. No script can run, no form can post, nothing is fetched from outside. I did not use an iframe sandbox: the CSP is enough, and a sandboxed (opaque) frame would make the stylesheet request fragile. The iframe on the card is not focusable and ignores the mouse (`pointer-events: none`).

## Deviations from `docs/HOTSPOT-LOGIN.md` (the brief assumes our server hosts the page)
- **No settings section, no logo upload, no theme in settings.** The router hosts static pages. The admin edits heading, subtitle and logo by hand (comments in `login.html` show where). Owner decision.
- **8 static zips** instead of one page with a chosen theme.
- No `?theme=` dev preview. The admin Router page previews every theme.
- Heading default is "Welcome" (not "Welcome to {business name}"): there is no business name feed.
- The form posts to `$(link-login-only)` with the hidden `dst` and `popup=false` fields (brief section 3: keep the hotspot contract).
- Page bottom padding is `max(24px, env(safe-area-inset-bottom))`. The reference had 92px for its preview bar, which is not shipped.
- Logo swap is `img/logo.png` (no logo file is shipped).
- The reference `.err[hidden]` rule is not needed: the router prints the error block only when `$(error)` is set.
- Added to the layout CSS (not in the reference, needed for the other pages): `.btn` also works as a link (`display:flex`), `.btn-quiet`, `.stats`, `.note`. They use only tokens, no new colors. The row border uses `color-mix` of `--muted`.
- Fonts: the reference lists Inter first, but we ship no font file. The system stack is used.

## Contrast (4.5:1, checked by `TestThemeContrast` in `internal/routerpack`)
Checked: text, muted text and error on the card; text on the input; button label on every gradient stop. Translucent cards are laid over the plain `--bg` color (an approximation; the real background has gradients).
Two tokens were adjusted. Everything else is exactly as in the reference.
- `midnight`: `--btn-bg` `#4c8dff` to `#2f6fe0`. White label 3.2 to 4.7.
- `aurora`: top of the button gradient `#6d6df6` to `#6060ee`. White label 4.1 to 4.8 (bottom stop `#5252e8` was 5.6).
Not checked: the hover state (`brightness(1.08)` lowers a white label a little), placeholder text (none used), the focus ring (non-text, 3:1 rule).

## Page sizes (themed zip, raw bytes of html + css + js + helpers, without `xml/` and `favicon.ico`)
See the final report of the step for the exact numbers; E12.01 fails over 30 KB.

## Licensing
`xml/*` (WISPr files and the schema), `favicon.ico` come from the MikroTik default folder. `redirect.html`, `rlogin.html`, `radvert.html`, `api.json` and `errors.txt` are written by us in the same router syntax; `redirect.html`, `rlogin.html` and `api.json` stay functionally the same as MikroTik's because the router needs exactly that. The `mikrotik` download is the unchanged MikroTik folder. Please check this is fine for you.

## Tests removed with the old pack (not weakened)
- `e2e/phase6_test.go` (whole file): E6.14 theme registry (38 themes), E6.15 theme downloads (also E6.11 lint and E6.18 disconnected hint), E6.16 default and unknown theme, E6.17 gallery, E6.20 Router page entry links, E6.12 (skipped stub). The fixture `p6` went with them.
- `e2e/themes_table_test.go` (the 38-theme table).
- `internal/routerpack/routerpack_test.go` (old): `TestRegistry`, `TestLoginHTMLEscapes`, the username preset test, the old zip and dictionary checks. Replaced by new unit tests (embedded copy equals reference, theme source list, deterministic zip, contrast, template preview).
- The reason-codes dictionary checks (E6.11/E6.18) no longer apply: the page has no JavaScript dictionary. E12.03 covers every domain reject code in the MESSAGES block instead.

## Tests changed
- E8.03: expects "extracted hotspot folder" (the script text) instead of "files.zip".
- E8.04: rewritten for the 8 downloads (zip under `hotspot/`, no template tokens, no `plain-passwd`, no password in a URL). File lists are E12.01.
- E8.05: the `/admin/router/themes` page left the list of pages that must not show the secret (the Router page itself shows it only in the script, checked separately).
- E8.09: README must mention "hotspot folder" instead of "files.zip".
- E10 (`adminPages`): the theme pages are no longer visited.

## Tests added
- E12.00 mikrotik zip == reference copy. E12.01 file lists, no md5.js, no outside address, size. E12.02 RouterOS syntax and balanced `$(if)`. E12.03 MESSAGES block and the preview of every code. E12.04 one theme block per CSS. E12.05 no inline code, works without JS. E12.06 admin Router page, downloads, previews, 404s, old routes gone.
- Unit: `TestMikrotikCopyEqualsReference`, `TestThemeSource`, `TestZipIsDeterministic`, `TestThemeContrast`, `TestRenderTemplate`.

## Decisions I made (please check)
- `TOO_MANY_ATTEMPTS` and `GENERAL_ERROR` were removed from `domain` (nothing sent them), so they are not in the MESSAGES block.
- The MikroTik `alogin` page goes to the status page (owner choice), not the MikroTik default.
- There is no CHAP branch. The setup script sets `login-by=http-pap`. If the admin turns on http-chap, logins fail. The Router page help says so.
- Data left is shown as raw bytes ("Data left (bytes)"), because `$(remain-bytes-total)` has no "nice" form.
- The status page does not show IP address or bytes up/down (not in the owner's list).
- Only `login.html` has the logo and the "edit here" comments; the other pages have a heading only, so the logo is edited in one place.
- `alogin.html` waits 1 second, then goes to `$(link-status)`.

## Needs hardware (PLAN section 13, items 9 to 14)
- `$(if error == "CODE")` matches the RADIUS Reply-Message; the fallback shows other router messages.
- `$(uptime)`, `$(session-time-left)`, `$(remain-bytes-total)` (unit and when it is set), `$(link-orig)` on the status page.
- `alogin.html` with `popup=false`, `api.json`, `errors.txt`.
- The look in the iOS and Android captive-portal mini browsers (`backdrop-filter`, `color-mix`, `dvh`).
