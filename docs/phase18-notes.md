# Phase 18 notes: the dashboard page (2026-11-07, owner request)

Built: `GET /admin` is the Dashboard (it replaced the redirect to Members) and the first sidebar item; `GET /admin/dashboard/online` (tile, polls every 10 s); `GET /admin/dashboard/link-status` (router status, polls every 5 s). New `internal/web/admin/dashboard.go` and `dashboard.templ`; new `ui.Tiles`, `ui.Tile`, `ui.TileValue`, `ui.TileNote`, a `home` icon and `css/components/tile.css`. The router status panel moved here from the Router page, which now has only the page designs and the setup script. Login, the password-change redirect and the brand link go to `/admin`.

## Decisions
- Page order: three tiles, router status, Needs attention (7-day expiry, 80% data), three charts (traffic 14 days, busy hours, active members per day). Only the online tile and the router status poll.
- Charts reuse `ui.BarChart`. Busy hours are drawn in hundredths (a non-zero average always shows a bar); `data-avg` has two decimals; the caption peak comes from the averages.
- The 7-day list has no "see all" link (the Members chip looks only 3 days ahead); it shows "and N more".
- E8.07 no longer re-baselines: a snapshot of every table proves that reading the panel writes nothing, that a reject changes only `login_failure_daily`, and that unanswered packets change nothing.

## Tests
`e2e/dashboard_page_test.go` E18.01-E18.14. Changed to the new landing page: E1.03 and the login and password checks, the sidebar test (8 links), E10.01 (adds `/admin`). E8.07, E8.07b and E8.13 read the panel on `/admin`.

## Not verified / known
- No e2e test was run by the agents. The page was not looked at in a browser, including the tile styling in both themes.
- Traffic today and Failed logins today are drawn on page load and go stale on a dashboard that stays open (by design, D25).

**Change on 2026-11-07 (owner):** the expiring list is 3 days, not 7 (`domain.ExpiringDays`, the same as the Members chip). The whole dashboard refreshes by itself: one container polls `GET /admin/dashboard/content` every 10 s. The routes `/admin/dashboard/online` and `/admin/dashboard/link-status` are gone. E18.15 is new; E18.03, E18.07, E18.08, E18.14 and E8.13 changed.
