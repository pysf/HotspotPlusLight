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

# Step B: default admin/admin and the password page (2026-11-04)

## What changed
- Migration `0003_admin_must_change.sql`: `admin.must_change_password INTEGER NOT NULL DEFAULT 0`. Existing installs keep 0 and are not forced. A fresh install is seeded with 1.
- First start creates `admin` / `admin` (bcrypt) and prints a block that says so. No random password any more. `HSP_ADMIN_USERNAME` still sets the username.
- BR-20 is now: 10+ characters, at most 72 bytes, not the username (any case), not `admin`, not in a list of about 100 common passwords (`internal/domain/commonpasswords.go`, only entries of 10+ characters). No class rules. UI and CLI use the same function.
- Page `GET/PUT /admin/password` (`internal/web/admin/password.go`, form in `pages.templ`). Fields: current, new, confirm. Reveal (existing `data-reveal`) and Generate (new `static/js/password.js`, 36 lines, `crypto.getRandomValues`, 16 characters without look-alikes, fills new and confirm). Top bar link "Password".
- `requirePasswordChanged` middleware after `requireSession` and `requireCSRF`: while the flag is set, only `/admin/password`, `/admin/logout` and static files are open.
- `auth.Service.ChangePassword` checks the current password inside the same per-IP throttle as login (wrong = a failure, locked = 429), then the BR-20 rule and the confirmation. `store.ChangeAdminPassword` keeps the current session and deletes the others. The CLI path (`SetAdminPassword`) deletes all sessions and also clears the flag.
- `htmxConfig` now also swaps `429` responses (so the "too many attempts" message shows in the form).

## Tests
New: `e2e/phase11b_test.go` E11.20 to E11.26. Unit tests: `domain` BR-20 table, `auth.ChangePassword` (rules, sessions, throttle), `store.ChangeAdminPassword`.

Old tests changed (owner-approved rule change, not weakened):
| Test | Change |
|---|---|
| E0.01 | checks the admin/admin block, bcrypt("admin") and the flag instead of a 20-character printed password |
| E0.02 | second start prints no "first start" block |
| E0.09 | shipped migration count 3; the deliberately bad migration is now `0004_bad.sql` |
| E1.01 | logs in with admin/admin, lands on `/admin/password`; after the change the landing page is `/admin/members` |
| E1.02 | throttle uses the password `admin` |
| E1.05 | inverted: the password page exists and is the only one with password inputs (old `/admin/account*` routes stay 404) |
| E1.06 | also asserts a session existed before the CLI change |
| E1.14 | admin/admin is printed and works; second start prints no block |
| E1.15 | new rule (10 chars, 72 bytes, username, common); a lower-case phrase is accepted |
| E8.07b, E8.08 | no printed password any more; E8.08 checks the block is on stdout and not in the log |
| E8.09 | unchanged, README first-start text updated and still satisfies it |
Unit: `TestValidateAdminPassword` (domain) rewritten for the new rule.

Harness: `LoginAsAdmin` reads the flag from the DB. If it is set, it logs in with admin/admin and sets `harness.AdminPassword` through the page. Otherwise it logs in with `harness.AdminPassword`. `Login` accepts a landing on `/admin/members` or `/admin/password`. `AdminPasswordFromStdout` is removed.

## Decisions I made (please check)
- Non-GET requests while forced get `HX-Redirect` + 403 (like `requireSession` does with 401), not 303. GET requests get 303.
- After a successful change the toast text travels in a 30-second cookie `hsp_toast` that `toast.js` shows and clears, because the page is reloaded by `HX-Redirect` and an event would be lost.
- A wrong current password is a field error (422), not a toast. A locked client gets 429 and the form with a notice.
- The password fields are never filled again on a 422.
- Generate also switches the new-password field to visible so the admin can copy it.
- The list of common passwords is my own short list; extend it if you like.

## Known gaps (step B)
- Until the first password change, anyone who can reach port 8081 can log in with `admin` / `admin`. The README tells the owner to log in right after the first start.

## Step C: Windows (Phase 11c)
What was built:
- `store.DSN` builds the SQLite `file:` URI. It works the same on every OS. Drive letters become `file:///C:/...`, backslashes become slashes (only for paths that look like Windows paths), and spaces, `#`, `?`, `%` are escaped. Test E11.30.
- `gen-key` prints a new `HSP_SECRET_KEY`. Test E11.31.
- `--env-file <path>` for `serve` and `admin set-password`. Real env wins over the file. Test E11.32.
- `service install | uninstall | start | stop` (`service_windows.go`). On other systems they print an error (`service_other.go`).
- `app.Options.LogWriter` lets the service send logs to `hsp.log`.
- `make build-windows`, `make build-all`.
- Harness adds `.exe` on Windows. E8.08 kills the child on Windows instead of sending SIGTERM and skips the "shutdown complete" check there.
- `.github/workflows/ci.yml`: Linux (gofmt, vet, race tests) and Windows (vet, tests, and the service smoke test E11.33 with `HSP_TEST_WINDOWS_SERVICE=1`).

