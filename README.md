# HotspotPlusLight

A single-binary hotspot manager for one MikroTik router: Go + SQLite, native RADIUS (PAP).
See `PLAN.md` for the full design. Status: Phase 0 (skeleton, harness, first-boot seeding).

## Run

```sh
make build
export HSP_SECRET_KEY="$(openssl rand -base64 32)"   # keep it: see Backup
./bin/hotspotpluslight serve
```

On the very first start the server creates `hsp.db` in `HSP_DATA_DIR`, generates the admin
password and prints it **once** to stdout in a block starting with
`=== HotspotPlusLight first start ===`. It is never shown again; change it later with
`hotspotpluslight admin set-password` (coming in Phase 1).

No plan exists after the first start: the admin creates plans in the web UI (admin listener) before adding members.

Make targets: `make run` (build and start the whole stack on ports 8080/8081/1812/1813 with data in `./data`; uses `HSP_SECRET_KEY` if set, otherwise a key kept in the git-ignored `.dev-secret-key`), `make test` (unit + end-to-end tests, `go test -race`), `make vet`, `make ci` (`vet` + `test`), `make build`, `make clean`. Other commands: `hotspotpluslight version`.

## Environment

| Variable | Default | Meaning |
|---|---|---|
| `HSP_SECRET_KEY` | (required) | base64 of 32 random bytes; AES-GCM key for stored member passwords |
| `HSP_DATA_DIR` | `data` | directory holding `hsp.db` |
| `HSP_HTTP_ADDR` | `:8080` | public listener: page API and `/healthz` |
| `HSP_ADMIN_ADDR` | `:8081` | admin listener (block it from the hotspot network) |
| `HSP_RADIUS_AUTH_ADDR` | `:1812` | RADIUS authentication (UDP) |
| `HSP_RADIUS_ACCT_ADDR` | `:1813` | RADIUS accounting (UDP) |
| `HSP_ADMIN_USERNAME` | `admin` | first-start admin username |
| `HSP_RADIUS_SHARED_SECRET` | generated | first-start RADIUS shared secret |
| `HSP_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |

The server refuses to start without `HSP_SECRET_KEY`, or with a key that cannot decrypt the
database (`settings.key_check`).

## Backup

State is one SQLite file plus the key. Take a consistent copy while the service is running with:

```sh
sqlite3 hsp.db ".backup backup-$(date +%F).db"
```

Keep `HSP_SECRET_KEY` together with every backup (but not in the same unprotected place):
without it the stored member passwords cannot be decrypted and the server will not start.

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
