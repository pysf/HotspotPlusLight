# HotspotPlusLight — agent context

Read this first. It is the entry point for any agent working in this folder.

## What this project is
A **rewrite of the legacy HotspotPlus platform** as a small single-tenant system:
**Go + HTMX + SQLite**, one binary, one SQLite file, native RADIUS server (PAP) for **one MikroTik router**, local-network deployment (plain HTTP).

The admin defines **internet plans**, creates **members** (one by one or in printable voucher batches), assigns plans; the system enforces **quota** (data, time, validity window, concurrent sessions) at login and during accounting, with a live **online sessions** list and **kick** (RADIUS Disconnect).

The goal of the rewrite is to **keep the legacy business logic and features** while removing the infrastructure and multi-tenant product layers around them.

## Source documents (in this folder)
| File | Purpose |
|---|---|
| `PLAN.md` | **The plan.** Decisions (§2), open decisions (§3), feature-parity matrix (§4), business rules BR-1…BR-18 (§5), SQLite schema (§6), RADIUS spec (§7), HTTP routes (§8), architecture (§9), E2E strategy (§10), phases 0–9 (§11), hardware checklist (§13), legacy code map (§15). |
| `CLAUDE.md` | This file. |

If `PLAN.md` and this file disagree, `PLAN.md` wins; fix this file.

## The legacy reference (read-only)
**Path:** `/Users/payamyousefi/code/hotspotplus` — branch `develop`, commit `30bddd19`. **Never modify it.** Consult it whenever behaviour is unclear; `PLAN.md` §15 maps each rule to the legacy file/function.

Legacy stack: Node.js + LoopBack 3 (Mongo) API, AngularJS dashboard + portal, FreeRADIUS calling the API over HTTP, Redis cache, Kafka/KSQL, ClickHouse for sessions/usage, Go collectors (GoFlow, sinker), TS log-worker. Only the **API's member/plan/session logic**, the **portal**, and the **admin dashboard** are being ported; everything else is dropped.

Where the core behaviour lives in the legacy repo (paths relative to it):
- Login/quota rules: `api/common/models/member.js` (`checkAuthorization`, `radiusPostAuth`, `getSubscriptionDuration`, `hasEnoughBulk`, `hasEnoughTime`, `getInternetUsage`)
- Plans: `api/common/models/internetPlan.js`, `internetPlan.json`
- Accounting/sessions: `api/common/models/clientSession.js`, `usage.js`, `api/server/modules/cacheManager.js`, `db.js`
- RADIUS glue: `api/server/boot/radiusRoutes.js`, `api/server/modules/{radiusAdaptor,avps,radiusMessages,radiusDisconnectService}.js`; FreeRADIUS config in `radius.3.0.19/`
- Units/helpers: `api/server/modules/utility.js`
- Portal: `hotspot/app/controllers/base.js`, `hotspot/app/scripts/services/{appService,routerService}.js`; router pages `api/server/modules/routers/mikrotik/html/*`
- Admin UI: `dashboard/src/app/{member,internetPlan,nas,widgets}/`
- Voucher template: `api/templates/memberPrintTemplate.dust.html`

