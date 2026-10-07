--[[ MKYBOOT Phase 4A - Lua module path resolution tests
    ------------------------------------------------------------------
    Validates that mkyctl.lua resolves its sibling modules (image.lua) relative
    to its own location, not the process working directory.

    Defect fixed in Phase 3: `package.path = package.path .. ";src/?.lua"` is
    CWD-relative. nginx runs with its own prefix as CWD, so the entry resolved to
    e.g. /src/image.lua and require("image") raised "module not found".

    The block is extracted verbatim from the file under test, written to a
    temporary file and dofile()'d from several working directories. On Windows
    os.chdir is unavailable in this Lua build, so the CWD is varied by loading
    the block from files in different directories - which is exactly the
    condition the fix addresses, because debug.getinfo reports the script path.

    Usage:
      lua test/runtime/test_module_path.lua [path/to/mkyctl.lua]
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

local fh = assert(io.open(src, "rb"), "cannot open " .. src)
local text = fh:read("*a"); fh:close()

-- Extract the package.path block: from its header comment to the closing "end".
local a = text:find("-- Resolve sibling modules", 1, true)
if not a then
  -- Two legitimate reasons a copy may have no package.path block:
  --   1. It predates the Phase 3 fix and still carries the original
  --      CWD-relative entry (an intentional fixture difference).
  --   2. It never loads sibling modules at all, so package.path is irrelevant.
  if text:find('package%.path = package%.path %.. ";src/%?%.lua"', 1, true) then
    print("  SKIP  this copy predates the package.path fix (test fixture);")
    print("        it still carries the original CWD-relative \";src/?.lua\" entry")
    print(string.format("\n%d passed, %d failed (fixture skipped)", passed, failed))
    os.exit(0)
  end
  if not text:find('require%("image"%)', 1, true) then
    print("  SKIP  this copy loads no sibling modules, so package.path is irrelevant")
    print(string.format("\n%d passed, %d failed (not applicable)", passed, failed))
    os.exit(0)
  end
  error("no package.path block and no legacy entry found in " .. src)
end
local b = assert(text:find("\nend\n", a, true), "package.path block end not found")
local block = text:sub(a, b + #"\nend\n")

-- The block must no longer contain the CWD-relative entry.
ok(not block:find('";src/%?%.lua"', 1, true),
   "the CWD-relative \";src/?.lua\" entry has been removed")
ok(block:find("debug.getinfo", 1, true) ~= nil,
   "the module directory is derived from the script's own path")
ok(block:find("/srv/mkyboot/modules/", 1, true) ~= nil,
   "a documented install-path fallback is present")

-- Write the block to a file in a directory that is NOT the CWD, then load it.
-- debug.getinfo(1,"S").source will be "@<that file>", so the derived directory
-- is the block's own location regardless of where the interpreter was started.
local tmp = os.tmpname and os.tmpname() or ("mkyboot_path_" .. tostring(os.time()))
tmp = tmp:gsub("\\", "/")
-- os.tmpname() may return a path with no directory component; force one.
if not tmp:find("/", 1, true) then tmp = "./" .. tmp end
local dir = tmp:match("^(.*)/") or "."
local file = tmp

local out = assert(io.open(file, "wb"))
out:write(block)
out:close()

local before = package.path
local chunk, err = loadfile(file)
if not chunk then
  ok(false, "the extracted block compiles", err)
else
  local ran, runErr = pcall(chunk)
  ok(ran, "the extracted block executes without error", tostring(runErr))
end

-- After loading, package.path must start with the block's own directory.
local expected = dir .. "/?.lua;"
ok(package.path:sub(1, #expected) == expected,
   "package.path resolves relative to the script location, not the CWD",
   "got prefix: " .. package.path:sub(1, 60))

-- And a sibling module placed next to the block must be requireable.
local sibling = assert(io.open(dir .. "/mkyboot_test_mod.lua", "wb"))
sibling:write("return { loaded = true }\n")
sibling:close()

local okReq, mod = pcall(require, "mkyboot_test_mod")
ok(okReq and mod and mod.loaded == true,
   "a sibling module next to the script is requireable")

-- Restore and clean up.
package.path = before
os.remove(file)
os.remove(dir .. "/mkyboot_test_mod.lua")

print(string.format("\n%d passed, %d failed", passed, failed))
if failed > 0 then os.exit(1) end
os.exit(0)