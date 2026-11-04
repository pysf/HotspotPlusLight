# Code review findings (2026-11-04)

A full code review was run in six pieces (`/code-review high`, logic only, no tests). Each piece was a Sonnet agent. This file lists everything they reported, to fix later.

**Status of every item: not fixed.**

**Trust level.** Most items are the reviewers' claims. Items marked **confirmed** were re-read in the code by an agent. Items marked **claim** were not. Check each item in the code before you fix it. Drop it if it is not real. Write a failing test first (CLAUDE.md rule 1).

**Plan.** Group A first, then group B, then group C only after the owner decides. Each step is: check in code, failing test, fix, review, commit.

Pieces: 1 domain + auth, 2 radius + linkstatus, 3 session + usage + member + plan + voucher + settings, 4 store, 5 app + config + secret + clock + cmd, 6 web + routerpack.

---

## Group A: real data or login problems

### A1. A repeated Start packet counts data twice (claim)
- **Result: not a bug, because** a Start carries zero counters, so a late duplicate looks exactly like a reboot Start; only a time guess could tell them apart. A real retransmit comes seconds after the first Start, before any Interim (the interval is minutes). In that window the open row still has 0 seconds, and `ApplyAccounting` already reuses it (E4.03, domain test). No code change.
- **Where:** `internal/domain/accounting.go:132` (piece 1).
- **What happens:** the router sends a Start again when our reply was lost.
- **Bug:** the server treats it as a router reboot. It closes the open row as Lost-Service and opens a new row at zero. The next Interim (cumulative) is compared with zero, so the earlier data is counted twice.
- **Example:** Ali used 100 MB. A repeated Start arrives. The next Interim says 150 MB total. Ali is charged 250 MB, and the Enforcer may disconnect him early.
- **Fix:** ignore a Start for a session that is already open with the same Acct-Session-Id. Treat it as a reboot only if the id differs or the counters went backwards.

### A2. Batch counter goes back after saving Settings (claim)
- **Fixed (date 2026-11-04), tests E16.03, E16.04** (E7.01 and the store test changed: a taken name is skipped, no 409). `UpdateSettings` writes `max(batch_counter, form value)`, so the field can only raise the counter (stale form cannot roll it back). `CreateBatch` skips taken names and stores the counter past the last number used (gives up only after 10000 taken names).
- **Where:** `internal/store/settings.go:39` (piece 4), and `internal/voucher/voucher.go:106` (piece 3).
- **Bug 1:** `UpdateSettings` writes `batch_counter` from the form. If a batch was made after the form was opened, the old value overwrites the new one. Example: counter 1360, batch of 30 makes it 1390, saving the old Settings form sets it back to 1360.
- **Bug 2:** when the next username is already taken, `CreateBatch` fails every time and the counter does not move. The admin must edit the counter by hand.
- **Fix:** leave `batch_counter` out of the generic Settings update. Make `CreateBatch` skip taken names, or move the counter past the conflict.

### A3. Wrong-secret checks run after a valid Message-Authenticator (claim)
- **Where:** `internal/radius/server.go:285` (piece 2).
- **Bug:** the password heuristics (`plausiblePassword`, `zeroPadded`) still run when a valid Message-Authenticator already proved the secret is right. A request whose password contains a tab or non-UTF-8 bytes gets no reply and raises the wrong-secret alarm.
- **Result:** the member sees "login server not answering" instead of INVALID_CREDENTIALS. The Router page shows a false alarm.
- **Fix:** run the heuristics only when no Message-Authenticator verified the packet.

### A4. The accounting read loop reads the database for every packet (claim)
- **Where:** `internal/radius/server.go:198` (piece 2).
- **Bug:** `sniffAcct` runs in the single UDP read loop. It reads the settings with `context.Background()` (no timeout) and computes an MD5 for each datagram. The library then reads the secret again, so there are two reads per packet.
- **Result:** if SQLite is busy, the loop stalls, the socket buffer fills and the kernel drops packets, even Stop.
- **Fix:** move the work into the per-packet goroutine, or cache the secret and clear it when settings are saved.

### A5. A huge Gigawords value can make a member look unlimited (claim)
- **Fixed (date 2026-11-04), test E16.01** (+ domain test). Readings above 1 PiB are capped; the SQL adds cap each counter at 2^60; `QuotaOf` adds with saturation.
- **Where:** `internal/domain/accounting.go:38` (piece 1).
- **Bug:** `withGiga` returns `math.MaxInt64` for large Gigawords, and the delta is added to the member counters. A bad packet with Gigawords >= 2^31 can overflow. If both directions overflow, `UsedDownload + UsedUpload` in `QuotaOf` goes negative.
- **Result:** remaining data becomes huge, so the member is unlimited.
- **Fix:** cap Gigawords (for example 2^20) or reject the packet. Use saturating adds in `QuotaOf` and in the SQL.

