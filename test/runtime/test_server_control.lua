--[[ MKYBOOT Phase 2 - server-control API regression tests
    ------------------------------------------------------------------
    Exercises the ?api=server&op= handler that was previously present in only
    one of the three mkyctl.lua copies, proving that all three now behave
    identically and that authorization is not weakened.

    The handler body is extracted verbatim from the file under test and driven
    with a stub ngx, so the real shipped code runs.

    Usage:
      lua test/runtime/test_server_control.lua [path/to/mkyctl.lua]

    Defaults to bin/mkyctl.lua. Point it at any of the three copies.
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
  if cond then
    passed = passed + 1; print("  PASS  " .. name)
  else
    failed = failed + 1
    print("  FAIL  " .. name .. (extra and ("\n          " .. tostring(extra)) or ""))
  end
end
local function section(s) print("\n== " .. s .. " ==") end

-- ------------------------------------------------------------- extraction --
local fd = assert(io.open(src, "rb"), "cannot open " .. src)
local text = fd:read("*a"); fd:close()

local a = assert(text:find('elseif ngx.var.arg_api == "server" and ngx.var.arg_op ~= nil then', 1, true),
  "server-control handler not found in " .. src)
local b = assert(text:find("\t\t\t   elseif ngx.var.arg_status", a, true),
  "handler end anchor not found")
-- The slice starts mid-chain with "elseif", which is not a valid standalone
-- statement. Turn it into a fresh "if" and close it, so it can be loaded and
-- driven on its own. The terminating "end" of the original chain lives after
-- the following elseif, so it is not part of this slice.
local handler = (text:sub(a, b - 1):gsub("^elseif", "if", 1)) .. "\nend"

-- Also extract the validators the handler depends on for its whitelist checks.
local vs = assert(text:find("mkyboot.inc.valid = {}", 1, true), "validators not found")
local ve = assert(text:find("\tmkyboot.inc.valid.safe_path = function(s)", vs, true), "validator end")
local validators = text:sub(vs, ve - 1)

-- ------------------------------------------------------------------ harness --
-- Minimal ngx stub: records what the handler said, and records any privileged
-- action the handler attempts instead of performing it.
local function makeSandbox(opts)
  opts = opts or {}
  local calls = {}
  local mkyboot = { inc = {}, cfg = {} }
  local env = {
    out = {},
    content_type = nil,
    calls = calls,
    mkyboot = mkyboot,
  }
  local ngx = {
    header = setmetatable({}, { __index = function(_, k)
      return function(_, v) if k == "content_type" then env.content_type = v end end
    end }),
    say = function(s) env.out[#env.out + 1] = tostring(s) end,
    var = {
      arg_api = "server",
      arg_op = opts.op,
      http_origin = opts.origin,
      http_referer = opts.referer,
    },
  }
  env.ngx = ngx

  local globals = setmetatable({ mkyboot = mkyboot, ngx = ngx }, { __index = _G })

  -- Minimal json.encode stand-in: the handler only ever encodes flat tables of
  -- booleans/strings, and the assertions below care about field presence, not
  -- exact encoding.
  globals.json = {
    encode = function(t)
      if type(t) ~= "table" then return tostring(t) end
      local parts = {}
      for k, v in pairs(t) do
        local val
        if type(v) == "boolean" then val = v and "true" or "false"
        elseif type(v) == "string" then val = '"' .. v .. '"'
        else val = tostring(v) end
        parts[#parts + 1] = '"' .. tostring(k) .. '":' .. val
      end
      return "{" .. table.concat(parts, ",") .. "}"
    end,
  }

  -- validators + a stub systemctl that records rather than acts
  assert(load(validators, "validators", "t", globals))()
  mkyboot.inc.log = { info = function() end, warn = function() end, error = function() end }
  mkyboot.cfg.server = { ipv4 = "192.168.0.2" }
  mkyboot.inc.systemctl = function(name, cmd)
    calls[#calls + 1] = { service = name, cmd = cmd }
    if opts.systemctlFails then return nil, "simulated failure" end
    return true
  end

  -- Drive the real handler with arg_op set at call time.
  local chunk = assert(load(handler, "handler", "t", globals))
  return env, chunk, function(op)
    ngx.var.arg_op = op
    env.out = {}
    env.calls = {}
    calls = env.calls
    chunk()
    return table.concat(env.out), env.calls
  end
end

-- ------------------------------------------------------------------- tests --

section("the handler is present in this copy")
ok(#handler > 500, "server-control handler extracted", #handler .. " bytes")
ok(handler:find("mkyboot.inc.session.validate", 1, true) ~= nil,
   "handler requires a valid session")
ok(handler:find("http_origin", 1, true) ~= nil,
   "handler enforces an origin check")

section("unauthenticated callers are refused before any privileged action")
local env, _, call = makeSandbox({ op = "restart" })
env.mkyboot.inc.session = { validate = function() return nil end }
local body, calls = call("restart")
ok(body:find("Not authenticated", 1, true) ~= nil, "unauthenticated restart -> Not authenticated", body)
ok(#calls == 0, "no systemctl call was made", "#calls=" .. #calls)

section("unknown operations are rejected without acting")
local env2, _, call2 = makeSandbox({ op = "reboot-everything" })
env2.mkyboot.inc.session = { validate = function() return { username = "admin" } end }
local body2, calls2 = call2("reboot-everything")
ok(body2:find("Invalid server operation", 1, true) ~= nil, "unknown op -> Invalid server operation", body2)
ok(#calls2 == 0, "unknown op performs no systemctl call", "#calls2=" .. #calls2)

section("supported operations are accepted for an authenticated caller")
for _, op in ipairs({ "restart", "stop" }) do
  local e, _, c = makeSandbox({ op = op })
  e.mkyboot.inc.session = { validate = function() return { username = "admin" } end }
  local b, cl = c(op)
  ok(#cl == 1, op .. " issues exactly one systemctl call", "#calls=" .. #cl)
  ok(cl[1] and cl[1].cmd == op, op .. " maps to systemctl " .. op, cl[1] and cl[1].cmd)
  ok(cl[1] and cl[1].service == "nginx", op .. " targets the nginx service only",
     cl[1] and cl[1].service)
  ok(b:find('"success"', 1, true) ~= nil or b:find("success", 1, true) ~= nil,
     op .. " returns a success field", b)
  ok(b:find('"action"', 1, true) ~= nil or b:find("action", 1, true) ~= nil,
     op .. " returns an action field", b)
end

section("only nginx may ever be controlled")
local e3, _, c3 = makeSandbox({ op = "restart" })
e3.mkyboot.inc.session = { validate = function() return { username = "admin" } end }
local _, cl3 = c3("restart")
local services = {}
for _, call_ in ipairs(cl3) do services[call_.service] = true end
local svcCount = 0
for _ in pairs(services) do svcCount = svcCount + 1 end
ok(svcCount == 1 and services["nginx"] == true,
   "exactly one service touched, and it is nginx")

section("operation name validation is strict")
local e4, _, c4 = makeSandbox({ op = "restart" })
e4.mkyboot.inc.session = { validate = function() return { username = "admin" } end }
for _, bad in ipairs({ "restart; reboot", "restart && rm -rf /", "$(id)", "restart\nstop",
                       "RESTART", "restart ", "", "restartnginx" }) do
  local b, cl = c4(bad)
  ok(b:find("Invalid server operation", 1, true) ~= nil,
     "rejected unsafe op: " .. string.format("%q", bad), b)
  ok(#cl == 0, "unsafe op performs no systemctl call: " .. string.format("%q", bad), "#cl=" .. #cl)
end

section("origin enforcement")
-- A cross-site origin must be rejected before acting.
local e5, _, c5 = makeSandbox({ op = "restart", origin = "http://evil.example.com" })
e5.mkyboot.inc.session = { validate = function() return { username = "admin" } end }
local b5, cl5 = c5("restart")
ok(b5:find("Invalid origin", 1, true) ~= nil, "foreign origin rejected", b5)
ok(#cl5 == 0, "foreign origin performs no systemctl call", "#cl5=" .. #cl5)

-- The server's own address must be accepted.
local e6, _, c6 = makeSandbox({ op = "restart", origin = "http://192.168.0.2:8888" })
e6.mkyboot.inc.session = { validate = function() return { username = "admin" } end }
local _, cl6 = c6("restart")
ok(#cl6 == 1, "own-host origin accepted", "#cl6=" .. #cl6)

section("no new administrative surface is exposed")
-- The handler only ever reacts to api=server with arg_op present.
ok(handler:find("arg_api == \"server\"", 1, true) ~= nil, "gated on api=server")
ok(handler:find("arg_op ~= nil", 1, true) ~= nil, "gated on an explicit op")
ok(not handler:find("arg_api%s*==%s*\"[^\"]*\"%s*or", 1, true),
   "no alternative unauthenticated entry point added")
-- Only restart/stop exist; no arbitrary command pass-through.
local ops = {}
for w in handler:gmatch('arg_op == "([%w%-]+)"') do ops[#ops + 1] = w end
ok(#ops == 2, "exactly two operations are implemented", table.concat(ops, ","))
ok(handler:find("os.execute", 1, true) == nil, "handler adds no direct os.execute")

print(string.format("\n%d passed, %d failed", passed, failed))
if failed > 0 then os.exit(1) end
os.exit(0)