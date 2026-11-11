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
11. Member sheets follow design 4 and 4b (`MemberSheet`, `MemberEdit`, 2026-11-09), where the product has more or less than the design:
   - New `ui.More` ("More options", a native `details`, CSS `components/more.css`, opens by itself when one of its fields has an error) and `ui.ActionRow`. The sheet head has a sub line (`data-sheet-sub`, set by `dialog.js`).
   - Add sheet: Username, Password, Plan and a summary line ("10 GB · 30 days · 10 Mbps · 2 devices", plus the time limit if the plan has one). The line follows the plan select (`data-summary` on each option, `data-summary-for` on the select, `forms.js`). It has no "Valid until <date>": in the product "Valid until" is an optional hard stop and empty by default; the plan days count from creation (BR-5a).
   - "More options" of the add sheet holds Valid until, first name, last name, mobile, email, national code, MAC and the Account enabled switch (the design has one "Name or note" field; deviation 8).
   - Credentials card: block "Print login details" button under the details, footer is Add another and Done. Copy buttons and "Copy all details" stay (deviation 8).
   - Edit sheet: title is the username, sub line "<name> · member since <date>" (year only if not this year). Order: Current state card (status pill with the word only, online dot, usage, Account enabled switch, Kick now, Print login details), then Username, Password, Plan, Valid until, then More options.
   - Username stays editable (the product allows a rename while the member is offline; the board's "can't be changed" is wrong). The server error shows under the field (an online member gets "This member is online: disconnect the member before renaming."). The field is in the form, not in More options.
   - Password: hidden field with the current password (the admin may view it); "New" makes a 6-digit number and shows it. Saving a changed value sets it (`PUT /password` rules, BR-20 limits); unchanged or empty keeps it. A bad value is checked before anything is saved and gives a 422 with the error under the field; nothing else changes. Only the sheet does this; the member page form has no password field.
   - Kick now is the old Disconnect (same route and confirm). It is disabled while the member is offline.
   - Disable and Enable: the Account enabled switch is saved with "Save changes" (Update disconnects when it turns off, BR-18). There is no separate Disable button in the sheet; the row menu keeps Disable and Enable.
   - Renew (clears usage), Extra data (value and form, saved inside the sheet; changed 2026-11-12, no Open details link) and Delete are rows in More options. They sit outside the `form` element, so htmx sends no form fields with them (the step 3 notes are fixed). The state card switch and the More fields belong to the form through the `form="member-form"` attribute; the Save changes button too. A 422 re-renders the whole sheet, so the actions stay.
   - The list (`Members.dc.html`, unchanged) was checked again. Known differences, all product facts: the data cell reads "7.9 GB / 10 GB" with a "left" line (the board has "7.9 of 10 GB"); the search hint mentions mobile; Expires column; bulk bar has Delete only; chip "Expiring soon" (deviation 8).
   - `dialog.js`: the `close-sheet` event now empties the sheet host first. Before, a saved edit could reopen the sheet, because the old form was still in the host when the empty answer was swapped in.
12. Router and Settings (step 6), where the product has more or less than the design:
   - The Router page has no "Router connected" pill. The router link status lives on the Dashboard ("Router link" card; E19.02).
   - Each of the 8 cards keeps a live preview in an `iframe` (`c-pack-frame`) instead of the drawn thumbnail, and has Download plus "Open preview". The card title and text are the design's name and About text (the design shows file names such as `login.html`). The grid is `l-cols` `data-cols="4"` (2 under 1000 px, 1 on a phone).
   - The "Copy script" button sits in the card head (`ui.CodeBlock` is now the text box only). The script stays in a read-only textarea, not a `pre`, because the copy button reads its value. The upload steps keep the product facts: `flash/hotspot`, PAP only, and a hint about editing `login.html`, MESSAGES and `errors.txt`. The steps are 4, as in the design.
   - Settings is one form with one "Save settings" button, not one Save per group: the server checks and saves all settings together (one `PUT /admin/settings`). So the page line is "Change what you need, then save once." instead of "Each group saves on its own." The groups are `ui.SettingGroup` (`c-setting`).
   - Fields the board shows but the product does not have (not added): Idle timeout, Longest session, Block after wrong passwords (the Limits group), Sign-in and Accounting port (the product has the RADIUS auth and acct listen addresses in Server addresses), Password length as a select (the product has a number field). Product fields the board lacks: batch counter, maximum vouchers, default plan (own group "Members"), session retention ("Keep finished sessions (days)"), log level and format (group "Logs"), CoA port and accounting interval (group RADIUS).
   - The restart-needed banner stays at the top of the form. Server addresses and Logs say that a change needs a restart.
   - The secret field has the eye button and a new "Generate" button (24 letters and digits, `data-generate-alnum`, `forms.js`). The old "Reveal" text button with `id="reveal-secret"` is gone; E1.10 now looks for `button[data-reveal]` and `button[data-generate-alnum]`.
   - Saving shows the inline "Settings saved." note (E16.04 reads it) and also a toast. A failed save shows a "Not saved. Fix N fields and try again." notice above the field errors.
13. Change the admin username (step 7), where the product differs from the boards:
   - The route is `PUT /admin/password/username` on the existing page `/admin/password`, not a new `/admin/account` page (E1.05 keeps `/admin/account*` at 404). The page title is "Password and username"; the top bar link keeps the label "Password" (board 1b).
   - Rules list: only the two product rules ("10 or more characters", and "Both passwords match" on the forced screen), not "A letter and a number" (BR-20 has no character classes). The Password card on the page is the existing form with the same fields (current, new with Generate and the eye button, repeat).
   - The Username card's password input is named `username_current_password` so the page still has one `current_password` input (E1.05). Its hint is "4 to 32 letters, numbers, dot or dash." (`domain.ValidateAdminUsername`, BR-28). Saving the same name is a no-op success.
   - The Set new password screen keeps its Current password field (the first-start password `admin`; the board omits it). The Username field comes first, pre-filled, with the hint "Keep “admin” or choose your own. 4 to 32 letters, numbers, dot or dash." (the second sentence is added for the rule).
   - "Taken" does not apply: there is one admin account. A name equal to the password is refused (BR-20 says the password must not equal the username).
   - The Username card and the Password card each have their own form and Save button, as in the board. The cards use `ui.SettingGroup` (`c-setting`).

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
| 6 | Router (8 download cards, script, upload steps) and Settings (grouped form) | DONE |
| 7 | Change the admin username (added 2026-11-09, owner request). Design: board `docs/design/Account.dc.html` ("1b Password and username", the page behind the Password link: a Username card and a Password card, each with its own Save button, current password required) and the Set new password screen in `Login.dc.html` (new Username field, pre-filled with the current name, hint "Keep admin or choose your own."). Server: username rules as for other usernames, other admin sessions end, e2e tests, README and site first-login page | DONE |

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
   Look at the app: `make run`, then open http://localhost:8080/admin (admin / admin on a new
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
- Fixed by deviation 11: the edit sheet actions now sit outside the form, and they stay after a 422.
- The bulk bar has a danger Delete selected button with a trash icon and a Clear button (part of deviation 8).