### A6. A tiny data limit becomes 0 bytes (claim)
- **Fixed (date 2026-11-04), test E16.02.** `bulkCap` gives a field error (422) for a value above 0 that rounds to 0 bytes.
- **Where:** `internal/domain/planform.go:36` (piece 1).
- **Bug:** `ParsePlan` accepts a positive value that rounds to 0 bytes, such as 0.0004 KB. The plan is still "limited" with 0 bytes allowed.
- **Result:** every login on that plan is rejected with OUT_OF_BULK.
- **Fix:** reject a value above 0 whose byte result is 0 or less.

### A7. Old sessions can block the single writer (claim)
- **Slow lookup:** `internal/store/accounting.go:61` (piece 4). `LatestSession` falls back to `ended_at IS NOT NULL AND username=? AND acct_session_id=? AND nas_ip=?` with no useful index. It scans up to 90 days of sessions inside the write transaction. Fix: add an index on `(username, acct_session_id, nas_ip)`, or drop the `ended_at` condition.
- **Big delete:** `internal/store/accounting.go:133` (piece 4) and `internal/app/app.go` around line 265 (piece 5). `PruneSessions` is one unbatched `DELETE` with no index on `ended_at` or `last_update_at`. After a long stop, or when the retention setting is lowered, it holds the only writer. Fix: delete in chunks of a fixed size and add an index.
- **Missing index:** `internal/store/migrations/0001_init.sql:107` (piece 4). No full index on `session(member_id)`. Deleting members and listing batches (`NOT EXISTS ... session`) get slow with many sessions. Fix: add a full index.
- **Result of all three:** logins and accounting wait, and the router sees timeouts.

---

## Group B: smaller correctness problems

### B1. Env file: quoted value followed by a comment (confirmed)
- **Where:** `cmd/hotspotpluslight/envfile.go:28-32` (piece 5).
- **Bug:** `HSP_ADMIN_ADDR="127.0.0.1:8081" # local` keeps the quotes, because the unquote branch needs the value to end with the opening quote. The bind fails. A quoted `HSP_SECRET_KEY` fails base64 decoding.
- **Fix:** read a leading quote first, find the closing quote, and ignore what follows.

### B2. Windows service stop (claim)
- **Where:** `cmd/hotspotpluslight/service_windows.go` (piece 5), around lines 151 and 78.
- **Bug 1:** `stopService` sends Stop to a service that is already in StopPending. That returns an error, so `service stop` and `service uninstall` report failure. Fix: wait when the state is StopPending.
- **Bug 2:** `Execute` does not read control requests until the app is ready. A Stop during a slow start is not handled, and `<-done` after cancel has no timeout. Fix: select on the change-request channel from the start.

### B3. Stopping the Enforcer twice crashes (confirmed)
- **Where:** `internal/session/enforcer.go:91-101` (piece 3).
- **Bug:** `Stop()` is not idempotent. The `stopped` flag is set but never checked. A second call closes a closed channel and panics.
- **Impact today:** none (called once). A later change could trigger it.
- **Fix:** return early if `stopped` is set, or use `sync.Once`.

### B4. Foreign-key errors shown as a server error (claim, store part confirmed as likely)
- **Where:** `internal/store/members.go:69` (piece 4), `internal/member/member.go:154` (piece 3), and `UpdateSettings` with a vanished `default_plan_id`.
- **Bug:** only the UNIQUE violation is mapped. A foreign-key failure (code 787) comes back raw. Example: a plan is deleted in another tab between the plan check and the write. `CreateBatch` already maps this to `ErrPlanGone`.
- **Result:** a 500 page, not a form message.
- **Fix:** map the FK failure to `domain.ErrPlanGone` in the member and settings paths.

### B5. Batch with the wrong number of passwords (claim)
- **Where:** `internal/store/batches.go:41` (piece 4).
- **Bug:** `CreateBatch` loops over `nb.PasswordsEnc` but sizes usernames and advances the counter by `nb.Input.Count`. Fewer passwords leave empty usernames and skipped numbers. More passwords panic with index out of range inside the transaction.
- **Fix:** check that the two lengths match at the top.

### B6. Migration check error read as a fresh database (claim)
- **Where:** `internal/store/migrate.go:100` (piece 4).
- **Bug:** `appliedChecksums` treats any error from the `sqlite_master` check as a fresh database. A short lock or I/O error then makes `Migrate` try 0001 again. It fails with a wrong "table already exists" error and skips the checksum and newer-DB checks.
- **Fix:** only `sql.ErrNoRows` means fresh.

### B7. Reject order (claim)
- **Where:** `internal/auth/evaluator.go:75` (piece 1).
- **Bug:** the plan checks (3-4) run before the active and `expires_at` checks (2). A disabled or expired member whose plan row is missing gets NO_PLAN or PLAN_REMOVED, not INACTIVE_USER or EXPIRED. This differs from PLAN §7.2.
- **Fix:** run rule 2 before the plan lookup, or change the docs to match.

