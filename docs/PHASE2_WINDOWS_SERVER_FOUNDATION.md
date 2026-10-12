# MKYBOOT-SERVER — Phase 2: Windows-Native Server Foundation

**Status: Foundation implemented, tested, and built. No PXE/diskless boot is claimed.**

| Repository | Branch | Commit |
|---|---|---|
| `mikkkoyy/MKYBOOT-SERVER` | `main` | see completion report |

This report records the Phase 2 deliverable: a native Windows Go server
foundation under `server/`, created per
[`WINDOWS_DISKLESS_MIGRATION.md`](WINDOWS_DISKLESS_MIGRATION.md) Phase 2. It is
a **separate Go module** from the existing `launcher/`; the launcher is reused
(read-only) for status display and is **not** modified. No Linux/WSL/Docker/VM
is required. No DHCP/TFTP/iSCSI/PXE service is started.

## What was built

A single Go binary (`server\dist\mkyboot-server.exe`) that runs as a Windows
console app **or** a Windows service, providing:

- **Configuration** under `%ProgramData%\MKYBOOT\config.json` (overridable with
  `MKYBOOT_DATA_DIR`), safe localhost default (`127.0.0.1:8888`), validated on
  load and save.
- **Health endpoint** `GET /healthz` (unauthenticated).
- **Authenticated admin API** using `Authorization: Bearer <token>`, compared in
  constant time. Admin token is provisioned on first run (random, shown once) or
  via `MKYBOOT_ADMIN_TOKEN`; only its SHA-256 hash is persisted.
- **Client registration + heartbeat** (`POST /api/clients`, `POST
  /api/clients/heartbeat`) with online/offline status and last-seen time.
- **Server-side session timer** (`POST`/`DELETE /api/session/{id}`) — expiry is
  absolute server time, so a client clock change cannot extend a session.
- **Authoritative lock/unlock** (`POST /api/client/{id}/lock|unlock`, `GET
  /api/client/{id}/state`). Only the admin token may change it. When a session
  timer elapses, the next heartbeat reports the client **locked**.
- **Structured JSON logging** and actionable startup errors.
- **Persistence** of config, admin credential, clients, and lock/session state
  across restarts, written atomically and restricted to the file owner
  (Windows owner-only DACL; 0600 on POSIX).
- **Graceful shutdown** and **Windows service lifecycle** (`-install`,
  `-uninstall`, SCM start/stop/status, restart-on-failure) via
  `golang.org/x/sys/windows/svc`.

## Files added (all under `server/`)

| File | Purpose |
|---|---|
| `go.mod`, `go.sum` | Module `github.com/mikkkoyy/MKYBOOT-SERVER/server`; dep `golang.org/x/sys v0.11.0` |
| `main.go` | Entry point: console vs service, `-install`/`-uninstall`/`-config`, signal handling, one-time token display |
| `config.go` | Config type, validate (safe defaults), load/save (atomic, 0600) |
| `admin.go` | Admin token provisioning (env/generated) + constant-time verify; stores only a hash |
| `paths.go` | `%ProgramData%\MKYBOOT` layout + `MKYBOOT_DATA_DIR` override |
| `atomic.go` | Atomic temp+fsync+rename writes + owner-only DACL hook |
| `log.go` | Structured JSON-line logger |
| `state.go`, `state_ops.go` | Client registry, heartbeat, lock/session state, persistence, MAC/id helpers |
| `server.go` | HTTP server, middleware (panic recovery, body limit), routing, graceful shutdown |
| `handlers.go`, `admin_handlers.go` | Endpoints incl. launcher-compatible `/?api=status` |
| `service_windows.go`, `service_other.go` | SCM service lifecycle (Windows) / stubs (other) |
| `fileperm_windows.go`, `fileperm_other.go` | Owner-only DACL on Windows |
| `*_test.go` | Unit + HTTP integration tests |
| `build.ps1`, `run.ps1`, `config.example.json`, `README.md` | Build, launch, example config, docs |
| `.gitignore` | Ignore `dist/`, `*.exe`, `*.log` |

