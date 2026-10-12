# MKYBOOT-SERVER — Phase 4C: Lab setup and readiness

**Status: Readiness documented. No real PXE acceptance is claimed.** VirtualBox 7.2.20 has been installed on drive `D:` and the lab folders exist, but **no VM has been created and no VM will start while AMD SVM remains disabled in firmware**. No Windows feature was enabled, no BIOS/UEFI setting was changed, no network adapter or firewall rule was altered, and no network service (DHCP/TFTP/iSCSI) was started.

| Repository | Branch | Commit |
|---|---|---|
| `mikkkoyy/MKYBOOT-SERVER` | `main` | `3b8e1201030a39de9defa445c3e51b406f72c45f` |

This document records a **read-only** inspection of the current Windows host, selects the safest practical path to a real Ubuntu PXE/diskless acceptance lab, records the verified VirtualBox installation, and lists the exact next actions required from the operator. It reuses the existing `docs/PHASE4A_PXE_TEST_NETWORK.md` for the network topology and acceptance sequence rather than replacing it. No application code was changed.

## 1. Readiness findings (this host, read-only)

| Item | Observed | Implication |
|---|---|---|
| Windows edition | **Windows 10 Home Single Language** (`CoreSingleLanguage`, build 19045) | **Hyper-V is not available** on Home; the `Microsoft-Hyper-V-All` feature is not present. Hyper-V requires Pro/Enterprise/Education. |
| CPU | AMD Ryzen 5 PRO 4650G, 6 cores / 12 threads, SLAT present | Hardware is capable of running VMs (enough cores), but… |
| Firmware virtualization | `VirtualizationFirmwareEnabled = False` (AMD SVM/VT is **off** in BIOS/UEFI) | Nested/accelerated virtualization is currently **disabled**. Enabling it requires a manual BIOS/UEFI change, which this phase does **not** perform. |
| Windows virtualization features | WSL, `HypervisorPlatform`, `VirtualMachinePlatform` — all **Disabled** | WSL2 and WSA cannot run until features are enabled. |
| WSL / Ubuntu | No distributions installed; no WSL2 kernel present | No Linux environment available today. |
| Third-party hypervisors | **VirtualBox 7.2.20r175154 installed** at `D:\Program Files\Oracle\VirtualBox`; default VM folder set to `D:\MKYBOOT-Lab\VMs` | Hypervisor is present. VMware/QEMU/Docker still not installed (not required). |
| RAM | 15.8 GB total, ~8.9 GB free | Enough for one Ubuntu server VM + one small PXE client VM if RAM is reclaimed, but tight alongside the host OS. |
| Disk (C:) | 116.6 GB total, **7.4 GB free** (verified post-install) | **Insufficient** for VM images on `C:`. |
| Disk (D:) | 180.9 GB total, **125.6 GB free** (verified post-install) | Usable for VM storage and ZFS test pool; all VM disks must live on `D:`. |
| Network adapters | Single physical `Ethernet` (Realtek PCIe GBE, `50-EB-F6-23-6B-44`), production LAN `30.0.0.55/24`, gateway `30.0.0.1` | **No private/virtual switch and no second adapter exist.** Any lab network must be created without disturbing this production NIC. |
| PXE client | No physical PXE machine, no VM | Must be provisioned. |

### Readiness matrix

| Requirement | Verdict | Note |
|---|---|---|
| Ubuntu runtime available now | BLOCKED | No Linux installed; WSL disabled; firmware VT off |
| Local accelerated virtualization possible today | BLOCKED | Firmware VT (SVM) still off in BIOS/UEFI; must be enabled manually |
| Hypervisor installed | **PASS** | VirtualBox 7.2.20r175154 on `D:` (verified) |
| Disposable storage ≥ image size | PASS (on `D:`) | 125.6 GB free on `D:`; `C:` too small (7.4 GB) |
| RAM for 2 VMs | MARGINAL | 15.8 GB total / ~8.9 GB free |
| Isolated/private network available now | BLOCKED | Single production NIC; no private switch yet (VirtualBox Internal Network will provide it) |
| Production network must stay untouched | PASS (by design) | No production NIC/firewall changed; guidance below forbids touching `30.0.0.0/24` |

