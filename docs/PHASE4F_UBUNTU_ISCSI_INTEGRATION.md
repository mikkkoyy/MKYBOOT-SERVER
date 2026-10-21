# Phase 4F — Ubuntu iSCSI target integration

This document describes the Phase 4F lab: how the Windows-native MKYBOOT
server is wired to a **separate Ubuntu VM that runs the LIO/targetcli iSCSI
target**, and the exact acceptance sequence that must be executed before the
iSCSI capability may be considered working.

> **Status.** The lab topology, the storage layout, the configuration and the
> server-side implementation are in place and unit-tested. The iSCSI layer has
> **not** yet been exercised against a live Ubuntu target, and **no diskless
> boot has been performed**. Nothing in this document may be read as a claim
> that a client has booted from iSCSI.

## 1. Topology

The lab uses the VirtualBox **host-only** network `192.168.56.0/24`. The
Windows host already owns `192.168.56.1` on the "VirtualBox Host-Only Ethernet
Adapter", so no Windows-wide network change is required and the lab has no
uplink to the office LAN (`30.0.0.0/24`).

```text
Windows host (MKYBOOT server)            192.168.56.1
  VirtualBox Host-Only Ethernet Adapter
  ├── MKYBOOT-Ubuntu      (iSCSI target)  192.168.56.10   static
  └── MKYBOOT-PXE-Client  (PXE client)    192.168.56.100+ DHCP lease
```

VirtualBox's own built-in DHCP server on the host-only adapter was **removed**
so that exactly one DHCP server (`mkyboot-server`) answers on this subnet.

| Role | Address | Notes |
|---|---|---|
| MKYBOOT server (DHCP/TFTP/HTTP) | `192.168.56.1` | host-only adapter, pre-existing |
| Ubuntu VM — iSCSI target | `192.168.56.10` | static; SSH + iSCSI portal |
| PXE client VM | `192.168.56.100`–`192.168.56.150` | DHCP lease range |
| iSCSI portal | `192.168.56.10:3260` | TCP |
| SSH (management) | `192.168.56.10:22` | key-only, `BatchMode` |

`docs/PHASE4A_PXE_TEST_NETWORK.md` uses `198.51.100.0/24` as a
documentation-only placeholder. Phase 4F replaces it with the real host-only
subnet above; the PXE acceptance sequence in that document still applies.

## 2. Storage layout (all on D:)

| Path | Contents |
|---|---|
| `D:\MKYBOOT-Lab\ISOs\` | `ubuntu-22.04.5-live-server-amd64.iso` + `SHA256SUMS` |
| `D:\MKYBOOT-Lab\VMs\` | VM configs, `MKYBOOT-Ubuntu-system.vdi` (35 GB) |
| `D:\MKYBOOT-Lab\Storage\` | `iscsi-testpool.vdi` (25 GB) + `images\` (server image root) |
| `D:\MKYBOOT-Lab\Logs\` | `mkyboot-server.log` (via `log_dir`) |
| `D:\MKYBOOT-Lab\Backups\` | VM `.vbox` backups taken before changes |
| `D:\MKYBOOT-Lab\Temp\` | Go build/module cache, download temp files |
| `D:\MKYBOOT-Lab\config.json` | lab server configuration |

The disposable iSCSI test pool is a **separate VDI attached to the Ubuntu VM**
(so the LUN files live inside the guest) while the VDI file itself sits in
`D:\MKYBOOT-Lab\Storage\`. The Ubuntu VM's system disk stays in
`D:\MKYBOOT-Lab\VMs\`.

## 3. Server configuration

`server/config.example.json` is the lab configuration. The relevant part:

```json
"storage": {
  "enabled": true,
  "root": "D:/MKYBOOT-Lab/Storage/images",
  "iscsi": {
    "enabled": false,
    "host": "192.168.56.10",
    "port": 22,
    "user": "mkyboot",
    "key_path": "D:/MKYBOOT-Lab/VMs/MKYBOOT-Ubuntu/keys/mkyboot_ed25519"
  }
}
```

`iscsi.enabled` is **false** in the shipped example. It must not be flipped
until §5 has been executed successfully.

Validate the configuration without binding any socket:

```powershell
$env:MKYBOOT_DATA_DIR = 'D:\MKYBOOT-Lab\Data'
.\dist\mkyboot-server.exe -config D:\MKYBOOT-Lab\config.json -dry-run
```

Expected output:

```text
  TFTP:  OK bind=192.168.56.1:69 root=...
  DHCP:  DHCP dry-run OK: bind=192.168.56.1:67 subnet=192.168.56.0/24 ...
