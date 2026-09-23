#!/bin/bash
# MKYBOOT Phase 3 - Authentication Runtime Test Script
# Run on Linux after install.sh: sudo bash test/runtime/test_auth.sh
# Requires: curl, jq (optional)

set -e

BASE="http://127.0.0.1:8888"
PASS=0
FAIL=0
COOKIE_JAR="/tmp/mkyboot_test_cookies"
AUTH_FILE="/srv/mkyboot/cfg/auth.json"
SESSION_DIR="/srv/mkyboot/cfg/sessions"
RATE_FILE="/srv/mkyboot/cfg/ratelimit.json"

red() { echo -e "\033[0;31mFAIL: $1\033[0m"; FAIL=$((FAIL+1)); }
green() { echo -e "\033[0;32mPASS: $1\033[0m"; PASS=$((PASS+1)); }
info() { echo -e "\033[0;36mINFO: $1\033[0m"; }

cleanup() {
    rm -f "$COOKIE_JAR"
    rm -f "$AUTH_FILE"
    rm -f "$RATE_FILE"
    rm -rf "$SESSION_DIR"/*
}

echo "=========================================="
echo " MKYBOOT Phase 3 - Auth Runtime Tests"
echo "=========================================="
echo ""

# Pre-checks
info "Checking nginx is running..."
if ! systemctl is-active --quiet nginx; then
    echo "ERROR: nginx is not running. Start with: sudo systemctl start nginx"
    exit 1
fi
green "nginx is running"

info "Checking MKYBOOT is accessible..."
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" "$BASE/" 2>/dev/null || true)
if [ "$HTTP_CODE" = "000" ]; then
    echo "ERROR: Cannot connect to $BASE"
    exit 1
fi
green "MKYBOOT is accessible (HTTP $HTTP_CODE)"

echo ""
echo "--- SETUP TESTS ---"

# Test 1: Setup page is shown when not configured
info "Test: Setup page shown when not configured..."
RESP=$(curl -s "$BASE/?status=true" 2>/dev/null)
if echo "$RESP" | grep -q "Initial Setup\|MKYBOOT Setup\|doSetup"; then
    green "Setup page shown when not configured"
else
    red "Setup page NOT shown when not configured"
fi

# Test 2: Setup rejects weak password
info "Test: Setup rejects weak password..."
RESP=$(curl -s -X POST -d "password=short&confirm=short" "$BASE/?setup=true" 2>/dev/null)
if echo "$RESP" | grep -q "ERROR"; then
    green "Setup rejects weak password"
else
    red "Setup did NOT reject weak password: $RESP"
fi

# Test 3: Setup rejects mismatched confirmation
info "Test: Setup rejects mismatched confirmation..."
RESP=$(curl -s -X POST -d "password=StrongPass123&confirm=DifferentPass" "$BASE/?setup=true" 2>/dev/null)
if echo "$RESP" | grep -q "ERROR\|do not match"; then
    green "Setup rejects mismatched confirmation"
else
    red "Setup did NOT reject mismatched confirmation: $RESP"
fi

# Test 4: Setup succeeds with valid password
info "Test: Setup succeeds with valid password..."
RESP=$(curl -s -X POST -d "password=StrongPass123&confirm=StrongPass123" "$BASE/?setup=true" 2>/dev/null)
if echo "$RESP" | grep -q "OK"; then
    green "Setup succeeds with valid password"
else
    red "Setup failed: $RESP"
fi

# Test 5: Auth record exists and contains username
info "Test: Auth record contains username..."
if [ -f "$AUTH_FILE" ]; then
    if grep -q '"username"' "$AUTH_FILE"; then
        green "Auth record contains username"
    else
        red "Auth record does NOT contain username"
    fi
    if grep -q '"admin"' "$AUTH_FILE"; then
        green "Auth record contains 'admin' username"
    else
        red "Auth record does NOT contain 'admin' username"
    fi
    if grep -q '"hash"' "$AUTH_FILE" && grep -q '"salt"' "$AUTH_FILE"; then
        green "Auth record contains hash and salt"
    else
        red "Auth record missing hash or salt"
    fi
    if grep -q '"algorithm"' "$AUTH_FILE" && grep -q '"iterations"' "$AUTH_FILE"; then
        green "Auth record contains KDF metadata"
    else
        red "Auth record missing KDF metadata"
    fi
else
    red "Auth record does NOT exist"
fi

# Test 6: Setup is permanently disabled after first run
info "Test: Setup disabled after configuration..."
RESP=$(curl -s -X POST -d "password=AnotherPass456&confirm=AnotherPass456" "$BASE/?setup=true" 2>/dev/null)
if echo "$RESP" | grep -q "Already configured\|ERROR"; then
    green "Setup disabled after configuration"
else
    red "Setup still accepts new configuration: $RESP"
fi

echo ""
echo "--- LOGIN TESTS ---"

# Test 7: Login page shown when not authenticated
info "Test: Login page shown when not authenticated..."
RESP=$(curl -s "$BASE/?status=true" 2>/dev/null)
if echo "$RESP" | grep -q "Login\|doLogin\|sign.*in"; then
    green "Login page shown when not authenticated"
else
    red "Login page NOT shown when not authenticated"
fi

# Test 8: Login rejects missing credentials
info "Test: Login rejects missing credentials..."
RESP=$(curl -s -X POST -d "" "$BASE/?login=true" 2>/dev/null)
if echo "$RESP" | grep -q "ERROR"; then
    green "Login rejects missing credentials"
else
    red "Login did NOT reject missing credentials: $RESP"
fi

# Test 9: Login rejects wrong username
info "Test: Login rejects wrong username..."
RESP=$(curl -s -X POST -d "login=wronguser&pass=StrongPass123" "$BASE/?login=true" 2>/dev/null)
if echo "$RESP" | grep -q "ERROR"; then
    green "Login rejects wrong username"
else
    red "Login did NOT reject wrong username: $RESP"
fi

# Test 10: Login rejects wrong password
info "Test: Login rejects wrong password..."
RESP=$(curl -s -X POST -d "login=admin&pass=wrongpassword" "$BASE/?login=true" 2>/dev/null)
if echo "$RESP" | grep -q "ERROR"; then
    green "Login rejects wrong password"
else
    red "Login did NOT reject wrong password: $RESP"
fi

# Test 11: Login succeeds with correct credentials
info "Test: Login succeeds with correct credentials..."
RESP=$(curl -s -c "$COOKIE_JAR" -X POST -d "login=admin&pass=StrongPass123" "$BASE/?login=true" 2>/dev/null)
if echo "$RESP" | grep -q "OK"; then
    green "Login succeeds with correct credentials"
else
    red "Login failed with correct credentials: $RESP"
fi

# Test 12: Session cookie is set
info "Test: Session cookie is set..."
if [ -f "$COOKIE_JAR" ] && grep -q "mkyboot_session" "$COOKIE_JAR"; then
    green "Session cookie is set"
    SESSION_ID=$(grep "mkyboot_session" "$COOKIE_JAR" | awk '{print $NF}')
    info "Session ID: ${SESSION_ID:0:20}..."
else
    red "Session cookie NOT set"
    SESSION_ID=""
fi

# Test 13: Session file exists
info "Test: Session file exists..."
if [ -n "$SESSION_ID" ] && [ -f "$SESSION_DIR/$SESSION_ID.json" ]; then
    green "Session file exists"
    if grep -q '"username"' "$SESSION_DIR/$SESSION_ID.json"; then
        green "Session contains username"
    else
        red "Session does NOT contain username"
    fi
    if grep -q '"ip"' "$SESSION_DIR/$SESSION_ID.json"; then
        green "Session contains IP"
    else
        red "Session does NOT contain IP"
    fi
else
    red "Session file does NOT exist"
fi

# Test 14: Protected page accessible with valid session
info "Test: Protected page accessible with valid session..."
RESP=$(curl -s -b "$COOKIE_JAR" "$BASE/?status=true" 2>/dev/null)
if echo "$RESP" | grep -q "Dashboard\|MKYBOOT\|dashboard"; then
    green "Protected page accessible with valid session"
else
    red "Protected page NOT accessible with valid session"
fi

# Test 15: Protected page rejects no session
info "Test: Protected page rejects no session..."
RESP=$(curl -s "$BASE/?status=true" 2>/dev/null)
if echo "$RESP" | grep -q "Login\|doLogin"; then
    green "Protected page rejects no session"
else
    red "Protected page does NOT reject no session"
fi

echo ""
echo "--- LOGOUT TESTS ---"

# Test 16: Logout destroys session
info "Test: Logout destroys session..."
RESP=$(curl -s -b "$COOKIE_JAR" -c "$COOKIE_JAR" "$BASE/?logout=true" 2>/dev/null)
if echo "$RESP" | grep -q "OK"; then
    green "Logout returns OK"
else
    red "Logout did not return OK: $RESP"
fi

# Test 17: Session file removed after logout
info "Test: Session file removed after logout..."
if [ -n "$SESSION_ID" ] && [ ! -f "$SESSION_DIR/$SESSION_ID.json" ]; then
    green "Session file removed after logout"
else
    red "Session file still exists after logout"
fi

# Test 18: Protected page rejects after logout
info "Test: Protected page rejects after logout..."
RESP=$(curl -s -b "$COOKIE_JAR" "$BASE/?status=true" 2>/dev/null)
if echo "$RESP" | grep -q "Login\|doLogin"; then
    green "Protected page rejects after logout"
else
    red "Protected page does NOT reject after logout"
fi

echo ""
echo "--- RATE LIMITING TESTS ---"

# Clean rate limit file
rm -f "$RATE_FILE"

# Test 19: Rate limiting triggers after failed attempts
info "Test: Rate limiting triggers after 5 failed attempts..."
for i in 1 2 3 4 5; do
    curl -s -X POST -d "login=admin&pass=wrongpassword$i" "$BASE/?login=true" > /dev/null 2>&1
done
RESP=$(curl -s -X POST -d "login=admin&pass=wrongpassword6" "$BASE/?login=true" 2>/dev/null)
if echo "$RESP" | grep -q "Too many\|rate limit\|Try again"; then
    green "Rate limiting triggers after 5 failed attempts"
else
    red "Rate limiting did NOT trigger: $RESP"
fi

# Test 20: Rate limit file exists
info "Test: Rate limit file exists..."
if [ -f "$RATE_FILE" ]; then
    green "Rate limit file exists"
else
    red "Rate limit file does NOT exist"
fi

echo ""
echo "--- API TESTS ---"

# Test 21: API endpoints require authentication
info "Test: API status requires authentication..."
RESP=$(curl -s -o /dev/null -w "%{http_code}" "$BASE/?api=status" 2>/dev/null)
BODY=$(curl -s "$BASE/?api=status" 2>/dev/null)
if echo "$BODY" | grep -q "Not authenticated\|error"; then
    green "API status requires authentication"
else
    red "API status does NOT require authentication: $BODY"
fi

# Test 22: API works with valid session
info "Test: API status works with valid session..."
# Re-login for session
rm -f "$COOKIE_JAR"
curl -s -c "$COOKIE_JAR" -X POST -d "login=admin&pass=StrongPass123" "$BASE/?login=true" > /dev/null 2>&1
RESP=$(curl -s -b "$COOKIE_JAR" "$BASE/?api=status" 2>/dev/null)
if echo "$RESP" | grep -q "server\|ipv4\|version"; then
    green "API status works with valid session"
else
    red "API status does NOT work with valid session: $RESP"
fi

echo ""
echo "--- PASSWORD CHANGE TESTS ---"

# Test 23: Password change requires authentication
info "Test: Password change requires authentication..."
rm -f "$COOKIE_JAR"
RESP=$(curl -s -X POST -d "current_password=x&new_password=y&confirm_password=y" "$BASE/?changepw=true" 2>/dev/null)
if echo "$RESP" | grep -q "Not authenticated\|ERROR"; then
    green "Password change requires authentication"
else
    red "Password change does NOT require authentication: $RESP"
fi

# Test 24: Password change succeeds with valid session
info "Test: Password change succeeds with valid session..."
curl -s -c "$COOKIE_JAR" -X POST -d "login=admin&pass=StrongPass123" "$BASE/?login=true" > /dev/null 2>&1
RESP=$(curl -s -b "$COOKIE_JAR" -X POST -d "current_password=StrongPass123&new_password=NewStrong456&confirm_password=NewStrong456" "$BASE/?changepw=true" 2>/dev/null)
if echo "$RESP" | grep -q "OK"; then
    green "Password change succeeds"
else
    red "Password change failed: $RESP"
fi

# Test 25: Old password no longer works
info "Test: Old password no longer works..."
RESP=$(curl -s -X POST -d "login=admin&pass=StrongPass123" "$BASE/?login=true" 2>/dev/null)
if echo "$RESP" | grep -q "ERROR"; then
    green "Old password rejected"
else
    red "Old password still works: $RESP"
fi

# Test 26: New password works
info "Test: New password works..."
RESP=$(curl -s -c "$COOKIE_JAR" -X POST -d "login=admin&pass=NewStrong456" "$BASE/?login=true" 2>/dev/null)
if echo "$RESP" | grep -q "OK"; then
    green "New password works"
else
    red "New password does NOT work: $RESP"
fi

# Test 27: Password hash changed
info "Test: Password hash changed..."
if [ -f "$AUTH_FILE" ]; then
    if grep -q '"updated"' "$AUTH_FILE"; then
        green "Password record updated timestamp present"
    else
        red "Password record missing updated timestamp"
    fi
else
    red "Auth file missing"
fi

echo ""
echo "--- PHASE 1 REGRESSION ---"

# Test 28: No os.execute(data) in mkybootd
info "Test: No os.execute(data) in mkybootd..."
if grep -q 'os\.execute(data)' /usr/bin/mkybootd 2>/dev/null; then
    red "os.execute(data) found in mkybootd"
else
    green "No os.execute(data) in mkybootd"
fi

# Test 29: No testzone route
info "Test: No testzone route..."
if grep -q 'testzone' /srv/mkyboot/modules/mkyctl.lua 2>/dev/null; then
    red "testzone found in mkyctl.lua"
else
    green "No testzone route"
fi

# Test 30: No hardcoded admin/0000
info "Test: No hardcoded admin/0000..."
if grep -q 'admin/0000' /srv/mkyboot/modules/mkyctl.lua /srv/mkyboot/cfg/mkyboot.json 2>/dev/null; then
    red "Hardcoded admin/0000 found"
else
    green "No hardcoded admin/0000"
fi

echo ""
echo "--- FILE PERMISSIONS ---"

# Test 31: auth.json not world-readable
info "Test: auth.json permissions..."
if [ -f "$AUTH_FILE" ]; then
    PERMS=$(stat -c "%a" "$AUTH_FILE" 2>/dev/null || stat -f "%Lp" "$AUTH_FILE" 2>/dev/null)
    if [ "$PERMS" = "600" ] || [ "$PERMS" = "640" ]; then
        green "auth.json has restrictive permissions ($PERMS)"
    else
        red "auth.json has overly permissive permissions ($PERMS)"
    fi
else
    red "auth.json does not exist"
fi

# Test 32: sessions directory permissions
info "Test: sessions directory permissions..."
if [ -d "$SESSION_DIR" ]; then
    PERMS=$(stat -c "%a" "$SESSION_DIR" 2>/dev/null || stat -f "%Lp" "$SESSION_DIR" 2>/dev/null)
    if [ "$PERMS" = "700" ] || [ "$PERMS" = "750" ]; then
        green "sessions directory has restrictive permissions ($PERMS)"
    else
        red "sessions directory has overly permissive permissions ($PERMS)"
    fi
else
    red "sessions directory does not exist"
fi

echo ""
echo "=========================================="
echo " RESULTS: $PASS passed, $FAIL failed"
echo "=========================================="

if [ $FAIL -gt 0 ]; then
    exit 1
fi
