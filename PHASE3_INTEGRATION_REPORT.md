# MKYBOOT-SERVER — Phase 3 Integration Report

**Date:** 2026-10-07
**Project:** MKYBOOT-SERVER v4.0.0 (`D:\project\diskless+timer\diskless`)
**Upstream:** `https://github.com/mikkkoyy/MKYBOOT-SERVER`
**Branch:** `main`
**Phase 3 baseline HEAD:** `816f3c0d7ad69f59c1d7d3ae8c0423e8aab4b4c6`
**Phase 3 final HEAD:** `79b47002d7721e2970ffe61f99e0d3a318576316`
**Go toolchain:** `go1.27.1 windows/amd64`
**Lua interpreter:** Lua 5.4.5 (local dev tool, installed via winget)

---

## 1. Baseline (recorded before any Phase 3 change)

| Item | Value |
|---|---|
| Branch | `main` |
| HEAD | `816f3c0d7ad69f59c1d7d3ae8c0423e8aab4b4c6` |
| Remote | `https://github.com/mikkkoyy/MKYBOOT-SERVER.git` (no credentials embedded) |
| Authentication | `gh auth status` → logged in as `mikkkoyy`, scopes `gist, read:org, repo` (write access confirmed) |

Initial `git status --short`:

```
 M .gitignore
 M bin/mkyctl.lua
 M launcher/app.go
 M launcher/main.go
 M launcher/server.go
 M launcher/taxonomy_test.go
 M launcher/tray.go
 M launcher/ui.go
 M launcher/win32.go
 M srv/mkyboot/mkyctl.lua
 M test/srv/mkyboot/mkyctl.lua
?? LAUNCHER_OFFLINE_FIX_REPORT.md
?? RELIABILITY_FIX_PHASE2_REPORT.md
?? REPO_COMPARISON_REPORT.md
?? launcher/health_test.go
?? launcher/instance.go
?? launcher/instance_test.go
?? launcher/uitest/instancetest.ps1
?? src/image.lua
?? test/runtime/test_server_control.lua
?? test/runtime/test_status_cache.lua
```

Baseline test totals: Go **61 PASS / 0 FAIL / 1 SKIP**; Lua **381 assertions / 0 failures** (127 × 3 copies).

---

## 2. Existing user changes preserved

| File | Status |
|---|---|
| `.gitignore` | Preserved; committed in `79b4700` (adds `/launcher/dist/`) |
| `bin/mkyctl.lua` | Preserved; image-lifecycle refactor intact, plus Phase 2/3 work |
| `srv/mkyboot/mkyctl.lua` | Preserved; image-lifecycle refactor intact, plus Phase 2/3 work |
| `src/image.lua` | Preserved; committed in `45a40ea` (required by the shipped `require("image")`) |
| `test/srv/mkyboot/mkyctl.lua` | Preserved; **intentional inline `qemu-img` lifecycle kept** (no `require("image")`) |

No pre-existing change was discarded, reverted or overwritten. No `git reset --hard`, `git clean`, stash, pull, branch switch or force-push was performed.

---

## 3. Files changed and reasons

### Committed in Phase 3

| Commit | Files | Purpose |
|---|---|---|
| `d7b5947` | `launcher/{server,app,ui,tray}.go`, `launcher/taxonomy_test.go`, `launcher/health_test.go`, `LAUNCHER_OFFLINE_FIX_REPORT.md` | Phase 1 launcher offline fix |
| `61264a4` | `launcher/{instance,instance_test,main,win32,ui}.go`, `launcher/uitest/instancetest.ps1` | Single-instance mutex |
| `70e8e58` | `bin/mkyctl.lua`, `srv/mkyboot/mkyctl.lua`, `test/srv/mkyboot/mkyctl.lua`, `test/runtime/{test_status_cache,test_server_control}.lua` | Non-blocking status + three-copy sync |
| `45a40ea` | `install.sh`, `bin/mkyctl.lua`, `srv/mkyboot/mkyctl.lua`, `src/image.lua`, `test/runtime/{test_status_cache,test_deploy_wiring}.lua` | Deployment wiring fix |
| `b898753` | `bin/mkyctl.lua`, `srv/mkyboot/mkyctl.lua`, `test/srv/mkyboot/mkyctl.lua`, `test/runtime/{test_auth,measure_status_perf}.lua` | Malformed username pattern + auth/perf tests |
| `79b4700` | `.gitignore`, `RELIABILITY_FIX_PHASE2_REPORT.md`, `REPO_COMPARISON_REPORT.md` | Docs and build ignore |

