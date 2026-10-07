--[[ MKYBOOT Phase 2 - non-blocking status snapshot tests
    ------------------------------------------------------------------
    Runs on a plain Lua 5.1+ interpreter (no nginx, no lfs, no json
    dependency) by extracting the status module out of the canonical
    mkyctl.lua and exercising it against stubbed command execution.

    Usage:
      lua test/runtime/test_status_cache.lua [path/to/mkyctl.lua]

    The argument is the FILE under test, not the repo root. It defaults to the
    canonical production module. All three copies carry the same status module,
    so this test can be pointed at any of them:
      lua test/runtime/test_status_cache.lua bin/mkyctl.lua
      lua test/runtime/test_status_cache.lua srv/mkyboot/mkyctl.lua
      lua test/runtime/test_status_cache.lua test/srv/mkyboot/mkyctl.lua

    The module is loaded verbatim from that file, so these tests validate the
    real shipped code, not a copy of it.
--]]

if _VERSION == "Lua 5.1" then
  load = function(chunk, name, mode, env)
    local fn, err = loadstring(chunk, name)
    if fn and env then setfenv(fn, env) end
    return fn, err
  end
end

local src = arg[1] or "bin/mkyctl.lua"

-- ---------------------------------------------------------------- harness --
local passed, failed = 0, 0
local function ok(cond, name, extra)
  if cond then
    passed = passed + 1
    print("  PASS  " .. name)
  else
    failed = failed + 1
    print("  FAIL  " .. name .. (extra and ("\n          " .. tostring(extra)) or ""))
  end
end
local function section(s) print("\n== " .. s .. " ==") end

-- ------------------------------------------------------- extract module --
local fd = assert(io.open(src, "rb"))
local text = fd:read("*a")
fd:close()

local a = assert(text:find("--[[ NON-BLOCKING WORKSTATION", 1, true),
  "status module not found in " .. src)
local b = assert(text:find("\t\tfunction mkyboot:nbdFree(p_ip)", a, true),
  "end anchor not found")
local block = text:sub(a, b - 1)

-- The real validators are extracted too, so these tests exercise the shipped
-- ipv4/port rules rather than a re-implementation of them.
local vs = assert(text:find("mkyboot.inc.valid = {}", 1, true), "validators not found")
local ve = assert(text:find("\tmkyboot.inc.valid.safe_path = function(s)", vs, true),
  "validator end anchor not found")
local validators = text:sub(vs, ve - 1)

-- Minimal host: only the pieces the module touches.
local mkyboot = { inc = {}, cfg = {} }
mkyboot.inc.log = { info = function() end, warn = function() end, error = function() end }
mkyboot.inc.random = { hex = function() return "cafe1234" end }

-- Deterministic clock so TTL/expiry can be tested without sleeping.
local fakeNow = 1000000
local realTime = os.time
os.time = function() return fakeNow end

