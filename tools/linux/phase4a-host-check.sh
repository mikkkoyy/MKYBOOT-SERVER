#!/usr/bin/env bash
set -u

repo="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
if [ -r /etc/os-release ]; then
    . /etc/os-release
    printf 'OS: %s\n' "${PRETTY_NAME:-unknown}"
else
    printf 'OS: /etc/os-release MISSING\n'
fi
printf 'Kernel: %s\nArchitecture: %s\n' "$(uname -r)" "$(uname -m)"
if command -v nproc >/dev/null 2>&1; then printf 'CPUs: %s\n' "$(nproc)"; fi
if command -v free >/dev/null 2>&1; then free -h; fi
if command -v lsblk >/dev/null 2>&1; then lsblk -o NAME,SIZE,TYPE,MOUNTPOINT; fi
if command -v ip >/dev/null 2>&1; then
    printf '\nNetwork interfaces:\n'
    ip -br addr
    printf '\nRoutes:\n'
    ip route
else
    printf 'MISSING: iproute2 (network inspection)\n'
fi
printf '\nDependency inventory:\n'
bash "$repo/tools/linux/check-deps.sh" || true
printf '\nService status (read-only):\n'
if command -v systemctl >/dev/null 2>&1; then
    for service in nginx tgt tftpd-hpa isc-dhcp-server mkybootd; do
        state="$(systemctl is-active "$service" 2>/dev/null || true)"
        printf '%-18s %s\n' "$service" "${state:-unknown}"
    done
else
    printf 'OPTIONAL: systemctl unavailable\n'
fi
printf '\nZFS availability (read-only):\n'
if command -v zpool >/dev/null 2>&1; then zpool status 2>&1 || true; else printf 'MISSING: zpool\n'; fi
if command -v zfs >/dev/null 2>&1; then zfs list 2>&1 || true; else printf 'MISSING: zfs\n'; fi
printf '\nPRIVILEGED / MANUAL: installation, ZFS creation, DHCP/TFTP service starts, iSCSI target and PXE boot are not performed.\n'
