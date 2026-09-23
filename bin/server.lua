#!/usr/bin/lua

local posix = require "posix"
local SOCKET = "/tmp/socket_mkyboot"
local LOG_FILE = "/var/log/mkyboot.log"

local function log_ipc(level, msg)
	local ts = os.date("%Y-%m-%d %H:%M:%S")
	local entry = ts .. " [" .. level .. "] [IPC] " .. msg .. "\n"
	local fd = io.open(LOG_FILE, "a")
	if fd then fd:write(entry); fd:close() end
	io.write(entry)
end

local function sanitize_arg(s)
	if type(s) ~= "string" then return nil end
	if s:find("[;|&`$(){}\n\r]") then return nil end
	if s:find("%.%.") then return nil end
	return s
end

local function validate_dev_path(s)
	if type(s) ~= "string" then return false end
	return s:match("^/dev/nbd%d+$") ~= nil
end

local function validate_file_path(s)
	if type(s) ~= "string" then return false end
	if #s == 0 or #s > 4096 then return false end
	if s:find("[;|&`$(){}\n\r]") then return false end
	if s:find("%.%.") then return false end
	return true
end

local function validate_cache(s)
	if type(s) ~= "string" then return false end
	return s == "none" or s == "unsafe" or s == "writeback" or s == "directsync" or s == "writethrough"
end

local COMMANDS = {}

COMMANDS["nbd_connect"] = function(args)
	if not args.dev or not args.path or not args.cache then
		return false, "missing required arguments: dev, path, cache"
	end
	if not validate_dev_path(args.dev) then
		return false, "invalid device path: must be /dev/nbdN"
	end
	if not validate_file_path(args.path) then
		return false, "invalid file path"
	end
	if not validate_cache(args.cache) then
		return false, "invalid cache mode: must be none/unsafe/writeback/directsync/writethrough"
	end
	local cmd = "/usr/bin/qemu-nbd --fork --connect=" .. args.dev .. " " .. args.path .. " --pid-file=" .. args.path .. ".pid --discard=unmap --cache=" .. args.cache
	local ok = os.execute(cmd)
	if ok then
		log_ipc("INFO", "nbd_connect: " .. args.dev .. " -> " .. args.path .. " cache=" .. args.cache)
		return true, "OK"
	else
		log_ipc("ERROR", "nbd_connect failed: " .. args.dev .. " -> " .. args.path)
		return false, "qemu-nbd connect failed"
	end
end

COMMANDS["nbd_disconnect"] = function(args)
	if not args.dev then
		return false, "missing required argument: dev"
	end
	if not validate_dev_path(args.dev) then
		return false, "invalid device path: must be /dev/nbdN"
	end
	local cmd = "/usr/bin/qemu-nbd -d " .. args.dev .. " 2>/dev/null"
	local ok = os.execute(cmd)
	if ok then
		log_ipc("INFO", "nbd_disconnect: " .. args.dev)
		return true, "OK"
	else
		log_ipc("WARN", "nbd_disconnect failed: " .. args.dev)
		return false, "qemu-nbd disconnect failed"
	end
end

local function parse_request(raw)
	if type(raw) ~= "string" or #raw == 0 then
		return nil, "empty request"
	end
	if #raw > 8192 then
		return nil, "request too large"
	end
	local json_ok, json = pcall(require, "json")
	if not json_ok then
		return nil, "json library not available"
	end
	local decode_ok, parsed = pcall(json.decode, raw)
	if not decode_ok or type(parsed) ~= "table" then
		return nil, "invalid JSON"
	end
	return parsed, nil
end

function StartServerSocket()
	local libsocket = require "socket"
	local libunix = require "socket.unix"
	local socket = assert(libunix())

	assert(socket:bind(SOCKET))
	assert(socket:listen())
	conn = assert(socket:accept())
	while true do
		local data = conn:receive()
		if data == nil then
			conn:close()
			os.remove(SOCKET)
			return
		end

		local request, err = parse_request(data)
		if request == nil then
			log_ipc("WARN", "rejected malformed request: " .. tostring(err))
			conn:send('{"ok":false,"error":"' .. tostring(err) .. '"}\n')
			break
		end

		local op = request.op
		if type(op) ~= "string" or #op == 0 then
			log_ipc("WARN", "rejected request with no operation")
			conn:send('{"ok":false,"error":"missing operation"}\n')
			break
		end

		if #op > 64 then
			log_ipc("WARN", "rejected operation name too long: " .. op:sub(1, 32) .. "...")
			conn:send('{"ok":false,"error":"invalid operation"}\n')
			break
		end

		local handler = COMMANDS[op]
		if handler == nil then
			log_ipc("WARN", "rejected unknown operation: " .. op)
			conn:send('{"ok":false,"error":"unknown operation: ' .. op .. '"}\n')
			break
		end

		local args = request.args or {}
		if type(args) ~= "table" then
			log_ipc("WARN", "rejected invalid args for operation: " .. op)
			conn:send('{"ok":false,"error":"invalid arguments"}\n')
			break
		end

		local ok, result = handler(args)
		if ok then
			conn:send('{"ok":true,"result":"' .. tostring(result) .. '"}\n')
		else
			log_ipc("WARN", "operation " .. op .. " failed: " .. tostring(result))
			conn:send('{"ok":false,"error":"' .. tostring(result) .. '"}\n')
		end

		break
	end
	conn:close()
	os.remove(SOCKET)
end

while true do
	local pf = io.open("/run/mkybootd.pid", "w")
	if pf then pf:write(tostring(posix.getpid().pid)); pf:close() end
	os.remove(SOCKET)

	StartServerSocket()
	print("RESTART SERVER ....")
end