### New files created in Phase 3

| File | Purpose |
|---|---|
| `launcher/instance.go` | Named-mutex single-instance guard |
| `launcher/instance_test.go` | 10 single-instance tests |
| `launcher/uitest/instancetest.ps1` | 13 cross-process single-instance checks |
| `test/runtime/test_auth.lua` | 63 authentication/session/rate-limit assertions |
| `test/runtime/test_deploy_wiring.lua` | 29 deployment-wiring assertions |
| `test/runtime/measure_status_perf.lua` | Latency and command-count measurement |
| `PHASE3_INTEGRATION_REPORT.md` | This report |

---

## 4. Defects found and fixed in Phase 3

### 4.1 A clean `install.sh` deployment could not serve anything

**Two independent defects, both confirmed by reading the shipped files.**

**(a) `src/cfg.lua` was never installed — pre-existing, present at the previous HEAD.**
`bin/mkyctl.lua` line 59 runs `mkyboot.cfg = dofile("/srv/mkyboot/cfg/cfg.lua").cfg`
at module load, but `install.sh` only created `/srv/mkyboot/cfg` and copied
`srv/cfg/mkyboot.json` into it. The module therefore fails to load, nginx
returns 500 on every request, and the admin UI, DHCP export and the PXE/iPXE
endpoints are all dead on a clean install.

**(b) `src/image.lua` was never installed and `package.path` was CWD-relative —
introduced by the uncommitted image refactor.**
`bin/mkyctl.lua` and `srv/mkyboot/mkyctl.lua` call `require("image")`, but
nothing installed `src/image.lua`, and `package.path = package.path .. ";src/?.lua"`
resolves against the process working directory. nginx runs with its own prefix as
CWD, so the relative entry pointed at e.g. `/src/image.lua` and never resolved:
`img.new/child/delete/commit/used` raised "module not found".

This also meant commit `70e8e58` initially left the repository referencing a
module that was not tracked — a clone would have been broken. Detected and
fixed in `45a40ea`.

**Fix:** `install.sh` now copies `src/cfg.lua` → `/srv/mkyboot/cfg/cfg.lua` and
`src/image.lua` → `/srv/mkyboot/modules/image.lua`; `package.path` is derived
from the script's own location via `debug.getinfo(1,"S").source` with the
documented install path as fallback.

### 4.2 Malformed Lua pattern in `setup_admin()`

`bin/mkyctl.lua` validated the username with:

```lua
if user:match("[%;%|%&%`%$%!%%%]") then
```

The character class ends with `%]` — an escaped `]` — followed by the closing
`]`. Lua 5.4 rejects that class (`malformed pattern (missing ']')`), so
`setup_admin()` raised a runtime error instead of returning
`"invalid username"` for any username that passed the length check.

Replaced with an explicit per-character loop over the same set
(`; | & \` $ ! % ]`). No pattern escaping is involved, so the check behaves
identically on LuaJIT (what OpenResty ships) and on plain Lua, and it is **not**
a relaxation — every character the original class listed is still rejected.

**Caveat:** LuaJIT behaviour could not be verified from this host (no LuaJIT
available). The replacement is unambiguous on both implementations by
construction.

---

## 5. Ubuntu / OpenResty integration — BLOCKED

**No Linux environment is available on this host.** Verified, not assumed:

| Capability | State |
|---|---|
| WSL feature (`Microsoft-Windows-Subsystem-Linux`) | **Disabled** |
| VirtualMachinePlatform | **Disabled** |
| Hyper-V PowerShell module | **Not available** |
| Docker | **Not installed** |
| `bash` / `sh` | **Not found** |
| nginx / openresty / tftpd / tgt / dhcp / zfs processes | **0 running** |
| WSL distros | **None installed** (`wsl -l -q` returns only the usage banner) |

Enabling WSL requires a reboot and administrator privileges, and even then WSL
does not provide the ZFS, `tgt` iSCSI target, `tftpd-hpa` or `isc-dhcp-server`
stack, nor PXE/DHCP/iPXE client behaviour, that live validation needs.

**Therefore every live Linux test in this phase is `NOT RUN`.** No claim is made
that the Linux server was launched or validated.

### Exact commands and prerequisites for an Ubuntu host

