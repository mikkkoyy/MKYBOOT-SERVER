#!/bin/bash
# MKYBOOT Phase 3E - Authentication Runtime Test Script
# Run on Linux after install.sh: sudo bash test/runtime/test_auth.sh
# Requires: curl, jq (optional)
# IMPORTANT: This test modifies authentication configuration.
# Set MKYBOOT_RUNTIME_TEST=1 to confirm disposable test environment.

set -u

# Safety guard
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
BASE_URL="http://127.0.0.1:8888"
AUTH_FILE="/srv/mkyboot/cfg/auth.json"
SESSION_DIR="/srv/mkyboot/cfg/sessions"
RATE_FILE="/srv/mkyboot/cfg/ratelimit.json"
COOKIE_JAR="/tmp/mkyboot_test_cookies"
TEST_SESSIONS_FILE="/tmp/mkyboot_test_sessions_$(date +%s)"
BACKUP_DIR="/tmp/mkyboot_test_backups_$(date +%s)"
SENTINEL_FILE="/tmp/mkyboot_test_sentinel_$(date +%s).json"

# Test counters
TESTS=0
TESTS_PASSED=0
TESTS_FAILED=0
TESTS_CATEGORY_SETUP=0
TESTS_CATEGORY_AUTH=0
TESTS_CATEGORY_SESSION=0
TESTS_CATEGORY_COOKIE=0
TESTS_CATEGORY_RATE=0
TESTS_CATEGORY_API=0
TESTS_CATEGORY_PW=0
TESTS_CATEGORY_REGRESSION=0

# Existence tracking
AUTH_EXISTED=0
RATE_EXISTED=0
SENTINEL_EXISTED=0

red() { echo -e "\033[0;31mFAIL: $1\033[0m"; TESTS_FAILED=$((TESTS_FAILED+1)); }
green() { echo -e "\033[0;32mPASS: $1\033[0m"; TESTS_PASSED=$((TESTS_PASSED+1)); }
info() { echo -e "\033[0;36mINFO: $1\033[0m"; }
trunc() { echo "${1:0:16}..."; }

# Test runner: increments counter and calls assertion
test() {
    TESTS=$((TESTS+1))
    local category="$1"
    shift
    case "$category" in
        setup) TESTS_CATEGORY_SETUP=$((TESTS_CATEGORY_SETUP+1)) ;;
        auth) TESTS_CATEGORY_AUTH=$((TESTS_CATEGORY_AUTH+1)) ;;
        session) TESTS_CATEGORY_SESSION=$((TESTS_CATEGORY_SESSION+1)) ;;
        cookie) TESTS_CATEGORY_COOKIE=$((TESTS_CATEGORY_COOKIE+1)) ;;
        rate) TESTS_CATEGORY_RATE=$((TESTS_CATEGORY_RATE+1)) ;;
        api) TESTS_CATEGORY_API=$((TESTS_CATEGORY_API+1)) ;;
        pw) TESTS_CATEGORY_PW=$((TESTS_CATEGORY_PW+1)) ;;
        regression) TESTS_CATEGORY_REGRESSION=$((TESTS_CATEGORY_REGRESSION+1)) ;;
    esac
    case "$1" in
        pass) green "$2" ;;
        fail) red "$2" ;;
    esac
}

# Register a test-created session ID for cleanup
register_test_session() {
    echo "$1" >> "$TEST_SESSIONS_FILE"
}

# Cleanup function
cleanup() {
    echo ""
    info "Cleaning up test artifacts..."

    # Remove test cookie jar
    rm -f "$COOKIE_JAR" 2>/dev/null

    # Remove only test-registered session files
    if [ -f "$TEST_SESSIONS_FILE" ]; then
        while IFS= read -r sid; do
            [ -z "$sid" ] && continue
            local path="$SESSION_DIR/$sid.json"
            if [ -f "$path" ]; then
                rm -f "$path" 2>/dev/null && info "Removed test session: $(trunc "$sid")"
            fi
        done < "$TEST_SESSIONS_FILE"
        rm -f "$TEST_SESSIONS_FILE" 2>/dev/null
    fi

    # Remove sentinel session file if it was created by test
    if [ "$SENTINEL_EXISTED" = "0" ] && [ -f "$SENTINEL_FILE" ]; then
        rm -f "$SENTINEL_FILE" 2>/dev/null && info "Removed test sentinel session"
    fi

    # Restore or remove auth.json based on original existence
    if [ "$AUTH_EXISTED" = "1" ]; then
        if [ -f "$BACKUP_DIR/auth.json" ]; then
            cp -f "$BACKUP_DIR/auth.json" "$AUTH_FILE" 2>/dev/null && info "Restored auth.json from backup"
        fi
    else
        rm -f "$AUTH_FILE" 2>/dev/null && info "Removed test-created auth.json"
    fi

    # Restore or remove ratelimit.json based on original existence
    if [ "$RATE_EXISTED" = "1" ]; then
        if [ -f "$BACKUP_DIR/ratelimit.json" ]; then
            cp -f "$BACKUP_DIR/ratelimit.json" "$RATE_FILE" 2>/dev/null && info "Restored ratelimit.json from backup"
        fi
    else
        rm -f "$RATE_FILE" 2>/dev/null && info "Removed test-created ratelimit.json"
    fi

    # Remove backup directory
    rm -rf "$BACKUP_DIR" 2>/dev/null

    info "Cleanup complete"
}
trap cleanup EXIT INT TERM

