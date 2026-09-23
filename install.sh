#!/bin/bash
# NSBoot v4.0.0 - Installation Script
# Tested on Ubuntu 20.04/22.04 LTS
# Run as root: sudo bash install.sh

set -e

echo "=========================================="
echo " NSBoot v4.0.0 - Diskless Boot Server"
echo " Installation Script"
echo "=========================================="

if [ "$(id -u)" -ne 0 ]; then
    echo "ERROR: This script must be run as root (sudo)"
    exit 1
fi

echo ""
echo "[1/7] Installing dependencies..."
apt-get update
apt-get install -y etherwake shellinabox qemu-utils lua-json lua-socket lua-posix nginx-extras zfsutils-linux tftpd-hpa isc-dhcp-server tgt

echo ""
echo "[2/7] Creating directory structure..."
mkdir -p /srv/nsboot/modules
mkdir -p /srv/nsboot/cfg
mkdir -p /srv/nsboot/images/boot
mkdir -p /srv/nsboot/images/boot/snap
mkdir -p /srv/nsboot/images/iso
mkdir -p /srv/nsboot/images/games
mkdir -p /srv/nsboot/images/storages
mkdir -p /srv/nsboot/writeback
mkdir -p /srv/tftp
mkdir -p /var/log

echo ""
echo "[3/7] Installing server files..."
cp -f bin/nsbctl.lua /srv/nsboot/modules/nsbctl.lua
cp -f bin/client.lua /srv/nsboot/client.lua
cp -f bin/server.lua /usr/bin/nsbootd
chmod +x /usr/bin/nsbootd
chmod +x /srv/nsboot/client.lua

echo ""
echo "[4/7] Installing TFTP boot files..."
cp -f srv/tftp/ipxe.kpxe /srv/tftp/ipxe.kpxe
cp -f srv/tftp/ipxe.efi /srv/tftp/ipxe.efi
cp -f srv/tftp/bg.png /srv/tftp/bg.png

echo ""
echo "[5/7] Installing configuration..."
if [ ! -f /srv/nsboot/cfg/nsboot.json ]; then
    cp -f srv/cfg/nsboot.json /srv/nsboot/cfg/nsboot.json
    echo "  Created default config: /srv/nsboot/cfg/nsboot.json"
else
    echo "  Config already exists, skipping (backup existing first to overwrite)"
fi

echo ""
echo "[6/7] Installing nginx configuration..."
cp -f examples/etc/nginx/sites-available/default /etc/nginx/sites-available/default
ln -sf /etc/nginx/sites-available/default /etc/nginx/sites-enabled/default

echo ""
echo "[7/7] Installing init.d service..."
cp -f test/etc/init.d/nsbootd /etc/init.d/nsbootd
chmod +x /etc/init.d/nsbootd
update-rc.d nsbootd defaults 2>/dev/null || true

echo ""
echo "=========================================="
echo " Installation Complete!"
echo "=========================================="
echo ""
echo " IMPORTANT: You must configure ZFS before starting."
echo ""
echo " Example ZFS setup:"
echo "   sudo zpool create -m /srv nsboot0 <disk1> <disk2> cache <ssd>"
echo "   sudo zfs create -o mountpoint=/srv/images nsboot0/images"
echo "   sudo zfs create -o mountpoint=/srv/images/boot nsboot0/images/boot"
echo "   sudo zfs create -o mountpoint=/srv/images/boot/snap nsboot0/images/boot/snap"
echo "   sudo zfs create -o mountpoint=/srv/images/games nsboot0/images/games"
echo "   sudo zfs create -o mountpoint=/srv/images/snap nsboot0/images/snap"
echo "   sudo zfs create -o mountpoint=/srv/images/storages nsboot0/images/storages"
echo "   sudo zfs create nsboot0/writeback"
echo ""
echo " To create a zvol for game storage:"
echo "   sudo zfs create -V60G -o snapdev=visible nsboot0/images/storages/lord.qcow2"
echo ""
echo " Configure network interface:"
echo "   Edit /etc/netplan/00-installer-config.yaml"
echo "   Set static IP (e.g., 192.168.0.2/24)"
echo "   Then run: sudo netplan apply"
echo ""
echo " Configure DHCP:"
echo "   Edit /srv/nsboot/cfg/nsboot.json"
echo "   Add your workstations with MAC/IP addresses"
echo "   Run: sudo systemctl restart isc-dhcp-server"
echo ""
echo " Start services:"
echo "   sudo systemctl restart nginx"
echo "   sudo systemctl restart tftpd-hpa"
echo "   sudo systemctl restart isc-dhcp-server"
echo "   sudo systemctl restart tgt"
echo "   sudo /etc/init.d/nsbootd start"
echo ""
echo " Web interface: http://<server-ip>:8888"
echo ""
echo " For more information, see README.md"
