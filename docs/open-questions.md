# Open questions for the owner (running list)

Work continues on the stated default until you answer. Newest last.

## Decided by the owner (2026-11-02)
- **Rows 1-29: all working defaults accepted**, except 12 (still open) and where noted below.
- **Row 5:** PLAN 7.3 is right: Accounting-On/Off are acknowledged and change nothing.
- **Row 12 (MAC spelling in Disconnect):** still open until tested on the router. Owner direction: store the MAC in RouterOS style (`AA:BB:CC:DD:EE:FF`) so the Disconnect sends exactly what the router sent us (change queued; BR-3 canonical form).
- **Row 23:** English only, nothing Persian: the sample Persian translation table and Persian labels are removed from the starter page (the code-keyed translation mechanism stays, English only by default).


| # | Phase | Question | Working default |
|---|---|---|---|
| 1 | 3 | Member passwords may not contain control characters (tab etc.); batch/generated passwords are digits. OK? | Forbidden (BR-14a) so the wrong-secret heuristic can never drop a real password |
| 2 | 3 | Keep the `1k` floor for speeds that round to 0 kbps (legacy sent `0k`, which RouterOS reads as unlimited)? | Keep `1k` |
| 3 | 3 | Build the bad-secret counters on the router link status panel (PLAN §8.2)? Today only a WARN log line exists | Not built |
| 4 | 3 | The concurrent-session check is not atomic with accept (two simultaneous logins at `max_sessions=1` can both pass) — legacy-faithful, `MONITOR:` comment | Left as legacy |
| 5 | 4 | PLAN section 11 E4.11 says "Accounting-On/Off ends open sessions", but PLAN 7.3 and 13.8 say they change nothing (reboot handling declined, 2026-11-01). Which one? | 7.3: acknowledge, change nothing (`MONITOR:` comment); E4.11 asserts exactly that |
| 6 | 4 | Retransmitted Stop / delayed Interim after a Stop: PLAN 7.3 says an Interim with no open row creates one, which would count a finished session's totals twice. | A packet whose figures are all <= the latest finished row of the same key is ignored (domain.ApplyAccounting); anything larger starts a new row |
| 7 | 4 | Pruning removes open sessions silent for longer than the retention as well as finished ones older than it (ghosts whose Stop never came). OK? | Yes: `COALESCE(ended_at, last_update_at) < cutoff` |
| 8 | 4 | A reordered/delayed Interim (cur < prev) is treated as a counter restart (E4.04), so it over-counts and rewinds the stored counter. | Accepted (LAN UDP); `MONITOR:` in `fieldDelta` |
| 9 | 4 | A late Interim/Stop with no open row and figures above the latest finished row creates a new row and double-bills the overlap (needed by E4.17). | Accepted; `MONITOR:` in `ApplyAccounting` |
| 10 | 4 | `LatestSession`'s finished-row fallback has no usable index; adding one needs a migration (E0.09 counts them). | No index; fine for 90 days on a LAN; `MONITOR:` in the store |
| 11 | 5 | After an ACKed kick the row is ended (`Admin-Reset`), but the router then sends its own Stop with the final totals. Without a rule that Stop would create a second row and bill the whole session again. | A row ended by a kick is completed ONLY by a Stop whose figures are all >= the stored ones (same row, usage counted once, `Admin-Reset` kept); an Interim or a lower Stop takes the normal path (duplicate guard / new row); a Start with the same id after a kick is a new session (`domain.ApplyAccounting`, BR-18) |
| 12 | 5 | `Calling-Station-Id` in the Disconnect-Request: BR-3 stores the MAC as 12 lower-case hex digits, but RouterOS writes `AA:BB:CC:DD:EE:FF`. Which spelling does the router match? | Send the router's spelling (upper case, colons; `domain.RouterMAC`), `VERIFY-ON-HARDWARE` (PLAN 13) |
| 13 | 5 | "Missing identifiers" (E5.10): which are required? PLAN says "those known". | Required: router address, `User-Name`, `Acct-Session-Id` (without them there is nothing to address); `Framed-IP-Address` and `Calling-Station-Id` are sent when known. Clear 422 error, nothing sent |
| 14 | 5 | A failed kick (NAK / no answer) answers `502`, and htmx is configured to swap 502 like 422/409 so the message shows in `#flash` (htmx ignores `HX-Reswap` for responses its config marks as not swappable). The panel has no proxy, so only our own kick failures produce 502. A narrower client-side `htmx:beforeSwap` script is possible. | Kept as is; the session stays as it was |
| 15 | 5 | Member-level "Disconnect" kicks every open row of the member, including stale ones the online page hides (a way to clear ghost sessions after a router reboot, Q5). The online page only has per-row buttons. | Yes, all open rows |
| 16 | 6 | The router setup script (secret, ports, walled garden) is Phase 8 (E8.03) in PLAN 11; `/admin/router` today has only the download and the theme gallery. Build the script in Phase 6 instead? | Phase 8, as in the plan |
| 17 | 6 | `account` for a lapsed window on an auto-resubscribe plan: the GET renews nothing but reports the renewed window/zero counters with `loginAllowed:true` (what the next login will see). Alternative: report the stored figures. | Renewed view |
| 18 | 6 | With unlimited data/time the allowed/remaining figures are `null` (not 0). | `null` |
| 19 | 6 | MAC auto-login with several members on one MAC: among the active ones, the newest whose login would be allowed; if none is allowed, the newest active one (so the page shows the real reject reason). Legacy used `findOne`; an expired old member would otherwise block a valid new one (review fix, 2026-11-02). | Newest allowed, else newest active |
| 20 | 6 | Router page set: `login, alogin, status, logout, error, redirect, rlogin`. Legacy `rstatus`/`fstatus` (JSON for the old SPA) are not carried over. The captive-check nudge runs in `alogin.html` (the login page is left by the native form submit). | As described |
| 21 | 6 | One shared `logo.png` instead of one per category (the six legacy logos are byte-identical). | Shared |
| 22 | 6 | Page-API throttle: 10 failures per source IP per minute lock that IP for a minute; a success does not reset the count; an unknown MAC does NOT count (new devices are normal; review fix 2026-11-02); check and record are atomic per IP. Clients behind one NAT share the count (`MONITOR:`; whether the router NATs hotspot clients towards the server is `VERIFY-ON-HARDWARE`). | As described |
| 23 | 6 | New catalog code `TOO_MANY_ATTEMPTS` (429), added to PLAN 7.6 so the page can translate it. Persian texts for `EXPIRED`, `UNSUPPORTED_AUTH_METHOD`, `TOO_MANY_ATTEMPTS` and the page labels are my own wording (legacy had none); please have a native speaker check the sample table. | Keep |
| 24 | 6 | E6.12 (headless browser) is skipped: no browser tooling in the repo. The page script was exercised by hand with a Node stub (error mapping, fallback, native submit, MAC auto-login, preview mode). | Skipped, optional per PLAN 10 |
| 25 | 6 | `GET /devices/{mac}/credentials` returns the plaintext password with no authentication and CORS `*`. With O8 switched on, anyone on the LAN who knows or spoofs a registered MAC (or any web page running in such a client's browser) can read that member's credentials; a 404 vs 200 also reveals which MACs are registered. | Accepted by O8: the setting is OFF by default and the MAC is the only secret; `MONITOR:` in the handler |
| 26 | 6 | `account` has no per-username limit: a 6-digit voucher password (O14) can be guessed at 10 tries/min per source IP; the throttle is in memory and a restart forgives. The RADIUS edge has none either. | Accepted (LAN, `MONITOR:`) |
| 27 | 6 | `GET /config` is not throttled (a cheap settings read, no secrets). | Accepted |
| 28 | 6 | `account` is a prediction, not a promise: `loginAllowed:true` for a lapsed auto-resubscribe member can still be rejected by the real RADIUS login (lost renewal race, concurrent-session check 9 which `account` skips). | Accepted, documented in the handler |
| 29 | 6 | The admin theme preview runs the generated page on the admin origin. It is served with `Content-Security-Policy: sandbox allow-scripts; default-src 'none'; ... connect-src 'none'`, built without the API base, and shown in an `<iframe sandbox="allow-scripts">` (opaque origin, no forms, no network). | Sandboxed |
