# MKYBOOT-SERVER — Reliability Fix Phase 2 Report

**Date:** 2026-10-07
**Project:** MKYBOOT-SERVER v4.0.0 (`D:\project\diskless+timer\diskless`)
**Upstream HEAD:** `816f3c0d7ad69f59c1d7d3ae8c0423e8aab4b4c6`, branch `main`
**Go toolchain:** `go1.27.1 windows/amd64` (`C:\Program Files\Go\bin\go.exe`)
**Lua interpreter:** Lua 5.4.5 (installed via winget for syntax/unit checking; see §8)

---

## 1. Root Causes

### Task 1 — Workstation status blocked the nginx worker

**Root cause: one blocking `tgtadm`+`grep` pipeline was executed per workstation, inside the HTTP request.**

`bin/mkyctl.lua:918` (original) ran, for every single workstation:

```lua
function mkyboot:checkstatpc(p_ip)
    if not mkyboot.inc.valid.ipv4(p_ip) then return false end
    local fd = io.popen("/usr/sbin/tgtadm --lld iscsi --op show --mode target 2>/dev/null | /usr/bin/grep 'IP Address: "..p_ip.."' 2>/dev/null")
    ...
```

`get_server_status()` (`bin/mkyctl.lua:1375`) called it once per workstation in
its loop (`:1394`) and additionally ran three `lsof` probes. With the 3
workstations shipped in `srv/cfg/mkyboot.json` that is **6 process spawns per
`?api=status` request**, and a blocking `io.popen` stalls the entire nginx
worker. This was the direct cause of the Phase 1 launcher's false `Timeout`
verdicts: the endpoint could not answer inside the budget when `tgt` was slow.

Aggravating factor: `lsof` was invoked per service *inside the request* as well,
so even a healthy `tgt` still cost three more blocking spawns.

### Task 2 — No single-instance protection

**Root cause: nothing prevented a second launcher process.** There was no
mutex, no PID file, no lock of any kind. `main()` went straight to
`NewApp()` → `Run()`. This was observable: my own screenshot of the running
launcher showed **two live `MKYBOOT Launcher.exe` processes** (PIDs 6224 and
12144), one of them still holding a stale build. Both ran their own poll loop
and tray icon.

### Task 3 — Three divergent Lua copies

**Root cause: the server-control API existed in exactly one of the three copies.**

| File | `?api=server&op=` before this phase | Role |
|---|---|---|
| `bin/mkyctl.lua` | **present** (line 1809) | **Canonical production** — `install.sh:40` installs it to `/srv/mkyboot/modules/mkyctl.lua` |
| `srv/mkyboot/mkyctl.lua` | **missing** | Deployed-tree copy; **not** installed by `install.sh` |
| `test/srv/mkyboot/mkyctl.lua` | **missing** | Test fixture; **not** installed by `install.sh` |

Confirmed by `install.sh:40`: `cp -f bin/mkyctl.lua /srv/mkyboot/modules/mkyctl.lua`,
and nginx serves that path (`examples/etc/nginx/sites-available/default:25`,
`content_by_lua_file /srv/mkyboot/modules/mkyctl.lua`).

Consequence: `launcher/app.go` asserts `controlAPISupported = true`, so a
deployment that installed the `srv/` copy would expose **non-functional**
Restart/Stop buttons — the launcher would prompt for the administrator
password and then fail against a route that does not exist.

---

## 2. Files Changed

### Lua (server)

| File | Change | Reason |
|---|---|---|
| `bin/mkyctl.lua` | +362 lines: non-blocking status snapshot module; `checkstatpc()` now cache-only; `statpc_state()` tri-state helper; `valid.port`; snapshot-backed `get_server_status()` | Canonical production source |
| `srv/mkyboot/mkyctl.lua` | +399 lines | Same status module **and** the `?api=server` handler ported in; now byte-identical to `bin/` |
| `test/srv/mkyboot/mkyctl.lua` | +373 lines | Same two additions; **deliberately keeps its inline `qemu-img` image lifecycle** |

