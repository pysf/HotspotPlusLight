# Phase 4 notes: accounting and usage

Built: `internal/domain/accounting.go` (pure BR-11: `CumulativeFromAcct` with Gigawords, `Counters.DeltaTo` with the restart rule, `ApplyAccounting` = PLAN 7.3 for Start/Interim/Stop), `internal/session` (`Service.Account`, one transaction per packet through the consumer-side `AcctTx`, `Prune`), `internal/store/accounting.go` (the SQL, `AcctTx`, `PruneSessions`), `internal/radius` `handleAcct` (parses attributes, replies only after `Account` returned), wiring in `internal/app` (`sessionRepo` adapter, pruning at start + every 24 h, `Options.AccountingBeforeCommit`).

E2E (`e2e/phase4_test.go`): E4.01-E4.17 plus `TestE3_12b_ConcurrentSessionsViaAccounting`; the seeded E3.12 is kept. All green. Harness: `fakenas.Acct/AcctPacket/Account/AcctExchange/AcctSilent`, `harness.Options.AccountingBeforeCommit`. No test is skipped. Unit tests: domain (delta, gigawords, lifecycle), session (fake repo with rollback), radius handler.

## How it works
- Identity key `(canonical username, Acct-Session-Id, packet source IP)`; the username is the member's stored spelling, so `ALICE` and `alice` are one session. Time is the server's clock (O19).
- Start reuses an open row that has `session_seconds = 0` (retransmission), else inserts. Interim/Stop update the open row, or insert one with `started_at = now - Acct-Session-Time`. Stop sets `ended_at = now` and `terminate_cause` (the attribute's name, e.g. `User-Request`).
- Usage is added in SQL (`used_x = used_x + delta`) inside the same transaction, so a renew between two packets is respected (E4.08).
- The Accounting-Response is written after the commit. DB errors and the `AccountingBeforeCommit` hook mean no reply (the router retransmits). E4.15 holds the transaction open and shows that no reply is seen and no data is visible until release.
- Request authenticators of accounting packets are verified by the layeh `PacketServer` before the handler runs (E4.12); wrong-secret and tampered packets are dropped, the library logs "packet validation failed".
- MAC (BR-12): `Calling-Station-Id` normalized by BR-3 (unparsable -> empty); the member's MAC is set only while empty.
- Pruning (O18): `settings.session_retention_days` read at each run; 0 keeps everything. Runs once in `App.Start` and then every 24 h (real timer, cut-off from the injected Clock). Test restarts the app with the data dir.

## Decisions to confirm
1. **Accounting-On/Off change nothing** (PLAN 7.3) although the E4.11 line in section 11 says "ends open sessions". I followed 7.3 and the owner's reboot decision; E4.11 asserts "acknowledged, nothing changes". See open question 5.
2. **Duplicate guard** for finished sessions (retransmitted Stop, delayed Interim): ignored when all three figures are <= the finished row's. This is an addition to PLAN 7.3 (without it a retransmitted Stop would double count). Question 6.
3. **Pruning also removes silent open rows** older than the retention. Question 7.
4. Out-of-order Interims (an older cumulative arriving later) are treated as a counter restart by the BR-11 rule and over-count; accepted (UDP on a LAN, same as legacy's deltas). A Start retransmission that arrives after an Interim would make a second open row; accepted.
5. Accounting continues to count for inactive/expired/unknown-plan members (only the user must exist).
6. `LatestSession` looks for an open row via the partial index, and for a finished one only when no open row exists (rare path: late duplicates, lost Start). That second query is not indexed (adding an index would be a new migration, and E0.09 counts migrations); fine for a 90-day table on a LAN, revisit if sessions grow large.
7. `Acct-Session-Time` overflow / bogus Gigawords saturate at int64 max instead of wrapping.
8. A Start/Interim/Stop without `Acct-Session-Id` is dropped with a WARN (no reply); without `User-Name` it is acknowledged as an unknown user.

## MONITOR / VERIFY-ON-HARDWARE
`MONITOR:` comments on `session.Service.Account` (no automatic disconnect on quota exhaustion, O3/O4; Accounting-On/Off ghosts). `VERIFY-ON-HARDWARE` on `CumulativeFromAcct` (Input = upload, when RouterOS sends Gigawords, PLAN 13.5/13.6). Not verified: whether RouterOS sends `Acct-Terminate-Cause`, a Start with non-zero session time, or `Calling-Station-Id` format (colon-separated upper case is assumed, normalized anyway).

## Questions added to docs/open-questions.md
5 (E4.11 vs 7.3), 6 (duplicate guard), 7 (pruning ghosts).

## Review fixes
- A Start on an open row that already counted time (router reboot reusing the id) closes the old row (`Lost-Service`, counters untouched, `AcctResult.Closed`) and creates a new one. Tests: domain `TestApplyAccountingStartClosesOpenRowWithTime`, `E4.09b`.
- The first prune runs inside the pruning goroutine, not before RADIUS serving; E4.16 polls for the expected row count.
- A present but malformed accounting attribute drops the packet (WARN, no reply); absent ones stay 0. Test: `TestAccountingMalformedAttributeDroppedWithoutReply`.
- An unparsable `Calling-Station-Id` logs a WARN (value truncated to 40 chars) and the packet is still accounted without a MAC.
- `ApplyAccounting` field copies are plain `if v != ""` assignments.

## Known edge cases (accepted, `MONITOR:` comments, open questions 8-10)
1. A reordered/delayed Interim (cur < prev) is treated as a counter restart: over-counts, rewinds the stored counter.
2. A late Interim/Stop with no open row and figures above the latest finished row creates a new row and double-bills the overlap (needed for E4.17).
3. `LatestSession`'s finished-row fallback is a full scan, only when there is no open row; an index migration is the fix if it ever matters.
