# Phase 11a notes: enforcement in two layers

Owner decision, 2026-11-04. It replaces O3 and O4 (the "legacy-faithful gap").

## What was built
**Layer 1: exact limits in the Access-Accept** (`domain.GrantFor`, `radius.Server.reply`)
- Data limited: `Mikrotik-Xmit-Limit` and `Mikrotik-Recv-Limit`, both = bytes left. From 4 GiB up, the high 32 bits go into the two `-Gigawords` attributes (ids 14 and 15). No Gigawords attribute below 4 GiB.
- Data unlimited: no data-limit attribute.
- `Session-Timeout` = the smallest of time left, seconds to `expires_at`, seconds to the window end. At least 1. At most 4294967295. Always sent. Auto-renew plans are cut at the window end too.
- Removed: `UnlimitedXmitLimit`, `UnlimitedSessionTimeout`, the 32-bit clamp, `XmitClamped`.
- `GrantFor` now takes the member state and `now`: `GrantFor(state, plan, interval, now)`.

**Layer 2: the server disconnects** (`domain.ShouldEnd`, `session.Enforcer`)
- `ShouldEnd` = `Evaluate` rejects, or `Evaluate` says `Renew`. One rule for login and for enforcement.
- `Enforcer.Check` is called after the accounting commit for a Start or Interim of an open session. Never for a Stop. It never blocks: buffered queue of 1024, drop and WARN when full.
- 8 workers. Each loads a fresh member and plan, runs `ShouldEnd`, and sends the Disconnect through `Service.DisconnectWithCause`. On ACK the row ends with `Server: <CODE>`. The admin Kick still writes `Admin-Reset`.
- After a successful Disconnect the server keeps a short "recently ended" mark (2 x accounting interval): a late Interim before the router's Stop does not cause a second Disconnect. Stop sets a flag first, so a late Check after shutdown does nothing (no panic).
- No floods: one job per session at a time. After a failed try, no new try within 2 x accounting interval (injected clock). After 3 failed tries it gives up with a WARN. A record of failures is forgotten after one hour.
- The Enforcer starts in `App.Start` and stops in `App.Shutdown` after RADIUS (it waits for running workers, then the DB closes).
- New store method `OpenSessionTarget` (finds the open row by username, Acct-Session-Id, NAS IP).
- Test seam: `App.EnforcementIdle()` (no check queued or running). The e2e tests use it to be deterministic.

**Router script:** the `/radius incoming` comment now says "Disconnect button and automatic disconnects" and notes that RouterOS listens on 1700 by default; we set the port explicitly.

## Tests
New: `e2e/phase11_test.go` E11.01 to E11.12 (plus E11.09b, a silent router does not delay the Accounting-Response). Unit tests: `GrantFor`, `SplitBytes`, `ShouldEnd`, `session.Enforcer` (retry limit, under quota, full queue).

Old tests whose expectation changed (owner decision, not weakened):
| Test | Old | New |
|---|---|---|
| E3.07 | unlimited data gave Xmit-Limit 3,900,000,000 | no data-limit attribute |
| E3.09 | no time limit gave 172800; not capped by the window | smallest of time left and window end |
| E3.11 | above 4 GiB the value was clamped and logged | Recv/Xmit plus Gigawords, no clamp |
| E4.13 | no Disconnect after quota runs out | exactly one Disconnect, after the Interim that crosses the quota |
| E5.09 | no Disconnect with quota used up | no Disconnect while under quota (over quota is E11.04) |

One fixture change: E9.03 used the default 10 GB plan with 12 GB of traffic. The server now disconnects that member after the first Interim, which would change what the test sees. It now uses an unlimited-data plan. The test still checks the same thing (Gigawords delta in the day row).

## Decisions I made (please check)
- The reason for a window end on an auto-renew plan is `NO_ACTIVE_SUBSCRIPTION` (so the cause is `Server: NO_ACTIVE_SUBSCRIPTION`).
- Recv and Xmit get the same value (bytes left). The router may count one direction each, so each direction can use the full allowance. The server checks the sum on every Interim.
- Dedupe state is in memory only. A restart forgets failed tries.
- A Disconnect that returns "session not found" (NAK 503) counts as success, as for the admin kick.

## Known gaps
- Over-grant window: Recv and Xmit each get the full bytes left. If the router counts the directions separately, a member can move up to 2x the bytes left before layer 2 catches it at the next Interim (PLAN section 13, item 2).
- A late Interim after a kick: the kicked row is ended. If the Interim has figures above the stored ones, `ApplyAccounting` treats it like a lost-Start case and may create a new row (see the MONITOR note in `domain/accounting.go`). Only the router's final Stop of a kicked session completes the same row (`kickEnded`). The "recently ended" mark keeps the Enforcer from sending a second Disconnect in that time, but such a new row can appear until the router stops sending.
- Up to one accounting interval of overshoot (default 60 s) if the router ignores layer 1.
- The Disconnect is best effort (the router must have `/radius incoming` on our port).
- Hardware items are in PLAN section 13: Recv/Xmit direction, Gigawords limits, Disconnect on a Hotspot user.
