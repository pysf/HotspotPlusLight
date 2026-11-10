# Automatic release and self-update: plan

Status: Steps 1 to 3 built (step 2: the update check, setting, notice and Update page; step 3: download, hash check, unpack, backup, swap and the Restarter hook; the real restart and the rollback are step 4). Later steps not built. Decisions are from the owner, 2026-11-10.

## Decisions

| Topic | Decision |
|---|---|
| Version rule | Conventional commits. `fix:` = patch, `feat:` = minor, `!` or `BREAKING CHANGE` = major. Normal rules, also below 1.0. |
| What makes a release | Only `feat` and `fix` (and breaking). `docs:`, `chore:`, `test:`, `site:`, `ci:`, `refactor:` make none. |
| When | Every push to `main`, after `ci` is green on Linux and Windows. |
| Start | `v0.9.0` already exists. The next release is computed from it. |
| Update check | Server asks the GitHub API for the latest release. Once a day, plus a "Check now" button. |
| Trust | `SHA256SUMS` from the same release. No signing. |
| Privacy | Setting "Check for updates", on by default. Off means no call to GitHub. |
| Approval | Admin presses Update and confirms in a dialog. No password re-entry. |
| Method | Replace the binary, exit, the service manager or the program restarts it. |
| Rollback | Automatic. Keep the old binary and a copy of the database. If the new one is not healthy in about 60 s, put the old ones back. |
| Platforms | Windows service, Linux systemd, run by hand. macOS stays manual (no work spent on it). |
| Linux service | `service install` on Linux writes a systemd unit. Must be reliable. |

## Part 1: automatic release (GitHub Actions)

1. New workflow `autorelease.yml`, trigger `workflow_run` of `ci` on `main`, only when `ci` succeeded.
2. Step 1: find the last tag (`git describe --tags --abbrev=0`) and read the commits since it.
3. Step 2: a small Go program (`tools/nextversion`, pure function, unit tested) reads the commit subjects and bodies and prints the next version, or nothing when no commit is `feat`/`fix`.
4. Step 3: if there is a version, create the tag on the tested commit and push it. Use `GITHUB_TOKEN` with `contents: write`.
5. Step 4: tags pushed with `GITHUB_TOKEN` do not start other workflows. So the same workflow runs the release job itself (calls `release.yml` through `workflow_call`, or runs `make release` and `gh release create`).
6. Release notes: `gh release create --generate-notes`.
7. Safety: one run at a time (`concurrency`), skip when the commit is already tagged, never move a tag.
8. Commit style is checked in `ci` (a small check on the PR title or the last commit) so a typo cannot silently skip a release.

## Part 2: the server checks for updates

1. Package `internal/update`. Pure part in `internal/domain`: compare versions, pick the asset name for this OS/arch, parse `SHA256SUMS`.
2. A background job asks `https://api.github.com/repos/pysf/HotspotPlusLight/releases/latest` once a day. It sends no data about the install. It stores the answer (version, notes URL, time) in the `settings` table or a small table.
3. New setting "Check for updates" (default on) on the Settings page. Off: no job, no calls, and the button is hidden.
4. The admin layout shows a small notice "Version X is available" with a link to the Update page. The dashboard shows it too.
5. Update page `GET /admin/update`: current version, latest version, release notes link, "Check now", "Update now".
6. A dev build (`dev` version) never offers an update.

## Part 3: the update itself

1. `POST /admin/update` (admin only, CSRF as for all POSTs, confirm dialog in the page).
2. The server downloads the archive for its OS/arch and `SHA256SUMS`, checks the hash, and unpacks the new binary next to the current one (`name.new`). A wrong hash stops everything and shows an error.
3. It copies the database to `hsp.db.bak-<oldversion>` (SQLite online backup) and keeps `hsp.key` as is.
4. Swap: rename the running binary to `name.old`, rename `name.new` to the real name. Windows allows renaming a running .exe, but not overwriting it.
5. Restart:
   - Windows service and Linux systemd: exit the process with a special code. The unit/service recovery setting restarts it (`Restart=always`, Windows recovery "restart").
   - Run by hand: the server starts the new binary as a child, then exits.
6. Health check: the new process, on first start after an update, writes `update-pending` state. It deletes the state when `/healthz` has answered OK and migrations are done. A small watcher (the new process itself, in the first 60 s) is not enough if it crashes, so the rollback is done by the **old** binary logic placed in a tiny launcher step: on every start the program looks for `update-pending` older than 60 s with `name.old` present, and if found restores `name.old` and the database backup, then restarts. The service manager's restart loop makes this run again after a crash.
7. Migrations are forward-only. A rollback after a migration must restore the database copy. Members lose changes made since the copy; the page tells the admin this.
8. After a good update, `name.old` and the backup stay until the next update (one generation only).

## Part 4: Linux `service install`

1. `service install` on Linux writes `/etc/systemd/system/hotspotpluslight.service`, runs `systemctl daemon-reload`, `enable`, and offers `start`. It needs root and says so.
2. Unit: `Restart=always`, `RestartSec=2`, `User=` the user who runs the command (or a `hotspotpluslight` user if the owner prefers), `WorkingDirectory` and `HSP_DATA_DIR` set, `NoNewPrivileges=yes`.
3. `uninstall`, `start`, `stop` use `systemctl`.
4. The binary must be writable by the service user, or self-update cannot swap it. The installer puts it in a directory owned by that user. This is checked and shown on the Update page ("cannot update: file not writable").
5. The default ports (1812, 1813, 8081) are above 1024, so no root is needed. If the admin picks a port below 1024, the doc says to add `AmbientCapabilities=CAP_NET_BIND_SERVICE` to the unit.

## Part 5: tests (E22 and later)

- Unit: next-version rules (every commit type, `!`, `BREAKING CHANGE`, no release case), asset-name pick, `SHA256SUMS` parse.
- E2E with a fake GitHub (an `httptest` server; the API base URL is a setting only in tests): update available shows; setting off makes no call; "Check now" works; a wrong hash is refused; a good update swaps files (the test uses a tiny test binary and a fake restart hook); an unhealthy new version is rolled back and the database is restored.
- `service install` for Linux: unit file text test (no systemd needed).
- Windows CI keeps running the swap test (rename of a running file).

## Docs to update in the same changes

README and site pages for: Settings (new switch), Update page, install on Linux, upgrade notes, the commit rules (`CONTRIBUTING` section), `PLAN.md` (new decision, routes, schema), `CLAUDE.md` (commit-message rule).

## Open points (small, I will pick the simple choice unless told)

- The update state lives in the `settings` row (two new columns) instead of a new table.
- The special exit code for "restart me" is 75 on Linux and a normal exit on Windows (recovery restarts on any exit).
- Linux service user: the user who runs `service install`. A separate system user is not created.
- No release candidate or pre-release channel. Only the latest full release is offered.

## Build order (each step one commit, tests first)

1. `tools/nextversion` and tests; `autorelease.yml`; commit-style check.
2. `internal/update` check job, setting, notice, Update page (no install yet).
3. Download, hash check, backup, swap.
4. Restart and rollback.
5. Linux `service install`.
6. Docs and site.
