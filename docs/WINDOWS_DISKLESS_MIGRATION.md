# MKYBOOT-SERVER — Windows-Native Diskless Migration (Phase 1: Audit)

**Status: Audit complete. No code rewritten, no network services started. Implementation phases 2–6 are planned, not executed.**

| Repository | Branch | Commit (audit baseline) |
|---|---|---|
| `mikkkoyy/MKYBOOT-SERVER` | `main` | `2e3ca1b9a9cedec70a9f87e101527cf31fff2d35` |

This document is the **Phase 1 compatibility audit** for converting MKYBOOT-SERVER from its current Linux/Ubuntu runtime into a **Windows-native** diskless-boot server, so that no Linux, WSL, Docker, or Linux VM is required at runtime for the final Windows edition. It inventories every server component, classifies each for Windows reuse, proposes a target architecture, a migration sequence, the Windows dependency list, security risks, and a test plan. **It performs no destructive rewrite and starts no service.** Later phases implement one milestone at a time against this plan.

## Executive summary

- The existing backend is a **Lua/OpenResty** application (`bin/mkyctl.lua`, ~3500 lines) served by `nginx-extras`, plus a Linux IPC daemon (`bin/server.lua`). It shells out to Linux-only tools: `tgtadm` (iSCSI), `zfs` (snapshots/zvols), `qemu-img`/`qemu-nbd` (QCOW2/NBD), `lsof`, `etherwake`, and depends on `/dev/nbdN`, `/dev/urandom`, `/srv/*`, `/etc/dhcp`, unix domain sockets, and `systemctl`. **None of this runs on Windows.**
- The repository **already contains a native Windows application** — the **Go launcher** (`launcher/*.go`): a tray GUI that talks HTTP to the MKYBOOT API, with single-instance enforcement, registry integration, Win32 rendering, config persistence, unit tests, and a reproducible Windows build. **This is the foundation to reuse and extend** for both the Windows server backend and the Windows client agent.
- The project's differentiating features the Linux version does **not yet implement** (and the migration must add natively): the **PC session timer / countdown**, **authoritative lock/unlock state**, **Windows client agent**, and **VHDX** image support. These are net-new on any platform.
- **Recommendation:** implement the Windows server as a **single Go service** (HTTP API + management), reuse the launcher's patterns for the client agent, use **VHDX + differencing disks** for per-client writable overlays on NTFS/ReFS, and serve PXE via a **Windows DHCP + TFTP + iPXE** stack. The **iSCSI target on Windows** is the largest unresolved dependency and is carried as a documented decision point (see §5, Phase 4) — it is **not** assumed available.

## 1. Component inventory and classification

Legend: **R** = Reusable on Windows as-is; **W** = Requires a Windows implementation; **D** = Requires an external Windows-compatible dependency; **V** = Not yet verified / to be evaluated in a later phase.