### B8. Timing leak for unknown usernames (claim)
- **Where:** `internal/auth/evaluator.go:131` (piece 1).
- **Bug:** a known user goes through AES-GCM `Decrypt` and a constant-time compare. An unknown user only compares against a short dummy. `ConstantTimeCompare` returns early when the lengths differ, which leaks the stored password length. Someone on the LAN can tell which usernames exist.
- **Fix:** on the unknown-user path, decrypt a valid dummy ciphertext and compare equal-length values (for example hash both sides first).

### B9. Disconnect results are unclear (claim)
- **Where:** `internal/radius/disconnect.go:66` and `:103` (piece 2).
- **Bug 1:** the Disconnect is signed and sent even when `RadiusSharedSecret` is empty. The router drops it. The caller gets `ErrKickTimeout` after 2 x 1.5 s and the panel says "no answer". Fix: return a clear configuration error early.
- **Bug 2:** a reply with a bad Message-Authenticator is reported as "no answer" although a reply arrived. A secret mismatch looks like a dead router. Fix: record a separate outcome.

### B10. Admin web page problems (confirmed 1 to 4, claim 5 to 10)
All in piece 6.
1. **Expired session on a self-refreshing list:** `internal/web/admin/admin.go:356` and `password.go:34`. `requireSession` and `requirePasswordChanged` answer GET and HEAD with a 303 to the login page and never check `HX-Request`. The Online page polls every 10 seconds. When the session ends, the login page is swapped into the table. Fix: when `HX-Request` is set, answer 401 plus `HX-Redirect`.
2. **Stale list after a bulk delete:** `internal/web/admin/members.go:451`. When some Disconnects fail, the branch renders `DeletedNotice` but does not send the `membersChanged` trigger. Deleted rows stay, and clicking one gives a 404 until a reload. Fix: send the trigger in this branch too.
3. **Error messages dropped:** `internal/web/ui/ui.templ:26`. `htmxConfig` swaps only 422, 409, 429 and 502. Handlers that render `ErrorFragment` with 400, 404 or 500 are ignored by htmx. Fix: return a swappable code (usually 422) or add the codes to the config.
4. **Generic toast covers the specific one:** `internal/web/static/js/toast.js:34`. `htmx:responseError` shows "Something went wrong." even when the response carried its own toast. Fix: skip the generic toast when `HX-Trigger` carries one.
5. **Preview rebuilds on every request (efficiency):** `internal/routerpack/preview.go:53`. `Files(key)` rebuilds the messages block and CSS each time. The Router page has 8 iframes. Fix: build once per key.
6. **Extra database reads on polling (efficiency):** `internal/web/admin/admin.go:190`. Each poll runs `Settings.Get`, `Authenticate` and `MustChangePassword`. Fix: load settings only for full-page renders.
7. **Three copies of the `{id}` parse:** `pathID` (`members.go:26`), `planID` (`plans.go:32`), and an inline copy in `sessions.go:76`. Fix: use `pathID` everywhere.
8. **`planOptions` duplicates `planFilterOptions`:** `pages.templ:211` and `members.templ:22`. Fix: call `planFilterOptions(plans, "(none)")`.
9. **Hard-coded confirm texts:** `members.templ:462-464` repeats `confirmRenew`, `confirmDisconnect` and `confirmDelete` (lines 217-220). Fix: use the constants.
10. **Fragment comments break the sentence rule:** `sessions.go:85` and `ui.templ:76`. Fix: rewrite as full sentences.