### Go (launcher)

| File | Change | Reason |
|---|---|---|
| `launcher/instance.go` | **new** — named-mutex single-instance guard | Task 2 |
| `launcher/instance_test.go` | **new** — 10 tests | Task 2 |
| `launcher/uitest/instancetest.ps1` | **new** — 13 cross-process checks | Task 2 (see §4 for why this file exists) |
| `launcher/main.go` | Acquire the lock before anything else; second instance notifies and exits 0 | Task 2 |
| `launcher/win32.go` | `CreateMutexW`/`WaitForSingleObject`/`ReleaseMutex`/`CloseHandle`/`ProcessIdToSessionId`/`FindWindowW`/`PostMessageW` bindings; `wmAppForeground` | Task 2 |
| `launcher/ui.go` | Handle `wmAppForeground`; `mainWindowClassName` constant | Task 2 |

### Tests

| File | Change |
|---|---|
| `test/runtime/test_status_cache.lua` | **new** — 85 assertions, Task 1 |
| `test/runtime/test_server_control.lua` | **new** — 42 assertions, Task 3 |

**No other file was touched.** `LICENSE` unmodified. No UI redesign. The Phase 1
offline fix files (`server.go`, `app.go`, `tray.go`, `taxonomy_test.go`,
`health_test.go`) were **not modified in this phase**.

---

## 3. Architecture: Non-Blocking Status Refresh

I investigated existing infrastructure first, as required. Findings:
`grep` for `ngx.shared`, `lua_shared_dict`, `ngx.timer`, `init_worker_by_lua`,
`init_by_lua` in `bin/mkyctl.lua` → **zero hits**. The nginx site config has no
`lua_shared_dict` and no timer directives. So no reusable cache/timer facility
existed and a new one was required.

### Design

```
        HTTP request                          background / bounded refresh
        -------------                          -----------------------------
get_server_status()  ──►  status.ensure()  ──►  get()   read snapshot file
                              │                          (no command)
                              ├─ snapshot fresh (age ≤ ttl=5s) ──► return as-is
                              ├─ snapshot stale  ──► ngx.timer.at(0, refresh)
                              │                     serve previous snapshot
                              │                     return IMMEDIATELY
                              └─ snapshot absent  ──► refresh() once, bounded
                                                        by budget = 3.0s
```

**Refresh cost: constant, not per-workstation.** One `tgtadm --op show --mode
target` invocation, whose entire output is parsed for *all* addresses at once,
plus at most three `lsof` probes. Previously: one `tgtadm` pipeline **per
workstation**.

**Shared state is a file, not Lua memory.** The snapshot lives at
`/srv/mkyboot/cfg/status.cache` as a small `key=value` text file, published
atomically (temp file + `rename`). It is therefore correct across nginx worker
processes — no shared mutable Lua table. The per-worker `status.memo` is a
read-only decode cache validated against the file mtime via LuaFileSystem, so
it can never serve a value that disagrees with the file for more than one
modification.

**Bounded execution.** Every command is wrapped in
`/usr/bin/timeout -s KILL <n>`, with `n` clamped to the shared 3.0s refresh
budget. If `timeout` is absent the bare command is used (documented fallback).

**Overlapping refreshes are prevented** by a timestamp lock file
(`status.lock`, TTL 10s, reclaimed if abandoned). Verified: of two concurrent
candidates exactly one wins and the loser spawns no commands.

**Freshness / validity is explicit, never inferred.**

| Constant | Value | Meaning |
|---|---|---|
| `status.ttl` | 5s | snapshot counts as fresh; served without any command |
| `status.max_age` | 60s | beyond this the snapshot is stale → status is `unknown` |
| `status.budget` | 3.0s | total wall-clock seconds for one whole refresh |
| `status.lock_ttl` | 10s | an older lock is treated as abandoned |

