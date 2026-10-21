package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// validImageID matches safe image identifiers: alphanumeric with hyphens and
// underscores, starting with a letter or digit. This prevents path injection
// through image IDs used in filesystem paths.
var validImageID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

const (
	maxImageIDLen   = 64
	maxImageNameLen = 128
	minImageSizeMB  = 1
	maxImageSizeMB  = 2 * 1024 * 1024 // 2 TiB
)

// sentinel errors for VHDX image operations.
var (
	errImageIDInvalid       = errors.New("invalid image id")
	errImageAlreadyExists   = errors.New("image already exists")
	errImageInUse           = errors.New("image is in use by one or more client assignments")
	errImageAttached        = errors.New("image is currently attached")
	errBackendRequired      = errors.New("VHDX backend not available")
	errISCISBackendRequired = errors.New("iSCSI target backend not available")
)

// Backend returns the storage's VHDX backend, or nil if none is available.
func (s *Storage) Backend() VHDXBackend {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.backend
}

// BackendName returns the backend name, or "none" if no backend is available.
func (s *Storage) BackendName() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.backend == nil {
		return "none"
	}
	return s.backend.Name()
}

// BackendAvailable reports whether a VHDX backend is available.
func (s *Storage) BackendAvailable() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.backend != nil && s.backend.IsAvailable()
}

// iSCSIBackend returns the storage's iSCSI target backend, or nil if none is available.
func (s *Storage) iSCSIBackend() iSCSITargetBackend {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.iscsiBackend
}

// iSCSIBackendName returns the iSCSI backend name, or "none" if no backend is available.
func (s *Storage) iSCSIBackendName() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.iscsiBackend == nil {
		return "none"
	}
	return s.iscsiBackend.Name()
}

// iSCSIBackendAvailable reports whether an iSCSI target backend is available.
func (s *Storage) iSCSIBackendAvailable() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.iscsiBackend != nil && s.iscsiBackend.IsAvailable()
}

// iSCSIState returns the current iSCSI health state.
func (s *Storage) iSCSIState() iSCSIHealthState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.iscsiState
}

// setISCSIState updates the iSCSI health state. Caller must not hold s.mu.
func (s *Storage) setISCSIState(state iSCSIHealthState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.iscsiState = state
}

// validateImageID checks that an image ID is safe and well-formed.
func validateImageID(id string) error {
	if id == "" {
		return errors.New("image id is required")
	}
	if len(id) > maxImageIDLen {
		return fmt.Errorf("image id too long (%d > %d)", len(id), maxImageIDLen)
	}
	if !validImageID.MatchString(id) {
		return fmt.Errorf("image id %q must contain only letters, digits, hyphens, and underscores, and start with a letter or digit", id)
	}
	return nil
}

// validateImageName checks that an image name is present and well-formed.
func validateImageName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("image name is required")
	}
	if len(name) > maxImageNameLen {
		return fmt.Errorf("image name too long (%d > %d)", len(name), maxImageNameLen)
	}
	return nil
}

// imageExtForFormat returns the expected file extension for a format.
func imageExtForFormat(f ImageFormat) string {
	switch f {
	case ImageFormatVHDX:
		return ".vhdx"
	case ImageFormatVHD:
		return ".vhd"
	case ImageFormatRAW:
		return ".raw"
	default:
		return ""
	}
}

// CreateImage creates a new VHDX base image at path and registers it.
// The image is created via the storage backend and registered as read-only.
// If parentPath is non-empty, a differencing disk is created with the given
// parent. Returns an error if no backend is available, the id is invalid or
// duplicate, the path is outside the root or has the wrong extension, the
// file already exists, or the backend fails to create the image.
func (s *Storage) CreateImage(id, name, path string, format ImageFormat, sizeMB int64, parentPath ...string) (*BaseImage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := validateImageID(id); err != nil {
		return nil, err
	}
	if err := validateImageName(name); err != nil {
		return nil, err
	}
	if _, ok := s.images[id]; ok {
		return nil, fmt.Errorf("%w: %q", errImageAlreadyExists, id)
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("image path %q: %w", path, err)
	}
	if !s.isContained(abs) {
		return nil, fmt.Errorf("image path %q is outside storage root %q", path, s.root)
	}

	ext := strings.ToLower(filepath.Ext(abs))
	expectedExt := imageExtForFormat(format)
	if expectedExt == "" {
		return nil, fmt.Errorf("unsupported image format %q", format.String())
	}
	if ext != expectedExt {
		return nil, fmt.Errorf("image path %q has extension %q, expected %q for format %s", abs, ext, expectedExt, format.String())
	}

	if _, err := os.Stat(abs); err == nil {
		return nil, fmt.Errorf("image path %q already exists", abs)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("stat image path %q: %w", abs, err)
	}

	if s.backend == nil || !s.backend.IsAvailable() {
		return nil, fmt.Errorf("%w: cannot create image %q", errBackendRequired, id)
	}

	var isDiff bool
	var diffParent string
	if len(parentPath) > 0 {
		diffParent = parentPath[0]
		if diffParent != "" {
			isDiff = true
		}
	}

	if !isDiff {
		if sizeMB < minImageSizeMB || sizeMB > maxImageSizeMB {
			return nil, fmt.Errorf("size_mb must be between %d and %d", minImageSizeMB, maxImageSizeMB)
		}
	}

	if isDiff {
		// Validate that the parent image exists and is read-only.
		parentImg, ok := s.imagesByPath(diffParent)
		if !ok {
			return nil, fmt.Errorf("parent image %q is not registered", diffParent)
		}
		if !parentImg.ReadOnly {
			return nil, fmt.Errorf("parent image %q is not read-only", diffParent)
		}
		if err := s.backend.CreateDifferencing(abs, diffParent); err != nil {
			return nil, fmt.Errorf("create differencing image %q: %w", id, err)
		}
	} else {
		if err := s.backend.CreateExpandable(abs, sizeMB); err != nil {
			return nil, fmt.Errorf("create image %q: %w", id, err)
		}
	}

	fi, err := os.Stat(abs)
	if err != nil {
		// Best-effort cleanup if we can't stat the newly created file.
		_ = s.backend.Remove(abs)
		return nil, fmt.Errorf("stat created image %q: %w", abs, err)
	}

	img := &BaseImage{
		ID:        id,
		Name:      name,
		Path:      abs,
		Format:    format,
		Status:    ImageStatusReady,
		SizeBytes: fi.Size(),
		ReadOnly:  true,
		CreatedAt: time.Now(),
	}
	s.images[id] = img
	s.log.Info("base image created", map[string]any{"id": id, "path": abs, "format": format.String(), "size_bytes": fi.Size(), "differencing": isDiff})
	return img, nil
}

