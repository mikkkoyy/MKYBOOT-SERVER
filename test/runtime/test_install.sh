#!/bin/bash
# MKYBOOT Phase 3D - Installation & Deployment Readiness Test Script
# Run after install.sh: sudo bash test/runtime/test_install.sh

set -u

PASS=0
FAIL=0

red() { echo -e "\033[0;31mFAIL: $1\033[0m"; FAIL=$((FAIL+1)); }
green() { echo -e "\033[0;32mPASS: $1\033[0m"; PASS=$((PASS+1)); }
info() { echo -e "\033[0;36mINFO: $1\033[0m"; }

echo "=========================================="
echo " MKYBOOT Phase 3D - Install Validation"
echo "=========================================="
echo ""

echo "--- DIRECTORY STRUCTURE ---"

for dir in /srv/mkyboot /srv/mkyboot/modules /srv/mkyboot/cfg /srv/mkyboot/cfg/sessions \
    /srv/mkyboot/images /srv/mkyboot/images/boot /srv/mkyboot/images/boot/snap \
    /srv/mkyboot/images/iso /srv/mkyboot/images/games /srv/mkyboot/images/storages \
    /srv/mkyboot/writeback /srv/tftp /var/log; do
    if [ -d "$dir" ]; then
        green "Directory exists: $dir"
    else
        red "Directory missing: $dir"
    fi
done

echo ""
echo "--- FILES INSTALLED ---"

for file in /srv/mkyboot/modules/mkyctl.lua /srv/mkyboot/client.lua /usr/bin/mkybootd \
    /srv/tftp/ipxe.kpxe /srv/tftp/ipxe.efi; do
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

# Check auth.json permissions
if [ -f /srv/mkyboot/cfg/auth.json ]; then
    PERMS=$(stat -c "%a" /srv/mkyboot/cfg/auth.json 2>/dev/null || echo "unknown")
    if [ "$PERMS" = "600" ] || [ "$PERMS" = "640" ]; then
        green "auth.json has restrictive permissions ($PERMS)"
    else
        red "auth.json has overly permissive permissions ($PERMS)"
    fi
fi

# Check ratelimit.json permissions
if [ -f /srv/mkyboot/cfg/ratelimit.json ]; then
    PERMS=$(stat -c "%a" /srv/mkyboot/cfg/ratelimit.json 2>/dev/null || echo "unknown")
    if [ "$PERMS" = "600" ] || [ "$PERMS" = "640" ]; then
        green "ratelimit.json has restrictive permissions ($PERMS)"
    else
        red "ratelimit.json has overly permissive permissions ($PERMS)"
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

# Check init.d script is executable
if [ -x /etc/init.d/mkybootd ]; then
    green "/etc/init.d/mkybootd is executable"
else
    red "/etc/init.d/mkybootd is NOT executable"
fi

echo ""
echo "--- SERVICE SCRIPT VERIFICATION ---"

# Check no nginx-specific references in init.d script
if [ -f /etc/init.d/mkybootd ]; then
    if grep -q 'Provides:.*nginx' /etc/init.d/mkybootd 2>/dev/null; then
        red "init.d script still has Provides: nginx"
    else
        green "init.d script has correct Provides field"
    fi
    if grep -q 'start_nginx\|upgrade_nginx\|rotate_logs\|test_config' /etc/init.d/mkybootd 2>/dev/null; then
        red "init.d script contains nginx-specific functions"
    else
        green "init.d script has no nginx-specific functions"
    fi
    if grep -q 'Provides:.*mkybootd' /etc/init.d/mkybootd 2>/dev/null; then
        green "init.d script has correct Provides: mkybootd"
    else
        red "init.d script missing Provides: mkybootd"
    fi
fi

# Check mkybootd binary exists and is a Lua script
if [ -f /usr/bin/mkybootd ]; then
    FIRST_LINE=$(head -1 /usr/bin/mkybootd 2>/dev/null)
    if echo "$FIRST_LINE" | grep -q '#!/usr/bin/lua'; then
        green "mkybootd has correct Lua shebang"
    else
        red "mkybootd missing Lua shebang: $FIRST_LINE"
    fi
fi

echo ""
echo "--- NGINX CONFIGURATION ---"

# Test nginx config
info "Testing nginx configuration..."
NGINX_CONF=$(nginx -t 2>&1)
if echo "$NGINX_CONF" | grep -q "successful\|test is successful"; then
    green "nginx configuration is valid"
else
    red "nginx configuration is INVALID"
    echo "$NGINX_CONF" | tail -5
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

# Verify content_by_lua_file points to correct path
info "Verifying content_by_lua_file path..."
if grep -q 'content_by_lua_file /srv/mkyboot/modules/mkyctl.lua' /etc/nginx/sites-available/default 2>/dev/null; then
    green "content_by_lua_file points to correct path"
else
    red "content_by_lua_file path incorrect or not found"
fi

echo ""
echo "--- NGINX + LUA RUNTIME ---"

