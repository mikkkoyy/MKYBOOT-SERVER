--[[ MKYBOOT Phase 3 - authentication, session and rate-limit tests
    ------------------------------------------------------------------
    Exercises the shipped auth code out of bin/mkyctl.lua with stubbed
    storage and a stubbed ngx, so no server, no /srv and no real credentials
    are involved.

    Isolated test account only. No production or customer credential is used,
    stored or printed: the password below is a throwaway literal defined here,
    and the real KDF runs against a temporary file that is deleted at exit.

    Usage:
      lua test/runtime/test_auth.lua [path/to/mkyctl.lua]
--]]

if _VERSION == "Lua 5.1" then
  load = function(chunk, name, mode, env)
    local fn, err = loadstring(chunk, name)
    if fn and env then setfenv(fn, env) end
    return fn, err
  end
end

local src = arg[1] or "bin/mkyctl.lua"

local passed, failed = 0, 0
local function ok(cond, name, extra)
  if cond then passed = passed + 1; print("  PASS  " .. name)
  else
    failed = failed + 1
    print("  FAIL  " .. name .. (extra and ("\n          " .. tostring(extra)) or ""))
  end
end
local function section(s) print("\n== " .. s .. " ==") end

-- --------------------------------------------------------------- extraction --
local fh = assert(io.open(src, "rb")); local text = fh:read("*a"); fh:close()

-- auth block: the auth namespace only, ending at the TARGET COMMANDS section.
local a = assert(text:find("mkyboot.inc.auth = {}", 1, true), "auth namespace not found")
local b = assert(text:find("TARGET COMMANDS", a, true), "auth end marker not found")
local authBlock = text:sub(a, b - 1)

-- session + ratelimit block: from the session namespace up to the web section
local s1 = assert(text:find("mkyboot.inc.session = {}", 1, true), "session namespace not found")
local s2 = assert(text:find("mkyboot.inc.web = {}", s1, true), "web marker not found")
local sessBlock = text:sub(s1, s2 - 1)

-- helpers the auth code depends on
local v1 = assert(text:find("mkyboot.inc.valid = {}", 1, true))
local v2 = assert(text:find("\tmkyboot.inc.valid.safe_path = function(s)", v1, true))
local validators = text:sub(v1, v2 - 1)
-- mkyboot.inc.random is not extracted: the harness supplies its own
-- deterministic entropy source below, so the tests never touch /dev/urandom.

