# Phase 4A.1 — Ubuntu CI and real-host readiness

**Date:** 2026-10-07
**Repository:** `mikkkoyy/MKYBOOT-SERVER`, branch `main`
**Starting commit:** `7ca5ab882095faa2d37597b961cb834c64282826`
**Source-validation CI evidence:** GitHub Actions run `37583170598` for `f4f1c9b0bc08baf32e7f09006d26b1238892a323` and run `37583443393` for the network-guide commit `2ee217ba4779600eff4517757079b69900c0aef5`: **Ubuntu and Windows jobs both PASS** on each run. The report's own commit cannot embed its own hash; that hash and remote verification belong in the final response.

## Environment and scope

The Windows development host has no authorized Ubuntu server or PXE client: WSL and VirtualMachinePlatform are disabled, with no Hyper-V, Docker, QEMU, configured SSH host or cloud credentials (Phase 4A discovery). GitHub-hosted `ubuntu-22.04` **did** execute isolated source/dependency/syntax checks. This is not an installed MKYBOOT host and provides no ZFS pool, running OpenResty application, iSCSI client or isolated DHCP/PXE network. `windows-2022` ran the launcher Go tests separately; a Windows test executable cannot run on Ubuntu.

The workflow is `.github/workflows/ubuntu-integration.yml`; it runs on pushes and PRs targeting `main` and by manual dispatch, with read-only repository permission. It installs only `lua5.4`, `luajit` and `isc-dhcp-server` for tests. An ephemeral `policy-rc.d` denies post-install service startup; the CI log confirms `policy-rc.d denied execution of start`. The policy file is restored after installation. No DHCP daemon, iSCSI target, ZFS pool or public network service is started.

## Automated coverage and evidence

| Runner | Checks actually executed | Result |
|---|---|---|
| Local Windows | Lua 5.4 syntax and isolated suites against three `mkyctl.lua` copies; Go formatting, vet, tests, and Windows build; Git Bash script syntax, static PXE asset and install-layout checks | PASS; 85 status + 42 control + 63 auth + 16 DHCP assertions per Lua copy; 29 install-wiring assertions; 6 module-path assertions; launcher tests PASS. No local LuaJIT, nginx or `dhcpd` execution. |
| Ubuntu 22.04 Actions | `bash -n` for `install.sh`, runtime/host scripts; dependency inventory; CI-safe installation mapping and production-copy comparison; `luac5.4 -p` and LuaJIT bytecode syntax for first-party Lua and tests; isolated suites on **both** Lua 5.4 and LuaJIT (all three copies); status measurement stubs; module resolution from `/`, `/tmp`, a new unrelated directory and the checkout; TFTP/DHCP static references; ISC `dhcpd -t` on a BOM-normalized example **and on the exporter-generated config**; Go formatting/vet/Windows cross-build; `git diff --check` | PASS, run `37583170598`. Lua suites: (85 + 42 + 63 + 16) × 3 + 29 = **647 assertions per interpreter**, 0 failures; 8 module-path invocations × 6 = 48 additional passing assertions. The generated DHCP config and normalized example both passed parser-only validation. No DHCP server started. |
| Windows 2022 Actions | `go vet -unsafeptr=false ./...`, `go test -timeout 300s ./...` (including security/unit tests), Windows GUI build and `git diff --check` | PASS, run `37583170598`. Go formatting is checked in the Ubuntu job. The visual/desktop instance harness is not executed in CI. |

The CI dependency checker `tools/linux/check-deps.sh` reports `PASS`, `MISSING`, `OPTIONAL` and `PRIVILEGED / MANUAL` separately. Missing **runtime** tools on the hosted runner do not masquerade as installed services or fail the independent unit suite. `tools/linux/check-install-layout.sh` uses `bash -n`, checks canonical `bin/mkyctl.lua`, `src/cfg.lua`, `src/image.lua`, TFTP artifacts, and compares the deployable `srv/` mirror without running `install.sh` or writing `/srv`. `tools/linux/check-pxe-assets.sh` compares actual `filename` directives against shipped TFTP artifacts and preserves optional `ipxe32.efi` semantics; `test_dhcp_export.lua` exercises both absent and installed branches. `tools/linux/phase4a-host-check.sh` is read-only and reports host, network, binaries, services and ZFS state on a future Ubuntu host; it is **syntax-checked**, not claimed as a real-host execution.

A CI failure uncovered a real production compatibility defect: `bin/mkyctl.lua:242` used Lua 5.3's infix `~`, rejected by LuaJIT (`')' expected near '~'`). `ce3ec99` replaced it in all three copies with LuaJIT's `bit.bxor` and extended the auth test double. Later CI verified LuaJIT syntax and isolated auth tests. Early workflow failures also identified the Windows Go 1.21 `gofmt` gate (all checked-out files reported despite the Ubuntu LF checkout passing), and `dhcpd` path restrictions; Windows vet/tests/build and Ubuntu formatting are now separate, and parser-only checks use a uniquely named temporary file under `/etc/dhcp` on the disposable runner, removed after checking.

### Raw DHCP example limitation

`examples/etc/dhcp/dhcpd.conf` still begins with a UTF-8 BOM. `dhcpd -t` rejects the **raw** example at line 0. `check-pxe-assets.sh` reports `MISSING` for that condition. CI removes the BOM **only in its temporary test copy**, then separately parser-checks a config emitted by `ExportDHCP()` (which does not include the BOM). Thus **normalized-example syntax and generated-config syntax are PASS; raw-example syntax is FAIL**, not silently counted as a passing raw-file test. A host operator should use the generator or remove the BOM before using that example directly. This is an open source-fixture issue, not evidence of a working DHCP service.