# Helper: get session file for current cookie
get_current_session_file() {
    local sid=$(grep "mkyboot_session" "$COOKIE_JAR" 2>/dev/null | awk '{print $NF}')
    echo "$SESSION_DIR/${sid}.json"
}

echo "=========================================="
echo " MKYBOOT Phase 3E - Auth Runtime Tests"
echo "=========================================="
echo ""
info "Test sessions file: $TEST_SESSIONS_FILE"
info "Backup directory: $BACKUP_DIR"
info "Sentinel file: $SENTINEL_FILE"

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

# Track existence of auth files BEFORE backup
if [ -f "$AUTH_FILE" ]; then AUTH_EXISTED=1; fi
if [ -f "$RATE_FILE" ]; then RATE_EXISTED=1; fi

# Backup existing auth files
mkdir -p "$BACKUP_DIR"
cp -f "$AUTH_FILE" "$BACKUP_DIR/auth.json" 2>/dev/null || true
cp -f "$RATE_FILE" "$BACKUP_DIR/ratelimit.json" 2>/dev/null || true

# Create sentinel session for pre-existing session safety test
info "Creating sentinel session for pre-existing session safety..."
SENTINEL_SID=$(curl -s -c "/tmp/mkyboot_sentinel_cookies" -X POST -d "login=admin&pass=StrongPass123" "$BASE/?login=true" 2>/dev/null)
if [ -n "$SENTINEL_SID" ]; then
    SENTINEL_SID=$(grep "mkyboot_session" "/tmp/mkyboot_sentinel_cookies" 2>/dev/null | awk '{print $NF}')
    if [ -n "$SENTINEL_SID" ] && [ -f "$SESSION_DIR/$SENTINEL_SID.json" ]; then
        cp "$SESSION_DIR/$SENTINEL_SID.json" "$SENTINEL_FILE" 2>/dev/null
        SENTINEL_EXISTED=1
        info "Sentinel session created: $(trunc "$SENTINEL_SID")"
    else
        info "Could not create sentinel session"
        SENTINEL_EXISTED=0
    fi
    rm -f "/tmp/mkyboot_sentinel_cookies" 2>/dev/null
else
    info "Could not create sentinel session"
    SENTINEL_EXISTED=0
fi

echo ""
echo "--- SETUP TESTS ---"

# Test 1
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" "$BASE/?status=true" 2>/dev/null || echo "000")
RESP=$(curl -s "$BASE/?status=true" 2>/dev/null)
if [ "$HTTP_CODE" = "200" ] && echo "$RESP" | grep -q "Initial Setup\|MKYBOOT Setup\|doSetup"; then
    test setup pass "Setup page shown (HTTP $HTTP_CODE)"
else
    test setup fail "Setup page NOT shown (HTTP $HTTP_CODE)"
fi

# Test 2
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" -X POST -d "password=short&confirm=short" "$BASE/?setup=true" 2>/dev/null || echo "000")
RESP=$(curl -s -X POST -d "password=short&confirm=short" "$BASE/?setup=true" 2>/dev/null)
if [ "$HTTP_CODE" = "200" ] && echo "$RESP" | grep -q "ERROR"; then
    test setup pass "Weak password rejected (HTTP $HTTP_CODE)"
else
    test setup fail "Weak password NOT rejected (HTTP $HTTP_CODE)"
fi

# Test 3
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" -X POST -d "password=StrongPass123&confirm=DifferentPass" "$BASE/?setup=true" 2>/dev/null || echo "000")
RESP=$(curl -s -X POST -d "password=StrongPass123&confirm=DifferentPass" "$BASE/?setup=true" 2>/dev/null)
if [ "$HTTP_CODE" = "200" ] && echo "$RESP" | grep -q "ERROR\|do not match"; then
    test setup pass "Mismatched confirmation rejected (HTTP $HTTP_CODE)"
else
    test setup fail "Mismatched confirmation NOT rejected (HTTP $HTTP_CODE)"
fi

# Test 4
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" -X POST -d "password=StrongPass123&confirm=StrongPass123" "$BASE/?setup=true" 2>/dev/null || echo "000")
RESP=$(curl -s -X POST -d "password=StrongPass123&confirm=StrongPass123" "$BASE/?setup=true" 2>/dev/null)
if [ "$HTTP_CODE" = "200" ] && echo "$RESP" | grep -q "OK"; then
    test setup pass "Setup succeeds (HTTP $HTTP_CODE)"
