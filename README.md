# MKYBOOT v4.0.0

Diskless boot Windows/Linux - Free Alternative to CCBoot

## Features

- **PXE/iPXE Boot** - UEFI and BIOS support via iPXE chainloading
- **iSCSI Storage** - Linux tgt-based iSCSI target management
- **QCOW2 Images** - QEMU image format with backing file support
- **ZFS Snapshots** - Copy-on-write image isolation via ZFS
- **Master/Child Model** - Per-client overlays with safe image commit
- **Write-back Cache** - Per-client QCOW2 overlay files
- **Web Interface** - Bootstrap-based admin dashboard
- **DHCP Integration** - ISC DHCP server config generation
- **Wake-on-LAN** - Remote power on via etherwake

## Requirements

- Ubuntu 20.04/22.04 LTS (or compatible)
- ZFS-capable storage (SSD recommended for cache)
- Network interface with static IP
- Physical NICs supporting PXE boot on clients

## Quick Install

```bash
sudo bash install.sh
```

## Manual Install

### 1. Install Dependencies

```bash
apt install -y etherwake shellinabox qemu-utils lua-json lua-socket lua-posix nginx-extras zfsutils-linux tftpd-hpa isc-dhcp-server tgt
```

### 2. Setup ZFS Pool

```bash
# Create ZFS pool (adjust disks for your hardware)
sudo zpool create -m /srv mkyboot0 /dev/sdb /dev/sdc cache /dev/sdd

# Create datasets
sudo zfs create -o mountpoint=/srv/images mkyboot0/images
sudo zfs create -o mountpoint=/srv/images/boot mkyboot0/images/boot
sudo zfs create -o mountpoint=/srv/images/boot/snap mkyboot0/images/boot/snap
sudo zfs create -o mountpoint=/srv/images/games mkyboot0/images/games
sudo zfs create -o mountpoint=/srv/images/storages mkyboot0/images/storages
sudo zfs create mkyboot0/writeback
```

### 3. Configure Network

Edit `/etc/netplan/00-installer-config.yaml`:

```yaml
network:
  ethernets:
    eno1:
      addresses:
        - 192.168.0.2/24
      dhcp4: false
      gateway4: 192.168.0.210
      nameservers:
        addresses:
          - 8.8.8.8
          - 8.8.4.4
  version: 2
```

```bash
sudo netplan apply
```

### 4. Install Server Files

```bash
sudo cp bin/mkyctl.lua /srv/mkyboot/modules/mkyctl.lua
sudo cp bin/client.lua /srv/mkyboot/client.lua
sudo cp bin/server.lua /usr/bin/mkybootd
sudo chmod +x /usr/bin/mkybootd
sudo cp srv/tftp/* /srv/tftp/
```

### 5. Configure Clients

Edit `/srv/mkyboot/cfg/mkyboot.json` to add your workstations:

```json
{
  "wks": [
    {
      "enable": 1,
      "tid": 1,
      "name": "PC001",
      "mac": "b4:2e:99:2c:dd:52",
      "ipv4": "192.168.0.3",
      "group": "DEFAULT",
      "fileboot": "ipxe",
      "img": [
        {"path": "win10.qcow2", "type": "dyndisk", "boot": 1, "enable": 1, "cache": "unsafe"},
        {"path": "data.qcow2", "type": "dynblock", "boot": 0, "enable": 1, "cache": "unsafe"}
      ]
    }
  ]
}
```

### 6. Start Services

```bash
sudo systemctl restart nginx
sudo systemctl restart tftpd-hpa
sudo systemctl restart isc-dhcp-server
sudo systemctl restart tgt
sudo /etc/init.d/mkybootd start
```

### 7. Access Web Interface

Open `http://192.168.0.2:8888` in your browser.

## Architecture

```
CLIENT PC
   |
   | PXE / iPXE
   v
DISKLESS SERVER (MKYBOOT)
   |
   +-- DHCP (ISC DHCP)
   +-- TFTP (tftpd-hpa)
   +-- iPXE Scripts (nginx + Lua)
   +-- iSCSI Targets (tgt)
   +-- NBD Devices (qemu-nbd)
   +-- ZFS Snapshots
   +-- QCOW2 Images
   +-- Web Admin UI
```

## Boot Flow

1. Client PXE boots → DHCP assigns IP and iPXE binary path
2. iPXE loads via TFTP
3. iPXE contacts HTTP endpoint on server
4. Server generates iPXE script with iSCSI connection info
5. Server creates ZFS snapshot, QCOW2 child, NBD device, iSCSI target
6. iPXE connects to iSCSI target
7. Client boots Windows from network-backed disk

## Image Types

- **dyndisk** - QCOW2 disk image (backing file = master image)
- **dynblock** - ZFS zvol block device
- **iso** - ISO disc image

## Super Mode

Super mode allows committing client changes back to the master image:

1. Enable super mode on a client via web UI
2. Select which disks to commit
3. The client's writes will be merged into the master image
4. Disable super mode when done

## Client Modes

- **Normal** - Non-persistent, writes discarded on reboot
- **Super** - Persistent, can commit changes to master image

## Troubleshooting

### Client doesn't PXE boot
- Check DHCP configuration
- Verify TFTP files exist in `/srv/tftp/`
- Check network connectivity

### iSCSI connection fails
- Verify tgt is running: `systemctl status tgt`
- Check target creation: `tgtadm --lld iscsi --op show --mode target`
- Verify firewall allows port 3260

### Image not found
- Check image path in client configuration
- Verify QCOW2 files exist in `/srv/images/boot/`
- Check ZFS datasets are mounted

## License

GNU Affero General Public License v3 (AGPLv3)

## Credits

- Nuke Technology LLC
- iPXE Project (https://ipxe.org)
- QEMU Project
- ZFS on Linux
