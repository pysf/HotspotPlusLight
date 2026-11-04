# HotspotPlusLight

A small hotspot manager for **one MikroTik router**: Go + HTMX + SQLite, one binary, one database file, a native RADIUS server (PAP).
The admin defines internet plans, creates members (one by one or as printable voucher batches) and watches who is online.
The server enforces data, time, validity and concurrent-session limits at login and through accounting, and can kick a session (RADIUS Disconnect).

The member login page is **not** served by this program. It is a static HTML page stored on the router, which asks a small JSON API of this server whether a login is allowed. This program generates that page for you (38 themes) as a downloadable folder.

The full design is in `PLAN.md`; decisions awaiting the owner are in `docs/open-questions.md`.

## Build and run

Requires Go (see `go.mod`). No CGO, no external services.

```sh
make build                                           # produces bin/hotspotpluslight
export HSP_SECRET_KEY="$(openssl rand -base64 32)"   # keep it: see Backup
./bin/hotspotpluslight serve
```

`./bin/hotspotpluslight gen-key` prints a new key, so `openssl` is not needed. Instead of environment variables you can put `KEY=VALUE` lines in a file and start with `serve --env-file <file>` (also works for `admin set-password`). A real environment variable wins over the file.

On Windows, build with `make build-windows` (bin/hotspotpluslight.exe, pure Go). To run it as a Windows service, open a terminal as Administrator and run `hotspotpluslight.exe service install` (default folder `C:\ProgramData\HotspotPlusLight`, change it with `--dir`). This creates `hsp.env` with a new secret key, and the service starts with Windows and restarts after a crash. Then run `hotspotpluslight.exe service start`. Logs and the first-start block are in `hsp.log` in that folder. Use `service stop` and `service uninstall` to stop and remove it; your data is kept. Keep a copy of `hsp.env`: the key is needed to read stored passwords. To reset the admin password later, run `hotspotpluslight.exe admin set-password --env-file C:\ProgramData\HotspotPlusLight\hsp.env` (use your own folder if you changed `--dir`). In the env file, a space followed by `#` starts a comment in an unquoted value. Allow ports 8080, 8081 (TCP) and 1812, 1813 (UDP) in the Windows firewall.

`make run` builds and starts everything with data in `./data` (it keeps a development key in the git-ignored `.dev-secret-key`). `make test` runs all tests with the race detector, `make ci` runs `go vet` and the tests.

The admin CSS and JS are plain files in `internal/web/static/` (no build step, no Node). The server bundles and hashes them at startup. Set `HSP_DEV=1` to see every UI component at `/admin/dev/components`. See `docs/ui-notes.md`.

### First start

On the very first start the server creates `hsp.db` in `HSP_DATA_DIR`, creates the admin account with the default login `admin` / `admin` (the username can be set with `HSP_ADMIN_USERNAME`) and prints a block starting with `=== HotspotPlusLight first start ===` that says so. When you log in, you are sent to the password page and cannot use any other page until you set a new password. The new password needs at least 10 characters (at most 72 bytes), must not be the username, `admin` or a very common password. There are no other rules. Press **Generate** to get a random one. Log in right after the first start: until the password is changed, anyone who can reach port 8081 can use `admin` / `admin`.

You can change the password at any time on the **Password** page (link in the top bar). The current password is asked, and wrong tries are throttled like login. Your other admin sessions end; this one stays.

If you forget the password, reset it with the command line:

```sh
./bin/hotspotpluslight admin set-password     # or: make set-password (for the ./data database)
```

It prompts twice without echo (or reads one line with `--stdin`). It uses the same rules as the page, and it ends every open admin session.

No plan exists after the first start: log in to the admin panel and create plans before adding members.

## Configuration (environment)