## Build & test (executed on this host)

Go 1.27.1 (`C:\Program Files\Go\bin\go.exe`). Commands run from `server/`:

```
gofmt -l .                         -> (no output: clean)
go vet -unsafeptr=false ./...      -> ok (exit 0)
go test -timeout 120s ./...        -> ok  github.com/mikkkoyy/MKYBOOT-SERVER/server
go build -trimpath -ldflags "-s -w" -o dist\mkyboot-server.exe .  -> BUILD OK (6.73 MB)
.\build.ps1 -Test                  -> gofmt + vet + test + build all PASS
```

### Live smoke test (real process, real HTTP)

1. Started `dist\mkyboot-server.exe` with `MKYBOOT_ADMIN_TOKEN` set.
2. `GET /healthz` → `{"service":"mkyboot-server","status":"ok","version":"0.1.0-phase2"}`.
3. `GET /api/clients` **without** token → **HTTP 401**; **with** admin token → `{"clients":[],"count":0}`.
4. `POST /api/clients` (register) → `client-0001` + per-client token.
5. `POST /api/clients/heartbeat` → ok with authoritative lock/session fields.
6. `POST /api/client/{id}/lock` **without** admin → **HTTP 401**; **with** admin → `"locked":true`.
7. `POST /api/session/{id} {"minutes":5}` → `session_expires_at` set; `GET .../state` → `session_remaining` ~299s.
8. **Restart** the process against the same data dir → `GET .../state` → `locked=True reason=lock` (state persisted).

## Test results

| Check | Result | Evidence |
|---|---|---|
| gofmt | **PASS** | `gofmt -l .` empty |
| go vet (`-unsafeptr=false`) | **PASS** | exit 0 |
| go test (`-timeout 120s`) | **PASS** | all tests ok (config, admin, state, API, expiry, persistence, permissions, launcher-compat status) |
| Windows AMD64 build | **PASS** | `dist\mkyboot-server.exe` produced |
| Health endpoint (live) | **PASS** | `{"status":"ok",...}` |
| Admin auth rejection/acceptance (live) | **PASS** | 401 without/with wrong token; 200 with valid |
| Registration + heartbeat (live) | **PASS** | client created, heartbeat ok, bad token 401 |
| Lock/unlock authz (live) | **PASS** | 401 without admin; 200 with admin |
| Persistence across restart (live) | **PASS** | `locked=True` after process restart |
| Windows service install/start | **NOT RUN** | Requires elevation + interactive SCM; code present (`-install`), not executed here |
| PXE/DHCP/TFTP/iSCSI/diskless boot | **NOT RUN / BLOCKED** | Not implemented in this phase; separate milestones |

## Security notes

- No secrets on disk: only a SHA-256 hash of the admin token, in an owner-only
  file. The plaintext is shown once and never logged.
- Admin API bound to `127.0.0.1` by default; never LAN-exposed unless configured.
- Constant-time token comparison; request bodies capped (64 KiB); strict input
  validation; unknown JSON fields rejected.
- Server-authoritative session time; clients cannot extend a session by
  changing their clock, and only the admin can lock/unlock.
- No production networking, firewall, adapter, or DHCP changes.

## Launcher integration

The existing `launcher/` was **not modified**. The server additionally serves
`GET /?api=status` in the exact shape the launcher already parses
(`server.version`, `clients.{total,online,offline}`, `services.{dhcp,tftp,iscsi}`,
and `{"error":"Not authenticated"}` when unauthenticated), so the launcher shows
this server as reachable/online without any launcher change. Diskless services
are reported `false` because they are not implemented yet — the launcher will
show "Degraded", which is truthful for Phase 2. The launcher is never made to
falsely claim the server is running.

## Scope / remaining work (not in this phase)

PXE network boot, DHCP/TFTP, iSCSI target, VHDX differencing disks, write-back
isolation, and the Windows client agent are separate milestones and remain
**BLOCKED / not implemented**. This phase does not claim diskless boot.

