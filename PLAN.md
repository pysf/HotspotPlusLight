# HotspotPlusLight — Implementation Plan

**Status:** DRAFT v2 — all decisions in §3 are resolved (grill session closed the open items; the plan time-of-day window was dropped) · 2026-11-01
**Source of truth for legacy behaviour:** `/Users/payamyousefi/code/hotspotplus` (branch `develop`, commit `30bddd19`)
**Approach:** end-to-end tests first. Every phase starts by writing its E2E tests (red), then implements until green.

> How to review: read §2 (what is decided), answer §3 (open decisions — each has a recommendation, so "accept all" is a valid answer), skim §4 (parity matrix: confirm nothing you care about is marked Dropped by mistake), then §10–11 (tests and phases).

---

## 1. What we are building

A single-tenant, single-router, MikroTik-only WiFi hotspot manager, shipped as **one Go binary** with **one SQLite file**:

```
 MikroTik hotspot ──UDP 1812/1813 (PAP, accounting)──►  ┌────────────────────────────┐
        ▲   ◄──UDP 3799 (Disconnect-Request)──────────  │  hotspotpluslight (Go)     │
        │                                               │   • RADIUS server          │
 member's browser ──JSON/CORS──► page API ───────────►│   • Page API (JSON, CORS)  │
 admin's browser  ──HTTP──► admin pages  (HTMX) ───────►│   • Admin  (one account)   │
                                                        │   • SQLite (WAL)           │
                                                        └────────────────────────────┘
```

The admin defines **internet plans**, creates **members** (one by one or in printable batches), assigns plans, and the system **enforces quota** (data, time, validity window, concurrent sessions) at login and during accounting, with a live **online-sessions** list and **kick**.

*(The member login page is a **static HTML page stored on the router**, customized by the admin from a downloadable starter pack. This project ships the pack and a small JSON API for it, but does not host the page.)*

### Out of scope (dropped from legacy)
Multi-tenancy/Business entity, resellers, operators, roles/ACL/tokens, invoices, payments (Payping), coupons, campaigns, tickets, departments, reports (netflow/DNS/web-proxy), SMS and verification, licensing, Dropbox, file storage, themes/pin-code/social links, a server-hosted member portal and any Persian/multi-language UI, ClickHouse, Kafka/KSQL, Redis, FreeRADIUS, GoFlow, sinker, Logstash, CHAP/MS-CHAP/EAP, other router vendors, data migration from the old system.

**Not now (owner 2026-11-01, Q26):** bulk change-plan / activate / deactivate on selected members. Legacy never had them (its member grid had multi-select with one bulk action, delete — `destroyMembersById`, kept: §4, §8.2, E2.10). Revisit if the admin ends up editing many members one by one.

---

## 2. Decisions already made (from our conversation)

**Guiding principles (owner 2026-11-01): KISS and reusability, at every level from feature to implementation. Do not add features until the owner asks for them. UI: build from small reusable templ components shared across pages (layout, table, pagination, filter bar, form field, confirm dialog, flash message) — never copy-paste markup between pages.**


| # | Decision |
|---|---|
| D1 | Single business. No tenant concept anywhere. |
| D2 | One admin account. |
| D3 | One router, MikroTik only. **No NAS entity and no router-type field.** |
| D4 | **PAP only.** Evidence: the portal posts the plaintext password to the router, and the router's `rlogin.html` redirect uses `$(plain-passwd)`. (Router-side `login-by` is not in the repo — confirm on hardware, §13.) |
| D5 | Native Go RADIUS server (auth, accounting, Disconnect). No FreeRADIUS. |
| D6 | SQLite only. No Redis, ClickHouse, Kafka. |
| D7 | Dropped: SMS/verification, payments, invoices, coupons, campaigns, tickets, departments, resellers, operators, reports, roles/ACL/tokens, themes. |
| D8 | Admin can **view** member passwords → stored reversibly (AES-GCM), not hashed. |
| D9 | Local network deployment; plain HTTP is acceptable — **also for the admin listener; HTTPS (self-signed or own cert) was offered and declined, owner 2026-11-01.** |
| D24 | **Admin UI = our own hand-written CSS, no Tailwind, no build step (owner 2026-11-03).** Supersedes the Tailwind v4 version of 2026-11-02 (and Tabler, Bootstrap before it). Look and code rules: `docs/DESIGN.md` and `docs/FRONTEND.md`; deviations and how-to: `docs/ui-notes.md`. Plain server-rendered templ (O1 kept) + htmx + small vanilla JS modules. CSS in `internal/web/static/css/` (tokens, base, layout, one file per `c-` component, cascade layers). Go inlines the `@import`s at startup and serves every asset with a content hash in the URL and a long cache. Themes: `air` (default) and `air-dark`, chosen by the `theme` cookie. Same menu as before. See Phase 10. |
| D23 | **Two HTTP listeners (owner 2026-11-01).** The *public* listener (`HTTP_ADDR`, :8080) serves only the page API (§8.1) and `/healthz`; the *admin* listener (`ADMIN_ADDR`, :8081) serves only the admin panel (§8.2). Only the public port goes in the router's walled garden; the admin port is not reachable from the hotspot network (README: block it in the router firewall). Admin routes return 404 on the public port and the reverse. |
| D10 | The member login page is a **static HTML page hosted on the router** (not by us), customized by the admin by hand from a downloadable starter pack; the server only offers a small JSON API for it (P1–P3). |
| D11 | Members are created by the admin: **one by one** and **bulk-generate (printable vouchers)**. **CSV import is dropped.** No self-signup. |
| D12 | Usage = quota enforcement + online list + kick. ~~No usage charts/reports.~~ **Superseded 2026-11-02 (owner asked for Phase 9):** a daily usage history (`usage_daily`), a 30-day chart and a top-10 members list on `/admin/usage`. Still no other reports (netflow, DNS, per-session history, exports). |
| D13 | Plan features kept: extra bulk (admin grant), auto-resubscribe, an optional default plan (preselected on the new-member form; the admin creates plans in the web UI — nothing is created at startup; plans are mandatory — O10, Q19). Dropped: plan history, IP pool, per-plan burst settings (the legacy default burst is still sent — O2), hotel fields, public/private plan flag. |
| D14 | Router/RADIUS/portal config lives in a single-row **`settings`** table. |
| D15 | No data migration. |
| D16 | E2E-test-first. |
| D17 | ~~Plan time-of-day window~~ **DROPPED (owner 2026-11-01, Q40)**: the owner isn't sure it's needed; legacy never enforced it. No columns, UI, API fields, messages or tests. Re-add only on request (needs a new migration). |
| D18 | Plan **bind-to-MAC** is **dropped**. |
| D19 | **Everything is English** — admin panel, API/RADIUS messages, voucher help text. The router-hosted page holds its own translation table keyed by message code. |
| D20 | Page API offers: one `GET /api/v1/account` (credentials check + balance + `loginAllowed`/`reason`), MAC auto-login lookup, site title/branding config (P1, P2, Q37). |
| D21 | The starter pack is the **full hotspot folder** (customizable login page + the small router pages) (P3). |
| D22 | The legacy portal themes are extracted as **38 independent, downloadable themes** (6 categories × their styles). Each theme = one background photo + a primary color + an overlay color on **one shared layout**; the layout has logo, site title, the login form and a footer text (T1, T2). See §9.4 and §16. |

---

## 3. Open decisions (need your answer)

Each row has my recommendation. Replying "accept all recommendations" is fine; otherwise answer by number.

| # | Question | Why it matters (legacy fact) | Recommendation |
|---|---|---|---|
| O1 | **Tech stack.** | **RESOLVED (owner, 2026-11-01).** | Go (the module currently requires 1.26 because `modernc.org/sqlite` does), **chi** router + **templ** components (htmx partials), vendored htmx (pinned), SQLite via `modernc.org/sqlite` (pure Go, no CGO), RADIUS via `layeh.com/radius`, `golang.org/x/crypto/bcrypt`, tests with `httptest` + `goquery`. |
| O2 | **Burst.** | **RESOLVED — keep the legacy default burst (owner, 2026-11-01).** Always send burst with factor 4 and time 8 s; no per-plan setting. | Implement exactly the legacy `getRateLimit` format (BR-10). |
| O3 | **Data cap attribute.** | **CHANGED — two layers (owner, 2026-11-04; replaces the legacy decision of 2026-11-01).** Layer 1: when the plan has a data limit, `Mikrotik-Xmit-Limit` and `Mikrotik-Recv-Limit` are each the bytes left; from 4 GiB up the high 32 bits go into `Mikrotik-Xmit-Limit-Gigawords` / `Mikrotik-Recv-Limit-Gigawords`. Unlimited data: no data-limit attribute (no 3.9 GB number, no clamp). Layer 2: the server sends a Disconnect itself when an Interim shows the member must not be online (`domain.ShouldEnd`, BR-10, §7.3). | Implemented in Phase 11a (`docs/phase11-notes.md`). Not proven on a router: the direction of Recv/Xmit, and the Gigawords limits (§13). |
| O4 | **Session-Timeout.** | **CHANGED (owner, 2026-11-04; replaces the legacy decision of 2026-11-01).** `Session-Timeout` = the smallest of: time quota left (if limited), seconds until `expires_at` (if set), seconds until the window end (also for auto-renew plans: the member is cut at the window end and the next login renews). At least 1, at most 4294967295. Always sent. The 48 h number (172800) is gone. | Implemented in Phase 11a. |
| O5 | ~~Plan time-of-day window~~ | **DROPPED (owner 2026-11-01, Q40).** Not implemented in any form. See D17. | — |
| O6 | **`bindMemberSigninToMac`** plan flag. | **RESOLVED — dropped (owner, 2026-11-01).** | None. |
| O7 | **Member `expires_at`** (voucher/account expiry). | **RESOLVED — enforce (owner, 2026-11-01).** A login after `expires_at` is rejected (`expired`); the admin list shows "expired". Legacy set the date but never checked it. | Implement as stated. |
| O8 | **MAC auto-login.** | **RESOLVED — keep as a setting, off by default (owner, 2026-11-01).** The settings page shows a warning about MAC spoofing. | Implement as stated. |
| O9 | **Member plan selection at the portal.** | **RESOLVED (owner, 2026-11-01).** Members never see plans and cannot choose or activate one; they are given a plan by the admin, who alone manages plans. There is **no public signup**: the portal has a single login page (plus the post-login status/account pages). The plan `access_type` (public/private) flag is dropped. | Implement as stated. |
| O10 | **Default plan.** | **RESOLVED (owner, 2026-11-01); revised Q19:** every member **must have a plan** (no plan-less members). The admin picks one default plan in settings; it is only the **preselected value** of the plan dropdown on the new-member form (nothing is assigned silently). **Nothing is created at startup** (owner 2026-11-01): the admin creates the first plan in the web UI; until a default is chosen the form has no preselection. The activation period/count settings, the auto-assign toggle and the member activation counter are dropped (nothing triggers re-activation). The admin can change any member's plan. | Implement as stated. |
| O11 | **Portal language.** | **RESOLVED (owner, 2026-11-01): English only, everywhere** (admin panel, API, router reject messages, voucher help). The router-hosted page carries its own translation dictionary keyed by message code, shipped with English only (Persian sample removed 2026-11-02); the admin can add a language by hand. | Implement as stated. |
| O12 | **Delete a plan that members use.** | **RESOLVED — block it and show the member count (owner, 2026-11-01).** The default plan also cannot be deleted until another default is chosen. | Implement as stated. |
| O13 | **Member username rules.** | **RESOLVED (owner, 2026-11-01): case-insensitive; only English letters and digits (`A–Z a–z 0–9`), 1–64 characters.** No `.`, `_`, `@`, `-` or non-ASCII characters. Persian/Arabic digits are **not** converted — they are rejected like any other invalid character (*to confirm: members typing Persian digits will simply fail to log in*). | Implement as stated. |
| O14 | **Batch (voucher) parameters.** | **RESOLVED (owner, 2026-11-01): legacy defaults with 6-digit passwords.** Max 30 accounts per batch, usernames `wifi` + counter starting at 1360, expiry = local midnight of (today + plan duration + 1 day); all configurable in settings (`batch_max_count`, `batch_username_prefix`, `batch_counter`, `voucher_password_length` = 6). | Implement as stated. |
| O15 | **Admin password reset.** | **RESOLVED (owner, 2026-11-01, revised in grill Q9): default admin/admin, forced change at first login (revised 2026-11-04, Phase 11b; before: random password printed once, CLI only).** On the first start the server creates the admin (`ADMIN_USERNAME`, default `admin`) with the password `admin` (bcrypt) and `admin.must_change_password = 1` (migration 0003; existing installs keep 0 and are not forced). It prints a marked block (`=== HotspotPlusLight first start ===` saying: log in with this username and password `admin`, you will be asked to set a new password; `Forgot it later? Run: hotspotpluslight admin set-password`); no `ADMIN_PASSWORD` env. While the flag is set, every admin route except `/admin/password`, `/admin/logout` and static files redirects to `/admin/password` (303 for a page, `HX-Redirect` + 403 for a change request). The password can be changed on the page `/admin/password` (current, new, confirm; show/hide; Generate) or with the CLI `admin set-password` (forgotten password). Both clear the flag. Admin passwords must meet BR-20. | Implement as stated. |
| O16 | **Router setup helper and page pack.** | **RESOLVED (owner, 2026-11-01): keep, as a full hotspot-folder download plus a generated setup script (P3).** | Implement as stated (§9.4). |
| O17 | **Backup/restore.** | **RESOLVED — dropped for now (owner, 2026-11-01).** No backup/restore feature in the first release (legacy had a JSON export/restore). Operators copy the SQLite file themselves while the service is stopped; the `SECRET_KEY` must be kept separately. | May return as a later phase. |
| O18 | **Session retention.** | **RESOLVED (owner, 2026-11-01): delete finished sessions after 90 days** (configurable via `settings.session_retention_days`, `0` = keep forever). Legacy kept everything forever; quota no longer depends on old sessions. | Implement as stated (daily prune job, test E4.16). |
| O19 | **Which clock stamps accounting records.** | **RESOLVED — server receive time (owner, 2026-11-01).** The router's `Event-Timestamp` is kept only for display/debugging. | Implement as stated. |
| O20 | **Error message format.** | **RESOLVED (owner, 2026-11-01): code + English text.** The API returns `{code, message}`; the RADIUS `Reply-Message` is `"CODE: message"`, so the page can translate router-delivered errors too and falls back to the English text. | Implement as stated (§7.6, §8.1). |
| O21 | **Consequences of English-only.** | **RESOLVED (owner, 2026-11-01):** one `settings.voucher_help` (English default); the voucher print page is **always left-to-right** (no `dir` option); `member.language` is dropped; new `settings.public_base_url` (how clients reach this server, baked into the router page pack); `settings.router_login_url` is removed. | Implement as stated. |
| P1–P3 | **Router-hosted page decisions.** | **RESOLVED (owner, 2026-11-01):** P1 the page learns login failures from our API (`GET /api/v1/account`); P2 the API provides **account (check + balance), MAC auto-login lookup and site config** (reshaped as REST v1, Q37); P3 the pack is the **full hotspot folder**. | See §8.1 and §9.4. |

