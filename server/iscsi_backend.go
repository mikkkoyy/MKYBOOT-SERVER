package main

import (
	"errors"
	"fmt"
	"regexp"
)

// iSCSITargetBackend is the interface for managing iSCSITargetBackend targets and LUNs.
// The actual iSCSI target runs on a separate Linux VM (e.g. LIO/targetcli
// on Ubuntu). This interface abstracts the management protocol used to
// control that remote target. All paths must be pre-validated and absolute.
// Backend implementations must never interpolate untrusted values into
// executable command text.
type iSCSITargetBackend interface {
	// Name returns a human-readable backend name (e.g. "linux-lio").
	Name() string
	// IsAvailable reports whether the backend is usable on this system.
	IsAvailable() bool
	// CreateLUN creates a new block-backed LUN file at path with sizeMB
	// capacity. The file must not already exist.
	CreateLUN(path string, sizeMB int64) error
	// RemoveLUN deletes the LUN file at path. The LUN must be unexported
	// first.
	RemoveLUN(path string) error
	// ExportLUN exports the LUN at path as an iSCSI target with the given
	// IQN. chapUser/chapSecret may be empty to disable CHAP authentication.
	ExportLUN(iqn, path string, sizeMB int64, chapUser, chapSecret string) error
	// UnexportLUN removes the iSCSI export for the given IQN. The LUN file
	// is not deleted.
	UnexportLUN(iqn string) error
	// ListTargets returns the currently exported iSCSI targets.
	ListTargets() ([]iSCSITargetInfo, error)
	// SetACL restricts access to the given IQN to the specified initiator
	// IQNs. An empty list allows all initiators.
	SetACL(iqn string, initiatorIQNs []string) error
	// VerifyConnection attempts a loopback iSCSI connection to confirm the
	// target is actually reachable and functional. Returns an error if the
	// target cannot be verified.
	VerifyConnection(iqn string) error
}

// iSCSITargetInfo describes an exported iSCSI target.
type iSCSITargetInfo struct {
	IQN         string   `json:"iqn"`
	LUNPath     string   `json:"lun_path"`
	SizeBytes   int64    `json:"size_bytes"`
	Exported    bool     `json:"exported"`
	Initiators  []string `json:"initiators"`
	CHAPEnabled bool     `json:"chap_enabled"`
	Verified    bool     `json:"verified"`
}

// iSCSIHealthState reports the verification state of the iSCSI target.
type iSCSIHealthState int

const (
	// iSCSIStateUnavailable means no iSCSI target backend is available.
	iSCSIStateUnavailable iSCSIHealthState = iota
	// iSCSIStateConfigured means a backend is present but not yet verified.
	iSCSIStateConfigured
	// iSCSIStateVerified means a loopback connection to the target succeeded.
	iSCSIStateVerified
)

func (s iSCSIHealthState) String() string {
	switch s {
	case iSCSIStateUnavailable:
		return "unavailable"
	case iSCSIStateConfigured:
		return "configured"
	case iSCSIStateVerified:
		return "verified"
	default:
		return "unknown"
	}
}

// ErrBackendUnavailable is returned when no iSCSI target backend is available.
var ErriSCSIBackendUnavailable = errors.New("no iSCSI target backend available")

// validIQNRE matches the prefix of a valid iSCSI IQN: iqn.yyyy-mm.naming-authority
var validIQNRE = regexp.MustCompile(`^iqn\.\d{4}-\d{2}\..+`)

// validateIQN checks that an iSCSI IQN is well-formed.
func validateIQN(iqn string) error {
	if iqn == "" {
		return errors.New("iSCSI IQN is required")
	}
	if len(iqn) > 255 {
		return fmt.Errorf("iSCSI IQN too long (%d > 255)", len(iqn))
	}
	if !validIQNRE.MatchString(iqn) {
		return fmt.Errorf("iSCSI IQN %q must match iqn.yyyy-mm.naming-authority", iqn)
	}
	return nil
}

// DetectiSCSIBackend returns the best available iSCSITargetBackend for this
// platform. On Linux it returns a linuxLIOBackend configured for the remote
// VM; on other platforms it returns an unavailableiSCSIBackend.
func DetectiSCSIBackend() (iSCSITargetBackend, error) {
	return detectiSCSIBackend()
}
