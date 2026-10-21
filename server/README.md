# MKYBOOT Windows-Native Server

A native Windows diskless-boot **server foundation** written in Go. This is the
Windows-native migration described in
[`../docs/WINDOWS_DISKLESS_MIGRATION.md`](../docs/WINDOWS_DISKLESS_MIGRATION.md).

**Scope:** it runs as a Windows console app or Windows service and provides the
server backbone — configuration, structured logging, a health endpoint, an
authenticated admin API, client registration and heartbeat with online/offline
status, a **server-side session timer**, and an **authoritative lock/unlock
state model** — with persistence across restarts. It also provides the
**network-boot delivery services** (TFTP + DHCP, both opt-in) and the
**iSCSI target management** layer (opt-in, SSH-driven).

It does **not** implement diskless boot, VHDX overlays, or the Windows client
agent. A working health/API endpoint is **not** proof of diskless boot, and the
iSCSI capability flag stays `false` until a real target passes loopback
verification.

## Requirements

- Windows (service mode) — the core also builds/tests on Linux/macOS.
- **Go 1.21+** (developed and tested with Go 1.27.1). No CGO. The only
  dependency is `golang.org/x/sys` (for the Windows service + DACL APIs).

## Build

```powershell
cd server
.\build.ps1 -Test     # gofmt check + go vet + go test + build
```

Or with plain Go (Go on PATH, e.g. `C:\Program Files\Go\bin`):

```powershell
cd server
gofmt -l .
go vet -unsafeptr=false ./...
go test -timeout 120s ./...
go build -trimpath -ldflags "-s -w" -o dist\mkyboot-server.exe .
```

The executable is written to `server\dist\mkyboot-server.exe`. Build output is
git-ignored (see `server\.gitignore`); it is **not** committed.

## Run (console)

```powershell
cd server
.\run.ps1                       # builds if needed, then runs in the foreground
```

