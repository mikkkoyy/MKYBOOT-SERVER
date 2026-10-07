# MKYBOOT Launcher — Offline / OFFLINE Diagnosis and Fix Report

**Date:** 2026-10-07
**Project:** MKYBOOT-SERVER v4.0.0 (`D:\project\diskless+timer\diskless`)
**Component:** `launcher/` — native Windows companion app (Go 1.21, Win32)
**Baseline commit:** `816f3c0d7ad69f59c1d7d3ae8c0423e8aab4b4c6`, branch `main`
**Go toolchain used:** `go1.27.1 windows/amd64` (`C:\Program Files\Go\bin\go.exe`)

---

## 1. Root Cause Discovered — CONFIRMED

### 1.1 A correction to the task premise, stated up front

The task asked me to fix "server startup": process creation, OpenResty startup,
working directory for launching the server, duplicate-instance detection, log
capture, and so on.

**That code does not exist, and must not be added.** The launcher is an HTTP
client and GUI only. Verified by exhaustive search of `launcher/*.go`:

- Zero occurrences of any process-creation API. Grep for
  `os/exec|exec.Command|CreateProcess|StartProcess|cmd.exe|powershell|syscall.Start|WorkDir`
  across the whole launcher returns **only two comment hits** (`app.go:19`,
  `app.go:506`), both prose about nginx.
- `main.go:2` states the role explicitly: *"native Windows companion
  application for the MKYBOOT-SERVER **Linux** diskless boot server."*
- `security_test.go:46` (`TestNoShellSSHOrProcessExecution`) is a **standing
  test that fails the build** if any process-execution token appears in launcher
  source. This is a deliberate architectural invariant of the codebase, not an
  oversight.
- The server is not even a single process. `install.sh` + `README.md` require
  nginx-extras (OpenResty/Lua), ZFS, `tgt` (iSCSI), `tftpd-hpa`, and
  `isc-dhcp-server`, all on Ubuntu 20.04/22.04. It cannot be started natively on
  Windows at all.

So "the launcher cannot start the local server" is **not a bug** — it is the
design. Implementing a "start the server" subsystem would have required
violating a security invariant and inventing a component that cannot function on
the target OS. I did not do that, and I did not fake it. **This is reported as
an out-of-scope finding, not silently ignored.**

### 1.2 The actual confirmed root cause

**The launcher applied one 5-second budget to both TCP connect *and* response,
against an endpoint whose server-side implementation blocks the nginx worker on
one subprocess spawn per configured workstation. A healthy but loaded server was
therefore classified as `Timeout`, and because every recovery action was gated
on `StateOnline`, the launcher disabled all recovery — a self-inflicted
dead end.**

The chain of causation, each link verified in source:

1. **`bin/mkyctl.lua:918` — `mkyboot:checkstatpc(p_ip)`** shells out and blocks:
   ```lua
   function mkyboot:checkstatpc(p_ip)
       if not mkyboot.inc.valid.ipv4(p_ip) then return false end
       local fd = io.popen("/usr/sbin/tgtadm --lld iscsi --op show --mode target 2>/dev/null | /usr/bin/grep 'IP Address: "..p_ip.."' 2>/dev/null")
       local result = (#fd:read("a*") > 0)
       fd:close()
       return result
   end;
   ```
   `io.popen` blocks the calling coroutine *and* the whole nginx worker.

2. **`bin/mkyctl.lua:1375-1400` — `get_server_status()`** calls
   `checkstatpc(v.ipv4)` once **per workstation** in its loop (line 1394), plus
   three blocking `lsof` calls (lines 1386-1388) and `ls_devices()`.
   `srv/cfg/mkyboot.json` ships **3 workstations**, so one status call costs
   3 × lsof + 3 × (shell fork + `tgtadm` + `grep`). When `tgt` is slow or
   briefly unresponsive, this exceeds 5 seconds easily.

3. **`launcher/server.go` (before) — a single 5s budget for everything:**
   `statusTimeout = 5 * time.Second` was set as `http.Client.Timeout`, and the
   bare `http.Transport{Proxy: nil}` had no `DialContext` timeout and no
   `ResponseHeaderTimeout`. Connect time and response time were
   indistinguishable, so "port is closed" and "server is slow" produced the
   same class of failure.

