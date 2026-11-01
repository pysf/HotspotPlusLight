# Phase 0 notes: ambiguities and interpretations

Each item: what PLAN.md says, the simplest reading chosen, and why.

1. **`schema_migrations` checksum column.** PLAN section 6 says a checksum is stored in `schema_migrations` but its DDL has only `version, applied_at`. Chose: `0001_init.sql` creates the table with an extra `checksum TEXT NOT NULL`; the runner inserts the 0001 row inside 0001's own transaction (the table does not exist before it).
2. **No default plan at startup.** Owner decision after Phase 0: the server creates no plan; the admin creates plans in the web UI. `settings.default_plan_id` stays NULL until the admin sets it.
3. **`admin set-password` is a stub.** PLAN section 11 gives it to Phase 1 (E1.06); it returns "not implemented yet" (exit 1). The first-boot password print (E1.14 area) is implemented now because Phase 0's task list requires seeding.
4. (removed with the startup default plan)
5. **`settings.voucher_help` default** stays `''` (DDL default); the English text is a Phase 7 concern.
6. **RADIUS in Phase 0.** Access-Request always gets `Access-Reject` with `Reply-Message = "INVALID_CREDENTIALS: Wrong username or password."`. The accounting listener is bound but drops every packet (no reply) until Phase 4, so nothing is acknowledged that was not stored. Packets with a wrong secret: layeh does not authenticate Access-Requests (they carry a random authenticator), so such a request still gets a reply the client cannot verify; PLAN E3.14 ("no reply") is Phase 3's to settle.
7. **Test harness is in-process, plus the real binary for E0.05.** Following PLAN section 10, the app runs in-process on `127.0.0.1:0` ports with an injectable clock (`app.Options`). Tests that need the real CLI (missing key exit code) build the binary once and exec it. `fakenas` has only `Authenticate`; accounting and the Disconnect listener come with Phases 4-5.
8. **Go version.** The dependencies (modernc.org/sqlite) require Go 1.26, so `go.mod` says `go 1.26.0` (CLAUDE.md says "Go >= 1.22"). The installed `go` auto-downloaded the 1.26 toolchain into its module cache (GOTOOLCHAIN=auto); nothing was installed globally.
9. **Module path** is `hotspotpluslight` (no VCS host); the layering test uses `go list`'s `Standard` flag rather than a dot-in-path heuristic.
10. **Admin username equals-password check** compares case-insensitively (BR-20 says only "not equal to the username").
11. **Unit tests of units written alongside their code** (domain, secret, config, clock, auth, services); the E2E tests E0.01-E0.10 and the store migration tests were written first and verified red (compile failure: no `app`/`store` packages) before implementation.
