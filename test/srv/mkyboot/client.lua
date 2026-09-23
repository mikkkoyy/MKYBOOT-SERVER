#!/usr/bin/lua

local SOCKET = "/tmp/socket_mkyboot"

local function isFile(name)
	local posix = require("posix")
	if name ~= nil and posix.stat(name) ~= nil then return true else return false end
end

local function WaitSocketReady()
	local attempts = 0
	while not isFile(SOCKET) do
		require("posix.unistd").sleep(0.5)
		attempts = attempts + 1
		if attempts > 60 then
			io.stderr:write("ERROR: socket_mkyboot not found after 30 seconds\n")
			os.exit(1)
		end
	end
end

local function SendCommand(op, args)
	local socket = require"socket"
	socket.unix = require"socket.unix"
	local c = assert(socket.unix())
	assert(c:connect(SOCKET))

	local json = require("json")
	local request = json.encode({ op = op, args = args })
	c:send(request .. "\n")

	local response = c:receive()
	c:close()

	if response then
		local ok, parsed = pcall(json.decode, response)
		if ok and parsed.ok then
			return true, parsed.result or "OK"
		else
			return false, parsed and parsed.error or "unknown error"
		end
	end
	return false, "no response"
end

if arg[1] ~= nil and arg[2] ~= nil and arg[3] ~= nil then
	WaitSocketReady()
	local ok, result = SendCommand("nbd_connect", {
		dev = arg[1],
		path = arg[2],
		cache = arg[3]
	})
	if ok then
		print("OK: " .. tostring(result))
	else
		io.stderr:write("ERROR: " .. tostring(result) .. "\n")
		os.exit(1)
	end
end