**Tri-state status — `unknown` is never reported as `offline`.**
`status.state_of()` returns `online` / `offline` / `unknown`. `unknown` covers:
cold cache, stale snapshot, missing `tgtadm`, failed command, invalid address,
and clock skew (future timestamp). This directly satisfies "distinguish
unknown/stale from confirmed offline".

**`checkstatpc()` executes no command at all.** It reads the cached snapshot.
`mkyboot:statpc_state()` exposes the tri-state form for callers that care.

**API compatibility preserved.** Existing fields are unchanged in name and
meaning:
`server.{ipv4,version,vendor}`, `clients.{total,online,offline}`, `iscsi`, `images`,
`services.{dhcp,tftp,iscsi}`. Two additive changes:
- `clients.unknown` — new counter (previously these were silently counted as offline)
- `status.{fresh,stale,age,targets,refreshed}` — new object

Note: `online + offline + unknown == total` now holds, whereas before
`online + offline == total` with unknown cases folded into `offline`.

**Security validators kept.** `valid.port` (new, integer 1–65535) gates every
`lsof` probe; `valid.ipv4` gates every parsed address; `valid.service_name`
and `valid.systemctl_cmd` unchanged. No user input reaches any command string —
the `tgtadm`/`lsof` command lines are built entirely from constants.

**Startup / service-unavailable behaviour (documented as required):**
- *Cold start (no snapshot file):* one bounded synchronous refresh (≤3s) so the
  first request reports real status instead of a false "unknown". `tgtadm` runs
  exactly once.
- *Snapshot stale:* served immediately from the previous snapshot; refresh runs
  in the background. **A hung `tgtadm` can no longer delay a response.**
- *`tgt` service down:* `tgtadm` returns empty output → `targets="ok"` with zero
  addresses → workstations correctly reported `offline`.
- *`tgtadm` binary missing:* `targets="missing"` → workstations `unknown`, never
  `offline`. Same for a failed command (`targets="failed"`).
- *`lsof` missing:* snapshot still written; service flags default to false.

**Portability bug found and fixed in my own code:** Windows `os.rename` fails
when the target exists (returns `nil`, no error), so the snapshot would never
update there. I added a remove+rename fallback. On POSIX (the production
platform) the first `rename` succeeds and the fallback never runs.

---

## 4. Single-Instance Mechanism and Its Scope

**Mechanism:** a Win32 named mutex, `Local\MKYBOOT-Launcher-<sessionID>`.

- `CreateMutexW(NULL, FALSE, name)` — create or open, no ownership yet.
- `WaitForSingleObject(handle, 0)` — non-blocking ownership test.
- `WAIT_OBJECT_0` → we own it. `WAIT_ABANDONED` → previous owner died holding
  it; Windows transfers ownership to us. Anything else → `ErrAlreadyRunning`.

**Scope: one interactive launcher per Windows session.** Two RDP sessions of
the same user each get their own launcher. `currentSessionID()` reads the real
session via `ProcessIdToSessionId`; on failure it falls back to `0`, which
merges sessions into one stricter scope (still safe).

Explicitly **not** used, per the requirements:
- **No PID file** — a PID file survives a crash and would be a false "already
  running" signal, the exact failure mode to avoid.
- **No fixed TCP port** — the port is configurable and belongs to the MKYBOOT
  server; binding it would collide with the server and change its meaning.

**Crash safety is structural**, not best-effort: the mutex is a kernel object
destroyed when the owning process terminates. A stale lock is impossible by
construction. Verified by killing the process and relaunching (§6, TEST 4/5).

**Second-instance behaviour:** `FindWindowW` locates the running launcher's
window class and posts `wmAppForeground` (restore + focus + re-check), then the
second process exits with code **0** — no error dialog, because double-clicking
is a normal user action, not a failure.