**Bottom line:** VirtualBox 7.2.20 is now installed on `D:` (hypervisor ready), but the **only remaining blocker for a local virtual lab is AMD SVM/VT, which is still disabled in firmware.** The operator must enable SVM/VT in BIOS/UEFI manually; then the local virtual lab (Option 1) becomes viable. Until SVM is enabled, no VM can be created or started. An **authorized separate Ubuntu machine** (Option 2) remains an equally valid path that needs no change to this Windows host.

## 2. Lab-path selection

Evaluated in the order the phase prescribes.

### Option 1 — Local isolated virtual lab (hypervisor READY; blocked only by firmware SVM/VT)

- **Hypervisor status:** VirtualBox **7.2.20r175154** is installed at `D:\Program Files\Oracle\VirtualBox`, default machine folder is `D:\MKYBOOT-Lab\VMs`, and the lab folders (`VMs`, `ISO`, `Files`) exist under `D:\MKYBOOT-Lab\`. Windows Home lacks Hyper-V, but VirtualBox does not require it.
- **What it takes now:** the **only** remaining step is to enable AMD SVM / "Virtualization Technology" in BIOS/UEFI (manual, by the operator — see §4.2). WSL2 is not needed and not recommended for this lab (no isolated L2 switch, poor PXE-to-VM fit).
- **Advantages:** fully self-contained; no cloud cost; two VMs (server + client) fit on this CPU (6C/12T) and `D:` storage (125.6 GB free).
- **Limitations:** AMD SVM/VT is **still disabled in firmware** (verified: `VirtualizationFirmwareEnabled = False`), so **no VM can run yet**; tight RAM (15.8 GB total) — host + 2 VMs is workable but not comfortable; PXE inside one hypervisor needs an **internal** network (VirtualBox Internal Network) so the client reaches the server VM but **not** the host LAN.
- **Approx. resources:** Ubuntu server VM 2 vCPU / 4 GB / 40 GB; PXE client VM 2 vCPU / 2 GB / 4 GB; store all VM disks on `D:`.
- **Next actions (operator):** enable SVM/VT in BIOS → verify (§4.3) → create the `mkyboot` Internal Network (§4.4) → build the server and client VMs (§4.5).

### Option 2 — Separate authorized Linux machine (RECOMMENDED if local VMs are not enabled)

- **What it takes:** an authorized spare PC/VM host running **Ubuntu 20.04 or 22.04**, connected to an **isolated** physical switch together with one (ideally two) PXE-capable client machines.
- **Advantages:** no firmware/hypervisor changes on this Windows host; a real, physical, truly isolated L2 network is the most trustworthy PXE environment; `install.sh` runs natively; ZFS uses a real disposable disk.
- **Limitations:** needs hardware (a second machine + an isolated switch + client NICs that support PXE).
- **Approx. resources:** 1 Ubuntu server (2+ cores, 4+ GB RAM, ≥40 GB disposable disk for a ZFS test pool), 1–2 client PCs with PXE-capable NICs, 1 small isolated/unmanaged switch or a direct cable, no uplink to the office LAN.
- **Next actions (operator):** stand up the Ubuntu machine, isolate it on a dedicated switch, then follow `docs/PHASE4A_PXE_TEST_NETWORK.md` and `PHASE4B_REAL_PXE_DISKLESS_ACCEPTANCE_REPORT.md` for the acceptance sequence.

### Option 3 — Remote authorized lab (only if options 1–2 are unavailable)

- **Requirements:** an authorized Ubuntu 20.04/22.04 host with console/iPXE-capable access and an **isolated private network** where MKYBOOT can bind DHCP/TFTP/iSCSI without touching anyone else's network. The client must be able to PXE-boot from that host.
- **Constraints:** **no paid resources, cloud accounts, or provisioning may be created without explicit user approval.** Many cloud VMs cannot bridge an isolated L2 for PXE or cannot expose nested VT; this is often the least practical option.
- **Next actions (operator):** provide authorized access details; only then proceed. This phase does not provision anything.

**Selected option:** With VirtualBox now installed on `D:`, **Option 1 (local virtual lab) is the primary path** and requires only the manual firmware SVM/VT change (§4.2) before VMs can be built. **Option 2 (authorized separate Ubuntu machine)** remains a valid alternative that needs no change to this Windows host. **Option 3 (remote lab) is last resort** and requires explicit authorization. Do not create or start any VM until SVM/VT is confirmed enabled (§4.3).

## 3. Network isolation (mandatory)

The lab **must never** serve DHCP to a real home/office LAN. This host's production NIC is on **`30.0.0.0/24` (gateway `30.0.0.1`)** and must not be reconfigured or given a second DHCP server.

- Use a **host-only / internal virtual switch** (VirtualBox/QEMU) with **no uplink**, or a **physically isolated switch** with no cable to the production network.
- Bind MKYBOOT DHCP/TFTP/iSCSI only to the **lab NIC**. Do not touch `30.0.0.0/24`.
- Reuse the topology in `docs/PHASE4A_PXE_TEST_NETWORK.md` (example subnet `198.51.100.0/24`). Replace example addresses with addresses reserved for the lab, and make the DHCP subnet, `next-server`, `srv/cfg/mkyboot.json` client IPs/MACs, TFTP root, and iSCSI portal all agree.
- The repository sample `srv/cfg/mkyboot.json` defaults to `192.168.0.0/24` (server `192.168.0.2`, gateway `192.168.0.210`). Do **not** use those addresses without confirming they do not conflict with the lab or any real network.

```text
Ubuntu MKYBOOT server (static lab IP, e.g. 198.51.100.2)
   | ISOLATED - no uplink to the production switch
   +-- host-only/internal virtual switch OR isolated physical switch
         +-- PXE client A (disposable)
         +-- PXE client B (disposable, desired for isolation test)
             lab DHCP range e.g. 198.51.100.10-198.51.100.30
