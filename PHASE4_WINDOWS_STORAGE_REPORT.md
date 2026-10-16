# MKYBOOT-SERVER — PHASE 4: Windows Diskless Storage

**Status:** Analysis complete; iSCSI target evaluation documented. Implementation of VHDX storage layer pending.

| Repository | Branch | HEAD |
|---|---|---|
| https://github.com/mikkkoyy/MKYBOOT-SERVER | main | 70b195a14980d7e964498c3b10832eb69112f281 |

## 1. Git Safety / Working Tree

- HEAD: `70b195a` ("feat: add Windows-native MKYBOOT server foundation (Phase 2)")
- Branch: `main`, remote: `origin` (fetch/push)
- Working tree: clean (no staged or unstaged source changes beyond untracked docs already present)
- No destructive commands used; no force push attempted

## 2. Current Server State (from existing code)

The server foundation is complete (Phase 2):
- Windows Service (SCM) support via `golang.org/x/sys/windows/svc`
- HTTP API with auth, client registry, heartbeat, lock/unlock, session timer
- Atomic writes with Windows ACL protection (owner-only DACL)
- Config in `%ProgramData%\MKYBOOT`, atomic persisted state
- All existing tests pass: 8.083s, all PASS (build/vet/gofmt clean)

**Missing for Phase 4:** Image/VHDX management endpoints, storage backend abstraction, VHDX operations, per-client differencing disk management, iSCSI target integration interface.

## 3. Windows iSCSI Target Dependency — Evaluation

There is **no first-party iSCSI target** in Windows client editions (Windows 10/11). Windows Server has "iSCSI Target Server" role; Windows 10 Home/Pro/Enterprise do not include it.

### Candidate Approaches

| Option | Windows Compat | Licensing | VHDX Support | Concurrent Clients | Automated Service Mgmt | Notes/Risks |
|---|---|---|---|---|---|---|
| **Microsoft iSCSI Target Server** (Windows Server) | Windows Server only | Included in Server licensing | Native VHD/VHDX | Yes | Windows Service/SCM | **Not available** on Windows 10 Home (this host). Cannot assume availability. |
| **StarWind Virtual SAN Free/Target** | Windows 10/11/Server | Free edition exists; commercial tiers vary | VHDX/VDI | Yes | Windows Service | Widely used; requires installation. Needs explicit authorization before installing third-party software. |
| **iSCSI Target (from Microsoft Store)** or other consumer targets | Windows 10/11 | Varies | Limited | Varies | Often GUI-driven | Uncertain long-term support/automation. Not suitable for headless service without approval. |
| **AOE/other, or custom kernel target** | Poor | Complex | Varies | Risky | Hard | Not practical/maintainable. |
| **TGT/SCST in WSL2/VM** | Requires Linux VM/WSL2 | Open-source | VHDX via tools | Yes | Linux service | Contradicts "Windows-native" requirement (needs Linux runtime). Also WSL2 needs VT/Hyper-V features. Current host blocked on SVM. |
| **Target-free approach (iPXE + NBD over network, or iSCSI initiator-less boot)** | Complex | Experimental | Varies | Varies | Hard | iPXE can boot from HTTP/NFS in some cases, but "diskless OS boot via iSCSI" as specified in chain requires iSCSI target or equivalent. Not a drop-in replacement. |

### Recommendation

**No acceptable, pre-approved Windows-native iSCSI target is available on this host's edition (Windows 10 Home).** Therefore, per the requirements (§2), we must:

1. **Do not download/install third-party iSCSI target software** without explicit authorization (explicitly stated). 
2. **Implement the storage abstraction and VHDX image management that can be tested independently.** This includes path validation, VHDX create/inspect, base immutability, per-client differencing VHDX (write isolation), locking, recovery/cleanup.
3. **Document the precise target integration interface** (LUN attachment, target IQN, exclusive access semantics) and the **remaining blocker**. 
4. **Do not fabricate iSCSI functionality.**