**Initialization errors are not swallowed.** Any failure other than
"already running" produces a message box and `exit(1)` — a nil lock is never
treated as permission to start.

### A testing trap I hit, and how I handled it

My first single-instance implementation used `bInitialOwner=TRUE` +
`ERROR_ALREADY_EXISTS`. Its tests **failed**: a burst of 24 goroutines all
"acquired" the lock.

I probed the cause with a standalone program rather than guessing:

```
thread 0: first wait  -> 0x0
thread 0: second wait (same thread) -> 0x0
thread 0: wait from another goroutine -> 0x0
```

**Windows mutexes are recursive per OS thread**, and the Go runtime multiplexes
goroutines onto a small reused pool of OS threads — so two goroutines can land on
the same thread and both legitimately re-acquire. Two consequences:

1. The production code was rewritten to the `WaitForSingleObject` design above
   (which is also the semantically correct way to ask about *ownership*).
2. **In-process goroutine tests cannot prove mutex exclusion at all.** They
   produce false passes. This is documented in `instance_test.go`.

Cross-process exclusion is therefore verified in
`launcher/uitest/instancetest.ps1`, following the repo's existing
`uitest/uitest.ps1` convention, by launching the real executable. The Go tests
now cover only what is genuinely valid single-threaded (acquire, release,
idempotent release, nil safety, name shape, stale-handle-is-not-running).

I also had to remove `os/exec` usage from my first test draft: the existing
`TestNoShellSSHOrProcessExecution` correctly flagged it, and I did **not** weaken
that test.

---

## 5. Canonical Lua Source and Deployment Findings

**Canonical production source: `bin/mkyctl.lua`.** Established from
`install.sh:40` plus the nginx `content_by_lua_file` path. `srv/mkyboot/` and
`test/srv/mkyboot/` are **not** referenced by any install step — they are a
deployed-tree mirror and a test fixture respectively.

**What was ported.** The `?api=server&op=` handler was added to both other
copies from the canonical source, and all three now carry an identical status
module. Verified:

```
bin/mkyctl.lua                 serverctl=42 passed, 0 failed  status=85 passed, 0 failed  syntax=0
srv/mkyboot/mkyctl.lua         serverctl=42 passed, 0 failed  status=85 passed, 0 failed  syntax=0
test/srv/mkyboot/mkyctl.lua    serverctl=42 passed, 0 failed  status=85 passed, 0 failed  syntax=0
```

`srv/mkyboot/mkyctl.lua` is now **byte-identical** to `bin/mkyctl.lua`
(SHA-256 comparison).

**What was deliberately NOT unified.** The copies are *not* forced to be
byte-identical, because the test fixture has a meaningful, intentional
difference:

| | `bin/` and `srv/` | `test/` |
|---|---|---|
| Image lifecycle | `require("image")` → `src/image.lua` (uncommitted user refactor) | inline `os.execute("/usr/bin/qemu-img …")` with its own validators |

That difference is the pre-existing `src/image.lua` refactor being present in
two files and absent from the third. **It was preserved exactly.** Verified:
`require("image")` count = 5 in `bin/`, 5 in `srv/`, **0** in `test/`.

### Meaningful differences between the copies (full list)

| Difference | Before | After |
|---|---|---|
| `?api=server&op=` handler | bin only | all three |
| Status snapshot module | none | all three (identical) |
| Image lifecycle → `src/image.lua` | bin, srv | bin, srv (unchanged) |
| Inline `qemu-img` lifecycle | bin (pre-refactor), srv, test | test only |

No test fixtures, mocks or test-only hooks were removed. `test/etc/init.d/mkybootd`
and `test/usr/bin/mkybootd` were not modified.

### Authorization not weakened

The ported handler is byte-identical to the canonical original, and its
behaviour is now regression-tested (42 assertions):

