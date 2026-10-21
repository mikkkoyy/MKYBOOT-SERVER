package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"time"
)

// AssignClient binds clientID to base imageID and creates a writable
// differencing disk overlay for the client. The differencing disk is created
// via the storage backend with the base image as its parent. Duplicate
// assignments for the same client are rejected. If no backend is available
// or differencing is not supported, an error is returned.
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

	if s.backend == nil || !s.backend.IsAvailable() {
		return nil, fmt.Errorf("%w: cannot create differencing overlay for client %q", errBackendRequired, clientID)
	}

	// Reserve the overlay path under root/overlays/<clientID>.avhdx. The
	// path is derived entirely from server-controlled inputs, so it cannot
	// be used for path traversal; we validate containment for defense in depth.
	overlayPath := filepath.Join(s.root, "overlays", clientID+".avhdx")
	if !s.isContained(overlayPath) {
		return nil, fmt.Errorf("computed overlay path %q escapes storage root", overlayPath)
	}

	// Create the differencing disk with the base image as its parent.
	if err := s.backend.CreateDifferencing(overlayPath, img.Path); err != nil {
		return nil, fmt.Errorf("create differencing overlay for client %q: %w", clientID, err)
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

// ReleaseAssignment removes the in-memory assignment for clientID, if any.
// The differencing disk is detached (if attached) but the overlay file is
// preserved so the client's persistent data is not destroyed. Use
// DiscardAssignment to explicitly delete the overlay file when the client's
// data should be discarded.
func (s *Storage) ReleaseAssignment(clientID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	a, exists := s.assignments[clientID]
	if !exists {
		return fmt.Errorf("%w: %q", errClientNotAssigned, clientID)
	}

	// If the overlay is attached, detach it first. The overlay file is kept
	// intact so client writes survive release.
	if s.backend != nil && s.backend.IsAvailable() {
		attached, err := s.isAttached(a.OverlayPath)
		if err != nil {
			return fmt.Errorf("check attachment for client %q: %w", clientID, err)
		}
		if attached {
			if err := s.backend.Detach(a.OverlayPath); err != nil {
				return fmt.Errorf("detach overlay for client %q: %w", clientID, err)
			}
		}
	}

	delete(s.assignments, clientID)
	s.log.Info("client image released (overlay preserved)", map[string]any{"client": clientID, "overlay": a.OverlayPath})
	return nil
}

// DiscardAssignment removes the assignment for clientID and explicitly
// deletes the client's writable overlay file. This is a destructive, explicit
// reset/discard operation: the client's persistent data in the overlay is
// lost. The operation is rejected if the overlay is attached and cannot be
// detached.
func (s *Storage) DiscardAssignment(clientID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	a, exists := s.assignments[clientID]
	if !exists {
		return fmt.Errorf("%w: %q", errClientNotAssigned, clientID)
	}

	if s.backend != nil && s.backend.IsAvailable() {
		attached, err := s.isAttached(a.OverlayPath)
		if err != nil {
			return fmt.Errorf("check attachment for client %q: %w", clientID, err)
		}
		if attached {
			if err := s.backend.Detach(a.OverlayPath); err != nil {
				return fmt.Errorf("detach overlay for client %q before discard: %w", clientID, err)
			}
		}
		// Explicitly delete the differencing disk file.
		if err := s.backend.Remove(a.OverlayPath); err != nil {
			return fmt.Errorf("remove overlay for client %q: %w", clientID, err)
		}
	}

	delete(s.assignments, clientID)
	s.log.Info("client image discarded (overlay deleted)", map[string]any{"client": clientID, "overlay": a.OverlayPath})
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
