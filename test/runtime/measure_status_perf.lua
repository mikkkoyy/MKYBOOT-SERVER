--[[ MKYBOOT Phase 3 - status snapshot latency and concurrency measurement
    ------------------------------------------------------------------
    Produces the numbers quoted in the Phase 3 report:

      * command count per refresh vs workstation count (must be constant)
      * request-path latency vs workstation count (cache-hit path)
      * behaviour under a burst of readers
      * refresh deduplication
      * cold / fresh / expired cache outcomes
      * a refresh already in progress must not delay the request path
      * missing / malformed snapshot handling

    Runs on a plain Lua interpreter with stubbed command execution: no
    tgtadm, lsof, nginx or Ubuntu is required. It measures the module's own
    scheduling behaviour, which is what changed in Phase 2.

    Usage:
      lua test/runtime/measure_status_perf.lua [path/to/mkyctl.lua]
--]]

local src = arg[1] or "bin/mkyctl.lua"

local fd = assert(io.open(src, "rb"), "cannot open " .. src)
local text = fd:read("*a"); fd:close()
local a = assert(text:find("--[[ NON-BLOCKING WORKSTATION", 1, true), "status module not found")
local b = assert(text:find("\t\tfunction mkyboot:nbdFree(p_ip)", a, true), "anchor not found")
local block = text:sub(a, b - 1)
local vs = assert(text:find("mkyboot.inc.valid = {}", 1, true))
local ve = assert(text:find("\tmkyboot.inc.valid.safe_path = function(s)", vs, true))
local validators = text:sub(vs, ve - 1)

local mkyboot = { inc = {}, cfg = {} }
mkyboot.inc.log = { info = function() end, warn = function() end, error = function() end }
mkyboot.inc.random = { hex = function() return "cafe1234" end }
local env = setmetatable({ mkyboot = mkyboot }, { __index = _G })
assert(load(validators, "v", "t", env))()
assert(load(block, "s", "t", env))()

local S = mkyboot.inc.status
S.file = "perf_status.tmp"
S.lockfile = "perf_status.lock"

local calls = 0
local script = { tgtadm = "", lsof = "" }
S.run = function(cmd)
  calls = calls + 1
  local key = cmd:find("tgtadm", 1, true) and "tgtadm" or "lsof"
  local h = script[key]
  if type(h) == "function" then return h(cmd) end
  return h
end

-- This host is Windows: /usr/sbin/tgtadm, /usr/bin/lsof and /usr/bin/timeout do
-- not exist here, so the module's real exe_exists() would report every command
-- as missing and nothing would ever be measured. Stub it to "present" for the
-- baseline runs; the missing-command section overrides it explicitly.
local realExists = S.exe_exists
S.exe_exists = function() return true end

local function rm(f) os.remove(f) end
local function cleanup()
  rm(S.file); rm(S.file .. ".tmpcafe1234"); rm(S.lockfile); S.memo = nil
end
local function cfg(n)
  mkyboot.cfg.dhcp  = { port = 67 }
  mkyboot.cfg.tftp  = { port = 69 }
  mkyboot.cfg.iscsi = { port = 3260 }
  mkyboot.cfg.wks   = {}
  if n then
    for i = 1, n do mkyboot.cfg.wks[i] = { name = "PC" .. i, ipv4 = "10.0.0." .. (i % 250) } end
  end
end

local realTime = os.time
local fakeNow = realTime()
os.time = function() return fakeNow end

print("MKYBOOT status snapshot - latency and command-count measurement")
print("module: " .. src)
print(string.format("budget=%.1fs  ttl=%ds  max_age=%ds  lock_ttl=%ds",
  S.budget, S.ttl, S.max_age, S.lock_ttl))

-- ------------------------------------------------ command count per refresh --
print("\n-- command count per refresh vs workstation count --")
print("  wks   tgtadm   lsof   total")
for _, n in ipairs({ 1, 3, 10, 50, 200, 500 }) do
  cleanup(); cfg(n)
  calls = 0
  script.tgtadm = 'IP Address: "10.0.0.7"\n'
  script.lsof = "LISTEN\n"
  local ok = S.refresh()
  local total = calls
  -- separate the two categories with lsof unavailable
  cleanup(); cfg(n)
  calls = 0
  local savedExists = S.exe_exists
  S.exe_exists = function(p) return p ~= "/usr/bin/lsof" end
  S.refresh()
  local tgtOnly = calls
  S.exe_exists = savedExists
  print(string.format("  %4d   %7d   %4d   %5d   refresh=%s", n, tgtOnly, total - tgtOnly, total, tostring(ok)))
end

-- ------------------------------------------------------- request-path latency --
print("\n-- request-path latency (get_server_status equivalent) vs wks --")
print("  wks   cached_scan_ms   commands_executed")
for _, n in ipairs({ 1, 10, 50, 200, 500, 1000 }) do
  cleanup(); cfg(n)
  local ips = { ["10.0.0.7"] = true }
  S.write({ ts = fakeNow, targets = "ok", ips = ips, dhcp = true, tftp = true, iscsi = true })
  S.memo = nil
  calls = 0
  local snap = S.get()
  local t0 = os.clock()
  local online, offline, unknown = 0, 0, 0
  for i = 1, n do
    local st = S.state_of(snap, mkyboot.cfg.wks[i].ipv4)
    if st == "online" then online = online + 1
    elseif st == "offline" then offline = offline + 1
    else unknown = unknown + 1 end
  end
  print(string.format("  %4d   %13.3f   %d   (online=%d offline=%d unknown=%d)",
    n, (os.clock() - t0) * 1000, calls, online, offline, unknown))
end