- Requires `mkyboot.inc.session.validate()`; unauthenticated → `{"error":"Not authenticated"}` and **zero** `systemctl` calls.
- Enforces an `Origin`/`Referer` check; foreign origin → `Invalid origin` and zero calls.
- Exactly two operations exist (`restart`, `stop`); everything else → `Invalid server operation`.
- `nginx` is the only service it can ever touch (hard-coded, and `valid.service_name` whitelists the names).
- Nine unsafe operation strings tested (`restart; reboot`, `restart && rm -rf /`, `$(id)`, embedded newline, `RESTART`, trailing space, empty, `restartnginx`) — all rejected, zero actions.
- No new administrative surface: still gated on `api=server` **and** an explicit `op`, and the handler adds no direct `os.execute`.

---

## 6. Tests Executed — Exact Results

### Go launcher

| Check | Command | Result |
|---|---|---|
| Formatting | `gofmt -l .` | **PASS** — no files listed |
| Static analysis | `go vet -unsafeptr=false ./...` | **PASS** — exit 0, no diagnostics |
| Compilation | `go build ./...` | **PASS** — exit 0 |
| Unit + security tests | `go test -timeout 300s ./...` | **PASS** — exit 0 |
| Test tally | `go test -v` | **61 PASS, 0 FAIL, 1 SKIP** |
| Windows release build | `go build -trimpath -ldflags "-s -w -H windowsgui" -o "dist/MKYBOOT Launcher.exe" .` | **PASS** — 7,102,976 bytes |

The 1 SKIP is `TestInstanceFailedAcquireYieldsNilLock`, which skips by design
when the calling thread already owns the mutex.

**All five security tests pass unchanged:**
`TestNoShellSSHOrProcessExecution`, `TestNoInsecureTLSCertBypass`,
`TestNoHardcodedCredentials`, `TestNoSecretsEverLogged`,
`TestNoArbitraryRemoteExecutionPrimitives`.

**Cross-process single-instance — `uitest/instancetest.ps1`: 13/13 PASS**

```
-- preflight --        PASS  no launcher running before the test
-- 1. first instance -- PASS  first launch starts and stays
-- 2. second instance -- PASS  second launch exits by itself
                           PASS  second launch exit code is 0
                           PASS  no duplicate instance created
-- 3. burst of 8 --    PASS  burst of 8 yields exactly one instance
-- 4. crash recovery - PASS  killed instance is gone
                           PASS  relaunch after crash succeeds (no stale lock)
-- 5. clean shutdown - PASS  instance running before shutdown
                           PASS  relaunch after clean shutdown succeeds
                           PASS  no launcher left running after the test
RESULT: PASS (all checks)
```

### Lua server

| Check | Command | Result |
|---|---|---|
| Syntax, all 3 copies | `luac -p <file>` | **PASS** (exit 0 each) |
| Status snapshot, all 3 copies | `lua test/runtime/test_status_cache.lua <file>` | **PASS** — 85/85 each |
| Server control, all 3 copies | `lua test/runtime/test_server_control.lua <file>` | **PASS** — 42/42 each |

**Total Lua: 381 assertions, 0 failures** (127 × 3 copies).

Both test files extract the module under test **verbatim** from the production
`.lua` file and drive it with stubbed command execution, so they exercise the
shipped code rather than a copy.

Status-snapshot coverage mapped to the required cases:

| Requirement | Assertion(s) |
|---|---|
| Slow external command does not block the response | `50 workstations served with ZERO command executions`; `stale path returns immediately`; `refresh completes under the wall-clock budget` |
| Multiple requests do not spawn duplicate refresh jobs | `exactly one of two callers wins the refresh lock`; `the loser spawns no tgtadm`; `a refresh blocked by the lock executes no commands` |
| Timeout does not hang the worker | command is wrapped in `timeout … KILL`; budget clamped |
| Missing `tgtadm` / `lsof` does not crash | 4 assertions across both binaries |
| Cache expiry and refresh correct | 5 freshness assertions + fresh-serves-nothing + cold-start-refreshes-once |
| Partial command failure accurate | 3 assertions (targets ok, ports false) |
| Response schema compatible | schema is additive only; `online+offline+unknown == total` asserted via counts |

