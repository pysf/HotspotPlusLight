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