---

## 4. Feature parity matrix (nothing silently lost)

Legend: **Keep** = same behaviour · **Change** = kept, behaviour differs (see note) · **Fix** = legacy bug fixed · **Drop** = intentionally removed.

### 4.1 RADIUS / router
| Legacy | Disposition | New |
|---|---|---|
| `POST /api/radius/authorize/:nasIp` (returns cleartext password; FreeRADIUS compares) | Change | PAP verified in-process inside Access-Request handling |
| `POST /api/radius/post-auth` (quota/plan/session checks + reply attributes) | Keep | Same checks, same order, in the Access-Request handler (§5, §7) |
| `POST /api/radius/accounting/:nasIp` (fire-and-forget) | Change | Synchronous, transactional; replies only after commit |
| Disconnect/POD (`radiusDisconnectService.sendPod`) | Fix | Legacy needs `nasSessionId`, which is never persisted → kick never worked. New stores `Acct-Session-Id` and waits for ACK/NAK |
| Reply: `Session-Timeout`, `Mikrotik-Rate-Limit`, `Mikrotik-Xmit-Limit`, `Acct-Interim-Interval` | Change | See O2, O3, O4 |
| Reply: `Framed-Pool` (plan IP pool), framed IP/netmask | Drop | D13 |
| Reply-Message (Persian error texts) | Change | English only, `"CODE: message"` (O11, O20); the legacy Persian texts are not carried over (removed 2026-11-02) |
| Accumulating usage deltas from cumulative counters | Fix | Legacy ignored `Acct-*-Gigawords` (>4 GiB sessions undercounted) and could double-count if a 1 h Redis key lapsed |
| Member MAC captured from first accounting | Keep | BR-12 |
| `Accounting-On/Off` (router reboot) | Same as legacy | Ignored (acknowledged only). Owner 2026-11-01: router reboots are not handled; ghost sessions and Session-Id reuse after a reboot are accepted risks |
| Dynamic RADIUS clients (any IP, one hard-coded secret) | Change | One secret from settings, any source IP |
| `updateIp` dynamic-IP script/route; `Nas.getStatus`, ping fields | Drop | Route never existed in legacy; single router |
| 3 router vendors, AVP maps for coova/engenius | Drop | D3 (already broken server-side since Sep 2019) |

### 4.2 Member-facing login page (now router-hosted)
| Legacy | Disposition | New |
|---|---|---|
| Portal SPA hosted by us (AngularJS app, themes) | Change | A **static HTML page on the router**, edited by the admin by hand from a downloadable starter pack (D10, P3). We do not host it |
| Router redirect (`rlogin.html`) → portal; `/api/radius/loadThemeConfig` URL parsing for 4 vendors | Drop | The page lives on the router and reads router variables (`$(mac)`, `$(link-orig)`, `$(error)`, …) directly |
| `Member.signIn` pre-check (business sub, member, active, plan, window, bulk, time) | Keep | `GET /api/v1/account` (`loginAllowed`/`reason`) — **now also verifies the password** (legacy did not) (P1, Q37) |
| Browser → router `POST /login` (JSON via CORS pages) | Change | The page submits the login to the router natively (same origin, no CORS) |
| Router `status.html`/`logout.html` JSON, status screen, logout | Keep | Router-hosted status/logout pages from the pack |
| `getMemberBalance` (remaining data/time, plan info) | Keep | `GET /api/v1/account` (same call, Q37) |
| Public plans list + `assignFreePlanToMember` + `checkDefaultPlan` | Drop | O9/O10: members never choose plans; the admin assigns one (default is only a form preselection) |
| Auto-redirect after login (`afterLoginAddress`) | Change | `settings.after_login_url`, delivered by `GET /api/v1/config` |
| Post-login `GET google.com / apple.com` (OS captive-check nudge) | Keep | In the starter page script |
| MAC auto-login (`loadMemberCredentialsByMac`, `loadProfile`) | Change | `GET /api/v1/devices/{mac}/credentials`, gated by the setting (O8, P2) |
| Self-signup, SMS/voice verification, `verifyHotSpot`, forgot-password/`recoverHotspotUser`, `sendPassword` | Drop | D7/D11 (forgot password = ask admin, who can view it) |
| `createHotSpotMember`, `createForeignHotSpotMember`, `signUpCustomer`, `findHotSpotMember`, `enquiry`, `getBusinessId`, `getDailyUsage`, `logOutHotSpot` | Drop | Dead/stub/unimplemented in legacy |
| `paySubscription`, `buyPlan`, `buyBulk`, payment return routes | Drop | D7 |
| Themes (alpha, restaurant, cafe, hotel, university, market; 38 styles) | Change | Extracted as **38 independent downloadable themes** sharing one layout (D22, §9.4, §16) |
| Form-field config, pin code, Telegram/Instagram, logo/background upload | Drop | No signup, no pin code, no social links; the admin replaces `logo.png` / `background.jpg` in the downloaded folder by hand |
| Multi-language texts (fa/en) | Change | English only (O11); the page keeps its own translation table keyed by message code |

### 4.3 Admin
| Legacy | Disposition | New |
|---|---|---|
| Business login/logout, business CRUD, settings (title, timezone, theme, payping, dropbox, SMS signature) | Change | Single admin + `settings` (§6) |
| Member CRUD via REST + search/pagination | Keep | §8 |
| `createNewMember` rules (username default = mobile; random numeric password; plan; default plan) | Keep | BR-14 |
| `createMembersByGroup` + voucher print (`renderMemberCredentials`, 14/page, 2 columns, help text) | Keep | "Batches" (§8, BR-15/16) |
| `loadMember`, `loadMemberPassword` | Keep | Member detail + password reveal |
| `destroyMembersById` | Keep | Single + bulk delete (also kicks open sessions) |
| `importMemberFromCsv` | Drop | D11 |
| `sendMessageToAll`, `getSmsCostForAllMembers`, SMS cost calc | Drop | D7 |
| `loadMemberUsage` | Keep | Usage columns in member list. The list intentionally shows only username / online / status / data (owner, 2026-11-02); the blocked reason (verdict) is visible on the member page only (`data-verdict` stays on the list row as a hidden hook for tests) |
| `getAllMembersCount`, `getNewMembersCount` | Keep | Dashboard counters |
| `getMembersChart` (stub), `getDailyUsage` (throws "not implemented") | Drop | |
| `InternetPlan` CRUD (name, duration, speed, bulk, timeDuration, accessType, autoResubscribe, extraBulkPrice, price, ip pool, from/to window, type) | Change | Keeps name/duration/speed/bulk/time/autoResubscribe The from/to time-of-day window is **dropped** (Q40, D17). Drops price, extraBulkPrice, ipPoolName, bindMemberSigninToMac (D18), accessType public/private (O9), plan `type` (UI only created `dynamic`) |
| `assignPlanToMember`, `updateInternetPlanHistory` | Change | Assign keeps its effects (BR-13); history dropped (D13) |
| `resetMemberSubscription` (auto-resubscribe path) | Keep | Plus admin "Renew now" button |
| Extra bulk purchase (`buyBulk` → `extraBulk` GB) | Change | Admin sets extra bulk (units, ≥ 0, `0` clears); not allowed on unlimited-data plans (Q17) |
| `defaultInternetPlan {id, period, count, autoAssign}` | Change | Only `settings.default_plan_id` remains; period/count/autoAssign dropped (O10) |
| `ClientSession.getOnlineUsers/getOnlineSessionCount/killOnlineSession` | Keep | Online page (no dashboard counter) |
| `Nas` CRUD, `loadRadiusInfo`, `downloadScripts` | Change | "Router setup" page (O16) |
| `Usage.getUsage/getTopMembers/reportStatus`, `Business.getTrafficUsage`, dashboard charts | Change | Phase 9 (2026-11-02): daily rollup `usage_daily` + `/admin/usage` chart and top-10 list; no other reports |
| Charge/credit ledger, `buyCredit`, `adminChargeCredit`, `getBalance`, service packages (`economic`), modules, subscription expiry | Drop | D7 |
| `makeBackup`, `restoreBackup`, `restoreBackupFromApi` | Drop | O17 (dropped for now) |
| FileStorage, BigFile, Dropbox, Payping OAuth | Drop | |
| Reports + log-worker + GoFlow + sinker + KSQL + Logstash | Drop | D7 |
| Tickets, campaigns, coupons, invoices, resellers, operators, departments, themes | Drop | D7 |
| Default admin bootstrap with roles | Change | O15 |
| Sentry, bunyan | Change | `log/slog` to stdout |

### 4.4 Legacy defects: the feature stays, the bug is fixed
> **Owner direction (2026-11-01): the features behind these bugs must be kept.** Each item below is a feature that existed but was broken or half-implemented in legacy; the new system implements the feature properly. Nothing here is dropped. The plan time-of-day window (D17) was the same kind of case, but the owner explicitly **dropped** it (Q40). Bind-to-MAC was explicitly **dropped** by the owner (D18).

1. Expiry (`expiresAt`) never enforced (O7). 2. Stop events left sessions "active" for ~2 min (Redis TTL), blocking re-login in single-session mode. 3. Kick never worked (no `nasSessionId`). 4. Gigawords ignored. 5. 32-bit limit overflow / 3.9 GB "unlimited". 6. `radiusAuthorize` crashed (HTTP 500) on unknown users. 7. Auto-assign of the default plan never applied (missing `await`); the activation counter was never incremented (the counter is dropped — O10). 8. Unauthenticated methods keyed only by `memberId` (IDOR). 9. Pre-check did not verify the password. 10. Accounting errors lost (fire-and-forget). 11. Admin password reset from env on each boot. 12. SQL string interpolation in ClickHouse queries (n/a now; all SQL parameterized).

### 4.5 ClickHouse and Redis inventory (what existed, what replaces it)

Everything below was re-verified against `api/server/modules/db.js`, `sql/init.sql`, `log-worker/src/modules/*`, `config/ksql/init.sql`, the sinker task files and `cacheManager.js`.

**ClickHouse database `hotspotplus`** (created by `db.init()` at API boot):

| Object | Definition | Written by | Read by | New |
|---|---|---|---|---|
| `Session` | MergeTree, partition `toStartOfDay(creationDate)`, order `(businessId, memberId, sessionId, departmentId, nasIp, framedIpAddress, creationDate, username)`. Columns: `sessionId, businessId, memberId, nasId, departmentId, groupIdentityId, nasIp, username, framedIpAddress, mac, creationDate (the router's Event-Timestamp), download/upload/sessionTime (UInt32), accStatusType (1/2/3)`. **One row per accounting packet holding the *delta* since the previous packet** (the API subtracts the previous cumulative values before publishing to Kafka `sessionTopic`; the sinker inserts). | API `setSession` → Kafka → sinker | `Usage` view, `getActiveSessionIds`, `countSessions`, `getMemberSessions` (only in commented code), `getSessionsById`, web-proxy report join | `session` table (one row per session with *cumulative* values) + `member.used_*` counters (BR-11) |
| `Usage` | `MATERIALIZED VIEW … SummingMergeTree((sessionTime, download, upload))`, partition month, order `(businessId, memberId, departmentId, sessionId)`, `POPULATE AS SELECT * FROM Session` | (view) | `getMemberUsage` (login quota: windowed `SUM` per session, result seeds Redis), `getBusinessUsage`, `getUsageByInterval` (daily chart), `getTopMembersByUsage`, `getSessionUsage` (online list) | Quota → `member.used_*`; online list → `session`; charts/top-N dropped (D12). **Caveat:** non-key columns such as `creationDate` collapse arbitrarily when parts merge, so legacy windowing was only eventually consistent (Redis masked it). The new design is deterministic. |
| `Charge` | MergeTree, month partition, order `businessId`; `(_id UUID, businessId, type, forThe, amount UInt32, date)`. Ledger of SMS credit (`adminCharge`, `buyCharge`, `serviceCharge`, `commissionCharge`); SMS debits were negative values stored in an unsigned column. | `Charge.addCharge` | `getCharges`, `getProfileBalance` (SMS gating, reseller balance, business balance) | Dropped (SMS, payments, resellers) |
| `license.Charge` (`getLicenseBalance`, `addLicenseCharge`) | licensing ledger | — | — | Dead code; dropped |
| `WebProxy` | from Logstash syslog (9002) → Kafka `webproxy` → sinker | sinker | log-worker, joined to `Session` by `nasIp`+member IP | Dropped (reports) |
| `NetflowReport`, `DnsReport` | produced by KSQL joins of flow/DNS streams with `sessionTopic` | sinker | log-worker | Dropped (reports). If reports ever return, attribution needs `nas_ip`, `framed_ip`, `username`, `member_id` — all kept on `session` |
| `UsageView`, `Netflow`, `Dns1`, `hotspotplusLicense`, graphite `retention` blocks | experiments in `sql/init.sql` / stock server config; not created by code | — | — | Ignored |
| `system.parts` query (`getDatabaseInfo`, `Usage.reportStatus`) | dashboard "report status" widget | — | admin | Dropped |

Other verified facts: there is **no TTL/retention** (O18); `getMemberDailyUsage` is called by `Member.getDailyUsage` but **is not defined** anywhere (dead); "online" in legacy = any Start/Interim row within ±2 min of now, even if a Stop followed (new rule: BR-17); accounting time = router `Event-Timestamp` (O19).

**Redis (all replaced by SQLite rows, same semantics preserved):**

| Key / cache | TTL | Meaning | New |
|---|---|---|---|
| `usage:<memberId>` hash (`<sessionId>:download/upload/sessionTime`) | 24 h, refreshed per write; deleted on every member save | per-session delta totals used by the quota check; reseeded from `Usage` when missing | `member.used_*` (BR-11) |
| `<sessionId>` previous-usage JSON | 1 h | previous cumulative values for delta calculation; if it lapsed the next delta double-counted | `session.*_bytes/seconds` stored in the DB (fixes the double count) |
| `member:session:list:<memberId>` / `member:session:<sessionId>` | interim + 60 s | active sessions of a member (concurrency check) | BR-9 |
| `business:session:<businessId>` | interim + 60 s | online session list | BR-17 (single business, so one query) |
| `member:`, `business:username:`, `nas:`, `business:`, `internetPlan:` caches | 1 h, cleared on save | read-through caches | Not needed (SQLite is local) |

---

## 5. Business rules to preserve (with legacy source)

Use these as the spec; each is exercised by named E2E tests (§10). Legacy reference = function name in `api/common/models/*.js` unless noted.

**BR-1 Username** — trim; only `A–Z a–z 0–9`, 1–64 characters, unique **case-insensitively** (O13); anything else is rejected (no digit conversion). Legacy: `Member.createUsername/extractUsername`, `before save` hook.

