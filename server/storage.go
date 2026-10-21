package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ImageFormat identifies the on-disk image format.
type ImageFormat int

const (
	ImageFormatUnknown ImageFormat = iota
	ImageFormatVHDX
	ImageFormatVHD
	ImageFormatRAW
)

func (f ImageFormat) String() string {
	switch f {
	case ImageFormatVHDX:
		return "vhdx"
	case ImageFormatVHD:
		return "vhd"
	case ImageFormatRAW:
		return "raw"
	default:
		return "unknown"
	}
}

// ImageStatus tracks the lifecycle state of a base image.
type ImageStatus int

const (
	ImageStatusUnknown     ImageStatus = iota
	ImageStatusReady                   // base image is present and read-only
	ImageStatusLocked                  // base image is in use by differencing disks
	ImageStatusUnavailable             // missing or corrupt
)

func (s ImageStatus) String() string {
	switch s {
	case ImageStatusReady:
		return "ready"
	case ImageStatusLocked:
		return "locked"
	case ImageStatusUnavailable:
		return "unavailable"
	default:
		return "unknown"
	}
}

// StorageCapabilities reports what the storage backend can actually do.
// VHDX creation, differencing disks, block isolation, and iSCSI export are
// intentionally false until a backing implementation is wired in, so callers
// never assume a capability that would bypass write isolation.
type StorageCapabilities struct {
	VHDX           bool `json:"vhdx"`
	Differencing   bool `json:"differencing"`
	BlockIsolation bool `json:"block_isolation"`
	ISCSI          bool `json:"iscsi"`
}

// BaseImage describes a registered read-only base image.
type BaseImage struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	Path      string      `json:"path"` // absolute, contained path
	Format    ImageFormat `json:"format"`
	Status    ImageStatus `json:"status"`
	SizeBytes int64       `json:"size_bytes"`
	ReadOnly  bool        `json:"read_only"`
	CreatedAt time.Time   `json:"created_at"`
}

// ClientAssignment binds a client to a writable overlay (differencing) image.
type ClientAssignment struct {
	ClientID    string    `json:"client_id"`
	ImageID     string    `json:"image_id"`     // base image id
	OverlayPath string    `json:"overlay_path"` // reserved differencing disk path
	Writable    bool      `json:"writable"`
	AssignedAt  time.Time `json:"assigned_at"`
}

// StorageHealth is the storage subsystem health report.
type StorageHealth struct {
	OK          bool   `json:"ok"`
	Message     string `json:"message,omitempty"`
	Images      int    `json:"images"`
	Assignments int    `json:"assignments"`
	// ISCSIState reports whether the iSCSI target backend is available,
	// configured, or verified via a loopback connection.
	ISCSIState string `json:"iscsi_state,omitempty"`
}

// sentinel errors for storage operations.
var (
	errImageNotFound     = errors.New("image not found")
	errClientAssigned    = errors.New("client already assigned")
	errClientNotAssigned = errors.New("client not assigned")
)

// Storage manages base images and per-client writable overlays. All image
// paths are strictly contained under root to prevent path traversal.
type Storage struct {
	mu           sync.Mutex
	root         string
	cap          StorageCapabilities
	backend      VHDXBackend
	iscsiBackend iSCSITargetBackend
	iscsiState   iSCSIHealthState
	images       map[string]*BaseImage
	assignments  map[string]*ClientAssignment
	log          *Logger
}