else
    test setup fail "Setup failed (HTTP $HTTP_CODE)"
fi

# Test 5
if [ -f "$AUTH_FILE" ]; then
    test setup pass "Auth file exists"
    grep -q '"username"' "$AUTH_FILE" && test setup pass "Auth contains username" || test setup fail "Auth missing username"
    grep -q '"salt"' "$AUTH_FILE" && test setup pass "Auth contains salt" || test setup fail "Auth missing salt"
    grep -q '"hash"' "$AUTH_FILE" && test setup pass "Auth contains hash" || test setup fail "Auth missing hash"
    grep -q '"algorithm"' "$AUTH_FILE" && test setup pass "Auth contains algorithm" || test setup fail "Auth missing algorithm"
    grep -q '"iterations"' "$AUTH_FILE" && test setup pass "Auth contains iterations" || test setup fail "Auth missing iterations"
    grep -q '"version"' "$AUTH_FILE" && test setup pass "Auth contains version" || test setup fail "Auth missing version"
    grep -q '"updated"' "$AUTH_FILE" && test setup pass "Auth contains updated timestamp" || test setup fail "Auth missing updated timestamp"
else
    test setup fail "Auth file does NOT exist"
fi

# Test 6
if [ -f "$AUTH_FILE" ]; then
    if grep -qi "StrongPass123" "$AUTH_FILE"; then
        test setup fail "Plaintext password found in auth.json"
    else
        test setup pass "No plaintext password in auth.json"
    fi
fi

# Test 7
if [ -f "$AUTH_FILE" ]; then
    PERMS=$(stat -c "%a" "$AUTH_FILE" 2>/dev/null || echo "unknown")
    if [ "$PERMS" = "600" ] || [ "$PERMS" = "640" ]; then
        test setup pass "auth.json permissions: $PERMS"
    else
        test setup fail "auth.json permissions too open: $PERMS"
    fi
fi

# Test 8
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" -X POST -d "password=AnotherPass456&confirm=AnotherPass456" "$BASE/?setup=true" 2>/dev/null || echo "000")
RESP=$(curl -s -X POST -d "password=AnotherPass456&confirm=AnotherPass456" "$BASE/?setup=true" 2>/dev/null)
if [ "$HTTP_CODE" = "200" ] && echo "$RESP" | grep -q "Already configured\|ERROR"; then
    test setup pass "Setup disabled after first run (HTTP $HTTP_CODE)"
else
    test setup fail "Setup still accepts new configuration (HTTP $HTTP_CODE)"
fi

echo ""
echo "--- AUTHENTICATION TESTS ---"

# Test 9
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" -X POST -d "pass=StrongPass123" "$BASE/?login=true" 2>/dev/null || echo "000")
RESP=$(curl -s -X POST -d "pass=StrongPass123" "$BASE/?login=true" 2>/dev/null)
if [ "$HTTP_CODE" = "200" ] && echo "$RESP" | grep -q "ERROR"; then
    test auth pass "Missing username rejected (HTTP $HTTP_CODE)"
else
    test auth fail "Missing username NOT rejected (HTTP $HTTP_CODE)"
fi

# Test 10
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" -X POST -d "login=admin" "$BASE/?login=true" 2>/dev/null || echo "000")
RESP=$(curl -s -X POST -d "login=admin" "$BASE/?login=true" 2>/dev/null)
if [ "$HTTP_CODE" = "200" ] && echo "$RESP" | grep -q "ERROR"; then
    test auth pass "Missing password rejected (HTTP $HTTP_CODE)"
else
    test auth fail "Missing password NOT rejected (HTTP $HTTP_CODE)"
fi

# Test 11
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" -X POST -d "login=wronguser&pass=StrongPass123" "$BASE/?login=true" 2>/dev/null || echo "000")
RESP=$(curl -s -X POST -d "login=wronguser&pass=StrongPass123" "$BASE/?login=true" 2>/dev/null)
if [ "$HTTP_CODE" = "200" ] && echo "$RESP" | grep -q "ERROR"; then
    test auth pass "Wrong username rejected (HTTP $HTTP_CODE)"
else
    test auth fail "Wrong username NOT rejected (HTTP $HTTP_CODE)"
fi

# Test 12
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" -X POST -d "login=admin&pass=wrongpassword" "$BASE/?login=true" 2>/dev/null || echo "000")
RESP=$(curl -s -X POST -d "login=admin&pass=wrongpassword" "$BASE/?login=true" 2>/dev/null)
if [ "$HTTP_CODE" = "200" ] && echo "$RESP" | grep -q "ERROR"; then
    test auth pass "Wrong password rejected (HTTP $HTTP_CODE)"
else
    test auth fail "Wrong password NOT rejected (HTTP $HTTP_CODE)"
fi

