# Phase 17 notes: dashboard data layer (2026-11-07, owner request)

Built: migration `0007_login_failure_daily.sql` (`login_failure_daily(day, reason, count)`, counts only); failed-login counting in `radius.Server.reply()` (the one place every Access-Reject is written); the retransmission cache in `internal/radius/retry.go`; pruning in `session.Service.Prune`; pure rules in `internal/domain/dashboard.go` (`PeakConcurrent`, `HourlyAverage`, `PercentChange`, `ExpiresWithin`, the 80% rule, `TopReason`); the read service `internal/dashboard`; queries in `internal/store/dashboard.go`.

## Decisions
- A reject is counted once per request. The server remembers the last 1024 requests for 60 seconds (same source `ip:port`, Identifier and Request Authenticator), because the router repeats a request when it does not get the reply.
- Only counts are stored: no user names, no addresses. Rows are pruned with `session_retention_days`.
- If the counter cannot be written, the reject is still sent and the error is logged. The write happens before the reply (one SQLite write per reject, accepted at LAN scale; `MONITOR:` in `countFailure`). A failed write is forgotten by the cache, so the next retry is counted.
- `radius.New` takes options (`WithFailureRecorder`, `WithClock`), so old callers did not change.
- Members "Expiring soon" (3 days) and the dashboard list (7 days) use one rule, `domain.ExpiresWithin`, with two named day constants.
- Peak today is never lower than online now. The running hour counts by the part that has passed (BR-29).

## Tests
`e2e/dashboard_data_test.go` E17.01-E17.08 (written, not run by the agents); unit tests in `internal/domain`, `internal/dashboard`, `internal/radius`, `internal/session` and `internal/store`. `wantMigrations` is 7. The race detector found a log buffer race in a new test; it uses a locked buffer now.

## Not verified / known
- The e2e tests were not run by the agents (the owner runs them).
- A flood above 1024 distinct rejects a minute can count a retry twice; a retry from a new source port counts again (`MONITOR:` comments).