The iSCSI layer is abstracted behind the storage/target interface. The application can track assignments and validate state even if the actual target binding is deferred until an authorized target is installed in the lab.

## 4. VHDX Image Management Design

### Storage Layout

Default base path: `%ProgramData%\MKYBOOT\images\` (configurable)
- `base/` - read-only base VHDX images
- `clients/{clientID}/` - per-client writable state (differencing VHDX + metadata)
- `tmp/` - atomic operations

Constraints/guarantees:
- All paths validated against storage root (no `..`, no traversal)
- Base images marked read-only via file attributes + application-level enforcement
- Per-client writable state isolated (never shared)
- Exclusive locks (file-based or mutex) around lifecycle ops
- Atomic writes for metadata

### VHDX Operations

Options for VHDX creation/management on Windows:
1. **qemu-img** (cross-platform, documented). Can create VHDX, create differencing disks (`qemu-img create -f vhdx -b base.vhdx diff.vhdx`). Available as Windows binary. 
2. **Windows Hyper-V APIs / WMI / PowerShell** (`New-VHD`, `Get-VHD`). Requires Hyper-V cmdlets/feature availability varies; better to avoid PowerShell dependency in Go service. 
3. **Third-party Go libraries** - avoid adding heavy deps unless necessary.

Prefer **qemu-img** (already referenced in repo, deterministic behavior). The service will invoke it as an external tool with validated args; output captured and errors surfaced.

Required operations:
- `CreateImage(path string, sizeBytes int64)` - create fixed/dynamic VHDX
- `CreateDifferencing(basePath, diffPath string)` - COW overlay
- `InspectImage(path string)` - get size, format, parent chain (metadata)
- `IsReadOnly(path string)` - enforce immutability
- `SetReadOnly(path string, ro bool)` - mark base images RO
- `ValidatePath(path string, root string)` - containment check

## 5. Per-Client Writable State

Isolation model: immutable base + per-client differencing VHDX.
- Each client gets `{storageRoot}/clients/{clientID}/overlay.vhdx` backed by a specific base
- Assignment stored in state (clientID → baseImage, overlayPath, locked)
- Prevent duplicate assignments (one overlay per client; one client per active boot)
- On release/recovery: do not auto-delete overlays (explicit cleanup required)
- Crash/restart: assignments persisted in state.json; recovery validates paths exist and are consistent

Guarantess enforced by app: base never written; clients isolated; no cross-contamination. Target must respect exclusive LUN attachment.

## 6. Implementation Plan (incremental)

1. **Storage config types** (paths, enabled, root) in config
2. **Path validation utilities** (containment, traversal rejection)
3. **Storage backend interface** (pluggable: file ops + VHDX ops)
4. **VHDX management** (create, inspect, diff, readonly enforcement)
5. **Image/assignment state** in state.json (persisted)
6. **Admin API endpoints** (authenticated):
- `GET /api/images` - list images (bases + assignments)
- `POST /api/images` - create base image
- `GET /api/images/{id}` - inspect
- `POST /api/images/{id}/assign` - assign base to client (creates diff)
- `DELETE /api/images/assignments/{clientID}` - release assignment
- `POST /api/images/assignments/release-all` - bulk release (admin)
7. **Validation, locking, error handling** per requirements
8. **Tests** (unit + integration with temp dirs)
9. **Report + commit**

This is defensive-by-default: all destructive ops require explicit auth, paths contained, base RO enforced at app level.

## 7. Status

- Static validation: build/gofmt/vet clean; all existing server tests pass
- Environment: Windows 10 Home, no authorized iSCSI target installed (and none will be installed without approval)
- **BLOCKED for actual iSCSI attachment/boot** until authorized target selected and installed in isolated lab
- Ready to implement storage abstraction + VHDX management + admin endpoints as specified

Report to follow with implementation details and test results.