package main

import (
	"os"
	"path/filepath"
)

// ProgramDataRoot returns the per-machine data root for MKYBOOT.
//
// On Windows this is %ProgramData%\MKYBOOT. The MKYBOOT_DATA_DIR environment
// variable overrides it, which lets tests and non-Windows development run
// against a temporary directory without elevated privileges.
func ProgramDataRoot() string {
	if override := os.Getenv("MKYBOOT_DATA_DIR"); override != "" {
		return override
	}
	if pd := os.Getenv("ProgramData"); pd != "" {
		return filepath.Join(pd, "MKYBOOT")
	}
	// Fallback for platforms without %ProgramData% (e.g. Linux dev/test).
	if cfg, err := os.UserConfigDir(); err == nil {
		return filepath.Join(cfg, "MKYBOOT")
	}
	return filepath.Join(os.TempDir(), "MKYBOOT")
}

// DefaultConfigPath is the configuration file under the data root.
func DefaultConfigPath() string { return filepath.Join(ProgramDataRoot(), "config.json") }

// DefaultStatePath is the persisted runtime state file (clients, sessions,
// lock state) under the data root.
func DefaultStatePath() string { return filepath.Join(ProgramDataRoot(), "state.json") }

// DefaultLogPath is the structured log file under the data root.
func DefaultLogPath() string { return filepath.Join(ProgramDataRoot(), "mkyboot-server.log") }

// ensureDir creates dir (and parents) with restrictive 0700 permissions.
func ensureDir(dir string) error {
	return os.MkdirAll(dir, 0o700)
}

// openAppend opens path for append, creating it with 0600 if it does not
// exist. Existing files keep their current permissions.
func openAppend(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
}