# Test that the Lua application loads
info "Testing Lua application responds..."
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" "http://127.0.0.1:8888/" 2>/dev/null || echo "000")
if [ "$HTTP_CODE" != "000" ] && [ -n "$HTTP_CODE" ]; then
    green "Lua application responds (HTTP $HTTP_CODE)"
else
    red "Lua application does NOT respond"
fi

# Test ?status=true endpoint
info "Testing ?status=true endpoint..."
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" "http://127.0.0.1:8888/?status=true" 2>/dev/null || echo "000")
if [ "$HTTP_CODE" != "000" ] && [ -n "$HTTP_CODE" ]; then
    green "?status=true responds (HTTP $HTTP_CODE)"
else
    red "?status=true does NOT respond"
fi

# Check for Lua errors in nginx log
info "Checking nginx error log for Lua errors..."
if [ -f /var/log/nginx/error.log ]; then
    LUA_ERRORS=$(grep -c "content_by_lua\|lua coroutine\|lua_pcall\|syntax error" /var/log/nginx/error.log 2>/dev/null || echo "0")
    if [ "$LUA_ERRORS" = "0" ] || [ "$LUA_ERRORS" = "0\n" ]; then
        green "No Lua errors in nginx log"
    else
        red "Found $LUA_ERRORS Lua errors in nginx log"
        grep "content_by_lua\|lua coroutine\|lua_pcall\|syntax error" /var/log/nginx/error.log 2>/dev/null | tail -5
    fi
else
    info "nginx error log not found (may be in different location)"
fi

echo ""
echo "--- SOURCE VERIFICATION ---"

# Verify installed source matches repository source
info "Verifying mkyctl.lua matches repository..."
if [ -f /srv/mkyboot/modules/mkyctl.lua ] && [ -f bin/mkyctl.lua ]; then
    MD5_INSTALLED=$(md5sum /srv/mkyboot/modules/mkyctl.lua 2>/dev/null | awk '{print $1}')
    MD5_REPO=$(md5sum bin/mkyctl.lua 2>/dev/null | awk '{print $1}')
    if [ "$MD5_INSTALLED" = "$MD5_REPO" ]; then
        green "mkyctl.lua matches repository"
    else
        red "mkyctl.lua does NOT match repository"
    fi
else
    red "Cannot compare source files"
fi

info "Verifying server.lua matches repository..."
if [ -f /usr/bin/mkybootd ] && [ -f bin/server.lua ]; then
    MD5_INSTALLED=$(md5sum /usr/bin/mkybootd 2>/dev/null | awk '{print $1}')
    MD5_REPO=$(md5sum bin/server.lua 2>/dev/null | awk '{print $1}')
    if [ "$MD5_INSTALLED" = "$MD5_REPO" ]; then
        green "server.lua matches repository"
    else
        red "server.lua does NOT match repository"
    fi
else
    red "Cannot compare server.lua"
fi

info "Verifying client.lua matches repository..."
if [ -f /srv/mkyboot/client.lua ] && [ -f bin/client.lua ]; then
    MD5_INSTALLED=$(md5sum /srv/mkyboot/client.lua 2>/dev/null | awk '{print $1}')
    MD5_REPO=$(md5sum bin/client.lua 2>/dev/null | awk '{print $1}')
    if [ "$MD5_INSTALLED" = "$MD5_REPO" ]; then
        green "client.lua matches repository"
    else
        red "client.lua does NOT match repository"
    fi
else
    red "Cannot compare client.lua"
fi

echo ""
echo "--- DEPENDENCY CHECK ---"

info "Checking installed packages..."
for pkg in lua-json lua-socket lua-posix lua-filesystem nginx-extras zfsutils-linux tftpd-hpa isc-dhcp-server tgt qemu-utils; do
    if dpkg -l "$pkg" 2>/dev/null | grep -q "^ii"; then
        green "Package installed: $pkg"
    else
        red "Package NOT installed: $pkg"
    fi
done

echo ""
echo "--- PACKAGE VERSIONS ---"

echo -n "Ubuntu: "; lsb_release -d 2>/dev/null | cut -f2 || echo "unknown"
echo -n "Kernel: "; uname -r
echo -n "nginx: "; nginx -v 2>&1 | head -1 || echo "unknown"
echo -n "qemu-img: "; qemu-img --version 2>&1 | head -1 || echo "unknown"
echo -n "TFTP: "; dpkg -l tftpd-hpa 2>/dev/null | tail -1 || echo "unknown"
echo -n "DHCP: "; dpkg -l isc-dhcp-server 2>/dev/null | tail -1 || echo "unknown"
echo -n "ZFS: "; zfs --version 2>&1 | head -1 || echo "unknown"
echo -n "tgt: "; dpkg -l tgt 2>/dev/null | tail -1 || echo "unknown"

echo ""
echo "=========================================="
echo " RESULTS: $PASS passed, $FAIL failed"
echo "=========================================="

if [ $FAIL -gt 0 ]; then
    exit 1
fi
exit 0
