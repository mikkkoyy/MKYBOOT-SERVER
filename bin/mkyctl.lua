---#!/usr/bin/lua


--tgtadm --lld iscsi --op new --mode target --tid 1 -T 											#CREATE TARGET
--lld iscsi --op new --mode target --tid --lun 													#ADD LUN
--tgtadm --lld iscsi --op new --mode logicalunit --tid 1 --lun 1 -b /dev/nbd1 					#ADD LUN
--lld iscsi --op delete --force --mode logicalunit --tid --lun 									#REMOVE LUN
--tgtadm --lld iscsi --op show --mode target 													#SHOW TARGET
--lld iscsi --op delete --force --mode target --tid                                            	#REMOVE TARGET FORCE
--lsof -i TCP@0.0.0.0:3260 																		#ибо — скомбинировать все эти ключи
--lsof -i :68 																					#Например, отобразить сервисы, прослушивающие порт 22 и/или уже установленные соединения на этом порту:
--tostring
--[[             INSTALL COMPONENTS                ]]
-- apt install etherwake shellinabox qemu-utils
--[[   

				_____________________________________________________________________
				[																	]
				[                 CREATE NEW WORKSTATION 							]
				---------------------------------------------------------------------
				|																	|
				|   ENABLE 					[X] 									|
				|   TARGET ID 				[1-500 \/] 								|
				|	HOSTNAME 				[                                    ]  |
				|   GROUP 					[ DEFAULT 						  \/ ]  |
				| ----------------------------------------------------------------- |	
				|	IP ADDRESS 				[                                    ]  |
				|   MAC ADDRESS 			[                                    ]  |
				|   GATEWAY 				[                                    ]  |
				|   DNS SERVERS 			[                                    ]  |
				|   DOMAIN SEARCH			[                                    ]  |	
				| ----------------------------------------------------------------- |
				|   IMAGE [selectid] SELECT [ IMG 1 [IMG 1]/[IMG 2]/[IMG 3]   \/ ]  |
				|   IMAGE [selectid] TYPE 	[device/iso/disk 				  \/ ]  |		
 				|   IMAGE [selectid] NAME	[ 									 ] 	|			
 				|   IMAGE [selectid] ENABLE	[X] 									|
				|   IMAGE [selectid] CACHE 	[none/unsafe/writeback 			  \/ ]  |
				| ----------------------------------------------------------------- |
				|   SELECT BOOT 1 			[                                 \/ ]  |
				|   SELECT BOOT 2 			[  NONE                           \/ ]  |
				|   SELECT BOOT 3 			[  NONE                           \/ ]  |
				|   PXE FILE 				[  ipxe 							 ]  |
				|   HARDWARE PROFILE 		[  NONE 							 ]  |
				|--------------------------------------------------------------------

 ]]--


