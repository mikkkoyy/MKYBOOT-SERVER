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
-- apt install etherwake shellinabox qemu-utils lua-json lua-socket lua-posix nginx-extras
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
	--[[ TARGET COMMANDS sets opt1,opt2,opt3  ]]
	--[[===========================================================================================================================================================================================]]

		mkyboot.lib.json		= require("json");
		mkyboot.lib.lfs  	= require("lfs");	
		mkyboot.lib.posix	= require("posix");	  	  	
	--[[ TARGET COMMANDS sets opt1,opt2,opt3  ]]
	--[[===========================================================================================================================================================================================]]
	  	mkyboot.cmd.tgt = 	{
					new 		= function(opt,p_tid) 	return os.execute("/usr/sbin/tgtadm --lld iscsi --op new --mode target --tid "..p_tid.." -T "..opt); 									end, 					--CREATE TARGET
					destroy		= function(opt) 		return os.execute("/usr/sbin/tgtadm --lld iscsi --op delete --mode target --tid "..opt); 												end, 					--REMOVE TARGET
					kill		= function(opt) 		return os.execute("/usr/sbin/tgtadm --lld iscsi --op delete --force --mode target --tid "..opt); 										end, 					--FORCE REMOVE TARGET
					show 		= function(opt) 		return os.execute("/usr/sbin/tgtadm --lld iscsi --op show --mode target "..opt); 														end, 					--INFO TARGETS
					rules		= function(p_tid,opt)	return os.execute("/usr/sbin/tgtadm --lld iscsi --mode target --op bind --tid "..p_tid.." -I "..opt); 									end, 					--ALLOW CLIENT IP
					unrul		= function(p_tid,opt)	return os.execute("/usr/sbin/tgtadm --lld iscsi --mode target --op unbind --tid "..p_tid.." -I "..opt); 								end,
					used		= function(p_tgt) 		local fd; 																																---
								  if os.execute("/usr/sbin/tgtadm --lld iscsi --op show --mode target | /usr/bin/grep --color \"Target [0-9]:\" | /usr/bin/grep "..p_tgt) ~= nil then
								  fd = io.popen("/usr/sbin/tgtadm --lld iscsi --op show --mode target | /usr/bin/grep --color \"Target [0-9]:\" | /usr/bin/grep "..p_tgt);						---
								  return (#fd:read("a*") > 0);
								  else return false; end; 																																		end
					};																																											---
		mkyboot.cmd.lun = 	{																																									---
							add		= function(p_tid,p_lun,p_dev) return os.execute("/usr/sbin/tgtadm --lld iscsi --op new --mode logicalunit --tid "..p_tid.." --lun "..p_lun.." -b "..p_dev); end,
							del 	= function(p_tid,p_lun) return os.execute("/usr/sbin/tgtadm --lld iscsi --op delete --mode logicalunit --tid "..p_tid.." --lun "..p_lun);					end,
							stop 	= function(p_opt) return os.execute("/usr/sbin/tgtadm --offline "..p_opt);																					end,
							start 	= function(p_opt) return os.execute("/usr/sbin/tgtadm --ready "..p_opt);																					end
					};																																											---
		mkyboot.cmd.nbd = 	{																														
							mod 	= function(p_max_part,p_nbds) return os.execute("/usr/sbin/modprobe nbd max_part "..p_max_part.." nbds "..p_nbds); 											end,
							unmod 	= function() return os.execute("/usr/sbin/modprobe -r nbd"); 																								end,
							add 	= function(p_dev,p_path,p_flags) os.execute("/srv/mkyboot/client.lua "..p_dev.." "..p_path.." "..p_flags);			
									tmpfile = io.open("/tmp/debug","w")
									tmpfile:write("/srv/mkyboot/client.lua /usr/bin/qemu-nbd '--connect="..p_dev.." "..p_path.." --pid-file="..p_path..".pid "..p_flags.."'")
									tmpfile:close()
								end,
							del 	= function(p_dev) return os.execute("/usr/bin/qemu-nbd -d "..p_dev.." 2>/dev/null"); 																		end,
							kill 	= function(p_pid) return os.execute("/usr/bin/kill -9 ", p_pid); 																							end,
							used	= function(p_dev) if p_dev ~= nil and os.execute("/usr/bin/lsof -t "..p_dev.." 2>/dev/null") ~= nil then local fd; fd = io.popen("/usr/bin/lsof -t "..p_dev.." 2>/dev/null"); return (#fd:read("a*") > 0);	else return false end end,
							usewho	= function(p_dev) local fd; 
															if os.execute("/usr/bin/lsof -t "..p_dev.." | /usr/bin/grep \"$(/usr/bin/pgrep qemu-nbd)\"  2>/dev/null") ~= nil then 
																fd = io.popen("/usr/bin/lsof -t "..p_dev.." | /usr/bin/grep \"$(/usr/bin/pgrep qemu-nbd)\"  2>/dev/null");							---
																if fd ~= nil and (#fd:read("a*") > 0) then return 1 end;
															 	else return false; end;																		---
															if os.execute("/usr/bin/lsof -t "..p_dev.." | /usr/bin/grep \"$(/usr/bin/pgrep tgtd)\" 2>/dev/null") ~= nil then
																fd = io.popen("/usr/bin/lsof -t "..p_dev.." | /usr/bin/grep \"$(/usr/bin/pgrep tgtd)\" 2>/dev/null");								---
																if fd ~= nil and (#fd:read("a*") > 0) then return 2 end; else return false; end;												end
					};
		mkyboot.cmd.img = 	{
							new 	= function(p_path,p_size) 
									return os.execute("/usr/bin/qemu-img -f qcow2 -o preallocation=metadata,compat=1.1,lazy_refcounts=on encryption=off "..p_path.." "..p_size);				end,
							child 	= function(p_parrent,p_child) 
									return os.execute("/usr/bin/qemu-img create -f qcow2 -b "..p_parrent.." "..p_child.." -o lazy_refcounts=on 2>>/tmp/result ");								end,
							del 	= function(p_image) return os.remove(p_image); 																												end,
							commit 	= function(p_image) local fd; fd = io.popen("/usr/bin/qemu-img commit "..p_image); 																			end,
							used 	= function(p_image) if os.execute("/usr/bin/lsof "..p_image.." 2>/dev/null") ~= nil then 
								local fd; fd = io.popen("/usr/bin/lsof -t "..p_image.." 2>/dev/null"); return (#fd:read("a*") > 0);	else return false; end;										end
							};

		mkyboot.cmd.zfs =  	{
							mtab 	= function(p_args) local fd_file, fd_data
																fd_file = io.open("/etc/mtab", "r");  
																fd_data = fd_file:read("*a");
															 fd_file:close(); 
															if string.find(fd_data,p_args) ~= nil then return true else return false; end;														end,
							snap 	= function(p_data) 			 return os.execute("/usr/sbin/zfs snap "..p_data.." 2>/dev/null"); 																end,
							unsnap 	= function(p_data) 			 return os.execute("/usr/sbin/zfs destroy -f "..p_data.." 2>/dev/null");														end,
							mount 	= function(p_data,p_point)	 return os.execute("/usr/bin/mount -t zfs "..p_data.." "..p_point.." 2>>/var/log/messages"); 									end,
							unmount  = function(p_point)			 return os.execute("/usr/bin/umount -f "..p_point.." 2>/dev/null");															end
							}
		mkyboot.cmd.power = 	{

							on = function(p_iface, p_mac) return os.execute("/usr/sbin/etherwake -i "..p_iface.." "..p_mac); 																end
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
					if os.execute("/usr/bin/lsof "..p_patern.." 2>/dev/null") ~= nil then 
					local fd; fd = io.popen("/usr/bin/lsof "..p_patern.." 2>/dev/null"); return (#fd:read("a*") > 0); 	
					else 
					return false; end;

		end;
		mkyboot.inc.lsofkill = function(p_path)
					mkyboot.inc.debug("TUT 0")
					if os.execute("/usr/bin/lsof -t "..p_path.." 2>/dev/null") ~= nil then 
					local fd; fd = io.popen("/usr/bin/kill -9 $(/usr/bin/lsof -t "..p_path..") 2>/dev/null"); return (#fd:read("a*") > 0); 	
					else 
					return false; end;
							 
		end;
		mkyboot.inc.search_nbd = function ()
					for i_index = 1,mkyboot.cfg.server.nbd_nbds,1 do
						mkyboot.inc.debug("TUT 2")
						if os.execute("/usr/bin/lsof /dev/nbd"..i_index.." 2>/dev/null | /usr/bin/wc -l") ~= nil then 
							local fd; fd = io.popen("/usr/bin/lsof /dev/nbd"..i_index.." 2>/dev/null | /usr/bin/wc -l"); if tonumber(fd:read("a*")) == 0 then return("/dev/nbd"..i_index);  end;
						else 
							return false; 
						end;			
					end;
		end; 

		mkyboot.inc.getpid_nbd = function (t_path)
			local result
			if t_path ~= nil and mkyboot.inc.isFile(t_path) then
				if os.execute("/usr/bin/lsof -t "..t_path.." 2>/dev/null") ~= nil then 

					local fd; fd = io.popen("/usr/bin/lsof -t "..t_path.." 2>/dev/null "); result = (fd:read("a*"));
					if result == '' then return nil else return result end;
				else
					return nil
				end; 
			end;
			result = nil
		end;
		
		mkyboot.inc.getdev_nbd = function (t_pid)
			local result
				if t_pid ~= nil and os.execute("/usr/bin/lsof -p "..t_pid:gsub('%W','').." 2>/dev/null |  /usr/bin/awk '/\\/dev\\/nbd/ { print $NF }' ") then 
					local fd; fd = io.popen("/usr/bin/lsof -p "..t_pid:gsub('%W','').." 2>/dev/null |  /usr/bin/awk '/\\/dev\\/nbd/ { print $NF }' "); result = fd:read("a*") ;
					if result == '' then return nil else return result end;
				else
					return nil;
				end; 	
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
						return os.execute("/usr/bin/systemctl "..p_cmd.." "..p_name)
		end;
		mkyboot.inc.monit = function()
					if not mkyboot.inc.lsof("-t -i:"..mkyboot.cfg.dhcp.port) 	then mkyboot.inc.systemctl("isc-dhcp-server","start"); mkyboot.inc.systemctl("isc-dhcp-server","restart");	end;
					if not mkyboot.inc.lsof("-t -i:"..mkyboot.cfg.tftp.port) 	then mkyboot.inc.systemctl("isc-dhcp-server","start"); mkyboot.inc.systemctl("tftpd-hpa","restart");	end;
					if not mkyboot.inc.lsof("-t -i:"..mkyboot.cfg.iscsi.port) then mkyboot.inc.systemctl("isc-dhcp-server","start"); mkyboot.inc.systemctl("tgt","restart");	end;
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
		local f_tmp,f_mac,f_data = os.tmpname()
		os.execute("/usr/sbin/arp -a "..p_ip.." | /usr/bin/awk '{ print $4 }' > "..f_tmp)
		f_mac = io.open(f_tmp, "r")
		f_data = f_mac:read("*a")
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
						if tostring(mkyboot.cfg.server.debug) == "1" then print(mkyboot.cfg.iscsi.iqn..":"..mkyboot.cfg.wks[l_id].mac:gsub('%W',''),mkyboot.cfg.wks[l_id].tid); print(mkyboot.cmd.tgt.used(mkyboot.cfg.iscsi.iqn..":"..mkyboot.cfg.wks[l_id].mac:gsub('%W',''))); end;
						if not mkyboot.cmd.tgt.used(mkyboot.cfg.iscsi.iqn..":"..mkyboot.cfg.wks[l_id].mac:gsub('%W','')) and tostring(mkyboot.cfg.wks[l_id].enable) == "1" then mkyboot.cmd.tgt.new(mkyboot.cfg.iscsi.iqn..":"..mkyboot.cfg.wks[l_id].mac:gsub('%W',''),mkyboot.cfg.wks[l_id].tid); mkyboot.cmd.tgt.rules(mkyboot.cfg.wks[l_id].tid,p_ip); end;
						if tostring(mkyboot.cfg.server.debug) == "1" then 	print("STARTED !"); print(mkyboot.cmd.tgt.used(mkyboot.cfg.iscsi.iqn..":"..mkyboot.cfg.wks[l_id].mac:gsub('%W',''))); end;
			end;
		end;
		function mkyboot:tgtstop(p_ip)
			mkyboot.inc.monit()
			if mkyboot.inc.scCheck() and mkyboot.inc.checkconf() then
					local l_id = mkyboot.inc.GetIDFromIPv4(p_ip);
						if tostring(mkyboot.cfg.server.debug) == "1" then print (mkyboot.cfg.wks[l_id].tid); print(mkyboot.cfg.iscsi.iqn..":"..mkyboot.cfg.wks[l_id].mac:gsub('%W','')); print(mkyboot.cmd.tgt.used(mkyboot.cfg.iscsi.iqn..":"..mkyboot.cfg.wks[l_id].mac:gsub('%W',''))); end;
						-- if 
						if mkyboot.cmd.tgt.used(mkyboot.cfg.iscsi.iqn..":"..mkyboot.cfg.wks[l_id].mac:gsub('%W','')) then mkyboot.cmd.tgt.kill(mkyboot.cfg.wks[l_id].tid); end;
						if tostring(mkyboot.cfg.server.debug) == "1" then print(mkyboot.cmd.tgt.used(mkyboot.cfg.iscsi.iqn..":"..mkyboot.cfg.wks[l_id].mac:gsub('%W',''))); end;
			end;
		end;
	--[[===========================================================================================================================================================================================]]
		function mkyboot:mkChild(p_ip)
			local l_id,l_lockf,p_child,p_parrent = mkyboot.inc.GetIDFromIPv4(p_ip) 
			local l_vid = mkyboot.cfg.wks[l_id].mac:gsub('%W','')
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
			local fd,result 
				fd = io.popen("/usr/sbin/tgtadm --lld iscsi --op show --mode target | /usr/bin/grep 'IP Address: "..p_ip.."'"); --/usr/sbin/tgtadm --lld iscsi --op show --mode target | grep --color "IP Address: 192.168.0.4"
				return (#fd:read("a*") > 0);
		end;
	

	--[[===========================================================================================================================================================================================]]
		function mkyboot:nbdFree(p_ip)
				local l_id,i,v = mkyboot.inc.GetIDFromIPv4(p_ip);

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
		function mkyboot:ImgCommit(id)


		end;
		function mkyboot:zfsmount(p_ip)
			local l_id,zdest,zpoint = mkyboot.inc.GetIDFromIPv4(p_ip)
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
			if l_id ~= nil then zpoint = mkyboot.cfg.zfs.mpoint.."/"..mkyboot.cfg.wks[l_id].mac:gsub('%W',''); zdest = mkyboot.cfg.zfs.dpoint..mkyboot.cfg.wks[l_id].mac:gsub('%W',''); 						end;
			if mkyboot.cfg.wks[l_id].img ~= nil  then mkyboot:nbdFree(p_ip); mkyboot.cmd.zfs.unmount(zpoint); mkyboot.cmd.zfs.unsnap(zdest); lfs.rmdir(zpoint); end;	


		end;		
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
									-- mkyboot:nbdFree("192.168.0.4");
									-- mkyboot:mkChild("192.168.0.4");
									-- mkyboot:nbdConnect("192.168.0.4")
									-- mkyboot:LunAdd("192.168.0.4");					
									-- ngx.say("#!ipxe\n:start\necho Boot menu\nmenu Selection\necho \"MY SHELL\"\nshell\n");


			    elseif ngx.req.get_body_data() then
			    		local file,temp,l_v,l_k
			    			temp = json.decode(ngx.req.get_body_data():gsub('&','","'):gsub('^','{"post":{"'):gsub('=' ,'":"')..'"}}').post;
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
			   elseif ngx.var.arg_testzone == "true" then
				local l_k,l_v,t_id	
								t_id = "1"
							ngx.say("<html><body><pre>") 

							mkyboot.inc.monit();
							mkyboot:tgtstop("192.168.0.4");
							mkyboot:nbdFree("192.168.0.4");
							mkyboot:zfsmount("192.168.0.4");
							mkyboot:mkChild("192.168.0.4");
							mkyboot:nbdConnect("192.168.0.4");
							mkyboot:LunAdd("192.168.0.4");
							ngx.say("GOOD!")
										-- ngx.say(mkyboot.inc.search_nbd())
									-- ngx.say(mkyboot.inc.getpid_nbd("/srv/writeback/win10cc.qcow2_child_b42e992cdddf"))
									-- ngx.say(mkyboot.inc.getpid_nbd("/srv/writeback/lord.qcow2_child_b42e992cdddf"))
									if 	mkyboot.inc.getpid_nbd("/srv/writeback/Win10_2004_Russian_x64.iso_child_b42e992cdddf") ~= nil then	ngx.say(mkyboot.inc.getpid_nbd("/srv/writeback/Win10_2004_Russian_x64.iso_child_b42e992cdddf")) end;
										--ngx.say(mkyboot.inc.getdev_nbd(mkyboot.inc.getpid_nbd("/srv/writeback/win10cc.qcow2_child_b42e992cdddf")))
								--		ngx.say(mkyboot.inc.isMacARP("192.168.0.4"))
							-- ngx.say("{\"WKS\"={\"enable\":1,\"group\":\"DEFAULT\",\"gateway\":\"DEFAULT\",\"dns\":\"DEFAULT\",\"domainsearch\":\"DEFAULT\",\"supper\":0,\"img\":[{\"path\":\"none\",\"commit\":0,\"enable\":1,\"nbd\":\"\\/dev\\/nbd"..(t_id+2).."\",\"type\":\"dyndisk\",\"boot\":0,\"cache\":\"none\"},{\"path\":\"none\",\"commit\":0,\"enable\":1,\"nbd\":\"\\/dev\\/nbd"..(t_id+3).."\",\"type\":\"dynblock\",\"boot\":0,\"cache\":\"none\"},{\"path\":\"none\",\"commit\":0,\"enable\":1,\"nbd\":\"\\/dev\\/nbd0\",\"type\":\"iso\",\"boot\":0,\"cache\":\"none\"}],\"fileboot\":\"ipxe\",\"mac\":\"\",\"tid\":"..t_id..",\"ipv4\":\"\",\"opt\":[],\"name\":\"PC00"..t_id.."\",\"swp\":0},\"images\":{\"dyndisk\":"..json.encode(mkyboot.inc.ls_files(mkyboot.cfg.server.imgdir))..",\"iso\":"..json.encode(mkyboot.inc.ls_files(mkyboot.cfg.server.imgisodir))..",\"dynblock\":"..json.encode(mkyboot.inc.ls_devices(mkyboot.cfg.zfs.devpoint)).."},\"groups\""..json.encode(mkyboot.cfg.groups.wks).."}")
							-- --ngx.say(json.encode(mkyboot.inc.ls_files(mkyboot.cfg.server.imgdir)))
			   				ngx.say("</pre></body></html>")
			   	l_k,l_v = nil,nil
			   elseif ngx.var.arg_status == "true" then
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
			<div class="header">
				<a href="#default" class="logo"><b style="color: red; box-shadow: 4px 0 10px rgba(0,0,0,0.5);">NS</b>Boot
				<div class="header-right">												    
			 		<a href="#Support">Support</a>
			 		<a href="#License">License</a>
			 		<a class="active" href="#Logout">Logout</a>
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
  <button class="tablinks" onclick="openCity(event, 'Logout')">
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
  <p><h2>Tokyo</h2></p>

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
