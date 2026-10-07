--[[ MKYBOOT Phase 4A - DHCP export regression tests
    ------------------------------------------------------------------
    Guards the Phase 4A fix for the 32-bit UEFI boot filename.

    Defect: ExportDHCP() emitted `filename "<fileboot>32.efi"` unconditionally,
    but srv/tftp/ ships only ipxe.efi and ipxe.kpxe. A 32-bit UEFI client was
    therefore handed a filename that does not exist in TFTP and failed to boot.

    The fix makes the 32-bit UEFI filename conditional on the file actually
    existing in the TFTP root, falling back to the 64-bit image (with a comment)
    when it is not installed. The reference is NOT removed: an operator who
    builds ipxe32.efi from the vendored src/ipxe tree still gets the 32-bit path.

    These tests extract ExportDHCP() verbatim from the file under test and drive
    it with stubbed I/O, so no DHCP server, no /srv and no real config are needed.

    Usage:
      lua test/runtime/test_dhcp_export.lua [path/to/mkyctl.lua]
--]]

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
local fh = assert(io.open(src, "rb"), "cannot open " .. src)
local text = fh:read("*a"); fh:close()

local a = assert(text:find("function mkyboot:ExportDHCP()", 1, true), "ExportDHCP not found")
local b = assert(text:find("\n\t\t\tend", a, true), "ExportDHCP end not found")
local fn = text:sub(a, b + #"\n\t\t\tend")

-- ------------------------------------------------------------------ harness --
-- Captured output of the generated dhcpd.conf.
local OUT = {}

local mkyboot = { inc = {}, cfg = {} }
mkyboot.inc.log = { info = function() end, warn = function() end, error = function() end }

-- isFile is driven by a table the test controls, so the 32-bit UEFI branch can
-- be exercised in both directions without touching the filesystem.
local FILES = {}
mkyboot.inc.isFile = function(p) return FILES[p] == true end
mkyboot.lib = { posix = { stat = function(p) return FILES[p] and {} or nil end } }

mkyboot.inc.checkconf = function() return true end

-- Minimal configuration covering every key ExportDHCP() reads.
mkyboot.cfg.server = { vendor = "Nuke Technology LLC", version = "4.0.0" }
mkyboot.cfg.tftp = { workdir = "/srv/tftp" }
mkyboot.cfg.dhcp = {
  workdir = "/etc/dhcp",
  config = {
    global = { authoritative = "" },
    opt = { ["domain-name"] = "mkyboot.local" },
    sub = { { sub = "192.168.0.0", mask = "255.255.255.0", ranges = { "192.168.0.10 192.168.0.149" } } },
    ipxe = "option ipxe.no-pxedhcp 1;",
  },
}
mkyboot.cfg.wks = {
  { name = "PC003", mac = "b4:2e:99:2c:dd:52", ipv4 = "192.168.0.3",
    enable = "1", fileboot = "ipxe", opt = {} },
}

-- Redirect the generated file into memory.
-- os.date("%D%T") is a strftime extension that plain Lua does not implement
-- (LuaJIT/OpenResty does). ExportDHCP only uses it for a backup filename, so a
-- fixed value is sufficient here.
os.date = function() return "010126-000000" end

local realOpen = io.open
io.open = function(path, mode)
  if path == mkyboot.cfg.dhcp.workdir .. "/dhcpd.conf" then
    if mode:find("r") then return nil, "not found" end
    return { write = function(_, ...) OUT[#OUT + 1] = table.concat({ ... }, "") end,
             close = function() end }
  end
  return realOpen(path, mode)
end

local env = setmetatable({ mkyboot = mkyboot, os = os }, { __index = _G })
assert(load(fn, "ExportDHCP", "t", env))()

local function run()
  OUT = {}
  mkyboot:ExportDHCP()
  return table.concat(OUT)
end

-- ------------------------------------------------------------------- tests --
section("ExportDHCP generates a complete dhcpd.conf")
local out = run()
ok(out:find("host PC003", 1, true) ~= nil, "host block emitted")
ok(out:find("hardware ethernet b4:2e:99:2c:dd:52", 1, true) ~= nil, "MAC address emitted")
ok(out:find("fixed-address 192.168.0.3", 1, true) ~= nil, "fixed address emitted")
ok(out:find("subnet 192.168.0.0", 1, true) ~= nil, "subnet emitted")
ok(out:find("option ipxe.no-pxedhcp 1", 1, true) ~= nil, "iPXE options emitted")

section("32-bit UEFI filename is offered only when the file is installed")
-- Not installed (the default): must NOT offer ipxe32.efi.
local without = run()
ok(without:find('filename "ipxe32.efi"', 1, true) == nil,
   "ipxe32.efi is not offered when it is not installed",
   "found ipxe32.efi in generated config")
ok(without:find("filename \"ipxe.efi\"", 1, true) ~= nil,
   "falls back to the 64-bit UEFI image")
ok(without:find("32.efi not installed", 1, true) ~= nil,
   "the fallback is explained in a comment")

-- Installed: the 32-bit path must be offered exactly as before.
FILES["/srv/tftp/ipxe32.efi"] = true
local with32 = run()
ok(with32:find("filename \"ipxe32.efi\"", 1, true) ~= nil,
   "ipxe32.efi is offered when it is installed")
ok(with32:find("32.efi not installed", 1, true) == nil,
   "no fallback comment when the 32-bit image is present")
FILES["/srv/tftp/ipxe32.efi"] = nil

section("BIOS and 64-bit UEFI paths are unaffected")
ok(without:find("filename \"ipxe.kpxe\"", 1, true) ~= nil,
   "BIOS clients still get ipxe.kpxe")
ok(without:find("filename \"ipxe.efi\"", 1, true) ~= nil,
   "64-bit UEFI clients still get ipxe.efi")

section("a missing TFTP workdir does not break export")
mkyboot.cfg.tftp.workdir = nil
local nodir = run()
ok(nodir:find("host PC003", 1, true) ~= nil,
   "export still works when tftp.workdir is unset")
ok(nodir:find('filename "ipxe32.efi"', 1, true) == nil,
   "no 32-bit filename offered when the TFTP dir is unknown")
mkyboot.cfg.tftp.workdir = "/srv/tftp"

section("multiple workstations each get their own host block")
mkyboot.cfg.wks[2] = { name = "PC004", mac = "b4:2e:99:2c:dd:df", ipv4 = "192.168.0.4",
                       enable = "1", fileboot = "ipxe", opt = {} }
local multi = run()
ok(multi:find("host PC003", 1, true) ~= nil and multi:find("host PC004", 1, true) ~= nil,
   "both hosts emitted")
ok(multi:find('filename "ipxe32.efi"', 1, true) == nil,
   "neither host offers the missing 32-bit image")

io.open = realOpen
print(string.format("\n%d passed, %d failed", passed, failed))
if failed > 0 then os.exit(1) end
os.exit(0)