First run creates `%ProgramData%\MKYBOOT\` with:

- `config.json` — configuration (see `config.example.json`)
- `admin.token.json` — SHA-256 hash of the admin token (**0600 / owner-only DACL**)
- `state.json` — persisted clients + lock/session state (owner-only)
- `mkyboot-server.log` — structured JSON logs

The log file honours the `log_dir` configuration key, so it can be kept on a
different volume than the rest of the data root.

On first run the server **generates** an administrator token and prints it
**once** to stderr. Save it — it is never shown or logged again. To provision
a known token non-interactively instead, set the environment variable before
first run:

```powershell
$env:MKYBOOT_ADMIN_TOKEN = "a-long-random-value-at-least-16-chars"
.\run.ps1
```

For development/testing without `%ProgramData%` write access, point the data
directory anywhere writable:

```powershell
$env:MKYBOOT_DATA_DIR = "$env:TEMP\mkyboot-dev"
.\run.ps1
```

## Endpoints

| Method & path | Auth | Purpose |
|---|---|---|
| `GET /healthz` | none | Liveness: `{"status":"ok",...}` |
| `GET /` | none | Service banner |
| `GET /api/clients` | **admin** | List clients with online/offline + last-seen |
| `POST /api/clients` | none (self-enroll) | Register a workstation; returns a per-client token |
| `POST /api/clients/heartbeat` | **client token** | Update last-seen; returns authoritative lock + session state |
| `GET /api/client/{id}/state` | **admin** | Read lock/session state |
| `POST /api/client/{id}/lock` \| `/unlock` | **admin** | Set authoritative lock state |
| `POST /api/session/{id}` | **admin** | Start a session timer (`{"minutes":N}`) |
| `DELETE /api/session/{id}` | **admin** | End the session timer |
| `GET /api/storage/health` | **admin** | Storage backend capabilities + iSCSI state |
| `POST /api/storage/images` | **admin** | Register a base image |
| `POST /api/storage/assign` | **admin** | Assign an overlay to a client |
| `GET /api/storage/iscsi/health` | **admin** | iSCSI target backend state + exported targets |
| `GET /api/storage/iscsi/targets` | **admin** | List exported iSCSI targets |
| `POST /api/storage/iscsi/luns` | **admin** | Create a LUN (`{"path":...,"size_mb":N}`) |
| `DELETE /api/storage/iscsi/luns/` | **admin** | Remove a LUN (`{"path":...}`) |
| `POST /api/storage/iscsi/export` | **admin** | Export a LUN (`{"iqn":...,"path":...,"size_mb":N}`) |
| `DELETE /api/storage/iscsi/export/` | **admin** | Unexport a target (`{"iqn":...}`) |
| `POST /api/storage/iscsi/acl` | **admin** | Restrict a target to initiator IQNs |
| `POST /api/storage/iscsi/verify` | **admin** | Loopback-verify a target |

Authentication uses `Authorization: Bearer <token>`. The admin token and
per-client tokens are compared in constant time.

## Network boot services (TFTP + DHCP)

Both services are **opt-in and off by default**. They bind only to an explicit
unicast address (never `0.0.0.0`), so the server cannot accidentally answer
DHCP on a production LAN. Validate the configuration without opening any
socket:

```powershell
.\dist\mkyboot-server.exe -config D:\MKYBOOT-Lab\config.json -dry-run
```

When enabled, the server binds UDP 69 (TFTP) and UDP 67 (DHCP) on the
configured `bind_address`, and the DHCP server answers only inside the
configured `subnet`/`range_start`/`range_end`.

## iSCSI target management

The iSCSI target itself runs on a **separate Linux VM** (LIO/`targetcli`).
The Windows server only issues management commands to it over SSH.

```json
"storage": {
  "enabled": true,
  "root": "D:/MKYBOOT-Lab/Storage/images",
  "iscsi": {
    "enabled": true,
    "host": "192.168.56.10",
    "port": 22,
    "user": "mkyboot",
    "key_path": "D:/MKYBOOT-Lab/VMs/MKYBOOT-Ubuntu/keys/mkyboot_ed25519"
  }
}
```

- `enabled` defaults to `false`. When disabled — or when `host`, `user` or
  `key_path` is missing — the backend reports itself unavailable and every
  management call fails closed.
- The SSH session is non-interactive (`BatchMode`), does not forward the agent
  or X11, and uses `StrictHostKeyChecking=yes`. The host key must already be
  trusted in `known_hosts`.
- The CHAP secret is sent on the remote shell's **standard input**, never as a
  command argument, so it does not appear in the remote process table. It is
  never logged.
- `POST /api/storage/iscsi/verify` performs a **loopback iSCSI discovery**
  (`iscsiadm --mode discovery`) on the target host. Only after that succeeds
  does the state become `verified`.

> The `iscsi` capability flag in `/api/storage/health` is hard-coded `false`.
> It is **not** flipped by configuration; it stays false until a real target
> has been verified on real hardware.

## Security model

- **No secrets on disk.** Only a SHA-256 hash of the admin token is stored, in
  a file restricted to the owner (Windows owner-only DACL; 0600 on POSIX). The
  plaintext admin token is printed once and never logged or persisted.
- **Localhost by default.** `bind_address` defaults to `127.0.0.1`, so the
  admin API is never exposed to the LAN unless an operator explicitly
  configures it.
- **Server-authoritative time.** Session expiry is computed from absolute
  server time; a client cannot extend a session by changing its own clock. If
  a session has elapsed, the next heartbeat reports the client **locked**
  (`lock_reason: "session expired"`).
- **No unauthorized lock/unlock.** Only the admin token can change lock state.
  Client tokens can only heartbeat and read their own authoritative state.
- **Bounded inputs.** Request bodies are capped (64 KiB); names, MACs, and
  session durations are validated; unknown JSON fields are rejected.
- **Least surprise on disk.** All persisted files are written atomically
  (temp + fsync + rename) and owner-only.

## Install as a Windows service (optional, requires Administrator)

```powershell
cd server
.\build.ps1
.\dist\mkyboot-server.exe -install      # registers MKYBOOTServer (delayed auto-start)
sc start MKYBOOTServer
sc query MKYBOOTServer
.\dist\mkyboot-server.exe -uninstall    # stop + remove
```

The service is configured to **restart on failure** (5s / 30s / 60s). When
started by the Service Control Manager the server runs headless and reports
Start/Running/Stop status to the SCM. Logs go to
`%ProgramData%\MKYBOOT\mkyboot-server.log`.

> Note: when installed as a service, the one-time admin token is not visible on
> a console. Provision it up front with `MKYBOOT_ADMIN_TOKEN` (set as a
> machine/service environment variable) before the first service start.

## Test

```powershell
cd server
go test -timeout 120s ./... -v
```

Coverage includes: health without auth, admin auth rejection/acceptance,
registration (valid + invalid MAC/name/JSON), heartbeat (valid + bad token),
client listing, lock/unlock (authorized + unauthorized + unknown client),
session start/end + invalid duration, **server-authoritative expiry forcing a
lock**, request body size limit, persistence across a simulated restart, and
owner-only file permissions.

## Current status

**Implemented and unit-tested:** config, structured logging, health,
authenticated admin API, client registry + heartbeat, session timer,
authoritative lock state, persistence, graceful shutdown, Windows service
lifecycle, TFTP server (RFC 1350), DHCP server (isolated-lab only), VHDX image
management, and SSH-driven iSCSI target management against a remote Linux
LIO/targetcli host.

**Not implemented / not proven:** diskless boot itself, VHDX differencing
overlay write-back isolation, and the Windows client agent. The iSCSI
capability flag remains `false` and the iSCSI layer has **not** yet been
exercised against a live Ubuntu target — see
[`../docs/PHASE4F_UBUNTU_ISCSI_INTEGRATION.md`](../docs/PHASE4F_UBUNTU_ISCSI_INTEGRATION.md)
for the acceptance sequence. A passing unit test is **not** proof of diskless
boot.

