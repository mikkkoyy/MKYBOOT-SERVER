package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"time"
)

// AssignClient binds clientID to base imageID, reserving a writable overlay
// path under the storage root. Duplicate assignments for the same client are
// rejected. The base image must exist and be read-only. The overlay path is
// validated to be contained within root (path traversal protection). The
// differencing disk file is not created yet; Differencing capability is
// false until a backing implementation is wired in.
func (s *Storage) AssignClient(clientID, imageID string) (*ClientAssignment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if clientID == "" {
		return nil, errors.New("client id is required")
	}
	if imageID == "" {
		return nil, errors.New("image id is required")
	}

	img, ok := s.images[imageID]
	if !ok {
		return nil, fmt.Errorf("%w: %q", errImageNotFound, imageID)
	}
	if !img.ReadOnly {
		return nil, fmt.Errorf("image %q is not a read-only base image", imageID)
	}

	if _, exists := s.assignments[clientID]; exists {
		return nil, fmt.Errorf("%w: %q", errClientAssigned, clientID)
	}

	// Reserve the overlay path under root/overlays/<clientID>.avhdx. The
	// path is derived entirely from server-controlled inputs, so it cannot
	// be used for path traversal; we validate containment for defense in depth.
	overlayPath := filepath.Join(s.root, "overlays", clientID+".avhdx")
	if !s.isContained(overlayPath) {
		return nil, fmt.Errorf("computed overlay path %q escapes storage root", overlayPath)
	}

	a := &ClientAssignment{
		ClientID:    clientID,
		ImageID:     imageID,
		OverlayPath: overlayPath,
		Writable:    true,
		AssignedAt:  time.Now(),
	}
	s.assignments[clientID] = a
	s.log.Info("client image assigned", map[string]any{"client": clientID, "image": imageID, "overlay": overlayPath})
	return a, nil
}

// ReleaseAssignment removes the assignment for clientId, if any.
func (s *Storage) ReleaseAssignment(clientID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.assignments[clientID]; !exists {
		return fmt.Errorf("%w: %q", errClientNotAssigned, clientID)
	}
	delete(s.assignments, clientID)
	s.log.Info("client image released", map[string]any{"client": clientID})
	return nil
}

// GetAssignment returns the assignment for clientID, or nil if none.
func (s *Storage) GetAssignment(clientID string) (*ClientAssignment, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.assignments[clientID]
	if !ok {
		return nil, false
	}
	return a, true
}

// ListAssignments returns a copy of all assignments, sorted by client id.
func (s *Storage) ListAssignments() []*ClientAssignment {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*ClientAssignment, 0, len(s.assignments))
	for _, a := range s.assignments {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ClientID < out[j].ClientID })
	return out
}