dry-run OK
```

## 4. Ubuntu target preparation

On the Ubuntu VM (installed from the verified ISO in `D:\MKYBOOT-Lab\ISOs`):

```bash
sudo apt-get update
sudo apt-get install -y targetcli-fb open-iscsi
sudo systemctl enable --now rtslib-fb-targetctl
sudo modprobe target_core_mod
```

Partition and mount the disposable pool that arrives as
`/dev/sdb` (25 GB VDI):

```bash
sudo mkfs.ext4 -L mkyboot-iscsi /dev/sdb
sudo mkdir -p /srv/iscsi
sudo mount /dev/sdb /srv/iscsi
```

Give the VM the static address `192.168.56.10/24` on its host-only NIC and
create an unprivileged management user with key-only SSH access:

```bash
sudo useradd -m -s /bin/bash mkyboot
sudo install -d -m 700 -o mkyboot -g mkyboot /home/mkyboot/.ssh
# install the public key, then:
sudo systemctl restart ssh
```

On the **Windows** side, trust the host key once (the server uses
`StrictHostKeyChecking=yes`, so an untrusted key fails closed):

```powershell
ssh-keyscan -t ed25519 192.168.56.10 | Out-File -Append $env:USERPROFILE\.ssh\known_hosts
```

## 5. Acceptance sequence

Run in this order. **Stop at the first failure** and record the output.

1. **Target reachable.** `ssh mkyboot@192.168.56.10 'targetcli /iscsi ls'` returns
   without error.
2. **Enable iSCSI management** in `D:\MKYBOOT-Lab\config.json`
   (`"iscsi": {"enabled": true, ...}`) and restart the server.
3. **Backend detected.** `GET /api/storage/iscsi/health` reports
   `"backend": "linux-lio"`, `"available": true`, `"state": "configured"`.
4. **Create a LUN** in `/srv/iscsi`:
   `POST /api/storage/iscsi/luns {"path":"/srv/iscsi/lun1.img","size_mb":1024}`.
   Confirm the file exists on the Ubuntu VM and that its size is correct.
5. **Export the target**:
   `POST /api/storage/iscsi/export {"iqn":"iqn.2026-10.com.mkyboot:disk1","path":"/srv/iscsi/lun1.img","size_mb":1024}`.
   Confirm `targetcli /iscsi ls` lists **exactly that IQN** — the LIO target
   name is the IQN.
6. **Restrict access** (optional but required before CHAP):
   `POST /api/storage/iscsi/acl {"iqn":"iqn.2026-10.com.mkyboot:disk1","initiators":["iqn.2026-10.com.client:pxe1"]}`.
7. **Verify.** `POST /api/storage/iscsi/verify {"iqn":"iqn.2026-10.com.mkyboot:disk1"}`
   performs a loopback `iscsiadm` discovery on the target host. Only when this
   returns 200 does the state become `verified`.
8. **Initiation from the client.** Attach an iSCSI target in the PXE client
   VM or with `iscsiadm` on the Windows host and confirm the LUN is visible
   and writable.
9. **Cleanup.** Unexport and delete the LUN, and confirm no stale target
   remains on the Ubuntu VM.

Only after step 9 succeeds may the `iscsi` capability flag be reconsidered.
Until then `/api/storage/health` must keep reporting `"iscsi": false`.

## 6. Security properties

These are enforced by the implementation, not by operator discipline:

- **Fail closed.** Without `enabled` + `host` + `user` + `key_path`, the
  backend reports itself unavailable and every management call fails.
- **No interpolation of untrusted input.** IQNs are matched against
  `iqn.<yyyy-mm>.<naming-authority>` with a restricted character set, LUN
  paths must be absolute and free of traversal and shell metacharacters, and
  CHAP user names are restricted. A shell-metacharacter IQN (for example
  `iqn.2026-10.com.x:disk1; rm -rf /`) is rejected before it can reach a
  command line.
- **Secret hygiene.** The CHAP secret travels on the remote shell's standard
  input, so it never appears in the remote process arguments, and it is never
  logged.
- **SSH hardening.** `BatchMode=yes`, `PasswordAuthentication=no`,
  `ForwardAgent=no`, `ForwardX11=no`, `StrictHostKeyChecking=yes`, bounded
  `ConnectTimeout`.
- **Capability flag stays false.** The `iscsi` capability is hard-coded
  `false` in the storage capabilities. It is not derived from configuration.

## 7. Known gaps

- **QEMU cannot use VHDX differencing disks.** The diskless overlay design
  relies on differencing images; QEMU's block layer does not support the
  VHDX differencing type. VirtualBox is used for the lab instead.
- **The Windows client agent is not implemented.** The PXE client path is
  exercised through VirtualBox's PXE stack and iPXE, not a native Windows
  agent.
- **No live-target test has run yet.** The `targetcli` command sequences in
  `server/iscsi_lio.go` are written against the documented LIO CLI, but they
  have not been executed against a running Ubuntu target. Treat §5 as the
  script that validates them, and expect to adjust details (for example
  `targetcli` object naming) if the real target disagrees.
