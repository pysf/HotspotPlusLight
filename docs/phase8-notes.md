# Phase 8 notes (ops, router pack, hardening) - 2026-11-02

Tests: `e2e/phase8_test.go` (E8.03, 03b, 03c, 04, 05, 06, 07, 07b, 08, 09), unit tests in `internal/linkstatus` and `internal/radius`.

## What was built
- **Setup script** (`internal/routerpack/script.go`, pure `SetupScript`): shown in a read-only box with a Copy button (`ui.CodeBlock`) on `/admin/router`. Commands: `/radius add`, `/radius incoming set`, `/ip hotspot profile set ... use-radius=yes login-by=http-pap radius-accounting=yes radius-interim-update=Ns`, a walled-garden entry limited to the public port, and `html-directory=hotspot`. Every command is under a comment and marked `VERIFY-ON-HARDWARE`. The legacy script (`mikrotik_setup.sh`) only supplied `/radius add` and the host-name walled garden form; the other lines come from PLAN 9.4. NTP, time zone and payment entries are not carried over. The secret is escaped for RouterOS (`\ " $ ?`). Empty `public_base_url`: placeholders plus the existing warning. A host name: `<SERVER-IP>` for RADIUS.
- **Link status panel** (`internal/linkstatus`): in-memory recorder (injected clock), fed by the RADIUS server (Access-Request, Accounting, bad secret) and the Disconnect client (outcome ACK/NAK/no answer). No DB writes (tested with `PRAGMA data_version`), empty after restart. Resolves open-questions row 3.
- **Graceful shutdown (E8.06)**: the library cancels the request context and closes the sockets as soon as `Shutdown` starts, which would roll back an in-flight accounting transaction and lose its reply. Handlers now use `context.WithoutCancel`, and the sockets are wrapped (`lateCloseConn`) so that library "close" only stops reading; `Server.Shutdown` closes them after the handlers finished. Pruning goroutine stop and listener release are asserted.
- **Logging**: `HSP_LOG_FORMAT=text|json` (default text), stderr. Tests check line shape, no password/secret in the log, SIGTERM exit 0.
- **README** rewritten; E8.09 checks its sections.
- E8.05 interpretation: see open-questions row 38.

## Skipped / not done
- No E8 test was skipped. Nothing was run on real hardware (PLAN 13 stays the gate).

## Review fixes
1. **Script injection via the secret (verified: yes).** The validator accepted CR/LF. Now `radius_shared_secret` rejects control characters (< 0x20, 0x7f) with a 422 field error "Must not contain control characters."; other characters are still stored exactly as typed. `routerosString` also escapes `\n` and `\r`. Tests: E8.10, `TestSetupScriptDefensive`.
4. **Script quality (verified: yes).** Ports: bound port, else configured, else 1812/1813 (never 0). `addrs` is guarded by an RWMutex. With a placeholder (`<SERVER-IP>`, `<PUBLIC-PORT>`) the command is printed commented out under "# EDIT THEN UNCOMMENT". Interval 0: no `radius-interim-update`. Test expectation changes: E8.03b host-name walled garden now expects the quoted host (`dst-host="hotspot.lan"`); E8.03b empty-URL case gained checks (no executable line holds a placeholder, edit marker present). Tests: E8.03b, `TestSetupScriptDefensive`.
9. **Host in `dst-host` (verified: yes).** Quoted with the RouterOS helper; `public_base_url` host limited to `[A-Za-z0-9.:-[]]` by the settings validator. Test E8.11.
8. **Inline copy script (verified: yes).** `CodeBlock` is markup-only (`data-copy-target`); the delegated click handler is `/admin/static/app.js`, loaded by the layout. Test E8.12 (script referenced, no inline script, no-store header).
2. **Secret lookup in the read loop (verified: yes, cost real but small).** Chose to leave it and document: `MONITOR:` in `sniffAcct` (cache with invalidation on settings save is the fix if volume grows). Row 46.
6. **lateCloseConn (verified: no bug).** The library calls `Close` only from its own `Shutdown` after setting its shutdown flag, so the deadline error is never retried as temporary; `Server.Shutdown` closes the real sockets even if a Serve loop ended early. Documented in the type comment.
3, 5, 7: documented (`MONITOR:` comments, open-questions rows 38, 44, 45). The NAK outcome now includes the Error-Cause when the reply carries one. `/admin/router` no-store is asserted in E8.12.

Test flake fixed: E8.05 compared the raw generated secret with the HTML source; a secret containing `& < > " '` is escaped there and the test failed on such secrets. It now compares the HTML-escaped form.