--[[#>
	[[=============================================================================================================================================================================================]]
	 	mkyboot={}
	  	mkyboot.cmd = {}
	  	mkyboot.lib = {}
	  	mkyboot.inc = {}
	  	mkyboot.web = {}
	  	mkyboot.bin = {}
	  	mkyboot.cfg = dofile("/srv/mkyboot/cfg/cfg.lua").cfg
	--[[===========================================================================================================================================================================================]]
	--[[ SECURITY: Input validation and sanitization 																				]]
	--[[===========================================================================================================================================================================================]]
		mkyboot.inc.log = {}
		mkyboot.inc.log.file = "/var/log/mkyboot.log"
		mkyboot.inc.log.write = function(level, subsystem, message)
			local ts = os.date("%Y-%m-%d %H:%M:%S")
			local entry = ts .. " [" .. level .. "] [" .. subsystem .. "] " .. message .. "\n"
			local fd = io.open(mkyboot.inc.log.file, "a")
			if fd then fd:write(entry); fd:close() end
			if mkyboot.cfg.server and tostring(mkyboot.cfg.server.debug) == "1" then
				io.write(entry)
			end
		end
		mkyboot.inc.log.info = function(sub, msg) mkyboot.inc.log.write("INFO", sub, msg) end
		mkyboot.inc.log.warn = function(sub, msg) mkyboot.inc.log.write("WARN", sub, msg) end
		mkyboot.inc.log.error = function(sub, msg) mkyboot.inc.log.write("ERROR", sub, msg) end

		mkyboot.inc.valid = {}
		mkyboot.inc.valid.mac = function(s)
			if type(s) ~= "string" then return false end
			return s:match("^%x%x:%x%x:%x%x:%x%x:%x%x:%x%x$") ~= nil
		end
		mkyboot.inc.valid.ipv4 = function(s)
			if type(s) ~= "string" then return false end
			local a,b,c,d = s:match("^(%d+)%.(%d+)%.(%d+)%.(%d+)$")
			if not a then return false end
			a,b,c,d = tonumber(a),tonumber(b),tonumber(c),tonumber(d)
			return a and b and c and d and a>=0 and a<=255 and b>=0 and b<=255 and c>=0 and c<=255 and d>=0 and d<=255
		end
		mkyboot.inc.valid.hostname = function(s)
			if type(s) ~= "string" or #s == 0 or #s > 63 then return false end
			return s:match("^[%w%-%.]+$") ~= nil
		end
		mkyboot.inc.valid.iqn = function(s)
			if type(s) ~= "string" then return false end
			return s:match("^[%w%-%.:]+$") ~= nil
		end
		mkyboot.inc.valid.path = function(s)
			if type(s) ~= "string" or #s == 0 then return false end
			if s:find("%.%.") or s:find(";") or s:find("|") or s:find("&") or s:find("`") or s:find("%$") then return false end
			return true
		end
		mkyboot.inc.valid.tid = function(n)
			local v = tonumber(n)
			return v ~= nil and v >= 1 and v <= 500
		end
		mkyboot.inc.sanitize = function(s)
			if type(s) ~= "string" then return "" end
			return s:gsub("[\"'\\;|&`$]", "")
		end
		mkyboot.inc.parse_post = function(body)
			if not body or body == "" then return nil end
			local ok, result = pcall(function()
				local tbl = {}
				for k, v in body:gmatch("([^&=]+)=([^&]*)") do
					tbl[k] = v
				end
				return tbl
			end)
			if ok then return result else return nil end
		end
	--[[ SECURITY: Input validation for shell command parameters  ]]
		mkyboot.inc.valid.dev_path = function(s)
			if type(s) ~= "string" then return false end
			return s:match("^/dev/nbd%d+$") ~= nil
		end
		mkyboot.inc.valid.lun_id = function(n)
			local v = tonumber(n)
			return v ~= nil and v >= 0 and v <= 255
		end
		mkyboot.inc.valid.pid = function(s)
			if type(s) ~= "string" then return false end
			return s:match("^%d+$") ~= nil and tonumber(s) > 0
		end
		mkyboot.inc.valid.img_size = function(s)
			if type(s) ~= "string" then return false end
			return s:match("^%d+[GMK]?$") ~= nil
		end
		mkyboot.inc.valid.cache_mode = function(s)
			if type(s) ~= "string" then return false end
			return s == "none" or s == "unsafe" or s == "writeback" or s == "directsync" or s == "writethrough"
		end
		mkyboot.inc.valid.service_name = function(s)
			if type(s) ~= "string" then return false end
			return s == "isc-dhcp-server" or s == "tftpd-hpa" or s == "tgt" or s == "nginx" or s == "mkybootd"
		end
		mkyboot.inc.valid.systemctl_cmd = function(s)
			if type(s) ~= "string" then return false end
			return s == "start" or s == "stop" or s == "restart" or s == "status" or s == "enable" or s == "disable"
		end
		mkyboot.inc.valid.safe_path = function(s)
			if type(s) ~= "string" or #s == 0 or #s > 4096 then return false end
			if s:find("[;|&`$(){}\n\r]") then return false end
			if s:find("%.%.") then return false end
			return true
		end
		mkyboot.inc.valid.img_path = function(s)
			if type(s) ~= "string" or #s == 0 or #s > 256 then return false end
			if s:find("[;|&`$(){}\n\r]") then return false end
			if s:find("%.%.") then return false end
			if s:match("[^%w%_%-%./]") then return false end
			return true
		end
	--[[===========================================================================================================================================================================================]]
	--[[ SECURITY: Cryptographically secure random bytes                                                                         ]]
	--[[===========================================================================================================================================================================================]]
		mkyboot.inc.random = {}
		mkyboot.inc.random.bytes = function(n)
			if type(n) ~= "number" or n < 1 then return nil end
			local fd = io.open("/dev/urandom", "rb")
			if not fd then return nil end
			local data = fd:read(n)
			fd:close()
			if not data or #data ~= n then return nil end
			return data
		end
		mkyboot.inc.random.hex = function(n)
			local bytes = mkyboot.inc.random.bytes(n)
			if not bytes then return nil end
			local hex = ""
			for i = 1, #bytes do
				hex = hex .. string.format("%02x", string.byte(bytes, i))
			end
			return hex
		end
		mkyboot.inc.random.base64 = function(n)
			local bytes = mkyboot.inc.random.bytes(n)
			if not bytes then return nil end
			return ngx.encode_base64(bytes)
		end
	--[[===========================================================================================================================================================================================]]
	--[[ SECURITY: Password hashing and authentication module (v2 - secure KDF)                                                   ]]
	--[[===========================================================================================================================================================================================]]
		mkyboot.inc.auth = {}
		mkyboot.inc.auth.file = "/srv/mkyboot/cfg/auth.json"
		mkyboot.inc.auth.VERSION = 2
		mkyboot.inc.auth.HASH_ITERATIONS = 250000
		mkyboot.inc.auth.SALT_BYTES = 64
		mkyboot.inc.auth.MIN_PASSWORD_LENGTH = 8

		mkyboot.inc.auth.generate_salt = function()
			return mkyboot.inc.random.hex(mkyboot.inc.auth.SALT_BYTES)
		end

		mkyboot.inc.auth.hash_password = function(password, salt, iterations)
			if type(password) ~= "string" or type(salt) ~= "string" then return nil end
			local iters = iterations or mkyboot.inc.auth.HASH_ITERATIONS
			local hash = salt .. ":" .. password
			for i = 1, iters do
				local h = ngx.sha1_bin(hash)
				local hex = ""
				for j = 1, #h do
					hex = hex .. string.format("%02x", string.byte(h, j))
				end
				hash = hex
			end
			return hash
		end

		mkyboot.inc.auth.verify_password = function(password, stored_hash, stored_salt, stored_iterations)
			if type(password) ~= "string" or type(stored_hash) ~= "string" or type(stored_salt) ~= "string" then return false end
			local iters = stored_iterations or mkyboot.inc.auth.HASH_ITERATIONS
			local computed = mkyboot.inc.auth.hash_password(password, stored_salt, iters)
			if computed == nil then return false end
			if #computed ~= #stored_hash then return false end
			local result = 0
			for i = 1, #computed do
				result = bit.bor(result, string.byte(computed, i) ~ string.byte(stored_hash, i))
			end
			return result == 0
		end

		mkyboot.inc.auth.load_auth = function()
			local json_ok, json = pcall(require, "json")
			if not json_ok then return nil, "json unavailable" end
			local fd = io.open(mkyboot.inc.auth.file, "r")
			if not fd then return nil, "no auth file" end
			local content = fd:read("*a")
			fd:close()
			local ok, data = pcall(json.decode, content)
			if not ok or type(data) ~= "table" then return nil, "invalid auth file" end
			return data, "OK"
		end

		mkyboot.inc.auth.save_auth = function(data)
			local json_ok, json = pcall(require, "json")
			if not json_ok then return false, "json unavailable" end
			local tmp = mkyboot.inc.auth.file .. ".tmp." .. (mkyboot.inc.random.hex(8) or tostring(os.time()))
			local fd = io.open(tmp, "w")
			if not fd then return false, "cannot write temp file" end
			fd:write(json.encode(data))
			fd:close()
			local ok, err = os.rename(tmp, mkyboot.inc.auth.file)
			if not ok then os.remove(tmp); return false, "atomic write failed" end
			return true, "OK"
		end

		mkyboot.inc.auth.is_configured = function()
			local data, msg = mkyboot.inc.auth.load_auth()
			return data ~= nil and data.admin ~= nil
		end

		mkyboot.inc.auth.setup_admin = function(password, username)
			if type(password) ~= "string" or #password < mkyboot.inc.auth.MIN_PASSWORD_LENGTH then
				return false, "password must be at least " .. mkyboot.inc.auth.MIN_PASSWORD_LENGTH .. " characters"
			end
			if mkyboot.inc.auth.is_configured() then
				return false, "already configured"
			end
			local user = username or "admin"
			local salt = mkyboot.inc.auth.generate_salt()
			if not salt then return false, "failed to generate salt" end
			local hash = mkyboot.inc.auth.hash_password(password, salt)
			if hash == nil then return false, "hash failed" end
			local auth_data = {
				version = mkyboot.inc.auth.VERSION,
				algorithm = "iter-sha1",
				iterations = mkyboot.inc.auth.HASH_ITERATIONS,
				salt_bytes = mkyboot.inc.auth.SALT_BYTES,
				admin = {
					hash = hash,
					salt = salt,
					created = os.date("!%Y-%m-%dT%H:%M:%SZ")
				}
			}
			local ok, msg = mkyboot.inc.auth.save_auth(auth_data)
			if not ok then return false, msg end
			mkyboot.inc.log.info("AUTH", "Admin account '" .. user .. "' configured")
			return true, "OK"
		end

		mkyboot.inc.auth.check_password = function(password)
			if not mkyboot.inc.auth.is_configured() then return false, "not configured" end
			local data, msg = mkyboot.inc.auth.load_auth()
			if not data then return false, msg end
			if type(data.admin) ~= "table" or type(data.admin.hash) ~= "string" or type(data.admin.salt) ~= "string" then
				return false, "invalid auth data"
			end
			local iters = data.iterations or mkyboot.inc.auth.HASH_ITERATIONS
			return mkyboot.inc.auth.verify_password(password, data.admin.hash, data.admin.salt, iters), "OK"
		end

		mkyboot.inc.auth.change_password = function(current_password, new_password)
			if type(current_password) ~= "string" or type(new_password) ~= "string" then
				return false, "invalid parameters"
			end
			if #new_password < mkyboot.inc.auth.MIN_PASSWORD_LENGTH then
				return false, "new password must be at least " .. mkyboot.inc.auth.MIN_PASSWORD_LENGTH .. " characters"
			end
			if not mkyboot.inc.auth.is_configured() then
				return false, "not configured"
			end
			local data, msg = mkyboot.inc.auth.load_auth()
			if not data then return false, msg end
			local iters = data.iterations or mkyboot.inc.auth.HASH_ITERATIONS
			if not mkyboot.inc.auth.verify_password(current_password, data.admin.hash, data.admin.salt, iters) then
				mkyboot.inc.log.warn("AUTH", "Password change rejected: incorrect current password")
				return false, "incorrect current password"
			end
			local new_salt = mkyboot.inc.auth.generate_salt()
			if not new_salt then return false, "failed to generate salt" end
			local new_hash = mkyboot.inc.auth.hash_password(new_password, new_salt)
			if not new_hash then return false, "hash failed" end
			data.admin.hash = new_hash
			data.admin.salt = new_salt
			data.admin.updated = os.date("!%Y-%m-%dT%H:%M:%SZ")
			local ok, msg2 = mkyboot.inc.auth.save_auth(data)
			if not ok then return false, msg2 end
			mkyboot.inc.log.info("AUTH", "Password changed successfully")
			return true, "OK"
		end
	--[[===========================================================================================================================================================================================]]
	--[[ TARGET COMMANDS sets opt1,opt2,opt3  ]]
	--[[===========================================================================================================================================================================================]]

		mkyboot.lib.json		= require("json");
		mkyboot.lib.lfs  	= require("lfs");	
		mkyboot.lib.posix	= require("posix");	  	  	
	--[[ TARGET COMMANDS sets opt1,opt2,opt3  ]]
	--[[===========================================================================================================================================================================================]]
	  	mkyboot.cmd.tgt = 	{
					new 		= function(opt,p_tid)
						if not mkyboot.inc.valid.tid(p_tid) then mkyboot.inc.log.warn("SECURITY", "tgt.new: invalid tid="..tostring(p_tid)); return false end
						if not mkyboot.inc.valid.iqn(opt) then mkyboot.inc.log.warn("SECURITY", "tgt.new: invalid target name"); return false end
						return os.execute("/usr/sbin/tgtadm --lld iscsi --op new --mode target --tid "..p_tid.." -T "..opt);
					end,
					destroy		= function(opt)
						if not mkyboot.inc.valid.tid(opt) then mkyboot.inc.log.warn("SECURITY", "tgt.destroy: invalid tid="..tostring(opt)); return false end
						return os.execute("/usr/sbin/tgtadm --lld iscsi --op delete --mode target --tid "..opt);
					end,
					kill		= function(opt)
						if not mkyboot.inc.valid.tid(opt) then mkyboot.inc.log.warn("SECURITY", "tgt.kill: invalid tid="..tostring(opt)); return false end
						return os.execute("/usr/sbin/tgtadm --lld iscsi --op delete --force --mode target --tid "..opt);
					end,
					show 		= function(opt) return os.execute("/usr/sbin/tgtadm --lld iscsi --op show --mode target"); end,
					rules		= function(p_tid,opt)
						if not mkyboot.inc.valid.tid(p_tid) then mkyboot.inc.log.warn("SECURITY", "tgt.rules: invalid tid="..tostring(p_tid)); return false end
						if not mkyboot.inc.valid.ipv4(opt) then mkyboot.inc.log.warn("SECURITY", "tgt.rules: invalid ip="..tostring(opt)); return false end
						return os.execute("/usr/sbin/tgtadm --lld iscsi --mode target --op bind --tid "..p_tid.." -I "..opt);
					end,
					unrul		= function(p_tid,opt)
						if not mkyboot.inc.valid.tid(p_tid) then mkyboot.inc.log.warn("SECURITY", "tgt.unrul: invalid tid="..tostring(p_tid)); return false end
						if not mkyboot.inc.valid.ipv4(opt) then mkyboot.inc.log.warn("SECURITY", "tgt.unrul: invalid ip="..tostring(opt)); return false end
						return os.execute("/usr/sbin/tgtadm --lld iscsi --mode target --op unbind --tid "..p_tid.." -I "..opt);
					end,
					used		= function(p_tgt)
						if type(p_tgt) ~= "string" or #p_tgt == 0 or #p_tgt > 128 then return false end
						if p_tgt:find("[;|&`$()]") then mkyboot.inc.log.warn("SECURITY", "tgt.used: rejected metacharacters in="..p_tgt); return false end
						local fd = io.popen("/usr/sbin/tgtadm --lld iscsi --op show --mode target 2>/dev/null | /usr/bin/grep \""..p_tgt.."\" 2>/dev/null")
						local result = (#fd:read("a*") > 0)
						fd:close()
						return result
					end
					};
		mkyboot.cmd.lun = 	{
							add		= function(p_tid,p_lun,p_dev)
								if not mkyboot.inc.valid.tid(p_tid) then mkyboot.inc.log.warn("SECURITY", "lun.add: invalid tid="..tostring(p_tid)); return false end
								if not mkyboot.inc.valid.lun_id(p_lun) then mkyboot.inc.log.warn("SECURITY", "lun.add: invalid lun="..tostring(p_lun)); return false end
								if not mkyboot.inc.valid.dev_path(p_dev) then mkyboot.inc.log.warn("SECURITY", "lun.add: invalid dev="..tostring(p_dev)); return false end
								return os.execute("/usr/sbin/tgtadm --lld iscsi --op new --mode logicalunit --tid "..p_tid.." --lun "..p_lun.." -b "..p_dev);
							end,
							del 	= function(p_tid,p_lun)
								if not mkyboot.inc.valid.tid(p_tid) then mkyboot.inc.log.warn("SECURITY", "lun.del: invalid tid="..tostring(p_tid)); return false end
								if not mkyboot.inc.valid.lun_id(p_lun) then mkyboot.inc.log.warn("SECURITY", "lun.del: invalid lun="..tostring(p_lun)); return false end
								return os.execute("/usr/sbin/tgtadm --lld iscsi --op delete --mode logicalunit --tid "..p_tid.." --lun "..p_lun);
							end,
							stop 	= function(p_opt)
								if not mkyboot.inc.valid.tid(p_opt) then mkyboot.inc.log.warn("SECURITY", "lun.stop: invalid tid="..tostring(p_opt)); return false end
								return os.execute("/usr/sbin/tgtadm --offline --tid "..p_opt);
							end,
							start 	= function(p_opt)
								if not mkyboot.inc.valid.tid(p_opt) then mkyboot.inc.log.warn("SECURITY", "lun.start: invalid tid="..tostring(p_opt)); return false end
								return os.execute("/usr/sbin/tgtadm --ready --tid "..p_opt);
							end
					};
		mkyboot.cmd.nbd = 	{
							mod 	= function(p_max_part,p_nbds)
								local maxp = tonumber(p_max_part)
								local nbds = tonumber(p_nbds)
								if not maxp or maxp < 1 or maxp > 255 then mkyboot.inc.log.warn("SECURITY", "nbd.mod: invalid max_part="..tostring(p_max_part)); return false end
								if not nbds or nbds < 1 or nbds > 256 then mkyboot.inc.log.warn("SECURITY", "nbd.mod: invalid nbds="..tostring(p_nbds)); return false end
								return os.execute("/usr/sbin/modprobe nbd max_part "..p_max_part.." nbds "..p_nbds);
							end,
							unmod 	= function() return os.execute("/usr/sbin/modprobe -r nbd"); end,
							add 	= function(p_dev,p_path,p_flags)
								if not mkyboot.inc.valid.dev_path(p_dev) then mkyboot.inc.log.warn("SECURITY", "nbd.add: invalid dev="..tostring(p_dev)); return false end
								if not mkyboot.inc.valid.safe_path(p_path) then mkyboot.inc.log.warn("SECURITY", "nbd.add: invalid path="..tostring(p_path)); return false end
								if not mkyboot.inc.valid.cache_mode(p_flags) then mkyboot.inc.log.warn("SECURITY", "nbd.add: invalid cache="..tostring(p_flags)); return false end
								os.execute("/srv/mkyboot/client.lua "..p_dev.." "..p_path.." "..p_flags);
							end,
							del 	= function(p_dev)
								if not mkyboot.inc.valid.dev_path(p_dev) then mkyboot.inc.log.warn("SECURITY", "nbd.del: invalid dev="..tostring(p_dev)); return false end
								return os.execute("/usr/bin/qemu-nbd -d "..p_dev.." 2>/dev/null");
							end,
							kill 	= function(p_pid)
								if not mkyboot.inc.valid.pid(p_pid) then mkyboot.inc.log.warn("SECURITY", "nbd.kill: invalid pid="..tostring(p_pid)); return false end
								return os.execute("/usr/bin/kill -9 "..p_pid);
							end,
							used	= function(p_dev)
								if not mkyboot.inc.valid.dev_path(p_dev) then return false end
								local fd = io.popen("/usr/bin/lsof -t "..p_dev.." 2>/dev/null")
								local result = (#fd:read("a*") > 0)
								fd:close()
								return result
							end,
							usewho	= function(p_dev)
								if not mkyboot.inc.valid.dev_path(p_dev) then return false end
								local fd = io.popen("/usr/bin/lsof -t "..p_dev.." 2>/dev/null")
								local out = fd:read("a*")
								fd:close()
								if #out == 0 then return false end
								local fd2 = io.popen("/usr/bin/lsof -t "..p_dev.." 2>/dev/null | /usr/bin/xargs -I{} /usr/bin/cat /proc/{}/comm 2>/dev/null | /usr/bin/head -1")
								local procname = fd2:read("*l") or ""
								fd2:close()
								if procname:find("qemu%-nbd") then return 1
								elseif procname:find("tgtd") then return 2
								else return false end
							end
					};
		mkyboot.cmd.img = 	{
							new 	= function(p_path,p_size)
								if not mkyboot.inc.valid.img_path(p_path) then mkyboot.inc.log.warn("SECURITY", "img.new: invalid path="..tostring(p_path)); return false end
								if not mkyboot.inc.valid.img_size(p_size) then mkyboot.inc.log.warn("SECURITY", "img.new: invalid size="..tostring(p_size)); return false end
								return os.execute("/usr/bin/qemu-img -f qcow2 -o preallocation=metadata,compat=1.1,lazy_refcounts=on encryption=off "..p_path.." "..p_size);
							end,
							child 	= function(p_parrent,p_child)
								if not mkyboot.inc.valid.img_path(p_parrent) then mkyboot.inc.log.warn("SECURITY", "img.child: invalid parent="..tostring(p_parrent)); return false end
								if not mkyboot.inc.valid.img_path(p_child) then mkyboot.inc.log.warn("SECURITY", "img.child: invalid child="..tostring(p_child)); return false end
								return os.execute("/usr/bin/qemu-img create -f qcow2 -b "..p_parrent.." "..p_child.." -o lazy_refcounts=on 2>>/tmp/result");
							end,
							del 	= function(p_image)
								if not mkyboot.inc.valid.safe_path(p_image) then mkyboot.inc.log.warn("SECURITY", "img.del: invalid path="..tostring(p_image)); return false end
								return os.remove(p_image);
							end,
							commit 	= function(p_image)
								if not mkyboot.inc.valid.safe_path(p_image) then mkyboot.inc.log.warn("SECURITY", "img.commit: invalid path="..tostring(p_image)); return false end
								local fd = io.popen("/usr/bin/qemu-img commit "..p_image.." 2>&1")
								local result = fd:read("a*")
								fd:close()
								return result
							end,
							used 	= function(p_image)
								if not mkyboot.inc.valid.safe_path(p_image) then return false end
								local fd = io.popen("/usr/bin/lsof -t "..p_image.." 2>/dev/null")
								local result = (#fd:read("a*") > 0)
								fd:close()
								return result
							end
							};

		mkyboot.cmd.zfs =  	{
							mtab 	= function(p_args)
								if type(p_args) ~= "string" or #p_args == 0 then return false end
								if p_args:find("[;|&`$()]") then return false end
								local fd_file = io.open("/etc/mtab", "r")
								if not fd_file then return false end
								local fd_data = fd_file:read("*a")
								fd_file:close()
								return string.find(fd_data, p_args) ~= nil
							end,
							snap 	= function(p_data)
								if not mkyboot.inc.valid.safe_path(p_data) then mkyboot.inc.log.warn("SECURITY", "zfs.snap: invalid data="..tostring(p_data)); return false end
								return os.execute("/usr/sbin/zfs snap "..p_data.." 2>/dev/null");
							end,
							unsnap 	= function(p_data)
								if not mkyboot.inc.valid.safe_path(p_data) then mkyboot.inc.log.warn("SECURITY", "zfs.unsnap: invalid data="..tostring(p_data)); return false end
								return os.execute("/usr/sbin/zfs destroy -f "..p_data.." 2>/dev/null");
							end,
							mount 	= function(p_data,p_point)
								if not mkyboot.inc.valid.safe_path(p_data) then mkyboot.inc.log.warn("SECURITY", "zfs.mount: invalid data="..tostring(p_data)); return false end
								if not mkyboot.inc.valid.safe_path(p_point) then mkyboot.inc.log.warn("SECURITY", "zfs.mount: invalid point="..tostring(p_point)); return false end
								return os.execute("/usr/bin/mount -t zfs "..p_data.." "..p_point.." 2>>/var/log/messages");
							end,
							unmount  = function(p_point)
								if not mkyboot.inc.valid.safe_path(p_point) then mkyboot.inc.log.warn("SECURITY", "zfs.unmount: invalid point="..tostring(p_point)); return false end
								return os.execute("/usr/bin/umount -f "..p_point.." 2>/dev/null");
							end
							}
		mkyboot.cmd.power = 	{
							on = function(p_iface, p_mac)
								if type(p_iface) ~= "string" or not p_iface:match("^[%w%-]+$") then mkyboot.inc.log.warn("SECURITY", "power.on: invalid iface="..tostring(p_iface)); return false end
								if not mkyboot.inc.valid.mac(p_mac) then mkyboot.inc.log.warn("SECURITY", "power.on: invalid mac="..tostring(p_mac)); return false end
								return os.execute("/usr/sbin/etherwake -i "..p_iface.." "..p_mac);
							end
							}			


	--[[===========================================================================================================================================================================================]]
	--[[ TARGET COMMANDS sets opt1,opt2,opt3  ]]
	--[[===========================================================================================================================================================================================]]
	    mkyboot.inc.checkconf = function(p_test) 
	    	local result = true
				if	mkyboot.cfg 						== nil then result=false end
				if  mkyboot.cfg ~= nil then
				if	mkyboot.cfg.server 				== nil then result=false end
				if	mkyboot.cfg.iscsi 				== nil then result=false end
				if	mkyboot.cfg.iscsi.iqn 			== nil then result=false end
				if	mkyboot.cfg.iscsi.listen 		== nil then result=false end
				if	mkyboot.cfg.iscsi.port 			== nil then result=false end
				if	mkyboot.cfg.iscsi.proto			== nil then result=false end
				if	mkyboot.cfg.dhcp 				== nil then result=false end
				if	mkyboot.cfg.dhcp.config			== nil then result=false end
				if	mkyboot.cfg.dhcp.config.global	== nil then result=false end
				if	mkyboot.cfg.server.vendor 		== nil then result=false end
				if	mkyboot.cfg.server.version		== nil then result=false end
				if	mkyboot.cfg.server.ipv4			== nil then result=false end
				if	mkyboot.cfg.server.mask			== nil then result=false end
				if	mkyboot.cfg.server.gateway		== nil then result=false end
				if	mkyboot.cfg.server.dns1			== nil then result=false end
				if	mkyboot.cfg.server.dns2			== nil then result=false end
				if	mkyboot.cfg.server.workdir		== nil then result=false end
				if	mkyboot.cfg.server.tftp			== nil then result=false end
				if	mkyboot.cfg.server.distdir		== nil then result=false end
				if	mkyboot.cfg.server.imgdir		== nil then result=false end
				if	mkyboot.cfg.server.imgdatadir	== nil then result=false end
				if	mkyboot.cfg.server.imgbackdir	== nil then result=false end
				if	mkyboot.cfg.server.config		== nil then result=false end
				if  mkyboot.cfg.wks 					== nil then result=false end
				if	mkyboot.cfg.dhcp.port 			== nil then result=false end
				if	mkyboot.cfg.dhcp.workdir			== nil then result=false end
				if	mkyboot.cfg.tftp.port 			== nil then result=false end
				if  mkyboot.cfg.tftp.workdir			== nil then result=false end
				if  mkyboot.cfg.server.image_prefix  == nil then result=false end
				if  mkyboot.cfg.server.nbd_nbds 		== nil then result=false end
				if  mkyboot.cfg.server.nbd_max_part  == nil then result=false end
				end
					return result
		end;			
		mkyboot.inc.debug = function(t_data)
			--if t_data ~= nil then  io.open("/tmp/debug.mkyboot","a"):write(t_data,"\n"):close() end;
		end;
		mkyboot.inc.lsof = function(p_patern)
					if type(p_patern) ~= "string" or #p_patern == 0 or #p_patern > 256 then return false end
					if p_patern:find("[;|&`$()]") then mkyboot.inc.log.warn("SECURITY", "lsof: rejected metacharacters"); return false end
					local fd = io.popen("/usr/bin/lsof "..p_patern.." 2>/dev/null")
					local result = (#fd:read("a*") > 0)
					fd:close()
					return result
		end;
		mkyboot.inc.lsofkill = function(p_path)
					if not mkyboot.inc.valid.safe_path(p_path) then mkyboot.inc.log.warn("SECURITY", "lsofkill: invalid path="..tostring(p_path)); return false end
					local fd = io.popen("/usr/bin/lsof -t "..p_path.." 2>/dev/null")
					local pids = fd:read("a*")
					fd:close()
					if #pids == 0 then return false end
					for pid in pids:gmatch("%d+") do
						os.execute("/usr/bin/kill -9 "..pid.." 2>/dev/null")
					end
					return true
		end;
		mkyboot.inc.search_nbd = function ()
					for i_index = 1,mkyboot.cfg.server.nbd_nbds,1 do
						local dev = "/dev/nbd"..i_index
						local fd = io.popen("/usr/bin/lsof -t "..dev.." 2>/dev/null")
						local pids = fd:read("a*") or ""
						fd:close()
						if #pids == 0 then return dev end
					end
					return false
		end; 

		mkyboot.inc.getpid_nbd = function (t_path)
			if t_path == nil or not mkyboot.inc.isFile(t_path) then return nil end
			if not mkyboot.inc.valid.safe_path(t_path) then return nil end
			local fd = io.popen("/usr/bin/lsof -t "..t_path.." 2>/dev/null")
			local result = fd:read("a*") or ""
			fd:close()
			result = result:gsub("%s+", "")
			if result == '' then return nil else return result end
		end;
		
		mkyboot.inc.getdev_nbd = function (t_pid)
			if t_pid == nil then return nil end
			local clean_pid = tostring(t_pid):gsub('%W','')
			if not mkyboot.inc.valid.pid(clean_pid) then return nil end
			local fd = io.popen("/usr/bin/lsof -p "..clean_pid.." 2>/dev/null | /usr/bin/awk '/\\/dev\\/nbd/ { print $NF }' ")
			local result = fd:read("a*") or ""
			fd:close()
			result = result:gsub("%s+", "")
			if result == '' then return nil else return result end
		end;

		mkyboot.inc.scCheck	= function()
				local result = true
				if mkyboot.inc.checkconf then
					if not mkyboot.inc.lsof("-t -i:"..mkyboot.cfg.dhcp.port) then result=false 	end
					if not mkyboot.inc.lsof("-t -i:"..mkyboot.cfg.tftp.port) then result=false 	end
					if not mkyboot.inc.lsof("-t -i:"..mkyboot.cfg.iscsi.port) then result=false	end
				end
				return result
		end;
		mkyboot.inc.systemctl = function(p_name,p_cmd)
						if not mkyboot.inc.valid.service_name(p_name) then mkyboot.inc.log.warn("SECURITY", "systemctl: invalid service="..tostring(p_name)); return false end
						if not mkyboot.inc.valid.systemctl_cmd(p_cmd) then mkyboot.inc.log.warn("SECURITY", "systemctl: invalid cmd="..tostring(p_cmd)); return false end
						return os.execute("/usr/bin/systemctl "..p_cmd.." "..p_name)
		end;
		mkyboot.inc.monit = function()
					if not mkyboot.inc.lsof("-t -i:"..mkyboot.cfg.dhcp.port) 	then mkyboot.inc.systemctl("isc-dhcp-server","start"); mkyboot.inc.systemctl("isc-dhcp-server","restart");	end;
					if not mkyboot.inc.lsof("-t -i:"..mkyboot.cfg.tftp.port) 	then mkyboot.inc.systemctl("tftpd-hpa","start"); mkyboot.inc.systemctl("tftpd-hpa","restart");	end;
					if not mkyboot.inc.lsof("-t -i:"..mkyboot.cfg.iscsi.port) then mkyboot.inc.systemctl("tgt","start"); mkyboot.inc.systemctl("tgt","restart");	end;
		end;
		mkyboot.inc.GetMacFromIPv4 = function(p_ipv4)
			if mkyboot.inc.checkconf() and p_ipv4 ~= nil then
				local i,v
					for i,v in pairs(mkyboot.cfg.wks) do
						if p_ipv4 == v.ipv4 then if v.mac ~= nil then return v.mac; end; end;
					end;
				i,v = nil,nil
			end;
		end;
		mkyboot.inc.GetIDFromIPv4 = function(p_ipv4)
			if mkyboot.inc.checkconf() and p_ipv4 ~= nil then
				local i,v
					for i,v in pairs(mkyboot.cfg.wks) do
						if p_ipv4 == v.ipv4 then  return i; end;
					end;
				i,v = nil,nil
			end;
		end;
	
		mkyboot.inc.unescape = function(s)
			s = string.gsub(s, "+", " ")
			s = string.gsub(s, "%%(%x%x)", function(h)return string.char(tonumber(h, 16))end)
			return s
		end
	function mkyboot.inc.isDir(name)
		local lfs = require("lfs")
	    if type(name)~="string" then return false end
	    local cd = lfs.currentdir()
	    local is = lfs.chdir(name) and true or false
	    lfs.chdir(cd)
	    return is
	end;
	function mkyboot.inc.isFile(name)
	        if name ~= nil and mkyboot.lib.posix.stat(name) ~= nil then return true else return false end;
	        -- note that the short evaluation is to
	        -- return false instead of a possible nil

	    return false
	end;

	function mkyboot.inc.isFileOrDir(name)
	    if type(name)~="string" then return false end
	    return os.rename(name, name) and true or false
	end;	
	function mkyboot.inc.isSupperMode(id)
		if mkyboot.inc.checkconf() and tostring(mkyboot.cfg.wks[tonumber(id)].supper) ~=nil and tostring(mkyboot.cfg.wks[tonumber(id)].supper) == "1" then 
			return true
		else
			return false
		end;
	end;
	function mkyboot.inc.ls_files(path)
		if mkyboot.inc.isDir(path) then
			local result, t_res = {}
				iter, dir_obj = lfs.dir (path)
				while true do
					t_res = dir_obj:next();
					if t_res ~= nil then if lfs.attributes(path.."/"..t_res).mode == "file" then  table.insert(result, t_res) end else break end
				end;
				dir_obj:close()
				t_res = nil
			return result
		else
			return "none"
		end;
	end;
	function mkyboot.inc.ls_devices(path)
		if mkyboot.inc.isDir(path) then
			local result, t_res = {}
				iter, dir_obj = lfs.dir (path)
				while true do
					t_res = dir_obj:next();
					if t_res ~= nil then if lfs.attributes(path.."/"..t_res).mode == "block device" then  if string.find(t_res, '@') ~= nil or string.find(t_res, '-') ~= nil  then else  table.insert(result, t_res) end end else break end
				end;
				dir_obj:close()
				t_res = nil
			return result
		else
			return "none"
		end;
	end;
	function mkyboot.inc.isMacARP(p_ip)
		if not mkyboot.inc.valid.ipv4(p_ip) then mkyboot.inc.log.warn("SECURITY", "isMacARP: invalid ip="..tostring(p_ip)); return "" end
		local f_tmp = os.tmpname()
		os.execute("/usr/sbin/arp -a "..p_ip.." | /usr/bin/awk '{ print $4 }' > "..f_tmp)
		local f_mac = io.open(f_tmp, "r")
		local f_data = f_mac:read("*a")
		f_mac:close()
		os.remove(f_tmp)
		return f_data
	end;
	--[[ TARGET COMMANDS sets opt1,opt2,opt3  ]]
	--[[===========================================================================================================================================================================================]]

			function mkyboot:SaveToFile(fpath, t_data)
				if mkyboot.inc.checkconf() then
					local json,result,file = require("json");
					file = io.open(fpath, "w");
					result = json.encode(t_data)
					file:write(result);
					file:close()
					return true
				else
					return false
				end
			end;
			function mkyboot:LoadFromFile(fpath)
				local l_data,l_result,file = {};
				file = io.open(fpath, "r");
				l_result = file:read("*a");
				l_data = json.decode(l_result)
				file:close()
				return l_data
			end;
			function mkyboot:ExportDHCP()
				if mkyboot.inc.checkconf() then
					-- if isDir(mkyboot.cfg.dhcp.workdir) and isFile(mkyboot.cfg.dhcp.workdir.."/dhcpd.conf") then
						os.rename(mkyboot.cfg.dhcp.workdir.."/dhcpd.conf",mkyboot.cfg.dhcp.workdir.."/dhcpd.conf.backup_"..os.date("%D%T"):gsub('%W',''));
						local file,data,i,v = io.open(mkyboot.cfg.dhcp.workdir.."/dhcpd.conf", "w") ;
							file:write("## ### THIS FILE AUTOGEENERATION ### #\n");
							file:write("# ### "..mkyboot.cfg.server.vendor.." "..mkyboot.cfg.server.version.."______  ### #\n");
							file:write("#[============================================================================================]#\n");
						for i,v in pairs(mkyboot.cfg.dhcp.config.global) do
							file:write(i," ",tostring(v)..";\n"); 
						end;
						file:write("#[============================================================================================]#\n");
						i,v = nil,nil
						for i,v in pairs(mkyboot.cfg.dhcp.config.opt) do
							if i == 'domain-name' then file:write("	option "..i," \"",tostring(v).."\";\n"); else file:write("	option "..i," ",tostring(v)..";\n"); end;
						end;
						file:write("#[============================================================================================]#\n");
						i,v = nil,nil
						for i,v in ipairs(mkyboot.cfg.dhcp.config.sub) do
							file:write("	subnet ",v.sub," netmask ",v.mask," {\n");
								local k,val 
								for k,val in ipairs(v.ranges) do
									file:write("            range ",val,";\n");
								end;
								k,val = nil,nil;
								file:write("}\n");	
						end;
						file:write("#[============================================================================================]#\n");
						file:write(mkyboot.cfg.dhcp.config.ipxe, "\n");
						file:write("#[============================================================================================]#\n");
						i,v = nil,nil
							for i,v in pairs(mkyboot.cfg.wks) do
								if v.name ~= nil then
								 if tostring(v.enable) == "1" then
									file:write("host ",v.name," {\n");
									file:write("	hardware ethernet ",v.mac," ;\n");
											file:write("	fixed-address ",v.ipv4,";\n");
											file:write("	option host-name \"",v.name,"\";\n");
											file:write("	if substring (option vendor-class-identifier, 15, 5) = \"00000\" {\n");
											file:write("		filename \"",v.fileboot,".kpxe\";\n"); 
											file:write("	}\n");
											file:write("	elsif substring (option vendor-class-identifier, 15, 5) = \"00006\" {\n");
											file:write("		filename \"",v.fileboot,"32.efi\";\n"); 
											file:write("	}\n");
											file:write("	else {\n");
											file:write("		filename \"",v.fileboot,".efi\";\n"); 
											file:write("	}\n");

								 end;
								local k,val 
									for k,val in ipairs(v.opt) do
										file:write("option	",val,";\n");
									end;
										file:write("}\n");
								end;

								 k,val = nil,nil
							end;														
							file:close();
						i,v = nil,nil
					-- end;
				end;
			end;   		
		
	--[[ TARGET COMMANDS sets opt1,opt2,opt3  ]]
	--[[===========================================================================================================================================================================================]]
		function mkyboot:tgtstart(p_ip)
			mkyboot.inc.monit()
			if mkyboot.inc.scCheck() and mkyboot.inc.checkconf() then
					local l_id = mkyboot.inc.GetIDFromIPv4(p_ip);
					if l_id == nil then mkyboot.inc.log.warn("ISCSI", "tgtstart: unknown IP "..p_ip); return end
						if tostring(mkyboot.cfg.server.debug) == "1" then print(mkyboot.cfg.iscsi.iqn..":"..mkyboot.cfg.wks[l_id].mac:gsub('%W',''),mkyboot.cfg.wks[l_id].tid); print(mkyboot.cmd.tgt.used(mkyboot.cfg.iscsi.iqn..":"..mkyboot.cfg.wks[l_id].mac:gsub('%W',''))); end;
						if not mkyboot.cmd.tgt.used(mkyboot.cfg.iscsi.iqn..":"..mkyboot.cfg.wks[l_id].mac:gsub('%W','')) and tostring(mkyboot.cfg.wks[l_id].enable) == "1" then mkyboot.cmd.tgt.new(mkyboot.cfg.iscsi.iqn..":"..mkyboot.cfg.wks[l_id].mac:gsub('%W',''),mkyboot.cfg.wks[l_id].tid); mkyboot.cmd.tgt.rules(mkyboot.cfg.wks[l_id].tid,p_ip); mkyboot.inc.log.info("ISCSI", "Target created: tid="..mkyboot.cfg.wks[l_id].tid.." for "..p_ip); end;
						if tostring(mkyboot.cfg.server.debug) == "1" then 	print("STARTED !"); print(mkyboot.cmd.tgt.used(mkyboot.cfg.iscsi.iqn..":"..mkyboot.cfg.wks[l_id].mac:gsub('%W',''))); end;
			end;
		end;
		function mkyboot:tgtstop(p_ip)
			mkyboot.inc.monit()
			if mkyboot.inc.scCheck() and mkyboot.inc.checkconf() then
					local l_id = mkyboot.inc.GetIDFromIPv4(p_ip);
					if l_id == nil then mkyboot.inc.log.warn("ISCSI", "tgtstop: unknown IP "..p_ip); return end
						if tostring(mkyboot.cfg.server.debug) == "1" then print (mkyboot.cfg.wks[l_id].tid); print(mkyboot.cfg.iscsi.iqn..":"..mkyboot.cfg.wks[l_id].mac:gsub('%W','')); print(mkyboot.cmd.tgt.used(mkyboot.cfg.iscsi.iqn..":"..mkyboot.cfg.wks[l_id].mac:gsub('%W',''))); end;
						-- if 
						if mkyboot.cmd.tgt.used(mkyboot.cfg.iscsi.iqn..":"..mkyboot.cfg.wks[l_id].mac:gsub('%W','')) then mkyboot.cmd.tgt.kill(mkyboot.cfg.wks[l_id].tid); end;
						if tostring(mkyboot.cfg.server.debug) == "1" then print(mkyboot.cmd.tgt.used(mkyboot.cfg.iscsi.iqn..":"..mkyboot.cfg.wks[l_id].mac:gsub('%W',''))); end;
			end;
		end;
	--[[===========================================================================================================================================================================================]]
		function mkyboot:mkChild(p_ip)
			local l_id,l_lockf,p_child,p_parrent = mkyboot.inc.GetIDFromIPv4(p_ip)
			if l_id == nil then mkyboot.inc.log.warn("IMG", "mkChild: unknown IP "..p_ip); return end
			local l_vid = mkyboot.cfg.wks[l_id].mac:gsub('%W','')
			mkyboot.inc.log.info("IMG", "mkChild: client "..mkyboot.cfg.wks[l_id].name.." ("..p_ip..")")
			if tostring(mkyboot.cfg.wks[l_id].enable) == "1" then
				
				for i,v in pairs(mkyboot.cfg.wks[l_id].img) do
						l_lockf = mkyboot.cfg.server.lockfile..i..l_vid;
						
						--[[ PROCESS FIND PATH PARRENTS ]]--

					if tostring(mkyboot.cfg.wks[l_id].enable) == "1" and tostring(v.enable) == "1" and v.type == "dyndisk" then
						if  tostring(mkyboot.cfg.wks[l_id].enable) == "1" and  tostring(mkyboot.cfg.wks[l_id].supper) == "1" and tostring(v.enable) == "1" and tostring(v.commit) == "1" then p_parrent = mkyboot.cfg.server.imgdir.."/"..v.path; p_child = mkyboot.cfg.server.imgbackdir.."/"..v.path..mkyboot.cfg.server.image_prefix..l_vid; 	end;
						if  tostring(mkyboot.cfg.wks[l_id].supper) == "0" and tostring(v.enable) == "1" and tostring(v.commit) == "0" then p_parrent = mkyboot.cfg.server.imgdir.."/"..mkyboot.cfg.zfs.tmpname.."/"..l_vid.."/"..v.path; p_child = mkyboot.cfg.server.imgbackdir.."/"..v.path..mkyboot.cfg.server.image_prefix..l_vid	end; --
						if  tostring(mkyboot.cfg.wks[l_id].supper) == "0" and tostring(v.enable) == "1" and tostring(v.commit) == "1" then p_parrent = mkyboot.cfg.server.imgdir.."/"..mkyboot.cfg.zfs.tmpname.."/"..l_vid.."/"..v.path; p_child = mkyboot.cfg.server.imgbackdir.."/"..v.path..mkyboot.cfg.server.image_prefix..l_vid	end;
						if  tostring(mkyboot.cfg.wks[l_id].supper) == "1" and tostring(v.enable) == "1" and tostring(v.commit) == "0" then p_parrent = mkyboot.cfg.server.imgdir.."/"..mkyboot.cfg.zfs.tmpname.."/"..l_vid.."/"..v.path; p_child = mkyboot.cfg.server.imgbackdir.."/"..v.path..mkyboot.cfg.server.image_prefix..l_vid	end;
					elseif 	tostring(mkyboot.cfg.wks[l_id].enable) == "1" and tostring(v.enable) == "1" and v.type == "dynblock" then
						if  tostring(mkyboot.cfg.wks[l_id].enable) == "1" and  tostring(mkyboot.cfg.wks[l_id].supper) == "1" and tostring(v.enable) == "1" and tostring(v.commit) == "1" then p_parrent = mkyboot.cfg.zfs.devpoint.."/"..v.path; p_child = mkyboot.cfg.server.imgbackdir.."/"..v.path..mkyboot.cfg.server.image_prefix..l_vid; 	end;
						if  tostring(mkyboot.cfg.wks[l_id].supper) == "0" and tostring(v.enable) == "1" and tostring(v.commit) == "0" then p_parrent = mkyboot.cfg.zfs.devpoint.."/"..v.path.."@"..mkyboot.cfg.zfs.tmpname.."_"..l_vid; p_child = mkyboot.cfg.server.imgbackdir.."/"..v.path..mkyboot.cfg.server.image_prefix..l_vid	end; --
						if  tostring(mkyboot.cfg.wks[l_id].supper) == "0" and tostring(v.enable) == "1" and tostring(v.commit) == "1" then p_parrent = mkyboot.cfg.zfs.devpoint.."/"..v.path.."@"..mkyboot.cfg.zfs.tmpname.."_"..l_vid; p_child = mkyboot.cfg.server.imgbackdir.."/"..v.path..mkyboot.cfg.server.image_prefix..l_vid	end;
						if  tostring(mkyboot.cfg.wks[l_id].supper) == "1" and tostring(v.enable) == "1" and tostring(v.commit) == "0" then p_parrent = mkyboot.cfg.zfs.devpoint.."/"..v.path.."@"..mkyboot.cfg.zfs.tmpname.."_"..l_vid; p_child = mkyboot.cfg.server.imgbackdir.."/"..v.path..mkyboot.cfg.server.image_prefix..l_vid	end;
					end;	

					if  tostring(mkyboot.cfg.wks[l_id].enable) == "1" and tostring(v.enable) == "1" and v.type ~= "iso"  then
						--[[ PROCESS CREATE CHILD FILE ]]--

						if  tostring(mkyboot.cfg.wks[l_id].enable) == "1" and  tostring(mkyboot.cfg.wks[l_id].supper) == "1" and tostring(v.enable) == "1" and tostring(v.commit) == "1" and not mkyboot.inc.isFile(l_lockf) and mkyboot.inc.isFile(p_child) then 
								while mkyboot.cmd.img.used(p_child) do
									mkyboot.inc.lsofkill(p_child)
									require("posix.unistd").sleep(0.5);
								end;
								mkyboot.cmd.img.del(p_child);
								mkyboot.cmd.img.child(p_parrent, p_child);
								local tmpfile = io.open(l_lockf, "w");
						elseif tostring(mkyboot.cfg.wks[l_id].enable) == "1" and  tostring(mkyboot.cfg.wks[l_id].supper) == "1" and tostring(v.enable) == "1" and tostring(v.commit) == "1" and not mkyboot.inc.isFile(l_lockf) and not mkyboot.inc.isFile(p_child) then
							local tmpfile = io.open(l_lockf, "w")
							mkyboot.cmd.img.child(p_parrent, p_child);
						elseif tostring(mkyboot.cfg.wks[l_id].enable) == "1" and  tostring(mkyboot.cfg.wks[l_id].supper) == "1" and tostring(v.enable) == "1" and tostring(v.commit) == "1" and mkyboot.inc.isFile(l_lockf) and mkyboot.inc.isFile(p_child) then
							local tmpfile = io.open(l_lockf, "w");
						else
							if mkyboot.inc.isFile(p_child) then
								while mkyboot.cmd.img.used(p_child) do
									mkyboot.inc.lsofkill(p_child)
									require("posix.unistd").sleep(0.5);
								end;
								mkyboot.cmd.img.del(p_child);
								if mkyboot.inc.isFile(l_lockf) then os.remove(l_lockf) end
							end;						
							if p_parrent ~= nil and p_child ~= nil  then
								while not mkyboot.inc.isFile(p_parrent) do
									require("posix.unistd").sleep(0.5);
								end;
									mkyboot.cmd.img.child(p_parrent, p_child);
									ngx.say(p_parrent," ", p_child)
							end;
						end;
						ngx.say(p_parrent," ",i," ", p_child)
						ngx.say("\n\nnext 1\n\n")
						p_child,p_parrent,l_lockf = nil,nil,nil
					end;
				end;
			end;			
			l_id,i,v = nil,nil,nil;
		end;
		function mkyboot:rmChild(p_ip)

			local l_id,i,v,img_bpath,img_ppath = mkyboot.inc.GetIDFromIPv4(p_ip);
			if l_id == nil then mkyboot.inc.log.warn("IMG", "rmChild: unknown IP "..p_ip); return end
			if mkyboot.cfg.wks[l_id].img ~=nil then
				for i,v in pairs(mkyboot.cfg.wks[l_id].img) do
					if mkyboot.inc.checkconf() and v.path ~= nil and v.type == "dyndisk" and tostring(v.enable) == "1" then
						img_bpath,img_ppath = mkyboot.cfg.server.imgbackdir.."/"..v.path..mkyboot.cfg.server.image_prefix..mkyboot.cfg.wks[l_id].mac:gsub('%W',''), mkyboot.cfg.server.imgdir.."/"..v.path;
					elseif mkyboot.inc.checkconf() and v.path ~= nil and v.type == "dyndata" and tostring(v.enable) == "1" then
						img_bpath,img_ppath = mkyboot.cfg.server.imgbackdir.."/"..v.path..mkyboot.cfg.server.image_prefix..mkyboot.cfg.wks[l_id].mac:gsub('%W',''), mkyboot.cfg.server.imgdatadir.."/"..v.path;
					end;
					if img_ppath  then
						if mkyboot.inc.isFile(img_bpath) then
							while mkyboot.cmd.img.used(img_bpath) do
								mkyboot.inc.lsofkill(img_bpath)
								require("posix.unistd").sleep(0.5);
							end;
							mkyboot.cmd.img.del(img_bpath);
						end;
					end;
					img_ppath = nil;
				end;
			end;				
		end;
		function mkyboot:checkstatpc(p_ip)
			if not mkyboot.inc.valid.ipv4(p_ip) then return false end
			local fd = io.popen("/usr/sbin/tgtadm --lld iscsi --op show --mode target 2>/dev/null | /usr/bin/grep 'IP Address: "..p_ip.."' 2>/dev/null")
			local result = (#fd:read("a*") > 0)
			fd:close()
			return result
		end;
	

	--[[===========================================================================================================================================================================================]]
		function mkyboot:nbdFree(p_ip)
				local l_id,i,v = mkyboot.inc.GetIDFromIPv4(p_ip);
			if l_id == nil then mkyboot.inc.log.warn("NBD", "nbdFree: unknown IP "..p_ip); return end

			if mkyboot.cfg.wks[l_id].img ~=nil then

				for i,v in pairs(mkyboot.cfg.wks[l_id].img) do

				if mkyboot.inc.isFile(mkyboot.cfg.server.imgbackdir.."/"..v.path..mkyboot.cfg.server.image_prefix..mkyboot.cfg.wks[l_id].mac:gsub('%W','')) and mkyboot.inc.getdev_nbd(mkyboot.inc.getpid_nbd(mkyboot.cfg.server.imgbackdir.."/"..v.path..mkyboot.cfg.server.image_prefix..mkyboot.cfg.wks[l_id].mac:gsub('%W',''))) ~= nil then  v.nbd = mkyboot.inc.getdev_nbd(mkyboot.inc.getpid_nbd(mkyboot.cfg.server.imgbackdir.."/"..v.path..mkyboot.cfg.server.image_prefix..mkyboot.cfg.wks[l_id].mac:gsub('%W',''))); else v.nbd = nil end;
				if v.nbd ~= nil then
					ngx.say(v.nbd)
					ngx.say(mkyboot.cmd.nbd.used(v.nbd))
				--	while mkyboot.cmd.nbd.used(v.nbd) do
							
									mkyboot:tgtstop(p_ip); 
									mkyboot.cmd.nbd.del(v.nbd); 
  									require("posix.unistd").sleep(0.5);
				--	end;
				 end;
				end;
			end;
			l_id,i,v = nil,nil,nil;
		end;
		function mkyboot:nbdConnect(p_ip)
			local l_id,i,v = mkyboot.inc.GetIDFromIPv4(p_ip);
			if l_id == nil then mkyboot.inc.log.warn("NBD", "nbdConnect: unknown IP "..p_ip); return end
			if l_id ~= nil and mkyboot.cfg.wks[l_id].img ~=nil then
				for i,v in pairs(mkyboot.cfg.wks[l_id].img) do
					v.nbd = mkyboot.inc.search_nbd();
					while mkyboot.cmd.nbd.used(v.nbd) do
							if tostring(mkyboot.cfg.server.debug) == "1" then io.write("blocked: "); print(mkyboot.cmd.nbd.usewho(v.nbd)); end;
							if mkyboot.cmd.nbd.usewho(v.nbd) == 2 then 
								while mkyboot.cmd.tgt.used(mkyboot.cfg.iscsi.iqn..":"..mkyboot.cfg.wks[l_id].mac:gsub('%W','')) do 
									mkyboot:tgtstop(p_ip); 
									require("posix.unistd").sleep(1);
								end
							elseif  mkyboot.cmd.nbd.usewho(v.nbd) == 1 then 
								mkyboot.cmd.nbd.del(v.nbd); 
							end;
							require("posix.unistd").sleep(0.5);
					end;
					while mkyboot.cmd.nbd.used(v.nbd) do
						require("posix.unistd").sleep(0.5);
					end
					if not mkyboot.cmd.nbd.used(v.nbd) then
						if tostring(mkyboot.cfg.server.debug) == "1" then io.write("unblocked: "); print(v.nbd); end;
						if v.path ~= nil and tostring(v.enable) == "1" then
							if tostring(v.enable) == "1"  and v.type == "dyndisk" or v.type == "dynblock" then
								mkyboot.cmd.nbd.add(v.nbd,mkyboot.cfg.server.imgbackdir.."/"..v.path..mkyboot.cfg.server.image_prefix..mkyboot.cfg.wks[l_id].mac:gsub('%W',''),v.cache)	
							end;
						end;
					end;					
				end;
			end;
			l_id,i,v = nil,nil,nil;
		end;
		function mkyboot:LunAdd(p_ip)
			local l_id,i,v = mkyboot.inc.GetIDFromIPv4(p_ip);
			if l_id == nil then mkyboot.inc.log.warn("ISCSI", "LunAdd: unknown IP "..p_ip); return end
				if mkyboot.inc.checkconf() then
					while not mkyboot.cmd.tgt.used(mkyboot.cfg.iscsi.iqn..":"..mkyboot.cfg.wks[l_id].mac:gsub('%W','')) do
						mkyboot:tgtstart(p_ip)
						require("posix.unistd").sleep(1);
					end;
					if mkyboot.cmd.tgt.used(mkyboot.cfg.iscsi.iqn..":"..mkyboot.cfg.wks[l_id].mac:gsub('%W','')) then

						for i,v in ipairs(mkyboot.cfg.wks[l_id].img) do
								if tostring(mkyboot.cfg.server.debug) == "1" then print(v.nbd,mkyboot.cfg.server.imgbackdir.."/"..v.path..mkyboot.cfg.server.image_prefix..mkyboot.cfg.wks[l_id].mac:gsub('%W','')); end;
								if tostring(v.enable) == "1" and v.type == "dyndisk" and mkyboot.cfg.wks[l_id].tid ~= nil and mkyboot.inc.isFile(v.nbd) then
								if tostring(mkyboot.cfg.server.debug) == "1" then print(mkyboot.cfg.wks[l_id].tid); end;
									if tostring(mkyboot.cfg.server.debug) == "1" then ngx.say("ADD 1:  :  : NUM:",mkyboot.cfg.wks[l_id].tid,i,v.nbd); end;
									mkyboot.cmd.lun.add(mkyboot.cfg.wks[l_id].tid,i,v.nbd)
								end;
								if tostring(v.enable) == "1" and v.type == "dynblock" and mkyboot.cfg.wks[l_id].tid ~= nil and mkyboot.inc.isFile(v.nbd) then
									if tostring(mkyboot.cfg.server.debug) == "1" then ngx.say("ADD 1:  :  : NUM:",mkyboot.cfg.wks[l_id].tid,i,v.nbd); end;
									mkyboot.cmd.lun.add(mkyboot.cfg.wks[l_id].tid,i,v.nbd)
								end;
								if tostring(v.enable) == "1" and v.type == "iso" then ngx.say("ADD 3: "..l_id.."  :  : NUM:",mkyboot.cfg.server.imgisodir.."/"..v.path); end;
								if tostring(v.enable) == "1" and v.type == "iso" and mkyboot.cfg.wks[l_id].tid ~= nil and mkyboot.inc.isFile(mkyboot.cfg.server.imgisodir.."/"..v.path) then
									if tostring(mkyboot.cfg.server.debug) == "1" then ngx.say("ADD 1111:  :  : NUM:",mkyboot.cfg.server.imgisodir.."/"..v.path); end;
										ngx.say("PATH: ", mkyboot.cfg.wks[l_id].tid,i,mkyboot.cfg.server.imgisodir.."/"..v.path.." -Y cd")									
									mkyboot.cmd.lun.add(mkyboot.cfg.wks[l_id].tid,i,mkyboot.cfg.server.imgisodir.."/"..v.path.." -Y cd")
								end;
						end;
					end;					
				end;
		end;
		function mkyboot:ImgCommit(p_ip)
			local l_id = mkyboot.inc.GetIDFromIPv4(p_ip)
			if l_id == nil then mkyboot.inc.log.warn("IMG", "ImgCommit: unknown IP "..p_ip); return false end
			if mkyboot.cfg.wks[l_id].supper ~= "1" then mkyboot.inc.log.warn("IMG", "ImgCommit: client "..mkyboot.cfg.wks[l_id].name.." not in super mode"); return false end

			mkyboot.inc.log.info("IMG", "ImgCommit: committing changes for "..mkyboot.cfg.wks[l_id].name.." ("..p_ip..")")
			local l_vid = mkyboot.cfg.wks[l_id].mac:gsub('%W','')

			for i, v in ipairs(mkyboot.cfg.wks[l_id].img) do
				if tostring(v.enable) == "1" and tostring(v.commit) == "1" and v.type == "dyndisk" then
					local p_child = mkyboot.cfg.server.imgbackdir.."/"..v.path..mkyboot.cfg.server.image_prefix..l_vid
					if mkyboot.inc.isFile(p_child) then
						while mkyboot.cmd.img.used(p_child) do
							mkyboot.inc.lsofkill(p_child)
							require("posix.unistd").sleep(0.5)
						end
						mkyboot.inc.log.info("IMG", "ImgCommit: committing "..p_child)
						mkyboot.cmd.img.commit(p_child)
						mkyboot.inc.log.info("IMG", "ImgCommit: committed "..p_child)
					else
						mkyboot.inc.log.warn("IMG", "ImgCommit: child image not found: "..p_child)
					end
				end
			end

			mkyboot.cfg.wks[l_id].supper = "0"
			for i = 1, 3 do
				if mkyboot.cfg.wks[l_id].img[i] ~= nil then
					mkyboot.cfg.wks[l_id].img[i].commit = "0"
				end
			end
			mkyboot:SaveToFile(mkyboot.cfg.server.workdir.."/"..mkyboot.cfg.server.distdir.."/cfg/"..mkyboot.cfg.server.config, mkyboot.cfg)
			mkyboot.inc.log.info("IMG", "ImgCommit: done for "..mkyboot.cfg.wks[l_id].name)
			return true
		end;
		function mkyboot:zfsmount(p_ip)
			local l_id,zdest,zpoint = mkyboot.inc.GetIDFromIPv4(p_ip)
			if l_id == nil then mkyboot.inc.log.warn("ZFS", "zfsmount: unknown IP "..p_ip); return end
			if l_id ~= nil then 
				zpoint = mkyboot.cfg.zfs.mpoint.."/"..mkyboot.cfg.wks[l_id].mac:gsub('%W','');
				zdest = mkyboot.cfg.zfs.dpoint..mkyboot.cfg.wks[l_id].mac:gsub('%W','');
			end;
			if mkyboot.cfg.wks[l_id] ~= nil  then mkyboot:nbdFree(p_ip); mkyboot.cmd.zfs.unmount(zpoint); mkyboot.cmd.zfs.unsnap(zdest); lfs.rmdir(zpoint); while not mkyboot.cmd.zfs.mtab(zdest) do  require("posix.unistd").sleep(1); mkyboot.cmd.zfs.snap(zdest); lfs.mkdir(zpoint); mkyboot.cmd.zfs.mount(zdest, zpoint); end;
				for i,v in ipairs(mkyboot.cfg.wks[l_id].img) do
					if tostring(v.enable) == "1" and v.type == "dynblock" then
						mkyboot.cmd.zfs.unsnap(mkyboot.cfg.zfs.snadev.."/"..v.path.."@"..mkyboot.cfg.zfs.tmpname.."_"..mkyboot.cfg.wks[l_id].mac:gsub('%W',''));
						mkyboot.cmd.zfs.snap(mkyboot.cfg.zfs.snadev.."/"..v.path.."@"..mkyboot.cfg.zfs.tmpname.."_"..mkyboot.cfg.wks[l_id].mac:gsub('%W',''));
						ngx.say("ZFS: "..mkyboot.cfg.zfs.snadev.."/"..v.path.."@"..mkyboot.cfg.zfs.tmpname.."_"..mkyboot.cfg.wks[l_id].mac:gsub('%W',''))
					end;
				end;
			end;				
		end;	
		function mkyboot:zfsdemount(p_ip)
			local l_id,zdest,zpoint = mkyboot.inc.GetIDFromIPv4(p_ip)
			if l_id == nil then mkyboot.inc.log.warn("ZFS", "zfsdemount: unknown IP "..p_ip); return end
			if l_id ~= nil then zpoint = mkyboot.cfg.zfs.mpoint.."/"..mkyboot.cfg.wks[l_id].mac:gsub('%W',''); zdest = mkyboot.cfg.zfs.dpoint..mkyboot.cfg.wks[l_id].mac:gsub('%W',''); 						end;
			if mkyboot.cfg.wks[l_id].img ~= nil  then mkyboot:nbdFree(p_ip); mkyboot.cmd.zfs.unmount(zpoint); mkyboot.cmd.zfs.unsnap(zdest); lfs.rmdir(zpoint); end;	


		end;		
	--[[===========================================================================================================================================================================================]]
	--[[ CSRF TOKEN FUNCTIONS (v2 - secure randomness)                                                                            ]]
	--[[===========================================================================================================================================================================================]]
		mkyboot.inc.csrf = {}
		mkyboot.inc.csrf._tokens = {}
		mkyboot.inc.csrf.TTL = 1800
		mkyboot.inc.csrf.generate = function()
			local token = mkyboot.inc.random.base64(32)
			if not token then return nil end
			mkyboot.inc.csrf._tokens[token] = ngx.now() + mkyboot.inc.csrf.TTL
			return token
		end
		mkyboot.inc.csrf.validate = function(token)
			if token == nil or token == "" then return false end
			local exp = mkyboot.inc.csrf._tokens[token]
			if exp == nil then return false end
			if ngx.now() > exp then
				mkyboot.inc.csrf._tokens[token] = nil
				return false
			end
			return true
		end
		mkyboot.inc.csrf.destroy = function(token)
			if token then mkyboot.inc.csrf._tokens[token] = nil end
		end
		mkyboot.inc.csrf.clean = function()
			local now = ngx.now()
			for k, v in pairs(mkyboot.inc.csrf._tokens) do
				if now > v then mkyboot.inc.csrf._tokens[k] = nil end
			end
		end
	--[[===========================================================================================================================================================================================]]
	--[[ SERVER-SIDE SESSION MANAGEMENT (v2)                                                                                      ]]
	--[[===========================================================================================================================================================================================]]
		mkyboot.inc.session = {}
		mkyboot.inc.session.dir = "/srv/mkyboot/cfg/sessions"
		mkyboot.inc.session.TTL = 1800
		mkyboot.inc.session.COOKIE_NAME = "mkyboot_session"
		mkyboot.inc.session._data = {}

		mkyboot.inc.session.init = function()
			local fd = io.open(mkyboot.inc.session.dir, "r")
			if not fd then
				lfs.mkdir(mkyboot.inc.session.dir)
			else
				fd:close()
			end
		end

		mkyboot.inc.session.generate_id = function()
			return mkyboot.inc.random.base64(32)
		end

		mkyboot.inc.session.create = function(username)
			mkyboot.inc.session.destroy_all()
			local sid = mkyboot.inc.session.generate_id()
			if not sid then return nil end
			local session_data = {
				sid = sid,
				username = username or "admin",
				created = ngx.now(),
				expires = ngx.now() + mkyboot.inc.session.TTL,
				ip = ngx.var.remote_addr or ""
			}
			mkyboot.inc.session._data[sid] = session_data
			local json_ok, json = pcall(require, "json")
			if json_ok then
				local path = mkyboot.inc.session.dir .. "/" .. sid .. ".json"
				local fd = io.open(path, "w")
				if fd then
					fd:write(json.encode(session_data))
					fd:close()
				end
			end
			return sid
		end

		mkyboot.inc.session.read = function(sid)
			if type(sid) ~= "string" or sid == "" then return nil end
			if mkyboot.inc.session._data[sid] then
				local s = mkyboot.inc.session._data[sid]
				if ngx.now() > s.expires then
					mkyboot.inc.session.destroy(sid)
					return nil
				end
				return s
			end
			local path = mkyboot.inc.session.dir .. "/" .. sid .. ".json"
			local fd = io.open(path, "r")
			if not fd then return nil end
			local content = fd:read("*a")
			fd:close()
			local json_ok, json = pcall(require, "json")
			if not json_ok then return nil end
			local ok, data = pcall(json.decode, content)
			if not ok or type(data) ~= "table" then return nil end
			if ngx.now() > (data.expires or 0) then
				mkyboot.inc.session.destroy(sid)
				return nil
			end
			mkyboot.inc.session._data[sid] = data
			return data
		end

		mkyboot.inc.session.destroy = function(sid)
			if type(sid) ~= "string" then return end
			mkyboot.inc.session._data[sid] = nil
			local path = mkyboot.inc.session.dir .. "/" .. sid .. ".json"
			os.remove(path)
		end

		mkyboot.inc.session.destroy_all = function()
			mkyboot.inc.session._data = {}
			local iter, dir_obj = lfs.dir(mkyboot.inc.session.dir)
			if iter then
				for fname in iter, dir_obj do
					if fname:match("%.json$") then
						os.remove(mkyboot.inc.session.dir .. "/" .. fname)
					end
				end
			end
		end

		mkyboot.inc.session.rotate = function(old_sid)
			local old = mkyboot.inc.session.read(old_sid)
			if old then
				mkyboot.inc.session.destroy(old_sid)
			end
			return mkyboot.inc.session.create(old and old.username or "admin")
		end

		mkyboot.inc.session.get_cookie = function()
			local cookie = ngx.var.http_cookie or ""
			return cookie:match(mkyboot.inc.session.COOKIE_NAME .. "=([^;]+)")
		end

		mkyboot.inc.session.set_cookie = function(sid, max_age)
			local age = max_age or mkyboot.inc.session.TTL
			ngx.header["Set-Cookie"] = mkyboot.inc.session.COOKIE_NAME .. "=" .. sid .. "; Path=/; HttpOnly; SameSite=Strict; Max-Age=" .. age
		end

		mkyboot.inc.session.clear_cookie = function()
			ngx.header["Set-Cookie"] = mkyboot.inc.session.COOKIE_NAME .. "=; Path=/; HttpOnly; SameSite=Strict; Max-Age=0"
		end

		mkyboot.inc.session.validate = function()
			local sid = mkyboot.inc.session.get_cookie()
			if not sid then return nil end
			local session = mkyboot.inc.session.read(sid)
			if not session then return nil end
			return session
		end
	--[[===========================================================================================================================================================================================]]
	--[[ LOGIN RATE LIMITING (v2)                                                                                                  ]]
	--[[===========================================================================================================================================================================================]]
		mkyboot.inc.ratelimit = {}
		mkyboot.inc.ratelimit._attempts = {}
		mkyboot.inc.ratelimit.MAX_ATTEMPTS = 5
		mkyboot.inc.ratelimit.LOCKOUT_SECONDS = 300
		mkyboot.inc.ratelimit.FILE = "/srv/mkyboot/cfg/ratelimit.json"

		mkyboot.inc.ratelimit._load = function()
			local fd = io.open(mkyboot.inc.ratelimit.FILE, "r")
			if not fd then return {} end
			local content = fd:read("*a")
			fd:close()
			local json_ok, json = pcall(require, "json")
			if not json_ok then return {} end
			local ok, data = pcall(json.decode, content)
			if not ok or type(data) ~= "table" then return {} end
			return data
		end

		mkyboot.inc.ratelimit._save = function(data)
			local json_ok, json = pcall(require, "json")
			if not json_ok then return end
			local fd = io.open(mkyboot.inc.ratelimit.FILE, "w")
			if fd then
				fd:write(json.encode(data))
				fd:close()
			end
		end

		mkyboot.inc.ratelimit.is_locked = function(ip)
			if type(ip) ~= "string" or ip == "" then return false end
			local now = ngx.now()
			local data = mkyboot.inc.ratelimit._load()
			local entry = data[ip]
			if not entry then return false end
			if entry.locked_until and now < entry.locked_until then
				return true
			end
			if entry.locked_until and now >= entry.locked_until then
				data[ip] = nil
				mkyboot.inc.ratelimit._save(data)
			end
			return false
		end

		mkyboot.inc.ratelimit.record_failure = function(ip)
			if type(ip) ~= "string" or ip == "" then return end
			local now = ngx.now()
			local data = mkyboot.inc.ratelimit._load()
			local entry = data[ip] or { count = 0, first_at = now }
			entry.count = entry.count + 1
			entry.last_at = now
			if entry.count >= mkyboot.inc.ratelimit.MAX_ATTEMPTS then
				entry.locked_until = now + mkyboot.inc.ratelimit.LOCKOUT_SECONDS
				mkyboot.inc.log.warn("SECURITY", "Rate limit triggered for " .. ip .. " (" .. entry.count .. " failures)")
			end
			data[ip] = entry
			mkyboot.inc.ratelimit._save(data)
		end

		mkyboot.inc.ratelimit.clear = function(ip)
			if type(ip) ~= "string" or ip == "" then return end
			local data = mkyboot.inc.ratelimit._load()
			data[ip] = nil
			mkyboot.inc.ratelimit._save(data)
		end

		mkyboot.inc.ratelimit.cleanup = function()
			local now = ngx.now()
			local data = mkyboot.inc.ratelimit._load()
			local changed = false
			for ip, entry in pairs(data) do
				if entry.locked_until and now >= entry.locked_until then
					data[ip] = nil
					changed = true
				elseif entry.last_at and (now - entry.last_at) > 86400 then
					data[ip] = nil
					changed = true
				end
			end
			if changed then mkyboot.inc.ratelimit._save(data) end
		end

		mkyboot.inc.session.init()
	--[[===========================================================================================================================================================================================]]
	--[[ JSON API ENDPOINTS                                                                                                        ]]
	--[[===========================================================================================================================================================================================]]
		mkyboot.inc.api = {}
		mkyboot.inc.api.list_clients = function()
			local result = {}
			for i,v in ipairs(mkyboot.cfg.wks) do
				if v ~= nil and v.name ~= nil then
					result[i] = {
						id = i,
						tid = v.tid,
						name = v.name,
						ipv4 = v.ipv4,
						mac = v.mac,
						enable = v.enable,
						group = v.group,
						supper = v.supper,
						fileboot = v.fileboot,
						online = mkyboot:checkstatpc(v.ipv4),
						img = {}
					}
					if v.img then
						for j,img in ipairs(v.img) do
							result[i].img[j] = {
								path = img.path,
								type = img.type,
								boot = img.boot,
								enable = img.enable,
								cache = img.cache
							}
						end
					end
				end
			end
			return result
		end
		mkyboot.inc.api.get_client = function(id)
			local idx = tonumber(id)
			if idx == nil or mkyboot.cfg.wks[idx] == nil then return nil end
			local v = mkyboot.cfg.wks[idx]
			local result = {
				id = idx, tid = v.tid, name = v.name, ipv4 = v.ipv4, mac = v.mac,
				enable = v.enable, group = v.group, supper = v.supper, fileboot = v.fileboot,
				gateway = v.gateway, dns = v.dns, domainsearch = v.domainsearch,
				online = mkyboot:checkstatpc(v.ipv4), img = v.img or {}, opt = v.opt or {}
			}
			return result
		end
		mkyboot.inc.api.list_images = function()
			local result = { boot = {}, iso = {}, storages = {} }
			result.boot = mkyboot.inc.ls_files(mkyboot.cfg.server.imgdir) or {}
			result.iso = mkyboot.inc.ls_files(mkyboot.cfg.server.imgisodir) or {}
			result.storages = mkyboot.inc.ls_devices(mkyboot.cfg.zfs.devpoint) or {}
			return result
		end
		mkyboot.inc.api.get_server_status = function()
			local status = {
				server = {
					ipv4 = mkyboot.cfg.server.ipv4,
					version = mkyboot.cfg.server.version,
					vendor = mkyboot.cfg.server.vendor
				},
				clients = { total = 0, online = 0, offline = 0 },
				iscsi = { port = mkyboot.cfg.iscsi.port, iqn = mkyboot.cfg.iscsi.iqn },
				images = mkyboot.inc.api.list_images(),
				services = {
					dhcp = mkyboot.inc.lsof("-t -i:"..mkyboot.cfg.dhcp.port),
					tftp = mkyboot.inc.lsof("-t -i:"..mkyboot.cfg.tftp.port),
					iscsi = mkyboot.inc.lsof("-t -i:"..mkyboot.cfg.iscsi.port)
				}
			}
			for i,v in ipairs(mkyboot.cfg.wks) do
				if v ~= nil and v.name ~= nil then
					status.clients.total = status.clients.total + 1
					if mkyboot:checkstatpc(v.ipv4) then
						status.clients.online = status.clients.online + 1
					else
						status.clients.offline = status.clients.offline + 1
					end
				end
			end
			return status
		end
	--[[===========================================================================================================================================================================================]]	
	mkyboot.inc.web = {}
	mkyboot.inc.web.pcListen = function() 
		local k,v,i,l_id,l_supper,l_status
			for k,v in ipairs(mkyboot.cfg.wks) do
				if v ~= nil and v.name ~= nil then
					-- if tostring(v.supper) == "1" then l_supper = '<b style="color:tomato;">YES</b>' else l_supper = "NO";
					if tostring(v.supper) == "1" and mkyboot:checkstatpc(v.ipv4) then  ngx.say("<tr class=\"ContextMenuTr\" style=\"font-weight: 600;color: tomato;  \"><td><svg width=\"2em\" height=\"1em\" viewBox=\"0 0 16 16\" class=\"bi bi-tv-fill\" fill=\"currentColor\" xmlns=\"http://www.w3.org/2000/svg\"><path fill-rule=\"evenod\" d=\"M2.5 13.5A.5.5 0 0 1 3 13h10a.5.5 0 0 1 0 1H3a.5.5 0 0 1-.5-.5zM2 2h12s2 0 2 2v6s0 2-2 2H2s-2 0-2-2V4s0-2 2-2z\"/></svg></td><td>",v.tid,"</td><td>",v.name,"</td><td>",v.ipv4,"</td><td>",v.mac,"</td><td>power on</td><td>yes</td><td>",v.fileboot,"</td><td>",v.img[1].path,"</td><td>",v.img[2].path,"</td><td>",v.img[3].path,"</td></tr>"); 
					elseif tostring(v.supper) == "1" and not mkyboot:checkstatpc(v.ipv4) then ngx.say("<tr class=\"ContextMenuTr\" style=\"font-weight: 600;color: #f9c3b9;  \"><td><svg width=\"2em\" height=\"1em\" viewBox=\"0 0 16 16\" class=\"bi bi-tv-fill\" fill=\"currentColor\" xmlns=\"http://www.w3.org/2000/svg\"><path fill-rule=\"evenod\" d=\"M2.5 13.5A.5.5 0 0 1 3 13h10a.5.5 0 0 1 0 1H3a.5.5 0 0 1-.5-.5zM2 2h12s2 0 2 2v6s0 2-2 2H2s-2 0-2-2V4s0-2 2-2z\"/></svg></td><td>",v.tid,"</td><td>",v.name,"</td><td>",v.ipv4,"</td><td>",v.mac,"</td><td>power off</td><td>yes</td><td>",v.fileboot,"</td><td>",v.img[1].path,"</td><td>",v.img[2].path,"</td><td>",v.img[3].path,"</h5></td></tr>"); 
					elseif tostring(v.supper) == "0" and mkyboot:checkstatpc(v.ipv4) then ngx.say("<tr class=\"ContextMenuTr\" style=\"font-weight: 600;color: #4e73df; \"><td><svg width=\"2em\" height=\"1em\" viewBox=\"0 0 16 16\" class=\"bi bi-tv-fill\" fill=\"currentColor\" xmlns=\"http://www.w3.org/2000/svg\"><path fill-rule=\"evenod\" d=\"M2.5 13.5A.5.5 0 0 1 3 13h10a.5.5 0 0 1 0 1H3a.5.5 0 0 1-.5-.5zM2 2h12s2 0 2 2v6s0 2-2 2H2s-2 0-2-2V4s0-2 2-2z\"/></svg></td><td>",v.tid,"</td><td>",v.name,"</td><td>",v.ipv4,"</td><td>",v.mac,"</td><td>power on</td><td>no</td><td>",v.fileboot,"</td><td>",v.img[1].path,"</td><td>",v.img[2].path,"</td><td>",v.img[3].path,"</td></tr>");
					elseif tostring(v.supper) == "0" and not mkyboot:checkstatpc(v.ipv4) then ngx.say("<tr class=\"ContextMenuTr\" style=\"font-weight: 600;color: #868686;  \"><td><svg width=\"2em\" height=\"1em\" viewBox=\"0 0 16 16\" class=\"bi bi-tv-fill\" fill=\"currentColor\" xmlns=\"http://www.w3.org/2000/svg\"><path fill-rule=\"evenod\" d=\"M2.5 13.5A.5.5 0 0 1 3 13h10a.5.5 0 0 1 0 1H3a.5.5 0 0 1-.5-.5zM2 2h12s2 0 2 2v6s0 2-2 2H2s-2 0-2-2V4s0-2 2-2z\"/></svg></td><td>",v.tid,"</td><td>",v.name,"</td><td>",v.ipv4,"</td><td>",v.mac,"</td><td>power off</td><td>no</td><td>",v.fileboot,"</td><td>",v.img[1].path,"</td><td>",v.img[2].path,"</td><td>",v.img[3].path,"</h5></td></tr>"); 
					end;
					
  				 end;
  				 end

			end;
		k,v,i,l_id,l_supper,l_status = nil,nil,nil,nil,nil,nil
 --<tr><td>1</td><td>PC001</td><td>Germany</td><td>Alfreds Futterkiste</td><td>Maria Anders</td><td>Germany</td><td>Alfreds Futterkiste</td><td>Maria Anders</td><td>Germany</td></tr>
	--[[===========================================================================================================================================================================================]]		
--return mkyboot
		function mkyboot:GetPage()
			mkyboot.inc.monit()
			ngargs  = ngx.req.read_body();

			if mkyboot.inc.checkconf() then
				if ngx.var.arg_getmebootargs == ngx.var.remote_addr and ngx.var.remote_addr ~= "::1"  then
					
						local l_id,l_num,l_key = mkyboot.inc.GetIDFromIPv4(ngx.var.remote_addr);
						if l_id == nil then
							mkyboot.inc.log.warn("PXE", "Boot request from unknown IP: "..ngx.var.remote_addr)
							ngx.say("#!ipxe\n:failed\necho Unknown client "..ngx.var.remote_addr.."\nshell\n")
							return
						end
						mkyboot.inc.log.info("PXE", "Boot request from "..ngx.var.remote_addr.." (client: "..mkyboot.cfg.wks[l_id].name..")")
						ngx.say("#!ipxe\n");
						ngx.say("set  initiator-iqn "..mkyboot.cfg.iscsi.iqn..":"..mkyboot.cfg.wks[l_id].mac:gsub('%W','').."\n");
						for l_num,l_key in ipairs(mkyboot.cfg.wks[l_id].img) do
							if tostring(l_key.boot) == "1" then  ngx.say("set root-path iscsi:${next-server}:"..mkyboot.cfg.iscsi.proto..":"..mkyboot.cfg.iscsi.port..":"..l_num..":"..mkyboot.cfg.iscsi.iqn..":"..mkyboot.cfg.wks[l_id].mac:gsub('%W','').."\n"); end;
							if tostring(l_key.boot) == "2" then  ngx.say("set root0 iscsi:${next-server}:"..mkyboot.cfg.iscsi.proto..":"..mkyboot.cfg.iscsi.port..":"..l_num..":"..mkyboot.cfg.iscsi.iqn..":"..mkyboot.cfg.wks[l_id].mac:gsub('%W','').."\n"); end;
							if tostring(l_key.boot) == "3" then  ngx.say("set root1 iscsi:${next-server}:"..mkyboot.cfg.iscsi.proto..":"..mkyboot.cfg.iscsi.port..":"..l_num..":"..mkyboot.cfg.iscsi.iqn..":"..mkyboot.cfg.wks[l_id].mac:gsub('%W','').."\n"); end;
						end;
						ngx.say(mkyboot.cfg.web.pages.ipxe.body:gsub("([\n])", '\n'));
						ngx.say(mkyboot.cfg.web.pages.ipxe.footer:gsub("([\n])", '\n'));
						if  tostring(mkyboot.cfg.wks[l_id].supper) == "1"  then
							mkyboot.inc.monit();
							mkyboot:tgtstop(ngx.var.remote_addr);
							mkyboot:nbdFree(ngx.var.remote_addr);
							mkyboot:zfsmount(ngx.var.remote_addr);
							mkyboot:mkChild(ngx.var.remote_addr);
							mkyboot:nbdConnect(ngx.var.remote_addr);
							mkyboot:LunAdd(ngx.var.remote_addr);
						else
							mkyboot.inc.monit();
							mkyboot:tgtstop(ngx.var.remote_addr);
							mkyboot:nbdFree(ngx.var.remote_addr);
							mkyboot:zfsmount(ngx.var.remote_addr);
							mkyboot:mkChild(ngx.var.remote_addr);
							mkyboot:nbdConnect(ngx.var.remote_addr);
							mkyboot:LunAdd(ngx.var.remote_addr);
						end;

			--[[ FIRST-RUN SETUP HANDLER ]]--
			elseif ngx.var.arg_setup == "true" and ngx.req.get_method() == "POST" then
				if mkyboot.inc.auth.is_configured() then
					ngx.say("ERROR: Already configured")
					return
				end
				local body = mkyboot.inc.parse_post(ngx.req.get_body_data())
				if body == nil or body.password == nil or #body.password < mkyboot.inc.auth.MIN_PASSWORD_LENGTH then
					ngx.say("ERROR: Password must be at least " .. mkyboot.inc.auth.MIN_PASSWORD_LENGTH .. " characters")
					return
				end
				if body.confirm ~= body.password then
					ngx.say("ERROR: Passwords do not match")
					return
				end
				local ok, msg = mkyboot.inc.auth.setup_admin(body.password)
				if ok then
					mkyboot.inc.log.info("AUTH", "Admin account configured from "..ngx.var.remote_addr)
					ngx.say("OK")
				else
					ngx.say("ERROR: " .. tostring(msg))
				end
				return

			--[[ LOGIN HANDLER ]]--
			elseif ngx.var.arg_login == "true" and ngx.req.get_method() == "POST" then
				local client_ip = ngx.var.remote_addr or "unknown"
				if mkyboot.inc.ratelimit.is_locked(client_ip) then
					mkyboot.inc.log.warn("AUTH", "Login blocked by rate limit from "..client_ip)
					ngx.say("ERROR: Too many failed attempts. Try again later.")
					return
				end
				local body = mkyboot.inc.parse_post(ngx.req.get_body_data())
				if body == nil or body.login == nil or body.pass == nil then
					mkyboot.inc.log.warn("AUTH", "Login attempt with missing credentials from "..client_ip)
					ngx.say("ERROR: Invalid request")
					return
				end
				if not mkyboot.inc.auth.is_configured() then
					ngx.say("ERROR: No admin configured. Use ?setup=true")
					return
				end
				local ok, msg = mkyboot.inc.auth.check_password(body.pass)
				if ok then
					local sid = mkyboot.inc.session.create(body.login)
					if sid then
						mkyboot.inc.session.set_cookie(sid)
						mkyboot.inc.ratelimit.clear(client_ip)
						mkyboot.inc.log.info("AUTH", "Successful login from "..client_ip.." user="..body.login)
						ngx.say("OK")
					else
						mkyboot.inc.log.error("AUTH", "Session creation failed from "..client_ip)
						ngx.say("ERROR: Session error")
					end
				else
					mkyboot.inc.ratelimit.record_failure(client_ip)
					mkyboot.inc.log.warn("AUTH", "Failed login attempt from "..client_ip)
					ngx.say("ERROR: Invalid credentials")
				end
				return

			--[[ LOGOUT HANDLER ]]--
			elseif ngx.var.arg_logout == "true" then
				local sid = mkyboot.inc.session.get_cookie()
				if sid then mkyboot.inc.session.destroy(sid) end
				mkyboot.inc.session.clear_cookie()
				mkyboot.inc.log.info("AUTH", "Logout from "..(ngx.var.remote_addr or "unknown"))
				ngx.say("OK")
				return

			--[[ PASSWORD CHANGE HANDLER ]]--
			elseif ngx.var.arg_changepw == "true" and ngx.req.get_method() == "POST" then
				local session = mkyboot.inc.session.validate()
				if not session then
					ngx.say("ERROR: Not authenticated")
					return
				end
				local body = mkyboot.inc.parse_post(ngx.req.get_body_data())
				if body == nil or body.current_password == nil or body.new_password == nil or body.confirm_password == nil then
					ngx.say("ERROR: All fields required")
					return
				end
				if body.new_password ~= body.confirm_password then
					ngx.say("ERROR: New passwords do not match")
					return
				end
				local ok, msg = mkyboot.inc.auth.change_password(body.current_password, body.new_password)
				if ok then
					local old_sid = mkyboot.inc.session.get_cookie()
					local new_sid = mkyboot.inc.session.create(session.username)
					if new_sid then
						mkyboot.inc.session.destroy(old_sid)
						mkyboot.inc.session.set_cookie(new_sid)
					end
					mkyboot.inc.log.info("AUTH", "Password changed by "..session.username.." from "..(ngx.var.remote_addr or "unknown"))
					ngx.say("OK")
				else
					ngx.say("ERROR: " .. tostring(msg))
				end
				return


			    elseif ngx.req.get_body_data() then
			    		local file,temp,l_v,l_k
			    			--[[ AUTH: Session check for all state-changing operations ]]--
			    			if not mkyboot.inc.session.validate() then
			    				ngx.say("ERROR: Not authenticated")
			    				return
			    			end
			    			local origin = ngx.var.http_origin or ngx.var.http_referer or ""
			    			local server_host = mkyboot.cfg.server.ipv4 or "127.0.0.1"
			    			if origin ~= "" and not origin:find(server_host, 1, true) and origin ~= "http://127.0.0.1:8888" and origin ~= "http://localhost:8888" then
			    				mkyboot.inc.log.warn("SECURITY", "CSRF rejected: origin="..origin)
			    				ngx.say("ERROR: Invalid origin")
			    				return
			    			end
			    			temp = mkyboot.inc.parse_post(ngx.req.get_body_data())
			    			if temp == nil then ngx.say("ERROR: Invalid request"); return end
			    			if mkyboot.inc.isFile(mkyboot.cfg.server.workdir.."/"..mkyboot.cfg.server.distdir.."/cfg/"..mkyboot.cfg.server.config) then mkyboot.cfg = mkyboot:LoadFromFile(mkyboot.cfg.server.workdir.."/"..mkyboot.cfg.server.distdir.."/cfg/"..mkyboot.cfg.server.config) else mkyboot.cfg = dofile("/srv/mkyboot/cfg/cfg.lua").cfg; mkyboot:SaveToFile(mkyboot.cfg.server.workdir.."/"..mkyboot.cfg.server.distdir.."/cfg/"..mkyboot.cfg.server.config,mkyboot.cfg); end;
			    				if temp['id'] ~= nil and temp['supper'] == "true" and mkyboot.inc.checkconf() and mkyboot.cfg.wks[tonumber(temp['id'])] ~= nil then
			    					mkyboot.cfg =  mkyboot:LoadFromFile(mkyboot.cfg.server.workdir.."/"..mkyboot.cfg.server.distdir.."/cfg/"..mkyboot.cfg.server.config);
			    					if temp['jsondata'] ~= nil then 
			    						local u_i,u_k
			    							for u_i,u_k in ipairs(json.decode(mkyboot.inc.unescape(temp['jsondata']))) do
			    								mkyboot.cfg.wks[tonumber(temp['id'])].img[tonumber(u_k)].commit = "1"

			    							end;
			    						u_i,u_k = nil,nil
			    					mkyboot.cfg.wks[tonumber(temp['id'])].supper = "1"
			    					mkyboot:SaveToFile(mkyboot.cfg.server.workdir.."/"..mkyboot.cfg.server.distdir.."/cfg/"..mkyboot.cfg.server.config, mkyboot.cfg);
			    					end;
			    					ngx.say("OK")
			    				elseif temp['id'] ~= nil and temp['supper'] == "disableUncommit" and mkyboot.inc.checkconf() and mkyboot.cfg.wks[tonumber(temp['id'])] then
			    					mkyboot.cfg =  mkyboot:LoadFromFile(mkyboot.cfg.server.workdir.."/"..mkyboot.cfg.server.distdir.."/cfg/"..mkyboot.cfg.server.config);
			    					mkyboot.cfg.wks[tonumber(temp['id'])].supper = "0"
			    					mkyboot.cfg.wks[tonumber(temp['id'])].img[1].commit = "0"
			    					mkyboot.cfg.wks[tonumber(temp['id'])].img[2].commit = "0"
			    					mkyboot.cfg.wks[tonumber(temp['id'])].img[3].commit = "0"
			    					mkyboot:SaveToFile(mkyboot.cfg.server.workdir.."/"..mkyboot.cfg.server.distdir.."/cfg/"..mkyboot.cfg.server.config, mkyboot.cfg);
			    					ngx.say("OK")
			    				elseif temp['id'] ~= nil and temp['supper'] == "disableCommit" and mkyboot.inc.checkconf() and mkyboot.cfg.wks[tonumber(temp['id'])] then
			    					mkyboot.cfg =  mkyboot:LoadFromFile(mkyboot.cfg.server.workdir.."/"..mkyboot.cfg.server.distdir.."/cfg/"..mkyboot.cfg.server.config);
										if temp['id'] ~= nil then 
			    						local u_i,u_k
			    								if mkyboot.cfg.wks[tonumber(temp['id'])] ~= nil and mkyboot.cfg.wks[tonumber(temp['id'])].supper == "1" then
			    									for u_i,u_k in pairs(mkyboot.cfg.wks[tonumber(temp['id'])].img) do
			    										if tostring(mkyboot.cfg.wks[tonumber(temp['id'])].img[u_i].enable) == "1" and tostring(mkyboot.cfg.wks[tonumber(temp['id'])].img[u_i].commit) == "1" and mkyboot.cfg.wks[tonumber(temp['id'])].img[u_i].type ~= "iso" then
			    											p_child = mkyboot.cfg.server.imgbackdir.."/"..mkyboot.cfg.wks[tonumber(temp['id'])].img[u_i].path..mkyboot.cfg.server.image_prefix..mkyboot.cfg.wks[tonumber(temp['id'])].mac:gsub('%W','');
			    											if mkyboot.inc.isFile(p_child) then
			    													if mkyboot.cfg.wks[tonumber(temp['id'])].ipv4 ~= nil then
			    											 			mkyboot:tgtstop(mkyboot.cfg.wks[tonumber(temp['id'])].ipv4);
																		mkyboot:nbdFree(mkyboot.cfg.wks[tonumber(temp['id'])].ipv4);
																	end;
			    												-- while mkyboot.cmd.img.used(p_child) do
			    												-- 	require("posix.unistd").sleep(0.5);
			    												-- end;	
			    												mkyboot.cmd.img.commit(p_child);
			    												ngx.say("Commited : ", p_child, " ",mkyboot.cfg.wks[tonumber(temp['id'])].ipv4);
			    											else
			    												ngx.say("False");
			    											end;
			    												
			    										end;
			    									end;
			    								end;
			    							--	p_child = mkyboot.cfg.server.imgbackdir.."/"..mkyboot.cfg.wks[tonumber(temp['id'])].img[tonumber(u_k)].commitmkyboot.cfg.wks[tonumber(temp['id'])].img[tonumber(u_k)].path..mkyboot.cfg.server.image_prefix..mkyboot.cfg.wks[tonumber(temp['id'])].img[tonumber(u_k)].commitmkyboot.cfg.wks[tonumber(temp['id'])].img[tonumber(u_k)].mac:gsub('%W','');
												-- mkyboot:tgtstop(mkyboot.cfg.wks[tonumber(temp['id'])].img[tonumber(u_i)].ipv4);
												-- mkyboot:nbdFree(mkyboot.cfg.wks[tonumber(temp['id'])].img[tonumber(u_i)].ipv4);	
												
						    					mkyboot.cfg.wks[tonumber(temp['id'])].supper = "0"
			    						u_i,u_k = nil,nil
			    						end;
			    					mkyboot.cfg.wks[tonumber(temp['id'])].supper = "0"
			    					mkyboot.cfg.wks[tonumber(temp['id'])].img[1].commit = "0"
			    					mkyboot.cfg.wks[tonumber(temp['id'])].img[2].commit = "0"
			    					mkyboot.cfg.wks[tonumber(temp['id'])].img[3].commit = "0"
			    					mkyboot:SaveToFile(mkyboot.cfg.server.workdir.."/"..mkyboot.cfg.server.distdir.."/cfg/"..mkyboot.cfg.server.config, mkyboot.cfg);

			    					ngx.say("OK")
			    				elseif temp['id'] ~= nil and temp['supper'] == "disableCommitPoint" and mkyboot.inc.checkconf() and mkyboot.cfg.wks[tonumber(temp['id'])] then
			    					mkyboot.cfg =  mkyboot:LoadFromFile(mkyboot.cfg.server.workdir.."/"..mkyboot.cfg.server.distdir.."/cfg/"..mkyboot.cfg.server.config);
			    					mkyboot.cfg.wks[tonumber(temp['id'])].supper = "0"
			    					mkyboot.cfg.wks[tonumber(temp['id'])].img[1].commit = "0"
			    					mkyboot.cfg.wks[tonumber(temp['id'])].img[2].commit = "0"
			    					mkyboot.cfg.wks[tonumber(temp['id'])].img[3].commit = "0"
			    					mkyboot:SaveToFile(mkyboot.cfg.server.workdir.."/"..mkyboot.cfg.server.distdir.."/cfg/"..mkyboot.cfg.server.config, mkyboot.cfg);
			    					ngx.say("OK")
			    				elseif temp['id'] ~= nil and temp['supper'] == "supperCheck" and mkyboot.inc.checkconf() and mkyboot.cfg.wks[tonumber(temp['id'])] then
			    						if tostring(mkyboot.cfg.wks[tonumber(temp['id'])].supper) == "1" and mkyboot:checkstatpc(mkyboot.cfg.wks[tonumber(temp['id'])].ipv4) then
			    								ngx.say("2");
			    						elseif tostring(mkyboot.cfg.wks[tonumber(temp['id'])].supper) == "1" and not mkyboot:checkstatpc(mkyboot.cfg.wks[tonumber(temp['id'])].ipv4) then
			    								ngx.say("1");
			    						else
			    								ngx.say(tostring(mkyboot.cfg.wks[tonumber(temp['id'])].supper));
			    						end;
			    				elseif temp['id'] ~= nil and temp['supper'] == "DiskList" and mkyboot.inc.checkconf() and mkyboot.cfg.wks[tonumber(temp['id'])] then
			    					local t_data = {}, t_i
			    						if mkyboot.cfg.wks[tonumber(temp['id'])].img[1].path ~= nil and mkyboot.cfg.wks[tonumber(temp['id'])].img[1].path ~= "none" and tostring(mkyboot.cfg.wks[tonumber(temp['id'])].img[1].enable) == "1" then	t_data['1'] = mkyboot.cfg.wks[tonumber(temp['id'])].img[1].path end;
			    						if mkyboot.cfg.wks[tonumber(temp['id'])].img[2].path ~= nil and mkyboot.cfg.wks[tonumber(temp['id'])].img[2].path ~= "none" and tostring(mkyboot.cfg.wks[tonumber(temp['id'])].img[2].enable) == "1" then	t_data['2'] = mkyboot.cfg.wks[tonumber(temp['id'])].img[2].path end;
			    						if mkyboot.cfg.wks[tonumber(temp['id'])].img[3].path ~= nil and mkyboot.cfg.wks[tonumber(temp['id'])].img[3].path ~= "none" and tostring(mkyboot.cfg.wks[tonumber(temp['id'])].img[3].enable) == "1" then	t_data['3'] = mkyboot.cfg.wks[tonumber(temp['id'])].img[3].path end;
			    						ngx.say(json.encode(t_data));
			    					t_data = nil
			    				end
			    				--[[ COMMAND GET WEB ADMIN ]]--
			    				if temp['id'] ~= nil and temp['cmd'] == "PowerON" and mkyboot.inc.checkconf() and mkyboot.cfg.wks[tonumber(temp['id'])] ~= nil then
			    							if tostring(mkyboot.cfg.wks[tonumber(temp['id'])].enable) == "1" and mkyboot.cfg.wks[tonumber(temp['id'])].mac ~= nil then
			    										local l_k,l_v
			    										for l_k,l_v in pairs(mkyboot.cfg.server.ifaces) do
			    											if mkyboot.cfg.server.ifaces[l_k] ~= nil then
			    												mkyboot.cmd.power.on(mkyboot.cfg.server.ifaces[l_k],mkyboot.cfg.wks[tonumber(temp['id'])].mac)
			    												ngx.say(mkyboot.cfg.server.ifaces[l_k],mkyboot.cfg.wks[tonumber(temp['id'])].mac)
			    											end;
			    										end;
			    										l_k,l_v = nil,nil
			    							end;
			    				end;
			    				if temp['id'] == "0" and temp['WKSCmd'] == "GetMy" then
									local l_k,l_v,t_id	
										for l_k,l_v in ipairs(mkyboot.cfg.wks) do
											if l_v.empty == "true" then t_id = l_k break elseif l_k == tonumber(l_v.tid) then t_id = l_k + 1  end
										end;
										local l_dns,l_gw,l_dsearch,l_ip
										if mkyboot.cfg.dhcp.config.opt['domain-name-servers'] ~= nil then 
											l_dns = mkyboot.cfg.dhcp.config.opt['domain-name-servers'] 
										elseif mkyboot.cfg.server.dns1 ~= nil then  
											l_dns = mkyboot.cfg.server.dns1 
										else
											l_dns = "127.0.0.1"
										end;
										if mkyboot.cfg.dhcp.config.opt['routers'] ~= nil then 
											l_gw = mkyboot.cfg.dhcp.config.opt['routers'] 
										elseif mkyboot.cfg.server.gateway ~= nil then  
											l_gw = mkyboot.cfg.server.gateway
										else
											l_gw = "127.0.0.1"
										end;										
										if mkyboot.cfg.dhcp.config.opt['domain-name'] ~= nil then 
											l_dsearch = mkyboot.cfg.dhcp.config.opt['domain-name']
										else
											l_dsearch = "mkyboot.local"
										end;
										if mkyboot.cfg.dhcp.config.sub[1].sub ~= nil then 
											l_ip = mkyboot.cfg.dhcp.config.sub[1].sub:gsub("[0-9]$",t_id)
										else
											l_ip = "192.168.10."..temp['id']
										end;
										l_mac = mkyboot.inc.isMacARP(l_ip):gsub('\n','')
			    					ngx.say("{\"WKS\":{\"enable\":1,\"group\":\"DEFAULT\",\"gateway\":\""..l_gw.."\",\"dns\":\""..l_dns.."\",\"domainsearch\":\""..l_dsearch.."\",\"supper\":0,\"img\":[{\"path\":\"none\",\"commit\":0,\"enable\":1,\"nbd\":\"\\/dev\\/nbd"..(t_id+2).."\",\"type\":\"dyndisk\",\"boot\":0,\"cache\":\"none\"},{\"path\":\"none\",\"commit\":0,\"enable\":1,\"nbd\":\"\\/dev\\/nbd"..(t_id+3).."\",\"type\":\"dynblock\",\"boot\":0,\"cache\":\"none\"},{\"path\":\"none\",\"commit\":0,\"enable\":1,\"nbd\":\"\\/dev\\/nbd0\",\"type\":\"iso\",\"boot\":0,\"cache\":\"none\"}],\"fileboot\":\"ipxe\",\"mac\":\""..l_mac.."\",\"tid\":"..t_id..",\"ipv4\":\""..l_ip.."\",\"opt\":[],\"name\":\"PC00"..t_id.."\",\"swp\":0},\"images\":{\"dyndisk\":"..json.encode(mkyboot.inc.ls_files(mkyboot.cfg.server.imgdir))..",\"iso\":"..json.encode(mkyboot.inc.ls_files(mkyboot.cfg.server.imgisodir))..",\"dynblock\":"..json.encode(mkyboot.inc.ls_devices(mkyboot.cfg.zfs.devpoint)).."},\"groups\":"..json.encode(mkyboot.cfg.groups.wks).."}")
				   				l_k,l_v = nil,nil
				   				l_dns,l_gw,l_dsearch,l_ip = nil,nil
				   				end;	
			    				if temp['id'] ~= "0" and temp['WKSCmd'] == "GetMy" then
									local t_id,t_temp,l_k,l_v = temp['id'], {}
				   					  ngx.say("{\"WKS\":"..json.encode(mkyboot.cfg.wks[tonumber(temp['id'])])..",".."\"images\":{\"dyndisk\":"..json.encode(mkyboot.inc.ls_files(mkyboot.cfg.server.imgdir))..",\"iso\":"..json.encode(mkyboot.inc.ls_files(mkyboot.cfg.server.imgisodir))..",\"dynblock\":"..json.encode(mkyboot.inc.ls_devices(mkyboot.cfg.zfs.devpoint)).."},\"groups\":"..json.encode(mkyboot.cfg.groups.wks).."}" )
				   				l_k,l_v,t_temp = nil,nil
				   				end;				   				
				   				if temp['id'] ~= nil and temp['WKSCmd'] == "ApplyMy" and temp['jsondata'] then
									local t_id,t_tmp = json.decode(mkyboot.inc.unescape(temp['jsondata'])).WKS.tid,json.decode(mkyboot.inc.unescape(temp['jsondata'])).WKS
											if mkyboot.cfg.wks ~= nil and mkyboot.cfg.wks[t_id] == nil then
												mkyboot.cfg.wks[tonumber(t_id)] = t_tmp
												mkyboot:SaveToFile(mkyboot.cfg.server.workdir.."/"..mkyboot.cfg.server.distdir.."/cfg/"..mkyboot.cfg.server.config,mkyboot.cfg); 
												mkyboot:ExportDHCP();
												ngx.say("SAVE CONFIGURATIONS JSON: ")

											end;

			    						
			    						

				   				l_k,l_v = nil,nil
				   				end;
				   				if temp['id'] ~= nil and temp['WKSCmd'] == "DeleteMachine" then
				   							
				   								--ngx.say(mkyboot.cfg.wks[tonumber(temp['id'])].tid)
				   								local tmpfile = io.open("/tmp/fack", "a")
				   								--tmpfile:write(json.encode(mkyboot.cfg.wks))
				   								if mkyboot.cfg.wks[tonumber(temp['id'])] ~= nil and tostring(mkyboot.cfg.wks[tonumber(temp['id'])].tid) == tostring(temp['id'])  then  mkyboot.cfg.wks[tonumber(temp['id'])] = {} mkyboot.cfg.wks[tonumber(temp['id'])].empty = "true" else ngx.say("ERROR") end --
				   								
				   								tmpfile:write(json.encode(mkyboot.cfg.wks))
				   								tmpfile:close()
				   								mkyboot:SaveToFile(mkyboot.cfg.server.workdir.."/"..mkyboot.cfg.server.distdir.."/cfg/"..mkyboot.cfg.server.config,mkyboot.cfg); 
				   								 
				   								
				   				end;
			   elseif ngx.var.arg_api == "status" then
			   		if not mkyboot.inc.session.validate() then
			   			ngx.header.content_type = 'application/json'
			   			ngx.say('{"error":"Not authenticated"}')
			   			return
			   		end
			   		ngx.header.content_type = 'application/json'
			   		ngx.say(json.encode(mkyboot.inc.api.get_server_status()))
			   elseif ngx.var.arg_api == "clients" then
			   		if not mkyboot.inc.session.validate() then
			   			ngx.header.content_type = 'application/json'
			   			ngx.say('{"error":"Not authenticated"}')
			   			return
			   		end
			   		ngx.header.content_type = 'application/json'
			   		ngx.say(json.encode(mkyboot.inc.api.list_clients()))
			   elseif ngx.var.arg_api == "client" and ngx.var.arg_id ~= nil then
			   		if not mkyboot.inc.session.validate() then
			   			ngx.header.content_type = 'application/json'
			   			ngx.say('{"error":"Not authenticated"}')
			   			return
			   		end
			   		ngx.header.content_type = 'application/json'
			   		local c = mkyboot.inc.api.get_client(ngx.var.arg_id)
			   		if c then ngx.say(json.encode(c)) else ngx.say("{}") end
			   elseif ngx.var.arg_api == "images" then
			   		if not mkyboot.inc.session.validate() then
			   			ngx.header.content_type = 'application/json'
			   			ngx.say('{"error":"Not authenticated"}')
			   			return
			   		end
			   		ngx.header.content_type = 'application/json'
			   		ngx.say(json.encode(mkyboot.inc.api.list_images()))
			   elseif ngx.var.arg_api == "logs" then
			   		if not mkyboot.inc.session.validate() then
			   			ngx.header.content_type = 'application/json'
			   			ngx.say('{"error":"Not authenticated"}')
			   			return
			   		end
			   		ngx.header.content_type = 'application/json'
			   		local lines = {}
			   		local fd = io.open(mkyboot.inc.log.file, "r")
			   		if fd then
			   			local all = fd:read("*a")
			   			fd:close()
			   			local count = 0
			   			for line in all:gmatch("[^\n]+") do
			   				count = count + 1
			   				lines[count] = line
			   				if count >= 100 then break end
			   			end
			   		end
			   		ngx.say(json.encode(lines))
			   elseif ngx.var.arg_status == "true" then
			   		--[[ CHECK AUTHENTICATION FOR ADMIN PAGES ]]--
					if not mkyboot.inc.auth.is_configured() then
						ngx.say(mkyboot.cfg.web.pages.html.setup)
						return
					end
					local session = mkyboot.inc.session.validate()
					if not session then
						ngx.say(mkyboot.cfg.web.pages.html.login)
						return
					end
			   		if mkyboot.inc.isFile(mkyboot.cfg.server.workdir.."/"..mkyboot.cfg.server.distdir.."/cfg/"..mkyboot.cfg.server.config) then mkyboot.cfg = mkyboot:LoadFromFile(mkyboot.cfg.server.workdir.."/"..mkyboot.cfg.server.distdir.."/cfg/"..mkyboot.cfg.server.config) else mkyboot.cfg = dofile("/srv/mkyboot/cfg/cfg.lua").cfg; mkyboot:SaveToFile(mkyboot.cfg.server.workdir.."/"..mkyboot.cfg.server.distdir.."/cfg/"..mkyboot.cfg.server.config,mkyboot.cfg); end;
					ngx.say(mkyboot.cfg.web.pages.html.main);
					mkyboot.inc.web.pcListen();
					ngx.say(os.date(),[[</table>
						<style>
						.bd-example-modal-lg .modal-dialog{
						    display: table;
						    position: relative;
						    margin: 0 auto;
						    top: calc(50% - 24px);
						  }
						  
						  .bd-example-modal-lg .modal-dialog .modal-content{
						    background-color: transparent;
						    border: none;
						  }
						  .bd-example-modal-lg {
						    z-index : 9999;
						  }
						</style>
				        <div class="dropdown-menu dropdown-menu-sm" id="context-menu">
				          <a class="dropdown-item" data-toggle="modal" data-target="#machineModal" onclick="UpdatemModalLabels(true);" data-id="1" href="#">Add machine</a>
				          <a class="dropdown-item" href="#">Add group</a>
				          <a class="dropdown-item" data-toggle="modal" data-target="#machineModal" onclick="UpdatemModalLabels(false);" href="#">Change machine</a>
				          <a class="dropdown-item" data-toggle="modal" data-target="#machineDeleteModal" href="#">Remove</a>
						  <a class="dropdown-item" id="enableSuperModeBtn" href="#" >Enable supper mode</a>
						  <a class="dropdown-item" data-toggle="modal" data-target="#supperModeDisableModal" id="disableSuperModeBtn" href="#">Disable supper mode</a>
				          <a class="dropdown-item" href="#" onclick=GetCommand('PowerON')>Power on</a>

				        </div>

						<div class="modal fade" id="supperModeDiskListModal">
							<div class="modal-dialog">
							  <div class="modal-content">
							  
							    <!-- Modal Header -->
							    <div class="modal-header">
							      <h4 class="modal-title">Modal Heading</h4>
							      <button type="button" class="close" data-dismiss="modal">&times;</button>
							    </div>
							    
							    <!-- Modal body -->
							    <div class="modal-body">

							    </div>
							    
							    <!-- Modal footer -->
							    <div class="modal-footer">
							      <button type="button" class="btn btn-success" data-dismiss="modal" onclick="SetSupper('true');">OK</button>
							      <button type="button" class="btn btn-danger" data-dismiss="modal">Cancel</button>
							    </div>
							    
							  </div>
							</div>
						</div>	




						<div class="modal fade" id="machineDeleteModal">
							<div class="modal-dialog">
							  <div class="modal-content">
							  
							    <!-- Modal Header -->
							    <div class="modal-header">
							      <h4 class="modal-title">Modal Heading</h4>
							      <button type="button" class="close" data-dismiss="modal">&times;</button>
							    </div>
							    
							    <!-- Modal body -->
							    <div class="modal-body">
							    	<p>Delete machine?</p>
							    </div>
							    
							    <!-- Modal footer -->
							    <div class="modal-footer">
							      <button type="button" class="btn btn-success" data-dismiss="modal" onclick="DeleteMachine();">OK</button>
							      <button type="button" class="btn btn-danger" data-dismiss="modal">Cancel</button>
							    </div>
							    
							  </div>
							</div>
						</div>	


   

						<div class="modal fade" id="machineModal">
							<div class="modal-dialog">
							  <div class="modal-content">
							  
							    <!-- Modal Header -->
							    <div class="modal-header">
							      <h4 class="modal-title">Modal Heading</h4>
							      <button type="button" class="close" data-dismiss="modal">&times;</button>
							    </div>
							    
							    <!-- Modal body -->
							    <div class="modal-body">

								  <div class="form-group row">
								    <div class="col-sm-6">
								        <label class="form-check-label" for="mEnabled">
								          ENABLE
								        </label>
								    </div>
								    <div class="col-sm-6">
								      <div class="form-check">
								      	<input type="hidden" id="mId">
								        <input class="form-check-input" type="checkbox" name="mEnabled" id="mEnabled">
								      </div>
								    </div>
								  </div>

								  <div class="form-group row">
								    <label for="mTargetId" class="col-sm-6 col-form-label">TARGET ID</label>
								    <div class="col-sm-6">
								      <input type="text" class="form-control" id="mTargetId" disabled>
								    </div>
								  </div>

								  <div class="form-group row">
								    <label for="mHostname" class="col-sm-6 col-form-label">HOSTNAME</label>
								    <div class="col-sm-6">
								      <input type="text" class="form-control" id="mHostname" placeholder="HOSTNAME">
								    </div>
								  </div>

								  <div class="form-group row">
								    <label for="mGroup" class="col-sm-6 col-form-label">GROUP</label>
								    <div class="col-sm-6">
										<select id="mGroup" class="form-control">
										  <option>Default select</option>
										</select>
								    </div>
								  </div>


 
								  <br/>
								  <hr/>
								  <br/>


								  <div class="form-group row">
								    <label for="mIpAddress" class="col-sm-6 col-form-label">IP ADDRESS</label>
								    <div class="col-sm-6">
								      <input type="text" class="form-control" id="mIpAddress" placeholder="IP ADDRESS">
								    </div>
								  </div>

								  <div class="form-group row">
								    <label for="mMacAddress" class="col-sm-6 col-form-label">MAC ADDRESS</label>
								    <div class="col-sm-6">
								      <input type="text" class="form-control" id="mMacAddress" placeholder="MAC ADDRESS">
								    </div>
								  </div>

								  <div class="form-group row">
								    <label for="mGateway" class="col-sm-6 col-form-label">GATEWAY</label>
								    <div class="col-sm-6">
								      <input type="text" class="form-control" id="mGateway" placeholder="GATEWAY">
								    </div>
								  </div>

								  <div class="form-group row">
								    <label for="mDnsServers" class="col-sm-6 col-form-label">DNS SERVERS</label>
								    <div class="col-sm-6">
								      <input type="text" class="form-control" id="mDnsServers" placeholder="DNS SERVERS">
								    </div>
								  </div>

								  <div class="form-group row">
								    <label for="mDomainSearch" class="col-sm-6 col-form-label">DOMAIN SEARCH</label>
								    <div class="col-sm-6">
								      <input type="text" class="form-control" id="mDomainSearch" placeholder="DOMAIN SEARCH">
								    </div>
								  </div>


								  <br/>
								  <hr/>
								  <br/>

								  <div class="form-group row">
								    <label for="mImgSelect" class="col-sm-6 col-form-label" data-lb1="IMAGE" data-lb2="SELECT"></label>
								    <div class="col-sm-6">
										<select id="mImgSelect" class="form-control">
										  <option value="0">IMG 1</option>
										  <option value="1">IMG 2</option>
										  <option value="2">IMG 3</option>
										</select>
								    </div>
								  </div>
								  <div class="form-group row">
								    <label for="mImgType" class="col-sm-6 col-form-label" data-lb1="IMAGE" data-lb2="TYPE"></label>
								    <div class="col-sm-6">
										<select id="mImgType" class="form-control">
										  <option value="dyndisk">dyndisk</option>
										  <option value="dynblock">dynblock</option>
										  <option value="iso">iso</option>
										</select>
								    </div>
								  </div>
								  <div class="form-group row">
								    <label for="mImgName" class="col-sm-6 col-form-label" data-lb1="IMAGE" data-lb2="NAME"></label>
								    <div class="col-sm-6">
										<select id="mImgName" class="form-control">
										</select>
								    </div>
								  </div>
								  <div class="form-group row">
								    <div class="col-sm-6">
								        <label class="form-check-label" for="mImgEnable" data-lb1="IMAGE" data-lb2="ENABLE"></label>
								    </div>
								    <div class="col-sm-6">
								      <div class="form-check">
								        <input class="form-check-input" type="checkbox" id="mImgEnable">
								      </div>
								    </div>
								  </div>
								  <div class="form-group row">
								    <label for="mImgCache" class="col-sm-6 col-form-label" data-lb1="IMAGE" data-lb2="CACHE"></label>
								    <div class="col-sm-6">
										<select id="mImgCache" class="form-control">
										  <option value="none">none</option>
										  <option value="unsafe">unsafe</option>
										  <option value="writeback">writeback</option>
										</select>
								    </div>
								  </div>

								  <br/>
								  <hr/>
								  <br/>

								  <div class="form-group row">
								    <label for="mBoot1" class="col-sm-6 col-form-label">SELECT BOOT 1</label>
								    <div class="col-sm-6">
										<select id="mBoot1" class="form-control" data-imgnum="1">
										  <option value="0">none</option>
										  <option value="1">IMG 1</option>
										  <option value="2">IMG 2</option>
										  <option value="3">IMG 3</option>
										</select>
								    </div>
								  </div>
								  <div class="form-group row">
								    <label for="mBoot2" class="col-sm-6 col-form-label">SELECT BOOT 2</label>
								    <div class="col-sm-6">
										<select id="mBoot2" class="form-control"  data-imgnum="2">
										  <option value="0">none</option>
										  <option value="1">IMG 1</option>
										  <option value="2">IMG 2</option>
										  <option value="3">IMG 3</option>
										</select>
								    </div>
								  </div>
								  <div class="form-group row">
								    <label for="mBoot3" class="col-sm-6 col-form-label">SELECT BOOT 3</label>
								    <div class="col-sm-6">
										<select id="mBoot3" class="form-control" data-imgnum="3">
										  <option value="0">none</option>
										  <option value="1">IMG 1</option>
										  <option value="2">IMG 2</option>
										  <option value="3">IMG 3</option>
										</select>
								    </div>
								  </div>
								  <div class="form-group row">
								    <label for="mPxeFile" class="col-sm-6 col-form-label">PXE FILE</label>
								    <div class="col-sm-6">
								      <input type="text" class="form-control" id="mPxeFile" placeholder="PXE FILE">
								    </div>
								  </div>
								  <div class="form-group row">
								    <label for="mHwProfile" class="col-sm-6 col-form-label">HARDWARE PROFILE</label>
								    <div class="col-sm-6">
										<select id="mHwProfile" class="form-control">
										</select>
								    </div>
								  </div>

							    </div>
							    
							    <!-- Modal footer -->
							    <div class="modal-footer">
							      <button type="button" class="btn btn-success" data-dismiss="modal" onclick="SaveMachineSettings();">OK</button>
							      <button type="button" class="btn btn-danger" data-dismiss="modal">Cancel</button>
							    </div>
							    
							  </div>
							</div>
						</div>	

						<div class="modal spinner fade bd-example-modal-lg" id="spinnerModal" data-backdrop="static" data-keyboard="false" tabindex="-1">
						    <div class="modal-dialog modal-sm">
						        <div class="modal-content" style="width: 48px">
						        <div class="spinner-border text-primary" role="status">
						          <span class="sr-only">Loading...</span>
						         </div>
						        </div>
						    </div>
						</div>

						<div class="modal fade" id="supperModeDisableModal">
							<div class="modal-dialog">
							  <div class="modal-content">
							  
							    <!-- Modal Header -->
							    <div class="modal-header">
							      <h4 class="modal-title">Modal Heading</h4>
							      <button type="button" class="close" data-dismiss="modal">&times;</button>
							    </div>
							    
							    <!-- Modal body -->
							    <div class="modal-body">
							    <form>
							    	update disks?
							    	<div class="custom-control custom-checkbox mb-3">
							    		<input type="checkbox" class="custom-control-input" id="createPointBtn">
							    		<label class="custom-control-label" for="createPointBtn">Create point</label>
							    	</div>
								    <div class="form-group">
								      <label for="pointNameInput">Name:</label>
								      <input type="text" class="form-control" disabled="disabled" id="pointNameInput">
								    </div>
							    </form>
							    </div>
							    
							    <!-- Modal footer -->
							    <div class="modal-footer">
							      <button type="button" class="btn btn-success" onclick="SetSupper('disableCommit');">yes</button>
							      <button type="button" class="btn btn-success" data-dismiss="modal" onclick="SetSupper('disableUncommit');">no</button>
							      <button type="button" class="btn btn-danger" data-dismiss="modal">Cancel</button>
							    </div>
							    
							  </div>
							</div>
						</div>	
								<script>
								]]..mkyboot.cfg.web.bootstrap.js..[[
								</script>
				        <script>
				        var mResponse = {};

				        $('#createPointBtn').change(function() {
					        if($(this).is(':checked')) {
					        	$('#pointNameInput').removeAttr('disabled');
					        } else {
					        	$('#pointNameInput').attr('disabled', 'disabled');
					        }    
					    });
				        $('tr.ContextMenuTr').dblclick(function(e) {
				        	window.stop();
				        	$('#context-menu').attr('data-id', $(this).children().eq(1).text());
				        	UpdatemModalLabels(false);
				        	$('#machineModal').modal('show');
				        });
				        
					    function UpdatemModalLabels(isAdd){
					    	var mId = $('#context-menu').attr('data-id');
					    	if (isAdd) {
					    		mId = 0;
					    	}
					    	$('#mId').val(mId);
					    	$.ajax({
					    		url:"/",
					    		method:"POST",
					    		data : {
					    			id : mId,
					    			WKSCmd : 'GetMy'
					    		},
					    		success : function(result) {
					    			mResponse = JSON.parse(result);
					    			var gps = '';
					    			$.each(mResponse['groups'], function(k,v){
					    				gps = gps + '<option value="'+v+'">'+v+'</option>';
					    			}); 
					    			$('#mGroup').html(gps);

					    			if (mResponse['WKS']['enable'] == '1') {
					    				$('#mEnabled').prop('checked', true);
					    			} else {
					    				$('#mEnabled').prop('checked', false);
					    			}
					    			$('#mHostname').val(mResponse['WKS']['name']);
					    			$('#mGroup').val(mResponse['WKS']['group']);
					    			$('#mIpAddress').val(mResponse['WKS']['ipv4']);
					    			$('#mMacAddress').val(mResponse['WKS']['mac']);
					    			$('#mTargetId').val(mResponse['WKS']['tid']);
					    			$('#mPxeFile').val(mResponse['WKS']['fileboot']);
					    			$('#mGateway').val(mResponse['WKS']['gateway']);
					    			$('#mDnsServers').val(mResponse['WKS']['dns']);
					    			$('#mDomainSearch').val(mResponse['WKS']['domainsearch']);
					    			$('[data-imgnum').val(0);
					    			$.each(mResponse['WKS']['img'], function(k,v){
					    				$('#mBoot'+v['boot']).val(k + 1);
					    			});
					    			$('#mImgSelect').val('0');
					    			$('#mImgSelect').data('imgindex', parseInt($('#mImgSelect').val()) + 1);
							    	$.each($('[data-lb1]'), function(k,v){
							    		$(v).text($(v).data('lb1') + ' ' + $('#mImgSelect').data('imgindex') + ' ' + $(v).data('lb2'));
							    	});

							    	if (mResponse['WKS']['img'][0]['enable'] == '1') {
							    		$('#mImgEnable').prop('checked', true);
							    	} else {
							    		$('#mImgEnable').prop('checked', false);
							    	}
							    	var diskStr = '<option value="none">none</option>';
							    	var pref = mResponse['WKS']['img'][0]['type'];

							    	if (pref == 'disk' || pref == 'block')
							    		pref = 'dyn'+pref;

							    	$.each(mResponse['images'][pref], function(k,v) {
							    		diskStr = diskStr + '<option value="'+v+'">'+v+'</option>';
							    	});
							    	$('#mImgName').html(diskStr);
							    	$('#mImgType').val(mResponse['WKS']['img'][0]['type']);
							    	$('#mImgName').val(mResponse['WKS']['img'][0]['path']);
							    	$('#mImgCache').val(mResponse['WKS']['img'][0]['cache']);
					    		}
					    	});

					    }

				        function DeleteMachine() {
					    	$.ajax({
					    		url:"/",
					    		method:"POST",
					    		data : {
					    			id : $('#context-menu').attr('data-id'),
					    			WKSCmd : 'DeleteMachine'
					    		},
					    		success : function(result) {
					    			location.reload();
					    		}
					    	});
				        }
					    $('#mImgSelect').on('change', function(e) {
					    	$('#mImgSelect').data('imgindex', parseInt($('#mImgSelect').val()) + 1);
					    	$.each($('[data-lb1]'), function(k,v){
					    		$(v).text($(v).data('lb1') + ' ' +  $('#mImgSelect').data('imgindex') + ' ' + $(v).data('lb2'));
					    	});

					    	if (mResponse['WKS']['img'][$('#mImgSelect').val()]['enable'] == 1) {
					    		$('#mImgEnable').prop('checked', true);
					    	} else {
					    		$('#mImgEnable').prop('checked', false);
					    	}
					    	var diskStr = '<option value="none">none</option>';
					    	$.each(mResponse['images'][mResponse['WKS']['img'][$('#mImgSelect').val()]['type'] ], function(k,v) {
					    		diskStr = diskStr + '<option value="'+v+'">'+v+'</option>';
					    	});
					    	$('#mImgName').html(diskStr);
					    	$('#mImgType').val(mResponse['WKS']['img'][$('#mImgSelect').val()]['type']);
					    	$('#mImgName').val(mResponse['WKS']['img'][$('#mImgSelect').val()]['path']);
					    	$('#mImgCache').val(mResponse['WKS']['img'][$('#mImgSelect').val()]['cache']);
					    });

					    $('#mImgType').on('change', function(e){
					    	mResponse['WKS']['img'][$('#mImgSelect').val()]['type'] = $('#mImgType').val();
					    	var diskStr = '<option value="none">none</option>';
					    	$.each(mResponse['images'][mResponse['WKS']['img'][$('#mImgSelect').val()]['type'] ], function(k,v) {
					    		diskStr = diskStr + '<option value="'+v+'">'+v+'</option>';
					    	});
					    	$('#mImgName').html(diskStr);
					    	$('#mImgName').val('none');
					    	mResponse['WKS']['img'][$('#mImgSelect').val()]['path'] = $('#mImgName').val();
					    });
					    $('#mImgCache').on('change', function(e){
					    	mResponse['WKS']['img'][$('#mImgSelect').val()]['cache'] = $('#mImgCache').val();
					    });
					    $('#mImgName').on('change', function(e){
					    	mResponse['WKS']['img'][$('#mImgSelect').val()]['path'] = $('#mImgName').val();
					    });
					    $('#mImgEnable').change(function(){
					    	if ($('#mImgEnable').is(':checked')) {
					    		mResponse['WKS']['img'][$('#mImgSelect').val()]['enable'] = 1;
					    		console.log(mResponse['WKS']['img'][$('#mImgSelect').val()]['enable']);
					    	} else {
					    		mResponse['WKS']['img'][$('#mImgSelect').val()]['enable'] = 0;
					    		console.log('not checked');
					    	}
					    });


					    $('[data-imgnum]').on('change', function() {
					    	mResponse['WKS']['img'][0]['boot'] = 0;
					    	mResponse['WKS']['img'][1]['boot'] = 0;
					    	mResponse['WKS']['img'][2]['boot'] = 0;

					    	$('[data-imgnum]').each(function(k,v){
					    		if ($(v).val() != 0) {
					    			mResponse['WKS']['img'][(parseInt($(v).val()) - 1)]['boot'] = $(v).data('imgnum');
					    		}
					    	});
					    });

					    function SaveMachineSettings() {
					    	mResponse['WKS']['enable'] = $('#mEnabled').is(':checked') ? 1 : 0;
					    	mResponse['WKS']['tid'] = $('#mTargetId').val();
					    	mResponse['WKS']['name'] = $('#mHostname').val();
					    	mResponse['WKS']['group'] = $('#mGroup').val();

					    	mResponse['WKS']['ipv4'] = $('#mIpAddress').val();
					    	mResponse['WKS']['mac'] = $('#mMacAddress').val();
					    	mResponse['WKS']['gateway'] = $('#mGateway').val();
					    	mResponse['WKS']['dns'] = $('#mDnsServers').val();
					    	mResponse['WKS']['domainsearch'] = $('#mDomainSearch').val();
					    	mResponse['WKS']['fileboot'] = $('#mPxeFile').val();

					    	$.ajax({
					    		url:"/",
					    		method:"POST",
					    		data : {
					    			id : $('#mId').val(),
					    			WKSCmd : "ApplyMy",
					    			jsondata : JSON.stringify(mResponse)
					    		},
					    		success: function(result) {
					    			location.reload();
					    		}
					    	});
					    }


				        /* AJAX Begin POST SEND */


				        $('tr.ContextMenuTr').on('contextmenu', function(e) {
							$('#context-menu').attr('data-id', $(this).children().eq(1).text());
							/*$('#table_refresh').removeAttr('content');*/
							window.stop();
							var top = e.pageY - 10;
							var left = e.pageX - 90;

							$("#context-menu").css({
							display: "block",
							top: top,
							left: left
							}).addClass("show");
				        	$.ajax({
				        		url : "/",
				        		method : "POST",
				        		data : {
				        			id : $('#context-menu').attr('data-id'),
				        			supper : "supperCheck"
				        		},
				        		success : function(result) {
				        			if (result == 1) {
				        				$('#disableSuperModeBtn').removeClass('disabled');
				        				$('#enableSuperModeBtn').addClass('disabled');
				        			} else if (result == 2) {
				        				$('#enableSuperModeBtn').addClass('disabled');
				        				$('#disableSuperModeBtn').addClass('disabled');
				        			} else {
				        				$('#enableSuperModeBtn').removeClass('disabled');
				        				$('#disableSuperModeBtn').addClass('disabled');
				        			}
				        		},
				        		error : function (jqXHR, exception) {
            						console.log(jqXHR);
            					}
				        	});
						  return false; //blocks default Webbrowser right click menu
						});

				        $('#enableSuperModeBtn').on('click', function(e) {
				        	e.preventDefault();
				        	$.ajax({
				        		url : "/",
				        		method : "POST",
				        		data : {
				        			id : $('#context-menu').attr('data-id'),
				        			supper : "DiskList"
				        		},
				        		success : function (result) {
				        			console.log(result);
				        			var data = JSON.parse(result);
				        			var str = "";
				        			$.each(data, function (key, val) {
				        				str = str + '<div class="custom-control custom-checkbox mb-3"><input type="checkbox" class="custom-control-input" value="'+key+'" id="sDisk'+key+'"><label class="custom-control-label" for="sDisk'+key+'">'+val+'</label></div>';
				        			});
				        			$('#supperModeDiskListModal').find('.modal-body').html(str);
				        			$('#supperModeDiskListModal').modal("show");
				        		},
				        		error : function (jqXHR, exception) {
            						console.log(jqXHR);
            					}
				        	});
				        });

				        function SetSupper(SetVal) {
				        	$('#spinnerModal').modal('show');
				        	var jdata = '';
				        	if (SetVal === 'true') {
				        		var disks = [];
				        		$("#supperModeDiskListModal input[type=checkbox]:checked").each(function(){
				        			disks.push($(this).val());
				        		});
				        		jdata = JSON.stringify(disks);
				        	} else if (SetVal === 'disableCommit') {
				        		if ($('#createPointBtn').is(':checked')) {
				        			if ($('#pointNameInput').val().length == 0) {
				        				alert('name must be filled');
				        				return;
				        			}
				        			SetVal = 'disableCommitPoint';
				        			jdata = $('#pointNameInput').val();
				        		}
				        	}
				        	$.ajax({
				        		url : "/",
				        		method : "POST",
				        		data : {
				        			id : $('#context-menu').attr('data-id'),
				        			supper : SetVal,
				        			jsondata : jdata
				        		},
				        		success : function (result) {
				        			$('#supperModeDisableModal').modal("hide");
				        			$('#supperModeDisableModal').find('form')[0].reset();
					        		$('#pointNameInput').attr('disabled', 'disabled');
				        			$('#spinnerModal').modal('hide');
				        			location.reload(); 
				        		},
				        		error : function (jqXHR, exception) {
				        			location.reload(); 
            						console.log(jqXHR);
				        			$('#spinnerModal').modal('hide');
            					}
				        	});
				        }

				        function GetCommand(SetVal) {
				        	var cmdargs = '';
				        	$.ajax({
				        		url : "/",
				        		method : "POST",
				        		data : {
				        			id : $('#context-menu').attr('data-id'),
				        			cmd : SetVal,
				        			cmdargs : cmdargs
				        		},
				        		success : function (result) {
				        			$('#supperModeDisableModal').modal("hide");
				        			$('#supperModeDisableModal').find('form')[0].reset();
					        		$('#pointNameInput').attr('disabled', 'disabled');
				        			location.reload(); 
				        		},
				        		error : function (jqXHR, exception) {
				        			location.reload(); 
            						console.log(jqXHR);
            					}
				        	});
				        }
				        /*AJAX End*/





						function act1() {
								    console.log(this.responseText);




							alert($('#context-menu').attr('data-id'));
						}

						$('body').on("click", function() {
						  $("#context-menu").removeClass("show").hide();
						   /*location.reload(); */ 
						});

						$("#context-menu a").on("click", function() {
						  $(this).parent().removeClass("show").hide();
						  
						});
				        </script>

						]])
					ngx.say("</body></html>");			    			
			    			
			    else
							if mkyboot.inc.isFile(mkyboot.cfg.server.workdir.."/"..mkyboot.cfg.server.distdir.."/cfg/"..mkyboot.cfg.server.config) then mkyboot.cfg = mkyboot:LoadFromFile(mkyboot.cfg.server.workdir.."/"..mkyboot.cfg.server.distdir.."/cfg/"..mkyboot.cfg.server.config) else mkyboot.cfg = dofile("/srv/mkyboot/cfg/cfg.lua").cfg; mkyboot:SaveToFile(mkyboot.cfg.server.workdir.."/"..mkyboot.cfg.server.distdir.."/cfg/"..mkyboot.cfg.server.config,mkyboot.cfg); end;
							mkyboot.inc.csrf.clean()
							local csrf_token = mkyboot.inc.csrf.generate()
							ngx.header["Set-Cookie"] = "csrf_token="..csrf_token.."; Path=/; HttpOnly; SameSite=Strict"
			   				ngx.say([[
					<!DOCTYPE html>
					<html>
					<head>
					<meta name="viewport" content="width=device-width, initial-scale=1">
					<style>
					* {box-sizing: border-box}
					body {font-family: "Lato", sans-serif;}


					.header {
						overflow: hidden;
						background-color: #fff;
						padding: 10px 10px;
						    
					}

					.header a {
						float: left;
						color: black;
						text-align: center;
						padding: 12px;
						text-decoration: none;
						font-size: 18px; 
						line-height: 25px;
						border-radius: 4px;
					}

					.header a.logo {
						font-size: 25px;
						font-weight: bold;
					}

					.header a:hover {
						background-color: #ddd;
						color: black;
					}

					.header a.active {
						background-color: #28a745;
						color: white;
					}

					.header-right {
						float: right;
					}

				@media screen and (max-width: 500px) {
					.header a {
						float: none;
						display: block;
						text-align: left;
					}
																  
					.header-right {
						float: none;
					}
				}

/* Style the tab */
.tab {
  float: left;
  border: 1px solid #ccc;
  background-color: #4e73df;
  color: #f8f9fc;
  width: 14%;
  text-decoration: underline;
  height: 100%;
  line-height: 1.5;
  box-shadow: 5px 5px 8px rgba(0,0,0,0.5);
  padding: 10px 0px;
  position: revert;
  border-radius: 8px;

}

/* Style the buttons inside the tab */
.tab button {
  display: block;
  background-color: inherit;
  color: red;
  padding: 16px 22px;
  width: 95%;
  border: 0;
  outline: none;
  text-align: left;
  cursor: pointer;
  transition: 0.3s;
  font-size: 16px;
  color: #f8f9fc;
  font-weight: 600;
  border-radius: 2px;
  
}

/* Change background color of buttons on hover */
.tab button:hover {
  background-color: #ddd;
}

/* Create an active/current "tab button" class */
.tab button.active {
  background-color: #4caf50;
}

/* Style the tab content */
.tabcontent {
  float: left;
  padding: 0 0;
  border: px solid #ccc;
  background-color: #fff;
  color: #f8f9fc;
  width: 85%;
  border-left: none;
  border-right: none;
  border-bottom: none;
  height: 100%;
  box-shadow: 5px 5px 10px rgba(0,0,0,0.5);
}
.tabcontent .toptab {
	overflow: hidden;
	background-color: #4e73df;
	padding: 0px 40%;
	height: 50px;
	position: relative;
	left: 0;
	margin: 0px; 
}
.bi .bi-display .imgsrc {
	width: 12%;
	height: 12%;
	left: 1%;
 	position: relative;
	color: #fff;
}
.brend {
	background: #4e73df;
	position: relative;
	top: 0px;
}
.iframe {

	width: 100%;height: 100%;position: relative; border: 0; scrolling: auto; background-color: #fff;
}
</style>
</head>
<body>
			<input type="hidden" id="csrf_token" value="]]..csrf_token..[[">
			<script>var CSRF_TOKEN = "]]..csrf_token..[[";</script>
			<div class="header">
				<a href="#default" class="logo"><b style="color: red; box-shadow: 4px 0 10px rgba(0,0,0,0.5);">MK</b>YBOOT
				<div class="header-right">												 
			 		<a href="#Support">Support</a>
			 		<a href="#" onclick="doLogout()">Logout</a>
		 		</div>
			</div>

<div class="tab">
<div class="brend">
  <svg width="3em" height="1.5em" viewBox="0 0 16 16" class="bi bi-bar-chart-line-fill" fill="currentColor" xmlns="http://www.w3.org/2000/svg">
  <path fill-rule="evenodd" d="M11 2a1 1 0 0 1 1-1h2a1 1 0 0 1 1 1v12h.5a.5.5 0 0 1 0 1H.5a.5.5 0 0 1 0-1H1v-3a1 1 0 0 1 1-1h2a1 1 0 0 1 1 1v3h1V7a1 1 0 0 1 1-1h2a1 1 0 0 1 1 1v7h1V2z"/>
</svg>

</div>
  <button class="tablinks" onclick="openCity(event, 'dashboard')" id="defaultOpen">
<svg width="3em" height="1.5em" viewBox="0 0 16 16" class="bi bi-bar-chart-line-fill" fill="currentColor" xmlns="http://www.w3.org/2000/svg">
  <path fill-rule="evenodd" d="M11 2a1 1 0 0 1 1-1h2a1 1 0 0 1 1 1v12h.5a.5.5 0 0 1 0 1H.5a.5.5 0 0 1 0-1H1v-3a1 1 0 0 1 1-1h2a1 1 0 0 1 1 1v3h1V7a1 1 0 0 1 1-1h2a1 1 0 0 1 1 1v7h1V2z"/>
</svg>
Dashboard
  </button>
  <button class="tablinks" onclick="openCity(event, 'Machines')">
<svg width="3em" height="1.5em" viewBox="0 0 16 16" class="bi bi-tv-fill" fill="currentColor" xmlns="http://www.w3.org/2000/svg">
  <path fill-rule="evenodd" d="M2.5 13.5A.5.5 0 0 1 3 13h10a.5.5 0 0 1 0 1H3a.5.5 0 0 1-.5-.5zM2 2h12s2 0 2 2v6s0 2-2 2H2s-2 0-2-2V4s0-2 2-2z"/>
</svg>
Workstations 
</button>

  <button class="tablinks" onclick="openCity(event, 'Samba')">
<svg width="3em" height="1.5em" viewBox="0 0 16 16" class="bi bi-people-fill" fill="currentColor" xmlns="http://www.w3.org/2000/svg">
  <path fill-rule="evenodd" d="M7 14s-1 0-1-1 1-4 5-4 5 3 5 4-1 1-1 1H7zm4-6a3 3 0 1 0 0-6 3 3 0 0 0 0 6zm-5.784 6A2.238 2.238 0 0 1 5 13c0-1.355.68-2.75 1.936-3.72A6.325 6.325 0 0 0 5 9c-4 0-5 3-5 4s1 1 1 1h4.216zM4.5 8a2.5 2.5 0 1 0 0-5 2.5 2.5 0 0 0 0 5z"/>
</svg>
 Samba
</button>
  <button class="tablinks" onclick="openCity(event, 'Images')">
<svg width="3em" height="1.5em" viewBox="0 0 16 16" class="bi bi-hdd-fill" fill="currentColor" xmlns="http://www.w3.org/2000/svg">
  <path d="M0 4s0-2 2-2h12s2 0 2 2v6s0 2-2 2h-4c0 .667.083 1.2.3 1.55a8.014 8.014 0 0 1-2.3.45H2s-2 0-2-2V4zm1.398 8.147c.612 0 .843-.172.924-.361a1.932 1.932 0 0 0 .35-1.41c.331-1.3 2-3.98 2-3.98s1.646.062 2.652 1.277V9.586c0-.286.136-.547.366-.728A2 2 0 0 0 13 8V6s-1.466 0-2.8-.5c.43.49.607 1.1.308 1.635-.27.462-.462.753-.562.874-.102.12-.22.218-.35.29-.13.072-.27.12-.42.137H3.8c-.138-.017-.278-.065-.42-.137a1.7 1.7 0 0 1-.35-.29c-.1-.121-.293-.412-.562-.874A2 2 0 0 0 2.5 8v2a2 2 0 0 0 1 .55V8.147z"/>
</svg>
 Images
</button>
  <button class="tablinks" onclick="openCity(event, 'Logs')">
<svg width="3em" height="1.5em" viewBox="0 0 16 16" class="bi bi-journal-text" fill="currentColor" xmlns="http://www.w3.org/2000/svg">
  <path d="M3 14s-1 0-1-1 1-4 6-4 6 3 6 4-1 1-1 1H3zm5-6a3 3 0 1 0 0-6 3 3 0 0 0 0 6z"/>
  <path fill-rule="evenodd" d="M2 2a2 2 0 0 1 2-2h8a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V2zm10 0H4v12h8V2z"/>
</svg>
 Logs
</button>
  <button class="tablinks" onclick="openCity(event, 'Network')">
<svg width="3em" height="1.5em" viewBox="0 0 16 16" class="bi bi-globe2" fill="currentColor" xmlns="http://www.w3.org/2000/svg">
  <path d="M0 8a8 8 0 1 1 16 0A8 8 0 0 1 0 8zm7.5-6.923c-.67.204-1.335.82-1.887 1.855A7.97 7.97 0 0 0 5.145 4H7.5V1.077zM4.09 4a9.267 9.267 0 0 1 .64-1.539 6.7 6.7 0 0 1 .597-.933A7.025 7.025 0 0 0 2.255 4H4.09zm-.582 3.5c.03-.877.138-1.718.312-2.5H1.674a6.958 6.958 0 0 0-.656 2.5h2.49zM7.5 7h-3.09c.04-.392.06-.788.06-1.192V4H5.51a7.025 7.025 0 0 0 2.49 2.56c-.048.135-.096.27-.144.4H7.5V7zm1 0v3.043c0 .392.02.788.06 1.192H9.5a8.966 8.966 0 0 0-2.49-2.56c-.048.135-.096.27-.144.4H8.5zm1.318 3.5H9.5c-.048.135-.096.27-.144.4A7.025 7.025 0 0 0 12.255 12h1.835a9.267 9.267 0 0 1-.64 1.539 6.7 6.7 0 0 1-.597.933zM10.5 7V4H11.1c.167.694.28 1.422.337 2.178h-2.437zm-1.318 3.5c.048-.135.096-.27.144-.4A7.025 7.025 0 0 0 6.245 12H4.41a9.267 9.267 0 0 1 .64-1.539 6.7 6.7 0 0 1 .597-.933zM3.5 7h2.49a7.025 7.025 0 0 0-2.49-2.56c.048.135.096.27.144.4H3.5V7zM4.41 4h1.835c-.048-.135-.096-.27-.144-.4A7.025 7.025 0 0 0 3.5 7V4.077z"/>
</svg>
 Network
</button>
  <button class="tablinks" onclick="openCity(event, 'Shell')">
<svg width="3em" height="1.5em" viewBox="0 0 16 16" class="bi bi-terminal-fill" fill="currentColor" xmlns="http://www.w3.org/2000/svg">
  <path fill-rule="evenodd" d="M0 3a2 2 0 0 1 2-2h12a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H2a2 2 0 0 1-2-2V3zm9.5 5.5h-3a.5.5 0 0 0 0 1h3a.5.5 0 0 0 0-1zm-6.354-.354L4.793 6.5 3.146 4.854a.5.5 0 1 1 .708-.708l2 2a.5.5 0 0 1 0 .708l-2 2a.5.5 0 0 1-.708-.708z"/>
</svg>
  Shell
  </button>

  <button class="tablinks" onclick="openCity(event, 'Support')">
  <svg width="3em" height="1.5em" viewBox="0 0 16 16" class="bi bi-life-preserver" fill="currentColor" xmlns="http://www.w3.org/2000/svg">
  <path fill-rule="evenodd" d="M14.43 10.772l-2.788-1.115a4.015 4.015 0 0 1-1.985 1.985l1.115 2.788a7.025 7.025 0 0 0 3.658-3.658zM5.228 14.43l1.115-2.788a4.015 4.015 0 0 1-1.985-1.985L1.57 10.772a7.025 7.025 0 0 0 3.658 3.658zm9.202-9.202a7.025 7.025 0 0 0-3.658-3.658L9.657 4.358a4.015 4.015 0 0 1 1.985 1.985l2.788-1.115zm-8.087-.87L5.228 1.57A7.025 7.025 0 0 0 1.57 5.228l2.788 1.115a4.015 4.015 0 0 1 1.985-1.985zM8 16A8 8 0 1 0 8 0a8 8 0 0 0 0 16zm0-5a3 3 0 1 0 0-6 3 3 0 0 0 0 6z"/>
</svg>
  Support
  </button>
  <button class="tablinks" onclick="openCity(event, 'Shutdown')">
  <svg width="3em" height="1.5em" viewBox="0 0 16 16" class="bi bi-power" fill="currentColor" xmlns="http://www.w3.org/2000/svg">
  <path fill-rule="evenodd" d="M5.578 4.437a5 5 0 1 0 4.922.044l.5-.866a6 6 0 1 1-5.908-.053l.486.875z"/>
  <path fill-rule="evenodd" d="M7.5 8V1h1v7h-1z"/>
</svg>
  Shutdown
  </button> 
  <button class="tablinks" onclick="openCity(event, 'Settings')">
  <svg width="3em" height="1.5em" viewBox="0 0 16 16" class="bi bi-gear-fill" fill="currentColor" xmlns="http://www.w3.org/2000/svg">
  <path d="M9.405 1.05c-.413-1.4-2.397-1.4-2.81 0l-.1.34a1.464 1.464 0 0 1-2.105.872l-.31-.17c-1.283-.698-2.686.705-1.987 1.987l.169.311c.446.82.023 1.841-.872 2.105l-.34.1c-1.4.413-1.4 2.397 0 2.81l.34.1a1.464 1.464 0 0 1 .872 2.105l-.17.31c-.698 1.283.705 2.686 1.987 1.987l.311-.169a1.464 1.464 0 0 1 2.105.872l.1.34c.413 1.4 2.397 1.4 2.81 0l.1-.34a1.464 1.464 0 0 1 2.105-.872l.31.17c1.283.698 2.686-.705 1.987-1.987l-.169-.311a1.464 1.464 0 0 1 .872-2.105l.34-.1c1.4-.413 1.4-2.397 0-2.81l-.34-.1a1.464 1.464 0 0 1-.872-2.105l.17-.31c.698-1.283-.705-2.686-1.987-1.987l-.311.169a1.464 1.464 0 0 1-2.105-.872l-.1-.34zM8 10.93a2.929 2.929 0 1 1 0-5.86 2.929 2.929 0 0 1 0 5.858z"/>
</svg>
  Settings
  </button>
  <button class="tablinks" onclick="doLogout()">
  <svg width="3em" height="1.5em" viewBox="0 0 16 16" class="bi bi-door-open-fill" fill="currentColor" xmlns="http://www.w3.org/2000/svg">
  <path fill-rule="evenodd" d="M1.5 15a.5.5 0 0 0 0 1h13a.5.5 0 0 0 0-1H13V2.5A1.5 1.5 0 0 0 11.5 1H11V.5a.5.5 0 0 0-.57-.495l-7 1A.5.5 0 0 0 3 1.5V15H1.5zM11 2v13h1V2.5a.5.5 0 0 0-.5-.5H11zm-2.5 8c-.276 0-.5-.448-.5-1s.224-1 .5-1 .5.448.5 1-.224 1-.5 1z"/>
</svg>
  Logout
  </button>
</div>

<div id="dashboard" class="tabcontent">
	<div class="toptab">
  		<b><h3>Dashboard</h3></b>
  	</div>
  	<div style="padding: 20px;">
  		<div id="dash-loading" style="text-align:center;padding:40px;"><h4>Loading...</h4></div>
  		<div id="dash-content" style="display:none;">
	  		<div class="row" style="display:flex;flex-wrap:wrap;gap:16px;padding:10px;">
	  			<div style="flex:1;min-width:200px;background:#4e73df;color:#fff;border-radius:8px;padding:20px;box-shadow:3px 3px 8px rgba(0,0,0,0.3);">
	  				<h6>Server</h6>
	  				<h3 id="d-server-ver"></h3>
	  				<small id="d-server-ip"></small>
	  			</div>
	  			<div style="flex:1;min-width:200px;background:#28a745;color:#fff;border-radius:8px;padding:20px;box-shadow:3px 3px 8px rgba(0,0,0,0.3);">
	  				<h6>Clients Online</h6>
	  				<h3 id="d-clients-online">0</h3>
	  				<small><span id="d-clients-total">0</span> total</small>
	  			</div>
	  			<div style="flex:1;min-width:200px;background:#dc3545;color:#fff;border-radius:8px;padding:20px;box-shadow:3px 3px 8px rgba(0,0,0,0.3);">
	  				<h6>Clients Offline</h6>
	  				<h3 id="d-clients-offline">0</h3>
	  			</div>
	  		</div>
	  		<div class="row" style="display:flex;flex-wrap:wrap;gap:16px;padding:10px;">
	  			<div style="flex:1;min-width:200px;background:#17a2b8;color:#fff;border-radius:8px;padding:20px;box-shadow:3px 3px 8px rgba(0,0,0,0.3);">
	  				<h6>Boot Images</h6>
	  				<h3 id="d-img-boot">0</h3>
	  			</div>
	  			<div style="flex:1;min-width:200px;background:#fd7e14;color:#fff;border-radius:8px;padding:20px;box-shadow:3px 3px 8px rgba(0,0,0,0.3);">
	  				<h6>ISO Images</h6>
	  				<h3 id="d-img-iso">0</h3>
	  			</div>
	  			<div style="flex:1;min-width:200px;background:#6f42c1;color:#fff;border-radius:8px;padding:20px;box-shadow:3px 3px 8px rgba(0,0,0,0.3);">
	  				<h6>iSCSI Port</h6>
	  				<h3 id="d-iscsi-port">3260</h3>
	  			</div>
	  		</div>
	  		<div style="padding:10px;">
	  			<h5>Services</h5>
	  			<table style="width:100%;border-collapse:collapse;">
	  				<tr style="background:#4caf50;color:#fff;"><th style="padding:8px;text-align:left;">Service</th><th style="padding:8px;text-align:left;">Status</th><th style="padding:8px;text-align:left;">Port</th></tr>
	  				<tr><td style="padding:6px;border-bottom:1px solid #ddd;">DHCP</td><td id="d-svc-dhcp" style="padding:6px;border-bottom:1px solid #ddd;">--</td><td style="padding:6px;border-bottom:1px solid #ddd;">67</td></tr>
	  				<tr style="background:#f2f2f2;"><td style="padding:6px;border-bottom:1px solid #ddd;">TFTP</td><td id="d-svc-tftp" style="padding:6px;border-bottom:1px solid #ddd;">--</td><td style="padding:6px;border-bottom:1px solid #ddd;">69</td></tr>
	  				<tr><td style="padding:6px;border-bottom:1px solid #ddd;">iSCSI</td><td id="d-svc-iscsi" style="padding:6px;border-bottom:1px solid #ddd;">--</td><td id="d-iscsi-port2" style="padding:6px;border-bottom:1px solid #ddd;">3260</td></tr>
	  			</table>
	  		</div>
	  		<div style="padding:10px;">
	  			<h5>Recent Logs</h5>
	  			<div id="d-logs" style="background:#1a1a2e;color:#e0e0e0;padding:10px;border-radius:6px;max-height:200px;overflow-y:auto;font-family:monospace;font-size:12px;"></div>
	  		</div>
  		</div>
  	</div>
  	<script>
  	(function(){
  		var apiBase = 'http://' + window.location.host + ':' + ]]..tostring(mkyboot.cfg.server.listen)..[[;
  		function loadDash(){
  			var xhr = new XMLHttpRequest();
  			xhr.open('GET', apiBase + '?api=status');
  			xhr.onload = function(){
  				if(xhr.status === 200){
  					var d = JSON.parse(xhr.responseText);
  					document.getElementById('dash-loading').style.display='none';
  					document.getElementById('dash-content').style.display='block';
  					document.getElementById('d-server-ver').textContent = d.server.vendor + ' v' + d.server.version;
  					document.getElementById('d-server-ip').textContent = d.server.ipv4;
  					document.getElementById('d-clients-online').textContent = d.clients.online;
  					document.getElementById('d-clients-total').textContent = d.clients.total;
  					document.getElementById('d-clients-offline').textContent = d.clients.offline;
  					document.getElementById('d-img-boot').textContent = (d.images.boot||[]).length;
  					document.getElementById('d-img-iso').textContent = (d.images.iso||[]).length;
  					document.getElementById('d-iscsi-port').textContent = d.iscsi.port;
  					document.getElementById('d-iscsi-port2').textContent = d.iscsi.port;
  					var svcDHCP = document.getElementById('d-svc-dhcp');
  					svcDHCP.textContent = d.services.dhcp ? 'Running' : 'Stopped';
  					svcDHCP.style.color = d.services.dhcp ? '#28a745' : '#dc3545';
  					var svcTFTP = document.getElementById('d-svc-tftp');
  					svcTFTP.textContent = d.services.tftp ? 'Running' : 'Stopped';
  					svcTFTP.style.color = d.services.tftp ? '#28a745' : '#dc3545';
  					var svcISCSI = document.getElementById('d-svc-iscsi');
  					svcISCSI.textContent = d.services.iscsi ? 'Running' : 'Stopped';
  					svcISCSI.style.color = d.services.iscsi ? '#28a745' : '#dc3545';
  				}
  			};
  			xhr.send();
  			var xhr2 = new XMLHttpRequest();
  			xhr2.open('GET', apiBase + '?api=logs');
  			xhr2.onload = function(){
  				if(xhr2.status === 200){
  					var logs = JSON.parse(xhr2.responseText);
  					var el = document.getElementById('d-logs');
  					el.innerHTML = '';
  					for(var i=0;i<logs.length;i++){
  						var line = document.createElement('div');
  						line.textContent = logs[i];
  						if(logs[i].indexOf('ERROR')>=0) line.style.color='#ff6b6b';
  						else if(logs[i].indexOf('WARN')>=0) line.style.color='#ffd93d';
  						el.appendChild(line);
  					}
  				}
  			};
  			xhr2.send();
  		}
  		loadDash();
  	})();
  	</script>
</div>

<div id="Machines" class="tabcontent">
<div class="toptab">
<b><h3>Workstations</h3></b>
</div>
	<iframe seamless allow="fullscreen" src="http://]]..ngx.var.host..[[:]]..tostring(mkyboot.cfg.server.listen)..[[?status=true"  class="iframe" id="frame1" name="mainFrame" frameborder="0" scrolling="no" onload="resizeIframe1(this);"></iframe>
</div>

<div id="Shell" class="tabcontent">
<div class="toptab">
<b><h3>Shell</h3></b>

</div>
	<iframe seamless allow="fullscreen" src="http://]]..ngx.var.host..[[:]]..tostring(mkyboot.cfg.server.shell_port)..[["  class="iframe" id="frame1" name="mainFrame" frameborder="0" scrolling="no" onload="resizeIframe1(this);"></iframe>
</div>

<div id="Samba" class="tabcontent">
  <div class="toptab"><b><h3>Samba</h3></b></div>
  <div style="padding:20px;"><p>Samba file sharing configuration - coming soon.</p></div>
</div>

<div id="Images" class="tabcontent">
  <div class="toptab"><b><h3>Disk Images</h3></b></div>
  <div style="padding:20px;">
  	<div style="display:flex;gap:16px;flex-wrap:wrap;">
  		<div style="flex:1;min-width:300px;">
  			<h5>Boot Images (QCOW2)</h5>
  			<table style="width:100%;border-collapse:collapse;" id="img-boot-table">
  				<tr style="background:#4caf50;color:#fff;"><th style="padding:6px;text-align:left;">Name</th><th style="padding:6px;text-align:left;">Size</th></tr>
  			</table>
  		</div>
  		<div style="flex:1;min-width:300px;">
  			<h5>ISO Images</h5>
  			<table style="width:100%;border-collapse:collapse;" id="img-iso-table">
  				<tr style="background:#fd7e14;color:#fff;"><th style="padding:6px;text-align:left;">Name</th><th style="padding:6px;text-align:left;">Size</th></tr>
  			</table>
  		</div>
  		<div style="flex:1;min-width:300px;">
  			<h5>Storage (ZFS ZVOLs)</h5>
  			<table style="width:100%;border-collapse:collapse;" id="img-stor-table">
  				<tr style="background:#6f42c1;color:#fff;"><th style="padding:6px;text-align:left;">Name</th></tr>
  			</table>
  		</div>
  	</div>
  </div>
  <script>
  (function(){
  	var apiBase = 'http://' + window.location.host + ':' + ]]..tostring(mkyboot.cfg.server.listen)..[[;
  	var xhr = new XMLHttpRequest();
  	xhr.open('GET', apiBase + '?api=images');
  	xhr.onload = function(){
  		if(xhr.status === 200){
  			var d = JSON.parse(xhr.responseText);
  			var bootT = document.getElementById('img-boot-table');
  			(d.boot||[]).forEach(function(name){
  				var tr = document.createElement('tr');
  				tr.style.borderBottom = '1px solid #ddd';
  				tr.innerHTML = '<td style="padding:6px;">' + name + '</td><td style="padding:6px;">--</td>';
  				bootT.appendChild(tr);
  			});
  			var isoT = document.getElementById('img-iso-table');
  			(d.iso||[]).forEach(function(name){
  				var tr = document.createElement('tr');
  				tr.style.borderBottom = '1px solid #ddd';
  				tr.innerHTML = '<td style="padding:6px;">' + name + '</td><td style="padding:6px;">--</td>';
  				isoT.appendChild(tr);
  			});
  			var storT = document.getElementById('img-stor-table');
  			(d.storages||[]).forEach(function(name){
  				var tr = document.createElement('tr');
  				tr.style.borderBottom = '1px solid #ddd';
  				tr.innerHTML = '<td style="padding:6px;">' + name + '</td>';
  				storT.appendChild(tr);
  			});
  		}
  	};
  	xhr.send();
  })();
  </script>
</div>

<div id="Logs" class="tabcontent">
  <div class="toptab"><b><h3>System Logs</h3></b></div>
  <div style="padding:20px;">
  	<button onclick="refreshLogs()" style="padding:6px 16px;background:#4e73df;color:#fff;border:none;border-radius:4px;cursor:pointer;margin-bottom:10px;">Refresh</button>
  	<div id="log-container" style="background:#1a1a2e;color:#e0e0e0;padding:10px;border-radius:6px;max-height:500px;overflow-y:auto;font-family:monospace;font-size:12px;white-space:pre-wrap;"></div>
  </div>
  <script>
  function refreshLogs(){
  	var apiBase = 'http://' + window.location.host + ':' + ]]..tostring(mkyboot.cfg.server.listen)..[[;
  	var xhr = new XMLHttpRequest();
  	xhr.open('GET', apiBase + '?api=logs');
  	xhr.onload = function(){
  		if(xhr.status === 200){
  			var logs = JSON.parse(xhr.responseText);
  			var el = document.getElementById('log-container');
  			el.innerHTML = '';
  			for(var i=0;i<logs.length;i++){
  				var line = document.createElement('div');
  				line.textContent = logs[i];
  				if(logs[i].indexOf('ERROR')>=0) line.style.color='#ff6b6b';
  				else if(logs[i].indexOf('WARN')>=0) line.style.color='#ffd93d';
  				el.appendChild(line);
  			}
  		}
  	};
  	xhr.send();
  }
  refreshLogs();
  </script>
</div>

<div id="Network" class="tabcontent">
  <div class="toptab"><b><h3>Network Configuration</h3></b></div>
  <div style="padding:20px;">
  	<h5>Server Interface</h5>
  	<table style="width:100%;border-collapse:collapse;margin-bottom:20px;" id="net-srv-table">
  		<tr style="background:#4caf50;color:#fff;"><th style="padding:6px;text-align:left;">Setting</th><th style="padding:6px;text-align:left;">Value</th></tr>
  	</table>
  	<h5>DHCP Subnet</h5>
  	<table style="width:100%;border-collapse:collapse;" id="net-dhcp-table">
  		<tr style="background:#fd7e14;color:#fff;"><th style="padding:6px;text-align:left;">Setting</th><th style="padding:6px;text-align:left;">Value</th></tr>
  	</table>
  	<h5 style="margin-top:20px;">Active Connections</h5>
  	<table style="width:100%;border-collapse:collapse;" id="net-conn-table">
  		<tr style="background:#4e73df;color:#fff;"><th style="padding:6px;text-align:left;">Client</th><th style="padding:6px;text-align:left;">IP</th><th style="padding:6px;text-align:left;">MAC</th><th style="padding:6px;text-align:left;">Status</th></tr>
  	</table>
  </div>
  <script>
  (function(){
  	var apiBase = 'http://' + window.location.host + ':' + ]]..tostring(mkyboot.cfg.server.listen)..[[;
  	var xhr = new XMLHttpRequest();
  	xhr.open('GET', apiBase + '?api=clients');
  	xhr.onload = function(){
  		if(xhr.status === 200){
  			var clients = JSON.parse(xhr.responseText);
  			var srvT = document.getElementById('net-srv-table');
  			srvT.innerHTML += '<tr style="border-bottom:1px solid #ddd;"><td style="padding:6px;">Server IP</td><td style="padding:6px;">' + ]]..tostring(mkyboot.cfg.server.ipv4)..[[ + '</td></tr>';
  			srvT.innerHTML += '<tr style="background:#f2f2f2;border-bottom:1px solid #ddd;"><td style="padding:6px;">Subnet Mask</td><td style="padding:6px;">' + ]]..tostring(mkyboot.cfg.server.mask)..[[ + '</td></tr>';
  			srvT.innerHTML += '<tr style="border-bottom:1px solid #ddd;"><td style="padding:6px;">Gateway</td><td style="padding:6px;">' + ]]..tostring(mkyboot.cfg.server.gateway)..[[ + '</td></tr>';
  			srvT.innerHTML += '<tr style="background:#f2f2f2;border-bottom:1px solid #ddd;"><td style="padding:6px;">DNS 1</td><td style="padding:6px;">' + ]]..tostring(mkyboot.cfg.server.dns1)..[[ + '</td></tr>';
  			srvT.innerHTML += '<tr style="border-bottom:1px solid #ddd;"><td style="padding:6px;">DNS 2</td><td style="padding:6px;">' + ]]..tostring(mkyboot.cfg.server.dns2)..[[ + '</td></tr>';
  			var dhcpT = document.getElementById('net-dhcp-table');
  			var ranges = ]]..tostring(json.encode(mkyboot.cfg.dhcp.ranges or {}))..[[;
  			dhcpT.innerHTML += '<tr style="border-bottom:1px solid #ddd;"><td style="padding:6px;">DHCP Port</td><td style="padding:6px;">' + ]]..tostring(mkyboot.cfg.dhcp.port)..[[ + '</td></tr>';
  			dhcpT.innerHTML += '<tr style="background:#f2f2f2;border-bottom:1px solid #ddd;"><td style="padding:6px;">IP Ranges</td><td style="padding:6px;">' + JSON.stringify(ranges) + '</td></tr>';
  			var connT = document.getElementById('net-conn-table');
  			for(var i=0;i<clients.length;i++){
  				var c = clients[i];
  				connT.innerHTML += '<tr style="border-bottom:1px solid #ddd;"><td style="padding:6px;">' + c.name + '</td><td style="padding:6px;">' + c.ipv4 + '</td><td style="padding:6px;">' + c.mac + '</td><td style="padding:6px;color:' + (c.online ? '#28a745' : '#dc3545') + ';">' + (c.online ? 'Online' : 'Offline') + '</td></tr>';
  			}
  		}
  	};
  	xhr.send();
  })();
  </script>
</div>

<div id="Settings" class="tabcontent">
  <div class="toptab"><b><h3>Account Settings</h3></b></div>
  <div style="padding:20px;max-width:400px;">
  	<h5>Change Password</h5>
  	<div style="margin-bottom:12px;">
  		<label style="font-weight:600;">Current Password</label>
  		<input type="password" id="current-pw" style="width:100%;padding:8px;border:1px solid #ddd;border-radius:4px;box-sizing:border-box;">
  	</div>
  	<div style="margin-bottom:12px;">
  		<label style="font-weight:600;">New Password</label>
  		<input type="password" id="new-pw" style="width:100%;padding:8px;border:1px solid #ddd;border-radius:4px;box-sizing:border-box;">
  		<div style="color:#888;font-size:12px;margin-top:4px;">Minimum 8 characters.</div>
  	</div>
  	<div style="margin-bottom:12px;">
  		<label style="font-weight:600;">Confirm New Password</label>
  		<input type="password" id="confirm-pw" style="width:100%;padding:8px;border:1px solid #ddd;border-radius:4px;box-sizing:border-box;">
  	</div>
  	<div id="pw-error" style="color:#dc3545;display:none;margin:8px 0;"></div>
  	<div id="pw-success" style="color:#28a745;display:none;margin:8px 0;"></div>
  	<button onclick="doChangePassword()" style="padding:8px 20px;background:#4e73df;color:#fff;border:none;border-radius:4px;cursor:pointer;">Change Password</button>
  </div>
  <script>
  function doChangePassword(){
  	var cp=document.getElementById('current-pw').value;
  	var np=document.getElementById('new-pw').value;
  	var cf=document.getElementById('confirm-pw').value;
  	var err=document.getElementById('pw-error');
  	var ok=document.getElementById('pw-success');
  	err.style.display='none';ok.style.display='none';
  	if(!cp||!np||!cf){err.textContent='All fields are required';err.style.display='block';return;}
  	if(np.length<8){err.textContent='New password must be at least 8 characters';err.style.display='block';return;}
  	if(np!==cf){err.textContent='New passwords do not match';err.style.display='block';return;}
  	var xhr=new XMLHttpRequest();
  	xhr.open('POST','?changepw=true',true);
  	xhr.setRequestHeader('Content-Type','application/x-www-form-urlencoded');
  	xhr.onload=function(){
  		if(xhr.responseText==='OK'){
  			ok.textContent='Password changed successfully';ok.style.display='block';
  			document.getElementById('current-pw').value='';
  			document.getElementById('new-pw').value='';
  			document.getElementById('confirm-pw').value='';
  		}else{err.textContent=xhr.responseText.replace('ERROR: ','');err.style.display='block';}
  	};
  	xhr.send('current_password='+encodeURIComponent(cp)+'&new_password='+encodeURIComponent(np)+'&confirm_password='+encodeURIComponent(cf));
  }
  function doLogout(){
  	var xhr=new XMLHttpRequest();
  	xhr.open('POST','?logout=true',true);
  	xhr.onload=function(){location.href='?status=true';};
  	xhr.send();
  }
  </script>
</div>

<script>

var fFrame = true;

function resizeIframe1(obj) {
	obj.style.height = (parseInt(obj.contentWindow.document.body.clientHeight) + 250) + 'px';
	if (fFrame) {
		fFrame = false;
		obj.contentWindow.location.reload();
	}
}

function resizeIframe2(obj) {
        obj.style.height = (parseInt(document.body.scrollHeight) - 100) + 'px';
}

function openCity(evt, cityName) {
  var i, tabcontent, tablinks;
  tabcontent = document.getElementsByClassName("tabcontent");
  for (i = 0; i < tabcontent.length; i++) {
    tabcontent[i].style.display = "none";
  }
  tablinks = document.getElementsByClassName("tablinks");
  for (i = 0; i < tablinks.length; i++) {
    tablinks[i].className = tablinks[i].className.replace(" active", "");
  }
  document.getElementById(cityName).style.display = "block";
  evt.currentTarget.className += " active";
}

// Get the element with id="defaultOpen" and click on it
document.getElementById("defaultOpen").click();
</script>


   
</body>
</html> 

			   					]]) --/*<iframe seamless src="http://]]..ngx.var.host..[[:]]..tostring(mkyboot.cfg.server.shell_port)..[["  class="iframe" id="frame" name="mainFrame" scrolling="auto" ></iframe>*/
				end;
			end;
		end;
	--[[===========================================================================================================================================================================================]]

		if mkyboot.inc.isFile(mkyboot.cfg.server.workdir.."/"..mkyboot.cfg.server.distdir.."/cfg/"..mkyboot.cfg.server.config) then mkyboot.cfg = mkyboot:LoadFromFile(mkyboot.cfg.server.workdir.."/"..mkyboot.cfg.server.distdir.."/cfg/"..mkyboot.cfg.server.config) else mkyboot.cfg = dofile("/srv/mkyboot/cfg/cfg.lua").cfg; mkyboot:SaveToFile(mkyboot.cfg.server.workdir.."/"..mkyboot.cfg.server.distdir.."/cfg/"..mkyboot.cfg.server.config,mkyboot.cfg); end;
		mkyboot:GetPage()
