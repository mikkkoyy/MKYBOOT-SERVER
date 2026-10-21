# MKYBOOT-SERVER — Phase 4D: Isolated local VirtualBox lab

**Status: Lab VMs created and network isolation verified. No PXE acceptance is claimed.**

> **Superseded in part by Phase 4F.** The Internal Network `mkyboot` described
> in §3 and §6 below has **no path to the Windows host**, so the
> Windows-native MKYBOOT server could neither reach the Ubuntu VM for iSCSI
> target management nor bind DHCP/TFTP for the client. Phase 4F therefore
> re-attached both VM NICs to the pre-existing VirtualBox **Host-Only**
> adapter (`192.168.56.1/24`), which is still isolated from the production LAN
> (`30.0.0.0/24`) and required **no** Windows adapter or route change. The
> authoritative topology, addressing and acceptance sequence are in
> `docs/PHASE4F_UBUNTU_ISCSI_INTEGRATION.md`. The Phase 4D text is retained
> as the record of what was built in Phase 4D.

AMD SVM has now been enabled in firmware (`VirtualizationFirmwareEnabled = True`, and VirtualBox reports `Processor supports HW virtualization: yes`), which unblocks the local VirtualBox path that was blocked in Phase 4C. This document records the read-only host inspection, the VirtualBox verification, the created isolated network, and the exact specifications of the two VMs, and lists the remaining operator steps. It reuses `docs/PHASE4A_PXE_TEST_NETWORK.md` (topology + acceptance sequence) and `docs/PHASE4C_LAB_SETUP_READINESS.md` (dependency checklist) rather than replacing them. **No application code was changed. No DHCP/TFTP/iSCSI service was started.**

| Repository | Branch | Commit |
|---|---|---|
| `mikkkoyy/MKYBOOT-SERVER` | `main` | `92e41732128b2755cae6edf13d30255597e496a0` |

## 1. Host prerequisites (re-verified, read-only)

| Item | Observed | Verdict |
|---|---|---|
| Firmware virtualization | `VirtualizationFirmwareEnabled = True` (rechecked); VirtualBox `Processor supports HW virtualization: yes` | **PASS** — unblocked since Phase 4C |
| CPU | AMD Ryzen 5 PRO 4650G, 6 cores / 12 threads, nested paging yes | PASS |
| RAM | 16177 MB total; **9806 MB available** to VirtualBox | PASS but tight — 4096 MB (server) + 2048 MB (client) = 6144 MB reserved; run one VM at a time if needed |
| Disk `D:` | **125.6 GB free** | PASS — both system (35 GB) and test pool (25 GB) vdisks are dynamically allocated, so real usage grows on demand |
| VirtualBox | `7.2.20r175154` at `D:\Program Files\Oracle\VirtualBox`; default machine folder `D:\MKYBOOT-Lab\VMs` | **PASS** |
| Production network | `Ethernet` = `30.0.0.55/24`, gateway `30.0.0.1` (ifIndex 14) | **PASS** — untouched (see §6) |

No Windows feature was enabled, no BIOS/UEFI setting was changed in this phase (SVM was already enabled by the operator), no firewall rule or adapter was altered, and no network service was started.

## 2. VirtualBox verification

- Executable: `D:\Program Files\Oracle\VirtualBox\VBoxManage.exe` → `--version` = `7.2.20r175154`. **PASS**
- Default machine folder: `D:\MKYBOOT-Lab\VMs` (all VM files live on `D:`). **PASS**
- `ostype` `Ubuntu_64` available. **PASS**
- No pre-existing VMs to conflict with; no existing VM was deleted or modified.

## 3. Isolated internal network

A VirtualBox **Internal Network** named `mkyboot` was created (confirmed via `VBoxManage list intnets`). Both VM adapters attach to it.

- **No host uplink, no NAT, no bridged adapter** — an Internal Network has no path to the Windows host or any physical LAN.
- A **server-only temporary NAT adapter may be attached during preparation only** to download Ubuntu packages; it **must be removed before MKYBOOT DHCP starts** so the server is reachable only on `mkyboot`. The VMs are currently created with **NIC1 = intnet only** and no NAT.
- **No DHCP server was created or enabled on the Windows host.**

## 4. VM specifications (created and verified)

All files are under `D:\MKYBOOT-Lab\VMs` on `D:`. Values below were read back with `VBoxManage showvminfo --machinereadable`.

### 4.1 MKYBOOT-Ubuntu (PXE/diskless server) — `d75851c9-4f6e-478b-a171-ff13babac3b9`