| # | Component | File(s) | What it does | Class | Notes |
|---|---|---|---|---|---|
| 1 | Web/API core (router, sessions, auth) | `bin/mkyctl.lua` | HTTP router (`?api=...`), admin UI, iPXE script generation, client/image CRUD, session + auth, logs | **W** | Logic portable; runtime is OpenResty (`ngx.*`). Reimplement in Go; keep the same HTTP API contract (§2). |
| 2 | Config + embedded web UI (HTML/JS/CSS) | `src/cfg.lua`, web pages in `mkyctl.lua` | Default config; Bootstrap admin dashboard; iPXE page templates | **R / W** | HTML/JS reusable as static assets; config paths (`/srv`, `/etc`, `/var`) re-point to Windows. |
| 3 | Image lifecycle | `src/image.lua` | `qemu-img` create/child(backing)/commit/delete + `lsof` usage check | **W** | `qemu-img` exists for Windows; QCOW2-with-backing maps to **VHDX differencing disks**. `lsof` has no Windows equivalent (use file locks / API). |
| 4 | IPC daemon (NBD connect/disconnect) | `bin/server.lua` | Unix-socket JSON daemon wrapping `qemu-nbd --connect` to `/dev/nbdN` | **W** | NBD is Linux-only. Windows uses **named pipes** and an iSCSI/VHDX target instead of qemu-nbd. Strict arg-validation pattern is worth reusing. |
| 5 | IPC client | `bin/client.lua` | Sends JSON ops to the daemon over a unix socket | **W** | Replace with Go in-process calls (no separate daemon on Windows). |
| 6 | DHCP config generation | `mkyctl.lua` `ExportDHCP`, `examples/etc/dhcp/dhcpd.conf` | Emits ISC `dhcpd.conf` with iPXE options, `next-server`, subnets, per-host MAC→IP | **R (logic) / D (server)** | Generation logic reusable; the **server** (ISC DHCP) is Linux. On Windows use the **Windows DHCP Server role** or a documented Go/3rd-party DHCP. |
| 7 | TFTP boot artifacts | `srv/tftp/ipxe.efi`, `ipxe.kpxe`, `bg.png` | UEFI + BIOS iPXE chainloaders and menu background | **R** | iPXE binaries are platform-independent and already present; they are **served** by TFTP (#8). |
| 8 | TFTP server | `tftpd-hpa` (Linux dep) | Serves `ipxe.efi`/`ipxe.kpxe` over UDP 69 | **D** | Windows: **WDS TFTP**, a small Go TFTP server, or the DHCP vendor's TFTP. Must serve UEFI `ipxe.efi` first. |
| 9 | iSCSI target | `tgt`/`tgtadm` (Linux dep) | Creates/deletes targets, LUNs bound to NBD devices | **D / V** | **Windows has only an iSCSI initiator, not a target.** Options/trade-offs evaluated in §5/Phase 4. **Do not assume a target exists.** |
| 10 | Storage / snapshots / zvols | `zfs` (`zpool`/`zfs`) (Linux dep) | CoW snapshots, `dynblock` zvol images, `writeback` dataset | **W / D** | No native ZFS on Windows. Replace with **VHDX differencing disks on NTFS/ReFS** + server-side locking for isolation. |
| 11 | QEMU image tooling | `qemu-img`, `qemu-nbd` | Create/commit QCOW2; attach as NBD | **R (qemu-img) / W (qemu-nbd)** | `qemu-img` has a Windows build; `qemu-nbd` is Linux-only, replaced by the storage/target layer. |
| 12 | Wake-on-LAN | `etherwake` (Linux dep) | Magic-packet WoL to power on clients | **W** | Reimplement in Go (UDP magic packet); cross-platform. |
| 13 | Web terminal | `shellinabox` (Linux dep) | Browser-based server shell | **V** | Optional convenience; not core to diskless boot. Decide later; may drop. |
| 14 | Service supervision | `systemctl`, `test/etc/init.d/mkybootd` | start/stop/restart/enable services | **W** | Windows: **Service Control Manager** (Go `golang.org/x/sys/windows/svc`) for the server; client agent is its own Windows service. |
| 15 | Launcher / GUI (companion) | `launcher/*.go` | Native Win32 tray app; HTTP client to `?api=status`, login, `server restart/stop`; single-instance; registry autostart | **R** | **Reusable as-is and extensible.** Provides HTTP client, config, Win32 patterns, build, and tests for the Windows edition. |
| 16 | Authentication | `mkyctl.lua` `mkyboot.inc.auth` | First-run admin setup; iterated-SHA1 KDF (250k) + salt; constant-time compare | **W (impl) / V (KDF)** | Review KDF for Windows (prefer stronger). `/dev/urandom` → Go `crypto/rand`; session files → Windows-backed store. |
| 17 | Sessions | `mkyboot.inc.session` | File-based server-side sessions, 30-min TTL, cookie `HttpOnly; SameSite=Strict`, fixation-safe rotate | **W** | Reimplement in Go; same cookie flags and TTL. |
| 18 | PC timer / countdown | **none** | — | **W (net-new)** | **Not present anywhere in the repo.** Design and build server-authoritative. See Phase 2/5. |
| 19 | Lock/unlock state | **none** | — | **W (net-new)** | **Not present.** Server-authoritative lock/unlock must be built. See Phase 2/5. |
| 20 | Windows client agent | **none** | — | **W (net-new)** | **Not present.** New Go service (register, heartbeat, signed commands, enforce lock/expiry, safe reconnect, admin recovery). See Phase 5. |
| 21 | CI / tests | `.github/workflows/ubuntu-integration.yml`, `test/runtime/*.lua`, `launcher/*_test.go` | Lua syntax/regression, DHCP syntax, Go vet/test/build (Ubuntu + Windows runners) | **R (Go) / W (Lua)** | Go launcher tests already run on `windows-2022`. Lua suites target the Linux runtime and do not exercise a Windows server. |

## 2. Component mapping (Linux → Windows)

The HTTP API contract is preserved so the existing Windows launcher and future agents keep working. Endpoint names mirror the current `mkyctl.lua` router (`?api=status|clients|client|images|logs|server&op=`, `?login=`, `?logout=`, `?getmebootargs=<ip>`).

| Linux component | Windows replacement | Mechanism |
|---|---|---|
| `nginx-extras` + OpenResty Lua (`ngx.*`) | **Go HTTP server** (`net/http`) | Single Go binary; same JSON HTTP API |
| `bin/mkyctl.lua` router/UI logic | Go handlers + static assets | Port endpoint-for-endpoint; embed HTML/JS via `embed` |
| `bin/server.lua` unix-socket NBD daemon | In-process storage controller | No qemu-nbd; Windows VHDX/iSCSI layer; named pipes only if a helper is needed |
| `tgt` iSCSI target | **TBD — see §5/Phase 4** | Third-party Windows iSCSI target, or non-iSCSI boot; trade-offs documented |
| `zfs` snapshots/zvols | **VHDX differencing disks** on NTFS/ReFS + explicit locking | Base VHDX read-only; per-client differencing VHDX |
| `qemu-img` overlay create/commit | `qemu-img` (Windows) or Windows VHD APIs | Prefer a documented, tested tool |
| `isc-dhcp-server` + generated `dhcpd.conf` | **Windows DHCP Server role** (or documented Go/3rd-party DHCP) on the isolated NIC | Same iPXE options/`next-server`; conflict detection before start |
| `tftpd-hpa` | **WDS TFTP** or a small Go TFTP server | Serve `ipxe.efi` (UEFI) first; `ipxe.kpxe` (BIOS) documented separately |
| `systemctl` / init.d | **Windows Service (SCM)** via `x/sys/windows/svc` | Start/stop/restart/status; recovery actions; bounded retries |
| `/dev/urandom` | Go `crypto/rand` | Session IDs, salts, tokens |
| `/srv/*`, `/etc/dhcp`, `/var/log`, `/var/lock` | `%ProgramData%\MKYBOOT\...` (server) / `%AppData%` (per-user) | Structured logs; atomic writes |
| `etherwake` | Go WoL (UDP magic packet) | Power-on clients |
| Launcher (existing) | **Reuse + extend** into client agent and admin UI | Same Win32/service idioms and tests |

## 3. Target architecture (Windows)

```text
                ┌───────────────────────────────────────────────┐
                │        MKYBOOT Windows Server (Go)            │
                │  Runs as a Windows Service (SCM), no console  │
                │                                               │
                │  ┌───────────────┐  ┌──────────────────────┐  │
                │  │ HTTP/JSON API │  │ Admin web UI (embed) │  │
                │  │ status/clients│  │ login/session/auth   │  │
                │  │ images/logs   │  │ timer + lock/unlock  │  │
                │  └───────┬───────┘  └──────────────────────┘  │
                │          │                                     │
                │  ┌───────▼──────────────────────────────────┐ │
                │  │ Core controller                          │ │
                │  │  - client registry + heartbeat          │ │
                │  │  - session timers (authoritative)       │ │
                │  │  - lock/unlock state machine            │ │
                │  │  - iPXE script generator (getmebootargs)│ │
                │  └───┬───────────────┬───────────────┬──────┘ │
                │      │               │               │        │
                │  ┌───▼─────┐   ┌─────▼─────┐   ┌─────▼─────┐  │
                │  │ Storage │   │ PXE svcs  │   │  WoL      │  │
                │  │ VHDX +  │   │ DHCP+TFTP │   │ (UDP)     │  │
                │  │ diff.   │   │ +iPXE     │   └───────────┘  │
                │  │ NTFS/   │   │ (isolated │                   │
                │  │ ReFS    │   │  NIC)     │                   │
                │  └───┬─────┘   └─────┬─────┘                   │
                │      │               │                          │
                │  ┌───▼─────────────┐ │   iSCSI target = TBD     │
                │  │ iSCSI target?   │─┘   (§5/Phase 4 decision)  │
                │  └─────────────────┘                             │
                └───────────────┬─────────────────────────────────┘
                                │  isolated lab VLAN / NIC (no uplink)
             DHCP ─ TFTP ─ iPXE ─ HTTPS API
                                │
        ┌───────────────────────▼───────────────────────────────┐
        │  MKYBOOT Windows Client Agent (Go Windows Service)    │
        │  register • heartbeat • signed commands • enforce      │
        │  server-authoritative expiry/lock • safe reconnect •   │
        │  admin recovery / emergency unlock                     │
        └────────────────────────────────────────────────────────┘
```

Principles: one Go service owns the API, timers, and lock state; storage is file-based VHDX with per-client differencing so one client cannot write another's data and the base stays read-only; PXE services bind only to the isolated NIC; the client agent is a separate signed service that treats the server as authoritative.

## 4. Migration sequence

Implement one milestone per commit; each is independently testable and reversible.

1. **Phase 2 — Windows server foundation.** Go service skeleton via `x/sys/windows/svc`: install/start/stop/restart, health, structured logs, config in `%ProgramData%\MKYBOOT`, first-run admin setup, session auth, authenticated admin API (`/api/*`), client registry + heartbeat, and the server-authoritative session timer + lock/unlock state machine. Extend the existing launcher/management surface. **No network/PXE yet.** Reuse launcher `config.go`/`server.go` patterns.
2. **Phase 3 — PXE and network services.** Detect existing DHCP/TFTP and port conflicts before starting anything; dry-run config validator; bind only to the isolated NIC; never touch the production DHCP or adapters. UEFI PXE (`ipxe.efi`) first; BIOS (`ipxe.kpxe`) documented separately. Verify bootloader files are served; emit diagnostics for DHCP offers, TFTP transfers, and boot failures.
3. **Phase 4 — Windows diskless storage.** Decide the iSCSI target (see §5). Implement base images + per-client differencing VHDX, locking, recovery, cleanup; enforce write isolation; protect base images. Verify UEFI/GPT boot requirements and required boot drivers. Never format a physical/unidentified disk; use disposable virtual disks for destructive tests.
4. **Phase 5 — Windows client agent.** Go Windows service: register + heartbeat, authenticated signed commands, enforce server-authoritative expiry/lock/unlock, safe reconnect with bounded retries, reject forged/expired/unauthorized commands, ignore client clock changes for session length, admin recovery + emergency unlock. Do not disable Task Manager or block system recovery.
5. **Phase 6 — Acceptance.** Run only tests the Windows environment supports; label each PASS/FAIL/NOT RUN/BLOCKED. Actual UEFI PXE → diskless OS boot stays BLOCKED until demonstrated end to end.

## 5. Windows dependency list and the iSCSI-target decision

### 5.1 Go module dependencies (server + agent; standard-library-first)

| Need | Package | Notes |
|---|---|---|
| Windows service lifecycle | `golang.org/x/sys/windows/svc`, `.../svc/mgr` | Install/start/stop/restart/status; recovery actions; event log |
| HTTP/JSON API | stdlib `net/http`, `encoding/json` | Reuse launcher `server.go` client patterns for consistency |
| Embedded admin UI + iPXE templates | stdlib `embed` | Port existing HTML/JS from `src/cfg.lua` / `mkyctl.lua` |
| Config + atomic writes | stdlib `os`, `path/filepath`, `encoding/json` | `%ProgramData%\MKYBOOT`; atomic temp+rename |
| Crypto (sessions, salts, command signing) | stdlib `crypto/rand`, `crypto/hmac`, `crypto/sha256` (evaluate `argon2`/`bcrypt` for passwords) | Replace `/dev/urandom` and iter-SHA1 KDF; review KDF strength |
| VHDX / VHD operations | `qemu-img` (Windows build) **or** Windows VHD/virtual-disk COM APIs | Documented, tested tool required; prefer first-party VHD APIs where feasible |
| DHCP config generation | stdlib templates | Same iPXE options/`next-server`; validator before apply |
| WoL | stdlib `net` (UDP magic packet) | Replaces `etherwake` |

No dependency forces Linux/WSL/Docker. The only unavoidable runtime choice is the **iSCSI target** (§5.2), which has no first-party Windows equivalent.

### 5.2 iSCSI target on Windows — decision required (do not assume availability)

**Fact: Windows ships an iSCSI *initiator* (client), not a target.** The Linux design's `tgt` must be replaced. Options and trade-offs (choose in Phase 4 before building storage):

| Option | Pros | Cons / risk |
|---|---|---|
| **Third-party Windows iSCSI target** (e.g. StarWind SAN & Free, or similar) | Native Windows iSCSI target; closest to current architecture; real PXE→iSCSI→boot path | External dependency; must validate license, support, architecture, and security posture before adopting |
| **Windows Deployment Services (WDS)** + multicast/unicast image streaming | First-party Microsoft; no third-party license | Different model (image deploy, not per-session iSCSI LUN); may not match the diskless "boot from server disk" UX; heavier |
| **Non-iSCSI boot: HTTP/iSCSI-free network boot (e.g. iPXE HTTP + VHDX via a Go-served protocol)** | No iSCSI target needed; fully in-repo control | Windows does not natively network-boot a raw VHD over HTTP without a SAN protocol; significant risk, needs proof of concept |
| **Keep iSCSI but host the target on a small dedicated appliance/VM** | Preserves architecture | Introduces a Linux/hypervisor dependency, which the goal explicitly forbids for the final edition |

**Default recommendation pending validation:** evaluate a third-party Windows iSCSI target first; if none is acceptable, fall back to WDS-based deployment. **No option is selected automatically, and none is assumed to exist.** This is the single largest unresolved dependency in the migration.

## 6. Security risks and mitigations

| Risk | Mitigation |
|---|---|
| Password storage: current iter-SHA1 (250k) is weak vs modern KDFs | Use a memory-hard KDF (`argon2id` or `bcrypt`) in the Go server; migrate on next password set |
| Sessions stored as plaintext JSON on disk | Store under `%ProgramData%\MKYBOOT` with restrictive ACLs; never store tokens in the launcher config (already enforced) |
| Forged/expired client commands | Sign server→client commands with HMAC (per-client key) + timestamp + nonce; reject stale/replayed/invalid |
| Client-side clock change extending a session | Server is authoritative for session start/expiry; client reports uptime/health but cannot lengthen a session |
| One client reading/writing another's data | Per-client differencing VHDX under a server-held lock; base VHDX opened read-only; deny cross-client paths |
| Accidental write to base image | Open base read-only at the storage layer; separate writable overlay; explicit commit flow guarded by super-mode semantics |
| DHCP on the wrong NIC / disrupting production | Detect existing DHCP + port conflicts; bind only to the isolated NIC; dry-run validator; refuse to start on production ranges |
| Privilege / service abuse | Run services under least-privilege accounts; validate all inputs (reuse the strict validators already in `mkyctl.lua`); no shell-out to `os.execute` from HTTP handlers |
| Admin lockout / bricked clients | Authenticated admin recovery + emergency unlock path; do not disable Task Manager or block system recovery |
| Command injection in legacy shell-outs | Eliminate `os.execute` shell-outs; use typed Go APIs; keep the existing metacharacter/path-traversal rejection as defense in depth |

## 7. Test plan (Windows)

Each test maps to a phase and a status label. Tests that need hardware/network/lab are marked **BLOCKED/NOT RUN** until the environment allows them; a build or simulated response is never counted as boot acceptance.

| Area | Test | Phase | Expected evidence |
|---|---|---|---|
| Build & vet | `go vet`, `gofmt`, `go build` (windows/amd64) for server + agent | 2 | Clean; mirrors existing CI (`windows-2022`) |
| Unit | Service lifecycle, config persistence, auth/KDF, session TTL, timer math, lock/unlock state machine, command sign/verify, reconnect/backoff | 2, 5 | Go `go test ./...` PASS |
| Service lifecycle | Install/start/stop/restart; crash recovery; bounded retries; health status | 2 | SCM state transitions; event log |
| API auth | Unauthenticated → 401/"Not authenticated"; bad origin rejected; brute-force lockout | 2 | HTTP traces |
| Client registry/heartbeat | Register, heartbeat, timeout→offline, duplicate MAC handling | 2 | API + logs |
| Timer/lock | Expiry triggers lock at server time; client clock change ignored; admin unlock; emergency unlock | 2, 5 | Timed logs; agent behavior |
| Restart recovery | Config + session/timer state survive service restart | 2 | Post-restart state dump |
| PXE file delivery | `ipxe.efi`/`ipxe.kpxe` present and served; correct `next-server` in iPXE script | 3 | TFTP/HTTP capture |
| DHCP/TFTP integration | DHCP offer with PXE options on isolated NIC only; production DHCP untouched; dry-run validator | 3 | Packet/lease capture on isolated NIC |
| iSCSI/image | Target connects; per-client LUN visible; base read-only; write isolation between two clients | 4 | `iscsiadm`/initiator + hash checks |
| Boot | **Actual UEFI PXE → diskless Windows OS boot** | 6 | **BLOCKED until demonstrated** — never claimed from a build or simulated response |
| Resilience | Reboot, network interruption, reconnect, recovery | 5, 6 | Agent + server logs |

## 8. Environment notes and blockers (this host, read-only)

- **Go toolchain is not installed** on this host (`go version` → not recognized). Server/agent implementation and `go test`/`go build` require the Go toolchain (the repo targets Go 1.21 per `launcher/go.mod`; CI uses `actions/setup-go`). This is a **BLOCKED** prerequisite for Phases 2–5 code work and is not assumed away.
- **No iSCSI target exists on Windows** and none is assumed; §5.2 must be decided before Phase 4 storage work.
- No network service (DHCP/TFTP/iSCSI) has been started; no Windows adapter, route, firewall rule, or IP setting was changed. The existing `30.0.0.55/24` production NIC is untouched.
- The existing Lua/OpenResty backend is **not modified** by this audit; it remains the reference implementation until Go parity is reached.

## Scope

Phase 1 documents the audit and plan only. It performs **no** rewrite, starts **no** service, changes **no** production networking, and does **not** claim any PXE or diskless-boot capability. Subsequent phases (2–6) will be implemented and tested one milestone at a time against this plan.



