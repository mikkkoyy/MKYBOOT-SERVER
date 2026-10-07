#!/usr/bin/env bash
set -euo pipefail

repo="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
bash -n "$repo/install.sh"
while read -r source destination; do
    [ -f "$repo/$source" ] || { printf 'FAIL missing source: %s\n' "$source"; exit 1; }
    grep -Fq "cp -f $source $destination" "$repo/install.sh" || {
        printf 'FAIL install.sh does not copy %s to %s\n' "$source" "$destination"
        exit 1
    }
    printf 'PASS %s -> %s\n' "$source" "$destination"
done <<'FILES'
bin/mkyctl.lua /srv/mkyboot/modules/mkyctl.lua
src/image.lua /srv/mkyboot/modules/image.lua
src/cfg.lua /srv/mkyboot/cfg/cfg.lua
bin/client.lua /srv/mkyboot/client.lua
bin/server.lua /usr/bin/mkybootd
srv/tftp/ipxe.efi /srv/tftp/ipxe.efi
srv/tftp/ipxe.kpxe /srv/tftp/ipxe.kpxe
srv/tftp/bg.png /srv/tftp/bg.png
examples/etc/nginx/sites-available/default /etc/nginx/sites-available/default
FILES
[ -f "$repo/srv/mkyboot/mkyctl.lua" ] || { printf 'FAIL missing production mirror\n'; exit 1; }
[ -f "$repo/test/srv/mkyboot/mkyctl.lua" ] || { printf 'FAIL missing test fixture\n'; exit 1; }
cmp -s "$repo/bin/mkyctl.lua" "$repo/srv/mkyboot/mkyctl.lua" || {
    printf 'FAIL production mirror differs from canonical bin/mkyctl.lua\n'; exit 1
}
printf 'PASS production mirror matches canonical; test fixture present\n'
"${LUA_BIN:-lua5.4}" "$repo/test/runtime/test_deploy_wiring.lua" "$repo"
