# MKYBOOT-SERVER — Phase 4F: Ubuntu iSCSI integration lab setup and validation report

**Branch:** `phase-4f-ubuntu-iscsi-integration`
**Host:** Windows 10 Home 19045.6693 (build 19045), AMD Ryzen 5 PRO 4650G, 16 GB RAM
**Date:** 2026-10-21

> **What this report does and does not claim.**
> The storage layout, the lab topology, the configuration and the server-side
> iSCSI implementation are in place, and the Go code passes `gofmt`, `go vet`
> and the unit tests on both Windows and Linux targets. The iSCSI layer has
> **not** been exercised against a live Ubuntu target, and **no diskless boot
> has been performed**. The `iscsi` capability flag remains `false`.

---

## 1. Drive capacity check (performed before any download)

Measured with `Get-PSDrive -PSProvider FileSystem` before the ISO download:

| Drive | Free | Verdict |
|---|---|---|
| `C:` | **7.29 GB** (7463 MB) | Critically low — nothing large may be placed here |
| `D:` | **117.29 GB** (120105 MB) | Sufficient for the ISO (2 GB), VM disks and the iSCSI pool |

The Ubuntu Server ISO needs ~2.0 GB and the Ubuntu VM plus its disposable
iSCSI pool need up to ~60 GB of *dynamic* allocation. `D:` satisfies this with
margin; `C:` does not. All downloads and installations were therefore placed
on `D:` from the start.

## 2. Existing-installation audit (before installing anything)

| Item | Finding |
|---|---|
| VirtualBox | **Already installed** — Oracle VirtualBox **7.2.20r175154** |
| Install path | `D:\Program Files\Oracle\VirtualBox\` (`InstallDir` registry value) |
| On `C:`? | **No.** Neither `C:\Program Files\Oracle\VirtualBox` nor the WOW6432Node key exists |
| Default machine folder | Already `D:\MKYBOOT-Lab\VMs` — **no change needed** |
| Existing VMs | `MKYBOOT-Ubuntu` (`d75851c9…`), `MKYBOOT-PXE-Client` (`7cf94900…`), both on `D:` |
| Pre-existing VirtualBox installer | `D:\VirtualBox-7.2.20-175154-Win.exe` (178 MB) and `D:\VirtualBox-Extracted\` — left untouched |
| Go toolchain | `C:\Program Files\Go\bin\go.exe`, **go1.27.1** — pre-existing, not installed by this phase |

**No installer was run.** VirtualBox was already present and already on `D:`,
so requirement 3 (install to `D:`) did not apply. Nothing was installed on
`C:`.

## 3. Directory layout created on D:

```text
D:\MKYBOOT-Lab\
├── Installers\      (empty — no installer was needed)
├── ISOs\            ubuntu-22.04.5-live-server-amd64.iso (2037.93 MB) + SHA256SUMS
├── VMs\             MKYBOOT-Ubuntu\, MKYBOOT-PXE-Client\  (configs + system.vdi)
├── Storage\         iscsi-testpool.vdi (25 GB dynamic) + images\
├── Logs\            mkyboot-server.log
├── Backups\         phase4f-20261021-102417\  (pre-change .vbox copies)
├── Temp\            gocache\, gomodcache\, gopath\, gotmp\
├── Data\            MKYBOOT_DATA_DIR for the lab run
├── Files\           (pre-existing, from the Phase 4C VirtualBox install)
└── config.json      lab server configuration
```

The legacy empty `D:\MKYBOOT-Lab\ISO` directory (Phase 4D) was **verified
empty** and then removed; all documentation now points at `ISOs\`.

## 4. Ubuntu Server ISO — downloaded directly to D:, checksum verified

| Item | Value |
|---|---|
| File | `D:\MKYBOOT-Lab\ISOs\ubuntu-22.04.5-live-server-amd64.iso` |
| Size | 2037.93 MB |
| Source | `https://releases.ubuntu.com/22.04.5/ubuntu-22.04.5-live-server-amd64.iso` |
| Expected SHA256 | `9bc6028870aef3f74f4e16b900008179e78b130e6b0b9a140635434a46aa98b0` |
| Actual SHA256 | identical — **`SHA256_MATCH=YES`** |
| Download method | `curl.exe -L --fail --retry 3` with an explicit `-o` destination |
| Temp directory | `TEMP`/`TMP` pointed at `D:\MKYBOOT-Lab\Temp` for the download process |

