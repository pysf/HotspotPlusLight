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
 member's browser ──(login page lives on the router)    │                            │
 admin's browser  ──HTTP──► admin pages  (HTMX) ───────►│   • Admin  (one account)   │
                                                        │   • SQLite (WAL)           │
                                                        └────────────────────────────┘
```

The admin defines **internet plans**, creates **members** (one by one or in printable batches), assigns plans, and the system **enforces quota** (data, time, validity window, concurrent sessions) at login and during accounting, with a live **online-sessions** list and **kick**.

*(The member login page is a **static HTML page stored on the router**, customized by the admin from a downloadable starter pack. This project ships the pack, but does not host the page and offers no API for it (removed 2026-11-04).)*

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
| D4 | **PAP only.** Evidence: the portal posts the plaintext password to the router, and the router's `rlogin.html` redirect uses `$(plain-passwd)`. (Router-side `login-by` is not in the repo — confirmed on hardware, §13.) |
| D5 | Native Go RADIUS server (auth, accounting, Disconnect). No FreeRADIUS. |
| D6 | SQLite only. No Redis, ClickHouse, Kafka. |
| D7 | Dropped: SMS/verification, payments, invoices, coupons, campaigns, tickets, departments, resellers, operators, reports, roles/ACL/tokens, themes. |
| D8 | Admin can **view** member passwords → stored reversibly (AES-GCM), not hashed. |
| D9 | Local network deployment; plain HTTP is acceptable — **also for the admin listener; HTTPS (self-signed or own cert) was offered and declined, owner 2026-11-01.** |
| D24 | **Admin UI = our own hand-written CSS, no Tailwind, no build step (owner 2026-11-03).** Supersedes the Tailwind v4 version of 2026-11-02 (and Tabler, Bootstrap before it). Look and code rules: `docs/DESIGN.md` and `docs/FRONTEND.md`; deviations and how-to: `docs/ui-notes.md`. Plain server-rendered templ (O1 kept) + htmx + small vanilla JS modules. CSS in `internal/web/static/css/` (tokens, base, layout, one file per `c-` component, cascade layers). Go inlines the `@import`s at startup and serves every asset with a content hash in the URL and a long cache. Themes: `light` and `dark`, chosen by the `theme` cookie; with no cookie (or a bad value) `data-theme` is `auto` and the OS decides (design v2, 2026-11-09, source `docs/design/`; before that `air` and `air-dark`). System fonts only: the Inter font file was removed. Menu: the 8 places of the design (Vouchers = batches page). See Phase 10. |
| D23 | **One HTTP listener (changed by the owner, 2026-11-04; was two listeners, 2026-11-01).** The *admin* listener (`ADMIN_ADDR`, :8081) serves the admin panel (§8.2) and `/healthz` (no login, never cached). The public listener (`HTTP_ADDR`, :8080) and the page API are gone. `HSP_HTTP_ADDR` is now ignored. The admin port must not be reachable from the hotspot network (README: block it in the firewall). |
| D10 | The member login page is a **static HTML page hosted on the router** (not by us), customized by the admin by hand from a downloadable starter pack. It needs nothing from our server: the page API was removed on 2026-11-04 (Phase 12a). The router shows RADIUS rejects through `$(error)`. |
| D11 | Members are created by the admin: **one by one** and **bulk-generate (printable vouchers)**. **CSV import is dropped.** No self-signup. |
| D12 | Usage = quota enforcement + online list + kick. ~~No usage charts/reports.~~ **Superseded 2026-11-02 (owner asked for Phase 9):** a daily usage history (`usage_daily`), a daily traffic chart (7/30/90 days since 2026-11-09, drawn by `ui.Plot`) and a top-10 members list on `/admin/usage`. Still no other reports (netflow, DNS, per-session history, exports). |
| D13 | Plan features kept: extra bulk (admin grant), auto-resubscribe, an optional default plan (preselected on the new-member form; the admin creates plans in the web UI — nothing is created at startup; plans are mandatory — O10, Q19). Dropped: plan history, IP pool, per-plan burst settings (the legacy default burst is still sent — O2), hotel fields, public/private plan flag. |
| D14 | Router/RADIUS/portal config lives in a single-row **`settings`** table. |
| D15 | No data migration. |
| D16 | E2E-test-first. |
| D17 | ~~Plan time-of-day window~~ **DROPPED (owner 2026-11-01, Q40)**: the owner isn't sure it's needed; legacy never enforced it. No columns, UI, API fields, messages or tests. Re-add only on request (needs a new migration). |
| D18 | Plan **bind-to-MAC** is **dropped**. |
| D19 | **Everything is English** — admin panel, API/RADIUS messages, voucher help text. The router-hosted page holds its own translation table keyed by message code. |
| D20 | ~~Page API~~ **Removed 2026-11-04 (Phase 12a):** no `GET /api/v1/*`, no MAC auto-login lookup, no branding config endpoint. The site title is edited by hand in the page. |
| D21 | The starter pack is the **full hotspot folder** (customizable login page + the small router pages) (P3). |
| D22 | **Router page designs (rewritten 2026-11-04, Phase 12b).** The 38 legacy themes are gone. There are **8 static downloads**, each a complete `hotspot` folder: **MikroTik default** (the RouterOS pages, byte for byte from `reference/hotspot/`; a failed login shows the raw code) and **7 themes** of one new layout: `air`, `midnight`, `aurora`, `dusk`, `ripple`, `sunrise`, `ocean`. The admin edits heading, subtitle, logo and messages by hand in the files; no settings feed them. See §9.4 and §16. |
| D25 | **Dashboard home page (owner, 2026-11-07, Phases 17-19).** `GET /admin` is a dashboard, the first page after login and the first sidebar item. It shows three tiles (Online now with today's peak; Traffic today against yesterday and against the same weekday last week; Failed logins today with the top reason), the router link status panel, a short Needs attention list (members expiring in 3 days, the same rule as the Members page chip; members at 80% of their data or more) and three charts (traffic for 14 days, busy hours, daily active users). Rules: BR-25 to BR-30. The whole dashboard (tiles, router status, Needs attention, charts) refreshes by itself: one container polls one fragment every 10 s (htmx), changed 2026-11-07 (owner: "yes"; before that only the Online tile and the router status polled). SVG and templ only, no chart library (`ui.Plot`, was `ui.BarChart` before 2026-11-09). All times and day keys use `settings.timezone`. No new settings. |
| D26 | **No prices and no revenue (owner, 2026-11-07).** Plans keep no price field; the dashboard shows no income figures. |
| D27 | **Failed logins are counted (owner, 2026-11-07).** Every Access-Reject the server sends adds 1 to a daily counter by reason code (`login_failure_daily`, migration 0007). Only counts are stored: no usernames, no IP addresses. A retransmitted request is counted once (BR-27). Packets that get no reply (wrong secret, malformed) stay in the link status counters and are not counted here. The rows are pruned with `session_retention_days`. |
| D28 | **The router link status panel moves to the dashboard (owner, 2026-11-07).** The Router page keeps the setup script and the 8 page designs and no longer shows the panel. The panel is part of the dashboard fragment `GET /admin/dashboard/content` (2026-11-07: the separate `/admin/dashboard/link-status` route and its 5 s poll are gone); the whole dashboard polls it every 10 s. The panel is still in memory and starts empty after a restart. |

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
| O8 | **MAC auto-login.** | **DROPPED (owner, 2026-11-04, Phase 12a).** It needed the page API. The setting `mac_auto_login` is gone (migration 0004). The member `mac` field stays as plain data. | Done. |
| O9 | **Member plan selection at the portal.** | **RESOLVED (owner, 2026-11-01).** Members never see plans and cannot choose or activate one; they are given a plan by the admin, who alone manages plans. There is **no public signup**: the portal has a single login page (plus the post-login status/account pages). The plan `access_type` (public/private) flag is dropped. | Implement as stated. |
| O10 | **Default plan.** | **RESOLVED (owner, 2026-11-01); revised Q19:** every member **must have a plan** (no plan-less members). The admin picks one default plan in settings; it is only the **preselected value** of the plan dropdown on the new-member form (nothing is assigned silently). **Nothing is created at startup** (owner 2026-11-01): the admin creates the first plan in the web UI; until a default is chosen the form has no preselection. The activation period/count settings, the auto-assign toggle and the member activation counter are dropped (nothing triggers re-activation). The admin can change any member's plan. | Implement as stated. |
| O11 | **Portal language.** | **RESOLVED (owner, 2026-11-01): English only, everywhere** (admin panel, API, router reject messages, voucher help). The router-hosted page carries its own translation dictionary keyed by message code, shipped with English only (Persian sample removed 2026-11-02); the admin can add a language by hand. | Implement as stated. |
| O12 | **Delete a plan that members use.** | **RESOLVED — block it and show the member count (owner, 2026-11-01).** The default plan also cannot be deleted until another default is chosen. | Implement as stated. |
| O13 | **Member username rules.** | **RESOLVED (owner, 2026-11-01): case-insensitive; only English letters and digits (`A–Z a–z 0–9`), 1–64 characters.** No `.`, `_`, `@`, `-` or non-ASCII characters. Persian/Arabic digits are **not** converted — they are rejected like any other invalid character (*to confirm: members typing Persian digits will simply fail to log in*). | Implement as stated. |
| O14 | **Batch (voucher) parameters.** | **RESOLVED (owner, 2026-11-01): legacy defaults with 6-digit passwords.** Max 30 accounts per batch, usernames `wifi` + counter starting at 1360, expiry = local midnight of (today + plan duration + 1 day); all configurable in settings (`batch_max_count`, `batch_username_prefix`, `batch_counter`, `voucher_password_length` = 6). | Implement as stated. |
| O15 | **Admin password reset.** | **RESOLVED (owner, 2026-11-01, revised in grill Q9): default admin/admin, forced change at first login (revised 2026-11-04, Phase 11b; before: random password printed once, CLI only).** On the first start the server creates the admin (`ADMIN_USERNAME`, default `admin`) with the password `admin` (bcrypt) and `admin.must_change_password = 1` (migration 0003; existing installs keep 0 and are not forced). It prints a marked block (`=== HotspotPlus Lite first start ===` saying: log in with this username and password `admin`, you will be asked to set a new password; `Forgot it later? Run: hotspotpluslight admin set-password`); no `ADMIN_PASSWORD` env. While the flag is set, every admin route except `/admin/password`, `/admin/logout` and static files redirects to `/admin/password` (303 for a page, `HX-Redirect` + 403 for a change request). The password can be changed on the page `/admin/password` (current, new, confirm; show/hide; Generate) or with the CLI `admin set-password` (forgotten password). Both clear the flag. Admin passwords must meet BR-20. *(2026-11-09, design v2 step 7: the username can be changed later on the same page and on the forced screen, BR-28; `ADMIN_USERNAME` only seeds the first start.)* | Implement as stated. |
| O16 | **Router setup helper and page pack.** | **RESOLVED (owner, 2026-11-01): keep, as a full hotspot-folder download plus a generated setup script (P3).** | Implement as stated (§9.4). |
| O17 | **Backup/restore.** | **RESOLVED — dropped for now (owner, 2026-11-01).** No backup/restore feature in the first release (legacy had a JSON export/restore). Operators copy the SQLite file themselves while the service is stopped; the `SECRET_KEY` must be kept separately. | May return as a later phase. |
| O18 | **Session retention.** | **RESOLVED (owner, 2026-11-01): delete finished sessions after 90 days** (configurable via `settings.session_retention_days`, `0` = keep forever). Legacy kept everything forever; quota no longer depends on old sessions. | Implement as stated (daily prune job, test E4.16). |
| O19 | **Which clock stamps accounting records.** | **RESOLVED — server receive time (owner, 2026-11-01).** The router's `Event-Timestamp` is kept only for display/debugging. | Implement as stated. |
| O20 | **Error message format.** | **RESOLVED (owner, 2026-11-01): code + English text. CHANGED 2026-11-04 (Phase 12a): the RADIUS `Reply-Message` is only the code** (for example `OUT_OF_BULK`). The router page turns the code into text, so the admin can translate it by editing the page. The logs and the admin UI keep code plus English text. | Implement as stated (§7.6). |
| O21 | **Consequences of English-only.** | **RESOLVED (owner, 2026-11-01):** one `settings.voucher_help` (English default); the voucher print page is **always left-to-right** (no `dir` option); `member.language` is dropped; new `settings.public_base_url` (how clients reach this server, baked into the router page pack); `settings.router_login_url` is removed. | Implement as stated. |
| P1–P3 | **Router-hosted page decisions.** | **P1 and P2 REMOVED (owner, 2026-11-04, Phase 12a):** the page no longer calls our server; there is no page API. P3 stays: each download is the **full hotspot folder** (new look since Phase 12b). | See §9.4. |

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
| Reply-Message (Persian error texts) | Change | English only; the Reply-Message is only the code (O11, O20, changed 2026-11-04); the legacy Persian texts are not carried over (removed 2026-11-02) |
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
| `Member.signIn` pre-check (business sub, member, active, plan, window, bulk, time) | Keep | The RADIUS login check only. The page API pre-check was removed on 2026-11-04 (Phase 12a); the router shows the reject code |
| Browser → router `POST /login` (JSON via CORS pages) | Change | The page submits the login to the router natively (same origin, no CORS) |
| Router `status.html`/`logout.html` JSON, status screen, logout | Keep | Router-hosted status/logout pages from the pack |
| `getMemberBalance` (remaining data/time, plan info) | Drop | Was the page API `account` call. Removed 2026-11-04 (Phase 12a). The admin still sees usage in the panel |
| Public plans list + `assignFreePlanToMember` + `checkDefaultPlan` | Drop | O9/O10: members never choose plans; the admin assigns one (default is only a form preselection) |
| Auto-redirect after login (`afterLoginAddress`) | Drop | `settings.after_login_url` was removed 2026-11-04 (Phase 12a). The router page decides |
| Post-login `GET google.com / apple.com` (OS captive-check nudge) | Keep | In the starter page script |
| MAC auto-login (`loadMemberCredentialsByMac`, `loadProfile`) | Drop | Removed 2026-11-04 (O8, Phase 12a) |
| Self-signup, SMS/voice verification, `verifyHotSpot`, forgot-password/`recoverHotspotUser`, `sendPassword` | Drop | D7/D11 (forgot password = ask admin, who can view it) |
| `createHotSpotMember`, `createForeignHotSpotMember`, `signUpCustomer`, `findHotSpotMember`, `enquiry`, `getBusinessId`, `getDailyUsage`, `logOutHotSpot` | Drop | Dead/stub/unimplemented in legacy |
| `paySubscription`, `buyPlan`, `buyBulk`, payment return routes | Drop | D7 |
| Themes (alpha, restaurant, cafe, hotel, university, market; 38 styles) | Change | Replaced (Phase 12b) by **7 new themes** of one layout plus the MikroTik default pages: 8 downloads (D22, §9.4, §16) |
| Form-field config, pin code, Telegram/Instagram, logo/background upload | Drop | No signup, no pin code, no social links; the admin replaces `logo.png` / `background.jpg` in the downloaded folder by hand |
| Multi-language texts (fa/en) | Change | English only (O11); the page keeps its own translation table keyed by message code |

### 4.3 Admin
| Legacy | Disposition | New |
|---|---|---|
| Business login/logout, business CRUD, settings (title, timezone, theme, payping, dropbox, SMS signature) | Change | Single admin + `settings` (§6) |
| Member CRUD via REST + search/pagination | Keep | §8 |
| `createNewMember` rules (username default = mobile; random numeric password; plan; default plan) | Keep | BR-14 |
| `createMembersByGroup` + voucher print (`renderMemberCredentials`, 18/page, 3 columns, help text) | Keep | "Batches" (§8, BR-15/16) |
| `loadMember`, `loadMemberPassword` | Keep | Member detail + password reveal |
| `destroyMembersById` | Keep | Single + bulk delete (also kicks open sessions) |
| `importMemberFromCsv` | Drop | D11 |
| `sendMessageToAll`, `getSmsCostForAllMembers`, SMS cost calc | Drop | D7 |
| `loadMemberUsage` | Keep | Usage columns in member list. The list intentionally shows only username / online / status / data (owner, 2026-11-02); the blocked reason (verdict) is visible on the member page only (`data-verdict` stays on the list row as a hidden hook for tests) |
| `getAllMembersCount`, `getNewMembersCount` | Keep | Status chip counts on the Members page (BR-24). Not on the dashboard (D25, 2026-11-07) |
| `getMembersChart` (stub), `getDailyUsage` (throws "not implemented") | Drop | |
| `InternetPlan` CRUD (name, duration, speed, bulk, timeDuration, accessType, autoResubscribe, extraBulkPrice, price, ip pool, from/to window, type) | Change | Keeps name/duration/speed/bulk/time/autoResubscribe The from/to time-of-day window is **dropped** (Q40, D17). Drops price, extraBulkPrice, ipPoolName, bindMemberSigninToMac (D18), accessType public/private (O9), plan `type` (UI only created `dynamic`) |
| `assignPlanToMember`, `updateInternetPlanHistory` | Change | Assign keeps its effects (BR-13); history dropped (D13) |
| `resetMemberSubscription` (auto-resubscribe path) | Keep | Plus admin "Renew now" button |
| Extra bulk purchase (`buyBulk` → `extraBulk` GB) | Change | Admin sets extra bulk (units, ≥ 0, `0` clears); not allowed on unlimited-data plans (Q17) |
| `defaultInternetPlan {id, period, count, autoAssign}` | Change | Only `settings.default_plan_id` remains; period/count/autoAssign dropped (O10) |
| `ClientSession.getOnlineUsers/getOnlineSessionCount/killOnlineSession` | Keep | Online page, plus the Online now tile on the dashboard (D25, 2026-11-07) |
| `Nas` CRUD, `loadRadiusInfo`, `downloadScripts` | Change | "Router setup" page (O16) |
| `Usage.getUsage/getTopMembers/reportStatus`, `Business.getTrafficUsage`, dashboard charts | Change | Phase 9 (2026-11-02): daily rollup `usage_daily` + `/admin/usage` chart and top-10 list; no other reports. **2026-11-07 (D25):** a dashboard at `/admin` adds a 14-day traffic chart, busy hours and daily active users (BR-26, BR-28, BR-29) |
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

**BR-15 Batch generation** — count `1…settings.batch_max_count`; reserve `counter..counter+count−1` atomically, persist the counter past the last number used; usernames `prefix + n`, a name that already exists is skipped (review A2, 2026-11-04); the Settings form can only raise the counter; random password of `voucher_password_length` digits; `active = true`; plan required; `expires_at` = local midnight of (today + `duration_days` + 1 day); all members linked to a `batch` row (label optional). Legacy: `createMembersByGroup` (note legacy required one of mobile/reserve/tour/national/passport identifiers — hotel-style, dropped).

**BR-16 Voucher print** — server-rendered A4 page(s): **18 vouchers per page, 3 columns of 6 rows** (design v2, changed 2026-11-09; before it was 14 in 2 columns); each shows the business name, help text (from `settings.voucher_help`, English default; always printed left-to-right), username, password. **Reprint and single-voucher print (owner 2026-11-01, Q24):** the batch list has a Reprint button per batch and the member page a "Print voucher" button; both reuse this template and read the current decrypted password at print time (so a changed password prints the new one). Legacy: `renderMemberCredentials` + `templates/memberPrintTemplate.dust.html`.

**BR-17 Online list** — sessions with `ended_at IS NULL` and `now − last_update_at ≤ interval + 60 s`, newest first, paginated. Display (Q30): columns username, IP, MAC, started, duration, session ↓/↑, kick; page auto-refreshes every 10 s via htmx; shows "Updated by the router every N s" with N = `settings.accounting_interval_seconds`.

**BR-18 Kick** — send `Disconnect-Request` to `session.nas_ip:settings.coa_port` with `User-Name`, `Acct-Session-Id`, `Framed-IP-Address`, `Calling-Station-Id` (those known), secret from settings; wait ≤3 s (1 retry). ACK ⇒ mark session ended (`terminate_cause = Admin-Reset`); NAK/timeout ⇒ show error, leave session. **Router Stop after a kick (Phase 5, 2026-11-01):** the kicked row is ended but its status is not Stop; only the router's own **Stop**, with every cumulative figure >= the stored one (no counter reset), completes that same row, so usage is counted once and `Admin-Reset` is kept; an Interim, or a Stop with a counter reset (reused `Acct-Session-Id`), follows the normal no-open-row path (duplicate guard / new row). **Review fixes (2026-11-01):** a Disconnect-NAK with `Error-Cause` 503 (Session-Context-Not-Found) counts as success (the router no longer has the session; the row is ended `Admin-Reset`); a DB failure after an ACK is logged, not returned; a member-level kick/deactivate/delete sends its Disconnects concurrently (8 at a time); a row kick accepts any open row, so a stale ghost can be cleared by a kick. **Also triggered when the admin deactivates a member (owner 2026-11-01, grill Q14):** all the member's open sessions get a Disconnect; the member is marked inactive regardless of the result, and a failed kick is shown to the admin. This is an admin action; the server's own kicks (Phase 11a, BR-10 layer 2) use the same path with the cause `Server: <CODE>`. Legacy: `sendPod`.

**BR-19 (dropped, Q40)** — the plan time-of-day window no longer exists; the number is kept so BR-20…BR-23 don't change.

**BR-20 Admin password strength (owner 2026-11-01, revised 2026-11-04, Phase 11b)** — applies to the password page and `admin set-password`: **at least 10 characters** (runes) and **at most 72 bytes** (the bcrypt limit); not equal to the username (case-insensitive); not `admin`; not in a small embedded list of common passwords (`domain/commonpasswords.go`, compared in lower case). **No character-class rules.** Pure function `domain.ValidateAdminPassword`, one rule for UI and CLI. Member/voucher passwords are not subject to it. (Before: 16+ characters with four classes.)

**BR-21 (dropped, owner 2026-11-01)** — the server does **not** create a plan at first start; the admin creates the first plan in the web UI. `settings.default_plan_id` is NULL until the admin chooses a default; the new-member form then preselects it (if none is set, the form just asks for a plan). The number is kept so BR-22/BR-23 don't change.

**BR-22 Usage summary on admin pages (owner 2026-11-01, Q23)** — the member list and the member page show, next to the used counters: **data** `used / allowed (left)` where allowed = plan bytes + extra bulk (or "unlimited"), **time** `used / limit (left)` (or "unlimited"), **window end** (`subscription_date + duration`, in `settings.timezone`) and the verdict if it would currently be rejected (`out of data`, `out of time`, `window ended`, `expired`, `inactive`). All values come from the **same domain functions as the login check** (BR-5, BR-7, BR-8), never a second implementation. Units follow the plan's units.

**BR-23 Batch actions (owner 2026-11-01, Q25; Deactivate all / Activate all dropped in Q32)** — (a) **Rename**: edits `batch.label` only. (b) **Delete batch** runs in one transaction: kick every open session of its members first (as in E5.08), then delete the members (and their session rows) and the batch row; the confirmation dialog shows the member count and how many are currently online. A kick failure is shown but does not stop the delete. Members edited individually stay in the batch until deleted; members removed individually reduce the count shown (the stored `batch.count` is the original size).

**BR-24 Member status in the admin list (2026-11-03, owner, Phase 10 step B)** — one pure rule, `domain.MemberStatus`, with this precedence: **expired** (`expires_at` set and now strictly after it) > **inactive** (shown as "Disabled") > **expiring** (active, `expires_at` set, not expired, **3 days or less left**: exactly 3 days still counts) > **active**. Times are compared in whole seconds. The four values exclude each other and cover every member, so the chip counts add up to "All". The SQL status filter (`store.statusClause`) is the same rule and is unit-tested against the Go rule at every boundary; the list chips get their counts from one `sum(CASE …)` query under the current search and plan filter (status ignored). The member page shows the same status word. **Only the admin list and member page use it**: the login check (BR-5…BR-9, `Evaluate`) and the page API are unchanged: an expiring member still logs in.


**BR-25 Online now and peak today (owner 2026-11-07, D25)** — *Online now* is the number of online sessions of BR-17 (open, last update within interval + 60 s). It counts sessions, so one member on two devices counts twice, as on the Online page. The *presence interval* of a session starts at `started_at` and ends at `ended_at` when it is set; for an open session that is online it ends at *now*; for an open session that is stale (BR-17 fails) it ends at `last_update_at`. *Peak today* is the largest number of presence intervals that overlap at one moment since local midnight (`settings.timezone`); a session that ended earlier today counts. Pure function `domain.PeakConcurrent`. Sessions removed by pruning (O18) are gone from every figure that uses session rows.

**BR-26 Traffic compare (owner 2026-11-07, D25)** — *Traffic of a day* is `SUM(down + up)` of `usage_daily` for that day key. *Today* is shown so far (the page says so). It is compared with *yesterday* (day key minus 1) and the *same weekday last week* (day key minus 7). Change = `(today − base) / base × 100`, rounded to a whole number. When the base is 0 the page shows "no data" and no percent. Pure function `domain.PercentChange`. The 14-day chart uses the same sums and the same zero-fill as `/admin/usage`.

**BR-27 Failed logins (owner 2026-11-07, D27)** — A *failed login* is one Access-Reject that the server sends for an Access-Request: wrong username or password and every other reason of §7.6 (including `UNSUPPORTED_AUTH_METHOD`). It is counted in the one place that writes the reject, by reason code, under the day key of the receive time in `settings.timezone`. Not counted: Access-Accept; packets that get no reply (wrong secret, bad Message-Authenticator, malformed User-Password, a database error) — the link status panel counts the bad-secret ones. A *retransmission* (same source address, same Identifier and same Request Authenticator, seen within the last 60 seconds) is counted once; the router repeats a request every few seconds when it does not get the reply. The server keeps the last 1024 requests in memory for this. If the counter cannot be written, the reject is still sent and the error is logged (MONITOR). The tile shows the total for today and the *top reason* (highest count; a tie goes to the code that sorts first), with the English text from §7.6. Rows are removed with `session_retention_days` (0 = keep).

**BR-28 Admin username (owner 2026-11-09, design v2 step 7)** — the admin may change the username on the page `/admin/password` (own card, asks for the current password) and on the forced first screen (together with the new password, one transaction). Rule `domain.ValidateAdminUsername`: 4 to 32 characters, only `A-Z a-z 0-9 . -`, trimmed. The name must not equal the password (case-insensitive; BR-20 forbids it), so a rename that would make them equal is refused. Saving the current name is a no-op success (an old install with a name outside the rule is not locked out). Login compares the name exactly, as before. All other admin sessions end; this one stays. A wrong current password uses the same throttle as the password change. There is no CLI for the username.

**BR-28 Daily active members (owner 2026-11-07, D25)** — A member is *active on a day* when `usage_daily` has a row for that member and day. Rows exist only when the accounting delta is not zero (E9.01), so a login without traffic does not count. The chart shows the number of such members for each of the last 14 day keys, today included, with missing days as 0.

**BR-29 Busy hours (owner 2026-11-07, D25)** — For each local clock hour 0 to 23, the value is the average number of sessions present during that hour, over the last 14 days including today. For one day and one hour it is the total overlap in seconds of all presence intervals (BR-25) with that hour, divided by 3600. An hour that has not started yet today is left out (it does not count as a day with 0). The hour that is running now counts only for the part that has passed (2026-11-07, review): at 14:05 the 14:00 period is 5 minutes of observation, not a full hour, so the bar is not pulled down. The value for the hour of day is the total presence divided by the total hours observed (a full past hour is 1 hour, the running one is the elapsed part); one decimal. A new install with fewer days of data uses only the days that exist (from the day of the oldest kept session). Pure function `domain.HourlyAverage`. On a day with a clock change the repeated or skipped hour just has more or fewer periods; tests use `UTC` and `Asia/Tehran`.

**BR-30 Needs attention (owner 2026-11-07, D25)** — Two short lists, at most 5 rows each, with the full count in the heading. (a) *Expiring*: active members with `expires_at` set, not expired, and `expires_at` at most 3 days after now (exactly 3 days counts; owner 2026-11-07: 3 days, not 7); soonest first. (b) *Near data limit*: active, not expired members on a plan with a data limit, whose used data (down + up) is at least 80% of the allowed bytes (plan bytes + extra bulk, the same function as BR-22); integer rule `used * 100 >= allowed * 80`; 100% or more is labelled "out of data"; highest percent first. Plans with unlimited data and disabled or expired members never appear. BR-24 and this list use one constant, `domain.ExpiringDays` (3), and one function, `domain.ExpiresWithin(now, expiresAt, days)`, so there is one rule and one implementation. The Members chip and the dashboard list always agree. Each row links to the member page.

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
  business_name TEXT NOT NULL DEFAULT '',            -- shown on vouchers, admin header, tab title when not empty (Phase 14, 2026-11-04; was site_title)
  timezone TEXT NOT NULL DEFAULT 'UTC',
  radius_shared_secret TEXT NOT NULL,
  coa_port INTEGER NOT NULL DEFAULT 3799,
  accounting_interval_seconds INTEGER NOT NULL DEFAULT 60 CHECK (accounting_interval_seconds >= 10),
  default_plan_id INTEGER REFERENCES plan(id) ON DELETE SET NULL,
  batch_username_prefix TEXT NOT NULL DEFAULT 'wifi',
  batch_counter INTEGER NOT NULL DEFAULT 1360,
  batch_max_count INTEGER NOT NULL DEFAULT 30,
  voucher_password_length INTEGER NOT NULL DEFAULT 6,
  voucher_help TEXT NOT NULL DEFAULT '',            -- English default text, admin-editable (O21)
  session_retention_days INTEGER NOT NULL DEFAULT 90,  -- 0 = keep forever (O18)
  -- Phase 12a (migration 0004) dropped the three columns mac_auto_login, after_login_url and public_base_url (they no longer exist in the table).
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

**Migration 0008 (review A7, 2026-11-04; was 0006 before the merge with the dashboard work): full session indexes**
```sql
CREATE INDEX session_member ON session(member_id);
CREATE INDEX session_key ON session(username, acct_session_id, nas_ip);
CREATE INDEX session_prune ON session(COALESCE(ended_at, last_update_at));
```
They serve the finished-row lookup, member delete, the unused-member check and pruning. `PruneSessions` deletes in chunks of 1000. `session_prune` is rewritten on each Interim; `make perf-quick` (500 and 1000 online users) showed no change in latency with or without it (2026-11-04), so it stays.

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

**Migration 0007 (Phase 17, 2026-11-07): `login_failure_daily`**
```sql
CREATE TABLE login_failure_daily (
  day TEXT NOT NULL,                  -- 'YYYY-MM-DD' in settings.timezone at receive time, like usage_daily
  reason TEXT NOT NULL,               -- a code of section 7.6, for example INVALID_CREDENTIALS
  count INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (day, reason)) WITHOUT ROWID;
```
One row per day and reason, written with an upsert (`count = count + 1`). No usernames, no IP addresses, no foreign keys. Pruned with the sessions: rows whose day is older than the cutoff day are deleted; `session_retention_days = 0` keeps everything. A new timezone does not rewrite old rows (same as `usage_daily`). The dashboard needs no other new storage: it reads `session`, `usage_daily`, `member`, `plan` and `login_failure_daily`.

**Why usage counters live on `member`** (design choice D-A — **CONFIRMED by the owner, 2026-11-01**): it keeps the legacy semantics exactly (usage counted since `subscription_date`, resets on renew (a plan change keeps them, BR-13a), mid-session resets handled by deltas) and makes login checks O(1) instead of summing sessions. Sessions remain as per-session upserted rows (your choice) for the online list, kick and short history. The alternative — pure per-session rows summed by window — cannot attribute a session that spans a reset correctly.

**Dropped legacy fields** (member): `gender` (owner 2026-11-01, Q33; only this field was dropped — email, national code and names are kept), `fullName` (derive), `age`, `birthday/Day/Month/Year`, `verification*`, `passportNumber`, `roomNumber`, `studentGrade`, `studentId`, `groupIdentity*`, `manualVerification`, `internetPlanName`, `internetPlanHistory`, `uniqueUserId`, `passwordText`/`salt`. (plan): `price`, `extraBulkPrice`, `type`, `burst*`, `ipPoolName`, `bindMemberSigninToMac`, `accessType` (the from/to window `fromToCheck/fromHour/fromMinute/toHour/toMinute` is dropped too, Q40).

---

## 7. RADIUS specification

### 7.1 Listeners
UDP `:1812` (auth) and `:1813` (acct), addresses configurable. **Secret is read from `settings` per packet** (changes apply without restart). Any source IP is accepted if the packet verifies; Accounting-Request authenticators must be verified and bad packets dropped silently. *(Phase 3, 2026-11-01: an Access-Request carries no verifiable authenticator, so a wrong secret is detected heuristically — a PAP `User-Password` that does not decrypt to valid UTF-8 text without control characters is dropped silently with a WARN log; see `docs/phase3-notes.md`.)* Unknown/other codes are dropped. *(2026-11-04: a wildcard address `:1812` or `0.0.0.0:1812` opens an IPv4-only socket so each reply leaves from the address the request was sent to; `[::]:1812` gives dual stack without that fix. A request with a wrong Message-Authenticator is dropped, logged and counted as a wrong secret; a forged packet can raise a false alarm, which the owner accepts.)*

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
See BR-18. `Calling-Station-Id` is the stored RouterOS-style MAC sent as is (BR-3, 2026-11-02). Packet code 40; expect 41 (ACK) / 42 (NAK). Router needs incoming RADIUS enabled (see §9.4).

### 7.5 MikroTik vendor attributes (vendor-id 14988)
| Attribute | Type id | Use |
|---|---|---|
| Mikrotik-Rate-Limit | 8 | string, format in BR-10 |
| Mikrotik-Recv-Limit | 1 | bytes left, low 32 bits (BR-10); not sent for unlimited data |
| Mikrotik-Xmit-Limit | 2 | bytes left, low 32 bits (BR-10); not sent for unlimited data |
| Mikrotik-Recv-Limit-Gigawords | 14 | high 32 bits of the bytes left; sent only when above 0 |
| Mikrotik-Xmit-Limit-Gigawords | 15 | the same for Xmit |

*Type ids (Recv-Limit 1, Xmit-Limit 2, Rate-Limit 8, Recv-Gigawords 14, Xmit-Gigawords 15, vendor 14988) match the MikroTik dictionary shipped with `layeh.com/radius`; checked on a real router (§13, 2026-11-09). `Mikrotik-Total-Limit` is not used: the MikroTik RADIUS page does not document it.*

### 7.6 Message catalog (English only — O11)
*(Phase 12, 2026-11-04: `TOO_MANY_ATTEMPTS` and `GENERAL_ERROR` were removed; nothing sent them. The page API is gone and the RADIUS `Reply-Message` is now only the `<CODE>` (O20 updated). The router pages map codes to text in their MESSAGES block.)* Each reject has a stable **code** and an English **message** (kept in the server log and the admin UI).

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


---

## 8. HTTP surface

Single listener (default `:8081`, D23). Server-rendered HTML with htmx partials. Time shown in `settings.timezone`; storage UTC.

### 8.1 (removed 2026-11-04, Phase 12a)
The page API (`/api/v1/account`, `/api/v1/devices/{mac}/credentials`, `/api/v1/config`, CORS, the throttle) and the public listener are gone. The only non-admin route is `GET /healthz` (`200 ok`, no login, `Cache-Control: no-store`), now served by the admin listener. `/api/v1/*` is a plain 404.

There is **no** server-rendered member page, cookie, language switch, plan list, status or account page; those live in the router-hosted HTML (§9.4).

### 8.2 Admin (admin listener `ADMIN_ADDR` only, D23; cookie `hsp_admin` = opaque random session id (server-side session in `admin_session`, sliding 1 h), HttpOnly, SameSite=Strict; all non-GET require `X-CSRF-Token`, supplied via `hx-headers`)
**Resource-style routes (owner 2026-11-01, Q39).** Collections are plural nouns; `GET /x` list, `GET /x/new` create form, `POST /x` create, `GET /x/{id}` detail/edit page, `PUT /x/{id}` update, `DELETE /x/{id}` delete. Things that are not CRUD are modelled as creating a sub-resource (`POST …/renewals`, `POST …/disconnections`) or are named after the thing they return (`…/voucher`). Browsers' forms only do GET/POST, so every form uses htmx (`hx-put`, `hx-delete`, `hx-post`); the admin panel requires JavaScript. Statuses: success `200`/`201` (+ `HX-Redirect` to the resulting page), validation error `422` (re-rendered form), conflict (e.g. plan in use) `409`, missing/invalid CSRF `403`, unknown id `404`, wrong verb `405`. htmx must be configured to swap `422`/`409` responses. Reusable components render the error fragments.

| Route | Behaviour |
|---|---|
| `GET/POST /admin/login`, `POST /admin/logout` | Single admin; generic error on failure; basic throttle (5 failures → 1 min). A login with `must_change_password` set lands on `/admin/password`, else on `/admin` (the dashboard, D25). |
| `GET /admin/password`, `PUT /admin/password` | (Phase 11b; page named "Password and username" from design v2 step 7, 2026-11-09) Change the admin password: `current_password`, `new_password`, `confirm_password`. `422` re-renders the form with field errors (wrong current, BR-20, mismatch); a wrong current password counts in the login throttle (`429` + `Retry-After` when locked). On success: new bcrypt hash, flag cleared, all OTHER admin sessions deleted, this one kept, a short `hsp_toast` cookie ("Password changed"), `HX-Redirect` to `/admin/members`. Linked from the top bar (label "Password"). On the forced first screen the form also has a `username` field, pre-filled with the current name: a different valid name renames the admin in the same transaction as the password (BR-28); a bad name gives `422` and nothing changes. While `must_change_password` is set, this page, logout and static files are the only open routes. |
| `PUT /admin/password/username` | (Design v2 step 7, 2026-11-09, BR-28) Change the admin username: `username`, `username_current_password`. `422` for a wrong current password (counted in the login throttle, `429` when locked) or a bad name; success: name stored, all OTHER admin sessions deleted, this one kept, toast "Username changed", `HX-Redirect` to `/admin/password`. Same name = no-op success. Closed while `must_change_password` is set. |
| `GET /admin` | **The dashboard (owner 2026-11-07, D25; before: a redirect to `/admin/members`, Q31).** Tiles (BR-25, BR-26, BR-27), the router link status panel (D28), Needs attention (BR-30), and three charts (BR-26, BR-29, BR-28). Needs a login; `no-store`. It answers 200 with empty states on a new install. |
| `GET /admin/dashboard/content` | Everything under the dashboard title as a fragment: three tiles, router status, Needs attention and the three charts (same login group as the page, `no-store`). The dashboard container polls it every 10 s with htmx (`hx-get`, `hx-trigger="every 10s"`, `hx-swap="outerHTML"`). Replaces `/admin/dashboard/online` and `/admin/dashboard/link-status` (2026-11-07). |
| `GET /admin/members?q&page&per&plan&status` | Search username/name/mobile; `status` ∈ all / active / **expiring** / inactive ("Disabled") / **expired** (`expires_at` passed; owner 2026-11-01, Q29; expiring = BR-24, Phase 10b). The bulk-delete bar offers **"Select all N results of this filter"** next to the page checkboxes, so one confirmation can delete every member matching the current search + filters (e.g. all expired). No automatic cleanup. Page size 10/20/40/60/80/100; columns: username, name, mobile, plan, status (active/inactive/expired), used ↓/↑/time, remaining data/time, window end, MAC, online dot (BR-22). |
| `GET /admin/members/new`, `POST /admin/members`, `GET /admin/members/{id}`, `PUT /admin/members/{id}`, `DELETE /admin/members/{id}` | Create form, create, detail/edit page, update, delete one (kicks first). Fields: username, password, names, mobile, email, national code, plan, active, MAC, expires_at. BR-1/2/3/14 validation. Changing the plan here only changes `plan_id` (BR-13a). Setting `active` to false kicks the member's open sessions (BR-18, Q14). |
| `POST /admin/members/{id}/toggle` | (Phase 10b) Disable an active member or enable a disabled one. It uses the member update service, so disabling disconnects the open sessions exactly like unticking "Active" (BR-18, kick failures are reported the same way). |
| `POST /admin/members/{id}/renewals` | BR-13b (creates a renewal; `last_renewed_at`, `renewal_count`, kind `manual`). |
| `PUT /admin/members/{id}/extra-bulk` | **Sets** `extra_bulk_bytes` to the entered amount (unit KB/MB/GB, decimal allowed, ≥ 0; `0` clears it) — owner 2026-11-01, Q17. Refused (`409`) with "This plan has unlimited data" when the member's plan has unlimited data (bulk 0). The member page shows the current value in the same units as the plan. |
| `GET /admin/members/{id}/password`, `PUT /admin/members/{id}/password` | htmx reveal (decrypted) / set a new member password. |
| `POST /admin/members/{id}/disconnections` | Disconnect all open sessions of the member (BR-18). |
| `POST /admin/members/bulk-delete` | (Phase 2: two steps on this one route — without `confirmed=1` it answers the confirmation fragment with the counts; with `confirmed=1` it deletes. Nothing ticked ⇒ `422` "No member selected", plus a client-side guard so no request is sent. `/admin/members/{id}` matches digits only, so `/admin/members/delete` is a plain 404.) Bulk delete from the member list: checkboxes + "select all on this page" + one **Delete selected** button (legacy grid parity, owner 2026-11-01, Q26); body = ids, or the filter (`q`, `plan`, `status`) for "Select all N results" (Q29: re-evaluated inside the delete transaction); confirmation shows the member count and how many are online; open sessions are kicked first (best effort, E5.08), then members and their session rows are removed in one transaction. |
| `GET /admin/batches`, `GET /admin/batches/new`, `POST /admin/batches` | BR-15. The batch list shows per batch: label, plan, created date, member count, unused count (members with all usage counters 0 and no session row), and a print icon link that opens the sheet again (design v2, 2026-11-09). |
| `GET /admin/batches/{id}/vouchers`, `PUT /admin/batches/{id}`, `DELETE /admin/batches/{id}` | Print (or reprint) the batch's vouchers (BR-16, decrypted passwords read at print time); rename the label; delete the batch with its members (BR-23, Q25). |
| `GET /admin/members/{id}/voucher` | **Print voucher** for a single member: the same template as BR-16 with one voucher (Q24); button on the member page. Works for any member, batch or not. |
| `GET /admin/plans`, `GET /admin/plans/new`, `POST /admin/plans`, `GET /admin/plans/{id}`, `PUT /admin/plans/{id}`, `DELETE /admin/plans/{id}` | CRUD with validation; editing a plan used by N members shows a warning "N members use this plan; changes apply to all of them at their next login" (Q16); delete answers `409` when the plan is in use (O12) or is the default plan (clear it first). |
| `GET /admin/usage` (`?days=7\|30\|90`, default 30) | Phase 9: daily traffic chart for the chosen range (7/30/90 days; drawn by `ui.Plot`, 2026-11-09) (all members, download + upload) and top 10 members by traffic for the chosen range (D12). |
| `GET /admin/sessions`, `GET /admin/sessions/table`, `POST /admin/sessions/{id}/disconnections` | BR-17/18 (Phase 5: `/table` is the polled fragment; a failed kick answers `422` missing identifiers, `502` NAK/no answer, `409` already ended, `404` unknown; success `200` + `HX-Redirect`). Columns: username, IP, MAC, started at, duration, data used in this session (↓/↑), disconnect button. The table refreshes itself every **10 s** (htmx polling) and shows the note "Updated by the router every <accounting_interval> s" (Q30). |
| `GET /admin/settings`, `PUT /admin/settings` | All `settings` columns (secret masked, reveal button). Validation ranges as in DDL. **2026-11-07:** the time zone is a dropdown (curated IANA list in `domain.TimezoneNames`; a stored zone outside the list is added as an extra option; any valid IANA name is still accepted). The RADIUS shared secret is **letters and digits only** (1-128 characters; no spaces or symbols). This replaces the older "kept exactly as typed" rule. The same rule applies to `HSP_RADIUS_SHARED_SECRET` (a bad value stops the start with an error), and the first-start secret is generated as 24 letters and digits (`auth.GenerateSecret`). An older stored secret with other characters stays valid: submitting it unchanged is accepted so other settings can still be saved, but a changed secret must follow the rule. The docs advise 20 or more characters (not enforced). |
| `GET /admin/router` ; `GET /admin/router/pages/{key}.zip` ; `GET /admin/router/pages/{key}/preview[?page=login\|alogin\|status\|logout\|error&error=CODE]` ; `GET /admin/router/pages/{key}/{css,img}/*` | Router setup page: the setup script, 8 page design cards (each with a small preview and a Download button), and nothing else. (2026-11-07, D28) The link status panel moved to the dashboard; this page no longer shows it. The panel is part of the dashboard fragment `GET /admin/dashboard/content` (2026-11-07), which the dashboard polls every 10 s with htmx. `{key}` is `mikrotik` or a theme key. The zip is `application/zip`, named `hotspot-{key}.zip`, root folder `hotspot/`. The preview fills the router variables with sample values and resolves the `$(if ...)` blocks; it carries its own strict CSP (own stylesheet and images only, no script, no form target). All need a login. *(Phase 8 added the setup script (E8.03) and the link status panel; the secret appears in the script only, see open-questions row 38. Phase 12b removed `/themes`, `/themes/{id}` and `files.zip`.)* The **router link status** panel (now on the dashboard; in-memory, resets on restart) shows: time + source IP of the last Access-Request, Accounting packet and Disconnect result, plus counters and source IPs of packets dropped for a bad shared secret (owner 2026-11-01). |
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
  internal/dashboard/          Service: read-only numbers for the dashboard (BR-25..BR-30); the pure rules are in `internal/domain`
  internal/radius/             UDP server, handlers, MikroTik attrs, CoA client, message catalog — calls the services
  internal/web/admin/          handlers + templ components (reusable: layout, table, pagination, filter bar, form field, confirm dialog, flash)
  internal/routerpack/         hotspot folders (Phase 12b): `mikrotik/` (byte copy of reference/hotspot), `src/hotspot/` (pages, helpers, xml, js), `src/css/layout.css` + `src/css/themes/<key>.css` (one token block per theme), preview with sample values, zip packaging; `go:embed`; plus `script.go` (router setup script)
  internal/web/static/         htmx (vendored, pinned), our CSS (`css/`) and JS (`js/`); `static.go` bundles and hashes them at startup (D24, 2026-11-03, `docs/ui-notes.md`)
  e2e/                         black-box tests, harness, fake router
  docs/
```
**Dependency injection rules (owner 2026-11-01, Q38):**
- Every service is a struct built by `New(deps…)`; its dependencies are **small interfaces declared in the consuming package** (e.g. `member.Repo`, `session.Disconnector`, `clock.Clock`), implemented by `store` / the CoA client in production and by **hand-written fakes** in unit tests. No mocking framework, no DI container, no reflection.
- `domain` imports only the standard library. `member`, `plan`, `session`, `voucher`, `auth` import `domain` (+ `clock`) but never `web`, `radius` or `store`. `radius` and `web/*` import the services, never `store`. Only `app` imports everything.
- Handlers stay thin: parse → call a service → render/reply. One implementation per rule (e.g. the login verdict and the admin usage summary both come from `auth`/`domain`).
- Unit tests cover `domain` and each service with fakes; e2e tests drive the real assembled binary.

### 9.1a Release builds (2026-11-04; names changed in Phase 13a)
`make release VERSION=x` cross-builds nine platforms with `CGO_ENABLED=0 -trimpath -ldflags "-s -w -X main.version=x"` into `dist/` (linux amd64/386/arm64/arm, windows amd64/386/arm64, darwin amd64/arm64). linux/arm uses GOARM=7 (no ARMv6 build). The platform list is kept in one place (`PLATFORMS` in the Makefile). Each is packed (`.zip` for windows, `.tar.gz` otherwise, with README.md and LICENSE) and `dist/SHA256SUMS` is written. Go has no windows/arm or darwin/386. CI runs `make build-cross` to catch a broken platform.
**Stable asset names (Phase 13a, owner 2026-11-04):** `hotspotpluslight-<os>-<arch>.tar.gz` (`.zip` for windows) and `SHA256SUMS`. The version is not in the file name (so `https://github.com/pysf/HotspotPlusLight/releases/latest/download/<name>` always works); it is inside the binary (`hotspotpluslight version`). `.github/workflows/release.yml` runs on a pushed tag `v*`, runs `make release VERSION=<tag>` and creates a GitHub Release from `dist/*` with `gh release create`. Test: E13.02.

### 9.2 Configuration (environment, all `HSP_` prefixed)
`DATA_DIR` (db path; default `data`), `SECRET_KEY` (base64 32 bytes: AES-GCM key; **auto-generated on first start** and saved to `{DATA_DIR}/hsp.key` — back this file up with the database; set it manually to override), `DEV` (`1` = serve the component gallery at `/admin/dev/components`, off by default). First-boot seeds (env sets the initial DB row; the dashboard is authoritative after that): `ADMIN_USERNAME` (default `admin`), `RADIUS_SHARED_SECRET`, `ADMIN_ADDR` (:8081), `RADIUS_AUTH_ADDR` (:1812), `RADIUS_ACCT_ADDR` (:1813), `LOG_LEVEL` (info), `LOG_FORMAT` (text). After first boot, the five server settings (`ADMIN_ADDR`, `RADIUS_AUTH_ADDR`, `RADIUS_ACCT_ADDR`, `LOG_LEVEL`, `LOG_FORMAT`) are read only from the settings table; the env vars are ignored (decision 2026-11-07: env seeds, dashboard wins). `LOG_LEVEL` is one of debug/info/warn/error. The three addresses must use different ports (port 0 excepted). Changes take effect after a restart. If a bad saved address stops the start, reset it with `sqlite3 data/hsp.db "UPDATE settings SET admin_addr=':8081', radius_auth_addr=':1812', radius_acct_addr=':1813';"`. `hsp.key` is generated only when it is missing and `hsp.db` does not exist; otherwise the server refuses to start (2026-11-07).

**Command line (Phase 11c, 2026-11-04).** `hotspotpluslight [--env-file <path>] serve | admin set-password [--stdin] | gen-key | service install [--dir <folder>] | uninstall | start | stop | version`. `--env-file` (for `serve` and `admin set-password`) reads `KEY=VALUE` lines (`#` comments, trimmed, optional quotes); a real environment variable wins over the file. `gen-key` prints a new `HSP_SECRET_KEY` (base64 of 32 random bytes), so `openssl` is not needed. The `service` commands work on Windows only: `install` (Administrator) creates the folder (default `C:\ProgramData\HotspotPlusLight`), writes `hsp.env` with a new key and `HSP_DATA_DIR=<folder>\data` if it is missing, and registers the auto-start service `HotspotPlusLight` that runs `serve --env-file <folder>\hsp.env`, with restart on failure. Under the service, logs and the first-start block go to `<folder>\hsp.log`. The SQLite path is turned into a safe `file:` URI by `store.DSN` (drive letters, spaces, `#`, `?`). Build: `make build-windows` (pure Go, no cgo).

### 9.3 Concurrency and time
SQLite single writer: all mutating work in short transactions; accounting handlers run synchronously per packet (fast). **One writer connection (2026-11-04, Phase 11d):** `store.Open` opens the file as two pools. The write pool has exactly one connection (`SetMaxOpenConns(1)`, `_txlock=immediate`) and runs every transaction and every write, so writers queue fairly in Go instead of fighting in the SQLite busy handler (which starved some writers until `database is locked`). The read pool has 8 connections and runs read-only queries. A read that must agree with a write stays inside the write transaction. Never call a `Store` method that writes from inside a transaction callback (one connection: it would wait for itself). `synchronous` stays at the default (FULL). `Clock` is injected everywhere so tests control time. Background jobs: session pruning (daily), nothing else.

### 9.4 Router artifacts (static, built into the binary)
Owner decisions 2026-11-04 (Phase 12). The login pages live on the router. No JavaScript calls our server. Everything is edited by hand in the files. Nothing is stored in the database, and no setting feeds the pages.

**8 downloads (D22).** `GET /admin/router/pages/{key}.zip`, root folder `hotspot/`:
- `mikrotik`: the RouterOS default pages, byte for byte from `reference/hotspot/` (an embedded copy; a test keeps it equal). It has `md5.js` and the CHAP block, and a failed login shows the raw reject code.
- `air` (default look), `midnight`, `aurora`, `dusk`, `ripple`, `sunrise`, `ocean`: one layout, one color theme each (§16).

**Files of a themed zip:** `login.html`, `alogin.html`, `status.html`, `logout.html`, `error.html` (new look); `css/hotspot.css` (the layout plus **only that theme's token block**); `js/busy.js` (optional spinner and double-submit guard, the pages work without it); plain helpers `redirect.html`, `rlogin.html`, `radvert.html`, `api.json` (RFC 8908 captive portal info), `errors.txt` (clear English, `radius-reply = $(error-orig)`); `xml/*` and `favicon.ico` (copied from the MikroTik folder so smart clients keep working). Not shipped: `md5.js`, the CHAP block (we use PAP only), the old `img/*.svg`. No external requests, no web fonts, no inline `style`, `onclick` or script bodies. Pages are well under 30 KB. `<body data-theme="air">` selects the theme.

**Source in the repo** (`internal/routerpack`): `mikrotik/` (the copy), `src/hotspot/` (pages, helpers, `xml/`, `js/`, with the placeholders `@THEME@` and `@MESSAGES@`), `src/css/layout.css` and `src/css/themes/<key>.css` (one token block each). Adding a theme = one new block file plus one line in `routerpack.Themes`. A function builds the zip or the file map for a key (`Pack.Zip`, `Pack.Files`).

**Page behaviour.** `login.html` posts `username` and `password` to `$(link-login-only)` with hidden `dst=$(link-orig)` and `popup=false`, and pre-fills `value="$(username)"`. Comments mark where to edit the heading ("Welcome"), the subtitle and the logo (default Wi-Fi mark; swap to `<img src="img/logo.png" alt="">`). One block, "MESSAGES - translate here", maps every reject code of `domain.AllReasons()` to English text: `$(if error == "CODE")text$(elif ...)...$(elif error)$(error)$(endif)`. `alogin.html` redirects to `$(link-status)` (no popup). `status.html` shows "Hi, `$(username)`", time online `$(uptime)`, time left `$(session-time-left)` when set, data left `$(remain-bytes-total)` when set, a Continue link to `$(link-orig)` when set, and a Log out button to `$(link-logout)`. To translate: change the MESSAGES block, `errors.txt` and the labels in the pages.

**Router prerequisites:** none for the page. The downloaded `hotspot` folder replaces the router's default one (`flash/hotspot` on flash routers).

**Setup script** (shown on `/admin/router`, copyable): `/radius add service=hotspot address=<server-ip> secret=<secret> authentication-port=1812 accounting-port=1813 timeout=5s`, `/radius incoming set accept=yes port=<coa_port>`, hotspot profile `use-radius=yes login-by=http-pap radius-accounting=yes radius-interim-update=<interval>s`, and a note on uploading the folder (it replaces the default `hotspot` folder; `flash/hotspot` on flash routers). No walled-garden and no `html-directory` line (Phase 12a). *RouterOS syntax must be validated on your router/version (§13).*

Legacy extras removed: NTP/time-zone lines, payment-gateway walled-garden entries, `nasIdentity`.

### 9.5 Security notes
**Admin sessions (Q11 answered "no handling", then revised):** **Revised (owner 2026-11-01, Q12b): opaque server-side sessions, sliding 1 hour; no JWT** (JWT can't be revoked and has no benefit with one binary and one DB). The cookie holds a random 32-byte id; only its SHA-256 is stored in `admin_session`; each request extends `expires_at` to now+1 h; logout deletes the row; expired rows are purged opportunistically. `admin set-password` deletes all `admin_session` rows, so a CLI password change logs out every open browser (reverses the earlier "no invalidation" answer — the cost is nil with this design). The password page (Phase 11b) deletes all rows except the current session. The wrong-current-password check uses the same per-IP throttle as login. `MONITOR:` comment at the session middleware.

**No RADIUS-edge or per-username brute-force protection (owner, 2026-11-01):** 6-digit voucher passwords on a LAN are guessable; accepted. Only the page-API per-IP throttle and the admin-login throttle exist. `MONITOR:` comment at the Access-Request handler.

Passwords: AES-256-GCM with random nonce (legacy used the deprecated `crypto.createCipher`). Admin: bcrypt. Cookies signed + CSRF tokens. All SQL parameterized. CORS `*` is acceptable only because of the LAN deployment (D9); the API throttles failed credentials per source IP. Shared secret never logged. HTTP-only is accepted for the LAN (D9) — note that PAP + HTTP login means credentials are visible on that LAN segment; acceptable per D9.

---

## 10. End-to-end test strategy

**Principle:** tests drive the product only through its real interfaces: UDP RADIUS, HTTP, and the CLI. They are written before the feature code.

**File layout (Phase 15, 2026-11-04):** the test files in `e2e/` are grouped by feature (`ops_startup_test.go`, `admin_auth_test.go`, `members_test.go`, `radius_auth_test.go`, `enforcement_test.go`, `site_test.go`, ...), not by phase. Shared helpers are in `e2e/helpers_test.go`. Test names and ids stay `E<phase>.<nn>`.

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
E2E first: **E0.01** boot with empty dir creates DB, admin (bcrypt of `admin`, `must_change_password=1`), settings · **E0.02** second boot keeps data (no re-seed) · **E0.03** `/healthz` · **E0.04** fakenas↔server smoke: an Access-Request gets *some* well-formed reply (Reject) · **E0.05** a missing `SECRET_KEY` is generated into `{DATA_DIR}/hsp.key` and reused on restart; an unreadable key file, or a lost key file next to an existing `hsp.db`, refuses to start and never writes a new key · **E0.06** `SECRET_KEY` that cannot decrypt `settings.key_check` refuses to start with a clear message (data intact) · **E0.08** first start creates no plan and leaves `default_plan_id` NULL; later starts create none either · **E0.09** migrations: all migrations apply to an empty DB in order; each runs in its own transaction (a deliberately failing migration leaves the DB at the previous version and aborts startup); a second start applies nothing; editing an applied migration file is detected by checksum and refuses to start; upgrading a DB created at each earlier version keeps its data · **E0.10** layering test: `go list -deps` shows `internal/domain` imports only the standard library, no service package imports `web`/`radius`/`store`, `radius` and `web/*` don't import `store` · **E0.07** README documents backing up the `.db` file (consistent copy via `sqlite3 ... ".backup"`) and keeping `SECRET_KEY` with it.
Exit: harness usable by later phases.

### Phase 1 — Admin auth, settings, plans
E2E first: **E1.01** admin login ok/fail · **E1.02** throttle after 5 failures · **E1.03** protected pages redirect to login; successful login lands on `/admin` (the dashboard, Phase 18) which answers 200 · **E1.04** CSRF rejected without token · **E1.05** (inverted in Phase 11b) the password page exists and is the only page with password inputs (`/admin/account/password` → 404) · **E1.06** `admin set-password` CLI · **E1.07** plan create/edit validation (units, negatives, duplicate name case-insensitive, `max_sessions` < 1 or non-integer rejected, `duration_days` < 1 or non-integer rejected) · **E1.08** decimal bulk/speed accepted · **E1.09** delete plan in use blocked with count (O12) · **E1.18** editing a plan used by N members shows the N-member warning (no warning at 0); after lowering data below a member's usage their next Access-Request is rejected `outOfBulk`, and after raising it they are accepted (BR-5b) · **E1.10** settings validation and persistence · **E1.11** changing the shared secret is picked up by the RADIUS listener without restart (uses fakenas, expecting reject vs. dropped) · **E1.14** first start prints the admin/admin block once and admin/admin logs in; second start prints none (O15) · **E1.15** BR-20: `admin set-password` rejects <10 characters, >72 bytes, the username (any case), a common password; accepts a lower-case phrase · **E1.16** D23: admin routes answer 404 on the public listener and page-API routes answer 404 on the admin listener; both listeners start independently · **E1.19** resource-style routes (Q39): `PUT`/`DELETE` work with a CSRF token and are `403` without; a wrong verb on a route is `405`; validation errors are `422` with the form re-rendered and field messages; a plan in use answers `409` on `DELETE`; unknown ids `404`; the old verb-style routes (`/members/delete`, `/{id}/kick`, `/{id}/renew`, `/print`) no longer exist · **E1.17** admin session: cookie is opaque (no claims), 61 minutes idle ⇒ redirected to login, activity within the hour extends it, logout invalidates the id server-side, `admin set-password` invalidates all sessions, only the hash is stored in the DB
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

### Phase 6 — Router page pack and page API (the page API was removed in Phase 12a, 2026-11-04)
*(Removed in Phase 12b, 2026-11-04: E6.11, E6.14-E6.18 and E6.20 (the 38 themes; replaced by E12.00-E12.06). Removed in Phase 12a, 2026-11-04: the page API, so E6.01-E6.10 and E6.12 no longer exist; `public_base_url` baking is gone too.)* E2E first: **E6.01** `GET /api/v1/account` with valid Basic auth ⇒ 200, `loginAllowed:true`, `reason:null` · **E6.02** each non-credential failure returns 200 with `loginAllowed:false` and its `reason` code + English `message` (inactive / no plan / plan removed / expired / validity window / data / time) · **E6.03** wrong password and unknown user both return `401` with an identical problem+json body and no `WWW-Authenticate` header · **E6.04** invalid username characters (non-ASCII, symbols) ⇒ `401 INVALID_CREDENTIALS` · **E6.05** CORS headers on every route (`Allow-Origin *`, `Allow-Headers: Authorization`) and `OPTIONS` preflight ⇒ 204 · **E6.06** the data/time figures in `account` equal the admin list (BR-7/8, BR-22), unlimited flags, and quota-exhausted members still get 200 · **E6.07** `GET /api/v1/devices/{mac}/credentials`: `403 DISABLED` when the setting is off; on ⇒ `200` credentials only for an active member, `404 DEVICE_NOT_FOUND` otherwise; MAC formats normalized (BR-3) · **E6.08** config reflects settings changes immediately · **E6.09** throttle after repeated `401`s: `429` with `Retry-After` and code `TOO_MANY_ATTEMPTS`, per source IP; resets via fake clock · **E6.10** credentials never appear in logs · **E6.11** every theme's `login.html` lint: no server template tokens left, API base baked from `public_base_url`, every message code has a dictionary entry · **E6.12** starter page script against `routerstub` + the API in a headless browser (optional, Playwright): error mapping, English fallback, native submit to the router login, after-login redirect, MAC auto-login · **E6.14** theme registry: exactly 38 themes in 6 categories, each with a manifest, a non-empty background image and parseable colors that match §16 · **E6.15** each of the 38 downloads is a valid zip whose `login.html` carries that theme's colors and background and the shared layout elements (logo, title, form, footer) · **E6.16** no `theme` parameter ⇒ `alpha-fall`; unknown id ⇒ `404` · **E6.17** gallery page lists all 38 grouped by category and each preview renders · **E6.19** error responses are `application/problem+json` with `type`, `title`, `status`, `detail`, `code`; no response contains an `ok` field; `account` and `credentials` carry `Cache-Control: no-store`; unknown routes ⇒ 404 problem+json · **E6.18** the generated `login.html` of every theme contains the disconnected-hint line (and its translation-dictionary key).
*(12a: API, CORS and `public_base_url` removed.)* Tasks: API handlers, CORS, throttling, the page pack sources (`login.html` + router pages), the translation dictionary, `public_base_url` baking, theme registry + layout template + zip packaging, gallery pages.

### Phase 7 — Batches and vouchers
E2E first: **E7.01** create batch ⇒ N members with `prefix+counter`, counter persisted (BR-15) · **E7.02** concurrent batch creations never reuse a number · **E7.03** count > max rejected · **E7.04** `expires_at` = local midnight of today+duration+1 (timezone setting) · **E7.05** password length setting · **E7.06** print page: 18 per A4 page, 3 columns, 6 rows, correct page count · **E7.07** help text comes from `settings.voucher_help` (English default) and the page is left-to-right · **E7.08** every generated member authenticates over RADIUS (plan applied) · **E7.09** after `expires_at`, login rejects (O7) · **E7.10** batch list and labels. · **E7.11** batch Reprint prints the same members again with their current passwords · **E7.12** single-member Print voucher renders one voucher (username, password, help text, left-to-right) for batch and non-batch members; unknown id ⇒ 404 · **E7.13** rename changes only the label · **E7.14** delete batch kicks open sessions, removes members, sessions and the batch row in one transaction; the confirmation shows member and online counts; the list's member/unused counts follow individual deletes and usage
Tasks: voucher generator, print template (port of `memberPrintTemplate.dust.html`), batch pages.

### Phase 8 — Ops, router pack, hardening
*(12a: E8.03 has no walled-garden entry; E8.04 has no `public_base_url` baking.)* E2E first: **E8.07** `/admin/router` status panel shows last Access-Request/Accounting/Disconnect times and sources, and counts a bad-shared-secret packet with its source IP (in-memory only, no DB writes) · **E8.13** (2026-11-07) the router status panel is in `/admin/dashboard/content`; the fragment has the three rows and shows a new packet without a full page load · **E8.03** `/admin/router` script contains secret, ports, interval and the server walled-garden entry · **E8.04** `files.zip` contains the customizable `login.html` (API base baked from `public_base_url`, no server template tokens) and the router pages, with no `plain-passwd` in any redirect· **E8.05** settings secret masked in HTML source · **E8.06** graceful shutdown completes in-flight accounting.
Tasks: router link status panel (in-memory counters fed by the RADIUS server and CoA client), README (run the single binary; example systemd unit as a README snippet only — **no Dockerfile**, owner 2026-11-01: ship the binary for now; install, router config, key/DB backup note, hardware lab checklist) (install, router config, hardware lab checklist), log format.

### Phase 11a — Enforcement in two layers (built 2026-11-04, owner decision)
E2E first: **E11.01** data limits and Gigawords split · **E11.02** unlimited data: no data-limit attribute · **E11.03** Session-Timeout is the smallest of time left, `expires_at`, window end · **E11.04** data over quota on an Interim: kick, row `Server: OUT_OF_BULK` · **E11.05** the same for time · **E11.06** `expires_at` in the past · **E11.07** auto-renew window end, next login renews · **E11.08** under quota: no kick · **E11.09** at most 3 tries, spaced by 2 x interval, then WARN; a silent router never delays the Accounting-Response · **E11.10** two sessions on one quota: both kicked · **E11.11** smaller plan: kick at the next Interim · **E11.12** Stop and unknown user never kick. Notes: `docs/phase11-notes.md`.

### Phase 11d — Capacity test (built 2026-11-04, owner asked for it)
`perf/` (build tag `perf`; `make perf`, `make perf-quick`, `make perf-windows`). It starts the real binary as a child process, seeds one plan and N members by SQL, and plays a fake router (UDP RADIUS, Disconnect listener, admin pages). Per step: N Starts, then N/60 Interims per second for the step time (default 120 s), 10% of N logins, 1% of members over quota (must get a Disconnect), admin Members and Online pages every 3 s. Steps 500, 1000, 2000, 5000, 10000, 20000, 30000, 40000, 50000; stops at the first failing step. Pass rules (steady part, open loop): no reply lost or later than 1 s, right reply codes, dispatch lag <= 1 s and steady rate >= 95% of planned, auth p99 < 500 ms, accounting p99 < 200 ms, pages p95 < 1 s, no SQLITE_BUSY in the log, and the database shows the real work (Server-ended rows, open sessions, used bytes). Only the default full run writes the report files. `HSP_PERF_MIN_USERS` is an optional floor. Output: `docs/perf-results.md` and the Performance section of `README.md` (between the `perf:start` and `perf:end` markers). Env: `HSP_PERF_STEPS`, `HSP_PERF_STEP_SECONDS`, `HSP_PERF_BIN`. The real limit may be the router, not the server. Notes: `docs/phase11-notes.md` (Step D).

### Phase 11b — Default admin/admin and the password page (built 2026-11-04, owner decision)
E2E first: **E11.20** first boot: admin/admin works and is forced to the password page (pages and htmx) · **E11.21** BR-20: short, username, common, too long and a wrong confirmation give 422 · **E11.22** a wrong current password is rejected and throttled (shared with login) · **E11.23** success: other sessions ended, this one kept, flag cleared, free navigation, toast · **E11.24** an existing install (flag 0) is not forced · **E11.25** the CLI uses the new rule and clears the flag · **E11.26** top bar link, Generate button and `password.js` present, no inline code. *(Design v2 step 7, 2026-11-09: E20.01 rename works (new name logs in, old one fails, other sessions end, this one stays) · E20.02 wrong current password is a field error and throttled · E20.03 invalid names give 422 · E20.04 same name is a no-op · E20.05 CSRF required · E20.06 page layout and `/admin/account*` stays 404 · E20.07 the forced screen renames with the password · E20.08 a bad name there leaves the password alone.)* Rewritten to the new rule: E0.01, E0.02, E0.09 (count 3), E1.01, E1.02, E1.05 (inverted), E1.06, E1.14, E1.15, E8.07b, E8.08, E8.09 (README text). Built: migration `0003_admin_must_change.sql`, `domain.ValidateAdminPassword` + `commonpasswords.go`, `auth.Service.ChangePassword` / `MustChangePassword`, `store.ChangeAdminPassword`, `internal/web/admin/password.go`, `static/js/password.js`. Notes: `docs/phase11-notes.md` (Step B).

### Phase 9 — Usage history (built 2026-11-02, owner asked for it)
E2E first: **E9.01** accounting deltas feed the day row (zero delta writes nothing, retransmission counts once, second session adds to the same row) · **E9.02** day = date in `settings.timezone` · **E9.03** Gigawords/counter-restart delta equals the member counter delta · **E9.04** same transaction (rolled-back packet leaves no row; retransmit counts once) · **E9.05** renew keeps history (supersedes BR-13 "no history", table only) · **E9.06** member delete removes only its rows · **E9.07** `/admin/usage` 30-day chart, all members summed, empty days zero · **E9.08** top 10 by download+upload for 7/30/90 days (default 30, ties by username) · **E9.09** nav link, login required. E0.09 now asserts the shipped migration count (2).
Built: migration `0002_usage_daily.sql`; `domain/usage.go` (day key, window, fill, ranges); `session.Service.Account` upserts the day row; `internal/usage` (report service); `GET /admin/usage[?days=7|30|90]`; reusable `ui.Plot` (was `ui.BarChart` before 2026-11-09) (inline SVG, no JS). The member page does not show per-member history (skipped, not trivial).

### Phase 10 — Own CSS, new look (2026-11-03, owner asked for it)
Step A (this step): remove Tailwind; own CSS with tokens and `c-` components; themes `air` and `air-dark` (replaced by light, dark and auto in design v2, 2026-11-09); every admin page restyled; behaviour, routes and field names unchanged. E2E first: **E10.01** no inline `style`/`onclick`/`hx-on`/inline script on any admin page · **E10.02** theme cookie sets `data-theme` to `light` or `dark`; no cookie or a bad value gives `auto` (the OS decides; was `air` before design v2) · **E10.03** asset URLs carry a hash, assets are cached for a year, pages are `no-store` · **E10.04** the gallery is 404 without `HSP_DEV=1` · **E10.05** the shell keeps the menu, Theme and Log out in the top bar. Existing selectors adapted (E1.20, E2.01, E6.14, E7.06, E8.12). Notes: `docs/phase10-notes.md`. Step B is described below.

Step B (2026-11-03): **expiring soon** (BR-24, status chips All / Active / Expiring soon / Expired / Disabled with counts); the members list updates in place (search with 300 ms delay, plan, page size, chips, pager; `hx-get` + `hx-push-url`); columns User, Plan, Usage (meter), Expires, Status (+ online dot), row menu; **sheets** for new and edit member and plan; **credentials card** after a member is created from the sheet; row **⋯ menus**; `POST /admin/members/{id}/toggle`; **toasts** (`HX-Trigger`). Rule for fragments: only the `HX-Target` header decides (the list region `member-list` / `plan-list`, or the sheet body `sheet-body`); a request without it gets today's full page or redirect. A change made from the list or sheet answers with `HX-Trigger` (`members-changed` or `plans-changed`, `toast`, `close-sheet` from the sheet) and `HX-Reswap: none` instead of `HX-Redirect`; a failure from there is a toast only. E2E: **E10.10** expiring boundaries (list, filter, pill words) · **E10.11** chip counts under q/plan · **E10.12** list fragment vs full page, toolbar attributes, refresher · **E10.13** columns, hooks, row menu · **E10.14** pager in place · **E10.15** sheet fragments (new/edit member and plan) · **E10.16** 422 in the sheet keeps input · **E10.17** sheet success headers, credentials card; unchanged without the sheet target · **E10.18** toggle route · **E10.19** list/sheet actions · **E10.20** plans list, menu, blocked delete toast · **E10.21** toast area, no inline code in fragments. Selector change: E2.12 compares the status cell with the status word ("inactive" is shown as "Disabled"); store test `TestFilterSelectionAndDelete`: the member expiring exactly now is now "expiring", not "active" (BR-24). Notes: `docs/phase10-notes.md`.

### Phase 12a — Drop the page API and :8080 (built 2026-11-04, owner decision)
Removed: `internal/web/pageapi`, the public listener, `HSP_HTTP_ADDR`, MAC auto-login (O8), the settings `public_base_url`, `mac_auto_login`, `after_login_url` (migration `0004_drop_page_api_settings.sql`), `Evaluator.Account`, `Evaluator.CredentialsByMAC` and `ListActiveMembersByMAC`. `/healthz` moved to the admin listener. The RADIUS `Reply-Message` is only the code. The setup script lost the walled-garden and `html-directory` lines. E2E: **E12.07** Reply-Message is exactly the code · **E12.08** one listener, `/healthz` without login, `/api/v1/*` 404 · **E12.09** migration 0004 · **E12.10** setup script. Removed tests are listed in `docs/phase12-notes.md`. Phase 12b (new router pages, zips, Router page cards) follows.


### Phase 12b - New router pages (built 2026-11-04, owner decision)
8 static downloads (D22, §9.4, §16): the MikroTik default pages byte for byte, and 7 themes of one new layout. New admin Router page with 8 cards (preview + Download); routes `GET /admin/router/pages/{key}.zip` and `.../preview` (§8). Removed: the 38-theme pack (`layout/`, `pages/`, `themes/`), `/admin/router/themes*`, `files.zip`, and their tests (listed in `docs/phase12-notes.md`). E2E: **E12.00** the `mikrotik` zip equals `reference/hotspot/` under `hotspot/` · **E12.01** file list per themed zip, no `md5.js`, no outside address, under 30 KB · **E12.02** RouterOS syntax per page, `$(if)`/`$(endif)` balanced in every file · **E12.03** the MESSAGES block covers every domain reject code with the fallback, and the preview shows each text · **E12.04** one theme block per CSS, same layout, no colors outside theme blocks, `body data-theme` matches · **E12.05** no inline style, handler or script body, previews need no script · **E12.06** the Router page lists 8 cards, downloads need a login, content type and file name, previews work.
### Phase 17 — Dashboard data layer (built 2026-11-07, owner decision D25-D28)
Scope: migration `0007_login_failure_daily.sql` (§6). Failed-login counting in the RADIUS path (BR-27): a small `FailureRecorder` interface declared in `internal/radius`, called from `reply()` in the reject branch (the one place every Access-Reject is written), plus the in-memory retransmission cache (60 s, 1024 entries); wired in `internal/app`. Pruning of the new rows inside `session.Service.Prune`. Pure rules in `internal/domain`: `PeakConcurrent`, `HourlyAverage`, `PercentChange`, `ExpiresWithin` (BR-24 and BR-30 both use it; `MemberStatus` is changed to call it), the 80% data rule, `TopReason`. The read service `internal/dashboard` (repo interface declared there, queries in `internal/store`), with unit tests using fakes. No page yet.
E2E first (UDP RADIUS, plus read-only SQL checks in the harness like E9.x): **E17.01** migration 0007 applies on an empty database and on a 0006 database and keeps the data (E0.09 now expects 7 migrations) · **E17.02** every reject reason sent over UDP adds 1 to its (day, reason) row: wrong password, unknown user, `INACTIVE_USER`, `NO_PLAN`, `PLAN_REMOVED`, `EXPIRED`, `NO_ACTIVE_SUBSCRIPTION`, `OUT_OF_BULK`, `OUT_OF_TIME`, `NO_MORE_SESSION`, `UNSUPPORTED_AUTH_METHOD` (CHAP) · **E17.03** an Access-Accept adds nothing; packets that get no reply (wrong secret, bad Message-Authenticator, malformed password, database failure as in E3.18) add nothing here, and the bad-secret ones still show in the link status counters · **E17.04** a retransmission (same source, Identifier and Request Authenticator) is counted once; the same packet after 60 s (fake clock) counts again; a new Identifier counts · **E17.05** the day key follows `settings.timezone` across midnight (fake clock); a later timezone change leaves old rows alone · **E17.06** pruning removes rows older than the cutoff day, keeps newer ones, and `session_retention_days = 0` keeps all · **E17.07** the table has only the columns day, reason, count (no username, no address), and a reject writes no session or member row · **E17.08** a failed counter write still sends the Access-Reject (a unit test with a fake repo if the harness cannot force it).
Unit tests (named in the code, not E ids): `PeakConcurrent` (overlaps, touching intervals, stale open session, midnight clipping), `HourlyAverage` (partial today, new install, `UTC` and `Asia/Tehran`), `PercentChange` (zero base, rounding, negative), `ExpiresWithin` and `MemberStatus` at every boundary, the 80% rule at 79/80/100%, `TopReason` tie, the dashboard service with fakes.
Exit: all E17 and unit tests green; `gofmt -l .`, `go vet ./...` clean; a separate review agent (`code-review`, medium) on the uncommitted changes with real findings fixed; the **owner** runs the e2e suite (agents do not run it); commit `Phase 17: …`.
**Built (2026-11-07; committed as 08d06c2 after a review; the owner runs the e2e suite).** Files: `internal/store/migrations/0007_login_failure_daily.sql`, `internal/store/dashboard.go`, `internal/dashboard/` (service and unit tests), `internal/domain/dashboard.go` (the pure rules) and `internal/radius/retry.go`. Choices made where the plan left room: `radius.New` takes `...Option` (`WithFailureRecorder`, `WithClock`), so the many existing callers did not change. The retransmission key uses the full source `ip:port`. The failed-login day key uses the clock at the moment the reject is written. *Peak today* is never lower than *online now* (a session that started this second has no length yet). `session.Service.Prune` also prunes the counts, with the day key of the same cutoff. E17.08 has a unit test in `internal/radius` and an e2e test that renames the table. E0.10 now also forbids `internal/dashboard` from importing `web`, `radius` and `store`. The E0.09 startup test listed only five shipped migrations plus a fake `0006_bad.sql`; it now lists the shipped 0001-0007 and fails with a bad `0008_bad.sql`.

### Phase 18 — The dashboard page (built 2026-11-07, owner decision D25-D28)
Scope: route `GET /admin` (replaces the redirect) and the fragment `GET /admin/dashboard/content`; sidebar item **Dashboard**, first in the list; login and password flow land on `/admin`; the page in templ from small reusable components: tile, attention list, chart (reuse `ui.Plot`, was `ui.BarChart` before 2026-11-09: one series of whole numbers, so busy hours use tenths with the tooltip showing the decimal; generalize it only if needed, still one component). The router link status panel is rendered on the dashboard and removed from the Router page (D28); The whole dashboard polls the fragment every 10 s (2026-11-07). Order on the page: three tiles; router status; Needs attention; the three charts. Empty states for a new install. No inline style, handler or script; `no-store`; own SVG only.
Tests that change: **E1.03** (login lands on `/admin`, which now answers 200 and does not redirect), **E8.07**, **E8.07b** and **E8.13** (the panel is read from `/admin` or the content fragment; the Router page must not contain it), **E8.12** (Router page assets), any test that asserts the sidebar items or the old landing page.
E2E first (HTTP and UDP only): **E18.01** `GET /admin` is 200 for a logged-in admin, redirects to login when anonymous, never redirects to Members · **E18.02** the sidebar has Dashboard first and marks it current; the other items are unchanged · **E18.03** the Online now number equals the Online page (BR-17), a stale session is not counted, · **E18.04** peak today: overlapping sessions made through accounting with a fake clock give the right maximum; a stopped session still counts; the next day (timezone) starts again · **E18.05** the Traffic today tile and its two comparisons match `usage_daily`, equal `/admin/usage` for the same day, and show "no data" when the base is 0 · **E18.06** the Failed logins tile shows the total and the top reason after rejects sent over UDP; the tie rule; the empty text · **E18.07** the router status panel is on `/admin` (once), is gone from `/admin/router`, and the panel is empty after a restart · **E18.08** Needs attention: expiring within 3 days (the 3-day boundary, disabled and expired excluded), near the data limit (79% out, 80% in, 100% labelled "out of data", unlimited plans and extra bulk), at most 5 rows with the full count, order, empty state, links to member pages · **E18.09** the traffic chart has 14 bars equal to the `usage_daily` sums with zero-fill and a link to `/admin/usage` · **E18.10** the busy hours chart has 24 bars with the BR-29 values; an hour that has not started today does not lower the average · **E18.11** the daily active chart has 14 bars; a member with several sessions counts once; a member with no traffic does not count · **E18.12** day boundaries and labels follow `settings.timezone` · **E18.13** a new install (empty database) answers 200 with all empty states; the page passes the E10.01 cleanliness check and is `no-store` · **E18.14** the dashboard fragment needs a login · **E18.15** (2026-11-07) the whole dashboard refreshes: one container with `hx-get="/admin/dashboard/content"`, `every 10s`, `outerHTML` holds the tiles, router status, Needs attention and charts; the fragment has all parts and no page frame, is `no-store`.
Built (2026-11-07; committed as 37dbaf8 after a review): `internal/web/admin/dashboard.go` and `dashboard.templ` (page, `OnlineTile`, `AttentionSection`, `ChartSection`, and `LinkStatusPanel`, moved from `router.templ`); new `ui.Tiles`, `ui.Tile`, `ui.TileValue`, `ui.TileNote` and a `home` icon; `css/components/tile.css`. The brand link and the password-change redirect also go to `/admin`. Charts reuse `ui.Plot` (was `ui.BarChart` before 2026-11-09); a chart whose bars are all 0 shows its empty text instead (`data-empty`). Busy hours use the average in tenths as the bar height, with the decimal in `data-avg` and the tooltip. Test hooks: `data-tile`, `data-online-now`, `data-peak-today`, `data-traffic-today`, `data-vs-yesterday`, `data-vs-lastweek`, `data-failed-total`, `data-failed-top`, `data-failed-empty`, `data-attention` with `data-count`, `data-attention-row`, `data-more`, `data-chart` with `data-bar`. Tests changed: E1.03 and the login and password landing checks (E1.x, E11.x), the sidebar test (8 links), the page list of E10.01 (adds `/admin`), E8.07, E8.07b and E8.13 (read the panel on `/admin`). E8.07 now reads its database baseline after the first reject, because a reject writes its failed-login count (D27); the link status itself still writes nothing. Not run: no e2e test was run by the agent (owner rule). Change (2026-11-07, owner): the expiring list is 3 days (one constant, `domain.ExpiringDays`) and the whole dashboard refreshes by itself with one polled fragment `GET /admin/dashboard/content` every 10 s; the routes `/admin/dashboard/online` and `/admin/dashboard/link-status` and `dashboard.Service.Online` were removed, and the Phase 18 tests E18.03, E18.07, E18.08, E18.14 and E8.13 changed, E18.15 is new. Review fixes (2026-11-07): busy-hours bars are drawn in hundredths (a non-zero average always shows a bar) and `data-avg` has two decimals; the caption peak comes from the averages; the link status fragment moved to `/admin/dashboard/link-status` and reads only the time zone.
Exit: all E18 and changed tests green; `gofmt`, `go vet` clean; review agent (`code-review`, medium) with real findings fixed; the owner runs the e2e suite; commit `Phase 18: …`.

### Phase 19 — Dashboard docs and clean-up (built 2026-11-07)
Scope (CLAUDE.md rule 9): README features and routes; a **Dashboard** page in `site/docs/` with its entry in the docs navigation and `search-index.js`; the Router page text, router-setup and troubleshooting pages say the link status is on the dashboard (today the troubleshooting page says "The Router page link status"); the failed-login counter is described (what is counted, what is not, no personal data, how long it is kept); update the site tests (E13.x strings) and `e2e/repo_docs_test.go` for the moved text; `docs/phase17-notes.md` and `docs/phase18-notes.md`; this plan's status lines and `CLAUDE.md` "Current status"; remove any dead code left by the move (the Router page props, unused templ).
E2E first: **E19.01** the site has the Dashboard page, linked from the docs navigation and the search index · **E19.02** no site page or README sentence still places the link status on the Router page · **E19.03** the README and site describe the failed-login counter and its retention.
Exit: `gofmt -l .`, `go vet ./...` clean; review agent on the whole phase; the owner runs the e2e suite; commit `Phase 19: …`.
**Built (2026-11-07; the owner runs the e2e suite).** The site page `docs/dashboard/` (what each tile, list and chart means; how failed logins are counted and kept) with its entry in the docs sidebar of every page, the pager (First login, Dashboard, Router setup), `sitemap.xml` and `search-index.js` (the six new sections, and the entries of the sections whose text changed). The README has a Dashboard section, the `internal/dashboard` and `internal/linkstatus` package rows and two decision rows. First login, Router setup and Troubleshooting say the router status is on the Dashboard. `docs/open-questions.md` row 43 is closed. `docs/phase17-notes.md`, `phase18-notes.md` and `phase19-notes.md` are written. Tests: `e2e/dashboard_docs_test.go` E19.01-E19.03; `TestE13_14_DocsLayout` now expects 12 docs pages. No dead code was left by the move: the Router page has no link status props, and `LinkStatusPanel` lives in `dashboard.templ`. Not verified: no e2e test was run by the agent; some older search index entries (installation, configuration, running-as-a-service, backup) are older than their pages, and only the entries touched here were rebuilt.

---

## 12. Definition of done (whole project)
All E2E tests green on a clean checkout with no external services; hardware checklist (§13) executed on one real MikroTik and results recorded in `docs/lab-results.md`; every row in §4 either implemented or explicitly signed off as Dropped; README lets a new person install and connect a router in under 30 minutes.

---

## 13. Hardware lab checklist (done by the owner on 2026-11-09)
The owner ran this checklist on a real MikroTik on 2026-11-09 and everything works. Items 16 and 17 are facts found in the lab and stay as documented behavior. Use `VERIFY-ON-HARDWARE` only for new unverified details.

1. Confirm hotspot profile `login-by` is `http-pap` (or at least PAP reaches RADIUS: Access-Request contains `User-Password`, not `CHAP-Password`).
2. Verify MikroTik VSA type ids (§7.5). **Phase 11a:** which direction `Mikrotik-Recv-Limit` and `Mikrotik-Xmit-Limit` mean (the MikroTik doc does not say; we send the same value in both, so each direction can use the full allowance and the server checks the sum); that the router honours the `-Gigawords` limits above 4 GiB; that the session ends at `Session-Timeout`. **Over-grant window:** each direction gets the full bytes left, so if the router counts the two directions separately, a member can move up to 2x the bytes left before the server (layer 2) catches it at the next Interim; measure it.
3. Rate limit string accepted and applied (`Nk/Nk`).
4. `Session-Timeout` disconnects at the expected time.
5. Interim updates arrive every `accounting_interval_seconds`; `Acct-Input/Output-*` direction matches BR-11 (upload = Input).
6. Gigawords appear in accounting after 4 GiB (usage counting).
7. `Disconnect-Request` to port 3799 works with the attribute set in BR-18; confirm which attribute set RouterOS requires. **Phase 11a:** also that a Hotspot user is disconnected by it (the automatic kicks depend on it), and that `/radius incoming` on our port (RouterOS default is 1700) answers.
8. (dropped) Router reboot handling is out of scope.
9. `$(error)` in the router page shows the RADIUS `Reply-Message`, which is now only the reject code (Phase 12a). Also check (Phase 12b) that `$(if error == "CODE")` matches it, so the MESSAGES block of `login.html` shows the mapped text (and the fallback `$(elif error)$(error)` shows any other router message).
11. (Phase 12b) Status page variables: `$(uptime)`, `$(session-time-left)`, `$(remain-bytes-total)` (is it filled, and in which unit: we assume bytes), and whether `$(link-orig)` holds the first site on the status page (the Continue link).
12. (Phase 12b) `alogin.html` with `popup=false` lands on the status page; `$(link-status)` works in the captive-portal mini browser of iOS and Android.
13. (Phase 12b) `api.json` is served and valid JSON for a logged-in and a logged-out client (RFC 8908), and `errors.txt` messages appear for router-side errors.
14. (Phase 12b) The new pages look right and work in the phone captive-portal mini browsers (no external requests; `backdrop-filter`, `color-mix`, `100dvh` on old iOS).
15. (Phase 15) RouterOS accepts replies that carry Message-Authenticator as the first attribute (Access-Accept, Access-Reject, and Disconnect requests on port 3799), and `require-message-auth=yes` on the router works with this server.
16. (2026-11-07, verified on RouterOS 6.49.23, hAP 951Ui-2nD) A server on the hotspot network is an unauthenticated guest: its replies are ignored and logins fail with "RADIUS server is not responding", although a capture shows correct replies (source, ports, Response Authenticator and Message-Authenticator all valid). Fixed on the router (hotspot rules). The docs only name it as a common issue.
17. (2026-11-07, verified on RouterOS 6.49.23) Disconnect goes to the source address of the session's accounting packets (BR-18). When the router had talked from its uplink address (ether1, DHCP), the default input rule "drop all not coming from LAN" dropped the request and the panel showed "no answer"; with that rule disabled the Disconnect was ACKed. Keep the router talking from its LAN address. Each Disconnect is now logged (`disconnect`, with the outcome). The docs name this only as a common issue.
10. (informational) `Acct-Session-Id` reuse after reboot is an accepted risk.

---

## 14. Risks
| Risk | Mitigation |
|---|---|
| RADIUS details differ on real RouterOS | Fake router is only as good as our assumptions → lab checklist §13 (done 2026-11-09) covered it |
| PAP over plain HTTP/UDP on the LAN | Accepted (D9); documented |
| SQLite write contention under many interims | 60 s interval × expected users is far below SQLite limits; WAL + short transactions; measured by the capacity test in `perf/` (Phase 11d, `make perf`, results in `docs/perf-results.md`) |
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

## 16. Appendix - The 8 router page designs (Phase 12b, replaces the legacy 38-theme catalog)

Visual source: `reference/hotspot-login.html` (CSS values copied exactly; the preview bar and demo scripts were not). Brief: `docs/HOTSPOT-LOGIN.md`. Same layout in every theme; a theme sets only color tokens and an optional background decoration (CSS only, no images).

| Key | Title | Look | Contrast change |
|---|---|---|---|
| `mikrotik` | MikroTik default | The RouterOS pages, unchanged | none (not ours) |
| `air` | Air | Light grey, Apple blue `#0071e3` (default) | none |
| `midnight` | Midnight | Graphite, blue glow, faint fading grid (dark) | button `#4c8dff` to `#2f6fe0` (white label 3.2 to 4.7) |
| `aurora` | Aurora | Pastel mesh, frosted glass card, indigo | top of the button gradient `#6d6df6` to `#6060ee` (white label 4.1 to 4.8) |
| `dusk` | Dusk | Violet-black, glow from below, glass card, violet button (dark) | none |
| `ripple` | Ripple | Sage paper, signal rings behind the card, deep green | none |
| `sunrise` | Sunrise | Warm cream, low sun glow, coral accent, espresso button | none |
| `ocean` | Ocean | Deep teal color-block, white card | none |

---

## Phase 13a: license, public name, technical README, release names (built 2026-11-04)
Owner decisions are in the Phase 13 plan. This step is only the repo part; the site is 13b.
- **License:** AGPL-3.0. `LICENSE` has the full, unmodified text (from gnu.org). The README has a license line. Test E13.01.
- **Public name:** "HotspotPlus Lite" on the admin page titles, the brand text in the admin sidebar, the voucher print title and the README title. The binary, Go module, repo, Windows service name and release files stay `hotspotpluslight`.
- **README is technical only:** what it is, an architecture flowchart, four sequence diagrams (member login, accounting with enforcement, admin kick, first admin login), the package map, key decisions, make targets, how the tests are organized, where the docs live, the Performance section (markers kept), and a link to https://pysf.github.io/HotspotPlusLight/ for user docs. The install, configuration, router setup, backup and systemd text moved to the site (13b). E8.09 and E0.07 were changed for this; see `docs/phase13-notes.md`.
- **Release names:** see 9.1a. Test E13.02.

## Phase 13b: the public site (built 2026-11-04)
- `site/`: landing page and 11 docs pages (plain HTML, CSS, small ES modules; dark Terminal look; no build step, no third-party requests, no inline code). Deployed by `.github/workflows/pages.yml` (checkout@v5, configure-pages@v6, upload-pages-artifact@v5, deploy-pages@v5; Node 24).
- Base URL `https://pysf.github.io/HotspotPlusLight/` is in the canonical/OG tags, `sitemap.xml`, `robots.txt` and `404.html`; the README says how to change it. All other links are relative.
- Tests E13.03 to E13.17 (`e2e/site_test.go`), including the moved README checks (hard exit item of 13a). Notes: `docs/phase13-notes.md`.
