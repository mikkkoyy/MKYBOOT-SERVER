package main

import (
	"os"
	"path/filepath"
)

// atomicWriteFile writes data to path atomically: it writes to a temporary
// file in the same directory, fsyncs it, sets restrictive permissions, then
// renames it over the target. A crash mid-write can never leave a truncated
// config or state file behind. The parent directory is created 0700 so that
// persisted state (client tokens, sessions, lock state) is not world-readable.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// Best-effort cleanup of the temp file if any step below fails.
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	// On Windows, os.Chmod only toggles the read-only attribute and cannot
	// express 0600. Apply an owner-only DACL so persisted secrets/state are not
	// readable by other users. On other platforms this is a no-op (the 0600
	// mode above already applies).
	if err := restrictFileToOwner(tmpName); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	return nil
}