4. **The dead end.** `app.go:449` (before): `if res.State != StateOnline { showNotice(...); return }`,
   and `ui.go:743` / `tray.go:97`: `canControl := ... && online`.
   A `Timeout` is not `StateOnline`, so **Restart Server and Stop Server were
   both disabled** — and the underlying condition (nginx merely busy) is exactly
   one that resolves on its own with a retry.

### 1.3 Reproduction — executed, not asserted

I wrote a temporary harness exercising the real `CheckStatus()` against mock
endpoints, ran it, then deleted it. Verbatim output:

```
=== RUN   TestReproSlowServerReportedAsTimeout
    server responded in 6s budget; launcher gave up after 5.0003146s
      -> State=Timeout Detail="request timed out"
    CONFIRMED: healthy-but-busy server reported as "Timeout"
    CONFIRMED DEAD-END: controlFlow() refuses Restart/Stop unless State==StateOnline (got Timeout)
--- PASS: TestReproSlowServerReportedAsTimeout (6.00s)
=== RUN   TestReproConnectionRefusedUseful
    CONFIRMED usable error: State=Offline Detail="connection failed" (fast=true)
--- PASS (0.20s)
=== RUN   TestReproHTTP500NotHealthy
    HTTP500 -> State=ServerError Detail="HTTP 500"
--- PASS (0.00s)
=== RUN   TestReproDegradedIsIndistinguishable
    all diskless services down -> State=Online Detail="MKYBOOT 4.0.0 at 192.168.0.2"
    CONFIRMED: degraded server still reports StateOnline (same state as fully healthy)
--- PASS (0.00s)
PASS
ok  github.com/mikkkoyy/MKYBOOT-SERVER/launcher  6.224s
```

### 1.4 Three further defects found while diagnosing

- **Windows-specific poor diagnostics.** The dial-phase matcher only looked for
  `"connection refused"` (POSIX wording). Windows reports *"No connection could
  be made because the target machine actively refused it"*, so **every Windows
  user got the useless generic `"cannot reach server"`**. Caught by my own new
  test failing after I restructured the classifier.
- **Shutdown race.** `verifyControl` looped on bare `time.Sleep` with no
  cancellation. `Run()` closed `pollStop` and destroyed the window, but
  `verifyControl` kept polling for up to 30s and could `postAppMessage` to a
  destroyed window.
- **Degraded was invisible.** `ServicesRunning` was parsed and then thrown away;
  a server with all three diskless services dead reported plain `Online`.

---

## 2. Files Modified

All changes are confined to `launcher/`. **No non-launcher project file was
touched**; the timer, lock system, image engine, DHCP/TFTP/iSCSI services and
the Lua server are untouched.

| File | Change | Size |
|---|---|---|
| `launcher/server.go` | Split connect vs response budgets; new `newTransport`; dial-vs-slow error classification; Windows refusal wording; added `StateStarting`/`StateDegraded`/`StateStopping`; `Reachable()`; `DownServices`; `CheckStatusContext`; `CheckStatusRetry`; degraded detection | +239/−64 |
| `launcher/app.go` | `stopCtx`/`stopCancel` shutdown context; poll loop uses `CheckStatusRetry(stopCtx)` and is single-flight; `verifyControl` cancellation; `canControlServer`; `setTransientState`; actionable offline dialog | +146 |
| `launcher/ui.go` | `stateColor` covers new states; `updateActionStates` uses `canControlServer`; `infoLine` surfaces the failure reason and down-services; added `strings` import | +30 |
| `launcher/tray.go` | Tray enablement uses `canControlServer` | +3 |
| `launcher/taxonomy_test.go` | Updated state labels; added `Reachable()` and `canControlServer` tests | +48 |
| `launcher/health_test.go` | **New.** 18 tests covering the offline/false-offline matrix | new |
| `LAUNCHER_OFFLINE_FIX_REPORT.md` | **New.** This report | new |

### Pre-existing user changes — verified preserved

Recorded at Phase 1, re-verified after all work:

```
 M .gitignore
 M bin/mkyctl.lua            (extracts image lifecycle to src/image.lua)
 M srv/mkyboot/mkyctl.lua    (same extraction)
?? src/image.lua
?? REPO_COMPARISON_REPORT.md (from the earlier audit task)
```

All still present and unmodified by me. `HEAD` is still `816f3c0d…` on `main`.
No reset, checkout, stash, pull, or branch switch was performed. I did not
delete or overwrite any pre-existing file.