```

### 4.2 Enable AMD SVM / virtualization in BIOS/UEFI (manual, operator-only)

**Do this first. VirtualBox cannot start any 64-bit (or any) VM until SVM/VT is enabled in firmware. This phase does NOT change firmware automatically.**

1. Save all work and **restart** the PC.
2. Enter BIOS/UEFI setup. On this board ("System Product Name", ASUS-family) this is usually **`Del`** or **`F2`** pressed during POST. Other common keys: `F10` (HP), `F12`/`Esc` (Dell), `F2` (Lenovo).
3. Locate the virtualization setting. On AMD it is labeled **`SVM Mode`** (ASUS, under *Advanced → CPU Configuration*) or **`AMD-V` / `Secure Virtual Machine (SVM)`** (Award/Phoenix). Set it to **Enabled**.
4. If present, leave **IOMMU** at its default — it is not required for this lab but harmless.
5. **Save & Exit** (usually `F10`). The PC reboots into Windows normally. Do **not** change boot order, secure boot, or any other setting for this lab.

> If you cannot find the setting, consult the motherboard manual. Do not flash firmware or change unrelated options.

### 4.3 Verify virtualization readiness (read-only, after reboot)

Run in an **elevated** PowerShell. SVM is ready only when `VirtualizationFirmwareEnabled` returns **True**:

```powershell
# 1. Firmware virtualization (must be True before creating any VM)
(Get-CimInstance Win32_Processor).VirtualizationFirmwareEnabled   # -> True

# 2. VirtualBox present and reports its version
& "D:\Program Files\Oracle\VirtualBox\VBoxManage.exe" --version  # -> 7.2.20r175154

# 3. Default machine folder points to D:
& "D:\Program Files\Oracle\VirtualBox\VBoxManage.exe" list systemproperties |
    Select-String "Default machine folder"                        # -> D:\MKYBOOT-Lab\VMs

# 4. Lab folders exist
"D:\MKYBOOT-Lab","D:\MKYBOOT-Lab\VMs","D:\MKYBOOT-Lab\ISO","D:\MKYBOOT-Lab\Files" |
    ForEach-Object { "$_ : $(Test-Path $_)" }                     # -> all True