The ISO was downloaded **once**. No duplicate copy exists on `C:`.

`22.04.5` was chosen over the newer 24.04 LTS because the project documents
Ubuntu 20.04/22.04 as compatible and CI runs on `ubuntu-22.04`; keeping the
guest on the same LTS as CI avoids an unsupported variable.

## 5. VirtualBox configuration (all on D:)

| Setting | Value |
|---|---|
| Executable | `D:\Program Files\Oracle\VirtualBox\VirtualBox.exe` |
| VBoxManage | `D:\Program Files\Oracle\VirtualBox\VBoxManage.exe` (7.2.20r175154) |
| Default machine folder | `D:\MKYBOOT-Lab\VMs` |
| `MKYBOOT-Ubuntu` config | `D:\MKYBOOT-Lab\VMs\MKYBOOT-Ubuntu\MKYBOOT-Ubuntu.vbox` |
| `MKYBOOT-PXE-Client` config | `D:\MKYBOOT-Lab\VMs\MKYBOOT-PXE-Client\MKYBOOT-PXE-Client.vbox` |
| Ubuntu system disk | `D:\MKYBOOT-Lab\VMs\MKYBOOT-Ubuntu\MKYBOOT-Ubuntu-system.vdi` (35 GB dynamic) — **SATA port 0** |
| Disposable iSCSI test storage | `D:\MKYBOOT-Lab\Storage\iscsi-testpool.vdi` (25 GB dynamic) — **SATA port 1** |
| Ubuntu ISO (optical) | `D:\MKYBOOT-Lab\ISOs\ubuntu-22.04.5-live-server-amd64.iso` — **SATA port 2, dvddrive** |

The 25 GB disposable disk was **moved** from `D:\MKYBOOT-Lab\VMs\MKYBOOT-Ubuntu\`
to `D:\MKYBOOT-Lab\Storage\` with `VBoxManage modifymedium disk --move`, so the
iSCSI test storage lives under `Storage\` while the VM's own disks stay under
`VMs\`. The `.vbox` files were backed up to `D:\MKYBOOT-Lab\Backups\phase4f-20261021-102417\`
before any change.

## 6. Lab network

Phase 4D attached both VMs to VirtualBox Internal Network `mkyboot`. An
Internal Network has **no path to the Windows host**, so the Windows-native
server could neither reach the Ubuntu target over SSH nor bind DHCP/TFTP for
the client. Phase 4F re-attached both NICs to the pre-existing **Host-Only**
adapter, which required **no** change to any Windows adapter, route or
firewall rule.

| Item | Value |
|---|---|
| MKYBOOT server (DHCP/TFTP/HTTP) | `192.168.56.1` — pre-existing VirtualBox Host-Only adapter |
| Ubuntu VM — iSCSI target | `192.168.56.10` (static, to be set during install) |
| PXE client VM | `192.168.56.100`–`192.168.56.150` (DHCP range) |
| iSCSI portal | `192.168.56.10:3260` |
| Production LAN | `Ethernet` = `30.0.0.55/24` via `30.0.0.1` — **untouched** |
| VirtualBox built-in DHCP server | **Removed** from the host-only adapter so exactly one DHCP server answers |

## 7. Server-side implementation (Phase 4F code)

Continuing the in-progress Phase 4F work:

- **The SSH/LIO backend is no longer Linux-only.** `server/iscsi_linux.go`
  (`//go:build linux`) and `server/iscsi_other.go` (`//go:build !linux`) were
  replaced by the host-portable `server/iscsi_lio.go` and
  `server/iscsi_unavailable.go`. The Windows server is the platform that
  actually manages the Ubuntu target, so a Linux-only build tag excluded the
  only working code path. The Linux cross-build now succeeds.