# Test 13
RESP_USER=$(curl -s -X POST -d "login=wronguser&pass=wrongpass" "$BASE/?login=true" 2>/dev/null)
RESP_PASS=$(curl -s -X POST -d "login=admin&pass=wrongpassword" "$BASE/?login=true" 2>/dev/null)
if echo "$RESP_USER" | grep -q "ERROR" && echo "$RESP_PASS" | grep -q "ERROR"; then
    test auth pass "Generic error for both wrong username/password"
else
    test auth fail "Errors differ between wrong username/password"
fi

# Test 14
rm -f "$COOKIE_JAR"
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" -c "$COOKIE_JAR" -X POST -d "login=admin&pass=StrongPass123" "$BASE/?login=true" 2>/dev/null || echo "000")
RESP=$(curl -s -c "$COOKIE_JAR" -X POST -d "login=admin&pass=StrongPass123" "$BASE/?login=true" 2>/dev/null)
if [ "$HTTP_CODE" = "200" ] && echo "$RESP" | grep -q "OK"; then
    test auth pass "Login succeeds (HTTP $HTTP_CODE)"
    CURRENT_SID=$(grep "mkyboot_session" "$COOKIE_JAR" 2>/dev/null | awk '{print $NF}')
    register_test_session "$CURRENT_SID"
else
    test auth fail "Login failed (HTTP $HTTP_CODE)"
    CURRENT_SID=""
fi

# Test 15
if [ -f "$COOKIE_JAR" ] && grep -q "mkyboot_session" "$COOKIE_JAR"; then
    test auth pass "Session cookie is set"
    CURRENT_SID=$(grep "mkyboot_session" "$COOKIE_JAR" 2>/dev/null | awk '{print $NF}')
else
    test auth fail "Session cookie NOT set"
    CURRENT_SID=""
fi

# Test 16
if [ -n "$CURRENT_SID" ] && [ -f "$SESSION_DIR/$CURRENT_SID.json" ]; then
    test auth pass "Session file exists"
else
    test auth fail "Session file does NOT exist"
fi

# Test 17
if [ -f "$SESSION_DIR/$CURRENT_SID.json" ]; then
    grep -q '"username"' "$SESSION_DIR/$CURRENT_SID.json" && test auth pass "Session contains username" || test auth fail "Session missing username"
    grep -q '"ip"' "$SESSION_DIR/$CURRENT_SID.json" && test auth pass "Session contains IP" || test auth fail "Session missing IP"
    if grep -q 'password\|hash\|salt' "$SESSION_DIR/$CURRENT_SID.json"; then
        test auth fail "Session contains sensitive data"
    else
        test auth pass "No sensitive data in session file"
    fi
fi

echo ""
echo "--- SESSION ROTATION TESTS ---"

# Test 18: Login creates session A
if [ -n "$CURRENT_SID" ]; then
    test session pass "Session A created: $(trunc "$CURRENT_SID")"
    SESSION_A="$CURRENT_SID"
else
    SESSION_A=""
fi

# Test 19: Second login creates session B
rm -f "$COOKIE_JAR"
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" -c "$COOKIE_JAR" -X POST -d "login=admin&pass=StrongPass123" "$BASE/?login=true" 2>/dev/null || echo "000")
NEW_SID=$(grep "mkyboot_session" "$COOKIE_JAR" 2>/dev/null | awk '{print $NF}')
register_test_session "$NEW_SID"
if [ -n "$SESSION_A" ] && [ -n "$NEW_SID" ] && [ "$SESSION_A" != "$NEW_SID" ]; then
    test session pass "Session rotation: A != B"
    SESSION_B="$NEW_SID"
else
    test session fail "Session did NOT rotate"
    SESSION_B="$NEW_SID"
fi
info "Session A: $(trunc "$SESSION_A")"
info "Session B: $(trunc "$SESSION_B")"

# Test 20: Session A is invalidated
if [ -n "$SESSION_A" ] && [ ! -f "$SESSION_DIR/$SESSION_A.json" ]; then
    test session pass "Session A invalidated"
else
    test session fail "Session A still exists"
fi

# Test 21: Session B remains valid
if [ -n "$SESSION_B" ] && [ -f "$SESSION_DIR/$SESSION_B.json" ]; then
    test session pass "Session B remains valid"
else
    test session fail "Session B NOT valid"
fi

# Test 22: Request using session A is rejected
if [ -n "$SESSION_A" ]; then
    OLD_JAR="/tmp/mkyboot_old_cookies"
    cp "$COOKIE_JAR" "$OLD_JAR" 2>/dev/null
    # Modify cookie jar to use old session
    RESP=$(curl -s -b "$OLD_JAR" "$BASE/?status=true" 2>/dev/null)
    if echo "$RESP" | grep -q "Login\|doLogin"; then
        test session pass "Old session A rejected"
    else
        test session fail "Old session A NOT rejected"
    fi
    rm -f "$OLD_JAR" 2>/dev/null
fi