# 5. Free space on C: and D:
Get-CimInstance Win32_LogicalDisk -Filter "DriveType=3" |
    Select-Object DeviceID, @{n='FreeGB';e={[math]::Round($_.FreeSpace/1GB,1)}}
```

If step 1 is still `False`, the BIOS change did not take — re-enter firmware and confirm **SVM Mode = Enabled**. Do not proceed to build VMs.

### 4.4 Create the VirtualBox Internal Network `mkyboot` (isolated, no host uplink)

An **Internal Network** in VirtualBox has **no connection to the host's physical NIC or the production LAN** — this is what keeps lab DHCP away from `30.0.0.0/24`. Attaching a VM NIC to the internal-network name creates the network; both VMs must use the **same** name (`mkyboot`) to reach each other (see §4.5).

**Never** set the DHCP/TFTP/iSCSI server VM's lab NIC to *Bridged* or *NAT* — that would expose those services beyond the lab. Internal Network mode is host-isolated by design, so no host uplink exists to remove.

## 4. Required lab components

Minimum set (all on the **isolated** lab network only):

- **Ubuntu 20.04 or 22.04** server compatible with the project.
- MKYBOOT-SERVER source checked out at the verified commit (`05925e0`).
- **OpenResty/nginx** and the Lua runtime the project uses (`lua5.4`, `luajit`).
- **ZFS** with a **disposable, empty** test pool/dataset for images.
- **tgt** iSCSI target service (`tgtadm`).
- **ISC DHCP** server (`isc-dhcp-server`) and **TFTP** server (`tftpd-hpa`).
- Existing MKYBOOT iPXE assets in `/srv/tftp`: `ipxe.kpxe` (BIOS) and `ipxe.efi` (64-bit UEFI). `ipxe32.efi` is optional/not shipped; build it only for a true 32-bit UEFI client. `bg.png` for the iPXE menu.
- At least **one PXE-capable disposable client**; a **second** client is desirable to prove write-back isolation.
- Enough disposable storage for the test image plus write-back/cache testing (`D:` has 125.6 GB free; all VM disks live on `D:`).
- `qemu-img` for image manipulation.

Dependencies are inventoried by the project's own tool: `bash tools/linux/check-deps.sh`. Host facts (OS, kernel, NIC, IP, service state) come from `bash tools/linux/phase4a-host-check.sh`. Reuse these; do not install unrelated packages.

### 4.5 VM specifications (build only after §4.3 confirms SVM = True)

Create VMs with disks under `D:\MKYBOOT-Lab\` using the installed `VBoxManage.exe`.

**Ubuntu Server VM — the MKYBOOT server:**

| Setting | Value |
|---|---|
| Name | `mkyboot-server` |
| OS type | `Ubuntu_64` |
| CPUs / RAM | 2 vCPU / 4096 MB |
| Disk | 40 GB VDI (+ a separate vdisk for the ZFS test pool), under `D:\MKYBOOT-Lab\VMs\mkyboot-server\` |
| Network | Internal Network `mkyboot` (DHCP, TFTP, iSCSI, OpenResty bind here only) |
| Firmware | Default (BIOS) is fine for the server |

**PXE client VM — disposable, tests PXE boot:**

| Setting | Value |
|---|---|
| Name | `mkyboot-client` |
| OS type | `Ubuntu_64` (or empty — boots entirely from iSCSI) |
| CPUs / RAM | 2 vCPU / 2048 MB |
| Disk | none required (network boot); 4 GB only if you want a local fallback |
| Network | Internal Network `mkyboot`, boot order **network first** |
| Firmware | Test **BIOS** (`ipxe.kpxe`) and **UEFI** (`ipxe.efi`) as separate runs; `ipxe32.efi` only with a real 32-bit UEFI client |

Example `VBoxManage` (run from an elevated shell; adjust as needed):

```powershell
$VBM = "D:\Program Files\Oracle\VirtualBox\VBoxManage.exe"
$VMs = "D:\MKYBOOT-Lab\VMs"