I removed exactly one file — `launcher/launcher.exe` — which **I** created
myself when running a baseline `go build ./...` (it is not gitignored, unlike
`launcher/dist/`). It was absent from the Phase 1 `git status`.

---

## 3. Fixes Implemented

### A. Correct health detection

- **Separated the budgets.** `dialTimeout = 3s` (connect only, via a
  `net.Dialer` in `DialContext`) and `statusTimeout = 20s` (whole response).
  A closed port now yields a fast, definitive `Offline`; a busy server gets a
  budget matched to its actual cost. `controlTimeout`/`loginTimeout` raised to
  20s for the same reason.
- **Distinguished dial failure from slow response.** `classifyTransportError`
  now checks `net.OpError.Op == "dial"` first. A post-dial timeout becomes
  `StateTimeout` with detail *"server reachable but slow to respond"* rather
  than being lumped in with unreachable.
- **Added `ServerState.Reachable()`** — the explicit separation of "the server
  is there" from "the server is usable right now".
- **Added `StateDegraded`** — server answers correctly but DHCP/TFTP/iSCSI is
  down, with `DownServices` naming exactly which. This is *not* offline.
- **Bounded retry with short backoff.** `CheckStatusRetry` = up to 2 retries at
  500ms. It retries **only** transient outcomes (`Timeout`); a refusal, HTTP
  error or malformed payload is a deterministic answer and returns immediately.
- **Cancellation everywhere.** `CheckStatusContext` takes a context;
  `CheckStatus()` delegates to it. Cancellation is reported as
  `"check cancelled"`, never as a server fault.
- **Robustness preserved.** HTTP errors, refusal, DNS failure, oversized bodies
  (>1 MiB), non-JSON and foreign-JSON payloads all handled without panics.
- **No duplicate work.** `pollLoop` runs one check at a time on a single
  goroutine; `requestCheck` is a 1-buffered channel, so rapid clicks coalesce
  rather than stacking.

### B. Correct launcher UI and recovery

- **States now shown:** `Connecting`, `Starting`, `Online`, `Degraded`,
  `Slow Response`, `Offline`, `Stopping`, `Unauthorized`, `Server Error`,
  `Malformed Response`.
- **`Starting` / `Stopping`** are set immediately when a control operation is
  requested (`setTransientState`), so the UI never claims `Online` during a
  restart or stop. They are distinct states precisely so neither "online" nor
  "offline" is asserted while unproven.
- **Retry action:** the existing `Server Status` button *is* the retry
  (`manualCheck` → `requestCheck`). I kept its label and ID deliberately because
  `launcher/uitest/uitest.ps1:41,58` asserts the exact string `"Server Status"`.
  It stays enabled in **every** state, reachable or not, so the user is never
  stuck.
- **Actions are disabled only when they cannot succeed.** `canControlServer`
  permits `Online`, `Degraded` and `Unauthorized` — all cases where nginx is
  genuinely answering the control endpoint. `Starting`/`Stopping` are blocked so
  a second operation cannot be queued during a transition.
- **Failure reason is surfaced.** `infoLine` now shows `Reason: <detail>` for
  unreachable states and a retry hint for `Slow Response`.

### C. Correct shutdown

- `App` gained `stopCtx`/`stopCancel`; `Run()` calls `stopCancel()` immediately
  after the message loop ends.
- `verifyControl` now selects on `stopCtx.Done()` instead of `time.Sleep`, and
  returns `false` promptly on shutdown — fixing the post-destruction window
  message.
- `pollLoop` returns on `stopCtx.Done()` and skips applying results after
  shutdown, so no goroutine mutates UI state against a destroyed window.

### D. Honest recovery boundary

While nginx is down, the control API **cannot** be used — it is served *by*
nginx. The launcher cannot restart nginx via nginx. Rather than hide this, the
offline dialog now states the state, the reason, and the remedy:

> Restart/Stop needs a reachable MKYBOOT server. Current state: Offline -
> connection refused.
> The control API is served by the same nginx process it controls, so it cannot
> be used while nginx is down. Start nginx on the MKYBOOT server (for example:
> `sudo systemctl start nginx`), then press Server Status to re-check.

### E. Offline operation

- `Proxy: nil` is retained and now enforced by a test: LAN traffic is never
  routed through a system proxy, so a dead proxy or missing internet cannot make
  a healthy local server look offline.
