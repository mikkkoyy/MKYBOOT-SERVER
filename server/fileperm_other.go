//go:build !windows

package main

import "errors"

// On non-Windows platforms the atomic writer already applies a 0600 mode, so
// there is no extra ACL step. restrictFileToOwner is a no-op and fileIsOwnerOnly
// defers to the POSIX mode check performed by the caller's tests.
func restrictFileToOwner(path string) error { return nil }

func fileIsOwnerOnly(path string) (bool, error) {
	return false, errors.New("DACL check is only available on Windows")
}
