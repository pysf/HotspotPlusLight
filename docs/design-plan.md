# Plan: apply the new admin design (started 2026-11-09)

Source of the design: `docs/design/` (boards + `hp.css`; see its README). The production CSS is
our own port in `internal/web/static/css/`. Rule from the owner: apply the design with ALL its
details, unless the design is wrong for the product.

## Allowed deviations from the design
1. Class names keep the `c-` prefix.
2. Usage bars stay on the native `<meter>` (no `pct-N` classes).
3. `@media` queries (phone < 700 px, icon rail < 1000 px), not container queries.
4. Status tokens are `--st-*` (our spacing tokens are `--s-N`).
5. Product facts win where the design is wrong:
   - the Router page has the MikroTik default plus 7 themes (not "eight pages: login, alogin, ...");
   - "over quota" is not a member status (it is a Needs attention item on the dashboard);
   - brand text is "HotspotPlus Lite".
6. The password rules list has two rules (10 or more characters, both match). "A letter and a number" is not a product rule (BR-20).
7. The Live line says "Live · updates every 10 s", because the whole page is replaced every 10 s and there is no timer.

8. Members list and sheet (step 3), where the product has more or less than the design:
   - The page line has no member count ("Guests and staff with Wi-Fi accounts."), because it sits outside the list region that refreshes.
   - The table keeps the Expires column; the status pill has the word only (the Expires cell holds the date and "in 2 days"). Online is its own column with dot and word.
   - The chip is "Expiring soon" (the product wording). The Plan and Page size selects stay in the toolbar without visible labels.
   - The bulk bar has only "Delete selected" (the product has no bulk disable), plus "Select all N results of this filter". It shows only while rows are ticked.
   - The form keeps first name, last name, mobile, email, national code and MAC (the design has one "Name or note" field). Its hints are the product rules (letters and digits only).
   - The credentials card has no "Wi-Fi network" row (the product does not store one). It shows Plan, Valid until and Devices (the plan's device limit). "Copy all details" stays as a ghost button.
   - "Generate" makes a random 6-digit number (the product default), not a longer password.

9. Plans and Online sessions (step 4), where the product has more or less than the design:
   - The plan edit form stays in the sheet (as for members), not as a card next to the plan cards, so no card is marked `aria-current`. The cards are an auto-fill grid (`l-grid`), not a fixed two columns.
   - The form keeps the product fields: one speed (value and unit, used for both directions), data limit with unit, time limit in minutes, "Renew automatically", and the hints "0 = no limit". The design's Download / Upload and "leave empty" are not product rules. Labels take the design wording ("Valid for (days)", "Devices at the same time", "Save plan", "Add plan").
   - The card row is "Speed limit" (one value), and a time limit that divides by 60 shows in hours ("8 hours", else "90 min").
   - Delete is blocked also for the default plan and for plans held by batches (the design only knew members); the reason text says which. The edit sheet shows the same reason in a warning box.
   - The online table has no Started column: the start time is the small line under "Time online". Download and upload share the "Data down / up" cell. The "Who" sub line is the member's first and last name (the design's "Voucher, 6 Nov" is not stored).
   - The button says "Kick". The confirm is the one shared dialog; a button can set `data-confirm-title`, `data-confirm-ok` and `data-confirm-icon` (see `dialog.js`).
   - The page head, with the Live line ("Live · updates every 10 s", deviation 7), is inside the polled fragment so the device count stays current. The "Updated by the router every N s" note stays under the table.
   - `plural` now adds "es" after "ch" ("2 batches"; it said "batchs").

10. Vouchers, print sheet and Usage (step 5), where the product has more or less than the design:
   - The form has no "Username start" and "Password length" fields: the prefix, the next number and the password length are settings (Settings page, BR-15), not per batch. It has "Batch name" (optional), "How many", "Plan" and "Valid for (days)" (product fields). The button says "Create and print" and has no live count, because a created batch goes straight to its print sheet (current behavior, E7.01); so the design's green "ready" alert with the Print sheet button is not shown.
   - `GET /admin/batches/new` is kept and shows the same page as `/admin/batches` (form and list together).
   - The batch table keeps the product facts: a Rename disclosure under the name, a Delete icon button next to Print (with the confirm of BR-23b, in the shared dialog), "Used" as "27 of 40" with a meter and an "N unused" line, and Created with date and time (the design has a short date). The icon button is a link with `data-reprint`.
   - The print sheet keeps one stylesheet (`voucher.css`) and no admin chrome (E7.06): the sheet header is a `div`, not a `header`, and the toolbar buttons are drawn by that file. Each card shows business name, "Wi-Fi", username, password and the help text from Settings (cut at 3 lines), not "Join <network>" (the Wi-Fi name is not stored). The sheet sub line is "Wi-Fi vouchers · batch name" (the plan summary of the design is not in the print data). 18 per page (`domain.VouchersPerPage`).
   - Usage: the 7/30/90 switch is a row of links (`c-seg` with `aria-current`), not radio buttons, and it now sets the chart, the three figures and the top list together (before, the chart was always 30 days). The chart is `ui.Plot` with the new kind `PlotWide`; the old `BarChart` is removed. The axis top and labels come from `domain.NewChartAxis` (round steps in the unit that fits). Bars get thinner for 90 days (`data-size`). Day labels are "11 Oct".
   - The top list shows a rank, the name (link), a meter (`ui.MeterPlain`, never turns warning), a small line "Down, Up, time" and the total with its unit (the design has bare numbers under a "GB used" title). Test hooks: `li[data-top]` with `[data-col=total|down|up|time]` (were `tr` and `td`).
   - Sizes of 1 TB and more print as "1.34 TB" (`FormatSize` gained a TB step).