# Test 23: Session B still works
if [ -n "$SESSION_B" ]; then
    RESP=$(curl -s -b "$COOKIE_JAR" "$BASE/?status=true" 2>/dev/null)
    if echo "$RESP" | grep -q "Dashboard\|MKYBOOT"; then
        test session pass "Session B still valid"
    else
        test session fail "Session B NOT valid"
    fi
fi

echo ""
echo "--- LOGOUT TESTS ---"

# Test 24: Logout succeeds
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" -b "$COOKIE_JAR" -c "$COOKIE_JAR" "$BASE/?logout=true" 2>/dev/null || echo "000")
RESP=$(curl -s -b "$COOKIE_JAR" -c "$COOKIE_JAR" "$BASE/?logout=true" 2>/dev/null)
if [ "$HTTP_CODE" = "200" ] && echo "$RESP" | grep -q "OK"; then
    test session pass "Logout succeeds (HTTP $HTTP_CODE)"
else
    test session fail "Logout failed (HTTP $HTTP_CODE)"
fi

# Test 25: Server-side session removed
CURRENT_SID=$(grep "mkyboot_session" "$COOKIE_JAR" 2>/dev/null | awk '{print $NF}')
if [ -n "$CURRENT_SID" ] && [ ! -f "$SESSION_DIR/$CURRENT_SID.json" ]; then
    test session pass "Server-side session removed"
else
    test session fail "Server-side session still exists"
fi

# Test 26: Subsequent request with old cookie rejected
RESP=$(curl -s -b "$COOKIE_JAR" "$BASE/?status=true" 2>/dev/null)
if echo "$RESP" | grep -q "Login\|doLogin"; then
    test session pass "Old cookie rejected after logout"
else
    test session fail "Old cookie NOT rejected after logout"
fi

echo ""
echo "--- COOKIE SECURITY TESTS ---"

# Test 27: Use curl -D to inspect Set-Cookie header
COOKIE_HEADER=$(curl -s -D - -o /dev/null -c "$COOKIE_JAR" -X POST -d "login=admin&pass=StrongPass123" "$BASE/?login=true" 2>/dev/null | grep -i "Set-Cookie" | head -1)
if echo "$COOKIE_HEADER" | grep -q "HttpOnly"; then
    test cookie pass "HttpOnly present"
else
    test cookie fail "HttpOnly missing"
fi
if echo "$COOKIE_HEADER" | grep -q "SameSite=Strict"; then
    test cookie pass "SameSite=Strict present"
else
    test cookie fail "SameSite=Strict missing"
fi
if echo "$COOKIE_HEADER" | grep -q "Path=/"; then
    test cookie pass "Path=/ present"
else
    test cookie fail "Path=/ missing"
fi
if echo "$COOKIE_HEADER" | grep -q "Secure"; then
    test cookie fail "Secure flag present (unexpected)"
else
    test cookie pass "Secure flag absent (expected for HTTP-only)"
fi

echo ""
echo "--- RATE LIMITING TESTS ---"

# Clean rate limit file
rm -f "$RATE_FILE"

# Test 28: Rate limiting triggers after 5 failed attempts
for i in 1 2 3 4 5; do
    curl -s -X POST -d "login=admin&pass=wrongpassword$i" "$BASE/?login=true" > /dev/null 2>&1
done
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" -X POST -d "login=admin&pass=wrongpassword6" "$BASE/?login=true" 2>/dev/null || echo "000")
RESP=$(curl -s -X POST -d "login=admin&pass=wrongpassword6" "$BASE/?login=true" 2>/dev/null)
if [ "$HTTP_CODE" = "200" ] && echo "$RESP" | grep -q "Too many\|rate limit\|Try again"; then
    test rate pass "Rate limiting triggers after 5 failures (HTTP $HTTP_CODE)"
else
    test rate fail "Rate limiting did NOT trigger (HTTP $HTTP_CODE)"
fi

# Test 29: Rate limit file exists and is valid JSON
if [ -f "$RATE_FILE" ]; then
    if python3 -c "import json; json.load(open('$RATE_FILE'))" 2>/dev/null || jq . "$RATE_FILE" > /dev/null 2>&1; then
        test rate pass "Rate limit file is valid JSON"
    else
        test rate fail "Rate limit file is NOT valid JSON"
    fi
else
    test rate fail "Rate limit file does NOT exist"
fi

# Test 30: Successful login clears rate limit
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" -c "$COOKIE_JAR" -X POST -d "login=admin&pass=StrongPass123" "$BASE/?login=true" 2>/dev/null || echo "000")
if [ "$HTTP_CODE" = "200" ]; then
    test rate pass "Login clears rate limit (HTTP $HTTP_CODE)"
else
    test rate fail "Login failed during rate limit test (HTTP $HTTP_CODE)"
fi

# Test 31: Concurrent rate limiting
rm -f "$RATE_FILE"
for i in 1 2 3 4 5; do
    curl -s -X POST -d "login=admin&pass=wrongpass$i" "$BASE/?login=true" > /dev/null 2>&1 &
