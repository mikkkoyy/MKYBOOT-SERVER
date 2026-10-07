# Repository Comparison Audit

## 1. Audit Metadata

| Item | Value |
|---|---|
| Audit date | 2026-10-07 |
| Audited local path | `D:\project\diskless+timer\diskless` |
| Audited remote | `https://github.com/mikkkoyy/MKYBOOT-SERVER` |
| Remote commit examined | `816f3c0d7ad69f59c1d7d3ae8c0423e8aab4b4c6` |
| Remote commit date | 2026-09-25T03:31:24Z |
| Remote tree SHA | `6c0d3b5fef1bbe1d64280f24d0042603bc7015b3` |
| Network access | Available (GitHub REST API and `git ls-remote` both reachable) |

## 2. Local Project Identity

| Attribute | Value |
|---|---|
| Application name | MKYBOOT |
| Version | 4.0.0 (`srv/cfg/mkyboot.json` → `server.version`) |
| Vendor | Nuke Technology LLC |
| License | AGPL-3.0 (`LICENSE`, 34523 bytes) |
| Languages | Lua 5.x (OpenResty/nginx Lua), POSIX shell, Go (Windows launcher), plus a vendored iPXE C tree |
| Local HEAD commit | `816f3c0d7ad69f59c1d7d3ae8c0423e8aab4b4c6` |
| Local HEAD date | 2026-09-25 11:31:24 +0800 |
| Branch | `main`, tracking `origin/main` |
| Git remote | `https://github.com/mikkkoyy/MKYBOOT-SERVER.git` (fetch and push identical; no embedded credentials) |
| Tags | None locally, none on remote (0 tags, 0 releases) |
| Tracked files | 1931 |
| Extra worktree | `.kilo/worktrees/shore-hall` — secondary checkout of the same repo at the same commit (detached HEAD) |

Repository identity is conclusive without inference: the local checkout's `origin` URL is exactly the audited remote, and the local HEAD equals the remote `main` tip.

## 3. Primary Classification

**Classification A — Exact same source (committed history).**

**Confidence: Very high.**

Basis is cryptographic and reproducible, not name-based:

- `git rev-parse HEAD` (local) == `refs/heads/main` from `git ls-remote origin` == GitHub API `main` SHA.
- `git rev-parse HEAD^{tree}` (local) == GitHub API commit tree SHA (`6c0d3b5f…`). A matching tree object means the entire committed directory structure and every blob is byte-identical, not merely similar.
- Local `origin` is the audited repository itself.

This is not a case of "matching names or similar features". It is the same Git object graph.

### Working-tree deviation (minor, uncommitted)

The working tree is *not* clean. Three files are modified and one is untracked. These are local, unpublished edits:

| File | State | Nature of change |
|---|---|---|
| `.gitignore` | modified | Adds `/launcher/dist/` ignore rule |
| `bin/mkyctl.lua` | modified | Extracts `mkyboot.cmd.img` (new/child/del/commit/used) into a new `src/image.lua` module; adds `package.path` extension |
| `srv/mkyboot/mkyctl.lua` | modified | Identical extraction of the same `img` command block |
| `src/image.lua` | untracked (new) | 97-line image lifecycle module |

Blob-level verification confirms the split: `src/cfg.lua` and `README.md` hash identical to HEAD; `bin/mkyctl.lua` and `srv/mkyboot/mkyctl.lua` differ from HEAD.

Net effect: local is the remote HEAD plus one small, self-consistent, uncommitted refactor. This does not change the classification — the shared committed source is exact.

## 4. Git Ancestry Findings

- 10 commits on `main`, oldest first: `75a8612` "Import existing MKYBOOT diskless server source" → `740174d` "Rename NSBoot to MKYBOOT across entire codebase" → security hardening commits (`33993e7`, `0e4553f`, `6dff842`, `51e0e69`) → runtime fixes (`b1da2e7`, `eae0cf1`, `c77bc9f`) → `816f3c0` "feat: add native Windows MKYBOOT launcher".
- Remote metadata: `fork: false`, no parent repository, created 2026-09-23, public, size ~7.2 MB.
- First commit is an import; the prior name was NSBoot. The origin is therefore a single imported/forked codebase, not a multi-contributor upstream.
- No merges, no tags, no releases, no forks. Linear history, single lineage.

## 5. File-Level Similarity

**Committed content: 100% identical.** Verified by tree-SHA equality across all 1931 tracked paths, not by sampling.

Structure (first-party, excluding vendored `src/ipxe` and generated `launcher/dist`):