// imagesByPath finds a registered image by its absolute path. Caller must
// hold s.mu.
func (s *Storage) imagesByPath(path string) (*BaseImage, bool) {
	for _, img := range s.images {
		if filepathEqual(img.Path, path) {
			return img, true
		}
	}
	return nil, false
}

// RemoveImage removes a base image and its file. The image must not be
// assigned to any client and must not be attached. Returns an error if no
// backend is available, the image is not found, or the image is in use.
func (s *Storage) RemoveImage(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	img, ok := s.images[id]
	if !ok {
		return fmt.Errorf("%w: %q", errImageNotFound, id)
	}

	// Reject removal if any client is assigned to this image.
	for _, a := range s.assignments {
		if a.ImageID == id {
			return fmt.Errorf("%w: %q", errImageInUse, id)
		}
	}

	// Reject removal if the VHDX is currently attached.
	if s.backend != nil && s.backend.IsAvailable() {
		attached, err := s.isAttached(img.Path)
		if err != nil {
			return fmt.Errorf("check attachment for image %q: %w", id, err)
		}
		if attached {
			return fmt.Errorf("%w: %q", errImageAttached, id)
		}
	}

	if err := s.backend.Remove(img.Path); err != nil {
		return fmt.Errorf("remove image %q: %w", id, err)
	}

	delete(s.images, id)
	s.log.Info("base image removed", map[string]any{"id": id, "path": img.Path})
	return nil
}

// InspectImage returns metadata about an image, including whether it is
// attached and, if so, its parent file (for differencing disks). Returns an
// error if the image is not found.
func (s *Storage) InspectImage(id string) (*BaseImage, *VHDXInfo, error) {
	s.mu.Lock()
	img, ok := s.images[id]
	if !ok {
		s.mu.Unlock()
		return nil, nil, fmt.Errorf("%w: %q", errImageNotFound, id)
	}
	s.mu.Unlock()

	info := &VHDXInfo{
		Filename: img.Path,
		Attached: false,
	}

	if s.backend != nil && s.backend.IsAvailable() {
		attached, err := s.isAttached(img.Path)
		if err != nil {
			return nil, nil, fmt.Errorf("check attachment for image %q: %w", id, err)
		}
		if attached {
			vinfo, err := s.backend.InspectAttached(img.Path)
			if err != nil {
				return nil, nil, fmt.Errorf("inspect attached image %q: %w", id, err)
			}
			info = vinfo
			info.Attached = true
		}
	}

	// Update size from filesystem.
	if fi, err := os.Stat(img.Path); err == nil {
		img.SizeBytes = fi.Size()
	}

	return img, info, nil
}

// isAttached reports whether the VHDX at path is currently attached.
func (s *Storage) isAttached(path string) (bool, error) {
	if s.backend == nil || !s.backend.IsAvailable() {
		return false, nil
	}
	attached, err := s.backend.ListAttached()
	if err != nil {
		return false, err
	}
	for _, p := range attached {
		if filepathEqual(p, path) {
			return true, nil
		}
	}
	return false, nil
}

// filepathEqual compares two file paths for equality, case-insensitively on
// Windows.
func filepathEqual(a, b string) bool {
	return strings.EqualFold(a, b)
}

// isImageAssigned reports whether any client assignment references imageID.
// Caller must hold s.mu.
func (s *Storage) isImageAssigned(imageID string) bool {
	for _, a := range s.assignments {
		if a.ImageID == imageID {
			return true
		}
	}
	return false
}