**BR-2 Mobile** (optional field) — keep only ASCII digits (spaces, `+`, `-` dropped), then the **last 10 digits**; fewer than 10 digits → validation error (Phase 2, 2026-11-01). Legacy: `utility.verifyAndTrimMobile`.

**BR-3 MAC** — canonical stored form is the **RouterOS spelling: upper-case hex pairs joined by `:` (`AA:BB:CC:DD:EE:FF`)** (owner, 2026-11-02; was 12 lower-case hex). One normalizer, `domain.NormalizeMAC`: strip `:`, `-`, `.` and spaces, the rest must be exactly 12 hex digits, else validation error (member form) / ignored with a WARN (accounting); empty stays allowed. Used by the member form, accounting capture (BR-12), the `devices/{mac}/credentials` lookup (any common spelling accepted, canonicalized before the query) and the session rows; the Disconnect sends the stored value as is. No data migration (pre-release). Legacy: `utility.trimMac` (+ lower-casing in `loadProfile`).

**BR-4 Units** — speed → kbps: Kbps×1, Mbps×1000, Gbps×1,000,000. Bulk → bytes: KB×1024, MB×1024², GB×1024³. Values may be decimal. Legacy: `utility.toKbps/toByte`.

**BR-5 Validity window** — `from = member.subscription_date`; `to = from + plan.duration_days × 24 h`. **`duration_days` is a whole number ≥ 1 (owner 2026-11-01, Q15): the plan form rejects 0, negatives and empty; there is no "never expires" plan.** Legacy: `getSubscriptionDuration`.

**BR-5a Window starts at creation (owner 2026-11-01, Q22)** — `subscription_date` is set when the member is created (or renewed), not at first login; vouchers printed well ahead of use may already be past their window. Legacy behaviour kept; use `expires_at` / a later batch for "use by" control.

**BR-5b Plan edits apply to everyone (owner 2026-11-01, Q16)** — plan limits are read live at each login (no per-member snapshot). Raising or lowering data/time or shortening the duration changes the verdict for existing members at their next login (e.g. lowered data limit below current usage ⇒ `outOfBulk`; shortened duration ⇒ window lapsed, or auto-resubscribe renews). Running sessions are untouched.

**BR-6 Window lapsed** — if `now` is outside the window: if `plan.auto_resubscribe`, perform *renew* (BR-13b) then re-evaluate; else reject `noActiveSubscription`. Legacy: `getSubscriptionDuration`, `resetMemberSubscription`.

**BR-7 Remaining data** — plan bulk `0` ⇒ unlimited. Else `allowed = plan_bytes + member.extra_bulk_bytes`; `remaining = max(allowed − (used_download + used_upload), 0)`; `0` ⇒ reject `outOfBulk`. (Legacy returns "unlimited" *before* adding extra bulk — same observable result.) Legacy: `hasEnoughBulk`, `getInternetUsage`.

**BR-8 Remaining time** — plan `time_limit_minutes = 0` ⇒ unlimited; else `remaining = minutes×60 − used_seconds`; `≤ 0` ⇒ reject `outOfTime`. Legacy: `hasEnoughTime`.

