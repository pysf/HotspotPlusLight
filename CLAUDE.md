# HotspotPlusLight — agent context

Read this first. It is the entry point for any agent working in this folder.

## What this project is
**Go + HTMX + SQLite**, one binary, one SQLite file, native RADIUS server (PAP) for **one MikroTik router**, local-network deployment (plain HTTP).

The admin defines internet plans, creates members (single or batch vouchers), assigns plans; the system enforces quota (data, time, validity, concurrent sessions) at login and during accounting, with a live online sessions list and kick (RADIUS Disconnect).

## Source documents (in this folder)
| File | Purpose |
|---|---|
| `PLAN.md` | **The plan.** Decisions (§2), open decisions (§3), business rules BR-1…BR-24 (§5), schema (§6), RADIUS spec (§7), HTTP routes (§8), architecture (§9), hardware checklist (§13). |
| `CLAUDE.md` | This file. |

If `PLAN.md` and this file disagree, `PLAN.md` wins; fix this file.

## Locked decisions (details and rationale in `PLAN.md` §2)
- Single business, one admin, one router, MikroTik only, PAP only.
- Native Go RADIUS (auth 1812, acct 1813, Disconnect to 3799). No FreeRADIUS.
- SQLite only. No Redis, ClickHouse, Kafka.
- One HTTP listener (`ADMIN_ADDR` :8081) — admin panel + `/healthz`. No public listener.
- The member login page lives on the MikroTik router (static HTML). The server does not host it and offers no API for it.
- 8 static router page downloads (MikroTik default + 7 themes). No settings feed the pages.
- Everything is English. `Reply-Message` = reject code only; the router page maps it to text.
- First start creates `admin` / `admin` with `must_change_password=1` (no `ADMIN_PASSWORD` env).
- Admin can view member passwords (stored AES-GCM, recoverable).
- Router/RADIUS config lives in a single-row `settings` table; listen addresses and log settings are in that table too (editable in the dashboard; take effect after restart).
- `HSP_SECRET_KEY` is auto-generated on first start and saved to `{DATA_DIR}/hsp.key`. Back up that file with the database. Set it manually to override.
- `HSP_DATA_DIR` and `HSP_DEV` stay env-only. All other `HSP_*` vars seed the first-boot settings row; the dashboard is authoritative after that.

## Open decisions
All open decisions are resolved except the hardware items (VERIFY-ON-HARDWARE). See `PLAN.md` §3 for the full list.

## Current status
Phases 0–16 are done and committed. Phases 17–19 are planned (the dashboard home page, `PLAN.md` D25–D28, BR-25–BR-30, §11). Hardware validation is still open — see `PLAN.md` §13 for the checklist. Per-phase notes are in `docs/phaseN-notes.md` and in git history.

## How we work here
00. **Simple language.** Short sentences, plain words. No jargon. This applies to chat, docs, comments, and commit messages.
0. **KISS + reusability.** No extra features until the owner asks. UI = small reusable templ components shared by all pages (layout, table, pagination, filter bar, form field, confirm dialog, flash); never duplicate markup. Go code: one implementation per rule.
1. **E2E tests first.** Tests drive the product only through real interfaces: UDP RADIUS, HTTP, CLI. Name tests `E<phase>.<nn>`. Do not weaken a test to make it pass.
2. **Don't assume — ask.** When behaviour is unclear, check the code and `PLAN.md` first; if still unclear, ask.
3. **Parity matters.** Before dropping or changing behaviour, check `PLAN.md` §4. If something is not listed, ask.
4. **Domain rules are pure Go** in `internal/domain` (BR-1…BR-24), no I/O, no clock reads. Handlers stay thin and call services. Dependency injection by constructors; no globals, no `init()`, no mocking framework. Migrations: forward-only, one transaction each, auto-applied at startup, checksum-verified.
5. **Parameterized SQL only.** No string-built queries.
6. **Keep `PLAN.md` current.** If a decision changes, update the plan in the same change and note the date.
7. **Enforcement in two layers.** The router gets exact limits at login (`Mikrotik-Xmit/Recv-Limit` + Gigawords, `Session-Timeout`). The server also disconnects by itself: on each Start/Interim, `session.Enforcer` runs `domain.ShouldEnd` and sends a Disconnect. Keep `MONITOR:` comments only for real remaining gaps.
8. **Do not guess RouterOS/RADIUS vendor details.** Mark anything unverified with `VERIFY-ON-HARDWARE`.
9. **Keep README and site in sync.** When you change user-facing features, routes, settings, or behavior, update `README.md` and the relevant `site/` pages in the same change.

## Go comments (owner, 2026-11-04)

These rules apply to every comment:

- Use `//` line comments. The only exception is a long package comment.
- Write **complete sentences**. Exception: a short end-of-line comment can be a fragment:
  ```go
  limit := cfg.Limit // zero means no limit
  ```
- **Keep comments up to date.** When you change logic, update or remove the comments around it.
- **No comment is better than a useless one.** If the name already says it, write nothing.
- **Keep it short.** Use one sentence if you can, two at most.

The project tags `MONITOR:` (rule 7) and `VERIFY-ON-HARDWARE` (rule 8) still apply.

### Doc comments

- Put the comment directly above the declaration, with no blank line.
- The first sentence **must start with the name being declared**.
- For functions and methods, say what they return or do in special cases.
- Add the package comment in only one file of the package. Start it with `Package <name>`.

### Other comments

- **Explain why, not what.** Explain the business rule, the special case, or the reason.
- **Document constraints and workarounds.** If a line looks strange or surprising, say why.
- **Refactor before commenting.** If a block needs a comment only to explain what it does, try a better name or a smaller function first.

## Phase workflow (owner, 2026-11-01)
Each phase is done by its own agent: tests first (E2E ids from `PLAN.md` §11), implement until green, **do not commit**. Then the orchestrator: (1) re-runs `gofmt -l .`, `go vet ./...`, `go test -race -count=1 ./...`; (2) starts a **separate review agent** (fresh context, not the implementing agent) that runs the `code-review` skill at `medium` on the uncommitted changes and reports findings; real findings are fixed and the tests re-run; (3) commits with a `Phase N: …` message; (4) only then starts the next phase.

## Conventions (decided by the owner, `PLAN.md` O1)
Go (module requires 1.26 because `modernc.org/sqlite` does), **chi** router + **templ** components, vendored pinned htmx, `modernc.org/sqlite` (no CGO), `layeh.com/radius`, `bcrypt`, `httptest` + `goquery` in tests, `log/slog`. Timestamps are Unix seconds UTC; display in `settings.timezone`. Env vars are prefixed `HSP_` (see `PLAN.md` §9.2). Layout in `PLAN.md` §9.1.