## Steps
Each step: a sonnet agent builds it (tests first where logic changes), a separate sonnet agent
reviews it (`code-review` at high), real findings are fixed, then commit `Design vN: ...`.
Checks: `go run github.com/a-h/templ/cmd/templ@v0.3.1100 generate`, `gofmt -l .`, `go vet ./...`,
`GOOS=windows go vet ./...`, `go test -race -count=1 ./internal/... ./cmd/...`, then the owner
(or the orchestrator) runs `go test ./e2e`. Update the e2e selectors only when the design forces
it, and keep each test's intent. No inline style or script (CSP).

| Step | Scope | State |
|---|---|---|
| 1 | Tokens, system fonts, shared components, app shell (side bar, top bar, phone tab bar with More), icon sprite, theme (cookie `light`/`dark`/auto), component gallery, review fixes | DONE (commit "Design v2 step 1") |
| 2 | Login, Set new password (rules list), Dashboard (tiles, Needs attention, Router link, 3 charts, Live dot, 10 s refresh without jumps) | DONE (commit "Design v2 step 2") |
| 3 | Members list (chips, bulk bar, stacked cards on phone), Add/edit sheet, credentials card, row menus | DONE (commit "Design v2 step 3") |
| 4 | Plans (cards + edit form, delete-blocked reason), Online sessions (kick + confirm) | DONE |
| 5 | Vouchers/batches (form, list, A4 print sheet 3x6), Usage (7/30/90 switch, chart, top 10) | DONE |
| 6 | Router (8 download cards, script, link status) and Settings (grouped form) | todo |

## How to continue from another machine
1. Get the branch:
   ```
   git clone git@github.com-personal:pysf/HotspotPlusLight.git   # or: git fetch
   cd HotspotPlusLight && git checkout design-v2 && git pull
   ```
   Use the GitHub account `pysf` only. Commits use `payam.ysfi@gmail.com`.
   Ignore the untracked file `docs/mikrotik_hotspot_setup_guide.md` (never stage it).
2. Needs Go 1.26. No Node. templ runs with `go run github.com/a-h/templ/cmd/templ@v0.3.1100 generate`
   (the generated `*_templ.go` files are committed, so run it after every `.templ` change).
3. Check that all is green before you start: `make ci` (vet + all tests incl. e2e, about 1-6 min).
   Look at the app: `make run`, then open http://localhost:8081/admin (admin / admin on a new
   database; `make reset-db` for a fresh one). `HSP_DEV=1 make run` adds the component gallery at
   `/admin/dev/components`.
4. Read, in this order: `CLAUDE.md`, this file, `docs/design/README.md`, `docs/ui-notes.md`
   ("Design v2"), `docs/DESIGN.md`.
5. Start the next step in the table above (first one that is not done). Open the matching boards
   in `docs/design/` (for example step 2: `Login.dc.html`, `Dashboard.dc.html`) and read them with
   `hp.css`. Port every detail. Work in small commits on `design-v2`.
6. Tell Claude Code, for example:
   > Continue the design work. Read docs/design-plan.md and do step 2. Use a sonnet agent to build
   > it and a separate sonnet agent to review it, as in CLAUDE.md. Do not push until I ask.
7. Finish a step: update the State column above, commit `Design vN: ...`, and push `design-v2`.
   Merge `design-v2` into `main` only when all six steps are done and the e2e suite passes.

## Known notes from step 1
- No max page width in the design: checked members at 1920 px (step 3). It stays wide; only the usage bar is capped (`--w-usage-max`).
- Phone table cells: done in step 3. A cell that wraps its value in `.c-table__v` is a flex row (label left, value right); other tables still use the old label-then-value layout until their step.
- The dashboard fade is done: `swap.js` adds `u-swap-in` only after an htmx swap, to changed regions (step 2 added no `swap-in` class to the markup).
- The Online badge shows only when the count is above 0; the router footer says "Router connected"
  only if a packet came in the last 5 minutes.
- Checked in headless Chrome in step 3: sheets (add, edit, credentials), filled member rows, row menu, phone cards. The More menu was seen open only on the phone tab bar earlier.

## Other open work (not design)
Router login bug (reply leaves, router times out; see `docs/radius-reply-bug.md`), Group C items in
`docs/review-findings.md`, GitHub Pages setup, hardware lab checks (`PLAN.md` section 13).

## Known notes from step 3
- The edit sheet actions (Renew, Disable, Disconnect, Delete) sit inside the edit form, so htmx sends the form fields with them. Delete puts them in the query string. Move the actions out of the form when the sheets are reworked.
- After a failed edit (422) the actions section is not shown until the sheet is opened again (as before).
- The bulk bar has a danger Delete selected button with a trash icon and a Clear button (part of deviation 8).