- **The LIO target name is the IQN.** The previous code derived the targetcli
  object name with `safeTargetName(iqn)`, which would have exported
  `iqn_2026-10.com.example_storage.disk1` instead of the requested
  `iqn.2026-10.com.example:storage.disk1` — the client would never have found
  its target. The validated IQN is now used verbatim.
- **Command-injection gap closed.** The previous code passed the CHAP user
  name and the initiator IQNs into the remote command line unvalidated.
  `validateIQN` now enforces a restricted character set, CHAP user names are
  validated separately, and LUN paths are shell-quoted. A regression test
  asserts that metacharacter-bearing IQNs are rejected.
- **The CHAP secret no longer appears in process arguments.** It is written to
  the remote shell's standard input and read into a variable there.
- **Verification is real.** `VerifyConnection` now performs a loopback
  `iscsiadm --mode discovery` on the target host instead of only inspecting
  the targetcli configuration.
- **ACLs use the correct LIO model.** Each initiator gets its own ACL whose
  `auth_username` is the initiator IQN.
- **Fail-closed detection.** `detectiSCSIBackend` returns an unavailable
  backend unless `enabled` is true *and* `host`, `user` and `key_path` are
  all present and valid.
- **`log_dir` is now honoured.** The config key existed but was ignored, so
  the log always went to the data root; `Config.LogFilePath()` and
  `main.go` now use it, which is what keeps the log on `D:`.
- **Untracked DHCP/TFTP/PXE sources are now tracked.** `main.go` referenced
  `NewTFTPServer` and `NewDHCPServer`, but `dhcp.go`, `dhcp_lease.go`,
  `dhcp_server.go`, `tftp.go`, `tftp_packet.go`, `pxeconfig.go`,
  `boot_handlers.go` and `ipxe.go` had never been committed — **the committed
  tree at `0b1c7b0` did not compile**. They are included in this commit.

### Verification performed

| Check | Command | Result |
|---|---|---|
| gofmt | `gofmt -l .` | **no output** (clean) |
| go vet | `go vet -unsafeptr=false ./...` | **PASS** |
| Unit tests | `go test -timeout 180s ./...` | **PASS** (`ok … 3.6s`) |
| Windows build | `go build -trimpath -ldflags '-s -w'` | **PASS** — `dist\mkyboot-server.exe`, 7.4 MB |
| Linux cross-build | `GOOS=linux go build` | **PASS** |
| Config dry-run | `mkyboot-server.exe -config D:\MKYBOOT-Lab\config.json -dry-run` | **PASS** — `TFTP: OK bind=192.168.56.1:69`, `DHCP: DHCP dry-run OK: bind=192.168.56.1:67 subnet=192.168.56.0/24 range=192.168.56.100-192.168.56.150`, `dry-run OK` |

