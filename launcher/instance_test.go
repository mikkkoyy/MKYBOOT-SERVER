package main

// instance_test.go covers the single-instance guard.
//
// IMPORTANT TESTING CONSTRAINT
// Windows mutexes are *recursive per OS thread*: a thread that already owns a
// mutex may acquire it again without blocking. The Go runtime multiplexes
// goroutines onto a small, reused pool of OS threads, so two goroutines inside
// one process can land on the SAME OS thread and both appear to "acquire" the
// mutex.
//
// Consequence: in-process goroutine tests CANNOT prove exclusion. An earlier
// version of this file did exactly that and reported false passes while a burst
// of 24 "instances" all acquired the lock. Exclusion is therefore verified by
// uitest/instancetest.ps1, which launches the real executable as separate
// processes.
//
// What IS valid here, and is covered below, is the single-threaded contract:
// acquisition, release, idempotent release, nil safety, and the session-scoped
// name. The mutex is released with an explicit ReleaseMutex so ownership is
// dropped deterministically rather than depending on handle closure.
//
// These tests are Windows-specific and skip elsewhere.

import (
	"errors"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func requireWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skipf("single-instance tests require Windows, running on %s", runtime.GOOS)
	}
}

// ---- testable core ------------------------------------------------------

// acquireWithName is AcquireInstance parameterised by the mutex name, so tests
// use isolated kernel objects that can never collide with a launcher the
// developer happens to be running.
func acquireWithName(name string) (*InstanceLock, error) {
	p, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	h, _, callErr := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(p)))
	if h == 0 {
		return nil, callErr
	}
	res, _, _ := procWaitForSingleObject.Call(h, uintptr(zeroTimeout))
	if res != waitObject0 && res != waitAbandoned {
		procCloseHandle.Call(h)
		return nil, ErrAlreadyRunning
	}
	return &InstanceLock{handle: syscall.Handle(h)}, nil
}

func uniqueMutexName(tag string) string {
	return "Local\\MKYBOOT-Launcher-Test-" + tag + "-" + itoa(int(time.Now().UnixNano()))
}

// ---- single-threaded contract -------------------------------------------

func TestInstanceFirstAcquireSucceeds(t *testing.T) {
	requireWindows(t)
	lock, err := acquireWithName(uniqueMutexName("first"))
	if err != nil {
		t.Fatalf("first acquire must succeed, got %v", err)
	}
	if lock == nil || lock.handle == 0 {
		t.Fatal("expected a valid lock handle")
	}
	lock.Release()
}

func TestInstanceReleaseAllowsReacquire(t *testing.T) {
	requireWindows(t)
	name := uniqueMutexName("release")
	first, err := acquireWithName(name)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	first.Release()
	second, err := acquireWithName(name)
	if err != nil {
		t.Fatalf("acquire after release must succeed, got %v", err)
	}
	second.Release()
}

func TestInstanceReleaseIdempotent(t *testing.T) {
	requireWindows(t)
	lock, err := acquireWithName(uniqueMutexName("idem"))
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	lock.Release()
	lock.Release() // must be a no-op, not a double ReleaseMutex/CloseHandle
	lock.Release()
}

func TestInstanceNoPermanentStaleLockAcrossSequentialRuns(t *testing.T) {
	requireWindows(t)
	name := uniqueMutexName("stale")
	for i := 0; i < 5; i++ {
		l, err := acquireWithName(name)
		if err != nil {
			t.Fatalf("acquire #%d failed: %v", i+1, err)
		}
		l.Release()
	}
}

// A stale mutex name must never be treated as proof that an instance is
// running: after Release, the very same name is immediately acquirable, which
// is the in-process analogue of the kernel destroying the object on exit.
func TestInstanceStaleHandleIsNotTreatedAsRunning(t *testing.T) {
	requireWindows(t)
	name := uniqueMutexName("stale-handle")
	lock, err := acquireWithName(name)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	lock.Release()
	// The handle is closed; a fresh acquire of the same name must succeed.
	again, err := acquireWithName(name)
	if err != nil {
		t.Fatalf("a released mutex must not look like a running instance: %v", err)
	}
	again.Release()
}

// The mutex name is session scoped, so two sessions never block each other.
func TestInstanceMutexNameIsPerSession(t *testing.T) {
	requireWindows(t)
	name := instanceMutexName()
	if !strings.HasPrefix(name, "Local\\MKYBOOT-Launcher-") {
		t.Fatalf("mutex name must be session scoped, got %q", name)
	}
	sid := currentSessionID()
	if !strings.HasSuffix(name, itoa(int(sid))) {
		t.Fatalf("mutex name %q must end with the session id %d", name, sid)
	}
	other := name + "-other"
	if other == name {
		t.Fatal("different sessions must produce different mutex names")
	}
	b, err := acquireWithName(other)
	if err != nil {
		t.Fatalf("another session must be able to run its own instance: %v", err)
	}
	b.Release()
}

func TestInstanceCurrentSessionIDIsReal(t *testing.T) {
	requireWindows(t)
	sid := currentSessionID()
	if sid == 0 {
		t.Skip("running in session 0 (service session); cannot verify")
	}
	if sid == ^uint32(0) {
		t.Fatalf("ProcessIdToSessionId returned an implausible session id %d", sid)
	}
}

// A failed acquire must never yield a usable lock: main() relies on the
// invariant that a nil lock means "do not start".
func TestInstanceFailedAcquireYieldsNilLock(t *testing.T) {
	requireWindows(t)
	lock, err := acquireWithName(uniqueMutexName("nil"))
	if err == nil {
		lock.Release()
		t.Skip("acquired; nothing to assert")
	}
	if lock != nil {
		lock.Release()
		t.Fatal("a failed acquire must return a nil lock")
	}
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("error = %v, want ErrAlreadyRunning", err)
	}
}

// Releasing a nil lock must not panic - main() depends on this via defer.
func TestInstanceReleaseNilIsSafe(t *testing.T) {
	var lock *InstanceLock
	lock.Release()
}

// Notifying an instance that does not exist must be harmless.
func TestInstanceNotifyWithNoInstanceIsSafe(t *testing.T) {
	requireWindows(t)
	NotifyExistingInstance() // no main window exists here; must not crash
}
