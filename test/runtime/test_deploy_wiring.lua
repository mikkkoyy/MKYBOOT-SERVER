--[[ MKYBOOT Phase 3 - deployment wiring regression tests
    ------------------------------------------------------------------
    Guards the two defects that made a clean install.sh deployment unable to
    serve anything:

      1. mkyctl.lua runs dofile("/srv/mkyboot/cfg/cfg.lua") at module load,
         but install.sh never installed src/cfg.lua -> nginx returns 500 on
         every request and the admin UI, DHCP generation and the PXE/iPXE
         endpoints are all dead.
      2. mkyctl.lua calls require("image"), but install.sh never installed
         src/image.lua, and package.path pointed at the relative "src/?.lua".
         nginx runs with its own prefix as CWD, so the relative path never
         resolved and the whole image lifecycle raised "module not found".

    These are static checks over install.sh and the Lua sources: they need no
    root, no Ubuntu and no running service.

    Usage:
      lua test/runtime/test_deploy_wiring.lua [repo-root]
--]]

local root = arg[1] or "."
if root == "." then
  root = "./"
elseif root:sub(-1) ~= "/" then
  root = root .. "/"
end

local passed, failed = 0, 0
local function ok(cond, name, extra)
  if cond then passed = passed + 1; print("  PASS  " .. name)
  else
    failed = failed + 1
    print("  FAIL  " .. name .. (extra and ("\n          " .. tostring(extra)) or ""))
  end
end
local function section(s) print("\n== " .. s .. " ==") end

local function read(p)
  local fd = io.open(p, "rb")
  if not fd then return nil end
  local s = fd:read("*a"); fd:close()
  return s
end

local install = read(root .. "install.sh")
local canon   = read(root .. "bin/mkyctl.lua")

ok(install ~= nil, "install.sh is readable")
ok(canon ~= nil, "bin/mkyctl.lua is readable")
if not install or not canon then
  print(string.format("\n%d passed, %d failed", passed, failed))
  os.exit(1)
end

-- --------------------------------------------------------------- cfg.lua --
section("cfg.lua is installed: mkyctl.lua dofile()s it at module load")
ok(canon:find('dofile("/srv/mkyboot/cfg/cfg.lua")', 1, true) ~= nil,
   "mkyctl.lua really does dofile /srv/mkyboot/cfg/cfg.lua")
ok(install:find("src/cfg%.lua%s+/srv/mkyboot/cfg/cfg%.lua") ~= nil,
   "install.sh installs src/cfg.lua -> /srv/mkyboot/cfg/cfg.lua",
   "missing: the server cannot load its configuration module")

-- ------------------------------------------------------------- image.lua --
section("image.lua is installed: mkyctl.lua require()s it")
local requires = {}
for m in canon:gmatch('require%("([%w_]+)"%)') do requires[m] = (requires[m] or 0) + 1 end
ok(requires["image"] ~= nil, "mkyctl.lua calls require(\"image\")",
   "count=" .. tostring(requires["image"]))
ok(install:find("src/image%.lua") ~= nil,
   "install.sh installs src/image.lua",
   "missing: require(\"image\") raises module not found")
ok(install:find("src/image%.lua /srv/mkyboot/modules/image%.lua") ~= nil,
   "image.lua is installed next to mkyctl.lua in /srv/mkyboot/modules/",
   "mkyctl.lua resolves sibling modules relative to its own directory")

-- ----------------------------------------------------------- package.path --
section("package.path is resolved relative to the file, not the CWD")
-- A relative entry such as "src/?.lua" resolves against the process working
-- directory, which for nginx is its own prefix - never the source tree.
ok(canon:find('package%.path = package%.path %.. "%;src/%?%.lua"', 1, true) == nil,
   "the CWD-relative package.path entry has been removed")
ok(canon:find("package.path", 1, true) ~= nil, "package.path is still set")
ok(canon:find("debug%.getinfo") ~= nil,
   "module directory is derived from the script's own path")
ok(canon:find("/srv/mkyboot/modules/", 1, true) ~= nil,
   "a documented install-path fallback is present",
   "needed when debug.getinfo is unavailable")

-- ------------------------------------------------------- required binaries --
section("commands the status refresh shells out to are installed")
-- A missing binary must not be fatal: status.refresh() records
-- targets="missing" and reports workstations as unknown. Verified at runtime by
-- test_status_cache.lua; here we only confirm the install script provides them.
for _, bin in ipairs({ "tgt", "tftpd%-hpa", "isc%-dhcp%-server", "lua%-filesystem" }) do
  ok(install:find(bin) ~= nil, "install.sh installs " .. bin:gsub("%%", ""))
end
ok(install:find("nginx%-extras") ~= nil, "install.sh installs nginx-extras (OpenResty/Lua)")

-- --------------------------------------------------------- config written --
section("the runtime status snapshot directory exists after install")
ok(install:find("mkdir %-p /srv/mkyboot/cfg") ~= nil, "/srv/mkyboot/cfg is created")
ok(install:find("mkdir %-p /srv/mkyboot/modules") ~= nil, "/srv/mkyboot/modules is created")
-- status.cache / status.lock are written into /srv/mkyboot/cfg at runtime, so
-- nginx must be able to write there.
ok(install:find("chmod 700 /srv/mkyboot/cfg/sessions") ~= nil,
   "sessions dir permissions are tightened")

-- ------------------------------------------------------ module consistency --
section("all three Lua copies stay consistent")
local srv  = read(root .. "srv/mkyboot/mkyctl.lua")
local test = read(root .. "test/srv/mkyboot/mkyctl.lua")
if srv and test then
  for _, p in ipairs({ "bin/mkyctl.lua", "srv/mkyboot/mkyctl.lua", "test/srv/mkyboot/mkyctl.lua" }) do
    local t = read(root .. p)
    ok(t ~= nil and t:find("NON%-BLOCKING WORKSTATION") ~= nil,
       p .. " carries the non-blocking status module")
    ok(t ~= nil and t:find('arg_api == "server"') ~= nil,
       p .. " carries the ?api=server control handler")
  end
  -- The test fixture intentionally keeps the inline qemu-img lifecycle: the
  -- image refactor lives only in bin/ and srv/.
  ok(canon:find('require%("image"%)') ~= nil, "bin delegates the image lifecycle to the module")
  ok(srv:find('require%("image"%)') ~= nil, "srv delegates the image lifecycle to the module")
  ok(test:find('require%("image"%)') == nil,
     "test fixture keeps its inline qemu-img lifecycle (intentional)")
  ok(test:find("qemu%-img") ~= nil, "test fixture still has the inline qemu-img calls")
end

print(string.format("\n%d passed, %d failed", passed, failed))
if failed > 0 then os.exit(1) end
os.exit(0)