| Variable | Default | Meaning |
|---|---|---|
| `HSP_SECRET_KEY` | (required) | base64 of 32 random bytes; AES-GCM key for stored member passwords |
| `HSP_DATA_DIR` | `data` | directory holding `hsp.db` |
| `HSP_HTTP_ADDR` | `:8080` | public listener: page API for the login page, and `/healthz` |
| `HSP_ADMIN_ADDR` | `:8081` | admin panel listener |
| `HSP_RADIUS_AUTH_ADDR` | `:1812` | RADIUS authentication (UDP) |
| `HSP_RADIUS_ACCT_ADDR` | `:1813` | RADIUS accounting (UDP) |
| `HSP_ADMIN_USERNAME` | `admin` | admin username, used at first start |
| `HSP_RADIUS_SHARED_SECRET` | generated | RADIUS shared secret, used at first start (later: Settings page) |
| `HSP_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `HSP_LOG_FORMAT` | `text` | `text` (key=value lines) or `json`; logs go to stderr |

The server refuses to start without `HSP_SECRET_KEY`, or with a key that cannot decrypt the database. Everything else (site title, time zone, CoA port, accounting interval, public base URL, voucher settings, ...) is edited on the **Settings** page and applies without a restart. The only exception: the listener addresses above.

## The two listeners and the firewall

| Listener | Default | Serves | Who may reach it |
|---|---|---|---|
| public | `:8080` | `/api/v1/*` (used by the login page) and `/healthz` | the hotspot clients, through the router's walled garden |
| admin | `:8081` | the admin panel under `/admin` | **only you**: plain HTTP, no TLS |

The admin panel must not be reachable from the hotspot network. Bind it to a management address (`HSP_ADMIN_ADDR=192.168.1.10:8081`) and/or block port 8081 from the hotspot subnet in the server's firewall. The router's walled-garden entry generated below allows only the public port.
RADIUS needs UDP 1812 and 1813 open from the router to the server, and UDP 3799 (Disconnect) open from the server to the router.

## Router configuration

1. In the admin panel open **Settings** and set **Public base URL** (`public_base_url`) to the address the hotspot clients use to reach this server, for example `http://192.168.88.2:8080` (use the server's IP address). Set the RADIUS shared secret (or keep the generated one) and the Disconnect port (router default 3799).
2. Open **Router** (`/admin/router`). It shows a **setup script** for RouterOS: the RADIUS server entry with the shared secret and ports, `/radius incoming` for Disconnect, the hotspot profile (`use-radius=yes login-by=http-pap radius-accounting=yes radius-interim-update=...`) and a walled-garden entry for the server's public port. Edit the profile name if yours is not `hsprof1`, then paste it into a RouterOS terminal.
3. On the same page download **files.zip** (the default theme, or pick one of the 38 in the theme gallery). Unzip it, edit `hotspot/login.html` (footer, texts, colors), replace `logo.png` and `background.jpg` with your own, and upload the content of the folder to the router (Files menu in WinBox, or FTP) into a directory named `hotspot`; the script's last command points the hotspot profile at it.
4. Log in from a phone on the hotspot network with a member you created. The Router page also has a **link status** panel (last Access-Request, Accounting packet and Disconnect with their source addresses, and packets with a wrong shared secret). It lives in memory and starts empty after a restart.

The generated RouterOS commands follow the plan and the legacy setup script but have **not yet been run on a real router**: every command is marked `VERIFY-ON-HARDWARE` in the script. Work through the hardware lab checklist in `PLAN.md` section 13 (PAP reaches RADIUS, the MikroTik attribute semantics, Session-Timeout, interim updates, Disconnect, the login page's cross-origin calls from a captive-portal pop-up) before relying on the setup.

## Backup

State is one SQLite file plus the key. Take a consistent copy while the service is running with:

```sh
sqlite3 hsp.db ".backup backup-$(date +%F).db"
```

Keep `HSP_SECRET_KEY` together with every backup, in a safe place. **If the key is lost, the stored member passwords are unrecoverable** and the server will not start on that database. There is no built-in backup or restore feature.

## systemd (example)

```ini
[Unit]
Description=HotspotPlusLight
After=network-online.target

[Service]
User=hsp
EnvironmentFile=/etc/hotspotpluslight.env      # HSP_SECRET_KEY=..., HSP_DATA_DIR=/var/lib/hotspotpluslight
ExecStart=/usr/local/bin/hotspotpluslight serve
AmbientCapabilities=CAP_NET_BIND_SERVICE
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

`systemctl stop` sends SIGTERM: the server stops accepting, lets accounting requests in flight finish, then closes the database. The first-start block appears in `journalctl -u hotspotpluslight` (stdout).

## Performance
The capacity test raises the number of online users step by step (500 up to 50000) and stops at the first step that fails. Run `make perf` (the full run, about 25-30 minutes): only this run rewrites this section and `docs/perf-results.md`. `make perf-quick` (short check) and runs with your own `HSP_PERF_STEPS` or `HSP_PERF_STEP_SECONDS` only print to the screen. The test never fails the build, unless you set `HSP_PERF_MIN_USERS`.

<!-- perf:start -->
Max online users on this machine: **30000** (last passing step). Run on 2026-11-04, each step 120 s after its warm-up.

Machine: OS darwin/arm64, CPU Apple M5, 10 cores, go1.26.0.

| Online users | Result | Start burst packets/s | Burst p50/p95/p99 (ms) | Steady packets/s | Auth p50/p95/p99 (ms) | Accounting p50/p95/p99 (ms) | Pages p50/p95/p99 (ms) | Lost | Server CPU % | Server RSS MB | DB MB |
|---|---|---|---|---|---|---|---|---|---|---|---|
| 500 | pass | 17 | 3.2/6.5/8.2 | 9 | 2.6/4.0/6.9 | 1.6/3.1/3.6 | 3.2/5.7/7.0 | 0 | 2 | 32 | 4.4 |
| 1000 | pass | 34 | 2.3/3.2/3.5 | 17 | 1.9/2.7/2.8 | 1.4/2.1/2.5 | 2.8/6.5/6.7 | 0 | 4 | 33 | 4.6 |
| 2000 | pass | 68 | 2.3/3.1/3.6 | 34 | 1.5/2.6/2.9 | 1.2/1.9/2.4 | 3.1/7.2/8.2 | 0 | 7 | 34 | 5.0 |
| 5000 | pass | 170 | 1.8/3.1/3.5 | 86 | 1.0/2.0/2.5 | 1.3/1.9/2.4 | 5.2/12.1/21.3 | 0 | 17 | 36 | 6.2 |
| 10000 | pass | 340 | 1.6/2.3/3.1 | 172 | 0.9/1.7/2.1 | 1.8/3.1/3.6 | 7.6/13.5/16.4 | 0 | 30 | 40 | 8.1 |
| 20000 | pass | 513 | 1.2/3.1/4.4 | 343 | 0.9/1.7/2.1 | 2.2/3.9/4.7 | 14.2/24.2/25.0 | 0 | 50 | 41 | 12.2 |
| 30000 | pass | 517 | 1.3/37.7/114.3 | 513 | 0.7/1.5/2.0 | 2.4/4.3/5.3 | 18.7/25.2/42.2 | 0 | 61 | 44 | 16.1 |
| 40000 | FAIL | 519 | 1.4/889.0/1437.7 | 681 | 0.7/1.6/2.0 | 2.5/4.7/5.9 | 21.8/139.6/657.8 | 144 | 69 | 48 | 21.1 |

Step 40000 failed: 144 replies lost or later than 1 s; the load generator itself fell behind: dispatch lag 1.275s > 1s.

Full report and explanation: [docs/perf-results.md](docs/perf-results.md).
<!-- perf:end -->
