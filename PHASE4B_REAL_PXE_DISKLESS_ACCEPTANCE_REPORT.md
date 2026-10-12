# MKYBOOT-SERVER — Phase 4B: Real PXE / iPXE / iSCSI / Diskless Acceptance

**Status: BLOCKED for live PXE/diskless acceptance — no authorized isolated Ubuntu/PXE environment exists. PASS for all safe local/static validation that can run on this host.**

| Repository | Branch | Starting commit | Validated commit | Final commit |
|---|---|---|---|---|
| `mikkkoyy/MKYBOOT-SERVER` | `main` | `c64b0e70d717d3813a0eb2588ee896207a09f543` | `c64b0e70d717d3813a0eb2588ee896207a09f543` | (recorded in final response after commit) |

This report's own commit cannot contain its own hash; the final commit hash and push verification are given in the final response after the commit is created. No source file was changed by this phase — every static check passed against the existing tree.

## Lab determination — why live acceptance is BLOCKED

Phase 4B requires an isolated, authorized lab (a private switch or host-only virtual network with a disposable Ubuntu server and at least one PXE-capable client). The current host was inspected for every prerequisite the phase lists. None are present, and per the safety rules the Windows host networking was **not** modified and **no DHCP/TFTP/iSCSI service was started**.

| Prerequisite | Required by Phase 4B | Observed on this host | Verdict |
|---|---|---|---|
| Linux / Ubuntu runtime | `cat /etc/os-release`, `uname -a`, Ubuntu 20.04/22.04 | Windows host (`win32`); no Ubuntu | ABSENT |
| WSL / WSL2 | `wsl --status`, `wsl -l -v` | `Microsoft-Windows-Subsystem-Linux` = **Disabled**; `wsl --list --verbose` shows **no installed distributions** (only the online catalog) | ABSENT |
| Nested virtualization | Phase 4B VM lab | `VirtualMachinePlatform` = Disabled, `HypervisorPlatform` = Disabled, `systeminfo`: **Virtualization Enabled In Firmware: No** | ABSENT |
| Hypervisor | Hyper-V / Docker / QEMU / VMware / VirtualBox | `docker`, `qemu-img`, `VBoxManage`, `vmrun` — **none on PATH**; no `Get-VMSwitch` (no Hyper-V role) | ABSENT |
| Isolated / private NIC or virtual switch | Phase 4B isolated network | Only **one** adapter: `Ethernet` (Realtek PCIe GBE, MAC `50-EB-F6-23-6B-44`) — the host's production LAN. No private switch, no host-only network. | ABSENT |
| PXE-capable client | Phase 4B client VM/PC | No VM (no hypervisor), no second physical machine | ABSENT |
| Remote authorized Ubuntu host | Phase 4B optional | None configured; arbitrary network scanning is disallowed | NOT CONFIGURED |

**Determination: BLOCKED — no authorized isolated Ubuntu/PXE environment is available.** Live PXE discovery, lease issuance, TFTP transfer, iPXE execution, iSCSI login, diskless OS boot and write-back isolation **cannot** be performed here. They are recorded as **NOT RUN / BLOCKED** below, never as PASS. Fabricating a result or reconfiguring the host's production network to force a pass would violate the phase's safety rules, so the live portion stops here.

The repository's own `docs/PHASE4A_PXE_TEST_NETWORK.md` documents exactly the isolated topology and the read-only first step (`tools/linux/phase4a-host-check.sh`) that a real lab must satisfy. To unblock this phase, provide an authorized Ubuntu 20.04/22.04 host on a private/host-only network with `nginx-extras`, `tgt`, `isc-dhcp-server`, `tftpd-hpa`, a disposable empty ZFS pool, and one or two PXE-capable client VMs.

## Dependency status (as installable / as validated)

| Dependency | Command | Status this phase | Note |
|---|---|---|---|
| OpenResty / nginx | `nginx -v` | BLOCKED | Linux-only runtime; not installed and not started (no lab) |
| ZFS | `zpool --version` / `zfs --version` | BLOCKED | Linux-only; no pool inspected, no disposable pool exists |
| tgt / iSCSI | `tgtadm --version` | BLOCKED | Linux-only; no target created or shown |
| DHCP | `dhcpd --version` | BLOCKED | Linux-only binary; syntax runs in Ubuntu CI |
| TFTP | `in.tftpd --version` | BLOCKED | Linux-only; assets validated statically only |
| qemu-img | `qemu-img --version` | BLOCKED | Linux-only; not present on host |

