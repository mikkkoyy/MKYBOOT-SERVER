# MKYBOOT-SERVER — Phase 4C: Lab setup and readiness

**Status: Readiness documented. No real PXE acceptance is claimed. No lab was created, no Windows feature was enabled, no BIOS/UEFI setting was changed, no VM was built, and no network service was started.**

| Repository | Branch | Commit |
|---|---|---|
| `mikkkoyy/MKYBOOT-SERVER` | `main` | `05925e0301625e98bcac9d0756344190b0bd0b6a` |

This document records a **read-only** inspection of the current Windows host, selects the safest practical path to a real Ubuntu PXE/diskless acceptance lab, and lists the exact next actions required from the operator. It reuses the existing `docs/PHASE4A_PXE_TEST_NETWORK.md` for the network topology and acceptance sequence rather than replacing it. No application code was changed.

## 1. Readiness findings (this host, read-only)

| Item | Observed | Implication |
|---|---|---|
| Windows edition | **Windows 10 Home Single Language** (`CoreSingleLanguage`, build 19045) | **Hyper-V is not available** on Home; the `Microsoft-Hyper-V-All` feature is not present. Hyper-V requires Pro/Enterprise/Education. |
| CPU | AMD Ryzen 5 PRO 4650G, 6 cores / 12 threads, SLAT present | Hardware is capable of running VMs (enough cores), but… |
| Firmware virtualization | `VirtualizationFirmwareEnabled = False` (AMD SVM/VT is **off** in BIOS/UEFI) | Nested/accelerated virtualization is currently **disabled**. Enabling it requires a manual BIOS/UEFI change, which this phase does **not** perform. |
| Windows virtualization features | WSL, `HypervisorPlatform`, `VirtualMachinePlatform` — all **Disabled** | WSL2 and WSA cannot run until features are enabled. |
| WSL / Ubuntu | No distributions installed; no WSL2 kernel present | No Linux environment available today. |
| Third-party hypervisors | VirtualBox, VMware, QEMU, Docker — **none installed** | A hypervisor would have to be installed. |
| RAM | 15.8 GB total, ~8.9 GB free | Enough for one Ubuntu server VM + one small PXE client VM if RAM is reclaimed, but tight alongside the host OS. |
| Disk (C:) | 116.6 GB total, **only 7.6 GB free** | **Insufficient** for VM images on `C:`. |
| Disk (D:) | 180.9 GB total, **126.1 GB free** | Usable for VM storage and ZFS test pool if a VM is created on `D:`. |
| Network adapters | Single physical `Ethernet` (Realtek PCIe GBE, `50-EB-F6-23-6B-44`), production LAN `30.0.0.55/24`, gateway `30.0.0.1` | **No private/virtual switch and no second adapter exist.** Any lab network must be created without disturbing this production NIC. |
| PXE client | No physical PXE machine, no VM | Must be provisioned. |

### Readiness matrix

| Requirement | Verdict | Note |
|---|---|---|
| Ubuntu runtime available now | BLOCKED | No Linux installed; WSL disabled; firmware VT off |
| Local accelerated virtualization possible today | BLOCKED | Firmware VT off **and** Windows Home lacks Hyper-V |
| Hypervisor installed | BLOCKED | None present |
| Disposable storage ≥ image size | PASS (on `D:`) | 126.1 GB free on `D:`; `C:` too small (7.6 GB) |
| RAM for 2 VMs | MARGINAL | 15.8 GB total / ~8.9 GB free |
| Isolated/private network available now | BLOCKED | Single production NIC; no private switch |
| Production network must stay untouched | PASS (by design) | This phase changes nothing; guidance below forbids touching `30.0.0.0/24` |

**Bottom line:** a **local** virtual lab is **not ready today**. It is *possible* only after operator-approved, manual changes: enable AMD SVM/VT in BIOS/UEFI, and either (a) install a third-party hypervisor (VirtualBox/VMware Workstation Player/QEMU) — which does **not** require upgrading Windows — or (b) upgrade to Windows Pro for Hyper-V. Until then, the recommended path is an **authorized separate Ubuntu machine** (or authorized remote lab).

## 2. Lab-path selection

Evaluated in the order the phase prescribes.

### Option 1 — Local isolated virtual lab (NOT ready today; possible after manual, operator-approved changes)

- **What it takes:** (a) enable AMD SVM / "Virtualization Technology" in BIOS/UEFI; (b) install a hypervisor. Because this is **Windows Home**, Hyper-V is unavailable, so use **VirtualBox**, **VMware Workstation Player**, or **QEMU/KVM**. WSL2 also requires firmware VT plus the `VirtualMachinePlatform` feature, but WSL2 is a poor fit here — a WSL2 VM cannot easily serve PXE to another VM and has no isolated L2 switch, so a full hypervisor is preferred.
- **Advantages:** fully self-contained; no cloud cost; two VMs (server + client) fit on this CPU (6C/12T) and `D:` storage (126 GB free).
- **Limitations:** requires a manual BIOS/UEFI change (cannot be done safely in software); tight RAM (15.8 GB total) — host + 2 VMs is workable but not comfortable; PXE inside one hypervisor needs an **internal/host-only** network so the client reaches the server VM but **not** the host LAN.
- **Approx. resources:** Ubuntu server VM 2 vCPU / 4 GB / 40 GB; PXE client VM 2 vCPU / 2 GB / 4 GB; store VM disks on `D:`.
- **Next actions (operator):** reboot → BIOS → enable SVM/VT → install VirtualBox (or QEMU) → create an **Internal Network** named `mkyboot` (no host uplink) → attach both VMs to it.

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

**Selected option: Option 2 (authorized separate Ubuntu machine on an isolated switch)** as the primary recommendation, because it needs no risky change to this Windows Home host and yields the most trustworthy real PXE evidence. **Option 1 (local VMs) is the fallback** if the operator enables firmware VT and installs a hypervisor. **Option 3 is last resort** and requires explicit authorization.

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
- Enough disposable storage for the test image plus write-back/cache testing (`D:` has 126 GB free if a local VM is used).
- `qemu-img` for image manipulation.

Dependencies are inventoried by the project's own tool: `bash tools/linux/check-deps.sh`. Host facts (OS, kernel, NIC, IP, service state) come from `bash tools/linux/phase4a-host-check.sh`. Reuse these; do not install unrelated packages.

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
