# Phase 5 notes: online sessions and kick

Built: real RADIUS Disconnect client (`internal/radius/disconnect.go`), `session.Service.{Disconnect,Kick,Online}`, store queries (`internal/store/sessions.go`), online page + polled fragment (`internal/web/admin/sessions.{go,templ}`), `POST /admin/members/{id}/disconnections` and a "Disconnect" button on the member page, fake router listener `e2e/fakenas/coa.go` (`ListenCoA`, replies ACK / NAK / silent, records requests and verifies their authenticator).

E2E (`e2e/phase5_test.go`): E5.01-E5.12 as in PLAN section 11, plus E5.03b (route guards: CSRF, 404, 405, 409), E5.13 (secret and port come from settings). All green, nothing skipped. Unit tests: domain (`FormatSize/Duration`, kick-ended accounting), session (`Online`, `Disconnect/Kick`). E5.06 takes 3 s (real timeout).

## How it works
- **Disconnector** (`radius.Disconnector`): reads secret + `coa_port` from settings at every call; sends code 40 to `session.nas_ip:coa_port` with `User-Name`, `Acct-Session-Id`, `Framed-IP-Address` and `Calling-Station-Id` (the last two only when known). Two attempts of 1.5 s with the same packet (so at most 3 s, 1 retry). Verified ACK = success; NAK = `ErrKickRefused` (final, no retry); no verifiable reply = `ErrKickTimeout`; missing router address / User-Name / Acct-Session-Id = `ErrKickIdentifiers` before anything is sent. `app.Options.Disconnector` still overrides it (Phase 2 tests keep their `FakeDisconnector`).
- **Marking ended:** `session.Service.Disconnect` wraps the sender: ACK => `UPDATE session SET ended_at, terminate_cause='Admin-Reset'` (by row id, only if still open). The member service now gets this service as its `Disconnector`, so deactivate, single delete and bulk delete end the sessions on ACK too (a batch delete in Phase 7 will go through the same member service). NAK/timeout leave the row alone.
- **Online = one definition:** `store.onlineSession` (`ended_at IS NULL AND last_update_at >= ActiveSince(now, interval)`) is used by the member list dot, `SelectionStats`, the concurrent-session count (BR-9) and the online page. E5.02 checks list and page agree at the 119 s / 121 s edge.
- **Online page:** `GET /admin/sessions` (full page) and `GET /admin/sessions/table` (fragment). The fragment's root `#online-table` carries `hx-get` (same page/size), `hx-trigger="every 10s"` and `hx-swap="outerHTML"`, so it keeps polling itself. Columns: username (link to member), IP, MAC, started, duration (now - started), download, upload (the router's cumulative values for this session, auto unit), Disconnect button (`ui.ConfirmAction`). Note "Updated by the router every N s" from `accounting_interval_seconds`. Paginated with the shared pager.
- **Responses:** kick success `200` + `HX-Redirect` (online page or member page); `422` missing identifiers; `502` NAK/no answer ("... the session was not changed."); `409` already ended; `404` unknown. `htmx-config` also swaps `502` (see Review fixes 6).
- **No automatic enforcement:** nothing disconnects on an Interim (E5.09; the existing `MONITOR:` comments stay).

## Change to Phase 4 rules (needed by kick)
After an ACK the router sends its own Stop with final totals. Treating the kicked row as finished would have created a second row and billed the whole session again. `domain.ApplyAccounting` now treats a row that is ended but whose status is not Stop (a kick-ended row) like an open one for Stop/Interim figures: same row, delta added once, `ended_at` and `Admin-Reset` kept, status becomes Stop (so a retransmitted Stop is ignored). A Start with the same id is a new session. Tests: domain `TestApplyAccountingStopCompletesKickedSession`, `E5.04`. Recorded in PLAN BR-18.

## VERIFY-ON-HARDWARE
Whether RouterOS accepts this attribute set and matches on it, the spelling of `Calling-Station-Id` (since 2026-11-02 the stored form is the RouterOS-style `AA:BB:CC:DD:EE:FF` and is sent as is), that incoming RADIUS is enabled on 3799 (PLAN 13), and whether a Disconnect-NAK carries `Error-Cause` (not read).

## Decisions to confirm
1. Kick-ended rows are completed by the router's Stop (above), question 11.
2. `Calling-Station-Id` sent as stored (RouterOS spelling), question 12.
3. Required identifiers for E5.10, question 13.
4. `502` for router failures, question 14.
5. Member-level Disconnect also kicks stale open rows, question 15.
6. (superseded by Review fixes 1) an ACK followed by a database failure is now a success.

## Not built
A per-member "Disconnect all" on the online page (the member page has it), batch delete kicks (Phase 7 reuses `member.Service.Delete`), router link status panel (Phase 8).

## Review fixes (2026-11-01)
1. **ACK then DB failure** (verified): `session.Service.Disconnect` now logs the `EndSession` error at ERROR and returns nil; the router's Stop closes the row. Test `TestDisconnectAckThenStoreFailureIsSuccess`.
2. **Serial Disconnects** (verified): `member.Service.disconnect` sends at most 8 concurrently (semaphore + WaitGroup, no new dependency); stale rows are still included (open-questions 15). Results are per-target slots, so counts are independent of the finishing order. A batch delete sends the late-appearing targets in one call. Test `TestKickSendsDisconnectsConcurrently` (24 sessions x 100 ms finish in well under 1 s).
3. **Row kick of a stale row**: kept (any open row, `ended_at IS NULL`); documented in PLAN BR-18.
4. **Disconnect-NAK Error-Cause 503** (verified): treated as success with an INFO log, session marked `Admin-Reset`; other NAKs stay `ErrKickRefused`. `NewDisconnector` now takes a logger. fakenas CoA gained `NAKNoSession`. E5.05b (503 closes the ghost) next to E5.05 (plain NAK stays final). `VERIFY-ON-HARDWARE`: that MikroTik sends Error-Cause.
5. **Kick-ended row overwritten by a reused id** (verified, real bug): `kickEnded(latest, r)` is now true only for a Stop whose figures are all >= the stored ones. Anything else follows the normal path (duplicate guard / new row). Domain test `TestApplyAccountingKickedRowIsStrict`, e2e `E5.04b`.
6. **Redirect lost page/per** (verified): the row button URL carries `page` and `per`; the success `HX-Redirect` returns to `/admin/sessions?page=..&per=..` (plain `/admin/sessions` when none are given). E5.03c. **Not done as asked:** removing `502` from the global htmx config. Verified in the vendored htmx source that `HX-Retarget`/`HX-Reswap` headers do not make htmx swap a response its `responseHandling` marks `swap:false`, so a server-only mechanism cannot work for 4xx/5xx. `502` stays in the config (the panel has no proxy, so only our own kick failures produce it); a narrower alternative would need a client-side `htmx:beforeSwap` script. The E5.12 selector for the row button changed from `hx-post$=` to `hx-post*=` because the URL now has a query string; the assertion is the same.
7. **Stale page after a kick** (verified): the kick button lives on the member page (no sessions table), so a partial failure or the "no open sessions" answer now appends the member's refreshed status line as an out-of-band swap (`MemberStatus(m, true)`, `#member-status`) next to the flash. Asserted in E5.07. A full success still redirects (reload).
8. `Online()` reading settings each poll: left as is.