```
README.md, LICENSE, install.sh, .gitignore
bin/          client.lua, mkyctl.lua, server.lua        (Lua IPC daemon + control module)
src/          cfg.lua (389 KB web UI + config), image.lua (untracked, local only), ipxe/ (vendored C)
srv/          cfg/mkyboot.json, mkyboot/mkyctl.lua, mkyboot.db.json, tftp/{ipxe.efi,ipxe.kpxe,bg.png}
examples/     etc/dhcp/dhcpd.conf, etc/netplan/, etc/nginx/, srv/cfg/mkyboot.json
launcher/     Go native Windows tray app (app.go, ui.go, server.go, win32.go, tray.go, config.go,
              Makefile, go.mod, assets/, uitest/*.ps1, plus *_test.go)
test/         runtime/test_auth.sh, test_dependencies.sh, test_install.sh,
              etc/init.d/mkybootd, usr/bin/mkybootd, srv/mkyboot/*
screenshots/
```

Key implementation files, with function noted:

| File | Role |
|---|---|
| `src/cfg.lua:1` | Configuration schema + entire embedded web admin UI (HTML/JS) as `mkyboot.cfg.web.pages.html.js`; `mkyboot.cfg.iscsi` at line 4; IQN `2020-02-10.com.mkyboot` at line 69 |
| `src/cfg.lua:197` | `HASH_ITERATIONS = 250000`; `:210` `ngx.sha1_bin`; `:283` `algorithm = "iter-sha1"` |
| `src/cfg.lua:244` | ISC DHCP `option iscsi-initiator-iqn code 203` and `option ipxe.iscsi code 17` |
| `srv/mkyboot/mkyctl.lua:170` | `/dev/urandom` entropy read |
| `srv/mkyboot/mkyctl.lua` | `tgtadm` 15 refs, `zfs` 34 refs, `dhcp` 36 refs, `ipxe` 7 refs, `fileboot` 12 refs, `qemu-nbd` 1 ref, `qcow2` 1 ref |
| `src/image.lua:1` | Local-only refactor; delegates `new/child/delete/commit/used` |
| `launcher/server.go:89` | `sessionCookieName = "mkyboot_session"`; documented API surface `/?api=status`, `/?api=server&op=…`, `/?login=true`, `/?logout=true` |

## 6. Feature Comparison Table

Because local and remote are the same source, every feature is identical on both sides. Status reflects what was actually found in source, not what the README claims.

| Feature | Status | Evidence |
|---|---|---|
| PXE / iPXE boot (UEFI + BIOS) | Implemented (server-side) | `srv/tftp/ipxe.efi` (995 KB) and `ipxe.kpxe` (336 KB) shipped; vendored full iPXE C tree under `src/ipxe`; `fileboot: "ipxe"` set per-client in `mkyboot.json`; `ipxe` script generation in `mkyctl.lua` |
| DHCP | Implemented (config generation) | `mkyctl.lua` `dhcp` 36 refs; `examples/etc/dhcp/dhcpd.conf` with iPXE/ISC options; delegated to external `isc-dhcp-server`. Server does not run its own DHCP daemon |
| TFTP | Delegated to external daemon | Ships TFTP payload files; runtime uses `tftpd-hpa` per `install.sh`. No TFTP server implemented in-project |
| iSCSI target management | Implemented | 15 `tgtadm` invocations; IQN/port/proto config in `src/cfg.lua:69-72`; external `tgt` daemon via `install.sh` |
| Diskless Windows boot | Implemented in design; **not runtime-verified** | iPXE + iSCSI path is fully coded; no Windows image or boot test evidence in repo. Verification would require hardware |
| Linux diskless boot | **Not found** | No Linux image type or kernel/rootfs handover logic. `sanboot` 0 refs. README's "Windows/Linux" claim is not supported by source |
| Master images + per-client overlays | Implemented | `img.child` builds QCOW2 overlay with `-b <parent>`; `img.commit` merges via `qemu-img commit`; client config uses `type: dyndisk` + `commit` flag |
| Write-back caching | Implemented | Per-client QCOW2 overlay files under writeback path; `cache: "unsafe"` per-image flag in config |
| ZFS snapshots | Implemented | 34 `zfs` refs, 9 `snap` refs in `mkyctl.lua`; `install.sh` creates pool/datasets |
| Image formats — QCOW2 | Implemented | `dyndisk` (QCOW2 backing-file) and `dynblock` (ZFS zvol), plus `iso` |
| Image formats — VHDX | **Not found** | Zero case-insensitive matches for `vhdx` across the entire working tree, including vendored iPXE |
| Client registration / MAC config | Implemented | `wks` array with `mac`, `ipv4`, `name`, `tid`, `group`, `enable`; 3 workstations configured |
| Web admin interface | Implemented | nginx-extras `content_by_lua_file /srv/mkyboot/modules/mkyctl.lua` on port 8888; full Bootstrap UI embedded in `src/cfg.lua` |
| Native Windows admin interface | Implemented (Go, committed at HEAD) | `launcher/` Go app: tray, Win32 GUI, server control, session proxy; has `_test.go` files and PowerShell UI tests |
| Auth / password hashing | Implemented | 250,000-round salted SHA-1 with `algorithm: "iter-sha1"` metadata; `/dev/urandom` salts. Weaker than bcrypt/argon2, as README admits |
| Session management (admin auth) | Implemented | File-based server sessions; `mkyboot_session` cookie in launcher; HttpOnly + SameSite=Strict |
| Rate limiting / lockout | Implemented | README documents 5 attempts / 5 min per-IP; lockout code path present in `mkyctl.lua` auth section |
| Server API | Implemented | `/?api=status`, `/?api=server&op=restart\|stop`, login/logout; `launcher/server.go` is a typed client for it |
| Client-agent communication | **Not found** | No agent binary, no client heartbeat/polling protocol. `bin/client.lua` (1354 bytes) and `test/usr/bin/mkybootd` (1963 bytes) are stub-sized; the "client" is the PXE-booting machine over iSCSI, not a software agent |
| **PC timer / class countdown** | **Not found** | Zero hits in first-party Lua/Go source. All `timer` matches are inside vendored iPXE (x86 ACPI/PIT/RDTSC/hypervisor timers). Nothing implements class-session timing |
| **Authoritative lock / unlock** | **Not found** | No `unlock` match in first-party source. `lock` appears only as SQLite-style table names in `mkyboot.db.json` (per-client image locks) and login lockout — a different concept from session lock/unlock |
| Wake-on-LAN | Implemented | `etherwake` invoked; documented in README |
| Shell-in-a-box remote console | Referenced only | `server.shell_port` config key and `shellinabox` in `install.sh` deps; no handler found in first-party Lua |

