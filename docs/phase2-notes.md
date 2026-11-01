# Phase 2 notes: members (admin)

Built: member CRUD (`internal/member`, `internal/store/members.go`, `internal/web/admin/members.{go,templ}`), pure rules in `internal/domain/{member,quota,format}.go`, new reusable `ui.Pagination` and `ui.ConfirmAction`, `secret.RandomDigits`, `FakeDisconnector` + `Options.Disconnector` for tests. E2E: `e2e/phase2_test.go` (E2.01-E2.14, plus E2.10b deactivation kick), written first and seen red.

Skips: `TestE2_13.../matches_Access-Request` (needs the Phase 3 RADIUS evaluator; written, compares Accept/Reject + `Reply-Message` code with the page verdict).

Decisions (simplest reading, flag if wrong):
1. **Shared verdict:** `domain.Evaluate(state, plan, now)` implements checks 2, 5, 6, 7, 8 of PLAN 7.2 including auto-resubscribe (`Decision.Renew`); `QuotaOf` gives allowed/used/left. Phase 3 calls the same functions. The window is inclusive at both ends (legacy `between`); `expires_at` is expired only when now is strictly after it.
2. **Status precedence in the list:** expired > inactive > active (`domain.MemberStatus`); the filters follow the same split, so an inactive member whose `expires_at` passed shows and filters as *expired*, while the verdict (login order) says `INACTIVE_USER`.
3. **Mobile:** only ASCII digits are kept, then the last 10 (legacy kept the last 10 characters). **MAC:** must be 12 hex digits after normalization (legacy did not validate). Both noted in PLAN BR-2/BR-3.
4. **Bulk delete** is one route in two steps (confirmation fragment, then `confirmed=1`); "Select all N results" posts the filter and the server re-evaluates it inside the delete transaction.
5. **Deleting/deactivating** kicks via `member.Disconnector`. Production wiring is a no-op (`MONITOR:` in `internal/app`) until Phase 5; sessions are not marked ended by the member service (ending on ACK is the session service's job, BR-18). Kick failures never stop the delete/deactivation and are shown to the admin.
6. **Not built (Phase 5/7 routes):** `POST /admin/members/{id}/disconnections`, voucher print buttons.
7. **expires_at** is entered as `datetime-local` in the settings time zone; saving the form drops seconds below the minute.
8. Member passwords: 1-64 bytes, not subject to BR-20; the detail page never contains the password, only the reveal route does.
9. E2.08 "refused on ... no plan": impossible (plan is NOT NULL); only the unlimited-plan refusal (409) is tested.
10. Redirects: create answers 201 + `Location`/`HX-Redirect` to the member page; update, renew, extra bulk answer 200 + `HX-Redirect` to the member page (except a partly failed kick, which re-renders with a warning).

Open questions: none blocking. Confirm decisions 2 and 3.

## Review fixes (post-Phase 2 review)
- Data amounts (extra bulk, plan bulk) are capped in bytes (`domain.MaxBulkBytes`, 1 PiB) after unit conversion; `BulkBytes` and `QuotaOf` saturate instead of overflowing; the window length is clamped (plan form already caps `duration_days` at 36500).
- Renaming a member who has an open session is refused with a field error (sessions are keyed by username); PLAN is silent, flag if you prefer another rule.
- `DeleteMembers` returns the open sessions seen inside its transaction; the service kicks any not already kicked.
- Explicit-id bulk selections are limited to 500 (`member.MaxSelectionIDs`, 422 in the admin); filter selections are unbounded.
- A bad `settings.timezone` is now an error (500), not a silent UTC fallback.
- `expires_at` in the past while online: no disconnect (O3/O7), `MONITOR:` comment added in `Update`.
- Not changed: `NormalizeMAC` strictness (owner decision pending).

## Owner decisions (2026-11-01)
Approved: MAC strictness (12 hex digits), mobile digit handling, status precedence expired > inactive > active. Q44: keep the shared secret in the masked settings input.