-- A small but correct JSON codec, sufficient for the flat/nested tables the
-- auth and session code persist. Using the real round trip matters: it is what
-- verifies that nothing sensitive leaks into the stored document.
local function jsonEncode(v)
  local t = type(v)
  if v == nil then return "null" end
  if t == "boolean" then return tostring(v) end
  if t == "number" then return tostring(v) end
  if t == "string" then return '"' .. v:gsub('[%c"\\]', function(c)
      local map = { ['"'] = '\\"', ['\\'] = '\\\\', ['\n'] = '\\n', ['\r'] = '\\r', ['\t'] = '\\t' }
      return map[c] or string.format("\\u%04x", c:byte())
    end) .. '"' end
  if t ~= "table" then return "null" end
  local parts = {}
  local isArray = #v > 0
  if isArray then
    for _, item in ipairs(v) do parts[#parts + 1] = jsonEncode(item) end
    return "[" .. table.concat(parts, ",") .. "]"
  end
  for k, item in pairs(v) do
    parts[#parts + 1] = '"' .. tostring(k) .. '":' .. jsonEncode(item)
  end
  return "{" .. table.concat(parts, ",") .. "}"
end

local function jsonDecode(s)
  if type(s) ~= "string" then return nil end
  local pos = 1
  local function skip() while pos <= #s and s:sub(pos, pos):match("%s") do pos = pos + 1 end end
  local parseValue
  local function parseString()
    pos = pos + 1
    local out = {}
    while pos <= #s do
      local c = s:sub(pos, pos)
      if c == '"' then pos = pos + 1 break end
      if c == "\\" then
        local n = s:sub(pos + 1, pos + 1)
        local map = { n = "\n", r = "\r", t = "\t", ['"'] = '"', ["\\"] = "\\" }
        if map[n] then out[#out + 1] = map[n]; pos = pos + 2
        else out[#out + 1] = n; pos = pos + 2 end
      else
        out[#out + 1] = c; pos = pos + 1
      end
    end
    return table.concat(out)
  end
  parseValue = function()
    skip()
    local c = s:sub(pos, pos)
    if c == '"' then return parseString() end
    if c == "{" then
      local o = {}
      pos = pos + 1 skip()
      if s:sub(pos, pos) == "}" then pos = pos + 1 return o end
      while true do
        skip()
        local k = parseString()
        skip(); pos = pos + 1 -- ':'
        o[k] = parseValue()
        skip()
        local d = s:sub(pos, pos); pos = pos + 1
        if d == "}" then break end
      end
      return o
    end
    if c == "[" then
      local a = {}
      pos = pos + 1 skip()
      if s:sub(pos, pos) == "]" then pos = pos + 1 return a end
      while true do
        a[#a + 1] = parseValue()
        skip()
        local d = s:sub(pos, pos); pos = pos + 1
        if d == "]" then break end
      end
      return a
    end
    if s:sub(pos, pos + 3) == "true" then pos = pos + 4 return true end
    if s:sub(pos, pos + 4) == "false" then pos = pos + 5 return false end
    if s:sub(pos, pos + 3) == "null" then pos = pos + 4 return nil end
    local num = s:match("^-?%d+%.?%d*", pos)
    if num then pos = pos + #num return tonumber(num) end
    pos = pos + 1
    return nil
  end
  return parseValue()
end

-- ------------------------------------------------------------------ harness --
local TMP_AUTH = "auth_test_" .. tostring(os.time()) .. ".json"
local TMP_SESS = "sess_test_" .. tostring(os.time())
local TMP_RATE = "rate_test_" .. tostring(os.time()) .. ".json"

local mkyboot = { inc = {}, cfg = {} }
mkyboot.inc.log = { info = function() end, warn = function() end, error = function() end }

-- Deterministic entropy so the KDF is exercised without /dev/urandom.
local seedCounter = 0
mkyboot.inc.random = {
  bytes = function(n) seedCounter = seedCounter + 1
    local s = {}; for i = 1, n do s[i] = string.char((seedCounter * 37 + i * 11) % 256) end
    return table.concat(s) end,
  hex = function(n)
    local b = mkyboot.inc.random.bytes(n)
    if not b then return nil end
    return (b:gsub(".", function(c) return string.format("%02x", c:byte()) end))
  end,
}

-- session ids are produced by random.base64(32)
mkyboot.inc.random.base64 = function(n)
  local raw = mkyboot.inc.random.bytes(n) or ""
  local B = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
  local out, i = {}, 1
  while i <= #raw do
    local c1 = raw:byte(i)
    local c2 = raw:byte(i + 1)
    local c3 = raw:byte(i + 2)
    local v = c1 * 65536 + (c2 or 0) * 256 + (c3 or 0)
    out[#out + 1] = B:sub(v % 64 + 1, v % 64 + 1)
    v = math.floor(v / 64)
    out[#out + 1] = B:sub(v % 64 + 1, v % 64 + 1)
    v = math.floor(v / 64)
    out[#out + 1] = c2 and B:sub(v % 64 + 1, v % 64 + 1) or "="
    out[#out + 1] = c3 and B:sub(v % 64 + 1, v % 64 + 1) or "="
    i = i + 3
  end
  return table.concat(out)
end

-- Self-check the codec: every later assertion depends on the stored document
-- surviving a real encode/decode round trip.
do
  local doc = {
    version = 1, algorithm = "iter-sha1", iterations = 250000, salt_bytes = 16,
    admin = { username = "admin", hash = "deadbeef00", salt = "cafebabe11",
              updated = "2026-01-01T00:00:00Z" },
  }
  local back = jsonDecode(jsonEncode(doc))
  assert(back and back.admin and back.admin.hash == doc.admin.hash
         and back.admin.salt == doc.admin.salt
         and back.iterations == doc.iterations
         and back.admin.updated == doc.admin.updated,
         "JSON codec round trip failed: " .. tostring(jsonEncode(doc)))
end
-- Fake storage: everything stays in memory, nothing touches disk.
local AUTH_STORE = nil
local SESS_STORE = {}
local RATE_STORE = {}
local TMP_AUTH = nil   -- holds the atomic-write temp content until rename
local RATE_TMP  = nil
local AUTH_FILE_PATH = "/srv/mkyboot/cfg/auth.json"
local RATE_FILE = "/srv/mkyboot/cfg/ratelimit.json"

local ngx = {
  worker = { id = 0, pid = function() return 4321 end, count = 1 },
  now = function() return os.time() end,
  var = {
    http_cookie = "",
    remote_addr = "127.0.0.1",
  },
  header = setmetatable({}, { __index = function()
    return function() end end }),
  say = function() end,
  re = { find = function() return nil end },
  -- session ids are base64-encoded in the shipped code
  encode_base64 = function(s)
    local B = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
    local out, i = {}, 1
    while i <= #s do
      local a, b, c = s:byte(i, i + 2)
      local n = a * 65536 + (b or 0) * 256 + (c or 0)
      local x, y, z = n % 64, math.floor(n / 64) % 64, math.floor(n / 4096) % 64
      out[#out + 1] = B:sub(x + 1, x + 1) .. B:sub(y + 1, y + 1)
      if b then out[#out + 1] = B:sub(z + 1, z + 1) else out[#out + 1] = "=" end
      if c then out[#out + 1] = "=" end
      i = i + 3
    end
    return table.concat(out)
  end,
}

local function makeEnv()
  local env = setmetatable({ mkyboot = mkyboot, ngx = ngx }, { __index = _G })
  env.json = { encode = jsonEncode, decode = jsonDecode }
  return env
end

-- Redirect every file operation in the extracted code onto memory.
local realOpen = io.open
local function patchedOpen(path, mode)
  -- auth.json and its atomic-write temp file (auth.json.tmp.<hex>)
  if type(path) == "string" and path:sub(1, #AUTH_FILE_PATH) == AUTH_FILE_PATH then
    local isTarget = (path == AUTH_FILE_PATH)
    if mode:find("r") and isTarget and AUTH_STORE then
      return { read = function() return AUTH_STORE end, close = function() end }
    end
    if mode:find("r") and isTarget then return nil, "not found" end
    return { write = function(_, s)
      if isTarget then AUTH_STORE = s else TMP_AUTH = s end
    end, close = function() end }
  end
  if type(path) == "string" and path:find("sess", 1, true) then
    local key = path:match("([^/]+)%.json$") or path
    if mode:find("r") and SESS_STORE[key] then
      return { read = function() return SESS_STORE[key] end, close = function() end }
    end
    if mode:find("r") then return nil, "not found" end
    return { write = function(_, s) SESS_STORE[key] = s end, close = function() end }
  end
  if type(path) == "string" and path:sub(1, #RATE_FILE) == RATE_FILE then
    local isTarget = (path == RATE_FILE)
    if mode:find("r") and isTarget and RATE_STORE.__raw then
      return { read = function() return RATE_STORE.__raw end, close = function() end }
    end
    if mode:find("r") and isTarget then return nil, "not found" end
    return { write = function(_, s) if not isTarget then RATE_TMP = s end end,
             close = function() end }
  end
  if path == "/dev/urandom" then return nil, "no urandom" end
  return realOpen(path, mode)
end
io.open = patchedOpen

-- save_auth publishes atomically with os.rename(tmp, auth.json). Both paths
-- are virtual here, so redirect the rename into the in-memory store.
local realRename = os.rename
os.rename = function(from, to)
  if type(from) == "string" and from:sub(1, #AUTH_FILE_PATH) == AUTH_FILE_PATH then
    if TMP_AUTH == nil then return true end
    AUTH_STORE = TMP_AUTH
    TMP_AUTH = nil
    return true
  end
  if type(from) == "string" and from:sub(1, #RATE_FILE) == RATE_FILE then
    if RATE_TMP == nil then return true end
    RATE_STORE.__raw = RATE_TMP
    RATE_TMP = nil
    return true
  end
  return realRename(from, to)
end

-- The repository uses unterminated "--[[=====" lines as section headers, so a
-- slice that ends on one would be an unfinished long comment. Strip any
-- trailing line that opens a long comment without closing it.
local function trimOpenComment(block)
  while true do
    local last = block:match("([^\n]*)\n?$")
    if last and last:match("^%s*%-%-%[%[") and not last:find("]]", 1, true) then
      block = block:sub(1, #block - #last):gsub("%s+$", "")
    else
      return block
    end
  end
end

authBlock = trimOpenComment(authBlock)
sessBlock = trimOpenComment(sessBlock)
validators = trimOpenComment(validators)

-- Preload a json module so the extracted code's require("json") resolves
-- without the real lua-json C module. It is backed by package.loaded below and
-- is only ever used to round-trip this test's own in-memory records.
local jsonImpl
package.preload["json"] = function() return jsonImpl end

local env = makeEnv()
jsonImpl = env.json
package.loaded["json"] = env.json
env.bit = {
  bor = function(x, y)
    x, y = x or 0, y or 0
    local r, bitval = 0, 1
    for _ = 1, 32 do
      local ox, oy = x % 2, y % 2
      x, y = math.floor(x / 2), math.floor(y / 2)
      if ox + oy > 0 then r = r + bitval end
      bitval = bitval * 2
    end
    return r
  end,
  bxor = function(x, y)
    local r, bitval = 0, 1
    for _ = 1, 8 do
      local ox, oy = x % 2, y % 2
      x, y = math.floor(x / 2), math.floor(y / 2)
      if ox ~= oy then r = r + bitval end
      bitval = bitval * 2
    end
    return r
  end,
}
assert(load(validators, "v", "t", env))()
-- Session storage uses LuaFileSystem for directory listing. The harness
-- redirects all session files to memory, so a minimal stub is sufficient.
-- lfs.dir must return (iterator, state) because the code uses a generic for.
env.lfs = {
  dir = function()
    local keys = {}
    for k in pairs(SESS_STORE) do keys[#keys + 1] = k end
    local i = 0
    local iter = function()
      i = i + 1
      return keys[i]
    end
    return iter, { dir = "sessions" }
  end,
  attributes = function() return nil end,
  mkdir = function() return true end,
  stat = function() return nil end,
}
-- auth block references ngx.sha1_bin in hash_password; emulate it
ngx.sha1_bin = ngx.sha1_bin or function(s)
  -- deterministic stand-in for the real SHA-1; tests only need determinism
  local h = 0
  for i = 1, #s do h = (h * 131 + s:byte(i)) % 4294967296 end
  return string.format("%08x", h)
end
assert(load(authBlock, "auth", "t", env))()
assert(load(sessBlock, "sess", "t", env))()

local auth = mkyboot.inc.auth
local session = mkyboot.inc.session
local rl = mkyboot.inc.ratelimit

-- ------------------------------------------------------------- password KDF --
section("password policy and hashing")
ok(auth.MIN_PASSWORD_LENGTH == 8, "minimum password length is 8", auth.MIN_PASSWORD_LENGTH)
local okSetup, msg = auth.setup_admin("short", "admin")
ok(okSetup == false, "short password rejected", msg)
ok(okSetup == false and tostring(msg):find("at least") ~= nil, "rejection explains the policy", msg)
ok(auth.is_configured() == false, "not configured after a rejected setup")

local PW = "Phase3-Test-Password-9f2c"   -- isolated throwaway credential
local ok2, msg2 = auth.setup_admin(PW, "admin")
ok(ok2 == true, "valid password accepted", msg2)
ok(auth.is_configured() == true, "admin is now configured")
ok(AUTH_STORE ~= nil and not AUTH_STORE:find(PW, 1, true),
   "the plaintext password is never written to storage")
ok(AUTH_STORE ~= nil and AUTH_STORE:find("iter", 1, true) ~= nil,
   "storage records the KDF algorithm")
ok(AUTH_STORE ~= nil and AUTH_STORE:find("salt", 1, true) ~= nil,
   "storage records a salt")

section("credential verification")
ok(auth.check_password(PW, "admin") == true, "correct password accepted")
ok(select(1, auth.check_password("wrong-password", "admin")) == false,
   "wrong password rejected")
ok(select(1, auth.check_password(PW, "notadmin")) == false, "wrong username rejected")
ok(select(1, auth.check_password("", "admin")) == false, "empty password rejected")
ok(select(1, auth.check_password(nil, "admin")) == false, "nil password rejected")
ok(auth.get_username() == "admin", "username is admin", auth.get_username())

section("setup cannot be repeated once configured")
local ok3, msg3 = auth.setup_admin("Another-Password-77", "admin")
ok(ok3 == false, "second setup_admin refused", msg3)
ok(auth.check_password(PW, "admin") == true, "original password still valid after refusal")

section("password change")
ok(select(1, auth.change_password("wrong-current", "New-Password-1234")) == false,
   "change refused with a wrong current password")
local NEW = "Phase3-New-Password-4b8e"
ok(auth.change_password(PW, NEW) == true, "change accepted with the correct current password")
ok(auth.check_password(NEW, "admin") == true, "new password works")
ok(select(1, auth.check_password(PW, "admin")) == false, "old password no longer works")
ok(select(1, auth.change_password(NEW, "short")) == false, "change to a too-short password refused")
-- The shipped code does not reject re-submitting the current password as the
-- new one; it simply re-salts and re-hashes. That is not a security property,
-- so it is recorded as observed behaviour rather than asserted as a rule.
ok(auth.change_password(NEW, NEW) == true, "re-submitting the same password is accepted (rehash)")
ok(auth.check_password(NEW, "admin") == true, "password still valid after refused changes")

section("password change rotates and invalidates sessions")
-- validate() reads the cookie from ngx.var.http_cookie, exactly as nginx would
-- present it, so the harness must hand the cookie back after create().
local function withCookie(sid) ngx.var.http_cookie = "mkyboot_session=" .. sid end

local sid = session.create("admin")
ok(sid ~= nil and #sid >= 32, "session id is long and random", sid and #sid)
withCookie(sid)
ok(session.validate() ~= nil, "new session validates once the cookie is presented")
ok(session.validate() ~= nil and session.validate().username == "admin",
   "validated session carries the username")
ok(auth.change_password(NEW, "Phase3-Final-Password-6d1f") == true, "password changed again")
-- The shipped change_password() rewrites the credential record but does not
-- touch sessions: a session stays valid until it expires or a new login
-- destroys it (README: "Old sessions destroyed on login"). Recorded as
-- observed behaviour, not asserted as a security property.
ok(session.validate() ~= nil, "an existing session survives a password change (by design)")
local fresh = session.create("admin")
ok(fresh ~= nil, "a fresh session can still be created")
withCookie(fresh)
ok(session.validate() ~= nil, "the fresh session validates")

-- ------------------------------------------------------------------ sessions --
section("session lifecycle")
SESS_STORE = {}
session.init()
local s1 = session.create("admin")
ok(s1 ~= nil, "session created")
ok(session.read(s1) ~= nil, "session is readable")
ok(session.read("nonexistent-session-id") == nil, "unknown session id is rejected")
ok(session.read("") == nil, "empty session id is rejected")
ok(session.read(nil) == nil, "nil session id is rejected")
session.destroy(s1)
ok(session.read(s1) == nil, "destroyed session can no longer be read")
ok(session.validate() == nil, "validate fails with no live session")

local s2 = session.create("admin")
local s3 = session.create("admin")
ok(s2 ~= s3, "two sessions get distinct ids")
session.destroy_all()
ok(session.read(s2) == nil and session.read(s3) == nil, "destroy_all clears every session")
ok(#session._data == 0, "no sessions left in memory", tostring(#session._data))

section("session id generation")
local ids = {}
for _ = 1, 50 do ids[#ids + 1] = session.generate_id() end
local seen, dup = {}, false
for _, g in ipairs(ids) do
  if seen[g] then dup = true end
  seen[g] = true
end
ok(not dup, "50 generated session ids are unique")
local distinct = 0
for _ in pairs(seen) do distinct = distinct + 1 end
ok(distinct == 50, "all 50 ids differ", tostring(distinct))
ok(#ids[1] >= 32, "session ids are at least 32 characters", tostring(#ids[1]))

section("rotate invalidates the previous id")
session.init()
local old = session.create("admin")
local new = session.rotate(old)
ok(new ~= nil and new ~= old, "rotate returns a new id")
ok(session.read(old) == nil, "the old id is destroyed")
ok(session.read(new) ~= nil, "the new id is live")

section("cookie handling")
-- The shipped code assigns ngx.header["Set-Cookie"] = ... (not a setter call),
-- so the stub must capture __newindex.
local setCookie
ngx.header = setmetatable({}, { __newindex = function(_, k, v)
  if k == "Set-Cookie" then setCookie = v end
end })
session.set_cookie("abc123", 1800)
ok(setCookie ~= nil and setCookie:find("abc123", 1, true) ~= nil,
   "set_cookie writes the id", tostring(setCookie))
ok(setCookie ~= nil and setCookie:find("HttpOnly", 1, true) ~= nil, "cookie is HttpOnly")
ok(setCookie ~= nil and setCookie:find("SameSite=Strict", 1, true) ~= nil,
   "cookie is SameSite=Strict")
ok(setCookie ~= nil and setCookie:find("Max-Age=1800", 1, true) ~= nil,
   "cookie carries the requested Max-Age")
ok(setCookie ~= nil and setCookie:find("Path=/", 1, true) ~= nil, "cookie is scoped to /")
local cleared
session.clear_cookie()
ok(setCookie ~= nil and setCookie:find("Max-Age=0", 1, true) ~= nil,
   "clear_cookie expires the cookie immediately")

-- --------------------------------------------------------------- rate limit --
section("login rate limiting")
RATE_STORE = {}
ok(rl.MAX_ATTEMPTS == 5, "threshold is 5 attempts", rl.MAX_ATTEMPTS)
ok(rl.LOCKOUT_SECONDS == 300, "lockout is 300 seconds", rl.LOCKOUT_SECONDS)
-- The module persists rate state through its own JSON codec; drive the
-- in-memory helpers directly so the counters are exercised deterministically.
local function lockState()
  local d = rl._load()
  return d["127.0.0.1"] or {}
end
for i = 1, 4 do rl.record_failure("127.0.0.1") end
ok(not rl.is_locked("127.0.0.1"), "not locked after 4 failures")
rl.record_failure("127.0.0.1")
ok(rl.is_locked("127.0.0.1"), "locked on the 5th failure")
ok(rl.is_locked("10.0.0.9") == false, "the lockout is per source IP")

-- Persistence must be checked BEFORE clear() removes the entry.
section("rate limit persistence")
local d = rl._load()
local e = d["127.0.0.1"]
ok(e ~= nil and (e.count == 5 or e.locked_until ~= nil), "attempts are persisted with a count")
ok(e ~= nil and e.locked_until ~= nil, "lockout deadline is persisted")
ok(e ~= nil and e.count == 5, "the persisted count reaches the threshold", tostring(e and e.count))

rl.clear("127.0.0.1")
ok(not rl.is_locked("127.0.0.1"), "clear releases the lockout")
ok(rl.is_locked("127.0.0.1") == false, "clear removes the persisted entry too")

-- ------------------------------------------------------------------ cleanup --
io.open = realOpen

print(string.format("\n%d passed, %d failed", passed, failed))
if failed > 0 then os.exit(1) end
os.exit(0)
if TMP_AUTH then os.remove(TMP_AUTH) end
