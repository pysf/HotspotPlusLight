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

`make run` builds and starts everything with data in `./data` (it keeps a development key in the git-ignored `.dev-secret-key`). `make test` runs all tests with the race detector, `make ci` adds `go vet` and a check that the committed CSS is current.

The admin CSS is built with Tailwind v4 (standalone CLI, no Node). After changing classes in the `.templ` files or `internal/web/ui/input.css`, run `make css` and commit `internal/web/static/app.css`. The first run downloads the pinned CLI into `tools/` (needs network). The built binary needs neither. See `docs/ui-notes.md`.

### First start

On the very first start the server creates `hsp.db` in `HSP_DATA_DIR`, generates the admin password and prints it **once** to stdout in a block starting with `=== HotspotPlusLight first start ===`. It is never shown again, so copy it from the terminal (or from the service journal) right away.

Change the password at any time with the command line (there is no password form in the web UI):

```sh
./bin/hotspotpluslight admin set-password     # or: make set-password (for the ./data database)
```

It prompts twice without echo (or reads one line with `--stdin`). Rules: at least 16 characters with upper case, lower case, digit and symbol. It ends every open admin session.

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

`systemctl stop` sends SIGTERM: the server stops accepting, lets accounting requests in flight finish, then closes the database. The first-start password appears in `journalctl -u hotspotpluslight` (stdout).