Decisions I made (please check):
- `install` restricts the folder to SYSTEM and Administrators with `icacls` (by SID, so it works in any Windows language), because `hsp.env` holds the secret key. It does this also when `hsp.env` already exists. If icacls fails, the install stops with an error.
- Recovery actions: restart after 5 s, 15 s, 60 s; the failure counter resets after one day. They also apply when the service exits with code 1 (not only on a crash).
- `install` fails if the service already exists. `uninstall` stops the service first and keeps the data. If the stop fails or takes more than 30 s, it returns the error and does not delete the service.
- If the app ends by itself under the service (for example a bad config), the service exits with code 1, so Windows restarts it.
- The service reports Running only after the listeners are bound (`app.Options.Ready`). If the app fails before that, it exits with code 1 and never reports Running.
- `service install` prints how to reset the admin password (`admin set-password --env-file <dir>\hsp.env`). The README says it too.
- On Windows, `store.DSN` also turns backslashes into slashes in relative paths.
- In the env file, an unquoted value loses a trailing ` #comment` (space, then `#`). `ab#cd` is kept. Quoted values are kept as they are.
- Graceful stop on Windows is checked by E11.33 ("shutdown complete" in hsp.log). E8.08 only kills the child there.
- The `env file` accepts one pair of quotes around a value, and a leading BOM.
- Backslashes are changed to slashes in the DSN only for Windows-looking paths (drive letter or `\\server`), so a Unix file name with a backslash still works.

Can only be checked on real Windows (CI runs E11.33 there):
- The whole service life cycle, the recovery actions, `icacls`, stop within 30 s.
- Windows firewall prompts for ports 8080, 8081, 1812, 1813.
- SQLite WAL files on Windows paths with spaces (the CI test uses a path with a space).

## Step D: capacity test (Phase 11d)
What was built:
- `perf/` (build tag `perf`, files `capacity_test.go` and `report.go`). Normal `go test ./...` does not build it.
- `make perf` (full), `make perf-quick` (500 and 1000 users, 30 s steps), `make perf-windows` (builds `bin/perf.test.exe` and `bin/hotspotpluslight.exe`; keep them in one folder).
- The server runs as a real child process, so CPU and memory are the server's own. The binary comes from `HSP_PERF_BIN`, or a file next to the test program, or a fresh `go build`.
- Load model, pass rules and notes are written into `docs/perf-results.md` by the test. The README section between `<!-- perf:start -->` and `<!-- perf:end -->` is rewritten too.
- Env: `HSP_PERF_STEPS` (comma list), `HSP_PERF_STEP_SECONDS` (default 120), `HSP_PERF_BIN`.

Decisions I made (please check):
- Members are made by SQL with one shared encrypted password (made with `internal/secret`). The admin password is also set by SQL.
- The load generator has 128 workers. Each has its own two UDP sockets and one request in flight, so identifiers never run out. Latency is measured from the planned send time (open loop), so a slow load generator or a slow server both show up. The test also fails a step if the load generator itself falls behind (see below).
- CPU % is the CPU time of the child over the step, as a share of one core (100 = one full core). I did not use `ps %cpu` because on macOS it is a decayed average. Linux reads `/proc`, macOS uses `ps -o time=,rss=`, Windows uses PowerShell (best effort, "n/a" if it fails).
- The login burst uses extra members (10% of N) who are not online, so the max-sessions rule does not reject them.
- The warm-up (Starts) is a quarter of the step time, at least 3 s.

### Review fixes (2026-11-04)
- Only the default full run (default steps and default step seconds) writes `README.md` and `docs/perf-results.md`. Other runs print only. The "what the test does" text is built from the steps and seconds really used. Files are written through a temp file and renamed; README first, then the docs file; the README markers must each appear exactly once.
- Open loop: every packet has a planned send time. Latency = reply time minus planned time. A step fails if the dispatch lag (real send minus planned send) is over 1 s, or if the steady packets/s is under 95% of the planned.
- Reply codes are checked: Access-Accept for logins, Accounting-Response for accounting.
- End-of-step checks in the database: session rows with `terminate_cause LIKE 'Server:%'` must equal the number of over-quota members; open sessions about N minus those; sum of used download bytes matches what was sent and answered (0.5%). Half of the over-quota members are over before their Start; the other half cross the limit on their first Interim, so the Interim kick path is exercised.
- The Start burst (warm-up) and the steady part are reported apart. Pass rules use the steady part. The warm-up is a quarter of the step time (at least 3 s) and long enough for at most 500 Starts per second (my decision, see below).
- `HSP_PERF_MIN_USERS=n`: the test fails if the max online users is below n. Without it the test never fails (it is a measurement tool).
- Sockets are opened before the workers start; an error stops the test with a clear message. If you see "too many open files", run `ulimit -n 4096` first (Linux, macOS).
- The server log of a failed step is copied to `bin/perf-last-fail.log` (git-ignored).
- "Online page" is now named "Online sessions page (/admin/sessions/)".