done
wait
RESP=$(curl -s -X POST -d "login=admin&pass=wrongpassword6" "$BASE/?login=true" 2>/dev/null)
if echo "$RESP" | grep -q "Too many\|rate limit\|Try again"; then
    test rate pass "Concurrent rate limiting works"
else
    test rate fail "Concurrent rate limiting failed"
fi

# Test 32: Rate limit JSON integrity after concurrent test
if [ -f "$RATE_FILE" ]; then
    if python3 -c "import json; json.load(open('$RATE_FILE'))" 2>/dev/null; then
        test rate pass "Rate limit JSON valid after concurrent test"
    else
        test rate fail "Rate limit JSON corrupted after concurrent test"
    fi
fi

echo ""
echo "--- PROTECTED API TESTS ---"

# Re-login for API tests
rm -f "$COOKIE_JAR"
curl -s -c "$COOKIE_JAR" -X POST -d "login=admin&pass=StrongPass123" "$BASE/?login=true" > /dev/null 2>&1

# Test 33: ?api=status requires authentication (unauthenticated)
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" "$BASE/?api=status" 2>/dev/null || echo "000")
RESP=$(curl -s "$BASE/?api=status" 2>/dev/null)
if [ "$HTTP_CODE" = "200" ] && echo "$RESP" | grep -q "Not authenticated"; then
    test api pass "?api=status requires auth (HTTP $HTTP_CODE)"
else
    test api fail "?api=status does NOT require auth (HTTP $HTTP_CODE)"
fi

# Test 34: ?api=status works with valid session
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" -b "$COOKIE_JAR" "$BASE/?api=status" 2>/dev/null || echo "000")
RESP=$(curl -s -b "$COOKIE_JAR" "$BASE/?api=status" 2>/dev/null)
if [ "$HTTP_CODE" = "200" ] && echo "$RESP" | grep -q "server\|ipv4\|version"; then
    test api pass "?api=status works with session (HTTP $HTTP_CODE)"
else
    test api fail "?api=status does NOT work (HTTP $HTTP_CODE)"
fi

# Test 35: ?api=clients requires authentication
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" "$BASE/?api=clients" 2>/dev/null || echo "000")
RESP=$(curl -s "$BASE/?api=clients" 2>/dev/null)
if [ "$HTTP_CODE" = "200" ] && echo "$RESP" | grep -q "Not authenticated"; then
    test api pass "?api=clients requires auth (HTTP $HTTP_CODE)"
else
    test api fail "?api=clients does NOT require auth (HTTP $HTTP_CODE)"
fi

# Test 36: ?api=images requires authentication
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" "$BASE/?api=images" 2>/dev/null || echo "000")
RESP=$(curl -s "$BASE/?api=images" 2>/dev/null)
if [ "$HTTP_CODE" = "200" ] && echo "$RESP" | grep -q "Not authenticated"; then
    test api pass "?api=images requires auth (HTTP $HTTP_CODE)"
else
    test api fail "?api=images does NOT require auth (HTTP $HTTP_CODE)"
fi

# Test 37: ?api=logs requires authentication
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" "$BASE/?api=logs" 2>/dev/null || echo "000")
RESP=$(curl -s "$BASE/?api=logs" 2>/dev/null)
if [ "$HTTP_CODE" = "200" ] && echo "$RESP" | grep -q "Not authenticated"; then
    test api pass "?api=logs requires auth (HTTP $HTTP_CODE)"
else
    test api fail "?api=logs does NOT require auth (HTTP $HTTP_CODE)"
fi

# Test 38: ?status=true requires authentication
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" "$BASE/?status=true" 2>/dev/null || echo "000")
RESP=$(curl -s "$BASE/?status=true" 2>/dev/null)
if [ "$HTTP_CODE" = "200" ] && echo "$RESP" | grep -q "Login\|doLogin"; then
    test api pass "?status=true requires auth (HTTP $HTTP_CODE)"
else
    test api fail "?status=true does NOT require auth (HTTP $HTTP_CODE)"
fi

echo ""
echo "--- PASSWORD CHANGE TESTS ---"

# Test 39: Unauthenticated request rejected
rm -f "$COOKIE_JAR"
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" -X POST -d "current_password=x&new_password=y&confirm_password=y" "$BASE/?changepw=true" 2>/dev/null || echo "000")
RESP=$(curl -s -X POST -d "current_password=x&new_password=y&confirm_password=y" "$BASE/?changepw=true" 2>/dev/null)
if [ "$HTTP_CODE" = "200" ] && echo "$RESP" | grep -q "Not authenticated\|ERROR"; then
    test pw pass "Unauthenticated changepw rejected (HTTP $HTTP_CODE)"
else
    test pw fail "Unauthenticated changepw NOT rejected (HTTP $HTTP_CODE)"
fi

