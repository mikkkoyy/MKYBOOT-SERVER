//go:build !windows

package main

import "fmt"

// UnavailableBackend is the VHDXBackend for non-Windows platforms. All
// operations return ErrBackendUnavailable.
type UnavailableBackend struct{}

// NewUnavailableBackend returns an UnavailableBackend.
func NewUnavailableBackend() (*UnavailableBackend, error) {
	return &UnavailableBackend{}, nil
}

// Name returns the backend name.
func (b *UnavailableBackend) Name() string { return "unavailable" }

// IsAvailable always returns false on non-Windows platforms.
func (b *UnavailableBackend) IsAvailable() bool { return false }

func (b *UnavailableBackend) CreateFixed(path string, sizeMB int64) error {
	return fmt.Errorf("%w: backend not available on this platform", ErrBackendUnavailable)
}

func (b *UnavailableBackend) CreateExpandable(path string, sizeMB int64) error {
	return fmt.Errorf("%w: backend not available on this platform", ErrBackendUnavailable)
}

func (b *UnavailableBackend) CreateDifferencing(path string, parentPath string) error {
	return fmt.Errorf("%w: backend not available on this platform", ErrBackendUnavailable)
}

func (b *UnavailableBackend) Attach(path string, readOnly bool) error {
	return fmt.Errorf("%w: backend not available on this platform", ErrBackendUnavailable)
}

func (b *UnavailableBackend) Detach(path string) error {
	return fmt.Errorf("%w: backend not available on this platform", ErrBackendUnavailable)
}

func (b *UnavailableBackend) Remove(path string) error {
	return fmt.Errorf("%w: backend not available on this platform", ErrBackendUnavailable)
}

func (b *UnavailableBackend) ListAttached() ([]string, error) {
	return nil, fmt.Errorf("%w: backend not available on this platform", ErrBackendUnavailable)
}

func (b *UnavailableBackend) InspectAttached(path string) (*VHDXInfo, error) {
	return nil, fmt.Errorf("%w: backend not available on this platform", ErrBackendUnavailable)
}

// detectBackend returns an UnavailableBackend on non-Windows platforms.
func detectBackend() (VHDXBackend, error) {
	return NewUnavailableBackend()
}