```bash
# 1. Install (requires root)
sudo bash install.sh

# 2. Syntax-check the installed Lua
luac -p /srv/mkyboot/modules/mkyctl.lua
luac -p /srv/mkyboot/cfg/cfg.lua
luac -p /srv/mkyboot/modules/image.lua

# 3. Run the Lua test suites against the installed module
lua test/runtime/test_status_cache.lua /srv/mkyboot/modules/mkyctl.lua
lua test/runtime/test_server_control.lua /srv/mkyboot/modules/mkyctl.lua
lua test/runtime/test_auth.lua /srv/mkyboot/modules/mkyctl.lua
lua test/runtime/test_deploy_wiring.lua .

# 4. Start services and check health
sudo systemctl restart nginx tftpd-hpa isc-dhcp-server tgt
curl -s "http://127.0.0.1:8888/?api=status" | jq .
#   first call after boot performs one bounded cold-start refresh

# 5. Existing runtime suite (mutates auth config - disposable env only)
export MKYBOOT_RUNTIME_TEST=1
sudo bash test/runtime/test_auth.sh
```

---

## 6. Status snapshot: latency and concurrency measurements

Produced by `test/runtime/measure_status_perf.lua` against `bin/mkyctl.lua` with
stubbed command execution (no tgtadm/lsof/nginx/Ubuntu required). It measures
the module's own scheduling behaviour, which is what changed in Phase 2.

### Command count per refresh — constant, not per-workstation

| Workstations | tgtadm | lsof | **total** |
|---|---|---|---|
| 1 | 1 | 3 | **4** |
| 3 | 1 | 3 | **4** |
| 10 | 1 | 3 | **4** |
| 50 | 1 | 3 | **4** |
| 200 | 1 | 3 | **4** |
| 500 | 1 | 3 | **4** |

Previously the cost was **N + 3** (one `tgtadm`+`grep` pipeline per workstation
plus three `lsof` probes). At 500 workstations that is 503 process spawns per
status request; now it is 4.

### Request-path latency (cache-hit path)

| Workstations | Scan time | Commands executed |
|---|---|---|
| 1 | 0.000 ms | 0 |
| 10 | 0.000 ms | 0 |
| 50 | 0.000 ms | 0 |
| 200 | 1.000 ms | 0 |
| 500 | 0.000 ms | 0 |
| 1000 | 1.000 ms | 0 |

1000 cached requests × 50 workstations = **50,000 lookups in 83 ms**, with
**zero** command executions.

### Refresh coordination

| Scenario | Result |
|---|---|
| 25 concurrent lock attempts | **exactly 1 winner**, 4 commands total |
| Sequential `lock()` while held | `true, false, false` |
| Cold cache | one bounded refresh, 4 commands, `targets=ok` |
| Fresh cache | 0 commands |
| Expired cache | served immediately, 0 commands, reported `unknown` |
| Refresh already holding the lock | request returns in **0.000 ms**, 0 commands |

### Timeout and failure handling

| Scenario | Result |
|---|---|
| `timeout` wrapper | `/usr/bin/timeout -s KILL 1.20 /usr/sbin/tgtadm --op show` |
| Budget clamp (999 s requested) | clamped to `3.00` |
| `tgtadm` missing | `targets=missing`, workstation `unknown` |
| `lsof` missing | snapshot still written, service flags default `false` |
| Both missing | refresh succeeds, `targets=missing`, workstation `unknown` |
| Empty / garbage / no-`ts` / bad-`ts` / oversized / absent snapshot | all decode to `nil`, workstation `unknown` — never a false offline |

---

## 7. Authentication and authorization results

`test/runtime/test_auth.lua` — **63 assertions, 0 failures**, run against all
three `mkyctl.lua` copies. Extracts the shipped auth code verbatim and drives it
with stubbed storage, so no `/srv`, no server and no real credentials are
involved. An isolated throwaway password is defined in the test only.

| Area | Assertions | Result |
|---|---|---|
| Password policy and hashing | 10 | PASS — min length 8, short rejected, KDF algorithm + salt recorded, **plaintext never written to storage** |
| Credential verification | 6 | PASS — correct accepted; wrong password, wrong user, empty and nil rejected |
| Setup lockout | 3 | PASS — second `setup_admin` refused, original password still valid |
| Password change | 7 | PASS — wrong current refused, too-short new refused, same-as-current accepted (rehash), old password stops working |
| Session lifecycle | 10 | PASS — create/read/destroy, unknown/empty/nil ids rejected, `destroy_all`, 50 unique ids, rotate invalidates the old id |
| Cookie handling | 5 | PASS — `HttpOnly`, `SameSite=Strict`, `Path=/`, `Max-Age`, immediate expiry on clear |
| Rate limiting | 12 | PASS — threshold 5, lockout 300 s, per-IP, persisted count and deadline, `clear` releases |

