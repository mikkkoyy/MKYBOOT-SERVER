-- image.lua
-- MKYBOOT Image Lifecycle Module
-- Extracted from mkyboot.cmd.img table and mkyboot:mkChild()/rmChild() logic
-- Preserves exact behavior; callers in mkyctl.lua remain responsible for
-- WKS state, lock files, NBD assignment, and ngx.say() API output.
-- 
-- Dependencies (available via mkyboot global):
--   mkyboot.inc.valid.img_path, mkyboot.inc.valid.img_size,
--   mkyboot.inc.valid.safe_path, mkyboot.inc.log.warn
--   io.popen, os.execute, os.remove

local M = {}

-- 1. Create QCOW2 image
-- Preserves: qemu-img create command and options exactly
-- Command: /usr/bin/qemu-img -f qcow2 -o preallocation=metadata,compat=1.1,lazy_refcounts=on encryption=off <path> <size>
function M.new(p_path, p_size)
	if type(p_path) ~= "string" or #p_path == 0 then
		mkyboot.inc.log.warn("SECURITY", "image.new: invalid path")
		return false
	end
	if type(p_size) ~= "string" or not mkyboot.inc.valid.img_size(p_size) then
		mkyboot.inc.log.warn("SECURITY", "image.new: invalid size")
		return false
	end
	return os.execute("/usr/bin/qemu-img -f qcow2 -o preallocation=metadata,compat=1.1,lazy_refcounts=on encryption=off " .. p_path .. " " .. p_size)
end

-- 2. Create child image with backing file
-- Preserves: qemu-img create -f qcow2 -b <parent> <child> exactly
-- 2>>/tmp/result for error output handling
function M.child(p_parent, p_child)
	if type(p_parent) ~= "string" or #p_parent == 0 then
		mkyboot.inc.log.warn("SECURITY", "image.child: invalid parent")
		return false
	end
	if type(p_child) ~= "string" or #p_child == 0 then
		mkyboot.inc.log.warn("SECURITY", "image.child: invalid child")
		return false
	end
	return os.execute("/usr/bin/qemu-img create -f qcow2 -b " .. p_parent .. " " .. p_child .. " -o lazy_refcounts=on 2>/tmp/result")
end

-- 3. Delete image
-- Preserves: os.remove(<image>) exactly; valid.safe_path check
function M.delete(p_image)
	if type(p_image) ~= "string" or #p_image == 0 then
		mkyboot.inc.log.warn("SECURITY", "image.delete: invalid path")
		return false
	end
	if not mkyboot.inc.valid.safe_path(p_image) then
		mkyboot.inc.log.warn("SECURITY", "image.delete: unsafe path=" .. p_image)
		return false
	end
	return os.remove(p_image)
end

-- 4. Commit (writeback)
-- Preserves: qemu-img commit exactly; captures stdout+stderr via io.popen
function M.commit(p_image)
	if type(p_image) ~= "string" or #p_image == 0 then
		mkyboot.inc.log.warn("SECURITY", "image.commit: invalid path")
		return false
	end
	if not mkyboot.inc.valid.safe_path(p_image) then
		mkyboot.inc.log.warn("SECURITY", "image.commit: unsafe path=" .. p_image)
		return false
	end
	local fd = io.popen("/usr/bin/qemu-img commit " .. p_image .. " 2>&1")
	local result
	if fd then
		result = fd:read("a*")
		fd:close()
	else
		result = ""
	end
	return result
end

-- 5. Usage detection
-- Preserves: lsof -t <image> 2>/dev/null exactly; valid.safe_path check
-- Returns: true if process using the image is found (lsof returns non-empty)
function M.used(p_image)
	if type(p_image) ~= "string" or #p_image == 0 then
		mkyboot.inc.log.warn("SECURITY", "image.used: invalid path")
		return false
	end
	if not mkyboot.inc.valid.safe_path(p_image) then
		mkyboot.inc.log.warn("SECURITY", "image.used: unsafe path=" .. p_image)
		return false
	end
	local fd = io.popen("/usr/bin/lsof -t " .. p_image .. " 2>/dev/null")
	local success
	if fd then
		local data = fd:read("a*")
		fd:close()
		success = #data > 0
	else
		success = false
	end
	return success
end

return M