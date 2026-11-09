# Phase 7 notes: batches and vouchers

Built: `internal/domain/batch.go` (pure rules), `internal/voucher` (service), `internal/store/batches.go`, admin pages `internal/web/admin/{batches.go,batches.templ,vouchers.templ}`, print stylesheet `internal/web/static/voucher.css`, nav item "Batches", "Print voucher" on the member page.

E2E: `e2e/phase7_test.go`, E7.01-E7.14 plus E7.01b (new form, default plan) and E7.15 (plan held by an empty batch). Unit tests: `internal/voucher`, `domain` (`BatchExpiry`, `VoucherPages`).

## How it works
- **Create (BR-15).** `voucher.Service.Create` validates (`domain.ParseBatchForm`, count 1..`batch_max_count`), encrypts one random password per member (`secret.RandomDigits(voucher_password_length)`), then `store.CreateBatch` runs ONE transaction: read prefix and counter, insert the batch row, insert the members `prefix+counter+i`, advance `batch_counter`. The store opens write transactions with `BEGIN IMMEDIATE`, so concurrent creations queue and never read the same counter (E7.02). A username clash rolls everything back (`UsernameConflictError`, 409).
- **Expiry.** `domain.BatchExpiry(now, loc, days)`: local midnight of (local date of now + days + 1). Same value on the batch row and on every member's `expires_at`. Members get `subscription_date = now`, plan window unchanged.
- **Print (BR-16).** One standalone templ page (`VoucherSheets`) for batch print, Reprint and single-member print: `dir="ltr"`, only `voucher.css` (no Tabler, no htmx), `<section class="sheet" data-page>` per A4 page holding up to 14 `.voucher` in a 2 x 7 CSS grid; `@media print` hides the toolbar and breaks after each sheet. Passwords are decrypted at print time. Help text = `settings.voucher_help`, or `domain.DefaultVoucherHelp` when empty; escaped by templ.
- **Delete (BR-23b).** `voucher.Service.Delete` -> `member.Service.Delete(MemberSelection{BatchID})` (kick, delete, re-kick sessions that appeared). `store.DeleteMembers` deletes the batch row in the same transaction when the selection is a batch. A kick failure is shown in a notice (no redirect) and does not stop the delete. E7.14 proves atomicity with a trigger that aborts the batch delete.
- **List.** Members, unused (all counters 0 and no session row) and online counts are live subqueries; `batch.count` stays the original size. Rename sends `PUT /admin/batches/{id}` (label only, htmx into `#flash`).
- **Routes.** `GET/POST /admin/batches`, `GET /admin/batches/new`, `PUT/DELETE /admin/batches/{id}`, `GET /admin/batches/{id}/vouchers`, `GET /admin/members/{id}/voucher`. Non-GET need CSRF; wrong verbs 405; unknown ids 404; `/admin/static/voucher.css` is public like the other assets.
- Small refactors: `memberID` -> `pathID` (shared by members and batches), `DeletedNotice` takes the back link, `domain.InUseError` counts batches.

## Decisions to confirm
`docs/open-questions.md` rows 30-37.

## Not built
CSV, QR codes, logo upload, PDF generation, per-batch activate/deactivate (dropped Q32).

## Review fixes
- **Batch delete vs. late logins.** Already covered: `voucher.Service.Delete` goes through `member.Service.Delete(MemberSelection{BatchID})`, whose `DeleteMembers` returns the open sessions seen inside the transaction and the service kicks those not yet kicked. Tests added: `member.TestDeleteBatchKicksSessionsThatAppearBeforeTheTransaction`, `store.TestDeleteBatchMembersReturnsOpenSessions` (also proves the batch row goes in the same transaction).
- **Plan deleted during batch creation.** `store.CreateBatch` maps the foreign-key violation on the batch insert to `domain.ErrPlanGone`; `voucher.Create` turns it into the same `plan_id` field error ("Choose a plan from the list.", 422). Tests: `store.TestCreateBatchPlanGone`, `voucher.TestCreatePlanDeletedMeanwhile`.
- **`DeletePlan` counts.** The member and batch counts come from one SELECT (one snapshot) and its error is returned instead of discarded. Test: `store.TestDeletePlanInUseCounts`.
- **Rename error shape.** Rename now renders the shared `ui.FieldError("label", ...)` (422, `data-error-for=label`); E7.13 asserts it.
- **Counter atomicity (verified).** `Open` uses `_txlock=immediate`, so `CreateBatch` takes the write lock at BEGIN; a failed creation rolls back the counter. Test: `store.TestCreateBatchRollbackKeepsCounter` (clash on the 3rd voucher leaves the counter unchanged; the next batch reuses the numbers). E7.02 covers concurrency.
- **Password generation (verified, tightened).** It used `crypto/rand` with `b%10` (small bias). `secret.RandomDigits` now uses rejection sampling (bytes >= 250 discarded) and rejects `n` outside 1..64; the settings form already limits the length to 4..20. Test: `secret.TestRandomDigitsBounds`.
- **`BatchExpiry` and DST (real bug fixed).** When a DST jump skips local midnight (e.g. America/Sao_Paulo 2018-11-04, Asia/Beirut), `time.Date` returned 23:00 of the previous day. `BatchExpiry` now steps forward to the first valid instant of the target day. Table test `domain.TestBatchExpiryAcrossDST` (Berlin, New York, Sao Paulo, Tehran, Beirut).
- **Print CSS.** Math checked: A4 with `@page margin 0`, sheet padding 10 mm, 7 rows of 1fr = about 39.6 mm each, 2 columns, 14 per sheet; `break-after: page` on every sheet but the last (`.sheet:last-of-type`, sheets are the only `section`s). Fixed: the printed sheet was exactly 297 mm high, so rounding could spill a blank trailing page; it is now 296 mm with `overflow: hidden` in `@media print`.
- **Emptied batches persist** (documented only): kept, see `docs/open-questions.md` row 35.

Note (2026-11-09): vouchers are now 18 per page, see BR-16.