### B11. Smaller items from pieces 2, 3 and 5
- **Rename while online (claim):** `internal/member/member.go:248-262`. The "is the member online?" check and the rename are separate steps. If the member logs in between them, the next Interim cannot find the session under the new name and inserts a second open row. Data is counted twice. Fix: do the check inside the `UpdateMember` transaction.
- **Plan delete message (claim):** `internal/plan/plan.go:93-110`. `Delete` pre-checks only `CountMembersOnPlan`. With members and batches it returns `InUseError{Members: n}` without the batch count. Fix: let the store's `DeletePlan` decide.
- **Bulk delete keyed by IP and MAC (claim):** `internal/member/member.go:433-440`. The `done` set is keyed by the whole `SessionTarget`, including mutable `FramedIP` and MAC. If an Interim changes either, the session is counted as missed and gets a second Disconnect. Fix: key by target ID.
- **Missing member in the Enforcer (claim):** `internal/session/enforcer.go:229`. A member deleted before the worker runs returns `ErrNotFound`, which is logged as an ERROR. Fix: treat it as nothing to do. The same for a missing plan.
- **Settings read per packet (claim):** `internal/session/session.go:197`. Every accounting packet reads the whole settings row just for the time zone, and the Enforcer reads it again on each Interim. About 330 extra reads per second at the 20,000-user load. Fix: cache it, or read it inside the open transaction.
- **Key check skipped in a race (confirmed trigger, body not read):** `internal/app/app.go:148-165` (piece 5). When `IsFirstBoot` is true only `seed` runs. If another process created the row, this process runs with a key that may not match. Fix: run `VerifyKeyCheck` after `seed` when it did not seed.
- **Trailing space in a piped password on Windows (confirmed):** `cmd/hotspotpluslight/main.go:116` (piece 5). Only `\r\n` is trimmed. `echo pw | hotspotpluslight admin set-password` in cmd sends `pw \r\n`, so the stored password has a trailing space. Fix: document it, or read from a prompt or file.
- **`set-password` on an old database (claim):** `internal/app/app.go` around line 355. It does not migrate first. On a database from before 0003 it fails with a raw "no such column must_change_password" error. Fix: show a clear hint to run `serve` first, or migrate.
- **`OpenSessions` on the write connection (claim):** `internal/store/members.go:338` (piece 4). It runs on the write connection outside a transaction. The read pool also sees committed WAL data, so a bulk kick holds the writer for no reason. Fix: use the read pool and correct the comment.

### B12. Cleanups (not bugs)
- `internal/linkstatus/linkstatus.go:28`: the `Event.Detail` comment is stale. Disconnect also records "NAK (session not found)" and "NAK (Error-Cause N)".
- `internal/radius/server.go:328`: `reply()` looks up User-Name a second time, only for a log line.
- `internal/radius/server.go:435`: `record()` and `linkstatus.host()` each split host and port by hand, with different fallbacks.
- `internal/domain/member.go:229`: `MemberStatus` compares expiry in whole seconds. `Evaluate` and `ShouldEnd` use `now.After(*ExpiresAt)`. They can disagree for up to one second. Fix: one shared helper for the expiry test.
- `internal/auth/auth.go:138`: `GeneratePassword(n<4)` returns 4 characters and its doc cites class rules BR-20 does not have. The only caller passes 24. Fix: honour `n` or reject small `n`, and fix the doc.

---

## Group C: doubts and design choices (owner decides)

- **Router limit counts both directions at full size.** `internal/domain/login.go:72`. `Mikrotik-Xmit-Limit` and `Mikrotik-Recv-Limit` are both set to the full remaining bytes, but the quota is download plus upload. A session can move up to double the remaining data before the Enforcer disconnects it. This follows owner decision O3. Option: `Mikrotik-Total-Limit`, which needs hardware verification (VERIFY-ON-HARDWARE).
- **First login open to the LAN.** `internal/app/app.go:150`. Until the password changes, `admin/admin` works from any computer on the network (default `:8081`). Option: bind to loopback for the first run. Question to the owner: yes or no?
- **MAC written back after the admin cleared it.** `internal/store/accounting.go:47`. `SetMemberMACIfEmpty` runs on every packet that carries a MAC. If the admin cleared a member's MAC on purpose, the next packet writes it back. Check against BR-13a.
- **Windows network paths.** `internal/store/dsn.go:28`. `\\server\share\db` becomes `file:////server/share/db`. SQLite may open the wrong path or fail. Not tested.
- **Slow delete of many online members.** `internal/member/member.go:195`. `disconnect()` runs up to 8 Disconnects at once inside one HTTP request, each waiting up to 3 s on a silent router. Deleting 500 online members with the router down takes about 190 s, so the browser times out. Option: run the Disconnects in the background.
- **Shutdown and the database.** `internal/app/app.go` around line 333. Server `Shutdown` can return a context error after 10 s, and `errors.Join` hides it. The code then calls `store.Close()` anyway, so an in-flight accounting transaction can be lost.
- **No HTTP timeouts on the admin server.** `internal/app/app.go:136`. Only `ReadHeaderTimeout` is set. Slow or idle clients can hold sockets open and `Shutdown` waits the full 10 s.
- **Login throttle scan.** `internal/auth/throttle.go:54`. Every failed login scans and prunes the whole table under one mutex. Many source addresses can slow login for everyone. Option: expire entries on a timer.
- **Dead password check.** `internal/domain/domain.go:51`. The `EqualFold(password, "admin")` check is never reached, because the 10-character rule rejects "admin" first. Near variants such as `Admin12345` pass. Option: remove the check or widen it.

---

## Not checked by the reviewers
- `internal/secret` and `internal/clock` were not read in piece 5.
- Piece 5 did not check nonce reuse, the randomness source, key logging, the `icacls` permissions on `hsp.env`, path quoting with spaces, the service recovery settings, or the Running-state timing.
- No piece said how deeply it checked the areas it reported as having no findings. "No finding" is not "checked and clean".