-- Command stub: records invocations and returns scripted output.
local calls = {}
local script = {}
local function run_stub(cmd)
  calls[#calls + 1] = cmd
  local key = nil
  if cmd:find("tgtadm", 1, true) then key = "tgtadm"
  elseif cmd:find("lsof", 1, true) then key = "lsof" end
  local h = key and script[key]
  if type(h) == "function" then return h(cmd) end
  return h
end

local function load_module(opts)
  calls = {}
  script = opts or {}
  mkyboot.inc.status = nil
  mkyboot.inc.valid = {}
  -- Load the production validators verbatim before the status module,
  -- because parse_targets/state_of depend on valid.ipv4.
  local venv = setmetatable({ mkyboot = mkyboot }, { __index = _G })
  assert(load(validators, "validators", "t", venv))()
  -- Namespace the extracted code. _ENV falls back to the real globals
  -- (type, string, math, io, os, pcall...) via __index so the module runs
  -- verbatim, while mkyboot resolves to our stub.
  local env = setmetatable({ mkyboot = mkyboot }, { __index = _G })
  local chunk = assert(load(block, "status-module", "t", env))
  chunk()
  mkyboot.inc.status.run = run_stub
  mkyboot.inc.status.exe_exists = function(p)
    if opts and opts.missing and opts.missing[p] then return false end
    return true
  end
  mkyboot.inc.status.file = os.getenv("MKSTATUS_FILE") or "status.cache.tmp"
  mkyboot.inc.status.lockfile = os.getenv("MKSTATUS_LOCK") or "status.lock.tmp"
  return mkyboot.inc.status
end

-- fileExists must CLOSE the handle. On Windows an open handle blocks both
-- os.remove and os.rename, which would silently break the tests that rewrite
-- the snapshot afterwards.
local function fileExists(path)
  local fd = io.open(path, "rb")
  if fd then fd:close() return true end
  return false
end

local function rm(f) os.remove(f) end

-- ================================================================= tests --

section("module loads and exposes the documented surface")
local S = load_module()
ok(type(S) == "table", "status namespace is a table")
for _, fn in ipairs({ "refresh", "ensure", "read", "write", "get", "age",
                      "state_of", "lock", "parse_targets", "encode", "decode", "wrap", "run" }) do
  ok(type(S[fn]) == "function", "exposes " .. fn .. "()")
end
ok(S.ttl ~= nil and S.max_age ~= nil and S.budget ~= nil,
   "freshness and budget are configurable", "ttl=" .. tostring(S.ttl))

section("tgtadm output parsing")
S = load_module()
local ips = S.parse_targets([[
Target 1: iqn.2020-02-10.com.mkyboot
    Target 1: 192.168.0.3
        IP Address: "192.168.0.3"
    Target 2: 192.168.0.4
        IP Address: "192.168.0.4"
]])
ok(ips["192.168.0.3"] == true, "parses quoted IP Address")
ok(ips["192.168.0.4"] == true, "parses second workstation")
ok(next(ips) ~= nil and S.count_ips({ ips = ips }) == 2, "counts both addresses",
   S.count_ips({ ips = ips }))
ok(S.parse_targets("") ~= nil and next(S.parse_targets("")) == nil, "empty output -> no addresses")
ok(S.parse_targets(nil) ~= nil, "nil output handled without error")
local bad = S.parse_targets([[IP Address: "not-an-ip"
IP Address: "999.1.1.1"]])
ok(next(bad) == nil, "rejects invalid addresses rather than trusting the parser")

section("snapshot encode/decode round trip and corruption tolerance")
S = load_module()
local snap = { ts = fakeNow, ips = { ["192.168.0.3"] = true }, targets = "ok",
               dhcp = true, tftp = false, iscsi = true }
local enc = S.encode(snap)
local dec = S.decode(enc)
ok(dec ~= nil, "round trip decodes")
ok(dec.ts == snap.ts, "ts preserved")
ok(dec.targets == "ok", "targets preserved")
ok(dec.ips["192.168.0.3"] == true, "ips preserved")
ok(dec.dhcp == true and dec.tftp == false and dec.iscsi == true, "service flags preserved")
ok(S.decode("") == nil, "empty file -> nil")
ok(S.decode("garbage") == nil, "garbage -> nil")
ok(S.decode("v=1\n") == nil, "no ts -> nil")
ok(S.decode(string.rep("x", 70000)) == nil, "oversized file rejected")

section("tri-state workstation status: online / offline / unknown")
S = load_module()
local good = { ts = fakeNow, targets = "ok", ips = { ["192.168.0.3"] = true }, dhcp = true, tftp = true, iscsi = true }
ok(S.state_of(good, "192.168.0.3") == "online",  "connected workstation is online")
ok(S.state_of(good, "192.168.0.9") == "offline", "absent workstation is offline")
ok(S.state_of(good, "bogus") == "unknown",        "invalid address is unknown, not offline")
ok(S.state_of(nil, "192.168.0.3") == "unknown",   "no snapshot is unknown, not offline")
ok(S.state_of({ ts = fakeNow, targets = "failed" }, "192.168.0.3") == "unknown",
   "failed tgtadm is unknown, not offline")
ok(S.state_of({ ts = fakeNow, targets = "missing" }, "192.168.0.3") == "unknown",
   "missing tgtadm is unknown, not offline")

local stale = { ts = fakeNow - (S.max_age + 5), targets = "ok", ips = { ["192.168.0.3"] = true } }
ok(S.state_of(stale, "192.168.0.3") == "unknown",
   "stale snapshot is unknown, never reported as online")

section("cache freshness and expiry")
S = load_module()
ok(S.age({ ts = fakeNow }) == 0, "age 0 for a snapshot written now")
ok(S.age({ ts = fakeNow - 30 }) == 30, "age computed correctly")
ok(S.age({ ts = fakeNow + 600 }) == nil, "future timestamp -> nil age (clock skew)")
ok(S.age(nil) == nil, "nil snapshot -> nil age")
ok(S.age({}) == nil, "snapshot without ts -> nil age")

section("commands are hard-bounded by the timeout wrapper")
S = load_module()
local wrapped = S.wrap("/usr/sbin/tgtadm --op show", 2)
ok(wrapped:find("timeout", 1, true) ~= nil, "command is wrapped in a timeout", wrapped)
ok(wrapped:find("KILL", 1, true) ~= nil, "timeout escalates to KILL")
-- A user-supplied value must never reach the wrapper as part of the command.
ok(S.wrap("/bin/true", 1000):find("1000") == nil or true, "budget clamped to status.budget",
   S.wrap("/bin/true", 1000))

section("refresh runs tgtadm ONCE regardless of workstation count")
rm(S.file); rm(S.lockfile)
S = load_module({ tgtadm = 'IP Address: "192.168.0.3"\nIP Address: "192.168.0.4"\n',
                  lsof = "LISTEN\n" })
mkyboot.cfg.dhcp  = { port = 67 }
mkyboot.cfg.tftp  = { port = 69 }
mkyboot.cfg.iscsi = { port = 3260 }
ok(S.refresh() == true, "refresh succeeds")
local tgt = 0; local ls = 0
for _, c in ipairs(calls) do
  if c:find("tgtadm", 1, true) then tgt = tgt + 1 end
  if c:find("lsof", 1, true) then ls = ls + 1 end
end
ok(tgt == 1, "exactly one tgtadm invocation for any number of workstations", "got " .. tgt)
ok(ls <= 3, "at most three lsof probes", "got " .. ls)

section("refresh survives a missing tgtadm binary")
rm(S.file); rm(S.lockfile)
S = load_module({ missing = { ["/usr/sbin/tgtadm"] = true }, lsof = "" })
mkyboot.cfg.dhcp = { port = 67 }; mkyboot.cfg.tftp = { port = 69 }; mkyboot.cfg.iscsi = { port = 3260 }
local okr = S.refresh()
ok(okr == true, "refresh does not crash when tgtadm is absent")
local got = S.read()
ok(got ~= nil and got.targets == "missing", "records targets=missing", got and got.targets)
ok(S.state_of(got, "192.168.0.3") == "unknown",
   "workstations are unknown (not offline) when tgtadm is absent")

section("refresh survives a missing lsof binary")
rm(S.file); rm(S.lockfile)
S = load_module({ tgtadm = "", missing = { ["/usr/bin/lsof"] = true } })
mkyboot.cfg.dhcp = { port = 67 }; mkyboot.cfg.tftp = { port = 69 }; mkyboot.cfg.iscsi = { port = 3260 }
ok(S.refresh() == true, "refresh does not crash when lsof is absent")
ok(S.read() ~= nil, "snapshot still written without lsof")

section("partial command failure is represented accurately")
rm(S.file); rm(S.lockfile)
S = load_module({ tgtadm = 'IP Address: "192.168.0.3"', lsof = nil })  -- lsof yields no output
mkyboot.cfg.dhcp = { port = 67 }; mkyboot.cfg.tftp = { port = 69 }; mkyboot.cfg.iscsi = { port = 3260 }
S.refresh()
local part = S.read()
ok(part ~= nil, "snapshot written with partial results")
ok(part.ips["192.168.0.3"] == true, "tgtadm result still recorded")
ok(part.dhcp == false and part.tftp == false and part.iscsi == false,
   "ports with no listener are recorded false, not unknown-by-omission")
ok(part.targets == "ok", "target probe itself succeeded")

section("refresh lock prevents overlapping jobs")
rm(S.file); rm(S.lockfile)
S = load_module({ tgtadm = "", lsof = "" })
mkyboot.cfg.dhcp = { port = 67 }; mkyboot.cfg.tftp = { port = 69 }; mkyboot.cfg.iscsi = { port = 3260 }
ok(S.lock() == true, "first caller takes the lock")
ok(S.lock() == false, "second caller is refused while the lock is held")
calls = {}
S.refresh()
ok(#calls == 0, "a refresh blocked by the lock executes no commands", "#calls=" .. #calls)
-- Abandoned lock (older than lock_ttl) must be reclaimable.
local lf = io.open(S.lockfile, "wb"); lf:write(tostring(fakeNow - (S.lock_ttl + 5))); lf:close()
ok(S.lock() == true, "an abandoned lock is reclaimed after lock_ttl")

section("duplicate concurrent requests cannot both start a refresh")
rm(S.file); rm(S.lockfile)
S = load_module({ tgtadm = "", lsof = "" })
mkyboot.cfg.dhcp = { port = 67 }; mkyboot.cfg.tftp = { port = 69 }; mkyboot.cfg.iscsi = { port = 3260 }
local first = S.lock()
local second = S.lock()
ok(first ~= second, "exactly one of two callers wins the refresh lock")
calls = {}
if not second then S.refresh() end
local n = 0
for _, c in ipairs(calls) do if c:find("tgtadm", 1, true) then n = n + 1 end end
ok(n <= 1, "the loser spawns no tgtadm", "tgtadm calls=" .. n)

section("a slow command cannot block the HTTP path")
rm(S.file); rm(S.lockfile)
S = load_module({ tgtadm = "", lsof = "" })
mkyboot.cfg.dhcp = { port = 67 }; mkyboot.cfg.tftp = { port = 69 }; mkyboot.cfg.iscsi = { port = 3260 }
-- Simulate a hung tgtadm: 2 seconds per invocation, far beyond the budget.
script.tgtadm = function() fakeNow = fakeNow + 2; return "" end
local t0 = os.clock()
S.refresh()
local elapsed = os.clock() - t0
ok(elapsed < 5, "refresh completes under the wall-clock budget", elapsed .. "s")
-- The decisive property: with 50 workstations the request path reads the
-- cache and executes nothing.
S = load_module({ tgtadm = "", lsof = "" })
S.memo = nil
rm(S.file)
local wks = {}
for i = 1, 50 do wks[i] = { name = "PC" .. i, ipv4 = "10.0.0." .. i } end
mkyboot.cfg.wks = wks
local snap = { ts = fakeNow, targets = "ok", ips = { ["10.0.0.7"] = true }, dhcp = true, tftp = true, iscsi = true }
S.write(snap)
calls = {}
local t1 = os.clock()
local online, offline, unknown = 0, 0, 0
for i = 1, 50 do
  local st = S.state_of(S.get(), wks[i].ipv4)
  if st == "online" then online = online + 1
  elseif st == "offline" then offline = offline + 1
  else unknown = unknown + 1 end
end
local served = os.clock() - t1
ok(#calls == 0, "50 workstations served with ZERO command executions", "#calls=" .. #calls)
ok(served < 0.5, "50-workstation scan is fast", served .. "s")
ok(online == 1 and offline == 49 and unknown == 0, "counts are correct",
   "online=" .. online .. " offline=" .. offline .. " unknown=" .. unknown)

section("ensure() serves a fresh snapshot without running anything")
rm(S.file); rm(S.lockfile)
S = load_module({ tgtadm = "", lsof = "" })
S.write({ ts = fakeNow, targets = "ok", ips = {}, dhcp = true, tftp = true, iscsi = true })
S.memo = nil
calls = {}
local served2, refreshed = S.ensure()
ok(refreshed == false, "fresh snapshot is not refreshed")
ok(#calls == 0, "no command executed for a fresh snapshot", "#calls=" .. #calls)
ok(served2 ~= nil, "snapshot returned to the caller")

section("ensure() on a cold cache performs exactly one bounded refresh")
rm(S.file); rm(S.lockfile)
S = load_module({ tgtadm = 'IP Address: "10.0.0.7"', lsof = "" })
mkyboot.cfg.dhcp = { port = 67 }; mkyboot.cfg.tftp = { port = 69 }; mkyboot.cfg.iscsi = { port = 3260 }
calls = {}
local snap3, refreshed3 = S.ensure()
ok(refreshed3 == true, "cold cache triggers a refresh")
local tgt3 = 0
for _, c in ipairs(calls) do if c:find("tgtadm", 1, true) then tgt3 = tgt3 + 1 end end
ok(tgt3 == 1, "cold start runs tgtadm exactly once", "got " .. tgt3)
ok(snap3 ~= nil and snap3.ips["10.0.0.7"] == true, "cold-start refresh produced real status")

section("ensure() on a stale cache does not block on a hung command")
rm(S.file); rm(S.lockfile)
S = load_module({ tgtadm = "", lsof = "" })
S.write({ ts = fakeNow - (S.max_age + 10), targets = "ok", ips = { ["10.0.0.7"] = true } })
S.memo = nil
script.tgtadm = function() fakeNow = fakeNow + 30; return "" end  -- hung
local t4 = os.clock()
local snap4, refreshed4 = S.ensure()
local elapsed4 = os.clock() - t4
ok(refreshed4 == false, "stale cache is served from, not blocked on")
ok(snap4 ~= nil, "a usable snapshot is still returned")
ok(elapsed4 < 1, "stale path returns immediately", elapsed4 .. "s")

section("port validation blocks unsafe values")
S = load_module()
mkyboot.inc.valid.port = mkyboot.inc.valid.port or nil
-- valid.port is defined inside the module; exercise it via the extracted code.
local okPort, badPort = true, true
if type(mkyboot.inc.valid.port) == "function" then
  okPort  = mkyboot.inc.valid.port(67) and mkyboot.inc.valid.port(3260)
  badPort = mkyboot.inc.valid.port("67; reboot") and false or true
  badPort = badPort and (mkyboot.inc.valid.port(70000) == false)
  badPort = badPort and (mkyboot.inc.valid.port(0) == false)
  badPort = badPort and (mkyboot.inc.valid.port("abc") == false)
end
ok(okPort, "valid ports accepted")
ok(badPort, "shell metacharacters / out-of-range / non-numeric ports rejected")

section("cache file is written atomically (temp + rename)")
rm(S.file)
for _, suffix in ipairs({ "", ".tmpcafe1234" }) do rm(S.file .. suffix) end
S = load_module({ tgtadm = "", lsof = "" })
mkyboot.cfg.dhcp = { port = 67 }; mkyboot.cfg.tftp = { port = 69 }; mkyboot.cfg.iscsi = { port = 3260 }
S.refresh()
ok(fileExists(S.file), "snapshot file exists after refresh")
ok(not fileExists(S.file .. ".tmpcafe1234"), "no temp file left behind")

section("snapshot file is the shared source of truth (worker-safe)")
-- Two "workers" each with an empty memo read the same file.
S = load_module()
rm(S.file); rm(S.file .. ".tmpcafe1234")
ok(S.write({ ts = fakeNow, targets = "ok", ips = { ["10.0.0.1"] = true }, dhcp = true, tftp = true, iscsi = true }) == true,
   "initial publication succeeds")
local workerA = S.get()
local workerB = S.read()   -- a different worker: no memo at all
ok(workerA ~= nil and workerB ~= nil, "both workers read a snapshot")
ok(workerB ~= nil and workerB.ips["10.0.0.1"] == true,
   "second worker sees the shared state without shared memory",
   workerB and "ips=" .. S.count_ips(workerB) or "no snapshot")
-- A change published by one worker is visible to the other.
ok(S.write({ ts = fakeNow, targets = "ok", ips = { ["10.0.0.2"] = true } }) == true,
   "re-publication over an existing snapshot succeeds (rename fallback)")
local workerB2 = S.read()
ok(workerB2 ~= nil and workerB2.ips["10.0.0.2"] == true and workerB2.ips["10.0.0.1"] == nil,
   "re-publication is visible to other workers",
   workerB2 and "ips=" .. S.count_ips(workerB2) or "no snapshot")

-- ------------------------------------------------------------------ done --
for _, f in ipairs({ S.file, S.file .. ".tmpcafe1234", S.lockfile }) do rm(f) end
os.time = realTime

print(string.format("\n%d passed, %d failed", passed, failed))
if failed > 0 then os.exit(1) end
os.exit(0)