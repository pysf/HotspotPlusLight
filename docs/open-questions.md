# Open questions for the owner (running list)

Work continues on the stated default until you answer. Newest last.

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
