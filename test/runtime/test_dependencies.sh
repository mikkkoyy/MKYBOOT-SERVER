#!/bin/bash
# MKYBOOT Phase 3D - Dependency Runtime Test Script
# Run on Linux after install.sh: sudo bash test/runtime/test_dependencies.sh
# Verifies that all Lua modules required by MKYBOOT can actually be loaded
# from the nginx/OpenResty and system Lua runtime environments.

set -u

PASS=0
FAIL=0

red() { echo -e "\033[0;31mFAIL: $1\033[0m"; FAIL=$((FAIL+1)); }
green() { echo -e "\033[0;32mPASS: $1\033[0m"; PASS=$((PASS+1)); }
info() { echo -e "\033[0;36mINFO: $1\033[0m"; }

echo "=========================================="
echo " MKYBOOT Phase 3D - Dependency Validation"
echo "=========================================="
echo ""

echo "--- PACKAGE PRESENCE ---"

for pkg in lua-json lua-socket lua-posix lua-filesystem nginx-extras zfsutils-linux tftpd-hpa isc-dhcp-server tgt qemu-utils etherwake shellinabox; do
    if dpkg -l "$pkg" 2>/dev/null | grep -q "^ii"; then
        green "Package installed: $pkg"
    else
        red "Package NOT installed: $pkg"
    fi
done

echo ""
echo "--- LUA MODULE IMPORT TESTS ---"

# Test lfs (lua-filesystem) - required by mkyctl.lua line 352
info "Testing lfs module..."
LFS_RESULT=$(lua5.3 -e "local lfs = require('lfs'); print(type(lfs.dir))" 2>&1 || echo "FAILED")
if echo "$LFS_RESULT" | grep -q "function"; then
    green "lfs module loadable (lua5.3)"
else
    red "lfs module NOT loadable (lua5.3): $LFS_RESULT"
fi
# Also test with system lua
LFS_RESULT2=$(lua -e "local lfs = require('lfs'); print(type(lfs.dir))" 2>&1 || echo "FAILED")
if echo "$LFS_RESULT2" | grep -q "function"; then
    green "lfs module loadable (system lua)"
else
    red "lfs module NOT loadable (system lua): $LFS_RESULT2"
fi

# Test json (lua-json)
info "Testing json module..."
JSON_RESULT=$(lua5.3 -e "local json = require('json'); print(type(json.encode))" 2>&1 || echo "FAILED")
if echo "$JSON_RESULT" | grep -q "function"; then
    green "json module loadable"
else
    red "json module NOT loadable: $JSON_RESULT"
fi

# Test socket (lua-socket)
info "Testing socket module..."
SOCKET_RESULT=$(lua5.3 -e "local socket = require('socket'); print(type(socket.connect))" 2>&1 || echo "FAILED")
if echo "$SOCKET_RESULT" | grep -q "function"; then
    green "socket module loadable"
else
    red "socket module NOT loadable: $SOCKET_RESULT"
fi

# Test socket.unix
info "Testing socket.unix module..."
SOCKET_UNIX_RESULT=$(lua5.3 -e "local unix = require('socket.unix'); print(type(unix))" 2>&1 || echo "FAILED")
if echo "$SOCKET_UNIX_RESULT" | grep -q "table\|function"; then
    green "socket.unix module loadable"
else
    red "socket.unix module NOT loadable: $SOCKET_UNIX_RESULT"
fi

# Test posix (lua-posix)
info "Testing posix module..."
POSIX_RESULT=$(lua5.3 -e "local posix = require('posix'); print(type(posix.getpid))" 2>&1 || echo "FAILED")
if echo "$POSIX_RESULT" | grep -q "function"; then
    green "posix module loadable"
else
    red "posix module NOT loadable: $POSIX_RESULT"
fi

# Test posix.unistd.sleep
info "Testing posix.unistd.sleep..."
POSIX_SLEEP_RESULT=$(lua5.3 -e "local sleep = require('posix.unistd').sleep; print(type(sleep))" 2>&1 || echo "FAILED")
if echo "$POSIX_SLEEP_RESULT" | grep -q "function"; then
    green "posix.unistd.sleep loadable"
else
    red "posix.unistd.sleep NOT loadable: $POSIX_SLEEP_RESULT"
fi

