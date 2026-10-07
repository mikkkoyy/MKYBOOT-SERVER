#!/usr/bin/env bash
set -u

missing_unit=0
check() {
    local group="$1" label="$2" name="$3"
    if command -v "$name" >/dev/null 2>&1; then
        printf 'PASS     %-22s %-20s %s\n' "$group" "$label" "$(command -v "$name")"
    else
        case "$group" in
            UNIT) missing_unit=$((missing_unit + 1)); printf 'MISSING  %-22s %s\n' "$group" "$label" ;;
            *) printf 'OPTIONAL %-22s %s (required for %s)\n' "$group" "$label" "$group" ;;
        esac
    fi
}

check UNIT 'Lua 5.4 tests' lua5.4
check UNIT 'LuaJIT syntax check' luajit
check UNIT 'Lua 5.4 syntax check' luac5.4
check SERVER_RUNTIME 'nginx-extras/OpenResty' nginx
check SERVER_RUNTIME 'process inspection' lsof
check SERVER_RUNTIME 'command timeout' timeout
check SERVER_RUNTIME 'QEMU image tools' qemu-img
check ISCSI 'tgt target administration' tgtadm
check ISCSI 'initiator discovery/login' iscsiadm
check ZFS 'ZFS CLI' zfs
check ZFS 'pool CLI' zpool
check PXE 'ISC DHCP configuration' dhcpd
check PXE 'TFTP daemon' in.tftpd
check PXE 'TFTP client' tftp
printf 'PRIVILEGED / MANUAL  ZFS pools, real iSCSI targets, DHCP/TFTP service, and PXE boot require an isolated host/network\n'
exit "$missing_unit"
