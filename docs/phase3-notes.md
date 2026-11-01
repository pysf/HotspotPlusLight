# Phase 3 notes: RADIUS authentication

Built: `internal/auth/evaluator.go` (THE login check, PLAN 7.2 checks 1-9, shared with the Phase 6 page API), `internal/domain/login.go` (username rule, `Grant`/`GrantFor`, `RateLimit`, `ActiveSince`, sentinels) and the message catalog (`Reason.Message/ReplyMessage` in `domain/quota.go`), `internal/store/login.go` (3 queries), the PAP handler in `internal/radius/server.go`, wiring in `internal/app`. The check reuses `domain.Evaluate`/`QuotaOf` from Phase 2, so the admin verdict and the Access-Request cannot disagree (E2.13 now compares them for 9 cases).

E2E (`e2e/phase3_test.go`, written first, seen red): E3.01-E3.18 all green, plus the RADIUS half of E2.13 (skip removed) and `TestE1_18_LimitsChangeAffectsNextAccessRequest` (data, duration, auto-resubscribe, time and `max_sessions` edits applied at the next login). Harness additions: `fakenas.PAPPacket/Exchange/Silent` (Silent = raw UDP, proves "no datagram at all"), `harness.Options.Logger` (tests assert WARN/ERROR lines).

Skips: none. E3.12 is enabled now: session rows are seeded through the DB with the Phase 2 `openSession` helper (existing harness pattern). Phase 4 should add an accounting-driven variant.

MikroTik attributes: vendor 14988, Xmit-Limit type 2, Rate-Limit type 8 are used via `layeh.com/radius/vendors/mikrotik`; E3 tests parse the raw VSA bytes independently and PLAN 7.5's ids match the library dictionary. Semantics (download-only cap, the 4294967295 clamp, Rate-Limit syntax) stay `VERIFY-ON-HARDWARE`; `MONITOR:` comments sit on `Grant.SessionTimeout/XmitLimit` and the Login function.

## Decisions to confirm (simplest reading)
1. **Wrong shared secret = silence, detected heuristically.** layeh/RFC 2865 give an Access-Request no verifiable authenticator, so the server cannot know the secret is wrong. A PAP `User-Password` that decrypts to invalid UTF-8 or contains control characters (< 0x20, 0x7f) is dropped silently with a WARN ("probably a wrong shared secret"). Consequences: (a) a wrong-secret packet whose password happens to decrypt to empty (1/256) still gets an (unverifiable) Reject; (b) a real member password containing control characters could never log in (the admin form does not forbid them; PLAN BR-14 says nothing). Message-Authenticator is not checked (unverified what RouterOS sends; VERIFY-ON-HARDWARE).
2. **Malformed User-Password** (length not a multiple of 16 etc.) is dropped silently; a missing User-Password gets `INVALID_CREDENTIALS`.
3. **Infrastructure failure** (DB error, undecryptable password) = no reply and an ERROR log (E3.18). The router retries/times out.
4. **Username at the edge:** trimmed, then BR-1 (A-Z a-z 0-9, 1-64); anything else is `INVALID_CREDENTIALS` without touching the database (E3.17). The password is compared constant-time, case-sensitive, without trimming.
5. **Rate limit rounding:** speed in kbps is rounded to a whole number (legacy `Math.round`); a speed that rounds to 0 (e.g. 0.0004 Kbps) is sent as `1k...` instead of legacy `0k`, which RouterOS reads as unlimited. Not covered by the plan; flag if you prefer legacy.
6. **Auto-resubscribe at login** runs after checks 1-5 (so inactive/expired members are not renewed) and renews with one guarded `UPDATE ... WHERE subscription_date = <seen value>`, so two simultaneous logins renew once; the member is then re-read and re-evaluated. The renewal commits even if the login is then rejected for `NO_MORE_SESSION` (as in legacy, where the renewal precedes the session check).
7. **Clamp log:** a WARN "Mikrotik-Xmit-Limit clamped ..." per Accept with remaining data above 4,294,967,295 bytes (any plan/extra bulk above ~4 GiB), so the log is noisy for such plans.
8. `Acct-Interim-Interval`, `Session-Timeout` are sent as 32-bit values; `Session-Timeout` is clamped to 4,294,967,295 defensively (unreachable with the form limits).
9. The `UNSUPPORTED_AUTH_METHOD` check is done before the secret heuristic (CHAP-Password is not encrypted), on any `CHAP-Password` or any Microsoft vendor (311) attribute.
10. The router-link-status panel counters for bad-secret drops (PLAN 8.2) are not built; the WARN log line is the only trace for now.

## Questions for the owner
- Decision 1(b): are control characters in member passwords acceptable to forbid (add a rule to the member form), or should the heuristic be dropped in favour of "wrong secret still gets an unverifiable Reject" (PLAN E3.14 would then only mean "the reply does not verify")?
- Decision 5: keep the `1k` floor?

## Review fixes
1. Renewal before the session check: **verified, kept.** PLAN 7.2 order (step 6 mutates before step 9) and legacy `member.js` (`checkAuthorization` at line 606 before the session rejection at 612) agree. Documented in PLAN 7.2 and a comment in `evaluator.go`.
2. Still-lapsed after renewal (concurrent admin edit): **real.** Now a normal `NO_ACTIVE_SUBSCRIPTION` Reject plus a WARN log (`NewEvaluator` takes a logger). Test `TestEvaluatorStillLapsedAfterRenewalRejects`. No-reply stays for DB failures only.
3. `GrantFor` lower clamp: **real.** `SessionTimeout`, `XmitLimit` and `InterimSeconds` are clamped to >= 0 before the uint32 conversion. Test `TestGrantForClampsNegativeValues`.
4. Non-atomic session check: legacy-faithful, unchanged; `MONITOR:` comment and PLAN 7.2 line.
5. Owner decision: control characters and invalid UTF-8 are forbidden in member passwords (`ValidateMemberPassword`, used by create and set-password); PLAN BR-14a. Test `TestMemberPasswordRejectsControlCharacters`. This answers decision 1(b) above.
6. Username-enumeration timing: **real, small.** An unknown username now does a constant-time compare against a dummy value. The PAP decrypt already happens before `Login` for both paths. The brute-force MONITOR note existed. Test `TestEvaluatorUnknownUserRejectsLikeWrongPassword` checks identical verdicts (timing itself is not tested).
7. `MaxUsernameLen`: the member form now passes `domain.MaxUsernameLen` to the parser.
- Rate-limit floor `1k` kept (answers decision 5); noted in PLAN BR-10.
