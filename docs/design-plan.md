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
| 4 | Plans (cards + edit form, delete-blocked reason), Online sessions (kick + confirm) | todo |
| 5 | Vouchers/batches (form, list, A4 print sheet 3x6), Usage (7/30/90 switch, chart, top 10) | todo |
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
