# MKYBOOT-SERVER Phase 4A.2 — DHCP and Linux readiness

**Status: PASS for Phase 4A.2 source/CI scope; BLOCKED for live Ubuntu and PXE acceptance.**

| Repository | Branch | Starting commit | Validated source commit |
|---|---|---|---|
| `mikkkoyy/MKYBOOT-SERVER` | `main` | `8280fbdb0b0f545493be909a1d42bd8e5fedb6fe` | `1e69a380ae8e872a3b7831315889551463eec339` |

The final report commit cannot contain its own hash. Its full hash and push verification are given in the final response after the commit is created.

## DHCP: raw and generated configuration

- **PASS — BOM removal.** `examples/etc/dhcp/dhcpd.conf` previously began with `EF BB BF`; it now begins with `23 23 20` (`## `). A byte-for-byte comparison confirmed the replacement file equals the previous file *after its first three bytes*. The staged Git diff contained exactly that one-line BOM removal; no DHCP directives were changed.
- **PASS — raw DHCP syntax, Ubuntu CI.** GitHub Actions run `37585547652` on commit `1e69a38` used `cmp -s` to verify a temporary config was byte-identical to the checked-in raw file and then ran `sudo dhcpd -t -cf` on it. The temporary location under `/etc/dhcp` is needed for the runner's `dhcpd` path restriction; this is **not** BOM normalization. No daemon was started and the temporary file was removed.
- **PASS — generated DHCP syntax, Ubuntu CI.** The separate `ExportDHCP()` regression suite wrote its isolated, default generated configuration. CI ran `sudo dhcpd -t -cf` on that output too; run `37585547652` logged `PASS generated DHCP syntax only; no DHCP server was started`.
- **PASS — static boot filename validation.** `tools/linux/check-pxe-assets.sh` now fails immediately on a BOM and prints `PASS raw DHCP example has no UTF-8 BOM` otherwise. It checks shipped `ipxe.kpxe`, `ipxe.efi`, `bg.png`, every active DHCP `filename`, and the iPXE splash reference. The optional `ipxe32.efi` is not required by the stock configuration.
- **PASS — optional filename behaviour.** The existing `test_dhcp_export.lua` tests absent and present `ipxe32.efi`. One new assertion checks that *all* stock generated filenames are `ipxe.kpxe` or `ipxe.efi`. That suite passed **17/17** for each of `bin/`, `srv/` and `test/` under local Lua 5.4 and CI Lua 5.4/LuaJIT. The optional reference remains in the generator. The default 64-bit fallback **cannot boot genuine 32-bit UEFI**; a real 32-bit artifact must be built and installed separately.

The workflow at `.github/workflows/ubuntu-integration.yml` no longer contains BOM detection or stripping. It runs a raw, byte-identical syntax check and retains an independent generated-config syntax check. Both CI jobs—`ubuntu-source-validation` and `windows-launcher-tests`—concluded **success** in run `37585547652` for the source commit.

## Tests and verification

| Check | Result | Evidence / limitation |
|---|---|---|
| Go formatting | PASS | Local `gofmt -l .` clean; Ubuntu CI checks `gofmt -l *.go`. PowerShell does not expand `*.go` for the local command, so `gofmt -l .` was used. |
| Go vet (repository command) | PASS | `go vet -unsafeptr=false ./...` local and CI. Plain `go vet ./...` returned **FAIL**, reporting the pre-existing Win32 callback `unsafe.Pointer` conversions at `launcher/ui.go:464,876`; `launcher/Makefile:38-42` documents disabling that one vet check. No unrelated Win32 code was changed. |
| Go tests and Windows build | PASS | Local `go test -timeout 300s ./...` and Windows GUI build. CI Windows job passed vet, tests and GUI build; Ubuntu job passed Windows cross-build. |
| Lua 5.4 | PASS | Local status 85/85, control 42/42, auth 63/63 and DHCP export 17/17 per `mkyctl.lua` copy; install wiring 29/29 and module path 6/6 per production copy. |
| LuaJIT | PASS in Ubuntu CI; NOT RUN locally | Both production syntax and isolated suites passed on the Ubuntu runner. LuaJIT is not installed on this Windows host. |
| Shell syntax | PASS | Git Bash `bash -n` on `install.sh`, all relevant `test/runtime/*.sh` and `tools/linux/*.sh`; Ubuntu CI ran the same syntax checks. |
| Static PXE/TFTP | PASS | Local and Ubuntu CI asset/filename checks; no network traffic or file transfer tested. |
| CI | PASS | GitHub Actions run `37585547652`, both jobs successful; the report-only commit triggers a later run that must be verified separately. |
| Local `dhcpd -t` | NOT RUN | No Linux `dhcpd` on Windows; raw and generated checks ran and passed in Ubuntu CI. |
| Real installed MKYBOOT server | BLOCKED | No authorized Ubuntu/PXE lab environment. |

`git diff --check` passed before the source commit. The BOM fix and strengthened regression test did **not** modify `bin/mkyctl.lua`, `srv/mkyboot/mkyctl.lua` or `test/srv/mkyboot/mkyctl.lua`: the deployable copy remains synchronized with the canonical production file and the test fixture retains its intentional inline image-lifecycle difference. No authorization, password verification, rate limiting or launcher security test was weakened.

## Runtime acceptance — not performed

| Subsystem | Result | Why |
|---|---|---|
| Ubuntu MKYBOOT clean installation | BLOCKED | No authorized isolated Ubuntu server |
| OpenResty/nginx serving Lua endpoints | NOT RUN | Hosted CI does not start the application |
| ZFS snapshots/images | NOT RUN | No disposable pool or Linux lab |
| `tgt`/iSCSI login | NOT RUN | No isolated target/client |
| DHCP service and client leases | NOT RUN | CI uses only `dhcpd -t`, never starts DHCP |
| TFTP service and transfer | NOT RUN | Static assets checked; no TFTP network |
| iPXE/PXE client | NOT RUN | No isolated PXE-capable VM/client |
| Diskless OS boot | NOT RUN | No boot image and client lab |
| Write-back isolation | NOT RUN | No booted pair of test clients |

Passing parser checks and GitHub Actions is **not** proof of DHCP lease issuance, TFTP transfer, iSCSI login or diskless boot. To proceed, provide an authorized Ubuntu 20.04/22.04 lab host, disposable ZFS storage, `nginx-extras`, `tgt`, `isc-dhcp-server`, `tftpd-hpa`, an isolated network and PXE-capable test clients. Start with the read-only `bash tools/linux/phase4a-host-check.sh`, then follow `docs/PHASE4A_PXE_TEST_NETWORK.md` on a disposable installation. Do not start a DHCP service on a production LAN.

## Git, security and scope

- Source commit: `1e69a380ae8e872a3b7831315889551463eec339` — `fix: validate raw BOM-free DHCP example in CI`; normal push to `origin/main` was verified against `git ls-remote`.
- This report's own commit and its GitHub push are recorded in the final response after verification; self-referencing a commit hash inside its own contents is impossible.
- No passwords, API tokens, SSH keys or private credentials were added. Existing example host addresses were preserved byte-for-byte.
- **No timer implementation added. No PC lock implementation added. No Windows client agent added. No VHDX implementation added.**