# Server
& $VBM createvm --name mkyboot-server --ostype Ubuntu_64 --basefolder "$VMs" --register
& $VBM modifyvm  mkyboot-server --cpus 2 --memory 4096 --nic1 intnet --intnet1 mkyboot
& $VBM createhd  --filename "$VMs\mkyboot-server\mkyboot-server.vdi" --size 40960 --format VDI
& $VBM storagectl mkyboot-server --name SATA --add sata
& $VBM storageattach mkyboot-server --storagectl SATA --port 0 --device 0 --type hdd `
      --medium "$VMs\mkyboot-server\mkyboot-server.vdi"

# Client (network-first boot)
& $VBM createvm --name mkyboot-client --ostype Ubuntu_64 --basefolder "$VMs" --register
& $VBM modifyvm  mkyboot-client --cpus 2 --memory 2048 --nic1 intnet --intnet1 mkyboot `
      --boot1 net --boot2 none --boot3 none --boot4 none
# For UEFI testing also run: & $VBM modifyvm mkyboot-client --firmware efi
```

Install Ubuntu Server from an official ISO in `D:\MKYBOOT-Lab\ISO\` (download from `ubuntu.com`; verify its checksum). Configure the server VM's lab NIC with the static address from `docs/PHASE4A_PXE_TEST_NETWORK.md` (e.g. `198.51.100.2/24`).

### 4.6 Ubuntu installation and MKYBOOT deployment prerequisites (inside the server VM)

After Ubuntu Server is installed on the isolated `mkyboot` network:

1. Confirm the lab NIC has the intended static IP and that **only** that NIC is up (no second NIC on the production LAN).
2. Install project dependencies per `tools/linux/check-deps.sh`: OpenResty/nginx, `lua5.4`, `luajit`, ZFS (`zfs`/`zpool`), `tgt` (`tgtadm`), `isc-dhcp-server`, `tftpd-hpa`, `qemu-img`, `iscsiadm`, `tftp`.
3. Create a **disposable, empty** ZFS test pool on a dedicated vdisk (never touch an existing pool).
4. Run the repository's own `install.sh` into `/srv/mkyboot` and `/srv/tftp`; confirm installed files match the repo commit.
5. Place MKYBOOT source at the verified commit on the server VM (clone and `git checkout` that commit).
6. Only then start the MKYBOOT services **bound to the lab NIC**, and proceed to Phase 4D real PXE acceptance per `docs/PHASE4A_PXE_TEST_NETWORK.md`.

Static validation that runs **before** any VM or network service: `bash -n install.sh`, `bash tools/linux/check-pxe-assets.sh`, `nginx -t`, `dhcpd -t -cf <generated-config>`, and the Lua test suites.

### 4.7 VM storage layout (all on `D:` to protect the small `C:` drive)

```text
D:\Program Files\Oracle\VirtualBox\     (application — installed)
D:\MKYBOOT-Lab\
    VMs\                                (VirtualBox default machine folder — set)
        mkyboot-server\                 (OS disk + ZFS test vdisk)
        mkyboot-client\                 (may have no disk for pure PXE boot)
    ISO\                                (Ubuntu Server ISO, live images)
    Files\                              (logs, install logs, notes, evidence)