The Phase 4A exporter fallback to `ipxe.efi` does not make genuine 32-bit UEFI firmware bootable. The 32-bit path requires building/installing `ipxe32.efi`; CI does not manufacture or claim that artifact.

## Coverage matrix

`Ubuntu CI` below means the Ubuntu **source-validation job** only. Go unit tests actually run on the separate Windows Actions job, not on Ubuntu. `PASS` in a source row does not mean live service acceptance.

| Test | Local Windows | Ubuntu CI | Real Ubuntu | PXE Client |
|---|---|---|---|---|
| Go tests | PASS | NOT RUN | NOT RUN | NOT RUN |
| Lua tests | PASS | PASS | NOT RUN | NOT RUN |
| Auth (isolated) | PASS | PASS | NOT RUN | NOT RUN |
| Module path | PASS | PASS | NOT RUN | NOT RUN |
| DHCP export (stubbed) | PASS | PASS | NOT RUN | NOT RUN |
| Status snapshot (stubbed) | PASS | PASS | NOT RUN | NOT RUN |
| Install wiring (static) | PASS | PASS | NOT RUN | NOT RUN |
| OpenResty runtime | BLOCKED | NOT RUN | NOT RUN | NOT RUN |
| ZFS operations | BLOCKED | NOT RUN | NOT RUN | NOT RUN |
| iSCSI target and login | BLOCKED | NOT RUN | NOT RUN | NOT RUN |
| DHCP service / leases | BLOCKED | NOT RUN | NOT RUN | NOT RUN |
| TFTP network transfer | BLOCKED | NOT RUN | NOT RUN | NOT RUN |
| iPXE network boot | BLOCKED | NOT RUN | NOT RUN | NOT RUN |
| Diskless OS boot | BLOCKED | NOT RUN | NOT RUN | NOT RUN |
| Write-back isolation | BLOCKED | NOT RUN | NOT RUN | NOT RUN |

Ubuntu Go **format, vet and Windows cross-build** and Windows Actions Go **tests** passed in run `37583170598`. CI performs static boot-artifact and `dhcpd` parser checks; these do not validate a lease, transfer or network boot. CI mocks status command execution and lock contention; it does **not** prove real multi-worker OpenResty latency or non-blocking behaviour when an actual `tgtadm` process hangs.

## Remaining manual acceptance

Use `docs/PHASE4A_PXE_TEST_NETWORK.md` to reserve a conflict-free **isolated** subnet and dedicated NIC/private virtual switch. Required: authorized Ubuntu 20.04/22.04 lab host with a disposable ZFS-capable disk/pool, `nginx-extras`/Lua, `tgt`, `tftpd-hpa`, `isc-dhcp-server`, image tools, lab NIC, disposable boot image, and a PXE-capable VM or physical client. Do **not** connect the DHCP service to a production LAN or overwrite production images.

On that host first run `bash tools/linux/phase4a-host-check.sh` (read-only), inspect output, then, only with authorization, test `bash -n install.sh`, clean installation, `nginx -t`, installed Lua dependencies, authenticated `/?api=status`, and multi-worker snapshot freshness/latency with real commands. On the private network capture DHCP lease/options, TFTP transfer, actual iPXE script, iSCSI discovery/login/LUN, disposable OS boot, overlay isolation, write-back persistence and interrupted-session cleanup. A successful CI run or a PXE shell is **not** diskless boot acceptance.

## Security and Git

No credentials, private keys or tokens were added. The Lua password verification keeps the existing constant-work comparison while using the supported LuaJIT `bit.bxor`. Workflow permissions are `contents: read`; no secrets are required, and privileged validation is confined to parser-only `dhcpd -t` on a disposable GitHub runner. Launcher security tests remain in the Windows Go suite. CI action runners emitted Node.js 20 deprecation warnings for `actions/checkout@v4` and `actions/setup-go@v5` (forced onto Node 24); run `37583170598` nevertheless passed. Updating action major versions is future maintenance, not a PXE result.

Phase 4A.1 commits pushed and independently verified on `origin/main` before writing this report:

| Commit | Purpose |
|---|---|
| `50006f1998dec223462b9eb52d6415f851fa8eb5` | Add Ubuntu/Windows workflow and CI-safe dependency/install/PXE/host scripts |
| `ce3ec99adb1489be3db5aa064d8e264141eb0b47` | Fix LuaJIT XOR syntax in authentication |
| `b54a1659d41adff0a407cf80419cc08d78545e53` | Diagnose Windows format and DHCP path failures |
| `4d4d983556b4a7e2a825f3efab5ae06533da3af6` | Enable Windows tests and move parser input to readable temporary path |
| `5e5cc99310384e0cd018719d5ba06cfe323e8331` | Try AppArmor-permitted test path, keep Go checks independent |
| `8b6e96f37edff7cf6802153de34c0c4fc37c9582` | Clearly distinguish BOM-normalized DHCP example from raw file |
| `f15e93d6f8aa5312a91f07f93dc64e5070eb6004` | Run isolated Lua suites with LuaJIT and Lua 5.4 |
| `f4f1c9b0bc08baf32e7f09006d26b1238892a323` | Syntax-check actual exporter output in addition to example |
| `2ee217ba4779600eff4517757079b69900c0aef5` | Document isolated PXE test network |

All listed commits were fast-forward pushed and verified against `git ls-remote origin refs/heads/main` after their push. Earlier failed CI runs are not described as successes. The **report's own commit hash and final push status** cannot appear in the same commit's contents and must be verified and supplied in the final response. No timer, PC-lock/unlock, client-agent or VHDX feature was added.
