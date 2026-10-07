#!/usr/bin/env bash
set -euo pipefail

repo="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
config="$repo/examples/etc/dhcp/dhcpd.conf"
root="$repo/srv/tftp"
if cmp -s <(head -c 3 "$config") <(printf '\357\273\277'); then
    printf 'FAIL raw DHCP example contains UTF-8 BOM\n'
    exit 1
fi
printf 'PASS raw DHCP example has no UTF-8 BOM\n'
for name in ipxe.kpxe ipxe.efi bg.png; do
    [ -s "$root/$name" ] || { printf 'FAIL missing TFTP asset: %s\n' "$name"; exit 1; }
    printf 'PASS TFTP asset: %s\n' "$name"
done
count=0
while read -r filename; do
    [ -n "$filename" ] || continue
    count=$((count + 1))
    [ -s "$root/$filename" ] || {
        printf 'FAIL DHCP filename %s is not shipped in srv/tftp\n' "$filename"
        exit 1
    }
    printf 'PASS DHCP filename: %s\n' "$filename"
done < <(awk '$1 == "filename" {gsub(/[";]/, "", $2); print $2}' "$config")
[ "$count" -gt 0 ] || { printf 'FAIL no DHCP filenames found\n'; exit 1; }
grep -Fq 'bin-i386-efi/ipxe.efi' "$config" || {
    printf 'FAIL missing documented optional 32-bit UEFI boot path\n'; exit 1
}
grep -Fq 'tftp://${next-server}/bg.png' "$repo/src/cfg.lua" || {
    printf 'FAIL iPXE splash references missing or changed boot asset\n'; exit 1
}
printf 'OPTIONAL ipxe32.efi: not shipped by default; the generator test checks both installed and absent cases\n'
printf 'PRIVILEGED / MANUAL: this is a static asset check, not a DHCP/TFTP/PXE transfer\n'