- No external ping, DNS lookup, GitHub call or download exists in the launcher,
  and none was added. Health is judged purely from the local server's own
  answers.
- No dependency is auto-downloaded and no missing binary is faked.

### Security posture — unchanged and re-verified

All five security tests pass unchanged:

- No process/shell/SSH execution primitives.
- No TLS verification bypass.
- No hardcoded credentials.
- No logging facility at all (so no secret can be logged).
- No remote-execution primitives.

I changed **no** endpoint, binding, privilege or firewall behavior. No
administrative endpoint was newly exposed; LAN access for PXE clients is
untouched. The 20s timeout raises do not weaken authentication, which remains
per-request.

One note on my own process: my first draft wrote the literal token `io.popen`
inside a `server.go` comment explaining the root cause, which **correctly
tripped** `TestNoShellSSHOrProcessExecution`. I reworded the comment to describe
the blocking spawn without the banned token. The security test was not weakened.

---

## 4. Tests Executed and Exact Results

### Build and static analysis

| Command (run from `launcher/`) | Result |
|---|---|
| `go build ./...` | **PASS**, exit 0 |
| `go vet -unsafeptr=false ./...` | **PASS**, exit 0, no diagnostics |
| `gofmt -l .` | **PASS**, no files listed (all formatted) |

### Baseline before changes (to prove no regression)

```
go version go1.27.1 windows/amd64
=== BUILD ===   exit=0
=== VET ===     exit=0
=== TEST BASELINE ===  ok  github.com/mikkkoyy/MKYBOOT-SERVER/launcher  2.094s
```

### Final test run

```
$ go test ./...
ok      github.com/mikkkoyy/MKYBOOT-SERVER/launcher      8.939s
test exit=0
```

`go test -v ./...` — **68 test cases, all PASS, 0 FAIL**, comprising 4 new
taxonomy tests and 18 new health tests plus all 34 pre-existing tests
(`config_test.go`, `server_test.go`, `control_test.go`, `security_test.go`).

New tests, all PASS:

| Test | Phase 5 requirement |
|---|---|
| `TestSlowButReachableIsNotOffline` | slow server is not Offline; detail explains it |
| `TestRetryPromotesRecoveringServerToOnline` | slow startup does not mislabel |
| `TestRetryIsBounded` | retry loop is bounded (≤3 attempts, <5s) |
| `TestNoRetryOnDefinitiveRefusal` | no wasted retries |
| `TestConnectionRefusalDetailIsActionable` | refusal yields a useful error, fast |
| `TestDegradedWhenServicesDown` | partial services → Degraded, names TFTP/iSCSI |
| `TestAllServicesDownIsStillDegradedNotOffline` | full outage still not offline |
| `TestHTTP500IsNotHealthyAndStaysReachable` | HTTP 500 not healthy |
| `TestHealthyLocalServerStaysOnlineWithoutInternet` | internet loss does not mark local offline |
| `TestWorksFromArbitraryWorkingDirectory` | foreign CWD does not break paths |
| `TestCheckStatusContextCancels` | cancellation aborts promptly |
| `TestCheckStatusRetryRespectsCancellation` | retry honours cancellation |
| `TestVerifyControlAbortsOnShutdown` | shutdown cancels background work |
| `TestVerifyControlSucceedsWhenServerRecovers` | restart confirmed when server returns |
| `TestVerifyControlConfirmsStop` | stop confirmed when server gone |
| `TestTransientStatesAreDistinctFromOnline` | Starting/Stopping not aliased to Online |
| `TestOversizedResponseRejected` | >1 MiB body rejected |
| `TestForeignJSONIsNotAccepted` | foreign JSON not accepted |
| `TestStateReachability`, `TestCanControlServer` | action gating is correct |

The "no internet" test is a genuine regression guard, not a label: it sets
`HTTP_PROXY`, `HTTPS_PROXY`, `http_proxy`, `https_proxy`, `ALL_PROXY` to a dead
address (`http://127.0.0.1:1`), clears `NO_PROXY`, asserts
`transport.Proxy == nil`, and requires `StateOnline`. If anyone re-enables
proxy support, this test fails.

### Release build

```
$ go build -trimpath -ldflags "-s -w -H windowsgui" -o "dist/MKYBOOT Launcher.exe" .
build exit=0
Name: MKYBOOT Launcher.exe   Length: 7097344 bytes
```

