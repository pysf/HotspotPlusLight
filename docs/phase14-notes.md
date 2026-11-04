# Phase 14 notes: Business name (2026-11-04)

The setting "Site title" is now "Business name" (`settings.business_name`, `domain.Settings.BusinessName`, form field `business_name`).

- Empty by default. It is optional (max 100 characters, trimmed). Nothing is shown while it is empty.
- When set, it shows in three places:
  - the top line of every printed voucher: "Hotel Aria · Wi-Fi" (`data-business`, still 14 vouchers per A4 sheet);
  - the admin header, small and muted under the brand (`.l-brand__name`);
  - the browser tab title: "Members - Hotel Aria · HotspotPlus Lite". Without a name it stays "Members - HotspotPlus Lite".
- The name reaches the layout through `ui.Env` (set by the admin `pageEnv` middleware from the settings service). A failed read hides the name; the page still renders.
- Migration `0005_business_name.sql` rebuilds the `settings` table. SQLite cannot change a column default, and new installs must start with an empty name, so a plain `RENAME COLUMN` was not enough. Old value `HotspotPlus` becomes empty; any other name is kept. All other settings are copied.
- Tests: `e2e/phase14_test.go` (E14.01 to E14.03). Site: `site/docs/configuration` and the search index updated.

## Voucher help limit
The business name adds one title line to each voucher. A long help text could push the credentials out of the cell, so `.help` is cut at 3 lines (`max-height` plus line-clamp in `voucher.css`). Manual check: set a very long voucher help, open the print preview, and confirm that 14 vouchers fit on A4 and every one shows its username and password.