`tools/linux/check-deps.sh` is the authoritative on-host inventory; it cannot run meaningfully on Windows because none of the Linux packages are present. Its UNIT probes (`lua5.4`, `luajit`, `luac5.4`) are satisfied locally by the standalone `lua`/`luac` 5.4.5 build, but `luajit` is not installed on this host (it runs in Ubuntu CI, as in Phase 4A.2).

## Acceptance matrix

Verdicts use exactly `PASS`, `FAIL`, `NOT RUN`, `BLOCKED`.

### Services (Linux runtime — not started on this host)

| Service | Verdict | Evidence / reason |
|---|---|---|
| OpenResty serving Lua endpoints | BLOCKED | No Linux runtime / isolated lab; service never started |
| ZFS image lifecycle | NOT RUN | No disposable pool; Linux-only |
| iSCSI target (`tgt`) | BLOCKED | No Linux `tgt`; no target created or shown |
| DHCP service + client lease | BLOCKED | No isolated NIC; DHCP deliberately **not** started |
| TFTP service + transfer | NOT RUN | Static assets validated; no TFTP network |

### PXE / diskless chain

| Stage | Verdict | Evidence / reason |
|---|---|---|
| DHCP discovery | NOT RUN | No PXE client, no isolated network, DHCP not started |
| DHCP lease | NOT RUN | Same — no client solicited, no lease issued |
| TFTP transfer | NOT RUN | Asset *presence/size* verified statically; no network transfer |
| iPXE startup | NOT RUN | No firmware/client executed `ipxe.kpxe`/`ipxe.efi` |
| MKYBOOT boot configuration | NOT RUN | Boot script is generated per-client at runtime; no client request |
| iSCSI discovery | NOT RUN | No initiator, no target |
| LUN attachment | NOT RUN | No target/LUN created |
| Diskless OS boot | NOT RUN | No client, no boot image, no isolated lab |
| Write-back/cache isolation | NOT RUN | No booted client pair |

### Safe local/static validation performed this phase (PASS)

| Check | Verdict | Evidence |
|---|---|---|
| Go formatting (`gofmt -l *.go`) | PASS | No files listed; clean |
| Go vet (documented) `go vet -unsafeptr=false ./...` | PASS | Exit 0, no findings |
| Go vet (plain) `go vet ./...` | FAIL (known, pre-existing) | Reports `ui.go:464` and `ui.go:876` `possible misuse of unsafe.Pointer`; unchanged by this phase |
| Go tests `go test -count=1 -timeout 300s ./...` | PASS | `ok github.com/mikkkoyy/MKYBOOT-SERVER/launcher 8.9s` |
| Windows launcher build (`-H windowsgui`) | PASS | Build artifact produced, then removed |
| Lua 5.4 isolated suites | PASS | status_cache 85/85, server_control 42/42, auth 63/63, dhcp_export 17/17 — for `bin/`, `srv/`, `test/` copies |
| Lua 5.4 deploy wiring / perf | PASS | test_deploy_wiring 29/29; measure_status_perf done |
| Lua syntax `luac -p` (all 16 Lua files) | PASS | All parse clean |
| Lua module resolution from unrelated CWDs | PASS | `/`, `/tmp`, fresh temp dir, repo — all PASS |
| LuaJIT suites | NOT RUN | `luajit` not installed on this Windows host; runs in Ubuntu CI |
| DHCP generator `ExportDHCP()` | PASS | Complete dhcpd.conf; 17/17 incl. BIOS `ipxe.kpxe`, 64-bit UEFI `ipxe.efi`, optional `ipxe32.efi` present/absent |
| Raw DHCP example BOM | PASS | First bytes `23 23 20 23 23 23` (`## ##`), **not** `EF BB BF` |
| `dhcpd -t` parse (raw + generated) | NOT RUN | Linux `dhcpd` not present on Windows; runs in Ubuntu CI per Phase 4A.2 |
| Static PXE assets `check-pxe-assets.sh` | PASS | No BOM; `ipxe.kpxe` (336,252 B), `ipxe.efi` (995,872 B), `bg.png` (1,471,754 B); every DHCP `filename` shipped |
| Install-layout wiring `check-install-layout.sh` | PASS | All `install.sh` copy assertions + 29/29 deploy wiring; test fixture retains inline qemu-img lifecycle |
| Shell syntax `bash -n` (8 scripts) | PASS | `install.sh`, all `test/runtime/*.sh`, all `tools/linux/*.sh` |
| `git diff --check` | PASS | No whitespace errors; working tree clean |