## Locked decisions (details and rationale in `PLAN.md` §2)
- Single business, **no tenant concept**; **one admin** account; **one router** (no NAS table, no router-type field); **MikroTik only**; **PAP only**.
- **Native Go RADIUS** (auth 1812, acct 1813, Disconnect to 3799). No FreeRADIUS.
- **SQLite only**: no Redis, ClickHouse, Kafka.
- Dropped entirely (usage charts were re-added by the owner in Phase 9, 2026-11-02): server-hosted portal pages (cookie/status/account/language), SMS/verification, payments, invoices, coupons, campaigns, tickets, departments, resellers, operators, roles/ACL/tokens, reports (netflow/DNS/web-proxy), themes, licensing, Dropbox, file storage, CSV import, backup/restore (for now), self-signup, plan history, IP pool, burst settings, hotel fields.
- Kept: plans (name, duration, speed, bulk, time limit, auto-resubscribe), members, extra bulk (admin grant), an optional default plan (preselected in the new-member form; the admin creates plans in the UI, none at startup; every member must have a plan), batch voucher generation + printing, online sessions + kick, member balance via the page API, router setup script + starter page pack.
- **The member login page is NOT hosted by this project.** It is a static HTML page stored on the MikroTik router, customized by the admin by hand from a downloadable **starter pack** (the full hotspot folder). The server provides only a small JSON API for that page (REST v1, RFC 9457 errors: `GET /api/v1/account` with Basic auth, `GET /api/v1/devices/{mac}/credentials`, `GET /api/v1/config`; CORS enabled) — see `PLAN.md` §8.1 and §9.4. Members never see or choose plans, there is no public signup, and the plan public/private flag and the default-plan activation limit are dropped (O9, O10).
- **Themes:** the 6 legacy portal themes (38 styles) are extracted as 38 independent downloadable themes: one shared layout (logo, site title, login form, footer) + a background photo + two colors each; the admin downloads one as a hotspot folder and edits it by hand (D22, `PLAN.md` §9.4, catalog in §16; source assets in the legacy `hotspot/app/{themeStyles,img}`).
- **Everything is English** (admin panel, API/RADIUS messages, voucher help). Each reject has a stable code + English text; the router-hosted page holds its own translation table keyed by code (O11, O20).
- **Two HTTP listeners (D23):** public `HTTP_ADDR` (:8080) = page API + `/healthz` only; admin `ADMIN_ADDR` (:8081) = admin panel only. First start creates `admin` / `admin` with `must_change_password=1` (no `ADMIN_PASSWORD` env); the admin is forced to the page `/admin/password` until a new password is set (Phase 11b, 2026-11-04). The password can be changed on that page or with the CLI `admin set-password` (forgotten password); BR-20: 10+ characters, at most 72 bytes, not the username, not `admin`, not a common password, no class rules.
- Admin can **view member passwords** (stored AES-GCM, recoverable).
- Router/RADIUS/portal config lives in a single-row **`settings`** table.
- No data migration from the old system.

### Owner direction on legacy bugs
**Keep the features even where legacy implemented them badly.** The bugs are fixed, the features are not dropped (see `PLAN.md` §4.4): working kick, enforced `expires_at`, Gigawords-aware usage, proper "unlimited" handling, password-verifying portal account check.

Explicitly **dropped** by the owner despite existing in legacy: plan **bind-to-MAC** (D18), CSV import, plan history, IP pool, burst settings, hotel fields (usage charts were re-added in Phase 9, 2026-11-02).

## Open decisions
`PLAN.md` §3 (O1–O19) lists decisions, each with a recommendation. Rows marked **RESOLVED** are decided; the others are **not approved** — if a task depends on one, ask the owner (or implement behind the recommended default and flag it clearly).

Resolved so far (2026-11-01): O1 stack (chi + templ), O2 legacy burst (4× / 8 s), O3/O4 (changed 2026-11-04, see rule 7: exact router limits + server-side Disconnect), O6 bind-to-MAC dropped, O7 expiry enforced at login, O8 MAC auto-login as a setting (off by default), O9 no portal plan selection / no signup, O10 default plan = optional form preselection, nothing created at startup; plans mandatory (no activation limit), O11 English only everywhere, O16 router setup helper + page pack kept, P1–P3 router-hosted page with a server API (account check + balance, MAC lookup, config) and a full hotspot-folder starter pack, O12 deleting a plan that members use is blocked (with the count), O13 member usernames are case-insensitive, English letters and digits only (no digit conversion), O14 batch defaults = legacy with 6-digit passwords, O15 admin starts as admin/admin and must change it at first login (page `/admin/password`; revised 2026-11-04) + `admin set-password` command, O17 backup/restore dropped for now, O18 finished sessions pruned after 90 days (configurable), O19 server receive time stamps accounting, O20 rejects carry `CODE` + English text (`Reply-Message = "CODE: message"`), O21 single English voucher help (printed left-to-right), no `member.language`, new `public_base_url` setting.

Usage counters live on the `member` row, with per-session rows for the online list (design choice D-A, §6) — confirmed.

**Nothing is open** except the items marked VERIFY-ON-HARDWARE (`PLAN.md` §13). The plan time-of-day window (O5) was dropped by the owner (Q40).

## ClickHouse / Redis knowledge
The legacy usage data lived in ClickHouse (`Session` = per-packet **delta** rows, `Usage` = summing view, `Charge` = SMS ledger) with Redis acting as the quota accumulator. `PLAN.md` §4.5 is the verified inventory of every table, query and cache key, and says what replaces each one. Read it before touching usage/quota logic. Key facts: usage is counted **since `subscription_date`**; legacy windowing was only eventually consistent; legacy had no retention; accounting time came from the router's `Event-Timestamp` (the plan uses server time, O19).

