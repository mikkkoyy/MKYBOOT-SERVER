//go:build linux

package main

import "fmt"

// linuxLIOBackend manages iSCSI targets via LIO/targetcli on a Linux VM.
// This is a stub interface definition; actual targetcli integration
// requires SSH or a management API to the Linux VM and is not implemented
// in Phase 4E because no Linux VM was available for validation.
type linuxLIOBackend struct {
	host    string
	sshPort int
	sshUser string
	sshKey  string // path to private key; secret, never logged
}

// newLinuxLIOBackend creates a linuxLIOBackend configured to manage a
// remote Linux VM running targetcli/LIO.
func newLinuxLIOBackend(host, user, keyPath string, port int) *linuxLIOBackend {
	return &linuxLIOBackend{
		host:    host,
		sshPort: port,
		sshUser: user,
		sshKey:  keyPath,
	}
}

// Name returns the backend name.
func (b *linuxLIOBackend) Name() string { return "linux-lio" }

// IsAvailable reports whether the backend can reach the Linux VM.
// In Phase 4E this always returns false because no SSH connection to a
// Linux VM was established for validation.
func (b *linuxLIOBackend) IsAvailable() bool { return false }

func (b *linuxLIOBackend) CreateLUN(path string, sizeMB int64) error {
	return fmt.Errorf("%w: linux-lio backend not available", ErriSCSIBackendUnavailable)
}

func (b *linuxLIOBackend) RemoveLUN(path string) error {
	return fmt.Errorf("%w: linux-lio backend not available", ErriSCSIBackendUnavailable)
}

func (b *linuxLIOBackend) ExportLUN(iqn, path string, sizeMB int64, chapUser, chapSecret string) error {
	return fmt.Errorf("%w: linux-lio backend not available", ErriSCSIBackendUnavailable)
}

func (b *linuxLIOBackend) UnexportLUN(iqn string) error {
	return fmt.Errorf("%w: linux-lio backend not available", ErriSCSIBackendUnavailable)
}

func (b *linuxLIOBackend) ListTargets() ([]iSCSITargetInfo, error) {
	return nil, fmt.Errorf("%w: linux-lio backend not available", ErriSCSIBackendUnavailable)
}

func (b *linuxLIOBackend) SetACL(iqn string, initiatorIQNs []string) error {
	return fmt.Errorf("%w: linux-lio backend not available", ErriSCSIBackendUnavailable)
}

func (b *linuxLIOBackend) VerifyConnection(iqn string) error {
	return fmt.Errorf("%w: linux-lio backend not available", ErriSCSIBackendUnavailable)
}

// detectiSCSIBackend returns a linuxLIOBackend. It is not available until a
// remote Linux VM is configured and reachable.
func detectiSCSIBackend() (iSCSITargetBackend, error) {
	return &linuxLIOBackend{}, nil
}