**Authorization on the server-control API** (from `test_server_control.lua`,
42 assertions): session required, origin check enforced, exactly two operations
(`restart`, `stop`), `nginx` the only reachable service, nine unsafe operation
strings rejected with zero privileged calls.

**Observed behaviour recorded, not asserted as a security property:**
`change_password()` rewrites the credential record but does **not** invalidate
existing sessions — a session survives until it expires or a new login destroys
it. This matches the README ("Old sessions destroyed on login").

---

## 8. PXE and diskless boot infrastructure — inspection results

No hardware or VM was available, so this is **source and configuration
inspection only**. Every item below is `NOT RUN` at runtime.

### 8.1 DHCP discovery and PXE options

**Files:** `examples/etc/dhcp/dhcpd.conf` (97 lines), generated by
`mkyboot:ExportDHCP()` (`bin/mkyctl.lua:768`).

- `authoritative`, `next-server 192.168.0.2`, two address ranges.
- Full iPXE option space: `option ipxe-encap-opts code 175`, feature indicators
  (`ipxe.pxeext`, `ipxe.iscsi`, `ipxe.http`, `ipxe.tftp`, `ipxe.efi`, …),
  `option iscsi-initiator-iqn code 203`.
- Per-host entries keyed on `hardware ethernet` with `fixed-address` and
  `option host-name`.
- Boot filename selected from `option vendor-class-identifier`:
  `00000` → `ipxe.kpxe` (BIOS), `00006` → `ipxe32.efi`, else `ipxe.efi`.
- `option ipxe.no-pxedhcp 1` and `option ipxe.keep-san 1` per host.

**Gap found:** the config references `ipxe32.efi`, but the repository ships only
`ipxe.efi` and `ipxe.kpxe` (`srv/tftp/`). A 32-bit UEFI client would be offered
a file that does not exist. Not fixed — needs a decision on whether 32-bit UEFI
is supported.

### 8.2 TFTP file delivery

**Files:** `srv/tftp/{ipxe.efi, ipxe.kpxe, bg.png}`, installed by
`install.sh:48-50`. `tftpd-hpa` is a declared dependency. No TFTP server is
implemented in-project; delivery is delegated to the external daemon.

### 8.3 iPXE chainloading

**File:** `bin/mkyctl.lua:1772-1806`.

Flow: client PXE boots → DHCP assigns IP and filename → iPXE loads via TFTP →
iPXE calls `?getmebootargs=<client-ip>` → server resolves the client by IP via
`GetIDFromIPv4` and emits an iPXE script containing:

```
set initiator-iqn <iqn>:<mac>
set root-path iscsi:${next-server}:<proto>:<port>:<lun>:<iqn>:<mac>
```

for each boot image, followed by the configured iPXE body and footer pages.
Unknown client IPs get a `#!ipxe` failure script and are logged.

**Code smell found:** the `supper == "1"` branch (L1790-1797) and the `else`
branch (L1798-1806) are **byte-for-byte identical** — both run
`monit → tgtstop → nbdFree → zfsmount → mkChild → nbdConnect → LunAdd`.
Supper mode therefore does nothing different at boot time. Not fixed: changing
boot behaviour without a test environment would be unsafe, and the duplication
may be intentional placeholder logic.

### 8.4 Boot menu and image selection

Image selection is driven by the per-client `img[]` array in
`srv/cfg/mkyboot.json` (`boot`, `enable`, `type`, `cache`, `commit`) and
surfaced through the web UI. The iPXE script sets `root-path` per LUN based on
`boot == 1/2/3`. No interactive boot menu is generated server-side.

### 8.5 iSCSI target configuration and discovery

**Files:** `bin/mkyctl.lua` — `mkyboot.cmd.tgt` (L362-411), `LunAdd` (L1308),
`tgtstop` (L844); `mkyboot.inc.systemctl` (L617) with `valid.service_name` and
`valid.systemctl_cmd` whitelists.

- Targets are created/deleted/bound via `tgtadm` with validated TIDs and IPs.
- `LunAdd` waits for the target to exist, then adds each enabled `dyndisk` image
  as a LUN.
- `tgtadm --op show --mode target` is the source of truth for client presence
  (now read once per refresh, not per workstation).

### 8.6 Image assignment and access permissions