```

Keep the Ubuntu ISO and all virtual disks on `D:`. Do not place VM media on `C:` (only 7.4 GB free).

### 4.8 Cleanup and recovery

**Lab teardown (when finished):**

```powershell
$VBM = "D:\Program Files\Oracle\VirtualBox\VBoxManage.exe"
& $VBM controlvm mkyboot-client poweroff -ErrorAction SilentlyContinue
& $VBM controlvm mkyboot-server poweroff -ErrorAction SilentlyContinue
& $VBM unregistervm mkyboot-client --delete      # deletes its disks under D:\MKYBOOT-Lab\VMs
& $VBM unregistervm mkyboot-server --delete
Remove-Item -Recurse -Force D:\MKYBOOT-Lab\VMs\*, D:\MKYBOOT-Lab\ISO\* -ErrorAction SilentlyContinue
```

**Recovery / rollback of the VirtualBox install (only if needed, operator-approved):**

- Uninstall via *Settings → Apps → Oracle VirtualBox* (or `msiexec /x` on the installed product). This removes drivers/services cleanly.
- The `D:\MKYBOOT-Lab\` folders can be deleted once all VMs are gone.
- The MKYBOOT-SERVER repository was not modified by the install; no repo rollback is required.
- **Never** run `icacls ... /reset /t` on `D:\`, and never delete `D:\Program Files\Oracle\VirtualBox` while VMs are still registered — unregister/delete VMs first.

**Safety during teardown:** stop **only** lab services on the isolated server VM, confirm no iSCSI clients remain, and remove **only** objects created for this disposable test. Never destroy a production pool, image, target, DHCP config, or route, and never touch the host's `30.0.0.0/24` NIC.

## 5. Setup steps (run on the isolated Ubuntu host, after the operator authorizes it)

1. Confirm readiness: `bash tools/linux/check-deps.sh` and `bash tools/linux/phase4a-host-check.sh`. Do **not** run `install.sh` against production storage.
2. Static validation first: `bash -n install.sh`, `nginx -t`, `dhcpd -t -cf <generated test dhcpd.conf>`, `bash tools/linux/check-pxe-assets.sh`, and the Lua test suites.
3. Create a **disposable** ZFS test pool/dataset (never touch an existing pool).
4. Install MKYBOOT with the repository's own `install.sh` into `/srv/mkyboot` and `/srv/tftp`; confirm installed files match the commit.
5. Start **only** the MKYBOOT services, bound to the **lab NIC**: nginx/OpenResty, tgt, isc-dhcp-server, tftpd-hpa. Never on `30.0.0.0/24`.
6. Configure the lab static IP so the DHCP subnet, `next-server`, `mkyboot.json` IPs/MACs, TFTP root, and iSCSI portal agree.
7. Then proceed to the real acceptance sequence in `docs/PHASE4A_PXE_TEST_NETWORK.md` / Phase 4D.

## 6. Verification commands (safe subset)

- `bash -n install.sh` and `bash -n` for each script.
- `bash tools/linux/check-pxe-assets.sh` (BOM-free, artifacts present).
- `nginx -t`, `dhcpd -t -cf <generated-config>` (start DHCP only on the isolated NIC).
- `tgtadm --lld iscsi --op show --mode target` (only the lab target).
- Go/Lua regression as documented in the acceptance reports.

## 7. Cleanup

Follow the existing `docs/PHASE4A_PXE_TEST_NETWORK.md` cleanup: power off clients, stop **only** test services on the isolated host, confirm no iSCSI clients remain, and remove **only** objects created for this disposable test. Never destroy an existing pool, image, target, dataset, production DHCP config, or production route.

## 8. Warnings

- **Do not enable Windows features, change BIOS/UEFI, edit network adapters, or start any DHCP/TFTP/iSCSI service without explicit authorization.** This phase performed none of these.
- **Do not serve DHCP on `30.0.0.0/24` or any production LAN.** Only the isolated lab NIC may run DHCP.
- **Disposable storage only.** Use an empty test ZFS pool; never operate on a production pool or a master image.
- **No paid/cloud provisioning without explicit user approval.**

## 9. Continue into Phase 4D (real PXE acceptance)

Phase 4D is **pending** until an isolated lab exists. Once the operator has an authorized Ubuntu host on an isolated network (Option 2) **or** has enabled firmware VT and installed a hypervisor (Option 1), follow the acceptance sequence in `docs/PHASE4A_PXE_TEST_NETWORK.md`, reusing the matrix and evidence format from `PHASE4B_REAL_PXE_DISKLESS_ACCEPTANCE_REPORT.md`. This readiness phase does **not** claim PXE acceptance.

## Scope

This phase documents readiness only. It does **not** implement the PC timer, session countdown, authoritative PC lock/unlock, Windows client agent, VHDX support, or production boot changes.