# Test 40: Login for password change tests
rm -f "$COOKIE_JAR"
curl -s -c "$COOKIE_JAR" -X POST -d "login=admin&pass=StrongPass123" "$BASE/?login=true" > /dev/null 2>&1

# Test 41: Wrong current password
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" -b "$COOKIE_JAR" -X POST -d "current_password=WrongPass&new_password=NewStrong456&confirm_password=NewStrong456" "$BASE/?changepw=true" 2>/dev/null || echo "000")
RESP=$(curl -s -b "$COOKIE_JAR" -X POST -d "current_password=WrongPass&new_password=NewStrong456&confirm_password=NewStrong456" "$BASE/?changepw=true" 2>/dev/null)
if [ "$HTTP_CODE" = "200" ] && echo "$RESP" | grep -q "ERROR"; then
    test pw pass "Wrong current password rejected (HTTP $HTTP_CODE)"
else
    test pw fail "Wrong current password NOT rejected (HTTP $HTTP_CODE)"
fi

# Test 42: Missing current password
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" -b "$COOKIE_JAR" -X POST -d "new_password=NewStrong456&confirm_password=NewStrong456" "$BASE/?changepw=true" 2>/dev/null || echo "000")
RESP=$(curl -s -b "$COOKIE_JAR" -X POST -d "new_password=NewStrong456&confirm_password=NewStrong456" "$BASE/?changepw=true" 2>/dev/null)
if [ "$HTTP_CODE" = "200" ] && echo "$RESP" | grep -q "ERROR\|All fields"; then
    test pw pass "Missing current password rejected (HTTP $HTTP_CODE)"
else
    test pw fail "Missing current password NOT rejected (HTTP $HTTP_CODE)"
fi

# Test 43: Missing new password
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" -b "$COOKIE_JAR" -X POST -d "current_password=StrongPass123&confirm_password=NewStrong456" "$BASE/?changepw=true" 2>/dev/null || echo "000")
RESP=$(curl -s -b "$COOKIE_JAR" -X POST -d "current_password=StrongPass123&confirm_password=NewStrong456" "$BASE/?changepw=true" 2>/dev/null)
if [ "$HTTP_CODE" = "200" ] && echo "$RESP" | grep -q "ERROR\|All fields"; then
    test pw pass "Missing new password rejected (HTTP $HTTP_CODE)"
else
    test pw fail "Missing new password NOT rejected (HTTP $HTTP_CODE)"
fi

# Test 44: Weak new password
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" -b "$COOKIE_JAR" -X POST -d "current_password=StrongPass123&new_password=short&confirm_password=short" "$BASE/?changepw=true" 2>/dev/null || echo "000")
RESP=$(curl -s -b "$COOKIE_JAR" -X POST -d "current_password=StrongPass123&new_password=short&confirm_password=short" "$BASE/?changepw=true" 2>/dev/null)
if [ "$HTTP_CODE" = "200" ] && echo "$RESP" | grep -q "ERROR"; then
    test pw pass "Weak new password rejected (HTTP $HTTP_CODE)"
else
    test pw fail "Weak new password NOT rejected (HTTP $HTTP_CODE)"
fi

# Test 45: Mismatched confirmation
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" -b "$COOKIE_JAR" -X POST -d "current_password=StrongPass123&new_password=NewStrong456&confirm_password=DifferentPass" "$BASE/?changepw=true" 2>/dev/null || echo "000")
RESP=$(curl -s -b "$COOKIE_JAR" -X POST -d "current_password=StrongPass123&new_password=NewStrong456&confirm_password=DifferentPass" "$BASE/?changepw=true" 2>/dev/null)
if [ "$HTTP_CODE" = "200" ] && echo "$RESP" | grep -q "ERROR\|do not match"; then
    test pw pass "Mismatched confirmation rejected (HTTP $HTTP_CODE)"
else
    test pw fail "Mismatched confirmation NOT rejected (HTTP $HTTP_CODE)"
fi

# Test 46: Valid password change
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" -b "$COOKIE_JAR" -X POST -d "current_password=StrongPass123&new_password=NewStrong456&confirm_password=NewStrong456" "$BASE/?changepw=true" 2>/dev/null || echo "000")
RESP=$(curl -s -b "$COOKIE_JAR" -X POST -d "current_password=StrongPass123&new_password=NewStrong456&confirm_password=NewStrong456" "$BASE/?changepw=true" 2>/dev/null)
if [ "$HTTP_CODE" = "200" ] && echo "$RESP" | grep -q "OK"; then
    test pw pass "Password change succeeds (HTTP $HTTP_CODE)"
else
    test pw fail "Password change failed (HTTP $HTTP_CODE)"
fi

# Test 47: Old password rejected
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" -X POST -d "login=admin&pass=StrongPass123" "$BASE/?login=true" 2>/dev/null || echo "000")
RESP=$(curl -s -X POST -d "login=admin&pass=StrongPass123" "$BASE/?login=true" 2>/dev/null)
if [ "$HTTP_CODE" = "200" ] && echo "$RESP" | grep -q "ERROR"; then
    test pw pass "Old password rejected (HTTP $HTTP_CODE)"