## How we work here
00. **Simple language (owner, 2026-11-02).** The owner finds long or complex English hard to follow. Write short sentences and plain words in chat, docs, comments and commit messages. No jargon when a simple word works.
0. **KISS + reusability everywhere (owner, 2026-11-01).** No extra features until the owner asks; don't propose or pre-build nice-to-haves. UI = small reusable templ components shared by all pages (layout, table, pagination, filter bar, form field, confirm dialog, flash); never duplicate markup. Go code: one implementation per rule (e.g. the login check and the admin usage summary share the domain functions).
1. **E2E tests first.** Each phase in `PLAN.md` §11 starts by writing its E2E tests (red), then implementing until green. Tests drive the product only through real interfaces: UDP RADIUS, HTTP, CLI. Name tests `E<phase>.<nn>` and reference the BR/O ids they cover. Do not weaken a test to make it pass.
2. **Don't assume — ask.** The owner has explicitly asked for questions over guesses. When legacy behaviour and the plan are unclear, read the legacy code first; if still unclear, ask.
3. **Parity matters.** Before dropping or changing behaviour, check `PLAN.md` §4. If something isn't listed there, stop and ask.
4. **Domain rules are pure Go** in `internal/domain` (BR-1…BR-23), no I/O, no clock reads. Handlers stay thin and call services (`internal/member|plan|session|voucher|auth`). **Dependency injection by constructors**: services take small consumer-side interfaces (repo, `Clock`, `Disconnector`…); production wiring lives only in `internal/app`; no globals, no `init()`, no mocking framework (hand-written fakes). Migrations: forward-only, one transaction each, auto-applied at startup, checksum-verified (`PLAN.md` §6, §9.1).
5. **Parameterized SQL only.** No string-built queries.
6. **Keep `PLAN.md` current.** If a decision changes, update the plan in the same change and note the date.
7. **Enforcement in two layers (owner, 2026-11-04; replaces the legacy-faithful gap).** The router gets exact limits at login (`Mikrotik-Xmit/Recv-Limit` + Gigawords, `Session-Timeout` = smallest of time left, `expires_at`, window end). The server also disconnects by itself: on each Start/Interim of an open session, `session.Enforcer` runs `domain.ShouldEnd` and sends a Disconnect (row ends `Server: <CODE>`). Do not add a second implementation of the rule; keep `MONITOR:` comments only for real remaining gaps (up to one interval of overshoot; Disconnect is best effort).
8. **Do not guess RouterOS/RADIUS vendor details.** The MikroTik attribute ids, the CoA attribute set, and `login-by` behaviour must be verified (`PLAN.md` §13). Mark anything unverified in code comments with `VERIFY-ON-HARDWARE`.

## Phase workflow (owner, 2026-11-01)
Each phase is done by its own agent: tests first (E2E ids from `PLAN.md` §11), implement until green, **do not commit**. Then the orchestrator: (1) re-runs `gofmt -l .`, `go vet ./...`, `go test -race -count=1 ./...`; (2) starts a **separate review agent** (fresh context, not the implementing agent) that runs the `code-review` skill at `medium` on the uncommitted changes and reports findings; real findings are fixed (by a fix agent or by the orchestrator) and the tests re-run; (3) commits with a `Phase N: …` message; (4) only then starts the next phase.

## Conventions (decided by the owner, `PLAN.md` O1)
Go (module requires 1.26 because `modernc.org/sqlite` does), **chi** router + **templ** components, vendored pinned htmx, `modernc.org/sqlite` (no CGO), `layeh.com/radius`, `bcrypt`, `httptest` + `goquery` in tests, `log/slog`. Timestamps are Unix seconds UTC; display in `settings.timezone`. Env vars are prefixed `HSP_` (see `PLAN.md` §9.2). Layout in `PLAN.md` §9.1.

## Current status
- **Phases 0 and 1 are done and committed** (skeleton, migrations, first-boot seeding, two listeners, RADIUS Reject stub, e2e harness + fake router; admin login/sessions/CSRF, `admin set-password`, plan CRUD, settings, shared templ UI components). Commands: `make run`, `make test`, `make ci`. Notes: `docs/phase0-notes.md`, `docs/phase1-notes.md`. Open owner question (Q44): the RADIUS shared secret is rendered in the settings page HTML in a password input — keep, or never put it in the page source. Run the `code-review` skill from inside this repo (it reviews the repo it is started in).
- **Phase 2 (members, admin) is done and committed** (review findings fixed). Notes: `docs/phase2-notes.md`. Owner approved (2026-11-01): strict 12-hex-digit MAC, mobile = ASCII digits then last 10, list status precedence expired > inactive > active; Q44 stays as is (secret in a masked input).
- **Phase 3 (RADIUS authentication) is done and committed** (review findings fixed). Notes: `docs/phase3-notes.md`. Admin UI styling: see the Phase 10 step A note at the end of this file (D24, `docs/ui-notes.md`). Next: Phase 4. **Autonomy (owner, 2026-11-01):** run the phases one after another without asking for confirmation; a blocker becomes an entry in `docs/open-questions.md` and work continues on the plan's recommended default.