## Regression (Section 20 scope)

| Suite | Result | Command / note |
|---|---|---|
| Go | PASS | `gofmt -l *.go` clean; `go vet -unsafeptr=false ./...` exit 0; `go test -count=1 -timeout 300s ./...` ok |
| Lua 5.4 | PASS | All isolated suites + syntax, run with `lua` 5.4.5 |
| LuaJIT | NOT RUN | Not installed locally; runs in Ubuntu CI (as Phase 4A.2) |
| Shell | PASS | `bash -n` on install.sh + all runtime/tools scripts |
| PXE static validation | PASS | `tools/linux/check-pxe-assets.sh` |
| `git diff --check` | PASS | Clean |

## Known Go vet distinction (Section 21)

The documented project command `go vet -unsafeptr=false ./...` passes clean (exit 0). Plain `go vet ./...` returns **FAIL**, reporting the pre-existing Win32 callback `unsafe.Pointer` conversions at `launcher/ui.go:464` and `launcher/ui.go:876`. This is the same documented distinction from prior phases (`launcher/Makefile:38-42` disables exactly that one check). No unrelated Windows API code was modified to manufacture a clean plain-vet result.

## Failures

No repository defect was discovered by this phase — every executable static check passed. No fix was required, so no retest cycle was needed. The only non-passing runtime item is the **absence of a lab**, which is an environmental blocker, not a repository failure:

```text
Stage:        Live PXE / diskless acceptance (entire chain)
Observed:     No Ubuntu runtime, firmware virtualization disabled, no hypervisor,
              single production NIC, no PXE client.
Root cause:   No authorized isolated lab provided.
Fix:          N/A (environmental). Provide an isolated Ubuntu 20.04/22.04 lab on a
              private/host-only network per docs/PHASE4A_PXE_TEST_NETWORK.md.
Retest:       Not possible on this host.
Result:       BLOCKED (recorded as NOT RUN / BLOCKED, never PASS).
```

## Limitations — explicitly not tested

- **Real DHCP discovery / lease / OFFER / REQUEST / ACK** — no client, no isolated network; DHCP deliberately not started.
- **Real TFTP network transfer** — only file presence and size were verified; no UDP transfer occurred.
- **iPXE execution and MKYBOOT boot-script retrieval** — no firmware/client ran `ipxe.kpxe`/`ipxe.efi`.
- **iSCSI target discovery, login, LUN attachment** — no `tgt` target created or shown.
- **Diskless OS boot** — no client, no boot image.
- **Write-back / cache isolation across clients** — no booted client pair.
- **Multiple concurrent clients** — no lab.
- **32-bit UEFI** — no 32-bit UEFI client; the default `ipxe.efi` fallback is not claimed to boot true 32-bit UEFI (documented; `ipxe32.efi` remains optional/unshipped).
- **`dhcpd -t` parse on-host** — Linux-only; executed in Ubuntu CI (Phase 4A.2 run `37585547652`), not on this Windows host.
- **LuaJIT suites on-host** — `luajit` not installed locally; executed in Ubuntu CI.

## Security and scope

- No passwords, API tokens, SSH keys, or credentials were added, read, or printed.
- No DHCP/TFTP/iSCSI/ZFS service was started; the host's production NIC and routing were not modified.
- No destructive ZFS command was run; no existing pool/image/target touched.
- `test/srv/mkyboot/mkyctl.lua` was **not** modified; it retains its intentional inline image-lifecycle difference (verified by the install-layout suite).
- **No timer implementation. No PC lock. No Windows client agent. No VHDX. No session countdown or heartbeat.** This phase only assessed whether the diskless boot foundation can be accepted, and it remains BLOCKED pending a real lab.
