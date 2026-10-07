package main

// instance.go enforces "one interactive launcher per Windows user session".
//
// Mechanism: a named Win32 mutex. CreateMutexW with a fixed name creates a
// kernel object owned by the creating process; a second CreateMutexW call with
// bInitialOwner=TRUE fails with ERROR_ALREADY_EXISTS (183) while the first
// process is alive. When the owning process exits - cleanly or by crashing -
// Windows destroys the object, so a stale lock is impossible by construction.
//
// Deliberate design choices:
//   - No PID file. A PID file can survive a crash and would then be a false
//     "already running" signal, which is exactly the failure mode to avoid.
//   - No fixed TCP port. The port is configurable and belongs to the MKYBOOT
//     server; binding it would collide with the server and change its meaning.
//   - Scope is per session. The mutex name carries the session id, so one
//     instance per interactive session (typically per logged-in user) and
//     several sessions may each run their own launcher without interfering.
//
// The second instance notifies the first (restore + focus) and exits. No IPC
// protocol is introduced: forwarding commands is out of scope for this change.

import (
	"errors"
	"sync"
	"syscall"
	"unsafe"
)

const (
	// WaitForSingleObject return values.
	waitObject0   = 0x00000000 // acquired
	waitAbandoned = 0x00000080 // acquired; previous owner died holding it
	waitTimeout   = 0x00000102 // still owned by someone else
	waitFailed    = 0xFFFFFFFF // error; check GetLastError
	zeroTimeout   = 0          // do not block, just test ownership
)

// instanceMutexName is the kernel object name for the current session.
func instanceMutexName() string {
	sid := currentSessionID()
	return "Local\\MKYBOOT-Launcher-" + itoa(int(sid))
}

// InstanceLock owns the single-instance mutex.
type InstanceLock struct {
	handle syscall.Handle
	once   sync.Once
}

// ErrAlreadyRunning reports that another interactive instance holds the lock.
var ErrAlreadyRunning = errors.New("another MKYBOOT Launcher instance is already running")

// AcquireInstance tries to become the single interactive instance.
//
// Returns ErrAlreadyRunning when another live process owns the mutex. Any
// other failure is returned as an error rather than being ignored, because
// silently continuing would allow duplicate instances - the one outcome this
// function exists to prevent.
func AcquireInstance() (*InstanceLock, error) {
	name, err := syscall.UTF16PtrFromString(instanceMutexName())
	if err != nil {
		return nil, err
	}
	// bInitialOwner = TRUE so the mutex is owned immediately and cannot be
	// acquired by another instance; lpSecurityAttributes = NULL.
	// CreateMutexW with bInitialOwner=FALSE only creates or opens the object;
	// it does not take ownership. Ownership must be established separately.
	//
	// GetLastError is deliberately NOT consulted: ERROR_ALREADY_EXISTS only
	// means "an object with this name exists", which is true even when the
	// previous owner exited but some handle is still open. Ownership is the
	// question that matters, and only the wait below answers it.
	h, _, callErr := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if h == 0 {
		return nil, errors.New("cannot create single-instance mutex: " + callErr.Error())
	}

	res, _, _ := procWaitForSingleObject.Call(h, uintptr(zeroTimeout))
	switch res {
	case waitObject0:
		// We hold the mutex.
	case waitAbandoned:
		// The previous owner died while holding it. Windows has already
		// transferred ownership to us, which is exactly the crash-recovery
		// behaviour we want: no stale lock is possible.
	default:
		// waitTimeout: another live instance owns it.
		// waitFailed: treat as unavailable rather than risking a duplicate.
		procCloseHandle.Call(h)
		return nil, ErrAlreadyRunning
	}
	return &InstanceLock{handle: syscall.Handle(h)}, nil
}

// Release drops ownership. Safe to call more than once.
func (l *InstanceLock) Release() {
	if l == nil {
		return
	}
	l.once.Do(func() {
		if l.handle != 0 {
			// Release ownership first: closing the last handle would drop it
			// anyway, but being explicit keeps the state obvious and lets a
			// second instance start immediately.
			procReleaseMutex.Call(uintptr(l.handle))
			procCloseHandle.Call(uintptr(l.handle))
			l.handle = 0
		}
	})
}

// processIDToSessionID is the syscall.Errno wrapper the standard library does
// not expose. Returns nil on success.
func processIDToSessionID(pid uint32, sid *uint32) error {
	r, _, err := procProcessIdToSessionId.Call(uintptr(pid), uintptr(unsafe.Pointer(sid)))
	if r == 0 {
		return err
	}
	return nil
}

// currentSessionID returns the Windows session id of this process.
//
// This is deliberately the process's own session rather than a per-user
// constant: two sessions of the same user (RDP + console, for example) each
// get their own launcher, which matches the "one interactive instance per
// session" scope. Falls back to 0 if the call fails, which simply puts every
// session into one shared scope - a stricter, still safe, degradation.
func currentSessionID() uint32 {
	var sid uint32
	if err := processIDToSessionID(uint32(syscall.Getpid()), &sid); err == nil {
		return sid
	}
	return 0
}

// NotifyExistingInstance asks an already-running launcher to come to the front.
// Best effort: failure is not reported as an error because the second instance
// is exiting regardless.
func NotifyExistingInstance() bool {
	cls, err := syscall.UTF16PtrFromString(mainWindowClassName)
	if err != nil {
		return false
	}
	hwnd, _, _ := procFindWindowW.Call(
		uintptr(unsafe.Pointer(cls)), 0)
	if hwnd == 0 {
		return false
	}
	ret, _, _ := procPostMessageWToWindow.Call(hwnd, uintptr(wmAppForeground), 0, 0)
	return ret != 0
}