### The SQLite write contention (found by the test)
What the test showed at 20000 users: lost replies, `database is locked` in the log, accounting p99 over 600 ms, while the server CPU was low.

Cause: one `sql.DB` with no limit on connections. Hundreds of goroutines each opened a connection and ran `BEGIN IMMEDIATE`. They all fought for the one SQLite write lock inside SQLite's busy handler. That handler sleeps and polls, with growing sleeps, and a new arrival can take the lock before an old waiter wakes up. Some writers were starved until `busy_timeout` (5 s) and failed.

Measured before the fix (temporary instrumentation in `InAcctTx`, 20000 users, 60 s step, 38000 accounting transactions): waiting for `BeginTx` p50 1.0 ms, p95 449 ms, p99 3.1 s, max 5.0 s, and 158 `BeginTx` failures. The work inside the transaction (run + commit) was small: p50 0.67 ms, p99 2.6 ms, max 24 ms. So the time was lost waiting for the lock, not working.

Fix (`internal/store/store.go`): the same file is opened as two pools.
- Write pool: exactly one connection (`SetMaxOpenConns(1)`, `_txlock=immediate` stays). All transactions and all writes use it. Writers now wait in Go in a fair queue, not in the SQLite busy handler.
- Read pool: 8 connections, for read-only queries. WAL lets readers run while the writer works. The write pool is opened (and its WAL pragma runs) before the read pool opens.
- Reads that must agree with a write stay inside the write transaction (for example the reads in `InAcctTx` and `DeleteMembers`).
- Migrations, first boot and tests (`Store.DB()`) use the write pool. `Close` closes both.
- Rule for callers: never call a `Store` method that writes from inside a transaction callback. With one connection it would wait for itself forever. No current code does this (all tests pass).
- `synchronous` is NOT changed (still the default FULL). That is a durability trade-off for the owner. Each commit does an fsync, and the writer can do roughly 1000 transactions per second on this Mac, which is why a Start burst above that makes a queue.

### Review fixes for the two pools (2026-11-04)
- Deadlock audit. Checked every user of the write connection: `InAcctTx` (only caller: `session.Service.Accounting`, its callback uses only `tx`; the Enforcer is called after the commit), `DeleteMembers`, `CreateBatch`, `FirstBoot`, `DeletePlan`, `SetAdminPassword`, `ChangeAdminPassword`, `apply` (migrations), `Store.DB()` (only migrate tests), the e2e harness (own connection to the file, not the store) and `perf` (own connection). No transaction callback calls another Store method. `appliedChecksums` and `openSessions` close their rows before they return. The rule is now written as a comment on `InAcctTx`.
- The read pool has `_pragma=query_only(1)`, so a write on it fails (not `mode=ro`: WAL readers need to write the -shm file). Grep showed no write on the read handle. A unit test (`TestReadPoolIsQueryOnly`) checks it.
- Paired reads: the member list page (count and rows) and the status chips, and the delete confirmation (`SelectionStats`), are display only: they use the read pool, and a tiny mismatch is fine. `OpenSessions` (bulk kick, rename check) now uses the write connection, so a kick never misses a session that was already committed. `DeleteMembers` still reads its open sessions inside its own transaction.
- Admin session check no longer writes on every request. It reads on the read pool and slides the expiry (still 1 hour) only when the stored expiry is at least 60 seconds behind.
- `DeletePlan` reads the default flag and the member/batch counts in the same write transaction as the failed delete.
- If the second pool fails to open, the first is closed. `Close` closes the read pool, then the write pool.
- Perf test: `make perf` time is "about 25-30 minutes" in the Makefile and README. A full run with no passing step does not overwrite README or docs. The sampler goroutine and the sockets are cleaned up if `newWorkers` fails. `ps` time with days (`D-HH:MM:SS`) is parsed. The expected download bytes are summed by username (`u0`..`u{N-1}`), not by member id.

Before and after (this Mac: Apple M5, 10 cores; before = the earlier README, 120 s steps, with the old test; after = the full run now, new stricter test):

| Online users | Before | After |
|---|---|---|
| 10000 | pass (acct p99 2.3 ms) | pass (acct p99 3.6 ms) |
| 20000 | FAIL: 617 lost, acct p99 330 ms, database is locked | pass: 0 lost, acct p99 4.7 ms |
| 30000 | not reached | pass: 0 lost, acct p99 5.3 ms |
| 40000 | not reached | FAIL: 144 lost, the Start/steady load is more than the one writer and the load generator can do |
| Max online users | 10000 | 30000 |

Decisions I made (please check):
- Warm-up at most 500 Starts per second. At 1000 per second the single writer was at its limit and the burst alone made one reply later than 1 s. A real router rarely sends 500 logins per second. If you want the stampede case, change `maxStartRate` in `perf/capacity_test.go`.
- Dispatch lag rule uses the worst packet (max), as asked.
- The over-quota count is max(2, N/100) so both kinds (over at Start, over at first Interim) exist at small N.
- Read pool size 8. Not tuned.