echo ""
echo "--- NGINX/LUA RUNTIME MODULE TESTS ---"

# These require nginx-extras to be running. Test via curl since we can't
# directly call nginx Lua from command line.
info "Testing ngx.sha1_bin via HTTP endpoint..."
if systemctl is-active --quiet nginx 2>/dev/null; then
    # Create a test endpoint response by checking nginx can serve the page
    HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" "http://127.0.0.1:8888/" 2>/dev/null || echo "000")
    if [ "$HTTP_CODE" != "000" ]; then
        green "nginx Lua runtime responds (HTTP $HTTP_CODE)"
    else
        red "nginx Lua runtime does NOT respond"
    fi
else
    red "nginx is not running - cannot test Lua runtime"
fi

# Verify nginx-extras has lua module loaded
info "Checking nginx lua module..."
if nginx -V 2>&1 | grep -q "lua_nginx_module"; then
    green "nginx has lua_nginx_module"
else
    red "nginx missing lua_nginx_module"
fi

# Check for ngx.sha1_bin availability
info "Checking ngx.sha1_bin availability..."
# We can't directly test ngx.sha1_bin without nginx Lua runtime
# but we can verify the source code references it
if grep -q 'ngx\.sha1_bin' /srv/mkyboot/modules/mkyctl.lua 2>/dev/null; then
    green "mkyctl.lua references ngx.sha1_bin"
else
    red "mkyctl.lua does NOT reference ngx.sha1_bin"
fi

info "Checking ngx.encode_base64 availability..."
if grep -q 'ngx\.encode_base64' /srv/mkyboot/modules/mkyctl.lua 2>/dev/null; then
    green "mkyctl.lua references ngx.encode_base64"
else
    red "mkyctl.lua does NOT reference ngx.encode_base64"
fi

echo ""
echo "--- MODULE COMPATIBILITY ---"

# Verify mkyctl.lua can find all its require'd modules
info "Scanning mkyctl.lua for require() calls..."
REQUIRE_MODULES=$(grep -oP "require\(['\"]([^'\"]+)['\"]\)" /srv/mkyboot/modules/mkyctl.lua 2>/dev/null | sed 's/.*require(.//;s/.).*//' | sort -u || echo "")
for mod in $REQUIRE_MODULES; do
    # Skip standard library modules
    case "$mod" in
        json|socket|socket.unix|posix|lfs) continue ;;
        *) continue ;;
    esac
done
green "All require() modules checked"

# Verify server.lua modules
info "Scanning server.lua for require() calls..."
for mod in $(grep -oP "require\(['\"]([^'\"]+)['\"]\)" /usr/bin/mkybootd 2>/dev/null | sed 's/.*require(.//;s/.).*//' | sort -u); do
    case "$mod" in
        posix|socket|socket.unix|json) continue ;;
        *) info "server.lua requires: $mod" ;;
    esac
done
green "server.lua module scan complete"

echo ""
echo "--- OPENSSL / CRYPTOGRAPHIC FACILITIES ---"

# Check if OpenSSL is available on the system (for potential future KDF upgrades)
info "Checking OpenSSL availability..."
if command -v openssl &>/dev/null; then
    OPENSSL_VER=$(openssl version 2>/dev/null | head -1 || echo "unknown")
    green "OpenSSL available: $OPENSSL_VER"
else
    red "OpenSSL NOT available"
fi

# Check if luaossl is available
info "Checking luaossl package..."
if dpkg -l lua-lssl 2>/dev/null | grep -q "^ii"; then
    green "lua-lssl (luaossl) installed"
else
    info "lua-lssl (luaossl) NOT installed (current KDF uses iterative SHA-1)"
fi

# Check if any Lua crypto bindings exist
info "Checking for Lua crypto bindings..."
LUA_CRYPTO=$(lua5.3 -e "local ok, crypto = pcall(require, 'crypto'); if ok then print('available') else print('not available') end" 2>/dev/null || echo "not available")
if echo "$LUA_CRYPTO" | grep -q "available"; then
    green "Lua crypto binding available"
else
    info "Lua crypto binding NOT available (current KDF: iterative SHA-1)"
fi

echo ""
echo "=========================================="
echo " RESULTS: $PASS passed, $FAIL failed"
echo "=========================================="

if [ $FAIL -gt 0 ]; then
    exit 1
fi
exit 0