**BR-9 Concurrent sessions** — a session is *active* if not ended and `now − last_update_at ≤ accounting_interval + 60 s` (mirrors the legacy Redis TTL). If active sessions of the member `≥ plan.max_sessions` (the member's plan, default 1; moved from a global setting, owner 2026-11-01, Q27) ⇒ reject `noMoreSessionAllowed`. Editing the plan's limit applies to everyone on it at their next login (BR-5b). Legacy: `radiusPostAuth`, `ClientSession.getActiveMemberSessions`, `cacheManager` TTL.

**BR-10 Reply attributes (O2; O3 and O4 changed 2026-11-04)** — **one speed per plan, used for both directions (owner 2026-11-01, Q28: kept like legacy; separate up/down speeds declined)** — `Acct-Interim-Interval = settings.accounting_interval_seconds` (default 60). `Session-Timeout` = the smallest of: remaining time-quota seconds (BR-8, only if the plan has a time limit), seconds until `expires_at` (only if set), seconds until the window end (BR-5, also for auto-renew plans); at least 1, at most 4294967295; always sent. Data (BR-7): when the plan has a data limit, `Mikrotik-Xmit-Limit` and `Mikrotik-Recv-Limit` are both the bytes left, split into the low 32 bits and `...-Gigawords` (the high 32 bits, sent only when above 0); when the plan is unlimited, no data-limit attribute is sent. `Mikrotik-Rate-Limit` only when speed > 0: let `s = round(speed in kbps)` (download = upload), burst `b = s × 4`, burst time `8`; if `b > 12,000,000` the value is just `"<s>k"` (deliberate deviation, 2026-11-01: `s` is floored at 1 so a sub-1-kbps speed sends `1k`, never legacy `0k`, which RouterOS reads as unlimited), otherwise `"<s>k/<s>k <b>k/<b>k <ceil(b/2)>k/<ceil(b/2)>k 8/8"`. **Layer 2 (server enforcement, 2026-11-04):** `domain.ShouldEnd(state, plan, now)` is true when the login rules would reject (`Evaluate`), or when the window of an auto-renew plan has ended (reason `NO_ACTIVE_SUBSCRIPTION`). `session.Enforcer` runs it after each stored Start/Interim of an open session (never for a Stop, never delaying the Accounting-Response): the job goes into a buffered queue (drop and WARN when full), 8 workers load fresh member and plan, and send the Disconnect through the kick path; on ACK the row ends with `terminate_cause = "Server: <CODE>"` (the admin Kick keeps `Admin-Reset`). One try at a time per session; after a failure no new try within 2 x the accounting interval; after 3 failed tries it gives up with a WARN. Admin edits (smaller plan, less extra data, past `expires_at`) therefore act at the member's next Interim, at most one interval later; disable and delete still kick at once. Legacy: `radiusPostAuth`, `avps.js getRateLimit`.

**BR-11 Accounting → usage** (replaces ClickHouse + Redis):
- `upload = Acct-Input-Octets (+ Acct-Input-Gigawords<<32)`; `download = Acct-Output-Octets (+ Acct-Output-Gigawords<<32)`; `seconds = Acct-Session-Time`. These are **cumulative per session**.
- On every Start/Interim/Stop for a known user: `delta = new_cumulative − stored_cumulative` (per field; if negative the counter restarted ⇒ `delta = new_cumulative`).
- `member.used_* += delta`; update the session row. This makes retransmitted packets idempotent (delta 0). Phase 9: the same non-zero delta is also upserted into `usage_daily` (member, day = date of the server receive time in `settings.timezone`) inside the same transaction.
- Usage counters are **per validity window**: they reset when the window restarts (BR-13). This reproduces the legacy behaviour where only usage *after* `subscriptionDate` counts.

**BR-12 MAC capture** — on the first accounting packet for a member whose `mac` is empty, store `Calling-Station-Id` (BR-3). Never overwritten automatically. Legacy: `setMemberMac`.

**BR-13 Plan assignment** — (a) *create / change plan*: on member creation `subscription_date = now`, counters 0, extra bulk 0. **Changing an existing member's plan only sets `plan_id`** — window start, usage counters and extra bulk are kept (owner 2026-11-01, legacy behaviour); it takes effect at the member's next login, running sessions are untouched and no Disconnect is sent. Only the *renew* action resets. Renew discards the old window's totals on the `member` row; the per-day history table `usage_daily` (Phase 9, 2026-11-02) is **not** touched by renew and survives it (this supersedes the earlier "no usage history is kept", for that table only). (b) *renew* (admin "Reset subscription" button / auto-resubscribe): `subscription_date = now`, `extra_bulk_bytes = 0`, usage counters = 0, **`last_renewed_at = now`, `renewal_count += 1`** (counts automatic and manual renewals; the member page shows "Renewed N times, last on <date> (automatic|manual)" — owner 2026-11-01, Q21; the manual/automatic label is kept in `member.last_renewal_kind` TEXT `'auto'|'manual'`). Legacy: `assignPlanToMember`, `resetMemberSubscription`.

**BR-14a Member password characters (Phase 3 review, 2026-11-01)** — a member password (create, set-password) must be valid UTF-8 and contain no control characters (< 0x20, 0x7f), because the RADIUS edge treats a `User-Password` that decrypts to such bytes as a wrong shared secret and drops it (§7.1). The form shows "No control characters.". Batch passwords are digits only.

**BR-14 Create member (admin)** — `username` default = mobile; `password` default = random 6-digit number (legacy `createRandomNumericalPassword`); `active` default true; `subscription_date = now`; plan = **required** (the form preselects `settings.default_plan_id`; creation without a plan is rejected, Q19); mobile per BR-2. Legacy: `createNewMember`.

**BR-15 Batch generation** — count `1…settings.batch_max_count`; reserve `counter..counter+count−1` atomically, persist `counter += count`; usernames `prefix + n`; random password of `voucher_password_length` digits; `active = true`; plan required; `expires_at` = local midnight of (today + `duration_days` + 1 day); all members linked to a `batch` row (label optional). Legacy: `createMembersByGroup` (note legacy required one of mobile/reserve/tour/national/passport identifiers — hotel-style, dropped).

**BR-16 Voucher print** — server-rendered A4 page(s): **14 vouchers per page, 2 columns**; each shows help text (from `settings.voucher_help`, English default; always printed left-to-right), username, password. **Reprint and single-voucher print (owner 2026-11-01, Q24):** the batch list has a Reprint button per batch and the member page a "Print voucher" button; both reuse this template and read the current decrypted password at print time (so a changed password prints the new one). Legacy: `renderMemberCredentials` + `templates/memberPrintTemplate.dust.html`.

**BR-17 Online list** — sessions with `ended_at IS NULL` and `now − last_update_at ≤ interval + 60 s`, newest first, paginated. Display (Q30): columns username, IP, MAC, started, duration, session ↓/↑, kick; page auto-refreshes every 10 s via htmx; shows "Updated by the router every N s" with N = `settings.accounting_interval_seconds`.

**BR-18 Kick** — send `Disconnect-Request` to `session.nas_ip:settings.coa_port` with `User-Name`, `Acct-Session-Id`, `Framed-IP-Address`, `Calling-Station-Id` (those known), secret from settings; wait ≤3 s (1 retry). ACK ⇒ mark session ended (`terminate_cause = Admin-Reset`); NAK/timeout ⇒ show error, leave session. **Router Stop after a kick (Phase 5, 2026-11-01):** the kicked row is ended but its status is not Stop; only the router's own **Stop**, with every cumulative figure >= the stored one (no counter reset), completes that same row, so usage is counted once and `Admin-Reset` is kept; an Interim, or a Stop with a counter reset (reused `Acct-Session-Id`), follows the normal no-open-row path (duplicate guard / new row). **Review fixes (2026-11-01):** a Disconnect-NAK with `Error-Cause` 503 (Session-Context-Not-Found) counts as success (the router no longer has the session; the row is ended `Admin-Reset`); a DB failure after an ACK is logged, not returned; a member-level kick/deactivate/delete sends its Disconnects concurrently (8 at a time); a row kick accepts any open row, so a stale ghost can be cleared by a kick. **Also triggered when the admin deactivates a member (owner 2026-11-01, grill Q14):** all the member's open sessions get a Disconnect; the member is marked inactive regardless of the result, and a failed kick is shown to the admin. This is an admin action; the server's own kicks (Phase 11a, BR-10 layer 2) use the same path with the cause `Server: <CODE>`. Legacy: `sendPod`.

**BR-19 (dropped, Q40)** — the plan time-of-day window no longer exists; the number is kept so BR-20…BR-23 don't change.

**BR-20 Admin password strength (owner 2026-11-01, revised 2026-11-04, Phase 11b)** — applies to the password page and `admin set-password`: **at least 10 characters** (runes) and **at most 72 bytes** (the bcrypt limit); not equal to the username (case-insensitive); not `admin`; not in a small embedded list of common passwords (`domain/commonpasswords.go`, compared in lower case). **No character-class rules.** Pure function `domain.ValidateAdminPassword`, one rule for UI and CLI. Member/voucher passwords are not subject to it. (Before: 16+ characters with four classes.)

**BR-21 (dropped, owner 2026-11-01)** — the server does **not** create a plan at first start; the admin creates the first plan in the web UI. `settings.default_plan_id` is NULL until the admin chooses a default; the new-member form then preselects it (if none is set, the form just asks for a plan). The number is kept so BR-22/BR-23 don't change.

**BR-22 Usage summary on admin pages (owner 2026-11-01, Q23)** — the member list and the member page show, next to the used counters: **data** `used / allowed (left)` where allowed = plan bytes + extra bulk (or "unlimited"), **time** `used / limit (left)` (or "unlimited"), **window end** (`subscription_date + duration`, in `settings.timezone`) and the verdict if it would currently be rejected (`out of data`, `out of time`, `window ended`, `expired`, `inactive`). All values come from the **same domain functions as the login check** (BR-5, BR-7, BR-8), never a second implementation. Units follow the plan's units.

**BR-23 Batch actions (owner 2026-11-01, Q25; Deactivate all / Activate all dropped in Q32)** — (a) **Rename**: edits `batch.label` only. (b) **Delete batch** runs in one transaction: kick every open session of its members first (as in E5.08), then delete the members (and their session rows) and the batch row; the confirmation dialog shows the member count and how many are currently online. A kick failure is shown but does not stop the delete. Members edited individually stay in the batch until deleted; members removed individually reduce the count shown (the stored `batch.count` is the original size).

**BR-24 Member status in the admin list (2026-11-03, owner, Phase 10 step B)** — one pure rule, `domain.MemberStatus`, with this precedence: **expired** (`expires_at` set and now strictly after it) > **inactive** (shown as "Disabled") > **expiring** (active, `expires_at` set, not expired, **3 days or less left**: exactly 3 days still counts) > **active**. Times are compared in whole seconds. The four values exclude each other and cover every member, so the chip counts add up to "All". The SQL status filter (`store.statusClause`) is the same rule and is unit-tested against the Go rule at every boundary; the list chips get their counts from one `sum(CASE …)` query under the current search and plan filter (status ignored). The member page shows the same status word. **Only the admin list and member page use it**: the login check (BR-5…BR-9, `Evaluate`) and the page API are unchanged: an expiring member still logs in.

---

## 6. Data model (SQLite)

Conventions: timestamps are **Unix seconds UTC** (`INTEGER`); booleans `INTEGER 0/1`; `PRAGMA foreign_keys=ON; journal_mode=WAL; busy_timeout=5000`; migrations are numbered `.sql` files embedded in the binary and tracked in `schema_migrations`.

**Migration rules (owner 2026-11-01, Q38):** files `internal/store/migrations/NNNN_name.sql`, applied in order, **forward-only** (no down migrations), **each in its own transaction** together with its `schema_migrations` row, applied **automatically at startup** before any listener opens; a failed migration aborts startup and leaves the DB at the previous version. An already-applied migration file is never edited (a checksum stored in `schema_migrations` is verified at startup; a mismatch refuses to start). Hand-rolled runner (~50 lines), no migration library (KISS). The schema in this section is the content of migration `0001_init.sql`; every later schema change is a new numbered file.

```sql
CREATE TABLE schema_migrations (
  version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL,
  checksum TEXT NOT NULL);                       -- sha256 of the migration file, verified at every start

CREATE TABLE admin (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  username TEXT NOT NULL,
  password_hash TEXT NOT NULL,                 -- bcrypt
  updated_at INTEGER NOT NULL);

CREATE TABLE admin_session (
  token_hash TEXT PRIMARY KEY,                 -- sha256 of the random 32-byte id sent in the cookie
  csrf_token TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL);                -- sliding: each request sets now + 3600

CREATE TABLE plan (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE COLLATE NOCASE,
  duration_days INTEGER NOT NULL CHECK (duration_days > 0),
  speed_value REAL NOT NULL CHECK (speed_value >= 0),          -- 0 = no rate limit
  speed_unit  TEXT NOT NULL CHECK (speed_unit IN ('Kbps','Mbps','Gbps')),
  bulk_value  REAL NOT NULL DEFAULT 0 CHECK (bulk_value >= 0), -- 0 = unlimited
  bulk_unit   TEXT NOT NULL DEFAULT 'MB' CHECK (bulk_unit IN ('KB','MB','GB')),
  time_limit_minutes INTEGER NOT NULL DEFAULT 0 CHECK (time_limit_minutes >= 0), -- 0 = unlimited
  max_sessions INTEGER NOT NULL DEFAULT 1 CHECK (max_sessions >= 1),    -- simultaneous devices, per plan (Q27)
  auto_resubscribe INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL);

CREATE TABLE settings (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  key_check TEXT NOT NULL DEFAULT '', -- AES-GCM encryption of a fixed test string, written at first boot; verified at every start
  site_title TEXT NOT NULL DEFAULT 'HotspotPlus',
  timezone TEXT NOT NULL DEFAULT 'UTC',
  radius_shared_secret TEXT NOT NULL,
  coa_port INTEGER NOT NULL DEFAULT 3799,
  accounting_interval_seconds INTEGER NOT NULL DEFAULT 60 CHECK (accounting_interval_seconds >= 10),
  default_plan_id INTEGER REFERENCES plan(id) ON DELETE SET NULL,
  mac_auto_login INTEGER NOT NULL DEFAULT 0,
  after_login_url TEXT NOT NULL DEFAULT '',
  batch_username_prefix TEXT NOT NULL DEFAULT 'wifi',
  batch_counter INTEGER NOT NULL DEFAULT 1360,
  batch_max_count INTEGER NOT NULL DEFAULT 30,
  voucher_password_length INTEGER NOT NULL DEFAULT 6,
  voucher_help TEXT NOT NULL DEFAULT '',            -- English default text, admin-editable (O21)
  public_base_url TEXT NOT NULL DEFAULT '',         -- how clients reach this server; baked into the router page pack (O21)
  session_retention_days INTEGER NOT NULL DEFAULT 90,  -- 0 = keep forever (O18)
  updated_at INTEGER NOT NULL);

CREATE TABLE batch (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  label TEXT NOT NULL DEFAULT '',
  plan_id INTEGER NOT NULL REFERENCES plan(id) ON DELETE RESTRICT,
  count INTEGER NOT NULL,
  duration_days INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  created_at INTEGER NOT NULL);

CREATE TABLE member (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  username TEXT NOT NULL UNIQUE COLLATE NOCASE,
  password_enc BLOB NOT NULL,                       -- AES-256-GCM: nonce || ciphertext
  active INTEGER NOT NULL DEFAULT 1,
  first_name TEXT NOT NULL DEFAULT '',
  last_name  TEXT NOT NULL DEFAULT '',
  mobile TEXT NOT NULL DEFAULT '',
  email TEXT NOT NULL DEFAULT '',
  national_code TEXT NOT NULL DEFAULT '',          -- free text, no validation (legacy had none)
  mac TEXT NOT NULL DEFAULT '', -- 'AA:BB:CC:DD:EE:FF' (BR-3) or ''
  plan_id INTEGER NOT NULL REFERENCES plan(id) ON DELETE RESTRICT,
  subscription_date INTEGER NOT NULL,
  extra_bulk_bytes INTEGER NOT NULL DEFAULT 0,
  expires_at INTEGER,                               -- NULL = never (O7)
  batch_id INTEGER REFERENCES batch(id) ON DELETE SET NULL,
  used_download_bytes INTEGER NOT NULL DEFAULT 0,   -- current validity window (BR-11)
  used_upload_bytes   INTEGER NOT NULL DEFAULT 0,
  used_seconds        INTEGER NOT NULL DEFAULT 0,
  last_renewed_at INTEGER,                          -- NULL = never renewed (Q21)
  renewal_count INTEGER NOT NULL DEFAULT 0,         -- renewals since creation, automatic + manual (Q21)
  last_renewal_kind TEXT NOT NULL DEFAULT '' CHECK (last_renewal_kind IN ('','auto','manual')),
  created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL);
CREATE INDEX member_mac  ON member(mac) WHERE mac <> '';
CREATE INDEX member_plan ON member(plan_id);

CREATE TABLE session (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  member_id INTEGER NOT NULL REFERENCES member(id) ON DELETE CASCADE,
  username TEXT NOT NULL,
  acct_session_id TEXT NOT NULL,                    -- router's Acct-Session-Id
  nas_ip TEXT NOT NULL,                             -- packet source IP (used for kick)
  nas_identifier TEXT NOT NULL DEFAULT '',
  framed_ip TEXT NOT NULL DEFAULT '',
  mac TEXT NOT NULL DEFAULT '', -- 'AA:BB:CC:DD:EE:FF' (BR-3) or ''
  status INTEGER NOT NULL CHECK (status IN (1,2,3)),-- 1 start, 2 stop, 3 interim
  started_at INTEGER NOT NULL,
  last_update_at INTEGER NOT NULL,
  ended_at INTEGER,
  terminate_cause TEXT NOT NULL DEFAULT '',
  download_bytes INTEGER NOT NULL DEFAULT 0,        -- last cumulative values from the router
  upload_bytes   INTEGER NOT NULL DEFAULT 0,
  session_seconds INTEGER NOT NULL DEFAULT 0);
CREATE INDEX session_open_member ON session(member_id) WHERE ended_at IS NULL;
CREATE INDEX session_open_key ON session(username, acct_session_id, nas_ip) WHERE ended_at IS NULL;
CREATE INDEX session_started ON session(started_at);
```

**Migration 0002 (Phase 9, 2026-11-02): `usage_daily`**
```sql
CREATE TABLE usage_daily (
  member_id INTEGER NOT NULL REFERENCES member(id) ON DELETE CASCADE,
  day TEXT NOT NULL,                  -- 'YYYY-MM-DD' in settings.timezone at receive time (sorts as text)
  down INTEGER NOT NULL DEFAULT 0, up INTEGER NOT NULL DEFAULT 0, seconds INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (member_id, day)) WITHOUT ROWID;
CREATE INDEX usage_daily_day ON usage_daily(day);
```
Rows are never pruned (about 1 row per active member per day), never touched by renew, removed with the member. If `settings.timezone` is changed later, old rows keep the day keys they were written with.

**Why usage counters live on `member`** (design choice D-A — **CONFIRMED by the owner, 2026-11-01**): it keeps the legacy semantics exactly (usage counted since `subscription_date`, resets on renew (a plan change keeps them, BR-13a), mid-session resets handled by deltas) and makes login checks O(1) instead of summing sessions. Sessions remain as per-session upserted rows (your choice) for the online list, kick and short history. The alternative — pure per-session rows summed by window — cannot attribute a session that spans a reset correctly.

**Dropped legacy fields** (member): `gender` (owner 2026-11-01, Q33; only this field was dropped — email, national code and names are kept), `fullName` (derive), `age`, `birthday/Day/Month/Year`, `verification*`, `passportNumber`, `roomNumber`, `studentGrade`, `studentId`, `groupIdentity*`, `manualVerification`, `internetPlanName`, `internetPlanHistory`, `uniqueUserId`, `passwordText`/`salt`. (plan): `price`, `extraBulkPrice`, `type`, `burst*`, `ipPoolName`, `bindMemberSigninToMac`, `accessType` (the from/to window `fromToCheck/fromHour/fromMinute/toHour/toMinute` is dropped too, Q40).

---

## 7. RADIUS specification

### 7.1 Listeners
UDP `:1812` (auth) and `:1813` (acct), addresses configurable. **Secret is read from `settings` per packet** (changes apply without restart). Any source IP is accepted if the packet verifies; Accounting-Request authenticators must be verified and bad packets dropped silently. *(Phase 3, 2026-11-01: an Access-Request carries no verifiable authenticator, so a wrong secret is detected heuristically — a PAP `User-Password` that does not decrypt to valid UTF-8 text without control characters is dropped silently with a WARN log; see `docs/phase3-notes.md`.)* Unknown/other codes are dropped.

### 7.2 Access-Request (PAP)
Inputs: `User-Name`, `User-Password` (PAP, decrypted with the secret), optional `Calling-Station-Id`, `NAS-IP-Address`, `NAS-Identifier`.
If `CHAP-Password` or `MS-CHAP-*` is present → **Access-Reject** (`Reply-Message` = unsupported method) and a WARN log (this is how we will notice if a router is actually on CHAP).

Evaluation order (first failing step rejects; message keys in §7.6):
1. member exists (case-insensitive) and password equals decrypted `password_enc` (constant-time) → else `invalidCredentials`
2. `active` → else `inactiveUser`
3. `plan_id` set → else `noPlan` *(unreachable since plans are mandatory, Q19; kept as a defensive check)*
4. plan exists → else `planRemoved` *(only reachable if deletion is allowed; kept as a safety net)*
5. `expires_at` not passed (O7) → else `expired`
6. validity window (BR-5/6) → else `noActiveSubscription`
7. remaining data > 0 (BR-7) → else `outOfBulk`
8. remaining time > 0 (BR-8) → else `outOfTime`
9. concurrent sessions (BR-9) → else `noMoreSession`

All pass ⇒ **Access-Accept** with the attributes of BR-10. (Auto-resubscribe in step 6 mutates the member before the Accept.) *(Phase 3 review, 2026-11-01:)* the renewal therefore also happens when step 9 then rejects `noMoreSession` — this is the legacy order (`checkAuthorization` renews before the session check) and is kept. Step 9 is **not atomic** with the Accept: two simultaneous logins at `max_sessions = 1` can both pass (legacy has no locking; `MONITOR:` in the evaluator). If a concurrent edit leaves the member outside the window right after the guarded renewal, the answer is a normal Reject `noActiveSubscription` (WARN log); "no reply" is reserved for database failures. An unknown username does an equivalent constant-time compare against a dummy value so timing does not reveal which usernames exist; there is no brute-force protection at the RADIUS edge (accepted).

### 7.3 Accounting-Request
`Acct-Status-Type`: 1 Start · 2 Stop · 3 Interim-Update · 7 Accounting-On · 8 Accounting-Off.
Attributes used: `User-Name`, `Acct-Session-Id`, `Acct-Input/Output-Octets`, `Acct-Input/Output-Gigawords`, `Acct-Session-Time`, `Framed-IP-Address`, `Calling-Station-Id`, `NAS-IP-Address`, `NAS-Identifier`, `Acct-Terminate-Cause`.
Session identity key = `(username, acct_session_id, packet source IP)`, matched against the **latest open** row (this replaces FreeRADIUS `acct_unique`).

| Type | Behaviour |
|---|---|
| Start | Create a new session row (status 1) — unless an open row for the key exists with `session_seconds = 0` (retransmission) → reuse. Capture MAC (BR-12). |
| Interim | Find open row, else create one (`started_at = now − Acct-Session-Time`). Apply BR-11 deltas, update row, `last_update_at = now`, status 3. After the commit, an open session is passed to the Enforcer (BR-10 layer 2): if `ShouldEnd` says so, the server sends a Disconnect (row ends `Server: <CODE>`). The Accounting-Response is never delayed. The router's own limits (layer 1) are the first line; the next login is rejected by §7.2. |
| Stop | Apply final deltas, status 2, `ended_at = now`, `terminate_cause`. A Stop/Interim with no open row whose figures are all <= the latest *finished* row of the same key is a retransmission/late duplicate and is ignored (Phase 4, 2026-11-01). |
| Start on an open row that already counted time | (Phase 4 review, 2026-11-01) The router reused the id after a reboot: the old row is closed (`ended_at = now`, `terminate_cause = Lost-Service`, counters unchanged) and a new row is created, so it cannot stay online or count toward `max_sessions` (BR-9). |
| Accounting-On/Off | (E4.11 asserts exactly this, 2026-11-01) Acknowledge with Accounting-Response, change nothing (legacy; reboot handling declined by owner, 2026-11-01). `MONITOR:` comment in code: ghost sessions after a router reboot can block `max_sessions` logins until the admin kicks or the session goes stale. |
| Unknown user | Reply Accounting-Response, change nothing (legacy "silently discarded"). |

Always respond with **Accounting-Response** after the transaction commits. A present but malformed accounting attribute drops the packet (WARN, no reply, no state change); absent ones count as 0/empty.

**Known edge cases (accepted, `MONITOR:` comments; Phase 4 review, 2026-11-01):** (1) a reordered/delayed Interim (cur < prev) is treated as a counter restart (over-counts, rewinds the stored counter); (2) a late Interim/Stop with no open row and figures above the latest finished row creates a new row and double-bills the overlap (needed for server downtime, E4.17); (3) the finished-row fallback of `LatestSession` is a full scan (no index; fix = an index migration).

### 7.4 Disconnect (CoA port)
See BR-18. `Calling-Station-Id` is the stored RouterOS-style MAC sent as is (BR-3, 2026-11-02; VERIFY-ON-HARDWARE that the router matches it). Packet code 40; expect 41 (ACK) / 42 (NAK). Router needs incoming RADIUS enabled (see §9.4, verify on hardware).

### 7.5 MikroTik vendor attributes (vendor-id 14988)
| Attribute | Type id | Use |
|---|---|---|
| Mikrotik-Rate-Limit | 8 | string, format in BR-10 |
| Mikrotik-Recv-Limit | 1 | bytes left, low 32 bits (BR-10); not sent for unlimited data |
| Mikrotik-Xmit-Limit | 2 | bytes left, low 32 bits (BR-10); not sent for unlimited data |
| Mikrotik-Recv-Limit-Gigawords | 14 | high 32 bits of the bytes left; sent only when above 0 |
| Mikrotik-Xmit-Limit-Gigawords | 15 | the same for Xmit |

*Type ids (Recv-Limit 1, Xmit-Limit 2, Rate-Limit 8, Recv-Gigawords 14, Xmit-Gigawords 15, vendor 14988) match the MikroTik dictionary shipped with `layeh.com/radius`; the semantics on a real router remain VERIFY-ON-HARDWARE (§13). `Mikrotik-Total-Limit` is not used: the MikroTik RADIUS page does not document it.*

### 7.6 Message catalog (English only — O11)
*(Phase 6, 2026-11-01: `TOO_MANY_ATTEMPTS` is page-API only (HTTP 429), never a RADIUS reject; it is in the catalog so the starter page's dictionary has it.)* Each reject has a stable **code** and an English **message**. The Page API returns both (`code`, `message`); the RADIUS `Reply-Message` is `"<CODE>: <message>"` (O20). The starter page ships this English table only (the legacy Persian texts were dropped, 2026-11-02); the admin adds a language by hand by copying the `en` block in the page script and setting `HSP_LANG`.

| Code | English message |
|---|---|
| INVALID_CREDENTIALS | Wrong username or password. |
| INACTIVE_USER | This account is disabled. |
| NO_PLAN | No internet plan is assigned to this account. |
| PLAN_REMOVED | This plan has been removed. |
| EXPIRED | This account has expired. |
| NO_ACTIVE_SUBSCRIPTION | Your plan period has ended. |
| OUT_OF_BULK | Your data allowance is used up. |
| OUT_OF_TIME | Your time allowance is used up. |
| NO_MORE_SESSION | Someone else is using this account. Try again in a few minutes. |
| UNSUPPORTED_AUTH_METHOD | This network only supports PAP login. |
| GENERAL_ERROR | Unexpected server error. Contact support or try again. |
| TOO_MANY_ATTEMPTS | Too many attempts. Try again in a minute. |


---

## 8. HTTP surface

Single listener (default `:8080`). Server-rendered HTML with htmx partials. Time shown in `settings.timezone`; storage UTC.

### 8.1 Page API v1 (public listener `HTTP_ADDR`, JSON, called by the router-hosted login page)
Redesigned along REST practice (owner 2026-11-01, Q37; legacy shape not followed). Rules:
- **Versioned, resource-style URLs**: `/api/v1/...`; reads are `GET`; no verbs in paths.
- **Credentials only in the `Authorization: Basic` header** (never in URLs, bodies or logs). The server sends **no `WWW-Authenticate` header**, so browsers never show their native login prompt.
- **Real HTTP status codes.** Errors are **RFC 9457 `application/problem+json`**: `{"type":"about:blank","title":"Unauthorized","status":401,"detail":"Wrong username or password.","code":"INVALID_CREDENTIALS"}` — `code` is the stable key the page translates (O20), `detail` is the English text. No `{"ok":…}` envelope anywhere.
- **CORS** (LAN, D9): `Access-Control-Allow-Origin: *`, `Allow-Methods: GET, OPTIONS`, `Allow-Headers: Authorization`, `Max-Age: 600`; `OPTIONS` preflight answers 204. Safe with `*` because there are no cookies.
- `Cache-Control: no-store` on `account` and `credentials`.
- **Throttle:** per source IP, failures = `401` responses on `account` (check and record are atomic per IP); default 10 per minute ⇒ `429` with `Retry-After` (seconds) and code `TOO_MANY_ATTEMPTS`.
- Unknown user, wrong password and an invalid username (BR-1) are indistinguishable: all `401 INVALID_CREDENTIALS`.

| Route | Behaviour |
|---|---|
| `GET /api/v1/account` | Basic auth (username normalized per BR-1, password verified). **`401`** wrong credentials. **`200`** with the member's status — this single call replaces the legacy pre-check *and* balance: `{"plan":{"name","speedKbps","durationDays","maxSessions"},"data":{"unlimited","allowedBytes","usedDownloadBytes","usedUploadBytes","remainingBytes"},"time":{"unlimited","allowedSeconds","usedSeconds","remainingSeconds"},"validUntil","expiresAt","loginAllowed":true|false,"reason":null|"<CODE>","message":null|"<English text>"}`. `loginAllowed`/`reason` come from checks 2–8 of §7.2 (not the session-count check) — the same domain code as the RADIUS login, so the page and the router can't disagree. A member over quota gets `200` with `loginAllowed:false, reason:"OUT_OF_BULK"` so the page can still show "0 left". |
| `GET /api/v1/devices/{mac}/credentials` | Only when `settings.mac_auto_login` is on (O8): otherwise **`403`** `DISABLED`. MAC accepted in any common spelling and canonicalized (BR-3: `AA:BB:CC:DD:EE:FF`) before the lookup. Active member with that MAC ⇒ **`200`** `{"username","password"}`; none ⇒ **`404`** `DEVICE_NOT_FOUND`. Throttled like the others. |
| `GET /api/v1/config` | `200` `{"siteTitle","afterLoginUrl"}`; read at page load so settings changes apply without re-uploading the page. |
| `GET /healthz` | `200 ok` (not versioned, not JSON). |

**Phase 6 details (2026-11-01):** (1) `account` is read-only: a member whose window has lapsed on an auto-resubscribe plan is reported as the next login will see them (renewed window and zeroed counters, `loginAllowed:true`), nothing is renewed by the GET. (2) With `unlimited:true` the `allowedBytes`/`remainingBytes` (data) and `allowedSeconds`/`remainingSeconds` (time) are `null`. (3) For `NO_PLAN` / `PLAN_REMOVED` the `plan`, `data`, `time` and `validUntil` fields are `null`. (4) `credentials`: among the ACTIVE members with that MAC (several may share one) the newest (highest id) whose login would be allowed; if none is allowed, the newest active one, so the page shows its real reject reason *(review fix, 2026-11-02)*. (5) Throttle: 10 failures (a `401` on `account`; an unknown MAC on `credentials` does NOT count, review fix 2026-11-02) per source IP within a minute lock that IP for a minute (`429`); a success does not reset the count; `config` and `healthz` are never throttled. (6) Every response of every route (also errors and `404`/`405`) carries the CORS headers; `Cache-Control: no-store` is on `account` and `credentials`. (7) Problem codes not in §7.6: `NOT_FOUND` (404), `METHOD_NOT_ALLOWED` (405), `DISABLED` (403), `DEVICE_NOT_FOUND` (404).

There is **no** server-rendered member page, cookie, language switch, plan list, status or account page; those live in the router-hosted HTML (§9.4).

### 8.2 Admin (admin listener `ADMIN_ADDR` only, D23; cookie `hsp_admin` = opaque random session id (server-side session in `admin_session`, sliding 1 h), HttpOnly, SameSite=Strict; all non-GET require `X-CSRF-Token`, supplied via `hx-headers`)
**Resource-style routes (owner 2026-11-01, Q39).** Collections are plural nouns; `GET /x` list, `GET /x/new` create form, `POST /x` create, `GET /x/{id}` detail/edit page, `PUT /x/{id}` update, `DELETE /x/{id}` delete. Things that are not CRUD are modelled as creating a sub-resource (`POST …/renewals`, `POST …/disconnections`) or are named after the thing they return (`…/voucher`). Browsers' forms only do GET/POST, so every form uses htmx (`hx-put`, `hx-delete`, `hx-post`); the admin panel requires JavaScript. Statuses: success `200`/`201` (+ `HX-Redirect` to the resulting page), validation error `422` (re-rendered form), conflict (e.g. plan in use) `409`, missing/invalid CSRF `403`, unknown id `404`, wrong verb `405`. htmx must be configured to swap `422`/`409` responses. Reusable components render the error fragments.

| Route | Behaviour |
|---|---|
| `GET/POST /admin/login`, `POST /admin/logout` | Single admin; generic error on failure; basic throttle (5 failures → 1 min). A login with `must_change_password` set lands on `/admin/password`, else on `/admin/members`. |
| `GET /admin/password`, `PUT /admin/password` | (Phase 11b) Change the admin password: `current_password`, `new_password`, `confirm_password`. `422` re-renders the form with field errors (wrong current, BR-20, mismatch); a wrong current password counts in the login throttle (`429` + `Retry-After` when locked). On success: new bcrypt hash, flag cleared, all OTHER admin sessions deleted, this one kept, a short `hsp_toast` cookie ("Password changed"), `HX-Redirect` to `/admin/members`. Linked from the top bar. While `must_change_password` is set, this page, logout and static files are the only open routes. |
| `GET /admin` | Redirects to `/admin/members` — **no dashboard page** (owner 2026-11-01, Q31: login lands on the member list; keep it simple). |
| `GET /admin/members?q&page&per&plan&status` | Search username/name/mobile; `status` ∈ all / active / **expiring** / inactive ("Disabled") / **expired** (`expires_at` passed; owner 2026-11-01, Q29; expiring = BR-24, Phase 10b). The bulk-delete bar offers **"Select all N results of this filter"** next to the page checkboxes, so one confirmation can delete every member matching the current search + filters (e.g. all expired). No automatic cleanup. Page size 10/20/40/60/80/100; columns: username, name, mobile, plan, status (active/inactive/expired), used ↓/↑/time, remaining data/time, window end, MAC, online dot (BR-22). |
| `GET /admin/members/new`, `POST /admin/members`, `GET /admin/members/{id}`, `PUT /admin/members/{id}`, `DELETE /admin/members/{id}` | Create form, create, detail/edit page, update, delete one (kicks first). Fields: username, password, names, mobile, email, national code, plan, active, MAC, expires_at. BR-1/2/3/14 validation. Changing the plan here only changes `plan_id` (BR-13a). Setting `active` to false kicks the member's open sessions (BR-18, Q14). |
| `POST /admin/members/{id}/toggle` | (Phase 10b) Disable an active member or enable a disabled one. It uses the member update service, so disabling disconnects the open sessions exactly like unticking "Active" (BR-18, kick failures are reported the same way). |
| `POST /admin/members/{id}/renewals` | BR-13b (creates a renewal; `last_renewed_at`, `renewal_count`, kind `manual`). |
| `PUT /admin/members/{id}/extra-bulk` | **Sets** `extra_bulk_bytes` to the entered amount (unit KB/MB/GB, decimal allowed, ≥ 0; `0` clears it) — owner 2026-11-01, Q17. Refused (`409`) with "This plan has unlimited data" when the member's plan has unlimited data (bulk 0). The member page shows the current value in the same units as the plan. |
| `GET /admin/members/{id}/password`, `PUT /admin/members/{id}/password` | htmx reveal (decrypted) / set a new member password. |
| `POST /admin/members/{id}/disconnections` | Disconnect all open sessions of the member (BR-18). |
| `POST /admin/members/bulk-delete` | (Phase 2: two steps on this one route — without `confirmed=1` it answers the confirmation fragment with the counts; with `confirmed=1` it deletes. Nothing ticked ⇒ `422` "No member selected", plus a client-side guard so no request is sent. `/admin/members/{id}` matches digits only, so `/admin/members/delete` is a plain 404.) Bulk delete from the member list: checkboxes + "select all on this page" + one **Delete selected** button (legacy grid parity, owner 2026-11-01, Q26); body = ids, or the filter (`q`, `plan`, `status`) for "Select all N results" (Q29: re-evaluated inside the delete transaction); confirmation shows the member count and how many are online; open sessions are kicked first (best effort, E5.08), then members and their session rows are removed in one transaction. |
| `GET /admin/batches`, `GET /admin/batches/new`, `POST /admin/batches` | BR-15. The batch list shows per batch: label, plan, created date, member count, unused count (members with all usage counters 0 and no session row), and a **Reprint** button. |
| `GET /admin/batches/{id}/vouchers`, `PUT /admin/batches/{id}`, `DELETE /admin/batches/{id}` | Print (or reprint) the batch's vouchers (BR-16, decrypted passwords read at print time); rename the label; delete the batch with its members (BR-23, Q25). |
| `GET /admin/members/{id}/voucher` | **Print voucher** for a single member: the same template as BR-16 with one voucher (Q24); button on the member page. Works for any member, batch or not. |
| `GET /admin/plans`, `GET /admin/plans/new`, `POST /admin/plans`, `GET /admin/plans/{id}`, `PUT /admin/plans/{id}`, `DELETE /admin/plans/{id}` | CRUD with validation; editing a plan used by N members shows a warning "N members use this plan; changes apply to all of them at their next login" (Q16); delete answers `409` when the plan is in use (O12) or is the default plan (clear it first). |
| `GET /admin/usage` (`?days=7\|30\|90`, default 30) | Phase 9: 30-day daily traffic chart (all members, download + upload) and top 10 members by traffic for the chosen range (D12). |
| `GET /admin/sessions`, `GET /admin/sessions/table`, `POST /admin/sessions/{id}/disconnections` | BR-17/18 (Phase 5: `/table` is the polled fragment; a failed kick answers `422` missing identifiers, `502` NAK/no answer, `409` already ended, `404` unknown; success `200` + `HX-Redirect`). Columns: username, IP, MAC, started at, duration, data used in this session (↓/↑), disconnect button. The table refreshes itself every **10 s** (htmx polling) and shows the note "Updated by the router every <accounting_interval> s" (Q30). |
| `GET /admin/settings`, `PUT /admin/settings` | All `settings` columns (secret masked, reveal button). Validation ranges as in DDL. |
| `GET /admin/router` ; `GET /admin/router/themes` ; `GET /admin/router/themes/{id}` (+ `/login.html`, `/background.jpg`, `/logo.png`: the live preview and its assets) ; `GET /admin/router/files.zip?theme={id}` | Router setup, theme gallery (38 themes grouped by 6 categories, live previews) and theme download (§9.4). Default theme (no parameter): `alpha-fall`. *(Phase 6 built the entry page with the download and gallery links and a warning when `public_base_url` is empty; Phase 8, 2026-11-02, added the setup script (E8.03) and the link status panel; the secret appears in the script only, see open-questions row 38.)* The page also shows a **router link status** panel (in-memory, resets on restart): time + source IP of the last Access-Request, Accounting packet and Disconnect result, plus counters and source IPs of packets dropped for a bad shared secret (owner 2026-11-01). |
| `GET /admin/static/*`, `GET /static/*` | htmx, css, branding override dir. |

### 8.3 CLI
`hotspotpluslight serve | admin set-password | version`.

---

## 9. Architecture and implementation notes

### 9.1 Layout
```
HotspotPlusLight/
  cmd/hotspotpluslight/        main.go (subcommands), envfile.go (--env-file), service_windows.go / service_other.go (Windows service, Phase 11c) — only calls internal/app
  internal/app/                composition root: builds config, store, services, servers; lifecycle, graceful shutdown. The ONLY place that wires concrete types; no globals, no init()
  internal/config/             env/flags
  internal/clock/              Clock interface + real and fake implementations
  internal/domain/             pure rules BR-1..BR-23 (no I/O, no clock reads) — the legacy logic lives here
  internal/store/              sqlite open, migrations/, hand-written parameterized queries; implements the repository interfaces the services declare
  internal/auth/               Evaluator: THE login check (§7.2), used by RADIUS and the JSON API; admin session service
  internal/member/             Service: create, edit, change plan, renew, extra bulk, delete (kicks first), usage summary (BR-22)
  internal/plan/               Service: CRUD, in-use checks
  internal/session/            Service: accounting (BR-11), online list, kick (BR-18), pruning
  internal/voucher/            Service: batch generation, print data (BR-15/16/23)
  internal/radius/             UDP server, handlers, MikroTik attrs, CoA client, message catalog — calls the services
  internal/web/pageapi/        JSON API v1 + CORS + throttling — calls auth/member services
  internal/web/admin/          handlers + templ components (reusable: layout, table, pagination, filter bar, form field, confirm dialog, flash)
  internal/routerpack/         starter hotspot folder (Phase 6): theme registry, shared login page layout (`layout/login.html.tmpl`, one `logo.png`), router pages (`pages/`), 38 theme manifests + backgrounds (`themes/<category>/<style>/{theme.json,background.jpg}`), zip packaging; `go:embed`
  internal/web/static/         htmx, the Inter font (vendored, pinned), our CSS (`css/`) and JS (`js/`); `static.go` bundles and hashes them at startup (D24, 2026-11-03, `docs/ui-notes.md`)
  e2e/                         black-box tests, harness, fake router
  docs/
```
**Dependency injection rules (owner 2026-11-01, Q38):**
- Every service is a struct built by `New(deps…)`; its dependencies are **small interfaces declared in the consuming package** (e.g. `member.Repo`, `session.Disconnector`, `clock.Clock`), implemented by `store` / the CoA client in production and by **hand-written fakes** in unit tests. No mocking framework, no DI container, no reflection.
- `domain` imports only the standard library. `member`, `plan`, `session`, `voucher`, `auth` import `domain` (+ `clock`) but never `web`, `radius` or `store`. `radius` and `web/*` import the services, never `store`. Only `app` imports everything.
- Handlers stay thin: parse → call a service → render/reply. One implementation per rule (e.g. the login verdict and the admin usage summary both come from `auth`/`domain`).
- Unit tests cover `domain` and each service with fakes; e2e tests drive the real assembled binary.

### 9.2 Configuration (environment, all `HSP_` prefixed)
`DATA_DIR` (db + branding), `HTTP_ADDR` (:8080, page API + `/healthz`), `ADMIN_ADDR` (:8081, admin panel — D23), `RADIUS_AUTH_ADDR` (:1812), `RADIUS_ACCT_ADDR` (:1813), `SECRET_KEY` (base64 32 bytes: AES-GCM key; **required**, losing it makes stored passwords unrecoverable), first-boot seeds: `ADMIN_USERNAME` (default `admin`; the password is `admin` until the first login changes it, O15), `RADIUS_SHARED_SECRET`; `LOG_LEVEL`; `DEV` (`1` = serve the component gallery at `/admin/dev/components`, off by default, Phase 10).

**Command line (Phase 11c, 2026-11-04).** `hotspotpluslight [--env-file <path>] serve | admin set-password [--stdin] | gen-key | service install [--dir <folder>] | uninstall | start | stop | version`. `--env-file` (for `serve` and `admin set-password`) reads `KEY=VALUE` lines (`#` comments, trimmed, optional quotes); a real environment variable wins over the file. `gen-key` prints a new `HSP_SECRET_KEY` (base64 of 32 random bytes), so `openssl` is not needed. The `service` commands work on Windows only: `install` (Administrator) creates the folder (default `C:\ProgramData\HotspotPlusLight`), writes `hsp.env` with a new key and `HSP_DATA_DIR=<folder>\data` if it is missing, and registers the auto-start service `HotspotPlusLight` that runs `serve --env-file <folder>\hsp.env`, with restart on failure. Under the service, logs and the first-start block go to `<folder>\hsp.log`. The SQLite path is turned into a safe `file:` URI by `store.DSN` (drive letters, spaces, `#`, `?`). Build: `make build-windows` (pure Go, no cgo).

### 9.3 Concurrency and time
SQLite single writer: all mutating work in short transactions; accounting handlers run synchronously per packet (fast). `Clock` is injected everywhere so tests control time. Background jobs: session pruning (daily), nothing else.

### 9.4 Router artifacts (generated from settings)
**Themes (D22, T1, T2).** The 38 legacy styles (§16) are independent themes: each is one background photo + a primary color + an overlay color applied to **one shared layout**. Every download is a complete, self-contained hotspot folder: `login.html` with the theme's two colors as CSS custom properties at the top (`--primary-color`, `--overlay-color`), `background.jpg` and a placeholder `logo.png` in the same folder (the admin replaces them by hand), plus the router pages below. Packs are generated at download time from one layout template and the theme manifest; nothing is stored in the database. Layout elements (T2): optional logo, site title text (from `/api/v1/config`, with an editable static fallback), the login form (username, password, button, error-message area) and a footer text (edited in the HTML). Not included: signup, forgot-password, language switch, pin code, social links, balance box (the balance API still exists for admins who add one). Direction left-to-right, system font stack (legacy IRANSans was for the legacy UI). Repo layout (moved in Phase 6, 2026-11-01, to its own package; the six legacy logos are byte-identical, so one shared placeholder): `internal/routerpack/layout/{login.html.tmpl,logo.png}`, `internal/routerpack/themes/<category>/<style>/{theme.json,background.jpg}`, `internal/routerpack/pages/*.html`, embedded with `go:embed`. The admin gallery lists the 38 themes grouped by the six categories with live previews; with no `theme` parameter the download is `alpha-fall` (the legacy default).

`GET /admin/router/files.zip?theme=<id>` returns the **full hotspot folder** (P3) for the admin to download, customize and upload to the router:
- `login.html` — the **customizable member page** (it carries a fixed hint line under the form: "If you were disconnected, your plan's data or time may be used up. Try logging in again to see why." — owner 2026-11-01, Q18; listed in the translation dictionary so it can be translated): static HTML + CSS + JS with no server templating. It holds a small config block (`HSP_API_BASE` = `settings.public_base_url`), an English-keyed **translation dictionary** (English only; to add a language the admin copies the `en` block by hand and sets `HSP_LANG`), and the script below. It uses RouterOS variables (`$(mac)`, `$(link-orig)`, `$(link-login-only)`, `$(error)`, `$(username)`, …), which the router substitutes when serving the file.
- *(Phase 6, 2026-11-01: the pack is `hotspot/{login,alogin,status,logout,error,redirect,rlogin}.html`, `background.jpg`, `logo.png`; the legacy JSON helpers `rstatus`/`fstatus` served only the old SPA and are not carried over; the OS captive-check nudge runs in `alogin.html`, because `login.html` is left by the native form submit.)* The small router pages for success/status/logout/redirect/error (derived from the legacy `alogin, rlogin, status, rstatus, fstatus, logout, redirect` pages), adjusted to the new flow. *The exact set and content must be finalized against RouterOS documentation and a real router (VERIFY-ON-HARDWARE).* Legacy `rlogin.html` is not carried over as is — it redirected to the external portal and put `pap=$(plain-passwd)` in the URL.

**Page script behaviour:** (1) read router variables; (2) `GET /api/v1/config`; (3) if MAC auto-login is enabled try `GET /api/v1/devices/{mac}/credentials` (404 ⇒ show the form); (4) on submit call `GET /api/v1/account` with the Basic header — `401`/`429` or `loginAllowed:false` ⇒ show the translated text for `code`/`reason` (fallback to the English `detail`/`message`); `loginAllowed:true` ⇒ submit the form natively to the router login (PAP, plaintext password); (5) the starter layout has no balance box (T2) — the status page only shows the router's own status values; the `account` response already carries the balance for admins who add one; (6) redirect to `afterLoginUrl` or the original URL; (7) fire the legacy OS captive-check nudge requests.

**Router prerequisites:** a walled-garden entry so unauthenticated clients can reach the server (`/ip hotspot walled-garden ip add dst-address=<server-ip> action=accept`); the server must be reachable at `settings.public_base_url`; HTTP only (D9).

**Setup script** (shown on `/admin/router`, copyable): `/radius add service=hotspot address=<server-ip> secret=<secret> authentication-port=1812 accounting-port=1813 timeout=5s`, `/radius incoming set accept=yes port=<coa_port>`, hotspot profile `use-radius=yes login-by=http-pap radius-accounting=yes radius-interim-update=<interval>s`, the walled-garden entry above, and instructions for uploading the folder (Files → the hotspot directory, or `html-directory` on the profile). *RouterOS syntax must be validated on your router/version (§13).*

Legacy extras removed: NTP/time-zone lines, payment-gateway walled-garden entries, `nasIdentity`.

### 9.5 Security notes
**Admin sessions (Q11 answered "no handling", then revised):** **Revised (owner 2026-11-01, Q12b): opaque server-side sessions, sliding 1 hour; no JWT** (JWT can't be revoked and has no benefit with one binary and one DB). The cookie holds a random 32-byte id; only its SHA-256 is stored in `admin_session`; each request extends `expires_at` to now+1 h; logout deletes the row; expired rows are purged opportunistically. `admin set-password` deletes all `admin_session` rows, so a CLI password change logs out every open browser (reverses the earlier "no invalidation" answer — the cost is nil with this design). The password page (Phase 11b) deletes all rows except the current session. The wrong-current-password check uses the same per-IP throttle as login. `MONITOR:` comment at the session middleware.

**No RADIUS-edge or per-username brute-force protection (owner, 2026-11-01):** 6-digit voucher passwords on a LAN are guessable; accepted. Only the page-API per-IP throttle and the admin-login throttle exist. `MONITOR:` comment at the Access-Request handler.

Passwords: AES-256-GCM with random nonce (legacy used the deprecated `crypto.createCipher`). Admin: bcrypt. Cookies signed + CSRF tokens. All SQL parameterized. CORS `*` is acceptable only because of the LAN deployment (D9); the API throttles failed credentials per source IP. Shared secret never logged. HTTP-only is accepted for the LAN (D9) — note that PAP + HTTP login means credentials are visible on that LAN segment; acceptable per D9.

---

## 10. End-to-end test strategy

**Principle:** tests drive the product only through its real interfaces: UDP RADIUS, HTTP, and the CLI. They are written before the feature code.

**Harness (`e2e/harness`)**
- Starts the whole app **in-process** on ephemeral ports with a temp data dir and a **controllable fake `Clock`** (still black-box over sockets).
- `fakenas` — a fake MikroTik: RADIUS client (`Authenticate`, `AcctStart/Interim/Stop/On/Off` with octets/gigawords/session-time) and a UDP listener answering Disconnect-Request with ACK/NAK/timeout on demand, recording every packet.
- `routerstub` — a tiny HTTP server standing in for the MikroTik hotspot pages (`/login`, `/status`, `/logout`), used to exercise the starter login page in a browser-level test (Phase 6).
- `adminclient` / `apiclient` — cookie jars, CSRF extraction, `goquery` assertions on returned HTML/partials.
- Seed helpers create plans/members through the **admin HTTP API** (not by writing SQL), so tests double as admin flow tests.

**Rules:** each test is named `E<phase>.<nn>` and references its BR/O ids; tests must pass with `go test ./... -race` without external services; a failing test blocks the phase. Browser-level automation (Playwright) is **optional** and only for the starter login page script in Phase 6.

---

## 11. Phases

Each phase: **write E2E tests first → implement → all green → phase exit**.

### Phase 0 — Skeleton and harness
Tasks: module, `cmd`, config, `clock`, store + migration runner (Q38 rules) + `0001_init.sql` from §6, service skeletons with consumer-side interfaces and fakes, composition root in `internal/app`, first-boot seeding (admin, settings with generated secret if unset; no plan is created), `/healthz`, graceful shutdown, `slog`, Makefile/CI (`go vet`, `go test -race`), the whole e2e harness and `fakenas`.
E2E first: **E0.01** boot with empty dir creates DB, admin (bcrypt of `admin`, `must_change_password=1`), settings · **E0.02** second boot keeps data (no re-seed) · **E0.03** `/healthz` · **E0.04** fakenas↔server smoke: an Access-Request gets *some* well-formed reply (Reject) · **E0.05** missing `SECRET_KEY` refuses to start · **E0.06** `SECRET_KEY` that cannot decrypt `settings.key_check` refuses to start with a clear message (data intact) · **E0.08** first start creates no plan and leaves `default_plan_id` NULL; later starts create none either · **E0.09** migrations: all migrations apply to an empty DB in order; each runs in its own transaction (a deliberately failing migration leaves the DB at the previous version and aborts startup); a second start applies nothing; editing an applied migration file is detected by checksum and refuses to start; upgrading a DB created at each earlier version keeps its data · **E0.10** layering test: `go list -deps` shows `internal/domain` imports only the standard library, no service package imports `web`/`radius`/`store`, `radius` and `web/*` don't import `store` · **E0.07** README documents backing up the `.db` file (consistent copy via `sqlite3 ... ".backup"`) and keeping `SECRET_KEY` with it.
Exit: harness usable by later phases.

### Phase 1 — Admin auth, settings, plans
E2E first: **E1.01** admin login ok/fail · **E1.02** throttle after 5 failures · **E1.03** protected pages redirect to login; successful login lands on `/admin/members` and `GET /admin` redirects there · **E1.04** CSRF rejected without token · **E1.05** (inverted in Phase 11b) the password page exists and is the only page with password inputs (`/admin/account/password` → 404) · **E1.06** `admin set-password` CLI · **E1.07** plan create/edit validation (units, negatives, duplicate name case-insensitive, `max_sessions` < 1 or non-integer rejected, `duration_days` < 1 or non-integer rejected) · **E1.08** decimal bulk/speed accepted · **E1.09** delete plan in use blocked with count (O12) · **E1.18** editing a plan used by N members shows the N-member warning (no warning at 0); after lowering data below a member's usage their next Access-Request is rejected `outOfBulk`, and after raising it they are accepted (BR-5b) · **E1.10** settings validation and persistence · **E1.11** changing the shared secret is picked up by the RADIUS listener without restart (uses fakenas, expecting reject vs. dropped) · **E1.14** first start prints the admin/admin block once and admin/admin logs in; second start prints none (O15) · **E1.15** BR-20: `admin set-password` rejects <10 characters, >72 bytes, the username (any case), a common password; accepts a lower-case phrase · **E1.16** D23: admin routes answer 404 on the public listener and page-API routes answer 404 on the admin listener; both listeners start independently · **E1.19** resource-style routes (Q39): `PUT`/`DELETE` work with a CSRF token and are `403` without; a wrong verb on a route is `405`; validation errors are `422` with the form re-rendered and field messages; a plan in use answers `409` on `DELETE`; unknown ids `404`; the old verb-style routes (`/members/delete`, `/{id}/kick`, `/{id}/renew`, `/print`) no longer exist · **E1.17** admin session: cookie is opaque (no claims), 61 minutes idle ⇒ redirected to login, activity within the hour extends it, logout invalidates the id server-side, `admin set-password` invalidates all sessions, only the hash is stored in the DB
Tasks: admin sessions/CSRF, templates + htmx, plans CRUD, settings page, domain unit conversions (BR-4).

### Phase 2 — Members (admin)
E2E first: **E2.01** create member defaults (BR-14); the plan dropdown preselects the default plan; creating a member with no plan is rejected (Q19); no plan exists ⇒ the form says to create a plan first · **E2.02** username rules: only `A–Z a–z 0–9`, 1–64 chars, case-insensitive uniqueness, non-ASCII and symbols rejected (BR-1/O13) · **E2.03** mobile rule (BR-2) · **E2.04** MAC normalization (BR-3) · **E2.13** BR-22: member page and list show data used/allowed/left (with extra bulk), time used/limit/left, window end in the settings timezone, and the would-be-rejected reason; unlimited shown as "unlimited"; the shown verdict equals the real Access-Request result for the same member (table-driven comparison) · **E2.05** password stored encrypted (raw DB bytes ≠ plaintext) yet revealable in UI · **E2.06** changing plan keeps window/extra bulk/counters and leaves running sessions alone (BR-13a); renew resets them (BR-13b) · **E2.07** renew (BR-13b) sets `last_renewed_at`, increments `renewal_count`, records `manual`; the member page shows count, date and kind; a member never renewed shows nothing · **E2.08** set extra bulk (KB/MB/GB, decimals; setting a lower value or 0 corrects it; negative rejected; refused on an unlimited-data plan or no plan, Q17) · **E2.09** search + pagination + per-page · **E2.14** status filter `expired` lists only members whose `expires_at` has passed (boundary: expired only when now is strictly after `expires_at`, matching O7 "a login after `expires_at`"; the same rule applies at login and in the list), combined with search/plan filters; "select all N results" deletes exactly the filtered set across pages, shows the count first, and leaves non-matching members untouched; no automatic deletion happens · **E2.10** bulk delete: select several rows, confirmation shows member and online counts, online members are kicked first, members and sessions are gone; nothing selected ⇒ "no member selected" message and no request · **E2.11** default plan is preselected in the form, not silently assigned (O10, Q19) · **E2.12** list shows status active/inactive/expired.
Tasks: member CRUD, domain rules, list view.

### Phase 3 — RADIUS authentication
E2E first (table-driven with fakenas): **E3.01** valid member → Accept with `Acct-Interim-Interval`, `Session-Timeout`, `Mikrotik-Rate-Limit` · **E3.02** wrong password / unknown user → same Reject message · **E3.03** inactive · **E3.04** no plan · **E3.05** window lapsed → reject; with `auto_resubscribe` → accept and window/extra-bulk/counters reset, `renewal_count` +1, kind `auto` (BR-6, Q21) · **E3.06** out of bulk; extra bulk flips it to accept (BR-7) · **E3.07** unlimited bulk ⇒ no data-limit attribute (O3, changed 2026-11-04) · **E3.08** time quota ⇒ `Session-Timeout`; exhausted ⇒ reject (BR-8) · **E3.09** `Session-Timeout` = the smallest of time left and window end (O4, changed 2026-11-04; was 172800) · **E3.10** rate-limit strings for Kbps/Mbps/Gbps and decimals, including the legacy burst format and the plain `"<s>k"` form when burst > 12,000,000 kbps (O2) · **E3.11** data cap = remaining bytes in Recv/Xmit-Limit, Gigawords above 4 GiB (O3, changed 2026-11-04; was a clamp) · **E3.12** concurrency: plan with `max_sessions=1` vs `max_sessions=2` (two members on different plans behave differently, Q27); stopped and stale sessions not counted (BR-9; needs Phase 4 helpers — written now, enabled in Phase 4) · **E3.13** CHAP/MS-CHAP request → Reject + warning · **E3.14** wrong shared secret → no reply · **E3.15** username case-insensitive · **E3.16** `expires_at` passed → reject (O7) · **E3.17** invalid username (non-ASCII/symbols) at the RADIUS edge ⇒ Reject `INVALID_CREDENTIALS` · **E3.18** DB failure → no reply (not a false Accept) ·
Tasks: `internal/radius` server, PAP, MikroTik attributes, domain evaluator (§7.2), message catalog.

### Phase 4 — Accounting and usage
E2E first: **E4.01** Start creates a session row; MAC captured once (BR-12) · **E4.02** Interim adds deltas to member counters (BR-11) · **E4.03** retransmitted Interim = no double count · **E4.04** counter restart (new < old) handled · **E4.05** Gigawords included in totals · **E4.06** Stop closes the session, final deltas applied · **E4.07** usage accumulates over multiple sessions in one window · **E4.08** renew mid-session ⇒ only later deltas count (BR-13b); plan change mid-session ⇒ counters untouched (BR-13a) · **E4.17** server downtime: interim packets lost, next interim (cumulative values) catches the counters up with no double count; no special outage handling (owner 2026-11-01) · **E4.09** Start with reused `Acct-Session-Id` after Stop creates a new row · **E4.10** unknown user ⇒ Response, no change · **E4.11** Accounting-On/Off ends open sessions · **E4.12** bad authenticator dropped · **E4.13** quota exhaustion via Interims ⇒ next Access-Request rejects `outOfBulk` · **E4.14** time exhaustion likewise · **E4.15** response sent only after commit (crash-injection hook).
Tasks: accounting handlers, session store, transaction boundaries, pruning job (O18) + test **E4.16**.

### Phase 5 — Online sessions and kick
E2E first: **E5.01** online list shows only active (not stopped/stale) · **E5.02** the member list's online dot matches the online page for the same member · **E5.12** online page shows the listed columns, the "updated every N s" note matches the setting, the table fragment endpoint is polled every 10 s (`hx-trigger="every 10s"`) and a new session appears without a full reload · **E5.03** kick sends a Disconnect with the right attributes to `nas_ip:coa_port` · **E5.04** ACK ⇒ session ended in UI · **E5.05** NAK ⇒ error shown, session stays · **E5.06** timeout ⇒ retry once, then error · **E5.07** kick all sessions of a member · **E5.08** deleting a member kicks first · **E5.09** no Disconnect is sent on an Interim while the member is under quota (changed 2026-11-04: over quota the server kicks, E11.04); a rejected login sends none · **E5.10** missing identifiers ⇒ clear error · **E5.11** deactivating a member sends a Disconnect for each open session; router NAK/timeout still leaves the member inactive and shows an error; re-activating sends nothing
Tasks: CoA client, online page, kick actions (automatic enforcement came in Phase 11a, O3/O4).

### Phase 6 — Router page pack and page API
E2E first: **E6.01** `GET /api/v1/account` with valid Basic auth ⇒ 200, `loginAllowed:true`, `reason:null` · **E6.02** each non-credential failure returns 200 with `loginAllowed:false` and its `reason` code + English `message` (inactive / no plan / plan removed / expired / validity window / data / time) · **E6.03** wrong password and unknown user both return `401` with an identical problem+json body and no `WWW-Authenticate` header · **E6.04** invalid username characters (non-ASCII, symbols) ⇒ `401 INVALID_CREDENTIALS` · **E6.05** CORS headers on every route (`Allow-Origin *`, `Allow-Headers: Authorization`) and `OPTIONS` preflight ⇒ 204 · **E6.06** the data/time figures in `account` equal the admin list (BR-7/8, BR-22), unlimited flags, and quota-exhausted members still get 200 · **E6.07** `GET /api/v1/devices/{mac}/credentials`: `403 DISABLED` when the setting is off; on ⇒ `200` credentials only for an active member, `404 DEVICE_NOT_FOUND` otherwise; MAC formats normalized (BR-3) · **E6.08** config reflects settings changes immediately · **E6.09** throttle after repeated `401`s: `429` with `Retry-After` and code `TOO_MANY_ATTEMPTS`, per source IP; resets via fake clock · **E6.10** credentials never appear in logs · **E6.11** every theme's `login.html` lint: no server template tokens left, API base baked from `public_base_url`, every message code has a dictionary entry · **E6.12** starter page script against `routerstub` + the API in a headless browser (optional, Playwright): error mapping, English fallback, native submit to the router login, after-login redirect, MAC auto-login · **E6.14** theme registry: exactly 38 themes in 6 categories, each with a manifest, a non-empty background image and parseable colors that match §16 · **E6.15** each of the 38 downloads is a valid zip whose `login.html` carries that theme's colors and background and the shared layout elements (logo, title, form, footer) · **E6.16** no `theme` parameter ⇒ `alpha-fall`; unknown id ⇒ `404` · **E6.17** gallery page lists all 38 grouped by category and each preview renders · **E6.19** error responses are `application/problem+json` with `type`, `title`, `status`, `detail`, `code`; no response contains an `ok` field; `account` and `credentials` carry `Cache-Control: no-store`; unknown routes ⇒ 404 problem+json · **E6.18** the generated `login.html` of every theme contains the disconnected-hint line (and its translation-dictionary key).
Tasks: API handlers, CORS, throttling, the page pack sources (`login.html` + router pages), the translation dictionary, `public_base_url` baking, theme registry + layout template + zip packaging, gallery pages.

### Phase 7 — Batches and vouchers
E2E first: **E7.01** create batch ⇒ N members with `prefix+counter`, counter persisted (BR-15) · **E7.02** concurrent batch creations never reuse a number · **E7.03** count > max rejected · **E7.04** `expires_at` = local midnight of today+duration+1 (timezone setting) · **E7.05** password length setting · **E7.06** print page: 14 per A4 page, 2 columns, correct page count · **E7.07** help text comes from `settings.voucher_help` (English default) and the page is left-to-right · **E7.08** every generated member authenticates over RADIUS (plan applied) · **E7.09** after `expires_at`, login rejects (O7) · **E7.10** batch list and labels. · **E7.11** batch Reprint prints the same members again with their current passwords · **E7.12** single-member Print voucher renders one voucher (username, password, help text, left-to-right) for batch and non-batch members; unknown id ⇒ 404 · **E7.13** rename changes only the label · **E7.14** delete batch kicks open sessions, removes members, sessions and the batch row in one transaction; the confirmation shows member and online counts; the list's member/unused counts follow individual deletes and usage
Tasks: voucher generator, print template (port of `memberPrintTemplate.dust.html`), batch pages.

### Phase 8 — Ops, router pack, hardening
E2E first: **E8.07** `/admin/router` status panel shows last Access-Request/Accounting/Disconnect times and sources, and counts a bad-shared-secret packet with its source IP (in-memory only, no DB writes) · **E8.03** `/admin/router` script contains secret, ports, interval and the server walled-garden entry · **E8.04** `files.zip` contains the customizable `login.html` (API base baked from `public_base_url`, no server template tokens) and the router pages, with no `plain-passwd` in any redirect· **E8.05** settings secret masked in HTML source · **E8.06** graceful shutdown completes in-flight accounting.
Tasks: router link status panel (in-memory counters fed by the RADIUS server and CoA client), README (run the single binary; example systemd unit as a README snippet only — **no Dockerfile**, owner 2026-11-01: ship the binary for now; install, router config, key/DB backup note, hardware lab checklist) (install, router config, hardware lab checklist), log format.

### Phase 11a — Enforcement in two layers (built 2026-11-04, owner decision)
E2E first: **E11.01** data limits and Gigawords split · **E11.02** unlimited data: no data-limit attribute · **E11.03** Session-Timeout is the smallest of time left, `expires_at`, window end · **E11.04** data over quota on an Interim: kick, row `Server: OUT_OF_BULK` · **E11.05** the same for time · **E11.06** `expires_at` in the past · **E11.07** auto-renew window end, next login renews · **E11.08** under quota: no kick · **E11.09** at most 3 tries, spaced by 2 x interval, then WARN; a silent router never delays the Accounting-Response · **E11.10** two sessions on one quota: both kicked · **E11.11** smaller plan: kick at the next Interim · **E11.12** Stop and unknown user never kick. Notes: `docs/phase11-notes.md`.

### Phase 11b — Default admin/admin and the password page (built 2026-11-04, owner decision)
E2E first: **E11.20** first boot: admin/admin works and is forced to the password page (pages and htmx) · **E11.21** BR-20: short, username, common, too long and a wrong confirmation give 422 · **E11.22** a wrong current password is rejected and throttled (shared with login) · **E11.23** success: other sessions ended, this one kept, flag cleared, free navigation, toast · **E11.24** an existing install (flag 0) is not forced · **E11.25** the CLI uses the new rule and clears the flag · **E11.26** top bar link, Generate button and `password.js` present, no inline code. Rewritten to the new rule: E0.01, E0.02, E0.09 (count 3), E1.01, E1.02, E1.05 (inverted), E1.06, E1.14, E1.15, E8.07b, E8.08, E8.09 (README text). Built: migration `0003_admin_must_change.sql`, `domain.ValidateAdminPassword` + `commonpasswords.go`, `auth.Service.ChangePassword` / `MustChangePassword`, `store.ChangeAdminPassword`, `internal/web/admin/password.go`, `static/js/password.js`. Notes: `docs/phase11-notes.md` (Step B).

### Phase 9 — Usage history (built 2026-11-02, owner asked for it)
E2E first: **E9.01** accounting deltas feed the day row (zero delta writes nothing, retransmission counts once, second session adds to the same row) · **E9.02** day = date in `settings.timezone` · **E9.03** Gigawords/counter-restart delta equals the member counter delta · **E9.04** same transaction (rolled-back packet leaves no row; retransmit counts once) · **E9.05** renew keeps history (supersedes BR-13 "no history", table only) · **E9.06** member delete removes only its rows · **E9.07** `/admin/usage` 30-day chart, all members summed, empty days zero · **E9.08** top 10 by download+upload for 7/30/90 days (default 30, ties by username) · **E9.09** nav link, login required. E0.09 now asserts the shipped migration count (2).
Built: migration `0002_usage_daily.sql`; `domain/usage.go` (day key, window, fill, ranges); `session.Service.Account` upserts the day row; `internal/usage` (report service); `GET /admin/usage[?days=7|30|90]`; reusable `ui.BarChart` (inline SVG, no JS). The member page does not show per-member history (skipped, not trivial).

### Phase 10 — Own CSS, new look (2026-11-03, owner asked for it)
Step A (this step): remove Tailwind; own CSS with tokens and `c-` components; themes `air` and `air-dark`; every admin page restyled; behaviour, routes and field names unchanged. E2E first: **E10.01** no inline `style`/`onclick`/`hx-on`/inline script on any admin page · **E10.02** theme cookie sets `data-theme` (bad value = `air`) · **E10.03** asset URLs carry a hash, assets are cached for a year, pages are `no-store` · **E10.04** the gallery is 404 without `HSP_DEV=1` · **E10.05** the shell keeps the menu, Theme and Log out in the top bar. Existing selectors adapted (E1.20, E2.01, E6.14, E7.06, E8.12). Notes: `docs/phase10-notes.md`. Step B is described below.

Step B (2026-11-03): **expiring soon** (BR-24, status chips All / Active / Expiring soon / Expired / Disabled with counts); the members list updates in place (search with 300 ms delay, plan, page size, chips, pager; `hx-get` + `hx-push-url`); columns User, Plan, Usage (meter), Expires, Status (+ online dot), row menu; **sheets** for new and edit member and plan; **credentials card** after a member is created from the sheet; row **⋯ menus**; `POST /admin/members/{id}/toggle`; **toasts** (`HX-Trigger`). Rule for fragments: only the `HX-Target` header decides (the list region `member-list` / `plan-list`, or the sheet body `sheet-body`); a request without it gets today's full page or redirect. A change made from the list or sheet answers with `HX-Trigger` (`members-changed` or `plans-changed`, `toast`, `close-sheet` from the sheet) and `HX-Reswap: none` instead of `HX-Redirect`; a failure from there is a toast only. E2E: **E10.10** expiring boundaries (list, filter, pill words) · **E10.11** chip counts under q/plan · **E10.12** list fragment vs full page, toolbar attributes, refresher · **E10.13** columns, hooks, row menu · **E10.14** pager in place · **E10.15** sheet fragments (new/edit member and plan) · **E10.16** 422 in the sheet keeps input · **E10.17** sheet success headers, credentials card; unchanged without the sheet target · **E10.18** toggle route · **E10.19** list/sheet actions · **E10.20** plans list, menu, blocked delete toast · **E10.21** toast area, no inline code in fragments. Selector change: E2.12 compares the status cell with the status word ("inactive" is shown as "Disabled"); store test `TestFilterSelectionAndDelete`: the member expiring exactly now is now "expiring", not "active" (BR-24). Notes: `docs/phase10-notes.md`.

---

## 12. Definition of done (whole project)
All E2E tests green on a clean checkout with no external services; hardware checklist (§13) executed on one real MikroTik and results recorded in `docs/lab-results.md`; every row in §4 either implemented or explicitly signed off as Dropped; README lets a new person install and connect a router in under 30 minutes.

---

## 13. Hardware lab checklist (cannot be proven from the repo)
1. Confirm hotspot profile `login-by` is `http-pap` (or at least PAP reaches RADIUS: Access-Request contains `User-Password`, not `CHAP-Password`).
2. Verify MikroTik VSA type ids (§7.5). **Phase 11a:** which direction `Mikrotik-Recv-Limit` and `Mikrotik-Xmit-Limit` mean (the MikroTik doc does not say; we send the same value in both, so each direction can use the full allowance and the server checks the sum); that the router honours the `-Gigawords` limits above 4 GiB; that the session ends at `Session-Timeout`. **Over-grant window:** each direction gets the full bytes left, so if the router counts the two directions separately, a member can move up to 2x the bytes left before the server (layer 2) catches it at the next Interim; measure it.
3. Rate limit string accepted and applied (`Nk/Nk`).
4. `Session-Timeout` disconnects at the expected time.
5. Interim updates arrive every `accounting_interval_seconds`; `Acct-Input/Output-*` direction matches BR-11 (upload = Input).
6. Gigawords appear in accounting after 4 GiB (usage counting).
7. `Disconnect-Request` to port 3799 works with the attribute set in BR-18; confirm which attribute set RouterOS requires. **Phase 11a:** also that a Hotspot user is disconnected by it (the automatic kicks depend on it), and that `/radius incoming` on our port (RouterOS default is 1700) answers.
8. (dropped) Router reboot handling is out of scope.
9. The starter `login.html` served from the router can call the server API (CORS, walled garden), and phones' captive-portal pop-ups run its script and allow the cross-origin requests; `$(error)` shows the RADIUS `Reply-Message`.
10. (informational) `Acct-Session-Id` reuse after reboot is an accepted risk.

---

## 14. Risks
| Risk | Mitigation |
|---|---|
| RADIUS details differ on real RouterOS | Fake router is only as good as our assumptions → lab checklist §13 gates the release |
| PAP over plain HTTP/UDP on the LAN | Accepted (D9); documented |
| SQLite write contention under many interims | 60 s interval × expected users is far below SQLite limits; WAL + short transactions; measure in Phase 4 with a load test (e.g. 500 members × 60 s) |
| Server down/restart | Accepted, no extra handling (owner 2026-11-01): no new logins during the outage, accounting gap heals on the next interim (D-A), sessions with a lost Stop go stale |
| Loss of `SECRET_KEY` | Documented in README (owner 2026-11-01: README note + startup key check, no backup feature); a wrong key is caught at startup via `settings.key_check`; if the SQLite file is copied by hand, the key must be kept with it (no built-in backup, O17) |
| Scope creep from "keep all features" | §4 is the contract; additions go to Phase 9+ |

---

## 15. Legacy code map (where to read the original behaviour)
- Auth/quota: `api/common/models/member.js` → `checkAuthorization`, `radiusPostAuth`, `radiusAuthorize`, `getSubscriptionDuration`, `hasEnoughBulk`, `hasEnoughTime`, `getInternetUsage`, `setMemberMac`, `getMemberBalance`, `checkDefaultPlan`, `createNewMember`, `createMembersByGroup`, `renderMemberCredentials`, `loadMemberCredentialsByMac`, `loadProfile`.
- Plans: `api/common/models/internetPlan.js` (`assignPlanToMember`, `assignFreePlanToMember`, `getPublicInternetPlans`); fields in `internetPlan.json`; admin form `dashboard/src/app/internetPlan/tpl/internetPlanForm.html`.
- Accounting/sessions: `api/common/models/clientSession.js` (`setSession`, `getOnlineUsers`, `killOnlineSession`), `api/common/models/usage.js` (`calculateUsage`), `api/server/modules/cacheManager.js` (TTL semantics), `api/server/modules/db.js` (ClickHouse schema/queries).
- RADIUS glue: `api/server/boot/radiusRoutes.js`, `api/server/modules/radiusAdaptor.js`, `avps.js`, `radiusMessages.js`, `radiusDisconnectService.js`; FreeRADIUS config `radius.3.0.19/` and `config/radius/`.
- Units/helpers: `api/server/modules/utility.js` (`toKbps`, `toByte`, `trimMac`, `verifyAndTrimMobile`, `create*Password`).
- Portal: `hotspot/app/controllers/base.js`, `scripts/services/{appService,routerService}.js`; router pages `api/server/modules/routers/mikrotik/html/*`, setup script `mikrotik_setup.sh`.
- Admin screens: `dashboard/src/app/{member,internetPlan,nas,widgets}/`.
- Voucher template: `api/templates/memberPrintTemplate.dust.html`.
- Themes: `api/server/modules/hotspotTemplates.js` (categories and style ids), `hotspot/app/themeStyles/<category>/<style>.less` (colors), `hotspot/app/themeStyles/alphaCommon.less` (shared layout styles), `hotspot/app/alpha/templates/alphaLayout.tpl.html` + `alphaSignIn.tpl.html` (layout markup), `hotspot/app/img/<category>/{background,logo}/` (assets).

---

## 16. Appendix — Theme catalog (extracted from legacy)

Source: `api/server/modules/hotspotTemplates.js` (styles per category), `hotspot/app/themeStyles/<category>/<style>.less` (two colors), `hotspot/app/img/<category>/background/<style>.jpg` (background), `hotspot/app/img/<category>/logo/logo.png` (placeholder logo). Theme id = `<category>-<style>`. All themes share one layout (`alphaCommon.less` in legacy). The legacy `themeStyles/customerClub` folder is a union of the same styles and is ignored.

| Theme id | Category | Style | Primary color | Overlay color | Background (legacy file, bytes) |
|---|---|---|---|---|---|
| `alpha-fall` | Alpha | Fall | `rgb(118, 24, 60)` | `rgba(118, 24, 60, 0.58)` | `img/alpha/background/fall.jpg` (50,298) |
| `alpha-space` | Alpha | Space | `rgb(89, 61, 255)` | `rgba(29, 22, 58, 0.64)` | `img/alpha/background/space.jpg` (49,615) |
| `alpha-field` | Alpha | Field | `rgb(255, 119, 56)` | `rgba(120, 60, 30, 0.58)` | `img/alpha/background/field.jpg` (50,730) |
| `restaurant-pasta` | Restaurant | Pasta | `rgb(52, 119, 13)` | `rgba(51, 55, 56, 0.5)` | `img/restaurant/background/pasta.jpg` (49,488) |
| `restaurant-pizza` | Restaurant | Pizza | `rgb(166, 57, 35)` | `rgba(116, 75, 66, 0.64)` | `img/restaurant/background/pizza.jpg` (51,394) |
| `restaurant-mushroomPizza` | Restaurant | Mushroom Pizza | `rgb(221, 48, 18)` | `rgba(0, 0, 0, 0.67)` | `img/restaurant/background/mushroomPizza.jpg` (52,172) |
| `restaurant-fastfood` | Restaurant | Fastfood | `rgb(241, 126, 49)` | `rgba(59, 45, 42, 0.4)` | `img/restaurant/background/fastfood.jpg` (51,350) |
| `restaurant-traditional` | Restaurant | Traditional | `rgb(48, 105, 84)` | `rgba(20, 36, 30, 0.5)` | `img/restaurant/background/traditional.jpg` (51,718) |
| `cafe-breakfast` | Cafe | Breakfast | `rgb(69, 99, 95)` | `rgba(58, 67, 66, 0.58)` | `img/cafe/background/breakfast.jpg` (51,251) |
| `cafe-walnut` | Cafe | Walnut | `rgb(175, 142, 122)` | `rgba(0, 0, 0, 0.5)` | `img/cafe/background/walnut.jpg` (50,712) |
| `cafe-sunflower` | Cafe | Sunflower | `rgb(209, 102, 0)` | `rgba(51, 15, 15, 0.5)` | `img/cafe/background/sunflower.jpg` (54,302) |
| `cafe-supper` | Cafe | Supper | `rgb(100, 71, 173)` | `rgba(57, 41, 41, 0.5)` | `img/cafe/background/supper.jpg` (49,843) |
| `cafe-lantern` | Cafe | Lantern | `rgb(233, 149, 82)` | `rgba(0, 0, 0, 0.5)` | `img/cafe/background/lantern.jpg` (50,910) |
| `cafe-croisan` | Cafe | Croisan | `rgb(3, 110, 186)` | `rgba(65, 77, 86, 0.47)` | `img/cafe/background/croisan.jpg` (49,692) |
| `cafe-caffeine` | Cafe | Caffeine | `rgb(169, 48, 31)` | `rgba(0, 0, 0, 0.5)` | `img/cafe/background/caffeine.jpg` (54,439) |
| `hotel-room` | Hotel | Room | `rgb(77, 19, 41)` | `rgba(71, 40, 53, 0.74)` | `img/hotel/background/room.jpg` (51,550) |
| `hotel-beach` | Hotel | Beach | `rgb(91, 149, 199)` | `rgba(0, 0, 0, 0.4)` | `img/hotel/background/beach.jpg` (49,397) |
| `hotel-bell` | Hotel | Bell | `rgb(176, 121, 25)` | `rgba(0, 0, 0, 0.38)` | `img/hotel/background/bell.jpg` (50,567) |
| `hotel-calm` | Hotel | Calm | `rgb(127, 74, 123)` | `rgba(0, 0, 0, 0.38)` | `img/hotel/background/calm.jpg` (46,865) |
| `hotel-lobby` | Hotel | Lobby | `rgb(54, 53, 71)` | `rgba(0, 0, 0, 0.38)` | `img/hotel/background/lobby.jpg` (51,602) |
| `hotel-luggage` | Hotel | Luggage | `rgb(60, 156, 135)` | `rgba(0, 0, 0, 0.5)` | `img/hotel/background/luggage.jpg` (55,869) |
| `hotel-mountains` | Hotel | Mountains | `rgb(233, 92, 37)` | `rgba(0, 0, 0, 0.3)` | `img/hotel/background/mountains.jpg` (53,083) |
| `hotel-reception` | Hotel | Reception | `rgb(220, 20, 26)` | `rgba(0, 0, 0, 0.38)` | `img/hotel/background/reception.jpg` (51,910) |
| `hotel-vacation` | Hotel | Vacation | `rgb(70, 109, 121)` | `rgba(0, 0, 0, 0.58)` | `img/hotel/background/vacation.jpg` (45,898) |
| `university-library` | University | Library | `rgb(15, 103, 200)` | `rgba(13, 14, 16, 0.45)` | `img/university/background/library.jpg` (50,373) |
| `university-drawing` | University | Drawing | `rgb(80, 127, 98)` | `rgba(0, 0, 0, 0.38)` | `img/university/background/drawing.jpg` (53,579) |
| `university-sketch` | University | Sketch | `rgb(244, 92, 36)` | `rgba(0, 0, 0, 0.38)` | `img/university/background/sketch.jpg` (51,256) |
| `university-fox` | University | Fox | `rgb(254, 108, 91)` | `rgba(0, 0, 0, 0.38)` | `img/university/background/fox.jpg` (51,415) |
| `university-notes` | University | Notes | `rgb(175, 65, 65)` | `rgba(0, 0, 0, 0.38)` | `img/university/background/notes.jpg` (51,748) |
| `university-graduation` | University | Graduation | `rgb(99, 70, 173)` | `rgba(0, 0, 0, 0.38)` | `img/university/background/graduation.jpg` (58,054) |
| `university-study` | University | Study | `rgb(0, 175, 181)` | `rgba(0, 0, 0, 0.38)` | `img/university/background/study.jpg` (51,871) |
| `market-alley` | Market | Alley | `rgb(234, 95, 24)` | `rgba(0, 0, 0, 0.4)` | `img/market/background/alley.jpg` (56,933) |
| `market-choice` | Market | Choice | `rgb(1, 92, 111)` | `rgba(0, 0, 0, 0.4)` | `img/market/background/choice.jpg` (54,917) |
| `market-autumn` | Market | Autumn | `rgb(195, 94, 1)` | `rgba(0, 0, 0, 0.4)` | `img/market/background/autumn.jpg` (50,880) |
| `market-fruitBasket` | Market | Fruit Basket | `rgb(207, 54, 14)` | `rgba(0, 0, 0, 0.4)` | `img/market/background/fruitBasket.jpg` (52,671) |
| `market-size` | Market | Size | `rgb(3, 142, 95)` | `rgba(0, 0, 0, 0.4)` | `img/market/background/size.jpg` (51,316) |
| `market-strawberries` | Market | Strawberries | `rgb(163, 7, 1)` | `rgba(0, 0, 0, 0.67)` | `img/market/background/strawberries.jpg` (56,173) |
| `market-veggies` | Market | Veggies | `rgb(91, 149, 199)` | `rgba(0, 0, 0, 0.4)` | `img/market/background/veggies.jpg` (51,152) |

Total: 38 themes, 1,966,993 bytes of background images (embedded in the binary).