## 7. Missing Features and Architectural Differences

There are no architectural differences — same source. The gaps below are gaps *in the shared project*:

1. **No PC timer subsystem.** This is the most significant gap relative to the local directory name (`diskless+timer`). No session countdown, no class-time management, no scheduled actions anywhere in the codebase.
2. **No authoritative lock/unlock.** No mechanism for a server to lock or unlock a running client session. Client state is derived from config and boot events.
3. **No client agent.** No polling/heartbeat/IPC client. Server-side knowledge of a client is limited to what the iPXE request and `mkyboot.json` provide.
4. **No Linux diskless boot.** README claims it; code does not implement it.
5. **No VHDX support.** QCOW2 and ZFS zvol only.
6. **Vendor-heavy integration.** DHCP, TFTP, iSCSI target, and ZFS are all delegated to external system daemons. The project is a control plane and orchestration layer, not a self-contained stack.
7. **Duplicated control module.** `bin/mkyctl.lua` (152947 B), `srv/mkyboot/mkyctl.lua` (151077 B), and `test/srv/mkyboot/mkyctl.lua` (152354 B) are three near-copies of the same 150 KB file. They can drift; they already differ in size. The uncommitted `src/image.lua` refactor was applied to two of the three but not `test/`.
8. **Path assumption bug risk.** The local refactor appends `;src/?.lua` to `package.path`, a relative path. That resolves against the process working directory, not the script location. It will not resolve correctly when nginx or the daemon runs from `/`.

## 8. Security and Licensing Concerns

**Licensing**
- AGPL-3.0. Network use of the web admin interface triggers source-provision obligations. Any derived or integrated deployment should be reviewed against AGPL §13.
- Vendored iPXE carries its own triple stack: `src/ipxe/COPYING` (GPL-2.0), `COPYING.GPLv2`, `COPYING.UBDL`. Redistribution must respect all three. This is a compliance obligation commonly missed.