---

## 5. Commands Used to Verify

```powershell
$env:Path = "C:\Program Files\Go\bin;$env:Path"
Set-Location "D:\project\diskless+timer\diskless\launcher"

gofmt -l .                                     # formatting
go vet -unsafeptr=false ./...                  # static checks
go test ./...                                  # unit tests
go test -v ./...                               # per-test results

# release build (the Makefile's documented plain command)
$env:CGO_ENABLED=0; $env:GOOS="windows"; $env:GOARCH="amd64"
go build -trimpath -ldflags "-s -w -H windowsgui" -o "dist/MKYBOOT Launcher.exe" .
```

`make` is unavailable on this host (no `make`), so I ran the exact plain
commands the `launcher/Makefile` documents as equivalents.

---

## 6. Remaining Failures and Environmental Limitations

Everything below is **NOT RUN**. I am not claiming these passed.

| Item | Status | Reason |
|---|---|---|
| **Local live health check against a real MKYBOOT server** | **NOT RUN** | No MKYBOOT server exists on this host. `Get-NetTCPConnection -LocalPort 8888` → no listener; `Test-NetConnection 127.0.0.1 -Port 8888` → `TcpTestSucceeded: False`; zero `nginx/openresty/tftpd/tgt/zfs` processes. The server requires Ubuntu + ZFS + `tgt` + `tftpd-hpa` + `isc-dhcp-server`. WSL is present but no distro/server is installed, and deploying one was outside an audit-and-fix scope. |
| **Internet-disconnected test (physical)** | **NOT RUN** (simulated instead) | Cannot disconnect the host's network. Covered by `TestHealthyLocalServerStaysOnlineWithoutInternet`, which is a proxy-reachability simulation, not a real air-gap test. |
| **Duplicate-process test** | **NOT APPLICABLE** | The launcher creates no processes, so there is no duplicate-instance risk to test. The guarantee is enforced statically by `TestNoShellSSHOrProcessExecution`. The equivalent runtime concern — concurrent health probes — is covered by `TestRetryIsBounded` and the single-flight poll loop. |
| **`go test -race`** | **NOT RUN** | `-race` requires cgo; `go test -race` with `CGO_ENABLED=1` fails: `C compiler "gcc" not found`. No MinGW/VS toolchain on this host. Concurrency is therefore argued structurally (single poll goroutine, `sync.Mutex`, cancellable contexts) but **not** race-detector-verified. |
| **`launcher/uitest/uitest.ps1` (scripted UI harness)** | **NOT RUN** | Requires a real interactive desktop session and inspects actual pixels. This session is non-interactive. Its expectations remain valid by construction: it asserts Restart/Stop are disabled when offline, and `canControlServer(StateOffline) == false` (verified by `TestCanControlServer`). Its button-text assertions (`"Server Status"`, `"Restart Server"`, `"Stop Server"`) are unchanged. |
| **Actual Windows GUI smoke test** | **NOT RUN** | Would require launching a GUI app in an interactive session. The exe builds; its runtime UI was not exercised. |
| **PXE / iSCSI / Windows boot behaviour** | **OUT OF SCOPE** | Server-side, requires diskless lab hardware. Untouched by this fix. |
| **TFTP port occupancy test (case 6 in Phase 3)** | **NOT RUN as specified** | Port 8888 occupied by another process is equivalent to "something answered that is not MKYBOOT" → covered by `TestForeignJSONIsNotAccepted` and `TestStatusHTTPErrorCodes`. Deliberately, the launcher does **not** kill whatever holds 8888. |

### Residual risks worth stating

1. **`statusTimeout` is now 20s.** Correct for a server blocked on N
   subprocess spawns, but with a very large workstation count the endpoint could
   still exceed it. The genuine fix is server-side: make `checkstatpc` (or add a
   cheap liveness endpoint) non-blocking. I did **not** change the Lua server,
   per the instruction not to touch unrelated services. This is the recommended
   follow-up.
2. **`bin/mkyctl.lua` vs `srv/mkyboot/mkyctl.lua` drift.** The control
   endpoint `?api=server&op=` exists **only** in `bin/mkyctl.lua` (line 1809),
   which is the copy `README.md:120` installs to
   `/srv/mkyboot/modules/mkyctl.lua`. The other two copies lack it. Since
   `app.go` asserts `controlAPISupported = true`, a deployment that installed
   the wrong copy would get a broken Restart/Stop. Pre-existing, not introduced
   by me, and flagged rather than changed.
