# Phase 10, step A: own CSS, new look

Done (2026-11-03). Tailwind removed. New hand-written CSS and JS, served with content hashes. All admin pages restyled.

## What changed
- Removed: `internal/web/ui/input.css`, generated `app.css`, the Tailwind block and `css`, `css-check`, `tailwind-verify` targets
  in the Makefile (also `css-check` from `ci`), `/tools/` in `.gitignore`, every utility class in the templ files, old `app.js`.
- Added: `static/css/*`, `static/js/*`, `static.go` (bundle, hash, cache), `ui/env.go`, `admin/dev.templ`, `HSP_DEV` in config,
  `e2e/phase10_test.go`.
- Components: `c-btn`, `c-icon-btn`, `c-field`, `c-input`, `c-select`, `c-search`, `c-chip`, `c-card`, `c-table`, `c-meter`, `c-pill`
  (and `c-dot`), `c-pager`, `c-empty`, `c-notice`, `c-menu`, `c-sheet`, `c-modal`, `c-toast`, `c-credentials`, `c-code`, `c-chart`.
  Menu, sheet, credentials, meter, chip and search are only used in the gallery so far (step B uses them).
- JS: removed every inline `onclick`, `hx-on` and `style=`. Settings Reveal, select-all, bulk Cancel and the empty-selection guard
  are in `forms.js` (data attributes). Voucher Print is `print.js`. Theme swatches use an SVG `fill`.
- htmx config has `includeIndicatorStyles:false`. htmx is still the first `script[src]`.

## Tests
New: E10.01 to E10.05. Adapted selectors (same checks):
- E1.20: `.nav-heading` -> `.l-nav__heading`, `a.nav-link` -> `a.l-nav__link`.
- E2.01: `.dot` -> `.c-dot` (role and aria-label checks unchanged).
- E6.14: `[data-theme]` -> `body [data-theme]` (the `<html>` tag now has `data-theme` too).
- E7.06: `button[onclick*=print]` -> `button[data-print]`, no `[onclick]`, and the `print.js?v=` script loads.
- E8.12: `app.js` -> `js/clipboard.js?v=<hash>` (there is no `app.js` any more; the copy code moved to `clipboard.js`).

## Deviations
See `docs/ui-notes.md`. Extra: the Theme button is in the top bar (not the sidebar footer); `menu.js` and `forms.js`, `nav.js`
exist beside the five JS modules named in FRONTEND.md; the shared confirm dialog has no `<form>`.

## Not verified
No real browser session. Headless Chrome screenshots only.

# Phase 10, step B: new patterns

Done (2026-11-03). Not committed. Not seen in a real browser (see "Not verified").

## What changed
- **BR-24 expiring soon.** `domain.MemberStatus` (expired > inactive > expiring > active; 3 days or less left; whole seconds), `domain.StatusCounts`,
  `domain.FormatRelative`. `store.statusClause` is the SQL form; `store.CountStatuses` is the one count query. Unit test
  `TestStatusClauseMatchesGoRule` checks both at every boundary. The login check and the page API do not use it and did not change.
- **Members list.** Toolbar (search, plan, page size, chips with counts), list region, pager in place, columns User / Plan / Usage / Expires / Status / menu.
- **Sheets.** `ui.SheetHost`, `ui.SheetForm`, `ui.SheetSection`; new and edit for members and plans. Credentials card after create.
- **Row menus** for members (Edit, Renew, Disable/Enable, Disconnect, Delete) and plans (Edit, Delete). `POST /admin/members/{id}/toggle`.
- **Toasts** by `HX-Trigger`; `aria-busy` on list regions; `hx-disabled-elt` on submit buttons; `Esc` and focus are native `<dialog>`.
- Gallery: two-line table cells with meter, pill and dot; sheet with form and Actions section; credentials in a sheet.

## Decisions
- The edit sheet reuses `GET /admin/members/{id}` (HX-Target `sheet-body`), no new route.
- Creating a member from the sheet does **not** send `close-sheet`: the credentials card is the answer and "Done" closes it.
- A change from a list or sheet always answers `HX-Reswap: none`; errors there are toasts with the original status code (409, 502 ...).
- Disable asks for confirmation (it disconnects); Enable does not.
- Chip links carry the whole filter (q, plan, per) and no page number. The status is also a hidden field inside the chips block.
- "Active" now means active and **not expiring**: a member expiring in under 3 days is counted under "Expiring soon", not "Active".

## Tests
New: `e2e/phase10b_test.go` (E10.10 to E10.21), domain `TestMemberStatusBoundary` (extended), `TestFormatRelative`, store `TestStatusClauseMatchesGoRule`.
Harness: `AdminClient.DoHX`, `GetHX`, `SendHX` (send an `HX-Target`).
Selectors/expectations changed (same checks):
- E2.12: the status cell text is compared with the status word (`Disabled` for `inactive`), not with the raw value.
- Store `TestFilterSelectionAndDelete`: the member expiring exactly now is `expiring`; `active` count 2 became 1 plus an `expiring` count of 1.

## Not verified
No real browser: htmx behaviour of search debounce, `hx-sync`, OOB chips, sheet opening and closing, focus return, popover placement, the phone
card layout of the new columns. Only the server answers are tested.

## Review fixes (step B)
- Toggle now calls `member.SetActive` (only the `active` column; same kick-on-disable as Update). The route needs `active=0|1` (hx-vals on the button); a repeat is a no-op. Missing value gives 400. (E10.18, E10.22)
- Bulk delete from the page sends `members-changed` + a toast, no redirect (E10.23).
- 404 on a list/sheet action: refresh event + "This member/plan no longer exists."; 500: toast "Something went wrong. Try again." (E10.24).
- Chips use `aria-current="true"` (they are links), not `aria-pressed`.
- Sheet title resets to "Details" when a fragment has none; a button inside the sheet that reloads it hides the old content first.
- `firstError` picks by sorted field name.