**Security (observed, not speculative)**
- **Password KDF is SHA-1 based** (`iter-sha1`, 250,000 rounds). SHA-1 is deprecated; argon2id or bcrypt should replace it. README discloses this.
- **Plain HTTP only.** `examples/etc/nginx/sites-available/default` listens on `8888` with the SSL block commented out. Admin credentials and session cookies traverse the LAN unencrypted.
- **Session files are plaintext JSON** on disk (README discloses).
- **Shell command construction.** The codebase builds command strings for `tgtadm`, `qemu-img`, `qemu-nbd`, and `zfs`. Path and argument validators exist (`mkyboot.inc.valid.safe_path`, `img_path`, `img_size`) and a prior commit (`33993e7` "security: harden IPC and remove unsafe command execution") shows this was actively addressed — but string-built shell execution remains the dominant pattern. Treat the validators as the single point of failure for injection defense.
- **Config files checked into the repo.** `srv/cfg/mkyboot.json` and `srv/mkyboot/mkyboot.db.json` contain real-looking deployment data: server IP `192.168.0.2`, gateway, DNS, and 3 workstations with genuine MAC addresses and image names (`win10cc.qcow2`). MAC addresses and topology are sensitive in a lab environment.
- **No plaintext credential found.** Scans of `mkyboot.json`, `mkyboot.db.json`, and `cfg.lua` for password/secret/token/apikey values returned no embedded secret values. Password material is generated at first setup. No secrets were found or reproduced in this audit.
- **`web.shell_port`** suggests a remote shell surface; if `shellinabox` is enabled, it is an additional unauthenticated-adjacent attack surface worth reviewing.

## 9. Is Source-Copying or Integration Worthwhile?

**Copying: no.** The local tree already *is* the repository at the identical commit. There is nothing to copy. Any "integration" of remote source into local would be a no-op or would reintroduce the uncommitted refactor's third copy.

**Differentiation is what has value.** The local project needs the three capabilities the remote lacks, and they are genuinely absent rather than merely undocumented:

1. A PC timer / class-session module — new subsystem, no existing hook to reuse beyond the config schema and web UI mounting pattern.
2. Authoritative lock/unlock — requires a server-to-client command channel, which does not exist. This is the hard prerequisite; without an agent or a protocol, unlock cannot reach a running client.
3. A client agent — the enabling layer for both of the above.

**Realistic path:** build the agent + API contract first, then layer timer and lock on top. Reuse the existing config schema, auth, and session mechanisms, which are sound enough to build on. If Windows-client locking is the goal, note that iPXE only runs pre-OS; a post-boot control channel on the Windows side is required and is entirely new work.

**Reuse warning:** `test/srv/mkyboot/mkyctl.lua` was left untouched by the `src/image.lua` refactor. Whichever copy you treat as canonical, converge them — three 150 KB near-duplicates will diverge.

## 10. Recommended Next Steps

1. Decide the canonical path for the control module and delete or generate the other two copies.
2. Fix `package.path` in `bin/mkyctl.lua` and `srv/mkyboot/mkyctl.lua` to resolve relative to the script location, not the CWD, or the new `src/image.lua` will fail to load under nginx/systemd.
3. Add `test/srv/mkyboot/mkyctl.lua` to the refactor, or replace it with a symlink/generation step.
4. Correct the README: remove the Linux diskless boot claim, or implement it. The current claim is unsupported by source.
5. Enable TLS on the admin interface, or document the plaintext-HTTP risk as accepted.
6. Replace the SHA-1 KDF with argon2id/bcrypt. The `algorithm` metadata field already supports a versioned upgrade.
7. Replace the MAC addresses, IPs, and image names in `srv/cfg/mkyboot.json` and `srv/mkyboot/mkyboot.db.json` with placeholders before any further public exposure.
8. For the timer feature: design the server↔client contract first. Nothing in the current architecture can deliver a lock command to a booted client.
9. Run the existing test suites (`test/runtime/test_auth.sh`, `test_dependencies.sh`, `test_install.sh`, `launcher/*_test.go`, `launcher/uitest/*.ps1`) to establish a baseline. **Not executed during this audit** — this was a read-only inspection on Windows, and the runtime tests target Ubuntu with `systemctl`, `zfs`, and `tgt`.

## 11. Audit Limitations

- No code was built, installed, or executed. All feature statuses are from source reading only.
- PXE, iSCSI, and Windows boot paths are **not runtime-verified**. Their status reflects implemented code, not observed working boot.
- Windows-client locking, iPXE ROM integrity, and the Go launcher build were not tested.
- The `.kilo/worktrees/shore-hall` worktree was inventoried but not deeply compared; it is a second checkout of the same repo at the same commit.
- Vendored iPXE was inventoried but its ~1900 C files were not audited individually; it is presumed upstream iPXE.

---

**Audit result: PASS**

**Classification: A — Exact same source** (committed history identical, tree SHA `6c0d3b5f…` matches on both sides, with a small uncommitted local refactor). Confidence: very high.

**Report path:** `D:\project\diskless+timer\diskless\REPO_COMPARISON_REPORT.md`