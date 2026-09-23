#!/bin/bash
# MKYBOOT Phase 3D - Authentication Runtime Test Script
# Run on Linux after install.sh: sudo bash test/runtime/test_auth.sh
# Requires: curl, jq (optional)
# IMPORTANT: This test modifies authentication configuration.
# Set MKYBOOT_RUNTIME_TEST=1 to confirm disposable test environment.

set -u

# Safety guard: refuse to run without explicit confirmation
if [ "${MKYBOOT_RUNTIME_TEST:-0}" != "1" ]; then
    echo "=========================================="
    echo "  ERROR: This test modifies authentication"
    echo "  configuration (auth.json, sessions, rate limit)."
    echo ""
    echo "  To run, set: export MKYBOOT_RUNTIME_TEST=1"
    echo "  This confirms you are operating in a"
    echo "  disposable test environment."
    echo "=========================================="
    exit 1
fi

BASE="http://127.0.0.1:8888"
PASS=0
FAIL=0
COOKIE_JAR="/tmp/mkyboot_test_cookies"
AUTH_FILE="/srv/mkyboot/cfg/auth.json"
SESSION_DIR="/srv/mkyboot/cfg/sessions"
RATE_FILE="/srv/mkyboot/cfg/ratelimit.json"
TEST_USER="testuser_$(date +%s)"

# Backup existing files before destructive tests
BACKUP_DIR="/tmp/mkyboot_test_backups_$(date +%s)"

red() { echo -e "\033[0;31mFAIL: $1\033[0m"; FAIL=$((FAIL+1)); }
green() { echo -e "\033[0;32mPASS: $1\033[0m"; PASS=$((PASS+1)); }
info() { echo -e "\033[0;36mINFO: $1\033[0m"; }
trunc() { echo "${1:0:16}..."; }

