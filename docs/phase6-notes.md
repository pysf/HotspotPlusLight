# Phase 6 notes: router page pack and page API

Built: the public JSON API (`internal/web/pageapi`), the starter-pack generator (`internal/routerpack`), the admin router pages (`internal/web/admin/router.{go,templ}`), `auth.Evaluator.Account` and `CredentialsByMAC`.

E2E (`e2e/phase6_test.go`, `e2e/themes_table_test.go` = the PLAN 16 catalog copied as expected data): E6.01-E6.11, E6.14-E6.19 plus E6.20 (entry page). E6.12 is skipped (optional, no headless browser). Unit tests: `routerpack`, `pageapi` (per-IP throttle, Basic parsing), `auth` (Account, CredentialsByMAC). The per-IP part of E6.09 is skipped on macOS (127.0.0.2 cannot be bound); the unit test covers it everywhere.

## How it works
- **Public listener** is a chi router: `/healthz`, `/api/v1/{config,account,devices/{mac}/credentials}`. CORS middleware wraps everything (also 404/405); unknown routes and wrong verbs are problem+json (`NOT_FOUND`, `METHOD_NOT_ALLOWED`). `account` and `credentials` are `no-store`.
- **One login implementation.** `Evaluator.Login` and `Evaluator.Account` share `authenticate` (check 1, constant-time, dummy compare for unknown users) and `planOf` (checks 3-4); `domain.Evaluate`/`QuotaOf` give checks 2, 5-8 and the figures, so the page, the RADIUS edge and the admin list cannot disagree. `Account` never renews and never counts sessions.
- **Throttle** reuses the admin login throttle (now exported as `auth.Throttle`): 10 failures/IP/minute. Checked before any work, so a locked client gets 429 even with the right password.
- **Pack.** `routerpack.New()` reads the 38 `theme.json` manifests (category dir + style dir must match the manifest; background must be non-empty) from the embedded tree. `login.html.tmpl` is rendered with `text/template` and two explicit helpers (`js` = JSON string literal, `html` = escape), so the API base is baked as `HSP_API_BASE = "<public_base_url>"` and the English dictionary is generated from `domain.AllReasons()` (one catalog). The page ships English only; `HSP_LANG` and a comment show how to add a language by hand (the Persian sample was removed 2026-11-02). The zip has everything under `hotspot/`.
- **Admin.** `/admin/router` (entry), `/themes` (gallery, 6 categories, 38 cards), `/themes/{id}` (detail with iframe preview and download), `/themes/{id}/{login.html,background.jpg,logo.png}` (preview assets; relative URLs of the generated page resolve to them), `/files.zip?theme=`. All behind the normal session; none exist on the public listener (E6.19). New "Router" nav item.
- **Starter page script** (plain JS, no build): reads router values from DOM elements (`$(error)` in a hidden `<p>`, `$(mac)` in a hidden input) instead of JS string literals, so a quote in a router value cannot break the script; unsubstituted `$(` values count as empty, so the admin preview works. Flow: config, router error mapping (`CODE: text`), MAC auto-login, `account` pre-check with Basic header, native submit to `$(link-login-only)` with `dst` = after-login URL or original URL. The button id is `login-button` (an element named `submit` would shadow `form.submit()`).

## VERIFY-ON-HARDWARE (marked in the HTML comments too)
RouterOS variable names (`$(link-login-only)`, `$(link-orig)`, `$(link-orig-esc)`, `$(link-redirect)`, `$(link-login)`, `$(link-logout)`, `$(error)`, `$(mac)`, `$(username)`, `$(ip)`, `$(uptime)`, `$(bytes-in-nice)`/`$(bytes-out-nice)` direction, `$(session-time-left)` and the `$(if x != "")` syntax), the form fields `username, password, dst, popup`, that `alogin.html`/`rlogin.html`/`error.html` are served as assumed, that a phone's captive-portal browser runs the script and allows the cross-origin calls, and that the walled garden lets the page reach `public_base_url` (PLAN 13.9). `login-by=http-pap` belongs to the Phase 8 setup script.

## Decisions to confirm
Open-questions 16-24: setup script deferred to Phase 8; renewed view in `account`; `null` for unlimited figures; MAC lookup picks lowest id; page set and nudge location; shared logo; throttle details; `TOO_MANY_ATTEMPTS` (the Persian wording was removed later); E6.12 skipped.

## Not built
Router setup script and router link status panel (Phase 8), a headless-browser test (E6.12), any page for balance/status inside the starter pack (T2).

## Review fixes (2026-11-02)
Verified against the code, each with a test:
1. **Unknown-MAC lookups no longer count as throttle failures** (real: 10 new devices locked everybody out). Only 401s on `/account` count. Tests: `TestUnknownMACDoesNotThrottle`, E6.09 subtest "MAC lookups that find nothing do not count". NAT note kept, plus `VERIFY-ON-HARDWARE` for router NAT towards the server.
3. **MAC lookup with several members** (real): store `ListActiveMembersByMAC` (active only, newest first); `CredentialsByMAC` returns the newest whose `domain.Evaluate` allows, else the newest active. Tests: `TestCredentialsByMACSeveralMembers`, E6.07 additions. PLAN 8.1 and open-questions 19 updated.
4. **Theme preview isolation** (real): CSP `sandbox allow-scripts; default-src 'none'; style-src/script-src 'unsafe-inline'; img-src 'self' data:; connect-src 'none'`, iframe `sandbox="allow-scripts"`, preview built with an empty `public_base_url` (the page script makes no API calls when `HSP_API_BASE` is empty). Test: E6.17 checks header, iframe attribute and empty API base.
7. **Atomic page-API throttle** (real, reproduced red): `account` takes `Throttle.Lock(ip)` (the lock the admin login already uses) around check, verify and record. Test `TestThrottleIsAtomic` (30 parallel wrong passwords: exactly 10 x 401, 20 x 429).
9. **`$(username)` through `routerValue`** (real): the input carries `data-router-value="$(username)"` and the script presets the value only when it is not an unsubstituted router variable. Test `TestUsernamePresetUsesRouterValue`.

Documented only (open-questions 25-28, `MONITOR:` comments in `pageapi.go`): 2 plaintext credentials with CORS `*` under O8; 5 no per-username limit, 6-digit passwords, in-memory throttle; 6 `/config` unthrottled; 8 `account` is a prediction.

Lint hints: `rows.Err()` in `e2e/phase4_test.go`; `for range n` in `e2e/phase6_test.go`.