The Go build/module/temp caches were redirected to `D:\MKYBOOT-Lab\Temp\`
(`GOCACHE`, `GOMODCACHE`, `GOTMPDIR`, `GOPATH`) for every build, so no new
build cache was written to `C:`.

## 8. What was deliberately NOT done

- **The iSCSI capability was not enabled.** `storage.iscsi.enabled` is
  `false` in the shipped example configuration, and the `iscsi` capability
  flag in `/api/storage/health` is hard-coded `false` in the implementation.
- **No Ubuntu installation was performed.** Installing Ubuntu and running the
  LIO target requires interactive console access to the VM, which is outside
  what can be completed unattended.
- **No iSCSI target was exported, verified or initiated.** The `targetcli`
  command sequences are written against the documented LIO CLI but have not
  been executed against a live target.
- **No diskless boot was attempted or claimed.** The client VM has no local
  disk and its only boot source is network PXE; no PXE lease, TFTP transfer,
  iPXE load or OS boot has been observed.
- **No Windows system setting was changed.** No environment variable was set
  system-wide, no adapter, route or firewall rule was modified, and no
  Windows service was installed. `MKYBOOT_DATA_DIR`, `TEMP`/`TMP` and the Go
  cache variables were set **per process** only.

## 9. Files unavoidably on C:

| Path | Size | Reason |
|---|---|---|
| `C:\Program Files\Go\` | Go 1.27.1 toolchain | **Pre-existing.** Not installed by this phase. Moving a toolchain requires editing the system `PATH`, which is a system-wide change and needs operator authorization. Builds were pointed at `D:` caches instead, so the toolchain adds no new project data to `C:`. |
| `C:\Users\Administrator\AppData\Local\go-build\` | ~177 MB (pre-existing) | Go build cache from earlier work. **Not** used by this phase (redirected to `D:\MKYBOOT-Lab\Temp\gocache`). Clearing it would reclaim ~177 MB on `C:`; it was left in place because deleting a user cache is a destructive action outside this task's scope. |
| `C:\Users\Administrator\go\pkg\mod\` | ~367 MB (pre-existing) | Go module cache from earlier work. Not used by this phase (redirected to `D:\MKYBOOT-Lab\Temp\gomodcache`). |
| `C:\Users\Administrator\.local\share\kilo\` | tooling | Kilo's own working data, not project-related. |

**Nothing new was installed on `C:`.** The only project artifacts created
during this phase are on `D:`. A single 7.4 MB test binary was written to the
Windows temp directory during an early probe and was deleted.

## 10. Free space after setup

| Drive | Used | Free |
|---|---|---|
| `C:` | 109.29 GB | **7.29 GB** (7463 MB) |
| `D:` | 63.61 GB | **117.29 GB** (120105 MB) |

`C:` lost 0.02 GB during this phase (the Go module cache re-download into the
`D:` cache and the transient probe binary). `D:` gained the 2.0 GB ISO, the
lab tree and the Go caches.

## 11. Next operator actions

1. Boot `MKYBOOT-Ubuntu` from the attached ISO and install Ubuntu Server onto
   the 35 GB system disk. Set the lab NIC statically to `192.168.56.10/24`.
2. Partition/mount the 25 GB pool at `/srv/iscsi`, install `targetcli-fb` and
   `open-iscsi`, and create the key-only `mkyboot` SSH user.
3. Trust the target's host key in `known_hosts` (the server uses
   `StrictHostKeyChecking=yes`).
4. Execute the acceptance sequence in
   `docs/PHASE4F_UBUNTU_ISCSI_INTEGRATION.md` §5 — **in order, stopping at the
   first failure**.
5. Only after §5 step 9 succeeds, reconsider the `iscsi` capability flag.

## 12. Repository state

| Item | Value |
|---|---|
| Branch | `phase-4f-ubuntu-iscsi-integration` |
| Base commit | `0b1c7b0` "Phase 4E: Linux VM iSCSI target proof of concept architecture" |
| Working tree before this phase | 8 modified tracked files, 13 untracked files, **0 staged** |
| Untracked files included | 8 Go sources + 5 Go test files that the committed tree needs to compile |
| New files added | `server/iscsi_lio.go`, `server/iscsi_unavailable.go`, `server/iscsi_lio_test.go`, `docs/PHASE4F_UBUNTU_ISCSI_INTEGRATION.md` |
| Files removed | `server/iscsi_linux.go`, `server/iscsi_other.go` (superseded by the portable backends) |
| Large binaries | **None committed.** `server/dist/` and `*.exe` are git-ignored; the 2 GB ISO and all VM disks live outside the repository. |