### Regression checks — Phase 1 offline fix preserved

| Check | Result |
|---|---|
| `TestSlowButReachableIsNotOffline` | **PASS** |
| `TestConnectionRefusalDetailIsActionable` | **PASS** |
| `TestHealthyLocalServerStaysOnlineWithoutInternet` | **PASS** |
| `TestRetryPromotesRecoveringServerToOnline` | **PASS** |
| `TestStateReachability` / `TestCanControlServer` | **PASS** |
| `TestDegradedWhenServicesDown` | **PASS** |
| `TestVerifyControlAbortsOnShutdown` | **PASS** |
| `LICENSE` unmodified | **YES** |
| No blocking command in `checkstatpc()` (all 3 copies) | **0** occurrences |
| Status handler reads cached snapshot (all 3 copies) | **1** occurrence each |
| No server-process execution added to launcher | **PASS** (only pre-existing comments mention nginx) |
| `require("image")` counts | bin **5**, srv **5**, test **0** — refactor preserved |

---

## 7. Pre-Existing Changes Preserved — Confirmed

Recorded at Phase 0 and re-verified at the end:

```
 M .gitignore                  (present)
 M bin/mkyctl.lua              (present, image refactor intact, + Phase 2 work)
 M srv/mkyboot/mkyctl.lua      (present, image refactor intact, + Phase 2 work)
 ?? src/image.lua              (present, untouched)
```

- `HEAD` is still `816f3c0d7ad69f59c1d7d3ae8c0423e8aab4b4c6` on `main`.
- No reset, clean, stash, pull, branch switch, or checkout was performed.
- No unrelated user file was overwritten.
- `LAUNCHER_OFFLINE_FIX_REPORT.md` and `REPO_COMPARISON_REPORT.md` untouched.
- `launcher/health_test.go` untouched.
- The only file I deleted was `launcher/launcher.exe`, a stray artifact **I**
  created with an earlier `go build ./...` (it is not gitignored, unlike
  `launcher/dist/`).

---

## 8. Environment Limitations

| Item | Status | Detail |
|---|---|---|
| **Ubuntu / OpenResty runtime test** | **NOT RUN** | No Ubuntu available. `wsl` is present but **has no distro installed** (`wsl --list --quiet` returns only the usage banner). |
| **`?api=status` under real nginx** | **NOT RUN** | Needs OpenResty + `tgtadm` + `lsof` on Ubuntu. Verified by source extraction + stubs instead. |
| **Real `tgtadm` timeout under load** | **NOT RUN** | Needs the `tgt` service and iSCSI targets. The timeout *wrapping* is unit-tested; the real duration is not. |
| **`ngx.timer.at` path** | **NOT RUN** | Requires OpenResty. Tested by inspection: guarded by `if ngx and ngx.timer and ngx.timer.at` and wrapped in `pcall`, so the non-OpenResty path is proven. |
| **Existing `test/runtime/test_auth.sh`** | **NOT RUN** | Bash + `curl` against a live server; requires Ubuntu and `MKYBOOT_RUNTIME_TEST=1` (it mutates auth config). |
| **`launcher/uitest/uitest.ps1`** (pixel UI harness) | **NOT RUN** | Requires an interactive desktop session; it inspects real pixels. This session is non-interactive. |
| **`go test -race`** | **NOT RUN** | `-race` needs cgo; `CGO_ENABLED=1` fails with `C compiler "gcc" not found`. No MinGW on this host. Concurrency is argued structurally (one poll goroutine, `sync.Mutex`, cancellable contexts) but **not** race-verified. |
| **Linux cross-compile** | **FAIL (expected)** | `GOOS=linux go build` fails on `syscall.NewCallback` and `syscall.Handle` — the launcher is Windows-only by design, as before. |