3. **`statusPollInterval` is 10s** while a status call may now run up to 20s
   (plus up to 2 retries), so checks can overlap slightly in wall-clock terms —
   but the single-flight loop serialises them, so no concurrency issue arises,
   only a slower effective refresh under heavy load. Accepted deliberately.

---

## 7. How to Launch and Verify Offline

### 7.1 On the MKYBOOT server (Ubuntu)

```bash
sudo bash install.sh                 # or follow README manual install
sudo systemctl start nginx           # the launcher judges nginx
sudo systemctl start isc-dhcp-server # optional: for full ONLINE not DEGRADED
sudo systemctl start tftpd-hpa
sudo systemctl start tgt
curl -s "http://127.0.0.1:8888/?api=status" | head -c 200
```

Expect `{"error":"Not authenticated"}` when unauthenticated — that alone proves
the server is up and reachable.

### 7.2 Build the launcher on Windows

```powershell
Set-Location "D:\project\diskless+timer\diskless\launcher"
$env:Path = "C:\Program Files\Go\bin;$env:Path"
$env:CGO_ENABLED=0; $env:GOOS="windows"; $env:GOARCH="amd64"
go build -trimpath -ldflags "-s -w -H windowsgui" -o "dist/MKYBOOT Launcher.exe" .
```

### 7.3 Verify the OFFLINE → ONLINE transition

1. With nginx **stopped**, launch the exe. Expect state **Offline** with reason
   `connection refused`, Restart/Stop **disabled**, `Server Status` **enabled**.
2. Press **Server Status**. It retries briefly, then still reports Offline —
   correct, and now with an explicit reason plus the `systemctl start nginx`
   remedy instead of a bare "not reachable".
3. On the server, `sudo systemctl start nginx`.
4. Press **Server Status**. Expect **Online** (or **Degraded** if DHCP/TFTP/
   iSCSI are down — the detail line names which).
5. Stop `isc-dhcp-server` and re-check. Expect **Degraded** naming **DHCP**,
   with Restart/Stop still enabled (nginx is answering).
6. Load the server slowly (`sudo systemctl stop tgt` while several workstations
   are configured, or throttle the worker) and re-check. Expect **Slow Response**
   after the 20s budget rather than a false **Offline**, and a recovery on the
   next poll.

### 7.4 Verify true offline (no internet)

Disconnect WAN / block the uplink, leave the LAN alone, then repeat step 3–5 of
7.3. The launcher must still reach **Online**/`Degraded`. It performs no DNS, no
GitHub call, no external ping and no download, and never routes through a proxy.

---

## 8. Final Summary

| Question | Answer |
|---|---|
| **Root cause** | **CONFIRMED** — single 5s connect+response budget vs an endpoint that blocks the nginx worker on one `tgtadm`+`grep` spawn per workstation (`bin/mkyctl.lua:918`, called per-client at `:1394`), producing false `Timeout`, which disabled all recovery via the `StateOnline` gate at `app.go:449` / `ui.go:743` / `tray.go:97`. |
| **Build** | **PASS** — `go build ./...` exit 0; release exe 7,097,344 bytes; `go vet` clean; `gofmt -l` empty |
| **Unit tests** | **PASS** — 68 cases, 0 failures, including 22 new ones |
| **Local health check (real MKYBOOT server)** | **NOT RUN** — no server on this host (no listener on 8888, no nginx/tgt/tftpd/ZFS); verified against mock endpoints matching the real payload shape instead |
| **Internet-disconnected test (physical)** | **NOT RUN** — simulated via unreachable-proxy environment in `TestHealthyLocalServerStaysOnlineWithoutInternet` |
| **Duplicate-process test** | **NOT APPLICABLE** — launcher creates no processes by design and by enforced test |

**Server-startup work requested in the task was deliberately not implemented**,
because the launcher has no process-execution capability by design, a build-time
security test forbids adding one, and the MKYBOOT server is Linux-only software
that cannot run natively on Windows. Inventing that subsystem would have
produced fake functionality. The equivalent user-facing need — not being
dead-ended when the server is unreachable — is addressed honestly instead:
accurate states, an always-available retry, a `Degraded` state for partial
service outages, actionable reasons, and an explicit statement of the
architectural limit.