// NewStorage creates a storage backend rooted at root. The root is created
// with restrictive permissions if missing. The VHDX backend is auto-detected;
// capabilities are set according to what the backend actually supports.
func NewStorage(root string, log *Logger, iscsiCfg ISCSIConfig) (*Storage, error) {
	if log == nil {
		log = NewLogger(io.Discard)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("storage root %q: %w", root, err)
	}
	if err := ensureDir(abs); err != nil {
		return nil, fmt.Errorf("storage root %q: %w", abs, err)
	}

	iscsiBackend, iscsiErr := DetectiSCSIBackend(iscsiCfg)
	if iscsiErr != nil {
		iscsiBackend = nil
	}

	vhdxBackend, err := DetectBackend()
	if err != nil {
		vhdxBackend = nil
	}

	s := &Storage{
		root:         abs,
		images:       map[string]*BaseImage{},
		assignments:  map[string]*ClientAssignment{},
		log:          log,
		backend:      vhdxBackend,
		iscsiBackend: iscsiBackend,
		iscsiState:   iSCSIStateUnavailable,
	}

	s.cap = StorageCapabilities{
		VHDX:           vhdxBackend != nil && vhdxBackend.IsAvailable(),
		Differencing:   vhdxBackend != nil && vhdxBackend.IsAvailable(),
		BlockIsolation: false,
		ISCSI:          false,
	}
	if vhdxBackend != nil {
		s.log.Info("storage backend detected", map[string]any{"backend": vhdxBackend.Name(), "available": vhdxBackend.IsAvailable()})
	} else {
		s.log.Info("no VHDX backend available", map[string]any{"vhdx": false, "differencing": false})
	}
	if iscsiBackend != nil {
		s.log.Info("iSCSI target backend detected", map[string]any{"backend": iscsiBackend.Name(), "available": iscsiBackend.IsAvailable()})
	} else {
		s.log.Info("no iSCSI target backend available", map[string]any{"iscsi": false})
	}
	if iscsiErr != nil {
		s.log.Error("iSCSI target backend rejected the configuration", map[string]any{"error": iscsiErr.Error()})
	}
	return s, nil
}

// Capabilities returns the storage backend's capabilities.
func (s *Storage) Capabilities() StorageCapabilities {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cap
}

// Root returns the absolute storage root.
func (s *Storage) Root() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.root
}

// isContained reports whether abs is rooted under s.root. Comparison is
// case-insensitive (Windows volumes are case-insensitive) and the character
// after the root prefix must be a separator or end-of-string, so
// C:\Storage does not falsely contain C:\StorageEvil.
func (s *Storage) isContained(abs string) bool {
	root := strings.ToLower(s.root)
	path := strings.ToLower(abs)
	if !strings.HasPrefix(path, root) {
		return false
	}
	if len(path) == len(root) {
		return true
	}
	return path[len(root)] == os.PathSeparator
}

// IsPathContained reports whether the given path (after cleaning and
// absolutization) is contained within the storage root.
func (s *Storage) IsPathContained(p string) bool {
	abs, err := filepath.Abs(p)
	if err != nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.isContained(abs)
}

// RegisterExisting registers an on-disk base image at path as read-only.
// The path must be contained within the storage root. Returns an error if
// the id is empty, already registered, the path is outside the root, the file
// does not exist, or the path is a directory.
func (s *Storage) RegisterExisting(id, name, path string) (*BaseImage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if id == "" {
		return nil, errors.New("image id is required")
	}
	if _, ok := s.images[id]; ok {
		return nil, fmt.Errorf("image %q already registered", id)
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("image path %q: %w", path, err)
	}
	if !s.isContained(abs) {
		return nil, fmt.Errorf("image path %q is outside storage root %q", path, s.root)
	}

	fi, err := os.Stat(abs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("image path %q does not exist", path)
		}
		return nil, fmt.Errorf("stat image %q: %w", path, err)
	}
	if fi.IsDir() {
		return nil, fmt.Errorf("image path %q is a directory, not a file", path)
	}

	fmt := ImageFormatUnknown
	ext := strings.ToLower(filepath.Ext(abs))
	switch ext {
	case ".vhdx":
		fmt = ImageFormatVHDX
	case ".vhd":
		fmt = ImageFormatVHD
	case ".raw", ".img":
		fmt = ImageFormatRAW
	}

	img := &BaseImage{
		ID:        id,
		Name:      name,
		Path:      abs,
		Format:    fmt,
		Status:    ImageStatusReady,
		SizeBytes: fi.Size(),
		ReadOnly:  true,
		CreatedAt: time.Now(),
	}
	s.images[id] = img
	s.log.Info("base image registered", map[string]any{"id": id, "path": abs, "format": fmt.String()})
	return img, nil
}

// GetImage returns the base image with the given id, or nil if not found.
func (s *Storage) GetImage(id string) (*BaseImage, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	img, ok := s.images[id]
	if !ok {
		return nil, false
	}
	return img, true
}

// ListImages returns a copy of all registered base images, sorted by id.
func (s *Storage) ListImages() []*BaseImage {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*BaseImage, 0, len(s.images))
	for _, img := range s.images {
		out = append(out, img)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