- **Phase 4 (accounting and usage) is done and committed** (review findings fixed). Notes: `docs/phase4-notes.md`; known edge cases are listed there and in PLAN §7.3. Next: Phase 5 (online sessions and kick). Open questions for the owner: `docs/open-questions.md`.
- **Phase 5 (online sessions and kick) is done and committed** (review findings fixed). Notes: `docs/phase5-notes.md`. Open questions for the owner: `docs/open-questions.md`.
- **Phase 6 (router page pack and page API) is done and committed** (review findings fixed). Notes: `docs/phase6-notes.md`. Owner decisions 2026-11-02 (see `docs/open-questions.md`): all defaults accepted except row 12; English only (Persian sample removed, done); MAC stored RouterOS-style `AA:BB:CC:DD:EE:FF` (done, `domain.NormalizeMAC`, see `docs/mac-and-english-notes.md`); admin UI member list trimmed to username/online/status/data. Next: Phase 7 (batches and vouchers).
- **Phase 7 (batches and vouchers) is done and committed** (review findings fixed; DST-safe batch expiry). Notes: `docs/phase7-notes.md`. Open questions for the owner: `docs/open-questions.md`.
- **Phase 8 (ops, router pack, hardening) is done and committed** (review findings fixed): router setup script and link status panel on `/admin/router`, graceful shutdown, structured logs (`HSP_LOG_FORMAT`), README. Notes: `docs/phase8-notes.md`. **All planned phases 0-8 are complete.**  What remains: the hardware lab checklist (PLAN §13, everything marked VERIFY-ON-HARDWARE) and the owner's open questions in `docs/open-questions.md`.

- **Phase 9 (usage history) is built (2026-11-02, owner asked for it).** Migration 0002 `usage_daily`, `/admin/usage` (30-day chart + top 10 for 7/30/90 days). D12's "no usage charts" is superseded; renew keeps this history (BR-13 updated). Notes: `docs/phase9-notes.md`.

## Facts about the owner's environment
- Deployment: a machine on the owner's local network; HTTP only is acceptable.
- Only MikroTik routers are used. Legacy code for CoovaChilli/EnGenius was already broken server-side since 2019 and is not needed.
- The legacy product was Iran-oriented (Persian UI, Payping, Kavenegar, Jalali dates). The rewrite is **English only**, including the starter login page (its translation table is keyed by reject code and ships English only; the admin can add a language by hand). No payment/SMS integrations.

- **Phase 10 step A (own CSS, owner 2026-11-03, D24) is built, not yet seen in a real browser** (only headless screenshots). Tailwind is gone. The admin UI is server-rendered templ + our own CSS in `internal/web/static/css/` (tokens, `c-` components, layers), small JS modules in `static/js/`, themes `air` and `air-dark` (cookie `theme`), no build step. Rules: `docs/DESIGN.md`, `docs/FRONTEND.md`; deviations, how to add a component or page: `docs/ui-notes.md`; notes: `docs/phase10-notes.md`. Step B (sheets, row menus, toasts, status chips) is not built. `HSP_DEV=1` serves the component gallery at `/admin/dev/components`.

- **Phase 10b (new patterns) is built (2026-11-03), not committed, not seen in a browser.** Expiring soon (BR-24), filterable members list with status chips, sheets for member and plan forms, credentials card, row menus, `POST /admin/members/{id}/toggle`, toasts. Fragments are chosen only by the `HX-Target` header. Notes: `docs/phase10-notes.md`, `docs/ui-notes.md`.

- **Phase 11a (enforcement in two layers) is built (2026-11-04), not committed.** Decisions O3/O4 changed by the owner: exact router limits plus server-side Disconnect on Interim. Notes: `docs/phase11-notes.md`. Hardware items in `PLAN.md` §13.

- **Phase 11b (default admin/admin and the password page) is built (2026-11-04), not committed.** Migration 0003 `admin.must_change_password`; page `GET/PUT /admin/password`; forced-change middleware; new BR-20; `password.js`. Notes: `docs/phase11-notes.md` (Step B).
