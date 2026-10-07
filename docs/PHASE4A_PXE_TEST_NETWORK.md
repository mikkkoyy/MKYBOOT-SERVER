# Isolated PXE acceptance network

This is a **test-only topology**, not a configuration to apply to a production LAN. Choose an unused subnet and dedicated virtual switch or disconnected physical switch after checking existing routes. Example addresses below use the documentation-only `198.51.100.0/24` range; replace them with addresses reserved for your isolated lab before configuring DHCP.

```text
Ubuntu MKYBOOT server (static example IP 198.51.100.2)
  │ isolated, no uplink to production switch
  └── test-only Ethernet switch / private virtual switch
        └── PXE-capable disposable client VM or test PC
            DHCP example range 198.51.100.10–198.51.100.30
```

Do not connect a second DHCP server to a production LAN. Use disposable test images, an empty test-only ZFS pool/dataset, and a dedicated NIC/virtual bridge. Verify that the DHCP subnet, `next-server`, `srv/cfg/mkyboot.json` client IPs/MACs, TFTP root, and iSCSI portal all agree before booting. The repository's sample config uses `192.168.0.0/24`; it must not be used without checking that it does not conflict with the lab network.

| Service | Location | Traffic |
|---|---|---|
| ISC DHCP (`isc-dhcp-server`) | lab NIC only | UDP 67 server / UDP 68 client; PXE options and boot filename |
| TFTP (`tftpd-hpa`) | test server, `/srv/tftp` | UDP 69 request, negotiated UDP transfer ports |
| nginx/Lua web/iPXE script | test server | TCP 8888, `/?api=status` and the actual `?getmebootargs=<client-ip>` route |
| `tgt` iSCSI target | test server | TCP 3260; client IQN and LUN must match the generated boot script |
| iPXE boot artifacts | `/srv/tftp` | `ipxe.kpxe` (BIOS), `ipxe.efi` (64-bit UEFI) |

`ipxe32.efi` is **optional and not shipped**. The exporter offers it only if it exists in the TFTP root. The default fallback to `ipxe.efi` is **not bootable on true 32-bit UEFI**; build a 32-bit iPXE binary on the isolated Ubuntu host before attempting to accept a 32-bit client. A static-file check or DHCP syntax check is not a PXE boot test.

## Acceptance sequence on the isolated host

1. Record Ubuntu release, kernel, NIC, IP, available tools and read-only service state using `bash tools/linux/phase4a-host-check.sh`. Do not run `install.sh` against production storage.
2. Validate `bash -n install.sh`, `nginx -t`, `dhcpd -t -cf <generated test dhcpd.conf>`, boot artifacts, and all Lua tests on the disposable host.
3. With the lab NIC physically/logically isolated, configure its DHCP interface and confirm a **test client** receives a lease and the intended PXE filename and `next-server`. Capture packet/lease evidence; never start DHCP on a shared LAN.
4. Capture the TFTP request and completed transfer, iPXE version and retrieved MKYBOOT script. An iPXE shell or menu alone is **not** acceptance.
5. Use a disposable image and target. Capture iSCSI discovery, login, LUN visibility, read/write checks and a complete supported OS boot. Do not modify a master image.
6. With two distinct test clients, confirm private overlays/write-back isolation and expected reboot/persistence policy; record hashes of the unchanged base image before and after.
7. Exercise client disconnect and failed boot/target creation, then verify no stale target, NBD device, snapshot, lock, or temporary overlay remains. Use read-only inventory first.

## Cleanup

Disconnect and power off the test clients. Stop **only test services on the isolated host**, verify no iSCSI clients remain, and remove **only objects created and recorded for this disposable test**. Confirm the base image hash is unchanged. Never destroy an existing pool, image, target, dataset, production DHCP configuration, or production network route. Retain redacted lease/packet/service logs and command outputs as evidence for runtime acceptance.