### Tooling note

No Lua interpreter existed on this host, so I installed **Lua 5.4.5**
(`winget install DEVCOM.Lua --scope user`) to run `luac -p` and the unit tests.
This is a local developer tool, not a project dependency; nothing in the
repository was modified to accommodate it. `install.sh` still uses the distro
`lua-json`/`lua-filesystem` packages unchanged.

---

## 9. Remaining Risks and Required Manual Tests

1. **Ubuntu verification is the real gap.** The status module was validated by
   extracting the shipped code and stubbing the shell. Before production, run on
   the target box:
   ```bash
   luac -p /srv/mkyboot/modules/mkyctl.lua
   lua /path/to/test/runtime/test_status_cache.lua /srv/mkyboot/modules/mkyctl.lua
   lua /path/to/test/runtime/test_server_control.lua /srv/mkyboot/modules/mkyctl.lua
   curl -s "http://127.0.0.1:8888/?api=status" | jq .    # first call: cold-start refresh
   ```
2. **`tgtadm` worst-case duration is still unknown.** The launcher budget is
   now 20s. If a real `tgtadm` under heavy load exceeds that, the status call
   will still be slow — though it can no longer *block* the worker.
   Recommended follow-up: make `checkstatpc` cheap or add a dedicated
   `?api=health` endpoint that does no inventory work. I did not change the
   Lua server's public API beyond the additive `status{}` field.
3. **Snapshot file permissions.** `/srv/mkyboot/cfg/status.cache` is created by
   the nginx worker (www-data) with default umask. It contains only client IP
   addresses and service flags — no secrets — but consider `chmod 644` in
   `install.sh` for consistency with the rest of `cfg/`. Not changed here to
   keep this phase's diff focused.
4. **`status.lock` staleness on a hung refresh.** If a refresh hangs past
   `lock_ttl` (10s), a second refresh may start while the first is still blocked
   in `tgtadm`. The `timeout -s KILL` wrapper bounds this, but the overlap is
   theoretically possible. Acceptable: both write via atomic rename and the
   later write simply wins.
5. **Multi-worker memo staleness.** `status.memo` is validated by file mtime via
   LuaFileSystem. If two refreshes land within the same mtime granularity tick,
   a worker could serve a decode from the previous file version for up to one
   `max_age` window. Impact is cosmetic (one poll cycle of stale service flags).
6. **Manual UI checks still required:** launch two copies (second must exit and
   the first must come forward); RDP a second session and confirm a second
   launcher is allowed; verify the new `wmAppForeground` restore path visually.
7. **Three-copy drift will recur.** Nothing in the build enforces that
   `srv/` and `test/` stay in sync with `bin/`. A CI check comparing SHA-256 of
   the status and server-control regions would prevent it. Not added here
   (would require CI infrastructure that does not exist in this repo).

---

## 10. Summary

| Metric | Result |
|---|---|
| **Non-blocking status** | **PASS** — verified by 85 assertions × 3 copies; constant-cost refresh, no per-request blocking |
| **Single-instance launcher** | **PASS** — 10 Go tests + 13/13 cross-process checks on the real exe |
| **Lua deployment consistency** | **PASS** — server-control API in all three copies; `srv/` byte-identical to canonical `bin/`; test fixture difference preserved |
| **Go build and tests** | **PASS** — gofmt clean, vet clean, build exit 0, 61 PASS / 0 FAIL / 1 SKIP |
| **Lua tests** | **PASS** — syntax clean, 381 assertions, 0 failures |
| **Existing offline fix preserved** | **YES** — all 8 regression tests pass; no Phase 1 file modified |
| **Existing image refactor preserved** | **YES** — `require("image")` 5/5/0 across bin/srv/test, unchanged |