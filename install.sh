#!/bin/bash
# MKYBOOT v4.0.0 - Installation Script
# Tested on Ubuntu 20.04/22.04 LTS
# Run as root: sudo bash install.sh

set -e

echo "=========================================="
echo " MKYBOOT v4.0.0 - Diskless Boot Server"
echo " Installation Script"
echo "=========================================="

if [ "$(id -u)" -ne 0 ]; then
    echo "ERROR: This script must be run as root (sudo)"
    exit 1
fi

echo ""
echo "[1/7] Installing dependencies..."
apt-get update
apt-get install -y etherwake shellinabox qemu-utils lua-json lua-socket lua-posix lua-filesystem nginx-extras zfsutils-linux tftpd-hpa isc-dhcp-server tgt

echo ""
echo "[2/7] Creating directory structure..."
mkdir -p /srv/mkyboot/modules
mkdir -p /srv/mkyboot/cfg
mkdir -p /srv/mkyboot/cfg/sessions
chmod 700 /srv/mkyboot/cfg/sessions
mkdir -p /srv/mkyboot/images/boot
mkdir -p /srv/mkyboot/images/boot/snap
mkdir -p /srv/mkyboot/images/iso
mkdir -p /srv/mkyboot/images/games
mkdir -p /srv/mkyboot/images/storages
mkdir -p /srv/mkyboot/writeback
mkdir -p /srv/tftp
mkdir -p /var/log

echo ""
echo "[3/7] Installing server files..."
cp -f bin/mkyctl.lua /srv/mkyboot/modules/mkyctl.lua
cp -f bin/client.lua /srv/mkyboot/client.lua
cp -f bin/server.lua /usr/bin/mkybootd
chmod +x /usr/bin/mkybootd
chmod +x /srv/mkyboot/client.lua

echo ""
echo "[4/7] Installing TFTP boot files..."
cp -f srv/tftp/ipxe.kpxe /srv/tftp/ipxe.kpxe
cp -f srv/tftp/ipxe.efi /srv/tftp/ipxe.efi
cp -f srv/tftp/bg.png /srv/tftp/bg.png

echo ""
echo "[5/7] Installing configuration..."
if [ ! -f /srv/mkyboot/cfg/mkyboot.json ]; then
    cp -f srv/cfg/mkyboot.json /srv/mkyboot/cfg/mkyboot.json
    echo "  Created default config: /srv/mkyboot/cfg/mkyboot.json"
else
    echo "  Config already exists, skipping (backup existing first to overwrite)"
fi

echo ""
echo "[6/7] Installing nginx configuration..."
cp -f examples/etc/nginx/sites-available/default /etc/nginx/sites-available/default
ln -sf /etc/nginx/sites-available/default /etc/nginx/sites-enabled/default

echo ""
echo "[7/7] Installing init.d service..."
cp -f test/etc/init.d/mkybootd /etc/init.d/mkybootd
chmod +x /etc/init.d/mkybootd
update-rc.d mkybootd defaults 2>/dev/null || true

echo ""
echo "=========================================="
echo " Installation Complete!"
echo "=========================================="
echo ""
echo " IMPORTANT: You must configure ZFS before starting."
echo ""
echo " Example ZFS setup:"
echo "   sudo zpool create -m /srv mkyboot0 <disk1> <disk2> cache <ssd>"
echo "   sudo zfs create -o mountpoint=/srv/images mkyboot0/images"
echo "   sudo zfs create -o mountpoint=/srv/images/boot mkyboot0/images/boot"
echo "   sudo zfs create -o mountpoint=/srv/images/boot/snap mkyboot0/images/boot/snap"
echo "   sudo zfs create -o mountpoint=/srv/images/games mkyboot0/images/games"
echo "   sudo zfs create -o mountpoint=/srv/images/snap mkyboot0/images/snap"
echo "   sudo zfs create -o mountpoint=/srv/images/storages mkyboot0/images/storages"
echo "   sudo zfs create mkyboot0/writeback"
echo ""
echo " To create a zvol for game storage:"
echo "   sudo zfs create -V60G -o snapdev=visible mkyboot0/images/storages/lord.qcow2"
echo ""
echo " Configure network interface:"
echo "   Edit /etc/netplan/00-installer-config.yaml"
echo "   Set static IP (e.g., 192.168.0.2/24)"
echo "   Then run: sudo netplan apply"
echo ""
echo " Configure DHCP:"
echo "   Edit /srv/mkyboot/cfg/mkyboot.json"
echo "   Add your workstations with MAC/IP addresses"
echo "   Run: sudo systemctl restart isc-dhcp-server"
echo ""
echo " Start services:"
echo "   sudo systemctl restart nginx"
echo "   sudo systemctl restart tftpd-hpa"
echo "   sudo systemctl restart isc-dhcp-server"
echo "   sudo systemctl restart tgt"
echo "   sudo /etc/init.d/mkybootd start"
echo ""
echo " Web interface: http://<server-ip>:8888"
echo ""
echo " For more information, see README.md"