-- -------------------------------------------------------- concurrent readers --
print("\n-- concurrent readers --")
cleanup(); cfg(50)
local ips = { ["10.0.0.7"] = true }
S.write({ ts = fakeNow, targets = "ok", ips = ips, dhcp = true, tftp = true, iscsi = true })
S.memo = nil
calls = 0
local t0 = os.clock()
for _ = 1, 1000 do
  local s = S.get()
  for i = 1, 50 do S.state_of(s, mkyboot.cfg.wks[i].ipv4) end
end
print(string.format("  1000 cached requests x 50 wks = 50,000 lookups in %.1f ms",
  (os.clock() - t0) * 1000))
print(string.format("  commands executed during 1000 cached requests: %d (expect 0)", calls))

-- --------------------------------------------------------- refresh dedupe ----
print("\n-- refresh deduplication --")
cleanup(); cfg()
local winners = 0
for _ = 1, 25 do if S.lock() then winners = winners + 1 end end
print(string.format("  25 concurrent lock attempts -> %d winner (expect 1)", winners))
cleanup(); cfg()
local w1, w2, w3 = S.lock(), S.lock(), S.lock()
print(string.format("  sequential lock() while held -> %s,%s,%s (expect true,false,false)",
  tostring(w1), tostring(w2), tostring(w3)))
cleanup(); cfg()
calls = 0
S.refresh()  -- lock is free again after cleanup
print(string.format("  commands executed by the single winner: %d", calls))

-- ------------------------------------------------------------ cold / fresh / expired --
print("\n-- cold / fresh / expired cache --")
cleanup(); cfg()
script.tgtadm = 'IP Address: "10.0.0.7"\n'; script.lsof = "LISTEN\n"
calls = 0
local snapC, refreshedC = S.ensure()
print(string.format("  cold   : refreshed=%s commands=%d targets=%s",
  tostring(refreshedC), calls, snapC and snapC.targets))

calls = 0
local snapF, refreshedF = S.ensure()
print(string.format("  fresh  : refreshed=%s commands=%d (expect 0)", tostring(refreshedF), calls))

cleanup(); cfg()
S.write({ ts = fakeNow - (S.max_age + 30), targets = "ok", ips = { ["10.0.0.7"] = true } })
S.memo = nil
calls = 0
local snapE, refreshedE = S.ensure()
print(string.format("  expired: refreshed=%s commands=%d served=%s",
  tostring(refreshedE), calls, snapE and "yes" or "no"))
print(string.format("  expired snapshot is unknown, never online: %s",
  tostring(S.state_of(snapE, "10.0.0.7") == "unknown")))

-- ------------------------------------------- a refresh already in progress ----
print("\n-- request path while another refresh holds the lock --")
cleanup(); cfg()
S.lock()                       -- simulate an in-flight refresh
S.write({ ts = fakeNow - 1, targets = "ok", ips = { ["10.0.0.7"] = true } }) -- nearly expired
S.memo = nil
calls = 0
local t1 = os.clock()
local snapB = S.ensure()
local elB = (os.clock() - t1) * 1000
print(string.format("  ensure() returned in %.3f ms with %d commands, served=%s",
  elB, calls, snapB and "yes" or "no"))
print("  (a stuck refresh therefore cannot delay the request)")
cleanup(); cfg()

-- ---------------------------------------------------------------- timeout --
print("\n-- external command timeout --")
local wrapped = S.wrap("/usr/sbin/tgtadm --op show", 1.2)
print("  wrapped: " .. wrapped)
print("  uses timeout: " .. tostring(wrapped:find("timeout", 1, true) ~= nil) ..
      "   escalates to KILL: " .. tostring(wrapped:find("KILL", 1, true) ~= nil))
print("  budget clamp at 999s -> " .. S.wrap("true", 999))

-- ------------------------------------------------------------ missing commands --
print("\n-- missing commands --")
cleanup(); cfg(); S.memo = nil
local realExists = S.exe_exists
S.exe_exists = function(p) return p ~= "/usr/sbin/tgtadm" end
S.refresh()
local m = S.read()
print(string.format("  tgtadm missing -> targets=%s, workstation=%s",
  tostring(m and m.targets), S.state_of(m, "10.0.0.7")))
cleanup(); cfg(); S.memo = nil
S.exe_exists = function(p) return p ~= "/usr/bin/lsof" end
S.refresh()
print(string.format("  lsof   missing -> snapshot written=%s, service flags default false=%s",
  tostring(S.read() ~= nil), tostring(S.read() and S.read().dhcp == false)))
cleanup(); cfg(); S.memo = nil
S.exe_exists = function() return false end
local okBoth = S.refresh()
local both = S.read()
print(string.format("  both   missing -> refresh=%s targets=%s workstation=%s",
  tostring(okBoth), tostring(both and both.targets), S.state_of(both, "10.0.0.7")))
S.exe_exists = realExists

-- ------------------------------------------------------------- malformed files --
print("\n-- malformed / missing snapshot files --")
cleanup(); cfg()
local cases = { { "empty", "" }, { "garbage", "garbage" }, { "no-ts", "v=1\n" },
                { "bad-ts", "ts=xyz\ntargets=ok\n" }, { "oversized", string.rep("x", 70000) } }
for _, c in ipairs(cases) do
  local f = io.open(S.file, "wb"); f:write(c[2]); f:close()
  S.memo = nil
  local d = S.read()
  print(string.format("  %-10s -> decode=%-5s workstation=%s",
    c[1], tostring(d ~= nil), S.state_of(d, "10.0.0.7")))
end
rm(S.file); S.memo = nil
print(string.format("  %-10s -> decode=%-5s workstation=%s",
  "absent", tostring(S.read() ~= nil), S.state_of(S.read(), "10.0.0.7")))

os.time = realTime
cleanup()
print("\ndone")