Per-client images are resolved in `nbdConnect` (L1275) as
`<imgbackdir>/<path><image_prefix><MAC>` with the configured cache mode
(`none` / `unsafe` / `writeback` / `directsync` / `writethrough`, validated by
`valid.cache`). `search_nbd` allocates a free `/dev/nbdN` device.

### 8.7 ZFS snapshots and overlays

**File:** `mkyboot:mkChild` (L856).

- `dyndisk` (QCOW2): parent is the master image, or a per-client ZFS snapshot
  path under `<imgdir>/<zfs.tmpname>/<vid>/<path>`; child is the per-client
  overlay in `imgbackdir`.
- `dynblock` (ZFS zvol): parent is the zvol or a `@<zfs.tmpname>_<vid>` snapshot.
- Per-client lock files (`<lockfile><n><vid>`) guard concurrent creation.
- `mkyboot.cmd.img.child` → `require("image").child` → `qemu-img create -f qcow2 -b`.

### 8.8 Write-back cache behaviour

Cache mode is per-image (`cache` field) and passed to `qemu-img` / NBD. Modes
are validated by `valid.cache`. `install.sh` creates `/srv/mkyboot/writeback`.

### 8.9 Per-client writable storage isolation

Isolation is by filename: every client's overlay embeds its MAC address
(`mac:gsub('%W','')`), so two clients never share an overlay file. ZFS snapshots
provide an additional copy-on-write layer for `dynblock` images.

### 8.10 Interrupted-session recovery and cleanup

`nbdFree` (L1252) releases NBD devices, `tgtstop` (L844) removes iSCSI targets,
`zfsmount` (L1374) handles ZFS unmount/remount. `mkyboot.inc.monit()` runs
before each boot to reconcile state. The status snapshot's tri-state `unknown`
ensures an interrupted refresh is never reported as a confirmed offline client.

### 8.11 Full PXE acceptance — NOT RUN

No PXE-capable client, VM or diskless lab hardware is available. The following
were **not** executed: DHCP lease acquisition, PXE option negotiation, TFTP
transfer, iPXE startup, image selection, iSCSI session establishment, OS boot,
writable-overlay isolation, write-back persistence, and interrupted-session
recovery.

A successful HTTP health check or a visible PXE menu is **not** proof of a
successful diskless OS boot, and none is claimed.

**Exact next steps:** run the commands in §5 on an Ubuntu 20.04/22.04 host with
ZFS, then attach a PXE-capable client (physical or VM) and record DHCP lease,
TFTP transfer, iPXE script, iSCSI session and boot completion.

---

## 9. Regression and security checks

### Go launcher

| Check | Command | Result |
|---|---|---|
| Formatting | `gofmt -l .` | **PASS** — no files listed |
| Static analysis | `go vet -unsafeptr=false ./...` | **PASS** — exit 0 |
| Compilation | `go build ./...` | **PASS** — exit 0 |
| Windows release build | `go build -trimpath -ldflags "-s -w -H windowsgui" -o "dist/MKYBOOT Launcher.exe" .` | **PASS** |
| Unit + security tests | `go test -timeout 300s ./...` | **PASS** — **61 PASS / 0 FAIL / 1 SKIP** |

All five security tests pass unchanged:

```
--- PASS: TestNoShellSSHOrProcessExecution
--- PASS: TestNoInsecureTLSCertBypass
--- PASS: TestNoHardcodedCredentials
--- PASS: TestNoSecretsEverLogged
--- PASS: TestNoArbitraryRemoteExecutionPrimitives
```

The 1 SKIP is `TestInstanceFailedAcquireYieldsNilLock`, which skips by design
when the calling thread already owns the mutex.

### Lua server

| Check | Command | Result |
|---|---|---|
| Syntax, all 3 copies | `luac -p <file>` | **PASS** — exit 0 each |
| Status snapshot | `lua test/runtime/test_status_cache.lua <file>` | **PASS** — 85/85 × 3 |
| Server control | `lua test/runtime/test_server_control.lua <file>` | **PASS** — 42/42 × 3 |
| Deployment wiring | `lua test/runtime/test_deploy_wiring.lua .` | **PASS** — 29/29 |
| Auth / sessions / rate limit | `lua test/runtime/test_auth.lua <file>` | **PASS** — 63/63 × 3 |

**Total Lua: 599 assertions, 0 failures.**

### Other

| Check | Result |
|---|---|
| `git diff --check` | **PASS** (only a pre-existing trailing-blank-line note in `.gitignore`) |
| `LICENSE` unmodified | **YES** |
| No server-process execution added to the launcher | **YES** |
| Timer / PC-lock features introduced | **NO** — none added in this phase |
| Pre-existing changes preserved | **YES** |

