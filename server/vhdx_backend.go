package main

import "errors"

// VHDXBackend is the platform-specific backend for VHDX disk operations.
// All paths passed to backend methods must be absolute and pre-validated
// for storage-root containment by the caller. Backend implementations must
// never interpolate untrusted values into executable command text.
type VHDXBackend interface {
	// Name returns a human-readable backend name (e.g. "diskpart").
	Name() string
	// IsAvailable reports whether the backend is usable on this system.
	IsAvailable() bool
	// CreateFixed creates a fixed-size VHDX at path with sizeMB capacity.
	CreateFixed(path string, sizeMB int64) error
	// CreateExpandable creates an expandable VHDX at path with sizeMB capacity.
	CreateExpandable(path string, sizeMB int64) error
	// CreateDifferencing creates a differencing VHDX at path with parentPath
	// as its parent. The parent must already exist.
	CreateDifferencing(path string, parentPath string) error
	// Attach attaches the VHDX at path. readOnly mounts it read-only.
	Attach(path string, readOnly bool) error
	// Detach detaches the VHDX at path.
	Detach(path string) error
	// Remove deletes the VHDX file at path. The caller must ensure it is
	// detached and not in use before calling Remove.
	Remove(path string) error
	// ListAttached returns the file paths of currently attached VHDs.
	ListAttached() ([]string, error)
	// InspectAttached returns details for an attached VHD, including parent
	// file if it is a differencing disk. Returns an error if the VHD is not
	// attached.
	InspectAttached(path string) (*VHDXInfo, error)
}

// VHDXInfo describes an attached VHD as reported by the backend.
type VHDXInfo struct {
	Filename     string `json:"filename"`
	IsChild      bool   `json:"is_child"`
	ParentFile   string `json:"parent_file,omitempty"`
	VirtualSize  int64  `json:"virtual_size_bytes"`
	PhysicalSize int64  `json:"physical_size_bytes"`
	Attached     bool   `json:"attached"`
}

// ErrBackendUnavailable is returned when no VHDX backend is available.
var ErrBackendUnavailable = errors.New("no VHDX backend available")

// DetectBackend returns the best available VHDXBackend for this platform.
func DetectBackend() (VHDXBackend, error) {
	return detectBackend()
}
