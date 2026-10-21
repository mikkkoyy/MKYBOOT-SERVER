//go:build !linux

package main

import "fmt"

// unavailableiSCSIBackend is the iSCSITargetBackend for platforms where no
// Linux VM with LIO/targetcli is available (e.g. Windows). All operations
// return ErriSCSIBackendUnavailable.
type unavailableiSCSIBackend struct{}

// newUnavailableiSCSIBackend returns an unavailableiSCSIBackend.
func newUnavailableiSCSIBackend() *unavailableiSCSIBackend {
	return &unavailableiSCSIBackend{}
}

// Name returns the backend name.
func (b *unavailableiSCSIBackend) Name() string { return "unavailable" }

// IsAvailable always returns false.
func (b *unavailableiSCSIBackend) IsAvailable() bool { return false }

func (b *unavailableiSCSIBackend) CreateLUN(path string, sizeMB int64) error {
	return fmt.Errorf("%w: no iSCSI target backend available on this platform", ErriSCSIBackendUnavailable)
}

func (b *unavailableiSCSIBackend) RemoveLUN(path string) error {
	return fmt.Errorf("%w: no iSCSI target backend available on this platform", ErriSCSIBackendUnavailable)
}

func (b *unavailableiSCSIBackend) ExportLUN(iqn, path string, sizeMB int64, chapUser, chapSecret string) error {
	return fmt.Errorf("%w: no iSCSI target backend available on this platform", ErriSCSIBackendUnavailable)
}

func (b *unavailableiSCSIBackend) UnexportLUN(iqn string) error {
	return fmt.Errorf("%w: no iSCSI target backend available on this platform", ErriSCSIBackendUnavailable)
}

func (b *unavailableiSCSIBackend) ListTargets() ([]iSCSITargetInfo, error) {
	return nil, fmt.Errorf("%w: no iSCSI target backend available on this platform", ErriSCSIBackendUnavailable)
}

func (b *unavailableiSCSIBackend) SetACL(iqn string, initiatorIQNs []string) error {
	return fmt.Errorf("%w: no iSCSI target backend available on this platform", ErriSCSIBackendUnavailable)
}

func (b *unavailableiSCSIBackend) VerifyConnection(iqn string) error {
	return fmt.Errorf("%w: no iSCSI target backend available on this platform", ErriSCSIBackendUnavailable)
}

// detectiSCSIBackend returns an unavailableiSCSIBackend on non-Linux platforms.
func detectiSCSIBackend() (iSCSITargetBackend, error) {
	return &unavailableiSCSIBackend{}, nil
}