| Setting | Value |
|---|---|
| OS type | `Ubuntu_64` |
| RAM | 4096 MB |
| CPUs | 2 |
| Firmware | BIOS (`chipset=piix3`) |
| System disk | `MKYBOOT-Ubuntu-system.vdi`, **35840 MB (35 GB)**, VDI dynamic, SATA port 0 |
| Test-pool disk | `MKYBOOT-Ubuntu-testpool.vdi`, **25600 MB (25 GB)**, VDI dynamic, SATA port 1 — **the only disk the future ZFS pool may use** |
| Network | NIC1 = Internal Network `mkyboot` (`nictype=82540EM`, cable on), no NAT/bridged/host-only |
| Boot order | dvd, disk, net, none |

### 4.2 MKYBOOT-PXE-Client — `7cf94900-061c-4d4e-b102-2c8e05ec02f0`

| Setting | Value |
|---|---|
| OS type | `Ubuntu_64` |
| RAM | 2048 MB |
| CPUs | 2 |
| Firmware | BIOS (`chipset=piix3`) |
| Local disk | **none** — intentionally diskless so it cannot silently boot from local media; its only boot source is network PXE |
| Network | NIC1 = Internal Network `mkyboot` (`nictype=82540EM`, cable on); `nic2=none`, `nic3=none` (no NAT/bridged/host-only) |
| Boot order | net, none |

Both adapters use the Intel `82540EM` type, which provides VirtualBox's built-in **PXE ROM**, so the client can network-boot. A temporary 15 GB client disk was **not** added; it is optional and unnecessary here because a diskless client is the correct test for diskless boot.

## 5. Storage identification and safety

- No existing physical disk was initialized, formatted, partitioned, or written to. Nothing outside `D:\MKYBOOT-Lab\VMs` was created.
- The disposable 25 GB virtual disk is `MKYBOOT-Ubuntu-testpool.vdi` (UUID `6cdeff3c-404c-4098-bb24-0b44ce46d16b`), attached as the **second** SATA disk (port 1) of the Ubuntu VM. The 35 GB system disk is the **first** SATA disk (port 0).
- **Before creating any ZFS pool, identify the disk by size/serial inside Ubuntu** (e.g. `lsblk`, `ls -l /dev/disk/by-id`, `blkid`); the 25 GB disk will appear as the second block device (commonly `/dev/sdb`). Do **not** assume `/dev/sda`; confirm the 25 GB size and empty/blank state first. Create the ZFS pool on that disk only.

## 6. Network verification and static IP plan

- Both VM adapters use Internal Network `mkyboot`. **Confirmed** (`nic1=intnet`, `intnet1=mkyboot`).
- The PXE client has **no NAT or bridged adapter** (`nic2=none`, `nic3=none`). **Confirmed.**
- Windows production networking is unchanged: `Ethernet` remains `30.0.0.55/24` via gateway `30.0.0.1`; no routes, adapters, or firewall rules were modified. The host-only `Ethernet 2` (`192.168.56.1/24`) is a pre-existing VirtualBox adapter, is **not** attached to either VM, and is not the production LAN.
- **No DHCP/TFTP/iSCSI was started.** When services are later started, the DHCP server **must bind only to the Ubuntu VM's lab interface** (`mkyboot`), never to the production NIC.

### Static IP plan (isolated `mkyboot` network)

Based on `docs/PHASE4A_PXE_TEST_NETWORK.md`, using the RFC 5737 `198.51.100.0/24` documentation range so it can never route or conflict. Because the network is fully isolated, no gateway is required.

| Node | Address | Notes |
|---|---|---|
| Ubuntu server lab NIC (`enp0s3`) | `198.51.100.2/24` (static) | MKYBOOT server; DHCP `next-server`; iSCSI portal; web/iPXE |
| Client DHCP pool | `198.51.100.10` – `198.51.100.30` | From `isc-dhcp-server` on the server VM |
| Client MAC (server reservation) | `08:00:27:18:1C:57` | MKYBOOT-PXE-Client NIC1 |
| Server MAC (reference) | `08:00:27:95:85:B3` | MKYBOOT-Ubuntu NIC1 |

Keep the DHCP subnet, `next-server`, `srv/cfg/mkyboot.json` client IPs/MACs, TFTP root, and iSCSI portal consistent. Do **not** use the sample `192.168.0.0/24` from the repo without confirming it does not conflict.

## 7. Ubuntu Server ISO

