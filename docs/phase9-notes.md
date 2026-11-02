# Phase 9 notes: usage history (2026-11-02, owner request)

Built: migration `internal/store/migrations/0002_usage_daily.sql` (`usage_daily(member_id, day, down, up, seconds)`, PK member+day, FK cascade, index on day); `internal/domain/usage.go` (pure: `DayKey`, `DayWindow`, `FillDays`, ranges 7/30/90, default 30); `session.AcctTx.AddDailyUsage` + `store.AcctTx` upsert, called in `Service.Account` right after `AddUsage` with the same `res.Delta`, same transaction; `internal/usage` (report service, `Report(ctx, days)`); `store.DailyTotals/TopMembers`; `GET /admin/usage` (nav item "Usage"); reusable `ui.BarChart` (inline SVG, no JS, Tabler colour via `currentColor`).

## Decisions
- Day key = `YYYY-MM-DD` in `settings.timezone` of the packet's server receive time (O19). Text keys sort chronologically and survive DST. `Account` reads settings once per packet (outside the transaction) for the zone.
- Chart: last 30 days including today, all members summed, one bar (download + upload) per day, tooltip with the split. Top list: 10 members by download + upload over 7/30/90 days (`?days=`, anything else = 30), ties by username, zero-traffic members omitted.
- Renew does not delete history; member delete cascades. BR-13 text, D12, PLAN section 4/6/7.3/8/11 updated. No pruning, no new setting (open question 48).
- The member detail page has no history (not trivial; skipped).

## Tests
`e2e/phase9_test.go` E9.01-E9.09 (written first, red, then green); unit tests in `internal/domain/usage_test.go`, `internal/usage/usage_test.go`, `internal/session/session_test.go` (day row + rollback).
E0.09 (`e2e/phase0_test.go`) only changed legitimately: it asserted exactly one migration; it now asserts `wantMigrations = 2` and the deliberately failing migration is numbered 0003 after the two shipped files. The unit migration tests in `internal/store/migrate_test.go` use their own fake FS and are unchanged.

## Not verified / known
- No backfill of history from before the upgrade.
- Usage between two interims that straddles midnight is booked entirely on the later packet's day.
- Chart is not tested visually in a browser (server-rendered SVG, structure asserted via data attributes).