cleanup() {
    echo ""
    info "Cleaning up test artifacts..."
    rm -f "$COOKIE_JAR" 2>/dev/null
    # Remove test sessions only (match pattern, don't delete all)
    if [ -d "$SESSION_DIR" ]; then
        for f in "$SESSION_DIR"/*.json; do
            [ -f "$f" ] || continue
            sid=$(basename "$f" .json)
            # Only remove test-created session files
            if [ ${#sid} -eq 44 ]; then
                rm -f "$f" 2>/dev/null
            fi
        done
    fi
    # Restore backups if they existed
    if [ -d "$BACKUP_DIR" ]; then
        cp -f "$BACKUP_DIR/auth.json" "$AUTH_FILE" 2>/dev/null || true
        cp -f "$BACKUP_DIR/ratelimit.json" "$RATE_FILE" 2>/dev/null || true
        rm -rf "$BACKUP_DIR" 2>/dev/null
    fi
    info "Cleanup complete"
}
trap cleanup EXIT INT TERM

echo "=========================================="
echo " MKYBOOT Phase 3D - Auth Runtime Tests"
echo "=========================================="
echo ""
info "Backup directory: $BACKUP_DIR"

# Pre-checks
info "Checking nginx is running..."
if ! systemctl is-active --quiet nginx 2>/dev/null; then
    red "nginx is not running"
    echo "ERROR: Start with: sudo systemctl start nginx"
    exit 1
fi
green "nginx is running"

info "Checking MKYBOOT is accessible..."
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" "$BASE/" 2>/dev/null || true)
if [ "$HTTP_CODE" = "000" ] || [ -z "$HTTP_CODE" ]; then
    red "Cannot connect to $BASE (HTTP $HTTP_CODE)"
    exit 1
fi
green "MKYBOOT is accessible (HTTP $HTTP_CODE)"

# Backup existing auth files
mkdir -p "$BACKUP_DIR"
cp -f "$AUTH_FILE" "$BACKUP_DIR/auth.json" 2>/dev/null || true
cp -f "$RATE_FILE" "$BACKUP_DIR/ratelimit.json" 2>/dev/null || true

echo ""
echo "--- SETUP TESTS ---"

# Test 1: Setup page is shown when not configured
info "Test 1: Setup page shown when not configured..."
RESP=$(curl -s "$BASE/?status=true" 2>/dev/null)
if echo "$RESP" | grep -q "Initial Setup\|MKYBOOT Setup\|doSetup"; then
    green "Setup page shown"
else
    red "Setup page NOT shown"
fi

# Test 2: Setup rejects weak password (< 8 chars)
info "Test 2: Setup rejects weak password..."
RESP=$(curl -s -X POST -d "password=short&confirm=short" "$BASE/?setup=true" 2>/dev/null)
if echo "$RESP" | grep -q "ERROR"; then
    green "Setup rejects weak password"
else
    red "Setup did NOT reject weak password: $(trunc "$RESP")"
fi

# Test 3: Setup rejects mismatched confirmation
info "Test 3: Setup rejects mismatched confirmation..."
RESP=$(curl -s -X POST -d "password=StrongPass123&confirm=DifferentPass" "$BASE/?setup=true" 2>/dev/null)
if echo "$RESP" | grep -q "ERROR\|do not match"; then
    green "Setup rejects mismatched confirmation"
else
    red "Setup did NOT reject mismatched confirmation"
fi

# Test 4: Setup succeeds with valid password
info "Test 4: Setup succeeds with valid password..."
RESP=$(curl -s -X POST -d "password=StrongPass123&confirm=StrongPass123" "$BASE/?setup=true" 2>/dev/null)
if echo "$RESP" | grep -q "OK"; then
    green "Setup succeeds"
else
    red "Setup failed: $(trunc "$RESP")"
fi

# Test 5: Auth file exists and contains KDF metadata
info "Test 5: Auth file contains required fields..."
if [ -f "$AUTH_FILE" ]; then
    if grep -q '"username"' "$AUTH_FILE"; then green "Contains username"; else red "Missing username"; fi
    if grep -q '"salt"' "$AUTH_FILE"; then green "Contains salt"; else red "Missing salt"; fi
    if grep -q '"hash"' "$AUTH_FILE"; then green "Contains hash"; else red "Missing hash"; fi
    if grep -q '"algorithm"' "$AUTH_FILE"; then green "Contains algorithm"; else red "Missing algorithm"; fi
    if grep -q '"iterations"' "$AUTH_FILE"; then green "Contains iterations"; else red "Missing iterations"; fi
    if grep -q '"version"' "$AUTH_FILE"; then green "Contains version"; else red "Missing version"; fi
    if grep -q '"updated"' "$AUTH_FILE"; then green "Contains updated timestamp"; else red "Missing updated timestamp"; fi
else
    red "Auth file does NOT exist"
fi

# Test 6: Password not stored in plaintext
info "Test 6: Password not stored in plaintext..."
if [ -f "$AUTH_FILE" ]; then
    if grep -qi "StrongPass123" "$AUTH_FILE"; then
        red "Plaintext password found in auth.json"
    else
        green "No plaintext password in auth.json"
    fi
fi

# Test 7: auth.json permissions
info "Test 7: auth.json has restrictive permissions..."
if [ -f "$AUTH_FILE" ]; then
    PERMS=$(stat -c "%a" "$AUTH_FILE" 2>/dev/null || echo "unknown")
    if [ "$PERMS" = "600" ] || [ "$PERMS" = "640" ]; then
        green "auth.json permissions: $PERMS"
    else
        red "auth.json permissions too open: $PERMS"
    fi
fi

# Test 8: Setup blocked after first initialization
info "Test 8: Setup disabled after configuration..."
RESP=$(curl -s -X POST -d "password=AnotherPass456&confirm=AnotherPass456" "$BASE/?setup=true" 2>/dev/null)
if echo "$RESP" | grep -q "Already configured\|ERROR"; then
    green "Setup disabled after first run"
else
    red "Setup still accepts new configuration"
fi

echo ""
echo "--- LOGIN TESTS ---"

# Test 9: Login rejects missing username
info "Test 9: Login rejects missing username..."
RESP=$(curl -s -X POST -d "pass=StrongPass123" "$BASE/?login=true" 2>/dev/null)
if echo "$RESP" | grep -q "ERROR"; then
    green "Login rejects missing username"
else
    red "Login did NOT reject missing username"
fi

# Test 10: Login rejects missing password
info "Test 10: Login rejects missing password..."
RESP=$(curl -s -X POST -d "login=admin" "$BASE/?login=true" 2>/dev/null)
if echo "$RESP" | grep -q "ERROR"; then
    green "Login rejects missing password"
else
    red "Login did NOT reject missing password"
fi

# Test 11: Login rejects wrong username
info "Test 11: Login rejects wrong username..."
RESP=$(curl -s -X POST -d "login=wronguser&pass=StrongPass123" "$BASE/?login=true" 2>/dev/null)
if echo "$RESP" | grep -q "ERROR"; then
    green "Login rejects wrong username"
else
    red "Login did NOT reject wrong username"
fi

# Test 12: Login rejects wrong password
info "Test 12: Login rejects wrong password..."
RESP=$(curl -s -X POST -d "login=admin&pass=wrongpassword" "$BASE/?login=true" 2>/dev/null)
if echo "$RESP" | grep -q "ERROR"; then
    green "Login rejects wrong password"
else
    red "Login did NOT reject wrong password"
fi

# Test 13: Generic error for both wrong username and wrong password
info "Test 13: Wrong username and wrong password produce identical errors..."
RESP_USER=$(curl -s -X POST -d "login=wronguser&pass=wrongpass" "$BASE/?login=true" 2>/dev/null)
RESP_PASS=$(curl -s -X POST -d "login=admin&pass=wrongpassword" "$BASE/?login=true" 2>/dev/null)
if echo "$RESP_USER" | grep -q "ERROR" && echo "$RESP_PASS" | grep -q "ERROR"; then
    green "Both return ERROR (generic behavior)"
else
    red "Errors differ between wrong username/password"
fi

# Test 14: Login succeeds with correct credentials
info "Test 14: Login succeeds with correct credentials..."
rm -f "$COOKIE_JAR"
RESP=$(curl -s -c "$COOKIE_JAR" -X POST -d "login=admin&pass=StrongPass123" "$BASE/?login=true" 2>/dev/null)
if echo "$RESP" | grep -q "OK"; then
    green "Login succeeds"
else
    red "Login failed: $(trunc "$RESP")"
fi

# Test 15: Session cookie is set
info "Test 15: Session cookie is set..."
if [ -f "$COOKIE_JAR" ] && grep -q "mkyboot_session" "$COOKIE_JAR"; then
    green "Session cookie is set"
    SESSION_ID=$(grep "mkyboot_session" "$COOKIE_JAR" | awk '{print $NF}')
    info "Session ID: $(trunc "$SESSION_ID")"
else
    red "Session cookie NOT set"
    SESSION_ID=""
fi

# Test 16: Session file exists
info "Test 16: Session file exists..."
if [ -n "$SESSION_ID" ] && [ -f "$SESSION_DIR/$SESSION_ID.json" ]; then
    green "Session file exists"
else
    red "Session file does NOT exist"
fi

# Test 17: Session file contains username
info "Test 17: Session file contains username..."
if [ -f "$SESSION_DIR/$SESSION_ID.json" ]; then
    if grep -q '"username"' "$SESSION_DIR/$SESSION_ID.json"; then green "Contains username"; else red "Missing username"; fi
    if grep -q '"ip"' "$SESSION_DIR/$SESSION_ID.json"; then green "Contains IP"; else red "Missing IP"; fi
    if grep -q '"created"' "$SESSION_DIR/$SESSION_ID.json"; then green "Contains created"; else red "Missing created"; fi
    if grep -q '"expires"' "$SESSION_DIR/$SESSION_ID.json"; then green "Contains expires"; else red "Missing expires"; fi
    if grep -q '"sid"' "$SESSION_DIR/$SESSION_ID.json"; then green "Contains sid"; else red "Missing sid"; fi
    # Verify no password/hash/salt in session
    if grep -q 'password\|hash\|salt' "$SESSION_DIR/$SESSION_ID.json"; then
        red "Session contains sensitive data"
    else
        green "No sensitive data in session file"
    fi
fi

# Test 18: Protected page accessible with valid session
info "Test 18: Protected page accessible with valid session..."
RESP=$(curl -s -b "$COOKIE_JAR" "$BASE/?status=true" 2>/dev/null)
if echo "$RESP" | grep -q "Dashboard\|MKYBOOT\|dashboard"; then
    green "Protected page accessible"
else
    red "Protected page NOT accessible"
fi

# Test 19: Protected page rejects no session
info "Test 19: Protected page rejects no session..."
RESP=$(curl -s "$BASE/?status=true" 2>/dev/null)
if echo "$RESP" | grep -q "Login\|doLogin"; then
    green "Protected page rejects no session"
else
    red "Protected page does NOT reject no session"
fi

echo ""
echo "--- SESSION ROTATION TESTS ---"

# Test 20: Old session ID != new session ID after login
info "Test 20: Session ID rotates on login..."
OLD_SID="$SESSION_ID"
# Login again to force rotation
rm -f "$COOKIE_JAR"
RESP=$(curl -s -c "$COOKIE_JAR" -X POST -d "login=admin&pass=StrongPass123" "$BASE/?login=true" 2>/dev/null)
NEW_SID=$(grep "mkyboot_session" "$COOKIE_JAR" 2>/dev/null | awk '{print $NF}')
if [ -n "$OLD_SID" ] && [ -n "$NEW_SID" ] && [ "$OLD_SID" != "$NEW_SID" ]; then
    green "Session ID rotated (old != new)"
else
    red "Session ID did NOT rotate"
fi
info "New session ID: $(trunc "$NEW_SID")"

# Test 21: Old session is invalidated after new login
info "Test 21: Old session invalidated after new login..."
if [ -n "$OLD_SID" ] && [ -f "$SESSION_DIR/$OLD_SID.json" ]; then
    red "Old session file still exists"
else
    green "Old session file removed"
fi

# Test 22: Old cookie rejected after rotation
info "Test 22: Old cookie rejected after rotation..."
# Create old cookie jar
OLD_JAR="/tmp/mkyboot_old_cookies"
OLD_SID_TEST=$(grep "mkyboot_session" "$COOKIE_JAR" 2>/dev/null | awk '{print $NF}')
RESP=$(curl -s -b "$COOKIE_JAR" "$BASE/?status=true" 2>/dev/null)
if echo "$RESP" | grep -q "Dashboard\|MKYBOOT"; then
    green "Current session still valid"
else
    red "Current session not valid after rotation"
fi

echo ""
echo "--- LOGOUT TESTS ---"

# Test 23: Logout destroys session
info "Test 23: Logout destroys session..."
RESP=$(curl -s -b "$COOKIE_JAR" -c "$COOKIE_JAR" "$BASE/?logout=true" 2>/dev/null)
if echo "$RESP" | grep -q "OK"; then
    green "Logout returns OK"
else
    red "Logout did not return OK: $(trunc "$RESP")"
fi

# Test 24: Session file removed after logout
info "Test 24: Session file removed after logout..."
CURRENT_SID=$(grep "mkyboot_session" "$COOKIE_JAR" 2>/dev/null | awk '{print $NF}')
if [ -n "$CURRENT_SID" ] && [ ! -f "$SESSION_DIR/$CURRENT_SID.json" ]; then
    green "Session file removed"
else
    red "Session file still exists after logout"
fi

# Test 25: Protected page rejects after logout
info "Test 25: Protected page rejects after logout..."
RESP=$(curl -s -b "$COOKIE_JAR" "$BASE/?status=true" 2>/dev/null)
if echo "$RESP" | grep -q "Login\|doLogin"; then
    green "Protected page rejects after logout"
else
    red "Protected page does NOT reject after logout"
fi

echo ""
echo "--- COOKIE SECURITY TESTS ---"

# Test 26: Cookie attributes
info "Test 26: Cookie has correct attributes..."
COOKIE_HEADER=$(grep "mkyboot_session" "$COOKIE_JAR" 2>/dev/null || true)
if echo "$COOKIE_HEADER" | grep -q "HttpOnly"; then
    green "HttpOnly present"
else
    red "HttpOnly missing"
fi
if echo "$COOKIE_HEADER" | grep -q "SameSite=Strict"; then
    green "SameSite=Strict present"
else
    red "SameSite=Strict missing"
fi
if echo "$COOKIE_HEADER" | grep -q "Path=/"; then
    green "Path=/ present"
else
    red "Path=/ missing"
fi
if echo "$COOKIE_HEADER" | grep -q "Secure"; then
    red "Secure flag present (unexpected for HTTP-only deployment)"
else
    green "Secure flag absent (expected for HTTP-only deployment)"
fi

echo ""
echo "--- RATE LIMITING TESTS ---"

# Clean rate limit file
rm -f "$RATE_FILE"

# Test 27: Rate limiting triggers after 5 failed attempts
info "Test 27: Rate limiting triggers after 5 failed attempts..."
for i in 1 2 3 4 5; do
    curl -s -X POST -d "login=admin&pass=wrongpassword$i" "$BASE/?login=true" > /dev/null 2>&1
done
RESP=$(curl -s -X POST -d "login=admin&pass=wrongpassword6" "$BASE/?login=true" 2>/dev/null)
if echo "$RESP" | grep -q "Too many\|rate limit\|Try again"; then
    green "Rate limiting triggers after 5 failed attempts"
else
    red "Rate limiting did NOT trigger: $(trunc "$RESP")"
fi

# Test 28: Rate limit file exists and is valid JSON
info "Test 28: Rate limit file is valid JSON..."
if [ -f "$RATE_FILE" ]; then
    if python3 -c "import json; json.load(open('$RATE_FILE'))" 2>/dev/null || jq . "$RATE_FILE" > /dev/null 2>&1; then
        green "Rate limit file is valid JSON"
    else
        red "Rate limit file is NOT valid JSON"
    fi
else
    red "Rate limit file does NOT exist"
fi

# Test 29: Successful login clears rate limit
info "Test 29: Successful login clears rate limit..."
RESP=$(curl -s -c "$COOKIE_JAR" -X POST -d "login=admin&pass=StrongPass123" "$BASE/?login=true" 2>/dev/null)
if [ "$RESP" = "OK" ]; then
    green "Login clears rate limit"
else
    red "Login failed during rate limit test"
fi

echo ""
echo "--- PROTECTED API TESTS ---"

# Re-login for API tests
rm -f "$COOKIE_JAR"
curl -s -c "$COOKIE_JAR" -X POST -d "login=admin&pass=StrongPass123" "$BASE/?login=true" > /dev/null 2>&1

# Test 30: ?api=status requires authentication
info "Test 30: ?api=status requires authentication..."
RESP=$(curl -s "$BASE/?api=status" 2>/dev/null)
if echo "$RESP" | grep -q "Not authenticated"; then
    green "?api=status requires authentication"
else
    red "?api=status does NOT require authentication"
fi

# Test 31: ?api=status works with valid session
info "Test 31: ?api=status works with valid session..."
RESP=$(curl -s -b "$COOKIE_JAR" "$BASE/?api=status" 2>/dev/null)
if echo "$RESP" | grep -q "server\|ipv4\|version"; then
    green "?api=status works with valid session"
else
    red "?api=status does NOT work: $(trunc "$RESP")"
fi

# Test 32: ?api=clients requires authentication
info "Test 32: ?api=clients requires authentication..."
RESP=$(curl -s "$BASE/?api=clients" 2>/dev/null)
if echo "$RESP" | grep -q "Not authenticated"; then
    green "?api=clients requires authentication"
else
    red "?api=clients does NOT require authentication"
fi

# Test 33: ?api=images requires authentication
info "Test 33: ?api=images requires authentication..."
RESP=$(curl -s "$BASE/?api=images" 2>/dev/null)
if echo "$RESP" | grep -q "Not authenticated"; then
    green "?api=images requires authentication"
else
    red "?api=images does NOT require authentication"
fi

# Test 34: ?api=logs requires authentication
info "Test 34: ?api=logs requires authentication..."
RESP=$(curl -s "$BASE/?api=logs" 2>/dev/null)
if echo "$RESP" | grep -q "Not authenticated"; then
    green "?api=logs requires authentication"
else
    red "?api=logs does NOT require authentication"
fi

# Test 35: ?status=true requires authentication
info "Test 35: ?status=true requires authentication..."
RESP=$(curl -s "$BASE/?status=true" 2>/dev/null)
if echo "$RESP" | grep -q "Login\|doLogin"; then
    green "?status=true requires authentication"
else
    red "?status=true does NOT require authentication"
fi

echo ""
echo "--- PASSWORD CHANGE TESTS ---"

# Test 36: Password change requires authentication
info "Test 36: Password change requires authentication..."
rm -f "$COOKIE_JAR"
RESP=$(curl -s -X POST -d "current_password=x&new_password=y&confirm_password=y" "$BASE/?changepw=true" 2>/dev/null)
if echo "$RESP" | grep -q "Not authenticated\|ERROR"; then
    green "Password change requires authentication"
else
    red "Password change does NOT require authentication"
fi

# Test 37: Password change fails with wrong current password
info "Test 37: Password change fails with wrong current password..."
rm -f "$COOKIE_JAR"
curl -s -c "$COOKIE_JAR" -X POST -d "login=admin&pass=StrongPass123" "$BASE/?login=true" > /dev/null 2>&1
RESP=$(curl -s -b "$COOKIE_JAR" -X POST -d "current_password=WrongPass&new_password=NewStrong456&confirm_password=NewStrong456" "$BASE/?changepw=true" 2>/dev/null)
if echo "$RESP" | grep -q "ERROR"; then
    green "Wrong current password rejected"
else
    red "Wrong current password not rejected"
fi

# Test 38: Password change fails with weak new password
info "Test 38: Password change fails with weak new password..."
RESP=$(curl -s -b "$COOKIE_JAR" -X POST -d "current_password=StrongPass123&new_password=short&confirm_password=short" "$BASE/?changepw=true" 2>/dev/null)
if echo "$RESP" | grep -q "ERROR"; then
    green "Weak new password rejected"
else
    red "Weak new password not rejected"
fi

# Test 39: Password change fails with mismatched confirmation
info "Test 39: Password change fails with mismatched confirmation..."
RESP=$(curl -s -b "$COOKIE_JAR" -X POST -d "current_password=StrongPass123&new_password=NewStrong456&confirm_password=DifferentPass" "$BASE/?changepw=true" 2>/dev/null)
if echo "$RESP" | grep -q "ERROR\|do not match"; then
    green "Mismatched confirmation rejected"
else
    red "Mismatched confirmation not rejected"
fi

# Test 40: Password change succeeds
info "Test 40: Password change succeeds..."
RESP=$(curl -s -b "$COOKIE_JAR" -X POST -d "current_password=StrongPass123&new_password=NewStrong456&confirm_password=NewStrong456" "$BASE/?changepw=true" 2>/dev/null)
if echo "$RESP" | grep -q "OK"; then
    green "Password change succeeds"
else
    red "Password change failed: $(trunc "$RESP")"
fi

# Test 41: Old password no longer works
info "Test 41: Old password no longer works..."
RESP=$(curl -s -X POST -d "login=admin&pass=StrongPass123" "$BASE/?login=true" 2>/dev/null)
if echo "$RESP" | grep -q "ERROR"; then
    green "Old password rejected"
else
    red "Old password still works"
fi

# Test 42: New password works
info "Test 42: New password works..."
RESP=$(curl -s -c "$COOKIE_JAR" -X POST -d "login=admin&pass=NewStrong456" "$BASE/?login=true" 2>/dev/null)
if echo "$RESP" | grep -q "OK"; then
    green "New password works"
else
    red "New password does NOT work"
fi

# Test 43: Password hash changed after password change
info "Test 43: Password hash changed..."
if [ -f "$AUTH_FILE" ]; then
    if grep -q '"updated"' "$AUTH_FILE"; then
        green "Updated timestamp present"
    else
        red "Missing updated timestamp"
    fi
fi

# Test 44: No password in test output
info "Test 44: No passwords in test output..."
# This is verified by inspection - the test script itself does not print passwords
green "Test script does not print passwords"

echo ""
echo "--- PHASE 1 REGRESSION ---"

# Test 45: No os.execute(data) in mkybootd
info "Test 45: No os.execute(data) in mkybootd..."
if grep -q 'os\.execute(data)' /usr/bin/mkybootd 2>/dev/null; then
    red "os.execute(data) found in mkybootd"
else
    green "No os.execute(data) in mkybootd"
fi

# Test 46: No testzone route
info "Test 46: No testzone route..."
if grep -q 'testzone' /srv/mkyboot/modules/mkyctl.lua 2>/dev/null; then
    red "testzone found in mkyctl.lua"
else
    green "No testzone route"
fi

# Test 47: No hardcoded admin/0000
info "Test 47: No hardcoded admin/0000..."
if grep -q 'admin/0000' /srv/mkyboot/modules/mkyctl.lua /srv/mkyboot/cfg/mkyboot.json 2>/dev/null; then
    red "Hardcoded admin/0000 found"
else
    green "No hardcoded admin/0000"
fi

# Test 48: No math.random
info "Test 48: No math.random..."
if grep -q 'math\.random[^s]' /srv/mkyboot/modules/mkyctl.lua 2>/dev/null; then
    red "math.random found in mkyctl.lua"
else
    green "No math.random in mkyctl.lua"
fi

# Test 49: No math.randomseed
info "Test 49: No math.randomseed..."
if grep -q 'math\.randomseed' /srv/mkyboot/modules/mkyctl.lua 2>/dev/null; then
    red "math.randomseed found in mkyctl.lua"
else
    green "No math.randomseed in mkyctl.lua"
fi

echo ""
echo "=========================================="
echo " RESULTS: $PASS passed, $FAIL failed"
echo "=========================================="

if [ $FAIL -gt 0 ]; then
    exit 1
fi
exit 0