**Status: DONE (Phase 4F).** The ISO was downloaded to `D:\MKYBOOT-Lab\ISOs\ubuntu-22.04.5-live-server-amd64.iso` and its SHA256 was verified against the official `SHA256SUMS` (`9bc6028870aef3f74f4e16b900008179e78b130e6b0b9a140635434a46aa98b0`, match confirmed). It is attached to the Ubuntu VM's optical drive and is **never committed to the repository**.

- The project documents **Ubuntu 20.04 or 22.04** as compatible, and CI runs on `ubuntu-22.04`. Use **Ubuntu Server 22.04 LTS** (64-bit).
- Official download location: `https://ubuntu.com/download/server` → **Install Ubuntu Server** → `https://releases.ubuntu.com/22.04/` (live-server ISO `ubuntu-22.04.x-live-server-amd64.iso`). Verify the SHA256 from `SHA256SUMS`.
- **Do not download until the operator authorizes it.** Once authorized, save it to `D:\MKYBOOT-Lab\ISOs\`, then attach it to the Ubuntu VM's optical drive and install onto the 35 GB system disk. This ISO is **never committed to the repository.**

## 8. Next steps (operator) — none performed automatically

1. Authorize the Ubuntu Server 22.04 LTS ISO download (§7); save to `D:\MKYBOOT-Lab\ISOs\`.
2. Attach the ISO, boot `MKYBOOT-Ubuntu`, and install Ubuntu onto the 35 GB system disk. During install, only the lab NIC needs the static `198.51.100.2/24`.
3. Inside the VM run the read-only checks first: `bash tools/linux/phase4a-host-check.sh`, then static validation (`bash -n install.sh`, `bash tools/linux/check-pxe-assets.sh`, `nginx -t`, `dhcpd -t -cf <generated test dhcpd.conf>`, Lua tests).
4. Identify the 25 GB disk (§5) and create a **disposable, empty** ZFS test pool on it only.
5. Install dependencies per `tools/linux/check-deps.sh` (OpenResty/nginx, `lua5.4`, `luajit`, ZFS, `tgt`, `isc-dhcp-server`, `tftpd-hpa`, `qemu-img`, `iscsiadm`), install MKYBOOT via its own `install.sh`, and only then start services **bound to the lab NIC**.
6. Remove any temporary server NAT adapter before DHCP starts.
7. Proceed to the real acceptance sequence in `docs/PHASE4A_PXE_TEST_NETWORK.md` (DHCP lease → TFTP → iPXE → iSCSI → diskless boot → write-back isolation). **A VM that powers on is not PXE acceptance.**

## 9. Cleanup and warnings

- Teardown: power off VMs, delete VMs via VirtualBox if the lab is retired, then the `D:\MKYBOOT-Lab\` folders can be removed. Never delete `D:\Program Files\Oracle\VirtualBox` while VMs are registered.
- **Never** run DHCP on `30.0.0.0/24` or any production LAN. Bind services only to the lab NIC.
- **Disposable storage only** for the ZFS pool; never touch an existing pool or a master image.
- Do not enable Windows features, edit network adapters, or start any network service without explicit authorization.

## 10. Status summary

| Acceptance item | Status | Note |
|---|---|---|
| SVM verification | **PASS** | `True`; VirtualBox HW virtualization `yes` |
| VirtualBox verification | **PASS** | `7.2.20r175154`, default folder on `D:` |
| VM creation | **PASS** | Both VMs created on `D:`, no duplicates |
| Internal network | **PASS** | `mkyboot`, no uplink/NAT/bridge |
| Ubuntu ISO | **PASS (Phase 4F)** | `ubuntu-22.04.5-live-server-amd64.iso` in `D:\MKYBOOT-Lab\ISOs\`, SHA256 verified, attached to the Ubuntu VM optical drive |
| Storage identification | **PASS (lab-level)** | 25 GB disk created as SATA port 1; in-guest identity still to confirm before any pool |
| Client isolation | **PASS** | Diskless client, intnet only, no NAT/bridge |
| Production network safety | **PASS** | `30.0.0.55/24` untouched; host-only not attached |
| PXE / diskless acceptance | **NOT RUN** | Requires later real DHCP, TFTP, iPXE, iSCSI, boot, write-back tests |

## Scope

This phase creates and verifies the isolated lab topology only. It does **not** start DHCP/TFTP/iSCSI, does **not** install Ubuntu or MKYBOOT services, and does **not** implement the PC timer, session countdown, authoritative PC lock/unlock, Windows client agent, VHDX support, or any unrelated production change.