---

## 10. Test classification totals

| Classification | Count | Detail |
|---|---|---|
| **PASS** | Go 61 + Lua 599 = **660 assertions** | All executed and passed |
| **FAIL** | **0** | |
| **NOT RUN** | Live Ubuntu/OpenResty integration; `?api=status` under real nginx; real `tgtadm` timing; `ngx.timer.at` path; `test/runtime/test_auth.sh`; `launcher/uitest/uitest.ps1` pixel harness; full PXE boot acceptance (DHCP, TFTP, iPXE, iSCSI, OS boot, overlay isolation, write-back, interrupted-session recovery) | No Linux environment, no PXE hardware |
| **BLOCKED** | `go test -race` | `-race` requires cgo; `CGO_ENABLED=1` fails with `C compiler "gcc" not found` |
| **NOT VERIFIED** | `install.sh` shell syntax | No `bash` on this host |
| **NOT VERIFIED** | LuaJIT-specific behaviour of the username check | No LuaJIT available; replacement is unambiguous by construction |

---

## 11. Remaining risks and exact next actions

1. **Ubuntu validation is the dominant gap.** Run the §5 command sequence on an
   Ubuntu 20.04/22.04 host with ZFS before any production deployment. Until
   then the server is validated by source extraction and stubs only.
2. **`install.sh` was never syntax-checked** (no bash here). Run `bash -n
   install.sh` on a Linux host.
3. **`ipxe32.efi` is referenced by the DHCP config but not shipped.** Decide
   whether 32-bit UEFI is supported; if so, build and install the binary, if
   not, remove the `elsif` branch.
4. **The `supper` boot branch is identical to the non-supper branch**
   (`bin/mkyctl.lua:1790-1806`). Supper mode currently has no boot-time effect.
   Needs a design decision and a test environment before changing.
5. **`status.cache` permissions.** Created by the nginx worker with default
   umask. Contains only client IPs and service flags (no secrets), but consider
   an explicit `chmod` in `install.sh` for consistency with the rest of `cfg/`.
6. **Three-copy drift will recur.** Nothing enforces that `srv/` and `test/` stay
   in sync with `bin/`. A CI check comparing the status and server-control
   regions would prevent it. Not added — no CI infrastructure exists in this
   repo.
7. **`tgtadm` worst-case duration is still unknown.** The launcher budget is
   20 s; if a real `tgtadm` under heavy load exceeds that, the status call will
   still be slow — though it can no longer block the worker.
8. **Manual UI checks still required:** launch two copies (second must exit and
   the first must come forward); RDP a second session and confirm a second
   launcher is allowed; verify the `wmAppForeground` restore path visually.

---

## 12. Phase 3 commits and push status

All commits were pushed to `origin main` and verified by re-fetching and
comparing hashes. No force-push, no divergence, no empty commits.

| # | Commit | Message | Push |
|---|---|---|---|
| 1 | `d7b59470514bacc58883824f735a3ec79d70953d` | `fix: correct launcher OFFLINE misdiagnosis and unblock recovery` | **VERIFIED** |
| 2 | `61264a4f3a45eb060287a438d40303a7c589ee10` | `fix: enforce a single interactive launcher instance per Windows session` | **VERIFIED** |
| 3 | `70e8e584afbcdf86ac1d4c464b8f37a08c6e1a1e` | `fix: make workstation status non-blocking and sync the three Lua copies` | **VERIFIED** |
| 4 | `45a40eadb49536d9788c72d2ebfb60d98aa8f903` | `fix: ship src/cfg.lua and src/image.lua so a clean install can actually serve` | **VERIFIED** |
| 5 | `b89875321b2e98faed23c79ef62117588e539681` | `fix: replace malformed username pattern and add auth/perf test coverage` | **VERIFIED** |
| 6 | `79b47002d7721e2970ffe61f99e0d3a318576316` | `docs: commit phase 2 report, repo audit report and launcher build ignore` | **VERIFIED** |

**No local commits remain unpushed.** Working tree is clean.

---

## 13. Confirmation

- **Timer and PC-lock features were NOT introduced in this phase.** No timer,
  session-countdown, lock/unlock or client-agent code was added. The launcher
  gained only a single-instance mutex; the Lua server gained a status cache, a
  deployment fix and a pattern fix.
- **No secrets appear in this report.** Credential scans over the committed
  files and reports returned nothing.
- **No test is reported as passing unless it actually ran and passed.**