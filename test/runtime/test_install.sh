#!/bin/bash
# MKYBOOT Phase 3 - Installation Validation Test Script
# Run after install.sh: sudo bash test/runtime/test_install.sh

set -e

PASS=0
FAIL=0

red() { echo -e "\033[0;31mFAIL: $1\033[0m"; FAIL=$((FAIL+1)); }
green() { echo -e "\033[0;32mPASS: $1\033[0m"; PASS=$((PASS+1)); }
info() { echo -e "\033[0;36mINFO: $1\033[0m"; }

echo "=========================================="
echo " MKYBOOT Phase 3 - Install Validation"
echo "=========================================="
echo ""

echo "--- DIRECTORY STRUCTURE ---"

for dir in /srv/mkyboot /srv/mkyboot/cfg /srv/mkyboot/cfg/sessions /srv/mkyboot/images /srv/mkyboot/images/boot /srv/mkyboot/images/iso /srv/mkyboot/images/storages /srv/mkyboot/writeback /srv/tftp /var/log; do
    if [ -d "$dir" ]; then
        green "Directory exists: $dir"
    else
        red "Directory missing: $dir"
    fi
done

echo ""
echo "--- FILE INSTALLED ---"

for file in /srv/mkyboot/modules/mkyctl.lua /srv/mkyboot/client.lua /usr/bin/mkybootd /srv/tftp/ipxe.kpxe /srv/tftp/ipxe.efi; do
    if [ -f "$file" ]; then
        green "File exists: $file"
    else
        red "File missing: $file"
    fi
done

echo ""
echo "--- PERMISSIONS ---"

# Check session directory permissions
if [ -d /srv/mkyboot/cfg/sessions ]; then
    PERMS=$(stat -c "%a" /srv/mkyboot/cfg/sessions 2>/dev/null || echo "unknown")
    if [ "$PERMS" = "700" ]; then
        green "sessions/ has correct permissions (700)"
    else
        red "sessions/ has incorrect permissions ($PERMS, expected 700)"
    fi
fi

# Check mkybootd is executable
if [ -x /usr/bin/mkybootd ]; then
    green "mkybootd is executable"
else
    red "mkybootd is NOT executable"
fi

# Check client.lua is executable
if [ -x /srv/mkyboot/client.lua ]; then
    green "client.lua is executable"
else
    red "client.lua is NOT executable"
fi

echo ""
echo "--- NGINX CONFIGURATION ---"

# Test nginx config
info "Testing nginx configuration..."
if nginx -t 2>&1 | grep -q "successful"; then
    green "nginx configuration is valid"
else
    red "nginx configuration is INVALID"
fi

# Check nginx is running
if systemctl is-active --quiet nginx 2>/dev/null; then
    green "nginx is running"
else
    red "nginx is NOT running"
fi

# Check nginx listens on 8888
if ss -tlnp 2>/dev/null | grep -q ":8888"; then
    green "nginx listens on port 8888"
else
    red "nginx does NOT listen on port 8888"
fi

echo ""
echo "--- NGINX + LUA RUNTIME ---"

# Test that the Lua application loads
info "Testing Lua application..."
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" "http://127.0.0.1:8888/" 2>/dev/null || echo "000")
if [ "$HTTP_CODE" != "000" ]; then
    green "Lua application responds (HTTP $HTTP_CODE)"
else
    red "Lua application does NOT respond"
fi

# Check for Lua errors in nginx log
info "Checking nginx error log for Lua errors..."
if [ -f /var/log/nginx/error.log ]; then
    LUA_ERRORS=$(grep -c "content_by_lua\|lua coroutine\|lua_pcall\|syntax error" /var/log/nginx/error.log 2>/dev/null || echo "0")
    if [ "$LUA_ERRORS" = "0" ]; then
        green "No Lua errors in nginx log"
    else
        red "Found $LUA_ERRORS Lua errors in nginx log"
        grep "content_by_lua\|lua coroutine\|lua_pcall\|syntax error" /var/log/nginx/error.log 2>/dev/null | tail -5
    fi
else
    info "nginx error log not found (may be in different location)"
fi

echo ""
echo "--- PACKAGE VERSIONS ---"

echo -n "Ubuntu: "; lsb_release -d 2>/dev/null | cut -f2 || echo "unknown"
echo -n "Kernel: "; uname -r
echo -n "nginx: "; nginx -v 2>&1 | head -1 || echo "unknown"
echo -n "Lua: "; lua -v 2>&1 | head -1 || echo "unknown"
echo -n "qemu-img: "; qemu-img --version 2>&1 | head -1 || echo "unknown"
echo -n "tgt: "; tgtadm --version 2>&1 | head -1 || echo "unknown"
echo -n "ZFS: "; zfs --version 2>&1 | head -1 || echo "unknown"
echo -n "DHCP: "; dhcpd --version 2>&1 | head -1 || echo "unknown"

echo ""
echo "=========================================="
echo " RESULTS: $PASS passed, $FAIL failed"
echo "=========================================="

if [ $FAIL -gt 0 ]; then
    exit 1
fi