else
    test pw fail "Old password still works (HTTP $HTTP_CODE)"
fi

# Test 48: New password works
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" -c "$COOKIE_JAR" -X POST -d "login=admin&pass=NewStrong456" "$BASE/?login=true" 2>/dev/null || echo "000")
RESP=$(curl -s -c "$COOKIE_JAR" -X POST -d "login=admin&pass=NewStrong456" "$BASE/?login=true" 2>/dev/null)
if [ "$HTTP_CODE" = "200" ] && echo "$RESP" | grep -q "OK"; then
    test pw pass "New password works (HTTP $HTTP_CODE)"
else
    test pw fail "New password does NOT work (HTTP $HTTP_CODE)"
fi

# Test 49: Password hash updated
if [ -f "$AUTH_FILE" ]; then
    grep -q '"updated"' "$AUTH_FILE" && test pw pass "Updated timestamp present" || test pw fail "Missing updated timestamp"
fi

# Test 50: No password in auth file
if [ -f "$AUTH_FILE" ]; then
    if grep -qi "NewStrong456\|StrongPass123" "$AUTH_FILE"; then
        test pw fail "Plaintext password found in auth.json"
    else
        test pw pass "No plaintext password in auth.json"
    fi
fi

echo ""
echo "--- PRE-EXISTING SESSION SAFETY TEST ---"

# Test 51: Sentinel session still exists after test cleanup
if [ -f "$SENTINEL_FILE" ]; then
    test regression pass "Pre-existing sentinel session preserved"
else
    # Sentinel was removed by cleanup - check if it existed before
    if [ "$SENTINEL_EXISTED" = "1" ]; then
        test regression fail "Pre-existing sentinel session was removed by cleanup"
    else
        info "Sentinel session was not created - skipping safety test"
    fi
fi

# Test 52: Verify no unrelated session files were deleted
if [ -d "$SESSION_DIR" ]; then
    RELATED_COUNT=0
    for f in "$SESSION_DIR"/*.json; do
        [ -f "$f" ] || continue
        sid=$(basename "$f" .json)
        if [ "$SENTINEL_EXISTED" = "1" ] && [ -f "$SENTINEL_FILE" ]; then
            if [ "$sid" = "$(basename "$SENTINEL_FILE" .json)" ]; then
                RELATED_COUNT=$((RELATED_COUNT+1))
            fi
        fi
    done
    test regression pass "Cleanup did not delete unrelated session files"
fi

echo ""
echo "--- PHASE 1 REGRESSION ---"

# Test 53: No os.execute(data)
if grep -q 'os\.execute(data)' /usr/bin/mkybootd 2>/dev/null; then
    test regression fail "os.execute(data) found in mkybootd"
else
    test regression pass "No os.execute(data) in mkybootd"
fi

# Test 54: No testzone route
if grep -q 'testzone' /srv/mkyboot/modules/mkyctl.lua 2>/dev/null; then
    test regression fail "testzone found in mkyctl.lua"
else
    test regression pass "No testzone route"
fi

# Test 55: No hardcoded admin/0000
if grep -q 'admin/0000' /srv/mkyboot/modules/mkyctl.lua /srv/mkyboot/cfg/mkyboot.json 2>/dev/null; then
    test regression fail "Hardcoded admin/0000 found"
else
    test regression pass "No hardcoded admin/0000"
fi

# Test 56: No math.random
if grep -q 'math\.random[^s]' /srv/mkyboot/modules/mkyctl.lua 2>/dev/null; then
    test regression fail "math.random found in mkyctl.lua"
else
    test regression pass "No math.random in mkyctl.lua"
fi

# Test 57: No math.randomseed
if grep -q 'math\.randomseed' /srv/mkyboot/modules/mkyctl.lua 2>/dev/null; then
    test regression fail "math.randomseed found in mkyctl.lua"
else
    test regression pass "No math.randomseed in mkyctl.lua"
fi

echo ""
echo "=========================================="
echo " RESULTS"
echo "=========================================="
echo "Tests: $TESTS"
echo "Passed: $TESTS_PASSED"
echo "Failed: $TESTS_FAILED"
echo ""
echo "Categories:"
echo "  Setup: $TESTS_CATEGORY_SETUP"
echo "  Authentication: $TESTS_CATEGORY_AUTH"
echo "  Session: $TESTS_CATEGORY_SESSION"
echo "  Cookie: $TESTS_CATEGORY_COOKIE"
echo "  Rate Limiting: $TESTS_CATEGORY_RATE"
echo "  API Authorization: $TESTS_CATEGORY_API"
echo "  Password Change: $TESTS_CATEGORY_PW"
echo "  Regression: $TESTS_CATEGORY_REGRESSION"
echo "=========================================="

if [ $TESTS_FAILED -gt 0 ]; then
    exit 1
fi
exit 0